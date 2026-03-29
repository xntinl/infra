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
	collectWithErrors()
}

// unsafeCollect demonstrates a data race when appending to a shared slice
// from multiple goroutines without synchronization.
// Run with: go run -race main.go
func unsafeCollect() {
	fmt.Println("=== Unsafe Collect (data race!) ===")
	var g errgroup.Group
	var results []string // shared, unprotected

	for i := 0; i < 10; i++ {
		i := i
		g.Go(func() error {
			time.Sleep(time.Duration(i*10) * time.Millisecond)
			// BUG: append is not goroutine-safe. This is a data race.
			results = append(results, fmt.Sprintf("result-%d", i))
			return nil
		})
	}

	_ = g.Wait()
	fmt.Printf("Got %d results (may be wrong due to race)\n", len(results))
}

// collectByIndex uses a pre-allocated slice where each goroutine writes
// to its own index. No mutex needed because indices do not overlap.
// TODO: Pre-allocate results, launch tasks, write to results[i].
func collectByIndex() {
	fmt.Println("\n=== Collect by Index (no mutex) ===")
	tasks := []string{"alpha", "bravo", "charlie", "delta", "echo"}

	// TODO: Pre-allocate results slice: results := make([]string, len(tasks))

	var g errgroup.Group
	for i, task := range tasks {
		i, task := i, task
		g.Go(func() error {
			time.Sleep(time.Duration(50+i*30) * time.Millisecond)
			// TODO: Write to results[i] instead of printing
			fmt.Printf("  Processed: %s (result lost -- nowhere to store it)\n", task)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// TODO: Print results in order
	fmt.Println("TODO: print collected results")
}

// collectWithMutex uses a mutex to protect a shared slice for cases
// where results don't map to a fixed index (e.g., filtering).
// TODO: Add a mutex, protect the append call.
func collectWithMutex() {
	fmt.Println("\n=== Collect with Mutex ===")
	var g errgroup.Group
	var results []string

	// TODO: Declare a sync.Mutex: var mu sync.Mutex

	for i := 0; i < 10; i++ {
		i := i
		g.Go(func() error {
			time.Sleep(time.Duration(i*20) * time.Millisecond)

			if i%2 == 0 {
				result := fmt.Sprintf("result-%d", i)
				// TODO: Protect this append with mu.Lock()/mu.Unlock()
				results = append(results, result)
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Collected %d results:\n", len(results))
	for _, r := range results {
		fmt.Printf("  %s\n", r)
	}

	_ = sync.Mutex{} // ensure sync import is used
}

// collectWithErrors demonstrates collecting partial results when some tasks fail.
// TODO: Use errgroup.WithContext, write to results[i], handle the failed task.
func collectWithErrors() {
	fmt.Println("\n=== Collect with Partial Results on Error ===")
	tasks := []string{"alpha", "bravo", "FAIL", "delta", "echo"}

	// TODO: Pre-allocate results: results := make([]string, len(tasks))
	// TODO: Create errgroup with context: g, ctx := errgroup.WithContext(context.Background())

	// TODO: For each task:
	//   1. Check ctx.Done() before starting work
	//   2. If task == "FAIL", return an error
	//   3. Otherwise, write to results[i]

	// TODO: After g.Wait(), print partial results
	// Empty strings indicate tasks that failed or were cancelled

	for i, task := range tasks {
		_, _ = i, task
	}

	fmt.Println("TODO: implement this function")

	_ = context.Background // ensure context import is used
}
