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

const (
	initialTickets = 100
	buyerCount     = 500
)

type UnsafeTicketBooth struct {
	tickets int64
	sold    int64
}

func NewUnsafeTicketBooth(stock int64) *UnsafeTicketBooth {
	return &UnsafeTicketBooth{tickets: stock}
}

func (tb *UnsafeTicketBooth) TryBuy() {
	// BUG: check and decrement are separate operations
	if tb.tickets > 0 {
		tb.tickets--
		atomic.AddInt64(&tb.sold, 1)
	}
}

func (tb *UnsafeTicketBooth) Report() {
	var oversold int64
	if tb.tickets < 0 {
		oversold = -tb.tickets
	}
	fmt.Println("=== Ticket Sales (BROKEN - no CAS) ===")
	fmt.Printf("Starting tickets: %d\n", initialTickets)
	fmt.Printf("Sold:             %d\n", tb.sold)
	fmt.Printf("Remaining:        %d\n", tb.tickets)
	fmt.Printf("Oversold:         %d\n", oversold)
}

func simulateBuyers(booth *UnsafeTicketBooth) {
	var wg sync.WaitGroup
	for i := 0; i < buyerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			booth.TryBuy()
		}()
	}
	wg.Wait()
}

func main() {
	booth := NewUnsafeTicketBooth(initialTickets)
	simulateBuyers(booth)
	booth.Report()
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

const (
	initialTickets = 100
	buyerCount     = 500
)

type TicketBooth struct {
	stock        int64
	successCount atomic.Int64
	failCount    atomic.Int64
}

func NewTicketBooth(stock int64) *TicketBooth {
	return &TicketBooth{stock: stock}
}

func (tb *TicketBooth) TryReserve(quantity int64) bool {
	for {
		current := atomic.LoadInt64(&tb.stock)
		if current < quantity {
			return false
		}
		if atomic.CompareAndSwapInt64(&tb.stock, current, current-quantity) {
			return true
		}
		// CAS failed: another goroutine modified stock. Retry.
	}
}

func (tb *TicketBooth) ProcessBuyer() {
	if tb.TryReserve(1) {
		tb.successCount.Add(1)
	} else {
		tb.failCount.Add(1)
	}
}

func (tb *TicketBooth) RemainingStock() int64 {
	return atomic.LoadInt64(&tb.stock)
}

func (tb *TicketBooth) Report() {
	fmt.Println("=== Ticket Sales (FIXED - CAS) ===")
	fmt.Printf("Starting tickets:     %d\n", initialTickets)
	fmt.Printf("Successful purchases: %d\n", tb.successCount.Load())
	fmt.Printf("Rejected (sold out):  %d\n", tb.failCount.Load())
	fmt.Printf("Remaining stock:      %d\n", tb.RemainingStock())
}

func simulateBuyers(booth *TicketBooth) {
	var wg sync.WaitGroup
	for i := 0; i < buyerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			booth.ProcessBuyer()
		}()
	}
	wg.Wait()
}

func main() {
	booth := NewTicketBooth(initialTickets)
	simulateBuyers(booth)
	booth.Report()
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

const (
	buyerCount     = 2000
	maxSeatsPerBuy = 4
)

type EventSection struct {
	Name     string
	stock    int64
	reserved atomic.Int64
	rejected atomic.Int64
}

func NewEventSection(name string, capacity int64) *EventSection {
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

type ConcertVenue struct {
	sections      []*EventSection
	totalReserved atomic.Int64
	totalRejected atomic.Int64
}

func NewConcertVenue() *ConcertVenue {
	return &ConcertVenue{
		sections: []*EventSection{
			NewEventSection("VIP Front Row", 20),
			NewEventSection("Orchestra", 200),
			NewEventSection("Balcony", 500),
		},
	}
}

func (v *ConcertVenue) ProcessBuyer() {
	section := v.sections[rand.Intn(len(v.sections))]
	quantity := int64(1 + rand.Intn(maxSeatsPerBuy))

	if section.Reserve(quantity) {
		v.totalReserved.Add(1)
	} else {
		v.totalRejected.Add(1)
	}
}

func (v *ConcertVenue) Report() {
	fmt.Println("=== Concert Seat Reservation Results ===")
	fmt.Println()
	for _, s := range v.sections {
		fmt.Printf("  %-15s  remaining: %3d  reservations: %3d  rejected: %3d\n",
			s.Name, s.Available(), s.reserved.Load(), s.rejected.Load())
	}
	fmt.Println()
	fmt.Printf("Total reservations: %d\n", v.totalReserved.Load())
	fmt.Printf("Total rejections:   %d\n", v.totalRejected.Load())
}

func (v *ConcertVenue) VerifyIntegrity() {
	for _, s := range v.sections {
		if s.Available() < 0 {
			fmt.Printf("BUG: %s has negative stock: %d\n", s.Name, s.Available())
		}
	}
}

func simulateSale(venue *ConcertVenue) {
	var wg sync.WaitGroup
	for i := 0; i < buyerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			venue.ProcessBuyer()
		}()
	}
	wg.Wait()
}

func main() {
	venue := NewConcertVenue()
	simulateSale(venue)
	venue.Report()
	venue.VerifyIntegrity()
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

const (
	totalBuyers  = 5000
	totalTickets = 500
)

type CASReservationSystem struct {
	stock   int64
	success atomic.Int64
}

func NewCASReservationSystem(stock int64) *CASReservationSystem {
	return &CASReservationSystem{stock: stock}
}

func (s *CASReservationSystem) TryReserve(quantity int64) bool {
	for {
		current := atomic.LoadInt64(&s.stock)
		if current < quantity {
			return false
		}
		if atomic.CompareAndSwapInt64(&s.stock, current, current-quantity) {
			s.success.Add(1)
			return true
		}
	}
}

func (s *CASReservationSystem) Remaining() int64 { return atomic.LoadInt64(&s.stock) }
func (s *CASReservationSystem) Sold() int64      { return s.success.Load() }

type MutexReservationSystem struct {
	mu      sync.Mutex
	stock   int64
	success atomic.Int64
}

func NewMutexReservationSystem(stock int64) *MutexReservationSystem {
	return &MutexReservationSystem{stock: stock}
}

func (s *MutexReservationSystem) TryReserve(quantity int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stock < quantity {
		return false
	}
	s.stock -= quantity
	s.success.Add(1)
	return true
}

func (s *MutexReservationSystem) Remaining() int64 { return s.stock }
func (s *MutexReservationSystem) Sold() int64      { return s.success.Load() }

type ReservationBenchmark struct {
	Duration time.Duration
	Sold     int64
	Remaining int64
}

func benchmarkCAS(buyers int, tickets int64) ReservationBenchmark {
	system := NewCASReservationSystem(tickets)
	var wg sync.WaitGroup

	start := time.Now()
	for i := 0; i < buyers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			system.TryReserve(1)
		}()
	}
	wg.Wait()

	return ReservationBenchmark{
		Duration:  time.Since(start),
		Sold:      system.Sold(),
		Remaining: system.Remaining(),
	}
}

func benchmarkMutex(buyers int, tickets int64) ReservationBenchmark {
	system := NewMutexReservationSystem(tickets)
	var wg sync.WaitGroup

	start := time.Now()
	for i := 0; i < buyers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			system.TryReserve(1)
		}()
	}
	wg.Wait()

	return ReservationBenchmark{
		Duration:  time.Since(start),
		Sold:      system.Sold(),
		Remaining: system.Remaining(),
	}
}

func main() {
	casResult := benchmarkCAS(totalBuyers, totalTickets)
	mutexResult := benchmarkMutex(totalBuyers, totalTickets)

	fmt.Println("=== CAS vs Mutex Reservation ===")
	fmt.Printf("CAS:   sold=%d remaining=%d time=%v\n",
		casResult.Sold, casResult.Remaining, casResult.Duration)
	fmt.Printf("Mutex: sold=%d remaining=%d time=%v\n",
		mutexResult.Sold, mutexResult.Remaining, mutexResult.Duration)
	fmt.Printf("Ratio (Mutex/CAS): %.2fx\n",
		float64(mutexResult.Duration)/float64(casResult.Duration))
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

// badReserve loads the stock value only once outside the loop.
// Under contention, every CAS after the first uses a stale value
// and fails indefinitely.
func badReserve(stock *int64) bool {
	current := atomic.LoadInt64(stock) // loaded once -- never refreshed
	for {
		if current <= 0 {
			return false
		}
		if atomic.CompareAndSwapInt64(stock, current, current-1) {
			return true
		}
		// BUG: current is stale. Every subsequent CAS uses the wrong old value.
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
