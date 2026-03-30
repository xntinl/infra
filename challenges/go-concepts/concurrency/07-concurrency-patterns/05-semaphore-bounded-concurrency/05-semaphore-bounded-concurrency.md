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

type UserProfile struct {
	UserID int
	Name   string
	Status string
}

var activeConnections int64

func fetchProfile(userID int) (UserProfile, error) {
	current := atomic.AddInt64(&activeConnections, 1)
	defer atomic.AddInt64(&activeConnections, -1)

	// Simulate API rate limit: reject if > 5 concurrent connections
	if current > 5 {
		return UserProfile{}, fmt.Errorf("HTTP 429: too many requests (active: %d)", current)
	}

	time.Sleep(time.Duration(50+rand.Intn(100)) * time.Millisecond)
	return UserProfile{
		UserID: userID,
		Name:   fmt.Sprintf("User_%d", userID),
		Status: "active",
	}, nil
}

func main() {
	fmt.Println("=== Unbounded Concurrency (NO semaphore) ===")
	fmt.Println("  Launching 30 goroutines with no limit...\n")

	var wg sync.WaitGroup
	var successes, failures int64

	for i := 1; i <= 30; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_, err := fetchProfile(id)
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

type UserProfile struct {
	UserID int
	Name   string
	Status string
}

var activeConnections int64

func fetchProfile(userID int) (UserProfile, error) {
	current := atomic.AddInt64(&activeConnections, 1)
	defer atomic.AddInt64(&activeConnections, -1)

	if current > 5 {
		return UserProfile{}, fmt.Errorf("HTTP 429: too many requests (active: %d)", current)
	}

	time.Sleep(time.Duration(50+rand.Intn(100)) * time.Millisecond)
	return UserProfile{
		UserID: userID,
		Name:   fmt.Sprintf("User_%d", userID),
		Status: "active",
	}, nil
}

func main() {
	fmt.Println("=== Bounded Concurrency (semaphore = 5) ===")

	const maxConcurrent = 5
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	var successes, failures int64
	var maxActive int64

	for i := 1; i <= 30; i++ {
		wg.Add(1)
		sem <- struct{}{} // acquire: blocks if 5 goroutines are already running
		go func(id int) {
			defer wg.Done()
			defer func() { <-sem }() // release

			current := atomic.LoadInt64(&activeConnections)
			if current > maxActive {
				atomic.StoreInt64(&maxActive, current)
			}

			profile, err := fetchProfile(id)
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
		atomic.LoadInt64(&maxActive), maxConcurrent)
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

func main() {
	fmt.Println("=== Semaphore Instrumentation ===")
	const maxConcurrent = 5
	const totalRequests = 20

	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	var active int64
	var peakActive int64

	for i := 1; i <= totalRequests; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(id int) {
			defer wg.Done()
			defer func() { <-sem }()

			current := atomic.AddInt64(&active, 1)
			for {
				old := atomic.LoadInt64(&peakActive)
				if current <= old || atomic.CompareAndSwapInt64(&peakActive, old, current) {
					break
				}
			}

			if current > int64(maxConcurrent) {
				fmt.Printf("  BUG: active=%d exceeds max=%d\n", current, maxConcurrent)
			}

			fmt.Printf("  request %2d: active=%d\n", id, current)
			time.Sleep(time.Duration(50+rand.Intn(100)) * time.Millisecond)
			atomic.AddInt64(&active, -1)
		}(i)
	}

	wg.Wait()
	fmt.Printf("\n  All %d requests completed. Peak active: %d (limit: %d)\n",
		totalRequests, atomic.LoadInt64(&peakActive), maxConcurrent)
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

func simulateAPICall(id int) {
	time.Sleep(time.Duration(50+rand.Intn(100)) * time.Millisecond)
}

func main() {
	const totalRequests = 30
	const concurrency = 5

	// Semaphore approach: one goroutine per request, limited by semaphore
	fmt.Println("=== Semaphore Approach ===")
	start := time.Now()
	sem := make(chan struct{}, concurrency)
	var wg1 sync.WaitGroup
	for i := 0; i < totalRequests; i++ {
		wg1.Add(1)
		sem <- struct{}{}
		go func(id int) {
			defer wg1.Done()
			defer func() { <-sem }()
			simulateAPICall(id)
		}(i)
	}
	wg1.Wait()
	fmt.Printf("  %d requests, max %d concurrent: %v\n\n", totalRequests, concurrency, time.Since(start))

	// Worker pool approach: fixed goroutines, shared job channel
	fmt.Println("=== Worker Pool Approach ===")
	start = time.Now()
	jobs := make(chan int, totalRequests)
	var wg2 sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			for id := range jobs {
				simulateAPICall(id)
			}
		}()
	}
	for i := 0; i < totalRequests; i++ {
		jobs <- i
	}
	close(jobs)
	wg2.Wait()
	fmt.Printf("  %d requests, %d workers: %v\n\n", totalRequests, concurrency, time.Since(start))

	fmt.Println("Both approaches achieve the same bounded concurrency.")
	fmt.Println("Semaphore: one goroutine per task, simpler for heterogeneous work.")
	fmt.Println("Worker pool: fixed goroutines, better for homogeneous long-lived processing.")
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
