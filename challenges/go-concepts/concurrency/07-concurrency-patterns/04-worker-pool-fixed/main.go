package main

// Exercise: Worker Pool (Fixed)
// Instructions: see 04-worker-pool-fixed.md

import (
	"fmt"
	"sync"
	"time"
)

// Step 1: Job and Result types are provided.
type Job struct {
	ID      int
	Payload int
}

type Result struct {
	Job    Job
	Output int
	Worker int
}

// Step 2: Implement worker.
// Read jobs from the jobs channel, compute Payload*Payload,
// simulate work with Sleep, and send the Result.
func worker(id int, jobs <-chan Job, results chan<- Result) {
	// TODO: range over jobs
	// TODO: sleep for (50 + job.Payload%50) milliseconds
	// TODO: compute Output = Payload * Payload
	// TODO: send Result{Job: job, Output: output, Worker: id}
	_ = id
}

// Step 3: Implement runPool.
// Launch numWorkers workers, send numJobs jobs, collect and print all results.
func runPool(numWorkers, numJobs int) {
	fmt.Printf("=== Worker Pool (%d workers, %d jobs) ===\n", numWorkers, numJobs)

	// TODO: create buffered jobs and results channels
	// TODO: launch numWorkers goroutines, each calling worker(id, jobs, results)
	// TODO: use WaitGroup + closer goroutine for results channel

	// TODO: send jobs (ID: 1..numJobs, Payload: j*10)
	// TODO: close jobs channel

	// TODO: range over results, print each one
	_ = numWorkers
	_ = numJobs

	fmt.Println()
}

// Step 4: Implement benchmarkPool.
// Run 20 jobs with different pool sizes (1, 2, 5, 10) and time each.
func benchmarkPool() {
	fmt.Println("=== Pool Performance ===")
	numJobs := 20
	_ = numJobs

	// TODO: for each pool size in [1, 2, 5, 10]:
	//   - record start time
	//   - create channels, launch workers, send jobs, collect results
	//   - print pool size, elapsed time, and result count

	fmt.Println()
}

// Verify: Implement a pool with a custom processing function.
// Compute factorials for payloads 1-12.
func factorial(n int) int {
	if n <= 1 {
		return 1
	}
	result := 1
	for i := 2; i <= n; i++ {
		result *= i
	}
	return result
}

func customPool() {
	fmt.Println("=== Verify: Custom Processing Pool ===")
	// TODO: create a worker pool where the processing function is factorial
	// TODO: send jobs with Payloads 1..12
	// TODO: print results and verify against known factorial values
	fmt.Println()
}

func main() {
	fmt.Println("Exercise: Worker Pool (Fixed)\n")

	// Step 2: single worker test
	fmt.Println("=== Single Worker Test ===")
	jobs := make(chan Job, 3)
	results := make(chan Result, 3)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		worker(1, jobs, results)
	}()
	jobs <- Job{ID: 1, Payload: 5}
	jobs <- Job{ID: 2, Payload: 10}
	jobs <- Job{ID: 3, Payload: 15}
	close(jobs)
	wg.Wait()
	close(results)
	for r := range results {
		fmt.Printf("  Job %d: %d -> %d\n", r.Job.ID, r.Job.Payload, r.Output)
	}
	fmt.Println()

	// Step 3
	runPool(3, 10)

	// Step 4
	benchmarkPool()

	// Verify
	customPool()

	_ = time.Millisecond // hint: use for benchmarking
}
