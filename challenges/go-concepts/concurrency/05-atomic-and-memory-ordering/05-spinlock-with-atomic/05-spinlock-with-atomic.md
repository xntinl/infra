---
difficulty: advanced
concepts: [spinlock, CompareAndSwapInt32, busy-wait, CPU waste, sync.Mutex comparison, TryLock]
tools: [go]
estimated_time: 35m
bloom_level: analyze
---

# 5. Spinlock with Atomic CAS

## Learning Objectives
After completing this exercise, you will be able to:
- **Build** a working spinlock from `atomic.CompareAndSwapInt32`
- **Explain** why spinlocks burn CPU while waiting and measure the waste
- **Compare** spinlock vs `sync.Mutex` under varying contention and articulate why Mutex wins in Go
- **Identify** the narrow scenarios where spinlocks make sense (very short critical sections, real-time systems)

## Why Build a Spinlock (and Why Not Use One)

A spinlock is the simplest possible mutex: `Lock()` spins in a CAS loop until it acquires the lock, `Unlock()` atomically releases it. No OS involvement, no goroutine parking, no scheduler interaction. When the lock is uncontended, a spinlock acquires in a single CAS -- faster than any other locking mechanism.

Building one from scratch solidifies your understanding of CAS and reveals a fundamental trade-off: a spinlock wastes CPU while waiting. The goroutine runs a tight loop instead of sleeping. In Go, where the runtime multiplexes thousands of goroutines onto a few OS threads, a spinning goroutine holds an OS thread hostage and prevents other goroutines from running. With `GOMAXPROCS=1`, this causes deadlock.

`sync.Mutex` is a hybrid: it spins briefly (optimistic fast path) then parks the goroutine with the OS scheduler (pessimistic slow path). You get spinlock speed when the lock is quickly available, and sleeping efficiency when it is not. For virtually all Go code, `sync.Mutex` is the right tool.

This exercise is educational. You should understand how spinlocks work so you can recognize them in systems code and understand why Go's standard library avoids exposing them.

## Step 1 -- Build a Spinlock and Prove Correctness

Implement `Lock()`, `Unlock()`, and `TryLock()` using `atomic.CompareAndSwapInt32`:

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

type SpinLock struct {
	state int32 // 0 = unlocked, 1 = locked
}

func (s *SpinLock) Lock() {
	for !atomic.CompareAndSwapInt32(&s.state, 0, 1) {
		// CAS failed: lock is held by another goroutine.
		// Yield so the lock holder can run and eventually Unlock.
		runtime.Gosched()
	}
}

func (s *SpinLock) Unlock() {
	atomic.StoreInt32(&s.state, 0)
}

func (s *SpinLock) TryLock() bool {
	return atomic.CompareAndSwapInt32(&s.state, 0, 1)
}

func main() {
	var lock SpinLock
	var counter int64
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				lock.Lock()
				counter++
				lock.Unlock()
			}
		}()
	}

	wg.Wait()
	fmt.Printf("Expected: 100000, Got: %d\n", counter)

	// Demonstrate TryLock
	lock.Lock()
	fmt.Printf("TryLock while held: %v (expected false)\n", lock.TryLock())
	lock.Unlock()
	fmt.Printf("TryLock while free: %v (expected true)\n", lock.TryLock())
	lock.Unlock()
}
```

### Verification
```bash
go run -race main.go
```
Counter is exactly 100,000. TryLock returns false when held, true when free. No race warnings.

## Step 2 -- Measure CPU Waste Under Contention

Show that spinlocks burn CPU while waiting. Compare wall-clock time AND CPU time between spinlock and mutex under high contention:

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

type SpinLock struct {
	state int32
}

func (s *SpinLock) Lock() {
	for !atomic.CompareAndSwapInt32(&s.state, 0, 1) {
		runtime.Gosched()
	}
}

func (s *SpinLock) Unlock() {
	atomic.StoreInt32(&s.state, 0)
}

func benchmarkSpinLock(goroutines, iterations int, workNanos int) time.Duration {
	var lock SpinLock
	var counter int64
	var wg sync.WaitGroup

	start := time.Now()
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				lock.Lock()
				// Simulate work inside critical section
				counter++
				deadline := time.Now().Add(time.Duration(workNanos))
				for time.Now().Before(deadline) {
				}
				lock.Unlock()
			}
		}()
	}
	wg.Wait()
	return time.Since(start)
}

func benchmarkMutex(goroutines, iterations int, workNanos int) time.Duration {
	var mu sync.Mutex
	var counter int64
	var wg sync.WaitGroup

	start := time.Now()
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				mu.Lock()
				counter++
				deadline := time.Now().Add(time.Duration(workNanos))
				for time.Now().Before(deadline) {
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return time.Since(start)
}

func main() {
	cpus := runtime.GOMAXPROCS(0)
	fmt.Printf("GOMAXPROCS: %d\n\n", cpus)

	scenarios := []struct {
		name       string
		goroutines int
		iterations int
		workNanos  int
	}{
		{"Tiny critical section (no work)", 100, 1000, 0},
		{"Short critical section (100ns)", 100, 100, 100},
		{"Medium critical section (1us)", 50, 100, 1000},
	}

	for _, s := range scenarios {
		spinTime := benchmarkSpinLock(s.goroutines, s.iterations, s.workNanos)
		mutexTime := benchmarkMutex(s.goroutines, s.iterations, s.workNanos)

		fmt.Printf("%s:\n", s.name)
		fmt.Printf("  SpinLock: %v\n", spinTime)
		fmt.Printf("  Mutex:    %v\n", mutexTime)

		if mutexTime > 0 {
			ratio := float64(spinTime) / float64(mutexTime)
			winner := "Mutex"
			if ratio < 1 {
				winner = "SpinLock"
			}
			fmt.Printf("  Winner:   %s (spin/mutex ratio: %.2f)\n", winner, ratio)
		}
		fmt.Println()
	}
}
```

### Verification
```bash
go run main.go
```
With tiny critical sections, spinlock may be competitive. As the critical section grows, mutex wins because blocked goroutines sleep instead of burning CPU. Under high contention, the spinlock wastes significant CPU time on failed CAS attempts and Gosched calls.

## Step 3 -- Demonstrate the Deadlock Risk Without Gosched

Show why `runtime.Gosched()` is essential in Go spinlocks. Without it, the spinning goroutine holds its OS thread and can prevent the lock holder from running:

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

type BadSpinLock struct {
	state int32
}

func (s *BadSpinLock) Lock() {
	for !atomic.CompareAndSwapInt32(&s.state, 0, 1) {
		// NO Gosched! Tight spin holds the OS thread.
	}
}

func (s *BadSpinLock) Unlock() {
	atomic.StoreInt32(&s.state, 0)
}

type GoodSpinLock struct {
	state int32
}

func (s *GoodSpinLock) Lock() {
	for !atomic.CompareAndSwapInt32(&s.state, 0, 1) {
		runtime.Gosched() // yield so lock holder can run
	}
}

func (s *GoodSpinLock) Unlock() {
	atomic.StoreInt32(&s.state, 0)
}

func main() {
	// Test with GOMAXPROCS=1 to make the problem visible
	runtime.GOMAXPROCS(1)
	fmt.Println("Running with GOMAXPROCS=1")
	fmt.Println()

	// Good spinlock: works because Gosched yields to the lock holder
	fmt.Println("Testing GoodSpinLock...")
	var good GoodSpinLock
	var wg sync.WaitGroup
	var counter int64

	start := time.Now()
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				good.Lock()
				counter++
				good.Unlock()
			}
		}()
	}
	wg.Wait()
	fmt.Printf("GoodSpinLock: counter=%d, time=%v\n", counter, time.Since(start))
	fmt.Println()

	// Bad spinlock: with GOMAXPROCS=1, this would deadlock.
	// We demonstrate the concept with a timeout instead of actually deadlocking.
	fmt.Println("Testing BadSpinLock with timeout protection...")
	var bad BadSpinLock
	done := make(chan bool, 1)

	go func() {
		bad.Lock()
		var wg2 sync.WaitGroup
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			bad.Lock() // with GOMAXPROCS=1, this spins forever
			bad.Unlock()
		}()

		// Give it a moment to show the problem
		time.Sleep(100 * time.Millisecond)
		bad.Unlock()

		// Check if the other goroutine completed
		ch := make(chan struct{})
		go func() {
			wg2.Wait()
			close(ch)
		}()

		select {
		case <-ch:
			done <- true
		case <-time.After(500 * time.Millisecond):
			done <- false
		}
	}()

	result := <-done
	if result {
		fmt.Println("BadSpinLock: completed (got lucky with scheduling)")
	} else {
		fmt.Println("BadSpinLock: TIMED OUT - spinning goroutine starved the lock holder")
	}

	// Restore GOMAXPROCS
	runtime.GOMAXPROCS(runtime.NumCPU())
	fmt.Printf("\nRestored GOMAXPROCS=%d\n", runtime.GOMAXPROCS(0))
}
```

### Verification
```bash
go run main.go
```
GoodSpinLock completes. BadSpinLock either times out or takes much longer, demonstrating that without Gosched, the spinning goroutine starves other goroutines when OS threads are limited.

## Step 4 -- When Spinlocks Actually Make Sense

Show the narrow scenario where spinlocks can outperform mutexes: an extremely short critical section (single memory operation) with low contention. Then show why even in this case, `atomic.AddInt64` is the better answer:

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

type SpinLock struct {
	state int32
}

func (s *SpinLock) Lock() {
	for !atomic.CompareAndSwapInt32(&s.state, 0, 1) {
		runtime.Gosched()
	}
}

func (s *SpinLock) Unlock() {
	atomic.StoreInt32(&s.state, 0)
}

func main() {
	const goroutines = 4
	const iterations = 100000

	// 1. SpinLock protecting a counter
	var spinLock SpinLock
	var spinCounter int64
	var wg sync.WaitGroup

	start := time.Now()
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				spinLock.Lock()
				spinCounter++
				spinLock.Unlock()
			}
		}()
	}
	wg.Wait()
	spinTime := time.Since(start)

	// 2. sync.Mutex protecting a counter
	var mu sync.Mutex
	var mutexCounter int64

	start = time.Now()
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				mu.Lock()
				mutexCounter++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	mutexTime := time.Since(start)

	// 3. Atomic (no lock at all)
	var atomicCounter atomic.Int64

	start = time.Now()
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				atomicCounter.Add(1)
			}
		}()
	}
	wg.Wait()
	atomicTime := time.Since(start)

	fmt.Printf("=== Single Counter, %d goroutines x %d iterations ===\n\n", goroutines, iterations)
	fmt.Printf("SpinLock: %v (counter=%d)\n", spinTime, spinCounter)
	fmt.Printf("Mutex:    %v (counter=%d)\n", mutexTime, mutexCounter)
	fmt.Printf("Atomic:   %v (counter=%d)\n", atomicTime, atomicCounter.Load())
	fmt.Println()
	fmt.Println("Key insight: if your critical section is just a counter increment,")
	fmt.Println("use atomic.Add -- it is faster than any lock, spinlock or otherwise.")
	fmt.Println()
	fmt.Println("Spinlocks only make sense when:")
	fmt.Println("  1. The critical section is a few nanoseconds (not expressible as atomic)")
	fmt.Println("  2. Contention is very low (few goroutines, short hold times)")
	fmt.Println("  3. Goroutine parking overhead is unacceptable (real-time constraints)")
	fmt.Println("  4. You have measured and proven it is actually faster for YOUR case")
}
```

### Verification
```bash
go run main.go
```
Atomic is fastest. SpinLock and Mutex are close for this trivial critical section. The lesson: if you can express the operation as an atomic, do so. If you need a lock, use `sync.Mutex`.

## Intermediate Verification

Run the race detector on each step:
```bash
go run -race main.go
```
All steps should pass with zero race warnings.

## Common Mistakes

### Omitting runtime.Gosched in the Spin Loop

**Wrong:**
```go
package main

import "sync/atomic"

type SpinLock struct{ state int32 }

func (s *SpinLock) Lock() {
	for !atomic.CompareAndSwapInt32(&s.state, 0, 1) {
		// tight spin -- holds OS thread, starves other goroutines
	}
}

func (s *SpinLock) Unlock() {
	atomic.StoreInt32(&s.state, 0)
}

func main() {
	var lock SpinLock
	lock.Lock()
	lock.Unlock()
}
```

**What happens:** With `GOMAXPROCS=1`, the spinner holds the only OS thread and the lock holder cannot run `Unlock()`. Deadlock.

**Fix:** Always call `runtime.Gosched()` in the spin loop.

### Unlocking Without Atomic Store

**Wrong:**
```go
func (s *SpinLock) Unlock() {
    s.state = 0 // non-atomic write -- data race with CAS in Lock()
}
```

**Fix:** Use `atomic.StoreInt32(&s.state, 0)`.

### Using a Spinlock for Long Critical Sections

**Wrong:**
```go
lock.Lock()
result := callExternalAPI() // holds lock for milliseconds
lock.Unlock()
```

**What happens:** Other goroutines spin for milliseconds, burning CPU. With 100 contending goroutines, that is 100 CPU-milliseconds wasted per call.

**Fix:** Use `sync.Mutex` for anything beyond a few nanoseconds. Mutex parks waiting goroutines instead of spinning.

### Deploying a Custom Spinlock in Production

The Go runtime's `sync.Mutex` is a sophisticated hybrid that spins briefly then parks. It handles edge cases (starvation mode, handoff) that a naive spinlock does not. Unless you have exceptional performance requirements backed by benchmark evidence, use `sync.Mutex`.

## Verify What You Learned

1. Why does a spinlock with `GOMAXPROCS=1` risk deadlock without `runtime.Gosched()`?
2. What makes `sync.Mutex` a "hybrid" lock?
3. In what scenario would a custom spinlock outperform `sync.Mutex`?
4. Why is `atomic.Add` better than a spinlock for simple counter increments?

## What's Next
Continue to [06-happens-before-guarantees](../06-happens-before-guarantees/06-happens-before-guarantees.md) to understand the formal memory model rules that make all atomic operations and locks work correctly.

## Summary
- A spinlock uses CAS to atomically transition from unlocked (0) to locked (1); Unlock stores 0
- Always call `runtime.Gosched()` in the spin loop to yield the OS thread and prevent starvation
- Spinlocks burn CPU while waiting; `sync.Mutex` parks blocked goroutines, saving CPU
- Under low contention with tiny critical sections, spinlocks can match mutex performance
- If the critical section is a single value update, use atomics directly -- no lock needed
- `sync.Mutex` is a hybrid spin-then-park lock with starvation prevention; prefer it for all production Go code
- Building a spinlock is educational; deploying one in production requires exceptional, measured justification

## Reference
- [sync.Mutex implementation (Go source)](https://github.com/golang/go/blob/master/src/sync/mutex.go)
- [runtime.Gosched](https://pkg.go.dev/runtime#Gosched)
- [Spinlock (Wikipedia)](https://en.wikipedia.org/wiki/Spinlock)
