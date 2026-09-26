# Benchmarks

Two sets of benchmarks, both against a real Redis:

- `../benchmark_test.go` runs goworker end to end: `Work` processing no-op
  jobs, and `Enqueue`. It uses only API that has existed since the first
  releases, so the same file runs on any version (it was added to master
  first, on the `claude/benchmarks` branch, for exactly that reason).
- `clients/` is a separate module comparing Go Redis clients on the commands
  goworker sends per job, so that goworker itself does not depend on them.

```sh
# goworker end to end; a fixed job count amortizes startup the same way everywhere
go test -run '^$' -bench . -benchtime 3000x -count 5

# client comparison
cd benchmarks/clients && go test -run '^$' -bench . -benchtime 3000x -count 3
```

Compare runs with [benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat).
Besides ns/op they report **jobs/s**, **cmds/job** (from Redis
`INFO total_commands_processed`) and **rtts/job** (client writes seen by the
latency proxy, one per command or pipeline that waits for Redis).

## Simulated network latency

A local Redis answers in about 50µs, which hides the cost of round trips. The
`rtt=1ms` variants route traffic through an in-process proxy that delays each
direction by half the configured RTT. Timer granularity makes it oversleep: in
the VM these numbers come from, a PING through the `rtt=1ms` proxy took
**2.55ms**. Every version and client below went through the same proxy, so the
comparisons hold, but read "rtt=1ms" as "a slow network". On a real 1ms
network, throughput would be roughly 2.5× higher across the board.

## Results

Linux, 4 vCPU Xeon @ 2.10GHz, Redis 7.0.15 on localhost, Go 1.26.8, 5 runs of
3000 jobs per version; medians. `Work` uses 25 workers; connections=2 is the
default. All differences called out are significant at p ≤ 0.016.

| Version | Work, local (jobs/s) | Work, slow net (jobs/s) | … 10 connections | rtts/job | Weighted queues, slow net | Enqueue, local | Enqueue, slow net |
|---|--:|--:|--:|--:|--:|--:|--:|
| master | 14,706 | 380 | 366 | 2.6 | – | 169,372 | 229,534 |
| vitess → redigo pool | 7,654 | 183 | 186 | 4.0 | – | 22,069 | 387 |
| bug fixes (replies checked) | 6,333 | 149 | 185 | 5.0 | – | 21,974 | 392 |
| before improvements | 5,955 | 146 | 184 | 5.0 | 106 | 21,943 | 385 |
| + one-round-trip finish | 7,622 | 183 | 185 | 4.0 | 108 | 22,961 | 385 |
| + skip empty queues | 8,069 | 183 | 184 | 4.0 | 137 | 21,451 | 392 |
| + scripted fetch | 8,134 | **242** | **360** | **3.0** | **239** | 20,902 | 387 |

**Why master is fastest, and why that is not a target.** master's pool
(vitess) handed connections back without reading replies, and the code sent
most commands with `Send`/`Flush` and never looked at the result. Replies were
read later by whoever used the connection next, or never. That is fast (2.6
round trips per job; `Enqueue` never waits at all) but every Redis error was
silently dropped, and an enqueue-only program let unread replies pile up
without limit. Swapping in redigo's pool made those round trips synchronous
(its pool drains pending replies before reuse), and the bug fixes made every
pipeline check its replies.

**Getting the speed back without giving up correctness.** Everything since
reduces round trips while still checking every reply:

- one-round-trip finish: record the result and clear the worker's entry in one
  pipeline (5.0 → 4.0 round trips per job, +25%);
- skip queues already found empty in a pass (weighted queues only, +29% there);
- scripted fetch: one Lua script pops from the first non-empty queue and bumps
  the poller's stat (4.0 → 3.0 round trips, +65% at the default two
  connections, +95% with ten, back to master's throughput).

The remaining gap at two connections is pool contention: each round trip holds
a connection, and 25 workers plus the poller share two.

`Enqueue` is now one round trip per job (~390/s on the slow network, versus
master's unbounded fire-and-forget). A batch `Enqueue` that pipelines many jobs
would recover most of that safely.

## Redis clients

`clients/` runs goworker's per-job pattern (poller: `LPOP` ×2 and `INCR`;
worker: a two-`SET` start pipeline and a finish pipeline) with one poller
feeding 25 workers, 3 runs each:

| Client | Local (jobs/s) | Slow net (jobs/s) | rtts/job | Code lines | Deps pulled in |
|---|--:|--:|--:|--:|--:|
| redigo v1.9.3, pool 2 / 10 | **6,748** / **6,926** | 126 / 124 | 5.0 | 76 | 0 |
| go-redis v9.22.0, pool 2 / 10 | 5,909 / 5,457 | 125 / 125 | 5.0 | 45 | 3 |
| radix v4.1.5, pool 2 / 10 | 4,531 / 4,615 | 124 / 124 | 4.4 / 4.9 | 41 | 1 |
| rueidis v1.0.78 (multiplexed) | 4,749 | 124 | 4.7 | 54 | 1 |

(valkey-go is a fork of rueidis with the same API and was not run separately.)

- **On a slow network all four are the same.** The poller's commands depend
  on each other's replies, so they cannot be pipelined, and that serial chain
  is the bottleneck. radix's and rueidis's automatic pipelining batch some of
  the workers' concurrent commands (4.4–4.7 round trips instead of 5.0) but
  cannot shorten the poller's chain. Cutting round trips in goworker's own
  protocol, as the scripted fetch does, is what moves throughput.
- **Locally, redigo is fastest.** Its overhead per command is lowest; the
  auto-pipelining clients pay for handing commands between goroutines.
- **The others read better.** For the same operations redigo needs 76 lines to
  their 41–54, mostly because a pipeline returns error replies inside the reply
  slice rather than as an error, so every caller needs a helper to find them,
  and every `Send` returns an error to check:

  ```go
  // redigo (plus a 20-line helper that runs the pipeline and scans replies for redis.Error)
  return c.pipeline(ctx, func(conn redis.Conn) error {
  	if err := conn.Send("INCR", stat); err != nil {
  		return err
  	}
  	if err := conn.Send("INCR", workerStat); err != nil {
  		return err
  	}
  	return conn.Send("DEL", key, key+":started")
  })

  // go-redis
  _, err := c.rdb.Pipelined(ctx, func(p redis.Pipeliner) error {
  	p.Incr(ctx, stat)
  	p.Incr(ctx, workerStat)
  	p.Del(ctx, key, key+":started")
  	return nil
  })
  ```

**Recommendation.** Stay on redigo for now: it is the fastest here, adds no
dependencies, and switching would not raise throughput. It is also exposed in
goworker's public API (`GetConn` returns a `*RedisConn` wrapping
`redis.Conn`), so changing clients is a breaking change. If v1 drops
`GetConn`/`PutConn` in favor of a narrower API, go-redis is the natural choice
for readability and ecosystem; rueidis if very high concurrency makes
automatic pipelining pay off.

### Client-side caching

It would not help. Client-side caching (RESP3 `CLIENT TRACKING`; rueidis opts
every connection in and caches reads made with `DoCache`, and go-redis v9 has
it too) caches the replies to reads and has the server invalidate them when
keys change. goworker's per-job traffic is all
writes: the pop script, `SET`, `INCR`, `DEL`, `RPUSH`. Nothing it reads is read
twice, so there would be no hits, only tracking overhead. The clients above
run with caching disabled. It could help a dashboard that repeatedly reads
stats, but that is not goworker.
