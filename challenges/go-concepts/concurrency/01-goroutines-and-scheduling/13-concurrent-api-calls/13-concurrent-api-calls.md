---
difficulty: intermediate
concepts: [fan-out goroutines, buffered channels, partial failure handling, struct methods, timing comparison]
tools: [go]
estimated_time: 30m
bloom_level: apply
---


# 13. Concurrent API Calls


## Learning Objectives
After completing this exercise, you will be able to:
- **Design** a struct-based API client that launches concurrent requests using goroutines
- **Implement** the fan-out pattern to query multiple suppliers simultaneously
- **Handle** partial failures where some goroutines succeed and others return errors
- **Measure** and report the speedup gained from concurrent execution over sequential execution


## Why Concurrent API Calls

Most backend services depend on data from multiple external systems. An inventory dashboard might pull stock levels from five different supplier APIs. If each call takes 200ms and you run them sequentially, the user waits a full second. But these calls are independent -- the result from Supplier A has no effect on the request to Supplier B. This is the textbook case for fan-out concurrency: launch one goroutine per call, let them all run simultaneously, and collect results through a channel. The total time drops from the sum of all latencies to the latency of the slowest single call.

The challenge is that real APIs fail. A supplier might be down, timing out, or returning garbage. Your code must handle partial failures gracefully: collect whatever succeeds, report what failed, and never block forever waiting for a response that will not come. This exercise builds that pattern from scratch.


## Step 1 -- Sequential Supplier Query

Start with the slow version. A `SupplierClient` struct represents a single supplier with a name and simulated latency. The `FetchInventory` method simulates an HTTP call by sleeping, then returns an `InventoryResult`. Running five suppliers sequentially establishes the baseline timing.

```go
package main

import (
	"fmt"
	"time"
)

const productName = "Industrial Sensor XR-500"

type InventoryResult struct {
	Supplier  string
	Product   string
	Quantity  int
	Latency   time.Duration
	Available bool
}

type SupplierClient struct {
	Name    string
	Latency time.Duration
	Stock   int
}

func NewSupplierClient(name string, latency time.Duration, stock int) *SupplierClient {
	return &SupplierClient{
		Name:    name,
		Latency: latency,
		Stock:   stock,
	}
}

func (sc *SupplierClient) FetchInventory(product string) InventoryResult {
	start := time.Now()
	time.Sleep(sc.Latency)
	return InventoryResult{
		Supplier:  sc.Name,
		Product:   product,
		Quantity:  sc.Stock,
		Latency:   time.Since(start),
		Available: sc.Stock > 0,
	}
}

func queryAllSequential(suppliers []*SupplierClient, product string) []InventoryResult {
	var results []InventoryResult
	for _, s := range suppliers {
		results = append(results, s.FetchInventory(product))
	}
	return results
}

func printResults(results []InventoryResult, wallClock time.Duration) {
	fmt.Println("  Supplier                 Qty    Latency    Available")
	fmt.Println("  -------                  ---    -------    ---------")
	for _, r := range results {
		fmt.Printf("  %-24s %4d   %7v    %v\n",
			r.Supplier, r.Quantity, r.Latency.Round(time.Millisecond), r.Available)
	}
	fmt.Printf("\n  Wall clock: %v\n", wallClock.Round(time.Millisecond))
}

func main() {
	suppliers := []*SupplierClient{
		NewSupplierClient("Acme Industrial", 200*time.Millisecond, 42),
		NewSupplierClient("GlobalParts Co.", 180*time.Millisecond, 15),
		NewSupplierClient("QuickSupply Ltd.", 250*time.Millisecond, 0),
		NewSupplierClient("TechSource Inc.", 120*time.Millisecond, 88),
		NewSupplierClient("MegaStock Corp.", 300*time.Millisecond, 23),
	}

	fmt.Println("=== Sequential Supplier Query ===")
	start := time.Now()
	results := queryAllSequential(suppliers, productName)
	wallClock := time.Since(start)
	printResults(results, wallClock)
}
```

**What's happening here:** Each `FetchInventory` call blocks until its `time.Sleep` completes. The total wall clock is the sum of all latencies: 200+180+250+120+300 = ~1050ms. The user stares at a spinner for over a second while five independent calls run one after another.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Sequential Supplier Query ===
  Supplier                 Qty    Latency    Available
  -------                  ---    -------    ---------
  Acme Industrial            42     200ms    true
  GlobalParts Co.            15     180ms    true
  QuickSupply Ltd.            0     250ms    false
  TechSource Inc.            88     120ms    true
  MegaStock Corp.            23     300ms    true

  Wall clock: 1.05s
```


## Step 2 -- Concurrent Version with Buffered Channel

Now launch one goroutine per supplier and collect results through a buffered channel. The wall clock drops to the latency of the slowest supplier.

```go
package main

import (
	"fmt"
	"time"
)

const productName = "Industrial Sensor XR-500"

type InventoryResult struct {
	Supplier  string
	Product   string
	Quantity  int
	Latency   time.Duration
	Available bool
}

type SupplierClient struct {
	Name    string
	Latency time.Duration
	Stock   int
}

func NewSupplierClient(name string, latency time.Duration, stock int) *SupplierClient {
	return &SupplierClient{
		Name:    name,
		Latency: latency,
		Stock:   stock,
	}
}

func (sc *SupplierClient) FetchInventory(product string) InventoryResult {
	start := time.Now()
	time.Sleep(sc.Latency)
	return InventoryResult{
		Supplier:  sc.Name,
		Product:   product,
		Quantity:  sc.Stock,
		Latency:   time.Since(start),
		Available: sc.Stock > 0,
	}
}

func queryAllConcurrent(suppliers []*SupplierClient, product string) []InventoryResult {
	results := make(chan InventoryResult, len(suppliers))

	for _, s := range suppliers {
		go func(client *SupplierClient) {
			results <- client.FetchInventory(product)
		}(s)
	}

	var collected []InventoryResult
	for i := 0; i < len(suppliers); i++ {
		collected = append(collected, <-results)
	}
	return collected
}

func printResults(results []InventoryResult, wallClock time.Duration) {
	fmt.Println("  Supplier                 Qty    Latency    Available")
	fmt.Println("  -------                  ---    -------    ---------")
	for _, r := range results {
		fmt.Printf("  %-24s %4d   %7v    %v\n",
			r.Supplier, r.Quantity, r.Latency.Round(time.Millisecond), r.Available)
	}
	fmt.Printf("\n  Wall clock: %v\n", wallClock.Round(time.Millisecond))
}

func main() {
	suppliers := []*SupplierClient{
		NewSupplierClient("Acme Industrial", 200*time.Millisecond, 42),
		NewSupplierClient("GlobalParts Co.", 180*time.Millisecond, 15),
		NewSupplierClient("QuickSupply Ltd.", 250*time.Millisecond, 0),
		NewSupplierClient("TechSource Inc.", 120*time.Millisecond, 88),
		NewSupplierClient("MegaStock Corp.", 300*time.Millisecond, 23),
	}

	fmt.Println("=== Concurrent Supplier Query ===")
	start := time.Now()
	results := queryAllConcurrent(suppliers, productName)
	wallClock := time.Since(start)
	printResults(results, wallClock)
}
```

**What's happening here:** Five goroutines start nearly simultaneously. Each sends its result into a buffered channel when done. The main goroutine drains the channel, receiving results in completion order (fastest first). The wall clock equals the slowest supplier: ~300ms instead of ~1050ms.

**Key insight:** The buffered channel with capacity `len(suppliers)` ensures no goroutine blocks on send. Without buffering, a goroutine that finishes early would block until the main goroutine is ready to receive, which could slow things down if receive logic is complex.

### Intermediate Verification
```bash
go run main.go
```
Expected output (order varies -- fastest suppliers arrive first):
```
=== Concurrent Supplier Query ===
  Supplier                 Qty    Latency    Available
  -------                  ---    -------    ---------
  TechSource Inc.            88     120ms    true
  GlobalParts Co.            15     180ms    true
  Acme Industrial            42     200ms    true
  QuickSupply Ltd.            0     250ms    false
  MegaStock Corp.            23     300ms    true

  Wall clock: 300ms
```


## Step 3 -- Partial Failure Handling

Real suppliers go down. The `FetchInventory` method now returns an error for some suppliers. The concurrent collector must separate successes from failures and report both.

```go
package main

import (
	"errors"
	"fmt"
	"math/rand"
	"time"
)

const productName = "Industrial Sensor XR-500"

type InventoryResult struct {
	Supplier  string
	Product   string
	Quantity  int
	Latency   time.Duration
	Available bool
}

type SupplierResponse struct {
	Result InventoryResult
	Err    error
}

type SupplierClient struct {
	Name        string
	Latency     time.Duration
	Stock       int
	FailureRate float64
}

func NewSupplierClient(name string, latency time.Duration, stock int, failureRate float64) *SupplierClient {
	return &SupplierClient{
		Name:        name,
		Latency:     latency,
		Stock:       stock,
		FailureRate: failureRate,
	}
}

func (sc *SupplierClient) FetchInventory(product string) (InventoryResult, error) {
	start := time.Now()
	time.Sleep(sc.Latency)

	if rand.Float64() < sc.FailureRate {
		return InventoryResult{}, fmt.Errorf("supplier %s: connection timed out", sc.Name)
	}

	return InventoryResult{
		Supplier:  sc.Name,
		Product:   product,
		Quantity:  sc.Stock,
		Latency:   time.Since(start),
		Available: sc.Stock > 0,
	}, nil
}

func queryAllConcurrent(suppliers []*SupplierClient, product string) []SupplierResponse {
	responses := make(chan SupplierResponse, len(suppliers))

	for _, s := range suppliers {
		go func(client *SupplierClient) {
			result, err := client.FetchInventory(product)
			responses <- SupplierResponse{Result: result, Err: err}
		}(s)
	}

	var collected []SupplierResponse
	for i := 0; i < len(suppliers); i++ {
		collected = append(collected, <-responses)
	}
	return collected
}

func printReport(responses []SupplierResponse, wallClock time.Duration) {
	var successes []InventoryResult
	var failures []error

	for _, resp := range responses {
		if resp.Err != nil {
			failures = append(failures, resp.Err)
		} else {
			successes = append(successes, resp.Result)
		}
	}

	fmt.Println("  --- Successful Responses ---")
	if len(successes) == 0 {
		fmt.Println("  (none)")
	}
	for _, r := range successes {
		fmt.Printf("  %-24s %4d units   %7v\n",
			r.Supplier, r.Quantity, r.Latency.Round(time.Millisecond))
	}

	fmt.Println("\n  --- Failed Suppliers ---")
	if len(failures) == 0 {
		fmt.Println("  (none)")
	}
	for _, err := range failures {
		fmt.Printf("  ERROR: %v\n", err)
	}

	fmt.Printf("\n  Wall clock: %v | Success: %d/%d | Failed: %d/%d\n",
		wallClock.Round(time.Millisecond),
		len(successes), len(successes)+len(failures),
		len(failures), len(successes)+len(failures))
}

func main() {
	suppliers := []*SupplierClient{
		NewSupplierClient("Acme Industrial", 200*time.Millisecond, 42, 0.0),
		NewSupplierClient("GlobalParts Co.", 180*time.Millisecond, 15, 1.0),
		NewSupplierClient("QuickSupply Ltd.", 250*time.Millisecond, 7, 0.0),
		NewSupplierClient("TechSource Inc.", 120*time.Millisecond, 88, 1.0),
		NewSupplierClient("MegaStock Corp.", 300*time.Millisecond, 23, 0.0),
	}

	_ = errors.New // ensure errors package is used

	fmt.Println("=== Concurrent Query with Partial Failures ===")
	start := time.Now()
	responses := queryAllConcurrent(suppliers, productName)
	wallClock := time.Since(start)
	printReport(responses, wallClock)
}
```

**What's happening here:** The `SupplierResponse` wrapper carries either a result or an error. Each goroutine always sends exactly one response, so the collector knows exactly how many receives to perform. After collection, results are split into successes and failures for separate reporting.

**Key insight:** Wrapping result-or-error into a single struct is the standard pattern for concurrent calls that can fail. The channel carries one type, so you bundle success and failure into that type. This avoids needing two channels or complex synchronization.

### Intermediate Verification
```bash
go run main.go
```
Expected output (GlobalParts and TechSource always fail with FailureRate 1.0):
```
=== Concurrent Query with Partial Failures ===
  --- Successful Responses ---
  Acme Industrial            42 units     200ms
  QuickSupply Ltd.            7 units     250ms
  MegaStock Corp.            23 units     300ms

  --- Failed Suppliers ---
  ERROR: supplier TechSource Inc.: connection timed out
  ERROR: supplier GlobalParts Co.: connection timed out

  Wall clock: 300ms | Success: 3/5 | Failed: 2/5
```


## Step 4 -- Full Inventory Report with Speedup Factor

Combine everything: run both sequential and concurrent versions, compute the speedup factor, and produce a final inventory summary with total available stock.

```go
package main

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
)

const (
	productName    = "Industrial Sensor XR-500"
	reportDivider  = "  " + "─────────────────────────────────────────────────────"
)

type InventoryResult struct {
	Supplier  string
	Product   string
	Quantity  int
	Latency   time.Duration
	Available bool
}

type SupplierResponse struct {
	Result InventoryResult
	Err    error
}

type InventoryReport struct {
	Product     string
	Successes   []InventoryResult
	Failures    []error
	TotalStock  int
	WallClock   time.Duration
}

type SupplierClient struct {
	Name        string
	Latency     time.Duration
	Stock       int
	FailureRate float64
}

func NewSupplierClient(name string, latency time.Duration, stock int, failureRate float64) *SupplierClient {
	return &SupplierClient{
		Name:        name,
		Latency:     latency,
		Stock:       stock,
		FailureRate: failureRate,
	}
}

func (sc *SupplierClient) FetchInventory(product string) (InventoryResult, error) {
	start := time.Now()
	time.Sleep(sc.Latency)

	if rand.Float64() < sc.FailureRate {
		return InventoryResult{}, fmt.Errorf("%s: connection refused", sc.Name)
	}

	return InventoryResult{
		Supplier:  sc.Name,
		Product:   product,
		Quantity:  sc.Stock,
		Latency:   time.Since(start),
		Available: sc.Stock > 0,
	}, nil
}

func querySequential(suppliers []*SupplierClient, product string) []SupplierResponse {
	var responses []SupplierResponse
	for _, s := range suppliers {
		result, err := s.FetchInventory(product)
		responses = append(responses, SupplierResponse{Result: result, Err: err})
	}
	return responses
}

func queryConcurrent(suppliers []*SupplierClient, product string) []SupplierResponse {
	ch := make(chan SupplierResponse, len(suppliers))

	for _, s := range suppliers {
		go func(client *SupplierClient) {
			result, err := client.FetchInventory(product)
			ch <- SupplierResponse{Result: result, Err: err}
		}(s)
	}

	collected := make([]SupplierResponse, 0, len(suppliers))
	for i := 0; i < len(suppliers); i++ {
		collected = append(collected, <-ch)
	}
	return collected
}

func buildReport(product string, responses []SupplierResponse, wallClock time.Duration) InventoryReport {
	report := InventoryReport{
		Product:   product,
		WallClock: wallClock,
	}
	for _, resp := range responses {
		if resp.Err != nil {
			report.Failures = append(report.Failures, resp.Err)
		} else {
			report.Successes = append(report.Successes, resp.Result)
			report.TotalStock += resp.Result.Quantity
		}
	}
	return report
}

func printReport(report InventoryReport) {
	fmt.Printf("  Product: %s\n", report.Product)
	fmt.Println(reportDivider)

	for _, r := range report.Successes {
		marker := "  "
		if !r.Available {
			marker = "!!"
		}
		fmt.Printf("  %s %-22s %4d units   %v\n",
			marker, r.Supplier, r.Quantity, r.Latency.Round(time.Millisecond))
	}
	for _, err := range report.Failures {
		fmt.Printf("  XX %-22s FAILED: %v\n", strings.SplitN(err.Error(), ":", 2)[0], err)
	}

	fmt.Println(reportDivider)
	total := len(report.Successes) + len(report.Failures)
	fmt.Printf("  Responded: %d/%d | Total stock: %d | Time: %v\n",
		len(report.Successes), total, report.TotalStock,
		report.WallClock.Round(time.Millisecond))
}

func main() {
	suppliers := []*SupplierClient{
		NewSupplierClient("Acme Industrial", 200*time.Millisecond, 42, 0.0),
		NewSupplierClient("GlobalParts Co.", 180*time.Millisecond, 15, 0.8),
		NewSupplierClient("QuickSupply Ltd.", 250*time.Millisecond, 0, 0.0),
		NewSupplierClient("TechSource Inc.", 120*time.Millisecond, 88, 0.0),
		NewSupplierClient("MegaStock Corp.", 300*time.Millisecond, 23, 0.5),
	}

	fmt.Println("=== Sequential Inventory Query ===")
	seqStart := time.Now()
	seqResponses := querySequential(suppliers, productName)
	seqDuration := time.Since(seqStart)
	seqReport := buildReport(productName, seqResponses, seqDuration)
	printReport(seqReport)

	fmt.Println()

	fmt.Println("=== Concurrent Inventory Query ===")
	concStart := time.Now()
	concResponses := queryConcurrent(suppliers, productName)
	concDuration := time.Since(concStart)
	concReport := buildReport(productName, concResponses, concDuration)
	printReport(concReport)

	fmt.Println()
	fmt.Println("=== Speedup Summary ===")
	speedup := float64(seqDuration) / float64(concDuration)
	fmt.Printf("  Sequential: %v\n", seqDuration.Round(time.Millisecond))
	fmt.Printf("  Concurrent: %v\n", concDuration.Round(time.Millisecond))
	fmt.Printf("  Speedup:    %.1fx faster\n", speedup)
}
```

**What's happening here:** Both sequential and concurrent versions run against the same suppliers. The report merges successes (with stock counts) and failures (with error messages). The speedup factor shows the real-world benefit: sequential time divided by concurrent time. With five suppliers averaging 200ms each, you see roughly 3-4x speedup.

**Key insight:** The `buildReport` function is pure -- it takes responses and produces a report with no side effects. Keeping I/O (printing) separate from logic (aggregation) makes the code testable and reusable. The `SupplierResponse` pattern cleanly separates the "did it work?" decision from the "launch goroutines" logic.

### Intermediate Verification
```bash
go run main.go
```
Expected output (failures are random for GlobalParts and MegaStock):
```
=== Sequential Inventory Query ===
  Product: Industrial Sensor XR-500
  ─────────────────────────────────────────────────────
     Acme Industrial         42 units   200ms
     GlobalParts Co.         15 units   180ms
  !! QuickSupply Ltd.         0 units   250ms
     TechSource Inc.         88 units   120ms
  XX MegaStock Corp.        FAILED: MegaStock Corp.: connection refused
  ─────────────────────────────────────────────────────
  Responded: 4/5 | Total stock: 145 | Time: 1.05s

=== Concurrent Inventory Query ===
  Product: Industrial Sensor XR-500
  ─────────────────────────────────────────────────────
     TechSource Inc.         88 units   120ms
     Acme Industrial         42 units   200ms
  !! QuickSupply Ltd.         0 units   250ms
  XX GlobalParts Co.        FAILED: GlobalParts Co.: connection refused
     MegaStock Corp.         23 units   300ms
  ─────────────────────────────────────────────────────
  Responded: 4/5 | Total stock: 153 | Time: 300ms

=== Speedup Summary ===
  Sequential: 1.05s
  Concurrent: 300ms
  Speedup:    3.5x faster
```


## Common Mistakes

### Forgetting to Size the Buffered Channel

**Wrong -- complete program:**
```go
package main

import (
	"fmt"
	"time"
)

func fetchData(id int, ch chan<- string) {
	time.Sleep(50 * time.Millisecond)
	ch <- fmt.Sprintf("result-%d", id)
}

func main() {
	ch := make(chan string) // unbuffered -- goroutines block on send
	for i := 0; i < 5; i++ {
		go fetchData(i, ch)
	}
	// only reading one result -- 4 goroutines leak, blocked on send forever
	fmt.Println(<-ch)
}
```
**What happens:** Four goroutines remain blocked on `ch <-` forever. They are leaked -- the runtime cannot garbage collect them. In a long-running server, this means memory grows without bound.

**Correct -- complete program:**
```go
package main

import (
	"fmt"
	"time"
)

func fetchData(id int, ch chan<- string) {
	time.Sleep(50 * time.Millisecond)
	ch <- fmt.Sprintf("result-%d", id)
}

func main() {
	ch := make(chan string, 5) // buffered -- all goroutines can send without blocking
	for i := 0; i < 5; i++ {
		go fetchData(i, ch)
	}
	for i := 0; i < 5; i++ {
		fmt.Println(<-ch)
	}
}
```

### Swallowing Errors in Concurrent Calls

**Wrong -- complete program:**
```go
package main

import (
	"fmt"
	"time"
)

func riskyFetch(name string) string {
	time.Sleep(50 * time.Millisecond)
	if name == "bad" {
		return "" // error silently dropped
	}
	return name + ": OK"
}

func main() {
	results := make(chan string, 3)
	names := []string{"good", "bad", "fine"}
	for _, n := range names {
		go func(name string) {
			results <- riskyFetch(name)
		}(n)
	}
	for i := 0; i < 3; i++ {
		r := <-results
		if r != "" {
			fmt.Println(r)
		}
	}
	// "bad" disappeared silently -- no one knows it failed
}
```
**What happens:** The caller has no idea that "bad" failed. In production, this means you report inventory data from 4 suppliers while silently ignoring that the 5th is down.

**Correct -- use a response wrapper:**
```go
package main

import (
	"fmt"
	"time"
)

type Response struct {
	Data string
	Err  error
}

func riskyFetch(name string) Response {
	time.Sleep(50 * time.Millisecond)
	if name == "bad" {
		return Response{Err: fmt.Errorf("%s: connection refused", name)}
	}
	return Response{Data: name + ": OK"}
}

func main() {
	results := make(chan Response, 3)
	names := []string{"good", "bad", "fine"}
	for _, n := range names {
		go func(name string) {
			results <- riskyFetch(name)
		}(n)
	}
	for i := 0; i < 3; i++ {
		r := <-results
		if r.Err != nil {
			fmt.Printf("FAILED: %v\n", r.Err)
		} else {
			fmt.Println(r.Data)
		}
	}
}
```


## Verify What You Learned

Build a "multi-supplier price comparison" tool that:
1. Defines 6 suppliers, each with a simulated response time (50-400ms) and a random price for the same product
2. Queries all suppliers concurrently, collecting results through a buffered channel
3. Some suppliers (at least 2) fail randomly with connection errors
4. Prints a sorted table of prices from cheapest to most expensive
5. Prints a failure summary listing which suppliers could not be reached
6. Reports wall-clock time and the speedup factor vs sequential execution

**Hint:** Create a `PriceQuote` struct with Supplier, Price, and Latency fields. Wrap it in a `QuoteResponse` with an optional error. Use `sort.Slice` to sort by price.


## What's Next
Continue to [14-background-job-processor](../14-background-job-processor/14-background-job-processor.md) to build a background job processing system where goroutines process CSV uploads asynchronously while reporting progress through channels.


## Summary
- The fan-out pattern launches one goroutine per independent API call, reducing wall clock from the sum of latencies to the maximum single latency
- Buffered channels sized to the number of goroutines prevent send-side blocking and goroutine leaks
- The `SupplierResponse` wrapper (result + error) is the standard pattern for concurrent calls that can fail
- Always collect exactly `N` results from `N` goroutines -- mismatched counts cause deadlocks or leaks
- Separate data collection (goroutines + channels) from data presentation (report building + printing) for testability
- Speedup factor = sequential time / concurrent time; with independent calls, this approaches the number of calls


## Reference
- [Go Blog: Concurrency Patterns](https://go.dev/blog/pipelines)
- [Go Tour: Channels](https://go.dev/tour/concurrency/2)
- [Effective Go: Channels](https://go.dev/doc/effective_go#channels)
- [time.Since](https://pkg.go.dev/time#Since)
