package goworker

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// signals returns a channel that is closed when the process
// receives a QUIT, TERM, or INT signal, and a function that
// stops listening for signals. Call stop once the returned
// channel is no longer needed so that signals regain their
// default behavior.
func signals() (quit <-chan struct{}, stop func()) {
	q := make(chan struct{})
	done := make(chan struct{})
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGQUIT, syscall.SIGTERM, os.Interrupt)

	go func() {
		defer signal.Stop(sigs)
		select {
		case <-sigs:
			close(q)
		case <-done:
		}
	}()

	var once sync.Once
	return q, func() { once.Do(func() { close(done) }) }
}
