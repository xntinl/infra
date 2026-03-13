# 3. GOGC and GOMEMLIMIT Tuning

<!--
difficulty: advanced
concepts: [gogc, gomemlimit, gc-frequency, heap-target, memory-ballast, soft-limit, gc-tuning]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [tri-color-mark-and-sweep, gc-phases]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of GC phases and tri-color marking from exercises 01-02
- Familiarity with `runtime.MemStats`

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** how GOGC controls GC frequency as a percentage of the live heap
- **Configure** GOMEMLIMIT to set a soft memory ceiling for the Go runtime
- **Analyze** the tradeoff between GC frequency, CPU overhead, and memory usage
- **Demonstrate** the interaction between GOGC and GOMEMLIMIT

## Why GOGC and GOMEMLIMIT Tuning Matters

GOGC (default 100) controls how aggressively the garbage collector runs. A higher value means less frequent GC (lower CPU cost) but more memory usage. GOMEMLIMIT (introduced in Go 1.19) sets a soft memory limit, allowing the GC to work harder to stay within a budget. Together, these two knobs let you trade CPU for memory or vice versa -- critical for services running in containers with fixed memory limits.

## The Problem

Build a benchmarking tool that measures the impact of different GOGC and GOMEMLIMIT settings on GC frequency, pause times, CPU overhead, and peak memory usage. You will run identical workloads under different configurations and compare the results.

## Requirements

1. **Write a function `allocateWorkload`** that performs a consistent allocation pattern returning the total bytes allocated:

```go
func allocateWorkload() uint64 {
    var sink []*[1024]byte
    for i := 0; i < 100_000; i++ {
        obj := new([1024]byte)
        obj[0] = byte(i)
        sink = append(sink, obj)
        if i%1000 == 0 {
            sink = sink[len(sink)/2:] // Drop half periodically
        }
    }
    _ = sink
    runtime.GC()

    var m runtime.MemStats
    runtime.ReadMemStats(&m)
    return m.TotalAlloc
}
```

2. **Write a function `benchWithGOGC`** that runs the workload at different GOGC values and reports results:

```go
func benchWithGOGC(values []int) {
    for _, gogc := range values {
        prev := debug.SetGCPercent(gogc)

        var mBefore runtime.MemStats
        runtime.ReadMemStats(&mBefore)
        start := time.Now()

        allocateWorkload()

        elapsed := time.Since(start)
        var mAfter runtime.MemStats
        runtime.ReadMemStats(&mAfter)

        gcCycles := mAfter.NumGC - mBefore.NumGC
        totalPause := time.Duration(mAfter.PauseTotalNs - mBefore.PauseTotalNs)
        peakHeap := mAfter.HeapSys

        fmt.Printf("  GOGC=%-4d | GCs: %-4d | Pause: %-10v | Peak heap: %-8d KB | Time: %v\n",
            gogc, gcCycles, totalPause, peakHeap/1024, elapsed)
        debug.SetGCPercent(prev)
        runtime.GC()
    }
}
```

3. **Write a function `benchWithGOMEMLIMIT`** that sets different soft memory limits and runs the same workload:

```go
func benchWithGOMEMLIMIT(limits []int64) {
    for _, limit := range limits {
        prevLimit := debug.SetMemoryLimit(limit)
        prevGC := debug.SetGCPercent(100)

        var mBefore runtime.MemStats
        runtime.ReadMemStats(&mBefore)
        start := time.Now()

        allocateWorkload()

        elapsed := time.Since(start)
        var mAfter runtime.MemStats
        runtime.ReadMemStats(&mAfter)

        gcCycles := mAfter.NumGC - mBefore.NumGC
        peakHeap := mAfter.HeapSys

        fmt.Printf("  Limit=%-8s | GCs: %-4d | Peak heap: %-8d KB | Time: %v\n",
            formatBytes(limit), gcCycles, peakHeap/1024, elapsed)
        debug.SetMemoryLimit(prevLimit)
        debug.SetGCPercent(prevGC)
        runtime.GC()
    }
}
```

4. **Write a function `demonstrateGOGCOff`** that disables GOGC entirely (`GOGC=off`) and relies solely on GOMEMLIMIT:

```go
func demonstrateGOGCOff() {
    fmt.Println("\n=== GOGC=off with GOMEMLIMIT ===")
    debug.SetGCPercent(-1)           // GOGC=off
    debug.SetMemoryLimit(64 << 20)   // 64 MB

    var mBefore runtime.MemStats
    runtime.ReadMemStats(&mBefore)

    allocateWorkload()

    var mAfter runtime.MemStats
    runtime.ReadMemStats(&mAfter)

    fmt.Printf("  GC cycles: %d\n", mAfter.NumGC-mBefore.NumGC)
    fmt.Printf("  Peak heap: %d MB\n", mAfter.HeapSys/1024/1024)

    debug.SetGCPercent(100)
    debug.SetMemoryLimit(math.MaxInt64)
}
```

5. **Write a `main` function** that runs all benchmarks with clear output:

```go
func main() {
    fmt.Println("GOGC and GOMEMLIMIT Tuning")
    fmt.Println("==========================")

    fmt.Println("\n=== GOGC Comparison ===")
    benchWithGOGC([]int{50, 100, 200, 400, 1000})

    fmt.Println("\n=== GOMEMLIMIT Comparison ===")
    benchWithGOMEMLIMIT([]int64{32 << 20, 64 << 20, 128 << 20, 256 << 20})

    demonstrateGOGCOff()
}
```

## Hints

- `GOGC=100` (default) means the GC triggers when the heap grows to 2x the live heap size. `GOGC=50` triggers at 1.5x, `GOGC=200` at 3x.
- `debug.SetGCPercent(-1)` disables GOGC-triggered collections entirely. Only GOMEMLIMIT or explicit `runtime.GC()` will trigger collection.
- `debug.SetMemoryLimit()` sets the GOMEMLIMIT at runtime. The GC will work harder (more frequent cycles, more assists) to stay under the limit.
- When both GOGC and GOMEMLIMIT are set, the GC triggers at whichever threshold is reached first.
- A common container pattern: set `GOGC=off` and `GOMEMLIMIT=<container_limit * 0.7>` to maximize throughput while respecting memory budgets.
- `math.MaxInt64` effectively disables GOMEMLIMIT.

## Verification

```bash
go run main.go
```

Confirm that:
1. Higher GOGC values result in fewer GC cycles but higher peak memory
2. Lower GOMEMLIMIT values force more frequent GC cycles
3. `GOGC=off` with GOMEMLIMIT still triggers GC before hitting the memory limit
4. There is a clear CPU-vs-memory tradeoff visible in the timing data

## What's Next

Continue to [04 - Write Barriers and GC Invariants](../04-write-barriers/04-write-barriers.md) to understand how write barriers maintain correctness during concurrent garbage collection.

## Summary

- GOGC controls GC frequency as a percentage: the GC triggers when heap reaches `live * (1 + GOGC/100)`
- GOMEMLIMIT sets a soft memory ceiling; the GC increases effort to stay under the limit
- Setting `GOGC=off` with a GOMEMLIMIT is an effective pattern for container workloads
- Lower GOGC or GOMEMLIMIT means more GC cycles (higher CPU) but less memory usage
- Higher GOGC means fewer GC cycles (lower CPU) but higher peak memory
- `debug.SetGCPercent` and `debug.SetMemoryLimit` allow runtime tuning without restarting

## Reference

- [Go GC Guide](https://tip.golang.org/doc/gc-guide)
- [GOGC documentation](https://pkg.go.dev/runtime#hdr-Environment_Variables)
- [runtime/debug.SetGCPercent](https://pkg.go.dev/runtime/debug#SetGCPercent)
- [runtime/debug.SetMemoryLimit](https://pkg.go.dev/runtime/debug#SetMemoryLimit)
- [Go 1.19 Soft Memory Limit Design](https://github.com/golang/proposal/blob/master/design/48409-soft-memory-limit.md)
