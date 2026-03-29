package main

import (
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"time"
)

// ============================================================
// Step 1: The problem — data race without protection
// Run with: go run -race main.go
// ============================================================

func step1DataRace() {
	fmt.Println("--- Step 1: Data Race (run with -race to detect) ---")

	var counter int
	var wg sync.WaitGroup

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			counter++ // DATA RACE: unsynchronized access
		}()
	}

	wg.Wait()
	fmt.Printf("Counter: %d (expected 1000, got something else with -race)\n", counter)
}

// ============================================================
// Step 2: Solution A — Mutex
// ============================================================

func step2Mutex() {
	fmt.Println("--- Step 2: Mutex Counter ---")

	var counter int
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
	fmt.Printf("Counter: %d (took %v)\n", counter, elapsed)
}

// ============================================================
// Step 3: Solution B — Channel
// ============================================================

func step3Channel() {
	fmt.Println("--- Step 3: Channel Counter ---")

	start := time.Now()

	// TODO: Create a channel for increment signals
	// increments := make(chan struct{}, 100)
	// result := make(chan int)

	// Counter goroutine: owns the counter, no shared state
	// TODO: Launch goroutine that counts increments, sends result when channel closes
	// go func() {
	//     counter := 0
	//     for range increments {
	//         counter++
	//     }
	//     result <- counter
	// }()

	// Send 1000 increments
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// TODO: Send increment signal
			// increments <- struct{}{}
		}()
	}

	wg.Wait()
	// TODO: Close increments channel and receive final count
	// close(increments)
	// total := <-result

	elapsed := time.Since(start)
	fmt.Printf("Counter: (implement me) (took %v)\n", elapsed)
}

// ============================================================
// Step 4: Richer problem — Concurrent Cache
// ============================================================

// --- Mutex version ---

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

// --- Channel version ---

type CacheResponse struct {
	Value string
	Found bool
}

type CacheRequest struct {
	Op    string // "get", "set"
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
			// TODO: Look up key, respond with value and found status
			val, ok := items[req.Key]
			req.Reply <- CacheResponse{Value: val, Found: ok}
		}
	}
}

func step4() {
	fmt.Println("--- Step 4: Cache Comparison ---")

	// Mutex version
	fmt.Println("  Mutex Cache:")
	mc := NewMutexCache()
	mc.Set("language", "Go")
	mc.Set("year", "2009")
	if val, ok := mc.Get("language"); ok {
		fmt.Println("    language =", val)
	}

	// Channel version
	fmt.Println("  Channel Cache:")
	requests := make(chan CacheRequest)
	go cacheService(requests)

	// TODO: Use the channel cache to set and get values
	// reply := make(chan CacheResponse, 1)
	// requests <- CacheRequest{Op: "set", Key: "language", Value: "Go", Reply: reply}
	// <-reply
	// requests <- CacheRequest{Op: "get", Key: "language", Reply: reply}
	// resp := <-reply
	// fmt.Println("    language =", resp.Value)

	close(requests)
}

// ============================================================
// Final Challenge: Hit Counter — Both Versions
//
// Track page views with record(page) and topPages(n)
// 100 goroutines, each recording 100 views across 10 pages
// Compare both implementations
// ============================================================

var pages = []string{
	"/home", "/about", "/products", "/blog", "/contact",
	"/faq", "/pricing", "/docs", "/login", "/signup",
}

// --- Mutex Hit Counter ---

type MutexHitCounter struct {
	mu   sync.Mutex
	hits map[string]int
}

func NewMutexHitCounter() *MutexHitCounter {
	return &MutexHitCounter{hits: make(map[string]int)}
}

func (h *MutexHitCounter) Record(page string) {
	// TODO: Lock, increment hits[page], unlock
}

func (h *MutexHitCounter) TopPages(n int) []struct {
	Page  string
	Count int
} {
	h.mu.Lock()
	defer h.mu.Unlock()

	type entry struct {
		Page  string
		Count int
	}
	var entries []entry
	for page, count := range h.hits {
		entries = append(entries, entry{page, count})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Count > entries[j].Count
	})

	result := make([]struct {
		Page  string
		Count int
	}, 0, n)
	for i := 0; i < n && i < len(entries); i++ {
		result = append(result, struct {
			Page  string
			Count int
		}{entries[i].Page, entries[i].Count})
	}
	return result
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

// --- Channel Hit Counter ---

type HitRequest struct {
	Op    string // "record", "top", "total"
	Page  string
	N     int
	Reply chan interface{}
}

func hitCounterService(requests <-chan HitRequest) {
	hits := make(map[string]int)

	for req := range requests {
		switch req.Op {
		case "record":
			// TODO: Increment hits[req.Page], reply with nil
			hits[req.Page]++
			req.Reply <- nil

		case "total":
			// TODO: Sum all hits, reply with total (int)
			total := 0
			for _, c := range hits {
				total += c
			}
			req.Reply <- total

		case "top":
			// TODO: Sort and reply with top N pages
			type entry struct {
				Page  string
				Count int
			}
			var entries []entry
			for page, count := range hits {
				entries = append(entries, entry{page, count})
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

func finalChallenge() {
	fmt.Println("--- Final: Hit Counter Comparison ---")

	// ---- Mutex version ----
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

	// ---- Channel version ----
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
				// TODO: Send record request and wait for reply
				requests <- HitRequest{Op: "record", Page: page, Reply: reply}
				<-reply
			}
		}()
	}
	wg2.Wait()
	elapsed2 := time.Since(start2)

	// Query total
	reply := make(chan interface{}, 1)
	requests <- HitRequest{Op: "total", Reply: reply}
	total := (<-reply).(int)

	// Query top 3
	requests <- HitRequest{Op: "top", N: 3, Reply: reply}
	// topResult := (<-reply) // type assert to use

	fmt.Printf("    Total hits: %d (expected 10000)\n", total)
	fmt.Printf("    Time: %v\n", elapsed2)

	// TODO: Print top 3 pages from channel version

	close(requests)

	// Comparison
	fmt.Println("\n  Comparison:")
	fmt.Printf("    Mutex:   %v\n", elapsed1)
	fmt.Printf("    Channel: %v\n", elapsed2)
	fmt.Println("    (Mutex is typically faster for this use case)")
	fmt.Println("    (Channels shine for coordination, not simple state guarding)")
}

func main() {
	// Comment out step1 if running with -race (it intentionally races)
	step1DataRace()
	fmt.Println()

	step2Mutex()
	fmt.Println()

	step3Channel()
	fmt.Println()

	step4()
	fmt.Println()

	finalChallenge()
}
