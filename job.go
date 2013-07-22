package goworker

type job struct {
	Queue   string
	Payload payload
}
