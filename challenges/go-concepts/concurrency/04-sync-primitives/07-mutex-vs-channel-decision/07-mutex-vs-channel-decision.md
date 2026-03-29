# 7. Mutex vs Channel: Decision Criteria

<!--
difficulty: intermediate
concepts: [mutex vs channel, share memory by communicating, state ownership, Go proverb]
tools: [go]
estimated_time: 25m
bloom_level: analyze
prerequisites: [sync.Mutex, channels, goroutines, sync.WaitGroup]
-->

## Prerequisites
- Go 1.22+ installed
- Understanding of `sync.Mutex` (exercise 01)
- Familiarity with Go channels (section 02)
- Ability to reason about goroutine communication

## Learning Objectives
After completing this exercise, you will be able to:
- **Solve** the same concurrency problem using both mutex and channel approaches
- **Compare** code clarity, safety, and performance of each approach
- **Apply** the decision framework: mutex for protecting state, channels for communication
- **Explain** the Go proverb "share memory by communicating"

## Why This Decision Matters
Go provides two fundamental mechanisms for concurrent coordination: mutexes and channels. Both are correct; neither is universally better. Choosing the wrong tool leads to code that is harder to understand, harder to maintain, and more prone to subtle bugs.

The Go proverb says: **"Do not communicate by sharing memory; share memory by communicating."** This does not mean "never use mutexes." It means: when goroutines need to exchange information or coordinate work, channels are usually clearer. When goroutines need to protect a piece of shared state from concurrent access, mutexes are usually simpler.

The decision framework:
- **Mutex** when you are protecting internal state (a counter, a cache, a configuration map). The state belongs to a struct; the mutex guards access.
- **Channel** when you are transferring ownership of data, coordinating phases of work, or signaling events between goroutines.
- **Guideline**: if your channel is used as a mutex (e.g., buffered channel of size 1 used as a semaphore with no data flow), consider an actual mutex. If your mutex is being locked and unlocked across multiple goroutines to coordinate steps, consider a channel.

## Step 1 -- Mutex-Based Bank Account

Run `main.go`. The mutex version protects the balance as a struct field:

```go
package main

import (
	"fmt"
	"sync"
)

type MutexAccount struct {
	mu      sync.Mutex
	balance int
}

func (a *MutexAccount) Deposit(amount int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.balance += amount
}

func (a *MutexAccount) Withdraw(amount int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.balance < amount {
		return false
	}
	a.balance -= amount
	return true
}

func (a *MutexAccount) Balance() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.balance
}

func main() {
	ma := &MutexAccount{balance: 1000}
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ma.Deposit(10)
			ma.Withdraw(5)
		}()
	}
	wg.Wait()
	fmt.Printf("Balance: %d (expected: 1500)\n", ma.Balance())
}
```

Expected output:
```
Balance: 1500 (expected: 1500)
```

### Intermediate Verification
```bash
go run -race main.go
```
No race conditions. Balance is exactly 1500 (1000 + 100*5).

## Step 2 -- Channel-Based Bank Account

The channel version uses a single goroutine as the exclusive owner of the balance:

```go
package main

import (
	"fmt"
	"sync"
)

type accountOp struct {
	kind     string
	amount   int
	response chan accountResult
}

type accountResult struct {
	balance int
	ok      bool
}

type ChannelAccount struct {
	ops  chan accountOp
	done chan struct{}
}

func NewChannelAccount(initialBalance int) *ChannelAccount {
	a := &ChannelAccount{
		ops:  make(chan accountOp),
		done: make(chan struct{}),
	}
	go a.run(initialBalance)
	return a
}

func (a *ChannelAccount) run(balance int) {
	for op := range a.ops {
		switch op.kind {
		case "deposit":
			balance += op.amount
			op.response <- accountResult{balance: balance, ok: true}
		case "withdraw":
			if balance >= op.amount {
				balance -= op.amount
				op.response <- accountResult{balance: balance, ok: true}
			} else {
				op.response <- accountResult{balance: balance, ok: false}
			}
		case "balance":
			op.response <- accountResult{balance: balance, ok: true}
		}
	}
	close(a.done)
}

func (a *ChannelAccount) Deposit(amount int) {
	resp := make(chan accountResult)
	a.ops <- accountOp{kind: "deposit", amount: amount, response: resp}
	<-resp
}

func (a *ChannelAccount) Withdraw(amount int) bool {
	resp := make(chan accountResult)
	a.ops <- accountOp{kind: "withdraw", amount: amount, response: resp}
	return (<-resp).ok
}

func (a *ChannelAccount) Balance() int {
	resp := make(chan accountResult)
	a.ops <- accountOp{kind: "balance", response: resp}
	return (<-resp).balance
}

func (a *ChannelAccount) Close() {
	close(a.ops)
	<-a.done
}

func main() {
	ca := NewChannelAccount(1000)
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ca.Deposit(10)
			ca.Withdraw(5)
		}()
	}
	wg.Wait()
	fmt.Printf("Balance: %d (expected: 1500)\n", ca.Balance())
	ca.Close()
}
```

Expected output:
```
Balance: 1500 (expected: 1500)
```

### Intermediate Verification
```bash
go run -race main.go
```
Both accounts produce identical results for the same operations.

## Step 3 -- Compare Performance

The program benchmarks 100 goroutines doing 1000 random operations each:

```bash
go run main.go
```

Expected output:
```
Mutex:   balance=XXXX, time=15ms
Channel: balance=XXXX, time=85ms
Mutex is typically faster for simple state protection.
```

The mutex version is faster because each operation is a simple lock/unlock. The channel version requires channel send, goroutine scheduling, channel receive -- more overhead per operation. The channel version's advantage is clarity of ownership, not speed.

## Step 4 -- Decision Guide

The program prints a decision framework:

```
Use MUTEX when:
  - Protecting internal state of a struct
  - Simple read/write access patterns
  - Performance is critical (lower overhead)
  - The protected data has a clear owner

Use CHANNELS when:
  - Transferring data ownership between goroutines
  - Coordinating sequential phases of work (pipelines)
  - Fan-out/fan-in patterns
  - Select-based multiplexing with timeouts/cancellation
```

## Common Mistakes

### Channel as a Mutex

```go
sem := make(chan struct{}, 1)
sem <- struct{}{} // "lock"
counter++
<-sem             // "unlock"
```

**Why this is a code smell:** It works but is a mutex in disguise. A real `sync.Mutex` is clearer, lighter, and has better tooling support (race detector, deadlock detection).

### Mutex for Pipeline Coordination

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	var mu sync.Mutex
	var phase1Done bool

	go func() {
		time.Sleep(100 * time.Millisecond)
		mu.Lock()
		phase1Done = true
		mu.Unlock()
	}()

	// Polling loop -- wasteful and ugly
	for {
		mu.Lock()
		done := phase1Done
		mu.Unlock()
		if done {
			break
		}
		time.Sleep(time.Millisecond)
	}
	fmt.Println("phase 1 done (but this code is terrible)")
}
```

**Why this is a code smell:** This is coordination, not state protection. A channel is far cleaner:
```go
phase1Done := make(chan struct{})
go func() {
    doPhase1()
    close(phase1Done)
}()
<-phase1Done // blocks cleanly, no polling
```

### Over-Channeling Simple State
Not every shared variable needs a channel. A cache miss counter, a request count, a configuration flag -- these are naturally protected by a mutex or even `sync/atomic`.

## Verify What You Learned

Implement a concurrent rate limiter two ways:
1. With a mutex: track timestamps of recent requests, reject if rate exceeded
2. With a channel: use a buffered channel as a token bucket

Compare code clarity and correctness under concurrent access from 50 goroutines.

## What's Next
Continue to [08-nested-locking-deadlock](../08-nested-locking-deadlock/08-nested-locking-deadlock.md) to learn how nested lock acquisition leads to deadlocks and how to prevent them.

## Summary
- Both mutexes and channels are valid concurrency tools; neither is universally better
- Mutex excels at protecting internal state of a struct (counter, cache, map)
- Channels excel at transferring data, coordinating work phases, and signaling events
- Using a channel as a mutex or a mutex for coordination are code smells
- The Go proverb is guidance, not dogma: choose the tool that makes the code clearest
- When in doubt: if a struct owns the data, use a mutex; if goroutines pass data, use a channel

## Reference
- [Go Wiki: MutexOrChannel](https://go.dev/wiki/MutexOrChannel)
- [Effective Go: Concurrency](https://go.dev/doc/effective_go#concurrency)
- [Go Proverbs: Rob Pike](https://go-proverbs.github.io/)
- [Bryan Mills - Rethinking Classical Concurrency Patterns](https://www.youtube.com/watch?v=5zXAHh5tJqQ)
