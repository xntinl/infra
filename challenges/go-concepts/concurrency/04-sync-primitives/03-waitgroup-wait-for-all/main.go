// Exercise 03: sync.WaitGroup -- Wait for All Goroutines
//
// Demonstrates reliable goroutine synchronization replacing time.Sleep.
// Covers: Add/Done/Wait, correct Add placement, batch Add, dynamic work.
//
// Expected output (approximate):
//
//   === 1. With time.Sleep (fragile) ===
//   Worker 0 finished (took 100ms)
//   ...
//   Slept for 600ms -- but did all workers finish? Maybe. Maybe not.
//
//   === 2. With WaitGroup (reliable) ===
//   Worker 0 finished (took 100ms)
//   ...
//   All workers done in 500ms (exactly as long as the slowest worker)
//
//   === 3. Correct: Add Before Go ===
//   Task "fetch-users" completed
//   Task "fetch-orders" completed
//   Task "fetch-products" completed
//   All 3 tasks completed.
//
//   === 4. Batch Add ===
//   Results: [0 1 4 9 16 25 36 49 64 81]
//
//   === 5. Dynamic Work (URLs) ===
//   Fetched: https://api.example.com/users
//   ...
//   All 4 URLs fetched.
//
//   === 6. Parallel Sum ===
//   Sequential sum: 499999500000
//   Parallel sum:   499999500000
//   Results match!
//
// Run: go run main.go

package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	withSleep()
	withWaitGroup()
	addBeforeGo()
	batchAdd()
	dynamicWork()
	parallelSum()
}

// withSleep uses time.Sleep to wait for goroutines -- fragile and unreliable.
// If workers take longer than expected, they get killed when main exits.
// If workers finish early, we waste time sleeping.
func withSleep() {
	fmt.Println("=== 1. With time.Sleep (fragile) ===")
	start := time.Now()

	for i := 0; i < 5; i++ {
		go func(id int) {
			duration := time.Duration(100*(id+1)) * time.Millisecond
			time.Sleep(duration)
			fmt.Printf("Worker %d finished (took %v)\n", id, duration)
		}(i)
	}

	// How long should we sleep? Too little = miss workers, too much = waste time.
	// This is inherently fragile because we are guessing.
	time.Sleep(600 * time.Millisecond)
	fmt.Printf("Slept for %v -- but did all workers finish? Maybe. Maybe not.\n", time.Since(start).Round(time.Millisecond))
	fmt.Println()
}

// withWaitGroup uses sync.WaitGroup for reliable synchronization.
// The counter tracks how many goroutines are still running.
// Wait() blocks until the counter reaches zero -- no guessing required.
func withWaitGroup() {
	fmt.Println("=== 2. With WaitGroup (reliable) ===")
	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < 5; i++ {
		wg.Add(1) // increment counter BEFORE launching the goroutine
		go func(id int) {
			defer wg.Done() // decrement counter when goroutine finishes
			duration := time.Duration(100*(id+1)) * time.Millisecond
			time.Sleep(duration)
			fmt.Printf("Worker %d finished (took %v)\n", id, duration)
		}(i)
	}

	wg.Wait() // blocks until counter == 0
	fmt.Printf("All workers done in %v (exactly as long as the slowest worker)\n", time.Since(start).Round(time.Millisecond))
	fmt.Println()
}

// addBeforeGo demonstrates the critical rule: Add MUST be called before the go statement.
// If Add is called inside the goroutine, the main goroutine might reach Wait() before
// the goroutine has called Add, causing Wait to return immediately with work still running.
func addBeforeGo() {
	fmt.Println("=== 3. Correct: Add Before Go ===")
	var wg sync.WaitGroup

	tasks := []string{"fetch-users", "fetch-orders", "fetch-products"}

	for _, task := range tasks {
		wg.Add(1) // CORRECT: Add in the launching goroutine, before go
		go func(name string) {
			defer wg.Done()
			time.Sleep(50 * time.Millisecond) // simulate work
			fmt.Printf("Task %q completed\n", name)
		}(task)
	}

	wg.Wait()
	fmt.Printf("All %d tasks completed.\n\n", len(tasks))
}

// batchAdd demonstrates calling Add once with the total count.
// When you know the number of goroutines upfront, one Add(n) is cleaner
// than calling Add(1) in every loop iteration.
func batchAdd() {
	fmt.Println("=== 4. Batch Add ===")

	const numWorkers = 10
	var wg sync.WaitGroup
	results := make([]int, numWorkers)

	wg.Add(numWorkers) // add all at once since the count is known
	for i := 0; i < numWorkers; i++ {
		go func(id int) {
			defer wg.Done()
			// Each goroutine writes to its own unique index.
			// No mutex needed because there is no shared write target.
			results[id] = id * id
		}(i)
	}

	wg.Wait()
	fmt.Printf("Results: %v\n\n", results)
}

// dynamicWork processes a list of URLs where the count is determined at runtime.
// WaitGroup handles any number of goroutines -- you do not need to know the count
// at compile time.
func dynamicWork() {
	fmt.Println("=== 5. Dynamic Work (URLs) ===")
	var wg sync.WaitGroup

	urls := []string{
		"https://api.example.com/users",
		"https://api.example.com/orders",
		"https://api.example.com/products",
		"https://api.example.com/inventory",
	}

	for _, url := range urls {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			// Simulate HTTP request with varying latency
			time.Sleep(time.Duration(50+len(u)) * time.Millisecond)
			fmt.Printf("Fetched: %s\n", u)
		}(url)
	}

	wg.Wait()
	fmt.Printf("All %d URLs fetched.\n\n", len(urls))
}

// parallelSum splits a large slice across goroutines for parallel computation.
// Each goroutine writes its partial sum to a unique index in the results slice,
// then the main goroutine combines partial sums after Wait.
func parallelSum() {
	fmt.Println("=== 6. Parallel Sum ===")

	// Build a slice of 1,000,000 integers
	const size = 1_000_000
	numbers := make([]int, size)
	for i := range numbers {
		numbers[i] = i
	}

	// Sequential sum for verification
	sequentialSum := int64(0)
	for _, n := range numbers {
		sequentialSum += int64(n)
	}

	// Parallel sum: split into 10 chunks
	const numWorkers = 10
	chunkSize := size / numWorkers
	partialSums := make([]int64, numWorkers)
	var wg sync.WaitGroup

	wg.Add(numWorkers)
	for i := 0; i < numWorkers; i++ {
		go func(workerID int) {
			defer wg.Done()
			start := workerID * chunkSize
			end := start + chunkSize

			var sum int64
			for _, n := range numbers[start:end] {
				sum += int64(n)
			}
			partialSums[workerID] = sum // unique index, no mutex needed
		}(i)
	}

	wg.Wait()

	// Combine partial sums
	parallelTotal := int64(0)
	for _, s := range partialSums {
		parallelTotal += s
	}

	fmt.Printf("Sequential sum: %d\n", sequentialSum)
	fmt.Printf("Parallel sum:   %d\n", parallelTotal)
	if sequentialSum == parallelTotal {
		fmt.Println("Results match!")
	} else {
		fmt.Println("BUG: results do not match!")
	}
}
