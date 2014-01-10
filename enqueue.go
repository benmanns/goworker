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
	pool := getConnectionPool()
	data := &payload{
		Class: class,
		Args:  args,
	}
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	resource, err := pool.Get()
	if err != nil {
		logger.Criticalf("Error on getting connection in goworker.Enqueue(%s, %s, %v)", queue, class, args)
		return
	}
	conn := resource.(*redisConn)
	err = conn.Send("RPUSH", fmt.Sprintf("%squeue:%s", namespace, queue), b)
	pool.Put(conn)
	if err != nil {
		logger.Criticalf("Cannot push message to redis in goworker.Enqueue(%s, %s, %v)", queue, class, args)
		return
	}

	conn.Flush()
	return
}
