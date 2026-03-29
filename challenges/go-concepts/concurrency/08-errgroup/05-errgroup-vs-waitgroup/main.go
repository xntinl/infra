package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

func main() {
	solveWithWaitGroup()
	solveWithErrgroup()
	waitgroupForFireAndForget()
}

// processFile simulates file processing. Files named "CORRUPT" or "MISSING" fail.
func processFile(name string) error {
	delay := time.Duration(50+rand.Intn(100)) * time.Millisecond
	time.Sleep(delay)

	switch name {
	case "CORRUPT":
		return fmt.Errorf("file %q is corrupted", name)
	case "MISSING":
		return fmt.Errorf("file %q not found", name)
	default:
		fmt.Printf("  Processed: %s\n", name)
		return nil
	}
}

// solveWithWaitGroup uses sync.WaitGroup with manual error handling.
// TODO: Add a mutex to protect firstErr and successCount.
// Notice how many moving parts this requires.
func solveWithWaitGroup() {
	fmt.Println("=== WaitGroup Solution ===")
	files := []string{"config.yaml", "data.csv", "CORRUPT", "readme.md", "MISSING"}

	var wg sync.WaitGroup
	// TODO: Add a sync.Mutex to protect shared state
	var firstErr error
	successCount := 0

	for _, file := range files {
		file := file
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := processFile(file); err != nil {
				// TODO: Lock mutex, set firstErr if nil, unlock
				firstErr = err // BUG: data race without mutex!
				return
			}
			// TODO: Lock mutex, increment successCount, unlock
			successCount++ // BUG: data race without mutex!
		}()
	}

	wg.Wait()

	fmt.Printf("Processed: %d/%d succeeded\n", successCount, len(files))
	if firstErr != nil {
		fmt.Printf("First error: %v\n", firstErr)
	}
}

// solveWithErrgroup solves the same problem with errgroup.
// TODO: Replace the manual approach with errgroup.Group and index-based results.
func solveWithErrgroup() {
	fmt.Println("\n=== Errgroup Solution ===")
	files := []string{"config.yaml", "data.csv", "CORRUPT", "readme.md", "MISSING"}

	// TODO: Create a results slice: results := make([]bool, len(files))
	// TODO: Create an errgroup.Group

	// TODO: Launch tasks with g.Go(), set results[i] = true on success

	for i, file := range files {
		_, _ = i, file
		// TODO: g.Go(func() error { ... })
	}

	// TODO: err := g.Wait()
	// TODO: Count successes from results slice
	// TODO: Print summary

	fmt.Println("TODO: implement this function")

	_ = errgroup.Group{} // ensure errgroup import is used
}

// waitgroupForFireAndForget demonstrates when WaitGroup is the better choice:
// tasks that never fail and have no meaningful error to return.
// TODO: Launch 5 workers that perform infallible side-effect work.
func waitgroupForFireAndForget() {
	fmt.Println("\n=== WaitGroup: Fire-and-Forget (best fit) ===")

	// TODO: Create a WaitGroup
	// TODO: Launch 5 goroutines that simulate sending notifications
	//   - Each worker sleeps i*50ms
	//   - Prints "Worker N: sent notification"
	//   - No error handling needed
	// TODO: Wait for all workers

	fmt.Println("TODO: implement this function")
}
