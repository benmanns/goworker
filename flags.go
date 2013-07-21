package goworker

import (
	"flag"
)

var (
	queuesString  string
	queues        queuesFlag
	intervalFloat float64
	interval      intervalFlag
	concurrency   int
	connections   int
)

func init() {
	flag.StringVar(&queuesString, "queues", "", "a comma-separated list of redis queues")

	flag.Float64Var(&intervalFloat, "interval", 5.0, "sleep interval when no jobs are found")

	flag.IntVar(&concurrency, "concurrency", 25, "the maximum number of concurrently executing jobs")

	flag.IntVar(&connections, "connections", 25+1, "the maximum number of connections to the Redis database")
}

func flags() error {
	if !flag.Parsed() {
		flag.Parse()
	}
	if err := queues.Set(queuesString); err != nil {
		return err
	}
	if err := interval.SetFloat(intervalFloat); err != nil {
		return err
	}
	return nil
}
