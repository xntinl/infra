# 10. Or-Done Channel Pattern

<!--
difficulty: advanced
concepts: [or-done, channel-wrapping, cancellation-propagation, done-channel]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [or-channel-pattern, channels, select, context]
-->

## Prerequisites

- Go 1.22+ installed
- Completed the Or-Channel Pattern exercise
- Solid understanding of `select` and done channels

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the or-done pattern and why raw channel reads are insufficient
- **Implement** an `orDone` wrapper that makes any channel read cancellable
- **Analyze** when to use or-done versus context-based cancellation

## Why the Or-Done Pattern

When reading from a channel, `range` blocks until the channel closes. But what if you need to stop reading when a done channel closes, not just when the source closes? Without or-done, every channel read in your pipeline requires a two-case `select`:

```go
select {
case v, ok := <-ch:
    if !ok { return }
    // use v
case <-done:
    return
}
```

The or-done pattern wraps a channel to produce a new channel that closes when either the source closes or the done channel closes. This cleans up pipeline code significantly.

## The Problem

Implement an `orDone` function that wraps a channel with done-channel awareness. Then use it to simplify a multi-stage pipeline where each stage must be cancellable.

## Requirements

1. `orDone[T](done <-chan struct{}, in <-chan T) <-chan T` wraps a channel
2. The returned channel forwards values from `in` until either `in` or `done` closes
3. The returned channel closes when either closes
4. Must not leak goroutines
5. Demonstrate in a pipeline where the consumer cancels mid-stream

## Hints

<details>
<summary>Hint 1: Basic Structure</summary>

```go
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
```
</details>

<details>
<summary>Hint 2: Usage in a Pipeline</summary>

Without or-done, every stage needs manual select:
```go
for {
    select {
    case v, ok := <-upstream:
        if !ok { return }
        process(v)
    case <-done:
        return
    }
}
```

With or-done, it becomes:
```go
for v := range orDone(done, upstream) {
    process(v)
}
```
</details>

<details>
<summary>Hint 3: Complete Implementation</summary>

```go
package main

import (
	"fmt"
	"time"
)

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

func generate(done <-chan struct{}) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		i := 0
		for {
			select {
			case out <- i:
				i++
				time.Sleep(50 * time.Millisecond)
			case <-done:
				return
			}
		}
	}()
	return out
}

func main() {
	done := make(chan struct{})
	nums := generate(done)

	// Use orDone to make range safe with cancellation
	count := 0
	for v := range orDone(done, nums) {
		fmt.Println(v)
		count++
		if count >= 5 {
			close(done)
			break
		}
	}
	fmt.Printf("Consumed %d values then cancelled\n", count)
}
```
</details>

## Verification

```bash
go run main.go
```

Expected: Numbers 0-4 printed, then "Consumed 5 values then cancelled". No goroutine leaks.

## What's Next

Continue to [11 - Tee Channel Pattern](../11-tee-channel-pattern/11-tee-channel-pattern.md) to learn how to split a channel into two.

## Summary

- The or-done pattern wraps a channel to make reads cancellable via a done channel
- It eliminates repetitive two-case `select` blocks throughout pipelines
- The wrapper returns a new channel that closes when either the source or done closes
- Enables clean `for v := range orDone(done, ch)` syntax in pipelines
- Always use `orDone` when reading from channels you do not own in cancellable contexts

## Reference

- [Concurrency in Go (book) by Katherine Cox-Buday](https://www.oreilly.com/library/view/concurrency-in-go/9781491941294/)
- [Go Concurrency Patterns: Pipelines (blog)](https://go.dev/blog/pipelines)
