package main

// Expected output:
//
//   === Fix Race with Mutex ===
//
//   --- Racy Counter (for comparison) ---
//   Result: 583921 (expected 1000000) -- WRONG
//
//   --- Fix 1: Basic Mutex ---
//   Result: 1000000 (expected 1000000) -- CORRECT
//
//   --- Fix 2: Defer Pattern ---
//   Result: 1000000 (expected 1000000) -- CORRECT
//
//   --- Fix 3: Encapsulated Counter ---
//   Result: 1000000 (expected 1000000) -- CORRECT
//
//   === Timing Comparison ===
//   Racy (wrong):    12.3ms
//   Mutex (basic):   245.6ms
//   Mutex (defer):   251.2ms
//   Slowdown: ~20x (the cost of correctness under high contention)
//
//   === Key Takeaways ===
//   - Mutex serializes access: only one goroutine increments at a time
//   - defer mu.Unlock() is safer: the lock is released even on panic
//   - Encapsulation prevents forgetting to lock
//   - High contention (1000 goroutines on 1 lock) is the worst case for mutex
//
//   Verify: go run -race main.go
//   Only racyCounter should trigger DATA RACE warnings.

import (
	"fmt"
	"sync"
	"time"
)

const (
	numGoroutines   = 1000
	incrementsPerGR = 1000
	expectedTotal   = numGoroutines * incrementsPerGR
)

// racyCounter is the broken version from exercise 01, included for comparison.
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

// safeCounterMutex fixes the race by protecting counter++ with a sync.Mutex.
// mu.Lock() ensures only one goroutine executes the critical section at a time.
// mu.Unlock() releases the lock so the next waiting goroutine can proceed.
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
				counter++ // only one goroutine at a time reaches this line
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	return counter
}

// safeCounterDefer uses the idiomatic defer pattern. By wrapping the critical
// section in a closure with defer mu.Unlock(), we guarantee the lock is
// released even if the code inside panics. This is the recommended pattern.
func safeCounterDefer() int {
	counter := 0
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Extract the critical section into a named closure.
	// This makes the locking scope explicit and limits it to the minimum.
	increment := func() {
		mu.Lock()
		defer mu.Unlock()
		counter++
	}

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGR; j++ {
				increment()
			}
		}()
	}

	wg.Wait()
	return counter
}

// SafeCounter encapsulates the mutex inside a struct, preventing callers
// from forgetting to lock. This is the production-quality pattern: the
// mutex is an implementation detail, not part of the public API.
type SafeCounter struct {
	mu    sync.Mutex
	value int
}

func (c *SafeCounter) Increment() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value++
}

func (c *SafeCounter) Value() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.value
}

func safeCounterEncapsulated() int {
	c := &SafeCounter{}
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGR; j++ {
				c.Increment()
			}
		}()
	}

	wg.Wait()
	return c.Value()
}

func printResult(label string, result int) {
	status := "CORRECT"
	if result != expectedTotal {
		status = "WRONG"
	}
	fmt.Printf("Result: %-10d (expected %d) -- %s\n", result, expectedTotal, status)
}

func timeFunc(name string, fn func() int) time.Duration {
	start := time.Now()
	fn()
	d := time.Since(start)
	fmt.Printf("  %-20s %v\n", name+":", d)
	return d
}

func main() {
	fmt.Println("=== Fix Race with Mutex ===")

	// Show the broken version for contrast.
	fmt.Println("\n--- Racy Counter (for comparison) ---")
	printResult("racy", racyCounter())

	// Fix 1: basic Lock/Unlock around the critical section.
	fmt.Println("\n--- Fix 1: Basic Mutex ---")
	printResult("mutex", safeCounterMutex())

	// Fix 2: defer Unlock for safety against panics.
	fmt.Println("\n--- Fix 2: Defer Pattern ---")
	printResult("defer", safeCounterDefer())

	// Fix 3: encapsulated counter hides locking from callers.
	fmt.Println("\n--- Fix 3: Encapsulated Counter ---")
	printResult("encap", safeCounterEncapsulated())

	// Timing comparison to show the contention cost.
	fmt.Println("\n=== Timing Comparison ===")
	racyDuration := timeFunc("Racy (wrong)", racyCounter)
	mutexDuration := timeFunc("Mutex (basic)", safeCounterMutex)
	_ = timeFunc("Mutex (defer)", safeCounterDefer)

	if racyDuration > 0 {
		fmt.Printf("  Slowdown: ~%.0fx (the cost of correctness under high contention)\n",
			float64(mutexDuration)/float64(racyDuration))
	}

	fmt.Println("\n=== Key Takeaways ===")
	fmt.Println("- Mutex serializes access: only one goroutine increments at a time")
	fmt.Println("- defer mu.Unlock() is safer: the lock is released even on panic")
	fmt.Println("- Encapsulation prevents forgetting to lock")
	fmt.Println("- High contention (1000 goroutines on 1 lock) is the worst case for mutex")

	fmt.Println("\nVerify: go run -race main.go")
	fmt.Println("Only racyCounter should trigger DATA RACE warnings.")
}
