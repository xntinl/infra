---
difficulty: intermediate
concepts: [semaphore, buffered channel, bounded concurrency, backpressure]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [goroutines, channels, sync.WaitGroup, worker pool]
---

# 5. Semaphore: Bounded Concurrency

## Learning Objectives
After completing this exercise, you will be able to:
- **Use** a buffered channel as a counting semaphore
- **Limit** the number of concurrently executing goroutines
- **Compare** the semaphore approach with fixed worker pools
- **Apply** bounded concurrency to protect rate-limited resources

## Why Semaphores

A semaphore limits the number of concurrent operations. In Go, a buffered channel is a natural semaphore: sending to it "acquires" a slot, and receiving from it "releases" a slot. When the buffer is full, the next acquire blocks until someone releases.

Consider a real scenario: your service fetches user profiles from a third-party API that enforces a rate limit of 5 concurrent connections. If you exceed this, you get HTTP 429 "Too Many Requests" responses and your requests are rejected. You have 50 user profiles to fetch. Launching 50 goroutines simultaneously hammers the API, but limiting concurrency to 5 with a semaphore keeps you within the limit while still being 5x faster than sequential.

The semaphore pattern differs from worker pools in a key way. With a worker pool, you have a fixed set of long-lived goroutines processing a shared queue. With a semaphore, you launch a new goroutine per task but limit how many run simultaneously.

```
  Semaphore Flow: API Client with 5 Concurrent Connections

  for each user:
    sem <- struct{}{}          // ACQUIRE (blocks if 5 already running)
    go func() {
      defer func() { <-sem }() // RELEASE
      fetchProfile(user)
    }()

  Buffered channel capacity = max concurrent API connections
```

## Step 1 -- The Problem: Unbounded Concurrency

First, see what happens when you launch a goroutine per request without any limit. The API rejects requests when too many arrive simultaneously.

```go
package main

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

const (
	apiConnectionLimit  = 5
	totalUnboundedUsers = 30
)

// UserProfile represents a user record from the external API.
type UserProfile struct {
	UserID int
	Name   string
	Status string
}

// APIClient simulates a rate-limited external API.
type APIClient struct {
	activeConnections int64
	maxConnections    int64
}

func NewAPIClient(maxConnections int) *APIClient {
	return &APIClient{maxConnections: int64(maxConnections)}
}

func (api *APIClient) FetchProfile(userID int) (UserProfile, error) {
	current := atomic.AddInt64(&api.activeConnections, 1)
	defer atomic.AddInt64(&api.activeConnections, -1)

	if current > api.maxConnections {
		return UserProfile{}, fmt.Errorf("HTTP 429: too many requests (active: %d)", current)
	}

	time.Sleep(time.Duration(50+rand.Intn(100)) * time.Millisecond)
	return UserProfile{
		UserID: userID,
		Name:   fmt.Sprintf("User_%d", userID),
		Status: "active",
	}, nil
}

func runUnbounded(api *APIClient) {
	fmt.Println("=== Unbounded Concurrency (NO semaphore) ===")
	fmt.Println("  Launching 30 goroutines with no limit...")
	fmt.Println()

	var wg sync.WaitGroup
	var successes, failures int64

	for i := 1; i <= totalUnboundedUsers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_, err := api.FetchProfile(id)
			if err != nil {
				atomic.AddInt64(&failures, 1)
				fmt.Printf("  user %2d: FAILED - %v\n", id, err)
			} else {
				atomic.AddInt64(&successes, 1)
			}
		}(i)
	}

	wg.Wait()
	fmt.Printf("\n  Results: %d succeeded, %d failed (429 errors)\n",
		atomic.LoadInt64(&successes), atomic.LoadInt64(&failures))
	fmt.Println("  The API rejected most requests because we exceeded the concurrent limit.")
}

func main() {
	api := NewAPIClient(apiConnectionLimit)
	runUnbounded(api)
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected: most requests fail with 429 errors:
```
=== Unbounded Concurrency (NO semaphore) ===
  Launching 30 goroutines with no limit...

  user  3: FAILED - HTTP 429: too many requests (active: 12)
  user  8: FAILED - HTTP 429: too many requests (active: 18)
  ...

  Results: 5 succeeded, 25 failed (429 errors)
  The API rejected most requests because we exceeded the concurrent limit.
```

## Step 2 -- Fix It with a Semaphore

Add a buffered channel as a semaphore to limit concurrent API connections to 5.

```go
package main

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

const (
	maxConcurrentConnections = 5
	totalBoundedUsers        = 30
)

// UserProfile represents a user record from the external API.
type UserProfile struct {
	UserID int
	Name   string
	Status string
}

// APIClient simulates a rate-limited external API.
type APIClient struct {
	activeConnections int64
	maxConnections    int64
}

func NewAPIClient(maxConnections int) *APIClient {
	return &APIClient{maxConnections: int64(maxConnections)}
}

func (api *APIClient) FetchProfile(userID int) (UserProfile, error) {
	current := atomic.AddInt64(&api.activeConnections, 1)
	defer atomic.AddInt64(&api.activeConnections, -1)

	if current > api.maxConnections {
		return UserProfile{}, fmt.Errorf("HTTP 429: too many requests (active: %d)", current)
	}

	time.Sleep(time.Duration(50+rand.Intn(100)) * time.Millisecond)
	return UserProfile{
		UserID: userID,
		Name:   fmt.Sprintf("User_%d", userID),
		Status: "active",
	}, nil
}

func runBounded(api *APIClient) {
	fmt.Println("=== Bounded Concurrency (semaphore = 5) ===")

	sem := make(chan struct{}, maxConcurrentConnections)
	var wg sync.WaitGroup
	var successes, failures int64
	var maxActive int64

	for i := 1; i <= totalBoundedUsers; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(id int) {
			defer wg.Done()
			defer func() { <-sem }()

			current := atomic.LoadInt64(&api.activeConnections)
			if current > maxActive {
				atomic.StoreInt64(&maxActive, current)
			}

			profile, err := api.FetchProfile(id)
			if err != nil {
				atomic.AddInt64(&failures, 1)
				fmt.Printf("  user %2d: FAILED - %v\n", id, err)
			} else {
				atomic.AddInt64(&successes, 1)
				fmt.Printf("  user %2d: OK - %s (%s)\n", id, profile.Name, profile.Status)
			}
		}(i)
	}

	wg.Wait()
	fmt.Printf("\n  Results: %d succeeded, %d failed\n",
		atomic.LoadInt64(&successes), atomic.LoadInt64(&failures))
	fmt.Printf("  Max concurrent connections: %d (limit: %d)\n",
		atomic.LoadInt64(&maxActive), maxConcurrentConnections)
}

func main() {
	api := NewAPIClient(maxConcurrentConnections)
	runBounded(api)
}
```

The `sem` channel has capacity 5. When 5 goroutines are running, the 6th `sem <- struct{}{}` blocks until one finishes and releases its slot with `<-sem`.

### Intermediate Verification
```bash
go run main.go
```
Expected: all requests succeed because concurrency stays within the API limit:
```
=== Bounded Concurrency (semaphore = 5) ===
  user  1: OK - User_1 (active)
  user  2: OK - User_2 (active)
  ...

  Results: 30 succeeded, 0 failed
  Max concurrent connections: 5 (limit: 5)
```

## Step 3 -- Track Active Goroutines

Add instrumentation to prove the semaphore works by tracking the count of active goroutines over time.

```go
package main

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

const (
	instrumentMaxConcurrent = 5
	instrumentTotalRequests = 20
)

// SemaphoreInstrument tracks active goroutine counts through a semaphore.
type SemaphoreInstrument struct {
	maxConcurrent int
	totalRequests int
	active        int64
	peakActive    int64
}

func NewSemaphoreInstrument(maxConcurrent, totalRequests int) *SemaphoreInstrument {
	return &SemaphoreInstrument{
		maxConcurrent: maxConcurrent,
		totalRequests: totalRequests,
	}
}

func (si *SemaphoreInstrument) updatePeak(current int64) {
	for {
		old := atomic.LoadInt64(&si.peakActive)
		if current <= old || atomic.CompareAndSwapInt64(&si.peakActive, old, current) {
			break
		}
	}
}

func (si *SemaphoreInstrument) handleRequest(id int) {
	current := atomic.AddInt64(&si.active, 1)
	si.updatePeak(current)

	if current > int64(si.maxConcurrent) {
		fmt.Printf("  BUG: active=%d exceeds max=%d\n", current, si.maxConcurrent)
	}

	fmt.Printf("  request %2d: active=%d\n", id, current)
	time.Sleep(time.Duration(50+rand.Intn(100)) * time.Millisecond)
	atomic.AddInt64(&si.active, -1)
}

func (si *SemaphoreInstrument) Run() {
	fmt.Println("=== Semaphore Instrumentation ===")

	sem := make(chan struct{}, si.maxConcurrent)
	var wg sync.WaitGroup

	for i := 1; i <= si.totalRequests; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(id int) {
			defer wg.Done()
			defer func() { <-sem }()
			si.handleRequest(id)
		}(i)
	}

	wg.Wait()
	fmt.Printf("\n  All %d requests completed. Peak active: %d (limit: %d)\n",
		si.totalRequests, atomic.LoadInt64(&si.peakActive), si.maxConcurrent)
}

func main() {
	instrument := NewSemaphoreInstrument(instrumentMaxConcurrent, instrumentTotalRequests)
	instrument.Run()
}
```

The active count should never exceed `maxConcurrent`.

### Intermediate Verification
```bash
go run main.go
```
Expected: active count stays at or below 5.

## Step 4 -- Compare Semaphore vs Worker Pool

Implement the same work using both approaches side by side to understand the tradeoffs.

```go
package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

const (
	comparisonRequests    = 30
	comparisonConcurrency = 5
)

// ConcurrencyComparison benchmarks semaphore vs worker pool approaches.
type ConcurrencyComparison struct {
	totalRequests int
	concurrency   int
}

func NewConcurrencyComparison() *ConcurrencyComparison {
	return &ConcurrencyComparison{
		totalRequests: comparisonRequests,
		concurrency:   comparisonConcurrency,
	}
}

func simulateAPICall(id int) {
	time.Sleep(time.Duration(50+rand.Intn(100)) * time.Millisecond)
}

func (cc *ConcurrencyComparison) RunSemaphore() time.Duration {
	start := time.Now()
	sem := make(chan struct{}, cc.concurrency)
	var wg sync.WaitGroup

	for i := 0; i < cc.totalRequests; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(id int) {
			defer wg.Done()
			defer func() { <-sem }()
			simulateAPICall(id)
		}(i)
	}
	wg.Wait()
	return time.Since(start)
}

func (cc *ConcurrencyComparison) RunWorkerPool() time.Duration {
	start := time.Now()
	jobs := make(chan int, cc.totalRequests)
	var wg sync.WaitGroup

	for w := 0; w < cc.concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				simulateAPICall(id)
			}
		}()
	}

	for i := 0; i < cc.totalRequests; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return time.Since(start)
}

func (cc *ConcurrencyComparison) Run() {
	fmt.Println("=== Semaphore Approach ===")
	semDuration := cc.RunSemaphore()
	fmt.Printf("  %d requests, max %d concurrent: %v\n\n", cc.totalRequests, cc.concurrency, semDuration)

	fmt.Println("=== Worker Pool Approach ===")
	poolDuration := cc.RunWorkerPool()
	fmt.Printf("  %d requests, %d workers: %v\n\n", cc.totalRequests, cc.concurrency, poolDuration)

	fmt.Println("Both approaches achieve the same bounded concurrency.")
	fmt.Println("Semaphore: one goroutine per task, simpler for heterogeneous work.")
	fmt.Println("Worker pool: fixed goroutines, better for homogeneous long-lived processing.")
}

func main() {
	comparison := NewConcurrencyComparison()
	comparison.Run()
}
```

### Intermediate Verification
```bash
go run main.go
```
Both approaches should take roughly the same time.

## Common Mistakes

### Acquiring Inside the Goroutine
**Wrong:**
```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	sem := make(chan struct{}, 5)
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) { // ALL 100 goroutines launch immediately
			sem <- struct{}{}     // acquire inside goroutine
			defer func() { <-sem }()
			defer wg.Done()
			fmt.Printf("request %d\n", id)
			time.Sleep(100 * time.Millisecond)
		}(i)
	}
	wg.Wait()
}
```
**What happens:** All goroutines launch immediately (unbounded), then compete for the semaphore. You get a burst of goroutine creation, defeating the purpose of bounding concurrency.

**Fix:** Acquire the semaphore before launching the goroutine. This blocks the launching loop, ensuring at most N goroutines exist at any time.

### Forgetting to Release
**Wrong:**
```go
go func(id int) {
	defer wg.Done()
	// forgot: defer func() { <-sem }()
	fetchProfile(id)
}(i)
```
**What happens:** Slots are acquired but never released. After N tasks, the program deadlocks.

**Fix:** Always pair acquire with a deferred release. Using `defer` ensures release happens even if the goroutine panics.

### Using a Mutex Instead of a Semaphore
A mutex limits concurrency to 1. If you need N > 1, a mutex does not work. A buffered channel generalizes to any N.

## Verify What You Learned

Run `go run main.go` and verify:
- Unbounded: most of the 30 requests fail with 429 errors
- Semaphore: all 30 requests succeed, concurrent connections never exceed 5
- Instrumentation: active count never exceeds the limit
- Comparison: semaphore and worker pool achieve similar performance

## What's Next
Continue to [06-generator-lazy-production](../06-generator-lazy-production/06-generator-lazy-production.md) to learn how to produce values lazily with channels.

## Summary
- A buffered channel of `struct{}` is Go's idiomatic counting semaphore
- Acquire: `sem <- struct{}{}` (blocks when buffer is full)
- Release: `<-sem` (frees a slot for another goroutine)
- Acquire before `go func()` to limit goroutine creation, not just execution
- Semaphores give per-task goroutines; worker pools reuse fixed goroutines
- Real-world use: respecting API rate limits, database connection limits, file descriptor limits

## Reference
- [Effective Go: Channels as Semaphores](https://go.dev/doc/effective_go#channels)
- [Go Blog: Advanced Concurrency Patterns](https://go.dev/blog/advanced-go-concurrency-patterns)
- [golang.org/x/sync/semaphore](https://pkg.go.dev/golang.org/x/sync/semaphore) -- weighted semaphore in the extended library
