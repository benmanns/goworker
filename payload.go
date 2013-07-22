package goworker

type payload struct {
	Class string        `json:"class"`
	Args  []interface{} `json:"args"`
}
