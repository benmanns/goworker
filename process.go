package goworker

import (
	"fmt"
	"github.com/garyburd/redigo/redis"
	"math/rand"
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

func (p *process) open(conn redis.Conn) error {
	conn.Send("SADD", fmt.Sprintf("%sworkers", namespace), p)
	conn.Send("SET", fmt.Sprintf("%sstat:processed:%v", namespace, p), "0")
	conn.Send("SET", fmt.Sprintf("%sstat:failed:%v", namespace, p), "0")
	return conn.Flush()
}

func (p *process) close(conn redis.Conn) error {
	logger.Infof("%v shutdown", p)
	conn.Send("SREM", fmt.Sprintf("%sworkers", namespace), p)
	conn.Send("DEL", fmt.Sprintf("%sstat:processed:%s", namespace, p))
	conn.Send("DEL", fmt.Sprintf("%sstat:failed:%s", namespace, p))
	return conn.Flush()
}

func (p *process) start(conn redis.Conn) error {
	conn.Send("SET", fmt.Sprintf("%sworker:%s:started", namespace, p), time.Now().String())
	return conn.Flush()
}

func (p *process) finish(conn redis.Conn) error {
	conn.Send("DEL", fmt.Sprintf("%sworker:%s", namespace, p))
	conn.Send("DEL", fmt.Sprintf("%sworker:%s:started", namespace, p))
	return conn.Flush()
}

func (p *process) fail(conn redis.Conn) error {
	conn.Send("INCR", fmt.Sprintf("%sstat:failed", namespace))
	conn.Send("INCR", fmt.Sprintf("%sstat:failed:%s", namespace, p))
	return conn.Flush()
}

func (p *process) queues(strict bool) []string {
	// If the queues order is strict then just return them.
	if strict {
		return p.Queues
	}

	// If not then we want to to shuffle the queues before returning them.
	queues := make([]string, len(p.Queues))
	for i, v := range rand.Perm(len(p.Queues)) {
		queues[i] = p.Queues[v]
	}
	return queues
}
