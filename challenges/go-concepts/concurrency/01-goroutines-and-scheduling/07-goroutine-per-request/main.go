package main

import (
	"fmt"
	"math/rand"
	"sort"
	"time"
)

// This program demonstrates the goroutine-per-request pattern: one goroutine per
// independent task, collecting results through channels, with panic isolation.
// Run: go run main.go
//
// Expected output pattern:
//   === Example 1: Basic Goroutine-Per-Task ===
//   (5 tasks complete in any order, wall-clock ~100ms not 500ms)
//
//   === Example 2: Structured Result Collection ===
//   (tasks with OK/FAIL status, error in one does not affect others)
//
//   === Example 3: Panic Recovery and Isolation ===
//   (task 3 panics but all other tasks succeed)
//
//   === Example 4: Simulated Request Handler ===
//   (15 requests processed concurrently with status distribution)

func main() {
	example1BasicPerTask()
	example2StructuredResults()
	example3PanicIsolation()
	example4SimulatedRequestHandler()
}

// example1BasicPerTask processes a list of independent tasks concurrently,
// each in its own goroutine. Results are collected via a buffered channel.
// Key insight: wall-clock time approaches the SLOWEST individual task,
// not the SUM of all tasks. This is the fundamental benefit of concurrency.
func example1BasicPerTask() {
	fmt.Println("=== Example 1: Basic Goroutine-Per-Task ===")

	tasks := []string{"fetch-users", "fetch-orders", "fetch-products", "fetch-reviews", "fetch-inventory"}

	// Buffer the channel to len(tasks) so goroutines never block on send.
	// Without buffering, goroutines would block until main reads, effectively
	// serializing the work.
	results := make(chan string, len(tasks))

	start := time.Now()

	for _, task := range tasks {
		go func(name string) {
			duration := time.Duration(rand.Intn(100)+50) * time.Millisecond
			time.Sleep(duration)
			results <- fmt.Sprintf("  %-20s completed in %v", name, duration)
		}(task)
	}

	// Collect exactly len(tasks) results. This is critical:
	// if you collect fewer, the uncollected goroutines leak (with unbuffered channels)
	// or their results are silently lost (with buffered channels).
	for i := 0; i < len(tasks); i++ {
		fmt.Println(<-results)
	}

	fmt.Printf("  Wall-clock: %v (vs sum ~375ms if sequential)\n\n", time.Since(start).Round(time.Millisecond))
}

// TaskResult holds the outcome of a single task execution, including both
// success data and error information.
type TaskResult struct {
	TaskName string
	Data     string
	Err      error
	Duration time.Duration
}

// example2StructuredResults uses a result struct to collect both data and errors.
// A failure in one task does not affect others.
// Key insight: each goroutine is isolated. An error in "recommendations" does not
// prevent "user-profile" from succeeding.
func example2StructuredResults() {
	fmt.Println("=== Example 2: Structured Result Collection ===")

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

	for _, task := range tasks {
		go func(name string) {
			results <- processTask(name)
		}(task)
	}

	var successes, failures int
	for i := 0; i < len(tasks); i++ {
		r := <-results
		if r.Err != nil {
			failures++
			fmt.Printf("  FAIL  %-20s error=%v (%v)\n", r.TaskName, r.Err, r.Duration.Round(time.Millisecond))
		} else {
			successes++
			fmt.Printf("  OK    %-20s data=%q (%v)\n", r.TaskName, r.Data, r.Duration.Round(time.Millisecond))
		}
	}
	fmt.Printf("  Summary: %d succeeded, %d failed\n\n", successes, failures)
}

// SafeResult holds the outcome of a task that might panic.
type SafeResult struct {
	TaskID   int
	Value    string
	Panicked bool
}

// example3PanicIsolation shows that a panic in one goroutine can be recovered
// without affecting other goroutines.
// Key insight: an UNRECOVERED panic in ANY goroutine crashes the ENTIRE process.
// The defer/recover pattern is essential for worker goroutines that might panic.
func example3PanicIsolation() {
	fmt.Println("=== Example 3: Panic Recovery and Isolation ===")

	safeWorker := func(id int, results chan<- SafeResult) {
		// This defer/recover MUST be at the top of the goroutine function.
		// It catches any panic and converts it to a SafeResult, ensuring the
		// collector still gets a result for every goroutine launched.
		defer func() {
			if r := recover(); r != nil {
				results <- SafeResult{
					TaskID:   id,
					Value:    fmt.Sprintf("recovered from panic: %v", r),
					Panicked: true,
				}
			}
		}()

		// Task 3 deliberately panics to demonstrate isolation
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

	for i := 1; i <= numTasks; i++ {
		go safeWorker(i, results)
	}

	// Collect ALL results -- including the panicked one
	for i := 0; i < numTasks; i++ {
		r := <-results
		status := "   OK"
		if r.Panicked {
			status = "PANIC"
		}
		fmt.Printf("  [%s] task %d: %s\n", status, r.TaskID, r.Value)
	}
	fmt.Println("  Task 3 panicked but all other tasks completed normally.")
	fmt.Println()
}

// example4SimulatedRequestHandler simulates handling 15 HTTP-like requests
// concurrently. Each request has random latency, and some return errors.
// Key insight: wall-clock time is ~max(individual latencies), not their sum.
// This is the fundamental reason Go servers use goroutine-per-request.
func example4SimulatedRequestHandler() {
	fmt.Println("=== Example 4: Simulated Request Handler ===")

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
	start := time.Now()

	// One goroutine per request -- the fundamental pattern
	for _, req := range requests {
		go func(r Request) {
			responses <- handleRequest(r)
		}(req)
	}

	// Collect all responses and build summary
	statusCounts := map[int]int{}
	var totalLatency time.Duration
	var allResponses []Response

	for i := 0; i < len(requests); i++ {
		resp := <-responses
		statusCounts[resp.Status]++
		totalLatency += resp.Latency
		allResponses = append(allResponses, resp)
	}

	// Sort by request ID for readable output
	sort.Slice(allResponses, func(i, j int) bool {
		return allResponses[i].RequestID < allResponses[j].RequestID
	})
	for _, resp := range allResponses {
		fmt.Printf("  req %2d -> %d (%v)\n", resp.RequestID, resp.Status, resp.Latency.Round(time.Millisecond))
	}

	wallClock := time.Since(start)
	fmt.Printf("\n  Wall-clock:        %v\n", wallClock.Round(time.Millisecond))
	fmt.Printf("  Sum of latencies:  %v\n", totalLatency.Round(time.Millisecond))
	fmt.Printf("  Concurrency gain:  %.1fx (processed %v of work in %v)\n",
		float64(totalLatency)/float64(wallClock), totalLatency.Round(time.Millisecond), wallClock.Round(time.Millisecond))
	fmt.Printf("  Status distribution: 200=%d, 404=%d, 500=%d\n\n",
		statusCounts[200], statusCounts[404], statusCounts[500])
}
