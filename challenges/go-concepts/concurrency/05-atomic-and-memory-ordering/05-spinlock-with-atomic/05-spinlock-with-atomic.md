# 5. Spinlock with Atomic CAS

<!--
difficulty: advanced
concepts: [spinlock, CompareAndSwapInt32, busy-wait, lock contention, sync.Mutex comparison]
tools: [go]
estimated_time: 40m
bloom_level: analyze
prerequisites: [goroutines, sync.WaitGroup, CAS, atomic Load/Store, sync.Mutex]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01-03 (atomic add, load/store, CAS)
- Understanding of `sync.Mutex` basics

## Learning Objectives
After completing this exercise, you will be able to:
- **Build** a working spinlock from atomic CAS operations
- **Explain** why spinlocks burn CPU while waiting
- **Implement** TryLock for non-blocking lock acquisition
- **Articulate** why `sync.Mutex` is almost always the better choice in Go

## Why Build a Spinlock

A spinlock is the simplest possible mutex: `Lock()` spins in a CAS loop until it acquires the lock, and `Unlock()` atomically releases it. There is no OS involvement, no goroutine parking, no scheduler interaction. This makes spinlocks extremely fast when the lock is uncontended and the critical section is tiny.

Building one from scratch solidifies your understanding of CAS and its limitations. More importantly, it reveals the fundamental trade-off: a spinlock wastes CPU cycles while waiting, because the goroutine runs a tight loop instead of sleeping. In Go, where the runtime multiplexes thousands of goroutines onto a few OS threads, a spinning goroutine holds an OS thread hostage and prevents other goroutines from running.

`sync.Mutex` is a hybrid: it spins briefly (optimistic fast path) and then parks the goroutine with the OS scheduler (pessimistic slow path). This gives you the speed of a spinlock when the lock is quickly available, and the efficiency of sleeping when it is not. For almost all Go code, `sync.Mutex` is the right tool.

## Example 1 -- Implement SpinLock

Build a spinlock using `atomic.CompareAndSwapInt32`:

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
		// Yield so the lock holder can run and eventually Unlock().
		runtime.Gosched()
	}
}

func (s *SpinLock) Unlock() {
	atomic.StoreInt32(&s.state, 0)
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
				counter++ // safe: protected by spinlock
				lock.Unlock()
			}
		}()
	}

	wg.Wait()
	fmt.Printf("Expected: 100000, Got: %d\n", counter)
}
```

`Lock()` repeatedly attempts CAS from 0 (unlocked) to 1 (locked). When it succeeds, the calling goroutine owns the lock. `Unlock()` stores 0 to release.

The `runtime.Gosched()` call is critical in Go. Without it, a spinning goroutine holds its OS thread and prevents other goroutines (including the lock holder) from running. With `GOMAXPROCS=1`, this causes deadlock.

### Verification
```bash
go run -race main.go
```
Expected: counter is exactly 100,000. No race warnings.

## Example 2 -- Compare SpinLock vs sync.Mutex Under Contention

Measure the time difference under varying contention levels:

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

func runSpinLock(goroutines, iterations int) {
	var lock SpinLock
	var counter int64
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				lock.Lock()
				counter++
				lock.Unlock()
			}
		}()
	}
	wg.Wait()
}

func runMutex(goroutines, iterations int) {
	var mu sync.Mutex
	var counter int64
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				mu.Lock()
				counter++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
}

func main() {
	// Low contention: few goroutines, short critical section
	start := time.Now()
	runSpinLock(4, 10000)
	fmt.Printf("Low contention  SpinLock: %v\n", time.Since(start))

	start = time.Now()
	runMutex(4, 10000)
	fmt.Printf("Low contention  Mutex:    %v\n", time.Since(start))

	// High contention: many goroutines, short critical section
	start = time.Now()
	runSpinLock(1000, 1000)
	fmt.Printf("High contention SpinLock: %v\n", time.Since(start))

	start = time.Now()
	runMutex(1000, 1000)
	fmt.Printf("High contention Mutex:    %v\n", time.Since(start))
}
```

### Verification
```bash
go run main.go
```
Under low contention, the spinlock may be slightly faster. Under high contention, the mutex wins because blocked goroutines sleep instead of burning CPU.

## Example 3 -- TryLock (Non-Blocking Acquisition)

`TryLock` attempts to acquire the lock exactly once. Useful for "try but don't wait" patterns:

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
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

func (s *SpinLock) TryLock() bool {
	return atomic.CompareAndSwapInt32(&s.state, 0, 1)
}

func main() {
	var lock SpinLock
	var acquired atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if lock.TryLock() {
				acquired.Add(1)
				runtime.Gosched() // hold briefly
				lock.Unlock()
			}
			// If TryLock failed, we skip — no waiting
		}()
	}

	wg.Wait()
	fmt.Printf("Out of 100 goroutines, %d acquired the lock\n", acquired.Load())
}
```

### Verification
```bash
go run main.go
```
A small number of goroutines (often 1-5) will successfully acquire the lock, because most goroutines arrive while the lock is held and TryLock returns false immediately.

## Common Mistakes

### Omitting runtime.Gosched in the Spin Loop

**Wrong:**
```go
package main

import (
	"sync/atomic"
)

type SpinLock struct{ state int32 }

func (s *SpinLock) Lock() {
	for !atomic.CompareAndSwapInt32(&s.state, 0, 1) {
		// tight spin — holds OS thread, starves other goroutines
	}
}

func (s *SpinLock) Unlock() {
	atomic.StoreInt32(&s.state, 0)
}

func main() {
	var lock SpinLock
	lock.Lock()
	// With GOMAXPROCS=1, another goroutine trying to Lock() will
	// spin forever because the lock holder cannot run Unlock().
	lock.Unlock()
}
```

**What happens:** With `GOMAXPROCS=1`, this deadlocks. Even with more threads, tight spinning wastes CPU and reduces throughput.

**Fix:** Always call `runtime.Gosched()` in the spin loop to yield the processor.

### Unlocking Without Atomic

**Wrong:**
```go
func (s *SpinLock) Unlock() {
    s.state = 0 // non-atomic write — data race
}
```

**What happens:** A non-atomic write races with the CAS in `Lock()`. The race detector will flag this.

**Fix:** Use `atomic.StoreInt32(&s.state, 0)`.

### Unlocking a Lock You Don't Hold

**Wrong:**
```go
lock.Unlock() // called without a preceding Lock()
```

**What happens:** A double-unlock sets state to 0 when it is already 0, potentially allowing two goroutines to both "acquire" the lock. Unlike `sync.Mutex`, our simple spinlock does not detect this.

**Fix:** Ensure every `Unlock()` is paired with a preceding `Lock()`. Use `defer lock.Unlock()` immediately after `Lock()`.

### Using a Spinlock for Long Critical Sections

**Wrong:**
```go
lock.Lock()
result := expensiveNetworkCall() // holds lock for milliseconds
lock.Unlock()
```

**What happens:** Other goroutines spin for milliseconds, burning CPU. With 100 contending goroutines, that is 100 CPU-milliseconds wasted per operation.

**Fix:** Use `sync.Mutex` for anything beyond a few nanoseconds of work. The mutex parks waiting goroutines instead of spinning.

## What's Next
Continue to [06-happens-before-guarantees](../06-happens-before-guarantees/06-happens-before-guarantees.md) to understand the formal memory model rules that make all these atomic operations work correctly.

## Summary
- A spinlock uses CAS to atomically transition from unlocked (0) to locked (1)
- `Lock()` spins in a CAS loop; `Unlock()` atomically stores 0
- Always call `runtime.Gosched()` in the spin loop to yield the OS thread
- `TryLock()` makes a single CAS attempt for non-blocking lock acquisition
- Spinlocks burn CPU while waiting; `sync.Mutex` parks blocked goroutines
- Under low contention, spinlocks can be marginally faster; under high contention, mutexes win
- In Go, `sync.Mutex` is almost always the right choice -- it uses a hybrid spin-then-park strategy internally
- Building a spinlock is a learning exercise; deploying one in production requires exceptional justification

## Reference
- [sync.Mutex implementation (Go source)](https://github.com/golang/go/blob/master/src/sync/mutex.go)
- [runtime.Gosched](https://pkg.go.dev/runtime#Gosched)
- [Spinlock (Wikipedia)](https://en.wikipedia.org/wiki/Spinlock)
