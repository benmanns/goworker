package goworker

import (
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/gomodule/redigo/redis"
)

// fakeConn is an in-memory redis.Conn. Every queue is empty,
// LPOP fails with lpopErr if set, and every other command
// succeeds. It never touches the network, so synctest's fake
// clock can advance while the poller waits.
type fakeConn struct {
	mu      *sync.Mutex
	lpops   *int
	lpopErr error
	pending int
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

func (c *fakeConn) Do(cmd string, _ ...any) (any, error) {
	switch cmd {
	case "":
		replies := make([]any, c.pending)
		for i := range replies {
			replies[i] = "OK"
		}
		c.pending = 0
		return replies, nil
	case "LPOP":
		c.mu.Lock()
		*c.lpops++
		c.mu.Unlock()
		return nil, c.lpopErr
	}
	return "OK", nil
}

// withFakeRedis points goworker at fake connections for the
// duration of the test and returns a function that reports how
// many LPOPs the poller has issued.
func withFakeRedis(t *testing.T, lpopErr error) (lpops func() int) {
	t.Helper()
	var mu sync.Mutex
	var n int
	oldPool, oldSettings := pool, workerSettings
	t.Cleanup(func() { pool, workerSettings = oldPool, oldSettings })

	workerSettings = WorkerSettings{Namespace: "test:"}
	pool = &redis.Pool{
		Dial: func() (redis.Conn, error) {
			return &fakeConn{mu: &mu, lpops: &n, lpopErr: lpopErr}, nil
		},
		MaxActive: 2,
		Wait:      true,
	}
	return func() int {
		mu.Lock()
		defer mu.Unlock()
		return n
	}
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
		lpops := withFakeRedis(t, nil)
		jobs, quit := startPoller(t, time.Second)

		synctest.Wait()
		if n := lpops(); n != 1 {
			t.Fatalf("after start: %d LPOPs, want 1", n)
		}
		time.Sleep(10*time.Second + time.Millisecond)
		synctest.Wait()
		if n := lpops(); n != 11 {
			t.Fatalf("after 10 intervals: %d LPOPs, want 11", n)
		}

		stopPoller(t, jobs, quit)
	})
}

func TestPollerRetriesAfterRedisErrors(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		lpops := withFakeRedis(t, errors.New("connection reset"))
		jobs, quit := startPoller(t, time.Second)

		// Before the fix, the first error stopped the poller
		// for good. Now it waits an interval and tries again.
		time.Sleep(5*time.Second + time.Millisecond)
		synctest.Wait()
		if n := lpops(); n != 6 {
			t.Fatalf("after 5 intervals of errors: %d LPOPs, want 6", n)
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
		lpops := withFakeRedis(t, nil)
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
		if n := lpops(); n != 1 {
			t.Errorf("%d LPOPs, want 1", n)
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
