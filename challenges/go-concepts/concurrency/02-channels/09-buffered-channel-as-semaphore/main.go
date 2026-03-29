package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

func timestamp() string {
	return time.Now().Format("15:04:05.000")
}

// ============================================================
// Step 1: Unlimited concurrency — all goroutines start at once
// ============================================================

func step1() {
	fmt.Println("--- Step 1: Unlimited Concurrency ---")
	var wg sync.WaitGroup

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
	fmt.Println("All done (all ran concurrently)")
}

// ============================================================
// Step 2: Semaphore limits concurrency to 3
// ============================================================

func step2() {
	fmt.Println("--- Step 2: Semaphore (max 3) ---")

	// TODO: Create semaphore: sem := make(chan struct{}, 3)
	var wg sync.WaitGroup

	for i := 1; i <= 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// TODO: Acquire semaphore slot
			// sem <- struct{}{}
			// defer func() { <-sem }()

			fmt.Printf("[%s] Goroutine %2d: start\n", timestamp(), id)
			time.Sleep(300 * time.Millisecond)
			fmt.Printf("[%s] Goroutine %2d: done\n", timestamp(), id)
		}(i)
	}

	wg.Wait()
	fmt.Println("All done (max 3 concurrent)")
}

// ============================================================
// Step 3: Observe batching with timestamps
// ============================================================

func step3() {
	fmt.Println("--- Step 3: Batching Effect ---")
	start := time.Now()

	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup

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
	fmt.Printf("Total: %s (expected ~2s for 12 items, batch 3, 500ms each)\n", total)
}

// ============================================================
// Step 4: Weighted semaphore
// ============================================================

func step4() {
	fmt.Println("--- Step 4: Weighted Semaphore ---")

	sem := make(chan struct{}, 5) // 5 total slots
	var wg sync.WaitGroup

	// Heavy work acquires 2 slots
	heavyWork := func(id int) {
		defer wg.Done()

		// TODO: Acquire 2 slots (uncomment the lines below)
		// sem <- struct{}{}
		// sem <- struct{}{}
		// defer func() { <-sem; <-sem }()
		_ = sem // remove when you uncomment the acquire/release above

		fmt.Printf("[%s] Heavy %d: working (2 slots)\n", timestamp(), id)
		time.Sleep(400 * time.Millisecond)
		fmt.Printf("[%s] Heavy %d: done\n", timestamp(), id)
	}

	// Light work acquires 1 slot
	lightWork := func(id int) {
		defer wg.Done()

		// TODO: Acquire 1 slot (uncomment the lines below)
		// sem <- struct{}{}
		// defer func() { <-sem }()
		_ = sem // remove when you uncomment the acquire/release above

		fmt.Printf("[%s] Light %d: working (1 slot)\n", timestamp(), id)
		time.Sleep(200 * time.Millisecond)
		fmt.Printf("[%s] Light %d: done\n", timestamp(), id)
	}

	// Launch mix: 3 heavy (6 slots needed) + 4 light (4 slots needed)
	for i := 1; i <= 3; i++ {
		wg.Add(1)
		go heavyWork(i)
	}
	for i := 1; i <= 4; i++ {
		wg.Add(1)
		go lightWork(i)
	}

	wg.Wait()
	fmt.Println("All done (max 5 total slots)")
}

// ============================================================
// Final Challenge: Concurrent URL Fetcher
//
// - 15 "URLs" to fetch
// - Semaphore limits to 4 concurrent fetches
// - Track active count with atomic counter
// - Assert max concurrent never exceeds 4
// ============================================================

func finalChallenge() {
	fmt.Println("--- Final: URL Fetcher (max 4 concurrent) ---")
	start := time.Now()

	urls := make([]string, 15)
	for i := range urls {
		urls[i] = fmt.Sprintf("https://example.com/page/%d", i+1)
	}

	// TODO: Create semaphore with capacity 4
	// sem := make(chan struct{}, 4)

	var wg sync.WaitGroup
	var activeCount atomic.Int32
	var maxConcurrent atomic.Int32

	for i, url := range urls {
		wg.Add(1)
		go func(id int, url string) {
			defer wg.Done()

			// TODO: Acquire semaphore

			// Track active goroutines
			current := activeCount.Add(1)
			for {
				old := maxConcurrent.Load()
				if current <= old || maxConcurrent.CompareAndSwap(old, current) {
					break
				}
			}

			duration := time.Duration((id%3+1)*100) * time.Millisecond
			fmt.Printf("[+%6s] Fetching %-35s (active: %d)\n",
				time.Since(start).Round(time.Millisecond), url, current)

			time.Sleep(duration) // simulate fetch
			activeCount.Add(-1)

			// TODO: Release semaphore
		}(i, url)
	}

	wg.Wait()

	elapsed := time.Since(start).Round(time.Millisecond)
	fmt.Printf("\nTotal: %s\n", elapsed)
	fmt.Printf("Max concurrent fetches: %d (limit was 4)\n", maxConcurrent.Load())

	if maxConcurrent.Load() <= 4 {
		fmt.Println("PASS: concurrency limit respected")
	} else {
		fmt.Println("FAIL: concurrency limit exceeded!")
	}
}

func main() {
	step1()
	fmt.Println()

	step2()
	fmt.Println()

	step3()
	fmt.Println()

	step4()
	fmt.Println()

	finalChallenge()
}
