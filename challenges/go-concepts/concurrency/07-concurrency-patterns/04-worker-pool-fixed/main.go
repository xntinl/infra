package main

// Worker Pool (Fixed) -- Complete Working Example
//
// A worker pool is a fixed number of goroutines that pull jobs from a shared
// queue, process them, and push results to a collection channel. It combines
// fan-out and fan-in into a structured unit with bounded concurrency.
//
// Expected output (order varies):
//   === Single Worker Test ===
//     Job 1: 5 -> 25
//     Job 2: 10 -> 100
//     Job 3: 15 -> 225
//
//   === Worker Pool (3 workers, 10 jobs) ===
//     Job 1 (payload=10) -> result=100 [worker 1]
//     Job 2 (payload=20) -> result=400 [worker 2]
//     ...
//
//   === Pool Performance ===
//      1 workers: ~1.2s (20 results)
//      2 workers: ~600ms (20 results)
//      5 workers: ~250ms (20 results)
//     10 workers: ~130ms (20 results)
//
//   === Custom Processing Pool (factorials) ===
//     Job 1: 1! = 1
//     Job 2: 2! = 2
//     ...
//     Job 12: 12! = 479001600

import (
	"fmt"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Job and Result: typed data flowing through the pool.
// Carrying the Job reference in Result gives traceability -- you always
// know which input produced which output, and which worker handled it.
// ---------------------------------------------------------------------------

type Job struct {
	ID      int
	Payload int
}

type Result struct {
	Job    Job
	Output int
	Worker int
}

// ---------------------------------------------------------------------------
// worker: the core processing goroutine.
// Reads jobs until the channel closes, computes the result, and sends it.
// The worker has no knowledge of pool size or total job count -- single
// responsibility: process one job at a time.
//
//   jobs channel ---> [worker] ---> results channel
// ---------------------------------------------------------------------------

func worker(id int, jobs <-chan Job, results chan<- Result) {
	for job := range jobs {
		// Simulate variable processing time based on payload.
		time.Sleep(time.Duration(50+job.Payload%50) * time.Millisecond)
		result := Result{
			Job:    job,
			Output: job.Payload * job.Payload,
			Worker: id,
		}
		results <- result
	}
}

// ---------------------------------------------------------------------------
// runPool: the pool orchestrator.
//
//   +--------+    +------+    +---------+
//   |producer| -> | jobs | -> | worker1 | --+
//   +--------+    | chan  | -> | worker2 | --+--> results chan --> consumer
//                 |      | -> | worker3 | --+
//                 +------+    +---------+
//
// Flow: send all jobs -> close jobs -> workers drain queue -> workers exit
// -> WaitGroup reaches zero -> closer goroutine closes results -> consumer
// finishes ranging over results.
// ---------------------------------------------------------------------------

func runPool(numWorkers, numJobs int) {
	fmt.Printf("=== Worker Pool (%d workers, %d jobs) ===\n", numWorkers, numJobs)

	// Buffer both channels to decouple producers from consumers.
	// Without buffering, sending all jobs before reading results
	// could deadlock when results backs up.
	jobs := make(chan Job, numJobs)
	results := make(chan Result, numJobs)

	// Launch the fixed pool of workers.
	var wg sync.WaitGroup
	for w := 1; w <= numWorkers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			worker(id, jobs, results)
		}(w)
	}

	// Send all jobs. This can happen synchronously because the channel
	// is buffered to hold all jobs. In production, you might send from
	// a goroutine if the source is slow or unbounded.
	for j := 1; j <= numJobs; j++ {
		jobs <- Job{ID: j, Payload: j * 10}
	}
	close(jobs) // signal workers: no more work coming

	// Closer goroutine: waits for all workers to finish, then closes results.
	// This MUST be a separate goroutine -- if you close results in main,
	// you risk closing before workers have sent their last result.
	go func() {
		wg.Wait()
		close(results)
	}()

	// Consume results. range exits when results is closed.
	for r := range results {
		fmt.Printf("  Job %d (payload=%d) -> result=%d [worker %d]\n",
			r.Job.ID, r.Job.Payload, r.Output, r.Worker)
	}
	fmt.Println()
}

// ---------------------------------------------------------------------------
// benchmarkPool: measures how pool size affects throughput.
// More workers = more parallelism = faster, up to the point where
// workers outnumber jobs or the bottleneck shifts elsewhere.
// ---------------------------------------------------------------------------

func benchmarkPool() {
	fmt.Println("=== Pool Performance ===")
	numJobs := 20

	for _, nw := range []int{1, 2, 5, 10} {
		start := time.Now()
		jobs := make(chan Job, numJobs)
		results := make(chan Result, numJobs)

		var wg sync.WaitGroup
		for w := 0; w < nw; w++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				worker(id, jobs, results)
			}(w)
		}

		for j := 0; j < numJobs; j++ {
			jobs <- Job{ID: j, Payload: j * 5}
		}
		close(jobs)

		go func() {
			wg.Wait()
			close(results)
		}()

		count := 0
		for range results {
			count++
		}

		fmt.Printf("  %2d workers: %v (%d results)\n", nw, time.Since(start), count)
	}
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Custom processing pool: demonstrates parameterizing the worker function.
// Instead of hardcoding square, pass any func(int)->int as the processor.
// ---------------------------------------------------------------------------

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

func customWorker(id int, jobs <-chan Job, results chan<- Result, process func(int) int) {
	for job := range jobs {
		time.Sleep(20 * time.Millisecond) // simulate work
		result := Result{
			Job:    job,
			Output: process(job.Payload),
			Worker: id,
		}
		results <- result
	}
}

func customPool() {
	fmt.Println("=== Custom Processing Pool (factorials) ===")

	numWorkers := 3
	numJobs := 12
	jobs := make(chan Job, numJobs)
	results := make(chan Result, numJobs)

	var wg sync.WaitGroup
	for w := 1; w <= numWorkers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			customWorker(id, jobs, results, factorial)
		}(w)
	}

	for j := 1; j <= numJobs; j++ {
		jobs <- Job{ID: j, Payload: j}
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect and sort results by Job ID for readable output.
	collected := make([]Result, 0, numJobs)
	for r := range results {
		collected = append(collected, r)
	}

	// Print in order of job ID (collected order is non-deterministic).
	resultByID := make(map[int]Result, len(collected))
	for _, r := range collected {
		resultByID[r.Job.ID] = r
	}
	for j := 1; j <= numJobs; j++ {
		r := resultByID[j]
		fmt.Printf("  Job %2d: %d! = %d [worker %d]\n",
			r.Job.ID, r.Job.Payload, r.Output, r.Worker)
	}
	fmt.Println()
}

func main() {
	fmt.Println("Exercise: Worker Pool (Fixed)")
	fmt.Println()

	// Single worker test: verify the worker function works.
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

	// Multi-worker pool
	runPool(3, 10)

	// Performance benchmark
	benchmarkPool()

	// Custom processor (factorial)
	customPool()
}
