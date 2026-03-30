---
difficulty: intermediate
concepts: [CompareAndSwapInt64, CAS loop, optimistic concurrency, lock-free, inventory reservation]
tools: [go]
estimated_time: 30m
bloom_level: apply
---

# 3. Atomic Compare-And-Swap

## Learning Objectives
After completing this exercise, you will be able to:
- **Explain** compare-and-swap (CAS) and why it is the foundation of lock-free algorithms
- **Implement** a CAS retry loop for lock-free inventory reservation
- **Build** a ticket/seat reservation system where multiple goroutines compete for limited stock
- **Compare** CAS (optimistic concurrency) with mutex (pessimistic locking) and explain the trade-offs

## Why Compare-And-Swap for Inventory Reservation

Imagine a flash sale: 500 concert tickets go live, and 10,000 users hit the "Buy" button simultaneously. Each purchase must atomically check if tickets remain and decrement the count. If two requests both see "1 ticket left," both decrement, and the count goes to -1 -- you just oversold.

`atomic.AddInt64` cannot help because it does not check a condition before modifying. You need check-then-act as a single indivisible operation. That is exactly what Compare-And-Swap (CAS) provides.

`CompareAndSwapInt64(&addr, old, new)` atomically checks if `*addr == old`, and if so, sets `*addr = new` and returns `true`. If `*addr != old`, it does nothing and returns `false`. The entire operation is one indivisible CPU instruction.

The CAS retry loop is the core pattern of optimistic concurrency: load the current value, compute the desired new value, attempt the swap. If another goroutine changed the value between your load and your swap, CAS fails and you retry. No locks, no blocking, no goroutine parking. Under low contention, most CAS attempts succeed on the first try. Under high contention, retries increase but the system never deadlocks.

## Step 1 -- The Overselling Bug Without CAS

Multiple goroutines try to buy tickets using a naive check-then-decrement. The check and the decrement are separate operations, creating a race window:

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

func main() {
	var tickets int64 = 100
	var sold int64
	var oversold int64
	var wg sync.WaitGroup

	for i := 0; i < 500; i++ {
		wg.Add(1)
		go func(buyerID int) {
			defer wg.Done()
			// BUG: check and decrement are separate operations
			if tickets > 0 {
				tickets--
				atomic.AddInt64(&sold, 1)
			}
		}(i)
	}

	wg.Wait()
	if tickets < 0 {
		oversold = -tickets
	}
	fmt.Println("=== Ticket Sales (BROKEN - no CAS) ===")
	fmt.Printf("Starting tickets: 100\n")
	fmt.Printf("Sold:             %d\n", sold)
	fmt.Printf("Remaining:        %d\n", tickets)
	fmt.Printf("Oversold:         %d\n", oversold)
}
```

### Verification
```bash
go run -race main.go
```
The race detector reports `DATA RACE`. Run without `-race` multiple times: the ticket count may go negative, meaning you sold tickets that do not exist.

## Step 2 -- Fix with CAS: Lock-Free Reservation

Use `CompareAndSwapInt64` to atomically check-and-decrement. Each buyer loads the current stock, checks if positive, and attempts a CAS to decrement. If the CAS fails (another buyer got there first), retry:

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

func tryReserve(stock *int64, quantity int64) bool {
	for {
		current := atomic.LoadInt64(stock)
		if current < quantity {
			return false // not enough stock
		}
		newStock := current - quantity
		if atomic.CompareAndSwapInt64(stock, current, newStock) {
			return true // reserved successfully
		}
		// CAS failed: another goroutine modified stock. Retry.
	}
}

func main() {
	var tickets int64 = 100
	var successCount atomic.Int64
	var failCount atomic.Int64
	var wg sync.WaitGroup

	for i := 0; i < 500; i++ {
		wg.Add(1)
		go func(buyerID int) {
			defer wg.Done()
			if tryReserve(&tickets, 1) {
				successCount.Add(1)
			} else {
				failCount.Add(1)
			}
		}(i)
	}

	wg.Wait()
	fmt.Println("=== Ticket Sales (FIXED - CAS) ===")
	fmt.Printf("Starting tickets:     100\n")
	fmt.Printf("Successful purchases: %d\n", successCount.Load())
	fmt.Printf("Rejected (sold out):  %d\n", failCount.Load())
	fmt.Printf("Remaining stock:      %d\n", atomic.LoadInt64(&tickets))
}
```

### Verification
```bash
go run -race main.go
```
Exactly 100 purchases succeed, 400 are rejected, remaining stock is 0. No overselling. No race warnings.

## Step 3 -- Full Seat Reservation System with Multiple Event Sections

Build a realistic event reservation system with multiple sections, each managed by a CAS-based counter. Buyers can request specific sections and quantities:

```go
package main

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
)

type EventSection struct {
	Name      string
	stock     int64
	reserved  atomic.Int64
	rejected  atomic.Int64
}

func NewSection(name string, capacity int64) *EventSection {
	return &EventSection{Name: name, stock: capacity}
}

func (s *EventSection) Reserve(quantity int64) bool {
	for {
		current := atomic.LoadInt64(&s.stock)
		if current < quantity {
			s.rejected.Add(1)
			return false
		}
		if atomic.CompareAndSwapInt64(&s.stock, current, current-quantity) {
			s.reserved.Add(1)
			return true
		}
	}
}

func (s *EventSection) Available() int64 {
	return atomic.LoadInt64(&s.stock)
}

func main() {
	sections := []*EventSection{
		NewSection("VIP Front Row", 20),
		NewSection("Orchestra", 200),
		NewSection("Balcony", 500),
	}

	var wg sync.WaitGroup
	var totalReserved atomic.Int64
	var totalRejected atomic.Int64

	// 2000 buyers competing for seats
	for i := 0; i < 2000; i++ {
		wg.Add(1)
		go func(buyerID int) {
			defer wg.Done()

			// Each buyer picks a random section and quantity (1-4 seats)
			section := sections[rand.Intn(len(sections))]
			quantity := int64(1 + rand.Intn(4))

			if section.Reserve(quantity) {
				totalReserved.Add(1)
			} else {
				totalRejected.Add(1)
			}
		}(i)
	}

	wg.Wait()

	fmt.Println("=== Concert Seat Reservation Results ===")
	fmt.Println()
	for _, s := range sections {
		fmt.Printf("  %-15s  remaining: %3d  reservations: %3d  rejected: %3d\n",
			s.Name, s.Available(), s.reserved.Load(), s.rejected.Load())
	}
	fmt.Println()
	fmt.Printf("Total reservations: %d\n", totalReserved.Load())
	fmt.Printf("Total rejections:   %d\n", totalRejected.Load())

	// Verify no section went negative
	for _, s := range sections {
		if s.Available() < 0 {
			fmt.Printf("BUG: %s has negative stock: %d\n", s.Name, s.Available())
		}
	}
}
```

### Verification
```bash
go run -race main.go
```
No section ever goes negative. Total reservations + rejections = 2000. No race warnings.

## Step 4 -- Compare CAS vs Mutex: Pessimistic Locking

The same reservation system using `sync.Mutex`. Compare the approaches structurally:

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// CAS-based (optimistic)
func reserveCAS(stock *int64, quantity int64) bool {
	for {
		current := atomic.LoadInt64(stock)
		if current < quantity {
			return false
		}
		if atomic.CompareAndSwapInt64(stock, current, current-quantity) {
			return true
		}
	}
}

// Mutex-based (pessimistic)
func reserveMutex(mu *sync.Mutex, stock *int64, quantity int64) bool {
	mu.Lock()
	defer mu.Unlock()
	if *stock < quantity {
		return false
	}
	*stock -= quantity
	return true
}

func main() {
	const buyers = 5000
	const tickets int64 = 500

	// Benchmark CAS approach
	casStock := tickets
	var casSuccess atomic.Int64
	var wg sync.WaitGroup

	start := time.Now()
	for i := 0; i < buyers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if reserveCAS(&casStock, 1) {
				casSuccess.Add(1)
			}
		}()
	}
	wg.Wait()
	casTime := time.Since(start)

	// Benchmark Mutex approach
	mutexStock := tickets
	var mu sync.Mutex
	var mutexSuccess atomic.Int64

	start = time.Now()
	for i := 0; i < buyers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if reserveMutex(&mu, &mutexStock, 1) {
				mutexSuccess.Add(1)
			}
		}()
	}
	wg.Wait()
	mutexTime := time.Since(start)

	fmt.Println("=== CAS vs Mutex Reservation ===")
	fmt.Printf("CAS:   sold=%d remaining=%d time=%v\n",
		casSuccess.Load(), atomic.LoadInt64(&casStock), casTime)
	fmt.Printf("Mutex: sold=%d remaining=%d time=%v\n",
		mutexSuccess.Load(), mutexStock, mutexTime)
	fmt.Printf("Ratio (Mutex/CAS): %.2fx\n",
		float64(mutexTime)/float64(casTime))
}
```

### Verification
```bash
go run main.go
```
Both approaches sell exactly 500 tickets with 0 remaining. CAS is typically faster for this simple check-and-decrement. Under higher contention with longer critical sections, mutex may win because it parks goroutines instead of spinning.

## Intermediate Verification

Run the race detector on Steps 2-4:
```bash
go run -race main.go
```
All should pass with zero warnings. Step 1 should show `DATA RACE`.

## Common Mistakes

### Forgetting to Reload in the CAS Loop

**Wrong:**
```go
package main

import "sync/atomic"

func badReserve(stock *int64) bool {
	current := atomic.LoadInt64(stock) // loaded once
	for {
		if current <= 0 {
			return false
		}
		if atomic.CompareAndSwapInt64(stock, current, current-1) {
			return true
		}
		// BUG: current is stale. Every subsequent CAS uses the wrong old value.
		// Under contention, this becomes an infinite loop.
	}
}

func main() {
	var s int64 = 10
	badReserve(&s) // works once, infinite-loops under contention
}
```

**Fix:** Reload `current` at the start of each loop iteration:
```go
for {
    current := atomic.LoadInt64(stock)
    if current <= 0 { return false }
    if atomic.CompareAndSwapInt64(stock, current, current-1) { return true }
}
```

### Using CAS Where AddInt64 Suffices

**Wrong (not broken, but wasteful):**
```go
func increment(addr *int64) {
    for {
        old := atomic.LoadInt64(addr)
        if atomic.CompareAndSwapInt64(addr, old, old+1) { return }
    }
}
```

This is `atomic.AddInt64` with extra steps: more code, more retries under contention, same result. Reserve CAS for operations that need a condition check before the update.

### Ignoring the ABA Problem

The ABA problem: a value changes from A to B and back to A. A CAS checking for A succeeds even though the value was modified in between. For simple counters and inventory this is harmless (stock going from 5 to 4 to 5 is fine -- the final state is valid). For pointer-based lock-free data structures, ABA can cause corruption. Go does not provide tagged pointers or double-word CAS. If you encounter ABA concerns, use a mutex.

## Verify What You Learned

1. Why can't `atomic.AddInt64` solve the inventory reservation problem?
2. What happens when a CAS fails? What should the goroutine do next?
3. Under what contention levels does CAS outperform mutex? When does mutex win?
4. In the retry loop, why is it critical to reload the current value inside the loop?

## What's Next
Continue to [04-atomic-value-dynamic-config](../04-atomic-value-dynamic-config/04-atomic-value-dynamic-config.md) to build a hot-reloadable configuration system using `atomic.Value` for swapping entire config structs atomically.

## Summary
- CAS (`CompareAndSwapInt64`) atomically checks and sets a value in one indivisible operation
- The CAS retry loop: load current value, check condition, compute new value, attempt CAS, retry on failure
- CAS is the right tool for "check-then-act" patterns like inventory reservation, rate limiting, and seat booking
- Always reload the current value inside the retry loop -- stale values cause infinite loops under contention
- CAS (optimistic) avoids lock overhead but retries under contention; mutex (pessimistic) blocks but parks goroutines efficiently
- For simple addition, use `atomic.AddInt64`; reserve CAS for conditional operations that Add cannot express
- CAS shines in low-to-medium contention; mutex wins when contention is very high or critical sections are long

## Reference
- [atomic.CompareAndSwapInt64](https://pkg.go.dev/sync/atomic#CompareAndSwapInt64)
- [Optimistic Concurrency Control (Wikipedia)](https://en.wikipedia.org/wiki/Optimistic_concurrency_control)
- [ABA Problem (Wikipedia)](https://en.wikipedia.org/wiki/ABA_problem)
