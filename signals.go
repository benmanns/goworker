// Signal Handling in goworker
//
// To stop goworker, send a QUIT, TERM, or INT
// signal to the process. This will immediately
// stop job polling. There can be up to
// $CONCURRENCY jobs currently running, which
// will continue to run until they are finished.
//
// Failure Modes
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
	"syscall"
)

func signals() <-chan bool {
	quit := make(chan bool)

	go func() {
		signals := make(chan os.Signal)
		defer close(signals)

		signal.Notify(signals, syscall.SIGQUIT, syscall.SIGTERM, os.Interrupt)
		defer signalStop(signals)

		<-signals
		quit <- true
	}()

	return quit
}
