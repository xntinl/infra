---
difficulty: intermediate
concepts: [concurrent result collection, mutex-protected slice, index-based pattern, parallel database queries]
tools: [go]
estimated_time: 30m
bloom_level: analyze
---

# 4. Errgroup Collect Results -- Parallel Database Queries

## Learning Objectives
After completing this exercise, you will be able to:
- **Build** a parallel database query system that runs 5 queries concurrently and collects results
- **Apply** the index-based pattern (pre-allocated slice, one slot per goroutine) for lock-free collection
- **Apply** the mutex-protected pattern for collecting heterogeneous or filtered results
- **Detect** data races with `go run -race` and understand why naive `append` is unsafe
- **Choose** the right collection pattern based on whether results map to known indices

## Why Result Collection Matters

Your analytics dashboard needs data from 5 different database queries: total revenue, active users, conversion rate, top products, and error rates. Each query takes 100-300ms. Running them sequentially means 500-1500ms page load. Running them in parallel means ~300ms (the slowest query).

The problem: goroutines produce results, but the errgroup pattern only returns an error from `Wait()`. There is no return value for data. You need a pattern for goroutines to write their results to a shared data structure without data races.

Two patterns exist:
1. **Index-based**: Pre-allocate a slice of known size. Goroutine `i` writes to `results[i]`. No mutex needed because each goroutine touches a different memory location.
2. **Mutex-protected**: When results are filtered, combined, or keyed by non-sequential identifiers, protect the shared structure with `sync.Mutex`.

## Step 1 -- The Data Race (What Goes Wrong Without Protection)

Naive `append` from multiple goroutines is a data race. Run with `-race` to see it:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type QueryResult struct {
	Name  string
	Value string
}

func main() {
	fmt.Println("=== Unsafe Collection (data race) ===")
	fmt.Println("Run with: go run -race main.go")

	var wg sync.WaitGroup
	var results []QueryResult // shared, unprotected

	queries := []string{"revenue", "active-users", "conversion", "top-products", "error-rate"}

	for _, q := range queries {
		q := q
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(50 * time.Millisecond)
			// DATA RACE: multiple goroutines call append concurrently.
			// append modifies the slice header (length, capacity) and may
			// reallocate the backing array. This corrupts the slice.
			results = append(results, QueryResult{Name: q, Value: "some-data"})
		}()
	}

	wg.Wait()
	fmt.Printf("Got %d results (may be wrong or corrupted due to race)\n", len(results))
}
```

**Expected output (with -race flag):**
```
=== Unsafe Collection (data race) ===
Run with: go run -race main.go
WARNING: DATA RACE
...
Got N results (may be wrong or corrupted due to race)
```

The race detector catches this immediately. In production without `-race`, the corruption is silent: you might get 3 results instead of 5, or garbage data. This class of bug is notoriously hard to reproduce because it depends on goroutine scheduling.

## Step 2 -- Index-Based Collection (No Mutex Needed)

When you know the number of results upfront and each goroutine maps to a fixed index, pre-allocate the slice. Each goroutine writes to its own slot:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type QueryResult struct {
	Name    string
	Value   float64
	Latency time.Duration
}

func main() {
	fmt.Println("=== Index-Based Collection (no mutex) ===")
	start := time.Now()

	queries := []struct {
		name    string
		latency time.Duration
		value   float64
	}{
		{"total-revenue", 150 * time.Millisecond, 1_247_893.50},
		{"active-users", 80 * time.Millisecond, 42_381},
		{"conversion-rate", 120 * time.Millisecond, 3.7},
		{"top-products", 200 * time.Millisecond, 15},
		{"error-rate", 60 * time.Millisecond, 0.02},
	}

	results := make([]QueryResult, len(queries)) // pre-allocate exact size

	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error

	for i, q := range queries {
		i, q := i, q
		wg.Add(1)
		go func() {
			defer wg.Done()

			time.Sleep(q.latency) // simulate database query

			// SAFE: each goroutine writes to a unique index.
			// Different indices are different memory locations.
			// The slice header (length, capacity, pointer) is never modified.
			results[i] = QueryResult{
				Name:    q.name,
				Value:   q.value,
				Latency: q.latency,
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start).Round(time.Millisecond)

	if firstErr != nil {
		fmt.Printf("Error: %v\n", firstErr)
	}
	_ = firstErr

	fmt.Printf("\nDashboard Data (loaded in %v):\n", elapsed)
	for _, r := range results {
		fmt.Printf("  %-20s = %12.2f  (query took %v)\n", r.Name, r.Value, r.Latency)
	}
}
```

**Expected output:**
```
=== Index-Based Collection (no mutex) ===

Dashboard Data (loaded in 200ms):
  total-revenue        =  1247893.50  (query took 150ms)
  active-users         =    42381.00  (query took 80ms)
  conversion-rate      =        3.70  (query took 120ms)
  top-products         =       15.00  (query took 200ms)
  error-rate           =        0.02  (query took 60ms)
```

All 5 queries ran in parallel. Total time is ~200ms (the slowest query), not ~610ms (sum). Results are ordered by index, matching the input order, regardless of which query finished first.

Run with `go run -race main.go` to confirm: no data race warnings.

## Step 3 -- Mutex-Protected Collection (Heterogeneous Results)

When results do not map to predictable indices -- for example, only some queries produce results, or you are aggregating data from varying sources -- use a mutex:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type Metric struct {
	Source string
	Name   string
	Value  float64
}

func main() {
	fmt.Println("=== Mutex-Protected Collection ===")
	start := time.Now()

	// Simulate querying multiple data sources where each source
	// may return zero, one, or many metrics
	sources := []struct {
		name    string
		latency time.Duration
		metrics []Metric
	}{
		{"postgres", 100 * time.Millisecond, []Metric{
			{Source: "postgres", Name: "total-revenue", Value: 1_247_893.50},
			{Source: "postgres", Name: "order-count", Value: 8_429},
		}},
		{"redis", 30 * time.Millisecond, []Metric{
			{Source: "redis", Name: "cache-hit-rate", Value: 94.7},
		}},
		{"prometheus", 150 * time.Millisecond, []Metric{
			{Source: "prometheus", Name: "p99-latency-ms", Value: 247},
			{Source: "prometheus", Name: "error-rate", Value: 0.02},
			{Source: "prometheus", Name: "qps", Value: 12_450},
		}},
		{"empty-source", 50 * time.Millisecond, nil}, // returns nothing
	}

	var mu sync.Mutex
	var allMetrics []Metric

	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error

	for _, src := range sources {
		src := src
		wg.Add(1)
		go func() {
			defer wg.Done()

			time.Sleep(src.latency)

			if src.metrics == nil {
				return // this source had no data -- nothing to collect
			}

			mu.Lock()
			allMetrics = append(allMetrics, src.metrics...)
			mu.Unlock()
		}()
	}

	wg.Wait()
	elapsed := time.Since(start).Round(time.Millisecond)
	_ = once
	_ = firstErr

	fmt.Printf("Collected %d metrics from %d sources in %v:\n", len(allMetrics), len(sources), elapsed)
	for _, m := range allMetrics {
		fmt.Printf("  [%-12s] %-20s = %.2f\n", m.Source, m.Name, m.Value)
	}
}
```

**Expected output:**
```
=== Mutex-Protected Collection ===
Collected 6 metrics from 4 sources in 150ms:
  [redis       ] cache-hit-rate       = 94.70
  [postgres    ] total-revenue        = 1247893.50
  [postgres    ] order-count          = 8429.00
  [prometheus  ] p99-latency-ms       = 247.00
  [prometheus  ] error-rate           = 0.02
  [prometheus  ] qps                  = 12450.00
```

The order depends on goroutine scheduling (redis finishes first at 30ms, then postgres at 100ms, then prometheus at 150ms). The `empty-source` contributed nothing. The mutex protects `append` because multiple goroutines modify the slice header.

## Step 4 -- Collecting Partial Results on Error

When some queries fail but you still want results from the ones that succeeded, combine index-based collection with context cancellation:

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type DashboardQuery struct {
	Name    string
	Latency time.Duration
	Fail    bool
}

type DashboardResult struct {
	Name  string
	Value string
	OK    bool
}

func main() {
	fmt.Println("=== Partial Results on Error ===")
	start := time.Now()

	queries := []DashboardQuery{
		{"revenue", 80 * time.Millisecond, false},
		{"active-users", 50 * time.Millisecond, false},
		{"conversion", 120 * time.Millisecond, true}, // THIS QUERY FAILS
		{"top-products", 200 * time.Millisecond, false},
		{"error-rate", 40 * time.Millisecond, false},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	results := make([]DashboardResult, len(queries))

	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error

	for i, q := range queries {
		i, q := i, q
		wg.Add(1)
		go func() {
			defer wg.Done()

			select {
			case <-ctx.Done():
				results[i] = DashboardResult{Name: q.Name, OK: false}
				return
			case <-time.After(q.Latency):
			}

			if q.Fail {
				once.Do(func() {
					firstErr = fmt.Errorf("query %q: connection timeout after %v", q.Name, q.Latency)
					cancel()
				})
				results[i] = DashboardResult{Name: q.Name, OK: false}
				return
			}

			results[i] = DashboardResult{
				Name:  q.Name,
				Value: fmt.Sprintf("data-for-%s", q.Name),
				OK:    true,
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start).Round(time.Millisecond)

	fmt.Printf("\nQuery results (in %v):\n", elapsed)
	succeeded := 0
	for _, r := range results {
		status := "FAIL"
		if r.OK {
			status = " OK "
			succeeded++
		}
		fmt.Printf("  [%s] %-15s %s\n", status, r.Name, r.Value)
	}
	fmt.Printf("\nSucceeded: %d/%d\n", succeeded, len(queries))
	if firstErr != nil {
		fmt.Printf("Error: %v\n", firstErr)
	}
}
```

**Expected output:**
```
=== Partial Results on Error ===

Query results (in 120ms):
  [ OK ] error-rate      data-for-error-rate
  [ OK ] active-users    data-for-active-users
  [FAIL] conversion
  [ OK ] revenue         data-for-revenue
  [FAIL] top-products

Succeeded: 3/5
Error: query "conversion": connection timeout after 120ms
```

Queries that completed before the failure (error-rate at 40ms, active-users at 50ms, revenue at 80ms) have their results. Conversion failed at 120ms, triggering cancellation. Top-products (200ms) was cancelled before it could complete. The dashboard can render a partial view with the 3 successful queries and show a "data unavailable" message for the other 2.

## Step 5 -- Map-Based Collection for Named Results

When query results are keyed by string identifiers rather than sequential indices, use a mutex-protected map:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	fmt.Println("=== Map-Based Collection ===")
	start := time.Now()

	type RegionStats struct {
		Region   string
		Revenue  float64
		Orders   int
		AvgValue float64
	}

	regions := []string{"us-east", "us-west", "eu-central", "ap-southeast"}

	var mu sync.Mutex
	regionData := make(map[string]RegionStats)

	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error

	for _, region := range regions {
		region := region
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Simulate querying region-specific database shards
			time.Sleep(80 * time.Millisecond)

			stats := RegionStats{Region: region}
			switch region {
			case "us-east":
				stats.Revenue, stats.Orders = 523_400, 4_200
			case "us-west":
				stats.Revenue, stats.Orders = 312_100, 2_800
			case "eu-central":
				stats.Revenue, stats.Orders = 445_700, 3_600
			case "ap-southeast":
				stats.Revenue, stats.Orders = 198_300, 1_900
			}
			if stats.Orders > 0 {
				stats.AvgValue = stats.Revenue / float64(stats.Orders)
			}

			mu.Lock()
			regionData[region] = stats
			mu.Unlock()
		}()
	}

	wg.Wait()
	elapsed := time.Since(start).Round(time.Millisecond)
	_ = once
	_ = firstErr

	fmt.Printf("Regional breakdown (loaded in %v):\n", elapsed)
	var totalRevenue float64
	var totalOrders int
	for _, region := range regions { // iterate in defined order
		s := regionData[region]
		fmt.Printf("  %-15s  revenue=$%10.2f  orders=%5d  avg=$%.2f\n",
			s.Region, s.Revenue, s.Orders, s.AvgValue)
		totalRevenue += s.Revenue
		totalOrders += s.Orders
	}
	fmt.Printf("  %-15s  revenue=$%10.2f  orders=%5d\n", "TOTAL", totalRevenue, totalOrders)
}
```

**Expected output:**
```
=== Map-Based Collection ===
Regional breakdown (loaded in 80ms):
  us-east          revenue=$ 523400.00  orders= 4200  avg=$124.62
  us-west          revenue=$ 312100.00  orders= 2800  avg=$111.46
  eu-central       revenue=$ 445700.00  orders= 3600  avg=$123.81
  ap-southeast     revenue=$ 198300.00  orders= 1900  avg=$104.37
  TOTAL            revenue=$1479500.00  orders=12500
```

The map requires a mutex because concurrent writes to a Go map are a race condition. Iterating the map after `wg.Wait()` is safe because all writes are complete (the Wait establishes a happens-before relationship).

## Intermediate Verification

At this point, verify:
1. `go run -race main.go` catches the unsafe append in Step 1
2. Index-based collection produces ordered results with no race
3. Mutex-protected collection handles variable-length results from multiple sources
4. Partial results show empty slots for failed/cancelled queries
5. Map-based collection works for string-keyed results

## Common Mistakes

### Reading results before Wait returns

**Wrong:**
```go
for i, q := range queries {
    wg.Add(1)
    go func() {
        defer wg.Done()
        results[i] = runQuery(q)
    }()
}
fmt.Println(results) // DATA RACE: goroutines may still be writing
wg.Wait()
```

**What happens:** You read the results slice while goroutines are still writing to it. The race detector catches this, but in production without `-race`, you get intermittent wrong data.

**Fix:** Always read results AFTER `wg.Wait()` returns. The `Wait()` call establishes a happens-before relationship -- all goroutine writes are visible after Wait.

### Using index-based pattern with a zero-length slice

**Wrong:**
```go
results := make([]QueryResult, 0) // length 0, capacity 0
results[i] = value // PANIC: index out of range
```

**Fix:** Pre-allocate with `make([]QueryResult, len(queries))`.

### Holding the mutex too long

**Wrong:**
```go
mu.Lock()
result := expensiveQuery() // holds the lock for 200ms
results = append(results, result)
mu.Unlock()
```

**What happens:** Other goroutines block on `mu.Lock()` for the entire query duration. Concurrency is effectively serialized.

**Fix:** Do the work outside the lock, only lock for the append:
```go
result := expensiveQuery() // no lock held
mu.Lock()
results = append(results, result)
mu.Unlock()
```

## Verify What You Learned

Run the full program and confirm:
1. The race detector catches unsafe append: `go run -race main.go`
2. Index-based collection produces ordered results without any mutex
3. Mutex-protected collection handles variable-count results from multiple sources
4. Partial results show which queries succeeded and which failed/were cancelled
5. Map-based collection works for string-keyed results

```bash
go run main.go
```

## What's Next
Continue to [05-errgroup-vs-waitgroup](../05-errgroup-vs-waitgroup/05-errgroup-vs-waitgroup.md) to understand when to use errgroup versus WaitGroup -- they are not interchangeable.

## Summary
- `Go()` returns only `error` -- there is no built-in mechanism to return data from goroutines
- **Index-based**: pre-allocate `results[len(tasks)]`, each goroutine writes to `results[i]` -- no mutex, preserves order
- **Mutex-protected**: protect `append` with `sync.Mutex` when output count is unknown or varies
- **Map-based**: protect a shared map with `sync.Mutex` when results are keyed by non-sequential identifiers
- Writing to distinct slice indices from different goroutines is safe (different memory locations)
- Always read results AFTER `Wait()` returns -- never before
- For partial results on error, use context cancellation and check for zero-value slots after Wait
- Hold the mutex for the minimum time possible -- do expensive work outside the critical section

## Reference
- [Go Memory Model: happens-before](https://go.dev/ref/mem)
- [Go Race Detector](https://go.dev/doc/articles/race_detector)
- [sync.Mutex documentation](https://pkg.go.dev/sync#Mutex)
- [errgroup package documentation](https://pkg.go.dev/golang.org/x/sync/errgroup)
