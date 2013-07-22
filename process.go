package goworker

type process struct {
	Hostname string
	Pid      int
	Id       string
	Queues   []string
}
