package goworker

// Payload is the Resque job payload: the class that
// selects which registered worker function runs, and the
// arguments passed to it.
type Payload struct {
	Class string `json:"class"`
	Args  []any  `json:"args"`
}
