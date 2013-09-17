// +build !go1.1

package goworker

import (
	"os"
)

// Stops signals channel. This does not exist in
// Go less than 1.1.
func signalStop(c chan<- os.Signal) {
}
