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
		errors.New("you must specify at least one queue"),
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
	{
		"low=,high=2",
		nil,
		errors.New("the weight must be a numeric value"),
	},
	{
		"low=a,high=2",
		nil,
		errors.New("the weight must be a numeric value"),
	},
	{
		"low=",
		nil,
		errors.New("the weight must be a numeric value"),
	},
	{
		"low=a",
		nil,
		errors.New("the weight must be a numeric value"),
	},
	{
		"high=2,,,=1",
		queuesFlag([]string{"high", "high"}),
		nil,
	},
	{
		",,,",
		nil,
		errors.New("you must specify at least one queue"),
	},
	{
		"=1",
		nil,
		errors.New("you must specify at least one queue"),
	},
}

func TestQueuesFlagSet(t *testing.T) {
	for _, tt := range queuesFlagSetTests {
		actual := new(queuesFlag)
		err := actual.Set(tt.v)
		if fmt.Sprint(actual) != fmt.Sprint(tt.expected) {
			t.Errorf("QueuesFlag: set to %s expected %v, actual %v", tt.v, tt.expected, actual)
		}
		if (err != nil && tt.err == nil) ||
			(err == nil && tt.err != nil) ||
			(err != nil && tt.err != nil && err.Error() != tt.err.Error()) {
			t.Errorf("QueuesFlag: set to %s expected err %v, actual err %v", tt.v, tt.err, err)
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
