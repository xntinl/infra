// Exercise 03 — Select with Timeout
//
// Demonstrates timeout patterns: time.After for one-shot timeouts,
// the timer leak problem in loops, and safe reuse with time.NewTimer.
//
// Expected output (approximate):
//
//   === Example 1: Basic timeout with time.After ===
//   timeout: operation took too long (500ms deadline exceeded)
//
//   === Example 2: Successful result before timeout ===
//   result: fast computation done
//
//   === Example 3: Demonstrating time.After leak (educational) ===
//   processed 100 items (100 leaked timers created internally)
//
//   === Example 4: Safe timeout with time.NewTimer in a loop ===
//   received: 0
//   received: 1
//   ...
//   received: 9
//   channel closed, all values received
//
//   === Example 5: NewTimer timeout fires when producer is slow ===
//   received: 0
//   timeout: no data for 200ms, stopping

package main

import (
	"fmt"
	"time"
)

func main() {
	// ---------------------------------------------------------------
	// Example 1: Basic timeout with time.After.
	// time.After returns a channel that receives a value after a delay.
	// Combined with select, it sets a deadline on any channel operation.
	// ---------------------------------------------------------------
	fmt.Println("=== Example 1: Basic timeout with time.After ===")

	result := make(chan string)

	go func() {
		time.Sleep(2 * time.Second) // Simulate slow work
		result <- "slow computation done"
	}()

	// The timeout channel fires after 500ms. Since the goroutine
	// takes 2 seconds, the timeout case wins.
	select {
	case res := <-result:
		fmt.Println("result:", res)
	case <-time.After(500 * time.Millisecond):
		fmt.Println("timeout: operation took too long (500ms deadline exceeded)")
	}

	// ---------------------------------------------------------------
	// Example 2: Successful result before timeout.
	// When the work finishes before the deadline, select picks the
	// result case. The timer from time.After still exists in memory
	// until it fires, but this is acceptable for one-shot operations.
	// ---------------------------------------------------------------
	fmt.Println("\n=== Example 2: Successful result before timeout ===")

	result2 := make(chan string)

	go func() {
		time.Sleep(50 * time.Millisecond) // Fast work
		result2 <- "fast computation done"
	}()

	select {
	case res := <-result2:
		fmt.Println("result:", res)
	case <-time.After(500 * time.Millisecond):
		fmt.Println("timeout: too slow")
	}

	// ---------------------------------------------------------------
	// Example 3: The time.After leak in loops (educational).
	// Each call to time.After allocates a new timer that persists until
	// it fires, even if the result arrived first. In a loop processing
	// thousands of items, this wastes memory on the timer heap.
	// ---------------------------------------------------------------
	fmt.Println("\n=== Example 3: Demonstrating time.After leak (educational) ===")

	leakyCh := make(chan int, 1)

	go func() {
		for i := 0; i < 100; i++ {
			leakyCh <- i
			time.Sleep(1 * time.Millisecond)
		}
		close(leakyCh)
	}()

	leakyCount := 0
	for val := range leakyCh {
		// BAD PATTERN: Every iteration creates a new timer (1 second each).
		// Since we receive data every 1ms, we accumulate ~100 timers
		// all waiting to fire 1 second from now. In a high-throughput loop,
		// this leaks significant memory.
		select {
		case <-time.After(1 * time.Second):
			fmt.Println("timeout")
		default:
			_ = val
			leakyCount++
		}
	}
	fmt.Printf("processed %d items (100 leaked timers created internally)\n", leakyCount)

	// ---------------------------------------------------------------
	// Example 4: Safe timeout with time.NewTimer in a loop.
	// A single timer is created, stopped, drained, and reset each
	// iteration. No leaks, deterministic cleanup.
	// ---------------------------------------------------------------
	fmt.Println("\n=== Example 4: Safe timeout with time.NewTimer in a loop ===")

	ch := make(chan int)

	go func() {
		for i := 0; i < 10; i++ {
			ch <- i
			time.Sleep(50 * time.Millisecond)
		}
		close(ch)
	}()

	timeout := time.NewTimer(500 * time.Millisecond)
	defer timeout.Stop() // Always stop when done — prevents timer heap leak.

	for {
		// Stop the timer before resetting. If Stop returns false,
		// the timer already fired and its channel has a pending value.
		// Drain it to prevent a stale timeout on the next iteration.
		if !timeout.Stop() {
			select {
			case <-timeout.C:
			default:
			}
		}
		timeout.Reset(500 * time.Millisecond)

		select {
		case val, ok := <-ch:
			if !ok {
				fmt.Println("channel closed, all values received")
				goto example5
			}
			fmt.Println("received:", val)
		case <-timeout.C:
			fmt.Println("timeout: no data for 500ms")
			goto example5
		}
	}

example5:
	// ---------------------------------------------------------------
	// Example 5: NewTimer timeout fires when producer is slow.
	// The producer sends one value then stalls. The timer detects
	// the gap and fires.
	// ---------------------------------------------------------------
	fmt.Println("\n=== Example 5: NewTimer timeout fires when producer is slow ===")

	slowCh := make(chan int)

	go func() {
		slowCh <- 0
		time.Sleep(50 * time.Millisecond)
		// Producer stalls here — next value takes 2 seconds.
		time.Sleep(2 * time.Second)
		slowCh <- 1
	}()

	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()

	for {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(200 * time.Millisecond)

		select {
		case val := <-slowCh:
			fmt.Println("received:", val)
		case <-timer.C:
			fmt.Println("timeout: no data for 200ms, stopping")
			return
		}
	}
}
