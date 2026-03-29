// Exercise 08: Nested Locking and Deadlock
//
// Demonstrates deadlock creation, analysis, and fix via consistent lock ordering.
// Covers: circular wait, Coffman conditions, lock ordering by ID, transfers.
//
// Expected output:
//
//   === 1. Deadlock Demonstration (commented out) ===
//   Skipping deadlock demo (would freeze the program).
//   Uncomment createDeadlock() to see it in action.
//
//   === 2. Fixed: Consistent Lock Ordering ===
//   G1: locked A
//   G1: locked B
//   G1: released both locks
//   G2: locked A
//   G2: locked B
//   G2: released both locks
//   No deadlock! Both goroutines completed.
//
//   === 3. Transfer Demo (safe ordering) ===
//   Account 1: balance=XXX
//   Account 2: balance=XXX
//   Account 3: balance=XXX
//   Total money: before=3000, after=3000 (should be equal)
//   Conservation of money verified!
//
//   === 4. Deadlock Analysis ===
//   Timeline of a deadlock:
//     T0: G1 locks A, G2 locks B
//     T1: G1 wants B (held by G2) -- BLOCKED
//         G2 wants A (held by G1) -- BLOCKED
//     Result: circular wait, program hangs.
//   ...
//
// Run: go run main.go
// NOTE: Uncomment createDeadlock() in main() to see a real deadlock.
//       Press Ctrl+C to kill the frozen program.

package main

import (
	"fmt"
	"sync"
	"time"
)

// Account represents a bank account that requires locking for transfers.
type Account struct {
	id      int
	mu      sync.Mutex
	balance int
}

func main() {
	skipDeadlockDemo()

	// Uncomment to see a real deadlock (will freeze!):
	// createDeadlock()

	fixedWithOrdering()
	transferDemo()
	deadlockAnalysis()
}

func skipDeadlockDemo() {
	fmt.Println("=== 1. Deadlock Demonstration (commented out) ===")
	fmt.Println("Skipping deadlock demo (would freeze the program).")
	fmt.Println("Uncomment createDeadlock() to see it in action.")
	fmt.Println()
}

// createDeadlock demonstrates a classic two-mutex deadlock.
// G1 locks A then B. G2 locks B then A. Circular wait ensues.
//
// WARNING: This will freeze the program! Press Ctrl+C to kill.
func createDeadlock() {
	fmt.Println("=== DEADLOCK DEMONSTRATION ===")
	fmt.Println("(This will freeze! Press Ctrl+C to kill.)")

	var muA, muB sync.Mutex
	var wg sync.WaitGroup

	// Goroutine 1: locks A then B
	wg.Add(1)
	go func() {
		defer wg.Done()
		muA.Lock()
		fmt.Println("G1: locked A")
		time.Sleep(50 * time.Millisecond) // give G2 time to lock B
		fmt.Println("G1: waiting for B...")
		muB.Lock() // BLOCKED: G2 holds B
		fmt.Println("G1: locked B")
		muB.Unlock()
		muA.Unlock()
	}()

	// Goroutine 2: locks B then A (OPPOSITE ORDER -- causes deadlock)
	wg.Add(1)
	go func() {
		defer wg.Done()
		muB.Lock()
		fmt.Println("G2: locked B")
		time.Sleep(50 * time.Millisecond) // give G1 time to lock A
		fmt.Println("G2: waiting for A...")
		muA.Lock() // BLOCKED: G1 holds A
		fmt.Println("G2: locked A")
		muA.Unlock()
		muB.Unlock()
	}()

	wg.Wait()
	fmt.Println("This line is never reached.")
}

// fixedWithOrdering fixes the deadlock by acquiring locks in consistent order.
// Both goroutines lock A first, then B. No circular wait is possible because
// if G1 holds A and wants B, G2 cannot hold B while wanting A -- G2 is either
// waiting for A (which G1 holds, so G2 blocks at A, not at B) or has finished.
func fixedWithOrdering() {
	fmt.Println("=== 2. Fixed: Consistent Lock Ordering ===")

	var muA, muB sync.Mutex
	var wg sync.WaitGroup

	// Both goroutines acquire locks in the SAME order: A then B
	wg.Add(1)
	go func() {
		defer wg.Done()
		muA.Lock() // always A first
		fmt.Println("G1: locked A")
		time.Sleep(50 * time.Millisecond)
		muB.Lock() // then B
		fmt.Println("G1: locked B")
		muB.Unlock()
		muA.Unlock()
		fmt.Println("G1: released both locks")
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		muA.Lock() // always A first (same order as G1!)
		fmt.Println("G2: locked A")
		time.Sleep(50 * time.Millisecond)
		muB.Lock() // then B
		fmt.Println("G2: locked B")
		muB.Unlock()
		muA.Unlock()
		fmt.Println("G2: released both locks")
	}()

	wg.Wait()
	fmt.Println("No deadlock! Both goroutines completed.")
	fmt.Println()
}

// transferSafe transfers money between accounts using consistent lock ordering.
// The key insight: always lock the account with the LOWER ID first, regardless
// of which is "from" and which is "to". This prevents circular wait because
// no two goroutines can hold locks in opposite order.
func transferSafe(from, to *Account, amount int) bool {
	// Determine lock order based on account ID -- a stable, global ordering
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

// transferDemo runs concurrent transfers between accounts and verifies
// that total money in the system is conserved (no money created or destroyed).
func transferDemo() {
	fmt.Println("=== 3. Transfer Demo (safe ordering) ===")

	accounts := []*Account{
		{id: 1, balance: 1000},
		{id: 2, balance: 1000},
		{id: 3, balance: 1000},
	}

	totalBefore := 0
	for _, a := range accounts {
		totalBefore += a.balance
	}

	var wg sync.WaitGroup
	// Run 200 concurrent transfers between account pairs
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

	totalAfter := 0
	for _, a := range accounts {
		fmt.Printf("Account %d: balance=%d\n", a.id, a.balance)
		totalAfter += a.balance
	}
	fmt.Printf("Total money: before=%d, after=%d (should be equal)\n", totalBefore, totalAfter)

	if totalBefore == totalAfter {
		fmt.Println("Conservation of money verified!")
	} else {
		fmt.Println("BUG: money was created or destroyed!")
	}
	fmt.Println()
}

// deadlockAnalysis prints the analysis of why deadlocks occur and how
// consistent ordering prevents them.
func deadlockAnalysis() {
	fmt.Println("=== 4. Deadlock Analysis ===")
	fmt.Println("Timeline of a deadlock:")
	fmt.Println("  T0: G1 locks A, G2 locks B")
	fmt.Println("  T1: G1 wants B (held by G2) -- BLOCKED")
	fmt.Println("      G2 wants A (held by G1) -- BLOCKED")
	fmt.Println("  Result: circular wait, program hangs.")
	fmt.Println()
	fmt.Println("Coffman conditions (all four must hold for deadlock):")
	fmt.Println("  1. Mutual exclusion: mutex provides exclusive access")
	fmt.Println("  2. Hold and wait: goroutine holds one lock, waits for another")
	fmt.Println("  3. No preemption: locks released only voluntarily")
	fmt.Println("  4. Circular wait: G1 -> G2 -> G1")
	fmt.Println()
	fmt.Println("Fix: break circular wait with consistent lock ordering.")
	fmt.Println("  Rule: always acquire the lower-ID lock first.")
	fmt.Println("  transfer(A->B): lock A, lock B (A.id < B.id)")
	fmt.Println("  transfer(B->A): lock A, lock B (A.id < B.id)")
	fmt.Println("  Both goroutines acquire in the same order -- no cycle possible.")
	fmt.Println()
	fmt.Println("Go runtime detects deadlocks only when ALL goroutines are blocked.")
	fmt.Println("In a real server with a listener goroutine, partial deadlocks go undetected.")
}
