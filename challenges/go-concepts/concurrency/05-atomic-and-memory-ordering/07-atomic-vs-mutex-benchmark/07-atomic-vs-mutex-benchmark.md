# 7. Atomic vs Mutex Benchmark

<!--
difficulty: advanced
concepts: [testing.B, benchmark functions, atomic performance, mutex performance, channel counter, contention analysis]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [goroutines, sync.WaitGroup, atomic operations, sync.Mutex, channels, Go testing package]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01-06 (atomic operations, happens-before)
- Basic familiarity with Go's `testing` package

## Learning Objectives
After completing this exercise, you will be able to:
- **Write** Go benchmark functions using `testing.B`
- **Measure** the performance of atomic, mutex, and channel-based counters
- **Analyze** how contention level affects the relative performance of each approach
- **Decide** when to use atomic operations vs mutexes based on measured evidence

## Why Benchmark
Statements like "atomics are faster than mutexes" are oversimplifications. The real answer depends on: how many goroutines contend, how long the critical section is, what CPU architecture you run on, and what the access pattern looks like. Benchmarking gives you concrete numbers for your specific scenario.

Go's `testing` package includes built-in support for benchmarks. Functions starting with `Benchmark` receive a `*testing.B` and run the measured code `b.N` times (the framework auto-calibrates `b.N`). The `b.RunParallel` method distributes iterations across multiple goroutines to measure concurrent performance.

In this exercise, you will benchmark three counter implementations (atomic, mutex, channel) under varying contention and observe the actual performance characteristics.

## Step 1 -- Define the Three Counter Implementations

Create three counter types in `main_test.go`:

```go
type AtomicCounter struct {
    val atomic.Int64
}

func (c *AtomicCounter) Inc() { c.val.Add(1) }
func (c *AtomicCounter) Get() int64 { return c.val.Load() }

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

type ChannelCounter struct {
    ch  chan struct{}
    val int64
}

func NewChannelCounter() *ChannelCounter {
    c := &ChannelCounter{ch: make(chan struct{}, 1)}
    c.ch <- struct{}{} // initial token
    return c
}

func (c *ChannelCounter) Inc() {
    <-c.ch     // acquire token
    c.val++
    c.ch <- struct{}{} // release token
}

func (c *ChannelCounter) Get() int64 {
    <-c.ch
    v := c.val
    c.ch <- struct{}{}
    return v
}
```

The channel counter uses a buffered channel with capacity 1 as a semaphore. Only the goroutine holding the token can access `val`. This is idiomatic Go but has the overhead of channel operations.

### Intermediate Verification
```bash
go test -run TestCounterCorrectness -v
```
All three counters should produce the correct result under concurrent access.

## Step 2 -- Write Sequential Benchmarks

Benchmark each counter without concurrency to establish the base cost per operation:

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

func BenchmarkChannelCounter_Sequential(b *testing.B) {
    c := NewChannelCounter()
    for i := 0; i < b.N; i++ {
        c.Inc()
    }
}
```

### Intermediate Verification
```bash
go test -bench=Sequential -benchmem
```
Expected: atomic is the fastest (single CPU instruction), mutex is slightly slower (lock/unlock overhead), channel is significantly slower (goroutine scheduling overhead).

## Step 3 -- Write Parallel Benchmarks

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

func BenchmarkChannelCounter_Parallel(b *testing.B) {
    c := NewChannelCounter()
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            c.Inc()
        }
    })
}
```

### Intermediate Verification
```bash
go test -bench=Parallel -benchmem
```
Now observe the difference: under high contention (many goroutines hitting the same counter), atomic still wins for this simple increment. The mutex overhead is moderate. The channel counter is the slowest because each increment involves two channel operations.

## Step 4 -- Benchmark with Mixed Read/Write Workload

Real workloads are not 100% writes. Implement a read-heavy benchmark where 90% of operations are reads and 10% are writes:

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

func BenchmarkMutexCounter_ReadHeavy(b *testing.B) {
    c := &MutexCounter{}
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

Under read-heavy workloads, atomic operations shine because readers never block each other. Mutex readers still contend because `sync.Mutex` does not distinguish readers from writers. For read-heavy scenarios in production, consider `sync.RWMutex`, but atomic operations are even better when applicable.

### Intermediate Verification
```bash
go test -bench=ReadHeavy -benchmem
```
Atomic should be significantly faster than mutex in the read-heavy scenario.

## Step 5 -- Run the Complete Suite and Analyze

Run all benchmarks together:

```bash
go test -bench=. -benchmem -count=3
```

The `-count=3` flag runs each benchmark three times so you can spot variance. Focus on:
- **ns/op**: nanoseconds per operation (lower is better)
- **B/op**: bytes allocated per operation (0 for all three in this case)
- **allocs/op**: allocations per operation

Create a summary table in your notes:

| Scenario | Atomic | Mutex | Channel |
|----------|--------|-------|---------|
| Sequential | ? ns/op | ? ns/op | ? ns/op |
| Parallel | ? ns/op | ? ns/op | ? ns/op |
| Read-Heavy | ? ns/op | ? ns/op | ? ns/op |

### Intermediate Verification
```bash
go test -bench=. -benchmem -count=3 | column -t
```
Fill in the table and analyze the results.

## Common Mistakes

### Benchmark Does Not Use b.N
**Wrong:**
```go
func BenchmarkBad(b *testing.B) {
    for i := 0; i < 1000; i++ { // fixed iteration count
        doWork()
    }
}
```
**What happens:** The benchmark framework cannot auto-calibrate. Results are meaningless because b.N is ignored.

**Fix:** Always loop to `b.N`:
```go
for i := 0; i < b.N; i++ { doWork() }
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
**What happens:** The compiler detects the result is unused and removes the operation entirely.

**Fix:** Assign the result to a package-level variable or use `b.StopTimer()`/`b.StartTimer()` around setup code.

### Not Resetting the Timer After Setup
**Wrong:**
```go
func BenchmarkWithSetup(b *testing.B) {
    c := NewChannelCounter() // setup time included in measurement
    for i := 0; i < b.N; i++ {
        c.Inc()
    }
}
```
**What happens:** For cheap setup this is fine, but for expensive setup the benchmark result includes setup time.

**Fix:**
```go
func BenchmarkWithSetup(b *testing.B) {
    c := NewChannelCounter()
    b.ResetTimer() // exclude setup from measurement
    for i := 0; i < b.N; i++ {
        c.Inc()
    }
}
```

## Verify What You Learned

Add a `BenchmarkAtomicCounter_HighContention` that uses `b.SetParallelism(100)` before `b.RunParallel` to simulate extreme contention (100x the default parallelism). Compare the results with the default parallel benchmark. Document how contention affects the throughput of each approach.

## What's Next
You have completed the atomics and memory ordering section. Continue to the next section on **context** to learn how Go programs propagate cancellation, deadlines, and values across API boundaries and goroutine trees.

## Summary
- Use `testing.B` and `b.N` for Go benchmarks; the framework auto-calibrates iteration count
- `b.RunParallel` benchmarks concurrent workloads with realistic goroutine counts
- Atomic operations are fastest for simple counters, especially under read-heavy workloads
- Mutex has moderate overhead but is simpler for complex critical sections
- Channel-based synchronization has the highest per-operation cost but offers the strongest Go-idiomatic guarantees
- Always benchmark your specific scenario -- "atomics are faster" is not universally true
- Run benchmarks multiple times (`-count=3`) and on the target hardware for reliable results

## Reference
- [testing.B](https://pkg.go.dev/testing#B)
- [Go Blog: Benchmarks](https://go.dev/blog/govulncheck)
- [b.RunParallel](https://pkg.go.dev/testing#B.RunParallel)
- [benchstat tool](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat)
