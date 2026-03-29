package main

import (
	"fmt"
	"time"
)

// ============================================================
// Step 1: Basic buffered channel — send without a receiver
// ============================================================

func step1() {
	fmt.Println("--- Step 1: Buffered Send/Receive ---")

	// TODO: Create a buffered channel of int with capacity 3
	// ch := make(chan int, ???)

	// TODO: Send 10, 20, 30 (none of these block)

	// TODO: Receive and print all three values (FIFO order)
}

// ============================================================
// Step 2: Blocking when buffer is full
// ============================================================

func step2() {
	fmt.Println("--- Step 2: Full Buffer Blocks ---")

	ch := make(chan int, 2)
	ch <- 1
	ch <- 2
	fmt.Printf("Buffer full, len: %d cap: %d\n", len(ch), cap(ch))

	// This goroutine will make room after 500ms
	go func() {
		time.Sleep(500 * time.Millisecond)
		val := <-ch
		fmt.Println("Received:", val, "-- made room in buffer")
	}()

	fmt.Println("Sending 3rd value (will block until space available)...")
	// TODO: Send a 3rd value — this will block until the goroutine receives
	fmt.Println("3rd value sent!")
}

// ============================================================
// Step 3: Inspecting with len() and cap()
// ============================================================

func step3() {
	fmt.Println("--- Step 3: len() and cap() ---")

	ch := make(chan string, 5)
	// TODO: Print len and cap when empty

	ch <- "a"
	ch <- "b"
	// TODO: Print len and cap after sending 2 values

	<-ch
	// TODO: Print len and cap after receiving 1 value
}

// ============================================================
// Step 4: Compare unbuffered vs buffered timing
// ============================================================

func compareUnbuffered() {
	fmt.Println("  Unbuffered:")
	ch := make(chan int)
	start := time.Now()

	go func() {
		for i := 1; i <= 5; i++ {
			ch <- i
			fmt.Printf("    Sent %d at %v\n", i, time.Since(start).Round(time.Millisecond))
		}
	}()

	for i := 0; i < 5; i++ {
		time.Sleep(100 * time.Millisecond) // consumer is slow
		<-ch
	}
	fmt.Printf("  Total: %v\n", time.Since(start).Round(time.Millisecond))
}

func compareBuffered() {
	fmt.Println("  Buffered (cap=5):")
	// TODO: Create a buffered channel with capacity 5
	start := time.Now()

	// TODO: Launch goroutine that sends 5 values (same as above)
	// The sends should complete almost instantly since buffer has room

	// TODO: Consume with 100ms delays (same as above)
	// Print total time — sends were faster because they didn't wait

	_ = start // remove when used
}

func step4() {
	fmt.Println("--- Step 4: Unbuffered vs Buffered ---")
	compareUnbuffered()
	fmt.Println()
	compareBuffered()
}

// ============================================================
// Final Challenge: Producer-Consumer with buffer monitoring
// - Producer sends 1..10 to a buffered channel (cap 3)
// - After each send, print buffer len
// - Consumer receives and prints each number
// - After each receive, print buffer len
// - Add small sleeps to make timing visible
// ============================================================

func producerConsumer() {
	fmt.Println("--- Final: Producer-Consumer ---")

	// TODO: Create buffered channel of int, capacity 3
	// TODO: Create done channel for synchronization

	// Producer goroutine
	// TODO: Send 1..10, print "Produced: <n>, buffer: <len>/<cap>"
	//       Sleep 50ms between sends to simulate production time

	// Consumer goroutine
	// TODO: Receive 10 values, print "Consumed: <n>, buffer: <len>/<cap>"
	//       Sleep 150ms between receives to simulate consumption time
	//       (consumer is 3x slower than producer, so buffer fills up)

	// TODO: Wait for both producer and consumer to finish
	fmt.Println("All items processed")
}

func main() {
	step1()
	fmt.Println()

	step2()
	fmt.Println()

	step3()
	fmt.Println()

	step4()
	fmt.Println()

	producerConsumer()
}
