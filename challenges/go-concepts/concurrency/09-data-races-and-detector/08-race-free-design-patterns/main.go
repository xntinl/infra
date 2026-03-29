package main

// Expected output:
//
//   === Race-Free Design Patterns ===
//   The best race fix is making races impossible by design.
//
//   --- Pattern 1: Confinement ---
//   Each goroutine owns its data exclusively. No sharing, no races.
//     Worker 0: sum of [1 2 3] = 6
//     Worker 1: sum of [4 5 6] = 15
//     Worker 2: sum of [7 8 9] = 24
//     Worker 3: sum of [10 11 12] = 33
//   Total: 78 (expected 78) -- CORRECT
//
//   --- Pattern 2: Immutability ---
//   Pass copies (by value) so goroutines cannot interfere.
//     worker-0: 0 * 10 = 0
//     worker-1: 1 * 10 = 10
//     worker-2: 2 * 10 = 20
//     worker-3: 3 * 10 = 30
//     worker-4: 4 * 10 = 40
//   (order may vary)
//
//   --- Pattern 3: Communication (Pipeline) ---
//   Data flows through channels. No shared variables between stages.
//     1 -> 1
//     2 -> 4
//     3 -> 9
//     4 -> 16
//     5 -> 25
//
//   --- Pattern 4: Combined (All Three) ---
//     Task 1: sum([1 2 3]) = 6
//     Task 2: sum([4 5 6]) = 15
//     Task 3: sum([7 8 9]) = 24
//     Task 4: sum([10 11 12]) = 33
//   Total: 78 (expected 78)
//
//   --- Pattern 5: Fan-Out / Fan-In ---
//   Distribute work across workers, merge results through single channel.
//     worker processed: 1 -> 2
//     worker processed: 2 -> 4
//     ...
//   Processed 10 items with 3 workers.
//
//   Verify: go run -race main.go
//   Expected: ZERO race warnings. No mutexes or atomics needed.

import (
	"fmt"
	"sync"
)

// --- Pattern 1: Confinement ---

// Config is an immutable configuration passed by value to goroutines.
type Config struct {
	WorkerID   int
	Multiplier int
	Label      string
}

// confinementPattern divides data into non-overlapping chunks. Each goroutine
// works exclusively on its own chunk. Since no goroutine touches another's
// data, races are structurally impossible.
func confinementPattern() {
	fmt.Println("--- Pattern 1: Confinement ---")
	fmt.Println("Each goroutine owns its data exclusively. No sharing, no races.")

	data := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	numWorkers := 4
	chunkSize := len(data) / numWorkers

	type workerResult struct {
		workerID int
		chunk    []int
		sum      int
	}

	results := make(chan workerResult, numWorkers)

	for w := 0; w < numWorkers; w++ {
		start := w * chunkSize
		end := start + chunkSize
		if w == numWorkers-1 {
			end = len(data) // last worker handles remainder
		}

		// Each goroutine gets its own sub-slice. The sub-slices do NOT overlap.
		// This is confinement: exclusive ownership eliminates concurrent access.
		chunk := data[start:end]
		go func(id int, myData []int) {
			sum := 0
			for _, v := range myData {
				sum += v
			}
			results <- workerResult{workerID: id, chunk: myData, sum: sum}
		}(w, chunk)
	}

	total := 0
	for w := 0; w < numWorkers; w++ {
		r := <-results
		fmt.Printf("  Worker %d: sum of %v = %d\n", r.workerID, r.chunk, r.sum)
		total += r.sum
	}

	status := "CORRECT"
	if total != 78 {
		status = "WRONG"
	}
	fmt.Printf("Total: %d (expected 78) -- %s\n\n", total, status)
}

// --- Pattern 2: Immutability ---

// immutabilityPattern passes Config structs by value. Each goroutine gets
// its own copy, so modifications in one goroutine cannot affect another.
// Even if a goroutine mutates its copy, it is invisible to all others.
func immutabilityPattern() {
	fmt.Println("--- Pattern 2: Immutability ---")
	fmt.Println("Pass copies (by value) so goroutines cannot interfere.")

	baseConfig := Config{Multiplier: 10, Label: "worker"}
	results := make(chan string, 5)

	for i := 0; i < 5; i++ {
		// VALUE COPY: cfg is a new struct, independent of baseConfig.
		cfg := baseConfig
		cfg.WorkerID = i
		cfg.Label = fmt.Sprintf("worker-%d", i)

		// Passed by value again: the goroutine's c is a double-copy.
		// Even if it modified c.Multiplier, no other goroutine would see it.
		go func(c Config) {
			result := fmt.Sprintf("  %s: %d * %d = %d",
				c.Label, c.WorkerID, c.Multiplier, c.WorkerID*c.Multiplier)
			results <- result
		}(cfg)
	}

	for i := 0; i < 5; i++ {
		fmt.Println(<-results)
	}
	fmt.Println()
}

// --- Pattern 3: Communication (Pipeline) ---

// communicationPattern builds a three-stage pipeline where data flows
// exclusively through channels. No goroutine accesses another's state.
// Each stage is self-contained: its only inputs and outputs are channels.
func communicationPattern() {
	fmt.Println("--- Pattern 3: Communication (Pipeline) ---")
	fmt.Println("Data flows through channels. No shared variables between stages.")

	// Stage 1: generate numbers 1..5.
	generate := func() <-chan int {
		out := make(chan int)
		go func() {
			defer close(out)
			for i := 1; i <= 5; i++ {
				out <- i
			}
		}()
		return out
	}

	// Stage 2: square each number.
	square := func(in <-chan int) <-chan [2]int {
		out := make(chan [2]int)
		go func() {
			defer close(out)
			for n := range in {
				out <- [2]int{n, n * n}
			}
		}()
		return out
	}

	// Connect and consume the pipeline.
	numbers := generate()
	squared := square(numbers)

	for pair := range squared {
		fmt.Printf("  %d -> %d\n", pair[0], pair[1])
	}
	fmt.Println()
}

// --- Pattern 4: Combined (All Three) ---

// combinedPattern demonstrates all three patterns working together:
//   - Immutability: each task is a value-copied struct
//   - Confinement: each goroutine works only on its own task
//   - Communication: results flow through a channel
func combinedPattern() {
	fmt.Println("--- Pattern 4: Combined (All Three) ---")

	type Task struct {
		ID    int
		Input []int
	}

	type Result struct {
		TaskID int
		Input  []int
		Sum    int
	}

	// Immutability: task values are copied when passed to goroutines.
	tasks := []Task{
		{ID: 1, Input: []int{1, 2, 3}},
		{ID: 2, Input: []int{4, 5, 6}},
		{ID: 3, Input: []int{7, 8, 9}},
		{ID: 4, Input: []int{10, 11, 12}},
	}

	// Communication: single channel for collecting results.
	resultCh := make(chan Result, len(tasks))

	var wg sync.WaitGroup
	for _, t := range tasks {
		wg.Add(1)
		task := t // value copy (immutability)

		// Confinement: each goroutine works exclusively on myTask.
		go func(myTask Task) {
			defer wg.Done()
			sum := 0
			for _, v := range myTask.Input {
				sum += v
			}
			// Communication: send result, don't share state.
			resultCh <- Result{TaskID: myTask.ID, Input: myTask.Input, Sum: sum}
		}(task)
	}

	// Close channel after all workers finish.
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	totalSum := 0
	for r := range resultCh {
		fmt.Printf("  Task %d: sum(%v) = %d\n", r.TaskID, r.Input, r.Sum)
		totalSum += r.Sum
	}
	fmt.Printf("Total: %d (expected 78)\n\n", totalSum)
}

// --- Pattern 5: Fan-Out / Fan-In ---

// fanOutFanIn distributes work across multiple workers (fan-out) and
// collects results through a single channel (fan-in). Each worker is
// confined to its own computation; coordination happens via channels.
func fanOutFanIn() {
	fmt.Println("--- Pattern 5: Fan-Out / Fan-In ---")
	fmt.Println("Distribute work across workers, merge results through single channel.")

	jobs := make(chan int, 10)
	results := make(chan string, 10)

	numWorkers := 3

	// Fan-out: launch workers that read from the shared jobs channel.
	// Each job is consumed by exactly one worker (channel guarantees this).
	var wg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				// Each worker processes its job independently (confinement).
				result := fmt.Sprintf("  worker processed: %d -> %d", job, job*2)
				results <- result
			}
		}()
	}

	// Send jobs.
	totalJobs := 10
	go func() {
		for i := 1; i <= totalJobs; i++ {
			jobs <- i
		}
		close(jobs)
	}()

	// Fan-in: close results after all workers finish.
	go func() {
		wg.Wait()
		close(results)
	}()

	// Consume results.
	count := 0
	for r := range results {
		fmt.Println(r)
		count++
	}
	fmt.Printf("Processed %d items with %d workers.\n\n", count, numWorkers)
}

func main() {
	fmt.Println("=== Race-Free Design Patterns ===")
	fmt.Println("The best race fix is making races impossible by design.")
	fmt.Println()

	confinementPattern()
	immutabilityPattern()
	communicationPattern()
	combinedPattern()
	fanOutFanIn()

	fmt.Println("Verify: go run -race main.go")
	fmt.Println("Expected: ZERO race warnings. No mutexes or atomics needed.")
}
