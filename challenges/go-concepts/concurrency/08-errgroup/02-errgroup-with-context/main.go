// Exercise 02: Errgroup with Context
//
// Demonstrates errgroup.WithContext: automatic cancellation when one task fails,
// cooperative shutdown via ctx.Done(), and the timing of cancellation propagation.
//
// Expected output (timestamps and ordering approximate):
//
//   === Without Context (all tasks run to completion) ===
//     Task 1: failing immediately
//     Task 0: step 0/2
//     Task 2: step 0/2
//     ...all tasks complete despite the failure...
//   Group error: task 1 failed
//
//   === With Context (sibling tasks cancel early) ===
//     Task 0: step 0/2
//     Task 1: failing immediately -- this cancels the context
//     Task 2: cancelled at step 0
//     ...remaining tasks bail out...
//   Group error: task 1 failed
//
//   === Cancellation Timeline ===
//     [50ms]  Slow task: iteration 0
//     [100ms] Fast task: returning error
//     [100ms] Slow task: cancelled at iteration 1
//     [100ms] Wait returned: fast task failed
//
//   === Context Is Also Cancelled on Success ===
//   ctx.Err() after Wait: context canceled
//
//   === Passing ctx to Standard Library Functions ===
//     Task 0: http fetch completed
//     Task 1: returning error
//     Task 2: http fetch cancelled: context canceled
//   Group error: task 1 deliberate failure

package main

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"
)

func main() {
	withoutContext()
	withContext()
	cancellationTimeline()
	contextCancelledOnSuccess()
	passingContextToLibs()
}

// withoutContext uses a plain errgroup.Group. When task 1 fails, the other tasks
// keep running to completion because nothing signals them to stop.
func withoutContext() {
	fmt.Println("=== Without Context (all tasks run to completion) ===")
	var g errgroup.Group

	for i := 0; i < 4; i++ {
		i := i
		g.Go(func() error {
			if i == 1 {
				fmt.Printf("  Task %d: failing immediately\n", i)
				return fmt.Errorf("task %d failed", i)
			}

			// Simulate multi-step work with no way to cancel
			for step := 0; step < 3; step++ {
				time.Sleep(80 * time.Millisecond)
				fmt.Printf("  Task %d: step %d/%d (still working despite failure)\n", i, step, 2)
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		fmt.Printf("Group error: %v\n", err)
	}
}

// withContext uses errgroup.WithContext. When task 1 fails, the derived context
// is cancelled automatically. Sibling tasks detect this via ctx.Done() and stop early.
func withContext() {
	fmt.Println("\n=== With Context (sibling tasks cancel early) ===")

	// WithContext returns a Group AND a derived context.
	// The context is cancelled when the first goroutine returns a non-nil error.
	g, ctx := errgroup.WithContext(context.Background())

	for i := 0; i < 4; i++ {
		i := i
		g.Go(func() error {
			// Check cancellation before starting work.
			// A task launched after another has already failed might find ctx already done.
			select {
			case <-ctx.Done():
				fmt.Printf("  Task %d: cancelled before starting\n", i)
				return ctx.Err()
			default:
			}

			if i == 1 {
				fmt.Printf("  Task %d: failing immediately -- this cancels the context\n", i)
				return fmt.Errorf("task %d failed", i)
			}

			// Simulate multi-step work with cancellation checks between steps
			for step := 0; step < 3; step++ {
				select {
				case <-ctx.Done():
					// A sibling failed -- stop wasting resources
					fmt.Printf("  Task %d: cancelled at step %d\n", i, step)
					return ctx.Err()
				case <-time.After(80 * time.Millisecond):
					fmt.Printf("  Task %d: step %d/%d\n", i, step, 2)
				}
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		fmt.Printf("Group error: %v\n", err)
	}
}

// cancellationTimeline demonstrates precise timing of cancellation propagation.
// A fast task fails at ~100ms; a slow task (checking every ~50ms) detects the
// cancellation shortly after and stops.
func cancellationTimeline() {
	fmt.Println("\n=== Cancellation Timeline ===")
	start := time.Now()
	g, ctx := errgroup.WithContext(context.Background())

	// Fast task: fails after 100ms
	g.Go(func() error {
		time.Sleep(100 * time.Millisecond)
		elapsed := time.Since(start).Round(time.Millisecond)
		fmt.Printf("  [%v] Fast task: returning error\n", elapsed)
		return fmt.Errorf("fast task failed")
	})

	// Slow task: would run for 500ms but gets cancelled at ~100ms
	g.Go(func() error {
		for i := 0; i < 10; i++ {
			select {
			case <-ctx.Done():
				elapsed := time.Since(start).Round(time.Millisecond)
				fmt.Printf("  [%v] Slow task: cancelled at iteration %d\n", elapsed, i)
				return ctx.Err()
			case <-time.After(50 * time.Millisecond):
				elapsed := time.Since(start).Round(time.Millisecond)
				fmt.Printf("  [%v] Slow task: iteration %d\n", elapsed, i)
			}
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		elapsed := time.Since(start).Round(time.Millisecond)
		fmt.Printf("  [%v] Wait returned: %v\n", elapsed, err)
	}
}

// contextCancelledOnSuccess shows that the derived context is cancelled even when
// all tasks succeed. The context is cancelled when Wait() returns, regardless of
// whether there was an error. This is important: do not use the derived context
// for work that outlives the group.
func contextCancelledOnSuccess() {
	fmt.Println("\n=== Context Is Also Cancelled on Success ===")

	g, ctx := errgroup.WithContext(context.Background())

	g.Go(func() error {
		return nil // succeeds
	})

	_ = g.Wait()

	// After Wait returns, the context is always cancelled
	fmt.Printf("ctx.Err() after Wait: %v\n", ctx.Err())
}

// passingContextToLibs demonstrates passing the errgroup-derived context to
// standard library functions that accept context.Context. This is the real power:
// when a sibling fails, HTTP requests, database queries, and other blocking I/O
// get cancelled automatically through the context.
func passingContextToLibs() {
	fmt.Println("\n=== Passing ctx to Standard Library Functions ===")

	g, ctx := errgroup.WithContext(context.Background())

	// Simulate three "HTTP fetches" where task 1 fails
	for i := 0; i < 3; i++ {
		i := i
		g.Go(func() error {
			if i == 1 {
				time.Sleep(50 * time.Millisecond)
				fmt.Printf("  Task %d: returning error\n", i)
				return fmt.Errorf("task %d deliberate failure", i)
			}

			// simulateHTTPFetch respects the context -- it returns early
			// if the context is cancelled, just like http.NewRequestWithContext would
			err := simulateHTTPFetch(ctx, i, 200*time.Millisecond)
			if err != nil {
				fmt.Printf("  Task %d: http fetch cancelled: %v\n", i, err)
				return err
			}
			fmt.Printf("  Task %d: http fetch completed\n", i)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		fmt.Printf("Group error: %v\n", err)
	}
}

// simulateHTTPFetch pretends to be an HTTP call that respects context cancellation.
// In real code this would be http.NewRequestWithContext(ctx, ...).
func simulateHTTPFetch(ctx context.Context, id int, duration time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(duration):
		return nil
	}
}
