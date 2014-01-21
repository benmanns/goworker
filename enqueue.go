package goworker

import (
	"encoding/json"
	"fmt"
)

// Enqueue function let you asynchronously enqueue a new job in Resque given
// the queue, the class name and the parameters.
//
// param queue: name of the queue (not including the namespace)
// param class: name of the Worker that can handle this job
// param args:  arguments to pass to the handler function. Must be the non-marshalled version.
//
// return an error if args cannot be marshalled
func Enqueue(queue string, class string, args []interface{}) (err error) {
	data := &payload{
		Class: class,
		Args:  args,
	}
	b, err := json.Marshal(data)
	if err != nil {
		return
	}

	// Retrieve a connection from the pool or create a new one if no pool is opened.
	if pool != nil && !pool.IsClosed() {
		resource, err := pool.Get()
		if err != nil {
			logger.Criticalf("Error on getting connection in goworker.Enqueue(%s, %s, %v)", queue, class, args)
			return
		}
		conn := resource.(*redisConn)
		defer pool.Put(conn)
	} else {
		// non-optimized mode, create a pool to avoid getting there.
		logger.Warn("No open pool available, will create a temporary connection to Redis.")
		conn, err := redisConnFromUri(uri)
		if err {
			logger.Criticalf("Error while connecting to Redis in goworker.Enqueue(%s, %s, %v)", queue, class, args)
			return
		}
		defer conn.Close()
	}

	// Push job in redis
	err = conn.Send("RPUSH", fmt.Sprintf("%squeue:%s", namespace, queue), b)
	if err != nil {
		logger.Criticalf("Cannot push message to redis in goworker.Enqueue(%s, %s, %v)", queue, class, args)
		return
	}

	conn.Flush()
	return
}
