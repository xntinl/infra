package main

// Expected output (timing approximate):
//
// Context-Aware Long Worker
//
// === Basic Worker Loop ===
//   worker: processing item 1
//   worker: processing item 2
//   worker: processing item 3
//   worker: stopped after 3 items (context deadline exceeded)
//
// === Worker Reading from Channel ===
//   worker: processing job 1
//   worker: processing job 2
//   worker: processing job 3
//   worker: processing job 4
//   worker: processing job 5
//   main: cancelling worker
//   worker: cancelled, processed 5 items: [1 2 3 4 5]
//
// === Graceful Item Completion (finish current item) ===
//   worker: starting item 1
//     step 1/3
//     step 2/3
//     step 3/3
//   worker: item 1 complete
//   worker: starting item 2
//     step 1/3
//     step 2/3
//     step 3/3
//   worker: item 2 complete
//   worker: stopped before item 3 (context deadline exceeded)
//
// === Worker with Progress Reporting ===
//   progress: [0] processing "alpha"
//   progress: [1] processing "beta"
//   progress: [2] processing "gamma"
//   progress: stopped at 3/5 items (context deadline exceeded)
//
// === Verify Knowledge: Batch Processor ===
//   Processed: [a b c d e f]
//   Skipped:   [g h i j]
//   Reason:    context deadline exceeded

import (
	"context"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Example 1: Basic loop with context check at each iteration
// ---------------------------------------------------------------------------
// The simplest pattern: check ctx.Done() at the TOP of each loop iteration.
// This guarantees we never start a new item after cancellation. The select
// with default is non-blocking: if Done is not ready, we fall through to work.
func basicWorkerLoop() {
	fmt.Println("=== Basic Worker Loop ===")

	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()

	for i := 1; ; i++ {
		// Non-blocking check: has the context been cancelled?
		select {
		case <-ctx.Done():
			fmt.Printf("  worker: stopped after %d items (%v)\n", i-1, ctx.Err())
			fmt.Println()
			return
		default:
			// Not cancelled yet -- continue working.
		}

		fmt.Printf("  worker: processing item %d\n", i)
		time.Sleep(100 * time.Millisecond) // simulate work
	}
}

// ---------------------------------------------------------------------------
// Example 2: Select with both ctx.Done() and a work channel
// ---------------------------------------------------------------------------
// When reading from a channel, combine ctx.Done() and the job channel in the
// SAME select. The runtime picks whichever is ready first. This is the
// standard pattern for cancellable consumers.
func workerWithChannel() {
	fmt.Println("=== Worker Reading from Channel ===")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	jobs := make(chan int, 10)
	done := make(chan []int)

	// Producer: sends 20 jobs (more than the worker will process).
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
				fmt.Printf("  worker: cancelled, processed %d items: %v\n", len(processed), processed)
				done <- processed
				return
			case job, ok := <-jobs:
				if !ok {
					// Channel closed -- all jobs consumed.
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

	// Cancel after 300ms -- worker will have processed ~5 items.
	time.Sleep(300 * time.Millisecond)
	fmt.Println("  main: cancelling worker")
	cancel()

	<-done
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Example 3: Finish current item before stopping (atomic items)
// ---------------------------------------------------------------------------
// Sometimes each item has multiple sub-steps that must complete together.
// Check cancellation BETWEEN items, but once an item starts, run all its
// sub-steps to completion. This ensures data consistency.
func gracefulItemCompletion() {
	fmt.Println("=== Graceful Item Completion (finish current item) ===")

	ctx, cancel := context.WithTimeout(context.Background(), 450*time.Millisecond)
	defer cancel()

	for item := 1; ; item++ {
		// Check cancellation BEFORE starting a new item.
		select {
		case <-ctx.Done():
			fmt.Printf("  worker: stopped before item %d (%v)\n", item, ctx.Err())
			fmt.Println()
			return
		default:
		}

		fmt.Printf("  worker: starting item %d\n", item)

		// Once started, the item runs to completion regardless of cancellation.
		// This is a domain decision: we prefer completed items over partial ones.
		for step := 1; step <= 3; step++ {
			fmt.Printf("    step %d/%d\n", step, 3)
			time.Sleep(50 * time.Millisecond)
		}

		fmt.Printf("  worker: item %d complete\n", item)
	}
}

// ---------------------------------------------------------------------------
// Example 4: Worker with progress reporting
// ---------------------------------------------------------------------------
// A Progress struct is sent through a channel so the caller can monitor
// the worker's state in real time. This is useful for UI progress bars,
// health checks, or partial result collection.

// Progress represents the current state of the worker.
type Progress struct {
	ItemsProcessed int
	TotalItems     int
	CurrentItem    string
	Done           bool
	Err            error
}

func workerWithProgress() {
	fmt.Println("=== Worker with Progress Reporting ===")

	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()

	progress := make(chan Progress)

	go func() {
		defer close(progress)

		items := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
		for i, item := range items {
			// Check cancellation before each item.
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

			// Report that we are working on this item.
			progress <- Progress{
				ItemsProcessed: i,
				TotalItems:     len(items),
				CurrentItem:    item,
			}

			time.Sleep(100 * time.Millisecond)
		}

		// All items completed successfully.
		progress <- Progress{
			ItemsProcessed: len(items),
			TotalItems:     len(items),
			Done:           true,
		}
	}()

	// Consumer: read progress updates and display them.
	for p := range progress {
		if p.Done {
			if p.Err != nil {
				fmt.Printf("  progress: stopped at %d/%d items (%v)\n",
					p.ItemsProcessed, p.TotalItems, p.Err)
			} else {
				fmt.Printf("  progress: completed all %d/%d items\n",
					p.ItemsProcessed, p.TotalItems)
			}
			break
		}
		fmt.Printf("  progress: [%d] processing %q\n", p.ItemsProcessed, p.CurrentItem)
	}
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Verify: Batch processor that tracks processed vs skipped items
// ---------------------------------------------------------------------------

// BatchResult summarizes what the processor accomplished.
type BatchResult struct {
	Processed []string
	Skipped   []string
	Reason    string
}

// batchProcessor processes items sequentially, checking cancellation before each.
// It returns a summary of what was processed and what was skipped.
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

		// Simulate processing each item.
		time.Sleep(80 * time.Millisecond)
		processed = append(processed, item)
	}

	return BatchResult{
		Processed: processed,
		Skipped:   nil,
		Reason:    "completed",
	}
}

func verifyKnowledge() {
	fmt.Println("=== Verify Knowledge: Batch Processor ===")

	items := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}

	// 500ms budget for 10 items @ 80ms each = 800ms total.
	// Should process ~6 items before timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	result := batchProcessor(ctx, items)

	fmt.Printf("  Processed: %v\n", result.Processed)
	fmt.Printf("  Skipped:   %v\n", result.Skipped)
	fmt.Printf("  Reason:    %s\n", result.Reason)
}

func main() {
	fmt.Println("Context-Aware Long Worker")
	fmt.Println()

	basicWorkerLoop()
	workerWithChannel()
	gracefulItemCompletion()
	workerWithProgress()
	verifyKnowledge()
}
