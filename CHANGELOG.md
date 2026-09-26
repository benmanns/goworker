# Changelog

This project follows [Semantic Versioning](https://semver.org). While the
major version is 0, minor releases may contain breaking changes; they are
called out below.

## Unreleased (proposed v0.2.0)

### Requirements

- Go 1.26 or later.

### Dependencies

- Removed `vitess.io/vitess`, `github.com/cihub/seelog`, and
  `golang.org/x/net`. The only remaining dependency is
  `github.com/gomodule/redigo`, updated to v1.9.3.

### Added

- `SetLogger` to supply a `*slog.Logger`. It is safe to call while
  workers are running.
- `rediss://` and `redis://` URIs without a port default to 6379.

### Changed

- Logs are structured `log/slog` text (`time=… level=… msg=…`) rather than
  seelog's format. Levels map as Critical → ERROR, others unchanged.
- The poller retries after Redis errors instead of shutting the process
  down.
- Job payloads that are not valid JSON are moved to the failed list
  instead of stopping the process.
- Failure records include a stack trace for panics, and `backtrace` is
  `[]` instead of `null` otherwise.
- `Init` (and so `Enqueue`) no longer requires `-queues`; `Work` does.
- Zero values for `IntervalFloat`, `Concurrency`, `Connections`, and `URI`
  in `SetSettings` fall back to the flag defaults.
- The `worker:…:started` timestamp uses Resque's format.

### Fixed

- Queues were duplicated each time `Init` ran after `Close`.
- Signal handling stayed installed after `Work` returned.
- The poller could unregister itself after `Work` had closed the
  connection pool, leaving a stale entry in the Resque workers set.
- A worker could stop consuming jobs after a Redis connection error.
- `Enqueue` ignored Redis error replies.
- `-insecure-tls` was ignored when `-tls-cert` was set.

## v0.1.3 (2017-03-29) and earlier

See the [commit history](https://github.com/benmanns/goworker/commits/v0.1.3).
