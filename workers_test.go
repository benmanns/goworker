package goworker

import (
	"reflect"
	"testing"
)

func TestWorkers(t *testing.T) {
	Register("SomeJob", fakePerformer)
	workers := Workers()
	if reflect.DeepEqual(workers, []string{"SomeJob"}) {
		t.Error("Excepted worker \"SomeJob\" to be registered")
	}
}

func fakePerformer(queue string, args ...interface{}) error {
	return nil
}
