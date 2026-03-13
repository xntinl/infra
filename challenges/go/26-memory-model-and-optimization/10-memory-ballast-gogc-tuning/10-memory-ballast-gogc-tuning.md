# 10. Memory Ballast and GOGC Tuning

<!--
difficulty: advanced
concepts: [gogc, gomemlimit, memory-ballast, gc-tuning, gc-pacer]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [memory-profiling, benchmarking-methodology, trace-tool-goroutine-scheduling]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of Go's garbage collector behavior
- Familiarity with memory profiling and trace analysis

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** how `GOGC` controls the GC trigger threshold
- **Use** `GOMEMLIMIT` (Go 1.19+) to set a soft memory ceiling
- **Understand** the historical memory ballast technique and why `GOMEMLIMIT` replaced it
- **Tune** GC behavior for latency-sensitive vs throughput-oriented workloads

## Why GOGC and GOMEMLIMIT

Go's garbage collector triggers when the heap reaches a certain size relative to the heap after the last GC. The `GOGC` environment variable controls this ratio: `GOGC=100` (default) means GC triggers when the heap doubles. `GOGC=200` lets it triple, reducing GC frequency at the cost of higher memory.

Before Go 1.19, teams used a "memory ballast" -- a large, never-freed allocation -- to inflate the baseline heap and reduce GC frequency. `GOMEMLIMIT` is the modern replacement: it sets a soft memory ceiling, allowing the GC to be lazy until memory pressure approaches the limit.

## The Problem

A latency-sensitive HTTP service performs many small allocations per request. Default GC settings cause frequent GC pauses. Tune the GC to reduce pause frequency while respecting memory constraints.

## Requirements

1. Observe GC behavior with default settings using `GODEBUG=gctrace=1`
2. Experiment with different `GOGC` values
3. Apply `GOMEMLIMIT` to achieve lower GC frequency without unbounded memory growth
4. Compare the memory ballast technique with `GOMEMLIMIT`

## Step 1 -- Observe Default GC Behavior

```bash
mkdir -p ~/go-exercises/gc-tuning && cd ~/go-exercises/gc-tuning
go mod init gc-tuning
```

Create `server.go`:

```go
package main

import (
	"fmt"
	"math/rand"
	"runtime"
	"time"
)

// simulateWork creates allocations that mimic request processing.
func simulateWork() []byte {
	// Allocate a response-sized buffer
	size := 1024 + rand.Intn(4096)
	buf := make([]byte, size)
	for i := range buf {
		buf[i] = byte(rand.Intn(256))
	}
	return buf
}

func printGCStats() {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	fmt.Printf("HeapAlloc: %d MB | NumGC: %d | PauseTotalNs: %d ms | NextGC: %d MB\n",
		stats.HeapAlloc/1024/1024,
		stats.NumGC,
		stats.PauseTotalNs/1_000_000,
		stats.NextGC/1024/1024,
	)
}

func main() {
	fmt.Println("Starting workload simulation...")
	start := time.Now()

	// Simulate processing 100,000 requests
	var retained [][]byte // keep some data alive
	for i := 0; i < 100_000; i++ {
		result := simulateWork()
		if i%10 == 0 {
			// Retain 10% of results to simulate live data
			retained = append(retained, result)
		}
		if i%25_000 == 0 {
			printGCStats()
		}
	}

	elapsed := time.Since(start)
	printGCStats()
	fmt.Printf("\nDuration: %v\n", elapsed)
	fmt.Printf("Retained items: %d\n", len(retained))
}
```

Run with GC tracing:

```bash
GODEBUG=gctrace=1 go run server.go 2>&1 | tail -20
```

The `gctrace` output shows each GC cycle: heap before/after, pause time, and CPU fraction.

## Step 2 -- Increase GOGC

```bash
# Default: GOGC=100 (GC triggers when heap doubles)
GOGC=100 go run server.go

# Aggressive: GOGC=50 (GC triggers at 1.5x)
GOGC=50 go run server.go

# Relaxed: GOGC=400 (GC triggers at 5x)
GOGC=400 go run server.go
```

Compare `NumGC` and `PauseTotalNs` across runs. Higher `GOGC` means fewer GC cycles but higher peak memory.

## Step 3 -- Use GOMEMLIMIT

`GOMEMLIMIT` sets a soft memory target. Combined with `GOGC=off`, the GC only runs when approaching the limit:

```bash
# 256 MB soft limit, GC off (relies solely on GOMEMLIMIT)
GOMEMLIMIT=256MiB GOGC=off go run server.go

# 256 MB soft limit, GOGC=100 (belt and suspenders)
GOMEMLIMIT=256MiB GOGC=100 go run server.go
```

The recommended production pattern is to set `GOMEMLIMIT` to ~90% of your container's memory limit and keep `GOGC=100` as a safety net.

## Step 4 -- Compare with Historical Ballast Technique

Before `GOMEMLIMIT`, the ballast technique was common:

```go
func main() {
	// Memory ballast: allocate a large block to inflate baseline heap.
	// GC triggers at 2x baseline, so a 100MB ballast means GC
	// triggers at 200MB instead of at a much lower threshold.
	ballast := make([]byte, 100<<20) // 100 MB
	_ = ballast

	// ... rest of the program
}
```

This works but wastes virtual memory and is a hack. `GOMEMLIMIT` achieves the same effect cleanly.

## Step 5 -- Programmatic Tuning

You can also set these values in code:

```go
package main

import (
	"runtime/debug"
)

func init() {
	// Set GOGC programmatically
	debug.SetGCPercent(200)

	// Set GOMEMLIMIT programmatically (Go 1.19+)
	debug.SetMemoryLimit(256 << 20) // 256 MB
}
```

## Hints

- `GOMEMLIMIT` is a soft limit -- Go may briefly exceed it under pressure
- Setting `GOGC=off` with `GOMEMLIMIT` is aggressive; the program may OOM if the limit is too tight
- Use `GODEBUG=gctrace=1` to observe the effect of tuning on GC frequency
- In containers, set `GOMEMLIMIT` to ~80-90% of the cgroup memory limit
- Monitor `runtime.MemStats.NumGC` and `PauseTotalNs` in production

## Verification

- Default `GOGC=100` produces the most GC cycles
- `GOGC=400` reduces GC cycles by ~4x but uses more memory
- `GOMEMLIMIT=256MiB GOGC=off` produces the fewest GC cycles while respecting the memory ceiling
- `gctrace` output confirms fewer GC events with tuned settings
- Total runtime decreases as GC frequency decreases (less CPU spent on GC)

## What's Next

With GC tuning understood, the next exercise explores false sharing and cache contention -- a performance problem invisible to profilers.

## Summary

`GOGC` controls the GC trigger ratio (heap growth factor), while `GOMEMLIMIT` (Go 1.19+) sets a soft memory ceiling. For latency-sensitive services, increase `GOGC` or use `GOMEMLIMIT` to reduce GC frequency. The memory ballast technique is obsolete -- use `GOMEMLIMIT` instead. Always set `GOMEMLIMIT` below your actual memory limit to leave headroom, and use `GODEBUG=gctrace=1` to observe the impact of changes.

## Reference

- [A Guide to the Go Garbage Collector](https://tip.golang.org/doc/gc-guide)
- [runtime/debug.SetGCPercent](https://pkg.go.dev/runtime/debug#SetGCPercent)
- [runtime/debug.SetMemoryLimit](https://pkg.go.dev/runtime/debug#SetMemoryLimit)
- [GOMEMLIMIT proposal](https://github.com/golang/go/issues/48409)
