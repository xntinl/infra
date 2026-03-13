# 7. atomic Package

<!--
difficulty: advanced
concepts: [sync-atomic, atomic-int64, load-store-add, compare-and-swap, lock-free]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [sync-mutex, goroutines, data-races]
-->

## Prerequisites

- Go 1.22+ installed
- Solid understanding of `sync.Mutex` and data races
- Familiarity with goroutines and `sync.WaitGroup`

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** when atomic operations are preferable to mutexes
- **Implement** lock-free counters and flags using `atomic.Int64`, `atomic.Bool`
- **Analyze** the semantics of `Load`, `Store`, `Add`, `Swap`, and `CompareAndSwap`

## Why atomic Operations

Mutexes protect critical sections by blocking goroutines. For simple operations on single values -- incrementing a counter, toggling a flag, or swapping a pointer -- the overhead of acquiring and releasing a lock is disproportionate.

The `sync/atomic` package provides low-level atomic operations that complete in a single CPU instruction without locking. They are faster than mutexes for simple operations but cannot protect multi-step transactions or compound operations.

Since Go 1.19, the package provides typed wrappers (`atomic.Int64`, `atomic.Bool`, `atomic.Pointer[T]`) that are safer and clearer than the raw `atomic.AddInt64(&val, 1)` functions.

## The Problem

Build a concurrent metrics collector that tracks multiple counters (requests, errors, bytes processed) using atomic operations instead of mutexes. Then implement a graceful shutdown flag using `atomic.Bool`.

## Requirements

1. Use `atomic.Int64` for counters that support `Add`, `Load`, and `Store`
2. Use `atomic.Bool` for a shutdown flag that multiple goroutines check
3. Implement a `CompareAndSwap` example that updates a value only if it matches an expected value
4. Demonstrate that atomics are race-free without mutexes

## Hints

<details>
<summary>Hint 1: Typed Atomics (Go 1.19+)</summary>

```go
var counter atomic.Int64
counter.Add(1)          // increment
counter.Add(-1)         // decrement
val := counter.Load()   // read
counter.Store(0)        // reset
```
</details>

<details>
<summary>Hint 2: CompareAndSwap</summary>

CAS atomically checks if the current value equals `old` and, if so, sets it to `new`:

```go
var state atomic.Int32
swapped := state.CompareAndSwap(0, 1) // only swaps if current value is 0
```
</details>

<details>
<summary>Hint 3: Complete Implementation</summary>

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type Metrics struct {
	Requests       atomic.Int64
	Errors         atomic.Int64
	BytesProcessed atomic.Int64
}

func (m *Metrics) Report() {
	fmt.Printf("Requests: %d, Errors: %d, Bytes: %d\n",
		m.Requests.Load(),
		m.Errors.Load(),
		m.BytesProcessed.Load())
}

func main() {
	metrics := &Metrics{}
	var shutdown atomic.Bool
	var wg sync.WaitGroup

	// Worker goroutines
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for !shutdown.Load() {
				metrics.Requests.Add(1)
				if id%3 == 0 {
					metrics.Errors.Add(1)
				}
				metrics.BytesProcessed.Add(int64(256 + id*64))
				time.Sleep(time.Millisecond)
			}
			fmt.Printf("Worker %d stopped\n", id)
		}(i)
	}

	// Let workers run for a bit
	time.Sleep(50 * time.Millisecond)
	metrics.Report()

	// Signal shutdown
	shutdown.Store(true)
	wg.Wait()

	fmt.Println("Final:")
	metrics.Report()

	// CompareAndSwap example
	var state atomic.Int32
	fmt.Println("\nCAS examples:")
	fmt.Println("CAS(0->1):", state.CompareAndSwap(0, 1)) // true
	fmt.Println("CAS(0->2):", state.CompareAndSwap(0, 2)) // false, current is 1
	fmt.Println("CAS(1->2):", state.CompareAndSwap(1, 2)) // true
	fmt.Println("Final state:", state.Load())               // 2
}
```
</details>

## Verification

```bash
go run -race main.go
```

Expected: Metrics are tracked without races. All workers stop after shutdown. CAS examples show `true`, `false`, `true`, and final state `2`.

## What's Next

Continue to [08 - atomic.Value Config Hot Reload](../08-atomic-value-config-hot-reload/08-atomic-value-config-hot-reload.md) to learn how to use `atomic.Value` for lock-free configuration updates.

## Summary

- `sync/atomic` provides lock-free operations for single values
- Use `atomic.Int64`, `atomic.Bool`, `atomic.Pointer[T]` (Go 1.19+) for type safety
- `Load`/`Store` for reads and writes; `Add` for counters; `CompareAndSwap` for conditional updates
- Atomics are faster than mutexes for simple single-value operations
- Atomics cannot protect multi-step operations or compound state changes -- use mutexes for those

## Reference

- [sync/atomic documentation](https://pkg.go.dev/sync/atomic)
- [The Go Memory Model](https://go.dev/ref/mem)
- [Go 1.19 release notes: atomic types](https://go.dev/doc/go1.19#atomic_types)
