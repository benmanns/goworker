package goworker

type Job struct {
	Queue   string
	Payload Payload
}
