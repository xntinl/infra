# 4. Worker Pool (Fixed)

<!--
difficulty: intermediate
concepts: [worker pool, job queue, result collection, goroutine lifecycle]
tools: [go]
estimated_time: 35m
bloom_level: apply
prerequisites: [goroutines, channels, sync.WaitGroup, fan-out, fan-in]
-->

## Prerequisites
- Go 1.22+ installed
- Understanding of goroutines, channels, and `sync.WaitGroup`
- Familiarity with fan-out and fan-in patterns (exercises 02-03)

## Learning Objectives
After completing this exercise, you will be able to:
- **Build** a fixed-size worker pool with typed Job and Result structs
- **Separate** job submission from result collection using distinct channels
- **Manage** worker lifecycle with proper channel closing and WaitGroup
- **Apply** the worker pool pattern to parallelize independent tasks

## Why Worker Pools
The worker pool is the most widely used concurrency pattern in Go. It combines fan-out and fan-in into a single, structured unit: a fixed number of goroutines (workers) pull jobs from a shared queue, process them, and push results into a collection channel.

Worker pools are essential when you need bounded concurrency. Unlike launching a goroutine per task (which can overwhelm resources), a pool caps the number of concurrent operations. This is critical for scenarios like database connections, HTTP clients, file I/O, or any resource with limited capacity. The pool provides backpressure naturally -- when all workers are busy, new job submissions block until a worker becomes available.

```
  Worker Pool Architecture

  +--------+    +------+    +---------+
  |producer| -> | jobs | -> | worker1 | --+
  +--------+    | chan  | -> | worker2 | --+--> results chan --> consumer
                |      | -> | worker3 | --+
                +------+    +---------+

  Flow: send jobs -> close jobs -> workers drain queue
  -> workers exit -> WaitGroup zero -> close results
  -> consumer finishes
```

## Step 1 -- Define Job and Result Types

Start by defining structured types for jobs and results. This makes the pool type-safe and extensible.

```go
type Job struct {
    ID      int
    Payload int
}

type Result struct {
    Job    Job
    Output int
    Worker int
}
```

Each `Result` carries a reference to the original `Job`, the computed output, and which worker processed it. This traceability is invaluable for debugging and monitoring.

## Step 2 -- Implement the Worker Function

Each worker reads from the jobs channel, processes the job, and sends the result:

```go
package main

import (
    "fmt"
    "sync"
    "time"
)

type Job struct {
    ID      int
    Payload int
}

type Result struct {
    Job    Job
    Output int
    Worker int
}

func worker(id int, jobs <-chan Job, results chan<- Result) {
    for job := range jobs {
        time.Sleep(time.Duration(50+job.Payload%50) * time.Millisecond)
        result := Result{
            Job:    job,
            Output: job.Payload * job.Payload,
            Worker: id,
        }
        results <- result
    }
}

func main() {
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
        fmt.Printf("Job %d: %d -> %d\n", r.Job.ID, r.Job.Payload, r.Output)
    }
}
```

The worker has no knowledge of how many jobs exist or how many other workers are running. It simply processes until the jobs channel is closed.

### Intermediate Verification
```bash
go run main.go
```
A single worker should process all jobs sequentially:
```
Job 1: 5 -> 25
Job 2: 10 -> 100
Job 3: 15 -> 225
```

## Step 3 -- Build the Pool

Now create the pool: launch N workers, send jobs, and collect results.

```go
package main

import (
    "fmt"
    "sync"
    "time"
)

type Job struct {
    ID      int
    Payload int
}

type Result struct {
    Job    Job
    Output int
    Worker int
}

func worker(id int, jobs <-chan Job, results chan<- Result) {
    for job := range jobs {
        time.Sleep(time.Duration(50+job.Payload%50) * time.Millisecond)
        result := Result{
            Job:    job,
            Output: job.Payload * job.Payload,
            Worker: id,
        }
        results <- result
    }
}

func runPool(numWorkers, numJobs int) {
    jobs := make(chan Job, numJobs)
    results := make(chan Result, numJobs)

    var wg sync.WaitGroup
    for w := 1; w <= numWorkers; w++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            worker(id, jobs, results)
        }(w)
    }

    for j := 1; j <= numJobs; j++ {
        jobs <- Job{ID: j, Payload: j * 10}
    }
    close(jobs)

    go func() {
        wg.Wait()
        close(results)
    }()

    for r := range results {
        fmt.Printf("  Job %d (payload=%d) -> result=%d [worker %d]\n",
            r.Job.ID, r.Job.Payload, r.Output, r.Worker)
    }
}

func main() {
    runPool(3, 10)
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected: all jobs processed, distributed across workers:
```
  Job 1 (payload=10) -> result=100 [worker 2]
  Job 2 (payload=20) -> result=400 [worker 1]
  ...
```

## Step 4 -- Measure Pool Performance

Compare execution time with different pool sizes to see the concurrency benefit:

```go
package main

import (
    "fmt"
    "time"
    "sync"
)

type Job struct {
    ID      int
    Payload int
}

type Result struct {
    Job    Job
    Output int
    Worker int
}

func worker(id int, jobs <-chan Job, results chan<- Result) {
    for job := range jobs {
        time.Sleep(time.Duration(50+job.Payload%50) * time.Millisecond)
        results <- Result{Job: job, Output: job.Payload * job.Payload, Worker: id}
    }
}

func main() {
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

        fmt.Printf("%2d workers: %v (%d results)\n", nw, time.Since(start), count)
    }
}
```

### Intermediate Verification
```bash
go run main.go
```
More workers should reduce total time (up to the point where workers outnumber jobs).

## Common Mistakes

### Sending Results After the Channel is Closed
**Wrong:**
```go
go func() {
    wg.Wait()
    close(results)
}()
// Worker still running, sends to closed results -> panic
```
**What happens:** If the WaitGroup is not properly coordinated, a worker might try to send after results is closed.

**Fix:** Ensure every worker calls `wg.Done()` only after it has finished all sends. The `defer wg.Done()` at the top of the worker goroutine guarantees this, since `range jobs` exits only when jobs is closed and drained.

### Forgetting to Buffer the Channels
**Wrong:**
```go
jobs := make(chan Job)       // unbuffered
results := make(chan Result) // unbuffered
```
**What happens:** With unbuffered channels, the sender blocks until a receiver is ready. If you try to send all jobs before collecting results, you deadlock (job send blocks because no worker can receive because it's blocked trying to send a result).

**Fix:** Buffer at least one of the channels, or send jobs and collect results concurrently.

### Pool Size of Zero
Always validate that the number of workers is at least 1. A pool with zero workers means nobody reads from the jobs channel, causing a deadlock.

## Verify What You Learned

Run `go run main.go` and verify:
- Single worker test: 3 jobs processed correctly
- Pool with 3 workers and 10 jobs: all results collected
- Performance benchmark: more workers = faster (up to a point)
- Custom pool: factorial values match known results (1!=1, 12!=479001600)

## What's Next
Continue to [05-semaphore-bounded-concurrency](../05-semaphore-bounded-concurrency/05-semaphore-bounded-concurrency.md) to learn an alternative approach to bounding concurrency using a buffered channel as a semaphore.

## Summary
- A worker pool is a fixed set of goroutines reading from a shared jobs channel
- Separate channels for jobs (input) and results (output) provide clean separation
- Typed Job and Result structs make the pool type-safe and debuggable
- Close the jobs channel to signal workers to stop, use WaitGroup to close results
- Buffer channels to avoid deadlocks when sending and receiving happen in sequence
- Worker pools provide bounded concurrency and natural backpressure

## Reference
- [Go by Example: Worker Pools](https://gobyexample.com/worker-pools)
- [Go Blog: Pipelines and Cancellation](https://go.dev/blog/pipelines)
- [Effective Go: Parallelization](https://go.dev/doc/effective_go#parallel)
