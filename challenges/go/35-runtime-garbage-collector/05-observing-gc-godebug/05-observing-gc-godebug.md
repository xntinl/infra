# 5. Observing GC with GODEBUG

<!--
difficulty: advanced
concepts: [godebug-gctrace, gc-observation, memstats, gc-cpu-fraction, heap-goal, runtime-metrics]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [gc-phases, gogc-and-gomemlimit]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of GC phases from exercise 02
- Familiarity with GOGC/GOMEMLIMIT from exercise 03

## Learning Objectives

After completing this exercise, you will be able to:

- **Interpret** `GODEBUG=gctrace=1` output fields including heap sizes, pause times, and CPU fractions
- **Use** `runtime.ReadMemStats` and `runtime/metrics` to programmatically monitor GC behavior
- **Build** a real-time GC monitor that tracks key metrics across GC cycles
- **Correlate** application behavior with GC activity through structured observation

## Why Observing the GC Matters

You cannot tune what you cannot measure. Go provides rich GC observability through `GODEBUG=gctrace=1`, `runtime.ReadMemStats`, and the `runtime/metrics` package. These tools let you see exactly when GC runs, how long it pauses your program, how much CPU it consumes, and how the heap grows. This is essential for diagnosing memory issues, validating tuning changes, and understanding production behavior.

## The Problem

Build a GC monitoring toolkit that captures, parses, and displays GC metrics in real time. The toolkit will use both `MemStats` and `runtime/metrics` to provide a comprehensive view of GC behavior during different allocation patterns.

## Requirements

1. **Write a function `parseGCTrace`** that explains each field of the `gctrace` output format:

```go
func parseGCTrace() {
    fmt.Println("=== GODEBUG=gctrace=1 Output Format ===")
    fmt.Println("gc # @#s #%: #+#+# ms clock, #+#/#/#+# ms cpu, #->#-># MB, # MB goal, # MB stacks, # MB globals, # P")
    fmt.Println()
    fmt.Println("Fields:")
    fmt.Println("  gc #          - GC cycle number")
    fmt.Println("  @#s           - seconds since program start")
    fmt.Println("  #%            - percentage of time in GC since start")
    fmt.Println("  #+#+# ms clock - wall-clock time: sweep-term STW + mark + mark-term STW")
    fmt.Println("  #+#/#/#+# ms cpu - CPU time: assist + bg mark (dedicated/fractional/idle) + mark-term")
    fmt.Println("  #->#-># MB    - heap before -> heap after -> live heap")
    fmt.Println("  # MB goal     - target heap size")
    fmt.Println("  # P           - GOMAXPROCS")
}
```

2. **Write a `GCMonitor` struct** that captures metrics every GC cycle using `runtime.ReadMemStats`:

```go
type GCSnapshot struct {
    Timestamp   time.Time
    NumGC       uint32
    PauseNs     uint64
    HeapAlloc   uint64
    HeapInuse   uint64
    HeapObjects uint64
    GCCPUFrac   float64
    NextGC      uint64
}

type GCMonitor struct {
    snapshots []GCSnapshot
    mu        sync.Mutex
    done      chan struct{}
}

func NewGCMonitor() *GCMonitor {
    return &GCMonitor{done: make(chan struct{})}
}

func (gm *GCMonitor) Start(interval time.Duration) {
    go func() {
        ticker := time.NewTicker(interval)
        defer ticker.Stop()
        for {
            select {
            case <-ticker.C:
                gm.capture()
            case <-gm.done:
                return
            }
        }
    }()
}
```

3. **Write a function `useRuntimeMetrics`** that demonstrates the `runtime/metrics` package for GC observation:

```go
func useRuntimeMetrics() {
    fmt.Println("\n=== runtime/metrics Package ===")

    samples := []metrics.Sample{
        {Name: "/gc/cycles/total:gc-cycles"},
        {Name: "/gc/heap/allocs:bytes"},
        {Name: "/gc/pauses:seconds"},
        {Name: "/memory/classes/heap/objects:bytes"},
        {Name: "/gc/heap/goal:bytes"},
    }

    metrics.Read(samples)

    for _, s := range samples {
        switch s.Value.Kind() {
        case metrics.KindUint64:
            fmt.Printf("  %s = %d\n", s.Name, s.Value.Uint64())
        case metrics.KindFloat64:
            fmt.Printf("  %s = %.4f\n", s.Name, s.Value.Float64())
        case metrics.KindFloat64Histogram:
            h := s.Value.Float64Histogram()
            fmt.Printf("  %s = histogram with %d buckets\n", s.Name, len(h.Buckets))
        }
    }
}
```

4. **Write a function `monitorDuringWorkload`** that runs a workload while the GC monitor captures metrics, then prints a summary table:

```go
func monitorDuringWorkload() {
    monitor := NewGCMonitor()
    monitor.Start(5 * time.Millisecond)

    // Run workload with varying allocation intensity
    phases := []struct {
        name  string
        alloc int
        size  int
    }{
        {"light", 10000, 64},
        {"heavy", 100000, 1024},
        {"burst", 50000, 4096},
    }

    for _, phase := range phases {
        fmt.Printf("\n  Phase: %s\n", phase.name)
        for i := 0; i < phase.alloc; i++ {
            _ = make([]byte, phase.size)
        }
    }

    monitor.Stop()
    monitor.PrintSummary()
}
```

5. **Wire everything together in `main`** and include instructions for running with `GODEBUG=gctrace=1`.

## Hints

- `runtime/metrics` is preferred over `runtime.ReadMemStats` for new code -- it is more efficient and covers more metrics
- `ReadMemStats` stops the world briefly to capture a consistent snapshot, so avoid calling it in hot paths
- The `GCCPUFraction` field in `MemStats` gives the fraction of CPU time spent on GC since program start
- `gctrace` output goes to stderr, not stdout -- redirect accordingly when parsing
- The `/gc/pauses:seconds` metric is a histogram; extract percentiles from the bucket boundaries

## Verification

```bash
GODEBUG=gctrace=1 go run main.go 2>gc.log
```

Confirm that:
1. You can read and interpret every field in the gctrace output
2. The GCMonitor captures snapshots with increasing NumGC counts
3. `runtime/metrics` returns meaningful values for GC-related metrics
4. Different workload phases show different GC frequencies and heap sizes
5. The GC CPU fraction stays reasonable (typically 1-5% for well-tuned programs)

## What's Next

Continue to [06 - GC Pacer and Target Heap](../06-gc-pacer/06-gc-pacer.md) to understand how the GC pacer algorithm decides when to trigger collection.

## Summary

- `GODEBUG=gctrace=1` outputs one line per GC cycle with phase timings, heap sizes, and CPU usage
- `runtime.ReadMemStats` provides programmatic access to GC statistics (but incurs a brief STW)
- `runtime/metrics` is the modern, efficient way to query GC metrics without STW overhead
- GC CPU fraction, pause duration, and heap goal are the key metrics for tuning
- Building a monitoring layer helps correlate application behavior with GC activity
- Always measure before tuning -- intuition about GC behavior is often wrong

## Reference

- [runtime.MemStats](https://pkg.go.dev/runtime#MemStats)
- [runtime/metrics](https://pkg.go.dev/runtime/metrics)
- [GODEBUG documentation](https://pkg.go.dev/runtime#hdr-Environment_Variables)
- [Go GC Guide -- Observing GC](https://tip.golang.org/doc/gc-guide#Observing)
