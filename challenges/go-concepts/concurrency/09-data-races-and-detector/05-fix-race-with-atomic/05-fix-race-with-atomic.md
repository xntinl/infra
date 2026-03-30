---
difficulty: intermediate
concepts: [sync/atomic, atomic.Int64, lock-free, CAS, server stats]
tools: [go]
estimated_time: 25m
bloom_level: apply
---

# 5. Fix Race with Atomic


## Learning Objectives
After completing this exercise, you will be able to:
- **Fix** a data race on a simple counter using `sync/atomic` operations
- **Build** a server stats tracker using atomic types for multiple metrics
- **Choose** when atomic operations are the right tool (single scalar values, no complex invariants)
- **Compare** the three approaches (mutex, channel, atomic) for the same problem

## Why Atomic Operations

For simple numeric operations (counters, flags, gauges), `sync/atomic` provides **lock-free** alternatives to mutexes. Atomic operations are implemented directly by the CPU using special instructions that guarantee the operation completes without interruption from other cores.

Compared to mutexes:
- **Faster**: no lock acquisition/release overhead, no goroutine blocking
- **No deadlocks**: no locks to hold
- **Limited**: only works for simple scalar types (integers, pointers)

The rule of thumb: use `sync/atomic` when you need a single counter or flag. Use mutexes when you need to protect complex data structures or multi-field updates. Use channels when you need communication between goroutines.

In a real server, you track simple metrics: total requests served, bytes sent, active connections, errors. Each is a single integer that gets incremented or decremented. These are the perfect use case for atomics.

## Step 1 -- Fix the Hit Counter with Atomic

Replace the racy `hitCount++` with an atomic operation:

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

func atomicHitCounter() int64 {
	var hitCount atomic.Int64
	var wg sync.WaitGroup

	for handler := 0; handler < 100; handler++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for req := 0; req < 100; req++ {
				hitCount.Add(1)
			}
		}()
	}

	wg.Wait()
	return hitCount.Load()
}

func main() {
	result := atomicHitCounter()
	fmt.Printf("Hit count: %d (expected 10000)\n", result)
}
```

Key details:
- `atomic.Int64` is the modern Go type-safe wrapper (Go 1.19+)
- `.Add(1)` atomically increments the counter: the entire read-modify-write is a single CPU instruction
- `.Load()` atomically reads the final value
- No lock, no channel, no blocking: the fastest option for a simple counter

### Verification
```bash
go run -race main.go
```
Expected: 10000 with zero race warnings.

## Step 2 -- Build a Server Stats Tracker

Build a real server stats tracker using multiple atomic values. This is what production Go servers use to expose metrics at `/debug/vars` or to Prometheus:

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type ServerStats struct {
	TotalRequests     atomic.Int64
	TotalBytesSent    atomic.Int64
	ActiveConnections atomic.Int64
	TotalErrors       atomic.Int64
}

func (s *ServerStats) HandleRequest(bytesWritten int64, isError bool) {
	s.TotalRequests.Add(1)
	s.TotalBytesSent.Add(bytesWritten)
	if isError {
		s.TotalErrors.Add(1)
	}
}

func (s *ServerStats) ConnectionOpened() {
	s.ActiveConnections.Add(1)
}

func (s *ServerStats) ConnectionClosed() {
	s.ActiveConnections.Add(-1)
}

func (s *ServerStats) Report() string {
	return fmt.Sprintf(
		"requests=%d bytes_sent=%d active_conns=%d errors=%d",
		s.TotalRequests.Load(),
		s.TotalBytesSent.Load(),
		s.ActiveConnections.Load(),
		s.TotalErrors.Load(),
	)
}

func main() {
	stats := &ServerStats{}
	var wg sync.WaitGroup

	// Simulate 50 concurrent connections.
	for conn := 0; conn < 50; conn++ {
		wg.Add(1)
		go func(connID int) {
			defer wg.Done()
			stats.ConnectionOpened()
			defer stats.ConnectionClosed()

			// Each connection processes 100 requests.
			for req := 0; req < 100; req++ {
				isError := req%20 == 0 // 5% error rate
				bytesWritten := int64(256 + req%512)
				stats.HandleRequest(bytesWritten, isError)
			}
		}(conn)
	}

	// Print stats while requests are in flight.
	fmt.Println("=== Server Stats (live) ===")
	for i := 0; i < 3; i++ {
		time.Sleep(1 * time.Millisecond)
		fmt.Printf("  [snapshot] %s\n", stats.Report())
	}

	wg.Wait()

	fmt.Println()
	fmt.Println("=== Server Stats (final) ===")
	fmt.Printf("  %s\n", stats.Report())
	fmt.Println()
	fmt.Printf("  Expected: requests=5000, active_conns=0, errors=250\n")
	fmt.Printf("  (bytes_sent varies by request size)\n")
}
```

Key design points:
- Each metric is an independent `atomic.Int64`: no lock contention between unrelated counters
- `ActiveConnections` uses `.Add(-1)` for decrement: atomics handle both directions
- `Report()` reads all values atomically, though the snapshot is not transactional (each Load is independent)
- Live snapshots work safely because every read is atomic

### Verification
```bash
go run -race main.go
```
Expected: 5000 total requests, 0 active connections at the end, 250 errors, zero race warnings. The live snapshots show counters being updated in real time.

## Step 3 -- Grand Comparison of All Three Approaches

Compare mutex, channel, and atomic side by side on the same counter problem:

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

func benchMutex() (int, time.Duration) {
	counter := 0
	var mu sync.Mutex
	var wg sync.WaitGroup

	start := time.Now()
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10000; j++ {
				mu.Lock()
				counter++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return counter, time.Since(start)
}

func benchChannel() (int, time.Duration) {
	inc := make(chan struct{}, 256)
	done := make(chan int)

	go func() {
		counter := 0
		for range inc {
			counter++
		}
		done <- counter
	}()

	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10000; j++ {
				inc <- struct{}{}
			}
		}()
	}
	wg.Wait()
	close(inc)
	result := <-done
	return result, time.Since(start)
}

func benchAtomic() (int64, time.Duration) {
	var counter atomic.Int64
	var wg sync.WaitGroup

	start := time.Now()
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10000; j++ {
				counter.Add(1)
			}
		}()
	}
	wg.Wait()
	return counter.Load(), time.Since(start)
}

func main() {
	fmt.Println("=== Grand Comparison: 100 goroutines x 10000 increments ===")
	fmt.Println()

	mutexVal, mutexTime := benchMutex()
	chanVal, chanTime := benchChannel()
	atomicVal, atomicTime := benchAtomic()

	fmt.Printf("  Mutex:   %d in %v\n", mutexVal, mutexTime)
	fmt.Printf("  Channel: %d in %v\n", chanVal, chanTime)
	fmt.Printf("  Atomic:  %d in %v\n", atomicVal, atomicTime)
	fmt.Println()

	fmt.Println("Decision Guide:")
	fmt.Println("  atomic  -> single counter, flag, or gauge")
	fmt.Println("  mutex   -> map, struct, multi-field update")
	fmt.Println("  channel -> ownership transfer, pipeline, command pattern")
}
```

### Verification
```bash
go run main.go
```

Typical ordering: **atomic < mutex < channel** for simple counter operations.

| Approach | Speed | Complexity | Best For |
|----------|-------|------------|----------|
| `atomic` | Fastest | Simple types only | Counters, flags, single values |
| `mutex` | Medium | Any type | Complex structs, multi-field updates |
| `channel` | Slowest | Communication | Ownership transfer, pipelines |

## Common Mistakes

### Using Regular Reads with Atomic Writes
```go
var counter atomic.Int64
counter.Add(1)           // atomic write
fmt.Println(counter)     // BUG: prints the struct, not the value
```
**Fix:** Always use `.Load()` to read: `fmt.Println(counter.Load())`.

### Using Atomic for Complex State
```go
var total atomic.Int64
var count atomic.Int64
total.Add(amount)
count.Add(1)
// BUG: another goroutine can read total and count between these two operations
// The average (total/count) may be calculated with mismatched values.
```
**Fix:** Use a mutex to protect multi-variable updates when the values must be consistent with each other.

### Thinking Atomic Operations Compose
Each atomic operation is individually atomic, but a **sequence** of atomic operations is NOT atomic as a whole:

```go
var counter atomic.Int64
val := counter.Load()    // step 1: atomic read
val++                    // step 2: local compute
counter.Store(val)       // step 3: atomic write
// RACE: another goroutine can modify counter between steps 1 and 3!

// USE THIS INSTEAD:
counter.Add(1) // single atomic operation
```

### Overusing Atomics for Readability
Code with many atomic operations scattered across a large struct can be harder to reason about than a single mutex. If you have more than 4-5 related atomic fields, consider whether a mutex with clear locking scope would be clearer.

## Verify What You Learned

1. Confirm zero race warnings for the atomic version with `go run -race main.go`
2. Why is `atomic.Int64` preferred over the older `atomic.AddInt64(&counter, 1)` style?
3. When would you choose `sync/atomic` over `sync.Mutex`?
4. Why is `counter.Store(counter.Load() + 1)` NOT equivalent to `counter.Add(1)`?

## What's Next
Continue to [06-subtle-race-map-access](../06-subtle-race-map-access/06-subtle-race-map-access.md) to explore a different kind of race: concurrent map access that causes a fatal crash.

## Summary
- `sync/atomic` provides lock-free operations for simple scalar types (integers, pointers)
- `atomic.Int64` is the modern, type-safe API (Go 1.19+): `.Add()`, `.Load()`, `.Store()`
- Atomic operations are the fastest option for simple counters, flags, and gauges
- Build server stats trackers with one `atomic.Int64` per metric for zero-contention updates
- Atomic operations do NOT compose: a sequence of atomic operations is not atomic as a whole
- **Decision**: atomic for single counters/flags, mutex for complex state, channels for communication

## Reference
- [sync/atomic Package](https://pkg.go.dev/sync/atomic)
- [Go Memory Model: Synchronization](https://go.dev/ref/mem#synchronization)
- [Go 1.19 Release Notes: atomic types](https://go.dev/doc/go1.19#atomic_types)
