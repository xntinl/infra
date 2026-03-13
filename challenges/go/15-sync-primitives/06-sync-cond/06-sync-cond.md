# 6. sync.Cond

<!--
difficulty: advanced
concepts: [sync-cond, wait-signal-broadcast, producer-consumer, condition-variable]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [sync-mutex, goroutines, channels]
-->

## Prerequisites

- Go 1.22+ installed
- Solid understanding of `sync.Mutex` and goroutines
- Familiarity with channels and blocking operations

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** what a condition variable is and when to use one
- **Implement** producer-consumer patterns using `sync.Cond`
- **Analyze** the difference between `Signal` (wake one) and `Broadcast` (wake all)

## Why sync.Cond

Channels are Go's preferred coordination mechanism, but some patterns are more naturally expressed with condition variables. `sync.Cond` lets goroutines wait for a condition to become true and be notified when it changes.

A condition variable pairs with a mutex: you hold the lock, check a condition, and if it is not met, call `Wait()` which atomically releases the lock and suspends the goroutine. When another goroutine changes the condition, it calls `Signal()` (wake one waiter) or `Broadcast()` (wake all waiters). The woken goroutine re-acquires the lock and re-checks the condition.

This is particularly useful for bounded buffers, barrier synchronization, and cases where multiple goroutines need to wait for the same state change.

## The Problem

Build a bounded buffer (a queue with a maximum capacity) that allows producers to add items and consumers to remove items. When the buffer is full, producers must wait. When the buffer is empty, consumers must wait.

## Requirements

1. The buffer has a configurable maximum capacity
2. `Put(item)` blocks when the buffer is full and resumes when space is available
3. `Get()` blocks when the buffer is empty and resumes when an item is available
4. Multiple producers and consumers must work concurrently without data races
5. Use `sync.Cond` with `Signal` for targeted wakeups

## Hints

<details>
<summary>Hint 1: Creating a Cond</summary>

```go
cond := sync.NewCond(&sync.Mutex{})
```

The `Cond` wraps a `Locker` (usually a `*sync.Mutex`). Access the mutex via `cond.L`.
</details>

<details>
<summary>Hint 2: Wait Loop Pattern</summary>

Always check conditions in a loop, not an if:

```go
cond.L.Lock()
for !conditionMet {
    cond.Wait() // releases lock, waits, re-acquires lock
}
// condition is now true, lock is held
cond.L.Unlock()
```
</details>

<details>
<summary>Hint 3: Two Conditions</summary>

For a bounded buffer, you need two conditions: "not full" (for producers) and "not empty" (for consumers). You can use two separate `sync.Cond` instances sharing the same mutex, or use `Broadcast` with one.
</details>

<details>
<summary>Hint 4: Complete Implementation</summary>

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type BoundedBuffer struct {
	buf     []int
	cap     int
	notFull *sync.Cond
	notEmpty *sync.Cond
	mu      sync.Mutex
}

func NewBoundedBuffer(capacity int) *BoundedBuffer {
	bb := &BoundedBuffer{
		buf: make([]int, 0, capacity),
		cap: capacity,
	}
	bb.notFull = sync.NewCond(&bb.mu)
	bb.notEmpty = sync.NewCond(&bb.mu)
	return bb
}

func (bb *BoundedBuffer) Put(item int) {
	bb.mu.Lock()
	defer bb.mu.Unlock()

	for len(bb.buf) == bb.cap {
		bb.notFull.Wait()
	}

	bb.buf = append(bb.buf, item)
	bb.notEmpty.Signal()
}

func (bb *BoundedBuffer) Get() int {
	bb.mu.Lock()
	defer bb.mu.Unlock()

	for len(bb.buf) == 0 {
		bb.notEmpty.Wait()
	}

	item := bb.buf[0]
	bb.buf = bb.buf[1:]
	bb.notFull.Signal()
	return item
}

func main() {
	bb := NewBoundedBuffer(5)
	var wg sync.WaitGroup

	// 3 producers
	for p := 0; p < 3; p++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				item := id*100 + i
				bb.Put(item)
				fmt.Printf("Producer %d: put %d\n", id, item)
				time.Sleep(time.Millisecond)
			}
		}(p)
	}

	// 2 consumers
	for c := 0; c < 2; c++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 15; i++ {
				item := bb.Get()
				fmt.Printf("  Consumer %d: got %d\n", id, item)
				time.Sleep(2 * time.Millisecond)
			}
		}(c)
	}

	wg.Wait()
}
```
</details>

## Verification

```bash
go run -race main.go
```

Expected: All 30 items produced are consumed (3 producers x 10 items = 30 produced, 2 consumers x 15 items = 30 consumed). No race conditions or deadlocks.

## What's Next

Continue to [07 - atomic Package](../07-atomic-package/07-atomic-package.md) to learn lock-free operations with the `sync/atomic` package.

## Summary

- `sync.Cond` pairs a condition variable with a mutex for wait/notify patterns
- Always check conditions in a `for` loop, never an `if` -- spurious wakeups can occur
- `Signal()` wakes one waiting goroutine; `Broadcast()` wakes all
- Two `Cond` instances can share a mutex for separate conditions (e.g., "not full" and "not empty")
- In most Go code, channels are preferred, but `sync.Cond` is useful for bounded buffers and barriers

## Reference

- [sync.Cond documentation](https://pkg.go.dev/sync#Cond)
- [Condition Variables (Wikipedia)](https://en.wikipedia.org/wiki/Monitor_(synchronization)#Condition_variables)
