---
difficulty: intermediate
concepts: [G (goroutine), M (machine/OS thread), P (processor/logical processor), runtime.GOMAXPROCS, runtime.NumGoroutine, scheduler internals]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [01-launching-goroutines, 02-goroutine-vs-os-thread]
---

# 3. GMP Model in Action


## Learning Objectives
After completing this exercise, you will be able to:
- **Describe** the three components of Go's GMP scheduler model
- **Observe** how G, M, and P counts change during program execution
- **Demonstrate** that M (OS thread) count can exceed P count during blocking syscalls
- **Analyze** scheduler behavior using runtime statistics with CPU-bound and IO-bound workloads

## Why the GMP Model

Go's scheduler uses a model with three key entities: G (goroutine), M (machine/OS thread), and P (processor). Understanding this model transforms goroutines from "magic lightweight threads" into a well-understood scheduling system.

**G (Goroutine):** The unit of work. Contains the stack, instruction pointer, and other scheduling state. Gs are what your code creates with the `go` keyword. You can have millions of Gs.

**M (Machine):** An OS thread. The Go runtime creates Ms as needed to execute Gs. An M must be attached to a P to run Go code. Ms can be blocked in syscalls without holding a P. The runtime creates new Ms when existing ones are blocked.

**P (Processor):** A logical processor that acts as a resource context. Each P has a local run queue of Gs waiting to execute. The number of Ps is set by `GOMAXPROCS` and determines the maximum parallelism. A P must be acquired by an M before it can execute any G.

The key insight is that when an M blocks on a syscall (like file I/O or a CGo call), it releases its P so another M can pick it up and continue running Gs. This is why the number of Ms can grow beyond the number of Ps -- blocked Ms need to be replaced to maintain throughput.

## Step 1 -- Observing P Count with CPU and IO Workloads

In a real system, you often need to understand how GOMAXPROCS affects your specific workload. A data pipeline that computes checksums is CPU-bound. A service that reads configuration files is IO-bound. This step shows how P count relates to both.

```go
package main

import (
	"crypto/sha256"
	"fmt"
	"runtime"
	"time"
)

func main() {
	numCPU := runtime.NumCPU()
	currentP := runtime.GOMAXPROCS(0) // read without changing

	fmt.Printf("Number of CPUs:    %d\n", numCPU)
	fmt.Printf("GOMAXPROCS (Ps):   %d\n", currentP)
	fmt.Printf("Default: GOMAXPROCS == NumCPU (since Go 1.5)\n\n")

	// CPU-bound: computing checksums benefits from more Ps
	data := make([]byte, 1024*1024) // 1 MB block
	for i := range data {
		data[i] = byte(i)
	}

	for _, procs := range []int{1, 2, numCPU} {
		runtime.GOMAXPROCS(procs)
		start := time.Now()

		done := make(chan struct{})
		for w := 0; w < 4; w++ {
			go func() {
				for i := 0; i < 50; i++ {
					sha256.Sum256(data)
				}
				done <- struct{}{}
			}()
		}
		for w := 0; w < 4; w++ {
			<-done
		}

		fmt.Printf("  GOMAXPROCS=%-2d  4 workers x 50 checksums: %v\n",
			procs, time.Since(start).Round(time.Millisecond))
	}

	runtime.GOMAXPROCS(numCPU)
	fmt.Printf("\nWith more Ps, CPU-bound checksum work runs faster because\n")
	fmt.Printf("multiple goroutines execute simultaneously on different cores.\n")
}
```

**What's happening here:** `GOMAXPROCS(0)` is a read-only call. We compute SHA-256 checksums of a 1 MB data block -- a realistic CPU-bound operation similar to verifying file integrity or processing uploads. With GOMAXPROCS=1, all workers share one P and take turns. With GOMAXPROCS=NumCPU, they run in parallel.

**Key insight:** GOMAXPROCS controls how many Ps exist, which limits how many goroutines can execute Go code simultaneously. It does NOT limit how many goroutines can exist.

**What would happen if you set GOMAXPROCS(1)?** Only one P would exist, so the four checksum workers would run sequentially on a single core. Total time would be 4x longer than with 4 Ps.

### Intermediate Verification
```bash
go run main.go
```
Expected output (CPU count and times vary):
```
Number of CPUs:    8
GOMAXPROCS (Ps):   8
Default: GOMAXPROCS == NumCPU (since Go 1.5)

  GOMAXPROCS=1   4 workers x 50 checksums: 680ms
  GOMAXPROCS=2   4 workers x 50 checksums: 350ms
  GOMAXPROCS=8   4 workers x 50 checksums: 175ms

With more Ps, CPU-bound checksum work runs faster because
multiple goroutines execute simultaneously on different cores.
```

## Step 2 -- G Count Under Load: Simulated File Processing Pipeline

In a data pipeline, you might process files in stages: read, transform, write. Each stage creates goroutines. This step shows how G count grows and shrinks as stages complete.

```go
package main

import (
	"fmt"
	"runtime"
	"time"
)

func main() {
	barriers := make([]chan struct{}, 3)
	for i := range barriers {
		barriers[i] = make(chan struct{})
	}

	stages := []struct {
		name  string
		count int
	}{
		{"file-readers", 100},
		{"checksum-workers", 500},
		{"write-uploaders", 200},
	}

	// Launch pipeline stages: G count grows cumulatively
	for i, stage := range stages {
		for j := 0; j < stage.count; j++ {
			go func(b <-chan struct{}) {
				<-b
			}(barriers[i])
		}
		time.Sleep(10 * time.Millisecond)
		fmt.Printf("After launching %-20s (+%d): total G = %d\n",
			stage.name, stage.count, runtime.NumGoroutine())
	}

	// Complete stages in reverse: writers finish first, then processors, then readers
	stageNames := []string{"write-uploaders", "checksum-workers", "file-readers"}
	for i := len(barriers) - 1; i >= 0; i-- {
		close(barriers[i])
		time.Sleep(10 * time.Millisecond)
		fmt.Printf("After completing %-20s:       total G = %d\n",
			stageNames[len(barriers)-1-i], runtime.NumGoroutine())
	}
}
```

**What's happening here:** We simulate a three-stage file processing pipeline. Each stage adds goroutines to the total count. Barriers keep them alive until the stage completes. Completing stages in reverse order shows the G count decreasing as each stage drains.

**Key insight:** The G count can grow to millions while P stays fixed at GOMAXPROCS. Gs are just data structures in the runtime's run queues; Ps are the execution slots. In a real pipeline, understanding this helps you reason about memory usage and backpressure.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
After launching file-readers          (+100): total G = 101
After launching checksum-workers      (+500): total G = 601
After launching write-uploaders       (+200): total G = 801
After completing write-uploaders      :       total G = 601
After completing checksum-workers     :       total G = 101
After completing file-readers         :       total G = 1
```

## Step 3 -- M Growth During Blocking I/O

When goroutines perform blocking system calls (file reads, DNS lookups, CGo), the runtime creates additional OS threads (Ms) to keep other goroutines running. This is critical for understanding why IO-heavy services sometimes have more OS threads than expected.

```go
package main

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"
)

func main() {
	old := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(old)

	fmt.Printf("GOMAXPROCS (Ps): %d\n", runtime.GOMAXPROCS(0))
	fmt.Printf("Goroutines before: %d\n\n", runtime.NumGoroutine())

	var wg sync.WaitGroup
	const numReaders = 20

	// Simulate 20 goroutines reading config files simultaneously.
	// Each file read is a blocking syscall that causes the M to enter the kernel.
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			f, err := os.CreateTemp("", "config-reader-*")
			if err != nil {
				return
			}
			name := f.Name()

			// Write simulated config data
			f.Write([]byte(`{"service": "auth", "port": 8080, "timeout": "30s"}` + "\n"))
			f.Sync() // blocking syscall: forces the M into the kernel
			f.Close()
			os.Remove(name)
		}(i)
	}

	time.Sleep(5 * time.Millisecond)
	fmt.Printf("During file I/O:   %d goroutines active\n", runtime.NumGoroutine())
	fmt.Println("With GOMAXPROCS=2, only 2 Ps exist, but the runtime creates")
	fmt.Println("additional OS threads (Ms) when goroutines block in syscalls.")
	fmt.Println("This is the M hand-off mechanism: blocked M releases its P")
	fmt.Println("so a new M can continue running other goroutines.")

	wg.Wait()
	fmt.Printf("\nAfter completion:  %d goroutines\n", runtime.NumGoroutine())
}
```

**What's happening here:** With GOMAXPROCS=2, only 2 Ps exist. But we launch 20 goroutines that each do file I/O (creating temp files, writing, fsyncing). When a goroutine's M blocks in `f.Sync()` (a kernel-level fsync call), the M releases its P. The runtime creates a new M to pick up the freed P and keep running other goroutines.

**Key insight:** P limits parallelism of Go code execution. M is the actual OS thread. During heavy syscall usage (file reads, DNS lookups, database connections via CGo), M count floats upward as the runtime compensates for blocked threads. In production, you might see 30+ OS threads on a service with GOMAXPROCS=8 if it does heavy file or network I/O.

**What would happen without the hand-off mechanism?** With 2 Ps and 2 Ms, as soon as both Ms enter syscalls, all other goroutines would be stuck. Your config file readers would serialize, and your service startup would take 10x longer.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
GOMAXPROCS (Ps): 2
Goroutines before: 1

During file I/O:   21 goroutines active
With GOMAXPROCS=2, only 2 Ps exist, but the runtime creates
additional OS threads (Ms) when goroutines block in syscalls.
This is the M hand-off mechanism: blocked M releases its P
so a new M can continue running other goroutines.

After completion:  1 goroutines
```

## Step 4 -- GMP Status Reporter for a Mixed Workload

Build a utility that prints GMP-related stats at labeled points during a mixed CPU-bound and IO-bound workload. This is the kind of instrumentation you would add to debug scheduler behavior in a real application.

```go
package main

import (
	"crypto/sha256"
	"fmt"
	"runtime"
	"time"
)

func gmpStatus(label string) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	fmt.Printf("[%-30s] G=%-6d P=%-3d NumCPU=%-3d StackInUse=%.1fKB  Sys=%.1fMB\n",
		label,
		runtime.NumGoroutine(),
		runtime.GOMAXPROCS(0),
		runtime.NumCPU(),
		float64(m.StackInuse)/1024,
		float64(m.Sys)/(1024*1024),
	)
}

func main() {
	gmpStatus("startup")

	done := make(chan struct{})

	// Phase 1: IO-bound goroutines (simulated file reads)
	for i := 0; i < 200; i++ {
		go func() {
			time.Sleep(5 * time.Millisecond) // simulated read latency
			<-done
		}()
	}
	time.Sleep(20 * time.Millisecond)
	gmpStatus("200 IO-bound goroutines")

	// Phase 2: Add CPU-bound goroutines (checksum computation)
	data := make([]byte, 64*1024)
	for i := 0; i < 50; i++ {
		go func() {
			for j := 0; j < 10; j++ {
				sha256.Sum256(data)
			}
			<-done
		}()
	}
	time.Sleep(50 * time.Millisecond)
	gmpStatus("200 IO + 50 CPU goroutines")

	// Phase 3: Add more IO-bound goroutines
	for i := 0; i < 300; i++ {
		go func() {
			time.Sleep(5 * time.Millisecond)
			<-done
		}()
	}
	time.Sleep(20 * time.Millisecond)
	gmpStatus("500 IO + 50 CPU goroutines")

	close(done)
	time.Sleep(50 * time.Millisecond)
	gmpStatus("all released")
}
```

**What's happening here:** At each snapshot, we print G count (changes), P count (stays constant), and memory metrics. IO-bound goroutines simulate file/network reads; CPU-bound goroutines compute checksums. This demonstrates that G and stack memory scale together while P remains fixed.

**Key insight:** P is constant (set once at startup). G can grow to millions. StackInuse correlates with G count because each goroutine has its own stack. This status reporter pattern is directly useful for production instrumentation -- adding periodic GMP snapshots to your metrics helps diagnose goroutine leaks and memory growth.

### Intermediate Verification
```bash
go run main.go
```
Expected output (memory values vary):
```
[startup                       ] G=1      P=8   NumCPU=8   StackInUse=32.0KB  Sys=7.2MB
[200 IO-bound goroutines       ] G=201    P=8   NumCPU=8   StackInUse=1664.0KB  Sys=12.1MB
[200 IO + 50 CPU goroutines    ] G=251    P=8   NumCPU=8   StackInUse=2080.0KB  Sys=14.5MB
[500 IO + 50 CPU goroutines    ] G=551    P=8   NumCPU=8   StackInUse=4544.0KB  Sys=18.3MB
[all released                  ] G=1      P=8   NumCPU=8   StackInUse=32.0KB  Sys=18.3MB
```

## Deep Dive: How P Acquisition Works

The GMP model has a subtle but important mechanism: work stealing. When a P's local run queue is empty, the M holding that P tries to steal work from other Ps' run queues. This ensures that idle Ps do not sit around while other Ps have goroutines waiting.

The scheduling cycle for an M looks like this:
1. Check local run queue on the current P
2. Check the global run queue (shared across all Ps)
3. Check the network poller (for goroutines waiting on I/O)
4. Attempt to steal work from another random P's run queue

This is why you do not need to manually distribute goroutines across Ps. The scheduler balances the load automatically. In a real server handling both CPU-intensive checksum verification and IO-heavy file serving, the scheduler ensures all cores stay busy without any manual tuning.

## Common Mistakes

### Confusing GOMAXPROCS with Goroutine Limit

**Wrong thinking:** "Setting GOMAXPROCS(4) means only 4 goroutines can exist."

**What happens:** GOMAXPROCS sets the number of Ps (logical processors), not the number of Gs. You can have millions of goroutines with GOMAXPROCS=1 -- they just run one at a time.

**Fix:** GOMAXPROCS controls parallelism (how many goroutines run simultaneously), not concurrency (how many goroutines exist).

### Assuming M Count Equals P Count

**Wrong thinking:** "There are always exactly GOMAXPROCS OS threads."

**What happens:** The runtime creates additional Ms when goroutines block in syscalls. The M count can grow well beyond GOMAXPROCS. A service doing heavy file I/O might have 50+ OS threads despite GOMAXPROCS=8.

**Fix:** Think of P as the parallelism limit for Go code execution. Ms are the actual OS threads, and their count floats based on demand from blocking syscalls.

### Using runtime.GOMAXPROCS in Production Code

**Wrong -- complete program:**
```go
package main

import (
	"fmt"
	"net/http"
	"runtime"
)

func handler(w http.ResponseWriter, r *http.Request) {
	runtime.GOMAXPROCS(1) // terrible: affects the ENTIRE process
	fmt.Fprintf(w, "hello")
}

func main() {
	http.HandleFunc("/", handler)
	http.ListenAndServe(":8080", nil)
}
```

**What happens:** `GOMAXPROCS` is a process-wide setting. Changing it in a request handler affects all goroutines in the process, not just the one handling this request. Every other connection handler suddenly runs on a single core.

**Correct approach:** Set GOMAXPROCS once at startup (via environment variable `GOMAXPROCS=N` or in `main`) or let the default apply. Never change it at runtime in business logic.

## Verify What You Learned

Create a program that:
1. Prints the initial GMP status
2. Launches 100 CPU-bound goroutines (computing SHA-256 checksums of data blocks), prints GMP status
3. Launches 100 IO-bound goroutines (simulated file reads with `time.Sleep`), prints GMP status
4. Launches both simultaneously, prints GMP status
5. Explains in comments why the behavior differs between phases

## What's Next
Continue to [04-goroutine-stack-growth](../04-goroutine-stack-growth/04-goroutine-stack-growth.md) to understand how goroutine stacks grow dynamically.

## Summary
- **G** (goroutine): lightweight unit of work, created with `go`
- **M** (machine): OS thread that executes goroutine code
- **P** (processor): logical processor; `GOMAXPROCS` sets the count
- A P must be held by an M to execute Go code
- When an M blocks in a syscall, it releases its P for another M
- The M count can exceed P count during heavy syscall usage
- `GOMAXPROCS` controls parallelism, not the number of goroutines
- Default `GOMAXPROCS` equals `runtime.NumCPU()` since Go 1.5
- The scheduler uses work stealing to balance load across Ps

## Reference
- [Go Scheduler Design Doc](https://docs.google.com/document/d/1TTj4T2JO42uD5ID9e89oa0sLKhJYD0Y_kqxDv3I3XMw)
- [runtime.GOMAXPROCS](https://pkg.go.dev/runtime#GOMAXPROCS)
- [runtime.NumGoroutine](https://pkg.go.dev/runtime#NumGoroutine)
- [Scalable Go Scheduler (Dmitry Vyukov)](https://docs.google.com/document/d/1TTj4T2JO42uD5ID9e89oa0sLKhJYD0Y_kqxDv3I3XMw)
