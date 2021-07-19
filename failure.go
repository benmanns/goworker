package goworker

import "time"

const (
	retriedAtLayout = "2006/01/02 15:04:05"
	failedAtLayout  = "2006/01/02 15:04:05 -07:00"
)

type failure struct {
	FailedAt  string   `json:"failed_at"`
	Payload   Payload  `json:"payload"`
	Exception string   `json:"exception"`
	Error     string   `json:"error"`
	Backtrace []string `json:"backtrace"`
	Worker    *worker  `json:"worker"`
	Queue     string   `json:"queue"`
	RetriedAt string   `json:"retried_at"`
}

// GetRetriedAtTime returns the RetriedAt as a time.Time
// converting it from the string. If it's not set it'll return
// an empty time.Time
func (f *failure) GetRetriedAtTime() (time.Time, error) {
	if f.RetriedAt == "" {
		return time.Time{}, nil
	}

	return time.Parse(retriedAtLayout, f.RetriedAt)
}

// SetFailedAt will set the FailedAt value with t with
// the right format
func (f *failure) SetFailedAt(t time.Time) {
	f.FailedAt = t.Format(failedAtLayout)
}
