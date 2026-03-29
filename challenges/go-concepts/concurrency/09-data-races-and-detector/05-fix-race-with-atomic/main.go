package main

// Expected output:
//
//   === Fix Race with Atomic ===
//
//   --- Racy Counter (for comparison) ---
//   Result: 583921   (expected 1000000) -- WRONG
//
//   --- Fix: atomic.AddInt64 ---
//   Result: 1000000  (expected 1000000) -- CORRECT
//
//   --- Exploring Atomic Operations ---
//   AddInt64:         counter = 1000000
//   Store + Load:     stored 42, loaded 42
//   Swap:             old = 42, new = 99
//   CompareAndSwap:   swapped 99 -> 200: true
//   CompareAndSwap:   swapped 200 -> 300 (wrong expected): false
//
//   === Grand Comparison: All Four Approaches ===
//     Mutex:       1000000 in 248.3ms
//     Channel:     1000000 in 1.82s
//     Atomic:      1000000 in 45.1ms
//   Atomic is fastest for simple counters (no lock, no channel overhead).
//   Channel is slowest (goroutine scheduling per increment).
//   Choose based on complexity of shared state, not just speed.
//
//   === Decision Guide ===
//   atomic  -> simple counters, flags, single values
//   mutex   -> complex structs, multi-field updates, read-heavy (RWMutex)
//   channel -> ownership transfer, pipelines, meaningful messages
//
//   Verify: go run -race main.go
//   Only racyCounter should trigger DATA RACE warnings.

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const (
	numGoroutines   = 1000
	incrementsPerGR = 1000
	expectedTotal   = numGoroutines * incrementsPerGR
)

// racyCounter is the broken version from exercise 01.
func racyCounter() int {
	counter := 0
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGR; j++ {
				counter++ // DATA RACE
			}
		}()
	}

	wg.Wait()
	return counter
}

// safeCounterMutex is the mutex solution from exercise 03 (for timing).
func safeCounterMutex() int {
	counter := 0
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGR; j++ {
				mu.Lock()
				counter++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	return counter
}

// safeCounterChannel is the channel solution from exercise 04 (for timing).
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
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGR; j++ {
				increments <- struct{}{}
			}
		}()
	}

	wg.Wait()
	close(increments)
	return <-done
}

// safeCounterAtomic fixes the race using sync/atomic.AddInt64.
// Atomic operations use CPU-level instructions (e.g., LOCK XADD on x86)
// that complete without interruption from other cores. No lock needed.
func safeCounterAtomic() int64 {
	var counter int64 // must be int64 for atomic functions
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGR; j++ {
				// AddInt64 atomically adds 1 to counter.
				// The entire read-modify-write happens as a single CPU instruction.
				atomic.AddInt64(&counter, 1)
			}
		}()
	}

	wg.Wait()
	// LoadInt64 atomically reads the value. Using counter directly would
	// technically be a race if any goroutine were still writing.
	return atomic.LoadInt64(&counter)
}

// exploreAtomicOps demonstrates the full range of atomic operations.
func exploreAtomicOps() {
	fmt.Println("\n--- Exploring Atomic Operations ---")

	// AddInt64: atomically add a delta.
	var counter int64
	var wg sync.WaitGroup
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGR; j++ {
				atomic.AddInt64(&counter, 1)
			}
		}()
	}
	wg.Wait()
	fmt.Printf("  AddInt64:         counter = %d\n", atomic.LoadInt64(&counter))

	// StoreInt64 + LoadInt64: write and read atomically.
	var flag int64
	atomic.StoreInt64(&flag, 42)
	val := atomic.LoadInt64(&flag)
	fmt.Printf("  Store + Load:     stored 42, loaded %d\n", val)

	// SwapInt64: atomically set a new value and return the old one.
	old := atomic.SwapInt64(&flag, 99)
	fmt.Printf("  Swap:             old = %d, new = %d\n", old, atomic.LoadInt64(&flag))

	// CompareAndSwapInt64 (CAS): set new value ONLY if current == expected.
	// This is the fundamental building block of lock-free algorithms.
	swapped := atomic.CompareAndSwapInt64(&flag, 99, 200)
	fmt.Printf("  CompareAndSwap:   swapped 99 -> 200: %v\n", swapped)

	// CAS fails if the current value does not match the expected value.
	swapped = atomic.CompareAndSwapInt64(&flag, 200, 300)
	fmt.Printf("  CompareAndSwap:   swapped 200 -> 300 (wrong expected): %v\n", !swapped)
}

func printResult(label string, result int64) {
	status := "CORRECT"
	if result != int64(expectedTotal) {
		status = "WRONG"
	}
	fmt.Printf("Result: %-10d (expected %d) -- %s\n", result, expectedTotal, status)
}

func main() {
	fmt.Println("=== Fix Race with Atomic ===")

	fmt.Println("\n--- Racy Counter (for comparison) ---")
	result := racyCounter()
	status := "WRONG"
	if result == expectedTotal {
		status = "CORRECT"
	}
	fmt.Printf("Result: %-10d (expected %d) -- %s\n", result, expectedTotal, status)

	fmt.Println("\n--- Fix: atomic.AddInt64 ---")
	printResult("atomic", safeCounterAtomic())

	exploreAtomicOps()

	// Grand comparison of all three correct approaches.
	fmt.Println("\n=== Grand Comparison: All Four Approaches ===")

	start := time.Now()
	safeCounterMutex()
	mutexD := time.Since(start)
	fmt.Printf("  Mutex:       %d in %v\n", expectedTotal, mutexD)

	start = time.Now()
	safeCounterChannel()
	channelD := time.Since(start)
	fmt.Printf("  Channel:     %d in %v\n", expectedTotal, channelD)

	start = time.Now()
	safeCounterAtomic()
	atomicD := time.Since(start)
	fmt.Printf("  Atomic:      %d in %v\n", expectedTotal, atomicD)

	fmt.Println()
	fmt.Println("Atomic is fastest for simple counters (no lock, no channel overhead).")
	fmt.Println("Channel is slowest (goroutine scheduling per increment).")
	fmt.Println("Choose based on complexity of shared state, not just speed.")

	fmt.Println("\n=== Decision Guide ===")
	fmt.Println("  atomic  -> simple counters, flags, single values")
	fmt.Println("  mutex   -> complex structs, multi-field updates, read-heavy (RWMutex)")
	fmt.Println("  channel -> ownership transfer, pipelines, meaningful messages")

	_ = mutexD
	_ = channelD
	_ = atomicD

	fmt.Println("\nVerify: go run -race main.go")
	fmt.Println("Only racyCounter should trigger DATA RACE warnings.")
}
