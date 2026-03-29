# 3. Errgroup SetLimit

<!--
difficulty: intermediate
concepts: [errgroup.SetLimit, bounded concurrency, semaphore pattern, backpressure]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [errgroup basics, goroutines, channels as semaphores]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercise 01 (errgroup basics)
- Understanding of unbounded vs bounded concurrency
- Familiarity with the semaphore pattern (buffered channel to limit goroutines)

## Learning Objectives
After completing this exercise, you will be able to:
- **Apply** `g.SetLimit(n)` to control maximum concurrent goroutines in an errgroup
- **Observe** that `g.Go()` blocks when the limit is reached, providing backpressure
- **Compare** the SetLimit approach with the manual semaphore pattern
- **Combine** SetLimit with WithContext for bounded concurrency with cancellation

## Why SetLimit

Launching one goroutine per task is fine when you have 10 tasks. When you have 10,000 tasks, you risk exhausting memory, file descriptors, or overwhelming a downstream service. The traditional solution is a semaphore pattern: a buffered channel of capacity N that goroutines acquire before starting and release when done.

`g.SetLimit(n)` encapsulates this pattern directly in errgroup. When the limit is set, `g.Go()` blocks if N goroutines are already running, waiting until one finishes before launching the next. This provides natural backpressure without any additional channels or synchronization code.

Available since `golang.org/x/sync v0.1.0`, SetLimit turns errgroup into a bounded worker pool with built-in error propagation.

## Step 1 -- Observe Unbounded Concurrency

Run the program:

```bash
go mod tidy
go run main.go
```

The `unboundedConcurrency` function launches 10 tasks simultaneously. All start at roughly the same time:

```go
package main

import (
    "fmt"
    "time"

    "golang.org/x/sync/errgroup"
)

func main() {
    start := time.Now()
    var g errgroup.Group

    for i := 0; i < 10; i++ {
        i := i
        g.Go(func() error {
            fmt.Printf("  [%v] Task %2d: started\n", time.Since(start).Round(time.Millisecond), i)
            time.Sleep(200 * time.Millisecond)
            return nil
        })
    }

    _ = g.Wait()
    fmt.Printf("Total: %v (all ran in parallel)\n", time.Since(start).Round(time.Millisecond))
}
```

**Expected output:**
```
  [0ms] Task  0: started
  [0ms] Task  1: started
  ...all 10 at ~0ms...
Total: ~200ms (all ran in parallel)
```

With real resources (HTTP connections, database queries), launching 10,000 of these simultaneously would be catastrophic.

## Step 2 -- Apply SetLimit

Add `g.SetLimit(3)` so at most 3 goroutines run at once:

```go
package main

import (
    "fmt"
    "time"

    "golang.org/x/sync/errgroup"
)

func main() {
    start := time.Now()

    var g errgroup.Group
    g.SetLimit(3) // MUST be called before any Go() call

    for i := 0; i < 10; i++ {
        i := i
        g.Go(func() error { // blocks if 3 are already running
            fmt.Printf("  [%v] Task %2d: started\n", time.Since(start).Round(time.Millisecond), i)
            time.Sleep(200 * time.Millisecond)
            fmt.Printf("  [%v] Task %2d: done\n", time.Since(start).Round(time.Millisecond), i)
            return nil
        })
    }

    _ = g.Wait()
    fmt.Printf("Total: %v (ceil(10/3) batches of ~200ms)\n", time.Since(start).Round(time.Millisecond))
}
```

**Expected output:**
```
  [0ms]   Task  0: started
  [0ms]   Task  1: started
  [0ms]   Task  2: started
  [200ms] Task  0: done
  [200ms] Task  3: started
  ...batches of 3...
Total: ~800ms (ceil(10/3) batches of ~200ms)
```

With limit 3 and 10 tasks of 200ms each: `ceil(10/3) * 200ms = 800ms`. The backpressure is automatic -- `g.Go()` blocks the calling goroutine until a slot opens.

## Step 3 -- Compare with the Manual Semaphore

The manual semaphore pattern that SetLimit replaces:

```go
package main

import (
    "fmt"
    "time"

    "golang.org/x/sync/errgroup"
)

func main() {
    start := time.Now()

    var g errgroup.Group
    sem := make(chan struct{}, 3) // buffered channel = semaphore

    for i := 0; i < 10; i++ {
        i := i
        sem <- struct{}{} // acquire: blocks if channel is full
        g.Go(func() error {
            defer func() { <-sem }() // release when done
            fmt.Printf("  [%v] Task %2d: started\n", time.Since(start).Round(time.Millisecond), i)
            time.Sleep(200 * time.Millisecond)
            fmt.Printf("  [%v] Task %2d: done\n", time.Since(start).Round(time.Millisecond), i)
            return nil
        })
    }

    _ = g.Wait()
    fmt.Printf("Total: %v (same behavior as SetLimit)\n", time.Since(start).Round(time.Millisecond))
}
```

**Expected output:** Same timing and batching as SetLimit. But the code requires: creating a channel, sending before `Go()`, deferring a receive inside the goroutine. `SetLimit(3)` replaces all of it with one line.

## Step 4 -- SetLimit + WithContext

The production pattern: bounded concurrency AND automatic cancellation on error. Combine `SetLimit` with `WithContext`:

```go
package main

import (
    "context"
    "fmt"
    "time"

    "golang.org/x/sync/errgroup"
)

func main() {
    g, ctx := errgroup.WithContext(context.Background())
    g.SetLimit(3)

    for i := 0; i < 10; i++ {
        i := i
        g.Go(func() error {
            select {
            case <-ctx.Done():
                fmt.Printf("  Task %d: cancelled\n", i)
                return ctx.Err()
            default:
            }

            time.Sleep(100 * time.Millisecond)
            if i == 5 {
                fmt.Printf("  Task %d: returning error\n", i)
                return fmt.Errorf("task %d failed", i)
            }
            fmt.Printf("  Task %d: done\n", i)
            return nil
        })
    }

    if err := g.Wait(); err != nil {
        fmt.Printf("Error: %v\n", err)
    }
}
```

**Expected output:**
```
  Task 0: done
  Task 1: done
  Task 2: done
  Task 5: returning error
  Task 6: cancelled
  Task 7: cancelled
  ...remaining tasks cancelled...
Error: task 5 failed
```

Tasks 0-4 run (in batches of 3). Task 5 fails, the context is cancelled, and tasks 6-9 detect the cancellation immediately without doing work.

## Common Mistakes

### Setting the limit after calling Go

**Wrong:**
```go
var g errgroup.Group
g.Go(func() error { return nil }) // launched before limit is set
g.SetLimit(3) // PANICS at runtime
```

**What happens:** `SetLimit` panics if called after `Go`. The limit must be set before any work is launched.

**Fix:** Always call `SetLimit` immediately after creating the group:
```go
var g errgroup.Group
g.SetLimit(3)
g.Go(func() error { return nil })
```

### Setting limit to 0

**Wrong:**
```go
g.SetLimit(0) // no goroutines can ever run
g.Go(func() error { return nil }) // blocks forever
```

**What happens:** With a limit of 0, `g.Go()` blocks forever because no goroutine slot is available. The program deadlocks.

**Fix:** Use a positive integer. A limit of -1 removes the restriction (equivalent to no SetLimit call).

### Acquiring the semaphore inside the goroutine (manual pattern)

**Wrong:**
```go
g.Go(func() error {
    sem <- struct{}{} // acquire INSIDE the goroutine
    defer func() { <-sem }()
    // work
    return nil
})
```

**What happens:** The goroutine is already launched before acquiring the semaphore. You get unbounded goroutine creation -- the semaphore only limits concurrent execution of the work, not goroutine count. Memory usage spikes.

**Fix:** Acquire BEFORE `g.Go()`, or use `SetLimit` which handles this correctly.

## Verify What You Learned

Run the full program and confirm:
1. Unbounded runs all tasks in parallel (~200ms total)
2. SetLimit(3) processes in batches of 3 (~800ms total)
3. The manual semaphore produces identical timing
4. SetLimit + WithContext cancels remaining tasks when one fails

```bash
go run main.go
```

## What's Next
Continue to [04-errgroup-collect-results](../04-errgroup-collect-results/04-errgroup-collect-results.md) to learn how to safely collect results from parallel errgroup tasks.

## Summary
- `g.SetLimit(n)` limits the number of concurrently running goroutines in an errgroup
- `g.Go()` blocks when the limit is reached, providing natural backpressure
- Always call `SetLimit` before the first `Go()` call -- calling it after panics
- SetLimit replaces the manual semaphore (buffered channel) pattern with a single line
- Combine SetLimit with WithContext for bounded concurrency + automatic cancellation
- Choose the limit based on your bottleneck: CPU cores, connection pool size, API rate limits
- A limit of -1 removes the restriction (equivalent to no limit)

## Reference
- [errgroup.Group.SetLimit documentation](https://pkg.go.dev/golang.org/x/sync/errgroup#Group.SetLimit)
- [Semaphore pattern in Go](https://go.dev/doc/effective_go#channels)
- [golang.org/x/sync release notes](https://github.com/golang/sync)
