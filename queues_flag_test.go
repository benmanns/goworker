package goworker

import (
	"errors"
	"fmt"
	"testing"
)

var queuesFlagSetTests = []struct {
	v        string
	expected queuesFlag
	err      error
}{
	{
		"",
		nil,
		errors.New("You must specify at least one queue."),
	},
	{
		"high",
		queuesFlag([]string{"high"}),
		nil,
	},
	{
		"high,low",
		queuesFlag([]string{"high", "low"}),
		nil,
	},
	{
		"high=2,low=1",
		queuesFlag([]string{"high", "high", "low"}),
		nil,
	},
	{
		"high=2,low",
		queuesFlag([]string{"high", "high", "low"}),
		nil,
	},
	{
		"low=1,high=2",
		queuesFlag([]string{"low", "high", "high"}),
		nil,
	},
}

func TestQueuesFlagSet(t *testing.T) {
	for _, tt := range queuesFlagSetTests {
		actual := new(queuesFlag)
		err := actual.Set(tt.v)
		if fmt.Sprint(actual) != fmt.Sprint(tt.expected) {
			t.Errorf("QueuesFlag: set to %s expected %v, actual %v", tt.v, tt.expected, actual)
		}
		if tt.err != nil && err.Error() != tt.err.Error() {
			t.Errorf("QueuesFlag: set to %s expected err %v, actual err %v", tt.v, tt.expected, actual)
		}
	}
}

var queuesFlagStringTests = []struct {
	q        queuesFlag
	expected string
}{
	{
		queuesFlag([]string{"high"}),
		"[high]",
	},
	{
		queuesFlag([]string{"high", "low"}),
		"[high low]",
	},
}

func TestQueuesFlagString(t *testing.T) {
	for _, tt := range queuesFlagStringTests {
		actual := tt.q.String()
		if actual != tt.expected {
			t.Errorf("QueuesFlag(%#v): expected %s, actual %s", tt.q, tt.expected, actual)
		}
	}
}

func TestParseQueueAndWeight(t *testing.T) {
	for _, tt := range parseQueueAndWeightTests {
		queue, weight, err := parseQueueAndWeight(tt.queueAndWeight)
		if queue != tt.queue {
			t.Errorf("parseQueueAndWeight#queue expected %s, actual %s", tt.queue, queue)
		}
		if weight != tt.weight {
			t.Errorf("parseQueueAndWeight#weight expected %d, actual %d", tt.weight, weight)
		}
		if err != tt.err {
			t.Errorf("parseQueueAndWeight#err expected %v, actual %v", tt.err, err)
		}
	}
}

var parseQueueAndWeightTests = []struct {
	queueAndWeight string
	queue          string
	weight         int
	err            error
}{
	{
		"q==",
		"",
		0,
		errorNoneNumericWeight,
	},
	{
		"q=a",
		"",
		0,
		errorNoneNumericWeight,
	},
	{
		"",
		"",
		1,
		nil,
	},
	{
		"q",
		"q",
		1,
		nil,
	},
	{
		"q=2",
		"q",
		2,
		nil,
	},
}
