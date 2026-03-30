---
difficulty: advanced
concepts: [sync.Mutex, sync.RWMutex, sync/atomic, benchmarking, tradeoffs]
tools: [go]
estimated_time: 40m
bloom_level: create
prerequisites: [sync.Mutex, sync.RWMutex, channels, goroutines, sync.WaitGroup]
---

# 10. Build a Thread-Safe Metrics System


## Learning Objectives
After completing this exercise, you will be able to:
- **Implement** multiple counter types using different sync primitives
- **Benchmark** each approach and compare throughput
- **Analyze** the tradeoffs: correctness, performance, code clarity
- **Choose** the right synchronization mechanism based on access patterns

## Why This Integration Exercise
Throughout this section you have learned individual sync primitives in isolation. Real-world systems require choosing between them based on the specific access pattern. A production metrics system has multiple counter types:

- **Simple counters** (total requests): write-heavy, rarely read. Every request increments.
- **Gauges** (active connections): read frequently by monitoring dashboards, written less often when connections open/close.
- **High-frequency counters** (bytes transferred): incremented on every packet, read only for periodic reporting.

Each counter type has a different read/write ratio, which determines the optimal sync primitive. This exercise forces you to implement all three, benchmark them under realistic conditions, and reason about when each approach is appropriate.

## Step 1 -- Simple Counter with Mutex

A request counter: every handler increments it, and the `/metrics` endpoint reads it. Writes heavily outnumber reads:

```go
package main

import (
	"fmt"
	"sync"
)

type MutexCounter struct {
	mu    sync.Mutex
	value int64
}

func (c *MutexCounter) Increment() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value++
}

func (c *MutexCounter) Add(n int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value += n
}

func (c *MutexCounter) Value() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.value
}

func main() {
	counter := &MutexCounter{}
	var wg sync.WaitGroup

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			counter.Increment()
		}()
	}

	wg.Wait()
	fmt.Printf("MutexCounter: %d (expected 1000)\n", counter.Value())
}
```

Expected output:
```
MutexCounter: 1000 (expected 1000)
```

**Characteristics**: simple, correct, all operations serialized (including reads). Good default choice.

### Intermediate Verification
```bash
go run -race main.go
```
The counter should report exactly 1000 with no race warnings.

## Step 2 -- Gauge with RWMutex (Concurrent Readers)

An active connections gauge: monitoring dashboards read it constantly, but only connection open/close events update it:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type RWGauge struct {
	mu    sync.RWMutex
	value int64
}

func (g *RWGauge) Set(val int64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.value = val
}

func (g *RWGauge) Increment() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.value++
}

func (g *RWGauge) Decrement() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.value--
}

func (g *RWGauge) Value() int64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.value
}

func main() {
	gauge := &RWGauge{}
	var wg sync.WaitGroup

	// Simulate 10 connections opening and closing
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			gauge.Increment() // connection opened
			time.Sleep(50 * time.Millisecond)
			gauge.Decrement() // connection closed
		}()
	}

	// Simulate 50 monitoring reads (dashboards, alerting)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = gauge.Value() // concurrent reads do not block each other
		}()
	}

	wg.Wait()
	fmt.Printf("RWGauge (active connections): %d (expected 0 -- all closed)\n", gauge.Value())
}
```

Expected output:
```
RWGauge (active connections): 0 (expected 0 -- all closed)
```

**Characteristics**: concurrent readers do not block each other. Writers get exclusive access. Optimal when reads significantly outnumber writes, like a Prometheus `/metrics` endpoint scraped every 15 seconds while connections change only a few times per second.

### Intermediate Verification
```bash
go run -race main.go
```
Gauge should be 0 (all connections opened and closed).

## Step 3 -- High-Frequency Counter with Atomic

A bytes-transferred counter: incremented on every network packet (potentially millions of times per second), read only for periodic reporting:

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

type AtomicCounter struct {
	value atomic.Int64
}

func (c *AtomicCounter) Increment() {
	c.value.Add(1)
}

func (c *AtomicCounter) Add(n int64) {
	c.value.Add(n)
}

func (c *AtomicCounter) Value() int64 {
	return c.value.Load()
}

func main() {
	counter := &AtomicCounter{}
	var wg sync.WaitGroup

	// Simulate high-frequency increments (like counting bytes on packets)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10000; j++ {
				counter.Increment()
			}
		}()
	}

	wg.Wait()
	fmt.Printf("AtomicCounter: %d (expected 1000000)\n", counter.Value())
}
```

Expected output:
```
AtomicCounter: 1000000 (expected 1000000)
```

**Characteristics**: lock-free, highest throughput. No deadlock possible. Limited to operations the CPU supports atomically (add, load, store, compare-and-swap). Cannot protect complex operations.

### Intermediate Verification
```bash
go run -race main.go
```
Exactly 1,000,000 with no race warnings.

## Step 4 -- Benchmark All Three Under Realistic Workloads

Run all three counter types under identical conditions and measure throughput:

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type MutexCounter struct {
	mu    sync.Mutex
	value int64
}

func (c *MutexCounter) Increment() { c.mu.Lock(); c.value++; c.mu.Unlock() }
func (c *MutexCounter) Value() int64 { c.mu.Lock(); defer c.mu.Unlock(); return c.value }

type RWGauge struct {
	mu    sync.RWMutex
	value int64
}

func (g *RWGauge) Increment() { g.mu.Lock(); g.value++; g.mu.Unlock() }
func (g *RWGauge) Value() int64 { g.mu.RLock(); defer g.mu.RUnlock(); return g.value }

type AtomicCounter struct {
	value atomic.Int64
}

func (c *AtomicCounter) Increment() { c.value.Add(1) }
func (c *AtomicCounter) Value() int64 { return c.value.Load() }

func benchmarkWriteHeavy(name string, inc func(), val func() int64, goroutines, opsPerG int) {
	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerG; j++ {
				inc()
				if j%100 == 0 { // read once per 100 writes
					_ = val()
				}
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)
	fmt.Printf("  %-15s %v  (final value: %d)\n", name, elapsed.Round(time.Millisecond), val())
}

func benchmarkReadHeavy(name string, inc func(), val func() int64, goroutines, opsPerG int) {
	var wg sync.WaitGroup
	start := time.Now()

	// Few writers
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerG/10; j++ {
				inc()
			}
		}()
	}

	// Many readers
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerG; j++ {
				_ = val()
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)
	fmt.Printf("  %-15s %v  (final value: %d)\n", name, elapsed.Round(time.Millisecond), val())
}

func main() {
	const goroutines = 100
	const opsPerG = 10000

	fmt.Printf("=== Write-Heavy Benchmark (%d goroutines x %d ops) ===\n", goroutines, opsPerG)
	fmt.Println("Scenario: request counter (every handler writes, metrics endpoint reads rarely)")

	mc := &MutexCounter{}
	rg := &RWGauge{}
	ac := &AtomicCounter{}

	benchmarkWriteHeavy("Mutex", mc.Increment, mc.Value, goroutines, opsPerG)
	benchmarkWriteHeavy("RWMutex", rg.Increment, rg.Value, goroutines, opsPerG)
	benchmarkWriteHeavy("Atomic", ac.Increment, ac.Value, goroutines, opsPerG)

	fmt.Printf("\n=== Read-Heavy Benchmark (2 writers, %d readers x %d ops) ===\n", goroutines, opsPerG)
	fmt.Println("Scenario: active connections gauge (dashboard reads constantly, few changes)")

	mc2 := &MutexCounter{}
	rg2 := &RWGauge{}
	ac2 := &AtomicCounter{}

	benchmarkReadHeavy("Mutex", mc2.Increment, mc2.Value, goroutines, opsPerG)
	benchmarkReadHeavy("RWMutex", rg2.Increment, rg2.Value, goroutines, opsPerG)
	benchmarkReadHeavy("Atomic", ac2.Increment, ac2.Value, goroutines, opsPerG)

	fmt.Println("\n=== Recommendation ===")
	fmt.Println("  Request counters (write-heavy):   atomic > mutex > rwmutex")
	fmt.Println("  Gauges (read-heavy):              atomic > rwmutex > mutex")
	fmt.Println("  Complex state (multi-field):      mutex (atomic cannot protect compound ops)")
}
```

Expected output (times vary by machine):
```
=== Write-Heavy Benchmark (100 goroutines x 10000 ops) ===
Scenario: request counter (every handler writes, metrics endpoint reads rarely)
  Mutex            15ms  (final value: 1000000)
  RWMutex          20ms  (final value: 1000000)
  Atomic           3ms   (final value: 1000000)

=== Read-Heavy Benchmark (2 writers, 100 readers x 10000 ops) ===
Scenario: active connections gauge (dashboard reads constantly, few changes)
  Mutex            25ms  (final value: 2000)
  RWMutex          10ms  (final value: 2000)
  Atomic           4ms   (final value: 2000)

=== Recommendation ===
  Request counters (write-heavy):   atomic > mutex > rwmutex
  Gauges (read-heavy):              atomic > rwmutex > mutex
  Complex state (multi-field):      mutex (atomic cannot protect compound ops)
```

### Intermediate Verification
```bash
go run main.go
```
All counters should report correct final values. Atomic should be fastest in both scenarios.

## Step 5 -- Complete Metrics Registry

Put it all together: a production-grade metrics registry that chooses the right primitive for each counter type:

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// RequestCounter uses atomic for high-throughput write-heavy counters.
type RequestCounter struct {
	value atomic.Int64
}

func (c *RequestCounter) Inc()          { c.value.Add(1) }
func (c *RequestCounter) Add(n int64)   { c.value.Add(n) }
func (c *RequestCounter) Value() int64  { return c.value.Load() }

// ConnectionGauge uses RWMutex for read-heavy gauges.
type ConnectionGauge struct {
	mu    sync.RWMutex
	value int64
}

func (g *ConnectionGauge) Inc()         { g.mu.Lock(); g.value++; g.mu.Unlock() }
func (g *ConnectionGauge) Dec()         { g.mu.Lock(); g.value--; g.mu.Unlock() }
func (g *ConnectionGauge) Value() int64 { g.mu.RLock(); defer g.mu.RUnlock(); return g.value }

// LatencyHistogram uses Mutex for complex multi-field state.
type LatencyHistogram struct {
	mu    sync.Mutex
	count int64
	sum   int64
	min   int64
	max   int64
}

func NewLatencyHistogram() *LatencyHistogram {
	return &LatencyHistogram{min: 1<<63 - 1}
}

func (h *LatencyHistogram) Record(latencyMs int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.count++
	h.sum += latencyMs
	if latencyMs < h.min {
		h.min = latencyMs
	}
	if latencyMs > h.max {
		h.max = latencyMs
	}
}

func (h *LatencyHistogram) Stats() (count, avg, min, max int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.count == 0 {
		return 0, 0, 0, 0
	}
	return h.count, h.sum / h.count, h.min, h.max
}

func main() {
	requests := &RequestCounter{}
	connections := &ConnectionGauge{}
	latency := NewLatencyHistogram()

	var wg sync.WaitGroup

	// Simulate 200 HTTP requests
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(reqID int) {
			defer wg.Done()

			connections.Inc() // connection opened
			requests.Inc()    // request counted

			// Simulate variable latency
			lat := int64(5 + reqID%50) // 5ms to 54ms
			time.Sleep(time.Duration(lat) * time.Millisecond / 10) // scaled down
			latency.Record(lat)

			connections.Dec() // connection closed
		}(i)
	}

	// Simulate monitoring dashboard reading metrics 20 times
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(10 * time.Millisecond)
			_ = requests.Value()
			_ = connections.Value()
		}()
	}

	wg.Wait()

	count, avg, min, max := latency.Stats()

	fmt.Println("=== Production Metrics Report ===")
	fmt.Printf("  Total requests:      %d\n", requests.Value())
	fmt.Printf("  Active connections:   %d (should be 0)\n", connections.Value())
	fmt.Printf("  Latency (ms):        count=%d avg=%d min=%d max=%d\n", count, avg, min, max)
	fmt.Println("\nPrimitive choices:")
	fmt.Println("  RequestCounter:      atomic (write-heavy, simple increment)")
	fmt.Println("  ConnectionGauge:     RWMutex (read-heavy, concurrent dashboard readers)")
	fmt.Println("  LatencyHistogram:    Mutex (must update count+sum+min+max atomically)")
}
```

Expected output:
```
=== Production Metrics Report ===
  Total requests:      200
  Active connections:   0 (should be 0)
  Latency (ms):        count=200 avg=29 min=5 max=54

Primitive choices:
  RequestCounter:      atomic (write-heavy, simple increment)
  ConnectionGauge:     RWMutex (read-heavy, concurrent dashboard readers)
  LatencyHistogram:    Mutex (must update count+sum+min+max atomically)
```

### Intermediate Verification
```bash
go run -race main.go
```
All metrics correct, zero active connections, no race warnings.

## Common Mistakes

### Using Atomic for Complex Operations

```go
var balance atomic.Int64
func transfer(amount int64) {
    if balance.Load() >= amount { // check
        balance.Add(-amount)      // act -- NOT atomic with the check!
    }
}
```

**What happens:** The check-then-act is not atomic. Another goroutine can drain the balance between Load and Add. This is why the `LatencyHistogram` uses a Mutex -- updating count, sum, min, and max must happen as one atomic unit.

**Fix:** Use `CompareAndSwap` in a loop, or switch to a mutex for compound operations.

### RWMutex for Write-Heavy Counters
Using `RWMutex` for a request counter (mostly writes) adds overhead for read-lock tracking with no benefit. The write-heavy benchmark proves this: `RWMutex` is slower than plain `Mutex` when writes dominate.

### Forgetting to Choose Based on Access Pattern
The default should be:
1. Simple increment/read? -> `atomic`
2. Read-heavy with concurrent readers? -> `RWMutex`
3. Multi-field update that must be atomic? -> `Mutex`

Do not use `atomic` for everything (it cannot protect compound operations). Do not use `RWMutex` for everything (it is slower than `Mutex` for write-heavy workloads).

## Verify What You Learned

Extend the metrics registry with:
- A `ErrorRate` counter that tracks both error count (atomic) and total count (atomic), and computes the rate as `errors/total` (requires reading both atomically -- what primitive do you need?)
- A `ResponseSizeHistogram` that tracks percentiles (p50, p95, p99) using a sorted slice protected by a mutex
- Benchmark all five metric types and write a one-paragraph recommendation for which to use when.

## What's Next
You have completed the sync primitives section. Continue to [05-atomic-and-memory-ordering](../../05-atomic-and-memory-ordering/) to learn about lock-free programming with the `sync/atomic` package.

## Summary
- Choose the sync primitive based on the access pattern, not by default
- `sync/atomic` offers the best throughput for simple counters (increment, load, store)
- `sync.Mutex` is the right choice for complex state that requires multi-field atomic updates
- `sync.RWMutex` helps when reads significantly outnumber writes (gauges, config, caches)
- Always benchmark with your actual workload -- intuition about performance is often wrong
- A production metrics system uses all three: atomic for high-frequency counters, RWMutex for gauges, Mutex for histograms
- The decision criteria: operation complexity (simple vs compound), read/write ratio, and throughput requirements

## Reference
- [sync package documentation](https://pkg.go.dev/sync)
- [sync/atomic package documentation](https://pkg.go.dev/sync/atomic)
- [Go Blog: Share Memory by Communicating](https://go.dev/blog/codelab-share)
- [Go Wiki: MutexOrChannel](https://go.dev/wiki/MutexOrChannel)
