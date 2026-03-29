# 7. Goroutine Per Request

<!--
difficulty: intermediate
concepts: [one-goroutine-per-task, isolation, independence, error handling, channels for results, panic recovery]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [01-launching-goroutines, 02-goroutine-vs-os-thread]
-->

## Prerequisites
- Go 1.22+ installed
- Completed [01-launching-goroutines](../01-launching-goroutines/01-launching-goroutines.md)
- Basic understanding of channels (send and receive)

## Learning Objectives
After completing this exercise, you will be able to:
- **Implement** the one-goroutine-per-task pattern for independent work items
- **Collect** results from multiple goroutines using buffered channels
- **Isolate** failures using `defer/recover` so one goroutine's panic does not crash others
- **Apply** this pattern to simulate real-world concurrent request processing

## Why Goroutine-Per-Request

The goroutine-per-request (or goroutine-per-task) pattern is one of Go's most common concurrency idioms. Each incoming request, job, or independent task gets its own goroutine. This pattern works because goroutines are cheap enough to create one for every task, and the Go scheduler efficiently multiplexes them onto OS threads.

This approach has three major advantages. First, each task is isolated: a panic in one goroutine does not crash others (provided you recover it). Second, the programming model is straightforward: each goroutine can be written as simple sequential code. Third, it scales naturally: as load increases, more goroutines are created, and the scheduler distributes them across available cores.

In web servers like `net/http`, this pattern is built in -- every incoming HTTP request is handled in its own goroutine. Understanding the pattern helps you apply it to your own use cases: batch processing, fan-out/fan-in, parallel data pipelines, and more. The key discipline is always collecting all results and recovering panics to prevent goroutine leaks and process crashes.

## Step 1 -- Basic Goroutine-Per-Task

Process a list of tasks independently, each in its own goroutine, collecting results via a buffered channel.

```go
package main

import (
	"fmt"
	"math/rand"
	"time"
)

func main() {
	tasks := []string{"fetch-users", "fetch-orders", "fetch-products", "fetch-reviews", "fetch-inventory"}

	// Buffer the channel to len(tasks) so goroutines never block on send.
	results := make(chan string, len(tasks))

	start := time.Now()

	for _, task := range tasks {
		go func(name string) {
			duration := time.Duration(rand.Intn(100)+50) * time.Millisecond
			time.Sleep(duration)
			results <- fmt.Sprintf("  %-20s completed in %v", name, duration)
		}(task)
	}

	// Collect exactly len(tasks) results
	for i := 0; i < len(tasks); i++ {
		fmt.Println(<-results)
	}

	fmt.Printf("  Wall-clock: %v (vs ~375ms if sequential)\n", time.Since(start).Round(time.Millisecond))
}
```

**What's happening here:** Five goroutines start simultaneously, each simulating work with a random delay. The buffered channel holds results without blocking the senders. We collect exactly 5 results, then report wall-clock time.

**Key insight:** Wall-clock time is approximately equal to the SLOWEST individual task (~150ms), not the SUM of all tasks (~375ms). This is the fundamental benefit of concurrency.

**What would happen with an unbuffered channel?** Goroutines that finish before main reads would block on send. With enough goroutines, this effectively serializes the work because each must wait for main to read before the next can send.

### Intermediate Verification
```bash
go run main.go
```
Expected output (order varies):
```
  fetch-products       completed in 52ms
  fetch-users          completed in 78ms
  fetch-orders         completed in 91ms
  fetch-reviews        completed in 103ms
  fetch-inventory      completed in 145ms
  Wall-clock: 147ms (vs ~375ms if sequential)
```

## Step 2 -- Collecting Structured Results

Use a result struct to collect both data and errors from goroutines. Errors in one task do not affect others.

```go
package main

import (
	"fmt"
	"math/rand"
	"time"
)

type TaskResult struct {
	TaskName string
	Data     string
	Err      error
	Duration time.Duration
}

func main() {
	tasks := []string{"user-profile", "order-history", "recommendations", "notifications"}

	processTask := func(name string) TaskResult {
		start := time.Now()
		duration := time.Duration(rand.Intn(80)+20) * time.Millisecond
		time.Sleep(duration)

		// Simulate occasional failures
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
	fmt.Printf("  Summary: %d succeeded, %d failed\n", successes, failures)
}
```

**What's happening here:** The `TaskResult` struct carries both success data and error information. Each goroutine returns a result regardless of success or failure. The collector processes all results uniformly.

**Key insight:** An error in "recommendations" does not prevent "user-profile" from succeeding. Each goroutine is isolated. The error is captured as data, not as a crash.

**What would happen if processTask panicked instead of returning an error?** The goroutine would crash, taking the entire process down (see Step 3 for the fix).

### Intermediate Verification
```bash
go run main.go
```
Expected output (recommendations may fail or succeed):
```
  OK    user-profile         data="data for user-profile" (45ms)
  FAIL  recommendations      error=service unavailable (32ms)
  OK    order-history        data="data for order-history" (67ms)
  OK    notifications        data="data for notifications" (55ms)
  Summary: 3 succeeded, 1 failed
```

## Step 3 -- Isolation: Panics Don't Propagate

Show that a panic in one goroutine can be recovered without affecting others. This is critical for production systems.

```go
package main

import (
	"fmt"
	"math/rand"
	"time"
)

type SafeResult struct {
	TaskID   int
	Value    string
	Panicked bool
}

func main() {
	safeWorker := func(id int, results chan<- SafeResult) {
		// This defer/recover MUST be at the top of the goroutine function.
		// It catches any panic and sends a result so the collector is not stuck.
		defer func() {
			if r := recover(); r != nil {
				results <- SafeResult{
					TaskID:   id,
					Value:    fmt.Sprintf("recovered from panic: %v", r),
					Panicked: true,
				}
			}
		}()

		// Task 3 deliberately panics
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

	for i := 0; i < numTasks; i++ {
		r := <-results
		status := "   OK"
		if r.Panicked {
			status = "PANIC"
		}
		fmt.Printf("  [%s] task %d: %s\n", status, r.TaskID, r.Value)
	}
}
```

**What's happening here:** Six goroutines are launched. Task 3 deliberately panics. The `defer/recover` in each goroutine catches the panic and sends a `SafeResult` with `Panicked=true`. All other tasks complete normally.

**Key insight:** An UNRECOVERED panic in ANY goroutine crashes the ENTIRE Go process. The `defer/recover` pattern is essential for worker goroutines that might panic. The key is that `recover()` only works inside a deferred function called directly by the panicking goroutine.

**What would happen without the defer/recover?** Task 3's panic would propagate and crash the entire process. Tasks 4, 5, and 6 would never complete. The collector would never receive all 6 results.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
  [   OK] task 1: task 1 completed successfully
  [PANIC] task 3: recovered from panic: something went terribly wrong in task 3
  [   OK] task 2: task 2 completed successfully
  [   OK] task 4: task 4 completed successfully
  [   OK] task 5: task 5 completed successfully
  [   OK] task 6: task 6 completed successfully
```

## Step 4 -- Simulating a Request Handler

Build a realistic simulation of concurrent request processing with status codes, latency tracking, and a concurrency summary.

```go
package main

import (
	"fmt"
	"math/rand"
	"sort"
	"time"
)

func main() {
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

	requests := make([]Request, 15)
	for i := range requests {
		requests[i] = Request{ID: i + 1, Payload: fmt.Sprintf("data-%d", i+1)}
	}

	responses := make(chan Response, len(requests))
	start := time.Now()

	// One goroutine per request
	for _, req := range requests {
		go func(r Request) {
			responses <- handleRequest(r)
		}(req)
	}

	// Collect all responses
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
	fmt.Printf("  Concurrency gain:  %.1fx\n",
		float64(totalLatency)/float64(wallClock))
	fmt.Printf("  Status distribution: 200=%d, 404=%d, 500=%d\n",
		statusCounts[200], statusCounts[404], statusCounts[500])
}
```

**What's happening here:** 15 requests are processed concurrently. Each has random latency (20-100ms) and different status codes based on request ID. Wall-clock time is ~100ms (slowest request), while the sum of all latencies is ~750ms. The concurrency gain is ~7.5x.

**Key insight:** This is exactly how `net/http` works: each HTTP request gets its own goroutine. The server processes thousands of requests concurrently because goroutines are cheap and I/O-bound work parks goroutines without blocking threads.

**What would happen if you processed requests sequentially?** Total time would be ~750ms (sum of all latencies). Concurrency gain would be 1.0x. The server would handle only ~13 requests per second instead of ~150.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
  req  1 -> 200 (45ms)
  req  2 -> 200 (78ms)
  ...
  req  7 -> 500 (32ms)
  ...
  req 15 -> 404 (67ms)

  Wall-clock:        98ms
  Sum of latencies:  742ms
  Concurrency gain:  7.6x
  Status distribution: 200=10, 404=3, 500=2
```

## Common Mistakes

### Not Buffering the Result Channel

**Wrong -- complete program:**
```go
package main

import (
	"fmt"
	"time"
)

func main() {
	tasks := []string{"a", "b", "c", "d", "e"}
	results := make(chan string) // unbuffered!

	for _, task := range tasks {
		go func(t string) {
			time.Sleep(10 * time.Millisecond)
			results <- t // blocks until someone reads
		}(task)
	}

	// This works but is slower: goroutines serialize on the unbuffered send
	for i := 0; i < len(tasks); i++ {
		fmt.Println(<-results)
	}
}
```

**What happens:** With an unbuffered channel, each goroutine blocks on send until main reads. This effectively serializes the collection.

**Correct -- buffer to expected capacity:**
```go
package main

import (
	"fmt"
	"time"
)

func main() {
	tasks := []string{"a", "b", "c", "d", "e"}
	results := make(chan string, len(tasks)) // buffered!

	for _, task := range tasks {
		go func(t string) {
			time.Sleep(10 * time.Millisecond)
			results <- t // non-blocking: buffer has room
		}(task)
	}

	for i := 0; i < len(tasks); i++ {
		fmt.Println(<-results)
	}
}
```

### Forgetting to Collect All Results

**Wrong -- complete program:**
```go
package main

import (
	"fmt"
	"time"
)

func main() {
	results := make(chan string, 10)

	for i := 0; i < 10; i++ {
		go func(id int) {
			time.Sleep(10 * time.Millisecond)
			results <- fmt.Sprintf("result %d", id)
		}(i)
	}

	// Only read 3 results -- 7 goroutines' results are silently lost
	for i := 0; i < 3; i++ {
		fmt.Println(<-results)
	}
}
```

**What happens:** With a buffered channel, the remaining 7 goroutines complete and their results sit in the buffer until the process exits. With an unbuffered channel, those 7 goroutines would be leaked (blocked on send).

**Fix:** Always collect exactly as many results as goroutines you launched, or use a `sync.WaitGroup`.

### Not Recovering Panics in Worker Goroutines

**Wrong -- complete program:**
```go
package main

import "fmt"

func riskyOperation(id int) {
	if id == 3 {
		panic("boom")
	}
	fmt.Printf("task %d done\n", id)
}

func main() {
	for i := 0; i < 5; i++ {
		go riskyOperation(i) // if task 3 panics, ENTIRE program crashes
	}
	select {} // block forever (will crash before reaching here)
}
```

**Fix:** Add defer/recover:
```go
package main

import "fmt"

func safeOperation(id int) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("task %d panicked: %v\n", id, r)
		}
	}()

	if id == 3 {
		panic("boom")
	}
	fmt.Printf("task %d done\n", id)
}

func main() {
	done := make(chan struct{})
	for i := 0; i < 5; i++ {
		go func(id int) {
			safeOperation(id)
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 5; i++ {
		<-done
	}
}
```

## Verify What You Learned

Build a "batch processor" that:
1. Accepts a slice of 20 URLs (simulated as strings)
2. Launches one goroutine per URL that simulates fetching (random latency 10-200ms)
3. 10% of requests fail randomly
4. Collects results with status, latency, and data
5. Prints a summary: total time, success rate, average latency, sorted by latency

**Hint:** Use a `TaskResult` struct and a buffered channel of the same size as the task list.

## What's Next
Continue to [08-million-goroutines](../08-million-goroutines/08-million-goroutines.md) to push goroutines to their scalability limits.

## Summary
- The goroutine-per-task pattern gives each independent work item its own goroutine
- Use buffered channels (`make(chan T, n)`) to collect results without blocking senders
- Goroutine isolation means a failure (or panic) in one does not affect others
- Always collect ALL results or use `WaitGroup` to avoid goroutine leaks
- Add `defer/recover` in worker goroutines that might panic
- Wall-clock time for N concurrent tasks approaches the slowest individual task, not the sum
- This pattern is the foundation of Go's HTTP server, gRPC server, and most concurrent applications

## Reference
- [Go Tour: Channels](https://go.dev/tour/concurrency/2)
- [Effective Go: Channels](https://go.dev/doc/effective_go#channels)
- [Go Blog: Go Concurrency Patterns](https://go.dev/blog/pipelines)
- [net/http: Handler goroutine model](https://pkg.go.dev/net/http#Server)
