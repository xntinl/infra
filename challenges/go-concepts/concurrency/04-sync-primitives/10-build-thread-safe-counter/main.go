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
// TODO: Protect all operations with sync.Mutex
// ---------------------------------------------------------------------------

type MutexCounter struct {
	mu    sync.Mutex
	value int64
}

func (c *MutexCounter) Increment() {
	// TODO: Lock, increment, Unlock
	c.value++
}

func (c *MutexCounter) Decrement() {
	// TODO: Lock, decrement, Unlock
	c.value--
}

func (c *MutexCounter) Value() int64 {
	// TODO: Lock, read, Unlock
	return c.value
}

// ---------------------------------------------------------------------------
// Implementation 2: RWMutex Counter
// TODO: Use RLock for Value(), Lock for Increment/Decrement
// ---------------------------------------------------------------------------

type RWMutexCounter struct {
	mu    sync.RWMutex
	value int64
}

func (c *RWMutexCounter) Increment() {
	// TODO: Lock (exclusive), increment, Unlock
	c.value++
}

func (c *RWMutexCounter) Decrement() {
	// TODO: Lock (exclusive), decrement, Unlock
	c.value--
}

func (c *RWMutexCounter) Value() int64 {
	// TODO: RLock (shared), read, RUnlock
	return c.value
}

// ---------------------------------------------------------------------------
// Implementation 3: Atomic Counter
// TODO: Use atomic.Int64 for lock-free operations
// ---------------------------------------------------------------------------

type AtomicCounter struct {
	value atomic.Int64
}

func (c *AtomicCounter) Increment() {
	// TODO: c.value.Add(1)
	c.value.Add(1)
}

func (c *AtomicCounter) Decrement() {
	// TODO: c.value.Add(-1)
	c.value.Add(-1)
}

func (c *AtomicCounter) Value() int64 {
	// TODO: c.value.Load()
	return c.value.Load()
}

// ---------------------------------------------------------------------------
// Implementation 4: Channel Counter
// TODO: A single goroutine owns the value. All operations are messages.
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
// TODO: Range over c.ops and handle inc/dec/val operations.
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

func (c *ChannelCounter) Close() {
	close(c.ops)
	<-c.done
}

// ---------------------------------------------------------------------------
// Testing and Benchmarking
// ---------------------------------------------------------------------------

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
	fmt.Printf("[%s] %-10s: expected=%d, got=%d\n", status, name, expected, actual)
}

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
					c.Value()
				}
			}
		}()
	}

	wg.Wait()
	return time.Since(start)
}

func main() {
	fmt.Println("=== Correctness Tests ===")

	// Test all four implementations
	testCounter("Mutex", &MutexCounter{})
	testCounter("RWMutex", &RWMutexCounter{})
	testCounter("Atomic", &AtomicCounter{})

	cc := NewChannelCounter()
	testCounter("Channel", cc)
	cc.Close()

	// Benchmarks
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
		fmt.Printf("%-10s: %v (final value: %d)\n", tc.name, duration.Round(time.Microsecond), finalValue)
		if tc.cleanup != nil {
			tc.cleanup()
		}
	}

	// Tradeoff summary
	fmt.Println("\n=== Tradeoff Summary ===")
	fmt.Println("Atomic:   Best throughput, simplest, but limited to simple operations")
	fmt.Println("Mutex:    Good default, flexible, works for complex critical sections")
	fmt.Println("RWMutex:  Helps for read-heavy workloads, overhead for write-heavy")
	fmt.Println("Channel:  Clearest ownership, highest overhead, best for state machines")
}
