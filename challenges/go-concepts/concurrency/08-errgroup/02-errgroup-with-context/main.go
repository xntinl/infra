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
}

// withoutContext shows that a plain errgroup runs all tasks to completion
// even when one fails. This wastes resources.
func withoutContext() {
	fmt.Println("=== Errgroup Without Context ===")
	var g errgroup.Group

	for i := 0; i < 5; i++ {
		i := i
		g.Go(func() error {
			// Task 1 fails immediately
			if i == 1 {
				fmt.Printf("  Task %d: failing immediately\n", i)
				return fmt.Errorf("task %d failed", i)
			}

			// Other tasks do expensive work that cannot be cancelled
			for step := 0; step < 3; step++ {
				time.Sleep(100 * time.Millisecond)
				fmt.Printf("  Task %d: step %d done (still working despite failure)\n", i, step)
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		fmt.Printf("Group error: %v\n", err)
	}
}

// withContext uses errgroup.WithContext for automatic cancellation.
// TODO: Replace the plain errgroup with errgroup.WithContext.
// TODO: Make goroutines check ctx.Done() so they stop when a sibling fails.
func withContext() {
	fmt.Println("\n=== Errgroup WithContext ===")

	// TODO: Replace this with: g, ctx := errgroup.WithContext(context.Background())
	var g errgroup.Group

	for i := 0; i < 5; i++ {
		i := i
		g.Go(func() error {
			// TODO: Add a select on ctx.Done() before starting work
			// to bail out if context is already cancelled

			for step := 0; step < 3; step++ {
				// TODO: Replace this sleep with a select on ctx.Done()
				// and time.After to enable cancellation
				time.Sleep(100 * time.Millisecond)
				fmt.Printf("  Task %d: step %d done\n", i, step)
			}

			if i == 1 {
				return fmt.Errorf("task %d: simulated failure", i)
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		fmt.Printf("Group error: %v\n", err)
	}

	// Remove this line once you use ctx in the goroutines
	_ = context.Background
}

// cancellationTimeline demonstrates the precise timing of cancellation.
// TODO: Create a fast task that fails at 100ms and a slow task that checks
// ctx.Done() every 50ms. Show the slow task getting cancelled.
func cancellationTimeline() {
	fmt.Println("\n=== Cancellation Timeline ===")

	// TODO: Record start time: start := time.Now()
	// TODO: Create errgroup with context: g, ctx := errgroup.WithContext(context.Background())

	// TODO: Launch "fast task" -- sleeps 100ms then returns an error
	// Print timestamp when it fails

	// TODO: Launch "slow task" -- loops 10 times with 50ms intervals
	// Uses select on ctx.Done() to detect cancellation
	// Print timestamp for each iteration and when cancelled

	// TODO: Call g.Wait() and print the error with timestamp

	fmt.Println("TODO: implement this function")
}
