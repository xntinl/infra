# 5. GOMAXPROCS and Parallelism

<!--
difficulty: intermediate
concepts: [GOMAXPROCS, concurrency vs parallelism, CPU-bound vs IO-bound, wall-clock time]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [01-launching-goroutines, 03-gmp-model-in-action]
-->

## Prerequisites
- Go 1.22+ installed
- Completed [03-gmp-model-in-action](../03-gmp-model-in-action/03-gmp-model-in-action.md)
- Understanding of GMP model basics

## Learning Objectives
After completing this exercise, you will be able to:
- **Differentiate** between concurrency and parallelism with concrete examples
- **Measure** the impact of GOMAXPROCS on CPU-bound workloads
- **Demonstrate** that IO-bound work benefits less from additional Ps
- **Determine** the optimal GOMAXPROCS for different workload types

## Why GOMAXPROCS Matters
Concurrency is about structure; parallelism is about execution. A program is concurrent if it is structured as multiple independently executing tasks. A program is parallel if those tasks actually run at the same time on different CPU cores. Go makes concurrency easy with goroutines, but parallelism is controlled by `GOMAXPROCS`.

With `GOMAXPROCS=1`, all goroutines share a single logical processor. They are concurrent (they can make progress independently) but not parallel (only one runs at any given instant). Increasing GOMAXPROCS allows multiple goroutines to execute truly simultaneously on different cores.

The practical impact depends on the workload. CPU-bound work (computation, hashing, sorting) benefits enormously from parallelism because more Ps mean more work happening simultaneously. IO-bound work (network calls, disk reads, database queries) benefits less because goroutines spend most of their time waiting, not computing. Understanding this distinction is essential for tuning real Go applications.

## Step 1 -- Concurrency vs Parallelism Visualization

Demonstrate the difference by running tasks with GOMAXPROCS=1 and GOMAXPROCS=N:

```go
func visualizeConcurrencyVsParallelism() {
    fmt.Println("=== Concurrency vs Parallelism ===")

    work := func(id int) {
        start := time.Now()
        // Simulate CPU work
        result := 0
        for i := 0; i < 50_000_000; i++ {
            result += i
        }
        elapsed := time.Since(start)
        fmt.Printf("  worker %d: %v (result: %d)\n", id, elapsed, result%1000)
    }

    for _, procs := range []int{1, runtime.NumCPU()} {
        runtime.GOMAXPROCS(procs)
        fmt.Printf("\nGOMAXPROCS=%d:\n", procs)

        start := time.Now()
        var wg sync.WaitGroup

        for i := 0; i < 4; i++ {
            wg.Add(1)
            go func(id int) {
                defer wg.Done()
                work(id)
            }(i)
        }

        wg.Wait()
        fmt.Printf("  Total wall-clock: %v\n", time.Since(start))
    }

    runtime.GOMAXPROCS(runtime.NumCPU())
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected pattern:
```
GOMAXPROCS=1:
  worker 0: 45ms
  worker 1: 44ms
  worker 2: 45ms
  worker 3: 44ms
  Total wall-clock: ~180ms  (sequential: all share one P)

GOMAXPROCS=8:
  worker 0: 45ms
  worker 3: 46ms
  worker 1: 46ms
  worker 2: 47ms
  Total wall-clock: ~48ms   (parallel: each on a separate P)
```

With GOMAXPROCS=1, total time is roughly `N * per_task` (sequential). With GOMAXPROCS=N, total time approaches `per_task` (parallel).

## Step 2 -- CPU-Bound Benchmark

Create a proper benchmark that measures speedup across different GOMAXPROCS values:

```go
func cpuBoundBenchmark() {
    fmt.Println("=== CPU-Bound Benchmark ===")

    cpuWork := func() int {
        result := 0
        for i := 0; i < 100_000_000; i++ {
            result ^= i
        }
        return result
    }

    numWorkers := runtime.NumCPU()
    maxProcs := []int{1, 2, 4}
    if runtime.NumCPU() >= 8 {
        maxProcs = append(maxProcs, 8)
    }
    if runtime.NumCPU() >= 16 {
        maxProcs = append(maxProcs, 16)
    }

    fmt.Printf("Workers: %d (one per CPU)\n", numWorkers)
    fmt.Printf("%-12s %-15s %-10s\n", "GOMAXPROCS", "Wall-Clock", "Speedup")
    fmt.Println(strings.Repeat("-", 40))

    var baselineTime time.Duration

    for _, procs := range maxProcs {
        runtime.GOMAXPROCS(procs)

        start := time.Now()
        var wg sync.WaitGroup

        for i := 0; i < numWorkers; i++ {
            wg.Add(1)
            go func() {
                defer wg.Done()
                cpuWork()
            }()
        }

        wg.Wait()
        elapsed := time.Since(start)

        if procs == 1 {
            baselineTime = elapsed
        }

        speedup := float64(baselineTime) / float64(elapsed)
        fmt.Printf("%-12d %-15v %-10.2fx\n", procs, elapsed.Round(time.Millisecond), speedup)
    }

    runtime.GOMAXPROCS(runtime.NumCPU())
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected pattern:
```
Workers: 8 (one per CPU)
GOMAXPROCS   Wall-Clock      Speedup
----------------------------------------
1            800ms           1.00x
2            410ms           1.95x
4            205ms           3.90x
8            105ms           7.62x
```

Speedup should be roughly linear for CPU-bound work until you hit the physical core count.

## Step 3 -- IO-Bound Comparison

Show that IO-bound work benefits much less from additional Ps:

```go
func ioBoundComparison() {
    fmt.Println("=== IO-Bound: GOMAXPROCS Has Less Impact ===")

    ioWork := func() {
        // Simulate IO-bound work: mostly waiting, minimal CPU
        time.Sleep(50 * time.Millisecond)
    }

    numWorkers := 20

    fmt.Printf("Workers: %d (IO-bound, 50ms sleep each)\n", numWorkers)
    fmt.Printf("%-12s %-15s %-10s\n", "GOMAXPROCS", "Wall-Clock", "Speedup")
    fmt.Println(strings.Repeat("-", 40))

    var baselineTime time.Duration

    for _, procs := range []int{1, 2, 4, runtime.NumCPU()} {
        runtime.GOMAXPROCS(procs)

        start := time.Now()
        var wg sync.WaitGroup

        for i := 0; i < numWorkers; i++ {
            wg.Add(1)
            go func() {
                defer wg.Done()
                ioWork()
            }()
        }

        wg.Wait()
        elapsed := time.Since(start)

        if procs == 1 {
            baselineTime = elapsed
        }

        speedup := float64(baselineTime) / float64(elapsed)
        fmt.Printf("%-12d %-15v %-10.2fx\n", procs, elapsed.Round(time.Millisecond), speedup)
    }

    runtime.GOMAXPROCS(runtime.NumCPU())
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected pattern:
```
Workers: 20 (IO-bound, 50ms sleep each)
GOMAXPROCS   Wall-Clock      Speedup
----------------------------------------
1            52ms            1.00x
2            51ms            1.02x
4            51ms            1.02x
8            51ms            1.02x
```

All goroutines sleep concurrently regardless of GOMAXPROCS, so wall-clock time is roughly constant.

## Step 4 -- Mixed Workload Analysis

Create a workload that mixes CPU and IO to show intermediate behavior:

```go
func mixedWorkload() {
    fmt.Println("=== Mixed Workload (CPU + IO) ===")

    mixedWork := func() {
        // CPU phase
        result := 0
        for i := 0; i < 50_000_000; i++ {
            result ^= i
        }
        // IO phase
        time.Sleep(20 * time.Millisecond)
    }

    numWorkers := 8

    fmt.Printf("Workers: %d (CPU work + 20ms IO wait each)\n", numWorkers)
    fmt.Printf("%-12s %-15s %-10s\n", "GOMAXPROCS", "Wall-Clock", "Speedup")
    fmt.Println(strings.Repeat("-", 40))

    var baselineTime time.Duration

    for _, procs := range []int{1, 2, 4, runtime.NumCPU()} {
        runtime.GOMAXPROCS(procs)

        start := time.Now()
        var wg sync.WaitGroup

        for i := 0; i < numWorkers; i++ {
            wg.Add(1)
            go func() {
                defer wg.Done()
                mixedWork()
            }()
        }

        wg.Wait()
        elapsed := time.Since(start)

        if procs == 1 {
            baselineTime = elapsed
        }

        speedup := float64(baselineTime) / float64(elapsed)
        fmt.Printf("%-12d %-15v %-10.2fx\n", procs, elapsed.Round(time.Millisecond), speedup)
    }

    runtime.GOMAXPROCS(runtime.NumCPU())
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
The speedup should be between pure CPU-bound and pure IO-bound -- better than flat but not linear.

## Common Mistakes

### Setting GOMAXPROCS Higher Than CPU Count
**Wrong:**
```go
runtime.GOMAXPROCS(100) // on a 4-core machine
```

**What happens:** For CPU-bound work, this provides no benefit beyond the physical core count and may slightly hurt performance due to context switching overhead. For IO-bound work, it makes no meaningful difference.

**Fix:** For most applications, leave GOMAXPROCS at its default (`runtime.NumCPU()`). Only tune it when benchmarks prove a different value is better.

### Assuming More Goroutines Means More Parallelism
**Wrong thinking:** "If I create 1000 goroutines, they'll all run in parallel."

**What happens:** Only `GOMAXPROCS` goroutines can execute Go code simultaneously. The rest wait in run queues.

**Fix:** For CPU-bound work, creating more goroutines than Ps increases scheduling overhead without improving throughput. Match goroutine count to GOMAXPROCS for CPU-bound tasks.

### Benchmarking Without Warming Up
**Wrong:**
```go
// First run includes JIT-like warmup costs (GC, runtime initialization)
start := time.Now()
doWork()
fmt.Println(time.Since(start))
```

**What happens:** The first measurement includes one-time costs that inflate the result.

**Fix:** Run the workload once as a warmup before measuring, or use `testing.B` for proper benchmarks.

## Verify What You Learned

Create a program that:
1. Takes a workload type as input: "cpu", "io", or "mixed"
2. Runs the workload with GOMAXPROCS from 1 to NumCPU
3. Prints a summary table and identifies the optimal GOMAXPROCS for that workload
4. Explains why the optimal value differs between workload types

## What's Next
Continue to [06-cooperative-scheduling](../06-cooperative-scheduling/06-cooperative-scheduling.md) to understand how the Go scheduler decides when to switch between goroutines.

## Summary
- **Concurrency** is structure (multiple tasks in flight); **parallelism** is execution (multiple tasks running simultaneously)
- `GOMAXPROCS` controls the number of Ps, which limits true parallelism
- CPU-bound work shows roughly linear speedup up to the physical core count
- IO-bound work benefits minimally from additional Ps because goroutines spend most time waiting
- Mixed workloads show intermediate speedup
- Default `GOMAXPROCS=NumCPU()` is correct for most applications
- Creating more goroutines than Ps does not increase parallelism for CPU-bound work

## Reference
- [runtime.GOMAXPROCS](https://pkg.go.dev/runtime#GOMAXPROCS)
- [Go Blog: Concurrency is not parallelism](https://go.dev/blog/waza-talk)
- [Rob Pike: Concurrency is not Parallelism (video)](https://www.youtube.com/watch?v=oV9rvDllKEg)
