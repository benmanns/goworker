package goworker

var (
	workers map[string]WorkerFunc
)

func init() {
	workers = make(map[string]WorkerFunc)
}

// Registers a goworker worker function. Class refers to the
// Ruby name of the class which enqueues the job. Worker
// is a function which accepts a queue and an arbitrary
// array of interfaces as arguments.
func Register(class string, worker WorkerFunc) {
	workers[class] = worker
}
