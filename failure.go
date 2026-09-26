package goworker

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type failure struct {
	FailedAt  time.Time `json:"failed_at"`
	Payload   Payload   `json:"payload"`
	Exception string    `json:"exception"`
	Error     string    `json:"error"`
	Backtrace []string  `json:"backtrace"`
	Worker    string    `json:"worker"`
	Queue     string    `json:"queue"`
}

// panicError is returned for a worker function that panicked.
type panicError struct {
	value any
	stack []byte
}

func (e *panicError) Error() string {
	return fmt.Sprint(e.value)
}

// recordFailure pushes a Resque-compatible failure record for
// job onto the failed list and updates the failure stats for
// the process p.
func recordFailure(conn *RedisConn, p string, job *Job, err error, backtrace []string) error {
	cmds, err := failureCommands(p, job, err, backtrace)
	if err != nil {
		return err
	}
	return pipeline(conn, cmds...)
}

// failureCommands returns the commands that recordFailure sends,
// so callers can pipeline them with others.
func failureCommands(p string, job *Job, err error, backtrace []string) ([]redisCommand, error) {
	if pe, ok := errors.AsType[*panicError](err); ok && backtrace == nil {
		backtrace = strings.Split(strings.TrimSpace(string(pe.stack)), "\n")
	}
	if backtrace == nil {
		// Resque clients expect an array here, not null.
		backtrace = []string{}
	}
	buffer, merr := json.Marshal(&failure{
		FailedAt:  time.Now(),
		Payload:   job.Payload,
		Exception: "Error",
		Error:     err.Error(),
		Backtrace: backtrace,
		Worker:    p,
		Queue:     job.Queue,
	})
	if merr != nil {
		return nil, merr
	}
	return []redisCommand{
		command("RPUSH", fmt.Sprintf("%sfailed", workerSettings.Namespace), buffer),
		command("INCR", fmt.Sprintf("%sstat:failed", workerSettings.Namespace)),
		command("INCR", fmt.Sprintf("%sstat:failed:%s", workerSettings.Namespace, p)),
	}, nil
}
