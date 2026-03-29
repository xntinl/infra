# 3. Atomic Compare-And-Swap

<!--
difficulty: intermediate
concepts: [CompareAndSwapInt64, CAS loop, optimistic concurrency, lock-free increment]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [goroutines, sync.WaitGroup, atomic.LoadInt64, atomic.StoreInt64]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01 and 02 (atomic add, atomic load/store)
- Understanding of why concurrent modification requires synchronization

## Learning Objectives
After completing this exercise, you will be able to:
- **Explain** the compare-and-swap (CAS) operation and why it is the foundation of lock-free algorithms
- **Implement** a CAS retry loop for lock-free increment
- **Build** a simple lock-free max tracker using CAS
- **Compare** CAS-based code with mutex-based code in terms of correctness and complexity

## Why Compare-And-Swap
`atomic.AddInt64` works for simple addition, but what if you need a more complex read-modify-write? For example, updating a maximum value, conditionally setting a flag, or implementing a custom accumulator.

Compare-And-Swap (CAS) is the universal atomic primitive. `CompareAndSwapInt64(&addr, old, new)` atomically checks if `*addr == old` and, if so, sets `*addr = new` and returns `true`. If `*addr != old`, it does nothing and returns `false`. The entire check-and-set is one indivisible operation.

CAS is the building block of all lock-free data structures. Mutexes themselves are typically built on top of CAS. Understanding CAS gives you the ability to build custom atomic operations for cases where `Add` is not enough.

The trade-off: CAS-based code is harder to reason about than mutex-based code. A CAS may fail, requiring a retry loop. Under high contention, many goroutines may repeatedly fail and retry, wasting CPU. For most applications, mutexes are simpler and fast enough. CAS shines in performance-critical, low-contention scenarios.

## Step 1 -- Lock-Free Increment with CAS

Implement `casIncrement` using a CAS retry loop. This does the same thing as `atomic.AddInt64` but manually:

```go
func casIncrement(addr *int64) {
    for {
        old := atomic.LoadInt64(addr)
        new := old + 1
        if atomic.CompareAndSwapInt64(addr, old, new) {
            return // success: we incremented from old to new
        }
        // CAS failed: another goroutine changed the value.
        // Loop back, reload, and try again.
    }
}
```

The pattern: (1) load current value, (2) compute new value, (3) CAS to apply it, (4) if failed, retry. This is the canonical CAS loop.

Use it from 1000 goroutines, each calling `casIncrement` 1000 times on a shared counter.

### Intermediate Verification
```bash
go run main.go
```
The result is exactly 1,000,000. Run with `-race` to confirm no data races.

## Step 2 -- Lock-Free Maximum Tracker

Implement `casUpdateMax` that atomically updates a shared maximum value:

```go
func casUpdateMax(addr *int64, val int64) {
    for {
        old := atomic.LoadInt64(addr)
        if val <= old {
            return // nothing to update
        }
        if atomic.CompareAndSwapInt64(addr, old, val) {
            return // successfully updated the max
        }
        // CAS failed: retry
    }
}
```

Launch 100 goroutines, each generating random values and calling `casUpdateMax`. The final value should be the true maximum across all goroutines.

```go
func trackMax() int64 {
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
```

### Intermediate Verification
```bash
go run main.go
```
The printed maximum should be close to 999,999 (with 100,000 random samples from 0-999,999). Run with `-race` to confirm safety.

## Step 3 -- Compare with Mutex Approach

Implement the same maximum tracker using a `sync.Mutex` to see the structural difference:

```go
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
```

Both approaches are correct. The mutex version is arguably clearer: lock, check, update, unlock. The CAS version avoids the overhead of locking but introduces the retry loop. For a single int64, the performance difference is small. CAS wins when contention is very low; mutexes win when the critical section is longer or contention is high (because blocked goroutines sleep instead of spinning).

### Intermediate Verification
```bash
go run main.go
```
Both versions produce similar maximums. Both pass `-race`.

## Common Mistakes

### Forgetting to Reload in the CAS Loop
**Wrong:**
```go
old := atomic.LoadInt64(addr)
for {
    new := old + 1
    if atomic.CompareAndSwapInt64(addr, old, new) {
        return
    }
    // BUG: old is stale! We never reloaded it.
}
```
**What happens:** After a failed CAS, `old` still holds the previous value. The next CAS will also fail because `old` is wrong. This becomes an infinite loop.

**Fix:** Reload `old` at the start of each loop iteration.

### Using CAS Where Add Suffices
**Wrong:**
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
**What happens:** It works, but it is `atomic.AddInt64` with extra steps. More code, same result.

**Fix:** Use `atomic.AddInt64(addr, 1)` for simple addition. Reserve CAS for operations that cannot be expressed as an add.

### Ignoring the ABA Problem
The ABA problem occurs when a value changes from A to B and back to A. A CAS checking for A succeeds even though the value was modified. For simple counters and maximums this is harmless, but for pointer-based lock-free data structures it can cause corruption. Go's `sync/atomic` does not provide tagged pointers or double-word CAS to solve ABA. If you encounter ABA concerns, use a mutex.

## Verify What You Learned

Implement `casClampedAdd` that atomically adds a delta to a counter but clamps the result to a maximum ceiling:

```go
func casClampedAdd(addr *int64, delta int64, ceiling int64) bool
```

The function returns `true` if the add was applied, `false` if it was skipped because the result would exceed the ceiling. Launch goroutines that attempt to add to a counter with a ceiling of 1000 and verify the counter never exceeds 1000.

## What's Next
Continue to [04-atomic-value-dynamic-config](../04-atomic-value-dynamic-config/04-atomic-value-dynamic-config.md) to learn how `atomic.Value` stores and loads arbitrary types -- enabling lock-free configuration hot-reload.

## Summary
- CAS (`CompareAndSwapInt64`) atomically checks and sets a value in one indivisible operation
- The CAS loop pattern: load, compute, CAS, retry on failure
- CAS enables custom atomic operations beyond simple add (max, min, conditional update)
- Always reload the current value inside the retry loop after a failed CAS
- Use `atomic.AddInt64` when simple addition suffices; reserve CAS for complex operations
- Mutex-based code is simpler and often fast enough; CAS shines in low-contention scenarios

## Reference
- [atomic.CompareAndSwapInt64](https://pkg.go.dev/sync/atomic#CompareAndSwapInt64)
- [Lock-Free Programming (Wikipedia)](https://en.wikipedia.org/wiki/Non-blocking_algorithm)
- [ABA Problem (Wikipedia)](https://en.wikipedia.org/wiki/ABA_problem)
