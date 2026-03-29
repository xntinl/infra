package main

import (
	"fmt"
	"time"
)

// ============================================================
// Step 1: The fragile sleep version
// Run this and observe that worker 3 never prints "done".
// ============================================================

func fragileVersion() {
	fmt.Println("--- Fragile Sleep Version ---")

	worker := func(id int) {
		fmt.Printf("Worker %d: starting\n", id)
		time.Sleep(time.Duration(id*100) * time.Millisecond)
		fmt.Printf("Worker %d: done\n", id)
	}

	for i := 1; i <= 3; i++ {
		go worker(i)
	}

	// This only waits 200ms, but worker 3 needs 300ms
	time.Sleep(200 * time.Millisecond)
	fmt.Println("main: exiting (some workers lost!)")
}

// ============================================================
// Step 2: Convert to done channel (chan bool)
// Fix fragileVersion by using a done channel.
// ============================================================

func doneChannelVersion() {
	fmt.Println("--- Done Channel Version ---")

	// TODO: Create a done channel of type chan bool

	workerWithSignal := func(id int, done chan bool) {
		fmt.Printf("Worker %d: starting\n", id)
		time.Sleep(time.Duration(id*100) * time.Millisecond)
		fmt.Printf("Worker %d: done\n", id)
		// TODO: Signal completion on the done channel
	}

	for i := 1; i <= 3; i++ {
		// TODO: Launch goroutine with workerWithSignal
		_ = workerWithSignal // remove when used
	}

	// TODO: Wait for all 3 workers by receiving 3 times from done
	fmt.Println("main: all workers completed")
}

// ============================================================
// Step 3: Use chan struct{} for signaling
// Refactor to use struct{} instead of bool.
// ============================================================

func structChannelVersion() {
	fmt.Println("--- Struct Channel Version ---")

	// TODO: Create done channel as chan struct{}

	// TODO: Launch 3 goroutines, each signaling done <- struct{}{}

	// TODO: Wait for all 3

	fmt.Println("main: all workers completed (struct version)")
}

// ============================================================
// Step 4: Waiting for N goroutines
// ============================================================

func waitForN(n int) {
	fmt.Printf("--- Waiting for %d Workers ---\n", n)

	// TODO: Create done channel

	// TODO: Launch n goroutines, each:
	//   - Prints "Worker <id>: processing"
	//   - Sleeps for id*50 ms
	//   - Prints "Worker <id>: finished"
	//   - Signals done

	// TODO: Receive n times from done

	fmt.Println("All workers completed")
	_ = n // remove when used
}

// ============================================================
// Final Challenge: reliableProcessor
// Launch 5 goroutines with variable work durations.
// Wait for ALL to complete using channels (no time.Sleep).
// Print total elapsed time — should be ~= slowest worker.
// ============================================================

func reliableProcessor() {
	fmt.Println("--- Reliable Processor ---")
	start := time.Now()

	// TODO: Create a done channel

	// TODO: Launch 5 goroutines where goroutine i sleeps for i*200ms
	// Each should print: "Task <i>: working for <duration>"
	// And when done: "Task <i>: complete"

	// TODO: Wait for all 5 goroutines

	elapsed := time.Since(start)
	fmt.Printf("Total time: %v (should be ~1s, not ~3s)\n", elapsed.Round(time.Millisecond*100))
}

func main() {
	step1 := true
	step2 := false // set to true as you progress
	step3 := false
	step4 := false
	final := false

	if step1 {
		fragileVersion()
		fmt.Println()
	}

	if step2 {
		doneChannelVersion()
		fmt.Println()
	}

	if step3 {
		structChannelVersion()
		fmt.Println()
	}

	if step4 {
		waitForN(5)
		fmt.Println()
	}

	if final {
		reliableProcessor()
	}
}
