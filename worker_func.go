package goworker

type workerFunc func(string, ...any) error
