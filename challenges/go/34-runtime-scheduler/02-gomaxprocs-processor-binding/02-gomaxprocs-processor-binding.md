# 2. GOMAXPROCS and Processor Binding

<!--
difficulty: advanced
concepts: [gomaxprocs, cpu-binding, processor-affinity, parallelism-control, runtime-tuning]
tools: [go]
estimated_time: 25m
bloom_level: analyze
prerequisites: [gmp-model, goroutines-and-channels, concurrency-patterns]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of the GMP model from the previous exercise
- Familiarity with CPU-bound vs I/O-bound workloads

## Learning Objectives

After completing this exercise, you will be able to:

- **Analyze** the impact of GOMAXPROCS on CPU-bound and I/O-bound workloads
- **Differentiate** between concurrency (goroutines) and parallelism (GOMAXPROCS)
- **Measure** performance characteristics under different GOMAXPROCS settings
- **Explain** when and why to override the default GOMAXPROCS value

## Why GOMAXPROCS Matters

GOMAXPROCS controls how many OS threads can execute goroutines simultaneously -- it sets the number of P's (processors) in the GMP model. Since Go 1.5, the default equals `runtime.NumCPU()`, which is usually correct. But understanding when to tune it -- container environments with CPU limits, I/O-heavy workloads, or latency-sensitive systems -- separates informed performance decisions from guesswork.

## The Problem

Build a benchmarking tool that measures how different GOMAXPROCS values affect CPU-bound and I/O-bound workloads, and demonstrate processor binding with `runtime.LockOSThread`.

## Requirements

1. **Implement a CPU-bound benchmark** that runs a fixed amount of computation with varying GOMAXPROCS:

```go
package main

import (
	"fmt"
	"math"
	"runtime"
	"sync"
	"time"
)

func cpuWork(iterations int) float64 {
	result := 0.0
	for i := 0; i < iterations; i++ {
		result += math.Sqrt(float64(i))
	}
	return result
}

func benchCPUBound(numGoroutines, gomaxprocs int) time.Duration {
	prev := runtime.GOMAXPROCS(gomaxprocs)
	defer runtime.GOMAXPROCS(prev)

	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cpuWork(1_000_000)
		}()
	}

	wg.Wait()
	return time.Since(start)
}
```

2. **Implement an I/O-bound benchmark** using simulated I/O with `time.Sleep`:

```go
func benchIOBound(numGoroutines, gomaxprocs int) time.Duration {
	prev := runtime.GOMAXPROCS(gomaxprocs)
	defer runtime.GOMAXPROCS(prev)

	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(10 * time.Millisecond)
		}()
	}

	wg.Wait()
	return time.Since(start)
}
```

3. **Implement a mixed workload benchmark** that combines CPU and I/O:

```go
func benchMixed(numGoroutines, gomaxprocs int) time.Duration {
	prev := runtime.GOMAXPROCS(gomaxprocs)
	defer runtime.GOMAXPROCS(prev)

	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if id%2 == 0 {
				cpuWork(500_000)
			} else {
				time.Sleep(5 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
	return time.Since(start)
}
```

4. **Demonstrate processor binding** showing that `LockOSThread` reserves a thread:

```go
func demonstrateProcessorBinding() {
	fmt.Println("\n=== Processor Binding ===")
	numCPU := runtime.NumCPU()
	runtime.GOMAXPROCS(numCPU)

	var wg sync.WaitGroup
	results := make([]string, numCPU)

	for i := 0; i < numCPU; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()

			start := time.Now()
			cpuWork(500_000)
			elapsed := time.Since(start)
			results[id] = fmt.Sprintf("  Thread %d: %v", id, elapsed)
		}(i)
	}

	wg.Wait()
	for _, r := range results {
		fmt.Println(r)
	}
}
```

5. **Write a `main` function** that runs all benchmarks and prints a comparison table:

```go
func main() {
	numCPU := runtime.NumCPU()
	fmt.Printf("NumCPU: %d\n", numCPU)

	procsValues := []int{1, 2, 4}
	if numCPU >= 8 {
		procsValues = append(procsValues, 8)
	}
	procsValues = append(procsValues, numCPU)

	numGoroutines := 50

	fmt.Println("\n=== CPU-Bound Workload ===")
	fmt.Printf("%-15s %s\n", "GOMAXPROCS", "Duration")
	for _, p := range procsValues {
		d := benchCPUBound(numGoroutines, p)
		fmt.Printf("%-15d %v\n", p, d)
	}

	fmt.Println("\n=== I/O-Bound Workload ===")
	fmt.Printf("%-15s %s\n", "GOMAXPROCS", "Duration")
	for _, p := range procsValues {
		d := benchIOBound(numGoroutines, p)
		fmt.Printf("%-15d %v\n", p, d)
	}

	fmt.Println("\n=== Mixed Workload ===")
	fmt.Printf("%-15s %s\n", "GOMAXPROCS", "Duration")
	for _, p := range procsValues {
		d := benchMixed(numGoroutines, p)
		fmt.Printf("%-15d %v\n", p, d)
	}

	demonstrateProcessorBinding()
}
```

## Hints

- CPU-bound work should show near-linear speedup as GOMAXPROCS increases up to `NumCPU`
- I/O-bound work shows minimal difference because goroutines are mostly sleeping, not consuming P's
- In container environments, `runtime.NumCPU()` may report the host CPU count, not the container's CPU quota -- the `automaxprocs` library from Uber addresses this
- `runtime.GOMAXPROCS(0)` reads the current value without changing it

## Verification

```bash
go run main.go
```

Confirm that:
1. CPU-bound duration decreases as GOMAXPROCS increases (up to NumCPU)
2. I/O-bound duration is roughly constant regardless of GOMAXPROCS
3. Mixed workload shows moderate improvement with more processors
4. Processor binding completes with each goroutine reporting its own timing

## What's Next

Continue to [03 - Work Stealing](../03-work-stealing/03-work-stealing.md) to learn how the scheduler balances work across processors using work-stealing queues.

## Summary

- `runtime.GOMAXPROCS(n)` sets the number of P's (processors) that can execute goroutines simultaneously
- The default since Go 1.5 is `runtime.NumCPU()`
- CPU-bound workloads benefit from higher GOMAXPROCS; I/O-bound workloads do not
- `runtime.LockOSThread()` pins a goroutine to its current OS thread, consuming one M permanently
- In containers, the reported NumCPU may not match the CPU quota -- use libraries like `automaxprocs` to correct this

## Reference

- [runtime.GOMAXPROCS](https://pkg.go.dev/runtime#GOMAXPROCS)
- [runtime.LockOSThread](https://pkg.go.dev/runtime#LockOSThread)
- [uber-go/automaxprocs](https://github.com/uber-go/automaxprocs)
