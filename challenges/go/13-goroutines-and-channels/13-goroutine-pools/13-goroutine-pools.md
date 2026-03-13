# 13. Goroutine Pools

<!--
difficulty: advanced
concepts: [worker-pool, job-queue, result-collection, pool-sizing, backpressure]
tools: [go]
estimated_time: 40m
bloom_level: analyze
prerequisites: [goroutines, channel-basics, waitgroup, ranging-over-channels]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [12 - Channel Patterns: Semaphore and Barrier](../12-channel-patterns-semaphore-barrier/12-channel-patterns-semaphore-barrier.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Build** a fixed-size worker pool that processes jobs from a shared channel
- **Collect** results from workers through a dedicated results channel
- **Implement** backpressure by choosing appropriate channel buffer sizes
- **Extend** the pool with graceful shutdown, error handling, and dynamic sizing

## Why Goroutine Pools

Launching one goroutine per task is simple but dangerous at scale. If you receive 100,000 requests, spawning 100,000 goroutines consumes memory and may overwhelm downstream resources like databases or APIs. A worker pool decouples "how many tasks exist" from "how many execute concurrently." A fixed set of workers pulls jobs from a queue, providing bounded concurrency, natural backpressure, and predictable resource usage.

This is the most commonly used concurrency pattern in production Go code.

## Step 1 -- Basic Worker Pool

```bash
mkdir -p ~/go-exercises/pool && cd ~/go-exercises/pool
go mod init pool
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func worker(id int, jobs <-chan int, wg *sync.WaitGroup) {
	defer wg.Done()
	for job := range jobs {
		fmt.Printf("worker %d: processing job %d\n", id, job)
		time.Sleep(50 * time.Millisecond) // simulate work
	}
	fmt.Printf("worker %d: no more jobs, exiting\n", id)
}

func main() {
	const numWorkers = 3
	jobs := make(chan int, 10)
	var wg sync.WaitGroup

	// Start workers
	for i := 1; i <= numWorkers; i++ {
		wg.Add(1)
		go worker(i, jobs, &wg)
	}

	// Send jobs
	for j := 1; j <= 9; j++ {
		jobs <- j
	}
	close(jobs) // signal no more jobs

	wg.Wait()
	fmt.Println("all jobs complete")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: 3 workers share 9 jobs (roughly 3 each). All workers exit after the jobs channel is closed.

## Step 2 -- Collecting Results

Add a results channel so the caller can receive output from workers:

```go
package main

import (
	"fmt"
	"math"
	"sync"
)

type Job struct {
	ID    int
	Value float64
}

type Result struct {
	JobID  int
	Output float64
}

func worker(id int, jobs <-chan Job, results chan<- Result, wg *sync.WaitGroup) {
	defer wg.Done()
	for job := range jobs {
		result := Result{
			JobID:  job.ID,
			Output: math.Sqrt(job.Value),
		}
		results <- result
	}
}

func main() {
	const numWorkers = 4
	jobs := make(chan Job, 20)
	results := make(chan Result, 20)
	var wg sync.WaitGroup

	// Start workers
	for i := 1; i <= numWorkers; i++ {
		wg.Add(1)
		go worker(i, jobs, results, &wg)
	}

	// Send jobs
	numJobs := 12
	for j := 1; j <= numJobs; j++ {
		jobs <- Job{ID: j, Value: float64(j * j)}
	}
	close(jobs)

	// Close results channel once all workers are done
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	for r := range results {
		fmt.Printf("job %2d: sqrt(%v) = %.2f\n", r.JobID, float64(r.JobID*r.JobID), r.Output)
	}

	fmt.Println("all results collected")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: 12 results printed (order varies), each showing the correct square root. The program exits cleanly.

## Step 3 -- Pool with Error Handling

Workers can encounter errors. Report them alongside successful results:

```go
package main

import (
	"fmt"
	"sync"
)

type Job struct {
	ID    int
	Input int
}

type Result struct {
	JobID int
	Value int
	Err   error
}

func worker(id int, jobs <-chan Job, results chan<- Result, wg *sync.WaitGroup) {
	defer wg.Done()
	for job := range jobs {
		if job.Input < 0 {
			results <- Result{JobID: job.ID, Err: fmt.Errorf("negative input: %d", job.Input)}
			continue
		}
		// Simulate computation
		results <- Result{JobID: job.ID, Value: job.Input * 2}
	}
}

func main() {
	jobs := make(chan Job, 10)
	results := make(chan Result, 10)
	var wg sync.WaitGroup

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go worker(i, jobs, results, &wg)
	}

	inputs := []int{5, -1, 10, -3, 7, 0, 8, -2, 3, 15}
	for i, v := range inputs {
		jobs <- Job{ID: i + 1, Input: v}
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	var successes, failures int
	for r := range results {
		if r.Err != nil {
			fmt.Printf("job %2d: ERROR %v\n", r.JobID, r.Err)
			failures++
		} else {
			fmt.Printf("job %2d: result = %d\n", r.JobID, r.Value)
			successes++
		}
	}

	fmt.Printf("\nsuccesses: %d, failures: %d\n", successes, failures)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: 7 successes and 3 failures (the negative inputs).

## Step 4 -- Backpressure with Unbuffered Jobs Channel

When the jobs channel has no buffer, the producer blocks until a worker is ready. This provides natural backpressure:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func worker(id int, jobs <-chan int, wg *sync.WaitGroup) {
	defer wg.Done()
	for job := range jobs {
		fmt.Printf("worker %d: job %d start\n", id, job)
		time.Sleep(100 * time.Millisecond)
	}
}

func main() {
	jobs := make(chan int) // unbuffered: producer blocks until a worker reads
	var wg sync.WaitGroup

	for i := 1; i <= 2; i++ {
		wg.Add(1)
		go worker(i, jobs, &wg)
	}

	start := time.Now()
	for j := 1; j <= 8; j++ {
		jobs <- j
		fmt.Printf("producer: submitted job %d at %v\n", j, time.Since(start).Truncate(time.Millisecond))
	}
	close(jobs)

	wg.Wait()
	fmt.Printf("total time: %v\n", time.Since(start).Truncate(time.Millisecond))
}
```

### Intermediate Verification

```bash
go run main.go
```

Notice that the producer can only submit a job when a worker is free. With 2 workers and 100ms per job, submitting 8 jobs takes about 400ms. The producer submission timestamps show it being throttled.

## Step 5 -- Reusable Pool Type

Wrap the pattern into a reusable type:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type Pool[J any, R any] struct {
	workers int
	fn      func(J) R
	jobs    chan J
	results chan R
	wg      sync.WaitGroup
}

func NewPool[J any, R any](workers int, bufferSize int, fn func(J) R) *Pool[J, R] {
	p := &Pool[J, R]{
		workers: workers,
		fn:      fn,
		jobs:    make(chan J, bufferSize),
		results: make(chan R, bufferSize),
	}
	for i := 0; i < workers; i++ {
		p.wg.Add(1)
		go p.work()
	}
	return p
}

func (p *Pool[J, R]) work() {
	defer p.wg.Done()
	for job := range p.jobs {
		p.results <- p.fn(job)
	}
}

func (p *Pool[J, R]) Submit(job J) {
	p.jobs <- job
}

func (p *Pool[J, R]) Close() <-chan R {
	close(p.jobs)
	go func() {
		p.wg.Wait()
		close(p.results)
	}()
	return p.results
}

func main() {
	pool := NewPool[int, string](4, 10, func(n int) string {
		time.Sleep(50 * time.Millisecond)
		return fmt.Sprintf("result-%d", n*2)
	})

	for i := 1; i <= 12; i++ {
		pool.Submit(i)
	}

	for r := range pool.Close() {
		fmt.Println(r)
	}
	fmt.Println("pool drained")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: 12 results printed, then "pool drained". The generic Pool type can be reused with any job and result types.

## Common Mistakes

### Forgetting to Close the Jobs Channel

If you never close the jobs channel, workers block forever on `range jobs` and the program deadlocks. Always close the jobs channel after all jobs are submitted.

### Closing the Results Channel Too Early

Closing the results channel before all workers finish causes a panic when a worker tries to send. Use a goroutine with `wg.Wait()` to close results at the right time.

### Choosing Buffer Size Without Thinking

A buffer that is too large wastes memory and hides backpressure. A buffer of zero gives maximum backpressure but may underutilize workers if the producer is slow. Start with a buffer equal to the number of workers and tune from there.

## Verify What You Learned

Build a URL checker: given a list of 20 URLs (you can simulate HTTP calls with `time.Sleep`), use a pool of 5 workers to "check" each URL. Collect results indicating whether each URL is "up" or "down" (simulate randomly). Print a summary at the end.

## What's Next

Continue to [14 - Deadlock Detection and Prevention](../14-deadlock-detection-and-prevention/14-deadlock-detection-and-prevention.md) to learn how to identify and avoid deadlocks in concurrent Go programs.

## Summary

- A worker pool is N goroutines reading from a shared jobs channel
- Close the jobs channel to signal workers to exit after all jobs are submitted
- Use a separate results channel to collect output from workers
- An unbuffered jobs channel provides natural backpressure
- Wrap the pattern in a generic type for reuse across different job types

## Reference

- [Go Blog: Pipelines and cancellation](https://go.dev/blog/pipelines)
- [Go by Example: Worker Pools](https://gobyexample.com/worker-pools)
- [Effective Go: Channels](https://go.dev/doc/effective_go#channels)
