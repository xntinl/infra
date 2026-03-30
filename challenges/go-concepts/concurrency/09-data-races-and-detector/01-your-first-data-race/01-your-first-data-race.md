---
difficulty: basic
concepts: [data race, shared variable, concurrent write, non-determinism, lost update]
tools: [go]
estimated_time: 20m
bloom_level: understand
---

# 1. Your First Data Race


## Learning Objectives
After completing this exercise, you will be able to:
- **Define** what a data race is in terms of concurrent unsynchronized memory access
- **Reproduce** a data race using multiple goroutines writing to a shared counter
- **Observe** non-deterministic behavior caused by a data race
- **Explain** the real production impact: lost analytics, wrong billing, incorrect metrics

## Why Data Races Matter

A data race occurs when three conditions are ALL true simultaneously:

1. Two or more goroutines access the same memory location
2. At least one of the accesses is a write
3. There is no synchronization between the accesses

Data races are the **number one concurrency bug** in production Go code. They are insidious because the program may appear correct most of the time, then fail unpredictably under load or on different hardware.

The Go memory model explicitly states that a data race results in **undefined behavior**. The compiler and runtime make no guarantees about the outcome.

Consider a web application that tracks page hits. Every HTTP handler increments a shared counter. Under light traffic, the counter looks correct. Under production load with hundreds of concurrent requests, increments silently disappear. Your analytics dashboard shows 50,000 daily visitors when the real number is 80,000. Your billing system undercharges because it counted fewer API calls than actually occurred. Your alerting thresholds never trigger because the error counter is perpetually low.

This is not a hypothetical scenario. It is the direct consequence of an unprotected shared counter.

## Step 1 -- Build the Hit Counter

Create a file called `main.go`. This simulates a web server where multiple HTTP handlers increment a shared hit counter concurrently. Each goroutine represents a request handler processing incoming traffic:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

const (
	defaultHandlers       = 100
	defaultReqsPerHandler = 100
	benchmarkRuns         = 5
)

// HitCounter simulates a web server's page-view tracker.
// BUG: hitCount is shared across goroutines without synchronization.
type HitCounter struct {
	handlers       int
	reqsPerHandler int
}

func NewHitCounter(handlers, reqsPerHandler int) *HitCounter {
	return &HitCounter{
		handlers:       handlers,
		reqsPerHandler: reqsPerHandler,
	}
}

// CountHits launches concurrent handlers that all increment the same variable.
// DATA RACE: the read-modify-write on hitCount has no synchronization.
func (hc *HitCounter) CountHits() int {
	hitCount := 0
	var wg sync.WaitGroup

	for handler := 0; handler < hc.handlers; handler++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for req := 0; req < hc.reqsPerHandler; req++ {
				hitCount++ // DATA RACE: read-modify-write without synchronization
			}
		}()
	}

	wg.Wait()
	return hitCount
}

func (hc *HitCounter) Expected() int {
	return hc.handlers * hc.reqsPerHandler
}

func runBenchmark(counter *HitCounter) {
	expected := counter.Expected()
	results := make([]int, benchmarkRuns)

	for run := 0; run < benchmarkRuns; run++ {
		start := time.Now()
		actual := counter.CountHits()
		elapsed := time.Since(start)
		lost := expected - actual
		results[run] = actual
		fmt.Printf("Run %d: %d hits recorded, %d lost (%v)\n",
			run+1, actual, lost, elapsed)
	}

	fmt.Println()
	printProductionImpact(results)
}

func printProductionImpact(results []int) {
	fmt.Println("--- Production Impact ---")
	fmt.Printf("Results across %d runs: %v\n", len(results), results)
	fmt.Println("Every run produces a different number. None reach 10000.")
	fmt.Println()
	fmt.Println("If this were real:")
	fmt.Println("  - Analytics: dashboard shows 6000 visitors instead of 10000")
	fmt.Println("  - Billing:   customer charged for 7000 API calls instead of 10000")
	fmt.Println("  - Alerting:  error counter shows 50 errors instead of 80, threshold never triggers")
	fmt.Println("  - Capacity:  load balancer thinks server handles fewer requests than it does")
}

func main() {
	fmt.Println("=== Web Hit Counter Data Race ===")
	fmt.Println("Expected: 10000 hits (100 handlers x 100 requests each)")
	fmt.Println()

	counter := NewHitCounter(defaultHandlers, defaultReqsPerHandler)
	runBenchmark(counter)
}
```

## Step 2 -- Run and Observe

### Verification
```bash
go run main.go
```

Sample output (your numbers WILL differ):
```
=== Web Hit Counter Data Race ===
Expected: 10000 hits (100 handlers x 100 requests each)

Run 1: 6482 hits recorded, 3518 lost (1.2ms)
Run 2: 7201 hits recorded, 2799 lost (1.1ms)
Run 3: 5893 hits recorded, 4107 lost (1.3ms)
Run 4: 6819 hits recorded, 3181 lost (1.1ms)
Run 5: 7044 hits recorded, 2956 lost (1.2ms)

--- Production Impact ---
Results across 5 runs: [6482 7201 5893 6819 7044]
Every run produces a different number. None reach 10000.
```

Run it several times. Each execution produces different results. This non-determinism is the unmistakable signature of a data race.

## Step 3 -- Understand Why Increments Are Lost

The operation `hitCount++` is **not atomic**. It consists of three CPU-level steps:
1. **READ** the current value of hitCount from memory
2. **ADD** one to the value
3. **WRITE** the new value back to memory

When two request handlers execute simultaneously:

```
Time    Handler A              Handler B              hitCount (memory)
----    ---------              ---------              -----------------
 1      READ hitCount (= 42)                          42
 2                             READ hitCount (= 42)   42
 3      WRITE hitCount (= 43)                         43
 4                             WRITE hitCount (= 43)  43  <-- increment LOST!
```

Both handlers read 42, both compute 43, both write 43. Two requests were processed, but the counter only went up by one. This is called a **lost update**. With 100 goroutines competing, thousands of increments vanish per second.

## Step 4 -- Measure How Bad It Gets

Add this function to see the relationship between concurrency level and data loss:

```go
package main

import (
	"fmt"
	"sync"
)

// TrafficScenario describes a concurrency level for benchmarking data loss.
type TrafficScenario struct {
	Handlers       int
	ReqsPerHandler int
	Label          string
}

// HitCounter simulates a web server's page-view tracker.
// BUG: hitCount is shared across goroutines without synchronization.
type HitCounter struct {
	handlers       int
	reqsPerHandler int
}

func NewHitCounter(handlers, reqsPerHandler int) *HitCounter {
	return &HitCounter{
		handlers:       handlers,
		reqsPerHandler: reqsPerHandler,
	}
}

func (hc *HitCounter) Expected() int {
	return hc.handlers * hc.reqsPerHandler
}

// CountHits launches concurrent handlers that all increment the same variable.
// DATA RACE: the read-modify-write on hitCount has no synchronization.
func (hc *HitCounter) CountHits() int {
	hitCount := 0
	var wg sync.WaitGroup

	for h := 0; h < hc.handlers; h++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := 0; r < hc.reqsPerHandler; r++ {
				hitCount++
			}
		}()
	}

	wg.Wait()
	return hitCount
}

func measureLoss(scenario TrafficScenario) {
	counter := NewHitCounter(scenario.Handlers, scenario.ReqsPerHandler)
	expected := counter.Expected()
	actual := counter.CountHits()
	lossPercent := float64(expected-actual) / float64(expected) * 100
	fmt.Printf("%-35s expected=%d actual=%d lost=%.1f%%\n",
		scenario.Label, expected, actual, lossPercent)
}

func main() {
	fmt.Println("=== Data Loss vs Concurrency Level ===")
	fmt.Println()

	scenarios := []TrafficScenario{
		{10, 1000, "Light traffic (10 handlers)"},
		{50, 1000, "Moderate traffic (50 handlers)"},
		{200, 1000, "Heavy traffic (200 handlers)"},
		{500, 1000, "Peak traffic (500 handlers)"},
	}

	for _, s := range scenarios {
		measureLoss(s)
	}

	fmt.Println()
	fmt.Println("More concurrency = more lost updates = worse data corruption")
}
```

### Verification
```bash
go run main.go
```

More goroutines means more contention, which means more lost updates. Under peak traffic (when accuracy matters most), the data is least reliable.

## Common Mistakes

### Thinking "It Worked Once, So It's Fine"
A data race may produce the correct result on some runs, especially on single-core machines or with few goroutines. The absence of symptoms does NOT prove the absence of the bug. Data races are undefined behavior: they must be eliminated, not tolerated.

### Assuming Small Operations Are Atomic
Even `hitCount++` (or `hitCount += 1`) is NOT atomic in Go. It compiles to multiple machine instructions. Only operations from the `sync/atomic` package are guaranteed to be atomic (see exercise 05).

### Using time.Sleep as Synchronization
Sleeping does not synchronize memory. Even if you sleep "long enough," the compiler and CPU may reorder memory operations. Only proper synchronization primitives (`sync.Mutex`, channels, `sync/atomic`) establish happens-before relationships.

## Verify What You Learned

Answer these questions:
1. What three conditions must be true for a data race to exist?
2. Why does `hitCount++` produce wrong results when called from multiple goroutines?
3. If you run the program and get 10000, does that prove there is no race? Why or why not?
4. Why does data loss get worse as traffic increases, which is exactly when accuracy matters most?

## What's Next
Continue to [02-race-detector-flag](../02-race-detector-flag/02-race-detector-flag.md) to learn how Go's built-in race detector can automatically find this bug.

## Summary
- A data race occurs when two or more goroutines access the same variable concurrently, at least one access is a write, and there is no synchronization
- `counter++` is not atomic: it is read-modify-write, and concurrent execution causes **lost updates**
- Data race results are non-deterministic: the program produces different results on different runs
- Correct output on one run does NOT prove the absence of a data race
- In production, data races cause wrong analytics, incorrect billing, missed alerts, and flawed capacity planning
- The problem gets worse under heavy load, which is exactly when you need accuracy the most

## Reference
- [Go Memory Model](https://go.dev/ref/mem)
- [Go Blog: Introducing the Go Race Detector](https://go.dev/blog/race-detector)
- [Go Spec: Statements](https://go.dev/ref/spec#Statements)
