package main

import (
	"fmt"
	"time"
)

// This program demonstrates nil channel behavior and the nil-disable pattern in select.
// Run: go run main.go
//
// Expected output:
//   === Example 1: Nil Channel Blocks Forever ===
//   receive on nil: timed out (as expected)
//   send on nil: timed out (as expected)
//
//   === Example 2: Nil Case Is Skipped in Select ===
//   backup channel selected: 99
//
//   === Example 3: Merge Two Channels with Nil-Disable ===
//   (interleaved values from evens and odds, all 7 values)
//   Merge complete: received 7 values
//
//   === Example 4: Pause/Resume Worker ===
//   ... worker processes, pauses, resumes, then exits ...
//
//   === Example 5: Priority Merger ===
//   [HIGH] alert
//   [LOW] info-1
//   ... all messages with priority labels ...

func main() {
	example1NilBlocks()
	example2NilSkippedInSelect()
	example3MergeWithNilDisable()
	example4PauseResumeWorker()
	example5PriorityMerger()
}

// example1NilBlocks proves that a nil channel blocks forever on both send and receive.
// This is not a bug -- it's the defined behavior. var ch chan T declares a nil channel.
// Only make(chan T) creates a usable (non-nil) channel.
func example1NilBlocks() {
	fmt.Println("=== Example 1: Nil Channel Blocks Forever ===")

	var ch chan int // nil -- not initialized with make()

	// Prove receive blocks by racing against a timeout.
	select {
	case val := <-ch:
		fmt.Println("received:", val) // never happens
	case <-time.After(200 * time.Millisecond):
		fmt.Println("receive on nil: timed out (as expected)")
	}

	// Prove send blocks the same way.
	select {
	case ch <- 42:
		fmt.Println("sent") // never happens
	case <-time.After(200 * time.Millisecond):
		fmt.Println("send on nil: timed out (as expected)")
	}
	fmt.Println()
}

// example2NilSkippedInSelect shows the key insight: in a select statement,
// a nil channel's case is NEVER chosen. It acts as if the case doesn't exist.
// This is what makes nil channels useful -- you can dynamically disable select cases.
func example2NilSkippedInSelect() {
	fmt.Println("=== Example 2: Nil Case Is Skipped in Select ===")

	var disabled chan int // nil -- permanently skipped in select
	backup := make(chan int, 1)
	backup <- 99

	select {
	case val := <-disabled:
		// This case is never chosen because disabled is nil.
		fmt.Println("disabled:", val)
	case val := <-backup:
		// This is the only eligible case, so it's always selected.
		fmt.Println("backup channel selected:", val)
	}
	fmt.Println()
}

// merge reads from two channels until both are closed. When one closes, it sets
// the variable to nil, which disables that select case. The loop exits when both
// channels are nil (both closed and fully drained).
func merge(a, b <-chan int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		// Loop while at least one channel is still open (non-nil).
		for a != nil || b != nil {
			select {
			case val, ok := <-a:
				if !ok {
					// Channel a is closed. Set to nil so select ignores it.
					a = nil
					continue
				}
				out <- val
			case val, ok := <-b:
				if !ok {
					b = nil
					continue
				}
				out <- val
			}
		}
		// Both nil: all sources exhausted.
	}()
	return out
}

// example3MergeWithNilDisable demonstrates the canonical nil-disable merge pattern.
func example3MergeWithNilDisable() {
	fmt.Println("=== Example 3: Merge Two Channels with Nil-Disable ===")

	evens := make(chan int)
	odds := make(chan int)

	go func() {
		for _, v := range []int{2, 4, 6} {
			evens <- v
		}
		close(evens)
	}()

	go func() {
		for _, v := range []int{1, 3, 5, 7} {
			odds <- v
		}
		close(odds)
	}()

	count := 0
	for val := range merge(evens, odds) {
		fmt.Printf("  %d\n", val)
		count++
	}
	fmt.Printf("Merge complete: received %d values\n", count)
	fmt.Println()
}

// example4PauseResumeWorker uses nil assignment to implement a state machine.
// When paused, the jobs channel variable is set to nil, disabling job processing.
// When resumed, it's set back to the original channel, re-enabling processing.
func example4PauseResumeWorker() {
	fmt.Println("=== Example 4: Pause/Resume Worker ===")

	jobs := make(chan string, 10)
	pauseCh := make(chan struct{})
	resumeCh := make(chan struct{})
	done := make(chan struct{})

	// Preload jobs.
	for i := 1; i <= 8; i++ {
		jobs <- fmt.Sprintf("job-%d", i)
	}

	go func() {
		active := jobs // start in active state
		for {
			select {
			case job, ok := <-active:
				if !ok {
					fmt.Println("  Worker: jobs channel closed, exiting")
					done <- struct{}{}
					return
				}
				fmt.Println("  Worker: processing", job)
				time.Sleep(40 * time.Millisecond)

			case <-pauseCh:
				fmt.Println("  Worker: PAUSED (jobs channel set to nil)")
				active = nil // nil disables the job case in select

			case <-resumeCh:
				fmt.Println("  Worker: RESUMED (jobs channel restored)")
				active = jobs // restore the channel to re-enable job processing
			}
		}
	}()

	// Let worker process a few jobs.
	time.Sleep(150 * time.Millisecond)

	// Pause the worker.
	pauseCh <- struct{}{}
	fmt.Println("  Main: pause sent")
	time.Sleep(200 * time.Millisecond) // worker is idle during this time

	// Resume the worker.
	resumeCh <- struct{}{}
	fmt.Println("  Main: resume sent")
	time.Sleep(200 * time.Millisecond) // let it process remaining jobs

	// Signal end by closing jobs.
	close(jobs)
	<-done
	fmt.Println()
}

// priorityMerge merges high-priority and low-priority channels, labeling each value.
func priorityMerge(high, low <-chan string) <-chan string {
	out := make(chan string)
	go func() {
		defer close(out)
		for high != nil || low != nil {
			select {
			case msg, ok := <-high:
				if !ok {
					high = nil
					continue
				}
				out <- "[HIGH] " + msg
			case msg, ok := <-low:
				if !ok {
					low = nil
					continue
				}
				out <- "[LOW] " + msg
			}
		}
	}()
	return out
}

// example5PriorityMerger shows a practical application: merging event streams with
// different priority levels. When the high-priority source closes, only low-priority
// events flow. Both must close before the merger exits.
func example5PriorityMerger() {
	fmt.Println("=== Example 5: Priority Merger ===")

	high := make(chan string)
	low := make(chan string)

	go func() {
		for _, msg := range []string{"alert", "critical", "urgent"} {
			high <- msg
			time.Sleep(30 * time.Millisecond)
		}
		close(high)
	}()

	go func() {
		for _, msg := range []string{"info-1", "info-2", "info-3", "info-4", "info-5"} {
			low <- msg
			time.Sleep(20 * time.Millisecond)
		}
		close(low)
	}()

	for msg := range priorityMerge(high, low) {
		fmt.Println(" ", msg)
	}
}
