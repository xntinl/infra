# 3. Fan-In Pattern

<!--
difficulty: intermediate
concepts: [fan-in, multiplexing, merge-channels, select-statement]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [fan-out-pattern, goroutines, channels, select]
-->

## Prerequisites

- Go 1.22+ installed
- Completed the Fan-Out Pattern exercise
- Understanding of `select` statement

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** how fan-in merges multiple channels into a single output channel
- **Apply** the fan-in pattern using goroutines and `sync.WaitGroup`
- **Combine** fan-out and fan-in into a complete pipeline

## Why Fan-In

Fan-in is the complement of fan-out. Multiple goroutines produce results on separate channels, and fan-in merges them into a single channel for downstream consumption. This pattern is essential for collecting results from parallel workers.

Without fan-in, consumers would need to read from multiple channels simultaneously, which complicates the code. Fan-in centralizes this complexity into a single merge function.

## Step 1 -- Basic Fan-In

```bash
mkdir -p ~/go-exercises/fan-in
cd ~/go-exercises/fan-in
go mod init fan-in
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"sync"
)

func merge(channels ...<-chan int) <-chan int {
	out := make(chan int)
	var wg sync.WaitGroup

	for _, ch := range channels {
		wg.Add(1)
		go func(c <-chan int) {
			defer wg.Done()
			for v := range c {
				out <- v
			}
		}(ch)
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

func producer(id int, values ...int) <-chan int {
	out := make(chan int)
	go func() {
		for _, v := range values {
			out <- v
			fmt.Printf("Producer %d sent %d\n", id, v)
		}
		close(out)
	}()
	return out
}

func main() {
	ch1 := producer(1, 1, 2, 3)
	ch2 := producer(2, 10, 20, 30)
	ch3 := producer(3, 100, 200, 300)

	merged := merge(ch1, ch2, ch3)

	for v := range merged {
		fmt.Printf("Received: %d\n", v)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: All 9 values from all 3 producers appear. The order is non-deterministic because channels are merged concurrently.

## Step 2 -- Fan-Out / Fan-In Pipeline

Combine fan-out and fan-in for parallel processing:

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

func expensiveSquare(in <-chan int) <-chan int {
	out := make(chan int)
	go func() {
		for n := range in {
			time.Sleep(20 * time.Millisecond) // Simulate slow computation
			out <- n * n
		}
		close(out)
	}()
	return out
}

func merge(channels ...<-chan int) <-chan int {
	out := make(chan int)
	var wg sync.WaitGroup
	for _, ch := range channels {
		wg.Add(1)
		go func(c <-chan int) {
			defer wg.Done()
			for v := range c {
				out <- v
			}
		}(ch)
	}
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

func main() {
	start := time.Now()
	jobs := generate(20)

	// Fan out to 4 workers
	workers := make([]<-chan int, 4)
	for i := range workers {
		workers[i] = expensiveSquare(jobs)
	}

	// Fan in results
	results := merge(workers...)

	count := 0
	for v := range results {
		_ = v
		count++
	}

	fmt.Printf("Processed %d items in %v\n", count, time.Since(start))
	fmt.Printf("Sequential would take ~%v\n", 20*20*time.Millisecond)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: 20 items processed in roughly 100ms (20 items / 4 workers * 20ms), much less than the sequential 400ms.

## Step 3 -- Typed Fan-In with Generics

```go
package main

import (
	"fmt"
	"sync"
)

func Merge[T any](channels ...<-chan T) <-chan T {
	out := make(chan T)
	var wg sync.WaitGroup

	for _, ch := range channels {
		wg.Add(1)
		go func(c <-chan T) {
			defer wg.Done()
			for v := range c {
				out <- v
			}
		}(ch)
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

func main() {
	// String channels
	ch1 := make(chan string, 2)
	ch2 := make(chan string, 2)
	ch1 <- "hello"
	ch1 <- "world"
	close(ch1)
	ch2 <- "foo"
	ch2 <- "bar"
	close(ch2)

	for s := range Merge(ch1, ch2) {
		fmt.Println(s)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: All four strings printed (order may vary).

## Common Mistakes

| Mistake | Why It Fails |
|---|---|
| Closing the output channel before all input channels are drained | Sends to a closed channel panic |
| Forgetting `sync.WaitGroup` in merge | Output channel never closes, consumer hangs |
| Not using a goroutine for the `wg.Wait(); close(out)` | Blocks the merge function, preventing it from returning |

## Verify What You Learned

1. Build a pipeline: generate 1-100 -> fan-out to 5 workers that double each number -> fan-in -> sum all results
2. Compare the wall-clock time with 1 worker vs 5 workers

## What's Next

Continue to [04 - Worker Pool Pattern](../04-worker-pool-pattern/04-worker-pool-pattern.md) to learn how to build structured worker pools with job and result channels.

## Summary

- Fan-in merges multiple input channels into a single output channel
- Each input channel gets a goroutine that forwards values to the shared output
- A `sync.WaitGroup` tracks when all inputs are drained, then closes the output
- Combined with fan-out, it enables parallel processing pipelines
- Generic `Merge[T]` functions work with any channel type

## Reference

- [Go Concurrency Patterns: Pipelines (blog)](https://go.dev/blog/pipelines)
- [Go Concurrency Patterns (talk)](https://go.dev/talks/2012/concurrency.slide)
