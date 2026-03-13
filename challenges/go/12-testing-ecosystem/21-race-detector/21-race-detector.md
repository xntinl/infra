# 21. Race Detector and Concurrent Test Safety

<!--
difficulty: advanced
concepts: [race-detector, data-race, go-test-race, mutex, atomic, happens-before, concurrent-testing]
tools: [go test, go build]
estimated_time: 35m
bloom_level: analyze
prerequisites: [01-your-first-test, 14-parallel-tests, 12-t-cleanup-patterns]
-->

## Prerequisites

- Go 1.22+ installed
- Familiarity with goroutines and `sync.Mutex`
- Understanding of `t.Parallel()` and parallel subtests

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** `go test -race` to detect data races in tests and production code
- **Interpret** race detector output to identify the conflicting goroutines and memory locations
- **Fix** common data race patterns: unprotected map writes, shared counters, closure captures
- **Design** tests that reliably expose races by increasing concurrency pressure

## The Problem

Data races are concurrent bugs where two goroutines access the same memory without synchronization, and at least one access is a write. Races cause unpredictable behavior: corrupted data, crashes, security vulnerabilities. They are notoriously hard to reproduce -- a program can run correctly millions of times before a race manifests. Go's race detector instruments memory accesses at compile time and reports races at runtime. It finds real bugs that manual code review misses.

You will work with a metrics collection system that has several intentional data races. Your job is to find them with `-race`, understand the output, and fix each one.

## Requirements

1. **Create a metrics collector** with intentional data races:

```go
// metrics.go
package metrics

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Collector gathers named counters and gauges.
// BUG: This implementation has several data races.
type Collector struct {
	counters map[string]int64
	gauges   map[string]float64
	history  []event
}

type event struct {
	name      string
	value     float64
	timestamp time.Time
}

func NewCollector() *Collector {
	return &Collector{
		counters: make(map[string]int64),
		gauges:   make(map[string]float64),
	}
}

// Increment adds delta to a named counter.
func (c *Collector) Increment(name string, delta int64) {
	c.counters[name] += delta // RACE: concurrent map write
}

// SetGauge sets a named gauge to the given value.
func (c *Collector) SetGauge(name string, value float64) {
	c.gauges[name] = value // RACE: concurrent map write
	c.history = append(c.history, event{ // RACE: concurrent slice append
		name:      name,
		value:     value,
		timestamp: time.Now(),
	})
}

// GetCounter returns the current value of a counter.
func (c *Collector) GetCounter(name string) int64 {
	return c.counters[name] // RACE: concurrent map read during write
}

// Snapshot returns a formatted string of all metrics.
func (c *Collector) Snapshot() string {
	var lines []string
	for name, val := range c.counters { // RACE: concurrent iteration
		lines = append(lines, fmt.Sprintf("counter.%s = %d", name, val))
	}
	for name, val := range c.gauges {
		lines = append(lines, fmt.Sprintf("gauge.%s = %.2f", name, val))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}
```

2. **Write tests that expose the races**:

```go
// metrics_race_test.go
package metrics

import (
	"sync"
	"testing"
)

func TestCollector_ConcurrentIncrements(t *testing.T) {
	c := NewCollector()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Increment("requests", 1)
		}()
	}
	wg.Wait()

	// With the race, the count may not be 100
	got := c.GetCounter("requests")
	if got != 100 {
		t.Errorf("counter = %d, want 100", got)
	}
}

func TestCollector_ConcurrentGauges(t *testing.T) {
	c := NewCollector()
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(val float64) {
			defer wg.Done()
			c.SetGauge("cpu", val)
		}(float64(i))
	}
	wg.Wait()
}

func TestCollector_ReadDuringWrite(t *testing.T) {
	c := NewCollector()
	var wg sync.WaitGroup

	// Writer
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			c.Increment("ops", 1)
		}
	}()

	// Reader
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			_ = c.Snapshot()
		}
	}()

	wg.Wait()
}
```

3. **Run the race detector** and observe the output:

```bash
go test -race -v
```

The race detector output shows:
- The type of race (read vs write)
- Stack traces for both conflicting accesses
- Which goroutines are involved

4. **Create the fixed version** using `sync.RWMutex`:

```go
// metrics_safe.go
package metrics

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// SafeCollector is a thread-safe version of Collector.
type SafeCollector struct {
	mu       sync.RWMutex
	counters map[string]int64
	gauges   map[string]float64
	history  []event
}

func NewSafeCollector() *SafeCollector {
	return &SafeCollector{
		counters: make(map[string]int64),
		gauges:   make(map[string]float64),
	}
}

func (c *SafeCollector) Increment(name string, delta int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counters[name] += delta
}

func (c *SafeCollector) SetGauge(name string, value float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gauges[name] = value
	c.history = append(c.history, event{
		name:      name,
		value:     value,
		timestamp: time.Now(),
	})
}

func (c *SafeCollector) GetCounter(name string) int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.counters[name]
}

func (c *SafeCollector) Snapshot() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var lines []string
	for name, val := range c.counters {
		lines = append(lines, fmt.Sprintf("counter.%s = %d", name, val))
	}
	for name, val := range c.gauges {
		lines = append(lines, fmt.Sprintf("gauge.%s = %.2f", name, val))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}
```

5. **Write tests for the safe version** and confirm no races:

```go
// metrics_safe_test.go
package metrics

import (
	"sync"
	"testing"
)

func TestSafeCollector_ConcurrentIncrements(t *testing.T) {
	c := NewSafeCollector()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Increment("requests", 1)
		}()
	}
	wg.Wait()

	got := c.GetCounter("requests")
	if got != 100 {
		t.Errorf("counter = %d, want 100", got)
	}
}

func TestSafeCollector_ConcurrentMixed(t *testing.T) {
	c := NewSafeCollector()
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(3)
		go func(v int) {
			defer wg.Done()
			c.Increment("ops", 1)
		}(i)
		go func(v int) {
			defer wg.Done()
			c.SetGauge("temp", float64(v))
		}(i)
		go func() {
			defer wg.Done()
			_ = c.Snapshot()
		}()
	}
	wg.Wait()
}
```

```bash
go test -race -v -run Safe
```

## Hints

- The race detector slows execution by 2-10x and uses 5-10x more memory. Do not run it in production. Run it in CI on every commit.
- A race-free run does not prove absence of races -- it only proves no race occurred during that particular execution. Run tests many times (`-count=100`) to increase confidence.
- `sync.RWMutex` allows multiple concurrent readers but exclusive writers. Use `RLock`/`RUnlock` for read-only operations.
- `sync/atomic` is an alternative for simple counters: `atomic.AddInt64(&counter, 1)`. It avoids the overhead of a mutex for single-variable updates.
- The race detector catches races in test code too. A test that calls `t.Errorf` from a goroutine other than the test goroutine is itself a race (unless the test uses `t.Parallel()` correctly).
- Maps in Go are not safe for concurrent use. Any concurrent read+write or write+write to a map is a data race, even if different keys are accessed.

## Verification

```bash
# Detect races in the buggy version
go test -race -run TestCollector 2>&1 | head -50

# Confirm the safe version is race-free
go test -race -run TestSafe -count=10 -v

# Run all tests with race detector
go test -race ./...
```

## What's Next

Continue to [22 - TestMain Setup and Teardown](../22-testmain-setup-teardown/22-testmain-setup-teardown.md) to learn how to run setup and teardown code for an entire test package.

## Summary

- `go test -race` enables the race detector, which instruments memory accesses at compile time
- Race detector output shows conflicting accesses with full stack traces
- Common race sources: unprotected map access, shared slice append, counter increment
- Fix with `sync.Mutex`, `sync.RWMutex`, or `sync/atomic` depending on the access pattern
- Run `-race` in CI on every commit; do not run it in production (performance overhead)
- A clean `-race` run does not guarantee no races -- increase confidence with `-count=N`
- Maps in Go are never safe for concurrent access without external synchronization

## Reference

- [Go race detector](https://go.dev/doc/articles/race_detector)
- [sync.RWMutex](https://pkg.go.dev/sync#RWMutex)
- [sync/atomic](https://pkg.go.dev/sync/atomic)
- [Go memory model](https://go.dev/ref/mem)
