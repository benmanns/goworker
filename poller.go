package goworker

import (
	"code.google.com/p/vitess/go/pools"
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

func (p *poller) getJob(conn *redisConn) (*job, error) {
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

func (p *poller) poll(pool *pools.ResourcePool, interval time.Duration, quit <-chan bool) <-chan *job {
	jobs := make(chan *job)

	resource, err := pool.Get()
	if err != nil {
		logger.Criticalf("Error on getting connection in poller %s", p)
	} else {
		conn := resource.(*redisConn)
		p.open(conn)
		p.start(conn)
		pool.Put(conn)
	}

	go func() {
		defer func() {
			close(jobs)

			resource, err := pool.Get()
			if err != nil {
				logger.Criticalf("Error on getting connection in poller %s", p)
			} else {
				conn := resource.(*redisConn)
				p.finish(conn)
				p.close(conn)
				pool.Put(conn)
			}
		}()

		for {
			select {
			case <-quit:
				return
			default:
				resource, err := pool.Get()
				if err != nil {
					logger.Criticalf("Error on getting connection in poller %s", p)
					return
				} else {
					conn := resource.(*redisConn)

					job, err := p.getJob(conn)
					if err != nil {
						logger.Errorf("Error on %v getting job from %v: %v", p, p.Queues, err)
					}
					if job != nil {
						conn.Send("INCR", fmt.Sprintf("%sstat:processed:%v", namespace, p))
						conn.Flush()
						pool.Put(conn)
						select {
						case jobs <- job:
						case <-quit:
							buf, err := json.Marshal(job.Payload)
							if err != nil {
								logger.Criticalf("Error requeueing %v: %v", job, err)
								return
							}
							resource, err := pool.Get()
							if err != nil {
								logger.Criticalf("Error on getting connection in poller %s", p)
							}

							conn := resource.(*redisConn)
							conn.Send("LPUSH", fmt.Sprintf("%squeue:%s", namespace, job.Queue), buf)
							conn.Flush()
							return
						}
					} else {
						pool.Put(conn)
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
		}
	}()

	return jobs
}
