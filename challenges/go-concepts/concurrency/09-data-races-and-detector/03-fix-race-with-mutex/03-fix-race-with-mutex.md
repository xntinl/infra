# 3. Fix Race with Mutex

<!--
difficulty: intermediate
concepts: [sync.Mutex, Lock, Unlock, defer, critical section, contention, encapsulation]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [goroutines, sync.WaitGroup, data race concept, race detector]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01 and 02 (understanding data races and the race detector)
- Basic familiarity with `sync.Mutex`

## Learning Objectives
After completing this exercise, you will be able to:
- **Fix** a data race by protecting shared state with `sync.Mutex`
- **Apply** the `Lock()`/`defer Unlock()` idiom correctly
- **Encapsulate** locking inside a struct for production-quality code
- **Measure** the contention cost of mutex-based synchronization
- **Verify** the fix using the `-race` flag

## Why Mutex

A `sync.Mutex` provides **mutual exclusion**: only one goroutine can hold the lock at a time. All others block until the lock is released. This is the most straightforward way to protect shared state.

How it works:
- `Lock()`: acquire the lock. If another goroutine holds it, block until it releases.
- `Unlock()`: release the lock. The next waiting goroutine can now proceed.

The tradeoff is **contention**: when many goroutines compete for the same lock, they serialize their access, reducing parallelism. For a simple counter this is acceptable. For high-throughput scenarios with mostly reads, consider `sync.RWMutex`. For simple numeric operations, consider `sync/atomic` (exercise 05).

This exercise uses the same counter problem from exercises 01-02.

## Step 1 -- Basic Mutex

The simplest fix wraps `counter++` in `Lock()`/`Unlock()`:

```go
package main

import (
    "fmt"
    "sync"
)

func safeCounterMutex() int {
    counter := 0
    var mu sync.Mutex
    var wg sync.WaitGroup

    for i := 0; i < 1000; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for j := 0; j < 1000; j++ {
                mu.Lock()
                counter++ // only one goroutine at a time reaches this line
                mu.Unlock()
            }
        }()
    }

    wg.Wait()
    return counter
}

func main() {
    result := safeCounterMutex()
    fmt.Printf("Result: %d (expected 1000000)\n", result)
}
```

### Verification
```bash
go run main.go
```
Expected:
```
Result: 1000000 (expected 1000000) -- CORRECT
```

```bash
go run -race main.go
```
Expected: NO `DATA RACE` warning from `safeCounterMutex`. The mutex establishes a happens-before relationship: each `Unlock()` happens-before the next `Lock()`.

## Step 2 -- Defer Pattern

The `defer` pattern ensures the mutex is always released, even if a panic occurs inside the critical section:

```go
package main

import (
    "fmt"
    "sync"
)

func safeCounterDefer() int {
    counter := 0
    var mu sync.Mutex
    var wg sync.WaitGroup

    // Extract the critical section into a named closure.
    // This makes the locking scope explicit and limits it to the minimum.
    increment := func() {
        mu.Lock()
        defer mu.Unlock() // guaranteed to execute even on panic
        counter++
    }

    for i := 0; i < 1000; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for j := 0; j < 1000; j++ {
                increment()
            }
        }()
    }

    wg.Wait()
    return counter
}

func main() {
    result := safeCounterDefer()
    fmt.Printf("Result: %d (expected 1000000)\n", result)
}
```

### Verification
```bash
go run -race main.go
```
Expected: 1,000,000 with zero race warnings.

## Step 3 -- Encapsulated Counter

In production code, the mutex should be an implementation detail, not something callers must remember to use:

```go
package main

import (
    "fmt"
    "sync"
)

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

func main() {
    c := &SafeCounter{}
    var wg sync.WaitGroup

    for i := 0; i < 1000; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for j := 0; j < 1000; j++ {
                c.Increment()
            }
        }()
    }

    wg.Wait()
    fmt.Printf("Result: %d (expected 1000000)\n", c.Value())
}
```

### Verification
```bash
go run -race main.go
```
Expected: 1,000,000 with zero race warnings.

This pattern prevents forgetting to lock because callers never access `value` directly.

## Step 4 -- Measure Contention Cost

The full `main.go` includes a timing comparison. The mutex version is slower because goroutines must wait for each other:

### Verification
```bash
go run main.go
```
Sample output:
```
=== Timing Comparison ===
  Racy (wrong):        12.3ms
  Mutex (basic):       245.6ms
  Mutex (defer):       251.2ms
  Slowdown: ~20x (the cost of correctness under high contention)
```

This is the worst case: 1000 goroutines competing for a single lock on a single integer. In real code, contention is usually lower because:
- Goroutines do useful work between lock acquisitions
- Lock scope is narrow
- Different goroutines lock different resources

## Common Mistakes

### Forgetting to Unlock
```go
mu.Lock()
counter++
// forgot mu.Unlock() -- all other goroutines are now blocked forever (deadlock)
```
**Fix:** Always use `defer mu.Unlock()` immediately after `Lock()`.

### Locking Too Much
```go
mu.Lock()
for j := 0; j < 1000; j++ {
    counter++
}
mu.Unlock()
```
This locks the entire loop, eliminating all parallelism. Each goroutine holds the lock for 1000 iterations.

**Better:** Lock only the specific operation that needs protection:
```go
for j := 0; j < 1000; j++ {
    mu.Lock()
    counter++
    mu.Unlock()
}
```

### Copying a Mutex
```go
var mu sync.Mutex
mu2 := mu // BUG: mu2 is a copy, not the same mutex
```
Never copy a `sync.Mutex` after first use. Pass mutexes by pointer, or embed them in a struct.

### Double-Locking from the Same Goroutine
```go
mu.Lock()
// ... some code that calls another function ...
mu.Lock() // DEADLOCK: same goroutine already holds the lock
```
`sync.Mutex` is NOT reentrant. Calling `Lock()` twice from the same goroutine without an `Unlock()` in between causes a deadlock.

## Verify What You Learned

```bash
go run -race main.go
```

1. Confirm zero race warnings for all mutex-protected functions
2. What happens if you call `Lock()` twice from the same goroutine without `Unlock()`?
3. Why is `defer mu.Unlock()` preferred over calling `mu.Unlock()` explicitly?
4. What is the tradeoff of using a mutex for this counter problem?

## What's Next
Continue to [04-fix-race-with-channel](../04-fix-race-with-channel/04-fix-race-with-channel.md) to fix the same race using channels instead of a mutex.

## Summary
- `sync.Mutex` provides mutual exclusion: only one goroutine enters the critical section at a time
- Always pair `Lock()` with `Unlock()`; prefer `defer mu.Unlock()` for safety
- Extract critical sections into small functions to make the locking scope explicit
- Encapsulate the mutex inside a struct to prevent callers from forgetting to lock
- The mutex establishes happens-before relationships that satisfy the race detector
- Tradeoff: mutexes add contention, reducing parallelism, but guarantee correctness
- Verify with `go run -race main.go` to confirm the race is eliminated

## Reference
- [sync.Mutex Documentation](https://pkg.go.dev/sync#Mutex)
- [Go Blog: Share Memory by Communicating](https://go.dev/blog/codelab-share)
- [Effective Go: Sharing by Communicating](https://go.dev/doc/effective_go#sharing)
