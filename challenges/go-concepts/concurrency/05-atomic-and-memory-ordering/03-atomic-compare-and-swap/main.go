package main

// Exercise: Atomic Compare-And-Swap
// Instructions: see 03-atomic-compare-and-swap.md

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
)

// Step 1: Implement casIncrement.
// Use a CAS retry loop to atomically increment *addr by 1.
// Pattern: load old, compute new, CAS(addr, old, new), retry if failed.
func casIncrement(addr *int64) {
	// TODO: implement CAS retry loop for increment
	_ = atomic.LoadInt64            // hint: load current value
	_ = atomic.CompareAndSwapInt64  // hint: CAS(addr, old, old+1)
}

// casCounter uses casIncrement from 1000 goroutines, each calling it 1000 times.
// Expected result: exactly 1,000,000.
func casCounter() int64 {
	var counter int64
	var wg sync.WaitGroup

	// TODO: launch 1000 goroutines, each calling casIncrement(&counter) 1000 times
	// TODO: wait and return counter
	_ = wg.Wait // hint: use WaitGroup to synchronize

	return counter
}

// Step 2: Implement casUpdateMax.
// Atomically update *addr to val if val > current value.
// Use a CAS retry loop. Return immediately if val <= current.
func casUpdateMax(addr *int64, val int64) {
	// TODO: implement CAS retry loop for max update
	_ = atomic.LoadInt64            // hint: load current max
	_ = atomic.CompareAndSwapInt64  // hint: CAS(addr, old, val)
}

// trackMax launches 100 goroutines, each generating 1000 random values
// and calling casUpdateMax. Returns the final maximum.
func trackMax() int64 {
	var maxVal int64
	var wg sync.WaitGroup

	// TODO: launch 100 goroutines, each generating 1000 random int64s
	// TODO: call casUpdateMax for each value
	// TODO: wait and return the maximum (use atomic.LoadInt64)
	_ = wg.Wait    // hint: use WaitGroup to synchronize
	_ = rand.Int63n // hint: generate random values with rand.Int63n(1_000_000)

	return atomic.LoadInt64(&maxVal)
}

// Step 3: Implement trackMaxMutex.
// Same as trackMax but using sync.Mutex instead of CAS.
func trackMaxMutex() int64 {
	var maxVal int64
	var mu sync.Mutex
	var wg sync.WaitGroup

	// TODO: launch 100 goroutines, each generating 1000 random int64s
	// TODO: lock mutex, compare and update maxVal, unlock
	// TODO: wait and return maxVal
	_ = wg.Wait  // hint: use WaitGroup to synchronize
	_ = mu.Lock   // hint: protect the critical section

	return maxVal
}

// Verify: Implement casClampedAdd.
// Atomically add delta to *addr, but only if the result does not exceed ceiling.
// Returns true if the add was applied, false if skipped.
func casClampedAdd(addr *int64, delta int64, ceiling int64) bool {
	// TODO: CAS loop: load old, check old+delta <= ceiling, CAS, retry
	_ = atomic.LoadInt64            // hint
	_ = atomic.CompareAndSwapInt64  // hint
	return false
}

// testClampedAdd launches goroutines that try to add to a counter with a ceiling.
func testClampedAdd() int64 {
	var counter int64
	var wg sync.WaitGroup

	// TODO: launch 100 goroutines, each attempting casClampedAdd(&counter, 1, 1000)
	//       in a loop of 100 iterations
	// TODO: wait and return final counter (should be <= 1000)
	_ = wg.Wait // hint: use WaitGroup to synchronize

	return atomic.LoadInt64(&counter)
}

func main() {
	fmt.Println("Exercise: Atomic Compare-And-Swap")
	fmt.Println()

	fmt.Println("=== Step 1: CAS Increment Counter ===")
	result := casCounter()
	fmt.Printf("  Expected: 1000000\n")
	fmt.Printf("  Got:      %d\n\n", result)

	fmt.Println("=== Step 2: Lock-Free Max Tracker (CAS) ===")
	maxCAS := trackMax()
	fmt.Printf("  Maximum found (CAS):   %d\n\n", maxCAS)

	fmt.Println("=== Step 3: Max Tracker (Mutex) ===")
	maxMutex := trackMaxMutex()
	fmt.Printf("  Maximum found (Mutex): %d\n\n", maxMutex)

	fmt.Println("=== Verify: Clamped Add ===")
	clamped := testClampedAdd()
	fmt.Printf("  Counter (ceiling 1000): %d\n", clamped)
	if clamped <= 1000 {
		fmt.Println("  PASS: counter did not exceed ceiling")
	} else {
		fmt.Println("  FAIL: counter exceeded ceiling!")
	}
	fmt.Println()
}
