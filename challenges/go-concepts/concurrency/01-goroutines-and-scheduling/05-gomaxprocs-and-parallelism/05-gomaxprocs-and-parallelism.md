---
difficulty: intermediate
concepts: [GOMAXPROCS, concurrency vs parallelism, CPU-bound vs IO-bound, wall-clock time, benchmarking]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [01-launching-goroutines, 03-gmp-model-in-action]
---

# 5. GOMAXPROCS and Parallelism


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

## Step 1 -- Image Filter Simulation: Concurrency vs Parallelism

Imagine a service that applies a CPU-intensive filter (e.g., blur, edge detection) to a batch of uploaded images. Each image is a large slice of data. With GOMAXPROCS=1, the filters run one at a time. With GOMAXPROCS=NumCPU, they run on separate cores simultaneously.

```go
package main

import (
	"fmt"
	"math"
	"runtime"
	"sync"
	"time"
)

func applyFilter(imageData []float64) float64 {
	var result float64
	for i := 1; i < len(imageData)-1; i++ {
		// Simulate a convolution kernel (blur-like operation)
		result += math.Sqrt(imageData[i-1]*imageData[i-1]+
			imageData[i]*imageData[i]+
			imageData[i+1]*imageData[i+1]) * 0.333
	}
	return result
}

func main() {
	const numImages = 4
	const imageSize = 2_000_000 // 2M "pixels" per image

	images := make([][]float64, numImages)
	for i := range images {
		images[i] = make([]float64, imageSize)
		for j := range images[i] {
			images[i][j] = float64(j%256) / 255.0
		}
	}

	for _, procs := range []int{1, runtime.NumCPU()} {
		runtime.GOMAXPROCS(procs)
		fmt.Printf("GOMAXPROCS=%d:\n", procs)

		start := time.Now()
		var wg sync.WaitGroup

		for i := 0; i < numImages; i++ {
			wg.Add(1)
			go func(imgIdx int) {
				defer wg.Done()
				imgStart := time.Now()
				result := applyFilter(images[imgIdx])
				fmt.Printf("  image %d filtered: %v (checksum: %.2f)\n",
					imgIdx, time.Since(imgStart).Round(time.Millisecond), result)
			}(i)
		}

		wg.Wait()
		fmt.Printf("  Total wall-clock: %v\n\n", time.Since(start).Round(time.Millisecond))
	}

	runtime.GOMAXPROCS(runtime.NumCPU())
}
```

**What's happening here:** Four workers each apply a mathematical filter to a 2M-element slice (simulating image processing). With GOMAXPROCS=1, they share one P, so they run sequentially: total time is ~4x one image. With GOMAXPROCS=NumCPU, all four run simultaneously on different cores: total time approaches 1x one image.

**Key insight:** The `go` keyword gives you concurrency (structure). GOMAXPROCS gives you parallelism (simultaneous execution). Without multiple Ps, goroutines take turns -- your image processing pipeline is no faster than sequential code.

**What would happen with GOMAXPROCS=2?** Two images would be processed simultaneously, then the other two. Total time would be ~2x one image instead of ~4x.

### Intermediate Verification
```bash
go run main.go
```
Expected output pattern:
```
GOMAXPROCS=1:
  image 0 filtered: 85ms (checksum: 123456.78)
  image 1 filtered: 84ms (checksum: 123456.78)
  image 2 filtered: 85ms (checksum: 123456.78)
  image 3 filtered: 84ms (checksum: 123456.78)
  Total wall-clock: 340ms

GOMAXPROCS=8:
  image 1 filtered: 86ms (checksum: 123456.78)
  image 3 filtered: 87ms (checksum: 123456.78)
  image 0 filtered: 87ms (checksum: 123456.78)
  image 2 filtered: 88ms (checksum: 123456.78)
  Total wall-clock: 90ms
```

## Step 2 -- Image Processing Benchmark Across GOMAXPROCS Values

Measure the exact speedup you get at each GOMAXPROCS level for the image filter workload.

```go
package main

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync"
	"time"
)

func applyFilter(data []float64) float64 {
	var result float64
	for i := 1; i < len(data)-1; i++ {
		result += math.Sqrt(data[i-1]*data[i-1]+
			data[i]*data[i]+
			data[i+1]*data[i+1]) * 0.333
	}
	return result
}

func main() {
	const numImages = 8
	const imageSize = 1_500_000

	images := make([][]float64, numImages)
	for i := range images {
		images[i] = make([]float64, imageSize)
		for j := range images[i] {
			images[i][j] = float64(j%256) / 255.0
		}
	}

	// Warm up CPU caches
	applyFilter(images[0])

	maxProcs := []int{1, 2, 4}
	if runtime.NumCPU() >= 8 {
		maxProcs = append(maxProcs, 8)
	}
	if runtime.NumCPU() >= 16 {
		maxProcs = append(maxProcs, 16)
	}

	fmt.Printf("Filtering %d images (%d pixels each):\n\n", numImages, imageSize)
	fmt.Printf("%-12s %-15s %-10s\n", "GOMAXPROCS", "Wall-Clock", "Speedup")
	fmt.Println(strings.Repeat("-", 40))

	var baselineTime time.Duration

	for _, procs := range maxProcs {
		runtime.GOMAXPROCS(procs)

		start := time.Now()
		var wg sync.WaitGroup

		for i := 0; i < numImages; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				applyFilter(images[idx])
			}(i)
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
	fmt.Println()
	fmt.Println("Speedup is roughly linear because image filtering is pure CPU work.")
	fmt.Println("Each core processes one image independently with no shared state.")
}
```

**What's happening here:** Eight images are filtered using different GOMAXPROCS values. Doubling Ps roughly halves the wall-clock time because the filter work distributes across more cores.

**Key insight:** Speedup is roughly linear for CPU-bound work until you hit the physical core count. Beyond that, adding Ps provides no benefit because there are no more cores to use. This is why your image processing service should set worker count to match available CPUs, not an arbitrary large number.

### Intermediate Verification
```bash
go run main.go
```
Expected output pattern:
```
Filtering 8 images (1500000 pixels each):

GOMAXPROCS   Wall-Clock      Speedup
----------------------------------------
1            520ms           1.00x
2            265ms           1.96x
4            135ms           3.85x
8            70ms            7.43x
```

## Step 3 -- IO-Bound Workload: Database Query Simulation

Show that simulated database queries (IO-bound work) benefit minimally from additional Ps. In a real service, this is why adding CPU cores does not speed up a database-heavy endpoint.

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
	queryDB := func(queryName string) string {
		time.Sleep(50 * time.Millisecond) // simulates database round-trip
		return queryName + ": 42 rows"
	}

	queries := []string{
		"SELECT users", "SELECT orders", "SELECT products",
		"SELECT reviews", "SELECT inventory", "SELECT payments",
		"SELECT sessions", "SELECT audit_log", "SELECT configs",
		"SELECT metrics", "SELECT alerts", "SELECT schedules",
	}

	fmt.Printf("Running %d database queries (50ms each, IO-bound):\n\n", len(queries))
	fmt.Printf("%-12s %-15s %-10s\n", "GOMAXPROCS", "Wall-Clock", "Speedup")
	fmt.Println(strings.Repeat("-", 40))

	var baselineTime time.Duration

	for _, procs := range []int{1, 2, 4, runtime.NumCPU()} {
		runtime.GOMAXPROCS(procs)

		start := time.Now()
		var wg sync.WaitGroup

		for _, q := range queries {
			wg.Add(1)
			go func(query string) {
				defer wg.Done()
				queryDB(query)
			}(q)
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
	fmt.Println()
	fmt.Println("IO-bound queries show ~1.0x speedup regardless of GOMAXPROCS.")
	fmt.Println("Goroutines park while waiting for the database. Adding cores")
	fmt.Println("does not make the database respond faster.")
}
```

**What's happening here:** Twelve simulated database queries each take 50ms. Sleeping goroutines do NOT occupy a P -- they are parked by the runtime and the P is free to run other goroutines. So all twelve can sleep concurrently even with GOMAXPROCS=1.

**Key insight:** IO-bound work shows ~1.0x speedup regardless of GOMAXPROCS because the goroutines spend almost no time on the CPU. They are waiting for the database, not computing. In production, if your service is slow because of database latency, adding more CPU cores will not help. You need to optimize queries, add caching, or scale the database.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
Running 12 database queries (50ms each, IO-bound):

GOMAXPROCS   Wall-Clock      Speedup
----------------------------------------
1            52ms            1.00x
2            51ms            1.02x
4            51ms            1.02x
8            51ms            1.02x

IO-bound queries show ~1.0x speedup regardless of GOMAXPROCS.
Goroutines park while waiting for the database. Adding cores
does not make the database respond faster.
```

## Step 4 -- Mixed Workload: API Handler with Compute + IO

Real API handlers mix CPU and IO. A request might validate input (CPU), query the database (IO), then serialize a response (CPU). The speedup from GOMAXPROCS is proportional to the CPU fraction.

```go
package main

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync"
	"time"
)

func handleAPIRequest(id int) {
	// CPU phase: validate and transform input (~40ms)
	result := 0.0
	for i := 0; i < 5_000_000; i++ {
		result += math.Sin(float64(i))
	}
	_ = result

	// IO phase: query database (~20ms)
	time.Sleep(20 * time.Millisecond)

	// CPU phase: serialize response (~10ms -- lighter)
	for i := 0; i < 1_000_000; i++ {
		result += math.Cos(float64(i))
	}
	_ = result
}

func main() {
	numRequests := 8

	fmt.Printf("Processing %d API requests (CPU validation + DB query + CPU serialization):\n\n", numRequests)
	fmt.Printf("%-12s %-15s %-10s\n", "GOMAXPROCS", "Wall-Clock", "Speedup")
	fmt.Println(strings.Repeat("-", 40))

	var baselineTime time.Duration

	for _, procs := range []int{1, 2, 4, runtime.NumCPU()} {
		runtime.GOMAXPROCS(procs)

		start := time.Now()
		var wg sync.WaitGroup

		for i := 0; i < numRequests; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				handleAPIRequest(id)
			}(i)
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
	fmt.Println()
	fmt.Println("Mixed workload: speedup is between pure-CPU (linear) and pure-IO (flat).")
	fmt.Println("The CPU phases parallelize, but the IO phase does not benefit from more Ps.")
	fmt.Println("This is Amdahl's Law: speedup is limited by the sequential fraction.")
}
```

**What's happening here:** Each API request handler does ~50ms of CPU work (validation + serialization) plus ~20ms of IO wait (database query). With GOMAXPROCS=1, the CPU portions serialize (8 * 50ms = 400ms) plus 20ms IO = ~420ms. With GOMAXPROCS=8, the CPU portions parallelize (~50ms) plus 20ms IO = ~70ms.

**Key insight:** Speedup is proportional to the CPU fraction of the workload. If 70% of the time is CPU-bound, you can speed that part up linearly. The IO fraction does not benefit from more Ps. This is Amdahl's Law in practice. When profiling a slow endpoint, first determine the CPU/IO split before throwing hardware at it.

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
	"math"
	"runtime"
	"sync"
	"time"
)

func main() {
	runtime.GOMAXPROCS(100) // on a 4-core machine

	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := 0.0
			for j := 0; j < 50_000_000; j++ {
				result += math.Sqrt(float64(j))
			}
			_ = result
		}()
	}
	wg.Wait()
	fmt.Printf("Wall-clock: %v\n", time.Since(start))
}
```

**What happens:** For CPU-bound work, GOMAXPROCS > NumCPU provides no benefit and may slightly hurt performance due to context switching overhead. The hardware has only NumCPU physical execution units. Your image filters will not run faster on 100 Ps if you only have 8 cores.

**Fix:** Leave GOMAXPROCS at its default (`runtime.NumCPU()`). Only tune it when benchmarks prove a different value is better.

### Assuming More Goroutines Means More Parallelism

**Wrong thinking:** "If I create 1000 goroutines for 1000 images, they'll all filter in parallel."

**What happens:** Only GOMAXPROCS goroutines can execute Go code simultaneously. The rest wait in run queues. With 8 cores, only 8 images are filtered at once; the other 992 wait their turn.

**Fix:** For CPU-bound work, creating more goroutines than Ps increases scheduling overhead without improving throughput. Match worker count to GOMAXPROCS for CPU-bound image processing.

### Benchmarking Without Warming Up

**Wrong -- complete program:**
```go
package main

import (
	"fmt"
	"math"
	"time"
)

func main() {
	// First run includes GC, CPU cache cold starts
	start := time.Now()
	result := 0.0
	for i := 0; i < 50_000_000; i++ {
		result += math.Sqrt(float64(i))
	}
	fmt.Printf("Time: %v (result: %.0f)\n", time.Since(start), result)
}
```

**What happens:** The first measurement includes one-time costs (GC warmup, CPU cache cold starts) that inflate the result.

**Correct -- warm up first:**
```go
package main

import (
	"fmt"
	"math"
	"time"
)

func doFilter() float64 {
	result := 0.0
	for i := 0; i < 50_000_000; i++ {
		result += math.Sqrt(float64(i))
	}
	return result
}

func main() {
	doFilter() // warmup run: fill CPU caches

	start := time.Now()
	result := doFilter()
	fmt.Printf("Time: %v (result: %.0f)\n", time.Since(start), result)
}
```

## Verify What You Learned

Create a program that:
1. Defines three workload types: "image-filter" (CPU-bound mathematical transformation), "db-queries" (IO-bound 50ms sleep), and "api-handler" (CPU + IO mix)
2. Runs each workload with GOMAXPROCS from 1 to NumCPU
3. Prints a summary table for each workload showing GOMAXPROCS, wall-clock, and speedup
4. Adds a comment explaining why the optimal GOMAXPROCS differs between workload types

## What's Next
Continue to [06-cooperative-scheduling](../06-cooperative-scheduling/06-cooperative-scheduling.md) to understand how the Go scheduler decides when to switch between goroutines.

## Summary
- **Concurrency** is structure (multiple tasks in flight); **parallelism** is execution (multiple tasks running simultaneously)
- `GOMAXPROCS` controls the number of Ps, which limits true parallelism
- CPU-bound work (image processing, checksums) shows roughly linear speedup up to the physical core count
- IO-bound work (database queries, API calls) benefits minimally from additional Ps
- Mixed workloads show intermediate speedup proportional to their CPU fraction (Amdahl's Law)
- Default `GOMAXPROCS=NumCPU()` is correct for most applications
- Creating more goroutines than Ps does not increase parallelism for CPU-bound work
- Always benchmark to find the optimal configuration for your specific workload

## Reference
- [runtime.GOMAXPROCS](https://pkg.go.dev/runtime#GOMAXPROCS)
- [Go Blog: Concurrency is not parallelism](https://go.dev/blog/waza-talk)
- [Rob Pike: Concurrency is not Parallelism (video)](https://www.youtube.com/watch?v=oV9rvDllKEg)
- [Amdahl's Law (Wikipedia)](https://en.wikipedia.org/wiki/Amdahl%27s_law)
