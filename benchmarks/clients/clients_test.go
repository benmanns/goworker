package clients

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gomodule/redigo/redis"
)

// BenchmarkLifecycle runs b.N jobs through goworker's shape, one poller
// feeding 25 workers, with each client:
//
//	go test -run '^$' -bench . -benchtime 3000x
//
// Metrics match goworker's own benchmarks: jobs/s, cmds/job, and rtts/job
// (client writes seen by the latency proxy) for the rtt= variants.
func BenchmarkLifecycle(b *testing.B) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	type factory func(addr string) (Client, error)
	pooled := func(size int, f func(string, int) Client) factory {
		return func(addr string) (Client, error) { return f(addr, size), nil }
	}
	clients := []struct {
		name string
		new  factory
	}{
		{"redigo/pool=2", pooled(2, NewRedigo)},
		{"redigo/pool=10", pooled(10, NewRedigo)},
		{"go-redis/pool=2", pooled(2, NewGoRedis)},
		{"go-redis/pool=10", pooled(10, NewGoRedis)},
		{"radix/pool=2", func(a string) (Client, error) { return NewRadix(context.Background(), a, 2) }},
		{"radix/pool=10", func(a string) (Client, error) { return NewRadix(context.Background(), a, 10) }},
		{"rueidis/multiplexed", NewRueidis},
	}
	for _, rtt := range []time.Duration{0, time.Millisecond} {
		for _, c := range clients {
			b.Run(fmt.Sprintf("rtt=%v/%s", rtt, c.name), func(b *testing.B) {
				runLifecycle(b, addr, rtt, c.new)
			})
		}
	}
}

func runLifecycle(b *testing.B, addr string, rtt time.Duration, newClient func(string) (Client, error)) {
	setup, err := redis.Dial("tcp", addr)
	if err != nil {
		b.Skipf("Redis unavailable at %s: %v", addr, err)
	}
	defer func() { _ = setup.Close() }()

	ns := fmt.Sprintf("clients-bench:%d:", time.Now().UnixNano())
	queues := []string{ns + "queue:high", ns + "queue:low"} // jobs are in the second queue
	payload := []byte(`{"class":"BenchJob","args":[1,"two"]}`)
	for sent := 0; sent < b.N; {
		for batch := 0; batch < 1000 && sent < b.N; batch, sent = batch+1, sent+1 {
			_ = setup.Send("RPUSH", queues[1], payload)
		}
		if _, err := setup.Do(""); err != nil {
			b.Fatal(err)
		}
	}
	defer func() {
		keys, _ := redis.Strings(setup.Do("KEYS", ns+"*"))
		for _, k := range keys {
			_, _ = setup.Do("DEL", k)
		}
	}()

	target := addr
	var proxy *latencyProxy
	if rtt > 0 {
		proxy = startLatencyProxy(b, addr, rtt/2)
		defer proxy.close()
		target = proxy.addr()
	}
	client, err := newClient(target)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	cmds := commandsProcessed(b, setup)
	var writes int64
	if proxy != nil {
		writes = atomic.LoadInt64(&proxy.writes)
	}
	b.ResetTimer()
	start := time.Now()

	jobs := make(chan []byte)
	var pollErr error
	go func() {
		defer close(jobs)
		for {
			job, err := client.Pop(ctx, queues, ns+"stat:processed:poller")
			if err != nil || job == nil {
				pollErr = err
				return
			}
			jobs <- job
		}
	}()
	var wg sync.WaitGroup
	var failed atomic.Int64
	for w := range 25 {
		wg.Go(func() {
			id := strconv.Itoa(w)
			key := ns + "worker:" + id
			for job := range jobs {
				if err := client.Start(ctx, key, job, "2026-09-26 00:00:00 +0000"); err != nil {
					failed.Add(1)
				}
				if err := client.Finish(ctx, key, ns+"stat:processed", ns+"stat:processed:"+id); err != nil {
					failed.Add(1)
				}
			}
		})
	}
	wg.Wait()

	elapsed := time.Since(start)
	b.StopTimer()
	if pollErr != nil || failed.Load() > 0 {
		b.Fatalf("poll error %v, %d failed calls", pollErr, failed.Load())
	}
	if n, _ := redis.Int(setup.Do("GET", ns+"stat:processed")); n != b.N {
		b.Fatalf("processed %d jobs, want %d", n, b.N)
	}
	n := float64(b.N)
	b.ReportMetric(n/elapsed.Seconds(), "jobs/s")
	b.ReportMetric(float64(commandsProcessed(b, setup)-cmds-1)/n, "cmds/job")
	if proxy != nil {
		b.ReportMetric(float64(atomic.LoadInt64(&proxy.writes)-writes)/n, "rtts/job")
	}
}

func commandsProcessed(b *testing.B, conn redis.Conn) int64 {
	b.Helper()
	info, err := redis.String(conn.Do("INFO", "stats"))
	if err != nil {
		b.Fatal(err)
	}
	for line := range strings.SplitSeq(info, "\r\n") {
		if v, ok := strings.CutPrefix(line, "total_commands_processed:"); ok {
			n, _ := strconv.ParseInt(v, 10, 64)
			return n
		}
	}
	b.Fatal("total_commands_processed not in INFO stats")
	return 0
}

// latencyProxy is the same proxy as goworker's benchmark_test.go: it
// delivers every chunk delay after reading it, in order, both ways.
type latencyProxy struct {
	ln     net.Listener
	target string
	delay  time.Duration
	writes int64

	mu    sync.Mutex
	conns []net.Conn
}

func startLatencyProxy(b *testing.B, target string, delay time.Duration) *latencyProxy {
	b.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	p := &latencyProxy{ln: ln, target: target, delay: delay}
	go p.serve()
	return p
}

func (p *latencyProxy) addr() string { return p.ln.Addr().String() }

func (p *latencyProxy) close() {
	_ = p.ln.Close()
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.conns {
		_ = c.Close()
	}
}

func (p *latencyProxy) serve() {
	for {
		client, err := p.ln.Accept()
		if err != nil {
			return
		}
		server, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", p.target)
		if err != nil {
			_ = client.Close()
			continue
		}
		p.mu.Lock()
		p.conns = append(p.conns, client, server)
		p.mu.Unlock()
		go p.pipe(server, client, &p.writes)
		go p.pipe(client, server, nil)
	}
}

func (p *latencyProxy) pipe(dst, src net.Conn, count *int64) {
	type chunk struct {
		data []byte
		due  time.Time
	}
	chunks := make(chan chunk, 4096)
	go func() {
		defer func() { _ = dst.Close() }()
		for c := range chunks {
			time.Sleep(time.Until(c.due))
			if _, err := dst.Write(c.data); err != nil {
				return
			}
		}
	}()
	buf := make([]byte, 64<<10)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if count != nil {
				atomic.AddInt64(count, 1)
			}
			chunks <- chunk{data: append([]byte(nil), buf[:n]...), due: time.Now().Add(p.delay)}
		}
		if err != nil {
			close(chunks)
			return
		}
	}
}
