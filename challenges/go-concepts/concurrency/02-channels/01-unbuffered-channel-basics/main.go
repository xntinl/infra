package main

import (
	"fmt"
	"time"
)

// This program demonstrates unbuffered channel fundamentals through 5 progressive examples.
// Run: go run main.go
//
// Expected output:
//   === Example 1: Basic Send and Receive ===
//   Received: hello from goroutine
//
//   === Example 2: Send Blocks Until Receive ===
//   goroutine: about to send (will block here)
//   main: about to receive (after 500ms delay)
//   main: received "data"
//   goroutine: send completed (unblocked by receiver)
//
//   === Example 3: Multiple Value Exchange ===
//   Received: 10
//   Received: 20
//   Received: 30
//
//   === Example 4: Type Safety — Channels Are Typed ===
//   Point received: {3 4}
//   Error received: something went wrong
//
//   === Example 5: Multiple Channels Converging ===
//   Sum of evens (2+4+6) and odds (1+3+5): 21

func main() {
	example1BasicSendReceive()
	example2SendBlocksUntilReceive()
	example3MultipleSends()
	example4TypeSafety()
	example5MultipleChannels()
}

// example1BasicSendReceive shows the simplest channel usage: one goroutine sends
// a value, the main goroutine receives it. The receive blocks until the send happens.
func example1BasicSendReceive() {
	fmt.Println("=== Example 1: Basic Send and Receive ===")

	// make(chan T) creates an unbuffered channel that transports values of type T.
	// Unbuffered means capacity is zero: no internal storage.
	messages := make(chan string)

	// Launch a goroutine that sends a single value.
	// The arrow points INTO the channel: data flows from right to left.
	go func() {
		messages <- "hello from goroutine"
	}()

	// Receive blocks until the goroutine completes its send.
	// The arrow points OUT of the channel: data flows from channel to variable.
	msg := <-messages
	fmt.Println("Received:", msg)
	fmt.Println()
}

// example2SendBlocksUntilReceive proves that an unbuffered send blocks the sender
// goroutine until a receiver is ready. This is the "rendezvous" property: both
// sides must arrive at the channel operation simultaneously.
func example2SendBlocksUntilReceive() {
	fmt.Println("=== Example 2: Send Blocks Until Receive ===")

	messages := make(chan string)

	go func() {
		fmt.Println("goroutine: about to send (will block here)")
		// This send blocks because main has not called <-messages yet.
		// The goroutine is suspended here for ~500ms until main receives.
		messages <- "data"
		// This only prints AFTER main's receive unblocks us.
		fmt.Println("goroutine: send completed (unblocked by receiver)")
	}()

	// Simulate main being busy. The goroutine is blocked on send this whole time.
	time.Sleep(500 * time.Millisecond)
	fmt.Println("main: about to receive (after 500ms delay)")

	val := <-messages
	fmt.Printf("main: received %q\n", val)

	// Small sleep to let the goroutine's final print execute before we move on.
	time.Sleep(50 * time.Millisecond)
	fmt.Println()
}

// example3MultipleSends demonstrates that each send/receive pair is an independent
// synchronization point. Three values flow through the same channel, one at a time.
func example3MultipleSends() {
	fmt.Println("=== Example 3: Multiple Value Exchange ===")

	ch := make(chan int)

	// The sender goroutine sends three values sequentially.
	// Each send blocks until the corresponding receive happens in main.
	go func() {
		ch <- 10
		ch <- 20
		ch <- 30
	}()

	// Each receive unblocks the sender, allowing it to proceed to the next send.
	// Values arrive in FIFO order: 10, then 20, then 30.
	for i := 0; i < 3; i++ {
		val := <-ch
		fmt.Println("Received:", val)
	}
	fmt.Println()
}

// example4TypeSafety shows that channels are strongly typed. You can create channels
// for any type: structs, errors, slices, even other channels (covered in exercise 08).
func example4TypeSafety() {
	fmt.Println("=== Example 4: Type Safety -- Channels Are Typed ===")

	type Point struct{ X, Y int }

	// A channel of Point values -- only Point can be sent/received.
	pointCh := make(chan Point)
	go func() {
		pointCh <- Point{3, 4}
	}()
	p := <-pointCh
	fmt.Println("Point received:", p)

	// A channel of error values -- useful for reporting failures from goroutines.
	errCh := make(chan error)
	go func() {
		errCh <- fmt.Errorf("something went wrong")
	}()
	err := <-errCh
	fmt.Println("Error received:", err)
	fmt.Println()
}

// example5MultipleChannels shows how multiple goroutines can feed separate channels
// that converge in main. Each channel is an independent communication path.
func example5MultipleChannels() {
	fmt.Println("=== Example 5: Multiple Channels Converging ===")

	evens := make(chan int)
	odds := make(chan int)

	// Goroutine 1 sends even numbers on its own channel.
	go func() {
		for _, v := range []int{2, 4, 6} {
			evens <- v
		}
	}()

	// Goroutine 2 sends odd numbers on its own channel.
	go func() {
		for _, v := range []int{1, 3, 5} {
			odds <- v
		}
	}()

	// Main receives all 6 values. We know exactly how many to expect,
	// so we receive 3 from each channel. Order within a channel is guaranteed (FIFO),
	// but the interleaving between channels is not.
	sum := 0
	for i := 0; i < 3; i++ {
		sum += <-evens
	}
	for i := 0; i < 3; i++ {
		sum += <-odds
	}
	fmt.Printf("Sum of evens (2+4+6) and odds (1+3+5): %d\n", sum)
}
