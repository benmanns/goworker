package goworker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

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

func (p *poller) getJob(conn *RedisConn) (*Job, error) {
	for _, queue := range p.queues(p.isStrict) {
		logger.Debug("checking queue", "queue", queue)

		reply, err := conn.Do("LPOP", fmt.Sprintf("%squeue:%s", workerSettings.Namespace, queue))
		if err != nil {
			return nil, err
		}
		if reply != nil {
			logger.Debug("found job", "queue", queue)

			job := &Job{Queue: queue}

			decoder := json.NewDecoder(bytes.NewReader(reply.([]byte)))
			if workerSettings.UseNumber {
				decoder.UseNumber()
			}

			if err := decoder.Decode(&job.Payload); err != nil {
				return nil, err
			}
			return job, nil
		}
	}

	return nil, nil
}

func (p *poller) poll(interval time.Duration, quit <-chan bool) (<-chan *Job, error) {
	jobs := make(chan *Job)

	conn, err := GetConn()
	if err != nil {
		logger.Error("getting connection in poller", "poller", p, "error", err)
		close(jobs)
		return nil, err
	} else {
		p.open(conn)
		p.start(conn)
		PutConn(conn)
	}

	go func() {
		defer func() {
			close(jobs)

			conn, err := GetConn()
			if err != nil {
				logger.Error("getting connection in poller", "poller", p, "error", err)
				return
			} else {
				p.finish(conn)
				p.close(conn)
				PutConn(conn)
			}
		}()

		for {
			select {
			case <-quit:
				return
			default:
				conn, err := GetConn()
				if err != nil {
					logger.Error("getting connection in poller", "poller", p, "error", err)
					return
				}

				job, err := p.getJob(conn)
				if err != nil {
					logger.Error("getting job", "poller", p, "queues", p.Queues, "error", err)
					PutConn(conn)
					return
				}
				if job != nil {
					conn.Send("INCR", fmt.Sprintf("%sstat:processed:%v", workerSettings.Namespace, p))
					conn.Flush()
					PutConn(conn)
					select {
					case jobs <- job:
					case <-quit:
						buf, err := json.Marshal(job.Payload)
						if err != nil {
							logger.Error("requeueing job", "queue", job.Queue, "class", job.Payload.Class, "error", err)
							return
						}
						conn, err := GetConn()
						if err != nil {
							logger.Error("getting connection in poller", "poller", p, "error", err)
							return
						}

						conn.Send("LPUSH", fmt.Sprintf("%squeue:%s", workerSettings.Namespace, job.Queue), buf)
						conn.Flush()
						PutConn(conn)
						return
					}
				} else {
					PutConn(conn)
					if workerSettings.ExitOnComplete {
						return
					}
					logger.Debug("no jobs found; sleeping", "interval", interval, "queues", p.Queues)

					timeout := time.After(interval)
					select {
					case <-quit:
						return
					case <-timeout:
					}
				}
			}
		}
	}()

	return jobs, nil
}
