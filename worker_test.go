package goworker

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestEnqueue(t *testing.T) {
	expectedArgs := []any{"a", "lot", "of", "params"}
	jobName := "SomethingCool"
	queueName := "testQueue"
	expectedJob := &Job{
		Queue: queueName,
		Payload: Payload{
			Class: jobName,
			Args:  expectedArgs,
		},
	}

	setupRedisTest(t, queueName)

	err := Enqueue(expectedJob)
	if err != nil {
		t.Errorf("Error while enqueue %s", err)
	}

	actualArgs := []any{}
	actualQueueName := ""
	Register(jobName, func(queue string, args ...any) error {
		actualArgs = args
		actualQueueName = queue
		return nil
	})
	if err := Work(); err != nil {
		t.Errorf("(Enqueue) Failed on work %s", err)
	}
	if !reflect.DeepEqual(actualArgs, expectedArgs) {
		t.Errorf("(Enqueue) Expected %v, actual %v", actualArgs, expectedArgs)
	}
	if !reflect.DeepEqual(actualQueueName, queueName) {
		t.Errorf("(Enqueue) Expected %v, actual %v", actualQueueName, queueName)
	}
}

// Use "go test -race -run TestRegister" to check for race conditions.
func TestRegister(t *testing.T) {
	t.Run("test normal registration", func(_ *testing.T) {
		name := "oneWorker"

		Register(name, func(string, ...any) error {
			return nil
		})
	})
	t.Run("test concurrent registration", func(t *testing.T) {
		name := "concurrentlyRegisteredWorker%d"

		var wg sync.WaitGroup
		for i := 1; i <= 10; i++ {
			wg.Go(func() {
				Register(fmt.Sprintf(name, i), func(string, ...any) error {
					return nil
				})
			})
		}
		wg.Wait()
		for i := 1; i <= 10; i++ {
			if _, ok := workers.Get(fmt.Sprintf(name, i)); !ok {
				t.Errorf("worker %d was not registered", i)
			}
		}
	})
}
