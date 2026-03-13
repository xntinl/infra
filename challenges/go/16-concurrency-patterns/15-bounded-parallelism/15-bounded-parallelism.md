# 15. Bounded Parallelism

<!--
difficulty: advanced
concepts: [semaphore, bounded-concurrency, channel-semaphore, weighted-semaphore]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [goroutines, channels, sync-waitgroup, context]
-->

## Prerequisites

- Go 1.22+ installed
- Familiarity with goroutines, WaitGroups, and channels
- Understanding of resource contention

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the semaphore pattern for bounding parallel work
- **Implement** bounded parallelism using channels and `golang.org/x/sync/semaphore`
- **Analyze** the tradeoff between parallelism and resource usage

## Why Bounded Parallelism

Unbounded goroutine creation can exhaust memory, file descriptors, or downstream service capacity. A semaphore limits the number of concurrent operations. In Go, a buffered channel of capacity N is a natural semaphore: acquire by sending, release by receiving.

The `golang.org/x/sync/semaphore` package provides a weighted semaphore for more complex scenarios where different operations consume different amounts of capacity.

## The Problem

Process a large batch of items with bounded parallelism using both a channel-based semaphore and `golang.org/x/sync/semaphore.Weighted`. Compare the approaches.

## Requirements

1. Process N items with at most M concurrent goroutines
2. Implement using a buffered channel as a semaphore
3. Implement using `semaphore.Weighted`
4. Both must be safe under the race detector
5. Demonstrate that concurrency is actually bounded

## Hints

<details>
<summary>Hint 1: Channel Semaphore</summary>

```go
sem := make(chan struct{}, maxConcurrency)
// Acquire:
sem <- struct{}{}
// Release:
<-sem
```
</details>

<details>
<summary>Hint 2: Weighted Semaphore</summary>

```go
import "golang.org/x/sync/semaphore"

sem := semaphore.NewWeighted(int64(maxConcurrency))
sem.Acquire(ctx, 1) // blocks until acquired
sem.Release(1)
```
</details>

<details>
<summary>Hint 3: Complete Implementation</summary>

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/semaphore"
)

func processWithChannelSem(items []int, maxConcurrency int) {
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup
	var peak atomic.Int64
	var current atomic.Int64

	for _, item := range items {
		wg.Add(1)
		sem <- struct{}{} // Acquire
		go func(id int) {
			defer wg.Done()
			defer func() {
				current.Add(-1)
				<-sem // Release
			}()

			c := current.Add(1)
			for {
				old := peak.Load()
				if c <= old || peak.CompareAndSwap(old, c) {
					break
				}
			}

			time.Sleep(50 * time.Millisecond) // Simulate work
			_ = id
		}(item)
	}

	wg.Wait()
	fmt.Printf("[channel-sem] Peak concurrency: %d (limit: %d)\n", peak.Load(), maxConcurrency)
}

func processWithWeightedSem(items []int, maxConcurrency int) {
	sem := semaphore.NewWeighted(int64(maxConcurrency))
	ctx := context.Background()
	var wg sync.WaitGroup
	var peak atomic.Int64
	var current atomic.Int64

	for _, item := range items {
		wg.Add(1)
		sem.Acquire(ctx, 1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				current.Add(-1)
				sem.Release(1)
			}()

			c := current.Add(1)
			for {
				old := peak.Load()
				if c <= old || peak.CompareAndSwap(old, c) {
					break
				}
			}

			time.Sleep(50 * time.Millisecond)
			_ = id
		}(item)
	}

	wg.Wait()
	fmt.Printf("[weighted-sem] Peak concurrency: %d (limit: %d)\n", peak.Load(), maxConcurrency)
}

func main() {
	items := make([]int, 50)
	for i := range items {
		items[i] = i
	}

	start := time.Now()
	processWithChannelSem(items, 5)
	fmt.Printf("  Time: %v\n", time.Since(start).Round(time.Millisecond))

	start = time.Now()
	processWithWeightedSem(items, 5)
	fmt.Printf("  Time: %v\n\n", time.Since(start).Round(time.Millisecond))

	// Compare different concurrency levels
	for _, limit := range []int{1, 5, 10, 25} {
		start = time.Now()
		processWithChannelSem(items, limit)
		fmt.Printf("  Time: %v\n", time.Since(start).Round(time.Millisecond))
	}
}
```
</details>

## Verification

```bash
go get golang.org/x/sync
go run -race main.go
```

Expected: Peak concurrency matches the configured limit. Higher limits complete faster (50 items * 50ms / N workers).

## What's Next

Continue to [16 - Pub/Sub with Channels](../16-pub-sub-with-channels/16-pub-sub-with-channels.md) to learn how to build a publish-subscribe system with channels.

## Summary

- Bounded parallelism prevents resource exhaustion by limiting concurrent goroutines
- A buffered channel of capacity N is the simplest semaphore in Go
- `golang.org/x/sync/semaphore.Weighted` supports weighted acquisition for heterogeneous workloads
- Acquire before launching work, release when done
- The optimal concurrency level depends on the bottleneck: CPU cores, I/O connections, or downstream capacity

## Reference

- [semaphore package documentation](https://pkg.go.dev/golang.org/x/sync/semaphore)
- [Go Concurrency Patterns: Pipelines (blog)](https://go.dev/blog/pipelines)
- [Semaphore (Wikipedia)](https://en.wikipedia.org/wiki/Semaphore_(programming))
