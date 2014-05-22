package goworker

import (
	"time"
)

type work struct {
	Queue   string    `json:"queue"`
	RunAt   time.Time `json:"run_at"`
	Payload Payload   `json:"payload"`
}
