package main

// Exercise: Spinlock with Atomic CAS
// Instructions: see 05-spinlock-with-atomic.md

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// SpinLock is a mutual exclusion lock built from atomic CAS.
// Zero value is unlocked.
type SpinLock struct {
	state int32 // 0 = unlocked, 1 = locked
}

// Step 1: Implement Lock using a CAS loop.
// Spin until CompareAndSwapInt32(&s.state, 0, 1) succeeds.
// Call runtime.Gosched() on each failed attempt to yield the OS thread.
func (s *SpinLock) Lock() {
	// TODO: CAS loop to acquire lock
	_ = atomic.CompareAndSwapInt32 // hint: CAS(&s.state, 0, 1)
	_ = runtime.Gosched            // hint: yield in the spin loop
}

// Step 1: Implement Unlock using atomic Store.
// Set state back to 0 to release the lock.
func (s *SpinLock) Unlock() {
	// TODO: atomic.StoreInt32(&s.state, 0)
	_ = atomic.StoreInt32 // hint
}

// Step 2: testSpinLock verifies mutual exclusion.
// 100 goroutines each increment a shared counter 1000 times, protected by SpinLock.
// Expected result: exactly 100,000.
func testSpinLock() int64 {
	var lock SpinLock
	var counter int64
	var wg sync.WaitGroup

	// TODO: launch 100 goroutines, each incrementing counter 1000 times
	//       using lock.Lock() / counter++ / lock.Unlock()
	// TODO: wait and return counter
	_ = wg.Wait  // hint: use WaitGroup to synchronize
	_ = lock.Lock // hint: protect counter access

	return counter
}

// Step 2: testMutex does the same using sync.Mutex for comparison.
func testMutex() int64 {
	var mu sync.Mutex
	var counter int64
	var wg sync.WaitGroup

	// TODO: launch 100 goroutines, each incrementing counter 1000 times
	//       using mu.Lock() / counter++ / mu.Unlock()
	// TODO: wait and return counter
	_ = wg.Wait // hint: use WaitGroup to synchronize
	_ = mu.Lock  // hint: protect counter access

	return counter
}

// testSpinLockN runs the spinlock test with configurable goroutines and iterations.
func testSpinLockN(goroutines, iterations int) int64 {
	var lock SpinLock
	var counter int64
	var wg sync.WaitGroup

	// TODO: same as testSpinLock but with goroutines and iterations parameters
	_ = wg.Wait  // hint
	_ = lock.Lock // hint
	_ = goroutines
	_ = iterations

	return counter
}

// testMutexN runs the mutex test with configurable goroutines and iterations.
func testMutexN(goroutines, iterations int) int64 {
	var mu sync.Mutex
	var counter int64
	var wg sync.WaitGroup

	// TODO: same as testMutex but with goroutines and iterations parameters
	_ = wg.Wait // hint
	_ = mu.Lock  // hint
	_ = goroutines
	_ = iterations

	return counter
}

// Step 3: compareContention measures SpinLock vs Mutex under low and high contention.
func compareContention() {
	// TODO: low contention test (4 goroutines, 10000 iterations each)
	//   time testSpinLockN(4, 10000) and testMutexN(4, 10000)
	//   print both durations

	// TODO: high contention test (1000 goroutines, 1000 iterations each)
	//   time testSpinLockN(1000, 1000) and testMutexN(1000, 1000)
	//   print both durations
	_ = time.Now    // hint: measure elapsed time
	_ = time.Since  // hint: calculate duration
}

// Verify: Implement TryLock.
// Attempt to acquire the lock exactly once (single CAS attempt).
// Return true if acquired, false if the lock was already held.
func (s *SpinLock) TryLock() bool {
	// TODO: single CAS attempt, return result
	_ = atomic.CompareAndSwapInt32 // hint: one CAS call
	return false
}

// testTryLock launches goroutines that all call TryLock simultaneously.
// Counts how many succeed.
func testTryLock() {
	var lock SpinLock
	var acquired atomic.Int32
	var wg sync.WaitGroup

	// TODO: launch 100 goroutines that each call lock.TryLock()
	// TODO: if TryLock returns true, increment acquired counter,
	//       do some work, then Unlock
	// TODO: print how many goroutines acquired the lock
	_ = wg.Wait      // hint: use WaitGroup to synchronize
	_ = lock.TryLock  // hint: non-blocking lock attempt
	_ = acquired.Add  // hint: count successful acquisitions
}

func main() {
	fmt.Println("Exercise: Spinlock with Atomic CAS")
	fmt.Println()

	fmt.Println("=== Step 2: SpinLock Mutual Exclusion Test ===")
	result := testSpinLock()
	fmt.Printf("  Expected: 100000\n")
	fmt.Printf("  Got:      %d\n\n", result)

	fmt.Println("=== Step 2: Mutex Comparison ===")
	result = testMutex()
	fmt.Printf("  Expected: 100000\n")
	fmt.Printf("  Got:      %d\n\n", result)

	fmt.Println("=== Step 3: Contention Comparison ===")
	compareContention()
	fmt.Println()

	fmt.Println("=== Verify: TryLock ===")
	testTryLock()
	fmt.Println()
}
