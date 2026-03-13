# 4. Cooperative vs Preemptive Scheduling

<!--
difficulty: advanced
concepts: [cooperative-scheduling, preemptive-scheduling, go-1.14-preemption, async-preemption, signal-based-preemption]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [gmp-model, gomaxprocs-processor-binding, work-stealing]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of the GMP model and work stealing from exercises 01-03
- Basic knowledge of OS signals (SIGURG)

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the difference between cooperative and preemptive scheduling in Go
- **Demonstrate** how tight loops without function calls behaved before Go 1.14
- **Analyze** how asynchronous preemption (signal-based) prevents goroutine starvation
- **Identify** code patterns that historically caused scheduling issues

## Why Cooperative vs Preemptive Matters

Before Go 1.14, the scheduler was purely cooperative: goroutines yielded control only at specific preemption points -- function calls, channel operations, system calls. A tight computational loop with no function calls could monopolize a P indefinitely, starving other goroutines. Go 1.14 introduced asynchronous preemption using OS signals (SIGURG on Unix), allowing the runtime to interrupt long-running goroutines at nearly any point. Understanding this history helps you write scheduler-friendly code and debug latency issues in older Go versions.

## Steps

### Step 1: Simulate Pre-1.14 Cooperative Scheduling

This loop has no function calls, which means no preemption points in a cooperative model. On modern Go (1.14+), it will still be preempted asynchronously:

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
	"time"
)

func tightLoop(iterations int) int {
	sum := 0
	for i := 0; i < iterations; i++ {
		sum += i * i
	}
	return sum
}

func demonstrateCooperativeIssue() {
	fmt.Println("=== Cooperative Scheduling Demonstration ===")
	runtime.GOMAXPROCS(1) // Single P to make starvation visible

	var wg sync.WaitGroup
	done := make(chan struct{})

	// Long-running CPU-bound goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		fmt.Println("  CPU-bound goroutine started")
		result := tightLoop(1_000_000_000)
		fmt.Printf("  CPU-bound goroutine finished: %d\n", result)
	}()

	// Short goroutine that should run concurrently
	wg.Add(1)
	go func() {
		defer wg.Done()
		start := time.Now()
		<-done // Wait for signal
		fmt.Printf("  Short goroutine unblocked after %v\n", time.Since(start))
	}()

	// Give goroutines time to start
	time.Sleep(10 * time.Millisecond)

	// Signal the short goroutine
	start := time.Now()
	close(done)
	fmt.Printf("  Signal sent at %v after start\n", time.Since(start))

	wg.Wait()
}
```

### Step 2: Show Preemption Points in Cooperative Mode

Demonstrate where the old cooperative scheduler would yield:

```go
func showPreemptionPoints() {
	fmt.Println("\n=== Preemption Points ===")
	runtime.GOMAXPROCS(1)

	var wg sync.WaitGroup
	timestamps := make([]time.Time, 0, 10)
	var mu sync.Mutex

	record := func(label string) {
		mu.Lock()
		timestamps = append(timestamps, time.Now())
		fmt.Printf("  [%s] at %v\n", label, time.Now().Format("15:04:05.000"))
		mu.Unlock()
	}

	// Goroutine with explicit preemption points
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			// Function calls are preemption points
			record(fmt.Sprintf("worker iteration %d", i))
			// Channel ops are preemption points
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// Another goroutine competing for the same P
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			record(fmt.Sprintf("competitor iteration %d", i))
			time.Sleep(1 * time.Millisecond)
		}
	}()

	wg.Wait()
}
```

### Step 3: Demonstrate Async Preemption (Go 1.14+)

Show that tight loops are now preemptible:

```go
func demonstrateAsyncPreemption() {
	fmt.Println("\n=== Async Preemption (Go 1.14+) ===")
	runtime.GOMAXPROCS(1)

	var wg sync.WaitGroup
	start := time.Now()

	// Launch a tight-loop goroutine (no function calls in the loop)
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Pure arithmetic -- no function calls, no allocations
		x := 0
		for i := 0; i < 2_000_000_000; i++ {
			x += i
		}
		_ = x
		fmt.Printf("  Tight loop finished after %v\n", time.Since(start))
	}()

	// This goroutine should still get scheduled thanks to async preemption
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond) // Yield immediately
		fmt.Printf("  Short goroutine ran at %v (should be ~50ms, not after tight loop)\n",
			time.Since(start))
	}()

	wg.Wait()
	fmt.Printf("  Total time: %v\n", time.Since(start))
}
```

### Step 4: Measure Scheduling Latency

Quantify how quickly goroutines get scheduled under different conditions:

```go
func measureSchedulingLatency() {
	fmt.Println("\n=== Scheduling Latency Measurement ===")

	for _, procs := range []int{1, 2, 4} {
		runtime.GOMAXPROCS(procs)
		var wg sync.WaitGroup

		latencies := make([]time.Duration, 100)
		for i := 0; i < 100; i++ {
			wg.Add(1)
			idx := i
			created := time.Now()
			go func() {
				defer wg.Done()
				latencies[idx] = time.Since(created)
				// Do some work
				sum := 0
				for j := 0; j < 10_000; j++ {
					sum += j
				}
				_ = sum
			}()
		}

		wg.Wait()

		var total time.Duration
		var max time.Duration
		for _, l := range latencies {
			total += l
			if l > max {
				max = l
			}
		}
		avg := total / time.Duration(len(latencies))
		fmt.Printf("  GOMAXPROCS=%d: avg=%v, max=%v\n", procs, avg, max)
	}
}

func main() {
	demonstrateCooperativeIssue()
	showPreemptionPoints()
	demonstrateAsyncPreemption()
	measureSchedulingLatency()
}
```

## Hints

- In Go 1.14+, the runtime sends SIGURG to the thread running a goroutine that needs preemption
- The signal handler sets a flag in the goroutine's stack that causes it to yield at the next safe point
- `runtime.Gosched()` is an explicit cooperative yield -- it is still useful for fairness hints
- You can disable async preemption with `GODEBUG=asyncpreemptoff=1` to observe the old behavior
- Preemption points in cooperative mode include: function calls, channel operations, `select`, `go` statements, memory allocation, and `runtime.Gosched()`

## Verification

```bash
go run main.go
```

Confirm that:
1. The short goroutine runs promptly even when a tight loop is active (async preemption)
2. Scheduling latency decreases with more GOMAXPROCS
3. Both worker and competitor goroutines interleave when using explicit preemption points

To see pre-1.14 behavior (goroutine starvation):

```bash
GODEBUG=asyncpreemptoff=1 go run main.go
```

With `asyncpreemptoff=1`, the tight-loop goroutine may delay other goroutines on the same P.

## What's Next

Continue to [05 - runtime.Gosched](../05-runtime-gosched/05-runtime-gosched.md) to explore explicit cooperative yielding and when it is useful even with preemptive scheduling.

## Summary

- Before Go 1.14, scheduling was cooperative: goroutines yielded only at function calls, channel ops, and other explicit points
- Tight loops without function calls could starve other goroutines on the same P
- Go 1.14 introduced asynchronous (signal-based) preemption using SIGURG
- Async preemption can interrupt goroutines at nearly any point, preventing starvation
- `GODEBUG=asyncpreemptoff=1` disables async preemption for testing/debugging
- Even with async preemption, writing scheduler-friendly code (avoiding unnecessarily tight loops) improves latency

## Reference

- [Go 1.14 Release Notes -- Goroutines are now asynchronously preemptible](https://go.dev/doc/go1.14#runtime)
- [Proposal: Non-cooperative goroutine preemption](https://go.dev/design/24543-non-cooperative-preemption)
- [runtime package](https://pkg.go.dev/runtime)
