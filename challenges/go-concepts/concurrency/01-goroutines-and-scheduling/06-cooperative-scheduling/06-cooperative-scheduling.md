# 6. Cooperative Scheduling

<!--
difficulty: intermediate
concepts: [scheduling points, runtime.Gosched, preemption, Go 1.14+ async preemption, tight loops]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [01-launching-goroutines, 03-gmp-model-in-action, 05-gomaxprocs-and-parallelism]
-->

## Prerequisites
- Go 1.22+ installed
- Completed [05-gomaxprocs-and-parallelism](../05-gomaxprocs-and-parallelism/05-gomaxprocs-and-parallelism.md)
- Understanding of GOMAXPROCS and the GMP model

## Learning Objectives
After completing this exercise, you will be able to:
- **Identify** natural scheduling points in Go code
- **Use** `runtime.Gosched()` to yield the processor explicitly
- **Explain** how Go 1.14+ async preemption prevents goroutine starvation
- **Analyze** scheduling behavior under different conditions

## Why Scheduling Matters
Go's scheduler is primarily cooperative: goroutines voluntarily yield the processor at well-defined scheduling points. These include channel operations, mutex locks, system calls, `time.Sleep`, and function calls (where the runtime checks if the stack needs growth). At each of these points, the scheduler has an opportunity to switch to a different goroutine.

Before Go 1.14, a goroutine running a tight computational loop with no function calls and no scheduling points could monopolize a P indefinitely, starving other goroutines. Go 1.14 introduced asynchronous preemption using OS signals: the runtime periodically sends a signal to running goroutines, forcing them to yield even in tight loops.

Understanding scheduling behavior helps you write code that plays well with the scheduler. While async preemption prevents outright starvation, cooperative yielding through natural scheduling points leads to smoother, more predictable concurrent behavior. In rare cases, you may need `runtime.Gosched()` to explicitly yield.

## Step 1 -- Natural Scheduling Points

Demonstrate where the scheduler gets a chance to switch goroutines:

```go
func naturalSchedulingPoints() {
    fmt.Println("=== Natural Scheduling Points ===")
    runtime.GOMAXPROCS(1) // force single P to make scheduling visible
    defer runtime.GOMAXPROCS(runtime.NumCPU())

    var wg sync.WaitGroup

    // Goroutine A: uses channel operations (scheduling points)
    wg.Add(1)
    go func() {
        defer wg.Done()
        ch := make(chan int, 1)
        for i := 0; i < 5; i++ {
            ch <- i   // scheduling point: channel send
            _ = <-ch   // scheduling point: channel receive
            fmt.Printf("A:%d ", i)
        }
    }()

    // Goroutine B: uses fmt.Println (involves syscall = scheduling point)
    wg.Add(1)
    go func() {
        defer wg.Done()
        for i := 0; i < 5; i++ {
            fmt.Printf("B:%d ", i) // scheduling point: I/O syscall
        }
    }()

    wg.Wait()
    fmt.Println("\n")
}
```

### Intermediate Verification
```bash
go run main.go
```
With GOMAXPROCS=1, the output will show interleaving because both goroutines hit scheduling points. The exact interleaving varies between runs.

## Step 2 -- Using runtime.Gosched()

Show how `runtime.Gosched()` explicitly yields the processor:

```go
func explicitYielding() {
    fmt.Println("=== Explicit Yielding with runtime.Gosched() ===")
    runtime.GOMAXPROCS(1)
    defer runtime.GOMAXPROCS(runtime.NumCPU())

    var wg sync.WaitGroup

    wg.Add(2)

    go func() {
        defer wg.Done()
        for i := 0; i < 5; i++ {
            fmt.Printf("A:%d ", i)
            runtime.Gosched() // explicitly yield to let B run
        }
    }()

    go func() {
        defer wg.Done()
        for i := 0; i < 5; i++ {
            fmt.Printf("B:%d ", i)
            runtime.Gosched() // explicitly yield to let A run
        }
    }()

    wg.Wait()
    fmt.Println()

    fmt.Println("With Gosched(), goroutines alternate more predictably.")
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output pattern (may vary slightly):
```
A:0 B:0 A:1 B:1 A:2 B:2 A:3 B:3 A:4 B:4
```
The alternation is much more regular than without `Gosched()`.

## Step 3 -- Async Preemption (Go 1.14+)

Demonstrate that even a tight loop without scheduling points gets preempted:

```go
func asyncPreemption() {
    fmt.Println("=== Async Preemption (Go 1.14+) ===")
    runtime.GOMAXPROCS(1)
    defer runtime.GOMAXPROCS(runtime.NumCPU())

    // In Go < 1.14, this tight loop would starve goroutine B forever
    // In Go 1.14+, the runtime sends a preemption signal

    var counter int64
    done := make(chan struct{})

    // Goroutine A: tight computational loop (no natural scheduling points)
    go func() {
        for {
            select {
            case <-done:
                return
            default:
                atomic.AddInt64(&counter, 1)
            }
        }
    }()

    // Goroutine B: tries to run periodically
    completed := make(chan bool)
    go func() {
        successes := 0
        for i := 0; i < 10; i++ {
            time.Sleep(10 * time.Millisecond) // scheduling point
            successes++
            fmt.Printf("  B got scheduled (iteration %d, counter=%d)\n",
                i, atomic.LoadInt64(&counter))
        }
        completed <- successes == 10
    }()

    success := <-completed
    close(done)

    if success {
        fmt.Println("  All 10 iterations of B completed!")
        fmt.Println("  Async preemption prevented starvation.")
    }
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Async Preemption (Go 1.14+) ===
  B got scheduled (iteration 0, counter=12345678)
  B got scheduled (iteration 1, counter=24567890)
  ...
  B got scheduled (iteration 9, counter=98765432)
  All 10 iterations of B completed!
  Async preemption prevented starvation.
```

Even though goroutine A is in a tight loop, B still gets to run.

## Step 4 -- Comparing Scheduling Behavior

Build a comparison that shows how different patterns affect fairness:

```go
func compareSchedulingBehavior() {
    fmt.Println("=== Scheduling Behavior Comparison ===")
    runtime.GOMAXPROCS(1)
    defer runtime.GOMAXPROCS(runtime.NumCPU())

    type result struct {
        name    string
        counts  [3]int64
    }

    runExperiment := func(name string, workFn func(id int, counter *int64, stop <-chan struct{})) result {
        var counters [3]int64
        stop := make(chan struct{})

        for i := 0; i < 3; i++ {
            go workFn(i, &counters[i], stop)
        }

        time.Sleep(100 * time.Millisecond)
        close(stop)
        time.Sleep(10 * time.Millisecond)

        return result{name: name, counts: counters}
    }

    // Experiment 1: tight loop (relies on async preemption)
    r1 := runExperiment("tight loop", func(id int, counter *int64, stop <-chan struct{}) {
        for {
            select {
            case <-stop:
                return
            default:
                atomic.AddInt64(counter, 1)
            }
        }
    })

    // Experiment 2: with Gosched
    r2 := runExperiment("with Gosched", func(id int, counter *int64, stop <-chan struct{}) {
        for {
            select {
            case <-stop:
                return
            default:
                atomic.AddInt64(counter, 1)
                if atomic.LoadInt64(counter)%1000 == 0 {
                    runtime.Gosched()
                }
            }
        }
    })

    // Experiment 3: with channel operation
    r3 := runExperiment("with channel", func(id int, counter *int64, stop <-chan struct{}) {
        ch := make(chan struct{}, 1)
        ch <- struct{}{}
        for {
            select {
            case <-stop:
                return
            default:
                <-ch
                atomic.AddInt64(counter, 1)
                ch <- struct{}{}
            }
        }
    })

    fmt.Printf("%-18s %15s %15s %15s %15s\n", "Pattern", "Worker 0", "Worker 1", "Worker 2", "Fairness")
    fmt.Println(strings.Repeat("-", 82))

    for _, r := range []result{r1, r2, r3} {
        total := r.counts[0] + r.counts[1] + r.counts[2]
        var fairness float64
        if total > 0 {
            // Perfect fairness = 1.0 (equal distribution)
            avg := float64(total) / 3.0
            variance := 0.0
            for _, c := range r.counts {
                diff := float64(c) - avg
                variance += diff * diff
            }
            fairness = 1.0 - (variance / (avg * avg * 3))
        }
        fmt.Printf("%-18s %15d %15d %15d %15.3f\n",
            r.name, r.counts[0], r.counts[1], r.counts[2], fairness)
    }
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected pattern: the tight loop shows the least fairness, `Gosched` shows better fairness, and the channel version shows the best fairness due to frequent scheduling points.

## Common Mistakes

### Relying on Gosched for Synchronization
**Wrong:**
```go
var data int

go func() {
    data = 42
    runtime.Gosched() // does NOT guarantee B sees data=42
}()

runtime.Gosched()
fmt.Println(data) // data race!
```

**What happens:** `Gosched` yields the processor but provides no memory ordering guarantees. This is a data race.

**Fix:** Use proper synchronization: channels, mutexes, or `sync/atomic`.

### Assuming Cooperative Scheduling Means Deterministic Order
**Wrong thinking:** "If I call Gosched after every iteration, goroutines will alternate perfectly."

**What happens:** `Gosched` puts the goroutine at the back of the run queue, but the scheduler may not pick the goroutine you expect next.

**Fix:** Never rely on execution order. Use explicit synchronization (channels, mutexes) when order matters.

### Adding Gosched Everywhere for "Performance"
**Wrong:**
```go
for i := 0; i < n; i++ {
    doWork(i)
    runtime.Gosched() // unnecessary and harmful
}
```

**What happens:** Each `Gosched` introduces overhead (context switch). In most code, natural scheduling points (I/O, channel ops) provide sufficient yielding.

**Fix:** Only use `Gosched` when you have a specific, measured scheduling problem. The compiler and runtime handle scheduling automatically in nearly all cases.

## Verify What You Learned

Create a program that:
1. Runs 4 CPU-intensive goroutines with GOMAXPROCS=1
2. Measures the fairness of execution time distribution with three strategies: no yielding, `Gosched` every N iterations, and using a channel ticker
3. Prints which strategy achieves the best balance of throughput and fairness

## What's Next
Continue to [07-goroutine-per-request](../07-goroutine-per-request/07-goroutine-per-request.md) to apply goroutines to a practical pattern: one goroutine per independent task.

## Summary
- Go's scheduler is primarily cooperative: goroutines yield at channel ops, syscalls, mutex locks, and function calls
- `runtime.Gosched()` explicitly yields the processor, placing the goroutine at the back of the run queue
- Go 1.14+ introduced async preemption: the runtime signals long-running goroutines to yield
- Async preemption prevents starvation but cooperative scheduling still produces smoother behavior
- `Gosched` is NOT a synchronization primitive -- it provides no memory ordering guarantees
- In practice, most code has enough natural scheduling points; explicit `Gosched` is rarely needed

## Reference
- [runtime.Gosched](https://pkg.go.dev/runtime#Gosched)
- [Go 1.14 Release Notes: Goroutine preemption](https://go.dev/doc/go1.14#runtime)
- [Proposal: Non-cooperative Goroutine Preemption](https://github.com/golang/proposal/blob/master/design/24543-non-cooperative-preemption.md)
