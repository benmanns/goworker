## [Unreleased]

### Added

- Closed function to be able to wait for the workers to fully finish
  ([PR #6](https://github.com/cycloidio/goworker/issues/6))

## [0.1.7] _2021-06-09_

### Fixed

- Solved error when trying to prune workers with `go-redis`
  ([PR #4](https://github.com/cycloidio/goworker/pull/4))

## [0.1.6] _2021-06-08_

### Changed

- Change the module definition from `github.com/benmanns/goworker` to `github.com/cycloidio/goworker`
  ([PR #3](https://github.com/cycloidio/goworker/pull/3))

## [0.1.5] _2021-06-08_

### Added

- Added heartbeat and prune functions to clean stuck workers
  ([Issue benmanns/goworker#65](https://github.com/benmanns/goworker/issues/65))

### Changed

- Moved from `redigo` to `go-redis` 
  ([Issue benmanns/goworker#69](https://github.com/benmanns/goworker/issues/69))

## [0.1.4] _2021-06-07_

Fork from https://github.com/benmanns/goworker from master beeing https://github.com/benmanns/goworker/commit/d28a4f34a4d183f3ea2e51b4b8268807e0984942
