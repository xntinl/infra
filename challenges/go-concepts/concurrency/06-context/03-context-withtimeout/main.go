package main

// Exercise: Context WithTimeout
// Instructions: see 03-context-withtimeout.md

import (
	"context"
	"fmt"
	"time"
)

// Step 1: Implement basicTimeout.
// Create a context with a 200ms timeout.
// Simulate a 500ms operation using time.After in a select.
// The timeout should fire first, printing ctx.Err().
func basicTimeout() {
	fmt.Println("=== Basic WithTimeout ===")
	// TODO: ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	// TODO: defer cancel()
	// TODO: select between time.After(500ms) and ctx.Done()
	//       - time.After: print "Operation completed successfully"
	//       - ctx.Done: print "Operation aborted: <ctx.Err()>"
}

// Step 2: Implement fastOperation.
// Create a context with a 500ms timeout.
// Simulate a 100ms operation -- it should complete before the timeout.
func fastOperation() {
	fmt.Println("=== Fast Operation (within timeout) ===")
	// TODO: ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	// TODO: defer cancel()
	// TODO: select between time.After(100ms) and ctx.Done()
	// TODO: print ctx.Err() after -- should be <nil>
}

// Step 3: Implement timeoutWithWorker.
// Create a context with a 350ms timeout.
// Launch a goroutine that loops, processing items (sleep 100ms each).
// The goroutine checks ctx.Done() in a select and reports back via a channel.
func timeoutWithWorker() {
	fmt.Println("=== Timeout with Worker Goroutine ===")
	// TODO: ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	// TODO: defer cancel()
	// TODO: create done channel (chan string)
	// TODO: launch goroutine that:
	//       - loops with select on ctx.Done() vs default
	//       - on ctx.Done(): send result message to done channel, return
	//       - on default: print item number, sleep 100ms
	// TODO: receive and print result from done channel
}

// Step 4: Implement earlyCancel.
// Create a context with a 5s timeout.
// Call cancel() after 100ms -- before the timeout fires.
// Observe that ctx.Err() returns context.Canceled, NOT DeadlineExceeded.
func earlyCancel() {
	fmt.Println("=== Early Cancel (before timeout) ===")
	// TODO: ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	// TODO: launch goroutine that waits on ctx.Done() and prints ctx.Err()
	// TODO: sleep 100ms
	// TODO: call cancel() manually
	// TODO: sleep briefly for goroutine output
}

// Verify: Implement simulateQuery and verifyKnowledge.
// simulateQuery simulates a database query that takes queryDuration.
// It should respect the context's cancellation/timeout.
func simulateQuery(ctx context.Context, queryDuration time.Duration) error {
	_ = ctx           // TODO: use in select
	_ = queryDuration // TODO: use with time.After
	// TODO: return nil on success, ctx.Err() on timeout/cancel
	return nil
}

func verifyKnowledge() {
	fmt.Println("=== Verify Knowledge ===")
	// TODO: call simulateQuery with timeout SHORTER than query -- should fail
	// TODO: call simulateQuery with timeout LONGER than query -- should succeed
	// TODO: print the error (or nil) for each case
}

func main() {
	fmt.Println("Exercise: Context WithTimeout\n")

	basicTimeout()
	fastOperation()
	timeoutWithWorker()
	earlyCancel()
	verifyKnowledge()
}
