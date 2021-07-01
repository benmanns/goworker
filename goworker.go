package goworker

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/go-redis/redis/v7"

	"golang.org/x/net/context"

	"github.com/cihub/seelog"
)

var (
	logger      seelog.LoggerInterface
	client      *redis.Client
	ctx         context.Context
	initMutex   sync.Mutex
	initialized bool
)

const (
	keyForCleaningExpiredRetries = "cleaning_expired_retried_in_progress"
)

var (
	cleaningExpiredRetriesInterval = time.Minute
)

var workerSettings WorkerSettings

type WorkerSettings struct {
	QueuesString   string
	Queues         queuesFlag
	IntervalFloat  float64
	Interval       intervalFlag
	Concurrency    int
	Connections    int
	URI            string
	Namespace      string
	ExitOnComplete bool
	IsStrict       bool
	UseNumber      bool
	SkipTLSVerify  bool
	TLSCertPath    string
	MaxAgeRetries  time.Duration

	closed chan struct{}
}

func SetSettings(settings WorkerSettings) {
	workerSettings = settings
}

// Init initializes the goworker process. This will be
// called by the Work function, but may be used by programs
// that wish to access goworker functions and configuration
// without actually processing jobs.
func Init() error {
	initMutex.Lock()
	defer initMutex.Unlock()
	if !initialized {
		var err error
		logger, err = seelog.LoggerFromWriterWithMinLevel(os.Stdout, seelog.InfoLvl)
		if err != nil {
			return err
		}

		if err := flags(); err != nil {
			return err
		}
		ctx = context.Background()

		opts, err := redis.ParseURL(workerSettings.URI)
		if err != nil {
			return err
		}

		if len(workerSettings.TLSCertPath) > 0 {
			certPool, err := getCertPool()
			if err != nil {
				return err
			}
			opts.TLSConfig = &tls.Config{
				RootCAs:            certPool,
				InsecureSkipVerify: workerSettings.SkipTLSVerify,
			}
		}

		client = redis.NewClient(opts).WithContext(ctx)
		err = client.Ping().Err()
		if err != nil {
			return err
		}

		workerSettings.closed = make(chan struct{})

		initialized = true
	}

	return nil
}

func getCertPool() (*x509.CertPool, error) {
	rootCAs, _ := x509.SystemCertPool()
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}
	certs, err := ioutil.ReadFile(workerSettings.TLSCertPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %q for the RootCA pool: %v", workerSettings.TLSCertPath, err)
	}
	if ok := rootCAs.AppendCertsFromPEM(certs); !ok {
		return nil, fmt.Errorf("failed to append %q to the RootCA pool: %v", workerSettings.TLSCertPath, err)
	}
	return rootCAs, nil
}

// Close cleans up resources initialized by goworker. This
// will be called by Work when cleaning up. However, if you
// are using the Init function to access goworker functions
// and configuration without processing jobs by calling
// Work, you should run this function when cleaning up.
func Close() error {
	initMutex.Lock()
	defer initMutex.Unlock()
	if initialized {
		err := client.Close()
		if err != nil {
			return err
		}
		initialized = false
		close(workerSettings.closed)
	}

	return nil
}

// Closed will return a channel that will be
// closed once the full process is done closing
// and cleaning all the workers
func Closed() <-chan struct{} {
	return workerSettings.closed
}

// Work starts the goworker process. Check for errors in
// the return value. Work will take over the Go executable
// and will run until a QUIT, INT, or TERM signal is
// received, or until the queues are empty if the
// -exit-on-complete flag is set.
func Work() error {
	err := Init()
	if err != nil {
		return err
	}
	defer Close()

	quit := signals()

	poller, err := newPoller(workerSettings.Queues, workerSettings.IsStrict)
	if err != nil {
		return err
	}
	jobs, err := poller.poll(time.Duration(workerSettings.Interval), quit)
	if err != nil {
		return err
	}

	var monitor sync.WaitGroup

	for id := 0; id < workerSettings.Concurrency; id++ {
		worker, err := newWorker(strconv.Itoa(id), workerSettings.Queues)
		if err != nil {
			return err
		}
		worker.work(jobs, &monitor)
	}

	if hasToCleanRetries() {
		cleanExpiredRetryTicker := time.NewTicker(cleaningExpiredRetriesInterval)
		waitChan := make(chan struct{})
		go func() {
			monitor.Wait()
			close(waitChan)
		}()
		for {
			select {
			case <-cleanExpiredRetryTicker.C:
				cleanExpiredRetries()
			case <-waitChan:
				cleanExpiredRetryTicker.Stop()
				return nil
			}
		}
	}

	monitor.Wait()

	return nil
}

func hasToCleanRetries() bool {
	return workerSettings.MaxAgeRetries != 0
}

func cleanExpiredRetries() {
	// This is used to set a lock so this operation is not done by more than 1 worker at the same time
	ok, err := client.SetNX(fmt.Sprintf("%s%s", workerSettings.Namespace, keyForCleaningExpiredRetries), os.Getpid(), cleaningExpiredRetriesInterval/2).Result()
	if err != nil {
		logger.Criticalf("Error on setting lock to clean retries: %v", err)
		return
	}

	if !ok {
		return
	}

	failures, err := client.LRange(fmt.Sprintf("%sfailed", workerSettings.Namespace), 0, -1).Result()
	if err != nil {
		logger.Criticalf("Error on getting list of all failed jobs: %v", err)
		return
	}

	for i, fail := range failures {
		var f failure
		err = json.Unmarshal([]byte(fail), &f)
		if err != nil {
			logger.Criticalf("Error on unmarshaling failure: %v", err)
			return
		}
		ra, err := f.GetRetriedAtTime()
		if err != nil {
			logger.Criticalf("Error on GetRetriedAtTime of failure job %q: %v", fail, err)
			return
		}
		if ra == *new(time.Time) {
			continue
		}

		// If the RetryAt has exceeded the MaxAgeRetries then we'll
		// remove the job from the list of failed jobs
		if ra.Add(workerSettings.MaxAgeRetries).Before(time.Now()) {
			hopefullyUniqueValueWeCanUseToDeleteJob := ""
			// This logic what it does it replace first the value (with the LSet) and then remove the first
			// occurrence on the failed queue of the replaced value. This value is the 'hopefullyUniqueValueWeCanUseToDeleteJob'
			client.LSet(fmt.Sprintf("%sfailed", workerSettings.Namespace), int64(i), hopefullyUniqueValueWeCanUseToDeleteJob)
			client.LRem(fmt.Sprintf("%sfailed", workerSettings.Namespace), 1, hopefullyUniqueValueWeCanUseToDeleteJob)
		}
	}
}
