# 13. Rate Limiter with Token Bucket

<!--
difficulty: advanced
concepts: [rate-limiting, token-bucket, time-ticker, throughput-control, golang-rate]
tools: [go]
estimated_time: 40m
bloom_level: analyze
prerequisites: [channels, select, time-ticker, goroutines, context]
-->

## Prerequisites

- Go 1.22+ installed
- Familiarity with tickers and channel-based timing
- Understanding of `context.Context`

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the token bucket algorithm and how it controls throughput
- **Implement** a rate limiter using channels and tickers
- **Analyze** burst capacity versus sustained rate in rate limiting

## Why Rate Limiting

Rate limiting protects services from being overwhelmed. The token bucket algorithm allows a steady rate of operations with configurable burst capacity. Tokens accumulate at a fixed rate up to a maximum bucket size. Each operation consumes a token; if no tokens are available, the operation blocks or is rejected.

Go's `golang.org/x/time/rate` package implements this, but building one from channels teaches the mechanics.

## The Problem

Build a token bucket rate limiter using Go channels and tickers. Then compare your implementation with Go's standard `rate.Limiter`.

## Requirements

1. A `RateLimiter` struct with configurable rate (tokens per second) and burst (bucket size)
2. `Wait(ctx context.Context) error` blocks until a token is available or context is cancelled
3. `Allow() bool` returns immediately: true if a token is available, false otherwise
4. Tokens refill at the configured rate using a ticker
5. Demonstrate controlled throughput under load

## Hints

<details>
<summary>Hint 1: Channel as Token Bucket</summary>

A buffered channel with capacity equal to burst size is a natural token bucket. A ticker goroutine sends tokens into the channel. Consuming a token is just receiving from the channel.

```go
type RateLimiter struct {
    tokens chan struct{}
    stop   chan struct{}
}
```
</details>

<details>
<summary>Hint 2: Refill Goroutine</summary>

```go
func (rl *RateLimiter) refill(rate time.Duration) {
    ticker := time.NewTicker(rate)
    defer ticker.Stop()
    for {
        select {
        case <-ticker.C:
            select {
            case rl.tokens <- struct{}{}:
            default: // bucket full
            }
        case <-rl.stop:
            return
        }
    }
}
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
	"time"
)

type RateLimiter struct {
	tokens chan struct{}
	stop   chan struct{}
}

func NewRateLimiter(ratePerSecond int, burst int) *RateLimiter {
	rl := &RateLimiter{
		tokens: make(chan struct{}, burst),
		stop:   make(chan struct{}),
	}

	// Pre-fill burst capacity
	for i := 0; i < burst; i++ {
		rl.tokens <- struct{}{}
	}

	// Refill at the configured rate
	interval := time.Second / time.Duration(ratePerSecond)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				select {
				case rl.tokens <- struct{}{}:
				default: // bucket full
				}
			case <-rl.stop:
				return
			}
		}
	}()

	return rl
}

func (rl *RateLimiter) Wait(ctx context.Context) error {
	select {
	case <-rl.tokens:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (rl *RateLimiter) Allow() bool {
	select {
	case <-rl.tokens:
		return true
	default:
		return false
	}
}

func (rl *RateLimiter) Stop() {
	close(rl.stop)
}

func main() {
	limiter := NewRateLimiter(10, 3) // 10/sec, burst of 3
	defer limiter.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	start := time.Now()
	completed := 0

	// Launch 30 requests
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if err := limiter.Wait(ctx); err != nil {
				return
			}
			elapsed := time.Since(start).Truncate(time.Millisecond)
			fmt.Printf("[%v] Request %d processed\n", elapsed, id)
			completed++
		}(i)
	}

	wg.Wait()
	fmt.Printf("\nCompleted %d requests in %v (rate: 10/sec, burst: 3)\n",
		completed, time.Since(start).Round(time.Millisecond))
}
```
</details>

## Verification

```bash
go run main.go
```

Expected: The first 3 requests process immediately (burst), then subsequent requests are spaced ~100ms apart (10/sec). After 2 seconds, roughly 23 requests complete (3 burst + 20 at rate).

## What's Next

Continue to [14 - Circuit Breaker Pattern](../14-circuit-breaker-pattern/14-circuit-breaker-pattern.md) to learn how to protect services from cascading failures.

## Summary

- Token bucket rate limiting controls throughput with burst capacity
- A buffered channel naturally models the bucket: capacity = burst size
- A ticker goroutine refills tokens at the configured rate
- `Wait` blocks until a token is available; `Allow` returns immediately
- Go's `golang.org/x/time/rate.Limiter` is the production-grade implementation

## Reference

- [Token bucket algorithm (Wikipedia)](https://en.wikipedia.org/wiki/Token_bucket)
- [golang.org/x/time/rate documentation](https://pkg.go.dev/golang.org/x/time/rate)
- [Rate Limiting in Go (blog)](https://go.dev/wiki/RateLimiting)
