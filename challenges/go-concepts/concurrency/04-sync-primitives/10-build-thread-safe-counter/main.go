// Exercise 10: Build a Thread-Safe Counter
//
// Integration exercise: the same counter implemented 4 ways.
// Covers: Mutex, RWMutex, atomic, channels -- benchmarks and tradeoffs.
//
// Expected output:
//
//   === Correctness Tests ===
//   [PASS] Mutex    : expected=100000, got=100000
//   [PASS] RWMutex  : expected=100000, got=100000
//   [PASS] Atomic   : expected=100000, got=100000
//   [PASS] Channel  : expected=100000, got=100000
//
//   === Benchmarks (100 goroutines x 10,000 ops) ===
//   Mutex    : Xus  (final value: 1000000+)
//   RWMutex  : Xus  (final value: 1000000+)
//   Atomic   : Xus  (final value: 1000000+)
//   Channel  : Xus  (final value: 1000000+)
//
//   === Tradeoff Summary ===
//   Atomic:   Best throughput, simplest, limited to simple operations
//   Mutex:    Good default, flexible, works for complex critical sections
//   RWMutex:  Helps for read-heavy workloads, overhead for write-heavy
//   Channel:  Clearest ownership, highest overhead, best for state machines
//
// Run: go run main.go

package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Counter defines the interface all implementations must satisfy.
type Counter interface {
	Increment()
	Decrement()
	Value() int64
}

// ---------------------------------------------------------------------------
// Implementation 1: Mutex Counter
// The simplest and most flexible approach. All operations serialized.
// ---------------------------------------------------------------------------

type MutexCounter struct {
	mu    sync.Mutex
	value int64
}

func (c *MutexCounter) Increment() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value++
}

func (c *MutexCounter) Decrement() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value--
}

func (c *MutexCounter) Value() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.value
}

// ---------------------------------------------------------------------------
// Implementation 2: RWMutex Counter
// Read lock for Value() allows concurrent reads. Write lock for mutations.
// For a counter (write-heavy), this adds overhead with no real benefit.
// ---------------------------------------------------------------------------

type RWMutexCounter struct {
	mu    sync.RWMutex
	value int64
}

func (c *RWMutexCounter) Increment() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value++
}

func (c *RWMutexCounter) Decrement() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value--
}

func (c *RWMutexCounter) Value() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.value
}

// ---------------------------------------------------------------------------
// Implementation 3: Atomic Counter
// Lock-free, highest throughput for simple operations.
// No deadlock possible. Limited to CPU-supported atomic operations.
// ---------------------------------------------------------------------------

type AtomicCounter struct {
	value atomic.Int64
}

func (c *AtomicCounter) Increment() {
	c.value.Add(1)
}

func (c *AtomicCounter) Decrement() {
	c.value.Add(-1)
}

func (c *AtomicCounter) Value() int64 {
	return c.value.Load()
}

// ---------------------------------------------------------------------------
// Implementation 4: Channel Counter
// A single goroutine owns the value. All operations are messages.
// No shared state, no locks. Clearest ownership model but highest overhead.
// ---------------------------------------------------------------------------

type counterOp struct {
	kind     string // "inc", "dec", "val"
	response chan int64
}

type ChannelCounter struct {
	ops  chan counterOp
	done chan struct{}
}

func NewChannelCounter() *ChannelCounter {
	c := &ChannelCounter{
		ops:  make(chan counterOp),
		done: make(chan struct{}),
	}
	go c.run()
	return c
}

// run is the owner goroutine that processes all operations sequentially.
// Since only this goroutine reads/writes the value, no synchronization is needed.
func (c *ChannelCounter) run() {
	var value int64
	for op := range c.ops {
		switch op.kind {
		case "inc":
			value++
			if op.response != nil {
				op.response <- value
			}
		case "dec":
			value--
			if op.response != nil {
				op.response <- value
			}
		case "val":
			op.response <- value
		}
	}
	close(c.done)
}

func (c *ChannelCounter) Increment() {
	c.ops <- counterOp{kind: "inc"}
}

func (c *ChannelCounter) Decrement() {
	c.ops <- counterOp{kind: "dec"}
}

func (c *ChannelCounter) Value() int64 {
	resp := make(chan int64)
	c.ops <- counterOp{kind: "val", response: resp}
	return <-resp
}

// Close shuts down the owner goroutine. Must be called to avoid goroutine leaks.
func (c *ChannelCounter) Close() {
	close(c.ops)
	<-c.done
}

// ---------------------------------------------------------------------------
// Testing and Benchmarking
// ---------------------------------------------------------------------------

// testCounter verifies correctness: 100 goroutines x 1000 increments = 100000.
func testCounter(name string, c Counter) {
	var wg sync.WaitGroup
	const goroutines = 100
	const increments = 1000

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < increments; j++ {
				c.Increment()
			}
		}()
	}

	wg.Wait()
	expected := int64(goroutines * increments)
	actual := c.Value()
	status := "PASS"
	if actual != expected {
		status = "FAIL"
	}
	fmt.Printf("[%s] %-8s: expected=%d, got=%d\n", status, name, expected, actual)
}

// benchmarkCounter measures throughput under concurrent load.
// Mix: mostly increments with occasional reads (every 10th op).
func benchmarkCounter(name string, c Counter, goroutines, opsPerGoroutine int) time.Duration {
	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				c.Increment()
				if j%10 == 0 {
					c.Value() // occasional read
				}
			}
		}()
	}

	wg.Wait()
	return time.Since(start)
}

func main() {
	fmt.Println("=== Correctness Tests ===")

	// Test all four implementations for correctness
	testCounter("Mutex", &MutexCounter{})
	testCounter("RWMutex", &RWMutexCounter{})
	testCounter("Atomic", &AtomicCounter{})

	cc := NewChannelCounter()
	testCounter("Channel", cc)
	cc.Close()

	// Benchmark all four implementations
	fmt.Println("\n=== Benchmarks (100 goroutines x 10,000 ops) ===")
	const goroutines = 100
	const ops = 10000

	type benchCase struct {
		name    string
		counter Counter
		cleanup func()
	}

	cases := []benchCase{
		{"Mutex", &MutexCounter{}, nil},
		{"RWMutex", &RWMutexCounter{}, nil},
		{"Atomic", &AtomicCounter{}, nil},
	}

	chCounter := NewChannelCounter()
	cases = append(cases, benchCase{"Channel", chCounter, func() { chCounter.Close() }})

	for _, tc := range cases {
		duration := benchmarkCounter(tc.name, tc.counter, goroutines, ops)
		finalValue := tc.counter.Value()
		fmt.Printf("%-8s: %v (final value: %d)\n", tc.name, duration.Round(time.Microsecond), finalValue)
		if tc.cleanup != nil {
			tc.cleanup()
		}
	}

	// Tradeoff summary
	fmt.Println("\n=== Tradeoff Summary ===")
	fmt.Println("Atomic:   Best throughput, simplest, limited to simple operations")
	fmt.Println("          (add, load, store, compare-and-swap). No deadlock risk.")
	fmt.Println("Mutex:    Good default for most use cases. Flexible -- works for")
	fmt.Println("          complex critical sections (read-modify-write on structs).")
	fmt.Println("RWMutex:  Benefits read-heavy workloads. For a counter (write-heavy),")
	fmt.Println("          it adds overhead tracking reader counts with no benefit.")
	fmt.Println("Channel:  Clearest ownership model, no shared state. Highest per-op")
	fmt.Println("          overhead. Best for complex state machines, not simple counters.")
	fmt.Println()
	fmt.Println("Decision guide:")
	fmt.Println("  - Simple counter/gauge?       Use atomic")
	fmt.Println("  - Complex struct protection?   Use mutex")
	fmt.Println("  - Read-heavy cache/config?     Use RWMutex")
	fmt.Println("  - State machine / actor model? Use channels")
}
