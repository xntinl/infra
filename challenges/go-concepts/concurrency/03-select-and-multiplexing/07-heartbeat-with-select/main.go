package main

import (
	"fmt"
	"time"
)

// heartbeatWorker starts a worker goroutine that emits heartbeats at
// pulseInterval and sends work results. Returns read-only channels.
func heartbeatWorker(
	done <-chan struct{},
	pulseInterval time.Duration,
	work func(i int) int,
) (<-chan struct{}, <-chan int) {
	heartbeat := make(chan struct{}, 1)
	results := make(chan int)

	go func() {
		defer close(results)
		// TODO: create a time.NewTicker with pulseInterval
		// TODO: defer ticker.Stop()

		i := 0
		_ = i
		// TODO: for-select loop with three cases:
		// 1. <-done: return
		// 2. <-ticker.C: non-blocking send on heartbeat
		// 3. results <- work(i): increment i
		_ = done
		_ = heartbeat
	}()

	return heartbeat, results
}

func main() {
	// Step 1: Basic heartbeat.
	fmt.Println("=== Step 1 ===")

	done := make(chan struct{})
	heartbeatCh := make(chan struct{}, 1)
	workResults := make(chan int)

	go func() {
		defer close(workResults)
		// TODO: create ticker at 200ms intervals
		// TODO: defer ticker.Stop()

		i := 0
		_ = i
		// TODO: for-select loop:
		// case <-done: return
		// case <-ticker.C: non-blocking send on heartbeatCh (select with default)
		// case workResults <- i: increment i, sleep 100ms to simulate work
		_ = done
		_ = heartbeatCh
	}()

	// TODO: consume results and heartbeats for 1 second using time.After
	// Print "result: <val>" for work results
	// Print "heartbeat received" for heartbeats
	// Stop after timeout
	_ = workResults
	_ = heartbeatCh

	close(done)

	fmt.Println("---")

	// Step 2: Detect a stalled worker.
	fmt.Println("\n=== Step 2 ===")

	done2 := make(chan struct{})
	heartbeat2 := make(chan struct{}, 1)

	go func() {
		// TODO: create ticker at 100ms
		// TODO: for loop with counter i
		// When i == 5, print "worker: entering stall", sleep 5 seconds
		// Select: <-done2 return, <-ticker.C heartbeat send, default work (sleep 50ms)
		_ = done2
		_ = heartbeat2
	}()

	// TODO: supervisor loop
	// Create a time.NewTimer with 500ms timeout
	// select: heartbeat2 received -> reset timer
	//         timer.C fired -> print "ALERT - worker stalled!", close(done2), return
	_ = heartbeat2

	// Step 3: Reusable heartbeatWorker function.
	fmt.Println("\n=== Step 3 ===")

	done3 := make(chan struct{})

	// TODO: call heartbeatWorker with:
	//   done3, 200ms interval, func that returns i*i with 80ms sleep
	// TODO: consume results and heartbeats for 1 second

	_ = done3

	fmt.Println("exercise complete")
}
