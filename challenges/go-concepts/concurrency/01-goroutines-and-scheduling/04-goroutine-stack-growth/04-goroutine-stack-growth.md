# 4. Goroutine Stack Growth

<!--
difficulty: intermediate
concepts: [initial stack size, dynamic growth, stack copying, segmented stacks, runtime.MemStats]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [01-launching-goroutines, 02-goroutine-vs-os-thread]
-->

## Prerequisites
- Go 1.22+ installed
- Completed [02-goroutine-vs-os-thread](../02-goroutine-vs-os-thread/02-goroutine-vs-os-thread.md)
- Understanding of recursion and call stacks

## Learning Objectives
After completing this exercise, you will be able to:
- **Explain** how goroutine stacks grow dynamically from a small initial size
- **Observe** stack growth by forcing deep recursion
- **Measure** stack memory usage before and after growth
- **Describe** how Go's contiguous stack implementation differs from segmented stacks

## Why Stack Growth Matters
Every function call uses stack space to store local variables, return addresses, and function arguments. In traditional threading models, each thread is given a large, fixed-size stack (typically 1-8 MB) at creation time. If the stack overflows, the program crashes. If the stack is mostly unused, that memory is wasted.

Go takes a radically different approach. Each goroutine starts with a tiny stack (currently 2-8 KB, depending on version). When a function call would exceed the current stack size, the Go runtime automatically allocates a larger stack, copies the contents of the old stack to the new one, and updates all pointers. This process is invisible to your code.

This design has two major benefits. First, goroutines are cheap to create because you only pay for the stack space you actually use. Second, deeply recursive functions that need megabytes of stack space work seamlessly -- the runtime just keeps growing the stack. The only limit is available memory.

Understanding this mechanism helps you reason about why goroutines can be so lightweight and what happens under the hood when your code goes deep.

## Step 1 -- Observing Baseline Stack Usage

Measure how much stack a goroutine uses when it does almost nothing:

```go
func baselineStack() {
    fmt.Println("=== Baseline Stack Usage ===")

    var before, after runtime.MemStats

    runtime.GC()
    runtime.ReadMemStats(&before)

    const count = 10_000
    done := make(chan struct{})

    for i := 0; i < count; i++ {
        go func() {
            <-done // minimal work: just block on channel
        }()
    }
    time.Sleep(50 * time.Millisecond)

    runtime.ReadMemStats(&after)

    stackGrowth := after.StackInuse - before.StackInuse
    perGoroutine := stackGrowth / count

    fmt.Printf("Goroutines:          %d\n", count)
    fmt.Printf("Stack in use:        %d bytes (%.2f MB)\n", stackGrowth, float64(stackGrowth)/(1024*1024))
    fmt.Printf("Stack per goroutine: %d bytes (%.1f KB)\n", perGoroutine, float64(perGoroutine)/1024)

    close(done)
    time.Sleep(100 * time.Millisecond)
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output (values vary):
```
=== Baseline Stack Usage ===
Goroutines:          10000
Stack in use:        81920000 bytes (~78 MB)
Stack per goroutine: 8192 bytes (8.0 KB)
```

Each goroutine uses roughly one page of stack (the minimum allocation unit).

## Step 2 -- Forcing Stack Growth with Recursion

Create a recursive function that forces the stack to grow, then measure the difference:

```go
func recursiveFunction(depth int) int {
    if depth <= 0 {
        return 0
    }
    // Each frame uses stack space for: local variable, argument, return address
    var padding [64]byte // force extra stack usage per frame
    padding[0] = byte(depth)
    _ = padding
    return recursiveFunction(depth-1) + 1
}

func measureStackGrowth() {
    fmt.Println("=== Stack Growth via Recursion ===")

    depths := []int{10, 100, 1_000, 10_000, 50_000}

    for _, depth := range depths {
        var before, after runtime.MemStats

        runtime.GC()
        runtime.ReadMemStats(&before)

        done := make(chan struct{})
        go func() {
            recursiveFunction(depth)
            close(done)
        }()
        <-done

        runtime.ReadMemStats(&after)

        stackDiff := int64(after.StackInuse) - int64(before.StackInuse)
        fmt.Printf("Depth %-8d -> stack change: %+d bytes (%+.1f KB)\n",
            depth, stackDiff, float64(stackDiff)/1024)
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
=== Stack Growth via Recursion ===
Depth 10       -> stack change: +0 bytes (fits in initial stack)
Depth 100      -> stack change: +0 bytes (may still fit)
Depth 1000     -> stack change: +32768 bytes (+32.0 KB)
Depth 10000    -> stack change: +1048576 bytes (+1024.0 KB)
Depth 50000    -> stack change: +4194304 bytes (+4096.0 KB)
```

The stack grows in powers of two as the runtime doubles the stack size each time it needs to expand.

## Step 3 -- Comparing Many Goroutines with Different Stack Depths

Show that goroutines with different workloads consume different amounts of stack:

```go
func compareStackDepths() {
    fmt.Println("=== Stack Usage: Shallow vs Deep Goroutines ===")

    const count = 1000

    scenarios := []struct {
        name  string
        depth int
    }{
        {"idle (blocking)", 0},
        {"shallow (10 frames)", 10},
        {"medium (100 frames)", 100},
        {"deep (1000 frames)", 1000},
    }

    for _, s := range scenarios {
        var before, after runtime.MemStats
        runtime.GC()
        runtime.ReadMemStats(&before)

        done := make(chan struct{})
        ready := make(chan struct{})

        for i := 0; i < count; i++ {
            go func() {
                if s.depth > 0 {
                    recursiveFunction(s.depth)
                }
                ready <- struct{}{}
                <-done
            }()
        }

        // Wait for all to reach their blocking point
        for i := 0; i < count; i++ {
            <-ready
        }

        runtime.ReadMemStats(&after)
        stackDiff := after.StackInuse - before.StackInuse
        perGoroutine := stackDiff / count

        fmt.Printf("%-25s -> %6d bytes/goroutine (%5.1f KB) | total: %.2f MB\n",
            s.name, perGoroutine, float64(perGoroutine)/1024,
            float64(stackDiff)/(1024*1024))

        close(done)
        time.Sleep(50 * time.Millisecond)
    }
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
You should see increasing stack usage per goroutine as the recursion depth increases.

## Step 4 -- Demonstrating that Growth is Transparent

Show that deeply recursive goroutines work seamlessly -- no stack overflow:

```go
func demonstrateTransparency() {
    fmt.Println("=== Transparent Stack Growth ===")

    // This depth would overflow a typical 1MB thread stack
    // but works fine with goroutine stack growth
    const depth = 100_000

    var before, after runtime.MemStats
    runtime.GC()
    runtime.ReadMemStats(&before)

    result := make(chan int)
    go func() {
        result <- recursiveFunction(depth)
    }()

    got := <-result

    runtime.ReadMemStats(&after)
    stackDiff := int64(after.StackInuse) - int64(before.StackInuse)

    fmt.Printf("Recursion depth:  %d\n", depth)
    fmt.Printf("Returned value:   %d\n", got)
    fmt.Printf("Stack grew by:    %.2f MB\n", float64(stackDiff)/(1024*1024))
    fmt.Printf("Status:           No stack overflow! Runtime grew the stack automatically.\n")

    // A fixed 1MB thread stack would have overflowed at ~depth 10,000-15,000
    // depending on frame size.
    fmt.Printf("Equivalent fixed stack: would need ~%.0f MB\n",
        float64(depth*128)/(1024*1024)) // rough estimate: ~128 bytes per frame
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
The program should complete successfully with depth 100,000, demonstrating that goroutine stacks can grow far beyond typical OS thread limits.

## Common Mistakes

### Assuming Stack Size is Fixed
**Wrong thinking:** "My goroutine uses 2KB of stack, so that's all it will ever use."

**What happens:** The 2-8 KB is just the initial allocation. As your code calls deeper functions, the runtime transparently grows the stack. After the goroutine finishes, the grown stack is eventually reclaimed.

**Fix:** Trust the runtime to manage stack sizes. Focus on your algorithm's correctness, not stack management.

### Confusing StackInuse with StackSys
**Wrong:**
```go
var m runtime.MemStats
runtime.ReadMemStats(&m)
fmt.Println(m.StackSys) // memory obtained from OS for stacks
```

**What happens:** `StackSys` is memory reserved from the OS (may include unused pages). `StackInuse` is the actual stack memory being used by goroutines.

**Fix:** Use `StackInuse` when you want to know how much stack your goroutines are actually consuming.

### Creating Goroutines with Unnecessarily Deep Stacks
**Wrong:**
```go
// Recursive processing when iteration would suffice
func processItems(items []int) {
    if len(items) == 0 {
        return
    }
    process(items[0])
    processItems(items[1:]) // unnecessary recursion
}
```

**What happens:** Each recursive call uses stack space. For large slices, this wastes memory and causes unnecessary stack growth.

**Fix:** Use iteration when recursion is not structurally necessary:
```go
func processItems(items []int) {
    for _, item := range items {
        process(item)
    }
}
```

## Verify What You Learned

Write a function that:
1. Launches 100 goroutines, each performing recursion to a different depth (100, 200, ..., 10000)
2. Captures the `StackInuse` at three points: before launch, after all goroutines are running, and after they complete
3. Prints a summary showing peak stack usage and how much was reclaimed after completion

## What's Next
Continue to [05-gomaxprocs-and-parallelism](../05-gomaxprocs-and-parallelism/05-gomaxprocs-and-parallelism.md) to understand the relationship between GOMAXPROCS and actual parallel execution.

## Summary
- Goroutine stacks start small (2-8 KB) and grow dynamically as needed
- The Go runtime detects when a stack is about to overflow and allocates a larger one
- Growth is done by copying the entire stack to a new, larger allocation (contiguous stacks)
- Stack growth is transparent to your code -- no special handling required
- Deep recursion that would crash an OS thread works seamlessly with goroutines
- `runtime.MemStats.StackInuse` measures actual stack memory consumed
- Stacks shrink back when goroutines finish, reclaiming memory

## Reference
- [Go Blog: Contiguous Stacks](https://go.dev/doc/go1.4#runtime)
- [runtime.MemStats](https://pkg.go.dev/runtime#MemStats)
- [Stack size discussion](https://go.dev/doc/faq#goroutines)
- [How Stacks are Handled in Go](https://blog.cloudflare.com/how-stacks-are-handled-in-go/)
