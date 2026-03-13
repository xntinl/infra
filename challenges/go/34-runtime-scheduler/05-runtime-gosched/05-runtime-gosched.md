# 5. runtime.Gosched

<!--
difficulty: advanced
concepts: [runtime-gosched, cooperative-yield, scheduling-fairness, goroutine-yield, scheduler-hints]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [gmp-model, gomaxprocs-processor-binding, cooperative-vs-preemptive]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of cooperative and preemptive scheduling from exercise 04
- Familiarity with goroutine scheduling concepts

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** `runtime.Gosched()` to yield processor time to other goroutines
- **Differentiate** between `runtime.Gosched()` and other yielding mechanisms
- **Evaluate** when explicit yielding improves fairness or latency
- **Measure** the overhead of calling `runtime.Gosched()`

## Why runtime.Gosched Matters

Even with Go 1.14's async preemption, there are situations where explicit yielding is valuable. `runtime.Gosched()` tells the scheduler "I can pause here -- run something else if needed." This is useful in cooperative algorithms, spin-wait loops, and fairness-sensitive code where you want other goroutines to make progress without waiting for the preemption signal. It is a lightweight hint to the scheduler, not a sleep.

## Steps

### Step 1: Basic Gosched Behavior

Observe how `Gosched` affects goroutine interleaving on a single P:

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
	"time"
)

func withoutGosched() {
	fmt.Println("=== Without Gosched (GOMAXPROCS=1) ===")
	runtime.GOMAXPROCS(1)

	var wg sync.WaitGroup
	for id := 0; id < 3; id++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 5; i++ {
				fmt.Printf("  goroutine %d: iteration %d\n", id, i)
			}
		}(id)
	}
	wg.Wait()
}

func withGosched() {
	fmt.Println("\n=== With Gosched (GOMAXPROCS=1) ===")
	runtime.GOMAXPROCS(1)

	var wg sync.WaitGroup
	for id := 0; id < 3; id++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 5; i++ {
				fmt.Printf("  goroutine %d: iteration %d\n", id, i)
				runtime.Gosched() // Yield to other goroutines
			}
		}(id)
	}
	wg.Wait()
}
```

### Step 2: Gosched in Spin-Wait Loops

Use `Gosched` to reduce CPU waste while polling:

```go
func spinWaitWithGosched() {
	fmt.Println("\n=== Spin-Wait with Gosched ===")
	runtime.GOMAXPROCS(2)

	ready := false
	var mu sync.Mutex

	// Producer: set ready after some work
	go func() {
		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		ready = true
		mu.Unlock()
	}()

	// Consumer: spin-wait with Gosched
	iterations := 0
	start := time.Now()
	for {
		mu.Lock()
		r := ready
		mu.Unlock()
		if r {
			break
		}
		runtime.Gosched() // Yield instead of busy-spinning
		iterations++
	}
	elapsed := time.Since(start)
	fmt.Printf("  Spin-wait completed in %v after %d iterations\n", elapsed, iterations)
}
```

### Step 3: Measure Gosched Overhead

Quantify the cost of a `Gosched` call:

```go
func measureGoschedOverhead() {
	fmt.Println("\n=== Gosched Overhead ===")
	runtime.GOMAXPROCS(4)

	const iterations = 1_000_000

	// Baseline: empty loop
	start := time.Now()
	for i := 0; i < iterations; i++ {
		// no-op
	}
	baseline := time.Since(start)

	// With Gosched
	start = time.Now()
	for i := 0; i < iterations; i++ {
		runtime.Gosched()
	}
	withGS := time.Since(start)

	fmt.Printf("  Baseline (empty loop):  %v (%v/iter)\n",
		baseline, baseline/time.Duration(iterations))
	fmt.Printf("  With Gosched:           %v (%v/iter)\n",
		withGS, withGS/time.Duration(iterations))
	fmt.Printf("  Gosched overhead:       ~%v/call\n",
		(withGS-baseline)/time.Duration(iterations))
}
```

### Step 4: Gosched vs Channel vs Sleep

Compare different yielding strategies:

```go
func compareYieldStrategies() {
	fmt.Println("\n=== Yield Strategy Comparison ===")
	runtime.GOMAXPROCS(2)

	const iterations = 100_000

	// Strategy 1: runtime.Gosched
	start := time.Now()
	for i := 0; i < iterations; i++ {
		runtime.Gosched()
	}
	goschedTime := time.Since(start)

	// Strategy 2: channel send/receive
	ch := make(chan struct{}, 1)
	start = time.Now()
	for i := 0; i < iterations; i++ {
		ch <- struct{}{}
		<-ch
	}
	channelTime := time.Since(start)

	// Strategy 3: time.Sleep(0) -- OS-level yield
	start = time.Now()
	for i := 0; i < iterations/100; i++ { // Fewer iterations; Sleep is slow
		time.Sleep(0)
	}
	sleepTime := time.Since(start)

	fmt.Printf("  Gosched  (%d iters): %v (%v/iter)\n",
		iterations, goschedTime, goschedTime/time.Duration(iterations))
	fmt.Printf("  Channel  (%d iters): %v (%v/iter)\n",
		iterations, channelTime, channelTime/time.Duration(iterations))
	fmt.Printf("  Sleep(0) (%d iters): %v (%v/iter)\n",
		iterations/100, sleepTime, sleepTime/time.Duration(iterations/100))
}

func main() {
	withoutGosched()
	withGosched()
	spinWaitWithGosched()
	measureGoschedOverhead()
	compareYieldStrategies()
}
```

## Hints

- `runtime.Gosched()` does not suspend the goroutine -- it places it back at the end of the run queue
- On a single P, `Gosched` enables round-robin-like interleaving between goroutines
- `Gosched` is much cheaper than `time.Sleep(0)` because it stays in user-space
- In production code, channels or `sync.Cond` are usually better than spin-wait + `Gosched`
- `Gosched` is most useful in algorithms where you want fairness without the overhead of synchronization primitives

## Verification

```bash
go run main.go
```

Confirm that:
1. Without `Gosched`, goroutines tend to run their full loop before yielding
2. With `Gosched`, goroutines interleave on a single P
3. `Gosched` overhead is in the tens-of-nanoseconds range
4. `Gosched` is significantly cheaper than `time.Sleep(0)`

## What's Next

Continue to [06 - Goroutine Stack Growth](../06-goroutine-stack-growth/06-goroutine-stack-growth.md) to understand how Go manages goroutine stacks, starting small and growing dynamically.

## Summary

- `runtime.Gosched()` yields the processor, placing the current goroutine back on the run queue
- It enables cooperative interleaving without blocking or sleeping
- Gosched is a user-space operation, much cheaper than OS-level yields like `time.Sleep(0)`
- Useful in spin-wait loops, fairness-sensitive algorithms, and cooperative scheduling patterns
- Channels and sync primitives are generally preferred over Gosched for synchronization

## Reference

- [runtime.Gosched](https://pkg.go.dev/runtime#Gosched)
- [runtime package](https://pkg.go.dev/runtime)
- [Effective Go -- Goroutines](https://go.dev/doc/effective_go#goroutines)
