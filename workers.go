package goworker

import (
	"context"
	"encoding/json"
	"fmt"
)

var (
	workers map[string]workerFunc
)

func init() {
	workers = make(map[string]workerFunc)
}

// Register registers a goworker worker function. Class
// refers to the Ruby name of the class which enqueues the
// job. Worker is a function which accepts a queue and an
// arbitrary array of interfaces as arguments.
func Register(class string, worker workerFunc) {
	workers[class] = worker
}

func Enqueue(job *Job) error {
	err := Init()
	if err != nil {
		return err
	}

	buffer, err := json.Marshal(job.Payload)
	if err != nil {
		_ = logger.Criticalf("Cant marshal payload on enqueue")
		return err
	}

	err = client.RPush(context.Background(), fmt.Sprintf("%squeue:%s", workerSettings.Namespace, job.Queue), buffer).Err()
	if err != nil {
		_ = logger.Criticalf("Cant push to queue")
		return err
	}

	err = client.SAdd(context.Background(), fmt.Sprintf("%squeues", workerSettings.Namespace), job.Queue).Err()
	if err != nil {
		_ = logger.Criticalf("Cant register queue to list of use queues")
		return err
	}

	return nil
}
