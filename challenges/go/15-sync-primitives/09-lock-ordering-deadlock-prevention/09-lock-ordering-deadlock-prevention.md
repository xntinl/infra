# 9. Lock Ordering and Deadlock Prevention

<!--
difficulty: advanced
concepts: [deadlock, lock-ordering, consistent-locking, timeout-patterns]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [sync-mutex, goroutines, channels]
-->

## Prerequisites

- Go 1.22+ installed
- Solid understanding of `sync.Mutex` and goroutines
- Experience with concurrent programs

## Learning Objectives

After completing this exercise, you will be able to:

- **Identify** conditions that lead to deadlocks in Go programs
- **Implement** consistent lock ordering to prevent deadlocks
- **Analyze** code for potential deadlock scenarios and apply fixes

## Why Lock Ordering Matters

A deadlock occurs when two or more goroutines each hold a lock and wait for a lock held by the other. The classic scenario:

- Goroutine A locks mutex 1, then tries to lock mutex 2
- Goroutine B locks mutex 2, then tries to lock mutex 1
- Both wait forever

The simplest prevention technique is **consistent lock ordering**: always acquire multiple locks in the same order. If every goroutine acquires mutex 1 before mutex 2, deadlock is impossible.

Go's runtime detects some deadlocks (when all goroutines are blocked) and panics with "fatal error: all goroutines are asleep - deadlock!", but this only catches cases where every goroutine is stuck. Partial deadlocks with some goroutines still running go undetected.

## The Problem

Build a bank transfer system where accounts have individual mutexes. Implement a `Transfer` function that locks both the source and destination accounts without deadlocking, even when two goroutines transfer between the same accounts in opposite directions.

## Requirements

1. Each account has its own mutex and a balance
2. Transfers must lock both accounts to ensure atomicity
3. Concurrent transfers between the same accounts must not deadlock
4. Use consistent lock ordering based on account ID to prevent deadlocks
5. Handle the case where source and destination are the same account

## Hints

<details>
<summary>Hint 1: The Deadlock</summary>

Without ordering, this deadlocks:
```go
// Goroutine 1: Transfer(A, B)
A.mu.Lock()   // holds A
B.mu.Lock()   // waits for B

// Goroutine 2: Transfer(B, A)
B.mu.Lock()   // holds B
A.mu.Lock()   // waits for A -- DEADLOCK
```
</details>

<details>
<summary>Hint 2: Lock Ordering by ID</summary>

Always lock the account with the lower ID first:

```go
if a.ID < b.ID {
    a.mu.Lock()
    b.mu.Lock()
} else {
    b.mu.Lock()
    a.mu.Lock()
}
```
</details>

<details>
<summary>Hint 3: Complete Implementation</summary>

```go
package main

import (
	"fmt"
	"sync"
)

type Account struct {
	ID      int
	mu      sync.Mutex
	Balance float64
}

func Transfer(from, to *Account, amount float64) error {
	if from.ID == to.ID {
		return fmt.Errorf("cannot transfer to same account")
	}

	// Always lock lower ID first to prevent deadlock
	first, second := from, to
	if from.ID > to.ID {
		first, second = to, from
	}

	first.mu.Lock()
	defer first.mu.Unlock()
	second.mu.Lock()
	defer second.mu.Unlock()

	if from.Balance < amount {
		return fmt.Errorf("insufficient funds: have %.2f, need %.2f", from.Balance, amount)
	}

	from.Balance -= amount
	to.Balance += amount
	return nil
}

func main() {
	accounts := []*Account{
		{ID: 1, Balance: 1000},
		{ID: 2, Balance: 1000},
		{ID: 3, Balance: 1000},
	}

	var wg sync.WaitGroup

	// Many concurrent transfers between all pairs
	for i := 0; i < 100; i++ {
		wg.Add(2)
		// Transfer A -> B
		go func() {
			defer wg.Done()
			Transfer(accounts[0], accounts[1], 10)
		}()
		// Transfer B -> A (opposite direction, same pair)
		go func() {
			defer wg.Done()
			Transfer(accounts[1], accounts[0], 10)
		}()
	}

	// Also transfer between other pairs
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Transfer(accounts[0], accounts[2], 5)
		}()
	}

	wg.Wait()

	total := 0.0
	for _, acc := range accounts {
		fmt.Printf("Account %d: $%.2f\n", acc.ID, acc.Balance)
		total += acc.Balance
	}
	fmt.Printf("Total: $%.2f (should be $3000.00)\n", total)
}
```
</details>

## Verification

```bash
go run -race main.go
```

Expected: The program completes without deadlock or race conditions. The total across all accounts remains $3000.00 (money is conserved).

## What's Next

Continue to [10 - Mutex vs Channel](../10-mutex-vs-channel/10-mutex-vs-channel.md) to learn the decision framework for choosing between mutexes and channels.

## Summary

- Deadlocks occur when goroutines hold locks and wait for each other circularly
- Consistent lock ordering (always lock lower ID first) prevents deadlocks
- Go's runtime only detects deadlocks when all goroutines are blocked
- Always check for same-account edge cases when locking multiple resources
- When lock ordering is impractical, consider `tryLock` patterns or channel-based designs

## Reference

- [Go FAQ: Why does my program deadlock?](https://go.dev/doc/faq#goroutines)
- [Dining Philosophers Problem (Wikipedia)](https://en.wikipedia.org/wiki/Dining_philosophers_problem)
- [Go Memory Model](https://go.dev/ref/mem)
