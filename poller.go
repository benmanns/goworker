package goworker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gomodule/redigo/redis"
)

var errInvalidPayload = errors.New("invalid job payload")

type poller struct {
	process
	isStrict bool
}

func newPoller(queues []string, isStrict bool) (*poller, error) {
	process, err := newProcess("poller", queues)
	if err != nil {
		return nil, err
	}
	return &poller{
		process:  *process,
		isStrict: isStrict,
	}, nil
}

// getJob pops the next job off the first non-empty queue.
// It returns a nil job when every queue is empty.
func (p *poller) getJob(conn *RedisConn) (*Job, error) {
	for _, queue := range p.queues(p.isStrict) {
		logger.Debug("checking queue", "queue", queue)

		reply, err := redis.Bytes(conn.Do("LPOP", fmt.Sprintf("%squeue:%s", workerSettings.Namespace, queue)))
		if errors.Is(err, redis.ErrNil) {
			continue
		}
		if err != nil {
			return nil, err
		}
		logger.Debug("found job", "queue", queue)

		job := &Job{Queue: queue}

		decoder := json.NewDecoder(bytes.NewReader(reply))
		if workerSettings.UseNumber {
			decoder.UseNumber()
		}

		if err := decoder.Decode(&job.Payload); err != nil {
			// The job is already off the queue, so record it
			// as failed rather than silently dropping it.
			logger.Error("decoding job payload", "queue", queue, "payload", string(reply), "error", err)
			err = fmt.Errorf("%w: %w: %s", errInvalidPayload, err, reply)
			if ferr := recordFailure(conn, p.String(), job, err, nil); ferr != nil {
				logger.Error("recording failure", "queue", queue, "error", ferr)
			}
			return nil, errInvalidPayload
		}
		return job, nil
	}

	return nil, nil
}

// next checks out a connection and pops the next job.
func (p *poller) next() (*Job, error) {
	conn, err := GetConn()
	if err != nil {
		return nil, err
	}
	defer PutConn(conn)

	job, err := p.getJob(conn)
	if err != nil || job == nil {
		return nil, err
	}
	if _, err := conn.Do("INCR", fmt.Sprintf("%sstat:processed:%v", workerSettings.Namespace, p)); err != nil {
		logger.Error("updating poller stats", "poller", p, "error", err)
	}
	return job, nil
}

// requeue pushes a job that was popped but never handed to
// a worker back onto the front of its queue.
func (p *poller) requeue(job *Job) error {
	buf, err := json.Marshal(job.Payload)
	if err != nil {
		return err
	}
	conn, err := GetConn()
	if err != nil {
		return err
	}
	defer PutConn(conn)

	_, err = conn.Do("LPUSH", fmt.Sprintf("%squeue:%s", workerSettings.Namespace, job.Queue), buf)
	return err
}

func (p *poller) poll(interval time.Duration, quit <-chan struct{}) (<-chan *Job, error) {
	conn, err := GetConn()
	if err != nil {
		logger.Error("getting connection in poller", "poller", p, "error", err)
		return nil, err
	}
	err = p.open(conn)
	if err == nil {
		err = p.start(conn)
	}
	PutConn(conn)
	if err != nil {
		logger.Error("registering poller", "poller", p, "error", err)
		return nil, err
	}

	jobs := make(chan *Job)

	go func() {
		defer func() {
			close(jobs)

			conn, err := GetConn()
			if err != nil {
				logger.Error("getting connection in poller", "poller", p, "error", err)
				return
			}
			defer PutConn(conn)
			if err := p.finish(conn); err != nil {
				logger.Error("finishing poller", "poller", p, "error", err)
			}
			if err := p.close(conn); err != nil {
				logger.Error("unregistering poller", "poller", p, "error", err)
			}
		}()

		for {
			select {
			case <-quit:
				return
			default:
			}

			job, err := p.next()
			if errors.Is(err, errInvalidPayload) {
				continue
			}
			if err != nil {
				// Redis errors are usually transient (a restart,
				// a failover, a dropped connection). Back off and
				// retry instead of shutting the worker down.
				logger.Error("getting job", "poller", p, "queues", p.Queues, "error", err)
				if !sleep(interval, quit) {
					return
				}
				continue
			}

			if job == nil {
				if workerSettings.ExitOnComplete {
					return
				}
				logger.Debug("no jobs found; sleeping", "interval", interval, "queues", p.Queues)
				if !sleep(interval, quit) {
					return
				}
				continue
			}

			select {
			case jobs <- job:
			case <-quit:
				if err := p.requeue(job); err != nil {
					logger.Error("requeueing job", "queue", job.Queue, "class", job.Payload.Class, "error", err)
				}
				return
			}
		}
	}()

	return jobs, nil
}

// sleep waits for d to elapse. It returns false if quit
// was closed first.
func sleep(d time.Duration, quit <-chan struct{}) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-quit:
		return false
	case <-timer.C:
		return true
	}
}
