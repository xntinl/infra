---
difficulty: intermediate
concepts: [sync.Mutex, Lock, Unlock, defer, critical section, contention, encapsulation]
tools: [go]
estimated_time: 25m
bloom_level: apply
---

# 3. Fix Race with Mutex


## Learning Objectives
After completing this exercise, you will be able to:
- **Fix** a data race by protecting shared state with `sync.Mutex`
- **Apply** the `Lock()`/`defer Unlock()` idiom correctly
- **Encapsulate** locking inside a MetricsCollector struct
- **Protect** a map of counters (request counts per endpoint)
- **Verify** the fix using the `-race` flag

## Why Mutex

A `sync.Mutex` provides **mutual exclusion**: only one goroutine can hold the lock at a time. All others block until the lock is released. This is the most straightforward way to protect shared state in Go.

How it works:
- `Lock()`: acquire the lock. If another goroutine holds it, block until it releases.
- `Unlock()`: release the lock. The next waiting goroutine can now proceed.

In a real web service, you need to track metrics: total requests, requests per endpoint, error counts, response times. Multiple HTTP handlers update these metrics concurrently. A mutex ensures no updates are lost.

## Step 1 -- Fix the Hit Counter with Mutex

Start by fixing the racy hit counter from exercises 01-02 with a simple mutex:

```go
package main

import (
	"fmt"
	"sync"
)

const (
	handlerCount       = 100
	requestsPerHandler = 100
)

// HitCounter protects a shared counter with a mutex.
type HitCounter struct {
	mu       sync.Mutex
	hitCount int
}

func NewHitCounter() *HitCounter {
	return &HitCounter{}
}

func (hc *HitCounter) Increment() {
	hc.mu.Lock()
	hc.hitCount++
	hc.mu.Unlock()
}

func (hc *HitCounter) Total() int {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	return hc.hitCount
}

func simulateTraffic(counter *HitCounter, handlers, reqsPerHandler int) {
	var wg sync.WaitGroup

	for handler := 0; handler < handlers; handler++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for req := 0; req < reqsPerHandler; req++ {
				counter.Increment()
			}
		}()
	}

	wg.Wait()
}

func main() {
	counter := NewHitCounter()
	simulateTraffic(counter, handlerCount, requestsPerHandler)
	expected := handlerCount * requestsPerHandler
	fmt.Printf("Hit count: %d (expected %d)\n", counter.Total(), expected)
}
```

### Verification
```bash
go run -race main.go
```
Expected:
```
Hit count: 10000 (expected 10000)
```
No `DATA RACE` warning. The mutex establishes a happens-before relationship: each `Unlock()` happens-before the next `Lock()`.

## Step 2 -- Build a MetricsCollector Struct

In production code, the mutex should be an implementation detail, not something callers must remember to use. Build a proper `MetricsCollector` that tracks request counts per endpoint, like a real HTTP service would need:

```go
package main

import (
	"fmt"
	"sync"
)

type MetricsCollector struct {
	mu       sync.Mutex
	counters map[string]int
}

func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		counters: make(map[string]int),
	}
}

func (m *MetricsCollector) RecordRequest(endpoint string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counters[endpoint]++
}

func (m *MetricsCollector) GetCount(endpoint string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counters[endpoint]
}

func (m *MetricsCollector) Snapshot() map[string]int {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Return a copy so the caller cannot cause races by reading the original map.
	snapshot := make(map[string]int, len(m.counters))
	for k, v := range m.counters {
		snapshot[k] = v
	}
	return snapshot
}

func main() {
	metrics := NewMetricsCollector()
	var wg sync.WaitGroup

	endpoints := []string{"/api/users", "/api/orders", "/api/products", "/healthz"}

	// Simulate 50 handlers per endpoint, each processing 100 requests.
	for _, ep := range endpoints {
		for handler := 0; handler < 50; handler++ {
			wg.Add(1)
			go func(endpoint string) {
				defer wg.Done()
				for req := 0; req < 100; req++ {
					metrics.RecordRequest(endpoint)
				}
			}(ep)
		}
	}

	wg.Wait()

	fmt.Println("=== Metrics Collector Results ===")
	snapshot := metrics.Snapshot()
	total := 0
	for endpoint, count := range snapshot {
		fmt.Printf("  %-20s %d requests\n", endpoint, count)
		total += count
	}
	fmt.Printf("  %-20s %d requests\n", "TOTAL", total)
	fmt.Printf("\nExpected: 5000 per endpoint, 20000 total\n")
}
```

Key patterns:
- The mutex is an unexported field: callers never see it
- Every public method acquires the lock with `defer Unlock()` for safety
- `Snapshot()` returns a copy of the map, not a reference, preventing races on the returned data
- `defer mu.Unlock()` guarantees the lock is released even if a panic occurs inside the method

### Verification
```bash
go run -race main.go
```
Expected: 5000 requests per endpoint, 20000 total, zero race warnings.

## Step 3 -- The Defer Pattern for Panic Safety

The `defer` pattern is not just about convenience. It guarantees the lock is released even in failure cases:

```go
package main

import (
	"fmt"
	"sync"
)

type SafeRegistry struct {
	mu    sync.Mutex
	items map[string]string
}

func NewSafeRegistry() *SafeRegistry {
	return &SafeRegistry{items: make(map[string]string)}
}

func (r *SafeRegistry) Register(key, value string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if key == "" {
		panic("empty key") // defer ensures Unlock() still runs
	}
	r.items[key] = value
}

func (r *SafeRegistry) Get(key string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.items[key]
	return v, ok
}

func main() {
	reg := NewSafeRegistry()
	var wg sync.WaitGroup

	// Writers
	keys := []string{"service-a", "service-b", "service-c"}
	for _, k := range keys {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			reg.Register(key, fmt.Sprintf("http://%s:8080", key))
		}(k)
	}

	wg.Wait()

	// Safe to read after all writers are done.
	for _, k := range keys {
		if v, ok := reg.Get(k); ok {
			fmt.Printf("  %s -> %s\n", k, v)
		}
	}
}
```

Without `defer`, forgetting to call `Unlock()` on any code path (early return, error, panic) causes a **permanent deadlock**: all other goroutines waiting for that lock will block forever.

### Verification
```bash
go run -race main.go
```
Expected: all three services registered, zero race warnings.

## Step 4 -- Measure Contention Cost

The mutex serializes access, which means goroutines wait for each other. Measure the overhead:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

const (
	benchWorkers        = 100
	benchIncrementsEach = 10000
)

// BenchmarkResult holds the outcome of a single counter benchmark.
type BenchmarkResult struct {
	Label   string
	Value   int
	Elapsed time.Duration
}

// RacyCounter increments without synchronization (produces wrong results).
// BUG: read-modify-write on counter has no synchronization.
type RacyCounter struct {
	counter int
}

func (rc *RacyCounter) RunBenchmark(workers, increments int) BenchmarkResult {
	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < increments; j++ {
				rc.counter++ // DATA RACE
			}
		}()
	}

	wg.Wait()
	return BenchmarkResult{"Racy (WRONG)", rc.counter, time.Since(start)}
}

// MutexCounter increments with mutex protection (correct but slower).
type MutexCounter struct {
	mu      sync.Mutex
	counter int
}

func (mc *MutexCounter) RunBenchmark(workers, increments int) BenchmarkResult {
	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < increments; j++ {
				mc.mu.Lock()
				mc.counter++
				mc.mu.Unlock()
			}
		}()
	}

	wg.Wait()
	return BenchmarkResult{"Mutex (correct)", mc.counter, time.Since(start)}
}

func printContentionAnalysis(racy, mutex BenchmarkResult) {
	fmt.Printf("  %-18s %d in %v\n", racy.Label+":", racy.Value, racy.Elapsed)
	fmt.Printf("  %-18s %d in %v\n", mutex.Label+":", mutex.Value, mutex.Elapsed)
	fmt.Printf("  Slowdown: %.1fx (the cost of correctness)\n",
		float64(mutex.Elapsed)/float64(racy.Elapsed))
	fmt.Println()
	fmt.Println("In real code, contention is usually lower because:")
	fmt.Println("  - Handlers do useful work between lock acquisitions")
	fmt.Println("  - Lock scope is narrow (microseconds, not the entire request)")
	fmt.Println("  - Different handlers lock different resources")
}

func main() {
	fmt.Println("=== Contention Cost ===")

	racyResult := (&RacyCounter{}).RunBenchmark(benchWorkers, benchIncrementsEach)
	mutexResult := (&MutexCounter{}).RunBenchmark(benchWorkers, benchIncrementsEach)

	printContentionAnalysis(racyResult, mutexResult)
}
```

### Verification
```bash
go run main.go
```

The mutex version is slower because goroutines must wait for each other. This is the worst case: 100 goroutines competing for a single lock on a single integer. In real web services, the lock is held for microseconds per request, and the work between requests (database queries, network calls) dominates the total time.

## Common Mistakes

### Forgetting to Unlock
```go
mu.Lock()
counter++
// forgot mu.Unlock() -- all other goroutines blocked forever (deadlock)
```
**Fix:** Always use `defer mu.Unlock()` immediately after `Lock()`.

### Locking Too Much
```go
mu.Lock()
for j := 0; j < 1000; j++ {
    counter++
}
mu.Unlock()
```
This locks the entire loop, eliminating all parallelism. Each goroutine holds the lock for 1000 iterations while others wait.

**Better:** Lock only the specific operation that needs protection:
```go
for j := 0; j < 1000; j++ {
    mu.Lock()
    counter++
    mu.Unlock()
}
```

### Copying a Mutex
```go
var mu sync.Mutex
mu2 := mu // BUG: mu2 is a copy, not the same mutex
```
Never copy a `sync.Mutex` after first use. Pass mutexes by pointer, or embed them in a struct (the struct itself must then be passed by pointer).

### Double-Locking from the Same Goroutine
```go
mu.Lock()
// ... some code that calls another function ...
mu.Lock() // DEADLOCK: same goroutine already holds the lock
```
`sync.Mutex` is NOT reentrant. Calling `Lock()` twice from the same goroutine without an `Unlock()` in between causes a deadlock.

## Verify What You Learned

1. Confirm zero race warnings for all mutex-protected functions with `go run -race main.go`
2. What happens if you call `Lock()` twice from the same goroutine without `Unlock()`?
3. Why is `defer mu.Unlock()` preferred over calling `mu.Unlock()` explicitly?
4. Why does `Snapshot()` return a copy of the map instead of the original?

## What's Next
Continue to [04-fix-race-with-channel](../04-fix-race-with-channel/04-fix-race-with-channel.md) to fix the same metrics problem using channels instead of a mutex.

## Summary
- `sync.Mutex` provides mutual exclusion: only one goroutine enters the critical section at a time
- Always pair `Lock()` with `Unlock()`; prefer `defer mu.Unlock()` for safety
- Encapsulate the mutex inside a struct (like `MetricsCollector`) so callers cannot forget to lock
- Protect maps with mutex: both reads and writes must be locked
- Return copies from getters (like `Snapshot()`) to prevent races on returned data
- The mutex establishes happens-before relationships that satisfy the race detector
- Tradeoff: mutexes add contention, but in real services the overhead is negligible compared to I/O
- Verify with `go run -race main.go` to confirm the race is eliminated

## Reference
- [sync.Mutex Documentation](https://pkg.go.dev/sync#Mutex)
- [Go Blog: Share Memory by Communicating](https://go.dev/blog/codelab-share)
- [Effective Go: Sharing by Communicating](https://go.dev/doc/effective_go#sharing)
