package main

import (
	"fmt"
	"time"
)

func main() {
	// Step 1: Non-blocking receive.
	// Create a buffered channel. Use select+default to try receiving
	// when the channel is empty, then again after adding a value.
	ch := make(chan string, 1)

	// TODO: select with default -- channel is empty, should print "no message available"

	ch <- "hello"

	// TODO: select with default -- channel has data, should print "received: hello"

	fmt.Println("---")

	// Step 2: Non-blocking send.
	// Create a buffered channel with capacity 1. Send twice using
	// select+default. The second send should be dropped.
	nums := make(chan int, 1)

	// TODO: first select+default send -- should succeed
	// TODO: second select+default send -- buffer full, should drop

	// TODO: receive and print the buffered value
	_ = nums

	fmt.Println("---")

	// Step 3: Polling pattern.
	// A goroutine will send a message after a delay. Use a for loop
	// with select+default to poll the channel while doing other work.
	messages := make(chan string, 1)

	go func() {
		time.Sleep(200 * time.Millisecond)
		messages <- "data ready"
	}()

	// TODO: loop up to 5 times. In each iteration:
	//   - select: if messages has data, print it and return
	//   - default: print "working...", sleep 100ms

	fmt.Println("polling complete")
}
