//go:build go1.27

package goworker

import (
	"bytes"
	"fmt"
	"os"
	"runtime/pprof"
	"strings"
	"testing"
	"time"
)

func init() {
	checkGoroutineLeaks = func() int {
		if n, stacks := goworkerLeaks(); n > 0 {
			fmt.Fprintf(os.Stderr, "goroutineleak: %d goroutine(s) leaked in goworker code:\n%s", n, stacks)
			return 1
		}
		return 0
	}
}

// goworkerLeaks runs the goroutineleak profile and returns the
// number of leaked goroutines whose stacks include goworker
// code, along with those stacks. The deliberate leak from
// TestGoroutineLeakProfileDetectsLeaks is ignored.
func goworkerLeaks() (int, string) {
	var n int
	var leaked strings.Builder
	for _, stack := range leakedStacks() {
		if strings.Contains(stack, "github.com/benmanns/goworker.") && !strings.Contains(stack, "TestGoroutineLeakProfileDetectsLeaks") {
			n++
			leaked.WriteString(stack + "\n\n")
		}
	}
	return n, leaked.String()
}

// leakedStacks returns the stack of every goroutine the
// goroutineleak profile reports as leaked. With debug=2 the
// profile also prints the calling goroutine, which is running
// rather than leaked, so only stacks marked "(leaked)" count.
func leakedStacks() []string {
	var buf bytes.Buffer
	if err := pprof.Lookup("goroutineleak").WriteTo(&buf, 2); err != nil {
		return []string{err.Error()}
	}
	var stacks []string
	for stack := range strings.SplitSeq(buf.String(), "\n\n") {
		header, _, _ := strings.Cut(stack, "\n")
		if strings.Contains(header, "(leaked)") {
			stacks = append(stacks, stack)
		}
	}
	return stacks
}

// TestGoroutineLeakProfileDetectsLeaks checks that the profile
// used by checkGoroutineLeaks really reports a goroutine that
// can never be unblocked, so a clean run means something.
func TestGoroutineLeakProfileDetectsLeaks(t *testing.T) {
	blocked := make(chan struct{})
	defer close(blocked)
	go func() { <-make(chan struct{}) }() // nothing can ever send
	go func() { <-blocked }()             // still reachable, not leaked

	// The goroutines may not have blocked yet, so poll briefly.
	var stacks []string
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		stacks = leakedStacks()
		// Earlier runs under -count leave their own leaked
		// goroutine behind, so look for at least one.
		if countContaining(stacks, "TestGoroutineLeakProfileDetectsLeaks") > 0 {
			return
		}
	}
	t.Fatalf("goroutineleak profile never reported the leaked goroutine:\n%s", strings.Join(stacks, "\n\n"))
}

func countContaining(stacks []string, s string) int {
	var n int
	for _, stack := range stacks {
		if strings.Contains(stack, s) {
			n++
		}
	}
	return n
}
