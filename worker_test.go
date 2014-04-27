package goworker

import (
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
