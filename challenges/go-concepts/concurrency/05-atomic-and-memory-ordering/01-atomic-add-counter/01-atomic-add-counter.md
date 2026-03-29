# 1. Atomic Add Counter

<!--
difficulty: basic
concepts: [sync/atomic, AddInt64, AddUint64, atomic.Int64, data race]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [goroutines, sync.WaitGroup, data races]
-->

## Prerequisites
- Go 1.22+ installed
- Understanding of goroutines and `sync.WaitGroup`
- Basic awareness that concurrent writes to shared variables cause data races

## Learning Objectives
After completing this exercise, you will be able to:
- **Identify** why non-atomic increments produce incorrect results under concurrency
- **Fix** data races using `atomic.AddInt64` and `atomic.AddUint64`
- **Use** the typed `atomic.Int64` wrapper introduced in Go 1.19
- **Verify** correctness with the race detector (`go run -race`)

## Why Atomic Operations
When multiple goroutines increment a shared counter without synchronization, the result is a data race. A simple `counter++` compiles to load-modify-store -- three separate operations that can interleave across goroutines. The result? Lost updates and unpredictable final values.

The `sync/atomic` package provides functions that perform read-modify-write as a single, indivisible CPU instruction. No goroutine can observe an intermediate state. Atomic operations are the lowest-level synchronization primitive in Go -- faster than mutexes for simple counters, but limited to operations on individual values.

Understanding atomics is essential because they form the building blocks of higher-level constructs: mutexes, channels, and lock-free data structures all rely on atomic operations internally.

## Step 1 -- Observe the Race

Edit `main.go` and implement `brokenCounter` to increment a shared `int64` from 1000 goroutines, each incrementing 1000 times:

```go
func brokenCounter() int64 {
    var counter int64
    var wg sync.WaitGroup

    for i := 0; i < 1000; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for j := 0; j < 1000; j++ {
                counter++ // not safe: load-modify-store is three operations
            }
        }()
    }

    wg.Wait()
    return counter
}
```

### Intermediate Verification
```bash
go run main.go
```
Run it several times. The final value will vary and almost never reach 1,000,000. Then confirm with the race detector:
```bash
go run -race main.go
```
You will see `DATA RACE` warnings.

## Step 2 -- Fix with atomic.AddInt64

Implement `atomicAddCounter` using `atomic.AddInt64` from the `sync/atomic` package:

```go
func atomicAddCounter() int64 {
    var counter int64
    var wg sync.WaitGroup

    for i := 0; i < 1000; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for j := 0; j < 1000; j++ {
                atomic.AddInt64(&counter, 1)
            }
        }()
    }

    wg.Wait()
    return counter
}
```

`atomic.AddInt64` takes a pointer to the value and the delta. The entire read-add-write happens as one CPU instruction. No goroutine can see a half-updated value.

### Intermediate Verification
```bash
go run main.go
```
The result is exactly 1,000,000 every time. Confirm no races:
```bash
go run -race main.go
```
Clean output, no warnings.

## Step 3 -- Use the Typed atomic.Int64 Wrapper

Go 1.19 introduced typed wrappers like `atomic.Int64`, `atomic.Uint64`, `atomic.Bool`, and `atomic.Pointer[T]`. These are method-based and harder to misuse:

```go
func typedAtomicCounter() int64 {
    var counter atomic.Int64
    var wg sync.WaitGroup

    for i := 0; i < 1000; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for j := 0; j < 1000; j++ {
                counter.Add(1)
            }
        }()
    }

    wg.Wait()
    return counter.Load()
}
```

The typed API eliminates the need to pass a pointer and prevents accidentally using the value non-atomically. Prefer this style in new code.

### Intermediate Verification
```bash
go run main.go
```
Same result: exactly 1,000,000. The typed wrapper is functionally equivalent but cleaner.

## Common Mistakes

### Using the Wrong Address
**Wrong:**
```go
var counter int64
c := counter
atomic.AddInt64(&c, 1) // increments a local copy, not the shared counter
```
**What happens:** Each goroutine modifies its own copy. The original `counter` stays at 0.

**Fix:** Always pass the address of the original variable: `atomic.AddInt64(&counter, 1)`.

### Mixing Atomic and Non-Atomic Access
**Wrong:**
```go
atomic.AddInt64(&counter, 1)
fmt.Println(counter) // non-atomic read -- data race!
```
**What happens:** Reading `counter` directly while other goroutines write atomically is still a race. All access to a shared variable must be atomic if any access is atomic.

**Fix:**
```go
atomic.AddInt64(&counter, 1)
fmt.Println(atomic.LoadInt64(&counter))
```

### Assuming Atomic Operations Provide Ordering
Atomic operations guarantee indivisibility of a single read-modify-write, but they do not by themselves create happens-before relationships for other variables. You will explore this in exercise 06.

## Verify What You Learned

Implement `bidirectionalCounter` that supports both increments and decrements:
1. Launch 500 goroutines that each call `atomic.AddInt64(&counter, 1)` 1000 times
2. Launch 500 goroutines that each call `atomic.AddInt64(&counter, -1)` 1000 times
3. The final result must be exactly 0

Run with `-race` to confirm correctness.

## What's Next
Continue to [02-atomic-load-store](../02-atomic-load-store/02-atomic-load-store.md) to learn how `atomic.LoadInt64` and `atomic.StoreInt64` provide visibility guarantees for published data.

## Summary
- Non-atomic `counter++` is a load-modify-store -- three operations that can interleave
- `atomic.AddInt64(&counter, delta)` performs the increment as a single indivisible operation
- `atomic.Int64` (Go 1.19+) is the preferred typed wrapper: `counter.Add(1)`, `counter.Load()`
- All access to a shared variable must be atomic if any access is atomic -- no mixing
- Atomic add is ideal for simple counters; for complex state, consider mutexes

## Reference
- [sync/atomic package](https://pkg.go.dev/sync/atomic)
- [atomic.Int64 type](https://pkg.go.dev/sync/atomic#Int64)
- [Go Race Detector](https://go.dev/doc/articles/race_detector)
