package goworker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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

// popScript pops the first job it finds in KEYS[1] .. KEYS[n-1],
// in order, and increments the poller's processed stat, KEYS[n],
// all in one round trip. It returns the 1-based index of the queue
// and the job, or nil when every queue is empty.
var popScript = redis.NewScript(-1, `
for i = 1, #KEYS - 1 do
  local job = redis.call('LPOP', KEYS[i])
  if job then
    redis.call('INCR', KEYS[#KEYS])
    return {i, job}
  end -- if
end -- for
return false
`)

// getJob pops the next job off the first non-empty queue.
// It returns a nil job when every queue is empty.
func (p *poller) getJob(conn *RedisConn) (*Job, error) {
	// A weighted queue appears several times in the shuffled
	// list. Checking it again after it was empty is pointless,
	// so pass each queue once, at its first position.
	var queues []string
	for _, queue := range p.queues(p.isStrict) {
		if !slices.Contains(queues, queue) {
			queues = append(queues, queue)
		}
	}
	args := make([]any, 0, len(queues)+2)
	args = append(args, len(queues)+1)
	for _, queue := range queues {
		args = append(args, fmt.Sprintf("%squeue:%s", workerSettings.Namespace, queue))
	}
	args = append(args, fmt.Sprintf("%sstat:processed:%v", workerSettings.Namespace, p))

	logger().Debug("checking queues", "queues", queues)
	values, err := redis.Values(popScript.Do(conn.Conn, args...))
	if errors.Is(err, redis.ErrNil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var index int
	var reply []byte
	if _, err := redis.Scan(values, &index, &reply); err != nil {
		return nil, err
	}
	if index < 1 || index > len(queues) {
		return nil, fmt.Errorf("pop script returned queue index %d for %d queues", index, len(queues))
	}
	queue := queues[index-1]
	logger().Debug("found job", "queue", queue)

	job := &Job{Queue: queue}

	decoder := json.NewDecoder(bytes.NewReader(reply))
	if workerSettings.UseNumber {
		decoder.UseNumber()
	}

	if err := decoder.Decode(&job.Payload); err != nil {
		// The job is already off the queue, so record it
		// as failed rather than silently dropping it.
		logger().Error("decoding job payload", "queue", queue, "payload", string(reply), "error", err)
		err = fmt.Errorf("%w: %w: %s", errInvalidPayload, err, reply)
		if ferr := recordFailure(conn, p.String(), job, err, nil); ferr != nil {
			logger().Error("recording failure", "queue", queue, "error", ferr)
		}
		return nil, errInvalidPayload
	}
	return job, nil
}

// next checks out a connection and pops the next job.
func (p *poller) next() (*Job, error) {
	conn, err := GetConn()
	if err != nil {
		return nil, err
	}
	defer PutConn(conn)
	return p.getJob(conn)
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
		logger().Error("getting connection in poller", "poller", p, "error", err)
		return nil, err
	}
	err = p.open(conn)
	if err == nil {
		err = p.start(conn)
	}
	PutConn(conn)
	if err != nil {
		logger().Error("registering poller", "poller", p, "error", err)
		return nil, err
	}

	jobs := make(chan *Job)

	go func() {
		// Closing jobs tells the workers to finish. The poller
		// stays registered until Work calls unregister after
		// they have.
		defer close(jobs)

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
				logger().Error("getting job", "poller", p, "queues", p.Queues, "error", err)
				if !sleep(interval, quit) {
					return
				}
				continue
			}

			if job == nil {
				if workerSettings.ExitOnComplete {
					return
				}
				logger().Debug("no jobs found; sleeping", "interval", interval, "queues", p.Queues)
				if !sleep(interval, quit) {
					return
				}
				continue
			}

			select {
			case jobs <- job:
			case <-quit:
				if err := p.requeue(job); err != nil {
					logger().Error("requeueing job", "queue", job.Queue, "class", job.Payload.Class, "error", err)
				}
				return
			}
		}
	}()

	return jobs, nil
}

// unregister removes the poller's entries from Redis. Work
// calls it after every worker has finished and unregistered.
func (p *poller) unregister() {
	conn, err := GetConn()
	if err != nil {
		logger().Error("getting connection in poller", "poller", p, "error", err)
		return
	}
	defer PutConn(conn)
	if err := p.finish(conn); err != nil {
		logger().Error("finishing poller", "poller", p, "error", err)
	}
	if err := p.close(conn); err != nil {
		logger().Error("unregistering poller", "poller", p, "error", err)
	}
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
