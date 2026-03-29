// Exercise 01: Errgroup Basics
//
// Demonstrates errgroup.Group fundamentals: Go(), Wait(), first-error propagation,
// and the contrast with the manual WaitGroup + mutex pattern.
//
// Expected output (order of fetched lines may vary):
//
//   === Manual WaitGroup + Error Handling ===
//     Fetched: https://example.com
//     Fetched: https://example.org
//     Fetched: https://example.net
//   First error: failed to fetch "INVALID": invalid URL
//
//   === Errgroup Basic ===
//     Fetched: https://example.com
//     Fetched: https://example.org
//     Fetched: https://example.net
//   First error: failed to fetch "INVALID": invalid URL
//
//   === Errgroup Multiple Errors ===
//     Task 1 succeeded
//     Task 3 succeeded
//   Wait returned: task 0 failed (only first error is kept)
//
//   === Errgroup Zero Value ===
//   Wait returned nil -- zero-value group with no tasks succeeds
//
//   === New Group Per Batch ===
//   Batch 1 error: batch-1 task 1 failed
//   Batch 2 error: <nil>

package main

import (
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

func main() {
	manualWaitGroupErrors()
	errgroupBasic()
	errgroupMultipleErrors()
	errgroupZeroValue()
	errgroupNewGroupPerBatch()
}

// fetchURL simulates a network fetch. Returns an error for the sentinel "INVALID".
func fetchURL(url string) error {
	time.Sleep(100 * time.Millisecond)
	if url == "INVALID" {
		return fmt.Errorf("failed to fetch %q: invalid URL", url)
	}
	fmt.Printf("  Fetched: %s\n", url)
	return nil
}

// manualWaitGroupErrors shows the manual pattern that errgroup replaces:
// WaitGroup for synchronization, mutex + variable for error capture.
// This requires four separate primitives wired together correctly.
func manualWaitGroupErrors() {
	fmt.Println("=== Manual WaitGroup + Error Handling ===")

	urls := []string{
		"https://example.com",
		"https://example.org",
		"INVALID",
		"https://example.net",
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for _, url := range urls {
		url := url
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fetchURL(url); err != nil {
				mu.Lock()
				// Only keep the first error -- mimic errgroup semantics
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if firstErr != nil {
		fmt.Printf("First error: %v\n", firstErr)
	} else {
		fmt.Println("All tasks succeeded")
	}
}

// errgroupBasic solves the same problem with errgroup.Group.
// g.Go() replaces go+Add+Done; g.Wait() replaces wg.Wait() + mutex + error var.
func errgroupBasic() {
	fmt.Println("\n=== Errgroup Basic ===")

	urls := []string{
		"https://example.com",
		"https://example.org",
		"INVALID",
		"https://example.net",
	}

	// Zero value is ready to use -- no constructor needed
	var g errgroup.Group

	for _, url := range urls {
		url := url // capture loop variable for the closure
		// g.Go accepts func() error, handles Add/Done internally
		g.Go(func() error {
			return fetchURL(url)
		})
	}

	// Wait blocks until every goroutine launched by Go() completes.
	// It returns the first non-nil error, or nil if all succeeded.
	if err := g.Wait(); err != nil {
		fmt.Printf("First error: %v\n", err)
	} else {
		fmt.Println("All tasks succeeded")
	}
}

// errgroupMultipleErrors proves that Wait() returns only the first error.
// Tasks 0, 2, 4 all fail, but Wait() keeps only one. The others are silently
// discarded. If you need all errors, collect them yourself (mutex + slice).
func errgroupMultipleErrors() {
	fmt.Println("\n=== Errgroup Multiple Errors ===")

	var g errgroup.Group

	for i := 0; i < 5; i++ {
		i := i
		g.Go(func() error {
			// Stagger execution so task 0 fails first deterministically
			time.Sleep(time.Duration(i) * 50 * time.Millisecond)
			if i%2 == 0 {
				return fmt.Errorf("task %d failed", i)
			}
			fmt.Printf("  Task %d succeeded\n", i)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		fmt.Printf("Wait returned: %v (only first error is kept)\n", err)
	}
}

// errgroupZeroValue shows that the zero value of errgroup.Group is usable.
// Calling Wait() on a group with no tasks returns nil immediately.
func errgroupZeroValue() {
	fmt.Println("\n=== Errgroup Zero Value ===")

	var g errgroup.Group
	// No tasks launched -- Wait returns nil instantly
	err := g.Wait()
	fmt.Printf("Wait returned %v -- zero-value group with no tasks succeeds\n", err)
}

// errgroupNewGroupPerBatch shows the correct pattern for sequential batches:
// create a new errgroup.Group for each batch. An errgroup.Group should not be
// reused after Wait() returns -- always create a fresh one for new work.
func errgroupNewGroupPerBatch() {
	fmt.Println("\n=== New Group Per Batch ===")

	// Batch 1: one task fails
	var g1 errgroup.Group
	for i := 0; i < 3; i++ {
		i := i
		g1.Go(func() error {
			if i == 1 {
				return fmt.Errorf("batch-1 task %d failed", i)
			}
			return nil
		})
	}
	err1 := g1.Wait()
	fmt.Printf("Batch 1 error: %v\n", err1)

	// Batch 2: fresh group, all tasks succeed
	var g2 errgroup.Group
	for i := 0; i < 3; i++ {
		g2.Go(func() error {
			return nil
		})
	}
	err2 := g2.Wait()
	fmt.Printf("Batch 2 error: %v\n", err2)
}
