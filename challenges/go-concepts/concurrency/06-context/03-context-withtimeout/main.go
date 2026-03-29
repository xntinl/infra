package main

// Expected output (timing approximate):
//
// Context WithTimeout
//
// === Basic Timeout: Slow Operation Exceeds Limit ===
//   Starting slow operation (needs 500ms, allowed 200ms)...
//   Operation aborted: context deadline exceeded
//
// === Fast Operation Completes Within Timeout ===
//   Starting fast operation (needs 100ms, allowed 500ms)...
//   Operation completed successfully
//   Context error after success: <nil>
//
// === Timeout with Worker Goroutine ===
//   worker: processing item 1
//   worker: processing item 2
//   worker: processing item 3
//   worker stopped at item 4: context deadline exceeded
//
// === Early Cancel vs Timeout: Different Errors ===
//   main: calling cancel() manually (timeout was 5s)
//   goroutine: context ended: context canceled
//   Key insight: Canceled (not DeadlineExceeded) because we cancelled manually
//
// === Child Cannot Extend Parent Timeout ===
//   Parent deadline remaining: ~1s
//   Child requested: 10s
//   Child actual deadline remaining: ~1s  (parent's wins)
//
// === Verify Knowledge ===
//   Fast query (100ms, timeout 500ms): success (<nil>)
//   Slow query (800ms, timeout 200ms): failed (context deadline exceeded)

import (
	"context"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Example 1: Operation exceeds its timeout
// ---------------------------------------------------------------------------
// WithTimeout creates a context that auto-cancels after the given duration.
// When the timeout fires, ctx.Done() closes and ctx.Err() returns
// context.DeadlineExceeded. The cancel func must STILL be deferred to free
// internal resources (a timer goroutine) immediately.
func basicTimeout() {
	fmt.Println("=== Basic Timeout: Slow Operation Exceeds Limit ===")

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel() // always defer -- frees the internal timer even if timeout fires first

	fmt.Println("  Starting slow operation (needs 500ms, allowed 200ms)...")

	select {
	case <-time.After(500 * time.Millisecond):
		fmt.Println("  Operation completed successfully")
	case <-ctx.Done():
		// The timeout (200ms) fires before the operation (500ms) finishes.
		fmt.Printf("  Operation aborted: %v\n", ctx.Err())
	}

	fmt.Println()
}

// ---------------------------------------------------------------------------
// Example 2: Operation completes before timeout
// ---------------------------------------------------------------------------
// When the work finishes before the timeout, the context is still alive.
// ctx.Err() returns nil because neither the timeout nor cancel() has fired.
func fastOperation() {
	fmt.Println("=== Fast Operation Completes Within Timeout ===")

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel() // required even when timeout will not fire -- stops the timer

	fmt.Println("  Starting fast operation (needs 100ms, allowed 500ms)...")

	select {
	case <-time.After(100 * time.Millisecond):
		fmt.Println("  Operation completed successfully")
	case <-ctx.Done():
		fmt.Printf("  Operation aborted: %v\n", ctx.Err())
	}

	// ctx.Err() is nil because the timeout has not fired and we have not cancelled.
	fmt.Printf("  Context error after success: %v\n", ctx.Err())
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Example 3: Worker goroutine with timeout
// ---------------------------------------------------------------------------
// A goroutine processes items in a loop, checking ctx.Done() between each.
// When the timeout fires, the goroutine reports what it accomplished and exits.
func timeoutWithWorker() {
	fmt.Println("=== Timeout with Worker Goroutine ===")

	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()

	done := make(chan string)

	go func(ctx context.Context) {
		for i := 1; ; i++ {
			select {
			case <-ctx.Done():
				// Timeout or cancellation -- report and exit.
				done <- fmt.Sprintf("worker stopped at item %d: %v", i, ctx.Err())
				return
			default:
				// No cancellation yet -- process the next item.
				fmt.Printf("  worker: processing item %d\n", i)
				time.Sleep(100 * time.Millisecond)
			}
		}
	}(ctx)

	result := <-done
	fmt.Printf("  %s\n", result)
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Example 4: Manual cancel before timeout fires
// ---------------------------------------------------------------------------
// When you call cancel() before the timeout, ctx.Err() returns
// context.Canceled -- NOT DeadlineExceeded. This distinction lets callers
// differentiate "we chose to stop" from "we ran out of time."
func earlyCancel() {
	fmt.Println("=== Early Cancel vs Timeout: Different Errors ===")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	// NOT deferring -- we will call cancel explicitly below.

	go func() {
		<-ctx.Done()
		fmt.Printf("  goroutine: context ended: %v\n", ctx.Err())
	}()

	time.Sleep(100 * time.Millisecond)
	fmt.Println("  main: calling cancel() manually (timeout was 5s)")
	cancel()

	time.Sleep(50 * time.Millisecond)
	fmt.Println("  Key insight: Canceled (not DeadlineExceeded) because we cancelled manually")
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Example 5: A child timeout cannot extend its parent
// ---------------------------------------------------------------------------
// If parent has 1s left, a child WithTimeout(10s) still expires at 1s.
// The shorter deadline always wins. This is a fundamental rule of the
// context tree.
func childCannotExtendParent() {
	fmt.Println("=== Child Cannot Extend Parent Timeout ===")

	parent, cancelParent := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancelParent()

	child, cancelChild := context.WithTimeout(parent, 10*time.Second)
	defer cancelChild()

	parentDeadline, _ := parent.Deadline()
	childDeadline, _ := child.Deadline()

	fmt.Printf("  Parent deadline remaining: ~%v\n", time.Until(parentDeadline).Round(time.Millisecond))
	fmt.Printf("  Child requested: 10s\n")
	fmt.Printf("  Child actual deadline remaining: ~%v  (parent's wins)\n",
		time.Until(childDeadline).Round(time.Millisecond))
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Verify: simulateQuery demonstrates timeout with a parameterized operation
// ---------------------------------------------------------------------------
// simulateQuery represents any blocking operation (DB query, HTTP call, etc.)
// that must respect the caller's context.
func simulateQuery(ctx context.Context, queryDuration time.Duration) error {
	select {
	case <-time.After(queryDuration):
		// Query finished before timeout.
		return nil
	case <-ctx.Done():
		// Context cancelled or timed out before query finished.
		return ctx.Err()
	}
}

func verifyKnowledge() {
	fmt.Println("=== Verify Knowledge ===")

	// Case 1: Query (100ms) finishes within timeout (500ms).
	ctx1, cancel1 := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel1()
	err1 := simulateQuery(ctx1, 100*time.Millisecond)
	fmt.Printf("  Fast query (100ms, timeout 500ms): success (%v)\n", err1)

	// Case 2: Query (800ms) exceeds timeout (200ms).
	ctx2, cancel2 := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel2()
	err2 := simulateQuery(ctx2, 800*time.Millisecond)
	fmt.Printf("  Slow query (800ms, timeout 200ms): failed (%v)\n", err2)
}

func main() {
	fmt.Println("Context WithTimeout")
	fmt.Println()

	basicTimeout()
	fastOperation()
	timeoutWithWorker()
	earlyCancel()
	childCannotExtendParent()
	verifyKnowledge()
}
