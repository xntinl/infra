# 2. Fan-Out Pattern

<!--
difficulty: intermediate
concepts: [fan-out, work-distribution, multiple-consumers, shared-channel]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [pipeline-pattern, goroutines, channels]
-->

## Prerequisites

- Go 1.22+ installed
- Completed the Pipeline Pattern exercise
- Understanding of goroutines and channels

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the fan-out pattern and when to use it
- **Apply** fan-out by launching multiple goroutines reading from the same channel
- **Identify** how fan-out distributes work across concurrent workers

## Why Fan-Out

Fan-out means starting multiple goroutines to read from the same channel. This is useful when a pipeline stage is CPU-bound or I/O-bound and you want to parallelize it. Each goroutine picks up the next available item from the channel, naturally load-balancing work.

Fan-out is one half of the fan-out/fan-in pattern. This exercise focuses on the distribution side.

## Step 1 -- Basic Fan-Out

```bash
mkdir -p ~/go-exercises/fan-out
cd ~/go-exercises/fan-out
go mod init fan-out
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func generate(nums ...int) <-chan int {
	out := make(chan int)
	go func() {
		for _, n := range nums {
			out <- n
		}
		close(out)
	}()
	return out
}

func heavyWork(id int, in <-chan int, wg *sync.WaitGroup) {
	defer wg.Done()
	for n := range in {
		time.Sleep(50 * time.Millisecond) // Simulate heavy work
		fmt.Printf("Worker %d processed %d -> %d\n", id, n, n*n)
	}
}

func main() {
	jobs := generate(1, 2, 3, 4, 5, 6, 7, 8, 9, 10)

	var wg sync.WaitGroup
	numWorkers := 3

	// Fan out: multiple workers read from the same channel
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go heavyWork(i, jobs, &wg)
	}

	wg.Wait()
	fmt.Println("All work done")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: Three workers process the 10 numbers concurrently. The order varies, but all 10 numbers are processed. Total time is roughly 200ms (10 items / 3 workers * 50ms) instead of 500ms with one worker.

## Step 2 -- Fan-Out with Results

Collect results from fan-out workers:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func generate(n int) <-chan int {
	out := make(chan int)
	go func() {
		for i := 1; i <= n; i++ {
			out <- i
		}
		close(out)
	}()
	return out
}

type Result struct {
	Input  int
	Output int
	Worker int
}

func worker(id int, in <-chan int, out chan<- Result, wg *sync.WaitGroup) {
	defer wg.Done()
	for n := range in {
		time.Sleep(20 * time.Millisecond) // Simulate work
		out <- Result{Input: n, Output: n * n, Worker: id}
	}
}

func main() {
	jobs := generate(12)
	results := make(chan Result, 12)

	var wg sync.WaitGroup
	numWorkers := 4

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go worker(i, jobs, results, &wg)
	}

	// Close results channel when all workers finish
	go func() {
		wg.Wait()
		close(results)
	}()

	for r := range results {
		fmt.Printf("Worker %d: %d -> %d\n", r.Worker, r.Input, r.Output)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: All 12 results printed, distributed across 4 workers. Work distribution depends on scheduling.

## Step 3 -- Fan-Out with Cancellation

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

func generateCtx(ctx context.Context, n int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for i := 1; i <= n; i++ {
			select {
			case out <- i:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

func workerCtx(ctx context.Context, id int, in <-chan int, wg *sync.WaitGroup) {
	defer wg.Done()
	for n := range in {
		select {
		case <-ctx.Done():
			fmt.Printf("Worker %d cancelled\n", id)
			return
		default:
			time.Sleep(30 * time.Millisecond)
			fmt.Printf("Worker %d: processed %d\n", id, n)
		}
	}
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	jobs := generateCtx(ctx, 100)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go workerCtx(ctx, i, jobs, &wg)
	}

	wg.Wait()
	fmt.Println("Done (cancelled after 100ms)")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: Only a subset of the 100 items are processed before the timeout cancels the work.

## Common Mistakes

| Mistake | Why It Fails |
|---|---|
| Closing the input channel from a worker | Only the producer should close the channel; workers just range over it |
| Not waiting for all workers before closing the results channel | Results channel closes prematurely, losing data |
| Creating too many workers | More workers than available CPU cores for CPU-bound work causes overhead |

## Verify What You Learned

1. Modify the basic example to use 1 worker and observe that it takes ~500ms
2. Increase to 5 workers and observe the speedup

## What's Next

Continue to [03 - Fan-In Pattern](../03-fan-in-pattern/03-fan-in-pattern.md) to learn how to merge multiple channels into one.

## Summary

- Fan-out distributes work by having multiple goroutines read from the same channel
- Go's channel semantics ensure each item is delivered to exactly one reader
- Work is naturally load-balanced: faster workers pick up more items
- Always wait for all workers to finish before closing the results channel
- Use context for cancellation support in fan-out patterns

## Reference

- [Go Concurrency Patterns: Pipelines (blog)](https://go.dev/blog/pipelines)
- [Advanced Go Concurrency Patterns (talk)](https://go.dev/talks/2013/advconc.slide)
