# 9. Rate Limiter: Token Bucket

<!--
difficulty: advanced
concepts: [rate limiting, token bucket, time.Ticker, buffered channel, burst handling]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [goroutines, channels, select, time.Ticker, buffered channels]
-->

## Prerequisites
- Go 1.22+ installed
- Understanding of goroutines, channels, `select`, and `time.Ticker`
- Familiarity with buffered channels as resource containers

## Learning Objectives
After completing this exercise, you will be able to:
- **Implement** a token bucket rate limiter using channels and time.Ticker
- **Configure** both steady-state rate and burst capacity
- **Observe** how the limiter throttles requests that exceed the rate
- **Build** a reusable rate limiter type with blocking and non-blocking acquire

## Why Rate Limiting
Rate limiting controls how frequently an operation can be performed. It protects services from being overwhelmed by too many requests, ensures fair resource sharing, and prevents abuse. The token bucket algorithm is the most widely used rate-limiting approach because it naturally handles both steady-state rate and short bursts.

The token bucket model works like this: a bucket holds tokens (up to a maximum capacity). A background process adds tokens at a fixed rate. To perform an operation, you must take a token from the bucket. If the bucket is empty, you wait until a token is added. If the bucket is full, excess tokens are discarded.

In Go, this maps perfectly to channels: a buffered channel is the bucket, `time.Ticker` fills it at a constant rate, and workers drain it by receiving tokens. The buffer capacity determines the burst size -- if tokens have been accumulating while the system is idle, a burst of requests can be served immediately up to the buffer capacity.

```
  Token Bucket Model

  +------------------+
  | token  token     |  <- buffered channel (capacity = burst)
  | token            |
  +------------------+
      ^           |
      |           |
   ticker       consumer
   refills      drains
   (select/     (<-tokens)
    default)
```

## Step 1 -- Basic Rate Limiter with Ticker

Create a rate limiter that allows one operation per interval.

```go
package main

import (
    "fmt"
    "time"
)

func main() {
    limiter := time.NewTicker(200 * time.Millisecond) // 5 per second
    defer limiter.Stop()

    requests := []int{1, 2, 3, 4, 5, 6, 7, 8}

    for _, req := range requests {
        <-limiter.C // wait for next tick
        fmt.Printf("request %d served at %v\n", req, time.Now().Format("04:05.000"))
    }
}
```

Each `<-limiter.C` blocks until the next tick, enforcing a maximum rate of 5 requests per second (one every 200ms).

### Intermediate Verification
```bash
go run main.go
```
Expected: requests spaced ~200ms apart:
```
request 1 served at 00:01.200
request 2 served at 00:01.400
request 3 served at 00:01.600
...
```

## Step 2 -- Token Bucket with Burst Support

Implement a token bucket that allows bursts by pre-filling tokens in a buffered channel:

```go
package main

import (
    "fmt"
    "time"
)

func main() {
    const rate = 200 * time.Millisecond
    const burstCapacity = 3

    tokens := make(chan struct{}, burstCapacity)

    // Pre-fill with initial burst capacity
    for i := 0; i < burstCapacity; i++ {
        tokens <- struct{}{}
    }

    // Background refiller
    ticker := time.NewTicker(rate)
    defer ticker.Stop()
    go func() {
        for range ticker.C {
            select {
            case tokens <- struct{}{}: // add token if bucket not full
            default:                   // bucket full, discard token
            }
        }
    }()

    // Simulate a burst of 10 requests
    for req := 1; req <= 10; req++ {
        <-tokens // acquire token
        fmt.Printf("request %d served at %v\n", req, time.Now().Format("04:05.000"))
    }
}
```

The first 3 requests are served immediately (burst from pre-filled tokens). Subsequent requests are served at the steady-state rate of one per 200ms.

### Intermediate Verification
```bash
go run main.go
```
Expected: first 3 requests instant, then ~200ms apart:
```
request 1 served at 00:01.000
request 2 served at 00:01.000
request 3 served at 00:01.000
request 4 served at 00:01.200
request 5 served at 00:01.400
...
```

## Step 3 -- Rate Limiter as a Reusable Type

Wrap the token bucket into a clean struct:

```go
package main

import (
    "fmt"
    "time"
)

type RateLimiter struct {
    tokens chan struct{}
    ticker *time.Ticker
    stop   chan struct{}
}

func NewRateLimiter(rate time.Duration, burst int) *RateLimiter {
    rl := &RateLimiter{
        tokens: make(chan struct{}, burst),
        ticker: time.NewTicker(rate),
        stop:   make(chan struct{}),
    }

    for i := 0; i < burst; i++ {
        rl.tokens <- struct{}{}
    }

    go func() {
        for {
            select {
            case <-rl.ticker.C:
                select {
                case rl.tokens <- struct{}{}:
                default:
                }
            case <-rl.stop:
                return
            }
        }
    }()

    return rl
}

func (rl *RateLimiter) Wait() {
    <-rl.tokens
}

func (rl *RateLimiter) TryAcquire() bool {
    select {
    case <-rl.tokens:
        return true
    default:
        return false
    }
}

func (rl *RateLimiter) Stop() {
    rl.ticker.Stop()
    close(rl.stop)
}

func main() {
    rl := NewRateLimiter(100*time.Millisecond, 3)
    for i := 1; i <= 8; i++ {
        rl.Wait()
        fmt.Printf("call %d at %v\n", i, time.Now().Format("04:05.000"))
    }
    rl.Stop()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected: first 3 calls instant (burst), then ~100ms apart.

## Step 4 -- Rate Limiter with Concurrent Workers

Apply the rate limiter to a pool of concurrent workers:

```go
package main

import (
    "fmt"
    "sync"
    "time"
)

type RateLimiter struct {
    tokens chan struct{}
    ticker *time.Ticker
    stop   chan struct{}
}

func NewRateLimiter(rate time.Duration, burst int) *RateLimiter {
    rl := &RateLimiter{
        tokens: make(chan struct{}, burst),
        ticker: time.NewTicker(rate),
        stop:   make(chan struct{}),
    }
    for i := 0; i < burst; i++ {
        rl.tokens <- struct{}{}
    }
    go func() {
        for {
            select {
            case <-rl.ticker.C:
                select {
                case rl.tokens <- struct{}{}:
                default:
                }
            case <-rl.stop:
                return
            }
        }
    }()
    return rl
}

func (rl *RateLimiter) Wait()        { <-rl.tokens }
func (rl *RateLimiter) Stop()        { rl.ticker.Stop(); close(rl.stop) }

func main() {
    rl := NewRateLimiter(100*time.Millisecond, 2)
    defer rl.Stop()

    var wg sync.WaitGroup
    for i := 1; i <= 20; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            rl.Wait()
            fmt.Printf("worker %2d: started at %v\n", id, time.Now().Format("04:05.000"))
            time.Sleep(50 * time.Millisecond)
        }(i)
    }

    wg.Wait()
}
```

All 20 goroutines launch immediately, but they are throttled by the rate limiter.

### Intermediate Verification
```bash
go run main.go
```
Expected: first 2 workers start immediately, then ~1 every 100ms.

## Common Mistakes

### Forgetting to Stop the Ticker
**Wrong:**
```go
ticker := time.NewTicker(100 * time.Millisecond)
// use ticker...
// forgot ticker.Stop()
```
**What happens:** The ticker goroutine leaks, continuously ticking and trying to fill the buffer.

**Fix:** Always call `ticker.Stop()` when done. Use `defer` to ensure cleanup.

### Not Using Default in the Refiller
**Wrong:**
```go
for range ticker.C {
    tokens <- struct{}{} // blocks if bucket is full!
}
```
**What happens:** The refiller goroutine blocks when the bucket is full, and tokens from subsequent ticks are lost (they back up in the ticker channel).

**Fix:** Use `select` with `default` to discard excess tokens: `select { case tokens <- struct{}{}: default: }`

### Setting Burst to Zero
**Wrong:**
```go
tokens := make(chan struct{}, 0) // unbuffered = no burst
```
**What happens:** The channel cannot hold any tokens. The refiller blocks on every send, and the rate becomes erratic.

**Fix:** Burst must be at least 1. The buffer capacity determines how many tokens can accumulate.

## Verify What You Learned

Run `go run main.go` and verify:
- Basic rate limiter: requests spaced ~200ms apart
- Token bucket: first 3 instant (burst), then ~200ms apart
- Reusable type: first 3 instant, then ~100ms apart
- Rate-limited workers: throttled to ~10/sec
- TryAcquire: first 2 accepted, rest rejected, then refill shows new accepts

## What's Next
Continue to [10-end-to-end-pipeline-with-cancel](../10-end-to-end-pipeline-with-cancel/10-end-to-end-pipeline-with-cancel.md) for the capstone exercise combining all patterns from this section.

## Summary
- Token bucket rate limiting maps naturally to Go: buffered channel = bucket, Ticker = refill
- Buffer capacity determines burst size; ticker interval determines steady-state rate
- Pre-fill the bucket for initial burst capacity
- Use `select/default` in the refiller to discard tokens when the bucket is full
- The rate limiter works transparently with concurrent workers
- `TryAcquire` enables non-blocking rate limiting (reject excess instead of queuing)
- Always stop the ticker to prevent goroutine leaks

## Reference
- [Go by Example: Rate Limiting](https://gobyexample.com/rate-limiting)
- [Token Bucket Algorithm (Wikipedia)](https://en.wikipedia.org/wiki/Token_bucket)
- [golang.org/x/time/rate](https://pkg.go.dev/golang.org/x/time/rate) -- production-grade rate limiter in the extended library
