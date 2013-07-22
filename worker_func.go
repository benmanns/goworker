package goworker

type WorkerFunc func(string, ...interface{}) error
