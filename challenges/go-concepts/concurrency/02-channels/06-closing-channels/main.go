package main

import (
	"fmt"
	"time"
)

// ============================================================
// Step 1: Zero-value reads after close
// ============================================================

func step1() {
	fmt.Println("--- Step 1: Zero-Value Reads After Close ---")

	ch := make(chan int, 3)
	ch <- 10
	ch <- 20
	close(ch)

	// TODO: Receive 4 times and print each value.
	// First two should be 10 and 20 (real values).
	// Last two should be 0 (zero value — channel closed and empty).
}

// ============================================================
// Step 2: Comma-ok idiom
// ============================================================

func step2() {
	fmt.Println("--- Step 2: Comma-Ok Idiom ---")

	ch := make(chan int, 2)
	ch <- 0 // real zero value!
	close(ch)

	// TODO: Use two-value receive: val, ok := <-ch
	// First read:  val=0, ok=true  (real value that happens to be 0)
	// Second read: val=0, ok=false (channel closed, this is zero-value)
	// Print both val and ok to see the difference
}

// ============================================================
// Step 3: Broadcasting with close
// ============================================================

func step3() {
	fmt.Println("--- Step 3: Broadcast via Close ---")

	quit := make(chan struct{})

	// Launch 5 workers that wait for shutdown signal
	for i := 0; i < 5; i++ {
		go func(id int) {
			// TODO: Block on <-quit, then print shutdown message
			fmt.Printf("Worker %d: received shutdown signal\n", id)
			_ = quit // remove when used
		}(i)
	}

	time.Sleep(100 * time.Millisecond) // let workers start

	fmt.Println("Broadcasting shutdown...")
	// TODO: Close quit to unblock ALL workers simultaneously

	time.Sleep(100 * time.Millisecond) // let workers print
}

// ============================================================
// Step 4: Panic — send on closed channel
// Uncomment to observe, then comment back.
// ============================================================

func step4PanicSend() {
	fmt.Println("--- Step 4: Send on Closed Channel (panic) ---")
	fmt.Println("(uncomment code to observe panic)")

	// ch := make(chan int)
	// close(ch)
	// ch <- 42 // panic: send on closed channel
}

// ============================================================
// Step 5: Panic — double close
// Uncomment to observe, then comment back.
// ============================================================

func step5PanicClose() {
	fmt.Println("--- Step 5: Double Close (panic) ---")
	fmt.Println("(uncomment code to observe panic)")

	// ch := make(chan int)
	// close(ch)
	// close(ch) // panic: close of closed channel
}

// ============================================================
// Final Challenge: Task Dispatcher with Graceful Shutdown
//
// - 3 workers run in loops, processing tasks
// - Workers check both the tasks channel and a quit channel
// - Main sends 10 tasks, then broadcasts shutdown via close(quit)
// - Workers finish current task, print shutdown message, signal done
// - Main waits for all 3 workers to exit
// ============================================================

func finalChallenge() {
	fmt.Println("--- Final: Task Dispatcher with Shutdown ---")

	tasks := make(chan string, 5)
	quit := make(chan struct{})
	done := make(chan struct{})

	// Worker function
	worker := func(id int) {
		defer func() { done <- struct{}{} }()

		for {
			// TODO: Use select (or comma-ok on tasks channel) to:
			// - Process tasks when available
			// - Exit when quit is closed
			//
			// Hint with comma-ok approach:
			//   select {
			//   case task, ok := <-tasks:
			//       if !ok { /* tasks channel closed */ }
			//       fmt.Printf("Worker %d: processing %s\n", id, task)
			//   case <-quit:
			//       fmt.Printf("Worker %d: shutting down\n", id)
			//       return
			//   }

			_ = id    // remove when used
			_ = tasks // remove when used
			_ = quit  // remove when used
			return    // remove — placeholder to prevent infinite loop
		}
	}

	// Launch 3 workers
	for i := 1; i <= 3; i++ {
		go worker(i)
	}

	// Send 10 tasks
	for i := 1; i <= 10; i++ {
		tasks <- fmt.Sprintf("task-%d", i)
	}

	// Give workers time to process some tasks
	time.Sleep(100 * time.Millisecond)

	// Broadcast shutdown
	fmt.Println("Sending shutdown signal...")
	close(quit)

	// Wait for all 3 workers to exit
	for i := 0; i < 3; i++ {
		<-done
	}
	fmt.Println("All workers shut down cleanly")
}

func main() {
	step1()
	fmt.Println()

	step2()
	fmt.Println()

	step3()
	fmt.Println()

	step4PanicSend()
	fmt.Println()

	step5PanicClose()
	fmt.Println()

	finalChallenge()
}
