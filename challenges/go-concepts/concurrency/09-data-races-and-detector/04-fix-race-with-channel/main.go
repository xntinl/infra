package main

// Exercise: Fix Race with Channel
// Instructions: see 04-fix-race-with-channel.md

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

// Step 1: Implement safeCounterChannel.
// Fix the data race by having a single owner goroutine manage the counter.
// Worker goroutines send increment signals through a channel.
//
// Pattern:
//   - Create a buffered channel for increment signals
//   - Create a done channel to receive the final count
//   - Launch an owner goroutine that ranges over the increment channel
//   - Launch 1000 worker goroutines, each sending 1000 increment signals
//   - After all workers finish, close the increment channel
//   - Read the final count from the done channel
func safeCounterChannel() int {
	// TODO: create channels
	// TODO: launch owner goroutine
	// TODO: launch 1000 worker goroutines
	// TODO: wait for workers, close increment channel, return final count
	return 0
}

// Step 3: Implement compareMutexAndChannel.
// Time both safeCounterMutex and safeCounterChannel.
func compareMutexAndChannel() {
	fmt.Println("\n=== Mutex vs Channel Timing ===")
	_ = time.Now // hint: use time.Now() and time.Since()
	// TODO: time both approaches and print results
}

func main() {
	fmt.Println("=== Fix Race with Channel ===")
	fmt.Println(`"Don't communicate by sharing memory; share memory by communicating."`)
	fmt.Println()

	fmt.Printf("Racy counter:   %d (expected 1000000)\n", racyCounter())
	fmt.Printf("Safe (channel): %d (expected 1000000)\n", safeCounterChannel())

	compareMutexAndChannel()

	fmt.Println("\nVerify: go run -race main.go")
	fmt.Println("Only racyCounter should trigger a DATA RACE warning.")
}
