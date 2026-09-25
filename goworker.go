package goworker

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gomodule/redigo/redis"
)

var (
	logger      = slog.New(slog.NewTextHandler(os.Stdout, nil))
	pool        *redis.Pool
	ctx         context.Context
	initMutex   sync.Mutex
	initialized bool
)

var errorNotInitialized = errors.New("goworker is not initialized; call Init or Work first")

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
}

func SetSettings(settings WorkerSettings) {
	workerSettings = settings
}

// SetLogger replaces the logger goworker writes to. By
// default goworker logs at the info level to stdout. Call
// SetLogger before Init or Work.
func SetLogger(l *slog.Logger) {
	logger = l
}

// Init initializes the goworker process. This will be
// called by the Work function, but may be used by programs
// that wish to access goworker functions and configuration
// without actually processing jobs.
func Init() error {
	initMutex.Lock()
	defer initMutex.Unlock()
	if !initialized {
		if err := flags(); err != nil {
			return err
		}
		ctx = context.Background()

		pool = newRedisPool(workerSettings.URI, workerSettings.Connections, workerSettings.Connections, time.Minute)

		initialized = true
	}
	return nil
}

// GetConn returns a connection from the goworker Redis
// connection pool. When using the pool, check in
// connections as quickly as possible, because holding a
// connection will cause concurrent worker functions to lock
// while they wait for an available connection. Expect this
// API to change drastically.
func GetConn() (*RedisConn, error) {
	if pool == nil {
		return nil, errorNotInitialized
	}
	conn, err := pool.GetContext(ctx)
	if err != nil {
		return nil, err
	}
	return &RedisConn{Conn: conn}, nil
}

// PutConn puts a connection back into the connection pool.
// Run this as soon as you finish using a connection that
// you got from GetConn. Expect this API to change
// drastically.
func PutConn(conn *RedisConn) {
	conn.Close()
}

// Close cleans up resources initialized by goworker. This
// will be called by Work when cleaning up. However, if you
// are using the Init function to access goworker functions
// and configuration without processing jobs by calling
// Work, you should run this function when cleaning up. For
// example,
//
//	if err := goworker.Init(); err != nil {
//		fmt.Println("Error:", err)
//	}
//	defer goworker.Close()
func Close() {
	initMutex.Lock()
	defer initMutex.Unlock()
	if initialized {
		if err := pool.Close(); err != nil {
			logger.Error("closing Redis pool", "error", err)
		}
		initialized = false
	}
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

	if len(workerSettings.Queues) == 0 {
		return errorEmptyQueues
	}

	quit, stop := signals()
	defer stop()

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

	monitor.Wait()

	return nil
}
