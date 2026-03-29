# 8. A Million Goroutines

<!--
difficulty: advanced
concepts: [goroutine scalability, memory overhead, runtime.MemStats, practical limits, when NOT to goroutine, worker pools]
tools: [go]
estimated_time: 45m
bloom_level: create
prerequisites: [01-launching-goroutines, 02-goroutine-vs-os-thread, 05-gomaxprocs-and-parallelism]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01 through 07 in this section
- Understanding of goroutine memory overhead and scheduling
- At least 4 GB of free RAM (this exercise creates many goroutines)

## Learning Objectives
After completing this exercise, you will be able to:
- **Measure** the actual memory and time cost of launching goroutines at scale (1K to 1M)
- **Identify** the practical limits of goroutine creation on your machine
- **Evaluate** when the goroutine-per-task pattern is appropriate vs when it adds overhead
- **Apply** `runtime.MemStats` to build detailed resource profiles

## Why Push the Limits

Go developers often hear that goroutines are "cheap" and that you can create millions of them. This exercise puts that claim to the test. By systematically measuring the cost of creating 1K, 10K, 100K, and 1M goroutines, you will develop an intuitive understanding of exactly how cheap (or expensive) goroutines are.

More importantly, this exercise teaches you when NOT to create goroutines. Just because you can create a million goroutines does not mean you should. Each goroutine consumes memory, occupies scheduler run queues, and competes for CPU time. For CPU-bound work, the optimal number of goroutines is typically `runtime.NumCPU()`, not "as many as possible." For IO-bound work, goroutines are often the right abstraction, but unbounded creation can exhaust memory.

Understanding these tradeoffs separates informed Go developers from those who use goroutines indiscriminately. After this exercise, you will be able to reason about goroutine costs in real systems and make architecture decisions grounded in measurement rather than folklore.

## Step 1 -- Measuring Launch Time at Scale

Measure how long it takes to create increasing numbers of goroutines.

```go
package main

import (
	"fmt"
	"runtime"
	"strings"
	"time"
)

func main() {
	counts := []int{1_000, 10_000, 100_000, 500_000, 1_000_000}

	fmt.Printf("%-12s %-15s %-15s %-15s\n", "Count", "Launch Time", "Per Goroutine", "Goroutines/sec")
	fmt.Println(strings.Repeat("-", 60))

	for _, count := range counts {
		done := make(chan struct{})

		start := time.Now()
		for i := 0; i < count; i++ {
			go func() {
				<-done
			}()
		}
		launchTime := time.Since(start)

		perGoroutine := launchTime / time.Duration(count)
		perSecond := float64(count) / launchTime.Seconds()

		fmt.Printf("%-12d %-15v %-15v %-15.0f\n",
			count, launchTime.Round(time.Millisecond), perGoroutine, perSecond)

		close(done)
		time.Sleep(100 * time.Millisecond)
		runtime.GC()
	}
}
```

**What's happening here:** For each count, we create goroutines that block on a channel, measure how long the creation loop takes, then calculate per-goroutine cost and throughput.

**Key insight:** Each goroutine takes roughly 500ns-1us to create. This means you can create approximately 1 million goroutines per second. The cost is dominated by stack allocation and runtime bookkeeping.

**What would happen if you didn't clean up between iterations?** Memory from the previous round's goroutines would still be in use, inflating the next round's measurements and potentially causing OOM for large counts.

### Intermediate Verification
```bash
go run main.go
```
Expected output pattern (values vary by machine):
```
Count        Launch Time     Per Goroutine   Goroutines/sec
------------------------------------------------------------
1000         1ms             1us             1000000
10000        8ms             800ns           1250000
100000       75ms            750ns           1333333
500000       380ms           760ns           1315789
1000000      780ms           780ns           1282051
```

## Step 2 -- Measuring Memory at Scale

Use `runtime.MemStats` to measure actual memory consumption per goroutine.

```go
package main

import (
	"fmt"
	"runtime"
	"strings"
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
	counts := []int{1_000, 10_000, 100_000, 500_000, 1_000_000}

	fmt.Printf("%-12s %-15s %-15s %-15s %-15s\n",
		"Count", "StackInUse", "HeapInUse", "Sys (Total)", "Per Goroutine")
	fmt.Println(strings.Repeat("-", 75))

	for _, count := range counts {
		runtime.GC()
		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)

		done := make(chan struct{})
		for i := 0; i < count; i++ {
			go func() {
				<-done
			}()
		}
		time.Sleep(50 * time.Millisecond)

		var after runtime.MemStats
		runtime.ReadMemStats(&after)

		stackDiff := after.StackInuse - before.StackInuse
		heapDiff := after.HeapInuse - before.HeapInuse
		sysDiff := after.Sys - before.Sys
		total := stackDiff + heapDiff
		perGoroutine := total / uint64(count)

		fmt.Printf("%-12d %-15s %-15s %-15s %-15s\n",
			count,
			formatBytes(stackDiff),
			formatBytes(heapDiff),
			formatBytes(sysDiff),
			formatBytes(perGoroutine),
		)

		close(done)
		time.Sleep(200 * time.Millisecond)
		runtime.GC()
	}
}
```

**What's happening here:** For each count, we measure three memory metrics. `StackInuse` is the dominant cost: stack memory actively used by goroutines. `HeapInuse` is for the goroutine descriptor structs and channel buffers. `Sys` is total memory obtained from the OS.

**Key insight:** At ~8 KB per goroutine (stack + heap), 1 million goroutines consume roughly 8 GB of memory. This is why unbounded goroutine creation is dangerous in production: under heavy load, you can OOM the process.

**What would happen if goroutines did real work instead of just blocking?** Memory would be higher because active stacks grow to fit their call depth. Our measurement shows the MINIMUM cost.

### Intermediate Verification
```bash
go run main.go
```
Expected output pattern:
```
Count        StackInUse      HeapInUse       Sys (Total)     Per Goroutine
---------------------------------------------------------------------------
1000         8.00 MB         0.50 MB         10.00 MB        8.50 KB
10000        80.00 MB        5.00 MB         90.00 MB        8.50 KB
100000       800.00 MB       50.00 MB        900.00 MB       8.50 KB
500000       3.91 GB         250.00 MB       4.50 GB         8.50 KB
1000000      7.81 GB         500.00 MB       9.00 GB         8.50 KB
```

## Step 3 -- Measuring GC Impact

Show how goroutine count affects garbage collection pause times.

```go
package main

import (
	"fmt"
	"runtime"
	"strings"
	"time"
)

func main() {
	counts := []int{1_000, 10_000, 100_000, 500_000}

	fmt.Printf("%-12s %-15s %-15s %-15s\n",
		"Count", "GC Pause", "Num GC", "Alloc Rate")
	fmt.Println(strings.Repeat("-", 60))

	for _, count := range counts {
		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)

		done := make(chan struct{})
		for i := 0; i < count; i++ {
			go func() {
				<-done
			}()
		}
		time.Sleep(50 * time.Millisecond)

		// Force a GC and measure how long it takes
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
}
```

**What's happening here:** We create goroutines, then force a GC cycle and measure how long it takes. GC pause time scales with goroutine count because the garbage collector must scan every goroutine's stack for pointers.

**Key insight:** GC pause time is the hidden cost of millions of goroutines. While individual goroutines are cheap, the GC must scan all their stacks during every cycle. At 500K goroutines, GC pauses can reach tens of milliseconds, which impacts latency-sensitive applications.

**What would happen in a real server with 500K goroutines?** Every GC cycle would pause for tens of milliseconds. For a service with p99 latency requirements under 10ms, this would be unacceptable.

### Intermediate Verification
```bash
go run main.go
```
Expected output (GC pause grows with count):
```
Count        GC Pause        Num GC          Alloc Rate
------------------------------------------------------------
1000         200us           2               0.50 MB
10000        1ms             3               5.00 MB
100000       15ms            5               50.00 MB
500000       75ms            8               250.00 MB
```

## Step 4 -- When NOT to Create Goroutines

Demonstrate that for CPU-bound work, more goroutines is NOT better. The optimal count is `runtime.NumCPU()`.

```go
package main

import (
	"fmt"
	"runtime"
	"strings"
	"time"
)

func main() {
	data := make([]int, 10_000_000)
	for i := range data {
		data[i] = i
	}

	sumSlice := func(slice []int) int64 {
		var sum int64
		for _, v := range slice {
			sum += int64(v)
		}
		return sum
	}

	goroutineCounts := []int{1, runtime.NumCPU(), 100, 1_000, 10_000}

	fmt.Printf("Summing %d elements:\n", len(data))
	fmt.Printf("%-15s %-15s %-15s\n", "Goroutines", "Wall-Clock", "vs Baseline")
	fmt.Println(strings.Repeat("-", 48))

	var baselineTime time.Duration

	for _, numG := range goroutineCounts {
		chunkSize := len(data) / numG
		if chunkSize == 0 {
			chunkSize = 1
		}

		start := time.Now()

		results := make(chan int64, numG)
		launched := 0

		for i := 0; i < len(data); i += chunkSize {
			end := i + chunkSize
			if end > len(data) {
				end = len(data)
			}
			chunk := data[i:end]
			launched++
			go func(s []int) {
				results <- sumSlice(s)
			}(chunk)
		}

		var total int64
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
	fmt.Println("Key insight: NumCPU goroutines is optimal for CPU-bound work.")
	fmt.Println("More goroutines add scheduling overhead without improving throughput.")
}
```

**What's happening here:** We sum 10 million integers using different numbers of goroutines. With 1 goroutine: no parallelism. With NumCPU: optimal parallelism. With 10,000: scheduling overhead dominates, making it SLOWER than 1 goroutine.

**Key insight:** For CPU-bound work, creating more goroutines than cores hurts performance. Each goroutine adds scheduling overhead (run queue insertion, context switching) without doing any additional useful work. The sweet spot is `runtime.NumCPU()`.

**What would happen with IO-bound work?** More goroutines WOULD help because they overlap wait times. The "too many goroutines" problem only affects CPU-bound workloads where goroutines compete for CPU time.

### Intermediate Verification
```bash
go run main.go
```
Expected output pattern:
```
Summing 10000000 elements:
Goroutines      Wall-Clock      vs Baseline
------------------------------------------------
1               12ms            1.00x
8               3ms             0.25x   (speedup from parallelism)
100             4ms             0.33x   (slight overhead)
1000            8ms             0.67x   (overhead growing)
10000           45ms            3.75x   (SLOWER than 1 goroutine!)
```

## Step 5 -- Building a Scalability Profile

Create a comprehensive report combining all measurements into a single table for your machine.

```go
package main

import (
	"fmt"
	"runtime"
	"strings"
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
	fmt.Println("Building a complete profile of goroutine costs on this machine...")
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

		launchStart := time.Now()
		for i := 0; i < count; i++ {
			go func() {
				<-done
			}()
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
		"Count", "Launch", "Stack", "Heap", "GC Pause", "KB/goroutine")
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
	fmt.Println("--- Guidelines for this machine ---")
	fmt.Printf("CPU cores:          %d\n", runtime.NumCPU())
	fmt.Printf("CPU-bound optimal:  %d goroutines (one per core)\n", runtime.NumCPU())
	fmt.Println("IO-bound:           1 goroutine per concurrent I/O operation")
	fmt.Println("Practical ceiling:  depends on RAM; ~100K-1M for most machines")
}
```

**What's happening here:** For each goroutine count, we measure launch time, stack memory, heap memory, and GC pause time. The result is a comprehensive profile specific to your machine.

**Key insight:** This gives you concrete numbers for YOUR hardware. Never assume goroutine costs -- measure them. A machine with 16 GB RAM can comfortably handle ~500K idle goroutines; a machine with 4 GB caps out at ~100K.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
Count      Launch       Stack        Heap         GC Pause     KB/goroutine
------------------------------------------------------------------------
100        0ms          800.00 KB    50.00 KB     50us         8.5
1000       1ms          8.00 MB     500.00 KB     200us        8.5
10000      8ms          80.00 MB    5.00 MB       2ms          8.5
100000     75ms         800.00 MB   50.00 MB      15ms         8.5

--- Guidelines for this machine ---
CPU cores:          8
CPU-bound optimal:  8 goroutines (one per core)
IO-bound:           1 goroutine per concurrent I/O operation
Practical ceiling:  depends on RAM; ~100K-1M for most machines
```

## Deep Dive: Bounded Concurrency with Semaphores

In production, never create goroutines without bounds. Use a semaphore pattern:

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	tasks := make([]int, 100)
	for i := range tasks {
		tasks[i] = i
	}

	// Semaphore: at most 10 goroutines run concurrently
	sem := make(chan struct{}, 10)
	done := make(chan struct{})

	for _, task := range tasks {
		sem <- struct{}{} // acquire slot (blocks when 10 are running)
		go func(id int) {
			defer func() { <-sem }() // release slot when done
			time.Sleep(50 * time.Millisecond)
			fmt.Printf("task %d done\n", id)
			done <- struct{}{}
		}(task)
	}

	// Collect all results
	for range tasks {
		<-done
	}
	fmt.Println("All tasks completed with bounded concurrency.")
}
```

**What's happening here:** The semaphore channel has a buffer of 10. Sending to it blocks when 10 goroutines are already running. Each goroutine releases its slot on completion. This ensures memory usage is bounded regardless of the task count.

**Key insight:** This is the production pattern for goroutine-per-task at scale. Unbounded creation is fine for small numbers (hundreds), but anything that could grow to thousands or more needs a bound.

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

**What happens:** Under heavy load, goroutine count grows without limit, eventually exhausting memory. Each goroutine + its buffer consumes ~10 KB.

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
		sem <- struct{}{} // blocks when at capacity
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

**Wrong thinking:** "Goroutines are free, so I'll create one per item in my 10M-row dataset."

**What happens:** 10M goroutines * ~8 KB each = ~80 GB of memory. OOM kill.

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

	// Fixed number of workers
	for i := 0; i < runtime.NumCPU(); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range work {
				_ = item * item // process item
			}
		}()
	}

	// Feed items to workers
	for _, item := range data {
		work <- item
	}
	close(work)
	wg.Wait()
	fmt.Printf("Processed %d items with %d workers\n", len(data), runtime.NumCPU())
}
```

### Not Measuring Before Deciding

**Wrong thinking:** "I'll use exactly 1000 goroutines because someone said that's a good number."

**What happens:** The optimal number depends on your workload, machine, and available memory. 1000 might be perfect for IO-bound tasks but terrible for CPU-bound ones.

**Fix:** Benchmark with different goroutine counts and measure actual performance. Use this exercise's approach to find the sweet spot.

## Verify What You Learned

Create a comprehensive benchmark that:
1. Finds the maximum number of goroutines your machine can create before consuming more than 2 GB of memory (use binary search, starting from 100K)
2. Prints the relationship between goroutine count and: launch time, memory usage, and GC pause
3. Recommends the practical ceiling for your machine with a 50% safety margin

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
- The practical ceiling is limited by RAM, not by the scheduler

## Reference
- [runtime.MemStats](https://pkg.go.dev/runtime#MemStats)
- [runtime.ReadMemStats](https://pkg.go.dev/runtime#ReadMemStats)
- [Go Blog: Go GC: Prioritizing low latency and simplicity](https://go.dev/blog/go15gc)
- [Concurrency in Go (Katherine Cox-Buday)](https://www.oreilly.com/library/view/concurrency-in-go/9781491941294/)
