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

The simplest pattern: check `ctx.Done()` at the top of each loop iteration using a non-blocking select:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()

	for i := 1; ; i++ {
		select {
		case <-ctx.Done():
			fmt.Printf("worker: stopped after %d items (%v)\n", i-1, ctx.Err())
			return
		default:
		}

		fmt.Printf("worker: processing item %d\n", i)
		time.Sleep(100 * time.Millisecond)
	}
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
worker: processing item 1
worker: processing item 2
worker: processing item 3
worker: stopped after 3 items (context deadline exceeded)
```

The worker processes items until the 350ms timeout fires. It checks `ctx.Done()` at the top of each iteration, so it never starts a new item after cancellation. The `default` case in the select makes it non-blocking -- if Done is not ready, execution falls through to the work.

## Step 2 -- Select with Work Channel

When reading from a channel, combine `ctx.Done()` and the job channel in the SAME select:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	jobs := make(chan int, 10)
	done := make(chan []int)

	// Producer: sends 20 jobs.
	go func() {
		for i := 1; i <= 20; i++ {
			jobs <- i
		}
		close(jobs)
	}()

	// Worker: processes jobs until cancelled or channel is drained.
	go func() {
		var processed []int
		for {
			select {
			case <-ctx.Done():
				fmt.Printf("worker: cancelled, processed %d items\n", len(processed))
				done <- processed
				return
			case job, ok := <-jobs:
				if !ok {
					fmt.Printf("worker: all jobs done, processed %d items\n", len(processed))
					done <- processed
					return
				}
				fmt.Printf("worker: processing job %d\n", job)
				time.Sleep(50 * time.Millisecond)
				processed = append(processed, job)
			}
		}
	}()

	// Cancel after 300ms.
	time.Sleep(300 * time.Millisecond)
	fmt.Println("main: cancelling worker")
	cancel()

	result := <-done
	fmt.Printf("main: worker completed %d items: %v\n", len(result), result)
}
```

### Verification
```bash
go run main.go
```
Expected output (approximately):
```
worker: processing job 1
worker: processing job 2
worker: processing job 3
worker: processing job 4
worker: processing job 5
main: cancelling worker
worker: cancelled, processed 5 items
main: worker completed 5 items: [1 2 3 4 5]
```

The `select` statement picks between `ctx.Done()` and a new job from the channel. When cancellation arrives, the worker reports what it processed. This is the standard pattern for cancellable consumers.

## Step 3 -- Finish Current Item Before Stopping

Sometimes each item has multiple sub-steps that must complete together. Check cancellation BETWEEN items, but once an item starts, run all its sub-steps to completion:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 450*time.Millisecond)
	defer cancel()

	for item := 1; ; item++ {
		// Check cancellation BEFORE starting a new item.
		select {
		case <-ctx.Done():
			fmt.Printf("worker: stopped before item %d (%v)\n", item, ctx.Err())
			return
		default:
		}

		fmt.Printf("worker: starting item %d\n", item)

		// Once started, the item runs to completion regardless of cancellation.
		for step := 1; step <= 3; step++ {
			fmt.Printf("  step %d/%d\n", step, 3)
			time.Sleep(50 * time.Millisecond)
		}

		fmt.Printf("worker: item %d complete\n", item)
	}
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
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

The worker finishes each item's sub-steps atomically. It only checks for cancellation between items, never in the middle of one. This ensures data consistency -- no partially processed items.

## Step 4 -- Progress Reporting

Build a worker that reports progress through a channel, letting the caller monitor how far along it is:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

type Progress struct {
	ItemsProcessed int
	TotalItems     int
	CurrentItem    string
	Done           bool
	Err            error
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()

	progress := make(chan Progress)

	go func() {
		defer close(progress)

		items := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
		for i, item := range items {
			select {
			case <-ctx.Done():
				progress <- Progress{
					ItemsProcessed: i,
					TotalItems:     len(items),
					Done:           true,
					Err:            ctx.Err(),
				}
				return
			default:
			}

			progress <- Progress{
				ItemsProcessed: i,
				TotalItems:     len(items),
				CurrentItem:    item,
			}
			time.Sleep(100 * time.Millisecond)
		}

		progress <- Progress{
			ItemsProcessed: len(items),
			TotalItems:     len(items),
			Done:           true,
		}
	}()

	for p := range progress {
		if p.Done {
			if p.Err != nil {
				fmt.Printf("progress: stopped at %d/%d items (%v)\n",
					p.ItemsProcessed, p.TotalItems, p.Err)
			} else {
				fmt.Printf("progress: completed all %d/%d items\n",
					p.ItemsProcessed, p.TotalItems)
			}
			break
		}
		fmt.Printf("progress: [%d] processing %q\n", p.ItemsProcessed, p.CurrentItem)
	}
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
progress: [0] processing "alpha"
progress: [1] processing "beta"
progress: [2] processing "gamma"
progress: stopped at 3/5 items (context deadline exceeded)
```

This pattern is useful for UI progress bars, health checks, or partial result collection.

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
**Fix:** Check `ctx.Done()` at multiple points within long operations, or break them into smaller steps. A worker that only checks at the start of each iteration is effectively unresponsive to cancellation during the work phase.

### Blocking on Channel Send After Cancellation
**Wrong:**
```go
select {
case <-ctx.Done():
    results <- partialResult // blocks forever if nobody is reading results!
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
doMoreWork() // this still runs
```
**Fix:** Always `return` after handling cancellation. The `select` case does not break out of the surrounding loop or function -- you must explicitly return.

### Using default in a Select with ctx.Done() and a Channel
**Caution:** When you have both `ctx.Done()` and a work channel in a select, adding a `default` case creates a busy loop:
```go
select {
case <-ctx.Done():
    return
case job := <-jobs:
    process(job)
default:
    // THIS SPINS THE CPU when both channels are empty!
}
```
Only use `default` when you intentionally want a non-blocking check (like the top-of-loop pattern in Step 1).

## Verify What You Learned

Build a batch processor that receives a slice of 10 strings. Each takes 80ms. Use a 500ms timeout. Track which items were processed and which were skipped:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

type BatchResult struct {
	Processed []string
	Skipped   []string
	Reason    string
}

func batchProcessor(ctx context.Context, items []string) BatchResult {
	var processed []string
	for i, item := range items {
		select {
		case <-ctx.Done():
			return BatchResult{
				Processed: processed,
				Skipped:   items[i:],
				Reason:    ctx.Err().Error(),
			}
		default:
		}
		time.Sleep(80 * time.Millisecond)
		processed = append(processed, item)
	}
	return BatchResult{
		Processed: processed,
		Reason:    "completed",
	}
}

func main() {
	items := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	result := batchProcessor(ctx, items)
	fmt.Printf("Processed: %v\n", result.Processed)
	fmt.Printf("Skipped:   %v\n", result.Skipped)
	fmt.Printf("Reason:    %s\n", result.Reason)
}
```

### Verification
```bash
go run main.go
```
Expected output (approximately):
```
Processed: [a b c d e f]
Skipped:   [g h i j]
Reason:    context deadline exceeded
```

## What's Next
Continue to [08-graceful-shutdown-with-context](../08-graceful-shutdown-with-context/08-graceful-shutdown-with-context.md) to build a complete graceful shutdown system using context, signals, and WaitGroup.

## Summary
- Check `ctx.Done()` at natural checkpoints: between iterations, before starting new work
- Use `select` with `ctx.Done()` and work channels to handle both cancellation and new items
- Decide whether to finish the current item on cancellation (domain-specific decision)
- Report progress through a channel so callers can monitor long-running operations
- Always `return` after handling cancellation -- do not fall through to more work
- For multi-step items, check cancellation between items but finish sub-steps atomically
- Avoid `default` in selects with work channels -- it creates busy loops

## Reference
- [Go Blog: Pipelines](https://go.dev/blog/pipelines)
- [Go Concurrency Patterns](https://go.dev/talks/2012/concurrency.slide)
- [Package context](https://pkg.go.dev/context)
