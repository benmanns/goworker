package goworker

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/gomodule/redigo/redis"
)

// fakeConn is an in-memory redis.Conn. Every queue is empty:
// the poller's pop script (EVALSHA) finds nothing, or fails with
// fetchErr if set, and every other command succeeds. It never
// touches the network, so synctest's fake clock can advance
// while the poller waits.
type fakeConn struct {
	stats    *fakeStats
	fetchErr error
	pending  int
}

// fakeStats records the poller's fetches across connections.
type fakeStats struct {
	mu          sync.Mutex
	fetches     int
	queueCounts []int // queue keys passed to each fetch
}

func (s *fakeStats) get() (fetches int, queueCounts []int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fetches, slices.Clone(s.queueCounts)
}

func (c *fakeConn) Close() error { return nil }
func (c *fakeConn) Err() error   { return nil }
func (c *fakeConn) Flush() error { return nil }

func (c *fakeConn) Send(string, ...any) error {
	c.pending++
	return nil
}

func (c *fakeConn) Receive() (any, error) {
	c.pending--
	return "OK", nil
}

func (c *fakeConn) Do(cmd string, args ...any) (any, error) {
	switch cmd {
	case "":
		replies := make([]any, c.pending)
		for i := range replies {
			replies[i] = "OK"
		}
		c.pending = 0
		return replies, nil
	case "EVALSHA":
		numKeys, _ := args[1].(int)
		c.stats.mu.Lock()
		c.stats.fetches++
		c.stats.queueCounts = append(c.stats.queueCounts, numKeys-1) // the last key is the stat
		c.stats.mu.Unlock()
		return nil, c.fetchErr
	}
	return "OK", nil
}

// withFakeRedis points goworker at fake connections for the
// duration of the test and returns their shared stats.
func withFakeRedis(t *testing.T, fetchErr error) *fakeStats {
	t.Helper()
	stats := &fakeStats{}
	oldPool, oldSettings := pool, workerSettings
	t.Cleanup(func() { pool, workerSettings = oldPool, oldSettings })

	workerSettings = WorkerSettings{Namespace: "test:"}
	pool = &redis.Pool{
		Dial: func() (redis.Conn, error) {
			return &fakeConn{stats: stats, fetchErr: fetchErr}, nil
		},
		MaxActive: 2,
		Wait:      true,
	}
	return stats
}

func fetches(stats *fakeStats) int {
	n, _ := stats.get()
	return n
}

func startPoller(t *testing.T, interval time.Duration) (jobs <-chan *Job, quit chan struct{}) {
	t.Helper()
	p, err := newPoller([]string{"q"}, true)
	if err != nil {
		t.Fatal(err)
	}
	quit = make(chan struct{})
	jobs, err = p.poll(interval, quit)
	if err != nil {
		t.Fatal(err)
	}
	return jobs, quit
}

// stopPoller closes quit and checks that the poller exits and
// closes its jobs channel without any time passing.
func stopPoller(t *testing.T, jobs <-chan *Job, quit chan struct{}) {
	t.Helper()
	start := time.Now()
	close(quit)
	if _, ok := <-jobs; ok {
		t.Fatal("jobs channel delivered a job instead of closing")
	}
	if waited := time.Since(start); waited != 0 {
		t.Errorf("poller took %v to stop, want no waiting", waited)
	}
}

func TestPollerSleepsIntervalWhenIdle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stats := withFakeRedis(t, nil)
		jobs, quit := startPoller(t, time.Second)

		synctest.Wait()
		if n := fetches(stats); n != 1 {
			t.Fatalf("after start: %d fetches, want 1", n)
		}
		time.Sleep(10*time.Second + time.Millisecond)
		synctest.Wait()
		if n := fetches(stats); n != 11 {
			t.Fatalf("after 10 intervals: %d fetches, want 11", n)
		}

		stopPoller(t, jobs, quit)
	})
}

func TestPollerChecksEachQueueOncePerPass(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stats := withFakeRedis(t, nil)
		// high has weight 3, so it appears three times in the
		// shuffled list, but each queue is only checked once.
		p, err := newPoller([]string{"high", "high", "high", "low"}, false)
		if err != nil {
			t.Fatal(err)
		}
		quit := make(chan struct{})
		jobs, err := p.poll(time.Second, quit)
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		n, queueCounts := stats.get()
		if n != 1 || !slices.Equal(queueCounts, []int{2}) {
			t.Fatalf("one pass: %d fetches with %v queue keys, want 1 fetch with 2", n, queueCounts)
		}
		stopPoller(t, jobs, quit)
	})
}

func TestPollerRetriesAfterRedisErrors(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stats := withFakeRedis(t, errors.New("connection reset"))
		jobs, quit := startPoller(t, time.Second)

		// Before the fix, the first error stopped the poller
		// for good. Now it waits an interval and tries again.
		time.Sleep(5*time.Second + time.Millisecond)
		synctest.Wait()
		if n := fetches(stats); n != 6 {
			t.Fatalf("after 5 intervals of errors: %d fetches, want 6", n)
		}
		select {
		case <-jobs:
			t.Fatal("poller stopped after a Redis error")
		default:
		}

		stopPoller(t, jobs, quit)
	})
}

func TestPollerExitsOnCompleteWithoutWaiting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stats := withFakeRedis(t, nil)
		workerSettings.ExitOnComplete = true
		p, err := newPoller([]string{"q"}, true)
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		jobs, err := p.poll(time.Hour, make(chan struct{}))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := <-jobs; ok {
			t.Fatal("jobs channel delivered a job instead of closing")
		}
		if waited := time.Since(start); waited != 0 {
			t.Errorf("exit-on-complete waited %v, want no waiting", waited)
		}
		if n := fetches(stats); n != 1 {
			t.Errorf("%d fetches, want 1", n)
		}
	})
}

func TestSleep(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		if !sleep(time.Minute, make(chan struct{})) {
			t.Fatal("sleep returned false without quit")
		}
		if got := time.Since(start); got != time.Minute {
			t.Errorf("slept %v, want 1m", got)
		}

		quit := make(chan struct{})
		go func() {
			time.Sleep(time.Second)
			close(quit)
		}()
		start = time.Now()
		if sleep(time.Hour, quit) {
			t.Fatal("sleep returned true after quit")
		}
		if got := time.Since(start); got != time.Second {
			t.Errorf("quit took %v to interrupt sleep, want 1s", got)
		}
	})
}
