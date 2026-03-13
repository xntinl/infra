# 1. Pipeline Pattern

<!--
difficulty: intermediate
concepts: [pipeline, stage-functions, channel-chaining, data-flow]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [goroutines, channels, range-over-channel]
-->

## Prerequisites

- Go 1.22+ installed
- Familiarity with goroutines and channels
- Understanding of `range` over channels and channel closing

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the pipeline pattern and how stages connect via channels
- **Apply** the pipeline pattern to build multi-stage data processing
- **Identify** the role of channel closing in pipeline termination

## Why the Pipeline Pattern

A pipeline is a series of stages connected by channels. Each stage is a goroutine (or group of goroutines) that receives values from an inbound channel, performs some processing, and sends results to an outbound channel.

Pipelines decompose complex processing into small, testable, composable stages. Each stage runs concurrently, so the pipeline overlaps I/O and computation naturally. This is one of Go's most fundamental concurrency patterns.

## Step 1 -- A Simple Pipeline

```bash
mkdir -p ~/go-exercises/pipeline
cd ~/go-exercises/pipeline
go mod init pipeline
```

Create `main.go` with a three-stage pipeline: generate numbers, square them, print results.

```go
package main

import "fmt"

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

func square(in <-chan int) <-chan int {
	out := make(chan int)
	go func() {
		for n := range in {
			out <- n * n
		}
		close(out)
	}()
	return out
}

func main() {
	// Set up the pipeline: generate -> square -> print
	ch := generate(2, 3, 4, 5)
	out := square(ch)

	for v := range out {
		fmt.Println(v)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
4
9
16
25
```

## Step 2 -- Multi-Stage Pipeline

Add more stages to the pipeline:

```go
package main

import "fmt"

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

func filter(in <-chan int, predicate func(int) bool) <-chan int {
	out := make(chan int)
	go func() {
		for n := range in {
			if predicate(n) {
				out <- n
			}
		}
		close(out)
	}()
	return out
}

func multiply(in <-chan int, factor int) <-chan int {
	out := make(chan int)
	go func() {
		for n := range in {
			out <- n * factor
		}
		close(out)
	}()
	return out
}

func toString(in <-chan int) <-chan string {
	out := make(chan string)
	go func() {
		for n := range in {
			out <- fmt.Sprintf("result: %d", n)
		}
		close(out)
	}()
	return out
}

func main() {
	// generate -> filter even -> multiply by 10 -> format
	nums := generate(1, 2, 3, 4, 5, 6, 7, 8, 9, 10)
	evens := filter(nums, func(n int) bool { return n%2 == 0 })
	scaled := multiply(evens, 10)
	formatted := toString(scaled)

	for s := range formatted {
		fmt.Println(s)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
result: 20
result: 40
result: 60
result: 80
result: 100
```

## Step 3 -- Pipeline with Done Channel

Add cancellation support so the pipeline can be stopped early:

```go
package main

import "fmt"

func generateWithDone(done <-chan struct{}, nums ...int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for _, n := range nums {
			select {
			case out <- n:
			case <-done:
				return
			}
		}
	}()
	return out
}

func squareWithDone(done <-chan struct{}, in <-chan int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for n := range in {
			select {
			case out <- n * n:
			case <-done:
				return
			}
		}
	}()
	return out
}

func main() {
	done := make(chan struct{})

	ch := generateWithDone(done, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10)
	out := squareWithDone(done, ch)

	// Take only 3 values, then cancel
	count := 0
	for v := range out {
		fmt.Println(v)
		count++
		if count == 3 {
			close(done)
			break
		}
	}
	fmt.Println("Pipeline cancelled after 3 values")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
1
4
9
Pipeline cancelled after 3 values
```

## Common Mistakes

| Mistake | Why It Fails |
|---|---|
| Forgetting to close output channels | Downstream `range` loops hang forever |
| Not using a done channel for cancellation | Goroutines leak when the consumer stops early |
| Using unbuffered channels when stages have different speeds | Can cause unnecessary blocking; consider buffered channels for throughput |
| Sending on a closed channel | Panics at runtime; only the sender should close |

## Verify What You Learned

1. Add a `sum` stage that accumulates all values and sends the total on close
2. Add a done channel to the multi-stage pipeline and cancel after the first 2 results

## What's Next

Continue to [02 - Fan-Out Pattern](../02-fan-out-pattern/02-fan-out-pattern.md) to learn how to distribute work across multiple goroutines.

## Summary

- A pipeline is a series of stages connected by channels
- Each stage is a goroutine that receives from an input channel and sends to an output channel
- Close output channels to signal completion to downstream stages
- Use a `done` channel to cancel pipelines and prevent goroutine leaks
- Pipeline stages are composable and independently testable

## Reference

- [Go Concurrency Patterns: Pipelines (blog)](https://go.dev/blog/pipelines)
- [Go Concurrency Patterns (talk)](https://go.dev/talks/2012/concurrency.slide)
