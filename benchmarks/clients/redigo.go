package clients

import (
	"context"
	"errors"

	"github.com/gomodule/redigo/redis"
)

type redigoClient struct{ pool *redis.Pool }

// NewRedigo returns a Client backed by a redigo pool of size connections.
func NewRedigo(addr string, size int) Client {
	return &redigoClient{pool: &redis.Pool{
		DialContext: func(ctx context.Context) (redis.Conn, error) {
			return redis.DialContext(ctx, "tcp", addr)
		},
		MaxIdle:   size,
		MaxActive: size,
		Wait:      true,
	}}
}

func (c *redigoClient) Pop(ctx context.Context, queues []string, stat string) ([]byte, error) {
	conn, err := c.pool.GetContext(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	for _, q := range queues {
		job, err := redis.Bytes(conn.Do("LPOP", q))
		if errors.Is(err, redis.ErrNil) {
			continue
		}
		if err != nil {
			return nil, err
		}
		_, err = conn.Do("INCR", stat)
		return job, err
	}
	return nil, nil
}

func (c *redigoClient) Start(ctx context.Context, key string, payload []byte, startedAt string) error {
	return c.pipeline(ctx, func(conn redis.Conn) error {
		if err := conn.Send("SET", key, payload); err != nil {
			return err
		}
		return conn.Send("SET", key+":started", startedAt)
	})
}

func (c *redigoClient) Finish(ctx context.Context, key, stat, workerStat string) error {
	return c.pipeline(ctx, func(conn redis.Conn) error {
		if err := conn.Send("INCR", stat); err != nil {
			return err
		}
		if err := conn.Send("INCR", workerStat); err != nil {
			return err
		}
		return conn.Send("DEL", key, key+":started")
	})
}

// pipeline sends the commands queued by send in one round trip. redigo
// returns error replies inside the reply slice, so they are checked here.
func (c *redigoClient) pipeline(ctx context.Context, send func(redis.Conn) error) error {
	conn, err := c.pool.GetContext(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := send(conn); err != nil {
		return err
	}
	replies, err := redis.Values(conn.Do(""))
	if err != nil {
		return err
	}
	for _, r := range replies {
		if err, ok := r.(redis.Error); ok {
			return err
		}
	}
	return nil
}

func (c *redigoClient) Close() error { return c.pool.Close() }
