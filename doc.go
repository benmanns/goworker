// goworker is a Resque-compatible, Go-based background
// worker. It allows you to push jobs into a queue using an
// expressive language like Ruby while harnessing the
// efficiency and concurrency of Go to minimize job latency
// and cost.
//
// goworker workers can run alongside Ruby Resque clients
// so that you can keep all but your most
// resource-intensive jobs in Ruby.
//
// To create a worker, write a function matching the
// signature
//
//	func(string, ...interface{}) error
//
// and register it using
//
//	goworker.Register("MyClass", myFunc)
//
// Here is a simple worker that prints its arguments:
//
//	package main
//
//	import (
//		"fmt"
//		"github.com/benmanns/goworker"
//	)
//
//	func myFunc(queue string, args ...interface{}) error {
//		fmt.Printf("From %s, %v", queue, args)
//		return
//	}
//
//	func init() {
//		goworker.Register("MyClass", myFunc)
//	}
//
//	func main() {
//		if err := goworker.Work(); err != nil {
//			fmt.Println("Error:", err)
//		}
//	}
//
// To create workers that share a database pool or other
// resources, use a closure to share variables. Clean up
// shared resources using the channel provided by the
// Signals function.
//
//	package main
//
//	import (
//		"github.com/benmanns/goworker"
//	)
//
//	func newMyFunc(uri string) func(string, args ...interface{}) error {
//		foo := NewFoo(uri)
//
//		quit := goworker.Signals()
//		go func() {
//			<-quit
//			foo.CleanUp()
//		}()
//
//		return func(queue string, args ...interface{}) error {
//			foo.Bar(args)
//			return nil
//		}
//	}
//
//	func init() {
//		goworker.Register("MyClass", newMyFunc())
//	}
//
//	func main() {
//		if err := goworker.Work(); err != nil {
//			fmt.Println("Error:", err)
//		}
//	}
//
// goworker worker functions receive the queue they are
// serving and a slice of interfaces. To use them as
// parameters to other functions, use Go type assertions
// to convert them into usable types.
//
//	// Expecting (int, string, float64)
//	func myFunc(queue, args ...interface{}) error {
//		id, ok := args[0].(int)
//		if !ok {
//			return errorInvalidParam
//		}
//		name, ok := args[1].(string)
//		if !ok {
//			return errorInvalidParam
//		}
//		weight, ok := args[2].(float64)
//		if !ok {
//			return errorInvalidParam
//		}
//		doSomething(id, name, weight)
//		return nil
//	}
//
// For testing, it is helpful to use the redis-cli program
// to insert jobs onto the Redis queue:
//
//	redis-cli -r 100 RPUSH resque:queue:myqueue '{"class":"MyClass","args":["hi","there"]}'
//
// will insert 100 jobs for the MyClass worker onto the
// myqueue queue. It is equivalent to:
//
//	class MyClass
//	  @queue = :myqueue
//	end
//
//	100.times do
//	  Resque.enqueue MyClass, ['hi', 'there']
//	end
package goworker
