# 5. Fix Race with Atomic

<!--
difficulty: intermediate
concepts: [sync/atomic, atomic.AddInt64, atomic.LoadInt64, lock-free, CAS]
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
- **Verify** the fix using the `-race` flag
- **Compare** the three approaches (mutex, channel, atomic) for a simple counter
- **Decide** when `sync/atomic` is the right choice

## Why Atomic Operations
For simple numeric operations (counters, flags, pointers), `sync/atomic` provides lock-free alternatives to mutexes. Atomic operations are implemented directly by the CPU using special instructions (like compare-and-swap) that guarantee the operation completes without interruption from other cores.

Compared to mutexes:
- Atomic operations are faster because they avoid the overhead of lock acquisition and release
- They cannot cause deadlocks (no locks to hold)
- They are limited to simple types: integers, pointers, and `atomic.Value` for arbitrary types

Compared to channels:
- Atomic operations have far less overhead (no goroutine scheduling, no channel buffer)
- They are appropriate only for simple state, not complex coordination

The rule of thumb: use `sync/atomic` for simple counters and flags. Use mutexes for protecting complex data structures. Use channels for communication between goroutines.

## Step 1 -- Use atomic.AddInt64

Edit `main.go` and implement `safeCounterAtomic`. Replace `counter++` with `atomic.AddInt64`:

```go
func safeCounterAtomic() int64 {
    var counter int64 // must be int64 for atomic operations
    var wg sync.WaitGroup

    for i := 0; i < 1000; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for j := 0; j < 1000; j++ {
                atomic.AddInt64(&counter, 1)
            }
        }()
    }

    wg.Wait()
    return atomic.LoadInt64(&counter)
}
```

Key details:
- The counter must be `int64` (or `int32`, `uint64`, etc.) -- atomic operations work on specific types
- `atomic.AddInt64(&counter, 1)` atomically adds 1 to counter
- `atomic.LoadInt64(&counter)` atomically reads the final value
- Pass the address of the counter (`&counter`), not the value

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Fix Race with Atomic ===
Racy counter:  <wrong number>
Safe (atomic): 1000000
```

## Step 2 -- Verify with the Race Detector

```bash
go run -race main.go
```

The race detector should report races only for `racyCounter`, NOT for `safeCounterAtomic`. Atomic operations are recognized as synchronization points by the race detector.

### Intermediate Verification
Confirm zero race warnings from `safeCounterAtomic`.

## Step 3 -- Compare All Three Approaches

Implement `compareAllApproaches` to benchmark mutex, channel, and atomic side by side:

```go
func compareAllApproaches() {
    fmt.Println("\n=== Comparison: Mutex vs Channel vs Atomic ===")

    start := time.Now()
    safeCounterMutex()
    mutexDuration := time.Since(start)

    start = time.Now()
    safeCounterChannel()
    channelDuration := time.Since(start)

    start = time.Now()
    safeCounterAtomic()
    atomicDuration := time.Since(start)

    fmt.Printf("Mutex:   %v\n", mutexDuration)
    fmt.Printf("Channel: %v\n", channelDuration)
    fmt.Printf("Atomic:  %v\n", atomicDuration)
    fmt.Println()
    fmt.Println("For simple counters, atomic is typically the fastest.")
    fmt.Println("Channels are the slowest due to goroutine scheduling overhead.")
    fmt.Println("Choose based on complexity, not just speed.")
}
```

### Intermediate Verification
```bash
go run main.go
```
You should see atomic as the fastest, mutex in the middle, and channel as the slowest for this specific problem.

## Step 4 -- Explore Other Atomic Operations

Review (do not need to implement) the other atomic operations available:

```go
// Store: atomically set a value
atomic.StoreInt64(&counter, 0)

// Load: atomically read a value
val := atomic.LoadInt64(&counter)

// Swap: atomically set a new value and return the old one
old := atomic.SwapInt64(&counter, 100)

// CompareAndSwap: set new value only if current equals expected
swapped := atomic.CompareAndSwapInt64(&counter, 42, 43) // if counter==42, set to 43
```

`CompareAndSwap` (CAS) is the fundamental building block of lock-free algorithms. All other atomic operations are built on top of it at the hardware level.

## Common Mistakes

### Using Regular Reads with Atomic Writes
**Wrong:**
```go
atomic.AddInt64(&counter, 1) // atomic write
fmt.Println(counter)          // non-atomic read -- DATA RACE
```
**Fix:** Always use `atomic.LoadInt64(&counter)` to read a value that is atomically written.

### Wrong Pointer Type
**Wrong:**
```go
counter := 0
atomic.AddInt64(&counter, 1) // compile error: counter is int, not int64
```
**Fix:** Declare the variable with the correct type: `var counter int64`.

### Using Atomic for Complex State
**Wrong:**
```go
// Two related values that must be updated together
atomic.AddInt64(&total, amount)
atomic.AddInt64(&count, 1)
// BUG: another goroutine can read total and count between these two operations
```
**Fix:** Use a mutex to protect multi-variable updates, or redesign to use a single atomic value.

### Thinking Atomic Operations Compose
Each atomic operation is individually atomic, but a sequence of atomic operations is NOT atomic as a whole. If you need to update multiple variables consistently, use a mutex.

## Verify What You Learned

1. Run `go run -race main.go` and confirm zero race warnings for the atomic version
2. Why must the counter be `int64` and not `int`?
3. When would you choose `sync/atomic` over `sync.Mutex`?
4. Why is `counter = atomic.LoadInt64(&counter) + 1` NOT equivalent to `atomic.AddInt64(&counter, 1)`?

## What's Next
Continue to [06-subtle-race-map-access](../06-subtle-race-map-access/06-subtle-race-map-access.md) to explore a different kind of race: concurrent map access that causes a fatal crash.

## Summary
- `sync/atomic` provides lock-free operations for simple types (integers, pointers)
- `atomic.AddInt64(&counter, 1)` is the atomic equivalent of `counter++`
- Always use `atomic.LoadInt64` to read atomically written values
- Atomic operations are faster than mutexes for simple counters
- Atomic operations do NOT compose: a sequence of atomic operations is not atomic as a whole
- Choose atomic for simple counters/flags, mutex for complex state, channels for communication
- The five exercises (01-05) show the same counter problem solved four different ways: racy, mutex, channel, atomic

## Reference
- [sync/atomic Package](https://pkg.go.dev/sync/atomic)
- [Go Memory Model: Synchronization](https://go.dev/ref/mem#synchronization)
- [Go Blog: Introducing the Go Race Detector](https://go.dev/blog/race-detector)
