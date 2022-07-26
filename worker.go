package goworker

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/go-redis/redis/v9"
	"github.com/pkg/errors"
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

func (w *worker) start(c *redis.Client, job *Job) error {
	work := &work{
		Queue:   job.Queue,
		RunAt:   time.Now(),
		Payload: job.Payload,
	}

	buffer, err := json.Marshal(work)
	if err != nil {
		return err
	}

	err = c.Set(c.Context(), fmt.Sprintf("%sworker:%s", workerSettings.Namespace, w), buffer, 0).Err()
	if err != nil {
		return err
	}

	logger.Debugf("Processing %s since %s [%v]", work.Queue, work.RunAt, work.Payload.Class)

	return w.process.start(c)
}

func (w *worker) fail(c *redis.Client, job *Job, err error) error {
	failure := &failure{
		FailedAt:  time.Now(),
		Payload:   job.Payload,
		Exception: "Error",
		// %+v for errors with stack produces stack with file and method names and line numbers
		Error:  fmt.Sprintf("%+v", err),
		Worker: w,
		Queue:  job.Queue,
	}
	buffer, err := json.Marshal(failure)
	if err != nil {
		return err
	}

	err = c.RPush(c.Context(), fmt.Sprintf("%sfailed", workerSettings.Namespace), buffer).Err()
	if err != nil {
		return err
	}

	return w.process.fail(c)
}

func (w *worker) succeed(c *redis.Client) error {
	err := c.Incr(c.Context(), fmt.Sprintf("%sstat:processed", workerSettings.Namespace)).Err()
	if err != nil {
		return err
	}

	err = c.Incr(c.Context(), fmt.Sprintf("%sstat:processed:%s", workerSettings.Namespace, w)).Err()
	if err != nil {
		return err
	}

	return nil
}

func (w *worker) finish(c *redis.Client, job *Job, err error) error {
	if err != nil {
		err = w.fail(c, job, errors.WithStack(err))
		if err != nil {
			return errors.WithStack(err)
		}
	} else {
		err = w.succeed(c)
		if err != nil {
			return err
		}
	}
	return w.process.finish(c)
}

func (w *worker) work(jobs <-chan *Job, monitor *sync.WaitGroup) {
	err := w.open(client)
	if err != nil {
		logger.Criticalf("Error on opening worker %v: %v", w, err)
		return
	}

	monitor.Add(1)

	go func() {
		defer func() {
			defer monitor.Done()

			err := w.close(client)
			if err != nil {
				logger.Criticalf("Error on closing worker %v: %v", w, err)
				return
			}
		}()
		for job := range jobs {
			if workerFunc, ok := workers[job.Payload.Class]; ok {
				w.run(job, workerFunc)

				logger.Debugf("done: (Job{%s} | %s | %v)", job.Queue, job.Payload.Class, job.Payload.Args)
			} else {
				errorLog := fmt.Sprintf("No worker for %s in queue %s with args %v", job.Payload.Class, job.Queue, job.Payload.Args)
				logger.Critical(errorLog)

				err := w.finish(client, job, errors.New(errorLog))
				if err != nil {
					logger.Criticalf("Error on finishing worker %v: %v", w, err)
					return
				}
			}
		}
	}()
}

func (w *worker) run(job *Job, workerFunc workerFunc) {
	var err error

	defer func() {
		errFinish := w.finish(client, job, err)
		if errFinish != nil {
			logger.Criticalf("Error on finishing worker %v: %v", w, errFinish)
			return
		}
	}()

	defer func() {
		if r := recover(); r != nil {
			err = errors.New(fmt.Sprint(r))
		}
	}()

	errStart := w.start(client, job)
	if errStart != nil {
		logger.Criticalf("Error on starting worker %v: %v", w, errStart)
		return
	}

	err = workerFunc(job.Queue, job.Payload.Args...)
}
