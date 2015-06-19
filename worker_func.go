package goworker

//type workerFunc func(string, ...interface{}) error

type workerFunc func(*Worker, *Job) error
