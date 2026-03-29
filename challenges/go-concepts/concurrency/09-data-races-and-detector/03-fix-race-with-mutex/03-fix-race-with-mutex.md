# 3. Fix Race with Mutex

<!--
difficulty: intermediate
concepts: [sync.Mutex, Lock, Unlock, defer, critical section, contention]
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
- **Verify** the fix using the `-race` flag
- **Identify** the tradeoff between correctness and contention when using mutexes

## Why Mutex
A `sync.Mutex` provides mutual exclusion: only one goroutine can hold the lock at a time. All others block until the lock is released. This is the most straightforward way to protect shared state. When a goroutine calls `Lock()`, it gains exclusive access to the critical section. When it calls `Unlock()`, the next waiting goroutine can proceed.

The mutex approach is simple and works for any type of shared state. The tradeoff is contention: when many goroutines compete for the same lock, they serialize their access, reducing parallelism. For a simple counter this is acceptable. For high-throughput scenarios with mostly reads, consider `sync.RWMutex`. For simple numeric operations, consider `sync/atomic` (exercise 05).

This exercise uses the same counter problem from exercises 01 and 02. You will fix the race by wrapping the increment in a mutex-protected critical section.

## Step 1 -- Add a Mutex to the Racy Counter

Edit `main.go` and implement `safeCounterMutex`. Protect the `counter++` operation with a `sync.Mutex`:

```go
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
                counter++
                mu.Unlock()
            }
        }()
    }

    wg.Wait()
    return counter
}
```

The `mu.Lock()` call ensures only one goroutine executes `counter++` at a time. No two goroutines can read-modify-write simultaneously.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Fix Race with Mutex ===
Racy counter:  <some wrong number>
Safe counter:  1000000
```

## Step 2 -- Verify with the Race Detector

Run with the `-race` flag to confirm the fix eliminates the race:

```bash
go run -race main.go
```

The race detector should report a race for `racyCounter` but NOT for `safeCounterMutex`. The mutex establishes a happens-before relationship: each `Unlock()` happens-before the next `Lock()`, so the read-modify-write sequence is properly ordered.

### Intermediate Verification
```bash
go run -race main.go 2>&1 | grep -c "DATA RACE"
```
You should see race reports only from `racyCounter`, not from `safeCounterMutex`.

## Step 3 -- Use the defer Pattern

Implement `safeCounterDefer` using the idiomatic `defer` pattern. This ensures the mutex is always released, even if a panic occurs inside the critical section:

```go
func safeCounterDefer() int {
    counter := 0
    var mu sync.Mutex
    var wg sync.WaitGroup

    increment := func() {
        mu.Lock()
        defer mu.Unlock()
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
```

Extracting the critical section into a named function makes the locking scope explicit and limits the critical section to the minimum necessary code.

### Intermediate Verification
```bash
go run -race main.go
```
Both `safeCounterMutex` and `safeCounterDefer` should produce 1,000,000 with no race warnings.

## Step 4 -- Observe Contention Cost

Implement `compareTiming` to measure the performance difference between the racy (unsynchronized) and safe (mutex-protected) versions:

```go
func compareTiming() {
    fmt.Println("\n=== Timing Comparison ===")

    start := time.Now()
    racyCounter()
    racyDuration := time.Since(start)

    start = time.Now()
    safeCounterMutex()
    safeDuration := time.Since(start)

    fmt.Printf("Racy (wrong but fast):   %v\n", racyDuration)
    fmt.Printf("Mutex (correct but slower): %v\n", safeDuration)
    fmt.Printf("Slowdown factor: %.1fx\n", float64(safeDuration)/float64(racyDuration))
}
```

The mutex version will be slower because goroutines must wait for each other. This is the cost of correctness. The slowdown factor depends on your hardware and number of CPU cores.

### Intermediate Verification
```bash
go run main.go
```
You should see the mutex version taking noticeably longer than the racy version.

## Common Mistakes

### Forgetting to Unlock
**Wrong:**
```go
mu.Lock()
counter++
// forgot mu.Unlock() -- all other goroutines are now blocked forever (deadlock)
```
**Fix:** Always use `defer mu.Unlock()` immediately after `Lock()`, or extract the critical section into a small function with `defer`.

### Locking Too Much
**Wrong:**
```go
mu.Lock()
for j := 0; j < 1000; j++ {
    counter++
}
mu.Unlock()
```
This locks the entire loop, eliminating all parallelism. Each goroutine holds the lock for 1000 iterations. Other goroutines cannot make progress until the lock is released.

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

## Verify What You Learned

1. Run `go run -race main.go` and confirm zero race warnings for the mutex-protected functions
2. What happens if you call `Lock()` twice from the same goroutine without `Unlock()`?
3. Why is `defer mu.Unlock()` preferred over calling `mu.Unlock()` explicitly?
4. What is the tradeoff of using a mutex for this counter problem?

## What's Next
Continue to [04-fix-race-with-channel](../04-fix-race-with-channel/04-fix-race-with-channel.md) to fix the same race using channels instead of a mutex.

## Summary
- `sync.Mutex` provides mutual exclusion: only one goroutine enters the critical section at a time
- Always pair `Lock()` with `Unlock()`; prefer `defer mu.Unlock()` for safety
- Extract critical sections into small functions to make the locking scope explicit
- The mutex establishes happens-before relationships that satisfy the race detector
- Tradeoff: mutexes add contention, reducing parallelism, but guarantee correctness
- Verify with `go run -race main.go` to confirm the race is eliminated

## Reference
- [sync.Mutex Documentation](https://pkg.go.dev/sync#Mutex)
- [Go Blog: Share Memory by Communicating](https://go.dev/blog/codelab-share)
- [Effective Go: Sharing by Communicating](https://go.dev/doc/effective_go#sharing)
