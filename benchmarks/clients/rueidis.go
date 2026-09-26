package clients

import (
	"context"

	"github.com/redis/rueidis"
)

type rueidisClient struct{ c rueidis.Client }

// NewRueidis returns a Client backed by rueidis. rueidis multiplexes
// concurrent commands over a single connection with automatic pipelining,
// so it has no pool size to set.
func NewRueidis(addr string) (Client, error) {
	c, err := rueidis.NewClient(rueidis.ClientOption{
		InitAddress:  []string{addr},
		DisableCache: true, // client-side caching needs RESP3 tracking; see README
	})
	if err != nil {
		return nil, err
	}
	return &rueidisClient{c: c}, nil
}

func (r *rueidisClient) Pop(ctx context.Context, queues []string, stat string) ([]byte, error) {
	for _, q := range queues {
		job, err := r.c.Do(ctx, r.c.B().Lpop().Key(q).Build()).AsBytes()
		if rueidis.IsRedisNil(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return job, r.c.Do(ctx, r.c.B().Incr().Key(stat).Build()).Error()
	}
	return nil, nil
}

func (r *rueidisClient) Start(ctx context.Context, key string, payload []byte, startedAt string) error {
	return firstError(r.c.DoMulti(ctx,
		r.c.B().Set().Key(key).Value(rueidis.BinaryString(payload)).Build(),
		r.c.B().Set().Key(key+":started").Value(startedAt).Build(),
	))
}

func (r *rueidisClient) Finish(ctx context.Context, key, stat, workerStat string) error {
	return firstError(r.c.DoMulti(ctx,
		r.c.B().Incr().Key(stat).Build(),
		r.c.B().Incr().Key(workerStat).Build(),
		r.c.B().Del().Key(key, key+":started").Build(),
	))
}

func firstError(results []rueidis.RedisResult) error {
	for _, res := range results {
		if err := res.Error(); err != nil {
			return err
		}
	}
	return nil
}

func (r *rueidisClient) Close() error {
	r.c.Close()
	return nil
}
