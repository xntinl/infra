package main

import (
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"time"
)

// This program compares channels vs shared memory (mutexes) for the same problems.
// Run: go run main.go
// Run with race detector: go run -race main.go (skip example 1 which intentionally races)
//
// Expected output:
//   === Example 1: Data Race (The Problem) ===
//   Counter: ~1000 (may be wrong without -race flag)
//
//   === Example 2: Mutex Solution ===
//   Counter: 1000 (always correct)
//
//   === Example 3: Channel Solution ===
//   Counter: 1000 (always correct, no shared state)
//
//   === Example 4: Cache -- Both Approaches ===
//   Mutex cache and channel cache produce same results
//
//   === Example 5: Hit Counter Benchmark ===
//   Mutex: faster for simple state guarding
//   Channel: cleaner for complex coordination

func main() {
	example1DataRace()
	example2MutexCounter()
	example3ChannelCounter()
	example4CacheComparison()
	example5HitCounterBenchmark()
}

// example1DataRace shows the problem: multiple goroutines modify shared state without
// synchronization. The result is undefined -- sometimes correct, sometimes wrong.
// Run with `go run -race main.go` to see the race detector flag it.
func example1DataRace() {
	fmt.Println("=== Example 1: Data Race (The Problem) ===")

	counter := 0
	var wg sync.WaitGroup

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// DATA RACE: counter++ is read-modify-write, not atomic.
			// Two goroutines can read the same value, both increment, both write --
			// one increment is lost.
			counter++
		}()
	}

	wg.Wait()
	fmt.Printf("Counter: %d (expected 1000, may be wrong due to race)\n\n", counter)
}

// example2MutexCounter fixes the race with sync.Mutex: lock before access, unlock after.
// Simple, low overhead, directly protects the data.
func example2MutexCounter() {
	fmt.Println("=== Example 2: Mutex Solution ===")

	counter := 0
	var mu sync.Mutex
	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mu.Lock()
			counter++
			mu.Unlock()
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)
	fmt.Printf("Counter: %d (took %v)\n\n", counter, elapsed)
}

// example3ChannelCounter fixes the race with channels: a dedicated goroutine owns
// the counter. Other goroutines send increment signals. No shared state at all.
func example3ChannelCounter() {
	fmt.Println("=== Example 3: Channel Solution ===")

	start := time.Now()

	// Increment signals and result channel.
	increments := make(chan struct{}, 100)
	result := make(chan int)

	// Counter goroutine: the ONLY goroutine that touches the counter variable.
	go func() {
		counter := 0
		for range increments {
			counter++
		}
		result <- counter
	}()

	// 1000 goroutines send increment signals. They never touch the counter directly.
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			increments <- struct{}{}
		}()
	}

	wg.Wait()
	close(increments) // signal counter goroutine that no more increments are coming
	total := <-result

	elapsed := time.Since(start)
	fmt.Printf("Counter: %d (took %v, no shared state)\n\n", total, elapsed)
}

// --- Cache: a richer problem that shows when channels vs mutexes each shine ---

// MutexCache uses RWMutex for concurrent-safe access. RLock allows multiple concurrent
// readers, which is efficient for read-heavy workloads.
type MutexCache struct {
	mu    sync.RWMutex
	items map[string]string
}

func NewMutexCache() *MutexCache {
	return &MutexCache{items: make(map[string]string)}
}

func (c *MutexCache) Set(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = value
}

func (c *MutexCache) Get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	val, ok := c.items[key]
	return val, ok
}

// ChannelCache uses a service goroutine that owns the map.
type CacheResponse struct {
	Value string
	Found bool
}

type CacheRequest struct {
	Op    string
	Key   string
	Value string
	Reply chan CacheResponse
}

func cacheService(requests <-chan CacheRequest) {
	items := make(map[string]string)
	for req := range requests {
		switch req.Op {
		case "set":
			items[req.Key] = req.Value
			req.Reply <- CacheResponse{Value: req.Value, Found: true}
		case "get":
			val, ok := items[req.Key]
			req.Reply <- CacheResponse{Value: val, Found: ok}
		}
	}
}

// example4CacheComparison runs the same operations on both cache implementations
// and verifies they produce identical results.
func example4CacheComparison() {
	fmt.Println("=== Example 4: Cache -- Both Approaches ===")

	// --- Mutex version ---
	fmt.Println("  Mutex Cache:")
	mc := NewMutexCache()
	mc.Set("language", "Go")
	mc.Set("creator", "Rob Pike")
	if val, ok := mc.Get("language"); ok {
		fmt.Printf("    language = %s\n", val)
	}
	if val, ok := mc.Get("creator"); ok {
		fmt.Printf("    creator = %s\n", val)
	}

	// --- Channel version ---
	fmt.Println("  Channel Cache:")
	requests := make(chan CacheRequest)
	go cacheService(requests)

	reply := make(chan CacheResponse, 1)
	requests <- CacheRequest{Op: "set", Key: "language", Value: "Go", Reply: reply}
	<-reply
	requests <- CacheRequest{Op: "set", Key: "creator", Value: "Rob Pike", Reply: reply}
	<-reply

	requests <- CacheRequest{Op: "get", Key: "language", Reply: reply}
	resp := <-reply
	fmt.Printf("    language = %s\n", resp.Value)

	requests <- CacheRequest{Op: "get", Key: "creator", Reply: reply}
	resp = <-reply
	fmt.Printf("    creator = %s\n", resp.Value)

	close(requests)
	fmt.Println()
}

// --- Hit Counter: benchmark comparing both approaches ---

var pages = []string{
	"/home", "/about", "/products", "/blog", "/contact",
	"/faq", "/pricing", "/docs", "/login", "/signup",
}

// MutexHitCounter protects a map with a mutex.
type MutexHitCounter struct {
	mu   sync.Mutex
	hits map[string]int
}

func NewMutexHitCounter() *MutexHitCounter {
	return &MutexHitCounter{hits: make(map[string]int)}
}

func (h *MutexHitCounter) Record(page string) {
	h.mu.Lock()
	h.hits[page]++
	h.mu.Unlock()
}

func (h *MutexHitCounter) Total() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	total := 0
	for _, c := range h.hits {
		total += c
	}
	return total
}

func (h *MutexHitCounter) TopPages(n int) []PageCount {
	h.mu.Lock()
	defer h.mu.Unlock()

	var entries []PageCount
	for page, count := range h.hits {
		entries = append(entries, PageCount{page, count})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Count > entries[j].Count
	})
	if n < len(entries) {
		entries = entries[:n]
	}
	return entries
}

type PageCount struct {
	Page  string
	Count int
}

// ChannelHitCounter uses a service goroutine to manage state.
type HitRequest struct {
	Op    string
	Page  string
	N     int
	Reply chan interface{}
}

func hitCounterService(requests <-chan HitRequest) {
	hits := make(map[string]int)

	for req := range requests {
		switch req.Op {
		case "record":
			hits[req.Page]++
			req.Reply <- nil

		case "total":
			total := 0
			for _, c := range hits {
				total += c
			}
			req.Reply <- total

		case "top":
			var entries []PageCount
			for page, count := range hits {
				entries = append(entries, PageCount{page, count})
			}
			sort.Slice(entries, func(i, j int) bool {
				return entries[i].Count > entries[j].Count
			})
			if req.N < len(entries) {
				entries = entries[:req.N]
			}
			req.Reply <- entries
		}
	}
}

// example5HitCounterBenchmark runs 10,000 operations on both implementations
// and compares timing. The mutex version is typically faster for this use case
// because channels have higher per-operation overhead.
func example5HitCounterBenchmark() {
	fmt.Println("=== Example 5: Hit Counter Benchmark ===")

	// --- Mutex version ---
	fmt.Println("\n  Mutex Version:")
	mhc := NewMutexHitCounter()
	var wg1 sync.WaitGroup

	start1 := time.Now()
	for g := 0; g < 100; g++ {
		wg1.Add(1)
		go func() {
			defer wg1.Done()
			for i := 0; i < 100; i++ {
				page := pages[rand.Intn(len(pages))]
				mhc.Record(page)
			}
		}()
	}
	wg1.Wait()
	elapsed1 := time.Since(start1)

	fmt.Printf("    Total hits: %d (expected 10000)\n", mhc.Total())
	fmt.Printf("    Time: %v\n", elapsed1)
	fmt.Println("    Top 3:")
	for _, p := range mhc.TopPages(3) {
		fmt.Printf("      %s: %d\n", p.Page, p.Count)
	}

	// --- Channel version ---
	fmt.Println("\n  Channel Version:")
	requests := make(chan HitRequest, 100)
	go hitCounterService(requests)

	var wg2 sync.WaitGroup
	start2 := time.Now()

	for g := 0; g < 100; g++ {
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			for i := 0; i < 100; i++ {
				page := pages[rand.Intn(len(pages))]
				reply := make(chan interface{}, 1)
				requests <- HitRequest{Op: "record", Page: page, Reply: reply}
				<-reply
			}
		}()
	}
	wg2.Wait()
	elapsed2 := time.Since(start2)

	// Query total.
	reply := make(chan interface{}, 1)
	requests <- HitRequest{Op: "total", Reply: reply}
	total := (<-reply).(int)

	// Query top 3.
	requests <- HitRequest{Op: "top", N: 3, Reply: reply}
	topResult := (<-reply).([]PageCount)

	fmt.Printf("    Total hits: %d (expected 10000)\n", total)
	fmt.Printf("    Time: %v\n", elapsed2)
	fmt.Println("    Top 3:")
	for _, p := range topResult {
		fmt.Printf("      %s: %d\n", p.Page, p.Count)
	}

	close(requests)

	// --- Comparison ---
	fmt.Println("\n  Comparison:")
	fmt.Printf("    Mutex:   %v\n", elapsed1)
	fmt.Printf("    Channel: %v\n", elapsed2)
	fmt.Println("    Mutex is typically faster for simple state guarding.")
	fmt.Println("    Channels shine for coordination, pipelines, and complex workflows.")
	fmt.Println()

	// --- Decision guide ---
	fmt.Println("  When to use which:")
	fmt.Println("    Channels: passing data, fan-out/fan-in, signaling, request-response")
	fmt.Println("    Mutexes:  protecting internal struct state, counters, caches")
	fmt.Println("    Both:     most real Go programs use both where each fits best")
}
