package main

// Exercise: Atomic Add Counter
// Instructions: see 01-atomic-add-counter.md

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// Step 1: Implement brokenCounter.
// Launch 1000 goroutines, each incrementing a shared int64 counter 1000 times
// using plain counter++. Return the final value.
// Expected: the result will NOT be 1,000,000 due to data races.
func brokenCounter() int64 {
	var counter int64
	var wg sync.WaitGroup

	// TODO: launch 1000 goroutines, each incrementing counter 1000 times with counter++
	// TODO: use wg.Add(1), wg.Done(), wg.Wait()
	// TODO: return counter
	_ = wg.Wait // hint: use WaitGroup to synchronize

	return counter
}

// Step 2: Implement atomicAddCounter.
// Same structure as brokenCounter, but use atomic.AddInt64 for safe increments.
// Expected: the result is exactly 1,000,000 every time.
func atomicAddCounter() int64 {
	var counter int64
	var wg sync.WaitGroup

	// TODO: launch 1000 goroutines, each incrementing counter 1000 times with atomic.AddInt64
	// TODO: use wg.Add(1), wg.Done(), wg.Wait()
	// TODO: return counter
	_ = wg.Wait        // hint: use WaitGroup to synchronize
	_ = atomic.AddInt64 // hint: atomic.AddInt64(&counter, 1)

	return counter
}

// Step 3: Implement typedAtomicCounter.
// Use atomic.Int64 (Go 1.19+) instead of raw int64 + atomic functions.
// Expected: the result is exactly 1,000,000 every time.
func typedAtomicCounter() int64 {
	var counter atomic.Int64
	var wg sync.WaitGroup

	// TODO: launch 1000 goroutines, each calling counter.Add(1) 1000 times
	// TODO: use wg.Add(1), wg.Done(), wg.Wait()
	// TODO: return counter.Load()
	_ = wg.Wait     // hint: use WaitGroup to synchronize
	_ = counter.Add  // hint: counter.Add(1)
	_ = counter.Load // hint: counter.Load() to read final value

	return 0
}

// Verify: Implement bidirectionalCounter.
// Launch 500 goroutines that increment 1000 times each (+500,000)
// Launch 500 goroutines that decrement 1000 times each (-500,000)
// The final result must be exactly 0.
func bidirectionalCounter() int64 {
	var counter int64
	var wg sync.WaitGroup

	// TODO: launch 500 incrementing goroutines (atomic.AddInt64(&counter, 1))
	// TODO: launch 500 decrementing goroutines (atomic.AddInt64(&counter, -1))
	// TODO: wait and return counter
	_ = wg.Wait        // hint: use WaitGroup to synchronize
	_ = atomic.AddInt64 // hint: atomic.AddInt64(&counter, 1) and atomic.AddInt64(&counter, -1)

	return counter
}

func main() {
	fmt.Println("Exercise: Atomic Add Counter")
	fmt.Println()

	fmt.Println("=== Step 1: Broken Counter (non-atomic) ===")
	result := brokenCounter()
	fmt.Printf("  Expected: 1000000\n")
	fmt.Printf("  Got:      %d (likely wrong due to data race)\n\n", result)

	fmt.Println("=== Step 2: Atomic Add Counter ===")
	result = atomicAddCounter()
	fmt.Printf("  Expected: 1000000\n")
	fmt.Printf("  Got:      %d\n\n", result)

	fmt.Println("=== Step 3: Typed atomic.Int64 Counter ===")
	result = typedAtomicCounter()
	fmt.Printf("  Expected: 1000000\n")
	fmt.Printf("  Got:      %d\n\n", result)

	fmt.Println("=== Verify: Bidirectional Counter ===")
	result = bidirectionalCounter()
	fmt.Printf("  Expected: 0\n")
	fmt.Printf("  Got:      %d\n\n", result)
}
