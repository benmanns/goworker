# goworker

[![Build Status](https://travis-ci.org/benmanns/goworker.png?branch=master)](https://travis-ci.org/benmanns/goworker)

goworker is a Resque-compatible, Go-based background worker. It allows you to push jobs into a queue using an expressive language like Ruby while harnessing the efficiency and concurrency of Go to minimize job latency and cost.

goworker workers can run alongside Ruby Resque clients so that you can keep all but your most resource-intensive jobs in Ruby.

## Installation

To install goworker, use

```sh
go get github.com/benmanns/goworker
```

to install the package, and then from your worker

```go
import "github.com/benmanns/goworker"
```

## Getting Started

To create a worker, write a function matching the signature

```go
func(string, ...interface{}) error
```

and register it using

```go
goworker.Register("MyClass", myFunc)
```

Here is a simple worker that prints its arguments:

```go
package main

import (
	"fmt"
	"github.com/benmanns/goworker"
)

func myFunc(queue string, args ...interface{}) error {
	fmt.Printf("From %s, %v\n", queue, args)
	return nil
}

func init() {
	goworker.Register("MyClass", myFunc)
}

func main() {
	if err := goworker.Work(); err != nil {
		fmt.Println("Error:", err)
	}
}
```

To create workers that share a database pool or other resources, use a closure to share variables.

```go
package main

import (
	"fmt"
	"github.com/benmanns/goworker"
)

func newMyFunc(uri string) (func(queue string, args ...interface{}) error) {
	foo := NewFoo(uri)
	return func(queue string, args ...interface{}) error {
		foo.Bar(args)
		return nil
	}
}

func init() {
	goworker.Register("MyClass", newMyFunc("http://www.example.com/"))
}

func main() {
	if err := goworker.Work(); err != nil {
		fmt.Println("Error:", err)
	}
}
```

goworker worker functions receive the queue they are serving and a slice of interfaces. To use them as parameters to other functions, use Go type assertions to convert them into usable types.

```go
// Expecting (int, string, float64)
func myFunc(queue, args ...interface{}) error {
	idNum, ok := args[0].(json.Number)
	if !ok {
		return errorInvalidParam
	}
	id, err := idNum.Int64()
	if err != nil {
		return errorInvalidParam
	}
	name, ok := args[1].(string)
	if !ok {
		return errorInvalidParam
	}
	weightNum, ok := args[2].(json.Number)
	if !ok {
		return errorInvalidParam
	}
	weight, err := weightNum.Float64()
	if err != nil {
		return errorInvalidParam
	}
	doSomething(id, name, weight)
	return nil
}
```

For testing, it is helpful to use the `redis-cli` program to insert jobs onto the Redis queue:

```sh
redis-cli -r 100 RPUSH resque:queue:myqueue '{"class":"MyClass","args":["hi","there"]}'
```

will insert 100 jobs for the `MyClass` worker onto the `myqueue` queue. It is equivalent to:

```ruby
class MyClass
  @queue = :myqueue
end

100.times do
  Resque.enqueue MyClass, ['hi', 'there']
end
```

## Flags

There are several flags which control the operation of the goworker client.

* `-queues="comma,delimited,queues"` — This is the only required flag. The recommended practice is to separate your Resque workers from your goworkers with different queues. Otherwise, Resque worker classes that have no goworker analog will cause the goworker process to fail the jobs. Because of this, there is no default queue, nor is there a way to select all queues (à la Resque's `*` queue). If you have multiple queues you can assign them weights. A queue with a weight of 2 will be checked twice as often as a queue with a weight of 1: `-queues='high=2,low=1'`.
* `-interval=5.0` — Specifies the wait period between polling if no job was in the queue the last time one was requested.
* `-concurrency=25` — Specifies the number of concurrently executing workers. This number can be as low as 1 or rather comfortably as high as 100,000, and should be tuned to your workflow and the availability of outside resources.
* `-connections=2` — Specifies the maximum number of Redis connections that goworker will consume between the poller and all workers. There is not much performance gain over two and a slight penalty when using only one. This is configurable in case you need to keep connection counts low for cloud Redis providers who limit plans on `maxclients`.
* `-uri=redis://localhost:6379/` — Specifies the URI of the Redis database from which goworker polls for jobs. Accepts URIs of the format `redis://user:pass@host:port/db` or `unix:///path/to/redis.sock`. The flag may also be set by the environment variable `$($REDIS_PROVIDER)` or `$REDIS_URL`. E.g. set `$REDIS_PROVIDER` to `REDISTOGO_URL` on Heroku to let the Redis To Go add-on configure the Redis database.
* `-namespace=resque:` — Specifies the namespace from which goworker retrieves jobs and stores stats on workers.
* `-exit-on-complete=false` — Exits goworker when there are no jobs left in the queue. This is helpful in conjunction with the `time` command to benchmark different configurations.

You can also configure your own flags for use within your workers. Be sure to set them before calling `goworker.Main()`. It is okay to call `flags.Parse()` before calling `goworker.Main()` if you need to do additional processing on your flags.

## Signal Handling in goworker

To stop goworker, send a `QUIT`, `TERM`, or `INT` signal to the process. This will immediately stop job polling. There can be up to `$CONCURRENCY` jobs currently running, which will continue to run until they are finished.

## Failure Modes

Like Resque, goworker makes no guarantees about the safety of jobs in the event of process shutdown. Workers must be both idempotent and tolerant to loss of the job in the event of failure.

If the process is killed with a `KILL` or by a system failure, there may be one job that is currently in the poller's buffer that will be lost without any representation in either the queue or the worker variable.

If you are running goworker on a system like Heroku, which sends a `TERM` to signal a process that it needs to stop, ten seconds later sends a `KILL` to force the process to stop, your jobs must finish within 10 seconds or they may be lost. Jobs will be recoverable from the Redis database under

```
resque:worker:<hostname>:<process-id>-<worker-id>:<queues>
```

as a JSON object with keys `queue`, `run_at`, and `payload`, but the process is manual. Additionally, there is no guarantee that the job in Redis under the worker key has not finished, if the process is killed before goworker can flush the update to Redis.

## Contributing

1. [Fork it](https://github.com/benmanns/goworker/fork)
2. Create your feature branch (`git checkout -b my-new-feature`)
3. Commit your changes (`git commit -am 'Add some feature'`)
4. Push to the branch (`git push origin my-new-feature`)
5. Create new Pull Request
