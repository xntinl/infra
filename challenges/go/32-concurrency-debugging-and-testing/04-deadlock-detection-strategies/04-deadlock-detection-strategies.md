# 4. Deadlock Detection Strategies

<!--
difficulty: advanced
concepts: [deadlock, lock-ordering, runtime-deadlock-detection, mutex-profiling, circular-wait]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [sync-mutex, goroutines, channels-basics]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of mutexes and channels
- Familiarity with goroutine scheduling
- Knowledge of classical deadlock conditions (mutual exclusion, hold and wait, no preemption, circular wait)

## Learning Objectives

After completing this exercise, you will be able to:

- **Identify** deadlock patterns in Go programs: lock ordering violations, channel deadlocks, self-deadlocks
- **Analyze** Go runtime deadlock detection (`fatal error: all goroutines are asleep`)
- **Apply** lock ordering and timeout strategies to prevent deadlocks
- **Design** code structures that are deadlock-free by construction

## Why Deadlock Detection Matters

Go's runtime detects only total deadlocks -- when every goroutine is blocked. If even one goroutine is running (like the main goroutine with a `time.Sleep`), the runtime will not detect the deadlock. Partial deadlocks are silent and manifest as hangs, timeouts, or resource exhaustion in production.

Understanding deadlock patterns and prevention strategies is essential for writing reliable concurrent Go programs. Prevention through design (lock ordering, timeouts, channels over mutexes) is always better than detection after the fact.

## The Problem

You will create, analyze, and fix several deadlock patterns:

1. A classic lock-ordering deadlock between two mutexes
2. A channel deadlock (unbuffered channel with no receiver)
3. A self-deadlock (goroutine re-acquiring a non-reentrant mutex)
4. A dining philosophers scenario

## Requirements

1. **Lock ordering deadlock** -- create two goroutines that acquire mutex A then B, and B then A; demonstrate the deadlock; fix with consistent lock ordering
2. **Channel deadlock** -- write to an unbuffered channel with no receiver in the same goroutine; show the runtime error; fix with a goroutine or buffered channel
3. **Self-deadlock** -- call `mu.Lock()` twice from the same goroutine; note that Go mutexes are not reentrant; fix by restructuring the code
4. **Timeout-based prevention** -- use `context.WithTimeout` to prevent indefinite blocking when acquiring resources
5. **Lock ordering enforcement** -- implement a wrapper that enforces a global lock ordering to prevent future deadlocks
6. **Tests** -- each pattern must have a test that detects the deadlock (or demonstrates the fix)

## Hints

<details>
<summary>Hint 1: Lock ordering deadlock</summary>

```go
var muA, muB sync.Mutex

// Goroutine 1: A -> B
go func() {
    muA.Lock()
    time.Sleep(time.Millisecond)
    muB.Lock() // blocks: goroutine 2 holds muB
    muB.Unlock()
    muA.Unlock()
}()

// Goroutine 2: B -> A
go func() {
    muB.Lock()
    time.Sleep(time.Millisecond)
    muA.Lock() // blocks: goroutine 1 holds muA -- DEADLOCK
    muA.Unlock()
    muB.Unlock()
}()
```

Fix: always acquire A before B.

</details>

<details>
<summary>Hint 2: Self-deadlock</summary>

```go
var mu sync.Mutex

func outer() {
    mu.Lock()
    defer mu.Unlock()
    inner() // calls mu.Lock() again -- DEADLOCK
}

func inner() {
    mu.Lock() // blocks forever: same goroutine already holds mu
    defer mu.Unlock()
    // ...
}
```

Fix: refactor `inner` to `innerLocked` (assumes lock is held) and call it from `outer`.

</details>

<details>
<summary>Hint 3: Timeout-based lock acquisition</summary>

```go
func tryLock(mu *sync.Mutex, timeout time.Duration) bool {
    done := make(chan struct{})
    go func() {
        mu.Lock()
        close(done)
    }()
    select {
    case <-done:
        return true
    case <-time.After(timeout):
        return false // could not acquire in time
    }
}
```

Note: this leaks a goroutine if the timeout fires. In practice, use channels or `context.Context` instead of mutexes for timeout-aware synchronization.

</details>

## Verification

```bash
go test -v -race -timeout 30s ./...
```

Your tests should:
- Demonstrate each deadlock pattern (with a timeout to prevent hanging)
- Show the runtime error message for total deadlocks
- Verify that fixed versions complete within the timeout
- Pass consistently with `-race` enabled

## What's Next

Continue to [05 - Contention Analysis](../05-contention-analysis/05-contention-analysis.md) to learn how to profile mutex contention.

## Summary

- Go detects total deadlocks (all goroutines blocked) but not partial deadlocks
- Lock ordering deadlocks occur when goroutines acquire multiple locks in different orders
- Go mutexes are not reentrant -- locking twice from the same goroutine deadlocks
- Fix lock ordering with consistent global ordering or by reducing the number of locks held simultaneously
- For timeout-aware resource acquisition, prefer channels and context over mutexes
- Design for deadlock-freedom: minimize lock scope, use one lock at a time, prefer channels for coordination

## Reference

- [Go runtime deadlock detection](https://pkg.go.dev/runtime#hdr-Goroutine_Scheduling)
- [sync.Mutex](https://pkg.go.dev/sync#Mutex)
- [Deadlock prevention](https://en.wikipedia.org/wiki/Deadlock_prevention_algorithms)
