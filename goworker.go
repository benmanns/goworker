package goworker

import (
	"github.com/cihub/seelog"
	"os"
)

var logger seelog.LoggerInterface

func Work() error {
	var err error
	logger, err = seelog.LoggerFromWriterWithMinLevel(os.Stdout, seelog.InfoLvl)
	if err != nil {
		return err
	}

	return nil
}
