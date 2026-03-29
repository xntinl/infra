# 8. A Million Goroutines

<!--
difficulty: advanced
concepts: [goroutine scalability, memory overhead, runtime.MemStats, practical limits, when NOT to goroutine]
tools: [go]
estimated_time: 45m
bloom_level: create
prerequisites: [01-launching-goroutines, 02-goroutine-vs-os-thread, 05-gomaxprocs-and-parallelism]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01 through 07 in this section
- Understanding of goroutine memory overhead and scheduling
- At least 4 GB of free RAM (this exercise creates many goroutines)

## Learning Objectives
After completing this exercise, you will be able to:
- **Measure** the actual memory and time cost of launching goroutines at scale
- **Identify** the practical limits of goroutine creation on your machine
- **Evaluate** when the goroutine-per-task pattern is appropriate vs when it is not
- **Use** `runtime.MemStats` to build detailed resource profiles

## Why Push the Limits
Go developers often hear that goroutines are "cheap" and that you can create millions of them. This exercise puts that claim to the test. By systematically measuring the cost of creating 1K, 10K, 100K, and 1M goroutines, you will develop an intuitive understanding of exactly how cheap (or expensive) goroutines are.

More importantly, this exercise teaches you when NOT to create goroutines. Just because you can create a million goroutines does not mean you should. Each goroutine consumes memory, occupies scheduler run queues, and competes for CPU time. For CPU-bound work, the optimal number of goroutines is typically `runtime.NumCPU()`, not "as many as possible." For IO-bound work, goroutines are often the right abstraction, but unbounded creation can exhaust memory.

Understanding these tradeoffs separates informed Go developers from those who use goroutines indiscriminately. After this exercise, you will be able to reason about goroutine costs in real systems and make architecture decisions grounded in measurement rather than folklore.

## Step 1 -- Measuring Launch Time at Scale

Measure how long it takes to create increasing numbers of goroutines:

```go
func measureLaunchTime() {
    fmt.Println("=== Goroutine Launch Time ===")

    counts := []int{1_000, 10_000, 100_000, 500_000, 1_000_000}

    fmt.Printf("%-12s %-15s %-15s %-15s\n", "Count", "Launch Time", "Per Goroutine", "Goroutines/sec")
    fmt.Println(strings.Repeat("-", 60))

    for _, count := range counts {
        done := make(chan struct{})

        start := time.Now()
        for i := 0; i < count; i++ {
            go func() {
                <-done
            }()
        }
        launchTime := time.Since(start)

        perGoroutine := launchTime / time.Duration(count)
        perSecond := float64(count) / launchTime.Seconds()

        fmt.Printf("%-12d %-15v %-15v %-15.0f\n",
            count, launchTime.Round(time.Millisecond), perGoroutine, perSecond)

        close(done)
        time.Sleep(100 * time.Millisecond) // let goroutines clean up
        runtime.GC()
    }
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected pattern:
```
Count        Launch Time     Per Goroutine   Goroutines/sec
------------------------------------------------------------
1000         1ms             1us             1000000
10000        8ms             800ns           1250000
100000       75ms            750ns           1333333
500000       380ms           760ns           1315789
1000000      780ms           780ns           1282051
```

Each goroutine takes roughly 500ns-1us to create. This means you can create ~1 million goroutines per second.

## Step 2 -- Measuring Memory at Scale

Use `runtime.MemStats` to measure actual memory consumption:

```go
func measureMemory() {
    fmt.Println("=== Memory Consumption at Scale ===")

    counts := []int{1_000, 10_000, 100_000, 500_000, 1_000_000}

    fmt.Printf("%-12s %-15s %-15s %-15s %-15s\n",
        "Count", "StackInUse", "HeapInUse", "Sys (Total)", "Per Goroutine")
    fmt.Println(strings.Repeat("-", 75))

    for _, count := range counts {
        // Baseline
        runtime.GC()
        runtime.GC() // double GC for thorough cleanup
        var before runtime.MemStats
        runtime.ReadMemStats(&before)

        done := make(chan struct{})
        for i := 0; i < count; i++ {
            go func() {
                <-done
            }()
        }
        time.Sleep(50 * time.Millisecond) // let all goroutines start

        var after runtime.MemStats
        runtime.ReadMemStats(&after)

        stackDiff := after.StackInuse - before.StackInuse
        heapDiff := after.HeapInuse - before.HeapInuse
        sysDiff := after.Sys - before.Sys
        total := stackDiff + heapDiff
        perGoroutine := total / uint64(count)

        fmt.Printf("%-12d %-15s %-15s %-15s %-15s\n",
            count,
            formatBytes(stackDiff),
            formatBytes(heapDiff),
            formatBytes(sysDiff),
            formatBytes(perGoroutine),
        )

        close(done)
        time.Sleep(200 * time.Millisecond)
        runtime.GC()
    }
    fmt.Println()
}

func formatBytes(b uint64) string {
    switch {
    case b >= 1024*1024*1024:
        return fmt.Sprintf("%.2f GB", float64(b)/(1024*1024*1024))
    case b >= 1024*1024:
        return fmt.Sprintf("%.2f MB", float64(b)/(1024*1024))
    case b >= 1024:
        return fmt.Sprintf("%.2f KB", float64(b)/1024)
    default:
        return fmt.Sprintf("%d B", b)
    }
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected pattern:
```
Count        StackInUse      HeapInUse       Sys (Total)     Per Goroutine
---------------------------------------------------------------------------
1000         8.00 MB         0.50 MB         10.00 MB        8.50 KB
10000        80.00 MB        5.00 MB         90.00 MB        8.50 KB
100000       800.00 MB       50.00 MB        900.00 MB       8.50 KB
500000       3.91 GB         250.00 MB       4.50 GB         8.50 KB
1000000      7.81 GB         500.00 MB       9.00 GB         8.50 KB
```

At ~8 KB per goroutine, 1 million goroutines consume roughly 8 GB of memory.

## Step 3 -- Measuring GC Impact

Show how goroutine count affects garbage collection:

```go
func measureGCImpact() {
    fmt.Println("=== GC Impact at Scale ===")

    counts := []int{1_000, 10_000, 100_000, 500_000}

    fmt.Printf("%-12s %-15s %-15s %-15s\n",
        "Count", "GC Pause", "Num GC", "Alloc Rate")
    fmt.Println(strings.Repeat("-", 60))

    for _, count := range counts {
        runtime.GC()
        var before runtime.MemStats
        runtime.ReadMemStats(&before)

        done := make(chan struct{})
        for i := 0; i < count; i++ {
            go func() {
                <-done
            }()
        }
        time.Sleep(50 * time.Millisecond)

        // Force a GC and measure its duration
        gcStart := time.Now()
        runtime.GC()
        gcDuration := time.Since(gcStart)

        var after runtime.MemStats
        runtime.ReadMemStats(&after)

        numGC := after.NumGC - before.NumGC
        allocRate := float64(after.TotalAlloc-before.TotalAlloc) / (1024 * 1024)

        fmt.Printf("%-12d %-15v %-15d %-15.2f MB\n",
            count, gcDuration.Round(time.Microsecond), numGC, allocRate)

        close(done)
        time.Sleep(200 * time.Millisecond)
        runtime.GC()
    }
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
GC pause time increases with goroutine count because the GC must scan goroutine stacks.

## Step 4 -- When NOT to Create Goroutines

Demonstrate that for CPU-bound work, more goroutines is not better:

```go
func whenNotToGoroutine() {
    fmt.Println("=== When NOT to Create Goroutines ===")

    // CPU-bound work: sum a large slice
    data := make([]int, 10_000_000)
    for i := range data {
        data[i] = i
    }

    sumSlice := func(slice []int) int64 {
        var sum int64
        for _, v := range slice {
            sum += int64(v)
        }
        return sum
    }

    goroutineCounts := []int{1, runtime.NumCPU(), 100, 1_000, 10_000}

    fmt.Printf("Summing %d elements:\n", len(data))
    fmt.Printf("%-15s %-15s %-15s\n", "Goroutines", "Wall-Clock", "Overhead")
    fmt.Println(strings.Repeat("-", 48))

    var baselineTime time.Duration

    for _, numG := range goroutineCounts {
        chunkSize := len(data) / numG
        if chunkSize == 0 {
            chunkSize = 1
        }

        start := time.Now()

        results := make(chan int64, numG)
        launched := 0

        for i := 0; i < len(data); i += chunkSize {
            end := i + chunkSize
            if end > len(data) {
                end = len(data)
            }
            chunk := data[i:end]
            launched++
            go func(s []int) {
                results <- sumSlice(s)
            }(chunk)
        }

        var total int64
        for i := 0; i < launched; i++ {
            total += <-results
        }

        elapsed := time.Since(start)
        if numG == 1 {
            baselineTime = elapsed
        }

        overhead := float64(elapsed) / float64(baselineTime)
        fmt.Printf("%-15d %-15v %-15.2fx\n", numG, elapsed.Round(time.Microsecond), overhead)
        _ = total
    }

    fmt.Println()
    fmt.Println("Key insight: for CPU-bound work, NumCPU goroutines is optimal.")
    fmt.Println("More goroutines add scheduling overhead without improving throughput.")
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected pattern:
```
Goroutines      Wall-Clock      Overhead
------------------------------------------------
1               12ms            1.00x
8               3ms             0.25x   (speedup from parallelism)
100             4ms             0.33x   (similar, slight overhead)
1000            8ms             0.67x   (overhead growing)
10000           45ms            3.75x   (SLOWER than single goroutine!)
```

## Step 5 -- Building a Scalability Profile

Create a comprehensive report:

```go
func scalabilityProfile() {
    fmt.Println("=== Scalability Profile ===")
    fmt.Println("Building a complete profile of goroutine costs on this machine...\n")

    type measurement struct {
        count       int
        launchTime  time.Duration
        stackMem    uint64
        heapMem     uint64
        gcPause     time.Duration
    }

    counts := []int{100, 1_000, 10_000, 100_000}

    var measurements []measurement

    for _, count := range counts {
        runtime.GC()
        runtime.GC()
        var before runtime.MemStats
        runtime.ReadMemStats(&before)

        done := make(chan struct{})

        launchStart := time.Now()
        for i := 0; i < count; i++ {
            go func() {
                <-done
            }()
        }
        launchTime := time.Since(launchStart)
        time.Sleep(50 * time.Millisecond)

        gcStart := time.Now()
        runtime.GC()
        gcPause := time.Since(gcStart)

        var after runtime.MemStats
        runtime.ReadMemStats(&after)

        measurements = append(measurements, measurement{
            count:      count,
            launchTime: launchTime,
            stackMem:   after.StackInuse - before.StackInuse,
            heapMem:    after.HeapInuse - before.HeapInuse,
            gcPause:    gcPause,
        })

        close(done)
        time.Sleep(200 * time.Millisecond)
    }

    // Print summary
    fmt.Printf("%-10s %-12s %-12s %-12s %-12s %-12s\n",
        "Count", "Launch", "Stack", "Heap", "GC Pause", "KB/goroutine")
    fmt.Println(strings.Repeat("-", 72))

    for _, m := range measurements {
        perG := float64(m.stackMem+m.heapMem) / float64(m.count) / 1024
        fmt.Printf("%-10d %-12v %-12s %-12s %-12v %-12.1f\n",
            m.count,
            m.launchTime.Round(time.Millisecond),
            formatBytes(m.stackMem),
            formatBytes(m.heapMem),
            m.gcPause.Round(time.Microsecond),
            perG,
        )
    }

    fmt.Println("\n--- Guidelines ---")
    fmt.Printf("CPU cores:         %d\n", runtime.NumCPU())
    fmt.Printf("CPU-bound optimal: %d goroutines\n", runtime.NumCPU())
    fmt.Println("IO-bound:          1 goroutine per concurrent I/O operation")
    fmt.Println("Practical ceiling:  depends on RAM; ~100K-1M for most machines")
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
This produces a complete profile of goroutine costs on your specific machine.

## Common Mistakes

### Unbounded Goroutine Creation in Servers
**Wrong:**
```go
for {
    conn, _ := listener.Accept()
    go handleConnection(conn) // unbounded: can create millions under load
}
```

**What happens:** Under heavy load, goroutine count grows without limit, eventually exhausting memory.

**Fix:** Use a semaphore or worker pool to bound concurrency:
```go
sem := make(chan struct{}, maxConcurrency)
for {
    conn, _ := listener.Accept()
    sem <- struct{}{} // blocks when at capacity
    go func() {
        defer func() { <-sem }()
        handleConnection(conn)
    }()
}
```

### Ignoring Memory When Scaling Goroutines
**Wrong thinking:** "Goroutines are free, so I'll create one per item in my 10M-row dataset."

**What happens:** 10M goroutines * ~8KB each = ~80GB of memory. OOM kill.

**Fix:** Use a worker pool pattern:
```go
work := make(chan Item, 1000)
for i := 0; i < runtime.NumCPU(); i++ {
    go worker(work)
}
for _, item := range bigDataset {
    work <- item
}
```

### Not Measuring Before Deciding
**Wrong thinking:** "I'll use exactly 1000 goroutines because someone said that's a good number."

**What happens:** The optimal number depends on your workload, machine, and available memory.

**Fix:** Benchmark with different goroutine counts and measure actual performance. Use this exercise's approach to find the sweet spot for your specific scenario.

## Verify What You Learned

Create a comprehensive benchmark that:
1. Finds the maximum number of goroutines your machine can create before running out of memory (use binary search, starting from 100K)
2. Plots (in text) the relationship between goroutine count and: launch time, memory usage, and GC pause
3. Recommends the practical ceiling for your machine with a safety margin

**Warning:** This may consume significant memory. Save your work before running.

## What's Next
You have completed the goroutines and scheduling section. Continue to [02-channels](../../02-channels/) to learn how goroutines communicate and synchronize.

## Summary
- Creating a goroutine takes approximately 500ns-1us (about 1M goroutines/second)
- Each goroutine consumes roughly 2-8 KB of stack memory plus heap overhead
- 1 million goroutines requires approximately 8-16 GB of memory
- GC pause time grows with goroutine count because stacks must be scanned
- For CPU-bound work, NumCPU goroutines is optimal; more adds overhead
- For IO-bound work, goroutine-per-task is appropriate but should be bounded
- Always measure on your target hardware; never assume goroutine costs
- Use worker pools or semaphores to bound goroutine creation in production

## Reference
- [runtime.MemStats](https://pkg.go.dev/runtime#MemStats)
- [runtime.ReadMemStats](https://pkg.go.dev/runtime#ReadMemStats)
- [Go Blog: Go GC: Prioritizing low latency and simplicity](https://go.dev/blog/go15gc)
- [Concurrency in Go (Katherine Cox-Buday)](https://www.oreilly.com/library/view/concurrency-in-go/9781491941294/)
