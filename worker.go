package goworker

import (
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"
)

type worker struct {
	process
}

type stacktraceError struct {
	Err        error
	Stacktrace []string
}

func (e *stacktraceError) Error() string {
	return e.Err.Error()
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

	conn.Send("SET", fmt.Sprintf("%sworker:%s", namespace, w), buffer)
	logger.Debugf("Processing %s since %s [%v]", work.Queue, work.RunAt, work.Payload.Class)

	return w.process.start(conn)
}

func (w *worker) fail(conn *RedisConn, job *Job, traceErr *stacktraceError) error {
	failure := &failure{
		FailedAt:  time.Now(),
		Payload:   job.Payload,
		Exception: "Error",
		Error:     traceErr.Error(),
		Worker:    w,
		Queue:     job.Queue,
		Backtrace: traceErr.Stacktrace,
	}
	buffer, err := json.Marshal(failure)
	if err != nil {
		return err
	}
	if multiQueue {
		conn.Send("RPUSH", fmt.Sprintf("%s%s_failed", namespace, job.Queue), buffer)
	} else {
		conn.Send("RPUSH", fmt.Sprintf("%sfailed", namespace), buffer)
	}

	return w.process.fail(conn)
}

func (w *worker) succeed(conn *RedisConn, job *Job) error {
	conn.Send("INCR", fmt.Sprintf("%sstat:processed", namespace))
	conn.Send("INCR", fmt.Sprintf("%sstat:processed:%s", namespace, w))

	return nil
}

func (w *worker) finish(conn *RedisConn, job *Job, err *stacktraceError) error {
	if err.Err != nil {
		w.fail(conn, job, err)
	} else {
		w.succeed(conn, job)
	}
	return w.process.finish(conn)
}

func (w *worker) work(jobs <-chan *Job, monitor *sync.WaitGroup) {
	conn, err := GetConn()
	if err != nil {
		logger.Criticalf("Error on getting connection in worker %v", w)
		return
	} else {
		w.open(conn)
		PutConn(conn)
	}

	monitor.Add(1)

	go func() {
		defer func() {
			defer monitor.Done()

			conn, err := GetConn()
			if err != nil {
				logger.Criticalf("Error on getting connection in worker %v", w)
				return
			} else {
				w.close(conn)
				PutConn(conn)
			}
		}()
		for job := range jobs {
			if workerFunc, ok := workers[job.Payload.Class]; ok {
				w.run(job, workerFunc)

				logger.Debugf("done: (Job{%s} | %s | %v)", job.Queue, job.Payload.Class, job.Payload.Args)
			} else {
				errorLog := fmt.Sprintf("No worker for %s in queue %s with args %v", job.Payload.Class, job.Queue, job.Payload.Args)
				logger.Critical(errorLog)

				conn, err := GetConn()
				if err != nil {
					logger.Criticalf("Error on getting connection in worker %v", w)
					return
				} else {
					stackErr := &stacktraceError{
						Err: errors.New(errorLog),
					}
					w.finish(conn, job, stackErr)
					PutConn(conn)
				}
			}
		}
	}()
}

func (w *worker) run(job *Job, workerFunc workerFunc) {
	var err stacktraceError
	defer func() {
		conn, errCon := GetConn()
		if errCon != nil {
			logger.Criticalf("Error on getting connection in worker %v", w)
			return
		} else {
			w.finish(conn, job, &err)
			PutConn(conn)
		}
	}()
	defer func() {
		if r := recover(); r != nil {
			stackBuf := make([]byte, 2048)
			runtime.Stack(stackBuf, false)
			stack := string(stackBuf[:])
			err.Err = errors.New(fmt.Sprint(r))
			err.Stacktrace = strings.Split(stack, "\n")
		}
	}()

	var conn *RedisConn
	conn, err.Err = GetConn()
	if err.Err != nil {
		logger.Criticalf("Error on getting connection in worker %v", w)
		return
	} else {
		w.start(conn, job)
		PutConn(conn)
	}
	err.Err = workerFunc(job.Queue, job.Payload.Args...)
}
