package goworker

import (
	"encoding/json"
	"fmt"
	"github.com/garyburd/redigo/redis"
	"testing"
)

func TestEnqueueHasNoDeadlock(t *testing.T) {
	exitOnComplete = true
	connections = 1
	queues.Set("test_enqueue_has_no_deadlock")
	concurrency = 1
	jobProcessed := false
	Register("NoDeadLock", func(q string, args ...interface{}) error {
		Enqueue("dummy", "Dummy", nil)
		jobProcessed = true
		return nil
	})
	Enqueue("test_enqueue_has_no_deadlock", "NoDeadLock", nil)
	err := Work()
	if !jobProcessed {
		t.Error("job has not been processed")
	}
	if err != nil {
		t.Errorf("Error occured %v", err)
	}
	resource, _ := pool.Get()
	conn := resource.(*redisConn)
	defer pool.Put(conn)
	defer conn.Do("DEL", fmt.Sprintf("%squeue:dummy", namespace))
	defer conn.Do("DEL", fmt.Sprintf("%squeue:test_enqueue_has_no_deadlock", namespace))
}

func TestEnqueueWriteToRedis(t *testing.T) {
	queues.Set("no")
	Enqueue("test2", "TestEnqueueWriteToRedis", nil)
	Work()
	resource, _ := pool.Get()
	conn := resource.(*redisConn)
	defer pool.Put(conn)
	defer conn.Do("DEL", fmt.Sprintf("%squeue:test2", namespace))
	res, err := conn.Do("LPOP", fmt.Sprintf("%squeue:test2", namespace))
	if err != nil {
		t.Errorf("%v", err)
	}
	jsonData, _ := redis.Bytes(res, nil)
	var data map[string]interface{}
	json.Unmarshal(jsonData, &data)
	if data["class"] != "TestEnqueueWriteToRedis" {
		t.Error(data["class"])
	}
}
