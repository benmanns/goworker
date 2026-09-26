package goworker

import (
	"encoding/json"
	"fmt"
	"runtime/debug"
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

func (w *worker) start(conn *RedisConn, job *Job) error {
	work := &work{
		Queue:   job.Queue,
		RunAt:   time.Now(),
		Payload: job.Payload,
	}

	buffer, err := json.Marshal(work)
	if err != nil {
		return err
	}

	logger().Debug("processing job", "queue", work.Queue, "class", work.Payload.Class, "run_at", work.RunAt)

	return pipeline(conn,
		command("SET", fmt.Sprintf("%sworker:%s", workerSettings.Namespace, w), buffer),
		command("SET", fmt.Sprintf("%sworker:%s:started", workerSettings.Namespace, w), time.Now().Format(startedFormat)),
	)
}

func (w *worker) succeed(conn *RedisConn) error {
	return pipeline(conn,
		command("INCR", fmt.Sprintf("%sstat:processed", workerSettings.Namespace)),
		command("INCR", fmt.Sprintf("%sstat:processed:%s", workerSettings.Namespace, w)),
	)
}

func (w *worker) finish(conn *RedisConn, job *Job, err error) error {
	var result error
	if err != nil {
		result = recordFailure(conn, w.String(), job, err, nil)
	} else {
		result = w.succeed(conn)
	}
	if ferr := w.process.finish(conn); result == nil {
		result = ferr
	}
	return result
}

func (w *worker) work(jobs <-chan *Job, monitor *sync.WaitGroup) {
	conn, err := GetConn()
	if err != nil {
		logger().Error("getting connection in worker", "worker", w, "error", err)
	} else {
		if err := w.open(conn); err != nil {
			logger().Error("registering worker", "worker", w, "error", err)
		}
		PutConn(conn)
	}

	// Keep consuming jobs even if registration failed so the
	// poller never blocks on a worker that is not listening.
	monitor.Go(func() {
		defer func() {
			conn, err := GetConn()
			if err != nil {
				logger().Error("getting connection in worker", "worker", w, "error", err)
				return
			}
			if err := w.close(conn); err != nil {
				logger().Error("unregistering worker", "worker", w, "error", err)
			}
			PutConn(conn)
		}()
		for job := range jobs {
			if workerFunc, ok := workers.Get(job.Payload.Class); ok {
				w.run(job, workerFunc)

				logger().Debug("done", "queue", job.Queue, "class", job.Payload.Class, "args", job.Payload.Args)
			} else {
				err := fmt.Errorf("no worker for %s in queue %s with args %v", job.Payload.Class, job.Queue, job.Payload.Args)
				logger().Error("no worker for job", "queue", job.Queue, "class", job.Payload.Class, "args", job.Payload.Args)
				w.report(job, err)
			}
		}
	})
}

func (w *worker) run(job *Job, workerFunc workerFunc) {
	conn, err := GetConn()
	if err != nil {
		// Bookkeeping failed, but the job is already off the
		// queue: run it anyway rather than dropping it.
		logger().Error("getting connection in worker on start", "worker", w, "error", err)
	} else {
		if err := w.start(conn, job); err != nil {
			logger().Error("recording job start", "worker", w, "error", err)
		}
		PutConn(conn)
	}

	w.report(job, call(job, workerFunc))
}

// report records the outcome of job in Redis.
func (w *worker) report(job *Job, err error) {
	conn, errConn := GetConn()
	if errConn != nil {
		logger().Error("getting connection in worker on finish", "worker", w, "queue", job.Queue, "class", job.Payload.Class, "job_error", err, "error", errConn)
		return
	}
	defer PutConn(conn)
	if ferr := w.finish(conn, job, err); ferr != nil {
		logger().Error("recording job result", "worker", w, "queue", job.Queue, "class", job.Payload.Class, "job_error", err, "error", ferr)
	}
}

// call runs workerFunc for job, converting a panic into an
// error that carries the stack trace.
func call(job *Job, workerFunc workerFunc) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &panicError{value: r, stack: debug.Stack()}
		}
	}()
	return workerFunc(job.Queue, job.Payload.Args...)
}
