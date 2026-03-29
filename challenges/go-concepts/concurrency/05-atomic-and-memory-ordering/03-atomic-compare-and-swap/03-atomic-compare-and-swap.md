---
difficulty: intermediate
concepts: [CompareAndSwapInt64, CAS loop, optimistic concurrency, lock-free increment]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [goroutines, sync.WaitGroup, atomic.LoadInt64, atomic.StoreInt64]
---

# 3. Atomic Compare-And-Swap


## Learning Objectives
After completing this exercise, you will be able to:
- **Explain** the compare-and-swap (CAS) operation and why it is the foundation of lock-free algorithms
- **Implement** a CAS retry loop for lock-free increment
- **Build** a lock-free max tracker and clamped-add using CAS
- **Compare** CAS-based code with mutex-based code in terms of correctness and complexity

## Why Compare-And-Swap

`atomic.AddInt64` works for simple addition, but what if you need a more complex read-modify-write? For example, updating a maximum value, conditionally setting a flag, or implementing a custom accumulator.

Compare-And-Swap (CAS) is the universal atomic primitive. `CompareAndSwapInt64(&addr, old, new)` atomically checks if `*addr == old` and, if so, sets `*addr = new` and returns `true`. If `*addr != old`, it does nothing and returns `false`. The entire check-and-set is one indivisible operation.

CAS is the building block of all lock-free data structures. Mutexes themselves are typically built on top of CAS. Understanding CAS gives you the ability to build custom atomic operations for cases where `Add` is not enough.

The trade-off: CAS-based code is harder to reason about than mutex-based code. A CAS may fail, requiring a retry loop. Under high contention, many goroutines may repeatedly fail and retry, wasting CPU. For most applications, mutexes are simpler and fast enough. CAS shines in performance-critical, low-contention scenarios.

## Example 1 -- Lock-Free Increment with CAS

The CAS retry loop is the canonical pattern for building custom atomic operations:

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

func casIncrement(addr *int64) {
	for {
		old := atomic.LoadInt64(addr)      // 1. load current value
		next := old + 1                     // 2. compute new value
		if atomic.CompareAndSwapInt64(addr, old, next) {
			return // 3. CAS succeeded — we atomically changed old -> old+1
		}
		// 4. CAS failed — another goroutine changed the value.
		//    Loop back, reload, try again.
	}
}

func main() {
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
	fmt.Printf("Expected: 1000000\n")
	fmt.Printf("Got:      %d\n", counter)
}
```

### Verification
```bash
go run main.go
```
The result is exactly 1,000,000. Run with `-race` to confirm no data races:
```bash
go run -race main.go
```

## Example 2 -- Lock-Free Maximum Tracker

CAS enables operations that `AddInt64` cannot express. Here, we atomically update a maximum value -- only writing if the new value is larger than the current one:

```go
package main

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
)

func casUpdateMax(addr *int64, val int64) {
	for {
		old := atomic.LoadInt64(addr)
		if val <= old {
			return // nothing to update — current max is >= val
		}
		if atomic.CompareAndSwapInt64(addr, old, val) {
			return // successfully updated the max
		}
		// CAS failed: another goroutine updated concurrently. Retry.
	}
}

func main() {
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
	fmt.Printf("Maximum (100k random samples from 0-999999): %d\n",
		atomic.LoadInt64(&maxVal))
}
```

### Verification
```bash
go run -race main.go
```
Expected: a value close to 999,999. With 100,000 random samples from [0, 999999), the maximum will be very high. No race warnings.

## Example 3 -- Compare with Mutex Approach

The same max tracker using `sync.Mutex`. Structurally simpler but with locking overhead:

```go
package main

import (
	"fmt"
	"math/rand"
	"sync"
)

func main() {
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
	fmt.Printf("Maximum (mutex): %d\n", maxVal)
}
```

Both approaches are correct. The mutex version is arguably clearer: lock, check, update, unlock. The CAS version avoids locking overhead but introduces the retry loop. For a single int64, the performance difference is small. CAS wins when contention is very low; mutexes win when the critical section is longer or contention is high (blocked goroutines sleep instead of spinning).

### Verification
```bash
go run -race main.go
```
Both versions produce similar maximums. Both pass `-race`.

## Example 4 -- Clamped Add (Conditional Atomic Update)

A real-world CAS pattern: atomically add a delta but reject the operation if the result would exceed a ceiling. This is used in rate limiters, connection pool limits, and resource quotas.

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

func casClampedAdd(addr *int64, delta int64, ceiling int64) bool {
	for {
		old := atomic.LoadInt64(addr)
		next := old + delta
		if next > ceiling {
			return false // would exceed ceiling — reject
		}
		if atomic.CompareAndSwapInt64(addr, old, next) {
			return true // applied successfully
		}
		// CAS failed — another goroutine modified the value. Retry.
	}
}

func main() {
	var counter int64
	var wg sync.WaitGroup

	// 100 goroutines x 100 attempts = 10,000 potential adds
	// but ceiling is 1000, so most will be rejected
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
	result := atomic.LoadInt64(&counter)
	fmt.Printf("Counter (ceiling 1000): %d\n", result)
	if result <= 1000 {
		fmt.Println("PASS: counter did not exceed ceiling")
	} else {
		fmt.Println("FAIL: counter exceeded ceiling!")
	}
}
```

### Verification
```bash
go run -race main.go
```
Expected: counter is exactly 1000 (all 1000 slots claimed, remaining 9000 attempts rejected). No race warnings.

## Example 5 -- Lock-Free State Machine

CAS naturally enforces valid state transitions. A transition from state A to state B only succeeds if the current state is exactly A:

```go
package main

import (
	"fmt"
	"sync/atomic"
)

const (
	stateIdle     int64 = 0
	stateRunning  int64 = 1
	stateStopping int64 = 2
	stateStopped  int64 = 3
)

func transition(state *int64, from, to int64) bool {
	return atomic.CompareAndSwapInt64(state, from, to)
}

func main() {
	var state int64 // starts at stateIdle (0)

	// Valid transitions: idle -> running -> stopping -> stopped
	fmt.Println(transition(&state, stateIdle, stateRunning))     // true
	fmt.Println(transition(&state, stateRunning, stateStopping))  // true
	fmt.Println(transition(&state, stateStopping, stateStopped))  // true

	// Invalid: cannot go from stopped back to running
	fmt.Println(transition(&state, stateRunning, stateStopped))   // false
}
```

### Verification
```bash
go run main.go
```
Expected: `true`, `true`, `true`, `false`.

## Common Mistakes

### Forgetting to Reload in the CAS Loop

**Wrong:**
```go
package main

import (
	"sync/atomic"
)

func badIncrement(addr *int64) {
	old := atomic.LoadInt64(addr) // loaded once
	for {
		next := old + 1
		if atomic.CompareAndSwapInt64(addr, old, next) {
			return
		}
		// BUG: old is stale — we never reloaded it.
		// Every subsequent CAS compares against the wrong value.
		// This becomes an infinite loop under contention.
	}
}

func main() {
	var x int64
	badIncrement(&x) // works once, infinite-loops under contention
}
```

**Fix:** Reload `old` at the start of each loop iteration:
```go
for {
    old := atomic.LoadInt64(addr)
    if atomic.CompareAndSwapInt64(addr, old, old+1) {
        return
    }
}
```

### Using CAS Where Add Suffices

**Wrong (not broken, but wasteful):**
```go
func increment(addr *int64) {
    for {
        old := atomic.LoadInt64(addr)
        if atomic.CompareAndSwapInt64(addr, old, old+1) {
            return
        }
    }
}
```

This works but is `atomic.AddInt64` with extra steps. More code, more CAS retries under contention, same result.

**Fix:** Use `atomic.AddInt64(addr, 1)` for simple addition. Reserve CAS for operations that cannot be expressed as an add.

### Ignoring the ABA Problem

The ABA problem: a value changes from A to B and back to A. A CAS checking for A succeeds even though the value was modified. For simple counters and maximums this is harmless, but for pointer-based lock-free data structures it can cause corruption. Go's `sync/atomic` does not provide tagged pointers or double-word CAS. If you encounter ABA concerns, use a mutex.

## What's Next
Continue to [04-atomic-value-dynamic-config](../04-atomic-value-dynamic-config/04-atomic-value-dynamic-config.md) to learn how `atomic.Value` stores and loads arbitrary types -- enabling lock-free configuration hot-reload.

## Summary
- CAS (`CompareAndSwapInt64`) atomically checks and sets a value in one indivisible operation
- The CAS loop pattern: load, compute, CAS, retry on failure
- CAS enables custom atomic operations beyond simple add (max, min, conditional update, state transitions)
- Always reload the current value inside the retry loop after a failed CAS
- Use `atomic.AddInt64` when simple addition suffices; reserve CAS for complex operations
- Mutex-based code is simpler and often fast enough; CAS shines in low-contention scenarios

## Reference
- [atomic.CompareAndSwapInt64](https://pkg.go.dev/sync/atomic#CompareAndSwapInt64)
- [Lock-Free Programming (Wikipedia)](https://en.wikipedia.org/wiki/Non-blocking_algorithm)
- [ABA Problem (Wikipedia)](https://en.wikipedia.org/wiki/ABA_problem)
