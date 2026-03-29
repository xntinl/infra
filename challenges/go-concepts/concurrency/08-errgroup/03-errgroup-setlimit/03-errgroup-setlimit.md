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
- **Choose** appropriate concurrency limits based on resource constraints

## Why SetLimit
Launching one goroutine per task is fine when you have 10 tasks. When you have 10,000 tasks, you risk exhausting memory, file descriptors, or overwhelming a downstream service. The traditional solution is a semaphore pattern: a buffered channel of capacity N that goroutines acquire before starting and release when done.

`g.SetLimit(n)` encapsulates this pattern directly in errgroup. When the limit is set, `g.Go()` blocks if N goroutines are already running, waiting until one finishes before launching the next. This provides natural backpressure without any additional channels or synchronization code.

Available since `golang.org/x/sync v0.1.0`, SetLimit turns errgroup into a bounded worker pool with built-in error propagation.

## Step 1 -- Observe Unbounded Concurrency

Run the starter code:

```bash
go mod tidy
go run main.go
```

The `unboundedConcurrency` function launches 20 tasks simultaneously. Observe the timestamps -- all tasks start at roughly the same time. With real resources (HTTP connections, database queries), this would overwhelm the target.

### Intermediate Verification
You see all 20 tasks printing "started" nearly simultaneously. The total time is approximately the duration of one task (they all run in parallel).

## Step 2 -- Apply SetLimit

Implement the `boundedWithSetLimit` function. Create an errgroup, set a limit, and launch the same 20 tasks:

```go
func boundedWithSetLimit() {
    fmt.Println("\n=== Bounded Concurrency with SetLimit ===")
    start := time.Now()
    var g errgroup.Group
    g.SetLimit(3) // at most 3 goroutines running concurrently

    for i := 0; i < 20; i++ {
        i := i
        g.Go(func() error { // blocks if 3 goroutines are already running
            fmt.Printf("  [%v] Task %2d: started\n",
                time.Since(start).Round(time.Millisecond), i)
            time.Sleep(200 * time.Millisecond) // simulate work
            fmt.Printf("  [%v] Task %2d: done\n",
                time.Since(start).Round(time.Millisecond), i)
            return nil
        })
    }

    if err := g.Wait(); err != nil {
        fmt.Printf("Error: %v\n", err)
    }
    fmt.Printf("Total time: %v\n", time.Since(start).Round(time.Millisecond))
}
```

With a limit of 3 and 20 tasks of 200ms each, the total time should be approximately `ceil(20/3) * 200ms ~ 1400ms` instead of ~200ms for unbounded.

### Intermediate Verification
```bash
go run main.go
```
Tasks start in batches of 3. You can see from the timestamps that at most 3 tasks are running at any given moment. When one finishes, the next one starts.

## Step 3 -- Compare with the Manual Semaphore Pattern

Implement `boundedWithSemaphore` to show the manual approach that SetLimit replaces:

```go
func boundedWithSemaphore() {
    fmt.Println("\n=== Bounded Concurrency with Semaphore (manual) ===")
    start := time.Now()
    var g errgroup.Group
    sem := make(chan struct{}, 3) // buffered channel as semaphore

    for i := 0; i < 20; i++ {
        i := i
        sem <- struct{}{} // acquire -- blocks if channel is full
        g.Go(func() error {
            defer func() { <-sem }() // release
            fmt.Printf("  [%v] Task %2d: started\n",
                time.Since(start).Round(time.Millisecond), i)
            time.Sleep(200 * time.Millisecond)
            fmt.Printf("  [%v] Task %2d: done\n",
                time.Since(start).Round(time.Millisecond), i)
            return nil
        })
    }

    if err := g.Wait(); err != nil {
        fmt.Printf("Error: %v\n", err)
    }
    fmt.Printf("Total time: %v\n", time.Since(start).Round(time.Millisecond))
}
```

The semaphore pattern achieves the same result but requires more ceremony: creating the channel, acquiring before `g.Go()`, releasing with defer inside the goroutine. `SetLimit` replaces all of this with a single line.

### Intermediate Verification
```bash
go run main.go
```
Both bounded approaches produce similar timing and batch behavior. The SetLimit version is significantly cleaner.

## Common Mistakes

### Setting the limit after calling Go
**Wrong:**
```go
var g errgroup.Group
g.Go(func() error { return nil }) // goroutine launched before limit is set
g.SetLimit(3) // panics!
```
**What happens:** `SetLimit` panics if called after `Go`. The limit must be set before launching any goroutines.

**Fix:** Always call `SetLimit` immediately after creating the group:
```go
var g errgroup.Group
g.SetLimit(3)
g.Go(func() error { return nil })
```

### Setting limit to 0 or negative
**Wrong:**
```go
g.SetLimit(0) // no goroutines can run
```
**What happens:** With a limit of 0, `g.Go()` blocks forever because no goroutine can start. A negative value removes the limit (equivalent to no SetLimit call).

**Fix:** Use a positive integer for the limit. Use -1 explicitly if you want to remove a previously set limit (uncommon).

### Acquiring the semaphore on the wrong side
**Wrong (semaphore pattern):**
```go
g.Go(func() error {
    sem <- struct{}{} // acquire INSIDE the goroutine
    defer func() { <-sem }()
    // work
})
```
**What happens:** The goroutine is already launched before acquiring the semaphore. You still get unbounded goroutine creation -- the semaphore only limits concurrent execution of the work portion, not goroutine count.

**Fix:** Acquire the semaphore BEFORE `g.Go()`, or just use `SetLimit` which handles this correctly.

## Verify What You Learned

Modify `boundedWithSetLimit` to add error handling: task 10 should fail. Confirm that:
1. The limit is still respected (at most 3 concurrent tasks)
2. All tasks get a chance to run despite the error
3. `Wait()` returns the error from task 10

Then experiment with different limits (1, 5, 10) and observe how total execution time changes.

## What's Next
Continue to [04-errgroup-collect-results](../04-errgroup-collect-results/04-errgroup-collect-results.md) to learn how to safely collect results from parallel errgroup tasks.

## Summary
- `g.SetLimit(n)` limits the number of concurrently running goroutines in an errgroup
- `g.Go()` blocks when the limit is reached, providing natural backpressure
- Always call `SetLimit` before the first `Go()` call -- calling it after panics
- SetLimit replaces the manual semaphore (buffered channel) pattern with a single line
- Choose the limit based on your bottleneck: CPU cores, connection pool size, rate limits
- A limit of -1 removes the restriction (equivalent to no limit)

## Reference
- [errgroup.Group.SetLimit documentation](https://pkg.go.dev/golang.org/x/sync/errgroup#Group.SetLimit)
- [Semaphore pattern in Go](https://go.dev/doc/effective_go#channels)
- [golang.org/x/sync release notes](https://github.com/golang/sync)
