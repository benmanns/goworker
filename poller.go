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

func (p *poller) getDeferred(conn *RedisConn) (*Deferred, error) {
	// Do a check for deferred jobs
	reply, err := deferredCommand.Do(conn.Conn, fmt.Sprintf("%s_deferred", namespace), int32(time.Now().Unix()))
	if reply == nil {
		// Got no deferred jobs
		return nil, nil
	} else if err == nil {
		// Got a reply
		deferred := &Deferred{}
		logger.Debugf("Decoding %v", string(reply.([]byte)))
		decoder := json.NewDecoder(bytes.NewReader(reply.([]byte)))
		decoder.UseNumber()
		if err := decoder.Decode(&deferred); err != nil {
			logger.Criticalf("Got error when processing deferred reply %v", err)
			return nil, err
		}

		buf, err := json.Marshal(deferred)
		logger.Debugf("Marshalled %v", string(buf))
		if err != nil {
			logger.Criticalf("Error requeueing %v: %v", deferred, err)
			return nil, err
		}

		conn.Send("LPUSH", fmt.Sprintf("%squeue:%s", namespace, deferred.Queue), buf)
		conn.Flush()

		return deferred, nil
	} else {
		// We got an error!
		logger.Criticalf("Got error when checking deferred queue %v %v", err, string(reply.([]byte)))
		return nil, err
	}
}

func (p *poller) getJob(conn *RedisConn) (*Job, error) {
	logger.Debugf("Checking deferred queue")
	for {
		deferred, err := p.getDeferred(conn)
		if err != nil {
			return nil, err
		}

		if deferred != nil {
			logger.Debugf("Enqueued deferred job %v", deferred)
		} else {
			break
		}
	}

	for _, queue := range p.queues(p.isStrict) {
		logger.Debugf("Checking %s", queue)

		reply, err := conn.Do("LPOP", fmt.Sprintf("%squeue:%s", namespace, queue))
		if err != nil {
			return nil, err
		}
		if reply != nil {
			logger.Debugf("Found job on %s", queue)

			job := &Job{Queue: queue}

			decoder := json.NewDecoder(bytes.NewReader(reply.([]byte)))
			if useNumber {
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

func (p *poller) poll(interval time.Duration, quit <-chan bool) <-chan *Job {
	jobs := make(chan *Job)

	conn, err := GetConn()
	if err != nil {
		logger.Criticalf("Error on getting connection in poller %s", p)
		close(jobs)
		return jobs
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
				logger.Criticalf("Error on getting connection in poller %s", p)
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
					logger.Criticalf("Error on getting connection in poller %s", p)
					return
				}

				job, err := p.getJob(conn)
				if err != nil {
					logger.Criticalf("Error on %v getting job from %v: %v", p, p.Queues, err)
					PutConn(conn)
					return
				}
				if job != nil {
					conn.Send("INCR", fmt.Sprintf("%sstat:processed:%v", namespace, p))
					conn.Flush()
					PutConn(conn)
					select {
					case jobs <- job:
					case <-quit:
						buf, err := json.Marshal(job.Payload)
						if err != nil {
							logger.Criticalf("Error requeueing %v: %v", job, err)
							return
						}
						conn, err := GetConn()
						if err != nil {
							logger.Criticalf("Error on getting connection in poller %s", p)
							return
						}

						conn.Send("LPUSH", fmt.Sprintf("%squeue:%s", namespace, job.Queue), buf)
						conn.Flush()
						return
					}
				} else {
					PutConn(conn)
					if exitOnComplete {
						return
					}
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
	}()

	return jobs
}
