package goworker

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
)

var workerMarshalJSONTests = []struct {
	w        worker
	expected []byte
}{
	{
		worker{},
		[]byte(`":0-:"`),
	},
	{
		worker{
			process: process{
				Hostname: "hostname",
				Pid:      12345,
				ID:       "123",
				Queues:   []string{"high", "low"},
			},
		},
		[]byte(`"hostname:12345-123:high,low"`),
	},
}

func TestWorkerMarshalJSON(t *testing.T) {
	for _, tt := range workerMarshalJSONTests {
		actual, err := tt.w.MarshalJSON()
		if err != nil {
			t.Errorf("Worker(%#v): error %s", tt.w, err)
		} else if string(actual) != string(tt.expected) {
			t.Errorf("Worker(%#v): expected %s, actual %s", tt.w, tt.expected, actual)
		}
	}
}

func TestEnqueue(t *testing.T) {
	expectedArgs := []interface{}{"a", "lot", "of", "params"}
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

	actualArgs := []interface{}{}
	actualQueueName := ""
	Register(jobName, func(queue string, args ...interface{}) error {
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

		Register(name, func(string, ...interface{}) error {
			return nil
		})
	})
	t.Run("test concurrent registration", func(t *testing.T) {
		name := "concurrentlyRegisteredWorker%d"

		var wg sync.WaitGroup
		for i := 1; i <= 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				Register(fmt.Sprintf(name, i), func(string, ...interface{}) error {
					return nil
				})
			}()
		}
		wg.Wait()
		for i := 1; i <= 10; i++ {
			if _, ok := workers.Get(fmt.Sprintf(name, i)); !ok {
				t.Errorf("worker %d was not registered", i)
			}
		}
	})
}
