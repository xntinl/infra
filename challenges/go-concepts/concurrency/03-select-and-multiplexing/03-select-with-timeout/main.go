package main

import (
	"fmt"
	"time"
)

func main() {
	// Step 1: Basic timeout with time.After.
	// A goroutine simulates slow work. Use select with time.After
	// to set a 500ms deadline.
	result := make(chan string)

	go func() {
		time.Sleep(2 * time.Second) // Simulate slow operation
		result <- "done"
	}()

	// TODO: select between <-result and <-time.After(500ms)
	// Print the result or "timeout" accordingly

	fmt.Println("---")

	// Step 2: Observe the time.After leak pattern (read-only, no code to write).
	// Understand that calling time.After inside a loop creates a new timer
	// per iteration. Each timer lives until it fires, even if unused.

	// Step 3: Safe timeout with time.NewTimer in a loop.
	// A producer sends 10 values with 50ms gaps.
	// Use time.NewTimer to set a per-iteration timeout of 500ms.
	ch := make(chan int, 1)

	go func() {
		for i := 0; i < 10; i++ {
			ch <- i
			time.Sleep(50 * time.Millisecond)
		}
		close(ch)
	}()

	// TODO: create a time.NewTimer with 500ms duration
	// TODO: defer timer.Stop()

	// TODO: loop with for { ... }
	//   1. Stop the timer. If Stop returns false, drain timer.C
	//   2. Reset the timer to 500ms
	//   3. select between:
	//      - receiving from ch (check ok for closed channel)
	//      - timeout from timer.C

	fmt.Println("exercise complete")
}
