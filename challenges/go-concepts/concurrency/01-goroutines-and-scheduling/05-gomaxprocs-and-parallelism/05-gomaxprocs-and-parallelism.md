# 5. GOMAXPROCS and Parallelism

<!--
difficulty: intermediate
concepts: [GOMAXPROCS, concurrency vs parallelism, CPU-bound vs IO-bound, wall-clock time, benchmarking]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [01-launching-goroutines, 03-gmp-model-in-action]
-->

## Prerequisites
- Go 1.22+ installed
- Completed [03-gmp-model-in-action](../03-gmp-model-in-action/03-gmp-model-in-action.md)
- Understanding of GMP model basics

## Learning Objectives
After completing this exercise, you will be able to:
- **Differentiate** between concurrency and parallelism with concrete measurements
- **Measure** the impact of GOMAXPROCS on CPU-bound workloads
- **Demonstrate** that IO-bound work benefits minimally from additional Ps
- **Analyze** mixed workloads to understand intermediate speedup behavior

## Why GOMAXPROCS Matters

Concurrency is about structure; parallelism is about execution. A program is concurrent if it is structured as multiple independently executing tasks. A program is parallel if those tasks actually run at the same time on different CPU cores. Go makes concurrency easy with goroutines, but parallelism is controlled by `GOMAXPROCS`.

With `GOMAXPROCS=1`, all goroutines share a single logical processor. They are concurrent (they can make progress independently) but not parallel (only one runs at any given instant). Increasing GOMAXPROCS allows multiple goroutines to execute truly simultaneously on different cores.

The practical impact depends on the workload. CPU-bound work (computation, hashing, sorting) benefits enormously from parallelism because more Ps mean more work happening simultaneously. IO-bound work (network calls, disk reads, database queries) benefits less because goroutines spend most of their time waiting, not computing. Understanding this distinction is essential for tuning real Go applications.

## Step 1 -- Concurrency vs Parallelism Visualization

Run 4 CPU-bound workers under GOMAXPROCS=1 (concurrent only) and GOMAXPROCS=NumCPU (concurrent + parallel).

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
	"time"
)

func main() {
	work := func(id int) {
		start := time.Now()
		result := 0
		for i := 0; i < 50_000_000; i++ {
			result += i
		}
		elapsed := time.Since(start)
		fmt.Printf("  worker %d: %v (result: %d)\n", id, elapsed.Round(time.Millisecond), result%1000)
	}

	for _, procs := range []int{1, runtime.NumCPU()} {
		runtime.GOMAXPROCS(procs)
		fmt.Printf("\nGOMAXPROCS=%d:\n", procs)

		start := time.Now()
		var wg sync.WaitGroup

		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				work(id)
			}(i)
		}

		wg.Wait()
		fmt.Printf("  Total wall-clock: %v\n", time.Since(start).Round(time.Millisecond))
	}

	runtime.GOMAXPROCS(runtime.NumCPU())
}
```

**What's happening here:** Four workers each do ~45ms of CPU work. With GOMAXPROCS=1, they share one P, so they run one at a time: total is ~180ms. With GOMAXPROCS=NumCPU (e.g., 8), all four run simultaneously on different cores: total is ~48ms.

**Key insight:** The `go` keyword gives you concurrency. GOMAXPROCS gives you parallelism. Without multiple Ps, goroutines take turns.

**What would happen with GOMAXPROCS=2?** Two workers would run simultaneously, then the other two. Total would be ~90ms (2 batches of 2).

### Intermediate Verification
```bash
go run main.go
```
Expected output pattern:
```
GOMAXPROCS=1:
  worker 0: 45ms
  worker 1: 44ms
  worker 2: 45ms
  worker 3: 44ms
  Total wall-clock: 180ms

GOMAXPROCS=8:
  worker 0: 45ms
  worker 3: 46ms
  worker 1: 46ms
  worker 2: 47ms
  Total wall-clock: 48ms
```

## Step 2 -- CPU-Bound Benchmark

Measure speedup across different GOMAXPROCS values for pure CPU work.

```go
package main

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"
)

func main() {
	cpuWork := func() int {
		result := 0
		for i := 0; i < 100_000_000; i++ {
			result ^= i
		}
		return result
	}

	numWorkers := runtime.NumCPU()

	maxProcs := []int{1, 2, 4}
	if runtime.NumCPU() >= 8 {
		maxProcs = append(maxProcs, 8)
	}
	if runtime.NumCPU() >= 16 {
		maxProcs = append(maxProcs, 16)
	}

	fmt.Printf("Workers: %d (one per CPU)\n", numWorkers)
	fmt.Printf("%-12s %-15s %-10s\n", "GOMAXPROCS", "Wall-Clock", "Speedup")
	fmt.Println(strings.Repeat("-", 40))

	var baselineTime time.Duration

	for _, procs := range maxProcs {
		runtime.GOMAXPROCS(procs)

		start := time.Now()
		var wg sync.WaitGroup

		for i := 0; i < numWorkers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				cpuWork()
			}()
		}

		wg.Wait()
		elapsed := time.Since(start)

		if procs == 1 {
			baselineTime = elapsed
		}

		speedup := float64(baselineTime) / float64(elapsed)
		fmt.Printf("%-12d %-15v %-10.2fx\n", procs, elapsed.Round(time.Millisecond), speedup)
	}

	runtime.GOMAXPROCS(runtime.NumCPU())
}
```

**What's happening here:** Each worker does 100M XOR operations (pure CPU work). With GOMAXPROCS=1, all workers share one core. Doubling Ps roughly halves the wall-clock time because the work distributes across more cores.

**Key insight:** Speedup is roughly linear for CPU-bound work until you hit the physical core count. Beyond that, adding Ps provides no benefit because there are no more cores to use.

**What would happen with 2x NumCPU workers?** Wall-clock time would approximately double at GOMAXPROCS=1, but the speedup ratio at GOMAXPROCS=NumCPU would remain similar because each core just runs two workers sequentially.

### Intermediate Verification
```bash
go run main.go
```
Expected output pattern:
```
Workers: 8 (one per CPU)
GOMAXPROCS   Wall-Clock      Speedup
----------------------------------------
1            800ms           1.00x
2            410ms           1.95x
4            205ms           3.90x
8            105ms           7.62x
```

## Step 3 -- IO-Bound Comparison

Show that IO-bound work benefits minimally from additional Ps.

```go
package main

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"
)

func main() {
	ioWork := func() {
		time.Sleep(50 * time.Millisecond) // simulates IO: waiting, not computing
	}

	numWorkers := 20

	fmt.Printf("Workers: %d (IO-bound, 50ms sleep each)\n", numWorkers)
	fmt.Printf("%-12s %-15s %-10s\n", "GOMAXPROCS", "Wall-Clock", "Speedup")
	fmt.Println(strings.Repeat("-", 40))

	var baselineTime time.Duration

	for _, procs := range []int{1, 2, 4, runtime.NumCPU()} {
		runtime.GOMAXPROCS(procs)

		start := time.Now()
		var wg sync.WaitGroup

		for i := 0; i < numWorkers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ioWork()
			}()
		}

		wg.Wait()
		elapsed := time.Since(start)

		if procs == 1 {
			baselineTime = elapsed
		}

		speedup := float64(baselineTime) / float64(elapsed)
		fmt.Printf("%-12d %-15v %-10.2fx\n", procs, elapsed.Round(time.Millisecond), speedup)
	}

	runtime.GOMAXPROCS(runtime.NumCPU())
}
```

**What's happening here:** 20 workers each sleep for 50ms. Sleeping goroutines do NOT occupy a P -- they are parked by the runtime and the P is free to run other goroutines. So all 20 can sleep concurrently even with GOMAXPROCS=1.

**Key insight:** IO-bound work shows ~1.0x speedup regardless of GOMAXPROCS because the goroutines spend almost no time on the CPU. They are waiting, not computing. Adding more Ps does not speed up waiting.

**What would happen if ioWork did real network I/O instead of sleep?** The result would be similar. Network I/O goroutines park while waiting for data, freeing the P for other goroutines.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
Workers: 20 (IO-bound, 50ms sleep each)
GOMAXPROCS   Wall-Clock      Speedup
----------------------------------------
1            52ms            1.00x
2            51ms            1.02x
4            51ms            1.02x
8            51ms            1.02x
```

## Step 4 -- Mixed Workload Analysis

Real workloads mix CPU and IO. The speedup is between pure CPU (linear) and pure IO (flat).

```go
package main

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"
)

func main() {
	mixedWork := func() {
		// CPU phase: ~40ms of computation
		result := 0
		for i := 0; i < 50_000_000; i++ {
			result ^= i
		}
		// IO phase: 20ms of waiting
		time.Sleep(20 * time.Millisecond)
	}

	numWorkers := 8

	fmt.Printf("Workers: %d (CPU work + 20ms IO wait each)\n", numWorkers)
	fmt.Printf("%-12s %-15s %-10s\n", "GOMAXPROCS", "Wall-Clock", "Speedup")
	fmt.Println(strings.Repeat("-", 40))

	var baselineTime time.Duration

	for _, procs := range []int{1, 2, 4, runtime.NumCPU()} {
		runtime.GOMAXPROCS(procs)

		start := time.Now()
		var wg sync.WaitGroup

		for i := 0; i < numWorkers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				mixedWork()
			}()
		}

		wg.Wait()
		elapsed := time.Since(start)

		if procs == 1 {
			baselineTime = elapsed
		}

		speedup := float64(baselineTime) / float64(elapsed)
		fmt.Printf("%-12d %-15v %-10.2fx\n", procs, elapsed.Round(time.Millisecond), speedup)
	}

	runtime.GOMAXPROCS(runtime.NumCPU())
}
```

**What's happening here:** Each worker does ~40ms of CPU work, then 20ms of IO wait. With GOMAXPROCS=1, the CPU portions serialize (8 * 40ms = 320ms) plus 20ms IO = ~340ms. With GOMAXPROCS=8, the CPU portions parallelize (~40ms) plus 20ms IO = ~60ms.

**Key insight:** Speedup is proportional to the CPU fraction of the workload. If 70% of the time is CPU-bound, you can speed that part up linearly. The IO fraction does not benefit from more Ps. This is Amdahl's Law in practice.

**What would happen if the IO portion were 90% of the total?** Speedup would be minimal even at GOMAXPROCS=NumCPU, because parallelizing 10% of the work has limited impact.

### Intermediate Verification
```bash
go run main.go
```
Expected output: speedup between pure CPU (linear) and pure IO (flat).

## Common Mistakes

### Setting GOMAXPROCS Higher Than CPU Count

**Wrong -- complete program:**
```go
package main

import (
	"fmt"
	"runtime"
	"sync"
	"time"
)

func main() {
	runtime.GOMAXPROCS(100) // on a 4-core machine

	work := func() {
		result := 0
		for i := 0; i < 100_000_000; i++ {
			result ^= i
		}
	}

	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); work() }()
	}
	wg.Wait()
	fmt.Printf("Wall-clock: %v\n", time.Since(start))
}
```

**What happens:** For CPU-bound work, GOMAXPROCS > NumCPU provides no benefit and may slightly hurt performance due to context switching overhead. The hardware has only NumCPU physical execution units.

**Fix:** Leave GOMAXPROCS at its default (`runtime.NumCPU()`). Only tune it when benchmarks prove a different value is better.

### Assuming More Goroutines Means More Parallelism

**Wrong thinking:** "If I create 1000 goroutines, they'll all run in parallel."

**What happens:** Only GOMAXPROCS goroutines can execute Go code simultaneously. The rest wait in run queues.

**Fix:** For CPU-bound work, creating more goroutines than Ps increases scheduling overhead without improving throughput. Match goroutine count to GOMAXPROCS for CPU-bound tasks.

### Benchmarking Without Warming Up

**Wrong -- complete program:**
```go
package main

import (
	"fmt"
	"time"
)

func doWork() int {
	result := 0
	for i := 0; i < 100_000_000; i++ {
		result ^= i
	}
	return result
}

func main() {
	// First run includes GC, runtime initialization, cache warming
	start := time.Now()
	doWork()
	fmt.Printf("Time: %v\n", time.Since(start))
}
```

**What happens:** The first measurement includes one-time costs (GC warmup, CPU cache cold starts) that inflate the result.

**Correct -- warm up first:**
```go
package main

import (
	"fmt"
	"time"
)

func doWork() int {
	result := 0
	for i := 0; i < 100_000_000; i++ {
		result ^= i
	}
	return result
}

func main() {
	doWork() // warmup run (discard result)

	start := time.Now()
	doWork()
	fmt.Printf("Time: %v\n", time.Since(start))
}
```

## Verify What You Learned

Create a program that:
1. Defines three workload types: "cpu" (100M iterations), "io" (50ms sleep), and "mixed" (50M iterations + 20ms sleep)
2. Runs each workload with GOMAXPROCS from 1 to NumCPU
3. Prints a summary table for each workload showing GOMAXPROCS, wall-clock, and speedup
4. Adds a comment explaining why the optimal GOMAXPROCS differs between workload types

## What's Next
Continue to [06-cooperative-scheduling](../06-cooperative-scheduling/06-cooperative-scheduling.md) to understand how the Go scheduler decides when to switch between goroutines.

## Summary
- **Concurrency** is structure (multiple tasks in flight); **parallelism** is execution (multiple tasks running simultaneously)
- `GOMAXPROCS` controls the number of Ps, which limits true parallelism
- CPU-bound work shows roughly linear speedup up to the physical core count
- IO-bound work benefits minimally from additional Ps because goroutines spend most time waiting
- Mixed workloads show intermediate speedup proportional to their CPU fraction (Amdahl's Law)
- Default `GOMAXPROCS=NumCPU()` is correct for most applications
- Creating more goroutines than Ps does not increase parallelism for CPU-bound work
- Always benchmark to find the optimal configuration for your specific workload

## Reference
- [runtime.GOMAXPROCS](https://pkg.go.dev/runtime#GOMAXPROCS)
- [Go Blog: Concurrency is not parallelism](https://go.dev/blog/waza-talk)
- [Rob Pike: Concurrency is not Parallelism (video)](https://www.youtube.com/watch?v=oV9rvDllKEg)
- [Amdahl's Law (Wikipedia)](https://en.wikipedia.org/wiki/Amdahl%27s_law)
