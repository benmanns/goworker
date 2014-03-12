package goworker

import (
	"github.com/garyburd/redigo/redis"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

type worker struct {
	process
}

func newWorker(id string, queues []string) (*worker, error) {
	process, err := newProcess(id, queues)
	if err != nil {
		return nil, err
	}
	return &worker{
		process: *process,
	}, nil
}

func (w *worker) MarshalJSON() ([]byte, error) {
	return json.Marshal(w.String())
}

func (w *worker) start(conn redis.Conn, job *job) error {
	work := &work{
		Queue:   job.Queue,
		RunAt:   time.Now(),
		Payload: job.Payload,
	}

	buffer, err := json.Marshal(work)
	if err != nil {
		return err
	}

	err = conn.Send("SET", fmt.Sprintf("%sworker:%s", namespace, w), buffer)
	if err != nil {
		return err
	}
	logger.Debugf("Processing %s since %s [%v]", work.Queue, work.RunAt, work.Payload.Class)

	return w.process.start(conn)
}

func (w *worker) fail(conn redis.Conn, job *job, err error) error {
	failure := &failure{
		FailedAt:  time.Now(),
		Payload:   job.Payload,
		Exception: "Error",
		Error:     err.Error(),
		Worker:    w,
		Queue:     job.Queue,
	}
	buffer, err := json.Marshal(failure)
	if err != nil {
		return err
	}
	conn.Send("RPUSH", fmt.Sprintf("%sfailed", namespace), buffer)

	return w.process.fail(conn)
}

func (w *worker) succeed(conn redis.Conn, job *job) error {
	conn.Send("INCR", fmt.Sprintf("%sstat:processed", namespace))
	conn.Send("INCR", fmt.Sprintf("%sstat:processed:%s", namespace, w))

	return nil
}

func (w *worker) finish(conn redis.Conn, job *job, err error) error {
	if err != nil {
		w.fail(conn, job, err)
	} else {
		w.succeed(conn, job)
	}
	return w.process.finish(conn)
}

func (w *worker) work(pool *redis.Pool, jobs <-chan *job, monitor *sync.WaitGroup) {
	conn := pool.Get()
	if err := w.open(conn); err != nil {
		logger.Criticalf("Error opening worker %v", w)
		panic("Error opening worker")
	}
	conn.Close()
	monitor.Add(1)

	go func() {
		defer func() {
			conn := pool.Get()
			if err := w.close(conn); err != nil {
				logger.Errorf("Worker close failed: %v", err)
			}
			conn.Close()

			monitor.Done()
		}()
		for job := range jobs {
			if workerFunc, ok := workers[job.Payload.Class]; ok {
				w.run(pool, job, workerFunc)

				logger.Debugf("done: (Job{%s} | %s | %v)", job.Queue, job.Payload.Class, job.Payload.Args)
			} else {
				errorLog := fmt.Sprintf("No worker for %s in queue %s with args %v", job.Payload.Class, job.Queue, job.Payload.Args)
				logger.Critical(errorLog)

				conn := pool.Get()
				w.finish(conn, job, errors.New(errorLog))
				conn.Close()
			}
		}
	}()
}

func (w *worker) run(pool *redis.Pool, job *job, workerFunc workerFunc) {
	var err error
	defer func() {
		conn := pool.Get()
		err := w.finish(conn, job, err)
		conn.Close()
		if err != nil {
			logger.Errorf("Error finishing work: %v", err)
		}

	}()
	defer func() {
		if r := recover(); r != nil {
			err = errors.New(fmt.Sprint(r))
		}
	}()

	conn := pool.Get()
	defer conn.Close()
	err = w.start(conn, job)
	if err != nil {
		return
	}

	err = workerFunc(job.Queue, job.Payload.Args...)
}
