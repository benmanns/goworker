package clients

import (
	"context"
	"errors"

	"github.com/redis/go-redis/v9"
)

type goRedisClient struct{ rdb *redis.Client }

// NewGoRedis returns a Client backed by a go-redis pool of size connections.
func NewGoRedis(addr string, size int) Client {
	return &goRedisClient{rdb: redis.NewClient(&redis.Options{
		Addr:            addr,
		PoolSize:        size,
		DisableIdentity: true, // skip CLIENT SETINFO on connect
	})}
}

func (c *goRedisClient) Pop(ctx context.Context, queues []string, stat string) ([]byte, error) {
	for _, q := range queues {
		job, err := c.rdb.LPop(ctx, q).Bytes()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return job, c.rdb.Incr(ctx, stat).Err()
	}
	return nil, nil
}

func (c *goRedisClient) Start(ctx context.Context, key string, payload []byte, startedAt string) error {
	_, err := c.rdb.Pipelined(ctx, func(p redis.Pipeliner) error {
		p.Set(ctx, key, payload, 0)
		p.Set(ctx, key+":started", startedAt, 0)
		return nil
	})
	return err
}

func (c *goRedisClient) Finish(ctx context.Context, key, stat, workerStat string) error {
	_, err := c.rdb.Pipelined(ctx, func(p redis.Pipeliner) error {
		p.Incr(ctx, stat)
		p.Incr(ctx, workerStat)
		p.Del(ctx, key, key+":started")
		return nil
	})
	return err
}

func (c *goRedisClient) Close() error { return c.rdb.Close() }
