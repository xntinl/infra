# 3. GMP Model in Action

<!--
difficulty: intermediate
concepts: [G (goroutine), M (machine/OS thread), P (processor/logical processor), runtime.GOMAXPROCS, runtime.NumGoroutine, scheduler internals]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [01-launching-goroutines, 02-goroutine-vs-os-thread]
-->

## Prerequisites
- Go 1.22+ installed
- Completed [01-launching-goroutines](../01-launching-goroutines/01-launching-goroutines.md) and [02-goroutine-vs-os-thread](../02-goroutine-vs-os-thread/02-goroutine-vs-os-thread.md)
- Conceptual understanding of OS threads vs goroutines

## Learning Objectives
After completing this exercise, you will be able to:
- **Describe** the three components of Go's GMP scheduler model
- **Observe** how G, M, and P counts change during program execution
- **Demonstrate** that M (OS thread) count can exceed P count during blocking syscalls
- **Analyze** scheduler behavior using runtime statistics

## Why the GMP Model
Go's scheduler uses a model with three key entities: G (goroutine), M (machine/OS thread), and P (processor). Understanding this model transforms goroutines from "magic lightweight threads" into a well-understood scheduling system.

**G (Goroutine):** The unit of work. Contains the stack, instruction pointer, and other scheduling state. Gs are what your code creates with the `go` keyword.

**M (Machine):** An OS thread. The Go runtime creates Ms as needed to execute Gs. An M must be attached to a P to run Go code. Ms can be blocked in syscalls without holding a P.

**P (Processor):** A logical processor that acts as a resource context. Each P has a local run queue of Gs waiting to execute. The number of Ps is set by `GOMAXPROCS` and determines the maximum parallelism. A P must be acquired by an M before it can execute any G.

The key insight is that when an M blocks on a syscall (like file I/O or a CGo call), it releases its P so another M can pick it up and continue running Gs. This is why the number of Ms can grow beyond the number of Ps -- blocked Ms need to be replaced to maintain throughput.

## Step 1 -- Observing P Count

Use `runtime.GOMAXPROCS` to read and set the number of logical processors.

Implement `observePCount`:

```go
func observePCount() {
    fmt.Println("=== P (Processor) Count ===")

    currentP := runtime.GOMAXPROCS(0) // 0 means "read current value"
    numCPU := runtime.NumCPU()

    fmt.Printf("Number of CPUs:    %d\n", numCPU)
    fmt.Printf("GOMAXPROCS (Ps):   %d\n", currentP)
    fmt.Printf("Default: GOMAXPROCS == NumCPU (since Go 1.5)\n")

    // Temporarily set to 2 and observe
    old := runtime.GOMAXPROCS(2)
    fmt.Printf("\nSet GOMAXPROCS to 2 (was %d)\n", old)
    fmt.Printf("Current GOMAXPROCS: %d\n", runtime.GOMAXPROCS(0))

    // Restore original
    runtime.GOMAXPROCS(old)
    fmt.Printf("Restored GOMAXPROCS to %d\n\n", old)
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output (CPU count varies):
```
=== P (Processor) Count ===
Number of CPUs:    8
GOMAXPROCS (Ps):   8
Default: GOMAXPROCS == NumCPU (since Go 1.5)

Set GOMAXPROCS to 2 (was 8)
Current GOMAXPROCS: 2
Restored GOMAXPROCS to 8
```

## Step 2 -- Observing G Count Under Load

Create goroutines in waves and observe `runtime.NumGoroutine()`.

Implement `observeGCount`:

```go
func observeGCount() {
    fmt.Println("=== G (Goroutine) Count Under Load ===")

    barriers := make([]chan struct{}, 3)
    for i := range barriers {
        barriers[i] = make(chan struct{})
    }

    waveSizes := []int{100, 500, 1000}

    for wave, size := range waveSizes {
        for i := 0; i < size; i++ {
            go func() {
                <-barriers[wave]
            }()
        }
        time.Sleep(10 * time.Millisecond)
        fmt.Printf("After wave %d (+%d goroutines): total G = %d\n",
            wave+1, size, runtime.NumGoroutine())
    }

    // Release in reverse order
    for i := len(barriers) - 1; i >= 0; i-- {
        close(barriers[i])
        time.Sleep(10 * time.Millisecond)
        fmt.Printf("After releasing wave %d: total G = %d\n",
            i+1, runtime.NumGoroutine())
    }
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output pattern:
```
After wave 1 (+100 goroutines): total G = 101
After wave 2 (+500 goroutines): total G = 601
After wave 3 (+1000 goroutines): total G = 1601
After releasing wave 3: total G = 601
After releasing wave 2: total G = 101
After releasing wave 1: total G = 1
```

## Step 3 -- Demonstrating M Growth During Syscalls

When goroutines make blocking syscalls, the runtime creates additional OS threads. Implement `demonstrateMGrowth`:

```go
func demonstrateMGrowth() {
    fmt.Println("=== M (OS Thread) Growth During Blocking ===")

    // Set a low P count to make the effect visible
    old := runtime.GOMAXPROCS(2)
    defer runtime.GOMAXPROCS(old)

    fmt.Printf("GOMAXPROCS: %d\n", runtime.GOMAXPROCS(0))
    fmt.Printf("Goroutines before: %d\n", runtime.NumGoroutine())

    var wg sync.WaitGroup
    const numBlockers = 20

    // Launch goroutines that perform blocking operations.
    // We simulate blocking by doing file operations (actual syscalls).
    for i := 0; i < numBlockers; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            // Create and immediately remove a temp file -- this involves syscalls
            // that can cause the M to block and release its P
            f, err := os.CreateTemp("", "gmp-demo-*")
            if err != nil {
                return
            }
            name := f.Name()
            // Write some data to force actual I/O
            f.Write([]byte("blocking syscall demo\n"))
            f.Sync() // force flush to disk -- blocking syscall
            f.Close()
            os.Remove(name)
        }(i)
    }

    // Check goroutine count while they're running
    time.Sleep(5 * time.Millisecond)
    fmt.Printf("Goroutines during blocking ops: %d\n", runtime.NumGoroutine())
    fmt.Println("(OS threads may exceed GOMAXPROCS during syscalls)")

    wg.Wait()
    fmt.Printf("Goroutines after completion: %d\n\n", runtime.NumGoroutine())
}
```

### Intermediate Verification
```bash
go run main.go
```
The goroutine count during blocking ops will be higher than GOMAXPROCS. The runtime creates extra Ms to compensate for those blocked in syscalls.

## Step 4 -- Building a GMP Status Reporter

Create a utility that prints a summary of the scheduler state at a point in time:

```go
func gmpStatus(label string) {
    var m runtime.MemStats
    runtime.ReadMemStats(&m)

    fmt.Printf("[%s] G=%d  P=%d  NumCPU=%d  StackInUse=%.1fKB  Sys=%.1fMB\n",
        label,
        runtime.NumGoroutine(),
        runtime.GOMAXPROCS(0),
        runtime.NumCPU(),
        float64(m.StackInuse)/1024,
        float64(m.Sys)/(1024*1024),
    )
}

func demonstrateGMPLifecycle() {
    fmt.Println("=== GMP Lifecycle ===")

    gmpStatus("initial")

    done := make(chan struct{})
    for i := 0; i < 500; i++ {
        go func() { <-done }()
    }
    time.Sleep(10 * time.Millisecond)
    gmpStatus("500 goroutines blocked")

    for i := 0; i < 500; i++ {
        go func() { <-done }()
    }
    time.Sleep(10 * time.Millisecond)
    gmpStatus("1000 goroutines blocked")

    close(done)
    time.Sleep(50 * time.Millisecond)
    gmpStatus("all released")

    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Observe how G count grows while P remains constant, and how StackInUse correlates with goroutine count.

## Common Mistakes

### Confusing GOMAXPROCS with Goroutine Limit
**Wrong thinking:** "Setting GOMAXPROCS(4) means only 4 goroutines can exist."

**What happens:** GOMAXPROCS sets the number of Ps (logical processors), not the number of Gs. You can have millions of goroutines with GOMAXPROCS=1 -- they just won't run in parallel.

**Fix:** Understand that GOMAXPROCS controls parallelism (how many goroutines run simultaneously), not concurrency (how many goroutines exist).

### Assuming M Count Equals P Count
**Wrong thinking:** "There are always exactly GOMAXPROCS OS threads."

**What happens:** The runtime creates additional Ms when goroutines block in syscalls. The M count can grow well beyond GOMAXPROCS.

**Fix:** Think of P as the parallelism limit for Go code execution. Ms are the actual OS threads, and their count floats based on how many are needed.

### Using runtime.GOMAXPROCS in Production Code
**Wrong:**
```go
func handler(w http.ResponseWriter, r *http.Request) {
    runtime.GOMAXPROCS(1) // terrible idea: affects the entire process
}
```

**What happens:** `GOMAXPROCS` is a process-wide setting. Changing it at runtime affects all goroutines, not just yours.

**Fix:** Set `GOMAXPROCS` once at startup (or let the default apply). Use it in diagnostic/educational code, not in business logic.

## Verify What You Learned

Create a program that:
1. Prints the initial GMP status
2. Launches 3 phases: 100 CPU-bound goroutines, then 100 I/O-bound goroutines (using temp file writes), then both simultaneously
3. Prints GMP status during each phase
4. Explains in comments why the behavior differs between phases

## What's Next
Continue to [04-goroutine-stack-growth](../04-goroutine-stack-growth/04-goroutine-stack-growth.md) to understand how goroutine stacks grow dynamically.

## Summary
- **G** (goroutine): lightweight unit of work, created with `go`
- **M** (machine): OS thread that executes goroutine code
- **P** (processor): logical processor; `GOMAXPROCS` sets the count
- A P must be held by an M to execute Go code
- When an M blocks in a syscall, it releases its P for another M
- The M count can exceed P count during heavy syscall usage
- `GOMAXPROCS` controls parallelism, not the number of goroutines
- Default `GOMAXPROCS` equals `runtime.NumCPU()` since Go 1.5

## Reference
- [Go Scheduler Design Doc](https://docs.google.com/document/d/1TTj4T2JO42uD5ID9e89oa0sLKhJYD0Y_kqxDv3I3XMw)
- [runtime.GOMAXPROCS](https://pkg.go.dev/runtime#GOMAXPROCS)
- [runtime.NumGoroutine](https://pkg.go.dev/runtime#NumGoroutine)
- [Scalable Go Scheduler (Dmitry Vyukov)](https://docs.google.com/document/d/1TTj4T2JO42uD5ID9e89oa0sLKhJYD0Y_kqxDv3I3XMw)
