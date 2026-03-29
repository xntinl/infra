package main

// Exercise: Rate Limiter -- Token Bucket
// Instructions: see 09-rate-limiter-token-bucket.md

import (
	"fmt"
	"sync"
	"time"
)

// Step 1: Implement basicRateLimiter.
// Use time.NewTicker to enforce one request per 200ms.
// Serve 8 requests, printing each with its timestamp.
func basicRateLimiter() {
	fmt.Println("=== Basic Rate Limiter ===")

	// TODO: create ticker at 200ms interval
	// TODO: defer ticker.Stop()
	// TODO: for each request 1..8:
	//   - wait for tick (<-limiter.C)
	//   - print request number and timestamp

	fmt.Println()
}

// Step 2: Implement tokenBucketLimiter.
// Create a buffered channel (capacity=burstCapacity) as the token bucket.
// Pre-fill it. Launch a background goroutine that adds a token every `rate` interval.
// Serve 10 requests, observing burst then steady-state behavior.
func tokenBucketLimiter() {
	fmt.Println("=== Token Bucket with Burst ===")
	const rate = 200 * time.Millisecond
	const burstCapacity = 3
	_ = rate
	_ = burstCapacity

	// TODO: create tokens channel (buffered, capacity burstCapacity)
	// TODO: pre-fill with burstCapacity tokens
	// TODO: start ticker, launch refiller goroutine (select with default to discard overflow)
	// TODO: serve 10 requests: <-tokens before each, print timestamp

	fmt.Println()
}

// Step 3: Implement RateLimiter as a reusable type.

type RateLimiter struct {
	tokens chan struct{}
	ticker *time.Ticker
}

// NewRateLimiter creates a rate limiter with the given rate and burst capacity.
func NewRateLimiter(rate time.Duration, burst int) *RateLimiter {
	// TODO: create the struct
	// TODO: pre-fill tokens
	// TODO: launch background refiller goroutine
	_ = rate
	_ = burst
	return &RateLimiter{
		tokens: make(chan struct{}, 1),
		ticker: time.NewTicker(rate),
	}
}

// Wait blocks until a token is available.
func (rl *RateLimiter) Wait() {
	// TODO: receive from tokens channel
}

// Stop cleans up the ticker.
func (rl *RateLimiter) Stop() {
	// TODO: stop the ticker
}

// Step 4: Implement rateLimitedWorkers.
// Launch 20 goroutines, each acquiring a rate limit token before starting work.
func rateLimitedWorkers() {
	fmt.Println("=== Rate-Limited Workers ===")

	// TODO: create rate limiter (100ms rate, burst 2)
	// TODO: launch 20 goroutines, each calling rl.Wait() before working
	// TODO: print worker id and timestamp when started
	// TODO: wait for all workers

	fmt.Println()
}

// Verify: Implement TryAcquire (non-blocking).
// Returns true if a token was available, false otherwise.
func (rl *RateLimiter) TryAcquire() bool {
	// TODO: use select with default to try receiving from tokens
	return false
}

func tryAcquireDemo() {
	fmt.Println("=== Verify: TryAcquire ===")

	// TODO: create rate limiter (200ms rate, burst 2)
	// TODO: try 10 rapid requests
	// TODO: print whether each was accepted or rejected
	// TODO: sleep briefly and try again to show token refill

	fmt.Println()
}

func main() {
	fmt.Println("Exercise: Rate Limiter -- Token Bucket\n")

	// Step 1
	basicRateLimiter()

	// Step 2
	tokenBucketLimiter()

	// Step 3
	fmt.Println("=== Reusable Rate Limiter ===")
	rl := NewRateLimiter(100*time.Millisecond, 3)
	for i := 1; i <= 8; i++ {
		rl.Wait()
		fmt.Printf("  call %d at %v\n", i, time.Now().Format("04:05.000"))
	}
	rl.Stop()
	fmt.Println()

	// Step 4
	rateLimitedWorkers()

	// Verify
	tryAcquireDemo()

	_ = sync.WaitGroup{} // hint
}
