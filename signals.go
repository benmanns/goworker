// Signal Handling in goworker
//
// To stop goworker, send a QUIT, TERM, or INT
// signal to the process. This will immediately
// stop job polling. There can be up to
// $CONCURRENCY jobs currently running, which
// will continue to run until they are finished.
// A second signal is handled by the Go runtime's
// default behavior, which terminates the process.
//
// # Failure Modes
//
// Like Resque, goworker makes no guarantees
// about the safety of jobs in the event of
// process shutdown. Workers must be both
// idempotent and tolerant to loss of the job in
// the event of failure.
//
// If the process is killed with a KILL or by a
// system failure, there may be one job that is
// currently in the poller's buffer that will be
// lost without any representation in either the
// queue or the worker variable.
//
// If you are running Goworker on a system like
// Heroku, which sends a TERM to signal a process
// that it needs to stop, ten seconds later sends
// a KILL to force the process to stop, your jobs
// must finish within 10 seconds or they may be
// lost. Jobs will be recoverable from the Redis
// database under
//
//	resque:worker:<hostname>:<process-id>-<worker-id>:<queues>
//
// as a JSON object with keys queue, run_at, and
// payload, but the process is manual.
// Additionally, there is no guarantee that the
// job in Redis under the worker key has not
// finished, if the process is killed before
// goworker can flush the update to Redis.
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
