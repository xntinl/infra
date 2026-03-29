package main

import (
	"fmt"
	"time"
)

// This program demonstrates replacing fragile time.Sleep with channel synchronization.
// Run: go run main.go
//
// Expected output:
//   === Example 1: Fragile Sleep (Worker 3 Lost) ===
//   Worker 1: starting
//   Worker 2: starting
//   Worker 3: starting
//   Worker 1: done
//   Worker 2: done
//   main: exiting (worker 3 lost!)
//
//   === Example 2: Done Channel (All Workers Complete) ===
//   Worker 1: starting
//   Worker 2: starting
//   Worker 3: starting
//   Worker 1: done
//   Worker 2: done
//   Worker 3: done
//   main: all workers completed
//
//   === Example 3: Signal Without Data (chan struct{}) ===
//   Worker 1: finished task
//   Worker 2: finished task
//   Worker 3: finished task
//   all 3 workers confirmed done
//
//   === Example 4: Collecting Results Through Channels ===
//   Worker 1 result: data-from-1
//   Worker 2 result: data-from-2
//   Worker 3 result: data-from-3
//
//   === Example 5: Reliable Processor (Elapsed ~ Slowest Worker) ===
//   Task 1: working for 200ms
//   Task 2: working for 400ms
//   Task 3: working for 600ms
//   Task 4: working for 800ms
//   Task 5: working for 1000ms
//   ...
//   Total time: ~1s (not ~3s)

func main() {
	example1FragileSleep()
	example2DoneChannel()
	example3SignalWithStruct()
	example4CollectResults()
	example5ReliableProcessor()
}

// example1FragileSleep shows how time.Sleep is a guess, not a guarantee.
// Worker 3 needs 300ms but main only waits 200ms, so its "done" message is lost.
func example1FragileSleep() {
	fmt.Println("=== Example 1: Fragile Sleep (Worker 3 Lost) ===")

	worker := func(id int) {
		fmt.Printf("Worker %d: starting\n", id)
		time.Sleep(time.Duration(id*100) * time.Millisecond)
		fmt.Printf("Worker %d: done\n", id)
	}

	for i := 1; i <= 3; i++ {
		go worker(i)
	}

	// This only waits 200ms. Worker 3 needs 300ms, so its output is lost.
	// In production, this means lost data, incomplete operations, or silent failures.
	time.Sleep(200 * time.Millisecond)
	fmt.Println("main: exiting (worker 3 lost!)")
	fmt.Println()
}

// example2DoneChannel replaces time.Sleep with a deterministic done channel.
// Each worker signals completion. Main receives exactly N signals, one per worker.
// This guarantees ALL workers finish before main proceeds, regardless of timing.
func example2DoneChannel() {
	fmt.Println("=== Example 2: Done Channel (All Workers Complete) ===")

	done := make(chan bool)

	worker := func(id int) {
		fmt.Printf("Worker %d: starting\n", id)
		time.Sleep(time.Duration(id*100) * time.Millisecond)
		fmt.Printf("Worker %d: done\n", id)
		// Signal completion. The value doesn't matter -- we just need the synchronization.
		done <- true
	}

	for i := 1; i <= 3; i++ {
		go worker(i)
	}

	// Receive once per worker. This blocks until ALL three have sent.
	// It doesn't matter if a worker takes 1ms or 10 seconds -- we wait exactly as long as needed.
	for i := 0; i < 3; i++ {
		<-done
	}
	fmt.Println("main: all workers completed")
	fmt.Println()
}

// example3SignalWithStruct uses chan struct{} instead of chan bool for pure signaling.
// struct{} is zero bytes -- it communicates intent: "this channel carries no data,
// only synchronization." This is the idiomatic Go convention for done/quit channels.
func example3SignalWithStruct() {
	fmt.Println("=== Example 3: Signal Without Data (chan struct{}) ===")

	done := make(chan struct{})

	for i := 1; i <= 3; i++ {
		go func(id int) {
			time.Sleep(time.Duration(id*50) * time.Millisecond)
			fmt.Printf("Worker %d: finished task\n", id)
			// struct{}{} is the zero-size value. It carries no information --
			// the synchronization itself IS the message.
			done <- struct{}{}
		}(i)
	}

	for i := 0; i < 3; i++ {
		<-done
	}
	fmt.Println("all 3 workers confirmed done")
	fmt.Println()
}

// example4CollectResults shows a more practical pattern: goroutines send actual
// results back through a channel, not just completion signals. This combines
// synchronization with data transfer.
func example4CollectResults() {
	fmt.Println("=== Example 4: Collecting Results Through Channels ===")

	type Result struct {
		WorkerID int
		Data     string
	}

	results := make(chan Result)

	for i := 1; i <= 3; i++ {
		go func(id int) {
			// Simulate work that produces a result.
			time.Sleep(time.Duration(id*50) * time.Millisecond)
			results <- Result{
				WorkerID: id,
				Data:     fmt.Sprintf("data-from-%d", id),
			}
		}(i)
	}

	// Collect all results. Each receive both synchronizes AND transfers data.
	for i := 0; i < 3; i++ {
		r := <-results
		fmt.Printf("Worker %d result: %s\n", r.WorkerID, r.Data)
	}
	fmt.Println()
}

// example5ReliableProcessor launches N goroutines with variable work times and
// waits for ALL to complete. Total elapsed time equals the slowest worker (parallel),
// not the sum of all workers (sequential).
func example5ReliableProcessor() {
	fmt.Println("=== Example 5: Reliable Processor (Elapsed ~ Slowest Worker) ===")

	start := time.Now()
	done := make(chan struct{})
	numTasks := 5

	for i := 1; i <= numTasks; i++ {
		go func(id int) {
			duration := time.Duration(id*200) * time.Millisecond
			fmt.Printf("Task %d: working for %v\n", id, duration)
			time.Sleep(duration)
			fmt.Printf("Task %d: complete\n", id)
			done <- struct{}{}
		}(i)
	}

	// Wait for all tasks. The total time is ~1s (the slowest task),
	// NOT ~3s (sum of 200+400+600+800+1000ms) because they run concurrently.
	for i := 0; i < numTasks; i++ {
		<-done
	}

	elapsed := time.Since(start).Round(100 * time.Millisecond)
	fmt.Printf("Total time: %v (parallel -- not the sum of all durations)\n", elapsed)
}
