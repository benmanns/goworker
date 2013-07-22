package goworker

type poller struct {
	process
}

func newPoller(queues []string) (*poller, error) {
	process, err := newProcess("poller", queues)
	if err != nil {
		return nil, err
	}
	return &poller{
		process: *process,
	}, nil
}
