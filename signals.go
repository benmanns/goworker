package goworker

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// signals returns a channel that is closed when the process
// receives a QUIT, TERM, or INT signal, and a function that
// stops listening for signals and closes the channel. Call
// stop once the channel is no longer needed so that signals
// regain their default behavior.
func signals() (quit <-chan struct{}, stop func()) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGQUIT, syscall.SIGTERM, os.Interrupt)
	// Restore default handling after the first signal so a
	// second one terminates the process.
	context.AfterFunc(ctx, stop)
	return ctx.Done(), stop
}
