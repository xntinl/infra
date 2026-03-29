// Exercise 01 — Select Basics
//
// Demonstrates the select statement: listening on multiple channels,
// receiving from the first ready channel, and observing random selection.
//
// Expected output (approximate — random selection causes ordering variation):
//
//   === Example 1: Two channels, different speeds ===
//   received: fast message (100ms)
//
//   === Example 2: Random selection when both ready ===
//   trial 0: selected from ch1   (or ch2 — varies each run)
//   trial 1: selected from ch2
//   trial 2: selected from ch1
//   ...
//   ch1 wins: ~5, ch2 wins: ~5
//
//   === Example 3: Drain both channels with select in a loop ===
//   round 0: alpha   (order varies)
//   round 1: beta
//
//   === Example 4: Select on three channels ===
//   received: sensor-temperature = 22.5
//   (whichever arrives first)
//
//   === Example 5: Select with send cases ===
//   sent to fast consumer (buffer was empty)

package main

import (
	"fmt"
	"time"
)

func main() {
	// ---------------------------------------------------------------
	// Example 1: Two channels with goroutines sending at different speeds.
	// select blocks until one case can proceed — the fastest sender wins.
	// ---------------------------------------------------------------
	fmt.Println("=== Example 1: Two channels, different speeds ===")

	fast := make(chan string)
	slow := make(chan string)

	go func() {
		time.Sleep(100 * time.Millisecond)
		fast <- "fast message (100ms)"
	}()

	go func() {
		time.Sleep(300 * time.Millisecond)
		slow <- "slow message (300ms)"
	}()

	// select evaluates all cases simultaneously and proceeds with
	// whichever channel has data ready first. The slow goroutine's
	// message is never consumed because the program moves on after
	// receiving from fast.
	select {
	case msg := <-fast:
		fmt.Println("received:", msg)
	case msg := <-slow:
		fmt.Println("received:", msg)
	}

	// ---------------------------------------------------------------
	// Example 2: Random selection when both channels are ready.
	// When multiple cases can proceed, select picks uniformly at random.
	// This prevents any single channel from monopolizing attention.
	// ---------------------------------------------------------------
	fmt.Println("\n=== Example 2: Random selection when both ready ===")

	ch1Wins, ch2Wins := 0, 0
	const trials = 10

	for t := 0; t < trials; t++ {
		// Buffered channels with a value already waiting — both cases
		// are ready the instant select evaluates them.
		ch1 := make(chan string, 1)
		ch2 := make(chan string, 1)
		ch1 <- "from ch1"
		ch2 <- "from ch2"

		select {
		case msg := <-ch1:
			fmt.Printf("  trial %d: selected %s\n", t, msg)
			ch1Wins++
		case msg := <-ch2:
			fmt.Printf("  trial %d: selected %s\n", t, msg)
			ch2Wins++
		}
	}
	fmt.Printf("ch1 wins: %d, ch2 wins: %d\n", ch1Wins, ch2Wins)

	// ---------------------------------------------------------------
	// Example 3: Drain both channels with a loop of selects.
	// The first select picks randomly; the second has only one choice.
	// ---------------------------------------------------------------
	fmt.Println("\n=== Example 3: Drain both channels with select in a loop ===")

	ch3 := make(chan string, 1)
	ch4 := make(chan string, 1)
	ch3 <- "alpha"
	ch4 <- "beta"

	for i := 0; i < 2; i++ {
		select {
		case msg := <-ch3:
			fmt.Printf("  round %d: %s\n", i, msg)
		case msg := <-ch4:
			fmt.Printf("  round %d: %s\n", i, msg)
		}
	}

	// ---------------------------------------------------------------
	// Example 4: Select on three channels — any number of cases works.
	// A realistic scenario: three sensors reporting at different rates.
	// ---------------------------------------------------------------
	fmt.Println("\n=== Example 4: Select on three channels ===")

	temperature := make(chan string)
	humidity := make(chan string)
	pressure := make(chan string)

	go func() {
		time.Sleep(80 * time.Millisecond)
		temperature <- "sensor-temperature = 22.5"
	}()
	go func() {
		time.Sleep(150 * time.Millisecond)
		humidity <- "sensor-humidity = 60%"
	}()
	go func() {
		time.Sleep(200 * time.Millisecond)
		pressure <- "sensor-pressure = 1013hPa"
	}()

	// Only the first sensor to report is consumed here.
	// In production you would wrap this in a loop (exercise 05).
	select {
	case msg := <-temperature:
		fmt.Println("received:", msg)
	case msg := <-humidity:
		fmt.Println("received:", msg)
	case msg := <-pressure:
		fmt.Println("received:", msg)
	}

	// ---------------------------------------------------------------
	// Example 5: Select with send cases.
	// select is not limited to receives — send operations are valid cases.
	// This is useful when a goroutine produces data for multiple consumers.
	// ---------------------------------------------------------------
	fmt.Println("\n=== Example 5: Select with send cases ===")

	fastConsumer := make(chan string, 1) // buffered — ready to accept
	slowConsumer := make(chan string)     // unbuffered — no one is reading

	// select tries to send "data" to whichever consumer is ready.
	// fastConsumer has buffer space, so its case succeeds immediately.
	select {
	case fastConsumer <- "data":
		fmt.Println("sent to fast consumer (buffer was empty)")
	case slowConsumer <- "data":
		fmt.Println("sent to slow consumer")
	}
}
