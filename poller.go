package goworker

import (
	"github.com/garyburd/redigo/redis"
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

func (p *poller) getJob(conn redis.Conn) (*job, error) {
	for _, queue := range p.queues(p.isStrict) {
		logger.Debugf("Checking %s", queue)

		reply, err := conn.Do("LPOP", fmt.Sprintf("%squeue:%s", namespace, queue))
		if err != nil {
			return nil, err
		}
		if reply != nil {
			logger.Debugf("Found job on %s", queue)

			job := &job{Queue: queue}

			if err := json.Unmarshal(reply.([]byte), &job.Payload); err != nil {
				return nil, err
			}
			return job, nil
		}
	}

	return nil, nil
}

func (p *poller) poll(pool *redis.Pool, interval time.Duration, quit <-chan bool) <-chan *job {
	jobs := make(chan *job)

	conn := pool.Get()
	err := p.open(conn)
	if err == nil {
		err = p.start(conn)
	}
	if err != nil {
		logger.Criticalf("Error starting poller: %v", err)
		panic(err)
	}
	conn.Close()

	go func() {
		defer func() {
			close(jobs)

			conn := pool.Get()
			p.finish(conn)
			p.close(conn)
			conn.Close()
		}()

		for {
			select {
			case <-quit:
				return
			default:
				conn := pool.Get()
				defer conn.Close()
				job, err := p.getJob(conn)
				if err != nil {
					logger.Criticalf("Error on %v getting job from %v: %v", p, p.Queues, err)
					panic("Error on getting job from queue")
				}
				if job != nil {
					conn.Send("INCR", fmt.Sprintf("%sstat:processed:%v", namespace, p))
					err := conn.Flush()
					if err != nil {
						logger.Errorf("Error on %v while incrementing processed jobs for %v: %v", p, p.Queues, err)
					}

					select {
					case jobs <- job:
					case <-quit:
						buf, err := json.Marshal(job.Payload)
						if err == nil {
							conn := pool.Get()
							err = conn.Send("LPUSH", fmt.Sprintf("%squeue:%s", namespace, job.Queue), buf)
							if err == nil {
								err = conn.Flush()
							}
						}
						if err != nil {
							logger.Criticalf("Error requeueing %v: %v", job, err)
							panic(err)
						}
						return
					}
				} else {
					if exitOnComplete {
						return
					} else {
						logger.Debugf("Sleeping for %v", interval)
						logger.Debugf("Waiting for %v", p.Queues)

						timeout := time.After(interval)
						select {
						case <-quit:
							return
						case <-timeout:
						}
					}
				}
			}
		}
	}()

	return jobs
}
