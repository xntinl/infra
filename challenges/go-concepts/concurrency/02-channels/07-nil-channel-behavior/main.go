package main

import (
	"fmt"
	"time"
)

// ============================================================
// Step 1: Nil channel blocks forever
// ============================================================

func step1() {
	fmt.Println("--- Step 1: Nil Channel Blocks ---")

	var ch chan int // nil channel

	// Prove that receive on nil blocks by using a timeout
	select {
	case val := <-ch:
		fmt.Println("received:", val) // never happens
	case <-time.After(1 * time.Second):
		fmt.Println("nil channel: receive timed out (as expected)")
	}

	// TODO: Try the same with a send on nil channel
	// Use select + time.After to prove it blocks without deadlocking
}

// ============================================================
// Step 2: Nil channel case is skipped in select
// ============================================================

func step2() {
	fmt.Println("--- Step 2: Nil Case Skipped ---")

	var disabled chan int // nil — will be skipped
	enabled := make(chan int, 1)
	enabled <- 42

	// TODO: Write a select that tries both channels
	// The nil channel case should never trigger
	// The enabled channel should be selected
	_ = disabled // remove when used
	_ = enabled  // remove when used
}

// ============================================================
// Step 3: Merge two channels using nil-disable pattern
// ============================================================

// merge reads from a and b until both are closed.
// When one closes, it sets the variable to nil to disable that select case.
func merge(a, b <-chan int) <-chan int {
	out := make(chan int)

	go func() {
		defer close(out)

		// TODO: Loop while a != nil || b != nil
		// In select:
		//   case val, ok := <-a: if !ok, set a = nil; else send to out
		//   case val, ok := <-b: if !ok, set b = nil; else send to out
		_ = a // remove when used
		_ = b // remove when used
	}()

	return out
}

func step3() {
	fmt.Println("--- Step 3: Merge with Nil-Disable ---")

	// Create two source channels
	evens := make(chan int)
	odds := make(chan int)

	go func() {
		for _, v := range []int{2, 4, 6} {
			evens <- v
		}
		close(evens)
	}()

	go func() {
		for _, v := range []int{1, 3, 5, 7} {
			odds <- v
		}
		close(odds)
	}()

	// TODO: Merge and print all values
	merged := merge(evens, odds)
	_ = merged // replace with range loop
}

// ============================================================
// Step 4: Stateful worker with pause/resume
// ============================================================

func statefulWorker(jobs <-chan string, pause, resume <-chan struct{}, done chan<- struct{}) {
	active := jobs // start accepting jobs

	for {
		select {
		case job, ok := <-active:
			if !ok {
				fmt.Println("  Worker: jobs channel closed, exiting")
				done <- struct{}{}
				return
			}
			fmt.Println("  Worker: processing", job)
			time.Sleep(50 * time.Millisecond)

		// TODO: Handle pause — set active to nil
		case <-pause:
			fmt.Println("  Worker: PAUSED")
			// active = ???

		// TODO: Handle resume — set active back to jobs
		case <-resume:
			fmt.Println("  Worker: RESUMED")
			// active = ???
		}
	}
}

func step4() {
	fmt.Println("--- Step 4: Stateful Worker ---")

	jobs := make(chan string, 10)
	pauseCh := make(chan struct{})
	resumeCh := make(chan struct{})
	done := make(chan struct{})

	// Preload some jobs
	for i := 1; i <= 8; i++ {
		jobs <- fmt.Sprintf("job-%d", i)
	}

	go statefulWorker(jobs, pauseCh, resumeCh, done)

	// Let it process a few
	time.Sleep(150 * time.Millisecond)

	// Pause
	pauseCh <- struct{}{}
	fmt.Println("Main: sent pause signal")
	time.Sleep(200 * time.Millisecond)

	// Resume
	resumeCh <- struct{}{}
	fmt.Println("Main: sent resume signal")
	time.Sleep(200 * time.Millisecond)

	// Close jobs to exit
	close(jobs)
	<-done
}

// ============================================================
// Final Challenge: Priority Merger
//
// Two channels: highPriority and lowPriority
// Merge both into output, using nil-disable when one closes
// Feed 3 high-priority and 5 low-priority messages
// Print with priority labels
// ============================================================

func priorityMerge(high, low <-chan string) <-chan string {
	out := make(chan string)

	go func() {
		defer close(out)

		// TODO: Merge high and low using the nil-disable pattern
		// When a value comes from high, prefix with "[HIGH] "
		// When a value comes from low, prefix with "[LOW] "
		// Set channel to nil when it closes
		// Exit when both are nil
		_ = high // remove when used
		_ = low  // remove when used
	}()

	return out
}

func finalChallenge() {
	fmt.Println("--- Final: Priority Merger ---")

	high := make(chan string)
	low := make(chan string)

	// Feed high-priority messages
	go func() {
		for _, msg := range []string{"alert", "critical", "urgent"} {
			high <- msg
			time.Sleep(30 * time.Millisecond)
		}
		close(high)
	}()

	// Feed low-priority messages
	go func() {
		for _, msg := range []string{"info-1", "info-2", "info-3", "info-4", "info-5"} {
			low <- msg
			time.Sleep(20 * time.Millisecond)
		}
		close(low)
	}()

	// TODO: Consume and print all merged messages
	merged := priorityMerge(high, low)
	_ = merged // replace with range loop
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

	finalChallenge()
}
