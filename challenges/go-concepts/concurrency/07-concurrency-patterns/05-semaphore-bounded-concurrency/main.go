package main

// Exercise: Semaphore -- Bounded Concurrency
// Instructions: see 05-semaphore-bounded-concurrency.md

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// Step 1: Implement basicSemaphore.
// Use a buffered channel of struct{} as a semaphore to limit concurrency to 3.
// Launch 10 tasks, each sleeping 100ms.
func basicSemaphore() {
	fmt.Println("=== Basic Semaphore ===")
	const maxConcurrency = 3
	const totalTasks = 10

	// TODO: create sem := make(chan struct{}, maxConcurrency)
	// TODO: for each task:
	//   - wg.Add(1)
	//   - sem <- struct{}{} (acquire -- blocks if 3 running)
	//   - launch goroutine that defers wg.Done() and <-sem (release)
	//   - print start/done, sleep 100ms
	// TODO: wg.Wait()
	_ = maxConcurrency
	_ = totalTasks
	fmt.Println()
}

// Step 2: Implement trackedSemaphore.
// Same as basicSemaphore but track the count of active goroutines
// using atomic.AddInt64 to prove the limit is respected.
func trackedSemaphore() {
	fmt.Println("=== Tracked Semaphore ===")
	const maxConcurrency = 3
	const totalTasks = 12

	var active int64
	_ = active // remove once implemented
	_ = atomic.AddInt64 // hint

	// TODO: same as Step 1 but:
	//   - atomic.AddInt64(&active, 1) on start
	//   - print the active count
	//   - check if active > maxConcurrency (print BUG if so)
	//   - atomic.AddInt64(&active, -1) on finish
	_ = maxConcurrency
	_ = totalTasks
	fmt.Println()
}

// Step 3: Implement compareApproaches.
// Run 15 tasks with concurrency=4 using both semaphore and worker pool.
// Time each approach and compare.
func compareApproaches() {
	fmt.Println("=== Semaphore vs Worker Pool ===")
	const numTasks = 15
	const concurrency = 4

	// TODO: Semaphore approach -- time it
	// TODO: Worker pool approach -- time it
	// TODO: Print both durations
	_ = numTasks
	_ = concurrency
	fmt.Println()
}

// Verify: Simulate 20 URL downloads with semaphore limiting to 5 concurrent.
// Each download sleeps for a random 50-150ms.
func urlFetcherSimulation() {
	fmt.Println("=== Verify: URL Fetcher ===")
	const maxConcurrent = 5
	const numURLs = 20

	// TODO: create semaphore with capacity maxConcurrent
	// TODO: for each URL (simulated as index 1..numURLs):
	//   - acquire semaphore
	//   - launch goroutine: print start with timestamp, sleep random 50-150ms, print done
	//   - release semaphore
	// TODO: wait for all
	_ = maxConcurrent
	_ = numURLs
	_ = rand.Intn // hint: rand.Intn(100) + 50 for 50-150ms range

	fmt.Println()
}

func main() {
	fmt.Println("Exercise: Semaphore -- Bounded Concurrency\n")

	// Step 1
	basicSemaphore()

	// Step 2
	trackedSemaphore()

	// Step 3
	compareApproaches()

	// Verify
	urlFetcherSimulation()

	_ = time.Millisecond  // hint
	_ = sync.WaitGroup{}  // hint
}
