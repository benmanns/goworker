package goworker

import (
	"encoding/json"
	"fmt"
)

type poller struct {
	process
}

func newPoller(queues []string) (*poller, error) {
	process, err := newProcess("poller", queues)
	if err != nil {
		return nil, err
	}
	return &poller{
		process: *process,
	}, nil
}

func (p *poller) getJob(conn *redisConn) (*job, error) {
	for _, queue := range p.Queues {
		logger.Debugf("Checking %s", queue)

		reply, err := conn.Do("LPOP", fmt.Sprintf("resque:queue:%s", queue))
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
