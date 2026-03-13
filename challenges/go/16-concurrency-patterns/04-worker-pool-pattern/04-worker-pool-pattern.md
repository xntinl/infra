# 4. Worker Pool Pattern

<!--
difficulty: intermediate
concepts: [worker-pool, job-channel, result-channel, bounded-concurrency]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [fan-out-pattern, fan-in-pattern, goroutines, channels]
-->

## Prerequisites

- Go 1.22+ installed
- Completed the Fan-Out and Fan-In exercises
- Understanding of buffered channels

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the worker pool pattern and its relationship to fan-out/fan-in
- **Apply** a fixed pool of workers with job and result channels
- **Analyze** how pool size affects throughput and resource usage

## Why Worker Pools

A worker pool maintains a fixed number of goroutines that process jobs from a shared queue. Unlike uncontrolled fan-out (spawning a goroutine per task), worker pools bound concurrency to prevent resource exhaustion.

Worker pools are the standard pattern for:
- Processing HTTP requests concurrently
- Batch job processing
- Rate-limited API calls
- Database query parallelism

## Step 1 -- Basic Worker Pool

```bash
mkdir -p ~/go-exercises/worker-pool
cd ~/go-exercises/worker-pool
go mod init worker-pool
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"time"
)

type Job struct {
	ID    int
	Input int
}

type Result struct {
	Job    Job
	Output int
	Worker int
}

func worker(id int, jobs <-chan Job, results chan<- Result) {
	for job := range jobs {
		time.Sleep(20 * time.Millisecond) // Simulate work
		results <- Result{
			Job:    job,
			Output: job.Input * job.Input,
			Worker: id,
		}
	}
}

func main() {
	numJobs := 20
	numWorkers := 4

	jobs := make(chan Job, numJobs)
	results := make(chan Result, numJobs)

	// Start workers
	for w := 0; w < numWorkers; w++ {
		go worker(w, jobs, results)
	}

	// Send jobs
	for j := 0; j < numJobs; j++ {
		jobs <- Job{ID: j, Input: j + 1}
	}
	close(jobs)

	// Collect results
	start := time.Now()
	for r := 0; r < numJobs; r++ {
		res := <-results
		fmt.Printf("Worker %d: job %d, %d^2 = %d\n",
			res.Worker, res.Job.ID, res.Job.Input, res.Output)
	}
	fmt.Printf("\nCompleted %d jobs with %d workers in %v\n",
		numJobs, numWorkers, time.Since(start))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: 20 results printed, distributed across 4 workers. Total time roughly 100ms (20 jobs / 4 workers * 20ms).

## Step 2 -- Worker Pool with Error Handling

```go
package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

type Job struct {
	ID int
}

type Result struct {
	JobID  int
	Output string
	Err    error
}

func worker(id int, jobs <-chan Job, results chan<- Result, wg *sync.WaitGroup) {
	defer wg.Done()
	for job := range jobs {
		time.Sleep(10 * time.Millisecond)

		if rand.Intn(5) == 0 {
			results <- Result{JobID: job.ID, Err: fmt.Errorf("worker %d: random failure on job %d", id, job.ID)}
			continue
		}

		results <- Result{
			JobID:  job.ID,
			Output: fmt.Sprintf("worker %d completed job %d", id, job.ID),
		}
	}
}

func main() {
	numJobs := 30
	numWorkers := 5

	jobs := make(chan Job, numJobs)
	results := make(chan Result, numJobs)

	var wg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go worker(w, jobs, results, &wg)
	}

	for j := 0; j < numJobs; j++ {
		jobs <- Job{ID: j}
	}
	close(jobs)

	// Close results when all workers finish
	go func() {
		wg.Wait()
		close(results)
	}()

	var successes, failures int
	for r := range results {
		if r.Err != nil {
			fmt.Printf("  FAIL: %v\n", r.Err)
			failures++
		} else {
			successes++
		}
	}
	fmt.Printf("\nResults: %d succeeded, %d failed\n", successes, failures)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: Roughly 80% successes and 20% failures (random). All 30 jobs are accounted for.

## Step 3 -- Struct-Based Worker Pool

Encapsulate the worker pool as a reusable struct:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type Pool[I any, O any] struct {
	workers int
	process func(I) O
	jobs    chan I
	results chan O
	wg      sync.WaitGroup
}

func NewPool[I any, O any](workers int, bufSize int, fn func(I) O) *Pool[I, O] {
	p := &Pool[I, O]{
		workers: workers,
		process: fn,
		jobs:    make(chan I, bufSize),
		results: make(chan O, bufSize),
	}
	p.start()
	return p
}

func (p *Pool[I, O]) start() {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			for job := range p.jobs {
				p.results <- p.process(job)
			}
		}()
	}
	go func() {
		p.wg.Wait()
		close(p.results)
	}()
}

func (p *Pool[I, O]) Submit(job I) {
	p.jobs <- job
}

func (p *Pool[I, O]) Close() {
	close(p.jobs)
}

func (p *Pool[I, O]) Results() <-chan O {
	return p.results
}

func main() {
	pool := NewPool[int, string](4, 20, func(n int) string {
		time.Sleep(10 * time.Millisecond)
		return fmt.Sprintf("%d^2 = %d", n, n*n)
	})

	go func() {
		for i := 1; i <= 20; i++ {
			pool.Submit(i)
		}
		pool.Close()
	}()

	for result := range pool.Results() {
		fmt.Println(result)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: All 20 results printed. The generic pool works with any input/output types.

## Common Mistakes

| Mistake | Why It Fails |
|---|---|
| Sending results on an unbuffered channel and collecting after all sends | Deadlock -- workers block on sends, never finish |
| Not closing the jobs channel | Workers range-loop forever |
| Using `numJobs` instead of `wg.Wait` to know when to close results | Fragile; breaks if job count changes |

## Verify What You Learned

1. Modify the pool size to 1 and observe linear execution
2. Add a context for cancellation support

## What's Next

Continue to [05 - Generator Pattern](../05-generator-pattern/05-generator-pattern.md) to learn about functions that return channels.

## Summary

- Worker pools maintain a fixed number of goroutines processing jobs from a shared channel
- Use a jobs channel for input and a results channel for output
- Close the jobs channel to signal workers to stop
- Use `sync.WaitGroup` to know when all workers are done, then close results
- Generic worker pools are reusable across different job types

## Reference

- [Go by Example: Worker Pools](https://gobyexample.com/worker-pools)
- [Go Concurrency Patterns (talk)](https://go.dev/talks/2012/concurrency.slide)
