# 2. GC Phases

<!--
difficulty: advanced
concepts: [gc-phases, sweep-termination, mark-phase, mark-termination, concurrent-sweep, stw-pause, gc-assist]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [tri-color-mark-and-sweep, runtime-scheduler]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of tri-color mark-and-sweep from exercise 01
- Familiarity with goroutine scheduling and runtime internals

## Learning Objectives

After completing this exercise, you will be able to:

- **Identify** the four main phases of Go's GC cycle: sweep termination, mark, mark termination, and sweep
- **Measure** the duration of stop-the-world pauses vs concurrent work
- **Explain** how GC assists force allocating goroutines to help with marking
- **Observe** phase transitions using `runtime` and `GODEBUG` tracing

## Why Understanding GC Phases Matters

Go's garbage collector is not a single monolithic operation. It proceeds through distinct phases -- some concurrent with your application, some requiring brief stop-the-world pauses. Knowing which phases pause your program and which run concurrently lets you predict latency impact, tune GC parameters, and interpret profiling data correctly.

## The Problem

Build a program that triggers and observes multiple GC cycles, measuring the duration of each phase. You will use `runtime.ReadMemStats`, `runtime.GC()`, and `GODEBUG=gctrace=1` to capture phase timing and correlate it with allocation behavior.

## Requirements

1. **Write a function `measureGCPhases`** that forces a GC cycle and captures `MemStats` before and after to compute pause times:

```go
func measureGCPhases() {
    var m runtime.MemStats

    runtime.ReadMemStats(&m)
    prevPauses := m.NumGC

    // Force a GC cycle
    runtime.GC()

    runtime.ReadMemStats(&m)
    newPauses := m.NumGC

    fmt.Printf("GC cycles completed: %d -> %d\n", prevPauses, newPauses)
    fmt.Printf("Total STW pause: %v\n", time.Duration(m.PauseTotalNs))
    fmt.Printf("Last pause: %v\n", time.Duration(m.PauseNs[(m.NumGC+255)%256]))
}
```

2. **Write a function `demonstrateConcurrentMarking`** that allocates objects in a tight loop while GC runs concurrently, showing that application goroutines continue during the mark phase:

```go
func demonstrateConcurrentMarking() {
    fmt.Println("\n=== Concurrent Marking ===")
    var wg sync.WaitGroup
    allocated := int64(0)

    // Allocator goroutine
    wg.Add(1)
    go func() {
        defer wg.Done()
        for i := 0; i < 1_000_000; i++ {
            _ = make([]byte, 256)
            atomic.AddInt64(&allocated, 1)
        }
    }()

    // Periodically report progress
    done := make(chan struct{})
    go func() {
        ticker := time.NewTicker(10 * time.Millisecond)
        defer ticker.Stop()
        for {
            select {
            case <-ticker.C:
                var m runtime.MemStats
                runtime.ReadMemStats(&m)
                fmt.Printf("  Allocated: %d | HeapInuse: %d KB | GC cycles: %d\n",
                    atomic.LoadInt64(&allocated), m.HeapInuse/1024, m.NumGC)
            case <-done:
                return
            }
        }
    }()

    wg.Wait()
    close(done)
}
```

3. **Write a function `measureSTWPauses`** that captures individual pause durations across multiple GC cycles and reports statistics:

```go
func measureSTWPauses(cycles int) {
    fmt.Printf("\n=== STW Pause Distribution (%d cycles) ===\n", cycles)

    // Allocate to generate garbage
    sink := make([][]byte, 0)
    pauses := make([]time.Duration, 0, cycles)

    for i := 0; i < cycles; i++ {
        for j := 0; j < 10000; j++ {
            sink = append(sink, make([]byte, 1024))
        }
        sink = sink[:0] // Drop references

        var m runtime.MemStats
        runtime.GC()
        runtime.ReadMemStats(&m)
        pause := time.Duration(m.PauseNs[(m.NumGC+255)%256])
        pauses = append(pauses, pause)
    }

    // Report statistics
    // ... compute min, max, median, p99
}
```

4. **Write a function `demonstrateGCAssist`** that shows how allocating goroutines are forced to assist with GC marking when allocation outpaces collection:

```go
func demonstrateGCAssist() {
    fmt.Println("\n=== GC Assist Demonstration ===")
    // Heavy allocation that triggers GC assists
    start := time.Now()
    for i := 0; i < 5_000_000; i++ {
        _ = make([]byte, 512)
    }
    elapsed := time.Since(start)

    var m runtime.MemStats
    runtime.ReadMemStats(&m)
    fmt.Printf("  Elapsed: %v\n", elapsed)
    fmt.Printf("  GC cycles: %d\n", m.NumGC)
    fmt.Printf("  Total GC pause: %v\n", time.Duration(m.PauseTotalNs))
}
```

5. **Write a `main` function** that runs all demonstrations and prints instructions for using `GODEBUG=gctrace=1`:

```go
func main() {
    fmt.Println("Go GC Phases Demonstration")
    fmt.Println("Run with: GODEBUG=gctrace=1 go run main.go")
    fmt.Println()

    measureGCPhases()
    demonstrateConcurrentMarking()
    measureSTWPauses(20)
    demonstrateGCAssist()
}
```

## Hints

- Go's GC has two brief STW pauses: one at the start of marking (sweep termination) and one at the end (mark termination). Everything between is concurrent.
- `MemStats.PauseNs` is a circular buffer of the last 256 GC pause durations in nanoseconds
- `GODEBUG=gctrace=1` prints a line per GC cycle showing wall-clock time, CPU time, and heap sizes for each phase
- GC assist means the runtime makes allocating goroutines do marking work proportional to their allocation rate -- this is how the GC keeps up with fast allocators
- Since Go 1.19, mark termination STW pauses are typically under 100 microseconds

## Verification

```bash
GODEBUG=gctrace=1 go run main.go
```

Confirm that:
1. `gctrace` output shows per-cycle phase timing
2. STW pauses (the first and last numbers in the gctrace line) are very short (sub-millisecond)
3. The concurrent mark phase runs alongside your allocator goroutine
4. Multiple GC cycles occur during heavy allocation
5. GC assist slows down allocating goroutines when the heap grows quickly

## What's Next

Continue to [03 - GOGC and GOMEMLIMIT Tuning](../03-gogc-and-gomemlimit/03-gogc-and-gomemlimit.md) to learn how to control GC frequency and memory targets.

## Summary

- Go's GC cycle has four phases: sweep termination (STW), concurrent mark, mark termination (STW), and concurrent sweep
- STW pauses are typically very short (microseconds to low milliseconds)
- The concurrent mark phase does the bulk of the work while application goroutines continue running
- GC assists force allocating goroutines to help with marking, preventing the heap from growing unboundedly
- `MemStats` provides programmatic access to pause durations and GC cycle counts
- `GODEBUG=gctrace=1` provides detailed per-cycle phase timing at runtime

## Reference

- [Go GC Guide](https://tip.golang.org/doc/gc-guide)
- [runtime.MemStats](https://pkg.go.dev/runtime#MemStats)
- [runtime.ReadMemStats](https://pkg.go.dev/runtime#ReadMemStats)
- [GODEBUG environment variable](https://pkg.go.dev/runtime#hdr-Environment_Variables)
