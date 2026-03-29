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
- **Analyze** scheduler behavior using runtime statistics

## Why the GMP Model

Go's scheduler uses a model with three key entities: G (goroutine), M (machine/OS thread), and P (processor). Understanding this model transforms goroutines from "magic lightweight threads" into a well-understood scheduling system.

**G (Goroutine):** The unit of work. Contains the stack, instruction pointer, and other scheduling state. Gs are what your code creates with the `go` keyword. You can have millions of Gs.

**M (Machine):** An OS thread. The Go runtime creates Ms as needed to execute Gs. An M must be attached to a P to run Go code. Ms can be blocked in syscalls without holding a P. The runtime creates new Ms when existing ones are blocked.

**P (Processor):** A logical processor that acts as a resource context. Each P has a local run queue of Gs waiting to execute. The number of Ps is set by `GOMAXPROCS` and determines the maximum parallelism. A P must be acquired by an M before it can execute any G.

The key insight is that when an M blocks on a syscall (like file I/O or a CGo call), it releases its P so another M can pick it up and continue running Gs. This is why the number of Ms can grow beyond the number of Ps -- blocked Ms need to be replaced to maintain throughput.

## Step 1 -- Observing P Count

Use `runtime.GOMAXPROCS` to read and set the number of logical processors.

```go
package main

import (
	"fmt"
	"runtime"
)

func main() {
	// GOMAXPROCS(0) reads without changing -- the idiomatic read-only call.
	currentP := runtime.GOMAXPROCS(0)
	numCPU := runtime.NumCPU()

	fmt.Printf("Number of CPUs:    %d\n", numCPU)
	fmt.Printf("GOMAXPROCS (Ps):   %d\n", currentP)
	fmt.Printf("Default: GOMAXPROCS == NumCPU (since Go 1.5)\n")

	// GOMAXPROCS returns the PREVIOUS value, then sets the new one.
	old := runtime.GOMAXPROCS(2)
	fmt.Printf("\nSet GOMAXPROCS to 2 (was %d)\n", old)
	fmt.Printf("Current GOMAXPROCS: %d\n", runtime.GOMAXPROCS(0))

	// Always restore the original value.
	runtime.GOMAXPROCS(old)
	fmt.Printf("Restored GOMAXPROCS to %d\n", old)
}
```

**What's happening here:** `GOMAXPROCS(0)` is a read-only call. `GOMAXPROCS(n)` for n > 0 sets the value and returns the previous one. Since Go 1.5, the default equals `runtime.NumCPU()`.

**Key insight:** GOMAXPROCS controls how many Ps exist, which limits how many goroutines can execute Go code simultaneously. It does NOT limit how many goroutines can exist.

**What would happen if you set GOMAXPROCS(1)?** Only one P would exist, so only one goroutine could run Go code at any given instant. All other goroutines would wait in the run queue. They are still concurrent (can make progress independently) but not parallel (cannot run simultaneously).

### Intermediate Verification
```bash
go run main.go
```
Expected output (CPU count varies by machine):
```
Number of CPUs:    8
GOMAXPROCS (Ps):   8
Default: GOMAXPROCS == NumCPU (since Go 1.5)

Set GOMAXPROCS to 2 (was 8)
Current GOMAXPROCS: 2
Restored GOMAXPROCS to 8
```

## Step 2 -- Observing G Count Under Load

Create goroutines in waves and observe `runtime.NumGoroutine()` grow and shrink.

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

	waveSizes := []int{100, 500, 1000}

	// Launch waves: G count grows cumulatively
	for wave, size := range waveSizes {
		for i := 0; i < size; i++ {
			go func(b <-chan struct{}) {
				<-b
			}(barriers[wave])
		}
		time.Sleep(10 * time.Millisecond)
		fmt.Printf("After wave %d (+%d goroutines): total G = %d\n",
			wave+1, size, runtime.NumGoroutine())
	}

	// Release in reverse order to show G count decreasing
	for i := len(barriers) - 1; i >= 0; i-- {
		close(barriers[i])
		time.Sleep(10 * time.Millisecond)
		fmt.Printf("After releasing wave %d: total G = %d\n",
			i+1, runtime.NumGoroutine())
	}
}
```

**What's happening here:** Each wave adds goroutines to the total count. Barriers keep them alive (blocking on channel receive). Closing barriers in reverse order shows the count decreasing wave by wave.

**Key insight:** The G count can grow to millions while P stays fixed at GOMAXPROCS. Gs are just data structures in the runtime's run queues; Ps are the execution slots.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
After wave 1 (+100 goroutines): total G = 101
After wave 2 (+500 goroutines): total G = 601
After wave 3 (+1000 goroutines): total G = 1601
After releasing wave 3: total G = 601
After releasing wave 2: total G = 101
After releasing wave 1: total G = 1
```

## Step 3 -- Demonstrating M Growth During Syscalls

When goroutines make blocking syscalls, the runtime creates additional OS threads (Ms) to keep other goroutines running.

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

	fmt.Printf("GOMAXPROCS: %d\n", runtime.GOMAXPROCS(0))
	fmt.Printf("Goroutines before: %d\n", runtime.NumGoroutine())

	var wg sync.WaitGroup
	const numBlockers = 20

	for i := 0; i < numBlockers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			f, err := os.CreateTemp("", "gmp-demo-*")
			if err != nil {
				return
			}
			name := f.Name()
			f.Write([]byte("blocking syscall demo\n"))
			f.Sync() // blocking syscall: forces the M into the kernel
			f.Close()
			os.Remove(name)
		}(i)
	}

	time.Sleep(5 * time.Millisecond)
	fmt.Printf("Goroutines during blocking ops: %d\n", runtime.NumGoroutine())
	fmt.Println("(OS threads may exceed GOMAXPROCS=2 during syscalls)")

	wg.Wait()
	fmt.Printf("Goroutines after completion: %d\n", runtime.NumGoroutine())
}
```

**What's happening here:** With GOMAXPROCS=2, only 2 Ps exist. But we launch 20 goroutines that each do file I/O. When a goroutine's M blocks in `f.Sync()` (a kernel-level fsync call), the M releases its P. The runtime creates a new M to pick up the freed P and keep running other goroutines. This is why M count can exceed P count.

**Key insight:** P limits parallelism of Go code execution. M is the actual OS thread. During heavy syscall usage, M count floats upward as the runtime compensates for blocked threads.

**What would happen without the hand-off mechanism?** With 2 Ps and 2 Ms, as soon as both Ms enter syscalls, all other goroutines would be stuck. The hand-off ensures throughput is maintained.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
GOMAXPROCS: 2
Goroutines before: 1
Goroutines during blocking ops: 21
(OS threads may exceed GOMAXPROCS=2 during syscalls)
Goroutines after completion: 1
```

## Step 4 -- Building a GMP Status Reporter

Create a utility that prints GMP-related runtime stats at labeled points during execution.

```go
package main

import (
	"fmt"
	"runtime"
	"time"
)

func gmpStatus(label string) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	fmt.Printf("[%-25s] G=%-6d P=%-3d NumCPU=%-3d StackInUse=%.1fKB  Sys=%.1fMB\n",
		label,
		runtime.NumGoroutine(),
		runtime.GOMAXPROCS(0),
		runtime.NumCPU(),
		float64(m.StackInuse)/1024,
		float64(m.Sys)/(1024*1024),
	)
}

func main() {
	gmpStatus("initial")

	done := make(chan struct{})

	for i := 0; i < 500; i++ {
		go func() { <-done }()
	}
	time.Sleep(10 * time.Millisecond)
	gmpStatus("500 goroutines blocked")

	for i := 0; i < 500; i++ {
		go func() { <-done }()
	}
	time.Sleep(10 * time.Millisecond)
	gmpStatus("1000 goroutines blocked")

	close(done)
	time.Sleep(50 * time.Millisecond)
	gmpStatus("all released")
}
```

**What's happening here:** At each snapshot, we print G count (changes), P count (stays constant), and memory metrics. This demonstrates that G and stack memory scale together while P remains fixed.

**Key insight:** P is constant (set once at startup). G can grow to millions. StackInuse correlates with G count because each goroutine has its own stack. This status reporter is a useful debugging tool for production systems.

### Intermediate Verification
```bash
go run main.go
```
Expected output (memory values vary):
```
[initial                  ] G=1      P=8   NumCPU=8   StackInUse=32.0KB  Sys=7.2MB
[500 goroutines blocked   ] G=501    P=8   NumCPU=8   StackInUse=4128.0KB  Sys=15.1MB
[1000 goroutines blocked  ] G=1001   P=8   NumCPU=8   StackInUse=8224.0KB  Sys=20.3MB
[all released             ] G=1      P=8   NumCPU=8   StackInUse=32.0KB  Sys=20.3MB
```

## Deep Dive: How P Acquisition Works

The GMP model has a subtle but important mechanism: work stealing. When a P's local run queue is empty, the M holding that P tries to steal work from other Ps' run queues. This ensures that idle Ps do not sit around while other Ps have goroutines waiting.

The scheduling cycle for an M looks like this:
1. Check local run queue on the current P
2. Check the global run queue (shared across all Ps)
3. Check the network poller (for goroutines waiting on I/O)
4. Attempt to steal work from another random P's run queue

This is why you do not need to manually distribute goroutines across Ps. The scheduler balances the load automatically.

## Common Mistakes

### Confusing GOMAXPROCS with Goroutine Limit

**Wrong thinking:** "Setting GOMAXPROCS(4) means only 4 goroutines can exist."

**What happens:** GOMAXPROCS sets the number of Ps (logical processors), not the number of Gs. You can have millions of goroutines with GOMAXPROCS=1 -- they just run one at a time.

**Fix:** GOMAXPROCS controls parallelism (how many goroutines run simultaneously), not concurrency (how many goroutines exist).

### Assuming M Count Equals P Count

**Wrong thinking:** "There are always exactly GOMAXPROCS OS threads."

**What happens:** The runtime creates additional Ms when goroutines block in syscalls. The M count can grow well beyond GOMAXPROCS.

**Fix:** Think of P as the parallelism limit for Go code execution. Ms are the actual OS threads, and their count floats based on demand.

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

**What happens:** `GOMAXPROCS` is a process-wide setting. Changing it in a request handler affects all goroutines in the process, not just the one handling this request.

**Correct approach:** Set GOMAXPROCS once at startup (via environment variable `GOMAXPROCS=N` or in `main`) or let the default apply. Never change it at runtime in business logic.

## Verify What You Learned

Create a program that:
1. Prints the initial GMP status
2. Launches 100 CPU-bound goroutines (tight loop), prints GMP status
3. Launches 100 I/O-bound goroutines (temp file writes), prints GMP status
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
