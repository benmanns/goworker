package goworker

// Job is a unit of work: a payload and the queue it was
// read from or will be pushed to.
type Job struct {
	Queue   string
	Payload Payload
}
