package goworker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-redis/redis/v7"
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

func (p *poller) getJob(c *redis.Client) (*Job, error) {
	for _, queue := range p.queues(p.isStrict) {
		logger.Debugf("Checking %s", queue)

		result, err := c.LPop(fmt.Sprintf("%squeue:%s", workerSettings.Namespace, queue)).Result()
		if err != nil {
			// no jobs for now, continue on another queue
			if err == redis.Nil {
				continue
			}
			return nil, err
		}
		if result != "" {
			logger.Debugf("Found job on %s", queue)

			job := &Job{Queue: queue}

			decoder := json.NewDecoder(bytes.NewReader([]byte(result)))
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
	err := p.open(client)
	if err != nil {
		return nil, err
	}

	err = p.start(client)
	if err != nil {
		return nil, err
	}

	jobs := make(chan *Job)
	go func() {
		defer func() {
			close(jobs)

			err = p.finish(client)
			if err != nil {
				return
			}

			err = p.close(client)
			if err != nil {
				return
			}
		}()

		for {
			select {
			case <-quit:
				return
			default:
				job, err := p.getJob(client)
				if err != nil {
					logger.Criticalf("Error on %v getting job from %v: %+v", p, p.Queues, err)
					return
				}
				if job != nil {
					err = client.Incr(fmt.Sprintf("%sstat:processed:%v", workerSettings.Namespace, p)).Err()
					if err != nil {
						return
					}

					select {
					case jobs <- job:
					case <-quit:
						buf, err := json.Marshal(job.Payload)
						if err != nil {
							logger.Criticalf("Error requeueing %v: %v", job, err)
							return
						}

						err = client.LPush(fmt.Sprintf("%squeue:%s", workerSettings.Namespace, job.Queue), buf).Err()
						if err != nil {
							return
						}

						return
					}
				} else {
					if workerSettings.ExitOnComplete {
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

	return jobs, nil
}
