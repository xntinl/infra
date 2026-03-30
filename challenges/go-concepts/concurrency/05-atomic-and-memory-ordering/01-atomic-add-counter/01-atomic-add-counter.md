---
difficulty: basic
concepts: [sync/atomic, AddInt64, atomic.Int64, data race, lock-free counters]
tools: [go]
estimated_time: 25m
bloom_level: apply
---

# 1. Atomic Add Counter

## Learning Objectives
After completing this exercise, you will be able to:
- **Identify** why non-atomic increments produce incorrect request metrics under concurrency
- **Fix** data races in web server counters using `atomic.AddInt64` and `atomic.Int64`
- **Track** multiple independent metrics (requests, bytes, errors) with lock-free counters
- **Compare** atomic vs mutex performance with benchmarks and explain when each wins

## Why Atomic Operations for Request Metrics

Every production web server needs request metrics: total requests handled, bytes transferred, errors encountered. These counters are incremented by every handler goroutine on every request. A busy server handles thousands of requests per second across dozens of goroutines.

A simple `counter++` compiles to three operations -- load, add, store. When two goroutines execute this simultaneously, both may load the same value, both add 1, and both store the same result. One increment is lost. At 10,000 requests per second, you lose hundreds of increments per second. Your monitoring dashboards show fewer requests than actually occurred, your error rates look artificially low, and your capacity planning is based on lies.

`sync/atomic` provides functions that perform read-modify-write as a single, indivisible CPU instruction. No goroutine can observe an intermediate state. For simple counters, atomics are faster than mutexes because they avoid the overhead of lock acquisition and goroutine parking.

## Step 1 -- Observe Lost Request Counts Without Atomics

Simulate a web server where 100 handler goroutines each process 1,000 requests. Each handler increments a shared request counter. Without atomic operations, the final count is wrong:

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var totalRequests int64
	var totalBytes int64
	var totalErrors int64
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for req := 0; req < 1000; req++ {
				totalRequests++ // BUG: load-modify-store, three separate operations
				totalBytes += 256
				if req%50 == 0 {
					totalErrors++
				}
			}
		}()
	}

	wg.Wait()
	fmt.Println("=== Request Metrics (BROKEN - no synchronization) ===")
	fmt.Printf("Total requests: %d (expected 100000)\n", totalRequests)
	fmt.Printf("Total bytes:    %d (expected 25600000)\n", totalBytes)
	fmt.Printf("Total errors:   %d (expected 2000)\n", totalErrors)
}
```

### Verification
```bash
go run main.go
```
Run it several times. The counts vary and never reach the expected values. Confirm the data race:
```bash
go run -race main.go
```
The race detector reports `DATA RACE` warnings pointing to the `counter++` lines.

## Step 2 -- Fix with atomic.AddInt64

Replace every `counter++` with `atomic.AddInt64`. The entire read-add-store happens as one CPU instruction (e.g., `LOCK XADD` on x86). No goroutine can see a half-updated value:

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

func main() {
	var totalRequests int64
	var totalBytes int64
	var totalErrors int64
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for req := 0; req < 1000; req++ {
				atomic.AddInt64(&totalRequests, 1)
				atomic.AddInt64(&totalBytes, 256)
				if req%50 == 0 {
					atomic.AddInt64(&totalErrors, 1)
				}
			}
		}()
	}

	wg.Wait()
	fmt.Println("=== Request Metrics (FIXED - atomic operations) ===")
	fmt.Printf("Total requests: %d (expected 100000)\n", totalRequests)
	fmt.Printf("Total bytes:    %d (expected 25600000)\n", totalBytes)
	fmt.Printf("Total errors:   %d (expected 2000)\n", totalErrors)
}
```

### Verification
```bash
go run -race main.go
```
All counts are exact every run. No race warnings.

## Step 3 -- Use Typed atomic.Int64 for a Metrics Struct

Go 1.19 introduced typed wrappers like `atomic.Int64`. These are method-based and harder to misuse because the underlying value is unexported -- you cannot accidentally access it non-atomically. Build a proper metrics collector:

```go
package main

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

type RequestMetrics struct {
	TotalRequests atomic.Int64
	TotalBytes    atomic.Int64
	TotalErrors   atomic.Int64
	ActiveConns   atomic.Int64
}

func (m *RequestMetrics) RecordRequest(bytes int64, isError bool) {
	m.TotalRequests.Add(1)
	m.TotalBytes.Add(bytes)
	if isError {
		m.TotalErrors.Add(1)
	}
}

func (m *RequestMetrics) ConnOpen()  { m.ActiveConns.Add(1) }
func (m *RequestMetrics) ConnClose() { m.ActiveConns.Add(-1) }

func (m *RequestMetrics) Snapshot() string {
	return fmt.Sprintf(
		"requests=%d bytes=%d errors=%d active_conns=%d",
		m.TotalRequests.Load(),
		m.TotalBytes.Load(),
		m.TotalErrors.Load(),
		m.ActiveConns.Load(),
	)
}

func main() {
	metrics := &RequestMetrics{}
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(handlerID int) {
			defer wg.Done()

			metrics.ConnOpen()
			defer metrics.ConnClose()

			for req := 0; req < 200; req++ {
				bytes := int64(64 + rand.Intn(4096))
				isError := rand.Intn(100) < 5
				metrics.RecordRequest(bytes, isError)
				time.Sleep(time.Microsecond)
			}
		}(i)
	}

	// Periodic reporting while handlers run
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case <-time.After(10 * time.Millisecond):
				fmt.Printf("[live] %s\n", metrics.Snapshot())
			}
		}
	}()

	wg.Wait()
	close(done)

	fmt.Printf("\n[final] %s\n", metrics.Snapshot())
	fmt.Printf("Expected total requests: 10000\n")
}
```

### Verification
```bash
go run -race main.go
```
Live metrics update while handlers run. Final request count is exactly 10,000. Active connections end at 0. No race warnings.

## Step 4 -- Benchmark: Atomic vs Mutex Counters

Measure the real performance difference. This program runs both approaches with the same workload and reports elapsed time:

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

func benchmarkAtomic(goroutines, iterations int) time.Duration {
	var counter atomic.Int64
	var wg sync.WaitGroup

	start := time.Now()
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				counter.Add(1)
			}
		}()
	}
	wg.Wait()
	return time.Since(start)
}

func benchmarkMutex(goroutines, iterations int) time.Duration {
	var mu sync.Mutex
	var counter int64
	var wg sync.WaitGroup

	start := time.Now()
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				mu.Lock()
				counter++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return time.Since(start)
}

func main() {
	scenarios := []struct {
		name       string
		goroutines int
		iterations int
	}{
		{"Low contention (4 goroutines)", 4, 100000},
		{"Medium contention (64 goroutines)", 64, 10000},
		{"High contention (1000 goroutines)", 1000, 1000},
	}

	fmt.Println("=== Atomic vs Mutex Counter Benchmark ===")
	fmt.Println()
	for _, s := range scenarios {
		atomicTime := benchmarkAtomic(s.goroutines, s.iterations)
		mutexTime := benchmarkMutex(s.goroutines, s.iterations)
		ratio := float64(mutexTime) / float64(atomicTime)

		fmt.Printf("%s:\n", s.name)
		fmt.Printf("  Atomic: %v\n", atomicTime)
		fmt.Printf("  Mutex:  %v\n", mutexTime)
		fmt.Printf("  Mutex/Atomic ratio: %.2fx\n\n", ratio)
	}
}
```

### Verification
```bash
go run main.go
```
Under all contention levels, atomic is faster for simple counter increments. The gap widens under high contention because mutex must park and wake goroutines while atomic uses a single CPU instruction.

## Intermediate Verification

Run the race detector on each step to confirm correctness:
```bash
go run -race main.go
```
All versions except Step 1 should produce zero race warnings and exact expected counts.

## Common Mistakes

### Mixing Atomic and Non-Atomic Access on the Same Variable

**Wrong:**
```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

func main() {
	var requests int64
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			atomic.AddInt64(&requests, 1)
		}()
	}

	wg.Wait()
	fmt.Println(requests) // BUG: direct read while other goroutines may use atomic writes
}
```

**What happens:** Reading `requests` directly is a data race. ALL access must be atomic if ANY access is atomic.

**Fix:** Use `atomic.LoadInt64(&requests)` to read, or in this specific case the read is safe only because `wg.Wait()` guarantees all writers finished. The rule: after full synchronization (WaitGroup, channel), a direct read is safe. Before that, always use atomic reads.

### Copying an atomic.Int64

**Wrong:**
```go
package main

import (
	"fmt"
	"sync/atomic"
)

func main() {
	var a atomic.Int64
	a.Store(42)
	b := a // BUG: copies the internal state — undefined behavior
	fmt.Println(b.Load())
}
```

**What happens:** `atomic.Int64` contains internal state that must not be copied. The compiler may warn, and the behavior is undefined.

**Fix:** Always use pointers to atomic types, or embed them in structs that are never copied.

### Using the Wrong Address with atomic.AddInt64

**Wrong:**
```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

func main() {
	var counter int64
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := counter           // copies value into local variable
			atomic.AddInt64(&c, 1) // increments LOCAL copy
		}()
	}
	wg.Wait()
	fmt.Printf("Expected: 100, Got: %d\n", counter) // always 0
}
```

**Fix:** Always pass the address of the original shared variable: `atomic.AddInt64(&counter, 1)`.

## Verify What You Learned

1. Why does `counter++` produce wrong results when called from multiple goroutines?
2. What CPU instruction does `atomic.AddInt64` compile to on x86?
3. When should you prefer `atomic.Int64` (Go 1.19+) over `atomic.AddInt64`?
4. If atomic is always faster for counters, why does `sync.Mutex` exist at all?

## What's Next
Continue to [02-atomic-load-store](../02-atomic-load-store/02-atomic-load-store.md) to build a feature flag system using atomic load and store operations for safe cross-goroutine visibility.

## Summary
- Non-atomic `counter++` is three operations (load-modify-store) that interleave under concurrency, losing increments
- `atomic.AddInt64(&counter, delta)` performs the increment as one indivisible CPU instruction
- `atomic.Int64` (Go 1.19+) is the preferred typed wrapper -- method-based, unexported internals prevent accidental non-atomic access
- For web server metrics (requests, bytes, errors), atomic counters are the right tool: lock-free, fast, zero allocation
- Atomic counters outperform mutex-protected counters by 2-10x depending on contention level
- ALL access to a shared variable must be atomic if ANY access is atomic -- no mixing
- Atomic add is ideal for independent counters; for multi-field state that must update together, use a mutex

## Reference
- [sync/atomic package](https://pkg.go.dev/sync/atomic)
- [atomic.Int64 type](https://pkg.go.dev/sync/atomic#Int64)
- [Go Race Detector](https://go.dev/doc/articles/race_detector)
