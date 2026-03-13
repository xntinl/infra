# 12. Bridge Channel Pattern

<!--
difficulty: advanced
concepts: [bridge-channel, channel-of-channels, flattening, stream-composition]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [or-done-channel-pattern, channels, goroutines, range-over-channel]
-->

## Prerequisites

- Go 1.22+ installed
- Completed the Or-Done Channel Pattern exercise
- Understanding of channels carrying channels

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the bridge pattern and when to use it
- **Implement** a bridge that flattens a channel of channels into a single stream
- **Analyze** how bridge simplifies consuming sequences of channels

## Why the Bridge Pattern

Sometimes a pipeline produces a sequence of channels rather than a sequence of values. For example, a generator might create a new channel for each batch of work, or a reconnection loop might produce a new data channel after each reconnect.

The bridge pattern consumes a `<-chan <-chan T` and produces a flat `<-chan T`, letting downstream stages read from a single channel without worrying about the channel-of-channels structure.

## The Problem

Implement a generic `bridge` function that flattens a channel of channels into a single output channel. Demonstrate it with a producer that generates batches of values on separate channels.

## Requirements

1. `bridge[T](done <-chan struct{}, chanStream <-chan <-chan T) <-chan T` flattens the stream
2. Values appear in order: all values from the first channel, then all from the second, etc.
3. Output closes when all inner channels and the outer channel are exhausted, or when done closes
4. Must not leak goroutines
5. Demonstrate with a batch-producing generator

## Hints

<details>
<summary>Hint 1: Nested Range</summary>

The bridge goroutine ranges over the outer channel to get each inner channel, then ranges over each inner channel to forward values.
</details>

<details>
<summary>Hint 2: Done-Aware Reading</summary>

Use `orDone` to wrap channel reads so the bridge can be cancelled mid-stream.
</details>

<details>
<summary>Hint 3: Complete Implementation</summary>

```go
package main

import "fmt"

func orDone[T any](done <-chan struct{}, in <-chan T) <-chan T {
	out := make(chan T)
	go func() {
		defer close(out)
		for {
			select {
			case <-done:
				return
			case v, ok := <-in:
				if !ok {
					return
				}
				select {
				case out <- v:
				case <-done:
					return
				}
			}
		}
	}()
	return out
}

func bridge[T any](done <-chan struct{}, chanStream <-chan <-chan T) <-chan T {
	out := make(chan T)
	go func() {
		defer close(out)
		for {
			var stream <-chan T
			select {
			case maybeStream, ok := <-chanStream:
				if !ok {
					return
				}
				stream = maybeStream
			case <-done:
				return
			}
			for v := range orDone(done, stream) {
				select {
				case out <- v:
				case <-done:
					return
				}
			}
		}
	}()
	return out
}

func batchGenerator(done <-chan struct{}, batches ...[]int) <-chan <-chan int {
	chanStream := make(chan (<-chan int))
	go func() {
		defer close(chanStream)
		for _, batch := range batches {
			ch := make(chan int)
			select {
			case chanStream <- ch:
			case <-done:
				return
			}
			go func(vals []int) {
				defer close(ch)
				for _, v := range vals {
					select {
					case ch <- v:
					case <-done:
						return
					}
				}
			}(batch)
		}
	}()
	return chanStream
}

func main() {
	done := make(chan struct{})
	defer close(done)

	batches := batchGenerator(done,
		[]int{1, 2, 3},
		[]int{10, 20, 30},
		[]int{100, 200, 300},
	)

	for v := range bridge(done, batches) {
		fmt.Printf("%d ", v)
	}
	fmt.Println()
}
```
</details>

## Verification

```bash
go run main.go
```

Expected:

```
1 2 3 10 20 30 100 200 300
```

Values appear in batch order, flattened into a single stream.

## What's Next

Continue to [13 - Rate Limiter with Token Bucket](../13-rate-limiter-token-bucket/13-rate-limiter-token-bucket.md) to learn how to control throughput with the token bucket algorithm.

## Summary

- The bridge pattern flattens a `<-chan <-chan T` into a `<-chan T`
- Useful when producers create new channels dynamically (batches, reconnections)
- Values preserve ordering: all values from one inner channel, then the next
- Use `orDone` inside the bridge for cancellation safety
- The consumer sees a simple flat stream regardless of the underlying structure

## Reference

- [Concurrency in Go (book) by Katherine Cox-Buday](https://www.oreilly.com/library/view/concurrency-in-go/9781491941294/)
- [Go Concurrency Patterns: Pipelines (blog)](https://go.dev/blog/pipelines)
