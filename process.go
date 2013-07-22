package goworker

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type process struct {
	Hostname string
	Pid      int
	Id       string
	Queues   []string
}

func newProcess(id string, queues []string) (*process, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}

	return &process{
		Hostname: hostname,
		Pid:      os.Getpid(),
		Id:       id,
		Queues:   queues,
	}, nil
}

func (p *process) String() string {
	return fmt.Sprintf("%s:%d-%s:%s", p.Hostname, p.Pid, p.Id, strings.Join(p.Queues, ","))
}

func (p *process) open(conn *redisConn) error {
	conn.Send("SADD", "resque:workers", p)
	conn.Send("SET", fmt.Sprintf("resque:stat:processed:%v", p), "0")
	conn.Send("SET", fmt.Sprintf("resque:stat:failed:%v", p), "0")
	conn.Flush()

	return nil
}

func (p *process) close(conn *redisConn) error {
	logger.Infof("%v shutdown", p)
	conn.Send("SREM", "resque:workers", p)
	conn.Send("DEL", fmt.Sprintf("resque:stat:processed:%s", p))
	conn.Send("DEL", fmt.Sprintf("resque:stat:failed:%s", p))
	conn.Flush()

	return nil
}

func (p *process) start(conn *redisConn) error {
	conn.Send("SET", fmt.Sprintf("resque:worker:%s:started", p), time.Now().String())
	conn.Flush()

	return nil
}

func (p *process) finish(conn *redisConn) error {
	conn.Send("DEL", fmt.Sprintf("resque:worker:%s", p))
	conn.Send("DEL", fmt.Sprintf("resque:worker:%s:started", p))
	conn.Flush()

	return nil
}

func (p *process) fail(conn *redisConn) error {
	conn.Send("INCR", "resque:stat:failed")
	conn.Send("INCR", fmt.Sprintf("resque:stat:failed:%s", p))
	conn.Flush()

	return nil
}
