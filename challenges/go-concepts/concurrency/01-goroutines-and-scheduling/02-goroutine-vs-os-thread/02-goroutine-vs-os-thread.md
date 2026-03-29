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

## Step 1 -- Counting Active Goroutines

Use `runtime.NumGoroutine()` to observe how the goroutine count changes as you create and destroy goroutines.

```go
package main

import (
	"fmt"
	"runtime"
	"time"
)

func main() {
	fmt.Printf("Goroutines at start: %d\n", runtime.NumGoroutine())

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			<-done
		}()
	}

	time.Sleep(10 * time.Millisecond)
	fmt.Printf("After launching 10:  %d\n", runtime.NumGoroutine())

	close(done)
	time.Sleep(10 * time.Millisecond)
	fmt.Printf("After releasing:     %d\n", runtime.NumGoroutine())
}
```

**What's happening here:** We create 10 goroutines that block on a channel. `runtime.NumGoroutine()` reports the exact count of goroutines alive at that moment. Closing the channel unblocks all of them simultaneously (a broadcast pattern).

**Key insight:** The initial count is 1 because `main` itself is a goroutine. After launching 10, the count is 11 (10 workers + main). After closing the channel, all workers exit and the count returns to 1.

**What would happen if you forgot `close(done)`?** The 10 goroutines would be leaked -- they would block on `<-done` forever, consuming memory until the process exits.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
Goroutines at start: 1
After launching 10:  11
After releasing:     1
```

## Step 2 -- Measuring Goroutine Memory

Use `runtime.MemStats` to measure the actual memory cost per goroutine at scale.

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
			<-done
		}()
	}
	time.Sleep(100 * time.Millisecond)

	runtime.GC()
	runtime.ReadMemStats(&after)

	totalBytes := after.Sys - before.Sys
	perGoroutine := totalBytes / count

	fmt.Printf("Goroutines created:  %d\n", count)
	fmt.Printf("Active goroutines:   %d\n", runtime.NumGoroutine())
	fmt.Printf("Memory increase:     %.2f MB (Sys)\n", float64(totalBytes)/(1024*1024))
	fmt.Printf("Per goroutine:       ~%d bytes (~%.1f KB)\n", perGoroutine, float64(perGoroutine)/1024)

	close(done)
	time.Sleep(200 * time.Millisecond)
}
```

**What's happening here:** We force garbage collection to get a clean baseline, create 100,000 goroutines, then measure the memory difference. The `Sys` field in `MemStats` represents total memory obtained from the OS, including stacks, heap, and runtime overhead.

**Key insight:** Each goroutine costs roughly 2-8 KB, not megabytes. The `Sys` metric may overcount because the OS allocates in pages, but the order of magnitude is correct: kilobytes, not megabytes.

**What would happen if you skipped `runtime.GC()`?** The baseline measurement would include garbage from previous allocations, making the per-goroutine calculation less accurate.

### Intermediate Verification
```bash
go run main.go
```
Expected output (values vary by system):
```
Goroutines created:  100000
Active goroutines:   100001
Memory increase:     ~200-800 MB (Sys includes reserves)
Per goroutine:       ~2000-8000 bytes (~2.0-8.0 KB)
```

## Step 3 -- Comparing with OS Thread Cost

Print a table showing the theoretical memory cost of goroutines vs OS threads at various scales.

```go
package main

import (
	"fmt"
	"strings"
)

func formatMB(mb float64) string {
	if mb >= 1024 {
		return fmt.Sprintf("%.1f GB", mb/1024)
	}
	return fmt.Sprintf("%.1f MB", mb)
}

func main() {
	goroutineStack := 8 * 1024       // 8 KB initial goroutine stack
	osThreadStack := 8 * 1024 * 1024 // 8 MB typical OS thread stack

	counts := []int{100, 1_000, 10_000, 100_000, 1_000_000}

	fmt.Printf("%-12s %-18s %-18s %-10s\n", "Count", "Goroutine Mem", "OS Thread Mem", "Ratio")
	fmt.Println(strings.Repeat("-", 62))

	for _, n := range counts {
		goroutineMB := float64(n*goroutineStack) / (1024 * 1024)
		threadMB := float64(n*osThreadStack) / (1024 * 1024)
		ratio := float64(osThreadStack) / float64(goroutineStack)

		fmt.Printf("%-12d %-18s %-18s 1:%.0f\n",
			n,
			formatMB(goroutineMB),
			formatMB(threadMB),
			ratio,
		)
	}

	fmt.Println()
	fmt.Println("At 1M goroutines: ~7.6 GB stack memory.")
	fmt.Println("At 1M OS threads: ~7,629 GB -- impossible on any current machine.")
}
```

**What's happening here:** We calculate theoretical memory for goroutines (8 KB each) vs OS threads (8 MB each) at scales from 100 to 1 million. The ratio is always 1:1024.

**Key insight:** This 1000x difference is why Go can do "one goroutine per connection" while languages with native threads need thread pools. A Java server with 10,000 connections needs ~80 GB of thread stack memory. A Go server needs ~80 MB.

**What would happen with different OS thread stack sizes?** Linux defaults to 8 MB, macOS to 512 KB-8 MB depending on the thread type. Even the smallest OS thread stack is still 10-100x larger than a goroutine.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
Count        Goroutine Mem      OS Thread Mem      Ratio
--------------------------------------------------------------
100          0.8 MB             800.0 MB           1:1024
1000         7.8 MB             7.8 GB             1:1024
10000        78.1 MB            78.1 GB            1:1024
100000       781.3 MB           781.3 GB           1:1024
1000000      7.6 GB             7629.4 GB          1:1024
```

## Step 4 -- Stack Size Observation with StackInuse

Measure `StackInuse` (more precise than `Sys`) to see actual stack memory consumed by idle goroutines.

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

	const count = 50_000
	done := make(chan struct{})

	for i := 0; i < count; i++ {
		go func() {
			<-done
		}()
	}
	time.Sleep(100 * time.Millisecond)

	runtime.ReadMemStats(&after)

	stackInUse := after.StackInuse - before.StackInuse
	perGoroutine := stackInUse / count

	fmt.Printf("Goroutines:          %d\n", count)
	fmt.Printf("Stack in use:        %d bytes (%.2f MB)\n", stackInUse, float64(stackInUse)/(1024*1024))
	fmt.Printf("Stack/goroutine:     %d bytes (%.1f KB)\n", perGoroutine, float64(perGoroutine)/1024)

	const osThreadDefault = 8_388_608
	fmt.Printf("\nOS thread default:   %d bytes (8 MB)\n", osThreadDefault)
	fmt.Printf("Ratio:               1 goroutine = 1/%.0f of an OS thread\n",
		float64(osThreadDefault)/float64(perGoroutine))

	osTotal := uint64(count) * osThreadDefault
	fmt.Printf("\nIf these were OS threads: %.2f GB of stack memory\n", float64(osTotal)/(1024*1024*1024))
	fmt.Printf("As goroutines:           %.2f MB of stack memory\n", float64(stackInUse)/(1024*1024))

	close(done)
	time.Sleep(200 * time.Millisecond)
}
```

**What's happening here:** `StackInuse` measures only the stack memory actively used by goroutines, excluding heap and runtime overhead. This gives a more precise per-goroutine stack measurement than `Sys`.

**Key insight:** The per-goroutine stack is typically 2,048-8,192 bytes for idle goroutines. This is the minimum allocation unit. Goroutines that do more work will have larger stacks (explored in exercise 04).

**What would happen if you used `StackSys` instead of `StackInuse`?** `StackSys` is memory *reserved* from the OS for stacks (may include unused pages). `StackInuse` is what goroutines are actually using. Always use `StackInuse` for measuring goroutine stack consumption.

### Intermediate Verification
```bash
go run main.go
```
Expected output (values vary):
```
Goroutines:          50000
Stack in use:        409600000 bytes (390.63 MB)
Stack/goroutine:     8192 bytes (8.0 KB)

OS thread default:   8388608 bytes (8 MB)
Ratio:               1 goroutine = 1/1024 of an OS thread

If these were OS threads: 390.63 GB of stack memory
As goroutines:           390.63 MB of stack memory
```

## Common Mistakes

### Assuming Goroutines Are Free

**Wrong thinking:** "Goroutines are cheap, so I'll create one for every tiny operation."

**What happens:** While goroutines are cheap, they are not free. Each one consumes memory and scheduler time. Creating millions of goroutines that contend for the same resource will cause performance degradation.

**Fix:** Use goroutines for genuinely concurrent work. For CPU-bound tasks, the optimal goroutine count is typically `runtime.NumCPU()`. For I/O-bound tasks, create goroutines based on the number of independent operations, not arbitrarily.

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

**What happens:** Goroutine leak. The goroutines stay alive consuming memory until the process exits.

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
1. Launches 1,000, 10,000, and 50,000 goroutines in separate rounds
2. For each round, measures the time to launch all goroutines and the `StackInuse` after launch
3. Prints a summary table showing count, launch time, and stack memory

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
- The lightweight nature of goroutines is what enables Go's "one-goroutine-per-task" pattern

## Reference
- [runtime.NumGoroutine](https://pkg.go.dev/runtime#NumGoroutine)
- [runtime.MemStats](https://pkg.go.dev/runtime#MemStats)
- [Go Blog: Goroutines are not threads](https://go.dev/blog/waza-talk)
- [Why goroutines instead of threads?](https://go.dev/doc/faq#goroutines)
