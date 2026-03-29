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

## Step 1 -- Natural Scheduling Points

Demonstrate where the scheduler gets a chance to switch goroutines. With GOMAXPROCS=1, only one goroutine runs at a time, making scheduling points visible.

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

	// Goroutine A: uses channel operations (each is a scheduling point)
	wg.Add(1)
	go func() {
		defer wg.Done()
		ch := make(chan int, 1)
		for i := 0; i < 5; i++ {
			ch <- i  // scheduling point: channel send
			_ = <-ch // scheduling point: channel receive
			fmt.Printf("A:%d ", i)
		}
	}()

	// Goroutine B: uses fmt.Printf (involves I/O syscall = scheduling point)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			fmt.Printf("B:%d ", i) // scheduling point: I/O syscall
		}
	}()

	wg.Wait()
	fmt.Println()
}
```

**What's happening here:** With GOMAXPROCS=1, only one goroutine can run at a time. Both goroutines hit scheduling points (channel ops for A, I/O for B), giving the scheduler opportunities to switch between them. The output will be interleaved.

**Key insight:** Natural scheduling points include: channel send/receive, mutex lock/unlock, `time.Sleep`, system calls (I/O), function calls with stack checks, and memory allocation. At each of these, the scheduler can context-switch.

**What would happen if goroutine A used a tight loop instead of channels?** Before Go 1.14, A would monopolize the P and B would never run. After Go 1.14, async preemption would eventually force A to yield.

### Intermediate Verification
```bash
go run main.go
```
Expected output (interleaved, exact order varies):
```
B:0 B:1 A:0 B:2 A:1 B:3 A:2 B:4 A:3 A:4
```

## Step 2 -- Using runtime.Gosched()

Show how `runtime.Gosched()` explicitly yields the processor, producing more predictable alternation.

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

	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			fmt.Printf("A:%d ", i)
			runtime.Gosched() // "I'm done for now, let others run"
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			fmt.Printf("B:%d ", i)
			runtime.Gosched() // yield after each iteration
		}
	}()

	wg.Wait()
	fmt.Println()
	fmt.Println("Gosched() produces more regular alternation (but NOT guaranteed).")
}
```

**What's happening here:** After each print, the goroutine calls `Gosched()` which places it at the back of the P's run queue. The scheduler picks the next goroutine from the front of the queue, which tends to produce alternating output.

**Key insight:** Gosched puts the current goroutine at the back of the queue, but the scheduler MAY not pick the goroutine you expect next. It is NOT a synchronization primitive. Never rely on Gosched for ordering guarantees.

**What would happen with GOMAXPROCS=NumCPU?** Both goroutines could run on separate Ps simultaneously, so Gosched would have no visible effect. The interleaving would be purely random.

### Intermediate Verification
```bash
go run main.go
```
Expected output (more regular but not guaranteed):
```
A:0 B:0 A:1 B:1 A:2 B:2 A:3 B:3 A:4 B:4
Gosched() produces more regular alternation (but NOT guaranteed).
```

## Step 3 -- Async Preemption (Go 1.14+)

Demonstrate that even a tight loop without scheduling points gets preempted. This was not possible before Go 1.14.

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

	// Goroutine A: tight loop with NO natural scheduling points.
	// Before Go 1.14, this would starve all other goroutines on this P.
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				atomic.AddInt64(&counter, 1)
			}
		}
	}()

	// Goroutine B: tries to run periodically.
	completed := make(chan bool)
	go func() {
		successes := 0
		for i := 0; i < 10; i++ {
			time.Sleep(10 * time.Millisecond)
			successes++
			fmt.Printf("  B got scheduled (iteration %d, counter=%d)\n",
				i, atomic.LoadInt64(&counter))
		}
		completed <- successes == 10
	}()

	success := <-completed
	close(done)

	if success {
		fmt.Println("  All 10 iterations of B completed!")
		fmt.Println("  Async preemption prevented A from starving B.")
	}
}
```

**What's happening here:** Goroutine A runs a tight loop with no channel ops, no I/O, no function calls that trigger stack checks. Despite this, goroutine B still gets to run because the runtime sends a SIGURG signal to force A to yield at safe points.

**Key insight:** Go 1.14+ uses OS signals to asynchronously preempt long-running goroutines. The runtime periodically (every ~10ms) checks if a goroutine has been running too long and sends a signal to interrupt it. This prevents starvation but does not guarantee fairness.

**What would happen on Go 1.13?** Goroutine A would monopolize the P indefinitely. B would never get scheduled. The program would appear to hang (B's sleep would never complete).

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
  B got scheduled (iteration 0, counter=12345678)
  B got scheduled (iteration 1, counter=24567890)
  ...
  B got scheduled (iteration 9, counter=98765432)
  All 10 iterations of B completed!
  Async preemption prevented A from starving B.
```

## Step 4 -- Comparing Scheduling Behavior

Build a comparison that measures how different scheduling patterns affect fairness across 3 workers competing for one P.

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

	// Experiment 1: tight loop (relies on async preemption)
	r1 := runExperiment("tight loop", func(id int, counter *int64, stop <-chan struct{}) {
		for {
			select {
			case <-stop:
				return
			default:
				atomic.AddInt64(counter, 1)
			}
		}
	})

	// Experiment 2: Gosched every 1000 iterations
	r2 := runExperiment("with Gosched", func(id int, counter *int64, stop <-chan struct{}) {
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

	// Experiment 3: channel operation every iteration
	r3 := runExperiment("with channel", func(id int, counter *int64, stop <-chan struct{}) {
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

	fmt.Printf("%-18s %15s %15s %15s %15s\n", "Pattern", "Worker 0", "Worker 1", "Worker 2", "Fairness")
	fmt.Println(strings.Repeat("-", 82))

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
		fmt.Printf("%-18s %15d %15d %15d %15.3f\n",
			r.name, r.counts[0], r.counts[1], r.counts[2], fairness)
	}

	fmt.Println()
	fmt.Println("Fairness 1.000 = perfectly equal distribution.")
	fmt.Println("Channel ops create most scheduling points -> best fairness.")
}
```

**What's happening here:** Three goroutines compete for one P (GOMAXPROCS=1). Each increments its own counter as fast as possible. We measure how evenly work distributes across the three workers after 100ms.

**Key insight:** Tight loops have worst fairness because the scheduler can only preempt at ~10ms intervals. Gosched every 1000 iterations improves fairness by yielding more frequently. Channel operations yield on every iteration, giving the best fairness but lower total throughput.

**What would happen with GOMAXPROCS=3?** All three workers could run simultaneously on separate Ps, and fairness would be perfect regardless of pattern. Scheduling patterns only matter when goroutines share Ps.

### Intermediate Verification
```bash
go run main.go
```
Expected output pattern:
```
Pattern            Worker 0        Worker 1        Worker 2        Fairness
----------------------------------------------------------------------------------
tight loop           45000000        1200000         1100000           0.421
with Gosched          8500000        8200000         8300000           0.998
with channel           350000         340000          345000           0.999
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

**What happens:** Gosched yields the processor but provides no memory ordering guarantees. This is a data race.

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

**What happens:** Gosched puts the goroutine at the back of the run queue, but the scheduler may not pick the goroutine you expect next. There could be other goroutines in the queue.

**Fix:** Never rely on execution order. Use explicit synchronization (channels, mutexes) when order matters.

## Verify What You Learned

Create a program that:
1. Runs 4 CPU-intensive goroutines with GOMAXPROCS=1
2. Measures the fairness (iteration count distribution) with three strategies: no yielding, Gosched every N iterations, and a channel ticker
3. Tests Gosched frequencies of 100, 1000, and 10000 iterations to find the best fairness/throughput tradeoff
4. Prints which strategy achieves the best balance

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
