# 6. Cond: Signal and Broadcast

<!--
difficulty: advanced
concepts: [sync.Cond, Wait, Signal, Broadcast, producer-consumer, spurious wakeup, condition variable]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [sync.Mutex, sync.WaitGroup, goroutines]
-->

## Prerequisites
- Go 1.22+ installed
- Solid understanding of `sync.Mutex`
- Familiarity with goroutines and `sync.WaitGroup`

## Learning Objectives
After completing this exercise, you will be able to:
- **Explain** when to use `sync.Cond` versus channels
- **Implement** producer-consumer patterns using `Signal` and `Broadcast`
- **Apply** the Wait-in-loop pattern to handle spurious wakeups
- **Distinguish** between `Signal` (wake one) and `Broadcast` (wake all)

## Why sync.Cond
`sync.Cond` is a condition variable -- a synchronization primitive that allows goroutines to wait until a particular condition becomes true. While channels can solve many signaling problems, `sync.Cond` excels in specific scenarios:

1. **Multiple goroutines waiting for the same condition**: With channels, you need complex fan-out logic. With `Broadcast`, you wake all waiters in one call.
2. **Condition that must be checked under a lock**: The condition depends on shared state protected by a mutex. `Cond.Wait` atomically releases the lock and suspends the goroutine, then re-acquires the lock when woken.
3. **Fine-grained notification**: `Signal` wakes exactly one waiter, useful for work-stealing or producer-consumer where only one consumer should proceed.

The critical pattern is **always Wait in a loop**:
```go
cond.L.Lock()
for !condition() {
    cond.Wait()
}
// condition is true, proceed while holding the lock
cond.L.Unlock()
```

Why a loop? Because after `Wait` returns, the condition might no longer be true -- another goroutine might have consumed the item between the signal and the wakeup. This is known as a spurious wakeup, and the loop re-checks the condition before proceeding.

## Step 1 -- Basic Cond: Wait and Signal

Run `main.go` to see the fundamental wait/signal pattern:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	var mu sync.Mutex
	cond := sync.NewCond(&mu)
	ready := false

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		cond.L.Lock()
		for !ready {
			fmt.Println("Waiter: condition not met, waiting...")
			cond.Wait() // atomically releases lock and suspends
		}
		fmt.Println("Waiter: condition met! Proceeding.")
		cond.L.Unlock()
	}()

	time.Sleep(100 * time.Millisecond)

	cond.L.Lock()
	ready = true
	fmt.Println("Signaler: setting condition, signaling...")
	cond.Signal()
	cond.L.Unlock()

	wg.Wait()
}
```

Expected output:
```
Waiter: condition not met, waiting...
Signaler: setting condition, signaling...
Waiter: condition met! Proceeding.
```

### Intermediate Verification
```bash
go run main.go
```

## Step 2 -- Producer-Consumer with Signal

A bounded buffer where the producer adds items and the consumer removes them:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	var mu sync.Mutex
	cond := sync.NewCond(&mu)
	queue := make([]int, 0, 5)
	done := false
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			cond.L.Lock()
			for len(queue) == 0 && !done {
				cond.Wait()
			}
			if len(queue) == 0 && done {
				cond.L.Unlock()
				fmt.Println("Consumer: done.")
				return
			}
			item := queue[0]
			queue = queue[1:]
			fmt.Printf("Consumer: consumed %d (queue len: %d)\n", item, len(queue))
			cond.L.Unlock()
			cond.Signal() // notify producer that space is available
		}
	}()

	for i := 1; i <= 8; i++ {
		cond.L.Lock()
		for len(queue) >= 5 {
			fmt.Println("Producer: queue full, waiting...")
			cond.Wait()
		}
		queue = append(queue, i)
		fmt.Printf("Producer: produced %d (queue len: %d)\n", i, len(queue))
		cond.L.Unlock()
		cond.Signal()
		time.Sleep(20 * time.Millisecond)
	}

	cond.L.Lock()
	done = true
	cond.L.Unlock()
	cond.Signal()
	wg.Wait()
}
```

Expected output:
```
Producer: produced 1 (queue len: 1)
Consumer: consumed 1 (queue len: 0)
Producer: produced 2 (queue len: 1)
...
Consumer: done.
```

### Intermediate Verification
```bash
go run main.go
```
Producer should produce 8 items. When the queue hits capacity 5, the producer waits.

## Step 3 -- Broadcast: Wake All Waiters

Multiple workers wait for a "start" signal:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	var mu sync.Mutex
	cond := sync.NewCond(&mu)
	started := false
	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			cond.L.Lock()
			for !started {
				fmt.Printf("Worker %d: waiting...\n", id)
				cond.Wait()
			}
			cond.L.Unlock()
			fmt.Printf("Worker %d: started!\n", id)
			time.Sleep(50 * time.Millisecond)
			fmt.Printf("Worker %d: done.\n", id)
		}(i)
	}

	time.Sleep(100 * time.Millisecond)

	fmt.Println("\nMain: broadcasting start signal!")
	cond.L.Lock()
	started = true
	cond.Broadcast() // wake ALL waiters at once
	cond.L.Unlock()

	wg.Wait()
	fmt.Println("All workers completed.")
}
```

Expected output:
```
Worker 0: waiting...
Worker 1: waiting...
...
Main: broadcasting start signal!
Worker 0: started!
...
All workers completed.
```

### Intermediate Verification
```bash
go run main.go
```
All 5 workers should print "waiting" first, then all start after the broadcast.

## Step 4 -- Wait-in-Loop (Spurious Wakeups)

Two consumers compete for items -- the loop ensures correctness:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	var mu sync.Mutex
	cond := sync.NewCond(&mu)
	itemCount := 0
	var wg sync.WaitGroup

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 3; j++ {
				cond.L.Lock()
				for itemCount == 0 { // FOR, not IF -- re-check after wakeup
					cond.Wait()
				}
				itemCount--
				fmt.Printf("Consumer %d: took item (remaining: %d)\n", id, itemCount)
				cond.L.Unlock()
			}
		}(i)
	}

	for i := 0; i < 6; i++ {
		time.Sleep(30 * time.Millisecond)
		cond.L.Lock()
		itemCount++
		fmt.Printf("Producer: added item (count: %d)\n", itemCount)
		cond.L.Unlock()
		cond.Signal()
	}

	wg.Wait()
	fmt.Println("Both consumers processed 3 items each.")
}
```

Expected output:
```
Producer: added item (count: 1)
Consumer 0: took item (remaining: 0)
Producer: added item (count: 1)
Consumer 1: took item (remaining: 0)
...
Both consumers processed 3 items each.
```

If you used `if` instead of `for`, a consumer might wake up and find `itemCount == 0` because the other consumer already took the item.

### Intermediate Verification
```bash
go run main.go
```
Both consumers should each consume exactly 3 items without panicking.

## Common Mistakes

### Wait Without Holding the Lock

```go
cond.Wait() // panic: sync: unlock of unlocked mutex
```

**What happens:** `Wait` calls `L.Unlock()` internally. If the lock is not held, it panics.

**Fix:** Always acquire `cond.L.Lock()` before calling `Wait`.

### Using if Instead of for

```go
cond.L.Lock()
if !ready { // NOT safe -- condition might change between Signal and wake
    cond.Wait()
}
// might proceed even though ready is false again
```

**Fix:** Always use `for`:
```go
for !ready {
    cond.Wait()
}
```

### Signal Without Changing the Condition

```go
cond.Signal() // wake a waiter, but the condition has not changed
```

**What happens:** The waiter wakes up, re-checks the condition in the loop, finds it still false, and goes back to sleep. Not a bug, but a wasted wakeup.

### Broadcast When Signal Suffices
Using `Broadcast` when only one goroutine should proceed causes a thundering herd: all waiters wake up, re-check the condition, and all but one go back to sleep. Use `Signal` for single-consumer patterns.

## Verify What You Learned

Implement a "barrier" using `sync.Cond` and `Broadcast`: N goroutines each do some work, then wait at the barrier until all N have arrived. Once all N are waiting, broadcast to release them all simultaneously.

## What's Next
Continue to [07-mutex-vs-channel-decision](../07-mutex-vs-channel-decision/07-mutex-vs-channel-decision.md) to learn when to choose mutexes versus channels for different concurrency problems.

## Summary
- `sync.Cond` allows goroutines to wait until a condition becomes true
- `Wait` atomically releases the mutex and suspends; re-acquires the lock on wakeup
- Always use `Wait` inside a `for` loop that checks the condition (not `if`)
- `Signal` wakes one waiting goroutine -- use for single-consumer patterns
- `Broadcast` wakes all waiting goroutines -- use for start gates and barriers
- `Cond` is most useful when multiple goroutines wait for the same condition under a shared lock
- For simple one-to-one communication, prefer channels over `Cond`

## Reference
- [sync.Cond documentation](https://pkg.go.dev/sync#Cond)
- [Go Blog: Share Memory by Communicating](https://go.dev/blog/codelab-share)
- [Bryan Mills - Rethinking Classical Concurrency Patterns](https://www.youtube.com/watch?v=5zXAHh5tJqQ)
