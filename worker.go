package goworker

import (
	"encoding/json"
)

type worker struct {
	process
}

func newWorker(id string, queues []string) (*worker, error) {
	process, err := newProcess(id, queues)
	if err != nil {
		return nil, err
	}
	return &worker{
		process: *process,
	}, nil
}

func (w *worker) MarshalJSON() ([]byte, error) {
	return json.Marshal(w.String())
}
