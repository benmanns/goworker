package goworker

import (
	"github.com/cihub/seelog"
	"os"
	"time"
)

var logger seelog.LoggerInterface

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

	poller, err := newPoller(queues)
	if err != nil {
		return err
	}
	_ = poller.poll(pool, time.Duration(interval), quit)

	<-quit

	return nil
}
