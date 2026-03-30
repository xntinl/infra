---
difficulty: intermediate
concepts: [mutex vs channel, share memory by communicating, state ownership, Go proverb]
tools: [go]
estimated_time: 25m
bloom_level: analyze
prerequisites: [sync.Mutex, channels, goroutines, sync.WaitGroup]
---

# 7. Mutex vs Channel: Decision Criteria


## Learning Objectives
After completing this exercise, you will be able to:
- **Solve** the same concurrency problem using both mutex and channel approaches
- **Compare** code clarity, safety, and performance of each approach
- **Apply** the decision framework: mutex for protecting state, channels for communication
- **Explain** the Go proverb "share memory by communicating"

## Why This Decision Matters
Go provides two fundamental mechanisms for concurrent coordination: mutexes and channels. Both are correct; neither is universally better. Choosing the wrong tool leads to code that is harder to understand, harder to maintain, and more prone to subtle bugs.

The Go proverb says: **"Do not communicate by sharing memory; share memory by communicating."** This does not mean "never use mutexes." It means: when goroutines need to exchange information or coordinate work, channels are usually clearer. When goroutines need to protect a piece of shared state from concurrent access, mutexes are usually simpler.

This exercise presents two real scenarios from production systems and implements each with both approaches, so you can see which fits naturally and which feels forced.

## Step 1 -- Scenario 1: Shared Metrics Map (Better with Mutex)

An HTTP server tracks request counts per endpoint. Multiple handler goroutines increment counters concurrently. A metrics endpoint reads the totals.

**Mutex approach -- natural fit:**

```go
package main

import (
	"fmt"
	"sync"
)

type MetricsStore struct {
	mu       sync.Mutex
	counters map[string]int64
}

func NewMetricsStore() *MetricsStore {
	return &MetricsStore{counters: make(map[string]int64)}
}

func (m *MetricsStore) Increment(endpoint string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counters[endpoint]++
}

func (m *MetricsStore) Snapshot() map[string]int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[string]int64, len(m.counters))
	for k, v := range m.counters {
		result[k] = v
	}
	return result
}

func main() {
	metrics := NewMetricsStore()
	var wg sync.WaitGroup

	endpoints := []string{"/api/users", "/api/orders", "/api/products", "/healthz"}

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(reqID int) {
			defer wg.Done()
			ep := endpoints[reqID%len(endpoints)]
			metrics.Increment(ep)
		}(i)
	}

	wg.Wait()

	fmt.Println("=== Request Metrics (mutex approach) ===")
	snap := metrics.Snapshot()
	total := int64(0)
	for ep, count := range snap {
		fmt.Printf("  %-20s %d requests\n", ep, count)
		total += count
	}
	fmt.Printf("  %-20s %d requests\n", "TOTAL", total)
}
```

Expected output:
```
=== Request Metrics (mutex approach) ===
  /api/users           250 requests
  /api/orders          250 requests
  /api/products        250 requests
  /healthz             250 requests
  TOTAL                1000 requests
```

This is clean: the mutex protects the map, each method is short, and the API is intuitive.

### Intermediate Verification
```bash
go run -race main.go
```
Total should be exactly 1000, no race conditions.

## Step 2 -- Scenario 1 with Channels (Forced Fit)

Now implement the same metrics store using a channel-based goroutine owner:

```go
package main

import (
	"fmt"
	"sync"
)

type metricsOp struct {
	kind     string
	endpoint string
	response chan map[string]int64
}

type ChannelMetrics struct {
	ops  chan metricsOp
	done chan struct{}
}

func NewChannelMetrics() *ChannelMetrics {
	m := &ChannelMetrics{
		ops:  make(chan metricsOp),
		done: make(chan struct{}),
	}
	go m.run()
	return m
}

func (m *ChannelMetrics) run() {
	counters := make(map[string]int64)
	for op := range m.ops {
		switch op.kind {
		case "inc":
			counters[op.endpoint]++
		case "snapshot":
			result := make(map[string]int64, len(counters))
			for k, v := range counters {
				result[k] = v
			}
			op.response <- result
		}
	}
	close(m.done)
}

func (m *ChannelMetrics) Increment(endpoint string) {
	m.ops <- metricsOp{kind: "inc", endpoint: endpoint}
}

func (m *ChannelMetrics) Snapshot() map[string]int64 {
	resp := make(chan map[string]int64)
	m.ops <- metricsOp{kind: "snapshot", response: resp}
	return <-resp
}

func (m *ChannelMetrics) Close() {
	close(m.ops)
	<-m.done
}

func main() {
	metrics := NewChannelMetrics()
	var wg sync.WaitGroup

	endpoints := []string{"/api/users", "/api/orders", "/api/products", "/healthz"}

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(reqID int) {
			defer wg.Done()
			ep := endpoints[reqID%len(endpoints)]
			metrics.Increment(ep)
		}(i)
	}

	wg.Wait()

	fmt.Println("=== Request Metrics (channel approach) ===")
	snap := metrics.Snapshot()
	total := int64(0)
	for ep, count := range snap {
		fmt.Printf("  %-20s %d requests\n", ep, count)
		total += count
	}
	fmt.Printf("  %-20s %d requests\n", "TOTAL", total)
	metrics.Close()
}
```

This works but requires more code: an operation struct, a response channel, a background goroutine, and a Close method. For simple state protection, the channel approach adds complexity without clarity.

### Intermediate Verification
```bash
go run -race main.go
```
Same result, but more ceremony to achieve it.

## Step 3 -- Scenario 2: Processing Pipeline (Better with Channels)

An image processing pipeline: fetch URLs, download images, resize them, and write to disk. Each stage runs independently and passes work to the next.

**Channel approach -- natural fit:**

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type ImageJob struct {
	URL       string
	Stage     string
	Data      string
}

func fetcher(urls []string, out chan<- ImageJob) {
	for _, url := range urls {
		time.Sleep(20 * time.Millisecond) // simulate HTTP fetch
		out <- ImageJob{URL: url, Stage: "fetched", Data: "raw-bytes"}
		fmt.Printf("  [fetch]  %s\n", url)
	}
	close(out)
}

func resizer(in <-chan ImageJob, out chan<- ImageJob) {
	for job := range in {
		time.Sleep(30 * time.Millisecond) // simulate resize
		job.Stage = "resized"
		job.Data = "resized-bytes"
		out <- job
		fmt.Printf("  [resize] %s\n", job.URL)
	}
	close(out)
}

func writer(in <-chan ImageJob, done chan<- struct{}) {
	for job := range in {
		time.Sleep(10 * time.Millisecond) // simulate disk write
		fmt.Printf("  [write]  %s -> saved\n", job.URL)
	}
	close(done)
}

func main() {
	urls := []string{
		"cdn.example.com/img/001.jpg",
		"cdn.example.com/img/002.jpg",
		"cdn.example.com/img/003.jpg",
		"cdn.example.com/img/004.jpg",
		"cdn.example.com/img/005.jpg",
	}

	fmt.Println("=== Image Processing Pipeline (channels) ===")
	start := time.Now()

	fetchCh := make(chan ImageJob, 2)
	resizeCh := make(chan ImageJob, 2)
	done := make(chan struct{})

	go fetcher(urls, fetchCh)
	go resizer(fetchCh, resizeCh)
	go writer(resizeCh, done)

	<-done
	fmt.Printf("\nPipeline complete: %d images in %v\n", len(urls), time.Since(start).Round(time.Millisecond))
}
```

Expected output:
```
=== Image Processing Pipeline (channels) ===
  [fetch]  cdn.example.com/img/001.jpg
  [resize] cdn.example.com/img/001.jpg
  [fetch]  cdn.example.com/img/002.jpg
  [write]  cdn.example.com/img/001.jpg -> saved
  ...

Pipeline complete: 5 images in ~180ms
```

The pipeline stages are naturally connected by channels. Each stage runs independently, processes items as they arrive, and the entire pipeline overlaps fetch/resize/write for maximum throughput.

### Intermediate Verification
```bash
go run main.go
```
All 5 images should flow through the pipeline. Total time should be less than doing all stages sequentially.

## Step 4 -- Scenario 2 with Mutex (Forced Fit)

Now try the same pipeline with mutexes. You end up polling shared state:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type ImageJob struct {
	URL   string
	Stage string
}

func main() {
	fmt.Println("=== Image Processing Pipeline (mutex -- awkward) ===")

	var mu sync.Mutex
	fetchedQueue := make([]ImageJob, 0)
	resizedQueue := make([]ImageJob, 0)
	fetchDone := false
	resizeDone := false

	urls := []string{
		"cdn.example.com/img/001.jpg",
		"cdn.example.com/img/002.jpg",
		"cdn.example.com/img/003.jpg",
	}

	var wg sync.WaitGroup

	// Fetcher
	wg.Add(1)
	go func() {
		defer wg.Done()
		for _, url := range urls {
			time.Sleep(20 * time.Millisecond)
			mu.Lock()
			fetchedQueue = append(fetchedQueue, ImageJob{URL: url, Stage: "fetched"})
			mu.Unlock()
			fmt.Printf("  [fetch] %s\n", url)
		}
		mu.Lock()
		fetchDone = true
		mu.Unlock()
	}()

	// Resizer: must POLL the fetchedQueue
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			mu.Lock()
			if len(fetchedQueue) > 0 {
				job := fetchedQueue[0]
				fetchedQueue = fetchedQueue[1:]
				mu.Unlock()
				time.Sleep(30 * time.Millisecond)
				mu.Lock()
				job.Stage = "resized"
				resizedQueue = append(resizedQueue, job)
				mu.Unlock()
				fmt.Printf("  [resize] %s\n", job.URL)
			} else if fetchDone {
				mu.Unlock()
				mu.Lock()
				resizeDone = true
				mu.Unlock()
				return
			} else {
				mu.Unlock()
				time.Sleep(5 * time.Millisecond) // POLLING -- wasteful
			}
		}
	}()

	// Writer: must POLL the resizedQueue
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			mu.Lock()
			if len(resizedQueue) > 0 {
				job := resizedQueue[0]
				resizedQueue = resizedQueue[1:]
				mu.Unlock()
				time.Sleep(10 * time.Millisecond)
				fmt.Printf("  [write] %s -> saved\n", job.URL)
			} else if resizeDone {
				mu.Unlock()
				return
			} else {
				mu.Unlock()
				time.Sleep(5 * time.Millisecond) // POLLING -- wasteful
			}
		}
	}()

	wg.Wait()
	fmt.Println("\nThis works, but the polling loops are ugly, wasteful, and error-prone.")
	fmt.Println("Channels are the natural fit for pipeline coordination.")
}
```

The mutex version works but requires polling loops, manual "done" flags, and careful lock ordering. The channel version is half the code and clearly communicates intent.

### Intermediate Verification
```bash
go run main.go
```
Same functional result, but the code is noticeably more complex and harder to reason about.

## Step 5 -- Decision Guide

```
Use MUTEX when:
  - Protecting internal state of a struct (counters, caches, config maps)
  - Simple read/write access patterns
  - Performance is critical (lower per-operation overhead)
  - The protected data has a clear owner (a single struct)

Use CHANNELS when:
  - Transferring data ownership between goroutines (pipelines)
  - Coordinating sequential phases of work
  - Fan-out/fan-in patterns
  - Select-based multiplexing with timeouts or cancellation
  - Signaling events (done, shutdown, ready)

Code smell indicators:
  - Using a buffered channel of size 1 as a lock -> use a mutex
  - Polling a mutex-protected flag in a loop -> use a channel
  - Channel with no data (chan struct{}) only for signaling -> consider sync.Once or context
```

## Common Mistakes

### Channel as a Mutex

```go
sem := make(chan struct{}, 1)
sem <- struct{}{} // "lock"
counter++
<-sem             // "unlock"
```

**Why this is a code smell:** It works but is a mutex in disguise. A real `sync.Mutex` is clearer, lighter, and has better tooling support (race detector, deadlock detection).

### Mutex for Pipeline Coordination

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	var mu sync.Mutex
	var phase1Done bool

	go func() {
		time.Sleep(100 * time.Millisecond)
		mu.Lock()
		phase1Done = true
		mu.Unlock()
	}()

	// Polling loop -- wasteful and ugly
	for {
		mu.Lock()
		done := phase1Done
		mu.Unlock()
		if done {
			break
		}
		time.Sleep(time.Millisecond)
	}
	fmt.Println("phase 1 done (but this code is terrible)")
}
```

**Why this is a code smell:** This is coordination, not state protection. A channel is far cleaner:
```go
phase1Done := make(chan struct{})
go func() {
    doPhase1()
    close(phase1Done)
}()
<-phase1Done // blocks cleanly, no polling
```

### Over-Channeling Simple State
Not every shared variable needs a channel. A cache miss counter, a request count, a configuration flag -- these are naturally protected by a mutex or even `sync/atomic`. Using channels for them adds unnecessary goroutines and complexity.

## Verify What You Learned

Implement a concurrent log aggregator two ways:
1. **Mutex approach**: multiple goroutines write log entries to a shared slice protected by a mutex. A flush goroutine periodically reads and clears the slice.
2. **Channel approach**: goroutines send log entries through a channel to a single writer goroutine that batches and flushes them.

Compare code clarity, correctness, and which approach feels more natural for this specific problem.

## What's Next
Continue to [08-nested-locking-deadlock](../08-nested-locking-deadlock/08-nested-locking-deadlock.md) to learn how nested lock acquisition leads to deadlocks and how to prevent them.

## Summary
- Both mutexes and channels are valid concurrency tools; neither is universally better
- Mutex excels at protecting internal state of a struct (counter, cache, config map)
- Channels excel at transferring data between pipeline stages, coordinating work phases, and signaling events
- Using a channel as a mutex or a mutex for coordination are code smells
- The Go proverb is guidance, not dogma: choose the tool that makes the code clearest
- When in doubt: if a struct owns the data, use a mutex; if goroutines pass data, use a channel

## Reference
- [Go Wiki: MutexOrChannel](https://go.dev/wiki/MutexOrChannel)
- [Effective Go: Concurrency](https://go.dev/doc/effective_go#concurrency)
- [Go Proverbs: Rob Pike](https://go-proverbs.github.io/)
- [Bryan Mills - Rethinking Classical Concurrency Patterns](https://www.youtube.com/watch?v=5zXAHh5tJqQ)
