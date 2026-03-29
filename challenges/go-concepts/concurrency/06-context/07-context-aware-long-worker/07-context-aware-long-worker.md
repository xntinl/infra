# 7. Context-Aware Long Worker

<!--
difficulty: advanced
concepts: [cancellation in loops, select with ctx.Done and work channel, partial work handling, cooperative cancellation]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [context.WithCancel, context.WithTimeout, select, channels, goroutines]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01 through 06 in this section
- Solid understanding of `select` with multiple channels

## Learning Objectives
After completing this exercise, you will be able to:
- **Build** a long-running worker that checks `ctx.Done()` between iterations
- **Use** the `select` pattern with both `ctx.Done()` and a work channel
- **Handle** partial work gracefully when cancellation occurs mid-processing
- **Design** workers that respond promptly to cancellation without data loss

## Why Context-Aware Workers

Real systems have workers that process items from queues, scan databases, generate reports, or run ETL pipelines. These workers loop continuously, and without context awareness, they cannot be stopped cleanly. Killing them abruptly risks leaving data in an inconsistent state.

A context-aware worker checks `ctx.Done()` at natural checkpoints -- between iterations, before starting a new item, after completing a unit of work. This gives the worker a chance to finish its current item, save progress, and exit cleanly. The pattern is simple but essential: in a `select` statement, combine `ctx.Done()` with the work channel. The runtime picks whichever is ready first.

The advanced challenge is handling partial work. If a worker is halfway through processing an item when cancellation arrives, it needs to decide: finish the current item (if fast enough), or abandon it and record where it stopped. This decision depends on the domain, but the mechanism is always the same context check.

## Step 1 -- Basic Loop with Context Check

Edit `main.go` and implement `basicWorkerLoop`. Build a worker that processes numbered items until cancelled:

```go
func basicWorkerLoop() {
    fmt.Println("=== Basic Worker Loop ===")

    ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
    defer cancel()

    for i := 1; ; i++ {
        select {
        case <-ctx.Done():
            fmt.Printf("  worker: stopped after %d items (%v)\n\n", i-1, ctx.Err())
            return
        default:
        }

        fmt.Printf("  worker: processing item %d\n", i)
        time.Sleep(100 * time.Millisecond)
    }
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Basic Worker Loop ===
  worker: processing item 1
  worker: processing item 2
  worker: processing item 3
  worker: stopped after 3 items (context deadline exceeded)
```

The worker processes items until the 350ms timeout fires. It checks `ctx.Done()` at the top of each iteration, so it never starts a new item after cancellation.

## Step 2 -- Select with Work Channel

Implement `workerWithChannel`. Build a worker that reads items from a channel, using `select` to handle both new work and cancellation:

```go
func workerWithChannel() {
    fmt.Println("=== Worker with Channel ===")

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    jobs := make(chan int, 10)
    done := make(chan []int)

    // Producer: send 20 jobs
    go func() {
        for i := 1; i <= 20; i++ {
            jobs <- i
        }
        close(jobs)
    }()

    // Worker: process jobs, respecting cancellation
    go func() {
        var processed []int
        for {
            select {
            case <-ctx.Done():
                fmt.Printf("  worker: cancelled, processed %d items\n", len(processed))
                done <- processed
                return
            case job, ok := <-jobs:
                if !ok {
                    fmt.Printf("  worker: all jobs done, processed %d items\n", len(processed))
                    done <- processed
                    return
                }
                fmt.Printf("  worker: processing job %d\n", job)
                time.Sleep(50 * time.Millisecond)
                processed = append(processed, job)
            }
        }
    }()

    // Cancel after 300ms
    time.Sleep(300 * time.Millisecond)
    fmt.Println("  main: cancelling worker")
    cancel()

    result := <-done
    fmt.Printf("  main: worker completed %d items: %v\n\n", len(result), result)
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output (approximately):
```
=== Worker with Channel ===
  worker: processing job 1
  worker: processing job 2
  worker: processing job 3
  worker: processing job 4
  worker: processing job 5
  main: cancelling worker
  worker: cancelled, processed 5 items
  main: worker completed 5 items: [1 2 3 4 5]
```

The `select` statement picks between `ctx.Done()` and a new job from the channel. When cancellation arrives, the worker reports what it processed.

## Step 3 -- Finish Current Item Before Stopping

Implement `gracefulItemCompletion`. Build a worker where each item has multiple sub-steps. On cancellation, finish the current item's remaining sub-steps before stopping:

```go
func gracefulItemCompletion() {
    fmt.Println("=== Graceful Item Completion ===")

    ctx, cancel := context.WithTimeout(context.Background(), 450*time.Millisecond)
    defer cancel()

    for item := 1; ; item++ {
        // Check cancellation BEFORE starting a new item
        select {
        case <-ctx.Done():
            fmt.Printf("  worker: stopped before item %d (%v)\n\n", item, ctx.Err())
            return
        default:
        }

        fmt.Printf("  worker: starting item %d\n", item)

        // Each item has 3 sub-steps -- once started, finish the item
        for step := 1; step <= 3; step++ {
            fmt.Printf("    step %d/%d\n", step, 3)
            time.Sleep(50 * time.Millisecond)
        }

        fmt.Printf("  worker: item %d complete\n", item)
    }
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Graceful Item Completion ===
  worker: starting item 1
    step 1/3
    step 2/3
    step 3/3
  worker: item 1 complete
  worker: starting item 2
    step 1/3
    step 2/3
    step 3/3
  worker: item 2 complete
  worker: stopped before item 3 (context deadline exceeded)
```

The worker finishes each item's sub-steps atomically. It only checks for cancellation between items, never in the middle of one. This ensures data consistency.

## Step 4 -- Progress Reporting

Implement `workerWithProgress`. Build a worker that reports its progress through a channel, letting the caller monitor how far along it is:

```go
type Progress struct {
    ItemsProcessed int
    CurrentItem    string
    Done           bool
    Err            error
}

func workerWithProgress() {
    fmt.Println("=== Worker with Progress ===")

    ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
    defer cancel()

    progress := make(chan Progress)

    go func() {
        items := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
        for i, item := range items {
            select {
            case <-ctx.Done():
                progress <- Progress{ItemsProcessed: i, Done: true, Err: ctx.Err()}
                return
            default:
            }

            progress <- Progress{ItemsProcessed: i, CurrentItem: item}
            time.Sleep(100 * time.Millisecond)
        }
        progress <- Progress{ItemsProcessed: len(items), Done: true}
    }()

    for p := range progress {
        if p.Done {
            if p.Err != nil {
                fmt.Printf("  progress: stopped at %d items (%v)\n", p.ItemsProcessed, p.Err)
            } else {
                fmt.Printf("  progress: completed all %d items\n", p.ItemsProcessed)
            }
            break
        }
        fmt.Printf("  progress: [%d] processing %q\n", p.ItemsProcessed, p.CurrentItem)
    }
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Worker with Progress ===
  progress: [0] processing "alpha"
  progress: [1] processing "beta"
  progress: [2] processing "gamma"
  progress: stopped at 3 items (context deadline exceeded)
```

## Common Mistakes

### Checking ctx.Done() Only at the Start
**Wrong:**
```go
for {
    select {
    case <-ctx.Done():
        return
    default:
    }
    veryLongOperation() // runs for minutes -- no cancellation check inside
}
```
**Fix:** Check `ctx.Done()` at multiple points within long operations, or break them into smaller steps.

### Blocking on Channel Send After Cancellation
**Wrong:**
```go
select {
case <-ctx.Done():
    results <- partialResult // blocks if nobody is reading results
    return
}
```
**Fix:** Use a select for the send too:
```go
select {
case <-ctx.Done():
    select {
    case results <- partialResult:
    default: // drop if nobody is listening
    }
    return
}
```

### Not Returning After Cancellation
**Wrong:**
```go
select {
case <-ctx.Done():
    fmt.Println("cancelled")
    // falls through to continue working!
}
doMoreWork()
```
**Fix:** Always `return` after handling cancellation.

## Verify What You Learned

Implement `verifyKnowledge`: build a batch processor that receives a slice of 10 strings to process. Each string takes 80ms. Use a 500ms timeout. The processor should:
1. Check cancellation before each item
2. Track which items were processed and which were skipped
3. Return a summary with processed items and the reason for stopping (completion or timeout)

## What's Next
Continue to [08-graceful-shutdown-with-context](../08-graceful-shutdown-with-context/08-graceful-shutdown-with-context.md) to build a complete graceful shutdown system using context, signals, and WaitGroup.

## Summary
- Check `ctx.Done()` at natural checkpoints: between iterations, before starting new work
- Use `select` with `ctx.Done()` and work channels to handle both cancellation and new items
- Decide whether to finish the current item on cancellation (domain-specific decision)
- Report progress through a channel so callers can monitor long-running operations
- Always `return` after handling cancellation -- do not fall through to more work
- For multi-step items, check cancellation between items but finish sub-steps atomically

## Reference
- [Go Blog: Pipelines](https://go.dev/blog/pipelines)
- [Go Concurrency Patterns](https://go.dev/talks/2012/concurrency.slide)
- [Package context](https://pkg.go.dev/context)
