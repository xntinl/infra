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
	// Uncomment createDeadlock() to see a real deadlock.
	// WARNING: This will freeze the program! Press Ctrl+C to kill.
	// createDeadlock()

	fixedWithOrdering()
	transferDemo()
}

// createDeadlock demonstrates a classic two-mutex deadlock.
// G1 locks A then B. G2 locks B then A. Circular wait ensues.
//
// UNCOMMENT TO RUN (will freeze!):
func createDeadlock() {
	fmt.Println("=== Deadlock Demonstration ===")
	fmt.Println("(This will freeze! Press Ctrl+C to kill.)")

	var muA, muB sync.Mutex
	var wg sync.WaitGroup

	// Goroutine 1: locks A then B
	wg.Add(1)
	go func() {
		defer wg.Done()
		muA.Lock()
		fmt.Println("G1: locked A")
		time.Sleep(50 * time.Millisecond)
		fmt.Println("G1: waiting for B...")
		muB.Lock()
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
		time.Sleep(50 * time.Millisecond)
		fmt.Println("G2: waiting for A...")
		muA.Lock()
		fmt.Println("G2: locked A")
		muA.Unlock()
		muB.Unlock()
	}()

	wg.Wait()
	fmt.Println("This line is never reached.")
}

// fixedWithOrdering fixes the deadlock by acquiring locks in consistent order.
// TODO: Both goroutines should lock A first, then B (same order).
func fixedWithOrdering() {
	fmt.Println("=== Fixed: Consistent Lock Ordering ===")

	var muA, muB sync.Mutex
	var wg sync.WaitGroup

	// Goroutine 1: locks A then B
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
		fmt.Println("G1: released both locks")
	}()

	// Goroutine 2: TODO -- lock in the SAME order as G1 (A then B)
	// Currently locks B then A (deadlock-prone). Fix the ordering.
	wg.Add(1)
	go func() {
		defer wg.Done()
		// TODO: Change this to lock A first, then B
		muB.Lock()
		fmt.Println("G2: locked B")
		time.Sleep(50 * time.Millisecond)
		muA.Lock()
		fmt.Println("G2: locked A")
		muA.Unlock()
		muB.Unlock()
		fmt.Println("G2: released both locks")
	}()

	wg.Wait()
	fmt.Println("No deadlock! Both goroutines completed.")
	fmt.Println()
}

// transferUnsafe transfers money between accounts but may deadlock.
// It locks "from" first, then "to" -- order depends on the caller.
func transferUnsafe(from, to *Account, amount int) bool {
	from.mu.Lock()
	defer from.mu.Unlock()
	to.mu.Lock()
	defer to.mu.Unlock()

	if from.balance < amount {
		return false
	}
	from.balance -= amount
	to.balance += amount
	return true
}

// transferSafe transfers money using consistent lock ordering.
// TODO: Always lock the account with the LOWER id first,
// regardless of which is "from" and which is "to".
func transferSafe(from, to *Account, amount int) bool {
	// TODO: Determine lock order based on account ID
	// first, second := from, to
	// if from.id > to.id {
	//     first, second = to, from
	// }
	// first.mu.Lock()
	// defer first.mu.Unlock()
	// second.mu.Lock()
	// defer second.mu.Unlock()

	// Current (unsafe) implementation -- fix this:
	from.mu.Lock()
	defer from.mu.Unlock()
	to.mu.Lock()
	defer to.mu.Unlock()

	if from.balance < amount {
		return false
	}
	from.balance -= amount
	to.balance += amount
	return true
}

// transferDemo runs concurrent transfers between accounts.
func transferDemo() {
	fmt.Println("=== Transfer Demo ===")

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
	// Run 100 concurrent transfers between random pairs
	for i := 0; i < 100; i++ {
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
}
