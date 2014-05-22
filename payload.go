package goworker

type Payload struct {
	Class string        `json:"class"`
	Args  []interface{} `json:"args"`
}
