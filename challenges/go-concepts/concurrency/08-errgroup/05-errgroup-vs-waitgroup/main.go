// Exercise 05: Errgroup vs WaitGroup
//
// Solves the same problem (parallel file processing) with both sync.WaitGroup
// and errgroup.Group, then demonstrates when each tool is the right choice.
//
// Expected output (order may vary for processed lines):
//
//   === WaitGroup Solution ===
//     Processed: config.yaml
//     Processed: data.csv
//     Processed: readme.md
//   Processed: 3/5 succeeded
//   First error: file "CORRUPT" is corrupted
//
//   === Errgroup Solution ===
//     Processed: config.yaml
//     Processed: data.csv
//     Processed: readme.md
//   Processed: 3/5 succeeded
//   First error: file "CORRUPT" is corrupted
//
//   === WaitGroup: Fire-and-Forget (best fit) ===
//     Worker 0: sent notification
//     Worker 1: sent notification
//     ...
//   All 5 notifications sent
//
//   === Both Tools in One Program ===
//   --- Fetching (errgroup, fallible) ---
//     Fetched: https://api.example.com/users
//     Fetched: https://api.example.com/orders
//   --- Logging (waitgroup, infallible) ---
//     Logger 0: wrote audit log
//     Logger 1: wrote audit log
//   Fetch error: fetch https://api.example.com/BROKEN: 500
//   Background logging complete

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
	bothToolsTogether()
}

// processFile simulates file processing. Some filenames trigger errors.
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
// Requires: WaitGroup + mutex + error variable + success counter.
// Four primitives interleaved -- easy to get wrong, hard to read.
func solveWithWaitGroup() {
	fmt.Println("=== WaitGroup Solution ===")
	files := []string{"config.yaml", "data.csv", "CORRUPT", "readme.md", "MISSING"}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	successCount := 0

	for _, file := range files {
		file := file
		wg.Add(1) // Must be called BEFORE the goroutine starts, not inside it
		go func() {
			defer wg.Done()
			if err := processFile(file); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			mu.Lock()
			successCount++
			mu.Unlock()
		}()
	}

	wg.Wait()

	fmt.Printf("Processed: %d/%d succeeded\n", successCount, len(files))
	if firstErr != nil {
		fmt.Printf("First error: %v\n", firstErr)
	}
}

// solveWithErrgroup solves the same problem with errgroup.Group.
// No WaitGroup, no mutex for errors, no manual Add/Done.
// Error propagation is built in. Index-based result tracking avoids mutex for
// the success count.
func solveWithErrgroup() {
	fmt.Println("\n=== Errgroup Solution ===")
	files := []string{"config.yaml", "data.csv", "CORRUPT", "readme.md", "MISSING"}
	results := make([]bool, len(files)) // true = success for this index

	var g errgroup.Group

	for i, file := range files {
		i, file := i, file
		g.Go(func() error {
			if err := processFile(file); err != nil {
				return err // errgroup captures the first error automatically
			}
			results[i] = true // safe: each goroutine owns its index
			return nil
		})
	}

	err := g.Wait()

	successCount := 0
	for _, ok := range results {
		if ok {
			successCount++
		}
	}

	fmt.Printf("Processed: %d/%d succeeded\n", successCount, len(files))
	if err != nil {
		fmt.Printf("First error: %v\n", err)
	}
}

// waitgroupForFireAndForget shows where WaitGroup is the better tool:
// goroutines that perform infallible side-effects. Using errgroup here would
// force every closure to return nil, adding noise with no benefit.
func waitgroupForFireAndForget() {
	fmt.Println("\n=== WaitGroup: Fire-and-Forget (best fit) ===")

	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Pure side-effect work: best-effort notifications, metrics, logging.
			// These never return errors worth propagating to the caller.
			time.Sleep(time.Duration(i*30) * time.Millisecond)
			fmt.Printf("  Worker %d: sent notification\n", i)
		}()
	}

	wg.Wait()
	fmt.Println("All 5 notifications sent")
}

// bothToolsTogether demonstrates using errgroup and WaitGroup in the same
// program, each for its natural use case. Fallible HTTP fetches use errgroup;
// infallible audit logging uses WaitGroup.
func bothToolsTogether() {
	fmt.Println("\n=== Both Tools in One Program ===")

	// --- Fallible work: errgroup for HTTP fetches ---
	fmt.Println("--- Fetching (errgroup, fallible) ---")
	var g errgroup.Group
	urls := []string{
		"https://api.example.com/users",
		"https://api.example.com/orders",
		"https://api.example.com/BROKEN",
	}

	for _, url := range urls {
		url := url
		g.Go(func() error {
			return simulateFetch(url)
		})
	}

	// --- Infallible work: WaitGroup for background logging ---
	fmt.Println("--- Logging (waitgroup, infallible) ---")
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(time.Duration(30+i*20) * time.Millisecond)
			fmt.Printf("  Logger %d: wrote audit log\n", i)
		}()
	}

	// Wait for both groups independently
	fetchErr := g.Wait()
	if fetchErr != nil {
		fmt.Printf("Fetch error: %v\n", fetchErr)
	}

	wg.Wait()
	fmt.Println("Background logging complete")
}

// simulateFetch simulates an HTTP fetch. URLs containing "BROKEN" fail.
func simulateFetch(url string) error {
	time.Sleep(time.Duration(50+rand.Intn(50)) * time.Millisecond)
	if url == "https://api.example.com/BROKEN" {
		return fmt.Errorf("fetch %s: 500", url)
	}
	fmt.Printf("  Fetched: %s\n", url)
	return nil
}
