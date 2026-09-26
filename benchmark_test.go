package goworker

// End-to-end benchmarks against a real Redis server. They use only the
// exported API that has existed since the first releases (SetSettings,
// Register, Enqueue, Work), so the same file can be run on any version of
// goworker to compare them.
//
// Run with a fixed job count so that startup and shutdown are amortized the
// same way for every version:
//
//	go test -run '^$' -bench . -benchtime 20000x
//
// REDIS_URL selects the server (default redis://localhost:6379/). The rtt=
// variants route goworker's traffic through an in-process proxy that delays
// each direction by half the round-trip time, to show the cost of round trips
// on a real network; a local Redis answers in tens of microseconds, which
// hides it. Reported metrics:
//
//	jobs/s   jobs processed (or enqueued) per second
//	cmds/job Redis commands per job, from INFO total_commands_processed
//	rtts/job client writes per job seen by the proxy; each is one round trip
//	         for a single command or a pipeline (rtt= variants only)

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gomodule/redigo/redis"
)

var benchmarkRTTs = []time.Duration{0, time.Millisecond}

func BenchmarkWork(b *testing.B) {
	for _, rtt := range benchmarkRTTs {
		for _, c := range []struct{ concurrency, connections int }{{25, 2}, {25, 10}} {
			name := fmt.Sprintf("rtt=%v/concurrency=%d/connections=%d", rtt, c.concurrency, c.connections)
			b.Run(name, func(b *testing.B) {
				benchmarkWork(b, rtt, c.concurrency, c.connections, "")
			})
		}
	}
}

// BenchmarkWorkWeighted polls a weighted queue list where the
// heavier queue is always empty, so every poll checks it first
// on most passes.
func BenchmarkWorkWeighted(b *testing.B) {
	for _, rtt := range benchmarkRTTs {
		b.Run(fmt.Sprintf("rtt=%v/queues=empty=3,bench=1", rtt), func(b *testing.B) {
			benchmarkWork(b, rtt, 25, 2, "empty=3,bench=1")
		})
	}
}

func BenchmarkEnqueue(b *testing.B) {
	for _, rtt := range benchmarkRTTs {
		b.Run(fmt.Sprintf("rtt=%v", rtt), func(b *testing.B) {
			env := newBenchmarkEnv(b, rtt, 1, 2, "")
			defer env.cleanup()
			job := &Job{Queue: "bench", Payload: Payload{Class: "BenchJob", Args: []any{1, "two"}}}

			env.start(b)
			for range b.N {
				if err := Enqueue(job); err != nil {
					b.Fatal(err)
				}
			}
			env.stop(b)
		})
	}
}

func benchmarkWork(b *testing.B, rtt time.Duration, concurrency, connections int, queues string) {
	b.Helper()
	env := newBenchmarkEnv(b, rtt, concurrency, connections, queues)
	defer env.cleanup()

	var processed int64
	Register("BenchJob", func(string, ...any) error {
		atomic.AddInt64(&processed, 1)
		return nil
	})
	env.push(b, b.N)

	env.start(b)
	if err := Work(); err != nil {
		b.Fatal(err)
	}
	env.stop(b)

	if n := atomic.LoadInt64(&processed); n != int64(b.N) {
		b.Fatalf("processed %d jobs, want %d", n, b.N)
	}
}

// benchmarkEnv points goworker at a fresh namespace, optionally through a
// latency proxy, and measures one timed section.
type benchmarkEnv struct {
	addr      string // Redis, reached directly for setup and stats
	namespace string
	proxy     *latencyProxy
	stdout    *os.File
	n         int
	began     time.Time
	cmds      int64
	writes    int64
}

// newBenchmarkEnv configures goworker for a benchmark. queues is a
// -queues value; when empty, goworker polls only the "bench" queue.
func newBenchmarkEnv(b *testing.B, rtt time.Duration, concurrency, connections int, queues string) *benchmarkEnv {
	b.Helper()
	env := &benchmarkEnv{
		addr:      benchmarkRedisAddr(),
		namespace: fmt.Sprintf("goworker-bench:%d:", time.Now().UnixNano()),
		n:         b.N,
	}
	conn, err := redis.Dial("tcp", env.addr)
	if err != nil {
		b.Skipf("Redis unavailable at %s: %v", env.addr, err)
	}
	_ = conn.Close()

	target := env.addr
	if rtt > 0 {
		env.proxy = startLatencyProxy(b, env.addr, rtt/2)
		target = env.proxy.addr()
	}

	// Older versions log to stdout with no way to redirect it.
	if devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0); err == nil {
		env.stdout, os.Stdout = os.Stdout, devnull
	}

	Close()
	var queueList []string
	if queues == "" {
		queueList = []string{"bench"}
	}
	SetSettings(WorkerSettings{
		URI:            "redis://" + target + "/",
		QueuesString:   queues,
		Queues:         queueList,
		IntervalFloat:  0.01,
		Concurrency:    concurrency,
		Connections:    connections,
		Namespace:      env.namespace,
		ExitOnComplete: true,
		UseNumber:      true,
	})
	return env
}

// push enqueues n jobs directly, outside the timed section.
func (env *benchmarkEnv) push(b *testing.B, n int) {
	b.Helper()
	conn := env.dial(b)
	defer func() { _ = conn.Close() }()
	payload := []byte(`{"class":"BenchJob","args":[1,"two"]}`)
	for sent := 0; sent < n; {
		batch := 0
		for ; batch < 1000 && sent < n; batch, sent = batch+1, sent+1 {
			if err := conn.Send("RPUSH", env.namespace+"queue:bench", payload); err != nil {
				b.Fatal(err)
			}
		}
		if _, err := conn.Do(""); err != nil {
			b.Fatal(err)
		}
	}
}

func (env *benchmarkEnv) dial(b *testing.B) redis.Conn {
	b.Helper()
	conn, err := redis.DialContext(b.Context(), "tcp", env.addr)
	if err != nil {
		b.Fatal(err)
	}
	return conn
}

func (env *benchmarkEnv) start(b *testing.B) {
	b.Helper()
	env.cmds = env.commandsProcessed(b)
	if env.proxy != nil {
		env.writes = atomic.LoadInt64(&env.proxy.writes)
	}
	b.ResetTimer()
	env.began = time.Now()
}

func (env *benchmarkEnv) stop(b *testing.B) {
	b.Helper()
	elapsed := time.Since(env.began)
	b.StopTimer()
	n := float64(env.n)
	b.ReportMetric(n/elapsed.Seconds(), "jobs/s")
	// The two INFO calls made by start and stop are negligible.
	b.ReportMetric(float64(env.commandsProcessed(b)-env.cmds)/n, "cmds/job")
	if env.proxy != nil {
		b.ReportMetric(float64(atomic.LoadInt64(&env.proxy.writes)-env.writes)/n, "rtts/job")
	}
}

func (env *benchmarkEnv) commandsProcessed(b *testing.B) int64 {
	b.Helper()
	conn := env.dial(b)
	defer func() { _ = conn.Close() }()
	info, err := redis.String(conn.Do("INFO", "stats"))
	if err != nil {
		b.Fatal(err)
	}
	for _, line := range strings.Split(info, "\r\n") {
		if after, ok := strings.CutPrefix(line, "total_commands_processed:"); ok {
			n, _ := strconv.ParseInt(after, 10, 64)
			return n
		}
	}
	b.Fatal("total_commands_processed not found in INFO stats")
	return 0
}

func (env *benchmarkEnv) cleanup() {
	Close()
	if env.stdout != nil {
		_ = os.Stdout.Close()
		os.Stdout = env.stdout
	}
	if env.proxy != nil {
		env.proxy.close()
	}
	conn, err := redis.Dial("tcp", env.addr)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	keys, _ := redis.Strings(conn.Do("KEYS", env.namespace+"*"))
	for _, key := range keys {
		_, _ = conn.Do("DEL", key)
	}
}

func benchmarkRedisAddr() string {
	uri := os.Getenv("REDIS_URL")
	if uri == "" {
		uri = "redis://localhost:6379/"
	}
	u, err := url.Parse(uri)
	if err != nil || u.Host == "" {
		return "localhost:6379"
	}
	if u.Port() == "" {
		return net.JoinHostPort(u.Hostname(), "6379")
	}
	return u.Host
}

// latencyProxy forwards TCP connections to target, delivering every chunk of
// data delay after it was read, in order, in both directions. With delay set
// to half a round-trip time, each request/response exchange takes one RTT.
type latencyProxy struct {
	ln     net.Listener
	target string
	delay  time.Duration
	writes int64 // client-to-server chunks, one per flush by the client

	mu    sync.Mutex
	conns []net.Conn
}

func startLatencyProxy(b *testing.B, target string, delay time.Duration) *latencyProxy {
	b.Helper()
	ln, err := (&net.ListenConfig{}).Listen(b.Context(), "tcp", "127.0.0.1:0")
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
			data := make([]byte, n)
			copy(data, buf[:n])
			chunks <- chunk{data: data, due: time.Now().Add(p.delay)}
		}
		if err != nil {
			close(chunks)
			return
		}
	}
}
