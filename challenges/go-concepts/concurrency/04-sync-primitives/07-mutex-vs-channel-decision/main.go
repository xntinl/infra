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
// TODO: Implement Deposit, Withdraw, and Balance methods using mu.Lock/Unlock.
type MutexAccount struct {
	mu      sync.Mutex
	balance int
}

func (a *MutexAccount) Deposit(amount int) {
	// TODO: Lock, update balance, Unlock
	a.balance += amount
}

func (a *MutexAccount) Withdraw(amount int) bool {
	// TODO: Lock, check balance, subtract if sufficient, Unlock
	if a.balance < amount {
		return false
	}
	a.balance -= amount
	return true
}

func (a *MutexAccount) Balance() int {
	// TODO: Lock, read balance, Unlock
	return a.balance
}

// ---------------------------------------------------------------------------
// Approach 2: Channel-based account
// ---------------------------------------------------------------------------

type accountOp struct {
	kind     string // "deposit", "withdraw", "balance"
	amount   int
	response chan accountResult
}

type accountResult struct {
	balance int
	ok      bool
}

// ChannelAccount uses a goroutine as the single owner of the balance.
// All operations are sent as messages via a channel.
type ChannelAccount struct {
	ops  chan accountOp
	done chan struct{}
}

// NewChannelAccount creates an account and starts the owner goroutine.
// TODO: Implement the run method that processes operations from the ops channel.
func NewChannelAccount(initialBalance int) *ChannelAccount {
	a := &ChannelAccount{
		ops:  make(chan accountOp),
		done: make(chan struct{}),
	}
	go a.run(initialBalance)
	return a
}

// run is the goroutine that owns the balance.
// It processes operations sequentially -- no mutex needed.
// TODO: Range over a.ops and handle deposit/withdraw/balance operations.
func (a *ChannelAccount) run(balance int) {
	for op := range a.ops {
		switch op.kind {
		case "deposit":
			balance += op.amount
			op.response <- accountResult{balance: balance, ok: true}
		case "withdraw":
			// TODO: check balance, subtract if sufficient
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

// Deposit sends a deposit operation to the owner goroutine.
func (a *ChannelAccount) Deposit(amount int) {
	resp := make(chan accountResult)
	a.ops <- accountOp{kind: "deposit", amount: amount, response: resp}
	<-resp
}

// Withdraw sends a withdraw operation to the owner goroutine.
func (a *ChannelAccount) Withdraw(amount int) bool {
	resp := make(chan accountResult)
	a.ops <- accountOp{kind: "withdraw", amount: amount, response: resp}
	result := <-resp
	return result.ok
}

// Balance sends a balance query to the owner goroutine.
func (a *ChannelAccount) Balance() int {
	resp := make(chan accountResult)
	a.ops <- accountOp{kind: "balance", response: resp}
	result := <-resp
	return result.balance
}

// Close shuts down the owner goroutine.
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
	fmt.Println("=== Mutex Account ===")
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

	fmt.Printf("Final balance: %d (expected: 1500)\n", ma.Balance())
}

func testChannelAccount() {
	fmt.Println("\n=== Channel Account ===")
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
}

// compareApproaches runs the same random workload on both implementations
// and measures execution time.
// TODO: Run runWorkload on both MutexAccount and ChannelAccount,
// print balances and execution times.
func compareApproaches() {
	fmt.Println("\n=== Comparison ===")

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

	// TODO: Channel approach -- same workload
	// ca := NewChannelAccount(10000)
	// ...
	// ca.Close()

	fmt.Println("TODO: implement channel approach benchmark")
}

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
