# 10. Mutex vs Channel

<!--
difficulty: advanced
concepts: [mutex-vs-channel, decision-framework, shared-state, message-passing]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [sync-mutex, channels, goroutines]
-->

## Prerequisites

- Go 1.22+ installed
- Solid understanding of both `sync.Mutex` and channels
- Experience writing concurrent Go programs

## Learning Objectives

After completing this exercise, you will be able to:

- **Analyze** when to use mutexes vs channels for a given concurrency problem
- **Implement** the same problem using both approaches
- **Evaluate** trade-offs in readability, performance, and correctness

## Why This Decision Matters

Go's concurrency proverb says "Do not communicate by sharing memory; share memory by communicating." But the standard library uses mutexes extensively. The Go wiki offers this guidance:

| Use channels when... | Use mutexes when... |
|---|---|
| Passing ownership of data | Protecting internal state of a struct |
| Distributing units of work | Simple counter or flag |
| Communicating async results | Caching or lookup table |
| Coordinating goroutine lifecycle | Multiple fields that must change atomically |

Neither is universally better. The right choice depends on the problem structure.

## The Problem

Implement a rate limiter using both approaches: one with a mutex tracking a token bucket, and one with a channel acting as a semaphore. Compare the implementations for clarity and correctness.

## Requirements

1. Implement `MutexLimiter` that uses a mutex to protect a token count
2. Implement `ChannelLimiter` that uses a buffered channel as a token bucket
3. Both must support `Allow() bool` that returns true if a request is allowed
4. Both must support token refilling at a configurable rate
5. Compare behavior under concurrent load

## Hints

<details>
<summary>Hint 1: Mutex Approach</summary>

```go
type MutexLimiter struct {
    mu       sync.Mutex
    tokens   float64
    max      float64
    refillRate float64 // tokens per second
    lastTime time.Time
}
```
</details>

<details>
<summary>Hint 2: Channel Approach</summary>

A buffered channel with capacity N naturally limits concurrency to N:

```go
type ChannelLimiter struct {
    tokens chan struct{}
}
```

Fill the channel to represent available tokens. `Allow()` tries a non-blocking receive.
</details>

<details>
<summary>Hint 3: Complete Implementation</summary>

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// -- Mutex-based Limiter --

type MutexLimiter struct {
	mu         sync.Mutex
	tokens     float64
	maxTokens  float64
	refillRate float64
	lastRefill time.Time
}

func NewMutexLimiter(maxTokens, refillRate float64) *MutexLimiter {
	return &MutexLimiter{
		tokens:     maxTokens,
		maxTokens:  maxTokens,
		refillRate: refillRate,
		lastRefill: time.Now(),
	}
}

func (l *MutexLimiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(l.lastRefill).Seconds()
	l.tokens += elapsed * l.refillRate
	if l.tokens > l.maxTokens {
		l.tokens = l.maxTokens
	}
	l.lastRefill = now

	if l.tokens >= 1 {
		l.tokens--
		return true
	}
	return false
}

// -- Channel-based Limiter --

type ChannelLimiter struct {
	tokens chan struct{}
	done   chan struct{}
}

func NewChannelLimiter(maxTokens int, refillInterval time.Duration) *ChannelLimiter {
	cl := &ChannelLimiter{
		tokens: make(chan struct{}, maxTokens),
		done:   make(chan struct{}),
	}
	// Fill initial tokens
	for i := 0; i < maxTokens; i++ {
		cl.tokens <- struct{}{}
	}
	// Refill goroutine
	go func() {
		ticker := time.NewTicker(refillInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				select {
				case cl.tokens <- struct{}{}:
				default: // full
				}
			case <-cl.done:
				return
			}
		}
	}()
	return cl
}

func (cl *ChannelLimiter) Allow() bool {
	select {
	case <-cl.tokens:
		return true
	default:
		return false
	}
}

func (cl *ChannelLimiter) Close() {
	close(cl.done)
}

func main() {
	fmt.Println("=== Mutex Limiter ===")
	ml := NewMutexLimiter(10, 100) // 10 burst, 100/sec refill
	testLimiter("Mutex", ml.Allow)

	fmt.Println("\n=== Channel Limiter ===")
	cl := NewChannelLimiter(10, 10*time.Millisecond) // 10 burst, ~100/sec refill
	defer cl.Close()
	testLimiter("Channel", cl.Allow)
}

func testLimiter(name string, allow func() bool) {
	var allowed, denied atomic.Int64
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if allow() {
					allowed.Add(1)
				} else {
					denied.Add(1)
				}
				time.Sleep(time.Millisecond)
			}
		}()
	}

	wg.Wait()
	fmt.Printf("%s: allowed=%d, denied=%d\n", name, allowed.Load(), denied.Load())
}
```
</details>

## Verification

```bash
go run -race main.go
```

Expected: Both limiters allow roughly the same number of requests. No race conditions.

## What's Next

Continue to [11 - Lock-Free Data Structures](../11-lock-free-data-structures/11-lock-free-data-structures.md) to build data structures using atomic compare-and-swap operations.

## Summary

- Use channels for passing ownership, distributing work, and goroutine lifecycle
- Use mutexes for protecting shared state, caching, and simple counters
- The same problem can often be solved with either approach -- choose based on clarity
- Mutex-based rate limiters are more precise; channel-based ones are simpler but coarser
- When in doubt, start with channels; refactor to mutexes only if channels make the code harder to follow

## Reference

- [Go wiki: Mutex or Channel](https://go.dev/wiki/MutexOrChannel)
- [Share Memory By Communicating (blog)](https://go.dev/blog/codelab-share)
- [Go Proverbs](https://go-proverbs.github.io/)
