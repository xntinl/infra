// Exercise 06 — Done Channel Pattern
//
// Demonstrates cancellation via close: single goroutine cancellation,
// broadcast to multiple goroutines, and pipeline cancellation propagation.
//
// Expected output (approximate):
//
//   === Example 1: Single goroutine cancellation ===
//   received: 0
//   received: 1
//   received: 2
//   received: 3
//   received: 4
//   worker: received cancellation
//   main: worker stopped
//
//   === Example 2: Broadcast cancellation to 3 workers ===
//   worker 1: working
//   worker 2: working
//   worker 3: working
//   ...
//   main: cancelling all workers
//   worker 2: stopping
//   worker 1: stopping
//   worker 3: stopping
//   main: all workers stopped
//
//   === Example 3: Pipeline cancellation ===
//   consumed: 0
//   consumed: 2
//   consumed: 4
//   consumed: 6
//   consumed: 8
//   stage1: cancelled
//   stage2: cancelled
//   pipeline shut down cleanly
//
//   === Example 4: Done channel with cleanup ===
//   worker: processing item 0
//   ...
//   worker: flushing 5 buffered items
//   worker: cleanup complete

package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	// ---------------------------------------------------------------
	// Example 1: Single goroutine cancellation.
	// The done channel is a chan struct{} — it carries no data,
	// only a signal. Closing it unblocks all receivers at once.
	// ---------------------------------------------------------------
	fmt.Println("=== Example 1: Single goroutine cancellation ===")

	done := make(chan struct{})
	results := make(chan int)

	go func() {
		defer close(results)
		i := 0
		for {
			select {
			case <-done:
				// close(done) makes this case succeed for every receiver.
				fmt.Println("worker: received cancellation")
				return
			case results <- i:
				i++
				time.Sleep(50 * time.Millisecond)
			}
		}
	}()

	// Consume exactly 5 values, then signal cancellation.
	for i := 0; i < 5; i++ {
		fmt.Println("received:", <-results)
	}

	close(done)
	time.Sleep(100 * time.Millisecond) // Let the worker print its message.
	fmt.Println("main: worker stopped")

	// ---------------------------------------------------------------
	// Example 2: Broadcast cancellation to multiple goroutines.
	// One close(done) stops ALL goroutines — no need to track each one.
	// WaitGroup ensures main waits for all workers to finish cleanup.
	// ---------------------------------------------------------------
	fmt.Println("\n=== Example 2: Broadcast cancellation to 3 workers ===")

	done2 := make(chan struct{})
	var wg sync.WaitGroup

	worker := func(id int) {
		defer wg.Done()
		for {
			select {
			case <-done2:
				fmt.Printf("worker %d: stopping\n", id)
				return
			default:
				fmt.Printf("worker %d: working\n", id)
				time.Sleep(100 * time.Millisecond)
			}
		}
	}

	for i := 1; i <= 3; i++ {
		wg.Add(1)
		go worker(i)
	}

	time.Sleep(350 * time.Millisecond)
	fmt.Println("main: cancelling all workers")
	close(done2) // Broadcast: all three workers see this simultaneously.
	wg.Wait()
	fmt.Println("main: all workers stopped")

	// ---------------------------------------------------------------
	// Example 3: Pipeline cancellation.
	// A two-stage pipeline where one done channel cancels both stages.
	// Each stage checks done on both read and write operations.
	// ---------------------------------------------------------------
	fmt.Println("\n=== Example 3: Pipeline cancellation ===")

	done3 := make(chan struct{})
	var wg2 sync.WaitGroup

	// Stage 1: generate incrementing numbers.
	stage1Out := make(chan int)
	wg2.Add(1)
	go func() {
		defer wg2.Done()
		defer close(stage1Out)
		i := 0
		for {
			select {
			case <-done3:
				fmt.Println("stage1: cancelled")
				return
			case stage1Out <- i:
				i++
				time.Sleep(50 * time.Millisecond)
			}
		}
	}()

	// Stage 2: double the numbers from stage 1.
	stage2Out := make(chan int)
	wg2.Add(1)
	go func() {
		defer wg2.Done()
		defer close(stage2Out)
		for {
			select {
			case <-done3:
				fmt.Println("stage2: cancelled")
				return
			case val, ok := <-stage1Out:
				if !ok {
					return // stage1 closed normally
				}
				// Also check done on the send side. Without this,
				// the goroutine could block on a write after cancellation.
				select {
				case <-done3:
					return
				case stage2Out <- val * 2:
				}
			}
		}
	}()

	// Consume 5 values from the pipeline.
	for i := 0; i < 5; i++ {
		fmt.Println("consumed:", <-stage2Out)
	}

	close(done3) // Cancel both stages at once.
	wg2.Wait()
	fmt.Println("pipeline shut down cleanly")

	// ---------------------------------------------------------------
	// Example 4: Done channel with graceful cleanup.
	// The worker drains its internal buffer before exiting.
	// This demonstrates that done is a signal, not a kill — the
	// goroutine decides how to shut down.
	// ---------------------------------------------------------------
	fmt.Println("\n=== Example 4: Done channel with cleanup ===")

	done4 := make(chan struct{})
	finished := make(chan struct{})

	go func() {
		defer close(finished)

		var buffer []int
		i := 0

		for {
			select {
			case <-done4:
				// Graceful shutdown: flush the buffer before exiting.
				fmt.Printf("worker: flushing %d buffered items\n", len(buffer))
				for _, item := range buffer {
					fmt.Printf("  flushed: %d\n", item)
				}
				fmt.Println("worker: cleanup complete")
				return
			default:
				buffer = append(buffer, i)
				fmt.Printf("worker: processing item %d\n", i)
				i++
				time.Sleep(50 * time.Millisecond)
				if i >= 5 {
					// Accumulate 5 items, then wait for cancellation.
					<-done4
					fmt.Printf("worker: flushing %d buffered items\n", len(buffer))
					for _, item := range buffer {
						fmt.Printf("  flushed: %d\n", item)
					}
					fmt.Println("worker: cleanup complete")
					return
				}
			}
		}
	}()

	time.Sleep(300 * time.Millisecond)
	close(done4)
	<-finished
}
