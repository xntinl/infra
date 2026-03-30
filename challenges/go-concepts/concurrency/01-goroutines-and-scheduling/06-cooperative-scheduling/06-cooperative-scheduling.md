---
difficulty: intermediate
concepts: [scheduling points, runtime.Gosched, preemption, Go 1.14+ async preemption, tight loops, fairness]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [01-launching-goroutines, 03-gmp-model-in-action, 05-gomaxprocs-and-parallelism]
---

# 6. Cooperative Scheduling


## Learning Objectives
After completing this exercise, you will be able to:
- **Identify** natural scheduling points in Go code (channel ops, syscalls, function calls)
- **Use** `runtime.Gosched()` to yield the processor explicitly
- **Explain** how Go 1.14+ async preemption prevents goroutine starvation
- **Analyze** scheduling fairness under different patterns

## Why Scheduling Matters

Go's scheduler is primarily cooperative: goroutines voluntarily yield the processor at well-defined scheduling points. These include channel operations, mutex locks, system calls, `time.Sleep`, and function calls (where the runtime checks if the stack needs growth). At each of these points, the scheduler has an opportunity to switch to a different goroutine.

Before Go 1.14, a goroutine running a tight computational loop with no function calls and no scheduling points could monopolize a P indefinitely, starving other goroutines. Go 1.14 introduced asynchronous preemption using OS signals (SIGURG on Unix): the runtime periodically sends a signal to running goroutines, forcing them to yield even in tight loops.

Understanding scheduling behavior helps you write code that plays well with the scheduler. While async preemption prevents outright starvation, cooperative yielding through natural scheduling points leads to smoother, more predictable concurrent behavior. In rare cases, you may need `runtime.Gosched()` to explicitly yield.

## Step 1 -- Task Scheduler: Natural Scheduling Points

Imagine you are building a task scheduler that runs multiple background jobs on a single core. Some jobs do IO (logging, metrics collection), and some do CPU-intensive work (computing prime numbers for cryptographic key generation). With GOMAXPROCS=1, we can see exactly when the scheduler switches between them.

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
)

func main() {
	runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(runtime.NumCPU())

	var wg sync.WaitGroup

	// Job A: IO-heavy metrics collector (channel ops are scheduling points)
	wg.Add(1)
	go func() {
		defer wg.Done()
		ch := make(chan int, 1)
		for i := 0; i < 5; i++ {
			ch <- i  // scheduling point: channel send
			_ = <-ch // scheduling point: channel receive
			fmt.Printf("metrics-collector:%d ", i)
		}
	}()

	// Job B: Log shipper (fmt.Printf involves IO syscall = scheduling point)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			fmt.Printf("log-shipper:%d ", i) // scheduling point: I/O syscall
		}
	}()

	wg.Wait()
	fmt.Println()
}
```

**What's happening here:** With GOMAXPROCS=1, only one goroutine can run at a time. Both jobs hit scheduling points (channel ops for the metrics collector, I/O for the log shipper), giving the scheduler opportunities to switch between them. The output will be interleaved.

**Key insight:** Natural scheduling points include: channel send/receive, mutex lock/unlock, `time.Sleep`, system calls (I/O), function calls with stack checks, and memory allocation. At each of these, the scheduler can context-switch. In a real task scheduler, this means IO-heavy jobs naturally yield to others without any explicit coordination.

**What would happen if the metrics collector used a tight loop instead of channels?** Before Go 1.14, it would monopolize the P and the log shipper would never run. After Go 1.14, async preemption would eventually force it to yield, but with much worse fairness than channel-based yielding.

### Intermediate Verification
```bash
go run main.go
```
Expected output (interleaved, exact order varies):
```
log-shipper:0 log-shipper:1 metrics-collector:0 log-shipper:2 metrics-collector:1 log-shipper:3 metrics-collector:2 log-shipper:4 metrics-collector:3 metrics-collector:4
```

## Step 2 -- CPU-Heavy Job Starving Others: runtime.Gosched()

When one job computes prime numbers for key generation (CPU-intensive, no natural scheduling points), it can starve other jobs. `runtime.Gosched()` explicitly yields control so the scheduler can run other goroutines.

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
	"time"
)

func isPrime(n int) bool {
	if n < 2 {
		return false
	}
	for i := 2; i*i <= n; i++ {
		if n%i == 0 {
			return false
		}
	}
	return true
}

func main() {
	runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(runtime.NumCPU())

	var wg sync.WaitGroup

	// Run the experiment twice: without and with Gosched
	for _, useGosched := range []bool{false, true} {
		label := "WITHOUT Gosched"
		if useGosched {
			label = "WITH Gosched"
		}
		fmt.Printf("--- %s ---\n", label)

		start := time.Now()
		healthChecksCompleted := 0

		wg.Add(2)

		// Job 1: CPU-heavy prime computation (key generation simulation)
		go func() {
			defer wg.Done()
			primes := 0
			for n := 2; n < 100_000; n++ {
				if isPrime(n) {
					primes++
				}
				if useGosched && n%1000 == 0 {
					runtime.Gosched() // "I've done enough, let others run"
				}
			}
			fmt.Printf("  prime-generator: found %d primes\n", primes)
		}()

		// Job 2: Periodic health check (needs to run regularly)
		go func() {
			defer wg.Done()
			for i := 0; i < 5; i++ {
				time.Sleep(10 * time.Millisecond)
				healthChecksCompleted++
				fmt.Printf("  health-check: tick %d (%v)\n", i, time.Since(start).Round(time.Millisecond))
			}
		}()

		wg.Wait()
		fmt.Printf("  Health checks completed: %d/5 | Total: %v\n\n",
			healthChecksCompleted, time.Since(start).Round(time.Millisecond))
	}
}
```

**What's happening here:** Without Gosched, the prime generator dominates the single P. Health checks rely on `time.Sleep` (which is a scheduling point), but the prime generator runs without natural scheduling points between primes. With Gosched every 1000 iterations, the prime generator periodically yields, giving the health checker regular access to the P.

**Key insight:** Gosched puts the current goroutine at the back of the queue, but the scheduler MAY not pick the goroutine you expect next. It is NOT a synchronization primitive. However, for CPU-intensive background tasks that share a processor with latency-sensitive work (health checks, heartbeats), periodic Gosched calls dramatically improve fairness.

**What would happen with GOMAXPROCS=NumCPU?** Both jobs could run on separate Ps simultaneously, so Gosched would have no visible effect. The problem only manifests when goroutines share a P.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
--- WITHOUT Gosched ---
  health-check: tick 0 (10ms)
  health-check: tick 1 (20ms)
  ...
  prime-generator: found 9592 primes
  Health checks completed: 5/5 | Total: 85ms

--- WITH Gosched ---
  health-check: tick 0 (10ms)
  health-check: tick 1 (20ms)
  ...
  prime-generator: found 9592 primes
  Health checks completed: 5/5 | Total: 90ms
```

## Step 3 -- Async Preemption (Go 1.14+): No More Starvation

Demonstrate that even a tight CPU loop without scheduling points gets preempted. Before Go 1.14, this pattern would cause complete starvation of other goroutines.

```go
package main

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"time"
)

func main() {
	runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(runtime.NumCPU())

	var counter int64
	done := make(chan struct{})

	// Goroutine A: tight prime-sieve loop with NO natural scheduling points.
	// Before Go 1.14, this would starve all other goroutines on this P.
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				// Pure CPU work: no function calls, no IO, no channels
				atomic.AddInt64(&counter, 1)
			}
		}
	}()

	// Goroutine B: periodic monitoring that must run on schedule.
	completed := make(chan bool)
	go func() {
		successes := 0
		for i := 0; i < 10; i++ {
			time.Sleep(10 * time.Millisecond)
			successes++
			fmt.Printf("  monitor tick %d (computations so far: %d)\n",
				i, atomic.LoadInt64(&counter))
		}
		completed <- successes == 10
	}()

	success := <-completed
	close(done)

	if success {
		fmt.Println()
		fmt.Println("  All 10 monitoring ticks completed on schedule.")
		fmt.Println("  Async preemption (Go 1.14+) prevented the CPU-heavy")
		fmt.Println("  goroutine from starving the monitor.")
		fmt.Println()
		fmt.Println("  Before Go 1.14, the monitor would NEVER run. The CPU loop")
		fmt.Println("  would hold the P forever, and your monitoring would be blind.")
	}
}
```

**What's happening here:** Goroutine A runs a tight loop with no channel ops, no I/O, no function calls that trigger stack checks. Despite this, goroutine B still gets to run because the runtime sends a SIGURG signal to force A to yield at safe points.

**Key insight:** Go 1.14+ uses OS signals to asynchronously preempt long-running goroutines. The runtime periodically (every ~10ms) checks if a goroutine has been running too long and sends a signal to interrupt it. In production, this prevents a single runaway computation from killing your service's health checks, metrics collection, or heartbeats.

**What would happen on Go 1.13?** Goroutine A would monopolize the P indefinitely. The monitor would never get scheduled. In production, this means your service appears healthy from the outside (process is running) but is actually unresponsive (no goroutine can make progress).

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
  monitor tick 0 (computations so far: 12345678)
  monitor tick 1 (computations so far: 24567890)
  ...
  monitor tick 9 (computations so far: 98765432)

  All 10 monitoring ticks completed on schedule.
  Async preemption (Go 1.14+) prevented the CPU-heavy
  goroutine from starving the monitor.

  Before Go 1.14, the monitor would NEVER run. The CPU loop
  would hold the P forever, and your monitoring would be blind.
```

## Step 4 -- Scheduling Fairness: Comparing Patterns for Worker Jobs

Build a comparison that measures how different scheduling patterns affect fairness across 3 worker jobs competing for one P. This simulates a scenario where your task scheduler must share CPU time fairly between background jobs.

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
	runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(runtime.NumCPU())

	type result struct {
		name   string
		counts [3]int64
	}

	runExperiment := func(name string, workFn func(id int, counter *int64, stop <-chan struct{})) result {
		var counters [3]int64
		stop := make(chan struct{})

		for i := 0; i < 3; i++ {
			go workFn(i, &counters[i], stop)
		}

		time.Sleep(100 * time.Millisecond)
		close(stop)
		time.Sleep(10 * time.Millisecond)

		return result{name: name, counts: counters}
	}

	// Experiment 1: tight loop (relies only on async preemption)
	r1 := runExperiment("tight loop (preemption only)", func(id int, counter *int64, stop <-chan struct{}) {
		for {
			select {
			case <-stop:
				return
			default:
				atomic.AddInt64(counter, 1)
			}
		}
	})

	// Experiment 2: Gosched every 1000 iterations (cooperative)
	r2 := runExperiment("Gosched every 1K iters", func(id int, counter *int64, stop <-chan struct{}) {
		for {
			select {
			case <-stop:
				return
			default:
				atomic.AddInt64(counter, 1)
				if atomic.LoadInt64(counter)%1000 == 0 {
					runtime.Gosched()
				}
			}
		}
	})

	// Experiment 3: channel operation every iteration (most cooperative)
	r3 := runExperiment("channel yield per iter", func(id int, counter *int64, stop <-chan struct{}) {
		ch := make(chan struct{}, 1)
		ch <- struct{}{}
		for {
			select {
			case <-stop:
				return
			default:
				<-ch
				atomic.AddInt64(counter, 1)
				ch <- struct{}{}
			}
		}
	})

	fmt.Println("=== Scheduling Fairness: 3 Workers Sharing 1 P for 100ms ===")
	fmt.Println()
	fmt.Printf("%-30s %12s %12s %12s %10s\n", "Strategy", "Worker 0", "Worker 1", "Worker 2", "Fairness")
	fmt.Println(strings.Repeat("-", 80))

	for _, r := range []result{r1, r2, r3} {
		total := r.counts[0] + r.counts[1] + r.counts[2]
		var fairness float64
		if total > 0 {
			avg := float64(total) / 3.0
			variance := 0.0
			for _, c := range r.counts {
				diff := float64(c) - avg
				variance += diff * diff
			}
			fairness = 1.0 - (variance / (avg * avg * 3))
		}
		fmt.Printf("%-30s %12d %12d %12d %10.3f\n",
			r.name, r.counts[0], r.counts[1], r.counts[2], fairness)
	}

	fmt.Println()
	fmt.Println("Fairness 1.000 = perfectly equal work distribution.")
	fmt.Println("Tight loops: worst fairness (one worker hogs the P for ~10ms at a time)")
	fmt.Println("Channel ops: best fairness but lowest throughput (context switch overhead)")
	fmt.Println("Gosched:     good balance of fairness and throughput")
}
```

**What's happening here:** Three goroutines compete for one P (GOMAXPROCS=1). Each increments its own counter as fast as possible. We measure how evenly work distributes across the three workers after 100ms.

**Key insight:** Tight loops have the worst fairness because the scheduler can only preempt at ~10ms intervals. Gosched every 1000 iterations improves fairness by yielding more frequently. Channel operations yield on every iteration, giving the best fairness but lower total throughput. For a real task scheduler, the Gosched approach gives you the best balance: high throughput with acceptable fairness.

**What would happen with GOMAXPROCS=3?** All three workers could run simultaneously on separate Ps, and fairness would be perfect regardless of strategy. Scheduling patterns only matter when goroutines share Ps -- which happens when you have more goroutines than cores.

### Intermediate Verification
```bash
go run main.go
```
Expected output pattern:
```
=== Scheduling Fairness: 3 Workers Sharing 1 P for 100ms ===

Strategy                         Worker 0     Worker 1     Worker 2   Fairness
--------------------------------------------------------------------------------
tight loop (preemption only)     45000000      1200000      1100000      0.421
Gosched every 1K iters            8500000      8200000      8300000      0.998
channel yield per iter              350000       340000       345000      0.999

Fairness 1.000 = perfectly equal work distribution.
Tight loops: worst fairness (one worker hogs the P for ~10ms at a time)
Channel ops: best fairness but lowest throughput (context switch overhead)
Gosched:     good balance of fairness and throughput
```

## Common Mistakes

### Relying on Gosched for Synchronization

**Wrong -- complete program:**
```go
package main

import (
	"fmt"
	"runtime"
)

func main() {
	var data int

	go func() {
		data = 42
		runtime.Gosched() // does NOT guarantee main sees data=42
	}()

	runtime.Gosched()
	fmt.Println(data) // DATA RACE! May print 0 or 42
}
```

**What happens:** Gosched yields the processor but provides no memory ordering guarantees. This is a data race. In production, this creates intermittent bugs that are nearly impossible to reproduce.

**Correct -- use a channel:**
```go
package main

import "fmt"

func main() {
	ch := make(chan int)

	go func() {
		ch <- 42 // channel send provides happens-before guarantee
	}()

	data := <-ch // guaranteed to see 42
	fmt.Println(data)
}
```

### Adding Gosched Everywhere for "Performance"

**Wrong -- complete program:**
```go
package main

import (
	"fmt"
	"runtime"
)

func main() {
	for i := 0; i < 1000; i++ {
		fmt.Println(i)
		runtime.Gosched() // unnecessary: fmt.Println already yields (I/O syscall)
	}
}
```

**What happens:** Each Gosched introduces a context switch. In this code, fmt.Println already yields because it does I/O. Adding Gosched doubles the context switch overhead for no benefit.

**Fix:** Only use Gosched when you have a measured scheduling problem. In nearly all code, natural scheduling points (I/O, channel ops) provide sufficient yielding.

### Assuming Cooperative Scheduling Means Deterministic Order

**Wrong thinking:** "If I call Gosched after every iteration, goroutines will alternate perfectly."

**What happens:** Gosched puts the goroutine at the back of the run queue, but the scheduler may not pick the goroutine you expect next. There could be other goroutines in the queue (GC workers, runtime goroutines).

**Fix:** Never rely on execution order. Use explicit synchronization (channels, mutexes) when order matters.

## Verify What You Learned

Create a program that simulates a task scheduler with:
1. 4 CPU-intensive background jobs with GOMAXPROCS=1
2. Measures the fairness (iteration count distribution) with three strategies: no yielding, Gosched every N iterations, and a channel ticker
3. Tests Gosched frequencies of 100, 1000, and 10000 iterations to find the best fairness/throughput tradeoff
4. Prints which strategy achieves the best balance for running background jobs alongside latency-sensitive health checks

## What's Next
Continue to [07-goroutine-per-request](../07-goroutine-per-request/07-goroutine-per-request.md) to apply goroutines to a practical pattern: one goroutine per independent task.

## Summary
- Go's scheduler is primarily cooperative: goroutines yield at channel ops, syscalls, mutex locks, and function calls
- `runtime.Gosched()` explicitly yields the processor, placing the goroutine at the back of the run queue
- Go 1.14+ introduced async preemption using OS signals (SIGURG on Unix) to prevent goroutine starvation
- Async preemption prevents starvation but cooperative scheduling still produces smoother, fairer behavior
- `Gosched` is NOT a synchronization primitive -- it provides no memory ordering guarantees
- In practice, most code has enough natural scheduling points; explicit Gosched is rarely needed
- With GOMAXPROCS=1, scheduling patterns matter most; with multiple Ps, workers run in parallel regardless

## Reference
- [runtime.Gosched](https://pkg.go.dev/runtime#Gosched)
- [Go 1.14 Release Notes: Goroutine preemption](https://go.dev/doc/go1.14#runtime)
- [Proposal: Non-cooperative Goroutine Preemption](https://github.com/golang/proposal/blob/master/design/24543-non-cooperative-preemption.md)
