// Exercise 02 — Select with Default
//
// Demonstrates the default case: non-blocking receives, non-blocking sends,
// polling patterns, and the CPU cost of tight spin loops.
//
// Expected output (approximate):
//
//   === Example 1: Non-blocking receive ===
//   no message available (channel empty)
//   received: hello
//
//   === Example 2: Non-blocking send ===
//   sent 1 successfully
//   channel full, value 2 dropped
//   buffered value: 1
//
//   === Example 3: Polling loop ===
//   no data yet, doing work... (iteration 0)
//   no data yet, doing work... (iteration 1)
//   got: data ready
//
//   === Example 4: Try-receive from multiple channels ===
//   nothing ready on any channel
//   (after sends)
//   api: response-200
//
//   === Example 5: Non-blocking multi-channel probe ===
//   events drained: 3

package main

import (
	"fmt"
	"time"
)

func main() {
	// ---------------------------------------------------------------
	// Example 1: Non-blocking receive.
	// default makes select return immediately if no channel is ready.
	// This turns a blocking receive into a "try receive".
	// ---------------------------------------------------------------
	fmt.Println("=== Example 1: Non-blocking receive ===")

	ch := make(chan string, 1)

	// Channel is empty — default runs immediately.
	select {
	case msg := <-ch:
		fmt.Println("received:", msg)
	default:
		fmt.Println("no message available (channel empty)")
	}

	// Put a value in the channel, then try again.
	ch <- "hello"

	select {
	case msg := <-ch:
		fmt.Println("received:", msg)
	default:
		fmt.Println("no message available (channel empty)")
	}

	// ---------------------------------------------------------------
	// Example 2: Non-blocking send.
	// Attempt to send without blocking. If the buffer is full,
	// the value is dropped gracefully instead of deadlocking.
	// This is the "fire and forget" pattern for non-critical data.
	// ---------------------------------------------------------------
	fmt.Println("\n=== Example 2: Non-blocking send ===")

	nums := make(chan int, 1)

	// First send succeeds — buffer has capacity.
	select {
	case nums <- 1:
		fmt.Println("sent 1 successfully")
	default:
		fmt.Println("channel full, value 1 dropped")
	}

	// Second send drops — buffer is already full.
	select {
	case nums <- 2:
		fmt.Println("sent 2 successfully")
	default:
		fmt.Println("channel full, value 2 dropped")
	}

	fmt.Println("buffered value:", <-nums)

	// ---------------------------------------------------------------
	// Example 3: Polling pattern.
	// A goroutine delivers data after a delay. The main goroutine
	// polls with select+default, doing useful work between checks.
	// ---------------------------------------------------------------
	fmt.Println("\n=== Example 3: Polling loop ===")

	messages := make(chan string, 1)

	go func() {
		time.Sleep(200 * time.Millisecond)
		messages <- "data ready"
	}()

	for i := 0; i < 5; i++ {
		select {
		case msg := <-messages:
			fmt.Println("got:", msg)
			goto pollDone // Exit the polling loop
		default:
			// No data yet — do useful work instead of blocking.
			fmt.Printf("no data yet, doing work... (iteration %d)\n", i)
			time.Sleep(100 * time.Millisecond)
		}
	}
	fmt.Println("gave up waiting")
pollDone:

	// ---------------------------------------------------------------
	// Example 4: Try-receive from multiple channels.
	// Combine default with multiple channel cases to probe several
	// channels without blocking on any of them.
	// ---------------------------------------------------------------
	fmt.Println("\n=== Example 4: Try-receive from multiple channels ===")

	apiCh := make(chan string, 1)
	dbCh := make(chan string, 1)
	cacheCh := make(chan string, 1)

	// All empty — default fires.
	select {
	case msg := <-apiCh:
		fmt.Println("api:", msg)
	case msg := <-dbCh:
		fmt.Println("db:", msg)
	case msg := <-cacheCh:
		fmt.Println("cache:", msg)
	default:
		fmt.Println("nothing ready on any channel")
	}

	// Now send on one and try again.
	apiCh <- "response-200"

	select {
	case msg := <-apiCh:
		fmt.Println("api:", msg)
	case msg := <-dbCh:
		fmt.Println("db:", msg)
	case msg := <-cacheCh:
		fmt.Println("cache:", msg)
	default:
		fmt.Println("nothing ready on any channel")
	}

	// ---------------------------------------------------------------
	// Example 5: Draining a channel without blocking.
	// Use select+default in a loop to consume all buffered values
	// and stop as soon as the channel is empty.
	// ---------------------------------------------------------------
	fmt.Println("\n=== Example 5: Non-blocking multi-channel probe ===")

	events := make(chan string, 10)
	events <- "click"
	events <- "scroll"
	events <- "keypress"

	drained := 0
	for {
		select {
		case <-events:
			drained++
		default:
			// Channel empty — exit the drain loop.
			goto drainDone
		}
	}
drainDone:
	fmt.Println("events drained:", drained)
}
