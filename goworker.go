package goworker

import (
	"code.google.com/p/vitess/go/pools"
	"github.com/cihub/seelog"
	"os"
	"strconv"
	"sync"
	"time"
)

var logger seelog.LoggerInterface

// Work starts the goworker process. Check for errors in
// the return value. Work will take over the Go executable
// and will run until a QUIT, INT, or TERM signal is
// received, or until the queues are empty if the
// -exit-on-complete flag is set.
func Work() error {
	err := initEnv()
	if err != nil {
		return err
	}
	pool := newRedisPool(uri, connections, connections, time.Minute)
	defer pool.Close()
	return startWithPool(pool)
}

// Call this function to run goworker with the given pool.
func WorkWithPool(pool *pools.ResourcePool) error {
	err := initEnv()
	if err != nil {
		return err
	}
	return startWithPool(pool)
}

// Init logger and flags.
func initEnv() error {
	var err error
	logger, err = seelog.LoggerFromWriterWithMinLevel(os.Stdout, seelog.InfoLvl)
	if err != nil {
		return err
	}

	if err := flags(); err != nil {
		return err
	}
	return nil
}

// Start worker with the given pool.
func startWithPool(pool *pools.ResourcePool) error {
	quit := signals()
	poller, err := newPoller(queues, isStrict)
	if err != nil {
		return err
	}
	jobs := poller.poll(pool, time.Duration(interval), quit)

	var monitor sync.WaitGroup

	for id := 0; id < concurrency; id++ {
		worker, err := newWorker(strconv.Itoa(id), queues)
		if err != nil {
			return err
		}
		worker.work(pool, jobs, &monitor)
	}

	monitor.Wait()
	return nil
}
