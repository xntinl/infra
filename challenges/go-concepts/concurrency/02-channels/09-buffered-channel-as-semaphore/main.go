package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// This program demonstrates using buffered channels as semaphores to limit concurrency.
// Run: go run main.go
//
// Expected output:
//   === Example 1: Unlimited Concurrency (The Problem) ===
//   [+0ms] all 10 goroutines start nearly simultaneously
//   Total: ~300ms
//
//   === Example 2: Semaphore Limits to 3 ===
//   Only 3 goroutines active at a time, rest queue up
//   Total: ~1.2s (4 batches of 3 * 300ms)
//
//   === Example 3: Batch Timing with 12 Items ===
//   Total: ~2s (12 items / 3 per batch * 500ms)
//
//   === Example 4: Weighted Semaphore ===
//   Heavy work takes 2 slots, light takes 1, total capacity 5
//
//   === Example 5: URL Fetcher (max 4 concurrent) ===
//   Max concurrent: 4 (limit respected)

func main() {
	example1Unlimited()
	example2Semaphore()
	example3BatchTiming()
	example4WeightedSemaphore()
	example5URLFetcher()
}

func timestamp() string {
	return time.Now().Format("15:04:05.000")
}

// example1Unlimited shows what happens without a semaphore: all goroutines start
// simultaneously. This is fine for CPU work, but dangerous for resources with
// concurrency limits (database connections, API rate limits, file descriptors).
func example1Unlimited() {
	fmt.Println("=== Example 1: Unlimited Concurrency (The Problem) ===")

	var wg sync.WaitGroup
	start := time.Now()

	for i := 1; i <= 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			fmt.Printf("[%s] Goroutine %2d: start\n", timestamp(), id)
			time.Sleep(300 * time.Millisecond)
			fmt.Printf("[%s] Goroutine %2d: done\n", timestamp(), id)
		}(i)
	}

	wg.Wait()
	fmt.Printf("Total: %v (all ran in parallel)\n\n", time.Since(start).Round(time.Millisecond))
}

// example2Semaphore adds a buffered channel as a concurrency limiter.
// The pattern: send to acquire a slot, defer receive to release it.
// When all slots are taken, new goroutines block on send until a slot opens.
func example2Semaphore() {
	fmt.Println("=== Example 2: Semaphore Limits to 3 ===")

	// The buffer capacity IS the concurrency limit. 3 slots = max 3 concurrent.
	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	start := time.Now()

	for i := 1; i <= 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Acquire: send blocks when buffer is full (3 goroutines already active).
			sem <- struct{}{}
			// Release: ALWAYS defer to guarantee the slot is freed, even on panic.
			defer func() { <-sem }()

			elapsed := time.Since(start).Round(time.Millisecond)
			fmt.Printf("[+%6s] Goroutine %2d: start\n", elapsed, id)
			time.Sleep(300 * time.Millisecond)
		}(i)
	}

	wg.Wait()
	fmt.Printf("Total: %v (max 3 concurrent)\n\n", time.Since(start).Round(time.Millisecond))
}

// example3BatchTiming runs 12 goroutines through a semaphore of size 3.
// Each takes 500ms. Expected: 4 batches * 500ms = ~2 seconds total.
func example3BatchTiming() {
	fmt.Println("=== Example 3: Batch Timing with 12 Items ===")

	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	start := time.Now()

	for i := 1; i <= 12; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			elapsed := time.Since(start).Round(time.Millisecond)
			fmt.Printf("[+%6s] Goroutine %2d: start\n", elapsed, id)
			time.Sleep(500 * time.Millisecond)
		}(i)
	}

	wg.Wait()
	total := time.Since(start).Round(time.Millisecond)
	fmt.Printf("Total: %s (expected ~2s for 12 items, batch 3, 500ms each)\n\n", total)
}

// example4WeightedSemaphore demonstrates that different tasks can consume different
// numbers of semaphore slots. Heavy operations take 2 slots, light ones take 1.
// Total capacity is 5 slots.
func example4WeightedSemaphore() {
	fmt.Println("=== Example 4: Weighted Semaphore ===")

	sem := make(chan struct{}, 5) // 5 total resource slots
	var wg sync.WaitGroup

	heavyWork := func(id int) {
		defer wg.Done()
		// Acquire 2 slots for heavy work.
		sem <- struct{}{}
		sem <- struct{}{}
		defer func() { <-sem; <-sem }()

		fmt.Printf("[%s] Heavy %d: working (2 slots)\n", timestamp(), id)
		time.Sleep(400 * time.Millisecond)
		fmt.Printf("[%s] Heavy %d: done\n", timestamp(), id)
	}

	lightWork := func(id int) {
		defer wg.Done()
		// Acquire 1 slot for light work.
		sem <- struct{}{}
		defer func() { <-sem }()

		fmt.Printf("[%s] Light %d: working (1 slot)\n", timestamp(), id)
		time.Sleep(200 * time.Millisecond)
		fmt.Printf("[%s] Light %d: done\n", timestamp(), id)
	}

	// 3 heavy tasks (need 6 slots total) + 4 light tasks (need 4 slots total).
	// With only 5 slots, they can't all run at once.
	for i := 1; i <= 3; i++ {
		wg.Add(1)
		go heavyWork(i)
	}
	for i := 1; i <= 4; i++ {
		wg.Add(1)
		go lightWork(i)
	}

	wg.Wait()
	fmt.Println("All tasks done (max 5 total resource slots)")
	fmt.Println()
}

// example5URLFetcher simulates fetching 15 URLs with a maximum of 4 concurrent fetches.
// An atomic counter tracks active goroutines to verify the limit is never exceeded.
func example5URLFetcher() {
	fmt.Println("=== Example 5: URL Fetcher (max 4 concurrent) ===")

	urls := make([]string, 15)
	for i := range urls {
		urls[i] = fmt.Sprintf("https://example.com/page/%d", i+1)
	}

	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	var activeCount atomic.Int32
	var maxConcurrent atomic.Int32
	start := time.Now()

	for i, url := range urls {
		wg.Add(1)
		go func(id int, url string) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			// Track active goroutines using atomics (lock-free counter).
			current := activeCount.Add(1)
			// Update max using compare-and-swap loop.
			for {
				old := maxConcurrent.Load()
				if current <= old || maxConcurrent.CompareAndSwap(old, current) {
					break
				}
			}

			duration := time.Duration((id%3+1)*100) * time.Millisecond
			fmt.Printf("[+%6s] Fetching %-35s (active: %d)\n",
				time.Since(start).Round(time.Millisecond), url, current)

			time.Sleep(duration) // simulate variable fetch time
			activeCount.Add(-1)
		}(i, url)
	}

	wg.Wait()

	elapsed := time.Since(start).Round(time.Millisecond)
	maxConc := maxConcurrent.Load()
	fmt.Printf("\nTotal: %s\n", elapsed)
	fmt.Printf("Max concurrent fetches: %d (limit was 4)\n", maxConc)

	if maxConc <= 4 {
		fmt.Println("PASS: concurrency limit respected")
	} else {
		fmt.Println("FAIL: concurrency limit exceeded!")
	}
}
