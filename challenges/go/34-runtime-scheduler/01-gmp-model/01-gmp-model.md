# 1. GMP Model

<!--
difficulty: advanced
concepts: [goroutine-model, runtime-scheduler, gmp-architecture, os-threads, processor-abstraction]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [goroutines-and-channels, sync-primitives, concurrency-patterns]
-->

## Prerequisites

- Go 1.22+ installed
- Solid understanding of goroutines and channels
- Familiarity with OS threads and concurrency concepts

## Learning Objectives

After completing this exercise, you will be able to:

- **Analyze** the relationship between Goroutines (G), Machine threads (M), and Processors (P)
- **Explain** why Go uses a user-space scheduler instead of relying solely on OS threads
- **Demonstrate** GMP interactions through instrumented programs
- **Distinguish** between the roles of G, M, and P in scheduling decisions

## Why the GMP Model Matters

Go's runtime scheduler multiplexes potentially millions of goroutines onto a small number of OS threads. The GMP model -- Goroutine, Machine (OS thread), and Processor (logical CPU context) -- is the core abstraction that makes this possible. Understanding GMP helps you reason about goroutine scheduling, diagnose performance issues, and write code that cooperates with the scheduler rather than fighting it.

## The Problem

Build a program that demonstrates the GMP model in action by creating scenarios where you can observe the interplay between goroutines, OS threads, and processors. You will visualize how goroutines are scheduled, how threads are created, and how processors manage local run queues.

## Requirements

1. **Write a function `showRuntimeState`** that prints the current number of goroutines, OS threads, and GOMAXPROCS value:

```go
package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"
	"time"
)

func showRuntimeState(label string) {
	fmt.Printf("[%s] Goroutines: %d | GOMAXPROCS: %d | NumCPU: %d\n",
		label, runtime.NumGoroutine(), runtime.GOMAXPROCS(0), runtime.NumCPU())
}
```

2. **Write a function `demonstrateGoroutineMultiplexing`** that creates many goroutines (e.g., 1000) performing CPU-bound work and shows they run on fewer OS threads:

```go
func demonstrateGoroutineMultiplexing() {
	fmt.Println("\n=== Goroutine Multiplexing ===")
	showRuntimeState("before")

	var wg sync.WaitGroup
	const numGoroutines = 1000

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// CPU-bound work: compute sum
			sum := 0
			for j := 0; j < 100_000; j++ {
				sum += j
			}
			_ = sum
		}(i)
	}

	showRuntimeState("during")
	wg.Wait()
	showRuntimeState("after")
}
```

3. **Write a function `demonstrateThreadCreation`** that forces the runtime to create extra OS threads by performing blocking system calls:

```go
func demonstrateThreadCreation() {
	fmt.Println("\n=== Thread Creation on Blocking Calls ===")
	showRuntimeState("before-blocking")

	var wg sync.WaitGroup
	const numBlocking = 20

	for i := 0; i < numBlocking; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// Simulate a blocking syscall (sleep blocks the OS thread)
			time.Sleep(100 * time.Millisecond)
		}(i)
	}

	// Give the runtime a moment to create threads
	time.Sleep(50 * time.Millisecond)
	showRuntimeState("during-blocking")

	wg.Wait()
	showRuntimeState("after-blocking")
}
```

4. **Write a function `demonstrateProcessorQueues`** that shows how work distributes across P's by varying GOMAXPROCS:

```go
func demonstrateProcessorQueues() {
	fmt.Println("\n=== Processor Queue Distribution ===")

	for _, procs := range []int{1, 2, 4} {
		prev := runtime.GOMAXPROCS(procs)
		start := time.Now()

		var wg sync.WaitGroup
		const work = 100

		for i := 0; i < work; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sum := 0
				for j := 0; j < 1_000_000; j++ {
					sum += j
				}
				_ = sum
			}()
		}

		wg.Wait()
		elapsed := time.Since(start)
		fmt.Printf("  GOMAXPROCS=%d -> %v\n", procs, elapsed)
		runtime.GOMAXPROCS(prev)
	}
}
```

5. **Write a function `demonstrateGMPRelationship`** that uses `runtime.LockOSThread` to pin a goroutine to a specific thread, illustrating the G-M binding:

```go
func demonstrateGMPRelationship() {
	fmt.Println("\n=== G-M Binding with LockOSThread ===")

	var wg sync.WaitGroup

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()

			fmt.Printf("  Goroutine %d locked to OS thread\n", id)
			showRuntimeState(fmt.Sprintf("locked-g%d", id))

			// Do some work while locked
			sum := 0
			for j := 0; j < 1_000_000; j++ {
				sum += j
			}
			_ = sum
			fmt.Printf("  Goroutine %d releasing thread lock\n", id)
		}(i)
	}

	wg.Wait()
}
```

6. **Write a `main` function** that ties all demonstrations together and disables GC to reduce noise:

```go
func main() {
	debug.SetGCPercent(-1) // Disable GC for cleaner output
	defer debug.SetGCPercent(100)

	fmt.Println("Go Runtime GMP Model Demonstration")
	fmt.Println("===================================")

	demonstrateGoroutineMultiplexing()
	demonstrateThreadCreation()
	demonstrateProcessorQueues()
	demonstrateGMPRelationship()

	fmt.Println("\n=== Summary ===")
	fmt.Printf("G = Goroutine (lightweight, user-space, ~2KB initial stack)\n")
	fmt.Printf("M = Machine   (OS thread, created as needed, cached when idle)\n")
	fmt.Printf("P = Processor (logical CPU, owns a local run queue, count = GOMAXPROCS)\n")
	fmt.Printf("Each P has a local run queue of G's. M's pull G's from P's to execute.\n")
}
```

## Hints

- `runtime.NumGoroutine()` returns the current number of goroutines including the main goroutine
- `runtime.GOMAXPROCS(0)` returns the current value without changing it
- `runtime.LockOSThread()` wires the calling goroutine to its current OS thread -- other goroutines will not run on that thread
- When a goroutine makes a blocking syscall, the runtime detaches the P from the M so other goroutines can still run on a new M
- The number of M's (threads) can exceed GOMAXPROCS when goroutines are blocked in syscalls

## Verification

Run the program and verify:

```bash
go run main.go
```

1. During goroutine multiplexing, the goroutine count should spike well above the thread count
2. During blocking calls, observe that the runtime can report more goroutines than GOMAXPROCS
3. Increasing GOMAXPROCS should reduce elapsed time for CPU-bound parallel work
4. `LockOSThread` should show goroutines pinned to dedicated threads

## What's Next

Continue to [02 - GOMAXPROCS and Processor Binding](../02-gomaxprocs-processor-binding/02-gomaxprocs-processor-binding.md) to explore how GOMAXPROCS controls parallelism and how to tune it for different workloads.

## Summary

- **G** (Goroutine): a lightweight concurrent function with its own stack (~2-8KB), managed entirely by the Go runtime
- **M** (Machine): an OS thread that executes goroutines; created on demand and cached when idle
- **P** (Processor): a logical processor that owns a local run queue; the count equals GOMAXPROCS
- A goroutine runs when it is assigned to a P, and that P is attached to an M
- Blocking syscalls cause the runtime to detach P from M, allowing other goroutines to continue
- `runtime.LockOSThread` pins a goroutine to a specific OS thread

## Reference

- [Go Scheduler Design Document](https://docs.google.com/document/d/1TTj4T2JO42uD5ID9e89oa0sLKhJYD0Y_kqxDv3I3XMw)
- [runtime package](https://pkg.go.dev/runtime)
- [Scheduling in Go (Ardan Labs)](https://www.ardanlabs.com/blog/2018/08/scheduling-in-go-part1.html)
