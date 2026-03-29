# 7. Goroutine Per Request

<!--
difficulty: intermediate
concepts: [one-goroutine-per-task, isolation, independence, error handling, channels for results]
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
- **Collect** results from multiple goroutines using channels
- **Demonstrate** that a failure in one goroutine does not affect others
- **Apply** this pattern to simulate real-world request processing

## Why Goroutine-Per-Request
The goroutine-per-request (or goroutine-per-task) pattern is one of Go's most common concurrency idioms. Each incoming request, job, or independent task gets its own goroutine. This pattern works because goroutines are cheap enough to create one for every task, and the Go scheduler efficiently multiplexes them onto OS threads.

This approach has three major advantages. First, each task is isolated: a panic in one goroutine does not crash others (though it will crash the process unless recovered). Second, the programming model is straightforward: each goroutine can be written as simple sequential code. Third, it scales naturally: as load increases, more goroutines are created, and the scheduler distributes them across available cores.

In web servers like `net/http`, this pattern is built in -- every incoming HTTP request is handled in its own goroutine. Understanding the pattern helps you apply it to your own use cases: batch processing, fan-out/fan-in, parallel data pipelines, and more.

## Step 1 -- Basic Goroutine-Per-Task

Process a list of tasks independently, each in its own goroutine:

```go
func basicPerTask() {
    fmt.Println("=== Basic Goroutine-Per-Task ===")

    tasks := []string{"fetch-users", "fetch-orders", "fetch-products", "fetch-reviews", "fetch-inventory"}

    results := make(chan string, len(tasks))

    for _, task := range tasks {
        go func(name string) {
            // Simulate varying processing times
            duration := time.Duration(rand.Intn(100)+50) * time.Millisecond
            time.Sleep(duration)
            results <- fmt.Sprintf("  %s completed in %v", name, duration)
        }(task)
    }

    // Collect all results
    for i := 0; i < len(tasks); i++ {
        fmt.Println(<-results)
    }
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output (order varies because tasks complete at different times):
```
=== Basic Goroutine-Per-Task ===
  fetch-products completed in 52ms
  fetch-users completed in 78ms
  fetch-orders completed in 91ms
  fetch-reviews completed in 103ms
  fetch-inventory completed in 145ms
```

## Step 2 -- Collecting Structured Results

Use a result struct to collect both data and errors from goroutines:

```go
type TaskResult struct {
    TaskName string
    Data     string
    Err      error
    Duration time.Duration
}

func structuredResults() {
    fmt.Println("=== Structured Result Collection ===")

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

    // Collect and display results
    var successes, failures int
    for i := 0; i < len(tasks); i++ {
        r := <-results
        if r.Err != nil {
            failures++
            fmt.Printf("  FAIL  %-20s error=%v (%v)\n", r.TaskName, r.Err, r.Duration)
        } else {
            successes++
            fmt.Printf("  OK    %-20s data=%q (%v)\n", r.TaskName, r.Data, r.Duration)
        }
    }
    fmt.Printf("  Summary: %d succeeded, %d failed\n\n", successes, failures)
}
```

### Intermediate Verification
```bash
go run main.go
```
Some runs will show a failure for "recommendations", others will show all successes. The key point is that failures in one task don't affect the others.

## Step 3 -- Isolation: Panics Don't Propagate

Show that a panic in one goroutine can be recovered without affecting others:

```go
func isolationDemo() {
    fmt.Println("=== Isolation: Panic Recovery ===")

    type SafeResult struct {
        TaskID  int
        Value   string
        Panicked bool
    }

    safeWorker := func(id int, results chan<- SafeResult) {
        defer func() {
            if r := recover(); r != nil {
                results <- SafeResult{
                    TaskID:   id,
                    Value:    fmt.Sprintf("recovered from panic: %v", r),
                    Panicked: true,
                }
            }
        }()

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

    for i := 1; i <= numTasks; i++ {
        go safeWorker(i, results)
    }

    for i := 0; i < numTasks; i++ {
        r := <-results
        status := "OK"
        if r.Panicked {
            status = "PANIC"
        }
        fmt.Printf("  [%5s] task %d: %s\n", status, r.TaskID, r.Value)
    }
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Isolation: Panic Recovery ===
  [   OK] task 1: task 1 completed successfully
  [PANIC] task 3: recovered from panic: something went terribly wrong in task 3
  [   OK] task 2: task 2 completed successfully
  [   OK] task 4: task 4 completed successfully
  [   OK] task 5: task 5 completed successfully
  [   OK] task 6: task 6 completed successfully
```

Task 3 panicked and was recovered, but all other tasks completed normally.

## Step 4 -- Simulating a Request Handler

Build a realistic simulation of processing independent requests:

```go
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

        // Simulate varying latencies
        latency := time.Duration(rand.Intn(80)+20) * time.Millisecond
        time.Sleep(latency)

        // Simulate different outcomes
        switch {
        case req.ID%7 == 0:
            return Response{req.ID, 500, "internal error", time.Since(start)}
        case req.ID%5 == 0:
            return Response{req.ID, 404, "not found", time.Since(start)}
        default:
            return Response{req.ID, 200, fmt.Sprintf("processed: %s", req.Payload), time.Since(start)}
        }
    }

    // Generate requests
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

    for i := 0; i < len(requests); i++ {
        resp := <-responses
        statusCounts[resp.Status]++
        totalLatency += resp.Latency
        fmt.Printf("  req %2d -> %d (%v)\n", resp.RequestID, resp.Status, resp.Latency)
    }

    wallClock := time.Since(start)
    fmt.Printf("\n  Wall-clock: %v | Sum of latencies: %v\n", wallClock, totalLatency)
    fmt.Printf("  Concurrency benefit: processed %v of work in %v\n", totalLatency, wallClock)
    fmt.Printf("  Status distribution: %v\n\n", statusCounts)
}
```

### Intermediate Verification
```bash
go run main.go
```
The wall-clock time should be close to the slowest single request (~100ms), while the sum of latencies should be much larger, demonstrating the concurrency benefit.

## Common Mistakes

### Not Buffering the Result Channel
**Wrong:**
```go
results := make(chan string) // unbuffered
for _, task := range tasks {
    go func(t string) {
        results <- t // blocks until someone reads
    }(task)
}
// If we don't read fast enough, goroutines pile up waiting to send
```

**What happens:** Goroutines block on send until the main goroutine reads. With an unbuffered channel, you effectively serialize the work.

**Fix:** Buffer the channel to the number of expected results:
```go
results := make(chan string, len(tasks))
```

### Forgetting to Collect All Results
**Wrong:**
```go
for _, task := range tasks {
    go process(task, results)
}
// Only read some results
for i := 0; i < 3; i++ {
    <-results
}
// Remaining goroutines are leaked if they try to send to results
```

**What happens:** Goroutines that cannot send their result to the channel are leaked. With a buffered channel, they complete but the results are lost.

**Fix:** Always collect exactly as many results as you launched goroutines (or use a `sync.WaitGroup`).

### Not Recovering Panics in Worker Goroutines
**Wrong:**
```go
go func() {
    // If this panics, the entire program crashes
    riskyOperation()
}()
```

**What happens:** An unrecovered panic in any goroutine crashes the entire Go program.

**Fix:** Add `defer recover()` in goroutines that might panic:
```go
go func() {
    defer func() {
        if r := recover(); r != nil {
            log.Printf("worker panicked: %v", r)
        }
    }()
    riskyOperation()
}()
```

## Verify What You Learned

Build a "batch processor" that:
1. Accepts a slice of 20 URLs (simulated as strings)
2. Launches one goroutine per URL that simulates fetching (random latency 10-200ms)
3. 10% of requests fail randomly
4. Collects results with status, latency, and data
5. Prints a summary: total time, success rate, average latency, p95 latency

## What's Next
Continue to [08-million-goroutines](../08-million-goroutines/08-million-goroutines.md) to push goroutines to their scalability limits.

## Summary
- The goroutine-per-task pattern gives each independent work item its own goroutine
- Use buffered channels to collect results without blocking senders
- Goroutine isolation means a failure (or panic) in one does not affect others
- Always collect all results or use `WaitGroup` to avoid goroutine leaks
- Add `defer recover()` in worker goroutines that might panic
- Wall-clock time for N concurrent tasks approaches the slowest individual task, not the sum
- This pattern is the foundation of Go's HTTP server, gRPC server, and most concurrent applications

## Reference
- [Go Tour: Channels](https://go.dev/tour/concurrency/2)
- [Effective Go: Channels](https://go.dev/doc/effective_go#channels)
- [Go Blog: Go Concurrency Patterns](https://go.dev/blog/pipelines)
- [net/http: Handler goroutine model](https://pkg.go.dev/net/http#Server)
