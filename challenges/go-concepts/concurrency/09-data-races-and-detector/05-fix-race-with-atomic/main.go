package main

// Exercise: Fix Race with Atomic
// Instructions: see 05-fix-race-with-atomic.md

import (
	"fmt"
	"sync"
	"time"
)

// racyCounter is the same racy function from exercise 01.
func racyCounter() int {
	counter := 0
	var wg sync.WaitGroup

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				counter++ // DATA RACE
			}
		}()
	}

	wg.Wait()
	return counter
}

// safeCounterMutex is the mutex solution from exercise 03 (for comparison).
func safeCounterMutex() int {
	counter := 0
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				mu.Lock()
				counter++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	return counter
}

// safeCounterChannel is the channel solution from exercise 04 (for comparison).
func safeCounterChannel() int {
	increments := make(chan struct{}, 100)
	done := make(chan int)

	go func() {
		counter := 0
		for range increments {
			counter++
		}
		done <- counter
	}()

	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				increments <- struct{}{}
			}
		}()
	}

	wg.Wait()
	close(increments)
	return <-done
}

// Step 1: Implement safeCounterAtomic.
// Fix the data race using sync/atomic.AddInt64.
//
// TODO:
//   - Declare counter as int64 (required for atomic operations)
//   - Replace counter++ with atomic.AddInt64(&counter, 1)
//   - Return the final value using atomic.LoadInt64(&counter)
//
// Hint: import "sync/atomic"
func safeCounterAtomic() int64 {
	// TODO: var counter int64
	var wg sync.WaitGroup

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				// TODO: atomic.AddInt64(&counter, 1)
				_ = j
			}
		}()
	}

	wg.Wait()
	// TODO: return atomic.LoadInt64(&counter)
	return 0
}

// Step 3: Implement compareAllApproaches.
// Time mutex, channel, and atomic approaches side by side.
func compareAllApproaches() {
	fmt.Println("\n=== Comparison: Mutex vs Channel vs Atomic ===")
	_ = time.Now // hint: use time.Now() and time.Since()
	// TODO: time all three approaches and print results
}

func main() {
	fmt.Println("=== Fix Race with Atomic ===")

	fmt.Printf("Racy counter:  %d (expected 1000000)\n", racyCounter())
	fmt.Printf("Safe (atomic): %d (expected 1000000)\n", safeCounterAtomic())

	compareAllApproaches()

	fmt.Println("\nVerify: go run -race main.go")
	fmt.Println("Only racyCounter should trigger a DATA RACE warning.")
}
