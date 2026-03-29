package main

// Exercise: Goroutine Per Request
// Instructions: see 07-goroutine-per-request.md

import (
	"fmt"
	"math/rand"
	"time"
)

// TaskResult holds the outcome of a single task execution.
type TaskResult struct {
	TaskName string
	Data     string
	Err      error
	Duration time.Duration
}

// Step 1: Implement basicPerTask.
// Process a list of tasks concurrently, one goroutine each.
// Collect results through a buffered channel.
func basicPerTask() {
	fmt.Println("=== Basic Goroutine-Per-Task ===")

	tasks := []string{"fetch-users", "fetch-orders", "fetch-products", "fetch-reviews", "fetch-inventory"}

	results := make(chan string, len(tasks))

	// TODO: for each task, launch a goroutine that:
	//   - sleeps for a random duration (50-150ms) to simulate work
	//   - sends a completion message to the results channel
	_ = results
	_ = rand.Intn // hint: use for random duration

	// TODO: collect len(tasks) results from the channel and print them

	fmt.Println()
}

// Step 2: Implement structuredResults.
// Use the TaskResult struct to collect both data and errors.
// Simulate occasional failures to show that they don't affect other tasks.
func structuredResults() {
	fmt.Println("=== Structured Result Collection ===")

	tasks := []string{"user-profile", "order-history", "recommendations", "notifications"}

	processTask := func(name string) TaskResult {
		start := time.Now()
		duration := time.Duration(rand.Intn(80)+20) * time.Millisecond
		time.Sleep(duration)

		// Simulate occasional failures for "recommendations"
		if name == "recommendations" && rand.Float32() < 0.5 {
			return TaskResult{
				TaskName: name,
				Err:      fmt.Errorf("service unavailable"),
				Duration: time.Since(start),
			}
		}

		return TaskResult{
			TaskName: name,
			Data:     fmt.Sprintf("data for %s", name),
			Duration: time.Since(start),
		}
	}

	results := make(chan TaskResult, len(tasks))

	// TODO: launch one goroutine per task, calling processTask(name)
	// TODO: collect all results, count successes and failures
	// TODO: print each result with OK/FAIL status
	_ = processTask
	_ = results

	fmt.Println()
}

// SafeResult holds the outcome of a task that might panic.
type SafeResult struct {
	TaskID   int
	Value    string
	Panicked bool
}

// Step 3: Implement isolationDemo.
// Launch 6 worker goroutines. Task 3 deliberately panics.
// Use defer/recover to prevent the panic from crashing other tasks.
func isolationDemo() {
	fmt.Println("=== Isolation: Panic Recovery ===")

	safeWorker := func(id int, results chan<- SafeResult) {
		// TODO: add defer/recover that sends a SafeResult with Panicked=true
		// on panic, so the collector still gets a result

		// Task 3 panics intentionally
		if id == 3 {
			panic("something went terribly wrong in task 3")
		}

		time.Sleep(time.Duration(rand.Intn(50)+10) * time.Millisecond)
		results <- SafeResult{
			TaskID: id,
			Value:  fmt.Sprintf("task %d completed successfully", id),
		}
	}

	numTasks := 6
	results := make(chan SafeResult, numTasks)

	// TODO: launch numTasks goroutines using safeWorker
	// TODO: collect all results and print with OK/PANIC status
	_ = safeWorker
	_ = results

	fmt.Println()
}

// Step 4: Implement simulateRequestHandler.
// Simulate handling 15 HTTP-like requests concurrently.
// Each request has random latency. Some return 500, some 404, most 200.
// Measure wall-clock time vs sum of individual latencies.
func simulateRequestHandler() {
	fmt.Println("=== Simulated Request Handler ===")

	type Request struct {
		ID      int
		Payload string
	}

	type Response struct {
		RequestID int
		Status    int
		Body      string
		Latency   time.Duration
	}

	handleRequest := func(req Request) Response {
		start := time.Now()
		latency := time.Duration(rand.Intn(80)+20) * time.Millisecond
		time.Sleep(latency)

		switch {
		case req.ID%7 == 0:
			return Response{req.ID, 500, "internal error", time.Since(start)}
		case req.ID%5 == 0:
			return Response{req.ID, 404, "not found", time.Since(start)}
		default:
			return Response{req.ID, 200, fmt.Sprintf("processed: %s", req.Payload), time.Since(start)}
		}
	}

	// Generate 15 requests
	requests := make([]Request, 15)
	for i := range requests {
		requests[i] = Request{ID: i + 1, Payload: fmt.Sprintf("data-%d", i+1)}
	}

	responses := make(chan Response, len(requests))

	// TODO: record start time
	// TODO: launch one goroutine per request, calling handleRequest
	// TODO: collect all responses
	// TODO: print each response with request ID, status, and latency
	// TODO: print summary: wall-clock time, sum of latencies, status distribution
	_ = handleRequest
	_ = responses

	fmt.Println()
}

func main() {
	fmt.Println("Exercise: Goroutine Per Request\n")

	basicPerTask()
	structuredResults()
	isolationDemo()
	simulateRequestHandler()
}
