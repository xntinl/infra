// Exercise 05 — Select in For Loop
//
// Demonstrates the for-select event loop pattern: continuous multiplexing,
// quit channels for clean shutdown, and the nil channel trick.
//
// Expected output (approximate):
//
//   === Example 1: Basic event loop with quit channel ===
//   processing: task-1
//   processing: task-2
//   processing: task-3
//   worker: shutting down
//
//   === Example 2: Multiple event sources ===
//   [ORDER] order-0
//   [ALERT] alert-0
//   [ORDER] order-1
//   ...
//   event loop stopped
//
//   === Example 3: Nil channel trick for close detection ===
//   source1: 0
//   source2: 10
//   ...
//   all sources closed
//
//   === Example 4: Labeled break to exit for-select ===
//   received: 0
//   received: 1
//   received: 2
//   done signal received
//
//   === Example 5: Event loop with periodic maintenance ===
//   [event] item-0
//   [maintenance] checked 1 events so far
//   ...

package main

import (
	"fmt"
	"time"
)

func main() {
	// ---------------------------------------------------------------
	// Example 1: Basic event loop with work + quit channels.
	// The goroutine runs forever until the quit channel is closed.
	// close() is a broadcast: every receiver unblocks immediately.
	// ---------------------------------------------------------------
	fmt.Println("=== Example 1: Basic event loop with quit channel ===")

	work := make(chan string)
	quit := make(chan struct{})

	go func() {
		for {
			select {
			case task := <-work:
				fmt.Println("processing:", task)
			case <-quit:
				// Closed channels return the zero value immediately.
				// This makes close() the idiomatic shutdown signal.
				fmt.Println("worker: shutting down")
				return
			}
		}
	}()

	work <- "task-1"
	work <- "task-2"
	work <- "task-3"
	close(quit)
	time.Sleep(50 * time.Millisecond) // Let the goroutine print its shutdown message.

	// ---------------------------------------------------------------
	// Example 2: Multiple event sources in one event loop.
	// A single select cleanly multiplexes different event streams
	// plus a shutdown signal. Adding a new source = adding a new case.
	// ---------------------------------------------------------------
	fmt.Println("\n=== Example 2: Multiple event sources ===")

	orders := make(chan string, 5)
	alerts := make(chan string, 5)
	quit2 := make(chan struct{})

	go func() {
		for i := 0; i < 5; i++ {
			orders <- fmt.Sprintf("order-%d", i)
			time.Sleep(30 * time.Millisecond)
		}
	}()

	go func() {
		for i := 0; i < 3; i++ {
			alerts <- fmt.Sprintf("alert-%d", i)
			time.Sleep(50 * time.Millisecond)
		}
	}()

	go func() {
		time.Sleep(300 * time.Millisecond)
		close(quit2)
	}()

	for {
		select {
		case order := <-orders:
			fmt.Println("[ORDER]", order)
		case alert := <-alerts:
			fmt.Println("[ALERT]", alert)
		case <-quit2:
			fmt.Println("event loop stopped")
			goto example3
		}
	}

example3:
	// ---------------------------------------------------------------
	// Example 3: Nil channel trick for close detection.
	// When a channel closes, set it to nil. A nil channel in select
	// is never ready, so the runtime skips it. This prevents the
	// closed channel from spinning (returning zero values forever).
	// ---------------------------------------------------------------
	fmt.Println("\n=== Example 3: Nil channel trick for close detection ===")

	source1 := make(chan int)
	source2 := make(chan int)

	go func() {
		for i := 0; i < 3; i++ {
			source1 <- i
			time.Sleep(50 * time.Millisecond)
		}
		close(source1)
	}()

	go func() {
		for i := 10; i < 14; i++ {
			source2 <- i
			time.Sleep(30 * time.Millisecond)
		}
		close(source2)
	}()

	s1Done, s2Done := false, false

	for {
		select {
		case val, ok := <-source1:
			if !ok {
				// Channel closed. Set to nil so select never picks it again.
				source1 = nil
				s1Done = true
			} else {
				fmt.Println("source1:", val)
			}
		case val, ok := <-source2:
			if !ok {
				source2 = nil
				s2Done = true
			} else {
				fmt.Println("source2:", val)
			}
		}

		if s1Done && s2Done {
			fmt.Println("all sources closed")
			break
		}
	}

	// ---------------------------------------------------------------
	// Example 4: Labeled break to exit for-select.
	// A bare `break` inside select breaks out of the select, NOT the
	// for loop. Use a labeled break or return to exit the loop.
	// ---------------------------------------------------------------
	fmt.Println("\n=== Example 4: Labeled break to exit for-select ===")

	dataCh := make(chan int, 5)
	doneCh := make(chan struct{})

	go func() {
		for i := 0; i < 3; i++ {
			dataCh <- i
			time.Sleep(30 * time.Millisecond)
		}
		close(doneCh)
	}()

loop: // Label for the for loop.
	for {
		select {
		case val := <-dataCh:
			fmt.Println("received:", val)
		case <-doneCh:
			fmt.Println("done signal received")
			break loop // Breaks out of the for loop, not just the select.
		}
	}

	// ---------------------------------------------------------------
	// Example 5: Event loop with periodic maintenance using Ticker.
	// The select handles both events and periodic tasks (like flushing
	// buffers, logging stats, or running health checks).
	// ---------------------------------------------------------------
	fmt.Println("\n=== Example 5: Event loop with periodic maintenance ===")

	eventCh := make(chan string, 10)
	stopCh := make(chan struct{})

	go func() {
		for i := 0; i < 8; i++ {
			eventCh <- fmt.Sprintf("item-%d", i)
			time.Sleep(40 * time.Millisecond)
		}
		close(stopCh)
	}()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	eventCount := 0

maintenanceLoop:
	for {
		select {
		case ev := <-eventCh:
			fmt.Println("[event]", ev)
			eventCount++
		case <-ticker.C:
			// Periodic maintenance: log stats, flush buffers, etc.
			fmt.Printf("[maintenance] checked %d events so far\n", eventCount)
		case <-stopCh:
			fmt.Printf("[shutdown] total events processed: %d\n", eventCount)
			break maintenanceLoop
		}
	}
}
