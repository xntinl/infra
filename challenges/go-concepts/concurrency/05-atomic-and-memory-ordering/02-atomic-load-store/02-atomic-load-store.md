# 2. Atomic Load and Store

<!--
difficulty: intermediate
concepts: [atomic.LoadInt64, atomic.StoreInt64, visibility, publish pattern, memory visibility]
tools: [go]
estimated_time: 25m
bloom_level: understand
prerequisites: [goroutines, sync.WaitGroup, atomic.AddInt64]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercise 01 (atomic add counter)
- Understanding of why non-atomic access to shared variables is unsafe

## Learning Objectives
After completing this exercise, you will be able to:
- **Explain** why reading a shared variable without atomic access is a data race
- **Use** `atomic.LoadInt64` and `atomic.StoreInt64` to safely read and write shared state
- **Implement** the publish pattern: prepare data, then atomically signal readiness
- **Distinguish** between atomicity (indivisible operation) and visibility (seeing the latest value)

## Why Atomic Load and Store
`atomic.AddInt64` handles read-modify-write. But many concurrent patterns only need to read or write a shared value -- not both in one step. A producer goroutine prepares data and then publishes a "ready" flag. Consumer goroutines check that flag to know when data is available.

Without atomic operations, a regular read of a shared variable is a data race, even if the writer finished "before" the reader started. The Go memory model does not guarantee that a write to a plain variable in one goroutine is visible to a read in another goroutine. The compiler and CPU may reorder operations, cache values in registers, or delay writes to main memory.

`atomic.StoreInt64` and `atomic.LoadInt64` establish visibility guarantees. When a goroutine stores a value atomically, any goroutine that later loads it atomically is guaranteed to see that value (or a more recent one). This is the foundation of safe flag-based coordination.

## Step 1 -- Observe the Visibility Problem

Implement `unsafeFlag` where a writer goroutine sets a flag variable to `1`, and a reader goroutine spins until it sees the flag change:

```go
func unsafeFlag() {
    var flag int64
    var data int64

    go func() {
        data = 42       // prepare data
        flag = 1        // signal ready (non-atomic)
    }()

    for flag == 0 {     // busy-wait (non-atomic read)
        runtime.Gosched()
    }

    fmt.Printf("  Data: %d (expected 42)\n", data)
}
```

This code has two data races: `flag` and `data` are accessed concurrently without synchronization. The race detector will report this. On some architectures, the reader may spin forever because the flag write is never visible, or it may see `flag == 1` but `data == 0` because writes were reordered.

### Intermediate Verification
```bash
go run -race main.go
```
The race detector will report at least one `DATA RACE`. The program may work "by accident" on x86 but is fundamentally broken.

## Step 2 -- Fix with Atomic Load and Store

Implement `atomicFlag` using `atomic.StoreInt64` and `atomic.LoadInt64`:

```go
func atomicFlag() {
    var flag int64
    var data int64

    go func() {
        data = 42
        atomic.StoreInt64(&flag, 1) // publish: data is ready
    }()

    for atomic.LoadInt64(&flag) == 0 {
        runtime.Gosched()
    }

    fmt.Printf("  Data: %d (expected 42)\n", data)
}
```

The atomic store of `flag` acts as a publication barrier. Any goroutine that atomically loads `flag` and sees `1` is guaranteed to also see the write to `data` that happened before the store. This is a happens-before relationship.

### Intermediate Verification
```bash
go run -race main.go
```
No race warnings. The value of `data` is reliably 42.

## Step 3 -- Typed Wrapper and the Published Config Pattern

Use `atomic.Int64` and `atomic.Bool` for a more realistic scenario: a configuration value that is published once and read many times.

```go
func publishedConfig() {
    var ready atomic.Bool
    var configValue atomic.Int64
    var wg sync.WaitGroup

    // Writer: prepare and publish config
    wg.Add(1)
    go func() {
        defer wg.Done()
        time.Sleep(10 * time.Millisecond) // simulate config loading
        configValue.Store(9090)
        ready.Store(true) // publish
        fmt.Println("  Config published: port=9090")
    }()

    // Readers: wait for config, then use it
    for i := 0; i < 5; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            for !ready.Load() {
                runtime.Gosched()
            }
            port := configValue.Load()
            fmt.Printf("  Reader %d: using port %d\n", id, port)
        }(i)
    }

    wg.Wait()
}
```

### Intermediate Verification
```bash
go run -race main.go
```
All five readers see port 9090. No race conditions.

## Common Mistakes

### Reading Without Atomic After Writing With Atomic
**Wrong:**
```go
atomic.StoreInt64(&flag, 1)
// ... in another goroutine:
if flag == 1 { // non-atomic read -- data race!
```
**What happens:** The read is not synchronized. The race detector will flag it, and on weakly-ordered architectures the read may return stale data.

**Fix:** Always pair `atomic.StoreInt64` with `atomic.LoadInt64`. If any goroutine uses atomic access, all goroutines must use atomic access for that variable.

### Assuming Atomic Store Orders Other Writes
**Wrong (subtle):**
```go
go func() {
    dataA = 1
    dataB = 2
    atomic.StoreInt64(&flag, 1)
}()

for atomic.LoadInt64(&flag) == 0 {}
// Can I safely read dataA and dataB?
```
**What happens:** In Go, the atomic store of `flag` does synchronize with the atomic load. Writes that happen-before the store are visible after the load. However, this relies on the Go memory model's guarantees for `sync/atomic`, not on CPU-level memory barriers that you control directly. Do not rely on this pattern for complex multi-variable synchronization -- use a mutex or channel instead.

### Busy-Waiting Without Yielding
**Wrong:**
```go
for atomic.LoadInt64(&flag) == 0 {} // tight loop, burns CPU
```
**Fix:** Add `runtime.Gosched()` to yield the processor, or better yet, use a channel or condition variable for waiting. Busy-waiting with atomics is acceptable only in very low-latency, performance-critical code.

## Verify What You Learned

Implement `multiStagePublish` with two stages:
1. Stage 1: a goroutine prepares `partA` and atomically signals `stageOneReady`
2. Stage 2: another goroutine waits for stage one, prepares `partB`, and signals `stageTwoReady`
3. A reader goroutine waits for stage two and prints both `partA` and `partB`

Use only `atomic.Int64` and `atomic.Bool` for synchronization. Run with `-race` to confirm correctness.

## What's Next
Continue to [03-atomic-compare-and-swap](../03-atomic-compare-and-swap/03-atomic-compare-and-swap.md) to learn the CAS operation -- the foundation of lock-free algorithms.

## Summary
- Regular reads and writes to shared variables are data races, even if timing "should" make them safe
- `atomic.StoreInt64` and `atomic.LoadInt64` provide visibility guarantees across goroutines
- The publish pattern: prepare data, then atomically store a flag; readers atomically load the flag before accessing data
- `atomic.Bool` and `atomic.Int64` (Go 1.19+) are cleaner than the function-based API
- Busy-waiting on atomic loads works but wastes CPU; prefer channels or condition variables for general waiting

## Reference
- [sync/atomic package](https://pkg.go.dev/sync/atomic)
- [Go Memory Model](https://go.dev/ref/mem)
- [atomic.Bool type](https://pkg.go.dev/sync/atomic#Bool)
