package main

// Atomic Compare-And-Swap — Production-quality educational code
//
// Demonstrates the CAS primitive and its applications:
// 1. CAS retry loop for lock-free increment
// 2. Lock-free maximum tracker
// 3. Mutex-based max tracker for comparison
// 4. Clamped add: conditional atomic update with ceiling
// 5. Lock-free state machine transitions
//
// Expected output:
//   === Example 1: CAS Increment Counter ===
//     Expected: 1000000
//     Got:      1000000
//
//   === Example 2: Lock-Free Max Tracker (CAS) ===
//     Maximum found (CAS): <close to 999999>
//
//   === Example 3: Max Tracker (Mutex comparison) ===
//     Maximum found (Mutex): <close to 999999>
//
//   === Example 4: Clamped Add ===
//     Counter (ceiling 1000): <value <= 1000>
//     PASS: counter did not exceed ceiling
//
//   === Example 5: Lock-Free State Machine ===
//     Transitioned: idle -> running
//     Transitioned: running -> stopping
//     Transitioned: stopping -> stopped
//     Invalid transition from stopped to running: false

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
)

// casIncrement atomically increments *addr by 1 using a CAS retry loop.
// This is functionally identical to atomic.AddInt64(addr, 1) but
// demonstrates the universal CAS pattern: load, compute, CAS, retry.
//
// Why this matters: atomic.AddInt64 only supports addition. CAS lets you
// build ANY atomic read-modify-write operation (max, min, conditional
// update, state machine transitions, etc.).
func casIncrement(addr *int64) {
	for {
		old := atomic.LoadInt64(addr)
		next := old + 1
		if atomic.CompareAndSwapInt64(addr, old, next) {
			return // CAS succeeded: we atomically changed old -> old+1
		}
		// CAS failed: another goroutine changed *addr between our Load and CAS.
		// The old value is stale. Loop back to reload and try again.
	}
}

// casCounter uses casIncrement from 1000 goroutines x 1000 iterations.
// The result must be exactly 1,000,000 to prove correctness.
func casCounter() int64 {
	var counter int64
	var wg sync.WaitGroup

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				casIncrement(&counter)
			}
		}()
	}

	wg.Wait()
	return counter
}

// casUpdateMax atomically updates *addr to val if val > current value.
// This cannot be done with atomic.AddInt64 because the operation is
// conditional: we only write if the new value is actually larger.
func casUpdateMax(addr *int64, val int64) {
	for {
		old := atomic.LoadInt64(addr)
		if val <= old {
			return // nothing to update — current max is already >= val
		}
		if atomic.CompareAndSwapInt64(addr, old, val) {
			return // successfully updated the max
		}
		// CAS failed: another goroutine updated the max concurrently.
		// Reload and re-check — the new max may already be >= val.
	}
}

// trackMaxCAS launches 100 goroutines, each generating 1000 random values
// and atomically tracking the global maximum using CAS.
func trackMaxCAS() int64 {
	var maxVal int64
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				val := rand.Int63n(1_000_000)
				casUpdateMax(&maxVal, val)
			}
		}()
	}

	wg.Wait()
	return atomic.LoadInt64(&maxVal)
}

// trackMaxMutex does the same as trackMaxCAS but using sync.Mutex.
// Structurally simpler (lock, check, update, unlock) but has locking overhead.
func trackMaxMutex() int64 {
	var maxVal int64
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				val := rand.Int63n(1_000_000)
				mu.Lock()
				if val > maxVal {
					maxVal = val
				}
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	return maxVal
}

// casClampedAdd atomically adds delta to *addr, but only if the result
// does not exceed ceiling. Returns true if the add was applied.
//
// This is a real-world pattern: rate limiters, connection pool limits,
// and resource quotas all use conditional atomic updates.
func casClampedAdd(addr *int64, delta int64, ceiling int64) bool {
	for {
		old := atomic.LoadInt64(addr)
		next := old + delta
		if next > ceiling {
			return false // would exceed ceiling — reject
		}
		if atomic.CompareAndSwapInt64(addr, old, next) {
			return true // successfully applied the clamped add
		}
		// CAS failed: another goroutine modified the value. Retry.
	}
}

// testClampedAdd launches goroutines that try to add to a counter with
// a ceiling. The final value must never exceed the ceiling.
func testClampedAdd() int64 {
	var counter int64
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				casClampedAdd(&counter, 1, 1000)
			}
		}()
	}

	wg.Wait()
	return atomic.LoadInt64(&counter)
}

// State constants for the lock-free state machine
const (
	stateIdle     int64 = 0
	stateRunning  int64 = 1
	stateStopping int64 = 2
	stateStopped  int64 = 3
)

func stateName(s int64) string {
	switch s {
	case stateIdle:
		return "idle"
	case stateRunning:
		return "running"
	case stateStopping:
		return "stopping"
	case stateStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// casTransition atomically transitions from expectedState to newState.
// Returns true if the transition succeeded (state was expectedState).
// This pattern enforces valid state transitions without locks.
func casTransition(state *int64, expectedState, newState int64) bool {
	return atomic.CompareAndSwapInt64(state, expectedState, newState)
}

// stateMachineDemo shows how CAS enforces valid state transitions.
// Only the correct sequence of transitions succeeds.
func stateMachineDemo() {
	var state int64 // starts at stateIdle (0)

	// Valid transitions: idle -> running -> stopping -> stopped
	if casTransition(&state, stateIdle, stateRunning) {
		fmt.Printf("  Transitioned: %s -> %s\n", stateName(stateIdle), stateName(stateRunning))
	}

	if casTransition(&state, stateRunning, stateStopping) {
		fmt.Printf("  Transitioned: %s -> %s\n", stateName(stateRunning), stateName(stateStopping))
	}

	if casTransition(&state, stateStopping, stateStopped) {
		fmt.Printf("  Transitioned: %s -> %s\n", stateName(stateStopping), stateName(stateStopped))
	}

	// Invalid transition: cannot go from stopped back to running
	ok := casTransition(&state, stateRunning, stateStopped)
	fmt.Printf("  Invalid transition from stopped to running: %v\n", ok)
}

func main() {
	fmt.Println("Atomic Compare-And-Swap")
	fmt.Println()

	fmt.Println("=== Example 1: CAS Increment Counter ===")
	result := casCounter()
	fmt.Printf("  Expected: 1000000\n")
	fmt.Printf("  Got:      %d\n\n", result)

	fmt.Println("=== Example 2: Lock-Free Max Tracker (CAS) ===")
	maxCAS := trackMaxCAS()
	fmt.Printf("  Maximum found (CAS):   %d\n\n", maxCAS)

	fmt.Println("=== Example 3: Max Tracker (Mutex comparison) ===")
	maxMutex := trackMaxMutex()
	fmt.Printf("  Maximum found (Mutex): %d\n\n", maxMutex)

	fmt.Println("=== Example 4: Clamped Add ===")
	clamped := testClampedAdd()
	fmt.Printf("  Counter (ceiling 1000): %d\n", clamped)
	if clamped <= 1000 {
		fmt.Println("  PASS: counter did not exceed ceiling")
	} else {
		fmt.Println("  FAIL: counter exceeded ceiling!")
	}
	fmt.Println()

	fmt.Println("=== Example 5: Lock-Free State Machine ===")
	stateMachineDemo()
	fmt.Println()
}
