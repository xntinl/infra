package main

import (
	"fmt"
	"time"
)

func main() {
	// Step 1: Demonstrate random selection.
	// Fill two buffered channels with 100 messages each.
	// Use a flat select in a loop to consume 100 messages.
	// Count how many came from each channel.
	high := make(chan string, 100)
	low := make(chan string, 100)

	for i := 0; i < 100; i++ {
		high <- "high"
		low <- "low"
	}

	highCount, lowCount := 0, 0

	// TODO: loop 100 times with a select on high and low
	// Increment the appropriate counter for each case

	fmt.Printf("flat select -- high: %d, low: %d\n", highCount, lowCount)

	fmt.Println("---")

	// Step 2: Nested select trick for priority.
	// Refill both channels.
	high2 := make(chan string, 100)
	low2 := make(chan string, 100)

	for i := 0; i < 100; i++ {
		high2 <- "high"
		low2 <- "low"
	}

	highCount2, lowCount2 := 0, 0

	// TODO: loop 200 times using the nested select pattern:
	// outer select: try <-high2, default: inner select on both
	// Count from each channel.

	fmt.Printf("priority select -- high: %d, low: %d\n", highCount2, lowCount2)

	fmt.Println("---")

	// Step 3: Priority with live producers.
	// Launch a high-priority producer (5 messages, 50ms apart)
	// and a low-priority producer (20 messages, 10ms apart).
	// Use the nested select pattern to process messages.
	highCh := make(chan string, 10)
	lowCh := make(chan string, 10)
	done := make(chan struct{})

	go func() {
		for i := 0; i < 5; i++ {
			highCh <- fmt.Sprintf("URGENT-%d", i)
			time.Sleep(50 * time.Millisecond)
		}
	}()

	go func() {
		for i := 0; i < 20; i++ {
			lowCh <- fmt.Sprintf("normal-%d", i)
			time.Sleep(10 * time.Millisecond)
		}
		close(done)
	}()

	// TODO: for loop with nested select pattern
	// Outer: try highCh with default
	// Inner: select on highCh, lowCh, and done
	// Print "[HIGH]" or "[LOW]" prefix for each message
	// Return when done is closed

	_ = highCh
	_ = lowCh
	_ = done
}
