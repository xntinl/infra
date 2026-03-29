---
difficulty: advanced
concepts: [sync.Mutex, sync.RWMutex, sync/atomic, channels, benchmarking, tradeoffs]
tools: [go]
estimated_time: 40m
bloom_level: create
prerequisites: [sync.Mutex, sync.RWMutex, channels, goroutines, sync.WaitGroup]
---

# 10. Build a Thread-Safe Counter


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

The `Counter` interface defines the contract all implementations must satisfy:

```go
type Counter interface {
    Increment()
    Decrement()
    Value() int64
}
```

All four implementations satisfy this interface, enabling identical testing and benchmarking.

## Step 2 -- Mutex Counter

The simplest approach:

```go
package main

import (
	"fmt"
	"sync"
)

type Counter interface {
	Increment()
	Decrement()
	Value() int64
}

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

func main() {
	c := &MutexCounter{}
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Increment()
		}()
	}
	wg.Wait()
	fmt.Printf("Value: %d\n", c.Value())
}
```

Expected output:
```
Value: 1000
```

**Characteristics**: simple, correct, all operations serialized (including reads).

### Intermediate Verification
```bash
go run main.go
```
The mutex counter should report the correct value.

## Step 3 -- RWMutex Counter

Optimize reads with shared read lock:

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

**Characteristics**: reads can proceed concurrently, writes still exclusive. For a counter (write-heavy), this adds overhead tracking readers with no benefit.

## Step 4 -- Atomic Counter

Lock-free operations using CPU-level atomics:

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

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

func main() {
	c := &AtomicCounter{}
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Increment()
		}()
	}
	wg.Wait()
	fmt.Printf("Value: %d\n", c.Value())
}
```

Expected output:
```
Value: 1000
```

**Characteristics**: lock-free, highest throughput for simple counters, no deadlock possible. Limited to operations the CPU supports atomically (add, load, store, compare-and-swap).

## Step 5 -- Channel Counter

A single goroutine owns the state:

```go
package main

import (
	"fmt"
	"sync"
)

type counterOp struct {
	kind     string
	response chan int64
}

type ChannelCounter struct {
	ops  chan counterOp
	done chan struct{}
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

func main() {
	c := NewChannelCounter()
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Increment()
		}()
	}
	wg.Wait()
	fmt.Printf("Value: %d\n", c.Value())
	c.Close()
}
```

Expected output:
```
Value: 1000
```

**Characteristics**: no shared state, no locks, clear ownership model. Higher overhead per operation due to channel send/receive and goroutine scheduling.

## Step 6 -- Benchmark All Four

Run the full program to see correctness tests and benchmarks:

```bash
go run main.go
```

Expected output (times vary by machine):
```
=== Correctness Tests ===
[PASS] Mutex   : expected=100000, got=100000
[PASS] RWMutex : expected=100000, got=100000
[PASS] Atomic  : expected=100000, got=100000
[PASS] Channel : expected=100000, got=100000

=== Benchmarks (100 goroutines x 10,000 ops) ===
Mutex   : 15ms  (final value: 1010000)
RWMutex : 18ms  (final value: 1010000)
Atomic  : 3ms   (final value: 1010000)
Channel : 180ms (final value: 1010000)
```

All four report the same final value. Atomic is fastest. Channel is slowest.

## Step 7 -- Analyze the Tradeoffs

| Approach | Throughput | Complexity | Flexibility | Deadlock Risk |
|----------|-----------|------------|-------------|---------------|
| Mutex | Medium | Low | High | Possible |
| RWMutex | Medium-High | Low | High | Possible |
| Atomic | Highest | Lowest | Low | None |
| Channel | Lowest | Medium | Medium | Possible |

- **Atomic** wins for simple counters but cannot protect complex operations (e.g., "read two fields, modify one, write both").
- **Mutex** is the workhorse: correct, flexible, reasonable performance.
- **RWMutex** helps only when reads vastly outnumber writes. For a counter (write-heavy), it adds overhead with no benefit.
- **Channel** is the most flexible for complex state machines but has the highest per-operation cost for simple operations.

## Common Mistakes

### Using Atomic for Complex Operations

```go
var balance atomic.Int64
func transfer(amount int64) {
    if balance.Load() >= amount { // check
        balance.Add(-amount)      // act -- NOT atomic with the check!
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
