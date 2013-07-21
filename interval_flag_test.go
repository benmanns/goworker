package goworker

import (
	"testing"
	"time"
)

var intervalFlagSetTests = []struct {
	v        string
	expected intervalFlag
}{
	{
		"0",
		intervalFlag(0),
	},
	{
		"1",
		intervalFlag(1 * time.Second),
	},
	{
		"1.5",
		intervalFlag(1500 * time.Millisecond),
	},
}

func TestIntervalFlagSet(t *testing.T) {
	for _, tt := range intervalFlagSetTests {
		actual := new(intervalFlag)
		if err := actual.Set(tt.v); err != nil {
			t.Errorf("IntervalFlag(%#v): set to %s error %s", actual, tt.v, err)
		} else {
			if *actual != tt.expected {
				t.Errorf("IntervalFlag: set to %s expected %v, actual %v", tt.v, tt.expected, actual)
			}
		}
	}
}

var intervalFlagSetFloatTests = []struct {
	v        float64
	expected intervalFlag
}{
	{
		0.0,
		intervalFlag(0),
	},
	{
		1.0,
		intervalFlag(1 * time.Second),
	},
	{
		1.5,
		intervalFlag(1500 * time.Millisecond),
	},
}

func TestIntervalFlagSetFloat(t *testing.T) {
	for _, tt := range intervalFlagSetFloatTests {
		actual := new(intervalFlag)
		if err := actual.SetFloat(tt.v); err != nil {
			t.Errorf("IntervalFlag(%#v): set to %f error %s", actual, tt.v, err)
		} else {
			if *actual != tt.expected {
				t.Errorf("IntervalFlag: set to %f expected %v, actual %v", tt.v, tt.expected, actual)
			}
		}
	}
}

var intervalFlagStringTests = []struct {
	i        intervalFlag
	expected string
}{
	{
		intervalFlag(0),
		"0",
	},
	{
		intervalFlag(1 * time.Second),
		"1000000000",
	},
	{
		intervalFlag(1500 * time.Millisecond),
		"1500000000",
	},
}

func TestIntervalFlagString(t *testing.T) {
	for _, tt := range intervalFlagStringTests {
		actual := tt.i.String()
		if actual != tt.expected {
			t.Errorf("IntervalFlag(%#v): expected %s, actual %s", tt.i, tt.expected, actual)
		}
	}
}
