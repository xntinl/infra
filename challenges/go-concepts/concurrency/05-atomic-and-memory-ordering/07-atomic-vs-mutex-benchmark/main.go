package main

// Exercise: Atomic vs Mutex Benchmark
// Instructions: see 07-atomic-vs-mutex-benchmark.md
//
// This exercise uses Go benchmarks (main_test.go) as the primary code.
// This main.go provides a quick demo that all three counters work correctly.

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// AtomicCounter uses sync/atomic for lock-free increments.
type AtomicCounter struct {
	val atomic.Int64
}

func (c *AtomicCounter) Inc() { c.val.Add(1) }
func (c *AtomicCounter) Get() int64 { return c.val.Load() }

// MutexCounter uses sync.Mutex to protect the counter.
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

// ChannelCounter uses a buffered channel as a semaphore.
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
	<-c.ch
	c.val++
	c.ch <- struct{}{}
}

func (c *ChannelCounter) Get() int64 {
	<-c.ch
	v := c.val
	c.ch <- struct{}{}
	return v
}

// testCounter runs n goroutines, each incrementing the counter iterations times.
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
	fmt.Println("Exercise: Atomic vs Mutex Benchmark")
	fmt.Println("Run benchmarks with: go test -bench=. -benchmem")
	fmt.Println()

	fmt.Println("=== Correctness Check (100 goroutines x 1000 iterations) ===")

	ac := &AtomicCounter{}
	testCounter("AtomicCounter", ac.Inc, ac.Get, 100, 1000)

	mc := &MutexCounter{}
	testCounter("MutexCounter", mc.Inc, mc.Get, 100, 1000)

	cc := NewChannelCounter()
	testCounter("ChannelCounter", cc.Inc, cc.Get, 100, 1000)

	fmt.Println("\nAll counters verified. Now run the benchmarks:")
	fmt.Println("  go test -bench=. -benchmem")
	fmt.Println("  go test -bench=. -benchmem -count=3")
	fmt.Println("  go test -bench=Parallel -benchmem -cpu=1,2,4,8")
}
