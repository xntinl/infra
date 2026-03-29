package main

// Rate Limiter: Token Bucket -- Complete Working Example
//
// The token bucket algorithm: a bucket holds tokens (buffered channel).
// A background ticker adds tokens at a fixed rate. To do work, you must
// take a token. Empty bucket = wait. Full bucket = discard new tokens.
// Buffer capacity = burst size.
//
// Expected output:
//   === Basic Rate Limiter (1 per 200ms) ===
//     request 1 served at XX:XX.XXX
//     request 2 served at XX:XX.XXX  (~200ms later)
//     ...
//
//   === Token Bucket with Burst (burst=3, rate=200ms) ===
//     request 1 served at XX:XX.XXX  (instant, from burst)
//     request 2 served at XX:XX.XXX  (instant, from burst)
//     request 3 served at XX:XX.XXX  (instant, from burst)
//     request 4 served at XX:XX.XXX  (~200ms wait)
//     ...
//
//   === Reusable Rate Limiter ===
//     call 1-3 instant (burst), then ~100ms apart
//
//   === Rate-Limited Workers ===
//     20 goroutines throttled by rate limiter
//
//   === TryAcquire (non-blocking) ===
//     first 2 accepted, rest rejected, then refill

import (
	"fmt"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// basicRateLimiter: uses time.NewTicker to enforce a fixed rate.
// Each operation waits for the next tick before proceeding.
// This is the simplest form: no burst, one operation per interval.
// ---------------------------------------------------------------------------

func basicRateLimiter() {
	fmt.Println("=== Basic Rate Limiter (1 per 200ms) ===")

	// 5 requests per second = one every 200ms.
	limiter := time.NewTicker(200 * time.Millisecond)
	defer limiter.Stop()

	requests := []int{1, 2, 3, 4, 5, 6, 7, 8}
	for _, req := range requests {
		<-limiter.C // Block until the next tick.
		fmt.Printf("  request %d served at %v\n", req, time.Now().Format("04:05.000"))
	}
	fmt.Println()
}

// ---------------------------------------------------------------------------
// tokenBucketLimiter: adds burst support.
//
// The bucket (buffered channel) is pre-filled with tokens. A background
// goroutine refills at a steady rate. Consumers drain tokens to do work.
//
//   +------------------+
//   | token  token     |  <- buffered channel (capacity = burst)
//   | token            |
//   +------------------+
//       ^           |
//       |           |
//    ticker       consumer
//    refills      drains
//
// Pre-filled tokens allow a burst of immediate requests. After the burst
// is consumed, requests are paced by the ticker rate.
// ---------------------------------------------------------------------------

func tokenBucketLimiter() {
	fmt.Println("=== Token Bucket with Burst (burst=3, rate=200ms) ===")
	const rate = 200 * time.Millisecond
	const burstCapacity = 3

	// The bucket: buffered channel holds up to burstCapacity tokens.
	tokens := make(chan struct{}, burstCapacity)

	// Pre-fill: initial burst capacity.
	for i := 0; i < burstCapacity; i++ {
		tokens <- struct{}{}
	}

	// Background refiller: adds a token every `rate` interval.
	ticker := time.NewTicker(rate)
	defer ticker.Stop()
	go func() {
		for range ticker.C {
			select {
			case tokens <- struct{}{}: // Add token if bucket not full.
			default: // Bucket full -- discard token to prevent blocking.
			}
		}
	}()

	// Burst of 10 requests arriving at once.
	for req := 1; req <= 10; req++ {
		<-tokens // Acquire token -- blocks when bucket is empty.
		fmt.Printf("  request %d served at %v\n", req, time.Now().Format("04:05.000"))
	}
	fmt.Println()
}

// ---------------------------------------------------------------------------
// RateLimiter: production-quality reusable type.
// Wraps the token bucket into a clean struct with Wait/TryAcquire/Stop.
// ---------------------------------------------------------------------------

type RateLimiter struct {
	tokens chan struct{}
	ticker *time.Ticker
	stop   chan struct{} // Signals the refiller goroutine to exit.
}

func NewRateLimiter(rate time.Duration, burst int) *RateLimiter {
	rl := &RateLimiter{
		tokens: make(chan struct{}, burst),
		ticker: time.NewTicker(rate),
		stop:   make(chan struct{}),
	}

	// Pre-fill burst capacity.
	for i := 0; i < burst; i++ {
		rl.tokens <- struct{}{}
	}

	// Background refiller with clean shutdown.
	go func() {
		for {
			select {
			case <-rl.ticker.C:
				select {
				case rl.tokens <- struct{}{}: // Refill if room.
				default: // Full -- drop.
				}
			case <-rl.stop:
				return // Clean exit when Stop() is called.
			}
		}
	}()

	return rl
}

// Wait blocks until a token is available (blocking acquire).
func (rl *RateLimiter) Wait() {
	<-rl.tokens
}

// TryAcquire attempts to take a token without blocking.
// Returns true if a token was available, false otherwise.
func (rl *RateLimiter) TryAcquire() bool {
	select {
	case <-rl.tokens:
		return true
	default:
		return false
	}
}

// Stop cleans up the ticker and refiller goroutine.
// Always call this when the limiter is no longer needed.
func (rl *RateLimiter) Stop() {
	rl.ticker.Stop()
	close(rl.stop)
}

// ---------------------------------------------------------------------------
// rateLimitedWorkers: applies the limiter to concurrent goroutines.
// All 20 goroutines launch immediately but are throttled by the limiter.
// At most `burst` pass through instantly, then one per `rate` interval.
// ---------------------------------------------------------------------------

func rateLimitedWorkers() {
	fmt.Println("=== Rate-Limited Workers ===")
	rl := NewRateLimiter(100*time.Millisecond, 2) // 10/sec, burst 2
	defer rl.Stop()

	var wg sync.WaitGroup
	for i := 1; i <= 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			rl.Wait() // Acquire rate-limit token.
			fmt.Printf("  worker %2d: started at %v\n", id, time.Now().Format("04:05.000"))
			time.Sleep(50 * time.Millisecond) // Simulate work.
		}(i)
	}

	wg.Wait()
	fmt.Println()
}

// ---------------------------------------------------------------------------
// tryAcquireDemo: shows non-blocking rate limiting.
// Useful when you want to reject excess requests immediately instead of
// queuing them (HTTP 429 Too Many Requests pattern).
// ---------------------------------------------------------------------------

func tryAcquireDemo() {
	fmt.Println("=== TryAcquire (non-blocking) ===")
	rl := NewRateLimiter(200*time.Millisecond, 2)
	defer rl.Stop()

	// Try 10 rapid requests. Only the first 2 (burst) should succeed.
	fmt.Println("  Rapid burst of 10 requests:")
	accepted, rejected := 0, 0
	for i := 1; i <= 10; i++ {
		if rl.TryAcquire() {
			fmt.Printf("    request %d: ACCEPTED\n", i)
			accepted++
		} else {
			fmt.Printf("    request %d: REJECTED (rate limited)\n", i)
			rejected++
		}
	}
	fmt.Printf("  Summary: %d accepted, %d rejected\n", accepted, rejected)

	// Wait for tokens to refill, then try again.
	fmt.Println("\n  Waiting 500ms for token refill...")
	time.Sleep(500 * time.Millisecond)

	fmt.Println("  After refill:")
	for i := 1; i <= 3; i++ {
		if rl.TryAcquire() {
			fmt.Printf("    request %d: ACCEPTED\n", i)
		} else {
			fmt.Printf("    request %d: REJECTED\n", i)
		}
	}
	fmt.Println()
}

func main() {
	fmt.Println("Exercise: Rate Limiter -- Token Bucket")
	fmt.Println()

	// Ticker-based rate limiting (no burst)
	basicRateLimiter()

	// Token bucket with burst support
	tokenBucketLimiter()

	// Reusable rate limiter type
	fmt.Println("=== Reusable Rate Limiter ===")
	rl := NewRateLimiter(100*time.Millisecond, 3)
	for i := 1; i <= 8; i++ {
		rl.Wait()
		fmt.Printf("  call %d at %v\n", i, time.Now().Format("04:05.000"))
	}
	rl.Stop()
	fmt.Println()

	// Rate-limited workers
	rateLimitedWorkers()

	// Non-blocking TryAcquire
	tryAcquireDemo()
}
