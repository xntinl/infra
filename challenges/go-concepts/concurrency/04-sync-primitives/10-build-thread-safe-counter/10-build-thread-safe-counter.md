# 10. Build a Thread-Safe Counter

<!--
difficulty: advanced
concepts: [sync.Mutex, sync.RWMutex, sync/atomic, channels, benchmarking, tradeoffs]
tools: [go]
estimated_time: 40m
bloom_level: create
prerequisites: [sync.Mutex, sync.RWMutex, channels, goroutines, sync.WaitGroup]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01-09 in this section
- Understanding of `sync.Mutex`, `sync.RWMutex`, channels, and `sync/atomic`

## Learning Objectives
After completing this exercise, you will be able to:
- **Implement** the same thread-safe counter using four different approaches
- **Benchmark** each approach and compare throughput
- **Analyze** the tradeoffs: correctness, performance, code clarity, flexibility
- **Choose** the right synchronization mechanism for a given use case

## Why This Integration Exercise
Throughout this section you have learned individual sync primitives in isolation. Real-world systems require choosing between them based on the specific access pattern, performance requirements, and code clarity. This exercise forces you to implement the same interface four ways, benchmark them under identical conditions, and reason about when each approach is appropriate.

The four approaches:
1. **sync.Mutex**: exclusive locking for all operations
2. **sync.RWMutex**: shared read lock, exclusive write lock
3. **sync/atomic**: lock-free atomic operations
4. **Channel**: single goroutine owns the state

Each has different strengths. By implementing and measuring all four, you develop the intuition to choose wisely in production code.

## Step 1 -- Define the Counter Interface

Open `main.go`. The `Counter` interface defines the contract:

```go
type Counter interface {
    Increment()
    Decrement()
    Value() int64
}
```

All four implementations must satisfy this interface.

### Intermediate Verification
Verify the interface is defined in `main.go`. Each implementation will be tested against it.

## Step 2 -- Mutex Counter

Implement the simplest approach with `sync.Mutex`:

```go
type MutexCounter struct {
    mu    sync.Mutex
    value int64
}

func (c *MutexCounter) Increment() {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.value++
}

func (c *MutexCounter) Decrement() {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.value--
}

func (c *MutexCounter) Value() int64 {
    c.mu.Lock()
    defer c.mu.Unlock()
    return c.value
}
```

**Characteristics**: simple, correct, all operations serialized (including reads).

### Intermediate Verification
```bash
go run main.go
```
The mutex counter should report the correct value after concurrent operations.

## Step 3 -- RWMutex Counter

Optimize reads with `sync.RWMutex`:

```go
type RWMutexCounter struct {
    mu    sync.RWMutex
    value int64
}

func (c *RWMutexCounter) Increment() {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.value++
}

func (c *RWMutexCounter) Decrement() {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.value--
}

func (c *RWMutexCounter) Value() int64 {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.value
}
```

**Characteristics**: reads can proceed concurrently, writes still exclusive. Benefits when reads vastly outnumber writes.

### Intermediate Verification
```bash
go run main.go
```
The RWMutex counter should match the mutex counter result.

## Step 4 -- Atomic Counter

Use `sync/atomic` for lock-free operations:

```go
type AtomicCounter struct {
    value atomic.Int64
}

func (c *AtomicCounter) Increment() {
    c.value.Add(1)
}

func (c *AtomicCounter) Decrement() {
    c.value.Add(-1)
}

func (c *AtomicCounter) Value() int64 {
    return c.value.Load()
}
```

**Characteristics**: lock-free, highest throughput for simple counters, no deadlock possible. Limited to operations the CPU supports atomically.

### Intermediate Verification
```bash
go run main.go
```
The atomic counter should match the others.

## Step 5 -- Channel Counter

Use a channel with a single owner goroutine:

```go
type ChannelCounter struct {
    ops   chan counterOp
    done  chan struct{}
}

type counterOp struct {
    kind     string // "inc", "dec", "val"
    response chan int64
}

func NewChannelCounter() *ChannelCounter {
    c := &ChannelCounter{
        ops:  make(chan counterOp),
        done: make(chan struct{}),
    }
    go c.run()
    return c
}

func (c *ChannelCounter) run() {
    var value int64
    for op := range c.ops {
        switch op.kind {
        case "inc":
            value++
            if op.response != nil {
                op.response <- value
            }
        case "dec":
            value--
            if op.response != nil {
                op.response <- value
            }
        case "val":
            op.response <- value
        }
    }
    close(c.done)
}

func (c *ChannelCounter) Increment() {
    c.ops <- counterOp{kind: "inc"}
}

func (c *ChannelCounter) Decrement() {
    c.ops <- counterOp{kind: "dec"}
}

func (c *ChannelCounter) Value() int64 {
    resp := make(chan int64)
    c.ops <- counterOp{kind: "val", response: resp}
    return <-resp
}

func (c *ChannelCounter) Close() {
    close(c.ops)
    <-c.done
}
```

**Characteristics**: no shared state, no locks, clear ownership model. Higher overhead per operation due to channel send/receive.

### Intermediate Verification
```bash
go run main.go
```
The channel counter should match all others.

## Step 6 -- Benchmark All Four

Implement `benchmarkCounter` and run all benchmarks:

```go
func benchmarkCounter(name string, c Counter, goroutines, opsPerGoroutine int) time.Duration {
    var wg sync.WaitGroup
    start := time.Now()

    for i := 0; i < goroutines; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for j := 0; j < opsPerGoroutine; j++ {
                c.Increment()
                if j%10 == 0 {
                    c.Value() // occasional read
                }
            }
        }()
    }

    wg.Wait()
    return time.Since(start)
}

func runBenchmarks() {
    fmt.Println("\n=== Benchmarks ===")
    const goroutines = 100
    const ops = 10000

    counters := []struct {
        name    string
        counter Counter
        cleanup func()
    }{
        {"Mutex", &MutexCounter{}, nil},
        {"RWMutex", &RWMutexCounter{}, nil},
        {"Atomic", &AtomicCounter{}, nil},
        {"Channel", NewChannelCounter(), nil},
    }
    // Set cleanup for channel counter
    counters[3].cleanup = func() { counters[3].counter.(*ChannelCounter).Close() }

    for _, tc := range counters {
        duration := benchmarkCounter(tc.name, tc.counter, goroutines, ops)
        finalValue := tc.counter.Value()
        fmt.Printf("%-10s: %v (final value: %d)\n", tc.name, duration.Round(time.Microsecond), finalValue)
        if tc.cleanup != nil {
            tc.cleanup()
        }
    }
}
```

### Intermediate Verification
```bash
go run main.go
```
All four should report the same final value (100 * 10000 = 1,000,000). Execution times will differ.

## Step 7 -- Analyze the Tradeoffs

After running the benchmarks, reflect on these tradeoffs:

| Approach | Throughput | Complexity | Flexibility | Deadlock Risk |
|----------|-----------|------------|-------------|---------------|
| Mutex | Medium | Low | High | Possible |
| RWMutex | Medium-High | Low | High | Possible |
| Atomic | Highest | Lowest | Low | None |
| Channel | Lowest | Medium | Medium | Possible |

- **Atomic** wins for simple counters but cannot protect complex operations (e.g., "read-modify-write on two fields").
- **Mutex** is the workhorse: correct, flexible, reasonable performance.
- **RWMutex** helps only when reads vastly outnumber writes. For a counter (write-heavy), it adds overhead with no benefit.
- **Channel** is the most flexible for complex state machines but has the highest per-operation cost for simple operations.

## Common Mistakes

### Using Atomic for Complex Operations
**Wrong:**
```go
var balance atomic.Int64
func transfer(amount int64) {
    if balance.Load() >= amount { // check
        balance.Add(-amount)      // act -- but another goroutine may have changed balance!
    }
}
```
**What happens:** The check-then-act is not atomic. Another goroutine can drain the balance between Load and Add.

**Fix:** Use `CompareAndSwap` in a loop, or switch to a mutex for the compound operation.

### RWMutex for Write-Heavy Workloads
Using `RWMutex` for a counter (mostly writes) adds overhead for read-lock tracking with no benefit. `Mutex` is simpler and equivalent or faster.

### Forgetting to Close Channel Counter
The owner goroutine leaks if you forget to close the ops channel. Always provide and call a `Close` method.

## Verify What You Learned

Extend the counter to support an `IncrementBy(n int64)` operation and add a fifth implementation using `sync.Map` (store the count under a fixed key). Benchmark all five and write a one-paragraph recommendation for "which counter to use when."

## What's Next
You have completed the sync primitives section. Continue to [05-atomic-and-memory-ordering](../../05-atomic-and-memory-ordering/) to learn about lock-free programming with the `sync/atomic` package.

## Summary
- Four approaches to thread-safe state: Mutex, RWMutex, atomic, channels
- Atomic operations offer the best throughput for simple, CPU-supported operations
- Mutex is the default choice: correct, flexible, well-understood
- RWMutex helps only for read-heavy workloads; for write-heavy work, Mutex is equivalent
- Channels provide the clearest ownership model but have the highest per-operation overhead
- Always benchmark with your actual workload -- intuition about performance is often wrong
- Choose based on: operation complexity, read/write ratio, code clarity, and team familiarity

## Reference
- [sync package documentation](https://pkg.go.dev/sync)
- [sync/atomic package documentation](https://pkg.go.dev/sync/atomic)
- [Go Blog: Share Memory by Communicating](https://go.dev/blog/codelab-share)
- [Go Wiki: MutexOrChannel](https://go.dev/wiki/MutexOrChannel)
