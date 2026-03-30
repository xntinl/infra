---
difficulty: intermediate
concepts: [scatter-gather, partial results, deadline-based collection, graceful degradation, concurrent API aggregation]
tools: [go]
estimated_time: 35m
bloom_level: apply
prerequisites: [goroutines, channels, time.After]
---


# 27. Scatter-Gather with Partial Results


## Learning Objectives
After completing this exercise, you will be able to:
- **Implement** the scatter-gather pattern to launch concurrent requests and collect results within a deadline
- **Design** APIs that return partial results instead of failing when some backends are slow or unavailable
- **Classify** individual request outcomes as success, error, or timeout for observability
- **Apply** deadline-based collection using `time.After` and `select` to enforce global time budgets


## Why Scatter-Gather with Partial Results

A Backend for Frontend (BFF) typically aggregates data from multiple microservices to build a single response. A user profile page might need data from the profile service, orders service, recommendations engine, notifications service, and reviews service. If the BFF waits for all five before responding, the slowest service determines the page load time. If any single service fails, the entire page fails.

Production BFFs use scatter-gather with partial results: launch all requests concurrently, wait up to a deadline (e.g., 500ms), return whatever has arrived by then. The profile page renders with recommendations missing rather than showing an error page. The frontend knows which sections are available and renders a skeleton for the rest.

This is fundamentally different from fan-out/fan-in where all results are required. The key design decision is that partial data is better than no data. Every major API aggregation layer -- Netflix Zuul, Shopify's storefront API, Airbnb's presentation layer -- uses this pattern. The challenge is not the concurrency itself but the classification of results (success/error/timeout) and the contract with the caller about what they will receive.


## Step 1 -- Define the Service Contract

Define the types for service calls, results, and the classification of outcomes. Use an enum for result status so callers can programmatically handle each case.

```go
package main

import (
	"fmt"
	"time"
)

type ResultStatus int

const (
	StatusSuccess ResultStatus = iota
	StatusError
	StatusTimeout
)

func (s ResultStatus) String() string {
	switch s {
	case StatusSuccess:
		return "success"
	case StatusError:
		return "error"
	case StatusTimeout:
		return "timeout"
	default:
		return "unknown"
	}
}

type ServiceResult struct {
	ServiceName string
	Status      ResultStatus
	Data        any
	Error       string
	Duration    time.Duration
}

type ServiceCall struct {
	Name string
	Fn   func() (any, error)
}

func main() {
	results := []ServiceResult{
		{ServiceName: "profile", Status: StatusSuccess, Data: "Alice", Duration: 50 * time.Millisecond},
		{ServiceName: "orders", Status: StatusSuccess, Data: "3 orders", Duration: 120 * time.Millisecond},
		{ServiceName: "recommendations", Status: StatusTimeout, Duration: 500 * time.Millisecond},
		{ServiceName: "notifications", Status: StatusError, Error: "503 Service Unavailable", Duration: 30 * time.Millisecond},
		{ServiceName: "reviews", Status: StatusSuccess, Data: "4.5 stars", Duration: 200 * time.Millisecond},
	}

	fmt.Println("=== Result Classification ===")
	for _, r := range results {
		switch r.Status {
		case StatusSuccess:
			fmt.Printf("  %-20s [%s] data=%v (%v)\n", r.ServiceName, r.Status, r.Data, r.Duration)
		case StatusError:
			fmt.Printf("  %-20s [%s] err=%s (%v)\n", r.ServiceName, r.Status, r.Error, r.Duration)
		case StatusTimeout:
			fmt.Printf("  %-20s [%s] exceeded deadline (%v)\n", r.ServiceName, r.Status, r.Duration)
		}
	}

	successes := 0
	for _, r := range results {
		if r.Status == StatusSuccess {
			successes++
		}
	}
	fmt.Printf("\n  Available: %d/%d services\n", successes, len(results))
}
```

**What's happening here:** The three-state classification (`StatusSuccess`, `StatusError`, `StatusTimeout`) captures every possible outcome. A timeout is different from an error: timeouts mean the service might be healthy but slow, while errors mean the service responded with a failure. This distinction matters for monitoring -- a spike in timeouts suggests latency issues, while errors suggest service failures.

**Key insight:** The `ServiceCall` struct decouples the scatter-gather engine from specific service implementations. The engine receives a list of functions to call; it does not know or care what protocol they use. This makes the pattern reusable across any aggregation scenario.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Result Classification ===
  profile              [success] data=Alice (50ms)
  orders               [success] data=3 orders (120ms)
  recommendations      [timeout] exceeded deadline (500ms)
  notifications        [error] err=503 Service Unavailable (30ms)
  reviews              [success] data=4.5 stars (200ms)

  Available: 3/5 services
```


## Step 2 -- Scatter-Gather Engine

Build the core engine: launch all service calls as goroutines, collect results through a channel, enforce a global deadline with `time.After`. Results that arrive after the deadline are classified as timeouts.

```go
package main

import (
	"errors"
	"fmt"
	"time"
)

type ResultStatus int

const (
	StatusSuccess ResultStatus = iota
	StatusError
	StatusTimeout
)

func (s ResultStatus) String() string {
	switch s {
	case StatusSuccess:
		return "success"
	case StatusError:
		return "error"
	case StatusTimeout:
		return "timeout"
	default:
		return "unknown"
	}
}

type ServiceResult struct {
	ServiceName string
	Status      ResultStatus
	Data        any
	Error       string
	Duration    time.Duration
}

type ServiceCall struct {
	Name string
	Fn   func() (any, error)
}

type ScatterGatherResult struct {
	Results  []ServiceResult
	Elapsed  time.Duration
	Gathered int
	TimedOut int
	Failed   int
}

func ScatterGather(calls []ServiceCall, deadline time.Duration) ScatterGatherResult {
	resultCh := make(chan ServiceResult, len(calls))
	start := time.Now()

	for _, call := range calls {
		go func(c ServiceCall) {
			callStart := time.Now()
			data, err := c.Fn()
			duration := time.Since(callStart)

			if err != nil {
				resultCh <- ServiceResult{
					ServiceName: c.Name,
					Status:      StatusError,
					Error:       err.Error(),
					Duration:    duration,
				}
				return
			}
			resultCh <- ServiceResult{
				ServiceName: c.Name,
				Status:      StatusSuccess,
				Data:        data,
				Duration:    duration,
			}
		}(call)
	}

	timer := time.After(deadline)
	collected := make(map[string]ServiceResult, len(calls))

	for len(collected) < len(calls) {
		select {
		case result := <-resultCh:
			collected[result.ServiceName] = result
		case <-timer:
			for _, call := range calls {
				if _, ok := collected[call.Name]; !ok {
					collected[call.Name] = ServiceResult{
						ServiceName: call.Name,
						Status:      StatusTimeout,
						Duration:    time.Since(start),
					}
				}
			}
		}
	}

	sgResult := ScatterGatherResult{
		Results: make([]ServiceResult, 0, len(calls)),
		Elapsed: time.Since(start),
	}
	for _, call := range calls {
		r := collected[call.Name]
		sgResult.Results = append(sgResult.Results, r)
		switch r.Status {
		case StatusSuccess:
			sgResult.Gathered++
		case StatusTimeout:
			sgResult.TimedOut++
		case StatusError:
			sgResult.Failed++
		}
	}
	return sgResult
}

func main() {
	calls := []ServiceCall{
		{Name: "profile", Fn: func() (any, error) {
			time.Sleep(50 * time.Millisecond)
			return map[string]string{"name": "Alice", "email": "alice@example.com"}, nil
		}},
		{Name: "orders", Fn: func() (any, error) {
			time.Sleep(120 * time.Millisecond)
			return "3 recent orders", nil
		}},
		{Name: "recommendations", Fn: func() (any, error) {
			time.Sleep(800 * time.Millisecond)
			return "5 items", nil
		}},
		{Name: "notifications", Fn: func() (any, error) {
			time.Sleep(30 * time.Millisecond)
			return nil, errors.New("503 Service Unavailable")
		}},
		{Name: "reviews", Fn: func() (any, error) {
			time.Sleep(200 * time.Millisecond)
			return "4.5 stars (128 reviews)", nil
		}},
	}

	fmt.Println("=== Scatter-Gather (500ms deadline) ===")
	result := ScatterGather(calls, 500*time.Millisecond)

	for _, r := range result.Results {
		switch r.Status {
		case StatusSuccess:
			fmt.Printf("  %-20s [%s] %v (%v)\n", r.ServiceName, r.Status, r.Data, r.Duration.Round(time.Millisecond))
		case StatusError:
			fmt.Printf("  %-20s [%s] %s (%v)\n", r.ServiceName, r.Status, r.Error, r.Duration.Round(time.Millisecond))
		case StatusTimeout:
			fmt.Printf("  %-20s [%s] deadline exceeded (%v)\n", r.ServiceName, r.Status, r.Duration.Round(time.Millisecond))
		}
	}

	fmt.Printf("\n  Total: %v | Gathered: %d | Failed: %d | Timed out: %d\n",
		result.Elapsed.Round(time.Millisecond), result.Gathered, result.Failed, result.TimedOut)
}
```

**What's happening here:** `ScatterGather` launches one goroutine per service call. Each goroutine sends its result on a buffered channel. The collector loop reads from the channel until all results are in or the deadline fires. When the timer fires, any uncollected services are immediately classified as timeouts, and the loop exits because `collected` is now full.

**Key insight:** The channel is buffered with `len(calls)` capacity. This is critical: goroutines that complete after the deadline can still send their result without blocking forever. Without buffering, those goroutines would be permanently blocked on the channel send, leaking goroutines. The buffered channel lets them complete and be garbage collected even though nobody reads their result.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Scatter-Gather (500ms deadline) ===
  profile              [success] map[email:alice@example.com name:Alice] (50ms)
  orders               [success] 3 recent orders (120ms)
  recommendations      [timeout] deadline exceeded (500ms)
  notifications        [error] 503 Service Unavailable (30ms)
  reviews              [success] 4.5 stars (128 reviews) (200ms)

  Total: 500ms | Gathered: 3 | Failed: 1 | Timed out: 1
```


## Step 3 -- Full BFF Simulation with Multiple Requests

Simulate a realistic BFF handling multiple user requests concurrently. Each request scatter-gathers from the same 5 services, but response times vary per request. Track aggregate statistics across all requests.

```go
package main

import (
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

type ResultStatus int

const (
	StatusSuccess ResultStatus = iota
	StatusError
	StatusTimeout
)

func (s ResultStatus) String() string {
	switch s {
	case StatusSuccess:
		return "success"
	case StatusError:
		return "error"
	case StatusTimeout:
		return "timeout"
	default:
		return "unknown"
	}
}

const (
	globalDeadline     = 300 * time.Millisecond
	concurrentRequests = 8
)

type ServiceResult struct {
	ServiceName string
	Status      ResultStatus
	Data        any
	Error       string
	Duration    time.Duration
}

type ServiceCall struct {
	Name string
	Fn   func() (any, error)
}

type ScatterGatherResult struct {
	Results  []ServiceResult
	Elapsed  time.Duration
	Gathered int
	TimedOut int
	Failed   int
}

func ScatterGather(calls []ServiceCall, deadline time.Duration) ScatterGatherResult {
	resultCh := make(chan ServiceResult, len(calls))
	start := time.Now()

	for _, call := range calls {
		go func(c ServiceCall) {
			callStart := time.Now()
			data, err := c.Fn()
			duration := time.Since(callStart)

			if err != nil {
				resultCh <- ServiceResult{
					ServiceName: c.Name, Status: StatusError,
					Error: err.Error(), Duration: duration,
				}
				return
			}
			resultCh <- ServiceResult{
				ServiceName: c.Name, Status: StatusSuccess,
				Data: data, Duration: duration,
			}
		}(call)
	}

	timer := time.After(deadline)
	collected := make(map[string]ServiceResult, len(calls))

	for len(collected) < len(calls) {
		select {
		case result := <-resultCh:
			collected[result.ServiceName] = result
		case <-timer:
			for _, call := range calls {
				if _, ok := collected[call.Name]; !ok {
					collected[call.Name] = ServiceResult{
						ServiceName: call.Name, Status: StatusTimeout,
						Duration: time.Since(start),
					}
				}
			}
		}
	}

	sgResult := ScatterGatherResult{
		Results: make([]ServiceResult, 0, len(calls)),
		Elapsed: time.Since(start),
	}
	for _, call := range calls {
		r := collected[call.Name]
		sgResult.Results = append(sgResult.Results, r)
		switch r.Status {
		case StatusSuccess:
			sgResult.Gathered++
		case StatusTimeout:
			sgResult.TimedOut++
		case StatusError:
			sgResult.Failed++
		}
	}
	return sgResult
}

type AggregateStats struct {
	mu            sync.Mutex
	TotalRequests int
	ByService     map[string][3]int
}

func NewAggregateStats() *AggregateStats {
	return &AggregateStats{
		ByService: make(map[string][3]int),
	}
}

func (a *AggregateStats) Record(result ScatterGatherResult) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.TotalRequests++
	for _, r := range result.Results {
		counts := a.ByService[r.ServiceName]
		counts[r.Status]++
		a.ByService[r.ServiceName] = counts
	}
}

func makeServiceCalls(rng *rand.Rand) []ServiceCall {
	type svcConfig struct {
		name     string
		baseMs   int
		jitterMs int
		errorPct float64
	}

	configs := []svcConfig{
		{"profile", 30, 40, 0.05},
		{"orders", 80, 100, 0.10},
		{"recommendations", 150, 300, 0.08},
		{"notifications", 20, 30, 0.15},
		{"reviews", 100, 200, 0.10},
	}

	calls := make([]ServiceCall, len(configs))
	for i, cfg := range configs {
		latency := time.Duration(cfg.baseMs+rng.Intn(cfg.jitterMs+1)) * time.Millisecond
		willFail := rng.Float64() < cfg.errorPct

		calls[i] = ServiceCall{
			Name: cfg.name,
			Fn: func() (any, error) {
				time.Sleep(latency)
				if willFail {
					return nil, errors.New("service error")
				}
				return fmt.Sprintf("%s-data", cfg.name), nil
			},
		}
	}
	return calls
}

func main() {
	stats := NewAggregateStats()

	fmt.Printf("=== BFF Scatter-Gather Simulation ===\n")
	fmt.Printf("  Concurrent requests: %d | Deadline: %v\n\n", concurrentRequests, globalDeadline)

	start := time.Now()
	var wg sync.WaitGroup

	for i := 1; i <= concurrentRequests; i++ {
		wg.Add(1)
		go func(reqID int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(reqID)))
			calls := makeServiceCalls(rng)
			result := ScatterGather(calls, globalDeadline)
			stats.Record(result)

			fmt.Printf("  Request %d: gathered=%d failed=%d timeout=%d (%v)\n",
				reqID, result.Gathered, result.Failed, result.TimedOut,
				result.Elapsed.Round(time.Millisecond))
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)

	fmt.Printf("\n=== Per-Service Reliability ===\n")
	fmt.Printf("  %-20s %8s %8s %8s %8s\n", "Service", "Success", "Error", "Timeout", "Avail%")
	for _, name := range []string{"profile", "orders", "recommendations", "notifications", "reviews"} {
		counts := stats.ByService[name]
		total := counts[0] + counts[1] + counts[2]
		availPct := float64(0)
		if total > 0 {
			availPct = float64(counts[0]) / float64(total) * 100
		}
		fmt.Printf("  %-20s %8d %8d %8d %7.1f%%\n",
			name, counts[0], counts[1], counts[2], availPct)
	}

	fmt.Printf("\n=== Summary ===\n")
	fmt.Printf("  Total wall time: %v\n", elapsed.Round(time.Millisecond))
	fmt.Printf("  Requests served: %d\n", stats.TotalRequests)
	fmt.Printf("  All requests completed within deadline despite partial failures\n")
}
```

**What's happening here:** Eight concurrent BFF requests each scatter-gather from the same five services. Each service has a configurable base latency, jitter range, and error probability. The `recommendations` service has a base latency of 150ms plus up to 300ms jitter, making it the most likely to exceed the 300ms deadline. Aggregate statistics track per-service reliability across all requests.

**Key insight:** The total wall time is approximately the deadline (300ms), not 8 times the deadline. All eight requests run concurrently, and each scatter-gather also runs its five service calls concurrently. This is nested concurrency: 8 request goroutines, each spawning 5 service goroutines, for 40 concurrent service calls. The system degrades gracefully -- even if recommendations times out on every request, the other four services still contribute data.

### Intermediate Verification
```bash
go run main.go
```
Expected output (values vary due to randomness):
```
=== BFF Scatter-Gather Simulation ===
  Concurrent requests: 8 | Deadline: 300ms

  Request 3: gathered=4 failed=0 timeout=1 (300ms)
  Request 1: gathered=5 failed=0 timeout=0 (250ms)
  Request 7: gathered=3 failed=1 timeout=1 (300ms)
  Request 2: gathered=4 failed=0 timeout=1 (300ms)
  Request 5: gathered=5 failed=0 timeout=0 (180ms)
  Request 4: gathered=4 failed=1 timeout=0 (220ms)
  Request 6: gathered=4 failed=0 timeout=1 (300ms)
  Request 8: gathered=5 failed=0 timeout=0 (270ms)

=== Per-Service Reliability ===
  Service               Success    Error  Timeout   Avail%
  profile                     8        0        0   100.0%
  orders                      7        1        0    87.5%
  recommendations             4        0        4    50.0%
  notifications               7        1        0    87.5%
  reviews                     6        1        1    75.0%

=== Summary ===
  Total wall time: 302ms
  Requests served: 8
  All requests completed within deadline despite partial failures
```


## Common Mistakes

### Using an Unbuffered Channel for Results

```go
// Wrong: unbuffered channel causes goroutine leaks
func ScatterGatherBroken(calls []ServiceCall, deadline time.Duration) []ServiceResult {
	resultCh := make(chan ServiceResult) // unbuffered!
	for _, call := range calls {
		go func(c ServiceCall) {
			data, _ := c.Fn()
			resultCh <- ServiceResult{ServiceName: c.Name, Data: data}
			// if deadline fires first, this goroutine blocks here forever
		}(call)
	}

	var results []ServiceResult
	timer := time.After(deadline)
	for range calls {
		select {
		case r := <-resultCh:
			results = append(results, r)
		case <-timer:
			return results // goroutines still trying to send are leaked
		}
	}
	return results
}
```
**What happens:** When the deadline fires, the function returns. Goroutines that complete after the deadline try to send on the unbuffered channel, but nobody is reading. They block forever, leaking goroutines and memory.

**Fix:** Use a buffered channel with capacity equal to the number of calls. Every goroutine can complete its send regardless of whether anyone reads the result.


### Not Preserving Result Order

```go
// Wrong: results arrive in completion order, not call order
func ScatterGatherUnordered(calls []ServiceCall, deadline time.Duration) []ServiceResult {
	resultCh := make(chan ServiceResult, len(calls))
	for _, call := range calls {
		go func(c ServiceCall) {
			data, _ := c.Fn()
			resultCh <- ServiceResult{ServiceName: c.Name, Data: data}
		}(call)
	}

	var results []ServiceResult
	for range calls {
		results = append(results, <-resultCh) // order depends on which goroutine finishes first
	}
	return results
	// caller cannot rely on results[0] being the profile service
}
```
**What happens:** The caller expects `results[0]` to always be the profile service, but it might be notifications (which was fastest). Code that indexes into the result slice breaks silently.

**Fix:** Collect results into a map keyed by service name, then build the final slice in the original call order. This is what the correct implementation does with `collected[result.ServiceName]`.


### Returning on First Error Instead of Collecting Partial Results

```go
// Wrong: any error aborts the entire aggregation
func ScatterGatherFailFast(calls []ServiceCall, deadline time.Duration) ([]ServiceResult, error) {
	resultCh := make(chan ServiceResult, len(calls))
	for _, call := range calls {
		go func(c ServiceCall) {
			data, err := c.Fn()
			if err != nil {
				resultCh <- ServiceResult{ServiceName: c.Name, Status: StatusError, Error: err.Error()}
				return
			}
			resultCh <- ServiceResult{ServiceName: c.Name, Status: StatusSuccess, Data: data}
		}(call)
	}

	var results []ServiceResult
	for range calls {
		r := <-resultCh
		if r.Status == StatusError {
			return nil, fmt.Errorf("service %s failed: %s", r.ServiceName, r.Error)
			// 4 other services responded successfully but we throw them away
		}
		results = append(results, r)
	}
	return results, nil
}
```
**What happens:** If the notifications service (a non-critical dependency) returns a 503, the entire profile page fails. Four healthy services responded successfully, but their data is discarded. The user sees an error page instead of a page with a missing notifications section.

**Fix:** Always collect all results. Classify each as success, error, or timeout. Let the caller decide which services are critical (e.g., profile is required but recommendations are optional).


## Verify What You Learned

Build a scatter-gather with **priority tiers**:
1. Define two tiers: `critical` (profile, orders) and `optional` (recommendations, notifications, reviews)
2. If all critical services respond successfully within the deadline, the request is a success (even if optional services failed or timed out)
3. If any critical service fails or times out, the request is a degraded failure -- return partial results but flag the response as incomplete
4. Add a second, shorter deadline for optional services (200ms) while critical services get the full deadline (500ms)
5. Print per-request classification: `full` (all succeeded), `partial` (critical OK, some optional missing), `degraded` (critical missing)

**Hint:** Run two scatter-gather passes or use a single channel with different deadline handling per tier.


## What's Next
Continue to [Goroutine Starvation and Fairness](../28-goroutine-starvation-fairness/28-goroutine-starvation-fairness.md) to understand how CPU-heavy goroutines can starve latency-sensitive ones and how to mitigate it.


## Summary
- Scatter-gather launches concurrent requests and collects results within a deadline -- partial data is better than no data
- Buffered channels are essential: capacity must match the number of goroutines to prevent leaks after deadline expiry
- `time.After` in a `select` enforces global deadlines without blocking on slow services
- Classify each result as success, error, or timeout for proper observability and degradation handling
- Preserve result order by collecting into a map and rebuilding in call order, not completion order
- This pattern is the foundation of every API aggregation layer: BFFs, API gateways, and GraphQL resolvers


## Reference
- [time.After](https://pkg.go.dev/time#After) -- deadline timers for concurrent operations
- [Go Blog: Pipelines and Cancellation](https://go.dev/blog/pipelines) -- channel patterns for concurrent processing
- [Scatter-Gather Pattern](https://www.enterpriseintegrationpatterns.com/patterns/messaging/BroadcastAggregate.html) -- enterprise integration pattern
