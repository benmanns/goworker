package goworker

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gomodule/redigo/redis"
)

func TestMain(m *testing.M) {
	flag.Parse()
	if !testing.Verbose() {
		SetLogger(slog.New(slog.DiscardHandler))
	}
	code := m.Run()
	if code == 0 && checkGoroutineLeaks != nil {
		code = checkGoroutineLeaks()
	}
	os.Exit(code)
}

// checkGoroutineLeaks is set by leak_test.go on Go versions
// that have the goroutineleak profile.
var checkGoroutineLeaks func() int

// setupRedisTest points goworker at a fresh namespace on the
// Redis server from $REDIS_URL (default localhost:6379) and
// skips the test if Redis is unreachable.
func setupRedisTest(t *testing.T, queues ...string) {
	t.Helper()
	Close()

	namespace := fmt.Sprintf("goworker-test:%s:%d:", t.Name(), time.Now().UnixNano())
	SetSettings(WorkerSettings{
		Queues:         queues,
		IntervalFloat:  0.01,
		Namespace:      namespace,
		ExitOnComplete: true,
		UseNumber:      true,
	})
	if err := Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer Close()
		if err := Init(); err != nil {
			return
		}
		conn, err := GetConn()
		if err != nil {
			return
		}
		defer PutConn(conn)
		keys, _ := redis.Strings(conn.Do("KEYS", namespace+"*"))
		for _, key := range keys {
			_, _ = conn.Do("DEL", key)
		}
	})

	conn, err := GetConn()
	if err == nil {
		_, err = conn.Do("PING")
		PutConn(conn)
	}
	if err != nil {
		t.Skipf("Redis unavailable: %v", err)
	}
}

func redisDo(t *testing.T, cmd string, args ...any) any {
	t.Helper()
	// Work closes the pool when it returns.
	if err := Init(); err != nil {
		t.Fatal(err)
	}
	conn, err := GetConn()
	if err != nil {
		t.Fatal(err)
	}
	defer PutConn(conn)
	reply, err := conn.Do(cmd, args...)
	if err != nil {
		t.Fatalf("%s: %v", cmd, err)
	}
	return reply
}

func failures(t *testing.T) []failure {
	t.Helper()
	raw, err := redis.ByteSlices(redisDo(t, "LRANGE", Namespace()+"failed", 0, -1), nil)
	if err != nil {
		t.Fatal(err)
	}
	var out []failure
	for _, r := range raw {
		var f failure
		if err := json.Unmarshal(r, &f); err != nil {
			t.Fatalf("decoding failure %s: %v", r, err)
		}
		out = append(out, f)
	}
	return out
}

func TestInvalidPayloadIsRecordedAndPollingContinues(t *testing.T) {
	setupRedisTest(t, "q")
	var ran int
	Register("InvalidPayloadNeighbor", func(string, ...any) error {
		ran++
		return nil
	})
	redisDo(t, "RPUSH", Namespace()+"queue:q", "not json", `{"class":"InvalidPayloadNeighbor","args":[]}`)

	if err := Work(); err != nil {
		t.Fatal(err)
	}
	if ran != 1 {
		t.Errorf("valid job after invalid one ran %d times, want 1", ran)
	}
	fs := failures(t)
	if len(fs) != 1 || !strings.Contains(fs[0].Error, "not json") {
		t.Fatalf("failures = %+v, want one for the invalid payload", fs)
	}
	if fs[0].Backtrace == nil {
		t.Error("backtrace is null, want an array")
	}
}

func TestPanicIsRecordedWithBacktrace(t *testing.T) {
	setupRedisTest(t, "q")
	Register("Panics", func(string, ...any) error {
		panic("boom")
	})
	if err := Enqueue(&Job{Queue: "q", Payload: Payload{Class: "Panics"}}); err != nil {
		t.Fatal(err)
	}
	if err := Work(); err != nil {
		t.Fatal(err)
	}
	fs := failures(t)
	if len(fs) != 1 || fs[0].Error != "boom" || fs[0].Queue != "q" {
		t.Fatalf("failures = %+v, want one boom failure", fs)
	}
	if len(fs[0].Backtrace) == 0 || !strings.Contains(strings.Join(fs[0].Backtrace, "\n"), "TestPanicIsRecordedWithBacktrace") {
		t.Errorf("backtrace = %q, want the panicking stack", fs[0].Backtrace)
	}
	if n, _ := redis.Int(redisDo(t, "GET", Namespace()+"stat:failed"), nil); n != 1 {
		t.Errorf("stat:failed = %d, want 1", n)
	}
}

func TestErrorAndMissingWorkerAreRecorded(t *testing.T) {
	setupRedisTest(t, "q")
	Register("Errors", func(string, ...any) error {
		return errors.New("nope")
	})
	for _, class := range []string{"Errors", "NobodyHandlesThis"} {
		if err := Enqueue(&Job{Queue: "q", Payload: Payload{Class: class}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := Work(); err != nil {
		t.Fatal(err)
	}
	fs := failures(t)
	errs := map[string]string{}
	for _, f := range fs {
		errs[f.Payload.Class] = f.Error
	}
	if len(fs) != 2 || errs["Errors"] != "nope" || !strings.HasPrefix(errs["NobodyHandlesThis"], "no worker for NobodyHandlesThis") {
		t.Fatalf("failures = %+v", fs)
	}
	if n, _ := redis.Int(redisDo(t, "GET", Namespace()+"stat:failed"), nil); n != 2 {
		t.Errorf("stat:failed = %d, want 2", n)
	}
	if n, _ := redis.Int(redisDo(t, "SCARD", Namespace()+"workers"), nil); n != 0 {
		t.Errorf("%d workers still registered after Work returned", n)
	}
}

func TestEnqueueWithoutQueues(t *testing.T) {
	setupRedisTest(t)
	if err := Enqueue(&Job{Queue: "q", Payload: Payload{Class: "X"}}); err != nil {
		t.Fatalf("Enqueue without -queues: %v", err)
	}
	if err := Work(); !errors.Is(err, errEmptyQueues) {
		t.Errorf("Work without queues = %v, want %v", err, errEmptyQueues)
	}
}

func TestInitDoesNotDuplicateQueues(t *testing.T) {
	Close()
	SetSettings(WorkerSettings{QueuesString: "high=2,low"})
	for range 3 {
		if err := Init(); err != nil {
			t.Fatal(err)
		}
		Close()
	}
	if got := strings.Join(workerSettings.Queues, ","); got != "high,high,low" {
		t.Errorf("queues = %s, want high,high,low", got)
	}
}

func TestSetSettingsZeroValuesGetDefaults(t *testing.T) {
	Close()
	defer Close()
	SetSettings(WorkerSettings{Queues: []string{"q"}, Interval: 5.0})
	if err := Init(); err != nil {
		t.Fatal(err)
	}
	if got := time.Duration(workerSettings.Interval); got != defaultInterval {
		t.Errorf("interval = %v, want %v", got, defaultInterval)
	}
	if workerSettings.Concurrency != defaultConcurrency {
		t.Errorf("concurrency = %d, want %d", workerSettings.Concurrency, defaultConcurrency)
	}
	if workerSettings.Connections != defaultConnections {
		t.Errorf("connections = %d, want %d", workerSettings.Connections, defaultConnections)
	}
	if workerSettings.URI == "" {
		t.Error("URI is empty")
	}

	SetSettings(WorkerSettings{Queues: []string{"q"}, IntervalFloat: 0.5})
	Close()
	if err := Init(); err != nil {
		t.Fatal(err)
	}
	if got := time.Duration(workerSettings.Interval); got != 500*time.Millisecond {
		t.Errorf("interval = %v, want 500ms", got)
	}
}

func TestRedisConnFromURIErrors(t *testing.T) {
	for _, uri := range []string{"http://localhost:6379/", "redis://localhost:6379/notadb"} {
		if _, err := redisConnFromURI(t.Context(), uri); err == nil {
			t.Errorf("redisConnFromURI(%q) succeeded, want error", uri)
		}
	}
}

func TestRedisConnFromURIDefaultPort(t *testing.T) {
	setupRedisTest(t)
	if !strings.Contains(workerSettings.URI, "localhost:6379") {
		t.Skip("Redis is not on localhost:6379")
	}
	conn, err := redisConnFromURI(t.Context(), "redis://localhost/1")
	if err != nil {
		t.Fatalf("dialing without a port: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Do("PING"); err != nil {
		t.Fatal(err)
	}
}

func TestStartedTimestampIsResqueFormat(t *testing.T) {
	setupRedisTest(t, "q")
	p, err := newProcess("x", []string{"q"})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := GetConn()
	if err != nil {
		t.Fatal(err)
	}
	defer PutConn(conn)
	if err = p.start(conn); err != nil {
		t.Fatal(err)
	}
	started, err := redis.String(conn.Do("GET", fmt.Sprintf("%sworker:%s:started", Namespace(), p)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := time.Parse(startedFormat, started); err != nil {
		t.Errorf("started = %q: %v", started, err)
	}
}

func TestSignals(t *testing.T) {
	quit, stop := signals()
	defer stop()
	select {
	case <-quit:
		t.Fatal("quit closed without a signal")
	default:
	}

	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-quit:
	case <-time.After(5 * time.Second):
		t.Fatal("quit not closed after SIGTERM")
	}
}

func TestWorkLeavesNoProcessStateInRedis(t *testing.T) {
	setupRedisTest(t, "q")
	Register("CleanupOK", func(string, ...any) error { return nil })
	Register("CleanupFails", func(string, ...any) error { return errors.New("nope") })
	for _, class := range []string{"CleanupOK", "CleanupFails", "CleanupOK"} {
		if err := Enqueue(&Job{Queue: "q", Payload: Payload{Class: class}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := Work(); err != nil {
		t.Fatal(err)
	}

	if n, _ := redis.Int(redisDo(t, "SCARD", Namespace()+"workers"), nil); n != 0 {
		t.Errorf("%d entries left in the workers set", n)
	}
	for _, pattern := range []string{"worker:*", "stat:processed:*", "stat:failed:*"} {
		keys, err := redis.Strings(redisDo(t, "KEYS", Namespace()+pattern), nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(keys) > 0 {
			t.Errorf("per-process keys left behind: %v", keys)
		}
	}
	// The global counters are not per-process and must survive.
	if n, _ := redis.Int(redisDo(t, "GET", Namespace()+"stat:processed"), nil); n != 2 {
		t.Errorf("stat:processed = %d, want 2", n)
	}
	if n, _ := redis.Int(redisDo(t, "GET", Namespace()+"stat:failed"), nil); n != 1 {
		t.Errorf("stat:failed = %d, want 1", n)
	}
}
