# 12. Channel Patterns: Semaphore and Barrier

<!--
difficulty: advanced
concepts: [channel-semaphore, barrier-pattern, countdown-latch, concurrency-limiting, synchronization-primitives]
tools: [go]
estimated_time: 40m
bloom_level: analyze
prerequisites: [goroutines, channel-basics, buffered-vs-unbuffered-channels, signaling-with-closed-channels]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [11 - Goroutine Lifecycle Management](../11-goroutine-lifecycle-management/11-goroutine-lifecycle-management.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** a counting semaphore using a buffered channel
- **Build** a barrier that synchronizes multiple goroutines at a rendezvous point
- **Construct** a countdown latch that blocks until N events have occurred
- **Choose** between these patterns based on the coordination requirement

## Why Channel-Based Synchronization Primitives

Go channels are more than message-passing pipes -- they are general-purpose synchronization primitives. A buffered channel of capacity N naturally limits concurrency to N (a semaphore). A closed channel naturally broadcasts to all waiters (a barrier). Combining these with goroutines and `select` gives you building blocks that are often simpler and safer than their mutex-based equivalents.

Understanding these patterns lets you control concurrency without reaching for external libraries or low-level sync primitives.

## Step 1 -- Channel as a Counting Semaphore

A buffered channel of size N allows at most N goroutines to proceed concurrently. Acquiring the semaphore means sending into the channel; releasing means receiving from it.

```bash
mkdir -p ~/go-exercises/chan-patterns && cd ~/go-exercises/chan-patterns
go mod init chan-patterns
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type Semaphore chan struct{}

func NewSemaphore(n int) Semaphore {
	return make(Semaphore, n)
}

func (s Semaphore) Acquire() {
	s <- struct{}{} // blocks when buffer is full
}

func (s Semaphore) Release() {
	<-s // frees a slot
}

func main() {
	sem := NewSemaphore(3) // max 3 concurrent
	var wg sync.WaitGroup

	for i := 1; i <= 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			sem.Acquire()
			defer sem.Release()

			fmt.Printf("worker %2d: start\n", id)
			time.Sleep(100 * time.Millisecond) // simulate work
			fmt.Printf("worker %2d: done\n", id)
		}(i)
	}

	wg.Wait()
	fmt.Println("all done")
}
```

### Intermediate Verification

```bash
go run main.go
```

You should see at most 3 "start" messages before corresponding "done" messages. Workers execute in batches of 3.

## Step 2 -- Semaphore with Timeout

Combine the semaphore with `select` to add a timeout on acquisition:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type Semaphore chan struct{}

func NewSemaphore(n int) Semaphore {
	return make(Semaphore, n)
}

func (s Semaphore) TryAcquire(timeout time.Duration) bool {
	select {
	case s <- struct{}{}:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (s Semaphore) Release() {
	<-s
}

func main() {
	sem := NewSemaphore(2)
	var wg sync.WaitGroup

	for i := 1; i <= 6; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if !sem.TryAcquire(150 * time.Millisecond) {
				fmt.Printf("worker %d: timed out waiting for semaphore\n", id)
				return
			}
			defer sem.Release()
			fmt.Printf("worker %d: acquired, working...\n", id)
			time.Sleep(200 * time.Millisecond)
			fmt.Printf("worker %d: done\n", id)
		}(i)
	}

	wg.Wait()
	fmt.Println("all done")
}
```

### Intermediate Verification

```bash
go run main.go
```

Some workers acquire the semaphore and complete; others time out. The first 2 get in immediately, but the remaining 4 compete for slots and some will exceed the 150ms timeout.

## Step 3 -- Barrier Pattern

A barrier forces all goroutines to wait until everyone has arrived before any of them proceed:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type Barrier struct {
	n       int
	count   int
	mu      sync.Mutex
	release chan struct{}
}

func NewBarrier(n int) *Barrier {
	return &Barrier{
		n:       n,
		release: make(chan struct{}),
	}
}

func (b *Barrier) Wait() {
	b.mu.Lock()
	b.count++
	if b.count == b.n {
		close(b.release) // last arrival opens the gate
		b.mu.Unlock()
		return
	}
	b.mu.Unlock()
	<-b.release // wait for the gate to open
}

func main() {
	const numWorkers = 4
	barrier := NewBarrier(numWorkers)
	var wg sync.WaitGroup

	for i := 1; i <= numWorkers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Phase 1: each worker takes a different amount of time
			work := time.Duration(id*50) * time.Millisecond
			time.Sleep(work)
			fmt.Printf("worker %d: phase 1 done (took %v)\n", id, work)

			barrier.Wait()

			// Phase 2: all start together
			fmt.Printf("worker %d: phase 2 started at %v\n", id, time.Now().UnixMilli()%1000)
		}(i)
	}

	wg.Wait()
	fmt.Println("all phases complete")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: all "phase 1 done" messages appear before any "phase 2 started" messages. Phase 2 timestamps should be nearly identical.

## Step 4 -- Reusable Barrier (Cyclic Barrier)

The single-use barrier from Step 3 can only fire once. A cyclic barrier resets after each use:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type CyclicBarrier struct {
	n       int
	count   int
	gen     int
	mu      sync.Mutex
	cond    *sync.Cond
}

func NewCyclicBarrier(n int) *CyclicBarrier {
	b := &CyclicBarrier{n: n}
	b.cond = sync.NewCond(&b.mu)
	return b
}

func (b *CyclicBarrier) Wait() {
	b.mu.Lock()
	gen := b.gen
	b.count++
	if b.count == b.n {
		b.count = 0
		b.gen++
		b.cond.Broadcast()
		b.mu.Unlock()
		return
	}
	for gen == b.gen {
		b.cond.Wait()
	}
	b.mu.Unlock()
}

func main() {
	const numWorkers = 3
	barrier := NewCyclicBarrier(numWorkers)
	var wg sync.WaitGroup

	for i := 1; i <= numWorkers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for round := 1; round <= 3; round++ {
				time.Sleep(time.Duration(id*20) * time.Millisecond)
				fmt.Printf("worker %d: round %d ready\n", id, round)
				barrier.Wait()
				fmt.Printf("worker %d: round %d go!\n", id, round)
			}
		}(i)
	}

	wg.Wait()
	fmt.Println("all rounds complete")
}
```

### Intermediate Verification

```bash
go run main.go
```

For each round, all "ready" messages appear before any "go!" messages of that round.

## Step 5 -- Countdown Latch

A countdown latch blocks waiters until a counter reaches zero. Unlike a barrier, the goroutines that decrement the counter and the goroutines that wait on it are different:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type CountdownLatch struct {
	count int
	mu    sync.Mutex
	done  chan struct{}
}

func NewCountdownLatch(n int) *CountdownLatch {
	return &CountdownLatch{
		count: n,
		done:  make(chan struct{}),
	}
}

func (l *CountdownLatch) CountDown() {
	l.mu.Lock()
	l.count--
	if l.count <= 0 {
		close(l.done)
	}
	l.mu.Unlock()
}

func (l *CountdownLatch) Wait() {
	<-l.done
}

func main() {
	latch := NewCountdownLatch(3)

	// Three initialization tasks
	tasks := []string{"load config", "connect db", "warm cache"}
	for _, task := range tasks {
		go func(t string) {
			fmt.Printf("  [init] %s starting\n", t)
			time.Sleep(100 * time.Millisecond)
			fmt.Printf("  [init] %s done\n", t)
			latch.CountDown()
		}(task)
	}

	// Main thread waits for all init tasks
	fmt.Println("waiting for initialization...")
	latch.Wait()
	fmt.Println("system ready -- serving requests")

	// Multiple waiters work too (latch is already done)
	latch.Wait() // returns immediately
	fmt.Println("second wait returned immediately")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
waiting for initialization...
  [init] load config starting
  [init] connect db starting
  [init] warm cache starting
  [init] load config done
  [init] connect db done
  [init] warm cache done
system ready -- serving requests
second wait returned immediately
```

## Common Mistakes

### Using an Unbuffered Channel as a Semaphore

An unbuffered channel has capacity 0, which means the semaphore would allow 0 concurrent operations -- every acquire blocks immediately with no one to release it. Always use a buffered channel for semaphores.

### Forgetting to Release the Semaphore on Error Paths

Use `defer sem.Release()` immediately after `Acquire()` to guarantee release even if the function panics or returns early.

### Barrier Deadlock with Wrong Count

If you create a `NewBarrier(5)` but only 4 goroutines call `Wait()`, the program deadlocks. The barrier count must exactly match the number of participants.

## Verify What You Learned

Build a web scraper simulator that:
1. Uses a semaphore to limit concurrent HTTP requests to 5
2. Uses a countdown latch to wait until all URLs are processed
3. Prints the results only after the latch reaches zero

## What's Next

Continue to [13 - Goroutine Pools](../13-goroutine-pools/13-goroutine-pools.md) to learn how to build fixed-size worker pools that process jobs from a shared queue.

## Summary

- A buffered channel of size N is a counting semaphore allowing N concurrent operations
- `select` with `time.After` adds timeout semantics to semaphore acquisition
- A barrier synchronizes goroutines at a rendezvous point using a closed channel
- A cyclic barrier resets after each round using a generation counter and `sync.Cond`
- A countdown latch blocks waiters until N events occur, using a channel closed at zero

## Reference

- [Go Blog: Share Memory By Communicating](https://go.dev/blog/codelab-share)
- [Go spec: Channel types](https://go.dev/ref/spec#Channel_types)
- [sync.Cond documentation](https://pkg.go.dev/sync#Cond)
- [Java CountDownLatch (concept reference)](https://docs.oracle.com/javase/8/docs/api/java/util/concurrent/CountDownLatch.html)
