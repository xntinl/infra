package main

import (
	"fmt"
	"time"
)

// This program demonstrates buffered channel behavior through 5 progressive examples.
// Run: go run main.go
//
// Expected output:
//   === Example 1: Basic Buffered Channel ===
//   10 (first in, first out)
//   20
//   30
//
//   === Example 2: Buffer Full Blocks Sender ===
//   Buffer state: len=2, cap=2 (full)
//   Sending 3rd value (will block)...
//   Drained one: 1 -- made room
//   3rd value sent successfully!
//
//   === Example 3: Inspecting len() and cap() ===
//   Empty:      len=0  cap=5
//   After 2:    len=2  cap=5
//   After recv: len=1  cap=5
//   After 3:    len=4  cap=5
//   Full:       len=5  cap=5
//
//   === Example 4: Unbuffered vs Buffered Timing ===
//   ...
//
//   === Example 5: Producer-Consumer with Buffer Monitoring ===
//   ...

func main() {
	example1BasicBuffered()
	example2FullBufferBlocks()
	example3LenAndCap()
	example4TimingComparison()
	example5ProducerConsumer()
}

// example1BasicBuffered shows that a buffered channel can hold values without
// a receiver being ready. This is the key difference from unbuffered channels.
func example1BasicBuffered() {
	fmt.Println("=== Example 1: Basic Buffered Channel ===")

	// make(chan T, capacity) creates a channel with an internal queue of size 'capacity'.
	// Sends don't block until the queue is full.
	ch := make(chan int, 3)

	// All three sends succeed immediately -- no goroutine needed as receiver.
	// With an unbuffered channel, these would deadlock.
	ch <- 10
	ch <- 20
	ch <- 30

	// Receives drain the buffer in FIFO order.
	fmt.Println(<-ch, "(first in, first out)")
	fmt.Println(<-ch)
	fmt.Println(<-ch)
	fmt.Println()
}

// example2FullBufferBlocks demonstrates that when the buffer is full,
// a send blocks until a receiver drains at least one value.
func example2FullBufferBlocks() {
	fmt.Println("=== Example 2: Buffer Full Blocks Sender ===")

	ch := make(chan int, 2)
	ch <- 1
	ch <- 2
	fmt.Printf("Buffer state: len=%d, cap=%d (full)\n", len(ch), cap(ch))

	// This goroutine will drain one value after 500ms, making room.
	go func() {
		time.Sleep(500 * time.Millisecond)
		val := <-ch
		fmt.Printf("Drained one: %d -- made room\n", val)
	}()

	// This send blocks because buffer is full. It unblocks when the goroutine receives.
	fmt.Println("Sending 3rd value (will block)...")
	ch <- 3
	fmt.Println("3rd value sent successfully!")
	fmt.Println()
}

// example3LenAndCap shows how to inspect a buffered channel's state.
// len(ch) = number of values currently queued. cap(ch) = total capacity.
// WARNING: These are snapshots. The values can change between checking and acting.
// Never use them for synchronization decisions.
func example3LenAndCap() {
	fmt.Println("=== Example 3: Inspecting len() and cap() ===")

	ch := make(chan string, 5)
	fmt.Printf("Empty:      len=%d  cap=%d\n", len(ch), cap(ch))

	ch <- "a"
	ch <- "b"
	fmt.Printf("After 2:    len=%d  cap=%d\n", len(ch), cap(ch))

	<-ch // drain one
	fmt.Printf("After recv: len=%d  cap=%d\n", len(ch), cap(ch))

	ch <- "c"
	ch <- "d"
	ch <- "e"
	fmt.Printf("After 3:    len=%d  cap=%d\n", len(ch), cap(ch))

	ch <- "f"
	fmt.Printf("Full:       len=%d  cap=%d\n", len(ch), cap(ch))

	// Drain remaining to avoid leak.
	for len(ch) > 0 {
		<-ch
	}
	fmt.Println()
}

// example4TimingComparison contrasts send timing between unbuffered and buffered channels
// when the consumer is slower than the producer. Buffered channels let the producer
// "drop off" values and continue, while unbuffered forces synchronous handoffs.
func example4TimingComparison() {
	fmt.Println("=== Example 4: Unbuffered vs Buffered Timing ===")

	// --- Unbuffered: producer waits for each receive ---
	fmt.Println("  Unbuffered (producer waits each time):")
	unbuffered := make(chan int)
	start := time.Now()

	go func() {
		for i := 1; i <= 5; i++ {
			unbuffered <- i
			fmt.Printf("    Sent %d at +%v\n", i, time.Since(start).Round(time.Millisecond))
		}
	}()

	for i := 0; i < 5; i++ {
		time.Sleep(100 * time.Millisecond) // slow consumer
		<-unbuffered
	}
	fmt.Printf("  Unbuffered total: %v\n\n", time.Since(start).Round(time.Millisecond))

	// --- Buffered: producer sends all 5 almost instantly ---
	fmt.Println("  Buffered (cap=5, producer sends instantly):")
	buffered := make(chan int, 5)
	start = time.Now()

	go func() {
		for i := 1; i <= 5; i++ {
			buffered <- i
			fmt.Printf("    Sent %d at +%v\n", i, time.Since(start).Round(time.Millisecond))
		}
	}()

	// Give the producer a moment to fill the buffer.
	time.Sleep(10 * time.Millisecond)

	for i := 0; i < 5; i++ {
		time.Sleep(100 * time.Millisecond) // slow consumer
		<-buffered
	}
	fmt.Printf("  Buffered total: %v\n", time.Since(start).Round(time.Millisecond))
	fmt.Println()
}

// example5ProducerConsumer builds a realistic producer-consumer system.
// The producer is 3x faster than the consumer, so the buffer fills up and
// the producer eventually blocks. Watch the buffer occupancy rise and fall.
func example5ProducerConsumer() {
	fmt.Println("=== Example 5: Producer-Consumer with Buffer Monitoring ===")

	ch := make(chan int, 3)
	done := make(chan struct{})

	// Producer: sends 1..10, faster than the consumer.
	go func() {
		for i := 1; i <= 10; i++ {
			ch <- i
			fmt.Printf("  Produced: %2d | buffer: %d/%d\n", i, len(ch), cap(ch))
			time.Sleep(50 * time.Millisecond)
		}
		close(ch)
	}()

	// Consumer: processes items 3x slower than the producer, causing buffer buildup.
	go func() {
		for val := range ch {
			fmt.Printf("  Consumed: %2d | buffer: %d/%d\n", val, len(ch), cap(ch))
			time.Sleep(150 * time.Millisecond)
		}
		done <- struct{}{}
	}()

	<-done
	fmt.Println("  All items processed")
}
