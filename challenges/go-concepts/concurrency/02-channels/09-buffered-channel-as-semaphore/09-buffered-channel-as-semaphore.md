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

Launching one goroutine per task is cheap, but some resources can't handle unlimited concurrency. A database might support 10 connections. An API might rate-limit to 5 requests per second. A filesystem might degrade with more than 20 concurrent reads.

A buffered channel makes a natural semaphore. Create a channel with capacity N -- that's your concurrency limit. Before doing work, send a value into the channel (acquire). If N goroutines are already active, the channel is full and the send blocks. When done, receive from the channel (release), making room for another goroutine.

This is lighter than OS semaphores and integrates naturally with Go's channel-based concurrency.

## Step 1 -- Unlimited Concurrency (The Problem)

Launch 10 goroutines that all access a resource simultaneously. All 10 start at nearly the same time.

```go
package main

import (
    "fmt"
    "sync"
    "time"
)

func main() {
    var wg sync.WaitGroup
    start := time.Now()

    for i := 1; i <= 10; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            fmt.Printf("[%s] Goroutine %2d: start\n",
                time.Now().Format("15:04:05.000"), id)
            time.Sleep(300 * time.Millisecond)
            fmt.Printf("[%s] Goroutine %2d: done\n",
                time.Now().Format("15:04:05.000"), id)
        }(i)
    }

    wg.Wait()
    fmt.Printf("Total: %v (all ran in parallel)\n",
        time.Since(start).Round(time.Millisecond))
}
```

### Verification
```bash
go run main.go
# Expected: all 10 goroutines start simultaneously, total ~300ms
```

In production, this might overwhelm the resource.

## Step 2 -- Add a Semaphore

Create a buffered channel of capacity 3. Before accessing the resource, acquire a slot. After finishing, release it.

```go
package main

import (
    "fmt"
    "sync"
    "time"
)

func main() {
    // The buffer capacity IS the concurrency limit.
    sem := make(chan struct{}, 3)
    var wg sync.WaitGroup
    start := time.Now()

    for i := 1; i <= 10; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()

            // Acquire: send blocks when buffer is full (3 already active).
            sem <- struct{}{}
            // Release: ALWAYS defer to guarantee the slot is freed, even on panic.
            defer func() { <-sem }()

            elapsed := time.Since(start).Round(time.Millisecond)
            fmt.Printf("[+%6s] Goroutine %2d: start\n", elapsed, id)
            time.Sleep(300 * time.Millisecond)
        }(i)
    }

    wg.Wait()
    fmt.Printf("Total: %v (max 3 concurrent)\n",
        time.Since(start).Round(time.Millisecond))
}
```

Now only 3 goroutines work at any time. The rest queue up, waiting to acquire.

### Verification
```bash
go run main.go
# Expected: goroutines start in batches of 3, total ~1.2s
```

## Step 3 -- Observe the Batching Effect

With a semaphore of size 3 and 12 goroutines each taking 500ms, the work completes in approximately 4 batches.

```go
package main

import (
    "fmt"
    "sync"
    "time"
)

func main() {
    sem := make(chan struct{}, 3)
    var wg sync.WaitGroup
    start := time.Now()

    for i := 1; i <= 12; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            sem <- struct{}{}
            defer func() { <-sem }()

            elapsed := time.Since(start).Round(time.Millisecond)
            fmt.Printf("[+%6s] Goroutine %2d: start\n", elapsed, id)
            time.Sleep(500 * time.Millisecond)
        }(i)
    }

    wg.Wait()
    total := time.Since(start).Round(time.Millisecond)
    fmt.Printf("Total: %s (expected ~2s for 12 items, batch 3, 500ms each)\n", total)
}
```

### Verification
```bash
go run main.go
# Expected: total ~2s (4 batches * 500ms)
```

## Step 4 -- Weighted Semaphore

Some operations need more "slots" than others. A heavy operation takes 2 slots, a light one takes 1.

```go
package main

import (
    "fmt"
    "sync"
    "time"
)

func main() {
    sem := make(chan struct{}, 5) // 5 total slots
    var wg sync.WaitGroup

    heavyWork := func(id int) {
        defer wg.Done()
        // Acquire 2 slots.
        sem <- struct{}{}
        sem <- struct{}{}
        defer func() { <-sem; <-sem }()

        fmt.Printf("[%s] Heavy %d: working (2 slots)\n",
            time.Now().Format("15:04:05.000"), id)
        time.Sleep(400 * time.Millisecond)
        fmt.Printf("[%s] Heavy %d: done\n",
            time.Now().Format("15:04:05.000"), id)
    }

    lightWork := func(id int) {
        defer wg.Done()
        // Acquire 1 slot.
        sem <- struct{}{}
        defer func() { <-sem }()

        fmt.Printf("[%s] Light %d: working (1 slot)\n",
            time.Now().Format("15:04:05.000"), id)
        time.Sleep(200 * time.Millisecond)
        fmt.Printf("[%s] Light %d: done\n",
            time.Now().Format("15:04:05.000"), id)
    }

    for i := 1; i <= 3; i++ {
        wg.Add(1)
        go heavyWork(i)
    }
    for i := 1; i <= 4; i++ {
        wg.Add(1)
        go lightWork(i)
    }

    wg.Wait()
    fmt.Println("All tasks done (max 5 total resource slots)")
}
```

### Verification
```bash
go run main.go
# Expected: heavy tasks take 2 slots, light take 1, never more than 5 total active
```

## Step 5 -- URL Fetcher with Concurrency Verification

A practical example: fetch 15 URLs with a maximum of 4 concurrent fetches. Track active goroutines to verify the limit.

```go
package main

import (
    "fmt"
    "sync"
    "sync/atomic"
    "time"
)

func main() {
    urls := make([]string, 15)
    for i := range urls {
        urls[i] = fmt.Sprintf("https://example.com/page/%d", i+1)
    }

    sem := make(chan struct{}, 4)
    var wg sync.WaitGroup
    var activeCount atomic.Int32
    var maxConcurrent atomic.Int32
    start := time.Now()

    for i, url := range urls {
        wg.Add(1)
        go func(id int, url string) {
            defer wg.Done()
            sem <- struct{}{}
            defer func() { <-sem }()

            current := activeCount.Add(1)
            // Track max using compare-and-swap.
            for {
                old := maxConcurrent.Load()
                if current <= old || maxConcurrent.CompareAndSwap(old, current) {
                    break
                }
            }

            duration := time.Duration((id%3+1)*100) * time.Millisecond
            fmt.Printf("[+%6s] Fetching %-35s (active: %d)\n",
                time.Since(start).Round(time.Millisecond), url, current)

            time.Sleep(duration)
            activeCount.Add(-1)
        }(i, url)
    }

    wg.Wait()
    maxConc := maxConcurrent.Load()
    fmt.Printf("\nMax concurrent fetches: %d (limit was 4)\n", maxConc)

    if maxConc <= 4 {
        fmt.Println("PASS: concurrency limit respected")
    } else {
        fmt.Println("FAIL: concurrency limit exceeded!")
    }
}
```

### Verification
```bash
go run main.go
# Expected: max concurrent never exceeds 4, PASS at the end
```

## Common Mistakes

### Forgetting to Release the Semaphore

**Wrong:**
```go
sem <- struct{}{}
doWork() // if this panics, the slot is never released!
<-sem    // never reached
```

**What happens:** One slot is permanently consumed. Eventually all slots are stuck and no new work can start.

**Fix:** Always use `defer` immediately after acquiring:
```go
sem <- struct{}{}
defer func() { <-sem }()
doWork() // even if this panics, defer releases the slot
```

### Using the Semaphore Backwards

**Wrong:**
```go
sem := make(chan struct{}, 3)
<-sem        // receive first -- blocks forever on empty channel!
defer func() { sem <- struct{}{} }()
```

**What happens:** The "acquire" receive blocks because the channel starts empty.

**Fix:** Send to acquire (fills buffer), receive to release (drains buffer):
```go
sem <- struct{}{}        // acquire: fill a slot
defer func() { <-sem }() // release: drain a slot
```

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
