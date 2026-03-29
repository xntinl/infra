# 9. Buffered Channel as Semaphore

<!--
difficulty: advanced
concepts: [semaphore, concurrency-limiting, buffered-channels, resource-management, backpressure]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [goroutines, unbuffered-channels, buffered-channels, channel-direction]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01-04 (channels basics through direction)
- Understanding of buffered channel blocking behavior

## Learning Objectives
After completing this exercise, you will be able to:
- **Implement** a semaphore using a buffered channel to limit concurrency
- **Apply** the acquire/release pattern with `sem <- struct{}{}` and `<-sem`
- **Control** the maximum number of concurrent goroutines accessing a resource
- **Recognize** when to use a semaphore vs. unbounded goroutines

## Why Semaphore With Buffered Channels

Launching one goroutine per task is cheap, but some resources can't handle unlimited concurrency. A database might support 10 connections. An API might rate-limit to 5 requests per second. A filesystem might degrade with more than 20 concurrent reads. You need a way to limit how many goroutines run simultaneously.

A buffered channel makes a natural semaphore. Create a channel with capacity N — that's your concurrency limit. Before doing work, send a value into the channel (acquire). If N goroutines are already active, the channel is full and the send blocks. When done, receive from the channel (release), making room for another goroutine. The buffer capacity directly controls the maximum parallelism.

This is lighter than OS semaphores and integrates naturally with Go's channel-based concurrency. You don't need external libraries — just a `make(chan struct{}, N)` and disciplined acquire/release.

## Step 1 -- Unlimited Concurrency (The Problem)

Launch 20 goroutines that all access a "resource" simultaneously. Observe that all 20 run at once.

```go
func accessResource(id int, wg *sync.WaitGroup) {
    defer wg.Done()
    fmt.Printf("[%s] Goroutine %2d: accessing resource\n",
        time.Now().Format("15:04:05.000"), id)
    time.Sleep(500 * time.Millisecond) // simulate work
    fmt.Printf("[%s] Goroutine %2d: done\n",
        time.Now().Format("15:04:05.000"), id)
}
```

When you launch all 20, they ALL start within milliseconds of each other. In a real system, this might overwhelm the resource.

### Intermediate Verification
```bash
go run main.go
# All 20 goroutines start nearly simultaneously
```

## Step 2 -- Add a Semaphore

Create a buffered channel of capacity 3. Before accessing the resource, acquire a slot. After finishing, release it.

```go
sem := make(chan struct{}, 3) // max 3 concurrent

func accessWithLimit(id int, sem chan struct{}, wg *sync.WaitGroup) {
    defer wg.Done()

    sem <- struct{}{}   // acquire — blocks if 3 already active
    defer func() { <-sem }() // release — always runs

    fmt.Printf("Goroutine %d: working\n", id)
    time.Sleep(500 * time.Millisecond)
    fmt.Printf("Goroutine %d: done\n", id)
}
```

Now only 3 goroutines work at any time. The rest queue up, waiting to acquire.

### Intermediate Verification
```bash
go run main.go
# Only 3 goroutines active at a time
# New ones start as previous ones finish
```

## Step 3 -- Observe the Batching Effect

With a semaphore of size 3 and 12 goroutines each taking 500ms, the work completes in approximately 4 batches of 3. Add timestamps to verify.

```go
start := time.Now()
// ... launch goroutines with semaphore ...
wg.Wait()
elapsed := time.Since(start)
fmt.Printf("Total: %v (expected ~2s for 12 items, batch size 3, 500ms each)\n", elapsed)
```

### Intermediate Verification
```bash
go run main.go
# Total time ~2s (4 batches * 500ms), not 500ms (all parallel)
```

## Step 4 -- Weighted Semaphore

Some operations need more "slots" than others. A heavy query might take 2 slots, a light one takes 1. Model this by acquiring multiple slots.

```go
sem := make(chan struct{}, 5) // 5 total slots

func heavyWork(sem chan struct{}) {
    // Acquire 2 slots
    sem <- struct{}{}
    sem <- struct{}{}
    defer func() { <-sem; <-sem }()

    // ... heavy work that needs double the resources ...
}

func lightWork(sem chan struct{}) {
    sem <- struct{}{} // 1 slot
    defer func() { <-sem }()

    // ... light work ...
}
```

### Intermediate Verification
```bash
go run main.go
# Mix of heavy (2 slots) and light (1 slot) work
# Total concurrent slots never exceeds 5
```

## Common Mistakes

### Forgetting to Release the Semaphore
**Wrong:**
```go
sem <- struct{}{}
doWork() // if this panics, the slot is never released
// <-sem never reached
```
**What happens:** One slot is permanently consumed. Eventually all slots are stuck and no new work can start. The program hangs.
**Fix:** Always use `defer func() { <-sem }()` immediately after acquiring to guarantee release.

### Using the Semaphore Backwards
**Wrong:**
```go
sem := make(chan struct{}, 3)
<-sem        // receive first — blocks forever on empty channel!
defer func() { sem <- struct{}{} }()
```
**What happens:** The "acquire" receive blocks because the channel is empty. You've inverted the pattern.
**Fix:** Send to acquire (fills buffer), receive to release (drains buffer). Pre-filling the channel and receiving to acquire is an alternative valid pattern, but the standard Go idiom is send-to-acquire.

## Verify What You Learned

Build a concurrent URL fetcher simulator in `main.go`:
1. Define 15 "URLs" (strings) to fetch
2. Use a semaphore to limit concurrent "fetches" to 4
3. Each fetch simulates variable duration (use URL index to vary sleep)
4. Track and print: which fetches are active at any moment, total time
5. Verify that no more than 4 fetches run simultaneously

Use a shared counter (protected by a mutex) to track active goroutines and assert the maximum never exceeds the semaphore size.

## What's Next
Continue to [10-channel-vs-shared-memory](../10-channel-vs-shared-memory/10-channel-vs-shared-memory.md) to compare channel-based and mutex-based approaches to the same problem.

## Summary
- `sem := make(chan struct{}, N)` creates a semaphore with capacity N
- `sem <- struct{}{}` acquires a slot (blocks if N slots are taken)
- `<-sem` releases a slot (always `defer` this to prevent leaks)
- The buffer capacity equals the maximum concurrent operations
- Use `defer func() { <-sem }()` immediately after acquiring for panic safety
- Weighted semaphores acquire multiple slots for resource-heavy operations

## Reference
- [Effective Go: Channels as semaphores](https://go.dev/doc/effective_go#channels)
- [golang.org/x/sync/semaphore](https://pkg.go.dev/golang.org/x/sync/semaphore) (standard library weighted semaphore)
- [Go Blog: Bounded concurrency](https://go.dev/blog/pipelines)
