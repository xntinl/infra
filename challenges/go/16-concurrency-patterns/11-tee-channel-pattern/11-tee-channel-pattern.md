# 11. Tee Channel Pattern

<!--
difficulty: advanced
concepts: [tee-channel, channel-splitting, broadcast, fan-out-copy]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [or-done-channel-pattern, channels, goroutines, select]
-->

## Prerequisites

- Go 1.22+ installed
- Completed the Or-Done Channel Pattern exercise
- Understanding of channel directions and closing semantics

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the tee pattern and how it duplicates a channel stream
- **Implement** a tee function that splits one channel into two
- **Analyze** backpressure implications when one consumer is slower than the other

## Why the Tee Pattern

Named after the Unix `tee` command, this pattern splits a single channel into two output channels. Every value sent on the input appears on both outputs. This is useful when you need to send the same data to two independent consumers -- for example, logging and processing, or writing to a database and a cache simultaneously.

Unlike fan-out (where each value goes to one worker), tee copies every value to every consumer.

## The Problem

Implement a generic `tee` function that takes a done channel and an input channel, and returns two output channels that both receive every value from the input.

## Requirements

1. `tee[T](done <-chan struct{}, in <-chan T) (<-chan T, <-chan T)` splits a channel
2. Every value from `in` must appear on both output channels
3. Both outputs close when `in` closes or `done` closes
4. Must not leak goroutines
5. Demonstrate with a pipeline that logs and processes simultaneously

## Hints

<details>
<summary>Hint 1: The Send Challenge</summary>

You cannot send to both outputs in one `select`. You must send to one, then the other. Use local variables to track what has been sent:

```go
var out1, out2 chan T
// after receiving v:
out1, out2 = ch1, ch2
// send to each in separate selects
```
</details>

<details>
<summary>Hint 2: Blocking Both Sends</summary>

The simplest approach sends to each output sequentially, blocking until both are consumed. This means the faster consumer waits for the slower one.
</details>

<details>
<summary>Hint 3: Complete Implementation</summary>

```go
package main

import "fmt"

func tee[T any](done <-chan struct{}, in <-chan T) (<-chan T, <-chan T) {
	out1 := make(chan T)
	out2 := make(chan T)

	go func() {
		defer close(out1)
		defer close(out2)
		for v := range orDone(done, in) {
			// Shadow to nil-out after send
			ch1, ch2 := out1, out2
			for i := 0; i < 2; i++ {
				select {
				case ch1 <- v:
					ch1 = nil // Already sent to out1
				case ch2 <- v:
					ch2 = nil // Already sent to out2
				case <-done:
					return
				}
			}
		}
	}()

	return out1, out2
}

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

func generate(done <-chan struct{}, values ...int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for _, v := range values {
			select {
			case out <- v:
			case <-done:
				return
			}
		}
	}()
	return out
}

func main() {
	done := make(chan struct{})
	defer close(done)

	source := generate(done, 1, 2, 3, 4, 5)
	logCh, processCh := tee(done, source)

	// Consumer 1: log values
	go func() {
		for v := range logCh {
			fmt.Printf("[log] received %d\n", v)
		}
	}()

	// Consumer 2: process values
	for v := range processCh {
		fmt.Printf("[process] %d * 10 = %d\n", v, v*10)
	}
}
```
</details>

## Verification

```bash
go run main.go
```

Expected: Each number 1-5 appears in both the log and process output. The order between the two consumers may interleave but every value appears exactly once in each.

## What's Next

Continue to [12 - Bridge Channel Pattern](../12-bridge-channel-pattern/12-bridge-channel-pattern.md) to learn how to flatten a channel of channels.

## Summary

- The tee pattern duplicates a channel stream to two output channels
- Every value from the input appears on both outputs
- Use nil-channel technique to send to both outputs without double-sending
- The tee goroutine blocks until both consumers have received each value
- If one consumer is significantly slower, consider buffered channels on the outputs

## Reference

- [Concurrency in Go (book) by Katherine Cox-Buday](https://www.oreilly.com/library/view/concurrency-in-go/9781491941294/)
- [Unix tee command](https://en.wikipedia.org/wiki/Tee_(command))
