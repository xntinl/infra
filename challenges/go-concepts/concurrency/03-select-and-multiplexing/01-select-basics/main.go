package main

import (
	"fmt"
	"time"
)

func main() {
	// Step 1: Two channels with goroutines sending at different speeds.
	// Create a 'fast' and 'slow' channel. Launch goroutines that send
	// after different delays. Use select to receive the first available message.
	fast := make(chan string)
	slow := make(chan string)

	go func() {
		// TODO: sleep briefly, then send a message on fast
		_ = fast
	}()

	go func() {
		// TODO: sleep longer, then send a message on slow
		_ = slow
	}()

	// TODO: use select to receive from whichever channel is ready first
	// select {
	// case msg := <-fast:
	//     ...
	// case msg := <-slow:
	//     ...
	// }

	fmt.Println("---")

	// Step 2: Observe random selection when both channels are ready.
	// Use buffered channels so values are available immediately.
	ch1 := make(chan string, 1)
	ch2 := make(chan string, 1)

	ch1 <- "from ch1"
	ch2 <- "from ch2"

	// TODO: use select to receive from ch1 or ch2.
	// Run multiple times to observe random behavior.

	fmt.Println("---")

	// Step 3: Multiple select rounds to drain both channels.
	ch3 := make(chan string, 1)
	ch4 := make(chan string, 1)

	ch3 <- "alpha"
	ch4 <- "beta"

	// TODO: use a for loop with 2 iterations, each containing a select
	// that reads from ch3 or ch4. Both messages should appear.

	// Prevent unused import error
	_ = time.Now()
}
