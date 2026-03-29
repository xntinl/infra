package main

import (
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"
)

func main() {
	unboundedConcurrency()
	boundedWithSetLimit()
	boundedWithSemaphore()
}

// unboundedConcurrency launches all tasks at once with no limit.
// All 20 tasks start nearly simultaneously.
func unboundedConcurrency() {
	fmt.Println("=== Unbounded Concurrency ===")
	start := time.Now()
	var g errgroup.Group

	for i := 0; i < 20; i++ {
		i := i
		g.Go(func() error {
			fmt.Printf("  [%v] Task %2d: started\n",
				time.Since(start).Round(time.Millisecond), i)
			time.Sleep(200 * time.Millisecond)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		fmt.Printf("Error: %v\n", err)
	}
	fmt.Printf("Total time: %v (all ran in parallel)\n", time.Since(start).Round(time.Millisecond))
}

// boundedWithSetLimit uses g.SetLimit(3) to run at most 3 tasks concurrently.
// TODO: Create an errgroup, set a limit of 3, and launch 20 tasks.
// Observe that tasks start in batches of 3.
func boundedWithSetLimit() {
	fmt.Println("\n=== Bounded Concurrency with SetLimit ===")
	start := time.Now()

	// TODO: Create an errgroup.Group
	// TODO: Call g.SetLimit(3) to limit concurrency

	// TODO: Launch 20 tasks with g.Go()
	// Each task should:
	//   1. Print "[timestamp] Task N: started"
	//   2. Sleep 200ms to simulate work
	//   3. Print "[timestamp] Task N: done"
	//   4. Return nil

	for i := 0; i < 20; i++ {
		_ = i // remove this line when implementing
		// TODO: g.Go(func() error { ... })
	}

	// TODO: Call g.Wait() and handle error

	fmt.Printf("Total time: %v\n", time.Since(start).Round(time.Millisecond))
	fmt.Println("TODO: implement this function")
}

// boundedWithSemaphore shows the manual semaphore pattern for comparison.
// TODO: Use a buffered channel of capacity 3 as a semaphore.
// Acquire before g.Go(), release inside the goroutine with defer.
func boundedWithSemaphore() {
	fmt.Println("\n=== Bounded Concurrency with Semaphore (manual) ===")
	start := time.Now()

	// TODO: Create an errgroup.Group
	// TODO: Create a buffered channel: sem := make(chan struct{}, 3)

	// TODO: For each of 20 tasks:
	//   1. Acquire semaphore: sem <- struct{}{}  (blocks if full)
	//   2. Launch with g.Go()
	//   3. Inside goroutine: defer func() { <-sem }() to release
	//   4. Do the same work as boundedWithSetLimit

	for i := 0; i < 20; i++ {
		_ = i
		// TODO: sem <- struct{}{}
		// TODO: g.Go(func() error { defer release; work; return nil })
	}

	// TODO: g.Wait()

	fmt.Printf("Total time: %v\n", time.Since(start).Round(time.Millisecond))
	fmt.Println("TODO: implement this function")
}
