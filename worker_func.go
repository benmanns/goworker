package goworker

type workerFunc func(string, ...interface{}) error
