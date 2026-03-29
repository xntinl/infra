package main

// Exercise: Context-Aware Long Worker
// Instructions: see 07-context-aware-long-worker.md

import (
	"context"
	"fmt"
	"time"
)

// Step 1: Implement basicWorkerLoop.
// A worker that processes numbered items in a loop.
// Checks ctx.Done() at the top of each iteration.
// Uses a 350ms timeout -- should process about 3 items.
func basicWorkerLoop() {
	fmt.Println("=== Basic Worker Loop ===")
	// TODO: ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	// TODO: defer cancel()
	// TODO: loop with counter starting at 1
	//       - select on ctx.Done() (stop) vs default (continue)
	//       - print item number, sleep 100ms
	//       - on stop: print total items processed and ctx.Err()
}

// Step 2: Implement workerWithChannel.
// A worker that reads jobs from a channel using select.
// The select handles both ctx.Done() and incoming jobs.
// Cancelled after 300ms; reports which jobs were completed.
func workerWithChannel() {
	fmt.Println("=== Worker with Channel ===")
	// TODO: ctx, cancel := context.WithCancel(context.Background())
	// TODO: defer cancel()
	// TODO: create jobs channel (buffered, capacity 10), done channel
	// TODO: producer goroutine: send jobs 1..20, close channel
	// TODO: worker goroutine:
	//       - select on ctx.Done() vs jobs channel
	//       - track processed items in a slice
	//       - on cancel or channel close: send processed slice to done
	// TODO: sleep 300ms, cancel, receive and print result
}

// Step 3: Implement gracefulItemCompletion.
// A worker where each item has 3 sub-steps (50ms each).
// Cancellation is checked between items, never mid-item.
// This ensures each started item completes fully.
func gracefulItemCompletion() {
	fmt.Println("=== Graceful Item Completion ===")
	// TODO: ctx, cancel := context.WithTimeout(context.Background(), 450*time.Millisecond)
	// TODO: defer cancel()
	// TODO: loop over items:
	//       - check ctx.Done() before starting each item
	//       - run 3 sub-steps (print each, sleep 50ms)
	//       - print item completion
}

// Step 4: Implement workerWithProgress.
// A worker that reports progress through a Progress channel.
// The caller can monitor how far the worker has gotten.

// Progress represents the current state of the worker.
type Progress struct {
	ItemsProcessed int
	CurrentItem    string
	Done           bool
	Err            error
}

func workerWithProgress() {
	fmt.Println("=== Worker with Progress ===")
	// TODO: ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	// TODO: defer cancel()
	// TODO: create progress channel
	// TODO: launch goroutine that:
	//       - iterates over items: ["alpha", "beta", "gamma", "delta", "epsilon"]
	//       - checks ctx.Done() before each item
	//       - sends Progress update for each item
	//       - sends final Progress with Done=true (and Err if cancelled)
	// TODO: read progress channel, print each update
}

// Verify: Implement batchProcessor and verifyKnowledge.
// batchProcessor processes a slice of strings (80ms each).
// It checks cancellation before each item and returns a summary.

// BatchResult summarizes what the processor accomplished.
type BatchResult struct {
	Processed []string
	Skipped   []string
	Reason    string // "completed" or the error message
}

func batchProcessor(ctx context.Context, items []string) BatchResult {
	_ = ctx   // TODO: check ctx.Done() before each item
	_ = items // TODO: process each item (sleep 80ms)
	// TODO: return BatchResult with processed, skipped, and reason
	return BatchResult{}
}

func verifyKnowledge() {
	fmt.Println("=== Verify Knowledge ===")
	items := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	_ = items
	// TODO: call batchProcessor with 500ms timeout
	// TODO: print which items were processed, which were skipped, and why
}

func main() {
	fmt.Println("Exercise: Context-Aware Long Worker\n")

	basicWorkerLoop()
	workerWithChannel()
	gracefulItemCompletion()
	workerWithProgress()
	verifyKnowledge()

	// Final pause for goroutine output
	time.Sleep(100 * time.Millisecond)
}
