package goworker

import (
	"flag"
	"reflect"
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
		} else {
			if string(actual) != string(tt.expected) {
				t.Errorf("Worker(%#v): expected %s, actual %s", tt.w, tt.expected, actual)
			}
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

	var err error
	err = flag.Set("exit-on-complete", "true")
	if err != nil {
		t.Errorf("Error setting flag exit-on-complete: %v", err)
	}
	err = flag.Set("queues", queueName)
	if err != nil {
		t.Errorf("Error setting flag queues: %v", err)
	}
	err = flag.Set("concurrency", "1")
	if err != nil {
		t.Errorf("Error setting flag concurrency: %v", err)
	}
	err = flag.Set("use-number", "true")
	if err != nil {
		t.Errorf("Error setting flag use-number: %v", err)
	}

	err = Enqueue(expectedJob)
	if err != nil {
		t.Errorf("Error while enqueue %s", err)
	}

	var actualArgs []interface{}
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
