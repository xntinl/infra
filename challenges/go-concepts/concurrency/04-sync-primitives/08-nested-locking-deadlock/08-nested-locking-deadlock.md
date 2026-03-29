---
difficulty: advanced
concepts: [deadlock, lock ordering, nested locking, deadlock detection, circular wait]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [sync.Mutex, goroutines, sync.WaitGroup]
---

# 8. Nested Locking and Deadlock


## Learning Objectives
After completing this exercise, you will be able to:
- **Identify** deadlock conditions caused by inconsistent lock ordering
- **Analyze** the "circular wait" condition in nested locking scenarios
- **Fix** deadlocks by establishing consistent lock ordering
- **Recognize** Go's runtime deadlock detector output

## Why Deadlock Prevention Matters
A deadlock occurs when two or more goroutines are each waiting for the other to release a lock, creating a circular dependency where no goroutine can proceed. The program freezes permanently with no error message beyond Go's runtime detection of "all goroutines are asleep."

Deadlocks from nested locking are particularly dangerous because:
- They may not manifest during testing if the timing is just right
- They require specific interleaving of goroutine execution to trigger
- They are invisible at compile time
- Once triggered, the only recovery is killing the process

The four conditions for deadlock (Coffman conditions):
1. **Mutual exclusion**: at least one resource is non-shareable (mutex)
2. **Hold and wait**: a goroutine holds one lock while waiting for another
3. **No preemption**: locks can only be released voluntarily
4. **Circular wait**: goroutine A waits for B's lock, B waits for A's lock

Breaking any one condition prevents deadlock. The most practical approach in Go is to break the **circular wait** by establishing a **consistent lock ordering**: always acquire locks in the same order, regardless of which goroutine is executing.

## Step 1 -- Understand the Deadlock

The `createDeadlock` function (commented out in main) demonstrates a classic two-mutex deadlock:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	var muA, muB sync.Mutex
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		muA.Lock()
		fmt.Println("G1: locked A")
		time.Sleep(50 * time.Millisecond) // give G2 time to lock B
		fmt.Println("G1: waiting for B...")
		muB.Lock() // BLOCKED: G2 holds B
		muB.Unlock()
		muA.Unlock()
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		muB.Lock()
		fmt.Println("G2: locked B")
		time.Sleep(50 * time.Millisecond) // give G1 time to lock A
		fmt.Println("G2: waiting for A...")
		muA.Lock() // BLOCKED: G1 holds A
		muA.Unlock()
		muB.Unlock()
	}()

	wg.Wait() // never returns
}
```

Expected output (then freeze):
```
G1: locked A
G2: locked B
G1: waiting for B...
G2: waiting for A...
fatal error: all goroutines are asleep - deadlock!
```

### Intermediate Verification
Uncomment `createDeadlock()` in `main.go` to see it. Press Ctrl+C to kill. Go's runtime detects the deadlock because ALL goroutines are blocked.

## Step 2 -- Analyze the Circular Wait

```
Timeline:
  T0: G1 locks A, G2 locks B         (both succeed)
  T1: G1 wants B (held by G2)        -- G1 BLOCKED
      G2 wants A (held by G1)        -- G2 BLOCKED

  G1 --> waits for B --> held by G2
  G2 --> waits for A --> held by G1

  Circular dependency! Neither can proceed.
```

The root cause: G1 acquires locks in order `A, B` while G2 acquires them in order `B, A`. This inconsistency creates the possibility of circular wait.

## Step 3 -- Fix with Consistent Lock Ordering

Both goroutines must acquire locks in the SAME order:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	var muA, muB sync.Mutex
	var wg sync.WaitGroup

	// Both goroutines: always A first, then B
	wg.Add(1)
	go func() {
		defer wg.Done()
		muA.Lock()
		fmt.Println("G1: locked A")
		time.Sleep(50 * time.Millisecond)
		muB.Lock()
		fmt.Println("G1: locked B")
		muB.Unlock()
		muA.Unlock()
		fmt.Println("G1: released both")
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		muA.Lock() // same order as G1
		fmt.Println("G2: locked A")
		time.Sleep(50 * time.Millisecond)
		muB.Lock()
		fmt.Println("G2: locked B")
		muB.Unlock()
		muA.Unlock()
		fmt.Println("G2: released both")
	}()

	wg.Wait()
	fmt.Println("No deadlock!")
}
```

Expected output:
```
G1: locked A
G1: locked B
G1: released both
G2: locked A
G2: locked B
G2: released both
No deadlock!
```

### Intermediate Verification
```bash
go run main.go
```
Both goroutines complete. One runs first (holding A then B), then the other.

## Step 4 -- Realistic Example: Safe Account Transfers

Transfer money between accounts by always locking the lower-ID account first:

```go
package main

import (
	"fmt"
	"sync"
)

type Account struct {
	id      int
	mu      sync.Mutex
	balance int
}

func transferSafe(from, to *Account, amount int) bool {
	// Always lock the lower-ID account first
	first, second := from, to
	if from.id > to.id {
		first, second = to, from
	}

	first.mu.Lock()
	defer first.mu.Unlock()
	second.mu.Lock()
	defer second.mu.Unlock()

	if from.balance < amount {
		return false
	}
	from.balance -= amount
	to.balance += amount
	return true
}

func main() {
	accounts := []*Account{
		{id: 1, balance: 1000},
		{id: 2, balance: 1000},
		{id: 3, balance: 1000},
	}

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			from := accounts[i%3]
			to := accounts[(i+1)%3]
			transferSafe(from, to, 10)
		}(i)
	}

	wg.Wait()

	total := 0
	for _, a := range accounts {
		fmt.Printf("Account %d: %d\n", a.id, a.balance)
		total += a.balance
	}
	fmt.Printf("Total: %d (should be 3000)\n", total)
}
```

Expected output:
```
Account 1: XXX
Account 2: XXX
Account 3: XXX
Total: 3000 (should be 3000)
```

### Intermediate Verification
```bash
go run -race main.go
```
No deadlocks, no data races. Total money is conserved.

## Common Mistakes

### Locking Based on Caller Order

```go
func transfer(from, to *Account, amount int) {
    from.mu.Lock()   // depends on which account is "from"
    to.mu.Lock()     // different callers may reverse this
    // ...
}
```

**What happens:** `transfer(A, B, 100)` and `transfer(B, A, 50)` running concurrently create a deadlock.

**Fix:** Lock based on a stable ordering (ID, address, etc.), not the parameter names.

### Assuming the Runtime Always Detects Deadlocks
Go's deadlock detector only triggers when ALL goroutines are blocked. In a real server with a listening goroutine, deadlocks between other goroutines go undetected. The program hangs partially, which is even worse than a full deadlock.

### Lock Escalation
Acquiring more locks while already holding one is inherently risky. Minimize nested locking. If you must, document the lock ordering invariant clearly.

### Trying to Detect Deadlocks with Timeouts

```go
// Tempting but fragile
select {
case <-time.After(5 * time.Second):
    log.Fatal("possible deadlock")
}
```

This hides the real problem. Fix the lock ordering instead.

## Verify What You Learned

Implement a dining philosophers problem with 5 philosophers and 5 forks. First, show the deadlock when each philosopher picks up their left fork then their right. Then fix it using consistent lock ordering (pick up the lower-numbered fork first).

## What's Next
Continue to [09-sync-map-concurrent-access](../09-sync-map-concurrent-access/09-sync-map-concurrent-access.md) to learn how `sync.Map` provides a concurrent-safe map without external locking.

## Summary
- Deadlock occurs when goroutines form a circular dependency waiting for locks
- Go's runtime detects deadlocks only when ALL goroutines are blocked
- The primary fix is consistent lock ordering: always acquire locks in the same global order
- Use a stable key (ID, memory address) to determine lock order, not parameter position
- Minimize nested locking: if you can avoid holding two locks at once, do so
- Document lock ordering invariants for any code that acquires multiple locks
- Test with `-race` and high concurrency to surface timing-dependent deadlocks

## Reference
- [Go Runtime Deadlock Detection](https://pkg.go.dev/runtime#hdr-Detecting_Deadlocks)
- [Coffman Conditions (Wikipedia)](https://en.wikipedia.org/wiki/Deadlock#Necessary_conditions)
- [Go FAQ: Goroutines and Threads](https://go.dev/doc/faq#goroutines)
