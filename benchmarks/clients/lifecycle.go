// Package clients compares Go Redis clients on the commands goworker sends
// for each job. It is a separate module so that goworker itself does not
// depend on every client it is compared with.
package clients

import "context"

// Client performs one job's Redis traffic the way goworker does. Each
// implementation is written the way that client's documentation suggests.
type Client interface {
	// Pop removes the next job from the first non-empty queue, in order,
	// and increments the poller's stat. It returns nil when every queue
	// is empty.
	Pop(ctx context.Context, queues []string, stat string) ([]byte, error)
	// Start records that a worker began a job: two SETs, pipelined.
	Start(ctx context.Context, key string, payload []byte, startedAt string) error
	// Finish records a successful job and clears the worker's entry:
	// two INCRs and a DEL, pipelined.
	Finish(ctx context.Context, key, stat, workerStat string) error
	Close() error
}
