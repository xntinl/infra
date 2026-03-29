package main

// Fan-Out: Distribute Work -- Complete Working Example
//
// Fan-out distributes work from a single channel to N competing workers.
// Go's channel semantics guarantee each value goes to exactly one receiver.
// The distribution is non-deterministic -- the scheduler decides who gets what.
//
// Expected output (order varies due to concurrency):
//   === Basic Fan-Out (3 workers, 9 jobs) ===
//     worker 1 processing job 1
//     worker 2 processing job 2
//     worker 3 processing job 3
//     ...all 9 jobs processed...
//
//   === Fan-Out Pipeline (3 workers) ===
//     worker 0: 1^2 = 1
//     worker 1: 2^2 = 4
//     ...
//   Results: 1 4 9 16 25 36 49 64 81 100 121 144
//
//   === Distribution Analysis ===
//     Workers: 1 -> worker 0 handled 20 jobs
//     Workers: 3 -> roughly even split
//     Workers: 5 -> roughly even split
//
//   === Speedup Comparison ===
//     1 worker(s): ~600ms
//     5 worker(s): ~120ms

import (
	"fmt"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Basic fan-out: single channel, multiple workers competing for values.
// This is the simplest form -- no pipeline, just work distribution.
//
//   jobs channel
//       |
//   +---+---+---+
//   |   |   |   |
//   w1  w2  w3  (workers compete for each value)
// ---------------------------------------------------------------------------

func basicFanOut() {
	fmt.Println("=== Basic Fan-Out (3 workers, 9 jobs) ===")

	// Buffered channel acts as a work queue.
	// Buffer lets the sender enqueue jobs without blocking on each one.
	jobs := make(chan int, 10)
	var wg sync.WaitGroup

	// Launch 3 workers. All read from the SAME channel.
	// The Go runtime ensures each value goes to exactly one goroutine.
	for w := 1; w <= 3; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// range exits when the channel is closed AND drained.
			for job := range jobs {
				fmt.Printf("  worker %d processing job %d\n", id, job)
				time.Sleep(50 * time.Millisecond) // simulate work
			}
		}(w)
	}

	// Send 9 jobs, then close to signal no more work.
	for j := 1; j <= 9; j++ {
		jobs <- j
	}
	close(jobs)

	// Wait blocks until all workers have finished.
	wg.Wait()
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Generator: produces a stream of integers on a channel.
// This is the pipeline entry point -- same pattern from exercise 01.
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Fan-out pipeline stage: N workers share a single input channel.
// Each worker reads values, squares them, and sends results to a shared
// output channel. A separate goroutine closes the output after all workers
// finish -- this is the standard WaitGroup + closer pattern.
//
//   input channel
//       |
//   +---+---+---+
//   |   |   |   |
//   w0  w1  w2      (each sends to results)
//   |   |   |
//   +---+---+---+
//       |
//   results channel
// ---------------------------------------------------------------------------

func fanOutSquare(in <-chan int, numWorkers int) <-chan int {
	results := make(chan int, numWorkers)
	var wg sync.WaitGroup

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// Multiple goroutines range over the same channel.
			// Each value is delivered to exactly one worker.
			for n := range in {
				result := n * n
				fmt.Printf("  worker %d: %d^2 = %d\n", id, n, result)
				results <- result
				time.Sleep(30 * time.Millisecond) // simulate work
			}
		}(w)
	}

	// The closer goroutine ensures results is closed only after
	// ALL workers have finished sending. Without this, ranging over
	// results would block forever or a worker would panic on send.
	go func() {
		wg.Wait()
		close(results)
	}()

	return results
}

// ---------------------------------------------------------------------------
// Distribution analysis: shows how work distributes across workers.
// With identical work duration, the distribution tends to be roughly even,
// but Go does NOT guarantee round-robin.
// ---------------------------------------------------------------------------

func distributionAnalysis() {
	fmt.Println("=== Distribution Analysis ===")
	const totalJobs = 20

	for _, numWorkers := range []int{1, 3, 5} {
		fmt.Printf("\n  Workers: %d\n", numWorkers)

		// Track how many jobs each worker handles.
		counts := make(map[int]int)
		var mu sync.Mutex
		var wg sync.WaitGroup

		jobs := make(chan int, totalJobs)
		for w := 0; w < numWorkers; w++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for range jobs {
					mu.Lock()
					counts[id]++
					mu.Unlock()
					time.Sleep(10 * time.Millisecond) // simulate work
				}
			}(w)
		}

		for j := 0; j < totalJobs; j++ {
			jobs <- j
		}
		close(jobs)
		wg.Wait()

		for id := 0; id < numWorkers; id++ {
			fmt.Printf("    worker %d handled %d jobs\n", id, counts[id])
		}
	}
	fmt.Println()
}

func main() {
	fmt.Println("Exercise: Fan-Out -- Distribute Work")
	fmt.Println()

	// Basic fan-out
	basicFanOut()

	// Fan-out as a pipeline stage
	fmt.Println("=== Fan-Out Pipeline (3 workers) ===")
	nums := generate(1, 12)
	squared := fanOutSquare(nums, 3)
	fmt.Print("Results: ")
	for r := range squared {
		fmt.Printf("%d ", r)
	}
	fmt.Println()
	fmt.Println()

	// Distribution analysis
	distributionAnalysis()

	// Speedup comparison: 1 worker vs 5 workers
	fmt.Println("=== Speedup Comparison ===")
	for _, nw := range []int{1, 5} {
		start := time.Now()
		results := fanOutSquare(generate(1, 20), nw)
		for range results {
			// drain the channel
		}
		fmt.Printf("  %d worker(s): %v\n", nw, time.Since(start))
	}
}
