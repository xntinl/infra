package main

// Spinlock with Atomic CAS — Production-quality educational code
//
// Builds a spinlock from atomic CAS and compares it to sync.Mutex:
// 1. SpinLock implementation (Lock, Unlock, TryLock)
// 2. Mutual exclusion correctness test
// 3. Contention comparison: SpinLock vs sync.Mutex
//
// Expected output:
//   === Example 1: SpinLock Mutual Exclusion ===
//     Expected: 100000
//     Got:      100000
//
//   === Example 2: Mutex Comparison ===
//     Expected: 100000
//     Got:      100000
//
//   === Example 3: Contention Comparison ===
//     Low contention  (4 goroutines):   SpinLock=<time>, Mutex=<time>
//     High contention (1000 goroutines): SpinLock=<time>, Mutex=<time>
//
//   === Example 4: TryLock ===
//     Out of 100 goroutines, <N> acquired the lock on first try
//
//   === Example 5: Why sync.Mutex Wins ===
//     SpinLock under sustained contention: <slower>
//     Mutex under sustained contention:    <faster>

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// SpinLock is a mutual exclusion lock built entirely from atomic CAS.
// The zero value is an unlocked spinlock.
//
// How it works:
// - state=0 means unlocked, state=1 means locked.
// - Lock() spins in a CAS loop trying to change 0->1.
// - Unlock() atomically stores 0.
//
// WHY this exists: to teach how CAS builds higher-level primitives.
// WHY you should not use it: sync.Mutex uses a hybrid spin-then-park
// strategy that is better in virtually all Go programs.
type SpinLock struct {
	state int32 // 0=unlocked, 1=locked
}

// Lock acquires the spinlock. If another goroutine holds it, this spins
// until the lock becomes available.
//
// runtime.Gosched() is CRITICAL here. Without it:
// - With GOMAXPROCS=1, the spinner holds the only OS thread and the lock
//   holder cannot run to call Unlock() — deadlock.
// - With more threads, tight spinning wastes CPU and starves other goroutines.
func (s *SpinLock) Lock() {
	for !atomic.CompareAndSwapInt32(&s.state, 0, 1) {
		runtime.Gosched() // yield to other goroutines
	}
}

// Unlock releases the spinlock. Must only be called by the goroutine
// that called Lock(). Unlike sync.Mutex, this simple implementation
// does not detect double-unlock or unlock-without-lock.
func (s *SpinLock) Unlock() {
	atomic.StoreInt32(&s.state, 0)
}

// TryLock attempts to acquire the lock exactly once (non-blocking).
// Returns true if the lock was acquired, false if it was already held.
// Useful for non-blocking algorithms where you want to try but not wait.
func (s *SpinLock) TryLock() bool {
	return atomic.CompareAndSwapInt32(&s.state, 0, 1)
}

// testSpinLock verifies that SpinLock provides correct mutual exclusion.
// 100 goroutines each increment a shared counter 1000 times, protected
// by the spinlock. If mutual exclusion works, the result is exactly 100,000.
func testSpinLock() int64 {
	var lock SpinLock
	var counter int64
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				lock.Lock()
				counter++ // safe: protected by spinlock
				lock.Unlock()
			}
		}()
	}

	wg.Wait()
	return counter
}

// testMutex does the same as testSpinLock using sync.Mutex for comparison.
func testMutex() int64 {
	var mu sync.Mutex
	var counter int64
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
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

// testSpinLockN runs the spinlock test with configurable goroutines and iterations.
func testSpinLockN(goroutines, iterations int) int64 {
	var lock SpinLock
	var counter int64
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				lock.Lock()
				counter++
				lock.Unlock()
			}
		}()
	}

	wg.Wait()
	return counter
}

// testMutexN runs the mutex test with configurable goroutines and iterations.
func testMutexN(goroutines, iterations int) int64 {
	var mu sync.Mutex
	var counter int64
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				mu.Lock()
				counter++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	return counter
}

// compareContention measures SpinLock vs Mutex under low and high contention.
// Low contention: few goroutines rarely collide — spinlock may be faster.
// High contention: many goroutines constantly collide — mutex wins because
// blocked goroutines sleep instead of burning CPU cycles spinning.
func compareContention() {
	// Low contention: 4 goroutines, short critical section
	start := time.Now()
	testSpinLockN(4, 10000)
	spinLow := time.Since(start)

	start = time.Now()
	testMutexN(4, 10000)
	mutexLow := time.Since(start)

	fmt.Printf("  Low contention  (4 goroutines):    SpinLock=%v, Mutex=%v\n", spinLow, mutexLow)

	// High contention: 1000 goroutines, short critical section
	start = time.Now()
	testSpinLockN(1000, 1000)
	spinHigh := time.Since(start)

	start = time.Now()
	testMutexN(1000, 1000)
	mutexHigh := time.Since(start)

	fmt.Printf("  High contention (1000 goroutines): SpinLock=%v, Mutex=%v\n", spinHigh, mutexHigh)
}

// testTryLock launches goroutines that all call TryLock simultaneously.
// Only one goroutine at a time can succeed. This demonstrates the
// non-blocking lock acquisition pattern.
func testTryLock() {
	var lock SpinLock
	var acquired atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if lock.TryLock() {
				acquired.Add(1)
				// Simulate holding the lock briefly
				runtime.Gosched()
				lock.Unlock()
			}
			// If TryLock returned false, we move on without waiting
		}()
	}

	wg.Wait()
	fmt.Printf("  Out of 100 goroutines, %d acquired the lock on first try\n", acquired.Load())
}

// sustainedContention demonstrates why sync.Mutex is superior under real
// workloads. With many goroutines contending for a long time, the spinlock
// wastes CPU on spinning while Mutex parks blocked goroutines efficiently.
func sustainedContention() {
	const goroutines = 500
	const iterations = 500

	// SpinLock under heavy contention
	start := time.Now()
	testSpinLockN(goroutines, iterations)
	spinDur := time.Since(start)

	// Mutex under same contention
	start = time.Now()
	testMutexN(goroutines, iterations)
	mutexDur := time.Since(start)

	fmt.Printf("  SpinLock under sustained contention: %v\n", spinDur)
	fmt.Printf("  Mutex under sustained contention:    %v\n", mutexDur)
}

func main() {
	fmt.Println("Spinlock with Atomic CAS")
	fmt.Println()

	fmt.Println("=== Example 1: SpinLock Mutual Exclusion ===")
	result := testSpinLock()
	fmt.Printf("  Expected: 100000\n")
	fmt.Printf("  Got:      %d\n\n", result)

	fmt.Println("=== Example 2: Mutex Comparison ===")
	result = testMutex()
	fmt.Printf("  Expected: 100000\n")
	fmt.Printf("  Got:      %d\n\n", result)

	fmt.Println("=== Example 3: Contention Comparison ===")
	compareContention()
	fmt.Println()

	fmt.Println("=== Example 4: TryLock ===")
	testTryLock()
	fmt.Println()

	fmt.Println("=== Example 5: Why sync.Mutex Wins ===")
	sustainedContention()
	fmt.Println()
}
