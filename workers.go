package goworker

import (
	"encoding/json"
	"fmt"
	"sync"
)

type workersMutex struct {
	sync.RWMutex
	workers map[string]workerFunc
}

func (wm *workersMutex) Add(class string, worker workerFunc) {
	wm.Lock()
	defer wm.Unlock()

	wm.workers[class] = worker
}

func (wm *workersMutex) Get(class string) (worker workerFunc, ok bool) {
	wm.RLock()
	defer wm.RUnlock()

	worker, ok = wm.workers[class]
	return
}

var workers = &workersMutex{workers: make(map[string]workerFunc)}

// Register registers a goworker worker function. Class
// refers to the Ruby name of the class which enqueues the
// job. Worker is a function which accepts a queue and an
// arbitrary array of interfaces as arguments.
func Register(class string, worker workerFunc) {
	workers.Add(class, worker)
}

// Enqueue pushes job onto its queue in the same format Resque
// uses, so it can be processed by goworker or by Ruby Resque
// workers. It initializes goworker if needed.
func Enqueue(job *Job) error {
	err := Init()
	if err != nil {
		return err
	}

	conn, err := GetConn()
	if err != nil {
		logger().Error("getting connection on enqueue", "error", err)
		return err
	}
	defer PutConn(conn)

	buffer, err := json.Marshal(job.Payload)
	if err != nil {
		logger().Error("marshaling payload on enqueue", "error", err)
		return err
	}

	err = pipeline(conn,
		command("SADD", fmt.Sprintf("%squeues", workerSettings.Namespace), job.Queue),
		command("RPUSH", fmt.Sprintf("%squeue:%s", workerSettings.Namespace, job.Queue), buffer),
	)
	if err != nil {
		logger().Error("pushing to queue", "queue", job.Queue, "error", err)
		return err
	}
	return nil
}
