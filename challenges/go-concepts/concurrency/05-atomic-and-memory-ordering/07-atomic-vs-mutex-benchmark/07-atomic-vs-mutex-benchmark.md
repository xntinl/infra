---
difficulty: advanced
concepts: [testing.B, benchmark functions, atomic performance, mutex performance, channel counter, RWMutex, contention analysis]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [goroutines, sync.WaitGroup, atomic operations, sync.Mutex, channels, Go testing package]
---

# 7. Atomic vs Mutex Benchmark


## Learning Objectives
After completing this exercise, you will be able to:
- **Write** Go benchmark functions using `testing.B`
- **Measure** the performance of atomic, mutex, RWMutex, and channel-based counters
- **Analyze** how contention level affects the relative performance of each approach
- **Decide** when to use atomic operations vs mutexes based on measured evidence

## Why Benchmark

Statements like "atomics are faster than mutexes" are oversimplifications. The real answer depends on: how many goroutines contend, how long the critical section is, what CPU architecture you run on, and what the access pattern looks like. Benchmarking gives you concrete numbers for your specific scenario.

Go's `testing` package includes built-in support for benchmarks. Functions starting with `Benchmark` receive a `*testing.B` and run the measured code `b.N` times (the framework auto-calibrates `b.N`). The `b.RunParallel` method distributes iterations across multiple goroutines to measure concurrent performance.

In this exercise, you benchmark four counter implementations (atomic, mutex, RWMutex, channel) under varying contention and observe the actual performance characteristics.

## The Four Counter Implementations

The counters are defined in `main.go`:

```go
package main

import (
	"sync"
	"sync/atomic"
)

// AtomicCounter: lock-free, single CPU instruction per operation
type AtomicCounter struct {
	val atomic.Int64
}
func (c *AtomicCounter) Inc()       { c.val.Add(1) }
func (c *AtomicCounter) Get() int64 { return c.val.Load() }

// MutexCounter: serializes all access (reads AND writes)
type MutexCounter struct {
	mu  sync.Mutex
	val int64
}
func (c *MutexCounter) Inc() {
	c.mu.Lock()
	c.val++
	c.mu.Unlock()
}
func (c *MutexCounter) Get() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.val
}

// RWMutexCounter: concurrent readers, exclusive writers
type RWMutexCounter struct {
	mu  sync.RWMutex
	val int64
}
func (c *RWMutexCounter) Inc() {
	c.mu.Lock()
	c.val++
	c.mu.Unlock()
}
func (c *RWMutexCounter) Get() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.val
}

// ChannelCounter: semaphore via buffered channel
type ChannelCounter struct {
	ch  chan struct{}
	val int64
}
func NewChannelCounter() *ChannelCounter {
	c := &ChannelCounter{ch: make(chan struct{}, 1)}
	c.ch <- struct{}{}
	return c
}
func (c *ChannelCounter) Inc() {
	<-c.ch
	c.val++
	c.ch <- struct{}{}
}
func (c *ChannelCounter) Get() int64 {
	<-c.ch
	v := c.val
	c.ch <- struct{}{}
	return v
}
```

### Verification
```bash
go test -run Test -v -race
```
Expected: all four correctness tests pass with no race warnings.

## Sequential Benchmarks

Measure the base cost per operation without concurrency. This isolates the overhead of each synchronization mechanism:

```go
func BenchmarkAtomicCounter_Sequential(b *testing.B) {
	c := &AtomicCounter{}
	for i := 0; i < b.N; i++ {
		c.Inc()
	}
}

func BenchmarkMutexCounter_Sequential(b *testing.B) {
	c := &MutexCounter{}
	for i := 0; i < b.N; i++ {
		c.Inc()
	}
}

func BenchmarkRWMutexCounter_Sequential(b *testing.B) {
	c := &RWMutexCounter{}
	for i := 0; i < b.N; i++ {
		c.Inc()
	}
}

func BenchmarkChannelCounter_Sequential(b *testing.B) {
	c := NewChannelCounter()
	for i := 0; i < b.N; i++ {
		c.Inc()
	}
}
```

### Verification
```bash
go test -bench=Sequential -benchmem
```
Expected ordering (fastest to slowest): Atomic < Mutex < RWMutex < Channel. Atomic is a single CPU instruction. Mutex involves lock/unlock. Channel involves two channel operations.

## Parallel Benchmarks

Use `b.RunParallel` to benchmark under realistic concurrency. The framework spawns `GOMAXPROCS` goroutines and distributes `b.N` iterations across them:

```go
func BenchmarkAtomicCounter_Parallel(b *testing.B) {
	c := &AtomicCounter{}
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.Inc()
		}
	})
}

func BenchmarkMutexCounter_Parallel(b *testing.B) {
	c := &MutexCounter{}
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.Inc()
		}
	})
}
```

### Verification
```bash
go test -bench=Parallel -benchmem
```
Under contention, atomic still wins for simple increment. Mutex overhead is moderate. Channel is slowest due to goroutine scheduling.

## Read-Heavy Benchmarks

Real workloads are not 100% writes. A 90% read / 10% write split shows where RWMutex and atomics shine:

```go
func BenchmarkAtomicCounter_ReadHeavy(b *testing.B) {
	c := &AtomicCounter{}
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%10 == 0 {
				c.Inc()
			} else {
				c.Get()
			}
			i++
		}
	})
}

func BenchmarkRWMutexCounter_ReadHeavy(b *testing.B) {
	c := &RWMutexCounter{}
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%10 == 0 {
				c.Inc()
			} else {
				c.Get()
			}
			i++
		}
	})
}
```

### Verification
```bash
go test -bench=ReadHeavy -benchmem
```
Atomic should be significantly faster than all mutex variants. RWMutex should outperform Mutex because concurrent reads don't block each other. Mutex forces all operations (even reads) to be serialized.

## High Contention Benchmarks

Use `b.SetParallelism(100)` to create 100x GOMAXPROCS goroutines, simulating extreme contention:

```go
func BenchmarkAtomicCounter_HighContention(b *testing.B) {
	c := &AtomicCounter{}
	b.SetParallelism(100)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.Inc()
		}
	})
}

func BenchmarkMutexCounter_HighContention(b *testing.B) {
	c := &MutexCounter{}
	b.SetParallelism(100)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.Inc()
		}
	})
}
```

### Verification
```bash
go test -bench=HighContention -benchmem
```
Under extreme contention, the gap between atomic and mutex widens. Mutex spends significant time parking and waking goroutines. Atomic operations, while contended, avoid the overhead of goroutine scheduling.

## Running the Complete Suite

```bash
go test -bench=. -benchmem -count=3
```

The `-count=3` flag runs each benchmark three times to spot variance. Focus on:
- **ns/op**: nanoseconds per operation (lower is better)
- **B/op**: bytes allocated per operation (0 for all four in this case)
- **allocs/op**: allocations per operation

For cross-CPU analysis:
```bash
go test -bench=Parallel -benchmem -cpu=1,2,4,8
```

Expected results summary:

| Scenario | Atomic | Mutex | RWMutex | Channel |
|----------|--------|-------|---------|---------|
| Sequential | fastest | moderate | moderate | slowest |
| Parallel (writes) | fastest | moderate | moderate | slowest |
| Read-Heavy | fastest | slow (readers block) | moderate (readers concurrent) | slowest |
| High Contention | fastest | moderate | moderate | slowest |

## Common Mistakes

### Benchmark Does Not Use b.N

**Wrong:**
```go
func BenchmarkBad(b *testing.B) {
	c := &AtomicCounter{}
	for i := 0; i < 1000; i++ { // fixed iteration count!
		c.Inc()
	}
}
```

**What happens:** The benchmark framework cannot auto-calibrate. Results are meaningless because b.N is ignored.

**Fix:** Always loop to `b.N`:
```go
func BenchmarkGood(b *testing.B) {
	c := &AtomicCounter{}
	for i := 0; i < b.N; i++ {
		c.Inc()
	}
}
```

### Compiler Optimizes Away the Work

**Wrong:**
```go
func BenchmarkAdd(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = 1 + 1 // compiler may eliminate this
	}
}
```

**Fix:** Assign the result to a package-level variable:
```go
var sink int

func BenchmarkAdd(b *testing.B) {
	s := 0
	for i := 0; i < b.N; i++ {
		s += 1
	}
	sink = s
}
```

### Not Resetting Timer After Expensive Setup

**Wrong:**
```go
func BenchmarkWithSetup(b *testing.B) {
	c := NewChannelCounter() // setup time included in measurement
	for i := 0; i < b.N; i++ {
		c.Inc()
	}
}
```

For cheap setup this is fine, but for expensive initialization:
```go
func BenchmarkWithSetup(b *testing.B) {
	c := expensiveSetup()
	b.ResetTimer() // exclude setup from measurement
	for i := 0; i < b.N; i++ {
		c.Inc()
	}
}
```

## Decision Guide: When to Use Which

| Scenario | Recommendation | Why |
|----------|---------------|-----|
| Simple counter, few variables | `atomic.Int64` | Fastest, zero allocation, no lock contention |
| Read-heavy, single value | `atomic.Int64` or `atomic.Value` | Readers never block each other |
| Multiple related fields updated together | `sync.Mutex` | Atomics cannot protect multi-field updates |
| Read-heavy, multiple fields | `sync.RWMutex` | Concurrent reads, exclusive writes |
| Complex critical section (I/O, computation) | `sync.Mutex` | Goroutines park instead of spinning |
| Need to pass ownership or signal | Channel | Different problem class entirely |

## What's Next
You have completed the atomics and memory ordering section. Continue to the next section on **context** to learn how Go programs propagate cancellation, deadlines, and values across API boundaries and goroutine trees.

## Summary
- Use `testing.B` and `b.N` for Go benchmarks; the framework auto-calibrates iteration count
- `b.RunParallel` benchmarks concurrent workloads with realistic goroutine counts
- Atomic operations are fastest for simple counters, especially under read-heavy workloads
- RWMutex outperforms Mutex for read-heavy workloads because readers run concurrently
- Mutex has moderate overhead but is essential for multi-field critical sections
- Channel-based synchronization has the highest per-operation cost but solves different problems (ownership transfer, signaling)
- Always benchmark YOUR specific scenario -- "atomics are faster" is not universally true
- Run benchmarks multiple times (`-count=3`) and on the target hardware for reliable results

## Reference
- [testing.B](https://pkg.go.dev/testing#B)
- [b.RunParallel](https://pkg.go.dev/testing#B.RunParallel)
- [benchstat tool](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat)
- [Go Performance Wiki](https://go.dev/wiki/Performance)
