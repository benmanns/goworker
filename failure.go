package goworker

import (
	"time"
)

type failure struct {
	FailedAt  time.Time `json:"failed_at"`
	Payload   payload   `json:"payload"`
	Exception string    `json:"exception"`
	Error     string    `json:"error"`
	Backtrace []string  `json:"backtrace"`
	Worker    *worker   `json:"worker"`
	Queue     string    `json:"queue"`
}
