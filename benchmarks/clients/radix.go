package clients

import (
	"context"

	"github.com/mediocregopher/radix/v4"
)

type radixClient struct{ pool radix.Client }

// NewRadix returns a Client backed by a radix pool of size connections.
func NewRadix(ctx context.Context, addr string, size int) (Client, error) {
	pool, err := radix.PoolConfig{Size: size}.New(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	return &radixClient{pool: pool}, nil
}

func (r *radixClient) Pop(ctx context.Context, queues []string, stat string) ([]byte, error) {
	for _, q := range queues {
		var job []byte
		reply := radix.Maybe{Rcv: &job}
		if err := r.pool.Do(ctx, radix.Cmd(&reply, "LPOP", q)); err != nil {
			return nil, err
		}
		if reply.Null {
			continue
		}
		return job, r.pool.Do(ctx, radix.Cmd(nil, "INCR", stat))
	}
	return nil, nil
}

func (r *radixClient) Start(ctx context.Context, key string, payload []byte, startedAt string) error {
	p := radix.NewPipeline()
	p.Append(radix.FlatCmd(nil, "SET", key, payload))
	p.Append(radix.Cmd(nil, "SET", key+":started", startedAt))
	return r.pool.Do(ctx, p)
}

func (r *radixClient) Finish(ctx context.Context, key, stat, workerStat string) error {
	p := radix.NewPipeline()
	p.Append(radix.Cmd(nil, "INCR", stat))
	p.Append(radix.Cmd(nil, "INCR", workerStat))
	p.Append(radix.Cmd(nil, "DEL", key, key+":started"))
	return r.pool.Do(ctx, p)
}

func (r *radixClient) Close() error { return r.pool.Close() }
