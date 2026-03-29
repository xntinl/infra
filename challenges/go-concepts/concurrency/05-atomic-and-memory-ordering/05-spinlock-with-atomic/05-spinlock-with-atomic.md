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
- **Test** a spinlock under contention and verify mutual exclusion
- **Articulate** why `sync.Mutex` is almost always the better choice in Go

## Why Build a Spinlock
A spinlock is the simplest possible mutex: `Lock()` spins in a CAS loop until it acquires the lock, and `Unlock()` atomically releases it. There is no OS involvement, no goroutine parking, no scheduler interaction. This makes spinlocks extremely fast when the lock is uncontended and the critical section is tiny.

Building one from scratch solidifies your understanding of CAS and its limitations. More importantly, it reveals the fundamental trade-off: a spinlock wastes CPU cycles while waiting, because the goroutine runs a tight loop instead of sleeping. In Go, where the runtime multiplexes thousands of goroutines onto a few OS threads, a spinning goroutine holds an OS thread hostage and prevents other goroutines from running.

`sync.Mutex` is a hybrid: it spins briefly (optimistic fast path) and then parks the goroutine with the OS scheduler (pessimistic slow path). This gives you the speed of a spinlock when the lock is quickly available, and the efficiency of sleeping when it is not. For almost all Go code, `sync.Mutex` is the right tool. This exercise exists to understand why.

## Step 1 -- Implement SpinLock

Build a spinlock using `atomic.CompareAndSwapInt32`:

```go
type SpinLock struct {
    state int32 // 0 = unlocked, 1 = locked
}

func (s *SpinLock) Lock() {
    for !atomic.CompareAndSwapInt32(&s.state, 0, 1) {
        // spin: keep trying until we successfully set state from 0 to 1
        runtime.Gosched() // yield to other goroutines
    }
}

func (s *SpinLock) Unlock() {
    atomic.StoreInt32(&s.state, 0)
}
```

`Lock()` repeatedly attempts to CAS from 0 (unlocked) to 1 (locked). When it succeeds, the calling goroutine owns the lock. `Unlock()` stores 0, releasing the lock for the next goroutine.

The `runtime.Gosched()` call is critical in Go. Without it, a spinning goroutine holds its OS thread and prevents other goroutines (including the lock holder) from running. This can cause a livelock where the spinner prevents the holder from reaching `Unlock()`.

### Intermediate Verification
```bash
go run main.go
```
At this point, just confirm it compiles. We will test correctness in the next step.

## Step 2 -- Test Mutual Exclusion

Use the spinlock to protect a shared counter and verify it provides mutual exclusion:

```go
func testSpinLock() int64 {
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
    return counter
}
```

If the spinlock provides correct mutual exclusion, the counter will be exactly 100,000.

### Intermediate Verification
```bash
go run main.go
```
Expected: counter is exactly 100,000.
```bash
go run -race main.go
```
No race warnings. The non-atomic `counter++` is safe because it is always protected by the spinlock.

## Step 3 -- Compare SpinLock vs sync.Mutex Under Contention

Implement the same counter test with `sync.Mutex` and measure the time difference under varying contention:

```go
func testMutex() int64 {
    var mu sync.Mutex
    var counter int64
    var wg sync.WaitGroup

    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for j := 0; j < 1000; j++ {
                mu.Lock()
                counter++
                mu.Unlock()
            }
        }()
    }

    wg.Wait()
    return counter
}

func compareContention() {
    // Low contention: few goroutines, short critical section
    start := time.Now()
    testSpinLockN(4, 10000)
    spinLow := time.Since(start)

    start = time.Now()
    testMutexN(4, 10000)
    mutexLow := time.Since(start)

    fmt.Printf("  Low contention  (4 goroutines):   SpinLock=%v, Mutex=%v\n", spinLow, mutexLow)

    // High contention: many goroutines, short critical section
    start = time.Now()
    testSpinLockN(1000, 1000)
    spinHigh := time.Since(start)

    start = time.Now()
    testMutexN(1000, 1000)
    mutexHigh := time.Since(start)

    fmt.Printf("  High contention (1000 goroutines): SpinLock=%v, Mutex=%v\n", spinHigh, mutexHigh)
}
```

Under low contention, the spinlock may be slightly faster because there is no OS scheduler involvement. Under high contention, the spinlock burns CPU while goroutines spin, and `sync.Mutex` wins because blocked goroutines sleep and free their OS threads.

### Intermediate Verification
```bash
go run main.go
```
Observe the timing difference. On most machines, the mutex is faster or comparable under high contention, and the spinlock is competitive only under low contention.

## Common Mistakes

### Omitting runtime.Gosched in the Spin Loop
**Wrong:**
```go
func (s *SpinLock) Lock() {
    for !atomic.CompareAndSwapInt32(&s.state, 0, 1) {
        // tight spin -- holds OS thread, starves other goroutines
    }
}
```
**What happens:** With `GOMAXPROCS=1`, this can deadlock: the spinner holds the only OS thread, and the lock holder cannot run to call `Unlock()`. Even with more threads, tight spinning wastes CPU and reduces throughput.

**Fix:** Always call `runtime.Gosched()` in the spin loop to yield the processor.

### Unlocking Without Atomic
**Wrong:**
```go
func (s *SpinLock) Unlock() {
    s.state = 0 // non-atomic write -- data race
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
result := expensiveComputation() // holds the lock for milliseconds
lock.Unlock()
```
**What happens:** Other goroutines spin for milliseconds, burning CPU. With 100 contending goroutines, that is 100 CPU-milliseconds wasted per operation.

**Fix:** Use `sync.Mutex` for anything beyond a few nanoseconds of work.

## Verify What You Learned

Implement a `TryLock` method for SpinLock:

```go
func (s *SpinLock) TryLock() bool
```

`TryLock` attempts to acquire the lock exactly once (one CAS attempt). Returns `true` if it succeeded, `false` if the lock was already held. This is useful for non-blocking algorithms where you want to try but not wait.

Test it: launch 100 goroutines that all call `TryLock`. Count how many succeed (should be exactly 1 at a time).

## What's Next
Continue to [06-happens-before-guarantees](../06-happens-before-guarantees/06-happens-before-guarantees.md) to understand the formal memory model rules that make all these atomic operations work correctly.

## Summary
- A spinlock uses CAS to atomically transition from unlocked (0) to locked (1)
- `Lock()` spins in a CAS loop; `Unlock()` atomically stores 0
- Always call `runtime.Gosched()` in the spin loop to yield the OS thread
- Spinlocks burn CPU while waiting; `sync.Mutex` parks blocked goroutines
- Under low contention, spinlocks can be marginally faster; under high contention, mutexes win
- In Go, `sync.Mutex` is almost always the right choice -- it uses a hybrid spin-then-park strategy internally
- Building a spinlock is a learning exercise; deploying one in production requires exceptional justification

## Reference
- [sync.Mutex implementation (Go source)](https://github.com/golang/go/blob/master/src/sync/mutex.go)
- [runtime.Gosched](https://pkg.go.dev/runtime#Gosched)
- [Spinlock (Wikipedia)](https://en.wikipedia.org/wiki/Spinlock)
