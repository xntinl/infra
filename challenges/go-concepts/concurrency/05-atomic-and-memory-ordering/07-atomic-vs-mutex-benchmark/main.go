package main

// Atomic vs Mutex Benchmark — Production-quality educational code
//
// Defines three counter implementations (atomic, mutex, channel) and
// verifies their correctness. The actual benchmarks are in main_test.go.
//
// Expected output:
//   === Correctness Check (100 goroutines x 1000 iterations) ===
//     AtomicCounter        expected=100000 got=100000 [PASS]
//     MutexCounter         expected=100000 got=100000 [PASS]
//     RWMutexCounter       expected=100000 got=100000 [PASS]
//     ChannelCounter       expected=100000 got=100000 [PASS]
//
//   All counters verified. Run benchmarks with:
//     go test -bench=. -benchmem
//     go test -bench=. -benchmem -count=3
//     go test -bench=Parallel -benchmem -cpu=1,2,4,8

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// AtomicCounter uses sync/atomic for lock-free increments.
// Best for simple counters with high read/write throughput.
// No allocation, no locking overhead, minimal cache-line contention.
type AtomicCounter struct {
	val atomic.Int64
}

func (c *AtomicCounter) Inc()       { c.val.Add(1) }
func (c *AtomicCounter) Get() int64 { return c.val.Load() }

// MutexCounter uses sync.Mutex to protect the counter.
// The mutex serializes all access — both reads and writes contend.
// Simple and correct, but readers block each other unnecessarily.
type MutexCounter struct {
	mu  sync.Mutex
	val int64
}

func (c *MutexCounter) Inc() {
	c.mu.Lock()
	c.val++
	c.mu.Unlock()
}

func (c *MutexCounter) Get() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.val
}

// RWMutexCounter uses sync.RWMutex: multiple concurrent readers,
// exclusive writers. Better than Mutex for read-heavy workloads because
// readers don't block each other. Writers still block everything.
type RWMutexCounter struct {
	mu  sync.RWMutex
	val int64
}

func (c *RWMutexCounter) Inc() {
	c.mu.Lock()
	c.val++
	c.mu.Unlock()
}

func (c *RWMutexCounter) Get() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.val
}

// ChannelCounter uses a buffered channel with capacity 1 as a semaphore.
// Only the goroutine holding the token can access val. This is idiomatic
// Go ("share memory by communicating") but has the highest per-operation
// cost due to channel send/receive overhead.
type ChannelCounter struct {
	ch  chan struct{}
	val int64
}

func NewChannelCounter() *ChannelCounter {
	c := &ChannelCounter{ch: make(chan struct{}, 1)}
	c.ch <- struct{}{} // initial token
	return c
}

func (c *ChannelCounter) Inc() {
	<-c.ch     // acquire token
	c.val++
	c.ch <- struct{}{} // release token
}

func (c *ChannelCounter) Get() int64 {
	<-c.ch
	v := c.val
	c.ch <- struct{}{}
	return v
}

// testCounter runs n goroutines, each incrementing the counter `iterations` times.
// Verifies the final count matches the expected value.
func testCounter(name string, inc func(), get func() int64, goroutines, iterations int) {
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				inc()
			}
		}()
	}
	wg.Wait()

	expected := int64(goroutines * iterations)
	actual := get()
	status := "PASS"
	if actual != expected {
		status = "FAIL"
	}
	fmt.Printf("  %-20s expected=%d got=%d [%s]\n", name, expected, actual, status)
}

func main() {
	fmt.Println("Atomic vs Mutex Benchmark")
	fmt.Println("Run benchmarks with: go test -bench=. -benchmem")
	fmt.Println()

	fmt.Println("=== Correctness Check (100 goroutines x 1000 iterations) ===")

	ac := &AtomicCounter{}
	testCounter("AtomicCounter", ac.Inc, ac.Get, 100, 1000)

	mc := &MutexCounter{}
	testCounter("MutexCounter", mc.Inc, mc.Get, 100, 1000)

	rwc := &RWMutexCounter{}
	testCounter("RWMutexCounter", rwc.Inc, rwc.Get, 100, 1000)

	cc := NewChannelCounter()
	testCounter("ChannelCounter", cc.Inc, cc.Get, 100, 1000)

	fmt.Println()
	fmt.Println("All counters verified. Run benchmarks with:")
	fmt.Println("  go test -bench=. -benchmem")
	fmt.Println("  go test -bench=. -benchmem -count=3")
	fmt.Println("  go test -bench=Parallel -benchmem -cpu=1,2,4,8")
}
