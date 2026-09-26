package goworker

import (
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"time"

	"github.com/gomodule/redigo/redis"
)

// startedFormat matches Ruby's Time#to_s, which Resque uses
// for the worker started timestamp.
const startedFormat = "2006-01-02 15:04:05 -0700"

type process struct {
	Hostname string
	Pid      int
	ID       string
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
		ID:       id,
		Queues:   queues,
	}, nil
}

func (p *process) String() string {
	return fmt.Sprintf("%s:%d-%s:%s", p.Hostname, p.Pid, p.ID, strings.Join(p.Queues, ","))
}

func (p *process) open(conn *RedisConn) error {
	return pipeline(conn,
		command("SADD", fmt.Sprintf("%sworkers", workerSettings.Namespace), p),
		command("SET", fmt.Sprintf("%sstat:processed:%v", workerSettings.Namespace, p), "0"),
		command("SET", fmt.Sprintf("%sstat:failed:%v", workerSettings.Namespace, p), "0"),
	)
}

func (p *process) close(conn *RedisConn) error {
	logger().Info("shutdown", "process", p)
	return pipeline(conn,
		command("SREM", fmt.Sprintf("%sworkers", workerSettings.Namespace), p),
		command("DEL", fmt.Sprintf("%sstat:processed:%s", workerSettings.Namespace, p)),
		command("DEL", fmt.Sprintf("%sstat:failed:%s", workerSettings.Namespace, p)),
	)
}

func (p *process) start(conn *RedisConn) error {
	return pipeline(conn,
		command("SET", fmt.Sprintf("%sworker:%s:started", workerSettings.Namespace, p), time.Now().Format(startedFormat)),
	)
}

func (p *process) finish(conn *RedisConn) error {
	return pipeline(conn, p.finishCommand())
}

// finishCommand deletes the process's current-job entries.
func (p *process) finishCommand() redisCommand {
	key := fmt.Sprintf("%sworker:%s", workerSettings.Namespace, p)
	return command("DEL", key, key+":started")
}

func (p *process) queues(strict bool) []string {
	// If the queues order is strict then just return them.
	if strict {
		return p.Queues
	}

	// If not then we want to shuffle the queues before returning them.
	// The shuffle only spreads polling across weighted queues, so it
	// does not need a cryptographic source.
	queues := make([]string, len(p.Queues))
	for i, v := range rand.Perm(len(p.Queues)) { //nolint:gosec // G404: load balancing, not security
		queues[i] = p.Queues[v]
	}
	return queues
}

type redisCommand struct {
	name string
	args []any
}

func command(name string, args ...any) redisCommand {
	return redisCommand{name: name, args: args}
}

// pipeline sends cmds to Redis in a single round trip and
// returns the first error, including error replies.
func pipeline(conn *RedisConn, cmds ...redisCommand) error {
	for _, c := range cmds {
		if err := conn.Send(c.name, c.args...); err != nil {
			return err
		}
	}
	replies, err := redis.Values(conn.Do(""))
	if err != nil {
		return err
	}
	for _, reply := range replies {
		if err, ok := reply.(redis.Error); ok {
			return err
		}
	}
	return nil
}
