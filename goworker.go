package goworker

import (
	"github.com/cihub/seelog"
	"os"
	"strconv"
	"sync"
	"time"
)

var logger seelog.LoggerInterface

// Call this function to run goworker. Check for errors in
// the return value. Work will take over the Go executable
// and will run until a QUIT, INT, or TERM signal is
// received, or until the queues are empty if the
// -exit-on-complete flag is set.
func Work() error {
	var err error
	logger, err = seelog.LoggerFromWriterWithMinLevel(os.Stdout, seelog.InfoLvl)
	if err != nil {
		return err
	}

	if err := flags(); err != nil {
		return err
	}

	quit := signals()

	pool := newRedisPool(uri, connections, connections, time.Minute)
	defer pool.Close()

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
