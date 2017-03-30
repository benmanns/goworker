package goworker

type Deferred struct {
	Queue string      `json:"queue"`
	Class string      `json:"class"`
	Args  interface{} `json:"args"`
}
