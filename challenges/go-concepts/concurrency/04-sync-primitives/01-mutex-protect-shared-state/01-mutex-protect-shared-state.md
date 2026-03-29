# 1. Mutex: Protect Shared State

<!--
difficulty: basic
concepts: [sync.Mutex, Lock, Unlock, defer, race condition, critical section]
tools: [go]
estimated_time: 15m
bloom_level: apply
prerequisites: [goroutines, go keyword, WaitGroup basics]
-->

## Prerequisites
- Go 1.22+ installed
- Ability to launch goroutines with the `go` keyword
- Understanding of shared memory between goroutines

## Learning Objectives
After completing this exercise, you will be able to:
- **Identify** race conditions caused by unsynchronized access to shared state
- **Protect** shared variables using `sync.Mutex` with `Lock` and `Unlock`
- **Apply** the `defer mu.Unlock()` pattern for safe critical sections
- **Detect** data races using Go's built-in race detector

## Why Mutex
When multiple goroutines read and write the same variable without synchronization, the result is a data race -- one of the most insidious classes of bugs in concurrent programming. The outcome depends on the precise interleaving of goroutine execution, making the bug non-deterministic: your program might appear correct in testing and fail silently in production.

A `sync.Mutex` (mutual exclusion lock) solves this by ensuring that only one goroutine at a time can execute a critical section of code. When a goroutine calls `Lock()`, any other goroutine that also calls `Lock()` will block until the first goroutine calls `Unlock()`. This serializes access to shared state, eliminating the race.

The idiomatic Go pattern is to call `defer mu.Unlock()` immediately after `Lock()`. This guarantees the lock is released even if the critical section panics, preventing deadlocks caused by forgotten unlocks.

## Step 1 -- Observe the Race Condition

Open `main.go`. The `unsafeIncrement` function launches 1000 goroutines that each increment a shared counter 1000 times. Run it and observe that the final count is wrong:

```bash
go run main.go
```

You should see output like:
```
=== Unsafe Counter (no mutex) ===
Expected: 1000000, Got: 547832
Race condition detected!
```

The exact number will vary between runs. Now run it with Go's race detector to confirm:

```bash
go run -race main.go
```

The race detector will report `DATA RACE` warnings with stack traces showing the conflicting accesses.

### Intermediate Verification
You should see `WARNING: DATA RACE` output from the race detector pointing to the `counter++` line.

## Step 2 -- Protect with sync.Mutex

Implement the `safeIncrement` function. Add a `sync.Mutex` and wrap the counter increment in a Lock/Unlock pair:

```go
func safeIncrement() {
    var mu sync.Mutex
    counter := 0
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
    fmt.Printf("\n=== Safe Counter (with mutex) ===\n")
    fmt.Printf("Expected: 1000000, Got: %d\n", counter)
    if counter == 1000000 {
        fmt.Println("No race condition -- mutex works!")
    }
}
```

### Intermediate Verification
```bash
go run main.go
```
The safe counter should always print exactly `1000000`.

```bash
go run -race main.go
```
No `DATA RACE` warnings should appear for the safe version.

## Step 3 -- The defer Unlock Pattern

Implement `safeIncrementWithDefer` using the idiomatic `defer` pattern. Extract the critical section into a helper to show how `defer` pairs naturally with `Lock`:

```go
func safeIncrementWithDefer() {
    var mu sync.Mutex
    counter := 0
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
    fmt.Printf("\n=== Safe Counter (defer pattern) ===\n")
    fmt.Printf("Expected: 1000000, Got: %d\n", counter)
}
```

The `defer mu.Unlock()` line executes when `increment()` returns, guaranteeing the lock is always released. This is especially important when the critical section might return early or panic.

### Intermediate Verification
```bash
go run -race main.go
```
All three functions should run. The unsafe version shows an incorrect count; both safe versions show exactly `1000000`.

## Common Mistakes

### Forgetting to Unlock
**Wrong:**
```go
mu.Lock()
counter++
// forgot mu.Unlock() -- all other goroutines block forever
```
**What happens:** Deadlock. All goroutines waiting on Lock will block permanently.

**Fix:** Always pair Lock with Unlock. Use `defer mu.Unlock()` immediately after Lock.

### Copying a Mutex
**Wrong:**
```go
func doWork(mu sync.Mutex) { // receives a COPY of the mutex
    mu.Lock()
    defer mu.Unlock()
    // this lock is independent of the original
}
```
**What happens:** Each goroutine locks its own copy -- no mutual exclusion at all.

**Fix:** Always pass `*sync.Mutex` (a pointer):
```go
func doWork(mu *sync.Mutex) {
    mu.Lock()
    defer mu.Unlock()
}
```

### Locking Too Broadly
**Wrong:**
```go
mu.Lock()
result := expensiveComputation() // holds the lock during slow work
counter += result
mu.Unlock()
```
**What happens:** All goroutines are serialized through the expensive computation, eliminating concurrency benefits.

**Fix:** Only hold the lock for the shared state access:
```go
result := expensiveComputation() // no lock needed here
mu.Lock()
counter += result
mu.Unlock()
```

## Verify What You Learned

Modify the program to protect a shared `map[string]int` instead of a simple counter. Launch 100 goroutines that each insert 100 key-value pairs into the map. Confirm with `-race` that there are no data races, and that the map contains all expected entries.

## What's Next
Continue to [02-rwmutex-readers-writers](../02-rwmutex-readers-writers/02-rwmutex-readers-writers.md) to learn how `sync.RWMutex` allows multiple concurrent readers while still protecting writes.

## Summary
- A data race occurs when multiple goroutines access shared state without synchronization and at least one writes
- `sync.Mutex` provides mutual exclusion: only one goroutine holds the lock at a time
- Always use `defer mu.Unlock()` immediately after `mu.Lock()` for safety
- Never copy a mutex -- pass it by pointer
- Minimize the critical section: hold the lock only while accessing shared state
- Use `go run -race` to detect data races during development

## Reference
- [sync.Mutex documentation](https://pkg.go.dev/sync#Mutex)
- [Go Data Race Detector](https://go.dev/doc/articles/race_detector)
- [Go Blog: Share Memory by Communicating](https://go.dev/blog/codelab-share)
