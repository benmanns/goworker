package goworker

import (
	"testing"
)

var processStringTests = []struct {
	p        process
	expected string
}{
	{
		process{},
		":0-:",
	},
	{
		process{
			Hostname: "hostname",
			Pid:      12345,
			ID:       "123",
			Queues:   []string{"high", "low"},
		},
		"hostname:12345-123:high,low",
	},
}

func TestProcessString(t *testing.T) {
	for _, tt := range processStringTests {
		actual := tt.p.String()
		if actual != tt.expected {
			t.Errorf("Process(%#v): expected %s, actual %s", tt.p, tt.expected, actual)
		}
	}
}
