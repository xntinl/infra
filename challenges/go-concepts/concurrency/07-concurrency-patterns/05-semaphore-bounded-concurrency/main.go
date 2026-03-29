package main

// Semaphore: Bounded Concurrency -- Complete Working Example
//
// A buffered channel of struct{} is Go's idiomatic counting semaphore.
// Send = acquire (blocks when full), receive = release (frees a slot).
// Unlike worker pools, semaphores launch a new goroutine per task
// but limit how many run simultaneously.
//
// Expected output:
//   === Basic Semaphore (max 3 concurrent, 10 tasks) ===
//     task 1: started
//     task 2: started
//     task 3: started
//     task 1: done
//     task 4: started
//     ...all 10 tasks complete...
//
//   === Tracked Semaphore (max 3 concurrent, 12 tasks) ===
//     task  1 running (active: 1)
//     task  2 running (active: 2)
//     task  3 running (active: 3)
//     ...active never exceeds 3...
//
//   === Semaphore vs Worker Pool ===
//     Semaphore:   ~200ms
//     Worker pool: ~200ms
//
//   === URL Fetcher Simulation (max 5 concurrent, 20 URLs) ===
//     ...at most 5 downloads active at any time...

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// Basic semaphore: limits concurrency to maxConcurrency.
//
// The key insight: acquire the semaphore BEFORE launching the goroutine.
// This blocks the loop itself, so at most maxConcurrency goroutines exist.
// If you acquire inside the goroutine, all goroutines launch immediately
// and the semaphore only gates their work -- defeating the purpose.
//
//   main loop ---> [sem acquire] ---> go func() { work; [sem release] }
//                  (blocks here if
//                   3 already running)
// ---------------------------------------------------------------------------

func basicSemaphore() {
	fmt.Println("=== Basic Semaphore (max 3 concurrent, 10 tasks) ===")
	const maxConcurrency = 3
	const totalTasks = 10

	// The semaphore: buffered channel with capacity = max concurrent.
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup

	for i := 1; i <= totalTasks; i++ {
		wg.Add(1)
		// Acquire: blocks if 3 goroutines are already running.
		// This happens in the main goroutine, NOT inside the worker.
		sem <- struct{}{}
		go func(id int) {
			defer wg.Done()
			// Release: always paired with acquire via defer.
			// defer ensures release even if the goroutine panics.
			defer func() { <-sem }()

			fmt.Printf("  task %d: started\n", id)
			time.Sleep(100 * time.Millisecond)
			fmt.Printf("  task %d: done\n", id)
		}(i)
	}

	wg.Wait()
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Tracked semaphore: same pattern but uses atomic counters to prove
// the invariant: active goroutines never exceed maxConcurrency.
// ---------------------------------------------------------------------------

func trackedSemaphore() {
	fmt.Println("=== Tracked Semaphore (max 3 concurrent, 12 tasks) ===")
	const maxConcurrency = 3
	const totalTasks = 12

	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup
	var active int64

	for i := 1; i <= totalTasks; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(id int) {
			defer wg.Done()
			defer func() { <-sem }()

			current := atomic.AddInt64(&active, 1)
			fmt.Printf("  task %2d running (active: %d)\n", id, current)

			// This would indicate a bug in the semaphore logic.
			if current > int64(maxConcurrency) {
				fmt.Printf("  BUG: active=%d exceeds max=%d\n", current, maxConcurrency)
			}

			time.Sleep(80 * time.Millisecond)
			atomic.AddInt64(&active, -1)
		}(i)
	}

	wg.Wait()
	fmt.Printf("  Max concurrency respected: active never exceeded %d\n\n", maxConcurrency)
}

// ---------------------------------------------------------------------------
// Compare semaphore vs worker pool: both achieve bounded concurrency,
// but through different mechanisms.
//
// Semaphore: one goroutine per task, semaphore gates entry.
// Worker pool: fixed goroutines, shared queue gates work.
//
// Performance is nearly identical for most workloads. Choose based on
// whether tasks are homogeneous (pool) or heterogeneous (semaphore).
// ---------------------------------------------------------------------------

func compareApproaches() {
	fmt.Println("=== Semaphore vs Worker Pool ===")
	const numTasks = 15
	const concurrency = 4

	// Semaphore approach: one goroutine per task, gated by semaphore.
	start := time.Now()
	sem := make(chan struct{}, concurrency)
	var wg1 sync.WaitGroup
	for i := 0; i < numTasks; i++ {
		wg1.Add(1)
		sem <- struct{}{}
		go func(id int) {
			defer wg1.Done()
			defer func() { <-sem }()
			time.Sleep(50 * time.Millisecond)
		}(i)
	}
	wg1.Wait()
	fmt.Printf("  Semaphore:   %v\n", time.Since(start))

	// Worker pool approach: fixed goroutines pulling from a queue.
	start = time.Now()
	jobs := make(chan int, numTasks)
	var wg2 sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			for range jobs {
				time.Sleep(50 * time.Millisecond)
			}
		}()
	}
	for i := 0; i < numTasks; i++ {
		jobs <- i
	}
	close(jobs)
	wg2.Wait()
	fmt.Printf("  Worker pool: %v\n\n", time.Since(start))
}

// ---------------------------------------------------------------------------
// URL fetcher simulation: a realistic use of the semaphore pattern.
// Each "download" is an independent task with random duration.
// The semaphore prevents overwhelming the network with too many
// concurrent connections.
// ---------------------------------------------------------------------------

func urlFetcherSimulation() {
	fmt.Println("=== URL Fetcher Simulation (max 5 concurrent, 20 URLs) ===")
	const maxConcurrent = 5
	const numURLs = 20

	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	var active int64

	startTime := time.Now()

	for i := 1; i <= numURLs; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(urlID int) {
			defer wg.Done()
			defer func() {
				atomic.AddInt64(&active, -1)
				<-sem
			}()

			current := atomic.AddInt64(&active, 1)
			elapsed := time.Since(startTime).Truncate(time.Millisecond)
			fmt.Printf("  [%v] download %2d started (active: %d)\n", elapsed, urlID, current)

			// Random download time between 50-150ms
			duration := time.Duration(rand.Intn(100)+50) * time.Millisecond
			time.Sleep(duration)

			elapsed = time.Since(startTime).Truncate(time.Millisecond)
			fmt.Printf("  [%v] download %2d done (%v)\n", elapsed, urlID, duration)
		}(i)
	}

	wg.Wait()
	fmt.Printf("  All %d downloads complete in %v\n\n", numURLs, time.Since(startTime).Truncate(time.Millisecond))
}

func main() {
	fmt.Println("Exercise: Semaphore -- Bounded Concurrency")
	fmt.Println()

	basicSemaphore()
	trackedSemaphore()
	compareApproaches()
	urlFetcherSimulation()
}
