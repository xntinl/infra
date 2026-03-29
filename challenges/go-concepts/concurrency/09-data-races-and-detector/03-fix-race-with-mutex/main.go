package main

// Exercise: Fix Race with Mutex
// Instructions: see 03-fix-race-with-mutex.md

import (
	"fmt"
	"sync"
	"time"
)

// racyCounter is the same racy function from exercise 01.
// It is included here for comparison.
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

// Step 1: Implement safeCounterMutex.
// Fix the data race by protecting counter++ with a sync.Mutex.
// Use mu.Lock() before counter++ and mu.Unlock() after.
//
// TODO: Declare a sync.Mutex and wrap counter++ in Lock/Unlock.
func safeCounterMutex() int {
	counter := 0
	var wg sync.WaitGroup
	// TODO: declare var mu sync.Mutex

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				// TODO: mu.Lock(), counter++, mu.Unlock()
				counter++
			}
		}()
	}

	wg.Wait()
	return counter
}

// Step 3: Implement safeCounterDefer.
// Same as safeCounterMutex, but extract the critical section into
// a closure that uses defer mu.Unlock().
//
// TODO: Create an increment closure with mu.Lock() / defer mu.Unlock().
func safeCounterDefer() int {
	counter := 0
	var wg sync.WaitGroup
	// TODO: declare var mu sync.Mutex
	// TODO: create increment := func() { mu.Lock(); defer mu.Unlock(); counter++ }

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				// TODO: call increment()
				counter++
			}
		}()
	}

	wg.Wait()
	return counter
}

// Step 4: Implement compareTiming.
// Measure the time taken by racyCounter vs safeCounterMutex.
func compareTiming() {
	fmt.Println("\n=== Timing Comparison ===")
	_ = time.Now // hint: use time.Now() and time.Since()
	// TODO: time racyCounter() and safeCounterMutex()
	// TODO: print both durations and the slowdown factor
}

func main() {
	fmt.Println("=== Fix Race with Mutex ===")

	fmt.Printf("Racy counter:  %d (expected 1000000)\n", racyCounter())
	fmt.Printf("Safe (mutex):  %d (expected 1000000)\n", safeCounterMutex())
	fmt.Printf("Safe (defer):  %d (expected 1000000)\n", safeCounterDefer())

	compareTiming()

	fmt.Println("\nVerify: go run -race main.go")
	fmt.Println("Only racyCounter should trigger a DATA RACE warning.")
}
