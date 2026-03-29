# 5. Fix Race with Atomic

<!--
difficulty: intermediate
concepts: [sync/atomic, atomic.AddInt64, atomic.LoadInt64, lock-free, CAS, comparison]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [goroutines, sync.WaitGroup, data race concept, race detector, mutex, channels]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01-04 (data races, race detector, mutex fix, channel fix)
- Basic understanding that some CPU operations can be atomic

## Learning Objectives
After completing this exercise, you will be able to:
- **Fix** a data race using `sync/atomic` operations
- **Choose** the appropriate atomic function for different operations
- **Compare** the three approaches (mutex, channel, atomic) for the same counter problem
- **Decide** when `sync/atomic` is the right choice vs mutex vs channel

## Why Atomic Operations

For simple numeric operations (counters, flags, pointers), `sync/atomic` provides **lock-free** alternatives to mutexes. Atomic operations are implemented directly by the CPU using special instructions (like `LOCK XADD` on x86) that guarantee the operation completes without interruption from other cores.

Compared to mutexes:
- **Faster**: no lock acquisition/release overhead
- **No deadlocks**: no locks to hold
- **Limited**: only works for simple types (integers, pointers, `atomic.Value`)

Compared to channels:
- **Far less overhead**: no goroutine scheduling, no channel buffer
- **Only for simple state**: not for complex coordination

**Rule of thumb**: use `sync/atomic` for simple counters and flags. Use mutexes for protecting complex data structures. Use channels for communication between goroutines.

## Step 1 -- Use atomic.AddInt64

Replace `counter++` with `atomic.AddInt64`:

```go
package main

import (
    "fmt"
    "sync"
    "sync/atomic"
)

func safeCounterAtomic() int64 {
    var counter int64 // must be int64 for atomic operations
    var wg sync.WaitGroup

    for i := 0; i < 1000; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for j := 0; j < 1000; j++ {
                // AddInt64 atomically adds 1 to counter.
                // The entire read-modify-write happens as a single CPU instruction.
                atomic.AddInt64(&counter, 1)
            }
        }()
    }

    wg.Wait()
    // LoadInt64 atomically reads the value.
    return atomic.LoadInt64(&counter)
}

func main() {
    result := safeCounterAtomic()
    fmt.Printf("Result: %d (expected 1000000)\n", result)
}
```

Key details:
- The counter must be `int64` (or `int32`, `uint64`, etc.) -- atomic operations work on specific types
- `atomic.AddInt64(&counter, 1)` atomically adds 1 to counter
- `atomic.LoadInt64(&counter)` atomically reads the final value
- Pass the **address** of the counter (`&counter`), not the value

### Verification
```bash
go run -race main.go
```
Expected: 1,000,000 with zero race warnings from `safeCounterAtomic`.

## Step 2 -- Explore Other Atomic Operations

The `main.go` demonstrates the full range of atomic operations:

```go
package main

import (
    "fmt"
    "sync/atomic"
)

func main() {
    var counter int64

    // Store: atomically set a value.
    atomic.StoreInt64(&counter, 42)
    fmt.Println(atomic.LoadInt64(&counter)) // 42

    // Swap: atomically set new value and return the old one.
    old := atomic.SwapInt64(&counter, 99)
    fmt.Printf("old = %d, new = %d\n", old, atomic.LoadInt64(&counter))

    // CompareAndSwap (CAS): set new value ONLY if current equals expected.
    // This is the fundamental building block of lock-free algorithms.
    swapped := atomic.CompareAndSwapInt64(&counter, 99, 200)
    fmt.Printf("CAS 99 -> 200: %v\n", swapped) // true

    // CAS fails if current value does not match expected.
    swapped = atomic.CompareAndSwapInt64(&counter, 99, 300)
    fmt.Printf("CAS 99 -> 300: %v\n", swapped) // false (current is 200, not 99)
}
```

### Verification
```bash
go run main.go
```
Expected output includes atomic operation results with correct values.

## Step 3 -- Grand Comparison of All Three Approaches

The `main.go` times mutex, channel, and atomic side by side on the same counter problem (1000 goroutines x 1000 increments):

### Verification
```bash
go run main.go
```
Sample output:
```
=== Grand Comparison: All Four Approaches ===
  Mutex:       1000000 in 248.3ms
  Channel:     1000000 in 1.82s
  Atomic:      1000000 in 45.1ms
```

Typical ordering: **atomic < mutex < channel** for simple counter operations.

| Approach | Speed | Complexity | Best For |
|----------|-------|------------|----------|
| `atomic` | Fastest | Simple types only | Counters, flags, single values |
| `mutex` | Medium | Any type | Complex structs, multi-field updates |
| `channel` | Slowest | Communication | Ownership transfer, pipelines |

## Common Mistakes

### Using Regular Reads with Atomic Writes
```go
atomic.AddInt64(&counter, 1) // atomic write
fmt.Println(counter)          // non-atomic read -- DATA RACE!
```
**Fix:** Always use `atomic.LoadInt64(&counter)` to read a value that is atomically written.

### Wrong Pointer Type
```go
counter := 0
atomic.AddInt64(&counter, 1) // COMPILE ERROR: counter is int, not int64
```
**Fix:** Declare the variable with the correct type: `var counter int64`.

### Using Atomic for Complex State
```go
// Two related values that must be updated together.
atomic.AddInt64(&total, amount)
atomic.AddInt64(&count, 1)
// BUG: another goroutine can read total and count between these two operations
```
**Fix:** Use a mutex to protect multi-variable updates, or redesign to use a single atomic value.

### Thinking Atomic Operations Compose
Each atomic operation is individually atomic, but a **sequence** of atomic operations is NOT atomic as a whole:

```go
// NOT ATOMIC as a sequence:
val := atomic.LoadInt64(&counter) // step 1: read
val++                              // step 2: compute
atomic.StoreInt64(&counter, val)   // step 3: write
// Another goroutine can modify counter between steps 1 and 3!

// USE THIS INSTEAD:
atomic.AddInt64(&counter, 1) // single atomic operation
```

## Decision Guide

Use this flowchart to choose:

1. **Is the shared state a single integer or pointer?** -> `sync/atomic`
2. **Is the shared state a complex struct or multi-field update?** -> `sync.Mutex` (or `sync.RWMutex` for read-heavy)
3. **Do you need to transfer ownership or coordinate stages?** -> channels
4. **Is the shared state a map?** -> `sync.Mutex` wrapping a regular map, or `sync.Map` for specific patterns (see exercise 06)

## Verify What You Learned

```bash
go run -race main.go
```

1. Confirm zero race warnings for the atomic version
2. Why must the counter be `int64` and not `int`?
3. When would you choose `sync/atomic` over `sync.Mutex`?
4. Why is `counter = atomic.LoadInt64(&counter) + 1` NOT equivalent to `atomic.AddInt64(&counter, 1)`?

## What's Next
Continue to [06-subtle-race-map-access](../06-subtle-race-map-access/06-subtle-race-map-access.md) to explore a different kind of race: concurrent map access that causes a fatal crash.

## Summary
- `sync/atomic` provides lock-free operations for simple types (integers, pointers)
- `atomic.AddInt64(&counter, 1)` is the atomic equivalent of `counter++`
- Always use `atomic.LoadInt64` to read atomically written values
- Atomic operations are fastest for simple counters (no lock, no channel overhead)
- Atomic operations do NOT compose: a sequence of atomic operations is not atomic as a whole
- **Decision**: atomic for simple counters/flags, mutex for complex state, channels for communication
- Exercises 01-05 demonstrate the same counter problem (1000 goroutines x 1000 increments) solved four ways: racy, mutex, channel, atomic

## Reference
- [sync/atomic Package](https://pkg.go.dev/sync/atomic)
- [Go Memory Model: Synchronization](https://go.dev/ref/mem#synchronization)
- [Go Blog: Introducing the Go Race Detector](https://go.dev/blog/race-detector)
