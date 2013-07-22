package goworker

import (
	"os"
)

type process struct {
	Hostname string
	Pid      int
	Id       string
	Queues   []string
}

func newProcess(id string, queues []string) (*process, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}

	return &process{
		Hostname: hostname,
		Pid:      os.Getpid(),
		Id:       id,
		Queues:   queues,
	}, nil
}
