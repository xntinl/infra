---
difficulty: advanced
concepts: [goroutine scalability, memory overhead, runtime.MemStats, practical limits, when NOT to goroutine, worker pools]
tools: [go]
estimated_time: 45m
bloom_level: create
prerequisites: [01-launching-goroutines, 02-goroutine-vs-os-thread, 05-gomaxprocs-and-parallelism]
---

# 8. A Million Goroutines


## Learning Objectives
After completing this exercise, you will be able to:
- **Measure** the actual memory and time cost of launching goroutines at scale (1K to 1M)
- **Identify** the practical limits of goroutine creation on your machine
- **Evaluate** when the goroutine-per-task pattern is appropriate vs when it adds overhead
- **Apply** `runtime.MemStats` to build detailed resource profiles

## Why Push the Limits

Go developers often hear that goroutines are "cheap" and that you can create millions of them. This exercise puts that claim to the test by simulating a load testing tool: launching goroutines that simulate users making requests against your service. By systematically measuring the cost of creating 1K, 10K, 100K, and 1M simulated users, you will develop an intuitive understanding of exactly how cheap (or expensive) goroutines are.

More importantly, this exercise teaches you when NOT to create goroutines. Just because you can create a million goroutines does not mean you should. Each goroutine consumes memory, occupies scheduler run queues, and competes for CPU time. For CPU-bound work, the optimal number of goroutines is typically `runtime.NumCPU()`, not "as many as possible." For IO-bound work, goroutines are often the right abstraction, but unbounded creation can exhaust memory.

Understanding these tradeoffs separates informed Go developers from those who use goroutines indiscriminately. After this exercise, you will be able to reason about goroutine costs in real systems and make architecture decisions grounded in measurement rather than folklore.

## Step 1 -- Load Test Simulation: Launch Time at Scale

Imagine building a load testing tool like `hey` or `k6`. Each simulated user is a goroutine that makes a request to your service. How fast can you spin up users?

```go
package main

import (
	"fmt"
	"math/rand"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

func main() {
	counts := []int{1_000, 10_000, 100_000, 500_000, 1_000_000}

	fmt.Println("=== Load Test Simulator: Goroutine Launch Benchmark ===")
	fmt.Printf("%-12s %-15s %-15s %-15s\n", "Users", "Spin-up Time", "Per User", "Users/sec")
	fmt.Println(strings.Repeat("-", 60))

	for _, count := range counts {
		done := make(chan struct{})
		var ready int64

		start := time.Now()
		for i := 0; i < count; i++ {
			go func() {
				atomic.AddInt64(&ready, 1)
				// Simulated user: random think time + request
				time.Sleep(time.Duration(rand.Intn(10)+1) * time.Millisecond)
				<-done
			}()
		}

		// Wait for all goroutines to start
		for atomic.LoadInt64(&ready) < int64(count) {
			runtime.Gosched()
		}
		launchTime := time.Since(start)

		perUser := launchTime / time.Duration(count)
		perSecond := float64(count) / launchTime.Seconds()

		fmt.Printf("%-12d %-15v %-15v %-15.0f\n",
			count, launchTime.Round(time.Millisecond), perUser, perSecond)

		close(done)
		time.Sleep(100 * time.Millisecond)
		runtime.GC()
	}
}
```

**What's happening here:** For each user count, we create goroutines that simulate load test users: each does a small random delay (think time) before waiting for a signal. We measure how long it takes to spin up all users and calculate per-user cost and throughput.

**Key insight:** Each goroutine takes roughly 500ns-1us to create. This means you can spin up approximately 1 million simulated users per second. For a load testing tool, this is the startup overhead before any actual requests are sent.

**What would happen if you didn't clean up between iterations?** Memory from the previous round's goroutines would still be in use, inflating the next round's measurements and potentially causing OOM for large counts.

### Intermediate Verification
```bash
go run main.go
```
Expected output pattern (values vary by machine):
```
=== Load Test Simulator: Goroutine Launch Benchmark ===
Users        Spin-up Time    Per User        Users/sec
------------------------------------------------------------
1000         1ms             1us             1000000
10000        8ms             800ns           1250000
100000       75ms            750ns           1333333
500000       380ms           760ns           1315789
1000000      780ms           780ns           1282051
```

## Step 2 -- Memory Footprint per Simulated User

Use `runtime.MemStats` to measure how much memory each simulated user consumes. This determines how many concurrent users your load testing machine can sustain.

```go
package main

import (
	"fmt"
	"math/rand"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

func formatBytes(b uint64) string {
	switch {
	case b >= 1024*1024*1024:
		return fmt.Sprintf("%.2f GB", float64(b)/(1024*1024*1024))
	case b >= 1024*1024:
		return fmt.Sprintf("%.2f MB", float64(b)/(1024*1024))
	case b >= 1024:
		return fmt.Sprintf("%.2f KB", float64(b)/1024)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func main() {
	counts := []int{1_000, 10_000, 100_000, 500_000}

	fmt.Println("=== Memory per Simulated User ===")
	fmt.Printf("%-12s %-15s %-15s %-15s %-15s\n",
		"Users", "StackInUse", "HeapInUse", "Sys (Total)", "Per User")
	fmt.Println(strings.Repeat("-", 75))

	for _, count := range counts {
		runtime.GC()
		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)

		done := make(chan struct{})
		var counter int64
		for i := 0; i < count; i++ {
			go func() {
				// Each simulated user has some local state
				userID := rand.Intn(1_000_000)
				requestPath := fmt.Sprintf("/api/resource/%d", userID)
				_ = requestPath
				atomic.AddInt64(&counter, 1)
				<-done
			}()
		}

		// Wait for all users to be active
		for atomic.LoadInt64(&counter) < int64(count) {
			runtime.Gosched()
		}
		time.Sleep(50 * time.Millisecond)

		var after runtime.MemStats
		runtime.ReadMemStats(&after)

		stackDiff := after.StackInuse - before.StackInuse
		heapDiff := after.HeapInuse - before.HeapInuse
		sysDiff := after.Sys - before.Sys
		total := stackDiff + heapDiff
		perUser := total / uint64(count)

		fmt.Printf("%-12d %-15s %-15s %-15s %-15s\n",
			count,
			formatBytes(stackDiff),
			formatBytes(heapDiff),
			formatBytes(sysDiff),
			formatBytes(perUser),
		)

		close(done)
		time.Sleep(200 * time.Millisecond)
		runtime.GC()
	}

	fmt.Println()
	fmt.Println("At ~8 KB per user, your load test machine's RAM determines the ceiling.")
	fmt.Println("A 16 GB machine can sustain ~1M simulated users.")
	fmt.Println("A 4 GB machine caps out at ~250K users.")
}
```

**What's happening here:** For each user count, we measure three memory metrics. `StackInuse` is the dominant cost: stack memory for each user goroutine. `HeapInuse` includes goroutine descriptor structs and any heap allocations. Each simulated user has local state (user ID, request path) to be more realistic than a bare channel receive.

**Key insight:** At ~8 KB per goroutine (stack + heap), 1 million users consume roughly 8 GB of memory. When planning a load test, your machine's RAM directly determines how many concurrent users you can simulate. This is why load testing tools like `k6` and `locust` are careful about per-user memory.

### Intermediate Verification
```bash
go run main.go
```
Expected output pattern:
```
=== Memory per Simulated User ===
Users        StackInUse      HeapInUse       Sys (Total)     Per User
---------------------------------------------------------------------------
1000         8.00 MB         0.50 MB         10.00 MB        8.50 KB
10000        80.00 MB        5.00 MB         90.00 MB        8.50 KB
100000       800.00 MB       50.00 MB        900.00 MB       8.50 KB
500000       3.91 GB         250.00 MB       4.50 GB         8.50 KB
```

## Step 3 -- Measuring GC Impact on Latency

In a load testing tool or a production server, GC pauses directly affect request latency. Show how goroutine count affects GC pause times.

```go
package main

import (
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

func main() {
	counts := []int{1_000, 10_000, 100_000, 500_000}

	fmt.Println("=== GC Impact at Scale ===")
	fmt.Println("More goroutines = more stacks for GC to scan = longer pauses")
	fmt.Println()
	fmt.Printf("%-12s %-15s %-15s %-15s\n",
		"Goroutines", "GC Pause", "Num GC", "Alloc Rate")
	fmt.Println(strings.Repeat("-", 60))

	for _, count := range counts {
		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)

		done := make(chan struct{})
		var ready int64
		for i := 0; i < count; i++ {
			go func() {
				atomic.AddInt64(&ready, 1)
				<-done
			}()
		}
		for atomic.LoadInt64(&ready) < int64(count) {
			runtime.Gosched()
		}
		time.Sleep(50 * time.Millisecond)

		gcStart := time.Now()
		runtime.GC()
		gcDuration := time.Since(gcStart)

		var after runtime.MemStats
		runtime.ReadMemStats(&after)

		numGC := after.NumGC - before.NumGC
		allocRate := float64(after.TotalAlloc-before.TotalAlloc) / (1024 * 1024)

		fmt.Printf("%-12d %-15v %-15d %-15.2f MB\n",
			count, gcDuration.Round(time.Microsecond), numGC, allocRate)

		close(done)
		time.Sleep(200 * time.Millisecond)
		runtime.GC()
	}

	fmt.Println()
	fmt.Println("GC pauses are the hidden cost of massive goroutine counts.")
	fmt.Println("For latency-sensitive services (p99 < 10ms), keep goroutine")
	fmt.Println("counts under the threshold where GC pauses start to matter.")
}
```

**What's happening here:** We create goroutines, then force a GC cycle and measure how long it takes. GC pause time scales with goroutine count because the garbage collector must scan every goroutine's stack for pointers.

**Key insight:** GC pause time is the hidden cost of millions of goroutines. While individual goroutines are cheap, the GC must scan all their stacks during every cycle. At 500K goroutines, GC pauses can reach tens of milliseconds. For a service with p99 latency requirements under 10ms, this is unacceptable. This is why production servers use bounded worker pools instead of unbounded goroutine creation.

### Intermediate Verification
```bash
go run main.go
```
Expected output (GC pause grows with count):
```
=== GC Impact at Scale ===
More goroutines = more stacks for GC to scan = longer pauses

Goroutines   GC Pause        Num GC          Alloc Rate
------------------------------------------------------------
1000         200us           2               0.50 MB
10000        1ms             3               5.00 MB
100000       15ms            5               50.00 MB
500000       75ms            8               250.00 MB
```

## Step 4 -- When NOT to Create Goroutines: CPU-Bound Work

Demonstrate that for CPU-bound work (like processing request payloads), more goroutines does NOT mean faster processing. The optimal count is `runtime.NumCPU()`.

```go
package main

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"time"
)

func main() {
	// Simulate processing 10M data points from user requests
	data := make([]float64, 10_000_000)
	for i := range data {
		data[i] = float64(i) * 0.001
	}

	processChunk := func(chunk []float64) float64 {
		var sum float64
		for _, v := range chunk {
			sum += math.Sqrt(v) // CPU-intensive transformation
		}
		return sum
	}

	goroutineCounts := []int{1, runtime.NumCPU(), 100, 1_000, 10_000}

	fmt.Printf("Processing %d data points (CPU-bound work):\n\n", len(data))
	fmt.Printf("%-15s %-15s %-15s\n", "Goroutines", "Wall-Clock", "vs Baseline")
	fmt.Println(strings.Repeat("-", 48))

	var baselineTime time.Duration

	for _, numG := range goroutineCounts {
		chunkSize := len(data) / numG
		if chunkSize == 0 {
			chunkSize = 1
		}

		start := time.Now()

		results := make(chan float64, numG)
		launched := 0

		for i := 0; i < len(data); i += chunkSize {
			end := i + chunkSize
			if end > len(data) {
				end = len(data)
			}
			chunk := data[i:end]
			launched++
			go func(s []float64) {
				results <- processChunk(s)
			}(chunk)
		}

		var total float64
		for i := 0; i < launched; i++ {
			total += <-results
		}

		elapsed := time.Since(start)
		if numG == 1 {
			baselineTime = elapsed
		}

		overhead := float64(elapsed) / float64(baselineTime)
		fmt.Printf("%-15d %-15v %-15.2fx\n", numG, elapsed.Round(time.Microsecond), overhead)
		_ = total
	}

	fmt.Println()
	fmt.Printf("Optimal: %d goroutines (= NumCPU)\n", runtime.NumCPU())
	fmt.Println("More goroutines add scheduling overhead without additional parallelism.")
	fmt.Println("For CPU-bound request processing, use a fixed worker pool, not goroutine-per-item.")
}
```

**What's happening here:** We process 10 million data points using different numbers of goroutines. With 1 goroutine: no parallelism. With NumCPU: optimal parallelism. With 10,000: scheduling overhead dominates, making it SLOWER than 1 goroutine.

**Key insight:** For CPU-bound work, creating more goroutines than cores hurts performance. Each goroutine adds scheduling overhead (run queue insertion, context switching) without doing any additional useful work. When processing request payloads, computing reports, or transforming data, use `runtime.NumCPU()` workers, not one goroutine per data item.

### Intermediate Verification
```bash
go run main.go
```
Expected output pattern:
```
Processing 10000000 data points (CPU-bound work):

Goroutines      Wall-Clock      vs Baseline
------------------------------------------------
1               12ms            1.00x
8               3ms             0.25x   (speedup from parallelism)
100             4ms             0.33x   (slight overhead)
1000            8ms             0.67x   (overhead growing)
10000           45ms            3.75x   (SLOWER than 1 goroutine!)

Optimal: 8 goroutines (= NumCPU)
More goroutines add scheduling overhead without additional parallelism.
For CPU-bound request processing, use a fixed worker pool, not goroutine-per-item.
```

## Step 5 -- Complete Scalability Profile for Your Machine

Create a comprehensive report combining all measurements. This is the kind of capacity planning data you would use to configure a production load testing tool or set goroutine limits in your server.

```go
package main

import (
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

func formatBytes(b uint64) string {
	switch {
	case b >= 1024*1024*1024:
		return fmt.Sprintf("%.2f GB", float64(b)/(1024*1024*1024))
	case b >= 1024*1024:
		return fmt.Sprintf("%.2f MB", float64(b)/(1024*1024))
	case b >= 1024:
		return fmt.Sprintf("%.2f KB", float64(b)/1024)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func main() {
	fmt.Println("=== Goroutine Scalability Profile ===")
	fmt.Println("Use this data for capacity planning: load testing, server limits,")
	fmt.Println("and worker pool sizing.")
	fmt.Println()

	type measurement struct {
		count      int
		launchTime time.Duration
		stackMem   uint64
		heapMem    uint64
		gcPause    time.Duration
	}

	counts := []int{100, 1_000, 10_000, 100_000}
	var measurements []measurement

	for _, count := range counts {
		runtime.GC()
		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)

		done := make(chan struct{})
		var ready int64

		launchStart := time.Now()
		for i := 0; i < count; i++ {
			go func() {
				atomic.AddInt64(&ready, 1)
				<-done
			}()
		}
		for atomic.LoadInt64(&ready) < int64(count) {
			runtime.Gosched()
		}
		launchTime := time.Since(launchStart)
		time.Sleep(50 * time.Millisecond)

		gcStart := time.Now()
		runtime.GC()
		gcPause := time.Since(gcStart)

		var after runtime.MemStats
		runtime.ReadMemStats(&after)

		measurements = append(measurements, measurement{
			count:      count,
			launchTime: launchTime,
			stackMem:   after.StackInuse - before.StackInuse,
			heapMem:    after.HeapInuse - before.HeapInuse,
			gcPause:    gcPause,
		})

		close(done)
		time.Sleep(200 * time.Millisecond)
	}

	fmt.Printf("%-10s %-12s %-12s %-12s %-12s %-12s\n",
		"Count", "Spin-up", "Stack", "Heap", "GC Pause", "KB/goroutine")
	fmt.Println(strings.Repeat("-", 72))

	for _, m := range measurements {
		perG := float64(m.stackMem+m.heapMem) / float64(m.count) / 1024
		fmt.Printf("%-10d %-12v %-12s %-12s %-12v %-12.1f\n",
			m.count,
			m.launchTime.Round(time.Millisecond),
			formatBytes(m.stackMem),
			formatBytes(m.heapMem),
			m.gcPause.Round(time.Microsecond),
			perG,
		)
	}

	fmt.Println()
	fmt.Println("--- Capacity Planning Guidelines ---")
	fmt.Printf("CPU cores:           %d\n", runtime.NumCPU())
	fmt.Printf("CPU-bound optimal:   %d goroutines (one per core)\n", runtime.NumCPU())
	fmt.Println("IO-bound (server):   1 goroutine per connection, bounded by semaphore")
	fmt.Println("IO-bound (load test): 1 goroutine per simulated user, limited by RAM")
	fmt.Println("Practical ceiling:   depends on RAM; ~100K-1M for most machines")
	fmt.Println()
	fmt.Println("Rule of thumb: if goroutine count can grow unbounded in production,")
	fmt.Println("add a semaphore. Unbounded goroutine creation is the #1 cause of")
	fmt.Println("Go service OOM kills under load.")
}
```

**What's happening here:** For each goroutine count, we measure launch time, stack memory, heap memory, and GC pause time. The result is a comprehensive profile specific to your machine, suitable for making real capacity decisions.

**Key insight:** This gives you concrete numbers for YOUR hardware. A machine with 16 GB RAM can comfortably sustain ~500K idle goroutines; a machine with 4 GB caps out at ~100K. Never assume goroutine costs -- measure them on the hardware where your code will actually run.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Goroutine Scalability Profile ===
Use this data for capacity planning: load testing, server limits,
and worker pool sizing.

Count      Spin-up      Stack        Heap         GC Pause     KB/goroutine
------------------------------------------------------------------------
100        0ms          800.00 KB    50.00 KB     50us         8.5
1000       1ms          8.00 MB     500.00 KB     200us        8.5
10000      8ms          80.00 MB    5.00 MB       2ms          8.5
100000     75ms         800.00 MB   50.00 MB      15ms         8.5

--- Capacity Planning Guidelines ---
CPU cores:           8
CPU-bound optimal:   8 goroutines (one per core)
IO-bound (server):   1 goroutine per connection, bounded by semaphore
IO-bound (load test): 1 goroutine per simulated user, limited by RAM
Practical ceiling:   depends on RAM; ~100K-1M for most machines
```

## Deep Dive: Bounded Concurrency with Semaphores

In production, never create goroutines without bounds. A load testing tool or server that creates unbounded goroutines will OOM under heavy traffic. Use a semaphore pattern:

```go
package main

import (
	"fmt"
	"math/rand"
	"sync/atomic"
	"time"
)

func main() {
	totalRequests := 1000
	maxConcurrent := 50 // at most 50 requests in flight

	sem := make(chan struct{}, maxConcurrent)
	done := make(chan struct{})
	var completed int64

	start := time.Now()

	for i := 0; i < totalRequests; i++ {
		sem <- struct{}{} // acquire slot (blocks when 50 are running)
		go func(id int) {
			defer func() { <-sem }() // release slot when done

			// Simulate request: random latency 1-10ms
			time.Sleep(time.Duration(rand.Intn(10)+1) * time.Millisecond)
			atomic.AddInt64(&completed, 1)

			if atomic.LoadInt64(&completed) == int64(totalRequests) {
				close(done)
			}
		}(i)
	}

	<-done
	fmt.Printf("Processed %d requests in %v\n", totalRequests, time.Since(start).Round(time.Millisecond))
	fmt.Printf("Max concurrent: %d (bounded by semaphore)\n", maxConcurrent)
	fmt.Println()
	fmt.Println("Without the semaphore, all 1000 goroutines would exist simultaneously.")
	fmt.Println("With it, at most 50 run at once, keeping memory predictable.")
}
```

**What's happening here:** The semaphore channel has a buffer of 50. Sending to it blocks when 50 goroutines are already running. Each goroutine releases its slot on completion. This ensures memory usage is bounded regardless of the total request count.

**Key insight:** This is the production pattern for goroutine-per-task at scale. Unbounded creation is fine for small numbers (hundreds), but anything that could grow to thousands or more needs a bound. Every production Go server should have a concurrency limit.

## Common Mistakes

### Unbounded Goroutine Creation in Servers

**Wrong -- complete program:**
```go
package main

import (
	"fmt"
	"net"
)

func main() {
	ln, _ := net.Listen("tcp", ":8080")
	for {
		conn, _ := ln.Accept()
		go func(c net.Conn) { // unbounded: can create millions under load
			defer c.Close()
			buf := make([]byte, 1024)
			c.Read(buf)
			fmt.Fprintf(c, "hello\n")
		}(conn)
	}
}
```

**What happens:** Under a traffic spike or DDoS, goroutine count grows without limit, consuming ~8 KB each. At 100K connections, that is 800 MB of stack alone. The process gets OOM-killed, and ALL in-flight requests are lost.

**Correct -- bounded with semaphore:**
```go
package main

import (
	"fmt"
	"net"
)

func main() {
	ln, _ := net.Listen("tcp", ":8080")
	sem := make(chan struct{}, 10000) // max 10K concurrent connections

	for {
		conn, _ := ln.Accept()
		sem <- struct{}{} // blocks when at capacity -- applies backpressure
		go func(c net.Conn) {
			defer func() { <-sem }()
			defer c.Close()
			buf := make([]byte, 1024)
			c.Read(buf)
			fmt.Fprintf(c, "hello\n")
		}(conn)
	}
}
```

### Ignoring Memory When Scaling Goroutines

**Wrong thinking:** "Goroutines are free, so I'll create one per row in my 10M-row dataset."

**What happens:** 10M goroutines * ~8 KB each = ~80 GB of memory. The process is immediately OOM-killed. In Kubernetes, the pod gets evicted and your batch job never completes.

**Fix:** Use a worker pool:
```go
package main

import (
	"fmt"
	"runtime"
	"sync"
)

func main() {
	data := make([]int, 10_000_000)
	for i := range data {
		data[i] = i
	}

	work := make(chan int, 1000)
	var wg sync.WaitGroup

	// Fixed number of workers: bounded memory
	for i := 0; i < runtime.NumCPU(); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range work {
				_ = item * item // process item
			}
		}()
	}

	for _, item := range data {
		work <- item
	}
	close(work)
	wg.Wait()
	fmt.Printf("Processed %d items with %d workers (bounded memory)\n",
		len(data), runtime.NumCPU())
}
```

### Not Measuring Before Deciding

**Wrong thinking:** "I'll use exactly 1000 goroutines because someone said that's a good number."

**What happens:** The optimal number depends on your workload (CPU-bound vs IO-bound), your machine's RAM, and the per-goroutine memory usage of your specific code. 1000 might be perfect for IO-bound API calls but terrible for CPU-bound data processing.

**Fix:** Benchmark with different goroutine counts and measure actual performance. Use this exercise's profiling approach to find the sweet spot for your specific workload and hardware.

## Verify What You Learned

Create a comprehensive load testing benchmark that:
1. Finds the maximum number of simulated users your machine can handle before consuming more than 2 GB of memory (use binary search, starting from 100K)
2. For each user count, simulates realistic user behavior: random think time (1-10ms) and request processing (1-5ms)
3. Prints the relationship between user count and: spin-up time, memory usage, and GC pause time
4. Recommends the practical ceiling for your machine with a 50% safety margin

**Warning:** This may consume significant memory. Save your work before running.

## What's Next
You have completed the goroutines and scheduling section. Continue to the next section to learn about channels and synchronization.

## Summary
- Creating a goroutine takes approximately 500ns-1us (~1M goroutines per second)
- Each goroutine consumes roughly 2-8 KB of stack memory plus heap overhead
- 1 million goroutines requires approximately 8-16 GB of memory
- GC pause time grows with goroutine count because stacks must be scanned
- For CPU-bound work, NumCPU goroutines is optimal; more adds scheduling overhead
- For IO-bound work, goroutine-per-task is appropriate but should be bounded
- Always measure on your target hardware; never assume goroutine costs
- Use semaphores or worker pools to bound goroutine creation in production
- Unbounded goroutine creation is the leading cause of Go service OOM kills under load

## Reference
- [runtime.MemStats](https://pkg.go.dev/runtime#MemStats)
- [runtime.ReadMemStats](https://pkg.go.dev/runtime#ReadMemStats)
- [Go Blog: Go GC: Prioritizing low latency and simplicity](https://go.dev/blog/go15gc)
- [Concurrency in Go (Katherine Cox-Buday)](https://www.oreilly.com/library/view/concurrency-in-go/9781491941294/)
