---
difficulty: basic
concepts: [goroutine lightweight nature, dynamic stack, OS thread comparison, runtime.NumGoroutine, runtime.MemStats]
tools: [go]
estimated_time: 20m
bloom_level: understand
prerequisites: [01-launching-goroutines]
---

# 2. Goroutine vs OS Thread


## Learning Objectives
After completing this exercise, you will be able to:
- **Explain** why goroutines are cheaper than OS threads
- **Measure** the memory footprint of goroutines at scale using `runtime.MemStats`
- **Use** `runtime.NumGoroutine()` to observe active goroutine counts
- **Compare** goroutine overhead with typical OS thread overhead using concrete numbers

## Why This Matters

One of Go's superpowers is the ability to run thousands or even millions of concurrent tasks without exhausting system resources. This is possible because goroutines are not OS threads. An OS thread typically reserves 1-8 MB of stack memory at creation, requires a kernel context switch to schedule, and consumes significant kernel resources. A goroutine, by contrast, starts with a stack of just 2-8 KB (the exact size depends on the Go version) and is scheduled entirely in user space by the Go runtime.

This difference is not academic. A Java application creating 10,000 threads would consume roughly 10-80 GB of stack memory alone, making it impractical on most machines. A Go application can comfortably create 10,000 goroutines using just 20-80 MB of stack memory. This is what enables the "one-goroutine-per-connection" pattern that makes Go so effective for network servers.

Understanding this cost difference helps you make informed architectural decisions. When you know a goroutine costs approximately the same as a small struct allocation, you stop worrying about creating them and start thinking in terms of concurrent tasks rather than thread pools.

## Step 1 -- Simulating 10K HTTP Connections

Imagine your service needs to handle 10,000 concurrent HTTP connections. Each connection reads a request, processes it, and sends a response. With goroutines, you can handle each connection independently. This step shows how `runtime.NumGoroutine()` tracks these simulated connections.

```go
package main

import (
	"fmt"
	"runtime"
	"time"
)

func handleConnection(id int, done <-chan struct{}) {
	// Phase 1: Read request
	time.Sleep(1 * time.Millisecond)
	// Phase 2: Process
	time.Sleep(1 * time.Millisecond)
	// Phase 3: Send response
	time.Sleep(1 * time.Millisecond)
	<-done
}

func main() {
	fmt.Printf("Goroutines at start: %d\n", runtime.NumGoroutine())

	const connections = 10_000
	done := make(chan struct{})

	for i := 0; i < connections; i++ {
		go handleConnection(i, done)
	}

	time.Sleep(50 * time.Millisecond)
	fmt.Printf("Active connections:  %d goroutines\n", runtime.NumGoroutine())

	close(done)
	time.Sleep(50 * time.Millisecond)
	fmt.Printf("After disconnect:    %d goroutines\n", runtime.NumGoroutine())
}
```

**What's happening here:** We simulate 10,000 concurrent HTTP connections, each in its own goroutine. Each connection goes through the typical read-process-respond lifecycle before blocking on a channel (simulating a keep-alive connection). `runtime.NumGoroutine()` reports the exact count of goroutines alive at that moment.

**Key insight:** The initial count is 1 because `main` itself is a goroutine. After launching 10,000, the count is 10,001. Closing the channel disconnects all simulated clients at once. In a real server, the operating system would be unable to create 10,000 OS threads on most machines, but 10,000 goroutines is trivial.

**What would happen if you forgot `close(done)`?** The 10,000 goroutines would be leaked -- they would block on `<-done` forever, consuming memory until the process exits. In a production server, goroutine leaks are a common source of memory exhaustion over time.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
Goroutines at start: 1
Active connections:  10001
After disconnect:    1
```

## Step 2 -- Measuring Memory per Connection

Use `runtime.MemStats` to measure how much memory 10,000 simulated connections actually consume. Then extrapolate what the same workload would cost with OS threads.

```go
package main

import (
	"fmt"
	"runtime"
	"time"
)

func main() {
	var before, after runtime.MemStats

	runtime.GC()
	runtime.ReadMemStats(&before)

	const count = 100_000
	done := make(chan struct{})

	for i := 0; i < count; i++ {
		go func() {
			// Simulate a connection handler: read, process, respond, wait
			time.Sleep(1 * time.Millisecond)
			<-done
		}()
	}
	time.Sleep(200 * time.Millisecond)

	runtime.GC()
	runtime.ReadMemStats(&after)

	totalBytes := after.Sys - before.Sys
	perGoroutine := totalBytes / count

	fmt.Printf("Simulated connections:  %d\n", count)
	fmt.Printf("Active goroutines:     %d\n", runtime.NumGoroutine())
	fmt.Printf("Memory increase:       %.2f MB (Sys)\n", float64(totalBytes)/(1024*1024))
	fmt.Printf("Per connection:        ~%d bytes (~%.1f KB)\n", perGoroutine, float64(perGoroutine)/1024)

	fmt.Println()
	osThreadMem := uint64(count) * 8 * 1024 * 1024
	fmt.Printf("Same connections as OS threads (8MB stack each):\n")
	fmt.Printf("  Would require:       %.2f GB\n", float64(osThreadMem)/(1024*1024*1024))
	fmt.Printf("  Goroutine advantage: ~%dx less memory\n", osThreadMem/totalBytes)

	close(done)
	time.Sleep(200 * time.Millisecond)
}
```

**What's happening here:** We force garbage collection to get a clean baseline, create 100,000 goroutines simulating connection handlers, then measure the memory difference. The `Sys` field in `MemStats` represents total memory obtained from the OS, including stacks, heap, and runtime overhead.

**Key insight:** Each goroutine costs roughly 2-8 KB, not megabytes. For the same 100,000 concurrent connections, OS threads would need ~800 GB of stack memory -- clearly impossible on any machine. This is why Go servers can handle C10K (and beyond) without thread pools.

**What would happen if you skipped `runtime.GC()`?** The baseline measurement would include garbage from previous allocations, making the per-goroutine calculation less accurate.

### Intermediate Verification
```bash
go run main.go
```
Expected output (values vary by system):
```
Simulated connections:  100000
Active goroutines:     100001
Memory increase:       ~200-800 MB (Sys includes reserves)
Per connection:        ~2000-8000 bytes (~2.0-8.0 KB)

Same connections as OS threads (8MB stack each):
  Would require:       781.25 GB
  Goroutine advantage: ~1000x less memory
```

## Step 3 -- Server Capacity Comparison Table

Print a table showing how many concurrent connections a server can handle with goroutines vs OS threads, given common server RAM sizes.

```go
package main

import (
	"fmt"
	"strings"
)

func formatCount(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func main() {
	goroutineCost := 8 * 1024       // 8 KB per goroutine
	osThreadCost := 8 * 1024 * 1024 // 8 MB per OS thread

	serverRAMs := []int{1, 4, 8, 16, 32, 64} // GB

	fmt.Println("=== Maximum Concurrent Connections by Server RAM ===")
	fmt.Println("(assuming 50% of RAM available for connection stacks)")
	fmt.Println()
	fmt.Printf("%-10s %-22s %-22s %-10s\n", "RAM", "Go (goroutines)", "Threads (8MB stack)", "Advantage")
	fmt.Println(strings.Repeat("-", 68))

	for _, gb := range serverRAMs {
		availableBytes := gb * 1024 * 1024 * 1024 / 2 // 50% for stacks
		goConns := availableBytes / goroutineCost
		threadConns := availableBytes / osThreadCost
		advantage := goConns / threadConns

		fmt.Printf("%-10s %-22s %-22s %-10s\n",
			fmt.Sprintf("%d GB", gb),
			formatCount(goConns),
			formatCount(threadConns),
			fmt.Sprintf("%dx", advantage),
		)
	}

	fmt.Println()
	fmt.Println("This is why Go servers use goroutine-per-connection while")
	fmt.Println("Java/C++ servers need thread pools with connection queuing.")
}
```

**What's happening here:** We calculate how many concurrent connections different server configurations can sustain with goroutines vs OS threads. A modest 4 GB server can handle 262K goroutines but only 256 OS threads. This drives real architectural decisions.

**Key insight:** This 1000x difference is why Go can do "one goroutine per connection" while languages with native threads need thread pools. A Java server with 10,000 connections needs ~80 GB of thread stack memory. A Go server needs ~80 MB. This directly impacts your infrastructure costs.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Maximum Concurrent Connections by Server RAM ===
(assuming 50% of RAM available for connection stacks)

RAM        Go (goroutines)        Threads (8MB stack)    Advantage
--------------------------------------------------------------------
1 GB       65.5K                  64                     1024x
4 GB       262.1K                 256                    1024x
8 GB       524.3K                 512                    1024x
16 GB      1.0M                   1.0K                   1024x
32 GB      2.1M                   2.0K                   1024x
64 GB      4.2M                   4.1K                   1024x
```

## Step 4 -- Live Stack Measurement with StackInuse

Measure `StackInuse` (more precise than `Sys`) to see actual stack memory consumed by simulated connection handlers at different scales.

```go
package main

import (
	"fmt"
	"runtime"
	"strings"
	"time"
)

func main() {
	scales := []int{1_000, 5_000, 10_000, 50_000}

	fmt.Println("=== Goroutine Stack Memory at Different Connection Scales ===")
	fmt.Printf("%-12s %-15s %-18s %-15s\n",
		"Connections", "Stack/conn", "Total Stack", "OS Thread Equiv")
	fmt.Println(strings.Repeat("-", 65))

	for _, count := range scales {
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)

		done := make(chan struct{})
		for i := 0; i < count; i++ {
			go func() {
				<-done
			}()
		}
		time.Sleep(100 * time.Millisecond)

		runtime.ReadMemStats(&after)

		stackInUse := after.StackInuse - before.StackInuse
		perGoroutine := stackInUse / uint64(count)
		osEquiv := uint64(count) * 8 * 1024 * 1024

		fmt.Printf("%-12d %-15s %-18s %-15s\n",
			count,
			fmt.Sprintf("%d B (%.1f KB)", perGoroutine, float64(perGoroutine)/1024),
			fmt.Sprintf("%.2f MB", float64(stackInUse)/(1024*1024)),
			fmt.Sprintf("%.2f GB", float64(osEquiv)/(1024*1024*1024)),
		)

		close(done)
		time.Sleep(200 * time.Millisecond)
		runtime.GC()
	}

	fmt.Println()
	fmt.Println("StackInuse measures ONLY stack memory, not heap or runtime overhead.")
	fmt.Println("This is the most accurate metric for goroutine stack consumption.")
}
```

**What's happening here:** `StackInuse` measures only the stack memory actively used by goroutines, excluding heap and runtime overhead. This gives a more precise per-goroutine stack measurement than `Sys`. At each scale, we compare with what OS threads would require.

**Key insight:** The per-goroutine stack is typically 2,048-8,192 bytes for idle goroutines. This is the minimum allocation unit. Goroutines that do more work will have larger stacks (explored in exercise 04). The takeaway for capacity planning: each idle connection handler costs about 8 KB, so 100K connections costs about 800 MB of stack alone.

**What would happen if you used `StackSys` instead of `StackInuse`?** `StackSys` is memory *reserved* from the OS for stacks (may include unused pages). `StackInuse` is what goroutines are actually using. Always use `StackInuse` for measuring goroutine stack consumption.

### Intermediate Verification
```bash
go run main.go
```
Expected output (values vary):
```
=== Goroutine Stack Memory at Different Connection Scales ===
Connections  Stack/conn      Total Stack        OS Thread Equiv
-----------------------------------------------------------------
1000         8192 B (8.0 KB) 8.00 MB            7.81 GB
5000         8192 B (8.0 KB) 40.00 MB           39.06 GB
10000        8192 B (8.0 KB) 80.00 MB           78.13 GB
50000        8192 B (8.0 KB) 400.00 MB          390.63 GB
```

## Common Mistakes

### Assuming Goroutines Are Free

**Wrong thinking:** "Goroutines are cheap, so I'll create one for every tiny operation."

**What happens:** While goroutines are cheap, they are not free. Each one consumes memory and scheduler time. Creating millions of goroutines that contend for the same resource will cause performance degradation. In a production server, unbounded goroutine creation during a traffic spike can OOM the process.

**Fix:** Use goroutines for genuinely concurrent work. For CPU-bound tasks, the optimal goroutine count is typically `runtime.NumCPU()`. For I/O-bound tasks, create goroutines based on the number of independent operations, but set upper bounds.

### Forgetting to Release Goroutines

**Wrong -- complete program:**
```go
package main

import (
	"fmt"
	"runtime"
)

func main() {
	done := make(chan struct{})
	for i := 0; i < 10000; i++ {
		go func() {
			<-done // blocks forever if done is never closed
		}()
	}
	// forgot to close(done) -- 10,000 goroutines leaked
	fmt.Printf("Leaked goroutines: %d\n", runtime.NumGoroutine()-1)
}
```

**What happens:** Goroutine leak. In a real server, this pattern appears when connection handlers block on a channel that is never closed. Over hours of operation, memory grows until the process is OOM-killed.

**Correct -- complete program:**
```go
package main

import (
	"fmt"
	"runtime"
	"time"
)

func main() {
	done := make(chan struct{})
	for i := 0; i < 10000; i++ {
		go func() {
			<-done
		}()
	}
	time.Sleep(10 * time.Millisecond)
	fmt.Printf("Before cleanup: %d goroutines\n", runtime.NumGoroutine())

	close(done) // release all goroutines
	time.Sleep(10 * time.Millisecond)
	fmt.Printf("After cleanup:  %d goroutines\n", runtime.NumGoroutine())
}
```

### Confusing StackInuse with Sys

**Wrong:**
```go
totalBytes := after.Sys - before.Sys // includes heap, runtime, AND stack
perGoroutine := totalBytes / count   // inflated number
```

**Fix:** Use `StackInuse` when you want to know stack-specific consumption:
```go
stackBytes := after.StackInuse - before.StackInuse // just stacks
perGoroutine := stackBytes / count                  // accurate stack cost
```

## Verify What You Learned

Create a benchmark function that:
1. Launches 1,000, 10,000, and 50,000 goroutines in separate rounds, each simulating an HTTP connection handler (read, process, respond phases with small delays)
2. For each round, measures the time to launch all goroutines and the `StackInuse` after launch
3. Prints a summary table showing count, launch time, stack memory, and what the equivalent OS thread cost would be

**Hint:** Use `time.Now()` and `time.Since()` for timing, and remember to GC between rounds.

## What's Next
Continue to [03-gmp-model-in-action](../03-gmp-model-in-action/03-gmp-model-in-action.md) to understand how the Go scheduler maps goroutines onto OS threads.

## Summary
- Goroutines start with a 2-8 KB stack; OS threads start with 1-8 MB
- This 1000x difference enables Go to run millions of concurrent goroutines
- `runtime.NumGoroutine()` reports the current count of active goroutines
- `runtime.MemStats.StackInuse` measures actual stack memory consumed
- `runtime.MemStats.Sys` includes all memory from the OS (stacks + heap + overhead)
- Goroutines are cheap but not free; leaked goroutines still consume resources
- The lightweight nature of goroutines is what enables Go's "one-goroutine-per-connection" server pattern, handling 10K+ concurrent connections on a single machine

## Reference
- [runtime.NumGoroutine](https://pkg.go.dev/runtime#NumGoroutine)
- [runtime.MemStats](https://pkg.go.dev/runtime#MemStats)
- [Go Blog: Goroutines are not threads](https://go.dev/blog/waza-talk)
- [Why goroutines instead of threads?](https://go.dev/doc/faq#goroutines)
