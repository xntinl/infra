package main

import (
	"fmt"
	"time"
)

// This program demonstrates close() semantics: zero-value reads, comma-ok, broadcast, and panics.
// Run: go run main.go
//
// Expected output:
//   === Example 1: Zero-Value Reads After Close ===
//   Read 1: 10 (real value)
//   Read 2: 20 (real value)
//   Read 3: 0 (zero value -- channel closed and empty)
//   Read 4: 0 (zero value -- will repeat forever)
//
//   === Example 2: Comma-Ok Idiom ===
//   val=0, ok=true  -- real value that happens to be zero
//   val=0, ok=false -- zero value because channel is closed
//
//   === Example 3: Broadcast via Close ===
//   Broadcasting shutdown to 5 workers...
//   Worker 0: received shutdown signal
//   Worker 1: received shutdown signal
//   ...
//
//   === Example 4: Safe Close Patterns ===
//   Produced 5 values, channel closed safely
//
//   === Example 5: Graceful Shutdown Dispatcher ===
//   ...all workers process tasks then shut down...

func main() {
	example1ZeroValueReads()
	example2CommaOk()
	example3BroadcastClose()
	example4SafeClosePatterns()
	example5GracefulShutdown()
}

// example1ZeroValueReads shows that after a channel is closed and drained,
// every subsequent receive returns the zero value of the channel's type, forever.
// The channel never blocks again.
func example1ZeroValueReads() {
	fmt.Println("=== Example 1: Zero-Value Reads After Close ===")

	ch := make(chan int, 3)
	ch <- 10
	ch <- 20
	close(ch)

	// First two reads return buffered values.
	fmt.Printf("Read 1: %d (real value)\n", <-ch)
	fmt.Printf("Read 2: %d (real value)\n", <-ch)
	// After buffer is drained, reads return 0 (int's zero value) forever.
	fmt.Printf("Read 3: %d (zero value -- channel closed and empty)\n", <-ch)
	fmt.Printf("Read 4: %d (zero value -- will repeat forever)\n", <-ch)
	fmt.Println()
}

// example2CommaOk demonstrates how to distinguish a real zero from a "channel closed" zero.
// The two-value receive form val, ok := <-ch reports ok=false when the channel is closed.
func example2CommaOk() {
	fmt.Println("=== Example 2: Comma-Ok Idiom ===")

	ch := make(chan int, 2)
	ch <- 0 // intentionally sending zero -- a real value
	close(ch)

	// First read: val=0, ok=true. The zero is a real value sent before close.
	val, ok := <-ch
	fmt.Printf("val=%d, ok=%v  -- real value that happens to be zero\n", val, ok)

	// Second read: val=0, ok=false. The zero is the type's zero value because
	// the channel is closed and empty.
	val, ok = <-ch
	fmt.Printf("val=%d, ok=%v -- zero value because channel is closed\n", val, ok)
	fmt.Println()
}

// example3BroadcastClose demonstrates close() as a one-to-many signal.
// Sending on a channel wakes ONE receiver. Closing wakes ALL receivers simultaneously.
// This is the standard pattern for graceful shutdown.
func example3BroadcastClose() {
	fmt.Println("=== Example 3: Broadcast via Close ===")

	quit := make(chan struct{})
	done := make(chan struct{})
	numWorkers := 5

	for i := 0; i < numWorkers; i++ {
		go func(id int) {
			// Each worker blocks here until quit is closed.
			// A single close() unblocks ALL of them simultaneously.
			<-quit
			fmt.Printf("Worker %d: received shutdown signal\n", id)
			done <- struct{}{}
		}(i)
	}

	// Give workers time to start and block on <-quit.
	time.Sleep(50 * time.Millisecond)

	fmt.Printf("Broadcasting shutdown to %d workers...\n", numWorkers)
	close(quit) // all workers unblock at once

	// Wait for all workers to confirm they received the signal.
	for i := 0; i < numWorkers; i++ {
		<-done
	}
	fmt.Println()
}

// example4SafeClosePatterns shows how to structure code so only the producer closes.
// This prevents the two panic scenarios: send-on-closed and double-close.
func example4SafeClosePatterns() {
	fmt.Println("=== Example 4: Safe Close Patterns ===")

	// Pattern: producer owns the channel, closes when done.
	// Consumers only read -- they never close or send.
	ch := make(chan int)
	go func() {
		for i := 1; i <= 5; i++ {
			ch <- i
		}
		close(ch) // producer closes -- safe because only this goroutine sends
	}()

	count := 0
	for range ch {
		count++
	}
	fmt.Printf("Produced %d values, channel closed safely\n", count)

	// Panic case 1 (DO NOT run): send on closed channel
	// ch2 := make(chan int); close(ch2); ch2 <- 1  // panic: send on closed channel

	// Panic case 2 (DO NOT run): close an already-closed channel
	// ch3 := make(chan int); close(ch3); close(ch3)  // panic: close of closed channel

	fmt.Println()
}

// example5GracefulShutdown builds a realistic task dispatcher:
// - Workers process tasks from a buffered channel
// - Main broadcasts shutdown via close(quit)
// - Workers finish current work, signal done, and exit
func example5GracefulShutdown() {
	fmt.Println("=== Example 5: Graceful Shutdown Dispatcher ===")

	tasks := make(chan string, 5)
	quit := make(chan struct{})
	done := make(chan struct{})
	numWorkers := 3

	worker := func(id int) {
		defer func() { done <- struct{}{} }()

		for {
			select {
			case task, ok := <-tasks:
				if !ok {
					// tasks channel was closed -- no more work available
					fmt.Printf("Worker %d: task channel closed, exiting\n", id)
					return
				}
				fmt.Printf("Worker %d: processing %s\n", id, task)
				time.Sleep(30 * time.Millisecond) // simulate work

			case <-quit:
				fmt.Printf("Worker %d: shutdown signal received\n", id)
				return
			}
		}
	}

	for i := 1; i <= numWorkers; i++ {
		go worker(i)
	}

	// Send some tasks.
	for i := 1; i <= 10; i++ {
		tasks <- fmt.Sprintf("task-%d", i)
	}

	// Give workers time to process a few tasks.
	time.Sleep(150 * time.Millisecond)

	// Broadcast shutdown: all workers' <-quit case unblocks.
	fmt.Println("Sending shutdown signal...")
	close(quit)

	// Wait for all workers to confirm exit.
	for i := 0; i < numWorkers; i++ {
		<-done
	}
	fmt.Println("All workers shut down cleanly")
}
