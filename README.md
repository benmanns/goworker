# goworker

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
	fmt.Printf("From %s, %v", queue, args)
	return
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
	"github.com/benmanns/goworker"
)

func newMyFunc(uri string) {
	foo := NewFoo(uri)
	return func(queue string, args ...interface{}) error {
		foo.Bar(args)
		return nil
	}
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

goworker worker functions receive the queue they are serving and a slice of interfaces. To use them as parameters to other functions, use Go type assertions to convert them into usable types.

```go
// Expecting (int, string, float64)
func myFunc(queue, args ...interface()) error {
	id, ok := args[0].(int)
	if !ok {
		return errorInvalidParam
	}
	name, ok := args[1].(string)
	if !ok {
		return errorInvalidParam
	}
	weight, ok := args[2].(float64)
	if !ok {
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
