# 3. Work Stealing

<!--
difficulty: advanced
concepts: [work-stealing, local-run-queue, global-run-queue, load-balancing, scheduler-fairness]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [gmp-model, gomaxprocs-processor-binding, goroutines-and-channels]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of the GMP model and GOMAXPROCS from exercises 01-02
- Familiarity with concurrent programming patterns

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** how the Go scheduler distributes goroutines across local and global run queues
- **Analyze** work-stealing behavior when processor load is imbalanced
- **Demonstrate** scenarios that trigger work stealing between processors
- **Measure** the impact of work distribution on throughput

## Why Work Stealing Matters

When goroutines are created, they are placed on the creating P's local run queue. Without work stealing, a P that finishes its queue would sit idle while other P's remain overloaded. The Go scheduler solves this by allowing idle P's to steal goroutines from busy P's local queues -- taking roughly half the pending work. This mechanism is what gives Go's scheduler its excellent load-balancing characteristics without requiring explicit work distribution by the programmer.

## Steps

### Step 1: Observe Imbalanced Work Creation

Build a program where a single goroutine spawns all the work, placing it on one P's local queue:

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	runtime.GOMAXPROCS(4)
	fmt.Printf("GOMAXPROCS: %d\n", runtime.GOMAXPROCS(0))

	// All goroutines created from main goroutine -> all initially
	// enqueued on main's P local queue. Work stealing distributes them.
	const numTasks = 10000
	var completed atomic.Int64
	var wg sync.WaitGroup

	start := time.Now()
	for i := 0; i < numTasks; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sum := 0
			for j := 0; j < 100_000; j++ {
				sum += j
			}
			_ = sum
			completed.Add(1)
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)
	fmt.Printf("Completed %d tasks in %v\n", completed.Load(), elapsed)
	fmt.Printf("Throughput: %.0f tasks/sec\n", float64(completed.Load())/elapsed.Seconds())
}
```

### Step 2: Demonstrate Per-P Work Creation

Compare throughput when work is created from multiple goroutines spread across P's:

```go
func distributedCreation(numTasks, numCreators int) time.Duration {
	var wg sync.WaitGroup
	start := time.Now()

	tasksPerCreator := numTasks / numCreators
	for c := 0; c < numCreators; c++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var inner sync.WaitGroup
			for i := 0; i < tasksPerCreator; i++ {
				inner.Add(1)
				go func() {
					defer inner.Done()
					sum := 0
					for j := 0; j < 100_000; j++ {
						sum += j
					}
					_ = sum
				}()
			}
			inner.Wait()
		}()
	}

	wg.Wait()
	return time.Since(start)
}
```

### Step 3: Observe Local Queue Overflow to Global Queue

The local run queue has a fixed size of 256. When it overflows, half the goroutines are moved to the global run queue:

```go
func demonstrateGlobalQueue() {
	fmt.Println("\n=== Global Queue Overflow ===")
	runtime.GOMAXPROCS(1) // Single P to force local queue pressure

	var wg sync.WaitGroup
	const burst = 500 // More than local queue capacity (256)

	start := time.Now()
	for i := 0; i < burst; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// Minimal work so we can observe scheduling behavior
			runtime.Gosched()
		}(i)
	}

	wg.Wait()
	fmt.Printf("Burst of %d goroutines completed in %v\n", burst, time.Since(start))
	fmt.Println("Goroutines beyond 256 spilled to the global run queue")
}
```

### Step 4: Measure Work-Stealing Impact

Compare single-P creation vs distributed creation:

```go
func compareDistribution() {
	fmt.Println("\n=== Work Distribution Comparison ===")
	runtime.GOMAXPROCS(4)
	const totalTasks = 8000

	// All work created from one goroutine (requires stealing)
	single := distributedCreation(totalTasks, 1)
	fmt.Printf("Single creator:       %v\n", single)

	// Work created from 4 goroutines (one per P, less stealing needed)
	distributed := distributedCreation(totalTasks, 4)
	fmt.Printf("4 creators (per-P):   %v\n", distributed)

	// Work created from 8 goroutines
	over := distributedCreation(totalTasks, 8)
	fmt.Printf("8 creators:           %v\n", over)
}
```

### Step 5: Visualize Stealing with Scheduler Trace

Run your program with the scheduler trace enabled to see stealing events:

```bash
GODEBUG=schedtrace=100 go run main.go
```

The output will show lines like:

```
SCHED 100ms: gomaxprocs=4 idleprocs=0 threads=5 runqueue=12 [34 28 31 29]
```

The numbers in brackets are the local run queue lengths for each P. The `runqueue` value is the global queue length. When you see `idleprocs=0` and varying local queue sizes, work stealing is actively rebalancing.

## Hints

- The local run queue per P holds up to 256 goroutines; overflow goes to the global queue
- An idle P first checks the global queue (every 61 scheduling ticks to prevent starvation), then tries to steal from other P's local queues
- When stealing, a P takes roughly half the victim's local queue
- `GODEBUG=schedtrace=100` prints scheduler state every 100ms
- Creating goroutines from multiple parent goroutines naturally distributes them across P's

## Verification

```bash
go run main.go
```

Confirm that:
1. Both single-creator and multi-creator complete all tasks
2. Throughput numbers are comparable (work stealing keeps things balanced)
3. With `GODEBUG=schedtrace=100`, you see non-zero values in local queue brackets

```bash
GODEBUG=schedtrace=100 go run main.go 2>&1 | head -20
```

## What's Next

Continue to [04 - Cooperative vs Preemptive](../04-cooperative-vs-preemptive/04-cooperative-vs-preemptive.md) to understand how Go transitioned from cooperative to preemptive scheduling in Go 1.14.

## Summary

- Each P has a local run queue (capacity 256) and shares access to a global run queue
- Goroutines are initially placed on the creating P's local queue
- When a local queue overflows, half the goroutines move to the global queue
- Idle P's steal roughly half the goroutines from another P's local queue
- The global queue is checked periodically (every 61 ticks) to prevent starvation
- Work stealing ensures load balancing without programmer intervention

## Reference

- [Go Scheduler Design Document](https://docs.google.com/document/d/1TTj4T2JO42uD5ID9e89oa0sLKhJYD0Y_kqxDv3I3XMw)
- [runtime package -- GODEBUG](https://pkg.go.dev/runtime#hdr-Environment_Variables)
- [Scheduling in Go Part II: Go Scheduler (Ardan Labs)](https://www.ardanlabs.com/blog/2018/08/scheduling-in-go-part2.html)
