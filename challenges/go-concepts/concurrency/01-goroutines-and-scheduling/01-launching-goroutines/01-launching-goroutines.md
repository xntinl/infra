---
difficulty: basic
concepts: [go keyword, concurrent execution, anonymous goroutines, safe argument passing, WaitGroup]
tools: [go]
estimated_time: 15m
bloom_level: apply
prerequisites: [Go basics, functions, closures]
---


# 1. Launching Goroutines


## Learning Objectives
After completing this exercise, you will be able to:
- **Launch** concurrent goroutines using the `go` keyword
- **Distinguish** between sequential and concurrent execution by measuring wall-clock time
- **Create** both named and anonymous goroutines
- **Pass** arguments safely to goroutines to avoid shared-variable bugs
- **Use** `sync.WaitGroup` for proper synchronization (instead of `time.Sleep`)

## Why Goroutines

Goroutines are the fundamental unit of concurrency in Go. Unlike threads in most languages, goroutines are extraordinarily cheap to create, use minimal memory (starting at just a few kilobytes of stack), and are multiplexed onto a small number of OS threads by the Go runtime scheduler.

The `go` keyword is the gateway to concurrent programming in Go. By placing `go` before a function call, you tell the runtime to execute that function independently, without waiting for it to finish. Understanding how goroutines launch, how they interleave with `main`, and how to pass data to them safely is the bedrock upon which all other concurrency patterns are built.

A critical subtlety is that `main` itself runs in a goroutine. When `main` returns, all other goroutines are terminated immediately, regardless of whether they have finished. This means you must explicitly wait for goroutines to complete -- a theme that will recur throughout this series. In this exercise, we use `sync.WaitGroup` for proper synchronization rather than `time.Sleep`, which is fragile and non-deterministic.

## Step 1 -- Sequential vs Concurrent Health Checks

Imagine you operate a platform that depends on several upstream services: an authentication API, a payment gateway, a notification service, and others. Before deploying a new release, your CLI tool checks that every dependency is healthy. Running these checks one after another wastes time when each check is just waiting for a network response.

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type ServiceEndpoint struct {
	Name    string
	Latency time.Duration
}

func checkService(name string, latency time.Duration) string {
	time.Sleep(latency)
	return fmt.Sprintf("%-18s UP  (%v)", name, latency)
}

func runSequentialChecks(services []ServiceEndpoint) time.Duration {
	start := time.Now()
	for _, svc := range services {
		result := checkService(svc.Name, svc.Latency)
		fmt.Printf("  %s\n", result)
	}
	return time.Since(start)
}

func runConcurrentChecks(services []ServiceEndpoint) time.Duration {
	start := time.Now()
	var wg sync.WaitGroup

	for _, svc := range services {
		wg.Add(1)
		go func(name string, latency time.Duration) {
			defer wg.Done()
			result := checkService(name, latency)
			fmt.Printf("  %s\n", result)
		}(svc.Name, svc.Latency)
	}
	wg.Wait()

	return time.Since(start)
}

func main() {
	services := []ServiceEndpoint{
		{"auth-api", 120 * time.Millisecond},
		{"payment-gateway", 200 * time.Millisecond},
		{"notification-svc", 80 * time.Millisecond},
		{"inventory-api", 150 * time.Millisecond},
		{"search-engine", 90 * time.Millisecond},
	}

	fmt.Println("--- Sequential Health Check ---")
	seqDuration := runSequentialChecks(services)
	fmt.Printf("  Sequential total: %v\n\n", seqDuration.Round(time.Millisecond))

	fmt.Println("--- Concurrent Health Check ---")
	concDuration := runConcurrentChecks(services)
	fmt.Printf("  Concurrent total: %v\n", concDuration.Round(time.Millisecond))
}
```

**What's happening here:** In the sequential version, each `checkService` call must finish before the next starts. Total time is the sum of all latencies: ~640ms. In the concurrent version, `go func(...)` launches each check as an independent goroutine. All five run simultaneously, so total time equals the slowest single check: ~200ms.

**Key insight:** The `go` keyword does not wait. It launches the function and returns immediately. `wg.Wait()` blocks until all goroutines call `wg.Done()`.

**What would happen if you removed `wg.Wait()`?** Main would exit immediately, killing all goroutines before they complete any health check. Your CLI would report nothing.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
--- Sequential Health Check ---
  auth-api           UP  (120ms)
  payment-gateway    UP  (200ms)
  notification-svc   UP  (80ms)
  inventory-api      UP  (150ms)
  search-engine      UP  (90ms)
  Sequential total: 640ms

--- Concurrent Health Check ---
  notification-svc   UP  (80ms)
  search-engine      UP  (90ms)
  auth-api           UP  (120ms)
  inventory-api      UP  (150ms)
  payment-gateway    UP  (200ms)
  Concurrent total: 200ms
```

## Step 2 -- Anonymous Goroutines with Channel Results

In production, you do not just print results -- you need to collect them for further processing. Anonymous goroutines can send results through channels so the caller decides what to do with them.

```go
package main

import (
	"fmt"
	"time"
)

const degradedService = "payment-gateway"

type HealthResult struct {
	Service string
	Healthy bool
	Latency time.Duration
}

func simulateHealthCheck(serviceName string) HealthResult {
	checkStart := time.Now()
	simulatedLatency := time.Duration(50+len(serviceName)*10) * time.Millisecond
	time.Sleep(simulatedLatency)

	return HealthResult{
		Service: serviceName,
		Healthy: serviceName != degradedService,
		Latency: time.Since(checkStart),
	}
}

func launchChecks(services []string) <-chan HealthResult {
	results := make(chan HealthResult, len(services))
	for _, svc := range services {
		go func(name string) {
			results <- simulateHealthCheck(name)
		}(svc)
	}
	return results
}

func collectResults(results <-chan HealthResult, count int) (downCount int) {
	for i := 0; i < count; i++ {
		r := <-results
		status := "UP"
		if !r.Healthy {
			status = "DOWN"
			downCount++
		}
		fmt.Printf("  %-20s %4s  (%v)\n", r.Service, status, r.Latency.Round(time.Millisecond))
	}
	return downCount
}

func main() {
	services := []string{"auth-api", "payment-gateway", "notification-svc", "inventory-api", "search-engine"}

	start := time.Now()
	results := launchChecks(services)
	downCount := collectResults(results, len(services))

	fmt.Printf("\n  Total: %v | Services down: %d/%d\n",
		time.Since(start).Round(time.Millisecond), downCount, len(services))
}
```

**What's happening here:** Each anonymous goroutine sends a `HealthResult` struct through a buffered channel. The main goroutine collects exactly `len(services)` results. The trailing `(svc)` on the anonymous function captures the loop variable safely.

**Key insight:** Parameters are copied at the moment the goroutine is launched, not when it executes. This is why passing `svc` as a function argument is safer than capturing the loop variable by reference.

### Intermediate Verification
```bash
go run main.go
```
Expected output (order varies):
```
  auth-api             UP    (120ms)
  search-engine        UP    (180ms)
  notification-svc     UP    (210ms)
  payment-gateway      DOWN  (210ms)
  inventory-api        UP    (180ms)

  Total: 213ms | Services down: 1/5
```

## Step 3 -- The Closure Capture Bug in Real Code

When building goroutines inside a loop, a common production bug is accidentally sharing the loop variable. In a health checker, this means every goroutine checks the SAME service, missing failures on others entirely.

```go
package main

import (
	"fmt"
	"sync"
)

func demonstrateBuggyCapture(endpoints []string) {
	fmt.Println("--- BUG: shared variable capture ---")
	var wg sync.WaitGroup
	idx := 0
	for idx = 0; idx < len(endpoints); idx++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fmt.Printf("  [BUG] checking: %s\n", endpoints[idx-1])
		}()
	}
	wg.Wait()
}

func demonstrateCorrectCapture(endpoints []string) {
	fmt.Println()
	fmt.Println("--- FIX: argument passing ---")
	var wg sync.WaitGroup
	for i, ep := range endpoints {
		wg.Add(1)
		go func(index int, endpoint string) {
			defer wg.Done()
			fmt.Printf("  [OK]  goroutine %d checking: %s\n", index, endpoint)
		}(i, ep)
	}
	wg.Wait()
}

func main() {
	endpoints := []string{
		"https://auth.internal/health",
		"https://payments.internal/health",
		"https://notifications.internal/health",
		"https://inventory.internal/health",
	}

	demonstrateBuggyCapture(endpoints)
	demonstrateCorrectCapture(endpoints)
}
```

**What's happening here:** In the BUG version, all goroutines share the same `idx` variable. By the time they execute, the loop has finished and `idx` equals `len(endpoints)`. Every goroutine checks the last endpoint, so you might think all services are healthy when in reality some are down. In the FIX version, each goroutine receives its own copy via function arguments.

**Key insight:** Go 1.22+ changed loop variable semantics so that `for i := 0` creates a new `i` per iteration. However, the explicit parameter passing pattern remains idiomatic and clearest. The bug can still occur with package-level variables or variables declared outside the loop. In production, this bug means your monitoring is blind to failures on most of your services.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
--- BUG: shared variable capture ---
  [BUG] checking: https://inventory.internal/health
  [BUG] checking: https://inventory.internal/health
  [BUG] checking: https://inventory.internal/health
  [BUG] checking: https://inventory.internal/health

--- FIX: argument passing ---
  [OK]  goroutine 2 checking: https://notifications.internal/health
  [OK]  goroutine 0 checking: https://auth.internal/health
  [OK]  goroutine 3 checking: https://inventory.internal/health
  [OK]  goroutine 1 checking: https://payments.internal/health
```

## Step 4 -- Fan-Out Health Check with Timeout Simulation

The complete pattern: launch one goroutine per service, collect results through a channel, and report a structured summary. This is the foundation of every concurrent CLI tool.

```go
package main

import (
	"fmt"
	"math/rand"
	"sort"
	"time"
)

const (
	minLatency       = 30
	maxExtraLatency  = 150
	degradedChance   = 0.15
	avgLatencyPerSvc = 100 // milliseconds, for sequential estimate
)

type ServiceHealth struct {
	Name    string
	Status  string
	Latency time.Duration
}

type HealthReport struct {
	Results  []ServiceHealth
	Healthy  int
	Degraded int
}

func checkServiceHealth(name string) ServiceHealth {
	checkStart := time.Now()
	latency := time.Duration(rand.Intn(maxExtraLatency)+minLatency) * time.Millisecond
	time.Sleep(latency)

	status := "UP"
	if rand.Float32() < degradedChance {
		status = "DEGRADED"
	}

	return ServiceHealth{
		Name:    name,
		Status:  status,
		Latency: time.Since(checkStart),
	}
}

func fanOutHealthChecks(services []string) []ServiceHealth {
	results := make(chan ServiceHealth, len(services))
	for _, svc := range services {
		go func(name string) {
			results <- checkServiceHealth(name)
		}(svc)
	}

	var all []ServiceHealth
	for i := 0; i < len(services); i++ {
		all = append(all, <-results)
	}

	sort.Slice(all, func(i, j int) bool { return all[i].Latency < all[j].Latency })
	return all
}

func buildReport(results []ServiceHealth) HealthReport {
	report := HealthReport{Results: results}
	for _, r := range results {
		if r.Status == "DEGRADED" {
			report.Degraded++
		} else {
			report.Healthy++
		}
	}
	return report
}

func printReport(report HealthReport, wallClock time.Duration) {
	fmt.Println("=== Service Health Report ===")
	for _, r := range report.Results {
		marker := "  "
		if r.Status == "DEGRADED" {
			marker = "!!"
		}
		fmt.Printf("  %s %-22s %-10s %v\n", marker, r.Name, r.Status, r.Latency.Round(time.Millisecond))
	}

	total := len(report.Results)
	fmt.Printf("\n  Checked %d services in %v\n", total, wallClock.Round(time.Millisecond))
	fmt.Printf("  Healthy: %d | Degraded: %d\n", report.Healthy, report.Degraded)
	fmt.Printf("  Sequential would have taken: ~%v\n",
		time.Duration(total*avgLatencyPerSvc)*time.Millisecond)
}

func main() {
	services := []string{
		"auth-api", "payment-gateway", "notification-svc",
		"inventory-api", "search-engine", "user-profile-svc",
		"order-service", "analytics-api", "cdn-gateway", "cache-cluster",
	}

	start := time.Now()
	results := fanOutHealthChecks(services)
	wallClock := time.Since(start)

	report := buildReport(results)
	printReport(report, wallClock)
}
```

**What's happening here:** Ten goroutines start simultaneously, each simulating a health check with variable latency. Results arrive in completion order through the channel, get sorted by latency, and are printed as a structured report. The fan-out pattern is safe because each goroutine operates on its own data.

**Key insight:** The fan-out pattern is the natural fit for independent checks. Wall-clock time equals the slowest service (~180ms), not the sum of all (~1000ms). In production, this is the difference between a deployment check that takes 10 seconds and one that takes 200ms.

### Intermediate Verification
```bash
go run main.go
```
Expected output (order and values vary):
```
=== Service Health Report ===
     notification-svc       UP         35ms
     cache-cluster          UP         52ms
  !! search-engine          DEGRADED   67ms
     auth-api               UP         89ms
     order-service          UP         102ms
     ...

  Checked 10 services in 178ms
  Healthy: 8 | Degraded: 2
  Sequential would have taken: ~1s
```

## Common Mistakes

### Capturing Loop Variables by Reference

**Wrong -- complete program:**
```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	endpoints := []string{"auth", "payments", "orders", "users"}
	var wg sync.WaitGroup
	for i := 0; i < len(endpoints); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fmt.Println(i) // captures variable i, not its value
		}()
	}
	wg.Wait()
}
```
**What happens:** All goroutines likely print `4` because they share the same `i`, which has reached 4 by the time they execute. In a real health checker, every goroutine would check only the last endpoint, leaving the others unmonitored.

**Correct -- complete program:**
```go
package main

import (
	"fmt"
	"sync"
)

func printEndpoint(endpoint string, wg *sync.WaitGroup) {
	defer wg.Done()
	fmt.Println(endpoint)
}

func main() {
	endpoints := []string{"auth", "payments", "orders", "users"}
	var wg sync.WaitGroup
	for _, ep := range endpoints {
		wg.Add(1)
		go printEndpoint(ep, &wg)
	}
	wg.Wait()
}
```

### Forgetting to Wait for Goroutines

**Wrong -- complete program:**
```go
package main

import "fmt"

func main() {
	go fmt.Println("health check complete")
	// main exits immediately -- goroutine never runs
}
```
**What happens:** The program exits before the goroutine has a chance to execute. In a CI/CD pipeline, your health check reports success without actually checking anything.

**Correct -- complete program:**
```go
package main

import (
	"fmt"
	"sync"
)

func reportHealthCheck(wg *sync.WaitGroup) {
	defer wg.Done()
	fmt.Println("health check complete")
}

func main() {
	var wg sync.WaitGroup
	wg.Add(1)
	go reportHealthCheck(&wg)
	wg.Wait()
}
```

### Trying to Get a Return Value from `go`

**Wrong:**
```go
go result := checkHealth("auth-api") // syntax error: go does not return values
```
**What happens:** Compilation error. The `go` keyword starts a function call concurrently; it cannot capture return values.

**Correct -- use a channel:**
```go
package main

import "fmt"

func checkHealth(service string) string {
	return service + ": UP"
}

func main() {
	ch := make(chan string)
	go func() {
		ch <- checkHealth("auth-api")
	}()
	result := <-ch
	fmt.Println(result) // auth-api: UP
}
```

## Verify What You Learned

Build a "multi-region health checker" that:
1. Defines a list of 6 services, each available in 3 regions (e.g., "auth-us-east", "auth-eu-west", "auth-ap-south")
2. Launches one goroutine per service-region combination (18 total)
3. Each goroutine simulates a check with random latency (30-200ms) and random success/failure
4. Sends results through a buffered channel
5. Collects and prints results grouped by region, showing the slowest and fastest per region

**Hint:** Use `make(chan HealthResult, 18)` as the result channel and collect exactly 18 results from it.

## What's Next
Continue to [02-goroutine-vs-os-thread](../02-goroutine-vs-os-thread/02-goroutine-vs-os-thread.md) to understand why goroutines are so much cheaper than OS threads.

## Summary
- The `go` keyword launches a function call as an independent goroutine
- `main` is itself a goroutine; when it exits, all other goroutines are killed
- Anonymous goroutines must be immediately invoked with `()`
- Always pass loop variables as function arguments to avoid shared-variable bugs
- Goroutine execution order is non-deterministic
- Use `sync.WaitGroup` for proper synchronization (not `time.Sleep`)
- The fan-out pattern launches one goroutine per independent task, reducing wall-clock time from the sum of latencies to the maximum single latency

## Reference
- [Go Tour: Goroutines](https://go.dev/tour/concurrency/1)
- [Effective Go: Goroutines](https://go.dev/doc/effective_go#goroutines)
- [Go Spec: Go Statements](https://go.dev/ref/spec#Go_statements)
- [sync.WaitGroup](https://pkg.go.dev/sync#WaitGroup)
