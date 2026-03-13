# 9. Or-Channel Pattern

<!--
difficulty: advanced
concepts: [or-channel, first-to-complete, select-multiplexing, recursive-merge]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [channels, select, goroutines, closures]
-->

## Prerequisites

- Go 1.22+ installed
- Solid understanding of channels and `select`
- Familiarity with recursive functions and variadic parameters

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the or-channel pattern for first-to-complete semantics
- **Implement** an `or` function that merges multiple done channels
- **Analyze** use cases like timeouts, cancellation, and race conditions

## Why the Or-Channel Pattern

Sometimes you need to wait for the first of several channels to close. For example: cancel if any of three services times out, or stop when any of multiple shutdown signals fire.

The or-channel pattern takes multiple `<-chan struct{}` channels and returns a single channel that closes when any of the inputs closes. This is more flexible than `context.Context` when you need to combine arbitrary done channels from different sources.

## The Problem

Implement an `or` function that takes a variadic number of done channels and returns a single channel that closes when any input closes. Then use it to implement a first-to-complete search across multiple data sources.

## Requirements

1. `or(channels ...<-chan struct{}) <-chan struct{}` merges done channels
2. The returned channel closes when any input channel closes
3. Must handle 0, 1, 2, and N input channels
4. Must not leak goroutines after the result channel closes
5. Demonstrate with a practical example

## Hints

<details>
<summary>Hint 1: Recursive Approach</summary>

```go
func or(channels ...<-chan struct{}) <-chan struct{} {
    switch len(channels) {
    case 0:
        return nil
    case 1:
        return channels[0]
    }
    // Recursively combine
}
```
</details>

<details>
<summary>Hint 2: Select-Based Approach</summary>

For two channels, select is straightforward:
```go
select {
case <-a:
case <-b:
}
```

For N channels, spawn a goroutine per channel that signals a shared done channel.
</details>

<details>
<summary>Hint 3: Complete Implementation</summary>

```go
package main

import (
	"fmt"
	"time"
)

func or(channels ...<-chan struct{}) <-chan struct{} {
	switch len(channels) {
	case 0:
		return nil
	case 1:
		return channels[0]
	}

	orDone := make(chan struct{})
	go func() {
		defer close(orDone)
		switch len(channels) {
		case 2:
			select {
			case <-channels[0]:
			case <-channels[1]:
			}
		default:
			select {
			case <-channels[0]:
			case <-channels[1]:
			case <-channels[2]:
			case <-or(append(channels[3:], orDone)...):
			}
		}
	}()

	return orDone
}

func after(d time.Duration) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		time.Sleep(d)
		close(ch)
	}()
	return ch
}

func main() {
	start := time.Now()

	done := or(
		after(2*time.Hour),
		after(5*time.Minute),
		after(1*time.Second),
		after(100*time.Millisecond),
		after(500*time.Millisecond),
	)

	<-done

	fmt.Printf("Done after %v\n", time.Since(start).Round(time.Millisecond))
	// Should be ~100ms (the shortest duration)
}
```
</details>

## Verification

```bash
go run main.go
```

Expected: "Done after ~100ms" -- the or-channel closes as soon as the fastest channel closes.

## What's Next

Continue to [10 - Or-Done Channel Pattern](../10-or-done-channel-pattern/10-or-done-channel-pattern.md) to learn how to wrap channel reads with a done channel.

## Summary

- The or-channel pattern merges multiple done channels into one
- The merged channel closes when any input channel closes
- Useful for combining cancellation signals from different sources
- The recursive approach handles any number of channels efficiently
- Prevents goroutine leaks by including `orDone` in the recursive call

## Reference

- [Concurrency in Go (book) by Katherine Cox-Buday](https://www.oreilly.com/library/view/concurrency-in-go/9781491941294/)
- [Go Concurrency Patterns (talk)](https://go.dev/talks/2012/concurrency.slide)
