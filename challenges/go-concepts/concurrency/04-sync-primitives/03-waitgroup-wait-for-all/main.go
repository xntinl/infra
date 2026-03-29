package main

import (
	"fmt"
	"time"
)

func main() {
	withSleep()
	withWaitGroup()
	demonstrateCorrectAdd()
	batchAdd()
	dynamicWork()
}

// withSleep uses time.Sleep to wait for goroutines -- fragile and unreliable.
func withSleep() {
	fmt.Println("=== With time.Sleep (fragile) ===")
	start := time.Now()

	for i := 0; i < 5; i++ {
		go func(id int) {
			duration := time.Duration(100*(id+1)) * time.Millisecond
			time.Sleep(duration)
			fmt.Printf("Worker %d finished (took %v)\n", id, duration)
		}(i)
	}

	// How long should we sleep? Too little = miss workers, too much = waste time
	time.Sleep(600 * time.Millisecond) // guess: longest worker takes 500ms
	fmt.Printf("Slept for %v -- but did all workers finish?\n", time.Since(start).Round(time.Millisecond))
}

// withWaitGroup uses sync.WaitGroup for reliable synchronization.
// TODO: Add `"sync"` to the import block above, then:
//   1. Declare a sync.WaitGroup
//   2. Call wg.Add(1) before each go statement
//   3. Call defer wg.Done() inside each goroutine
//   4. Replace time.Sleep with wg.Wait()
func withWaitGroup() {
	fmt.Println("\n=== With WaitGroup ===")
	start := time.Now()

	// TODO: var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		// TODO: wg.Add(1) here, BEFORE the go statement
		go func(id int) {
			// TODO: defer wg.Done()
			duration := time.Duration(100*(id+1)) * time.Millisecond
			time.Sleep(duration)
			fmt.Printf("Worker %d finished (took %v)\n", id, duration)
		}(i)
	}

	// TODO: replace this Sleep with wg.Wait()
	time.Sleep(600 * time.Millisecond)
	fmt.Printf("All workers done in %v\n", time.Since(start).Round(time.Millisecond))
}

// demonstrateCorrectAdd shows the correct pattern: Add before go.
// TODO: Use WaitGroup to wait for all tasks to complete.
func demonstrateCorrectAdd() {
	fmt.Println("\n=== Correct: Add Before Go ===")

	tasks := []string{"fetch-users", "fetch-orders", "fetch-products"}

	// TODO: declare WaitGroup
	// TODO: for each task, Add(1) then launch goroutine with Done

	for _, task := range tasks {
		go func(name string) {
			time.Sleep(50 * time.Millisecond)
			fmt.Printf("Task %q completed\n", name)
		}(task)
	}

	// TODO: replace Sleep with Wait
	time.Sleep(100 * time.Millisecond)
	fmt.Println("All tasks completed.")
}

// batchAdd demonstrates calling Add once with the total count.
// TODO: Use wg.Add(numWorkers) once, then launch all goroutines.
func batchAdd() {
	fmt.Println("\n=== Batch Add ===")
	const numWorkers = 10
	results := make([]int, numWorkers)

	// TODO: declare WaitGroup and Add(numWorkers)

	for i := 0; i < numWorkers; i++ {
		go func(id int) {
			// TODO: defer wg.Done()
			results[id] = id * id
		}(i)
	}

	// TODO: replace Sleep with Wait
	time.Sleep(50 * time.Millisecond)
	fmt.Printf("Results: %v\n", results)
}

// dynamicWork processes a dynamic list of URLs concurrently.
// TODO: Use WaitGroup to wait for all fetches to complete.
func dynamicWork() {
	fmt.Println("\n=== Dynamic Work ===")

	urls := []string{
		"https://api.example.com/users",
		"https://api.example.com/orders",
		"https://api.example.com/products",
		"https://api.example.com/inventory",
	}

	// TODO: declare WaitGroup

	for _, url := range urls {
		// TODO: Add(1) before go
		go func(u string) {
			// TODO: defer Done()
			time.Sleep(time.Duration(50+len(u)) * time.Millisecond)
			fmt.Printf("Fetched: %s\n", u)
		}(url)
	}

	// TODO: replace Sleep with Wait
	time.Sleep(200 * time.Millisecond)
	fmt.Println("All URLs fetched.")
}
