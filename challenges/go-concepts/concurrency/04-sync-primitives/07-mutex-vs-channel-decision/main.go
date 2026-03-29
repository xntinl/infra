// Exercise 07: Mutex vs Channel -- Decision Criteria
//
// The same bank account problem solved with both mutex and channels.
// Demonstrates when to choose each approach and why.
//
// Expected output:
//
//   === 1. Mutex Account ===
//   Final balance: 1500 (expected: 1500)
//
//   === 2. Channel Account ===
//   Final balance: 1500 (expected: 1500)
//
//   === 3. Performance Comparison ===
//   Mutex:   balance=XXXX, time=Xms
//   Channel: balance=XXXX, time=Xms
//   Mutex is typically faster for simple state protection.
//
//   === 4. Decision Guide ===
//   Use MUTEX when:
//     - Protecting internal state of a struct
//     - Simple read/write access patterns
//     - Performance is critical (lower overhead)
//     - The protected data has a clear owner
//
//   Use CHANNELS when:
//     - Transferring data ownership between goroutines
//     - Coordinating sequential phases of work (pipelines)
//     - Fan-out/fan-in patterns
//     - Select-based multiplexing with timeouts/cancellation
//
//   Go Proverb: "Do not communicate by sharing memory;
//                share memory by communicating."
//
// Run: go run main.go

package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Approach 1: Mutex-based account
// ---------------------------------------------------------------------------

// MutexAccount protects its balance with a sync.Mutex.
// This is the natural choice when a struct owns data and needs to make it
// safe for concurrent access. The mutex is an implementation detail -- callers
// do not need to know about it.
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

// ---------------------------------------------------------------------------
// Approach 2: Channel-based account
// ---------------------------------------------------------------------------

// ChannelAccount uses a single goroutine as the exclusive owner of the balance.
// All operations are sent as messages through a channel. The owner goroutine
// processes them sequentially -- no mutex needed because there is no shared state.
type ChannelAccount struct {
	ops  chan accountOp
	done chan struct{}
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

// run is the owner goroutine. It processes operations sequentially from the
// ops channel. Since only this goroutine touches the balance, no lock is needed.
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

// ---------------------------------------------------------------------------
// Workload runner
// ---------------------------------------------------------------------------

func runWorkload(goroutines, opsPerGoroutine int, deposit func(int), withdraw func(int) bool) {
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				amount := rand.Intn(10) + 1
				if rand.Intn(2) == 0 {
					deposit(amount)
				} else {
					withdraw(amount)
				}
			}
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	testMutexAccount()
	testChannelAccount()
	compareApproaches()
	decisionGuide()
}

func testMutexAccount() {
	fmt.Println("=== 1. Mutex Account ===")
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

	// Each goroutine: +10, -5 = +5. 100 goroutines: 100 * 5 = 500. 1000 + 500 = 1500.
	fmt.Printf("Final balance: %d (expected: 1500)\n", ma.Balance())
	fmt.Println()
}

func testChannelAccount() {
	fmt.Println("=== 2. Channel Account ===")
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

	fmt.Printf("Final balance: %d (expected: 1500)\n", ca.Balance())
	ca.Close()
	fmt.Println()
}

// compareApproaches runs the same random workload on both implementations
// and measures execution time.
func compareApproaches() {
	fmt.Println("=== 3. Performance Comparison ===")

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
	fmt.Printf("Mutex:   balance=%d, time=%v\n", ma.Balance(), mutexTime.Round(time.Millisecond))

	// Channel approach -- same workload
	ca := NewChannelAccount(10000)
	start = time.Now()
	runWorkload(
		goroutines, opsPerGoroutine,
		func(amount int) { ca.Deposit(amount) },
		func(amount int) bool { return ca.Withdraw(amount) },
	)
	channelTime := time.Since(start)
	fmt.Printf("Channel: balance=%d, time=%v\n", ca.Balance(), channelTime.Round(time.Millisecond))
	ca.Close()

	if mutexTime < channelTime {
		fmt.Println("Mutex is typically faster for simple state protection.")
	} else {
		fmt.Println("Results vary; mutex generally wins for pure state protection.")
	}
	fmt.Println()
}

func decisionGuide() {
	fmt.Println("=== 4. Decision Guide ===")
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
