package goworker

import (
	"flag"
)

var (
	queuesString string
	queues       queuesFlag
)

func init() {
	flag.StringVar(&queuesString, "queues", "", "a comma-separated list of redis queues")
}

func flags() error {
	if !flag.Parsed() {
		flag.Parse()
	}
	if err := queues.Set(queuesString); err != nil {
		return err
	}
	return nil
}
