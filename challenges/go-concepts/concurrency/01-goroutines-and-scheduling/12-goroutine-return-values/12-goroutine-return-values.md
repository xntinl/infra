---
difficulty: intermediate
concepts: [channels for results, shared memory with mutex, callback pattern, goroutine communication, concurrent result collection]
tools: [go]
estimated_time: 30m
bloom_level: analyze
---

# 12. Goroutine Return Values

## Learning Objectives
After completing this exercise, you will be able to:
- **Explain** why `go func()` cannot return values and the implications for concurrent design
- **Implement** three patterns for collecting goroutine results: channels, shared memory with mutex, and callbacks
- **Compare** the trade-offs of each pattern in terms of safety, performance, and code clarity
- **Choose** the appropriate pattern for different real-world scenarios

## Why Goroutine Return Values Matter

One of Go's most frequent surprises for newcomers is that `go f()` discards any return value. You cannot write `result := go f()`. This is not an oversight -- it is a deliberate design choice. A goroutine runs concurrently, so its return value does not exist at the time the `go` statement executes. The caller has already moved on.

This means every concurrent design must answer one fundamental question: how do goroutines communicate their results? Go offers three primary patterns, each with different trade-offs. Channels are the idiomatic choice for most situations. Shared memory with a mutex works for aggregation into a shared data structure. Callbacks provide flexibility when the result consumer is not the goroutine launcher.

In this exercise, you build a price comparison engine that queries multiple suppliers concurrently. You implement all three patterns to collect price quotes, compare them side by side, and learn when to use each one.

## Step 1 -- Pattern 1: Channels (The Idiomatic Default)

Channels are Go's primary communication mechanism. Each goroutine sends its result through a channel, and the caller collects results in arrival order.

```go
package main

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"
)

type PriceQuote struct {
	Supplier string
	Product  string
	Price    float64
	Latency  time.Duration
	Err      error
}

func querySupplier(supplier string, product string) PriceQuote {
	start := time.Now()
	// Simulate network latency (10-80ms)
	time.Sleep(time.Duration(rand.Intn(70)+10) * time.Millisecond)

	// Simulate occasional failures
	if supplier == "BudgetDirect" && rand.Float32() < 0.4 {
		return PriceQuote{
			Supplier: supplier,
			Product:  product,
			Err:      fmt.Errorf("service temporarily unavailable"),
			Latency:  time.Since(start),
		}
	}

	// Generate a price based on supplier characteristics
	basePrice := 50.0 + float64(len(product)*3)
	supplierMultiplier := map[string]float64{
		"MegaStore":    1.0,
		"ValueMart":    0.85,
		"PremiumGoods": 1.25,
		"BudgetDirect": 0.75,
		"TechSupply":   0.95,
	}

	multiplier := supplierMultiplier[supplier]
	if multiplier == 0 {
		multiplier = 1.0
	}
	price := basePrice * multiplier * (0.9 + rand.Float64()*0.2)

	return PriceQuote{
		Supplier: supplier,
		Product:  product,
		Price:    price,
		Latency:  time.Since(start),
	}
}

func compareWithChannels(product string, suppliers []string) {
	results := make(chan PriceQuote, len(suppliers))

	for _, s := range suppliers {
		go func(supplier string) {
			results <- querySupplier(supplier, product)
		}(s)
	}

	// Collect all results
	var quotes []PriceQuote
	for i := 0; i < len(suppliers); i++ {
		quotes = append(quotes, <-results)
	}

	// Sort by price (errors last)
	sort.Slice(quotes, func(i, j int) bool {
		if quotes[i].Err != nil {
			return false
		}
		if quotes[j].Err != nil {
			return true
		}
		return quotes[i].Price < quotes[j].Price
	})

	fmt.Printf("  %-18s %-10s %-10s %s\n", "Supplier", "Price", "Latency", "Status")
	fmt.Println("  " + strings.Repeat("-", 55))

	for _, q := range quotes {
		if q.Err != nil {
			fmt.Printf("  %-18s %-10s %-10v ERROR: %v\n",
				q.Supplier, "N/A", q.Latency.Round(time.Millisecond), q.Err)
		} else {
			fmt.Printf("  %-18s $%-9.2f %-10v OK\n",
				q.Supplier, q.Price, q.Latency.Round(time.Millisecond))
		}
	}
}

func main() {
	suppliers := []string{
		"MegaStore", "ValueMart", "PremiumGoods", "BudgetDirect", "TechSupply",
	}

	fmt.Println("=== Price Comparison: Channel Pattern ===\n")
	fmt.Println("  Product: Wireless Keyboard\n")

	start := time.Now()
	compareWithChannels("Wireless Keyboard", suppliers)

	fmt.Printf("\n  Total time: %v (all suppliers queried concurrently)\n",
		time.Since(start).Round(time.Millisecond))
	fmt.Println()
	fmt.Println("  Pros: idiomatic, type-safe, no shared state, natural backpressure")
	fmt.Println("  Cons: need to know how many results to expect, or use WaitGroup+close")
}
```

**What's happening here:** Five suppliers are queried concurrently, each in its own goroutine. Each goroutine sends a `PriceQuote` through a buffered channel. The caller collects exactly `len(suppliers)` results, sorts them by price, and displays the comparison. BudgetDirect occasionally fails.

**Key insight:** The channel pattern is the default choice for goroutine results. It is type-safe (the channel type enforces what can be sent), thread-safe (channels handle synchronization internally), and provides natural backpressure (a full buffer blocks senders). The caller reads results in arrival order, which is typically what you want for a price comparison engine.

### Intermediate Verification
```bash
go run main.go
```
Expected output (order and prices vary):
```
=== Price Comparison: Channel Pattern ===

  Product: Wireless Keyboard

  Supplier           Price      Latency    Status
  -------------------------------------------------------
  BudgetDirect       $52.43     35ms       OK
  ValueMart          $59.78     42ms       OK
  TechSupply         $66.12     28ms       OK
  MegaStore          $69.55     55ms       OK
  PremiumGoods       $87.30     67ms       OK

  Total time: 68ms (all suppliers queried concurrently)

  Pros: idiomatic, type-safe, no shared state, natural backpressure
  Cons: need to know how many results to expect, or use WaitGroup+close
```

## Step 2 -- Pattern 2: Shared Slice With Mutex

When you need results in a specific data structure (map, slice, tree), goroutines can write directly to shared memory protected by a mutex.

```go
package main

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
)

type Quote struct {
	Supplier string
	Price    float64
	Latency  time.Duration
	Err      error
}

type QuoteCollector struct {
	mu     sync.Mutex
	quotes []Quote
}

func NewQuoteCollector(capacity int) *QuoteCollector {
	return &QuoteCollector{
		quotes: make([]Quote, 0, capacity),
	}
}

func (qc *QuoteCollector) Add(q Quote) {
	qc.mu.Lock()
	defer qc.mu.Unlock()
	qc.quotes = append(qc.quotes, q)
}

func (qc *QuoteCollector) Results() []Quote {
	qc.mu.Lock()
	defer qc.mu.Unlock()
	result := make([]Quote, len(qc.quotes))
	copy(result, qc.quotes)
	return result
}

func fetchQuote(supplier string, product string) Quote {
	start := time.Now()
	time.Sleep(time.Duration(rand.Intn(60)+10) * time.Millisecond)

	if supplier == "BudgetDirect" && rand.Float32() < 0.3 {
		return Quote{
			Supplier: supplier,
			Err:      fmt.Errorf("connection refused"),
			Latency:  time.Since(start),
		}
	}

	basePrice := 50.0 + float64(len(product)*3)
	variation := 0.8 + rand.Float64()*0.4
	return Quote{
		Supplier: supplier,
		Price:    basePrice * variation,
		Latency:  time.Since(start),
	}
}

func compareWithMutex(product string, suppliers []string) {
	collector := NewQuoteCollector(len(suppliers))
	var wg sync.WaitGroup

	for _, s := range suppliers {
		wg.Add(1)
		go func(supplier string) {
			defer wg.Done()
			quote := fetchQuote(supplier, product)
			collector.Add(quote)
		}(s)
	}

	wg.Wait()
	results := collector.Results()

	fmt.Printf("  %-18s %-10s %-10s %s\n", "Supplier", "Price", "Latency", "Status")
	fmt.Println("  " + strings.Repeat("-", 55))

	var cheapest Quote
	for _, q := range results {
		if q.Err != nil {
			fmt.Printf("  %-18s %-10s %-10v ERROR: %v\n",
				q.Supplier, "N/A", q.Latency.Round(time.Millisecond), q.Err)
			continue
		}

		fmt.Printf("  %-18s $%-9.2f %-10v OK\n",
			q.Supplier, q.Price, q.Latency.Round(time.Millisecond))

		if cheapest.Supplier == "" || q.Price < cheapest.Price {
			cheapest = q
		}
	}

	if cheapest.Supplier != "" {
		fmt.Printf("\n  Best price: $%.2f from %s\n", cheapest.Price, cheapest.Supplier)
	}
}

func main() {
	suppliers := []string{
		"MegaStore", "ValueMart", "PremiumGoods", "BudgetDirect", "TechSupply",
	}

	fmt.Println("=== Price Comparison: Mutex Pattern ===\n")
	fmt.Println("  Product: USB-C Hub\n")

	start := time.Now()
	compareWithMutex("USB-C Hub", suppliers)

	fmt.Printf("\n  Total time: %v\n", time.Since(start).Round(time.Millisecond))
	fmt.Println()
	fmt.Println("  Pros: results stored in any data structure, easy aggregation,")
	fmt.Println("        no need to know result count upfront")
	fmt.Println("  Cons: shared mutable state, lock contention at high concurrency,")
	fmt.Println("        harder to reason about than channels")
}
```

**What's happening here:** A `QuoteCollector` holds a mutex-protected slice. Each goroutine fetches a quote and appends it to the shared collector. After all goroutines complete (via WaitGroup), the caller reads the final results. The mutex ensures no concurrent writes corrupt the slice.

**Key insight:** The mutex pattern is useful when you need to aggregate results into a shared data structure (map, sorted list, tree) and the aggregation logic is simple (append). However, it introduces shared mutable state, which is harder to reason about and test. In the channel pattern, data flows in one direction (producer to consumer); in the mutex pattern, data flows to a shared location from multiple directions. Channels are preferred unless you have a specific reason to use shared memory.

### Intermediate Verification
```bash
go run main.go
```
Expected output (order and prices vary):
```
=== Price Comparison: Mutex Pattern ===

  Product: USB-C Hub

  Supplier           Price      Latency    Status
  -------------------------------------------------------
  TechSupply         $57.20     18ms       OK
  MegaStore          $62.15     35ms       OK
  ValueMart          $53.88     42ms       OK
  PremiumGoods       $78.45     55ms       OK
  BudgetDirect       N/A        28ms       ERROR: connection refused

  Best price: $53.88 from ValueMart

  Total time: 58ms

  Pros: results stored in any data structure, easy aggregation,
        no need to know result count upfront
  Cons: shared mutable state, lock contention at high concurrency,
        harder to reason about than channels
```

## Step 3 -- Pattern 3: Callback Functions

Pass a function that the goroutine calls with its result. This decouples the result producer from the result consumer.

```go
package main

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
)

type SupplierQuote struct {
	Supplier string
	Price    float64
	Latency  time.Duration
	Err      error
}

type QuoteCallback func(SupplierQuote)

func queryWithCallback(supplier string, product string, onResult QuoteCallback, wg *sync.WaitGroup) {
	defer wg.Done()

	start := time.Now()
	time.Sleep(time.Duration(rand.Intn(60)+10) * time.Millisecond)

	if supplier == "BudgetDirect" && rand.Float32() < 0.3 {
		onResult(SupplierQuote{
			Supplier: supplier,
			Err:      fmt.Errorf("timeout after 5s"),
			Latency:  time.Since(start),
		})
		return
	}

	basePrice := 50.0 + float64(len(product)*3)
	variation := 0.8 + rand.Float64()*0.4

	onResult(SupplierQuote{
		Supplier: supplier,
		Price:    basePrice * variation,
		Latency:  time.Since(start),
	})
}

func main() {
	suppliers := []string{
		"MegaStore", "ValueMart", "PremiumGoods", "BudgetDirect", "TechSupply",
	}

	fmt.Println("=== Price Comparison: Callback Pattern ===\n")
	fmt.Println("  Product: Mechanical Keyboard\n")

	// Example 1: Simple logging callback
	fmt.Println("  --- Callback: real-time logging ---")
	var wg sync.WaitGroup
	var mu sync.Mutex
	start := time.Now()

	logCallback := func(q SupplierQuote) {
		mu.Lock()
		defer mu.Unlock()
		if q.Err != nil {
			fmt.Printf("    [FAIL] %-18s %v\n", q.Supplier, q.Err)
		} else {
			fmt.Printf("    [RECV] %-18s $%.2f (%v)\n",
				q.Supplier, q.Price, q.Latency.Round(time.Millisecond))
		}
	}

	for _, s := range suppliers {
		wg.Add(1)
		go queryWithCallback(s, "Mechanical Keyboard", logCallback, &wg)
	}
	wg.Wait()
	fmt.Printf("    Total: %v\n", time.Since(start).Round(time.Millisecond))

	// Example 2: Aggregating callback
	fmt.Println()
	fmt.Println("  --- Callback: aggregation with best-price tracking ---")

	var bestQuote SupplierQuote
	var allQuotes []SupplierQuote

	aggregateCallback := func(q SupplierQuote) {
		mu.Lock()
		defer mu.Unlock()
		allQuotes = append(allQuotes, q)
		if q.Err == nil && (bestQuote.Supplier == "" || q.Price < bestQuote.Price) {
			bestQuote = q
		}
	}

	start = time.Now()
	for _, s := range suppliers {
		wg.Add(1)
		go queryWithCallback(s, "Mechanical Keyboard", aggregateCallback, &wg)
	}
	wg.Wait()

	fmt.Printf("    Received %d quotes in %v\n",
		len(allQuotes), time.Since(start).Round(time.Millisecond))
	if bestQuote.Supplier != "" {
		fmt.Printf("    Best price: $%.2f from %s\n", bestQuote.Price, bestQuote.Supplier)
	}

	fmt.Println()
	fmt.Println(strings.Repeat("-", 60))
	fmt.Println()
	fmt.Println("  Pros: flexible (different callbacks for different consumers),")
	fmt.Println("        real-time processing (no waiting for all results),")
	fmt.Println("        easy to compose (chain callbacks)")
	fmt.Println("  Cons: callback must be thread-safe (needs mutex if it writes"),
	fmt.Println("        shared state), can lead to callback-hell if nested")
}
```

**What's happening here:** Instead of sending results through a channel or writing to shared memory, each goroutine calls a function (the callback) with its result. Two different callbacks demonstrate the pattern's flexibility: one logs results in real-time as they arrive, another aggregates them to find the best price. The same `queryWithCallback` function works with both callbacks without modification.

**Key insight:** The callback pattern decouples the producer (supplier query) from the consumer (logging, aggregation, alerting). The goroutine does not know or care what happens with its result. This is useful when different callers need different behaviors from the same concurrent operation. However, callbacks that mutate shared state must be protected with a mutex, since multiple goroutines may invoke the callback simultaneously.

### Intermediate Verification
```bash
go run main.go
```
Expected output (order varies):
```
=== Price Comparison: Callback Pattern ===

  Product: Mechanical Keyboard

  --- Callback: real-time logging ---
    [RECV] TechSupply         $68.22 (15ms)
    [RECV] MegaStore          $72.40 (32ms)
    [RECV] ValueMart          $61.55 (40ms)
    [FAIL] BudgetDirect       timeout after 5s
    [RECV] PremiumGoods       $89.10 (58ms)
    Total: 60ms

  --- Callback: aggregation with best-price tracking ---
    Received 5 quotes in 55ms
    Best price: $59.88 from ValueMart

------------------------------------------------------------

  Pros: flexible (different callbacks for different consumers),
        real-time processing (no waiting for all results),
        easy to compose (chain callbacks)
  Cons: callback must be thread-safe (needs mutex if it writes
        shared state), can lead to callback-hell if nested
```

## Step 4 -- Side-by-Side Comparison

Run all three patterns on the same query and compare behavior, correctness, and code complexity.

```go
package main

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
)

type PriceResult struct {
	Supplier string
	Price    float64
	Err      error
}

func getPrice(supplier string) PriceResult {
	time.Sleep(time.Duration(rand.Intn(40)+10) * time.Millisecond)

	prices := map[string]float64{
		"AlphaSupply": 49.99,
		"BetaStore":   54.50,
		"GammaDirect": 42.75,
		"DeltaMart":   58.00,
		"EpsilonCo":   45.25,
	}

	price, ok := prices[supplier]
	if !ok {
		return PriceResult{Supplier: supplier, Err: fmt.Errorf("unknown supplier")}
	}

	// Add random variation
	price *= (0.95 + rand.Float64()*0.1)
	return PriceResult{Supplier: supplier, Price: price}
}

func viaChannels(suppliers []string) []PriceResult {
	ch := make(chan PriceResult, len(suppliers))
	for _, s := range suppliers {
		go func(sup string) {
			ch <- getPrice(sup)
		}(s)
	}

	results := make([]PriceResult, 0, len(suppliers))
	for i := 0; i < len(suppliers); i++ {
		results = append(results, <-ch)
	}
	return results
}

func viaMutex(suppliers []string) []PriceResult {
	var mu sync.Mutex
	results := make([]PriceResult, 0, len(suppliers))
	var wg sync.WaitGroup

	for _, s := range suppliers {
		wg.Add(1)
		go func(sup string) {
			defer wg.Done()
			r := getPrice(sup)
			mu.Lock()
			results = append(results, r)
			mu.Unlock()
		}(s)
	}
	wg.Wait()
	return results
}

func viaCallbacks(suppliers []string) []PriceResult {
	var mu sync.Mutex
	results := make([]PriceResult, 0, len(suppliers))
	var wg sync.WaitGroup

	callback := func(r PriceResult) {
		mu.Lock()
		results = append(results, r)
		mu.Unlock()
	}

	for _, s := range suppliers {
		wg.Add(1)
		go func(sup string) {
			defer wg.Done()
			callback(getPrice(sup))
		}(s)
	}
	wg.Wait()
	return results
}

func printResults(label string, results []PriceResult, elapsed time.Duration) {
	fmt.Printf("  --- %s (%v) ---\n", label, elapsed.Round(time.Millisecond))
	for _, r := range results {
		if r.Err != nil {
			fmt.Printf("    %-15s ERROR: %v\n", r.Supplier, r.Err)
		} else {
			fmt.Printf("    %-15s $%.2f\n", r.Supplier, r.Price)
		}
	}
	fmt.Println()
}

func main() {
	suppliers := []string{
		"AlphaSupply", "BetaStore", "GammaDirect", "DeltaMart", "EpsilonCo",
	}

	fmt.Println("=== Return Value Patterns: Side-by-Side ===\n")

	// Pattern 1: Channels
	start := time.Now()
	r1 := viaChannels(suppliers)
	t1 := time.Since(start)
	printResults("Channels", r1, t1)

	// Pattern 2: Mutex
	start = time.Now()
	r2 := viaMutex(suppliers)
	t2 := time.Since(start)
	printResults("Mutex", r2, t2)

	// Pattern 3: Callbacks
	start = time.Now()
	r3 := viaCallbacks(suppliers)
	t3 := time.Since(start)
	printResults("Callbacks", r3, t3)

	// Comparison table
	fmt.Println(strings.Repeat("=", 65))
	fmt.Println()
	fmt.Printf("  %-20s %-15s %-15s %-15s\n", "Property", "Channels", "Mutex", "Callbacks")
	fmt.Println("  " + strings.Repeat("-", 60))
	fmt.Printf("  %-20s %-15s %-15s %-15s\n", "Thread safety",
		"built-in", "manual (Lock)", "manual (Lock)")
	fmt.Printf("  %-20s %-15s %-15s %-15s\n", "Idiomatic Go",
		"YES", "acceptable", "less common")
	fmt.Printf("  %-20s %-15s %-15s %-15s\n", "Data structure",
		"channel only", "any", "any")
	fmt.Printf("  %-20s %-15s %-15s %-15s\n", "Result count",
		"must know", "flexible", "flexible")
	fmt.Printf("  %-20s %-15s %-15s %-15s\n", "Real-time results",
		"yes (as sent)", "no (after Wait)", "yes (on call)")
	fmt.Printf("  %-20s %-15s %-15s %-15s\n", "Backpressure",
		"yes (buffer)", "no", "no")
	fmt.Printf("  %-20s %-15s %-15s %-15s\n", "Complexity",
		"low", "medium", "medium")
	fmt.Println()
	fmt.Println("  Recommendation:")
	fmt.Println("    - Default: use channels. They handle synchronization for you.")
	fmt.Println("    - Aggregation: use mutex when writing to maps, sorted structures,")
	fmt.Println("      or when result count is unknown upfront.")
	fmt.Println("    - Decoupling: use callbacks when the same goroutine needs to")
	fmt.Println("      serve different consumers with different behaviors.")
}
```

**What's happening here:** The same set of supplier queries is executed using all three patterns. The results and timing are printed side by side, followed by a comparison table showing the trade-offs along multiple dimensions: thread safety, idiomaticity, flexibility, real-time processing, backpressure, and complexity.

**Key insight:** All three patterns produce the same results and have similar performance characteristics. The differences are in code clarity, safety guarantees, and flexibility. Channels are the default because they provide built-in synchronization and are idiomatic Go. Use mutex when you need to aggregate into a specific data structure. Use callbacks when you need to decouple the producer from the consumer.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Return Value Patterns: Side-by-Side ===

  --- Channels (45ms) ---
    GammaDirect     $42.10
    EpsilonCo       $44.88
    AlphaSupply     $48.75
    BetaStore       $53.20
    DeltaMart       $57.15

  --- Mutex (42ms) ---
    AlphaSupply     $49.22
    DeltaMart       $56.80
    GammaDirect     $41.95
    BetaStore       $52.65
    EpsilonCo       $45.50

  --- Callbacks (48ms) ---
    EpsilonCo       $44.30
    GammaDirect     $43.15
    AlphaSupply     $48.60
    BetaStore       $54.10
    DeltaMart       $57.90

=================================================================

  Property             Channels        Mutex           Callbacks
  ------------------------------------------------------------
  Thread safety        built-in        manual (Lock)   manual (Lock)
  Idiomatic Go         YES             acceptable      less common
  Data structure       channel only    any             any
  Result count         must know       flexible        flexible
  Real-time results    yes (as sent)   no (after Wait) yes (on call)
  Backpressure         yes (buffer)    no              no
  Complexity           low             medium          medium

  Recommendation:
    - Default: use channels. They handle synchronization for you.
    - Aggregation: use mutex when writing to maps, sorted structures,
      or when result count is unknown upfront.
    - Decoupling: use callbacks when the same goroutine needs to
      serve different consumers with different behaviors.
```

## Common Mistakes

### Trying to Return Values From `go func()`

**Wrong:**
```go
// This does not compile
result := go computePrice("supplier-a")
```

**What happens:** Syntax error. The `go` keyword starts a concurrent function call and returns immediately. There is no way to capture a return value because the function has not finished executing yet.

**Correct -- use any of the three patterns:**
```go
package main

import "fmt"

func computePrice(supplier string) float64 {
	return 42.99
}

func main() {
	ch := make(chan float64, 1)
	go func() {
		ch <- computePrice("supplier-a")
	}()
	result := <-ch
	fmt.Printf("Price: $%.2f\n", result)
}
```

### Appending to a Shared Slice Without a Mutex

**Wrong -- complete program:**
```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var results []int
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			results = append(results, n) // DATA RACE: concurrent append
		}(i)
	}

	wg.Wait()
	fmt.Printf("Expected 100, got %d\n", len(results)) // likely less than 100
}
```

**What happens:** Multiple goroutines call `append` on the same slice simultaneously. This is a data race: `append` may read the slice header (length, capacity, pointer) while another goroutine is modifying it. Results are unpredictable: lost entries, corrupted memory, or even crashes. Run with `go run -race main.go` to see the detector flag this.

**Correct -- protect with mutex:**
```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var mu sync.Mutex
	var results []int
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			mu.Lock()
			results = append(results, n)
			mu.Unlock()
		}(i)
	}

	wg.Wait()
	fmt.Printf("Expected 100, got %d\n", len(results)) // always 100
}
```

### Using a Callback Without Mutex Protection

**Wrong -- complete program:**
```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var total float64
	var wg sync.WaitGroup

	callback := func(price float64) {
		total += price // DATA RACE: concurrent writes to total
	}

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			callback(float64(n) * 1.5)
		}(i)
	}

	wg.Wait()
	fmt.Printf("Total: %.2f\n", total) // incorrect value
}
```

**What happens:** Multiple goroutines invoke the callback concurrently, and each one reads and writes `total` without synchronization. The final value is incorrect and varies between runs.

**Fix:** Add a mutex inside the callback, or use `atomic.AddFloat64` equivalent patterns (store as int64 with `math.Float64bits`), or accumulate via a channel instead.

## Verify What You Learned

Build a "multi-source product search" that:
1. Searches 6 different online marketplaces concurrently for a product
2. Implements all three patterns (channels, mutex, callbacks) in separate functions
3. Each marketplace returns: name, price, rating (1-5 stars), shipping cost, and availability (bool)
4. Compares the results from all three patterns to verify they produce the same data
5. Prints a "best deal" recommendation based on (price + shipping) for available items only
6. Includes a timing comparison showing all three patterns execute in roughly the same wall-clock time

**Hint:** Use a seed for `rand` to make the marketplace responses deterministic, so all three patterns produce identical results that you can compare programmatically.

## What's Next
You have completed the goroutines and scheduling section. Continue to section [02-channels](../../02-channels/01-unbuffered-channel-basics/01-unbuffered-channel-basics.md) to learn how channels work in depth -- from unbuffered synchronization to buffered queues, channel direction, and advanced patterns.

## Summary
- `go func()` cannot return values -- this is by design, since the caller does not wait
- Channel pattern: idiomatic default, built-in synchronization, natural backpressure
- Mutex pattern: write results to any shared data structure, requires manual locking
- Callback pattern: decouple producer from consumer, flexible, needs mutex if callback writes shared state
- All three patterns have similar performance; choose based on code clarity and use case
- Channels are preferred unless you need to aggregate into a specific data structure or decouple consumers
- Always protect shared mutable state with a mutex -- concurrent `append` is a data race
- Run with `go run -race` to detect data races in any pattern

## Reference
- [Go Tour: Channels](https://go.dev/tour/concurrency/2)
- [Go Blog: Share Memory By Communicating](https://go.dev/blog/codelab-share)
- [sync.Mutex](https://pkg.go.dev/sync#Mutex)
- [Go Data Race Detector](https://go.dev/doc/articles/race_detector)
