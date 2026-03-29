# 5. Semaphore: Bounded Concurrency

<!--
difficulty: intermediate
concepts: [semaphore, buffered channel, bounded concurrency, backpressure]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [goroutines, channels, sync.WaitGroup, worker pool]
-->

## Prerequisites
- Go 1.22+ installed
- Understanding of goroutines, buffered channels, and `sync.WaitGroup`
- Familiarity with the worker pool pattern (exercise 04)

## Learning Objectives
After completing this exercise, you will be able to:
- **Use** a buffered channel as a counting semaphore
- **Limit** the number of concurrently executing goroutines
- **Compare** the semaphore approach with fixed worker pools
- **Apply** bounded concurrency to protect limited resources

## Why Semaphores
A semaphore limits the number of concurrent operations. In Go, a buffered channel is a natural semaphore: sending to it "acquires" a slot, and receiving from it "releases" a slot. When the buffer is full, the next acquire blocks until someone releases.

The semaphore pattern differs from worker pools in a subtle but important way. With a worker pool, you have a fixed set of long-lived goroutines processing a shared queue. With a semaphore, you launch a new goroutine per task but limit how many run simultaneously. The goroutines are short-lived -- each handles exactly one task and exits.

```
  Semaphore Flow

  main loop:
    for each task:
      sem <- struct{}{}    // ACQUIRE (blocks if N already running)
      go func() {
        defer func() { <-sem }()  // RELEASE
        doWork()
      }()

  Buffered channel capacity = max concurrent goroutines
```

## Step 1 -- Buffered Channel as Semaphore

Create a semaphore and use it to limit concurrency.

```go
package main

import (
    "fmt"
    "sync"
    "time"
)

func main() {
    const maxConcurrency = 3
    const totalTasks = 10

    sem := make(chan struct{}, maxConcurrency)
    var wg sync.WaitGroup

    for i := 1; i <= totalTasks; i++ {
        wg.Add(1)
        sem <- struct{}{} // acquire: blocks if 3 goroutines are already running
        go func(id int) {
            defer wg.Done()
            defer func() { <-sem }() // release

            fmt.Printf("  task %d: started\n", id)
            time.Sleep(100 * time.Millisecond)
            fmt.Printf("  task %d: done\n", id)
        }(i)
    }

    wg.Wait()
}
```

The `sem` channel has capacity 3. When 3 goroutines are running, the 4th `sem <- struct{}{}` blocks until one finishes and releases its slot with `<-sem`.

### Intermediate Verification
```bash
go run main.go
```
Expected: at most 3 tasks run concurrently. You will see groups of 3 starting, then finishing:
```
  task 1: started
  task 2: started
  task 3: started
  task 1: done
  task 4: started
  ...
```

## Step 2 -- Track Active Goroutines

Add instrumentation to prove the semaphore works by tracking the count of active goroutines:

```go
package main

import (
    "fmt"
    "sync"
    "sync/atomic"
    "time"
)

func main() {
    const maxConcurrency = 3
    const totalTasks = 12

    sem := make(chan struct{}, maxConcurrency)
    var wg sync.WaitGroup
    var active int64

    for i := 1; i <= totalTasks; i++ {
        wg.Add(1)
        sem <- struct{}{}
        go func(id int) {
            defer wg.Done()
            defer func() { <-sem }()

            current := atomic.AddInt64(&active, 1)
            fmt.Printf("  task %2d running (active: %d)\n", id, current)

            if current > int64(maxConcurrency) {
                fmt.Printf("  BUG: active=%d exceeds max=%d\n", current, maxConcurrency)
            }

            time.Sleep(80 * time.Millisecond)
            atomic.AddInt64(&active, -1)
        }(i)
    }

    wg.Wait()
    fmt.Printf("Max concurrency respected: active never exceeded %d\n", maxConcurrency)
}
```

The active count should never exceed `maxConcurrency`.

### Intermediate Verification
```bash
go run main.go
```
Expected: active count stays at or below 3.

## Step 3 -- Compare with Worker Pool

Implement the same work using both approaches and compare:

```go
package main

import (
    "fmt"
    "sync"
    "time"
)

func main() {
    const numTasks = 15
    const concurrency = 4

    // Semaphore approach
    start := time.Now()
    sem := make(chan struct{}, concurrency)
    var wg1 sync.WaitGroup
    for i := 0; i < numTasks; i++ {
        wg1.Add(1)
        sem <- struct{}{}
        go func(id int) {
            defer wg1.Done()
            defer func() { <-sem }()
            time.Sleep(50 * time.Millisecond)
        }(i)
    }
    wg1.Wait()
    fmt.Printf("Semaphore:   %v\n", time.Since(start))

    // Worker pool approach
    start = time.Now()
    jobs := make(chan int, numTasks)
    var wg2 sync.WaitGroup
    for w := 0; w < concurrency; w++ {
        wg2.Add(1)
        go func() {
            defer wg2.Done()
            for range jobs {
                time.Sleep(50 * time.Millisecond)
            }
        }()
    }
    for i := 0; i < numTasks; i++ {
        jobs <- i
    }
    close(jobs)
    wg2.Wait()
    fmt.Printf("Worker pool: %v\n", time.Since(start))
}
```

### Intermediate Verification
```bash
go run main.go
```
Both approaches should take roughly the same time.

## Common Mistakes

### Acquiring Inside the Goroutine
**Wrong:**
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

    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func(id int) { // ALL 100 goroutines launch immediately
            sem <- struct{}{}     // acquire inside goroutine
            defer func() { <-sem }()
            defer wg.Done()
            fmt.Printf("task %d\n", id)
            time.Sleep(100 * time.Millisecond)
        }(i)
    }
    wg.Wait()
}
```
**What happens:** All goroutines launch immediately (unbounded), then compete for the semaphore. You get a burst of goroutine creation, defeating the purpose.

**Fix:** Acquire the semaphore before launching the goroutine. This blocks the launching loop, ensuring at most N goroutines exist at any time.

### Forgetting to Release
**Wrong:**
```go
go func(id int) {
    defer wg.Done()
    // forgot: defer func() { <-sem }()
    doWork()
}(i)
```
**What happens:** Slots are acquired but never released. After N tasks, the program deadlocks.

**Fix:** Always pair acquire with a deferred release. Using `defer` ensures release happens even if the goroutine panics.

### Using a Mutex Instead of a Semaphore
A mutex limits concurrency to 1. If you need N > 1, a mutex does not work. A buffered channel generalizes to any N.

## Verify What You Learned

Run `go run main.go` and verify:
- Basic semaphore: all 10 tasks complete, at most 3 concurrent
- Tracked semaphore: active count never exceeds 3
- Comparison: semaphore and worker pool have similar performance
- URL fetcher: at most 5 concurrent downloads, all 20 complete

## What's Next
Continue to [06-generator-lazy-production](../06-generator-lazy-production/06-generator-lazy-production.md) to learn how to produce values lazily with channels.

## Summary
- A buffered channel of `struct{}` is Go's idiomatic counting semaphore
- Acquire: `sem <- struct{}{}` (blocks when buffer is full)
- Release: `<-sem` (frees a slot for another goroutine)
- Acquire before `go func()` to limit goroutine creation, not just execution
- Semaphores give per-task goroutines; worker pools reuse fixed goroutines
- Both achieve bounded concurrency; choose based on task uniformity

## Reference
- [Effective Go: Channels as Semaphores](https://go.dev/doc/effective_go#channels)
- [Go Blog: Advanced Concurrency Patterns](https://go.dev/blog/advanced-go-concurrency-patterns)
- [golang.org/x/sync/semaphore](https://pkg.go.dev/golang.org/x/sync/semaphore) -- weighted semaphore in the extended library
