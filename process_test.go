package goworker

import (
	"testing"
)

var (
	processStringTests = []struct {
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

	processPriorityQueueTests = []struct {
		p              *process
		expectedQueues []string
	}{
		// no queues, no priorities
		{
			&process{
				Pid:    1,
				Queues: []string{},
			},
			[]string{},
		},
		// queues + priorities for all queues
		{
			&process{
				Pid:    2,
				Queues: []string{"low-priority", "high-priority", "med-priority"},
				QueuesPriority: map[string]int{
					"high-priority": 5,
					"med-priority":  10,
					"low-priority":  15,
				},
			},
			[]string{"high-priority", "med-priority", "low-priority"},
		},
		// queues + priorities for some queues
		{
			&process{
				Pid:    3,
				Queues: []string{"low-priority", "high-priority", "med-priority"},
				QueuesPriority: map[string]int{
					"med-priority": 10,
					"low-priority": 15,
				},
			},
			[]string{"high-priority", "med-priority", "low-priority"},
		},
		// queues + no priorities (all queues get the same priority: zero)
		{
			&process{
				Pid:    4,
				Queues: []string{"low-priority", "high-priority", "med-priority"},
			},
			[]string{"low-priority", "high-priority", "med-priority"},
		},
		// queues + negative priority (automatically set to zero: the highest priority)
		{
			&process{
				Pid:    4,
				Queues: []string{"low-priority", "med-priority", "high-priority"},
				QueuesPriority: map[string]int{
					"high-priority": -10,
					"low-priority":  15,
				},
			},
			[]string{"med-priority", "high-priority", "low-priority"},
		},
	}
)

func TestProcessString(t *testing.T) {
	for _, tt := range processStringTests {
		actual := tt.p.String()
		if actual != tt.expected {
			t.Errorf("Process(%#v): expected %s, actual %s", tt.p, tt.expected, actual)
		}
	}
}

// TestProcessPriorityQueues verifies that queues get sorted by incremental priority (if priorities were provided)
func TestProcessPriorityQueues(t *testing.T) {
	for _, ppqt := range processPriorityQueueTests {
		ppqt.p.queuesSortedByPriority = ppqt.p.getQueuesSortedByPriority()
		actual := ppqt.p.queues(true)
		if !testStringSliceEq(ppqt.expectedQueues, actual) {
			t.Errorf("Process(%#v): expected %s, actual %s", ppqt.p.Pid, ppqt.expectedQueues, actual)
		}
	}
}

// testStringSliceEq returns true if a == b (a, b []string)
func testStringSliceEq(a, b []string) bool {
	// If one is nil, the other must also be nil.
	if (a == nil) != (b == nil) {
		return false
	}

	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
