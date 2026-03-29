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

## Step 1 -- Problem: Concurrent Bank Account

The same problem, two solutions. A bank account supports concurrent deposits and withdrawals, and must report the final balance correctly.

Open `main.go`. Implement the mutex-based solution first:

```go
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
```

### Intermediate Verification
```bash
go run main.go
```
The mutex account should show the correct final balance after 1000 concurrent operations.

## Step 2 -- Channel-Based Solution

Implement the same account using channels. A single goroutine owns the balance; all operations are sent as messages:

```go
type ChannelAccount struct {
    ops     chan accountOp
    done    chan struct{}
}

type accountOp struct {
    kind     string // "deposit", "withdraw", "balance"
    amount   int
    response chan accountResult
}

type accountResult struct {
    balance int
    ok      bool
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
    result := <-resp
    return result.ok
}

func (a *ChannelAccount) Balance() int {
    resp := make(chan accountResult)
    a.ops <- accountOp{kind: "balance", response: resp}
    result := <-resp
    return result.balance
}

func (a *ChannelAccount) Close() {
    close(a.ops)
    <-a.done
}
```

### Intermediate Verification
```bash
go run main.go
```
Both accounts should produce identical final balances for the same sequence of operations.

## Step 3 -- Compare the Approaches

Implement `compareApproaches` to run the same workload on both:

```go
func compareApproaches() {
    fmt.Println("=== Comparison ===")

    const goroutines = 100
    const opsPerGoroutine = 1000

    // Mutex approach
    ma := &MutexAccount{balance: 10000}
    start := time.Now()
    runWorkload(
        goroutines, opsPerGoroutine,
        func(amount int) { ma.Deposit(amount) },
        func(amount int) bool { return ma.Withdraw(amount) },
    )
    mutexTime := time.Since(start)
    fmt.Printf("Mutex:   balance=%d, time=%v\n", ma.Balance(), mutexTime)

    // Channel approach
    ca := NewChannelAccount(10000)
    start = time.Now()
    runWorkload(
        goroutines, opsPerGoroutine,
        func(amount int) { ca.Deposit(amount) },
        func(amount int) bool { return ca.Withdraw(amount) },
    )
    channelTime := time.Since(start)
    fmt.Printf("Channel: balance=%d, time=%v\n", ca.Balance(), channelTime)
    ca.Close()
}
```

### Intermediate Verification
```bash
go run -race main.go
```
Both should be race-free and produce consistent balances.

## Step 4 -- Analyze When to Use Which

Implement `decisionGuide` that prints the decision framework:

```go
func decisionGuide() {
    fmt.Println("\n=== Decision Guide ===")
    fmt.Println("Use MUTEX when:")
    fmt.Println("  - Protecting internal state of a struct")
    fmt.Println("  - Simple read/write access patterns")
    fmt.Println("  - Performance is critical (lower overhead)")
    fmt.Println("  - The protected data has a clear owner")
    fmt.Println()
    fmt.Println("Use CHANNELS when:")
    fmt.Println("  - Transferring data ownership between goroutines")
    fmt.Println("  - Coordinating sequential phases of work (pipelines)")
    fmt.Println("  - Fan-out/fan-in patterns")
    fmt.Println("  - Select-based multiplexing with timeouts/cancellation")
    fmt.Println()
    fmt.Println("Go Proverb: 'Do not communicate by sharing memory;")
    fmt.Println("             share memory by communicating.'")
    fmt.Println()
    fmt.Println("Translation: If goroutines need to TALK, use channels.")
    fmt.Println("             If a struct needs to be SAFE, use a mutex.")
}
```

### Intermediate Verification
The guide should print clearly and help internalize the decision criteria.

## Common Mistakes

### Channel as a Mutex
**Questionable:**
```go
sem := make(chan struct{}, 1)
sem <- struct{}{} // "lock"
counter++
<-sem             // "unlock"
```
**Why:** This works but is a mutex in disguise. A real `sync.Mutex` is clearer, lighter, and has better tooling support (race detector, deadlock detection).

### Mutex for Pipeline Coordination
**Questionable:**
```go
var mu sync.Mutex
var phase1Done, phase2Done bool

go func() {
    doPhase1()
    mu.Lock()
    phase1Done = true
    mu.Unlock()
}()

// Polling loop to wait for phase 1
for {
    mu.Lock()
    if phase1Done { mu.Unlock(); break }
    mu.Unlock()
    time.Sleep(time.Millisecond)
}
```
**Why:** This is coordination, not state protection. A channel is far cleaner:
```go
phase1Done := make(chan struct{})
go func() {
    doPhase1()
    close(phase1Done)
}()
<-phase1Done
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
