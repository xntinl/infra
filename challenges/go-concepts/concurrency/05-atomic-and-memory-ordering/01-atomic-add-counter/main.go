package main

// Atomic Add Counter — Production-quality educational code
//
// Demonstrates three approaches to concurrent counting:
// 1. Broken non-atomic counter (data race)
// 2. Fixed counter using atomic.AddInt64
// 3. Modern counter using atomic.Int64 (Go 1.19+)
// 4. Bidirectional counter with increments and decrements
//
// Expected output:
//   === Example 1: Broken Counter (non-atomic) ===
//     Expected: 1000000
//     Got:      <varies, typically less than 1000000>
//
//   === Example 2: Atomic AddInt64 Counter ===
//     Expected: 1000000
//     Got:      1000000
//
//   === Example 3: Typed atomic.Int64 Counter ===
//     Expected: 1000000
//     Got:      1000000
//
//   === Example 4: Bidirectional Counter ===
//     Expected: 0
//     Got:      0
//
//   === Example 5: Multiple Independent Counters ===
//     Reads:    <value>
//     Writes:   <value>
//     Errors:   <value>
//     Total ops: <sum equals reads+writes+errors>

import (
	"fmt"
	"sync"
	"sync/atomic"
)

const (
	numGoroutines = 1000
	numIterations = 1000
)

// brokenCounter demonstrates the data race: counter++ is load-modify-store,
// three separate operations that interleave across goroutines. The result
// is always less than the expected 1,000,000 because updates get lost.
func brokenCounter() int64 {
	var counter int64
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				// BUG: this compiles to load -> add -> store.
				// Two goroutines can load the same value, both add 1,
				// and both store the same result — one increment is lost.
				counter++
			}
		}()
	}

	wg.Wait()
	return counter
}

// atomicAddCounter fixes the race by using atomic.AddInt64, which performs
// the entire read-add-write as a single CPU instruction (e.g., LOCK XADD on x86).
// No goroutine ever sees a half-updated value.
func atomicAddCounter() int64 {
	var counter int64
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				// Atomic: the pointer ensures we modify the original,
				// and the hardware guarantees indivisibility.
				atomic.AddInt64(&counter, 1)
			}
		}()
	}

	wg.Wait()
	return counter
}

// typedAtomicCounter uses atomic.Int64 (Go 1.19+), the recommended modern API.
// Benefits over the function-based API:
//   - Method-based: counter.Add(1) instead of atomic.AddInt64(&counter, 1)
//   - Impossible to accidentally read/write non-atomically (the underlying
//     value is unexported)
//   - No need to pass pointers — the receiver handles it
func typedAtomicCounter() int64 {
	var counter atomic.Int64
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				counter.Add(1)
			}
		}()
	}

	wg.Wait()
	return counter.Load()
}

// bidirectionalCounter proves atomics handle negative deltas correctly.
// 500 goroutines increment 1000 times each (+500,000), and 500 goroutines
// decrement 1000 times each (-500,000). The net result must be exactly 0.
func bidirectionalCounter() int64 {
	var counter int64
	var wg sync.WaitGroup

	halfGoroutines := numGoroutines / 2

	// Incrementers
	for i := 0; i < halfGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				atomic.AddInt64(&counter, 1)
			}
		}()
	}

	// Decrementers — AddInt64 with a negative delta
	for i := 0; i < halfGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				atomic.AddInt64(&counter, -1)
			}
		}()
	}

	wg.Wait()
	return counter
}

// multipleCounters shows a realistic pattern: tracking independent metrics
// concurrently. Each counter is independent, so there is no contention
// between counters — only between goroutines updating the same counter.
func multipleCounters() (reads, writes, errors int64) {
	var readCount, writeCount, errorCount atomic.Int64
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				// Simulate mixed operations based on goroutine ID
				switch id % 3 {
				case 0:
					readCount.Add(1)
				case 1:
					writeCount.Add(1)
				case 2:
					errorCount.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()
	return readCount.Load(), writeCount.Load(), errorCount.Load()
}

func main() {
	fmt.Println("Atomic Add Counter")
	fmt.Println()

	fmt.Println("=== Example 1: Broken Counter (non-atomic) ===")
	result := brokenCounter()
	fmt.Printf("  Expected: %d\n", numGoroutines*numIterations)
	fmt.Printf("  Got:      %d (lost updates due to data race)\n\n", result)

	fmt.Println("=== Example 2: Atomic AddInt64 Counter ===")
	result = atomicAddCounter()
	fmt.Printf("  Expected: %d\n", numGoroutines*numIterations)
	fmt.Printf("  Got:      %d\n\n", result)

	fmt.Println("=== Example 3: Typed atomic.Int64 Counter ===")
	result = typedAtomicCounter()
	fmt.Printf("  Expected: %d\n", numGoroutines*numIterations)
	fmt.Printf("  Got:      %d\n\n", result)

	fmt.Println("=== Example 4: Bidirectional Counter ===")
	result = bidirectionalCounter()
	fmt.Printf("  Expected: 0\n")
	fmt.Printf("  Got:      %d\n\n", result)

	fmt.Println("=== Example 5: Multiple Independent Counters ===")
	reads, writes, errors := multipleCounters()
	fmt.Printf("  Reads:     %d\n", reads)
	fmt.Printf("  Writes:    %d\n", writes)
	fmt.Printf("  Errors:    %d\n", errors)
	fmt.Printf("  Total ops: %d (should be 100000)\n", reads+writes+errors)
}
