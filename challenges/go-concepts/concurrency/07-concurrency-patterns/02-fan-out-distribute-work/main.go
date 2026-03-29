package main

// Exercise: Fan-Out -- Distribute Work
// Instructions: see 02-fan-out-distribute-work.md

import (
	"fmt"
	"sync"
	"time"
)

// Step 1: Implement basicFanOut.
// Create a buffered jobs channel, launch 3 workers that range over it,
// send 9 jobs, close the channel, and wait for all workers to finish.
func basicFanOut() {
	fmt.Println("=== Basic Fan-Out ===")
	// TODO: create jobs channel (buffered, capacity 10)
	// TODO: launch 3 workers, each reading from jobs via range
	// TODO: each worker prints its id and the job number, sleeps 50ms
	// TODO: send jobs 1..9
	// TODO: close jobs channel
	// TODO: wait for all workers
	fmt.Println()
}

// generate produces integers from start to end on a channel.
func generate(start, end int) <-chan int {
	out := make(chan int)
	go func() {
		for i := start; i <= end; i++ {
			out <- i
		}
		close(out)
	}()
	return out
}

// Step 2: Implement fanOutSquare.
// Launch numWorkers goroutines, each reading from the same input channel.
// Each worker squares the value and sends the result to the output channel.
// Use a WaitGroup to close the output channel when all workers are done.
func fanOutSquare(in <-chan int, numWorkers int) <-chan int {
	results := make(chan int, numWorkers)
	var wg sync.WaitGroup

	// TODO: launch numWorkers goroutines
	// Each worker: range over in, compute n*n, print worker id and result, send to results
	// Simulate work with time.Sleep(30 * time.Millisecond)
	_ = wg // remove once implemented

	// TODO: launch a goroutine that waits for all workers, then closes results

	return results
}

// Step 3: Implement distributionAnalysis.
// For each worker count (1, 3, 5), run 20 jobs and count how many each worker handles.
// Use a mutex to protect the counts map.
func distributionAnalysis() {
	fmt.Println("=== Distribution Analysis ===")
	const totalJobs = 20
	_ = totalJobs

	// TODO: for each numWorkers in [1, 3, 5]:
	//   - create a map[int]int to count jobs per worker
	//   - create a buffered jobs channel
	//   - launch workers that increment their count and sleep 10ms per job
	//   - send totalJobs jobs, close channel, wait
	//   - print the distribution
	fmt.Println()
}

func main() {
	fmt.Println("Exercise: Fan-Out -- Distribute Work\n")

	// Step 1
	basicFanOut()

	// Step 2
	fmt.Println("=== Fan-Out Pipeline ===")
	nums := generate(1, 12)
	squared := fanOutSquare(nums, 3)
	fmt.Print("Results: ")
	for r := range squared {
		fmt.Printf("%d ", r)
	}
	fmt.Println("\n")

	// Step 3
	distributionAnalysis()

	// Verify: time the pipeline with 1 worker vs 5 workers
	fmt.Println("=== Verify: Speedup ===")
	for _, nw := range []int{1, 5} {
		start := time.Now()
		results := fanOutSquare(generate(1, 20), nw)
		for range results {
		}
		fmt.Printf("  %d worker(s): %v\n", nw, time.Since(start))
	}
}
