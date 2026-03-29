// Exercise 03: Errgroup SetLimit
//
// Demonstrates g.SetLimit(n) for bounded concurrency: limiting the number of
// goroutines running at the same time. Compares with the manual semaphore pattern.
//
// Expected output (timestamps approximate):
//
//   === Unbounded Concurrency (10 tasks) ===
//     [0ms] Task  0: started
//     [0ms] Task  1: started
//     ...all 10 start at ~0ms...
//   Total: ~200ms (all ran in parallel)
//
//   === Bounded with SetLimit(3) ===
//     [0ms]   Task  0: started
//     [0ms]   Task  1: started
//     [0ms]   Task  2: started
//     [200ms] Task  0: done
//     [200ms] Task  3: started
//     ...batches of 3...
//   Total: ~800ms (ceil(10/3) * 200ms)
//
//   === Bounded with Manual Semaphore ===
//     ...same batching behavior as SetLimit...
//   Total: ~800ms
//
//   === SetLimit with Errors ===
//     Task 5: returning error
//     ...remaining tasks still run (no context)...
//   Error: task 5 failed
//
//   === SetLimit + WithContext (cancel on error) ===
//     Task 5: returning error
//     Task 7: cancelled
//     ...tasks after the failure get cancelled...
//   Error: task 5 failed

package main

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"
)

func main() {
	unboundedConcurrency()
	boundedWithSetLimit()
	boundedWithSemaphore()
	setLimitWithErrors()
	setLimitWithContext()
}

// unboundedConcurrency launches all tasks at once. With 10 tasks, all start
// nearly simultaneously. In production this could overwhelm a downstream service.
func unboundedConcurrency() {
	fmt.Println("=== Unbounded Concurrency (10 tasks) ===")
	start := time.Now()
	var g errgroup.Group

	for i := 0; i < 10; i++ {
		i := i
		g.Go(func() error {
			fmt.Printf("  [%v] Task %2d: started\n",
				time.Since(start).Round(time.Millisecond), i)
			time.Sleep(200 * time.Millisecond)
			return nil
		})
	}

	_ = g.Wait()
	fmt.Printf("Total: %v (all ran in parallel)\n",
		time.Since(start).Round(time.Millisecond))
}

// boundedWithSetLimit uses SetLimit(3) so at most 3 goroutines run at once.
// When the limit is reached, g.Go() blocks the caller until a slot opens.
// This provides natural backpressure without channels or semaphores.
func boundedWithSetLimit() {
	fmt.Println("\n=== Bounded with SetLimit(3) ===")
	start := time.Now()

	var g errgroup.Group
	g.SetLimit(3) // MUST be called before any Go() call -- panics otherwise

	for i := 0; i < 10; i++ {
		i := i
		// This call blocks if 3 goroutines are already running
		g.Go(func() error {
			fmt.Printf("  [%v] Task %2d: started\n",
				time.Since(start).Round(time.Millisecond), i)
			time.Sleep(200 * time.Millisecond)
			fmt.Printf("  [%v] Task %2d: done\n",
				time.Since(start).Round(time.Millisecond), i)
			return nil
		})
	}

	_ = g.Wait()
	fmt.Printf("Total: %v (ceil(10/3) batches of ~200ms each)\n",
		time.Since(start).Round(time.Millisecond))
}

// boundedWithSemaphore achieves the same result with the manual semaphore pattern.
// A buffered channel of capacity N acts as a counting semaphore: acquire (send)
// before launching, release (receive) when the goroutine finishes.
// SetLimit replaces all of this with a single line.
func boundedWithSemaphore() {
	fmt.Println("\n=== Bounded with Manual Semaphore ===")
	start := time.Now()

	var g errgroup.Group
	sem := make(chan struct{}, 3) // capacity = max concurrency

	for i := 0; i < 10; i++ {
		i := i
		// Acquire: blocks if the channel is full (3 goroutines already running)
		sem <- struct{}{}
		g.Go(func() error {
			defer func() { <-sem }() // Release the semaphore slot when done
			fmt.Printf("  [%v] Task %2d: started\n",
				time.Since(start).Round(time.Millisecond), i)
			time.Sleep(200 * time.Millisecond)
			fmt.Printf("  [%v] Task %2d: done\n",
				time.Since(start).Round(time.Millisecond), i)
			return nil
		})
	}

	_ = g.Wait()
	fmt.Printf("Total: %v (same behavior as SetLimit)\n",
		time.Since(start).Round(time.Millisecond))
}

// setLimitWithErrors shows that SetLimit and error propagation work together.
// Task 5 fails, but without WithContext, all other tasks still run to completion.
func setLimitWithErrors() {
	fmt.Println("\n=== SetLimit with Errors ===")

	var g errgroup.Group
	g.SetLimit(3)

	for i := 0; i < 10; i++ {
		i := i
		g.Go(func() error {
			time.Sleep(100 * time.Millisecond)
			if i == 5 {
				fmt.Printf("  Task %d: returning error\n", i)
				return fmt.Errorf("task %d failed", i)
			}
			fmt.Printf("  Task %d: done\n", i)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}

// setLimitWithContext combines SetLimit with WithContext for bounded concurrency
// AND automatic cancellation. This is the production pattern for making N requests
// with at most M in flight, stopping on first failure.
func setLimitWithContext() {
	fmt.Println("\n=== SetLimit + WithContext (cancel on error) ===")

	g, ctx := errgroup.WithContext(context.Background())
	g.SetLimit(3)

	for i := 0; i < 10; i++ {
		i := i
		g.Go(func() error {
			// Check if a sibling already failed before doing work
			select {
			case <-ctx.Done():
				fmt.Printf("  Task %d: cancelled\n", i)
				return ctx.Err()
			default:
			}

			time.Sleep(100 * time.Millisecond)
			if i == 5 {
				fmt.Printf("  Task %d: returning error\n", i)
				return fmt.Errorf("task %d failed", i)
			}
			fmt.Printf("  Task %d: done\n", i)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}
