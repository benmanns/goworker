package goworker

import (
	"encoding/json"
	"fmt"
)

/*
Enqueue function let you enqueue a new job in Resque given
the queue, the class name and the parameters.
*/
func Enqueue(queue string, class string, args []interface{}) error {
	conn, err := redisConnFromUri(uri)
	if err != nil {
		return err
	}
	defer conn.Close()

	data := &payload{
		Class: class,
		Args:  args,
	}
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	err = conn.Send("RPUSH", fmt.Sprintf("%squeue:%s", namespace, queue), b)
	conn.Flush()
	if err != nil {
		return err
	}
	return nil
}
