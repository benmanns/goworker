package goworker

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gomodule/redigo/redis"
)

var (
	pool        *redis.Pool
	initMutex   sync.Mutex
	initialized bool
)

var errNotInitialized = errors.New("goworker is not initialized; call Init or Work first")

var workerSettings WorkerSettings

// WorkerSettings configures goworker. Pass it to
// SetSettings to configure goworker from code instead of
// with command-line flags. The fields correspond to the
// flags described in the package documentation.
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

// SetSettings replaces goworker's settings. Call it before
// Init or Work.
func SetSettings(settings WorkerSettings) {
	workerSettings = settings
}

// SetLogger replaces the logger goworker writes to. By
// default goworker logs at the info level to stdout. It is
// safe to call at any time; a nil logger restores the default.
func SetLogger(l *slog.Logger) {
	if l == nil {
		l = defaultLogger
	}
	currentLogger.Store(l)
}

var (
	defaultLogger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	currentLogger atomic.Pointer[slog.Logger]
)

// logger returns the logger set with SetLogger, or the
// default logger. It is safe to call while SetLogger runs.
func logger() *slog.Logger {
	if l := currentLogger.Load(); l != nil {
		return l
	}
	return defaultLogger
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
		return nil, errNotInitialized
	}
	conn, err := pool.GetContext(context.Background())
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
			logger().Error("closing Redis pool", "error", err)
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
		return errEmptyQueues
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

	for id := range workerSettings.Concurrency {
		worker, err := newWorker(strconv.Itoa(id), workerSettings.Queues)
		if err != nil {
			return err
		}
		worker.work(jobs, &monitor)
	}

	monitor.Wait()

	return nil
}
