# 2. Goroutine vs OS Thread

<!--
difficulty: basic
concepts: [goroutine lightweight nature, dynamic stack, OS thread comparison, runtime.NumGoroutine]
tools: [go]
estimated_time: 20m
bloom_level: understand
prerequisites: [01-launching-goroutines]
-->

## Prerequisites
- Go 1.22+ installed
- Completed [01-launching-goroutines](../01-launching-goroutines/01-launching-goroutines.md)
- Basic understanding of memory concepts (stack, heap, kilobytes vs megabytes)

## Learning Objectives
After completing this exercise, you will be able to:
- **Explain** why goroutines are cheaper than OS threads
- **Measure** the memory footprint of goroutines at scale
- **Use** `runtime.NumGoroutine()` to observe active goroutine counts
- **Compare** goroutine overhead with typical OS thread overhead

## Why This Matters
One of Go's superpowers is the ability to run thousands or even millions of concurrent tasks without exhausting system resources. This is possible because goroutines are not OS threads. An OS thread typically reserves 1-8 MB of stack memory at creation, requires a kernel context switch to schedule, and consumes significant kernel resources. A goroutine, by contrast, starts with a stack of just 2-8 KB (the exact size depends on the Go version) and is scheduled entirely in user space by the Go runtime.

This difference is not academic. A Java application creating 10,000 threads would consume roughly 10-80 GB of stack memory alone, making it impractical on most machines. A Go application can comfortably create 10,000 goroutines using just 20-80 MB of stack memory. This is what enables the "one-goroutine-per-connection" pattern that makes Go so effective for network servers.

Understanding this cost difference helps you make informed architectural decisions. When you know a goroutine costs approximately the same as a small struct allocation, you stop worrying about creating them and start thinking in terms of concurrent tasks rather than thread pools.

## Step 1 -- Counting Active Goroutines

Use `runtime.NumGoroutine()` to observe how the goroutine count changes as you create and destroy goroutines.

Implement the `countGoroutines` function:

```go
func countGoroutines() {
    fmt.Println("=== Counting Goroutines ===")

    fmt.Printf("Goroutines at start: %d\n", runtime.NumGoroutine())

    done := make(chan struct{})
    for i := 0; i < 10; i++ {
        go func() {
            <-done // block until channel is closed
        }()
    }

    // Give goroutines a moment to start
    time.Sleep(10 * time.Millisecond)
    fmt.Printf("Goroutines after launching 10: %d\n", runtime.NumGoroutine())

    close(done) // release all goroutines
    time.Sleep(10 * time.Millisecond)
    fmt.Printf("Goroutines after releasing: %d\n\n", runtime.NumGoroutine())
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Counting Goroutines ===
Goroutines at start: 1
Goroutines after launching 10: 11
Goroutines after releasing: 1
```

The initial count is 1 because `main` itself is a goroutine.

## Step 2 -- Measuring Goroutine Memory

Use `runtime.MemStats` to measure the actual memory cost per goroutine. Implement `measureMemory`:

```go
func measureMemory() {
    fmt.Println("=== Goroutine Memory Measurement ===")

    var before, after runtime.MemStats

    // Force GC and read baseline
    runtime.GC()
    runtime.ReadMemStats(&before)

    const count = 100_000
    done := make(chan struct{})

    for i := 0; i < count; i++ {
        go func() {
            <-done
        }()
    }
    time.Sleep(50 * time.Millisecond)

    runtime.GC()
    runtime.ReadMemStats(&after)

    totalBytes := after.Sys - before.Sys
    perGoroutine := totalBytes / count

    fmt.Printf("Goroutines created: %d\n", count)
    fmt.Printf("Active goroutines:  %d\n", runtime.NumGoroutine())
    fmt.Printf("Memory increase:    %.2f MB\n", float64(totalBytes)/(1024*1024))
    fmt.Printf("Per goroutine:      ~%d bytes\n", perGoroutine)

    close(done)
    time.Sleep(100 * time.Millisecond)
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output (values will vary by system):
```
=== Goroutine Memory Measurement ===
Goroutines created: 100000
Active goroutines:  100001
Memory increase:    ~200-800 MB (Sys includes reserves)
Per goroutine:      ~2000-8000 bytes
```

The `Sys` metric includes memory reserved from the OS, so it may overcount. The key insight is that each goroutine costs kilobytes, not megabytes.

## Step 3 -- Comparing with OS Thread Cost

Implement `compareWithThreads` to show the theoretical cost difference:

```go
func compareWithThreads() {
    fmt.Println("=== Goroutine vs OS Thread: Cost Comparison ===")

    goroutineStack := 8 * 1024        // 8 KB initial goroutine stack
    osThreadStack := 8 * 1024 * 1024  // 8 MB typical OS thread stack (Linux default)

    counts := []int{100, 1_000, 10_000, 100_000, 1_000_000}

    fmt.Printf("%-12s %-18s %-18s %-10s\n", "Count", "Goroutine Mem", "OS Thread Mem", "Ratio")
    fmt.Println(strings.Repeat("-", 62))

    for _, n := range counts {
        goroutineMB := float64(n*goroutineStack) / (1024 * 1024)
        threadMB := float64(n*osThreadStack) / (1024 * 1024)
        ratio := float64(osThreadStack) / float64(goroutineStack)

        fmt.Printf("%-12d %-18s %-18s %-10s\n",
            n,
            formatMB(goroutineMB),
            formatMB(threadMB),
            fmt.Sprintf("1:%.0f", ratio),
        )
    }
    fmt.Println()
}

func formatMB(mb float64) string {
    if mb >= 1024 {
        return fmt.Sprintf("%.1f GB", mb/1024)
    }
    return fmt.Sprintf("%.1f MB", mb)
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Goroutine vs OS Thread: Cost Comparison ===
Count        Goroutine Mem      OS Thread Mem      Ratio
--------------------------------------------------------------
100          0.8 MB             800.0 MB           1:1024
1000         7.8 MB             7.8 GB             1:1024
10000        78.1 MB            78.1 GB            1:1024
100000       781.3 MB           781.3 GB           1:1024
1000000      7.6 GB             7629.4 GB          1:1024
```

This is a theoretical comparison. In practice, the gap is even larger because OS threads also consume kernel resources.

## Step 4 -- Stack Size Observation

Implement `showStackInfo` to demonstrate that goroutine stacks are dynamic:

```go
func showStackInfo() {
    fmt.Println("=== Goroutine Stack Info ===")

    // A goroutine doing minimal work uses minimal stack
    var before, after runtime.MemStats

    runtime.GC()
    runtime.ReadMemStats(&before)

    const count = 50_000
    done := make(chan struct{})

    for i := 0; i < count; i++ {
        go func() {
            <-done // minimal stack usage: just waiting
        }()
    }
    time.Sleep(50 * time.Millisecond)

    runtime.ReadMemStats(&after)
    stackInUse := after.StackInuse - before.StackInuse
    perGoroutine := stackInUse / count

    fmt.Printf("Goroutines:       %d\n", count)
    fmt.Printf("Stack in use:     %.2f MB\n", float64(stackInUse)/(1024*1024))
    fmt.Printf("Stack/goroutine:  %d bytes\n", perGoroutine)
    fmt.Printf("(Compare to OS thread default: 8,388,608 bytes)\n")

    close(done)
    time.Sleep(100 * time.Millisecond)
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
The stack per goroutine should be in the range of 2,048-8,192 bytes, confirming the lightweight nature.

## Common Mistakes

### Assuming Goroutines Are Free
**Wrong thinking:** "Goroutines are cheap, so I'll create one for every tiny operation."

**What happens:** While goroutines are cheap, they are not free. Each one still consumes memory and scheduler time. Creating millions of goroutines that contend for the same resource will cause performance degradation.

**Fix:** Use goroutines for genuinely concurrent work. For CPU-bound tasks, the optimal goroutine count is typically `runtime.NumCPU()`. For I/O-bound tasks, create goroutines based on the number of independent operations, not arbitrarily.

### Forgetting to Release Goroutines
**Wrong:**
```go
done := make(chan struct{})
for i := 0; i < 10000; i++ {
    go func() {
        <-done // blocks forever if done is never closed
    }()
}
// forgot to close(done) -- 10,000 goroutines leaked
```

**What happens:** Goroutine leak. The goroutines stay alive consuming memory until the process exits.

**Fix:** Always have a clear lifecycle for goroutines. Use `close(done)`, `context.Context`, or `sync.WaitGroup` to ensure cleanup.

## Verify What You Learned

Create a benchmark function that:
1. Launches 1,000, 10,000, and 50,000 goroutines in separate rounds
2. For each round, measures the time to launch all goroutines and the `StackInuse` after launch
3. Prints a summary table showing count, launch time, and stack memory

This will give you an intuitive feel for the real cost of goroutines on your machine.

## What's Next
Continue to [03-gmp-model-in-action](../03-gmp-model-in-action/03-gmp-model-in-action.md) to understand how the Go scheduler maps goroutines onto OS threads.

## Summary
- Goroutines start with a 2-8 KB stack; OS threads start with 1-8 MB
- This 1000x difference enables Go to run millions of concurrent goroutines
- `runtime.NumGoroutine()` reports the current count of active goroutines
- `runtime.MemStats` provides detailed memory metrics including stack usage
- Goroutines are cheap but not free; leaked goroutines still consume resources
- The lightweight nature of goroutines is what enables Go's "one-goroutine-per-task" pattern

## Reference
- [runtime.NumGoroutine](https://pkg.go.dev/runtime#NumGoroutine)
- [runtime.MemStats](https://pkg.go.dev/runtime#MemStats)
- [Go Blog: Goroutines are not threads](https://go.dev/blog/waza-talk)
- [Why goroutines instead of threads?](https://go.dev/doc/faq#goroutines)
