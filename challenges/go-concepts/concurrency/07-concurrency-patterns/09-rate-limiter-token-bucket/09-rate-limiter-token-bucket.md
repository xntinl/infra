---
difficulty: advanced
concepts: [rate limiting, token bucket, time.Ticker, buffered channel, burst handling]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [goroutines, channels, select, time.Ticker, buffered channels]
---

# 9. Rate Limiter: Token Bucket

## Learning Objectives
After completing this exercise, you will be able to:
- **Implement** a token bucket rate limiter using channels and time.Ticker
- **Configure** both steady-state rate and burst capacity
- **Observe** how the limiter throttles requests that exceed the rate
- **Build** a reusable rate limiter type with blocking and non-blocking acquire

## Why Rate Limiting

Rate limiting controls how frequently an operation can be performed. It protects services from being overwhelmed by too many requests, ensures fair resource sharing, and prevents abuse. The token bucket algorithm is the most widely used rate-limiting approach because it naturally handles both steady-state rate and short bursts.

Consider a real scenario: you build an API endpoint that queries an expensive external service (database, ML model, third-party API). Without rate limiting, a traffic spike or misbehaving client can saturate your backend, degrading service for everyone. A token bucket at 10 requests/second with a burst capacity of 3 means: after an idle period, up to 3 requests are served instantly (burst), then subsequent requests are paced at 10/second. This gives responsive behavior for normal traffic while protecting against overload.

In Go, the token bucket maps perfectly to channels: a buffered channel is the bucket, `time.Ticker` fills it at a constant rate, and workers drain it by receiving tokens. The buffer capacity determines the burst size.

```
  Token Bucket: API Rate Limiter

  +------------------+
  | token  token     |  <- buffered channel (capacity = burst)
  | token            |
  +------------------+
      ^           |
      |           |
   ticker       API handler
   refills      drains
   (10/sec)     (<-tokens)

  Burst: 3 instant requests after idle period
  Steady: 10 req/sec sustained
```

## Step 1 -- Basic Rate Limiter with Ticker

Create a rate limiter that allows one API call per interval, simulating an endpoint that processes incoming requests.

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	fmt.Println("=== Basic API Rate Limiter (5 req/sec) ===\n")

	limiter := time.NewTicker(200 * time.Millisecond)
	defer limiter.Stop()

	type Request struct {
		ID     int
		Path   string
		Client string
	}

	requests := []Request{
		{1, "/api/users/42", "mobile-app"},
		{2, "/api/orders", "web-client"},
		{3, "/api/products/search", "mobile-app"},
		{4, "/api/users/42/profile", "web-client"},
		{5, "/api/orders/create", "mobile-app"},
		{6, "/api/products/99", "web-client"},
		{7, "/api/users/list", "admin-panel"},
		{8, "/api/orders/export", "admin-panel"},
	}

	start := time.Now()
	for _, req := range requests {
		<-limiter.C
		elapsed := time.Since(start).Round(time.Millisecond)
		fmt.Printf("  [%6v] %d %s %s -> 200 OK\n",
			elapsed, req.ID, req.Client, req.Path)
	}
	fmt.Printf("\n  8 requests served in %v (rate: 5/sec)\n", time.Since(start))
}
```

Each `<-limiter.C` blocks until the next tick, enforcing a maximum rate of 5 requests per second.

### Intermediate Verification
```bash
go run main.go
```
Expected: requests spaced ~200ms apart:
```
=== Basic API Rate Limiter (5 req/sec) ===

  [ 200ms] 1 mobile-app /api/users/42 -> 200 OK
  [ 400ms] 2 web-client /api/orders -> 200 OK
  [ 600ms] 3 mobile-app /api/products/search -> 200 OK
  ...

  8 requests served in 1.6s (rate: 5/sec)
```

## Step 2 -- Token Bucket with Burst Support

Implement a token bucket that allows bursts by pre-filling tokens in a buffered channel. This models a real API that allows a short burst of requests after an idle period.

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	fmt.Println("=== Token Bucket with Burst (rate=10/sec, burst=3) ===\n")

	const rate = 100 * time.Millisecond // 10 per second
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
			default: // bucket full, discard token
			}
		}
	}()

	// Simulate a burst of 10 API requests arriving at once
	start := time.Now()
	for req := 1; req <= 10; req++ {
		<-tokens
		elapsed := time.Since(start).Round(time.Millisecond)
		fmt.Printf("  [%6v] request %d -> 200 OK\n", elapsed, req)
	}

	fmt.Println("\n  First 3 requests served instantly (burst).")
	fmt.Println("  Remaining requests throttled to 10/sec.")
}
```

The first 3 requests are served immediately (burst from pre-filled tokens). Subsequent requests are served at the steady-state rate.

### Intermediate Verification
```bash
go run main.go
```
Expected: first 3 instant, then ~100ms apart:
```
=== Token Bucket with Burst (rate=10/sec, burst=3) ===

  [   0ms] request 1 -> 200 OK
  [   0ms] request 2 -> 200 OK
  [   0ms] request 3 -> 200 OK
  [ 100ms] request 4 -> 200 OK
  [ 200ms] request 5 -> 200 OK
  [ 300ms] request 6 -> 200 OK
  ...

  First 3 requests served instantly (burst).
  Remaining requests throttled to 10/sec.
```

## Step 3 -- Rate Limiter as a Reusable Type

Wrap the token bucket into a clean struct with both blocking (`Wait`) and non-blocking (`TryAcquire`) methods. `TryAcquire` is what you use when you want to reject excess requests with HTTP 429 instead of queuing them.

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
	fmt.Println("=== Rate Limiter: Blocking vs Non-Blocking ===\n")

	// Blocking mode: queue requests
	fmt.Println("--- Blocking (Wait) ---")
	rl := NewRateLimiter(100*time.Millisecond, 3)
	start := time.Now()
	for i := 1; i <= 8; i++ {
		rl.Wait()
		fmt.Printf("  [%6v] request %d served\n",
			time.Since(start).Round(time.Millisecond), i)
	}
	rl.Stop()

	// Non-blocking mode: reject excess
	fmt.Println("\n--- Non-Blocking (TryAcquire) ---")
	rl2 := NewRateLimiter(100*time.Millisecond, 3)
	var accepted, rejected int
	for i := 1; i <= 10; i++ {
		if rl2.TryAcquire() {
			accepted++
			fmt.Printf("  request %d -> 200 OK\n", i)
		} else {
			rejected++
			fmt.Printf("  request %d -> 429 Too Many Requests\n", i)
		}
	}
	fmt.Printf("\n  Accepted: %d, Rejected: %d\n", accepted, rejected)

	// Wait for tokens to refill, then try again
	time.Sleep(300 * time.Millisecond)
	fmt.Println("\n--- After 300ms idle (tokens refilled) ---")
	for i := 11; i <= 14; i++ {
		if rl2.TryAcquire() {
			fmt.Printf("  request %d -> 200 OK\n", i)
		} else {
			fmt.Printf("  request %d -> 429 Too Many Requests\n", i)
		}
	}
	rl2.Stop()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected: blocking mode queues, non-blocking mode rejects excess:
```
=== Rate Limiter: Blocking vs Non-Blocking ===

--- Blocking (Wait) ---
  [   0ms] request 1 served
  [   0ms] request 2 served
  [   0ms] request 3 served
  [ 100ms] request 4 served
  [ 200ms] request 5 served
  ...

--- Non-Blocking (TryAcquire) ---
  request 1 -> 200 OK
  request 2 -> 200 OK
  request 3 -> 200 OK
  request 4 -> 429 Too Many Requests
  request 5 -> 429 Too Many Requests
  ...

  Accepted: 3, Rejected: 7

--- After 300ms idle (tokens refilled) ---
  request 11 -> 200 OK
  request 12 -> 200 OK
  request 13 -> 200 OK
  request 14 -> 429 Too Many Requests
```

## Step 4 -- Rate Limiter with Concurrent Workers

Apply the rate limiter to a pool of concurrent API handlers, simulating a real server where multiple goroutines handle requests but all share a single rate limit.

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
	fmt.Println("=== Rate-Limited API Server (10 req/sec, burst=3, 20 handlers) ===\n")

	rl := NewRateLimiter(100*time.Millisecond, 3)
	defer rl.Stop()

	start := time.Now()
	var wg sync.WaitGroup

	// Simulate 20 concurrent API requests
	for i := 1; i <= 20; i++ {
		wg.Add(1)
		go func(reqID int) {
			defer wg.Done()
			rl.Wait() // all goroutines share the rate limiter
			elapsed := time.Since(start).Round(time.Millisecond)
			fmt.Printf("  [%6v] handler processed request %d\n", elapsed, reqID)
			time.Sleep(10 * time.Millisecond) // simulate response generation
		}(i)
	}

	wg.Wait()
	total := time.Since(start)
	fmt.Printf("\n  20 requests processed in %v\n", total)
	fmt.Printf("  Effective rate: %.1f req/sec\n", 20.0/total.Seconds())
}
```

All 20 goroutines launch immediately, but they are throttled by the shared rate limiter.

### Intermediate Verification
```bash
go run main.go
```
Expected: first 3 instant (burst), then ~100ms between each:
```
=== Rate-Limited API Server (10 req/sec, burst=3, 20 handlers) ===

  [   0ms] handler processed request 3
  [   0ms] handler processed request 1
  [   0ms] handler processed request 2
  [ 100ms] handler processed request 5
  [ 200ms] handler processed request 4
  ...

  20 requests processed in 1.7s
  Effective rate: 11.8 req/sec
```

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
- Token bucket: first 3 instant (burst), then ~100ms apart
- Blocking mode: all requests eventually served
- Non-blocking mode: excess requests rejected with 429, then accepted after refill
- Concurrent handlers: 20 requests throttled to ~10/sec effective rate

## What's Next
Continue to [10-end-to-end-pipeline-with-cancel](../10-end-to-end-pipeline-with-cancel/10-end-to-end-pipeline-with-cancel.md) for the capstone exercise combining all patterns from this section.

## Summary
- Token bucket rate limiting maps naturally to Go: buffered channel = bucket, Ticker = refill
- Buffer capacity determines burst size; ticker interval determines steady-state rate
- Pre-fill the bucket for initial burst capacity
- Use `select/default` in the refiller to discard tokens when the bucket is full
- `Wait()` blocks until a token is available (queue excess requests)
- `TryAcquire()` returns immediately (reject excess with HTTP 429)
- The rate limiter works transparently with concurrent handlers sharing a single limiter
- Always stop the ticker to prevent goroutine leaks

## Reference
- [Go by Example: Rate Limiting](https://gobyexample.com/rate-limiting)
- [Token Bucket Algorithm (Wikipedia)](https://en.wikipedia.org/wiki/Token_bucket)
- [golang.org/x/time/rate](https://pkg.go.dev/golang.org/x/time/rate) -- production-grade rate limiter in the extended library
