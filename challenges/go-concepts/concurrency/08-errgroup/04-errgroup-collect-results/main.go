// Exercise 04: Errgroup Collect Results
//
// Demonstrates safe patterns for collecting data from parallel errgroup tasks:
// index-based (no mutex), mutex-guarded (for dynamic/filtered results), and
// partial results when some tasks fail.
//
// Expected output (run with -race flag to verify safety):
//
//   === Unsafe Collect (data race!) ===
//   Got 10 results (may be wrong due to race)
//
//   === Collect by Index (no mutex needed) ===
//   Results (ordered):
//     [0] processed-alpha
//     [1] processed-bravo
//     [2] processed-charlie
//     [3] processed-delta
//     [4] processed-echo
//
//   === Collect with Mutex (filtered results) ===
//   Collected 5 even results:
//     result-0
//     result-2
//     ...
//
//   === Collect with Partial Results on Error ===
//   Error: task 2 ("FAIL") failed
//   Partial results:
//     [0] processed-alpha
//     [1] processed-bravo
//     [2] (empty -- task failed or was cancelled)
//     [3] processed-delta
//     [4] processed-echo
//
//   === Collect Heterogeneous Results with Map ===
//   Results:
//     user-1: {ID:1 Name:Alice Score:92}
//     user-2: {ID:2 Name:Bob Score:87}
//     user-3: {ID:3 Name:Charlie Score:95}

package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

func main() {
	unsafeCollect()
	collectByIndex()
	collectWithMutex()
	collectWithPartialResults()
	collectHeterogeneousResults()
}

// unsafeCollect shows the data race that occurs when multiple goroutines
// append to a shared slice without synchronization. Run with: go run -race main.go
func unsafeCollect() {
	fmt.Println("=== Unsafe Collect (data race!) ===")
	var g errgroup.Group
	var results []string // shared, unprotected -- BUG

	for i := 0; i < 10; i++ {
		i := i
		g.Go(func() error {
			time.Sleep(time.Duration(i*10) * time.Millisecond)
			// append modifies the slice header (len) and may reallocate.
			// Multiple goroutines doing this concurrently = data race.
			results = append(results, fmt.Sprintf("result-%d", i))
			return nil
		})
	}

	_ = g.Wait()
	fmt.Printf("Got %d results (may be wrong due to race)\n", len(results))
}

// collectByIndex is the preferred pattern when the number of results is known.
// Pre-allocate a slice of the exact size. Each goroutine writes to its own index.
// Writing to distinct indices of the same slice is safe without a mutex because
// each goroutine touches a different memory location.
func collectByIndex() {
	fmt.Println("\n=== Collect by Index (no mutex needed) ===")
	tasks := []string{"alpha", "bravo", "charlie", "delta", "echo"}
	results := make([]string, len(tasks)) // pre-allocate exact size

	var g errgroup.Group
	for i, task := range tasks {
		i, task := i, task
		g.Go(func() error {
			time.Sleep(time.Duration(50+i*30) * time.Millisecond)
			// Safe: each goroutine writes to a unique index
			results[i] = fmt.Sprintf("processed-%s", task)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// IMPORTANT: only read results AFTER Wait() returns. Before that,
	// goroutines may still be writing.
	fmt.Println("Results (ordered):")
	for i, r := range results {
		fmt.Printf("  [%d] %s\n", i, r)
	}
}

// collectWithMutex is the pattern for cases where results don't map to fixed
// indices: filtering, dynamic discovery, or when the output count differs from
// the input count. A mutex protects the shared append.
func collectWithMutex() {
	fmt.Println("\n=== Collect with Mutex (filtered results) ===")

	var g errgroup.Group
	var mu sync.Mutex
	var results []string

	for i := 0; i < 10; i++ {
		i := i
		g.Go(func() error {
			time.Sleep(time.Duration(i*20) * time.Millisecond)

			// Only collect even-numbered results (simulates filtering)
			if i%2 == 0 {
				result := fmt.Sprintf("result-%d", i)
				mu.Lock()
				results = append(results, result)
				mu.Unlock()
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Collected %d even results:\n", len(results))
	for _, r := range results {
		fmt.Printf("  %s\n", r)
	}
}

// collectWithPartialResults demonstrates collecting results when some tasks fail.
// Uses errgroup.WithContext so tasks can detect siblings' failures.
// The index-based pattern naturally handles partial results: failed/cancelled tasks
// leave their slot as the zero value (empty string).
func collectWithPartialResults() {
	fmt.Println("\n=== Collect with Partial Results on Error ===")

	tasks := []string{"alpha", "bravo", "FAIL", "delta", "echo"}
	results := make([]string, len(tasks))

	g, ctx := errgroup.WithContext(context.Background())

	for i, task := range tasks {
		i, task := i, task
		g.Go(func() error {
			// Check if a sibling already failed
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			// Simulate work with staggered timing
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(50+i*30) * time.Millisecond):
			}

			if task == "FAIL" {
				return fmt.Errorf("task %d (%q) failed", i, task)
			}

			results[i] = fmt.Sprintf("processed-%s", task)
			return nil
		})
	}

	err := g.Wait()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}

	// After Wait, we can safely inspect partial results.
	// Empty strings indicate tasks that failed or were cancelled.
	fmt.Println("Partial results:")
	for i, r := range results {
		if r != "" {
			fmt.Printf("  [%d] %s\n", i, r)
		} else {
			fmt.Printf("  [%d] (empty -- task failed or was cancelled)\n", i)
		}
	}
}

// UserProfile is used by collectHeterogeneousResults.
type UserProfile struct {
	ID    int
	Name  string
	Score int
}

// collectHeterogeneousResults shows how to collect results into a map
// when results are keyed by something other than a sequential index.
// A mutex protects the map since map writes are not goroutine-safe.
func collectHeterogeneousResults() {
	fmt.Println("\n=== Collect Heterogeneous Results with Map ===")

	userIDs := []string{"user-1", "user-2", "user-3"}

	// Simulated database of user profiles
	profiles := map[string]UserProfile{
		"user-1": {ID: 1, Name: "Alice", Score: 92},
		"user-2": {ID: 2, Name: "Bob", Score: 87},
		"user-3": {ID: 3, Name: "Charlie", Score: 95},
	}

	var g errgroup.Group
	var mu sync.Mutex
	results := make(map[string]UserProfile)

	for _, uid := range userIDs {
		uid := uid
		g.Go(func() error {
			time.Sleep(50 * time.Millisecond) // simulate fetch

			profile, ok := profiles[uid]
			if !ok {
				return fmt.Errorf("user %s not found", uid)
			}

			mu.Lock()
			results[uid] = profile
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Println("Results:")
	for _, uid := range userIDs {
		fmt.Printf("  %s: %+v\n", uid, results[uid])
	}
}
