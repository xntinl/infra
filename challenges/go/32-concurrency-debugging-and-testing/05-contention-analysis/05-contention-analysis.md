# 5. Contention Analysis

<!--
difficulty: advanced
concepts: [mutex-profiling, contention, pprof, block-profile, sync-mutex-contention]
tools: [go, pprof]
estimated_time: 40m
bloom_level: analyze
prerequisites: [sync-mutex, sync-rwmutex, goroutines, http-programming]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of mutexes, RWMutex, and concurrent access patterns
- Familiarity with `pprof` basics
- Knowledge of HTTP servers (for pprof endpoints)

## Learning Objectives

After completing this exercise, you will be able to:

- **Profile** mutex contention using Go's block and mutex profilers
- **Analyze** pprof output to identify hotspot locks causing performance degradation
- **Evaluate** lock granularity trade-offs: coarse vs fine-grained locking
- **Optimize** contention by switching to `sync.RWMutex`, sharding, or lock-free data structures

## Why Contention Analysis Matters

A program can be correct (race-free) and still perform poorly because goroutines spend most of their time waiting on locks instead of doing work. Mutex contention is the silent performance killer in concurrent programs. The Go runtime provides block profiling and mutex profiling to identify which locks are causing goroutines to wait.

Without contention profiling, you are guessing which lock is the bottleneck. With it, you can precisely measure wait time per lock site and optimize where it matters.

## The Problem

You have a key-value store protected by a single `sync.Mutex`. Under high read load, performance degrades because readers block each other. Your task is to:

1. Build the contended key-value store
2. Profile it under load using `runtime/pprof` and `net/http/pprof`
3. Identify the contention bottleneck
4. Fix it using progressively better strategies

## Requirements

1. **Baseline** -- a key-value store with a single `sync.Mutex` protecting all reads and writes
2. **Load generator** -- run 100 concurrent goroutines doing 80% reads and 20% writes for 5 seconds
3. **Block profiling** -- enable `runtime.SetBlockProfileRate(1)` and capture a block profile
4. **Mutex profiling** -- enable `runtime.SetMutexProfileFraction(1)` and capture a mutex contention profile
5. **Optimization 1** -- replace `sync.Mutex` with `sync.RWMutex`; measure improvement
6. **Optimization 2** -- shard the store into N buckets with independent locks; measure improvement
7. **Comparison** -- print throughput (ops/sec) for each approach

## Hints

<details>
<summary>Hint 1: Enabling mutex profiling</summary>

```go
import "runtime"

func init() {
    runtime.SetBlockProfileRate(1)      // capture all blocking events
    runtime.SetMutexProfileFraction(1)  // capture all mutex contention
}
```

</details>

<details>
<summary>Hint 2: Capturing profiles programmatically</summary>

```go
import "runtime/pprof"

f, _ := os.Create("mutex.prof")
defer f.Close()
pprof.Lookup("mutex").WriteTo(f, 0)

// Analyze with:
// go tool pprof mutex.prof
```

</details>

<details>
<summary>Hint 3: Sharded store</summary>

```go
type ShardedStore struct {
    shards    []*Shard
    numShards int
}

type Shard struct {
    mu   sync.RWMutex
    data map[string]string
}

func (s *ShardedStore) getShard(key string) *Shard {
    h := fnv.New32a()
    h.Write([]byte(key))
    return s.shards[h.Sum32()%uint32(s.numShards)]
}
```

</details>

<details>
<summary>Hint 4: Benchmarking with b.RunParallel</summary>

```go
func BenchmarkStore(b *testing.B) {
    store := NewStore()
    b.RunParallel(func(pb *testing.PB) {
        i := 0
        for pb.Next() {
            if i%5 == 0 {
                store.Set(fmt.Sprintf("key-%d", i), "value")
            } else {
                store.Get(fmt.Sprintf("key-%d", i))
            }
            i++
        }
    })
}
```

</details>

## Verification

```bash
# Run benchmarks
go test -bench=. -benchmem -race ./...

# Capture and view mutex profile
go test -run TestContention -mutexprofile=mutex.prof ./...
go tool pprof -top mutex.prof

# Capture and view block profile
go test -run TestContention -blockprofile=block.prof ./...
go tool pprof -top block.prof
```

Your benchmarks should show:
- Single-mutex store has the lowest throughput under contention
- RWMutex improves read throughput significantly (readers do not block each other)
- Sharded store provides the best throughput by eliminating cross-key contention
- Profiles clearly identify the lock site causing contention

## What's Next

Continue to [06 - Goroutine Dump Analysis](../06-goroutine-dump-analysis/06-goroutine-dump-analysis.md) to learn how to analyze goroutine stack dumps for debugging.

## Summary

- Mutex contention degrades performance even in race-free programs
- Enable block profiling with `runtime.SetBlockProfileRate(1)` and mutex profiling with `runtime.SetMutexProfileFraction(1)`
- Use `go tool pprof` to identify which lock sites cause the most waiting
- `sync.RWMutex` reduces contention when reads dominate writes
- Lock sharding eliminates cross-key contention by giving each shard its own lock
- Benchmark with `b.RunParallel` to measure throughput under contention

## Reference

- [runtime.SetBlockProfileRate](https://pkg.go.dev/runtime#SetBlockProfileRate)
- [runtime.SetMutexProfileFraction](https://pkg.go.dev/runtime#SetMutexProfileFraction)
- [pprof package](https://pkg.go.dev/runtime/pprof)
- [Profiling Go programs](https://go.dev/blog/pprof)
