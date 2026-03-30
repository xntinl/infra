---
difficulty: basic
concepts: [errgroup pattern, WaitGroup, sync.Once, error propagation, concurrent health checks]
tools: [go]
estimated_time: 25m
bloom_level: apply
---

# 1. Errgroup Basics -- Deployment Health Checker

## Learning Objectives
After completing this exercise, you will be able to:
- **Build** a concurrent health checker that verifies multiple infrastructure services in parallel
- **Implement** the errgroup pattern from scratch using `sync.WaitGroup` + `sync.Once` + error variable
- **Explain** how first-error propagation works and why it matters for deployment gates
- **Compare** the manual implementation with the production-ready `golang.org/x/sync/errgroup`

## Why This Pattern Matters

Before deploying a new version of your application, you need to verify that all dependencies are healthy: database is reachable, cache is responding, message queue is accepting connections, object storage is accessible. These checks are independent, so running them sequentially wastes time. Running them in parallel cuts your deployment gate from 4x the slowest check to 1x.

The problem: when you launch goroutines with `sync.WaitGroup`, there is no built-in way to collect errors. You need a WaitGroup for synchronization, a mutex or `sync.Once` for thread-safe error capture, and an error variable to store the first failure. This boilerplate repeats in every concurrent-with-errors scenario in your codebase.

The "errgroup" pattern encapsulates all of this: launch goroutines that return errors, wait for all of them, get back the first error. The `golang.org/x/sync/errgroup` package provides this ready-made, but understanding the underlying mechanics is more valuable than blindly importing a package.

## Step 1 -- Sequential Health Checks (The Baseline)

Start with the sequential version to understand the problem. Each service check takes time, and we run them one after another:

```go
package main

import (
	"fmt"
	"time"
)

type ServiceName string

const (
	Postgres ServiceName = "postgres"
	Redis    ServiceName = "redis"
	RabbitMQ ServiceName = "rabbitmq"
	S3       ServiceName = "s3"
)

type HealthChecker struct {
	services []ServiceName
}

func NewHealthChecker(services []ServiceName) *HealthChecker {
	return &HealthChecker{services: services}
}

func (hc *HealthChecker) RunSequential() error {
	for _, svc := range hc.services {
		if err := hc.checkHealth(svc); err != nil {
			return err
		}
		fmt.Printf("  OK: %s\n", svc)
	}
	return nil
}

func (hc *HealthChecker) checkHealth(service ServiceName) error {
	switch service {
	case Postgres:
		time.Sleep(120 * time.Millisecond) // simulates TCP connect + ping
		return nil
	case Redis:
		time.Sleep(30 * time.Millisecond)
		return nil
	case RabbitMQ:
		time.Sleep(80 * time.Millisecond)
		return fmt.Errorf("rabbitmq: connection refused on port 5672")
	case S3:
		time.Sleep(150 * time.Millisecond)
		return nil
	default:
		return fmt.Errorf("unknown service: %s", service)
	}
}

func main() {
	checker := NewHealthChecker([]ServiceName{Postgres, Redis, RabbitMQ, S3})

	fmt.Println("=== Sequential Health Check ===")
	start := time.Now()

	if err := checker.RunSequential(); err != nil {
		fmt.Printf("FAIL: %v\n", err)
	} else {
		fmt.Print("All services healthy. ")
	}
	fmt.Printf("Total time: %v\n", time.Since(start).Round(time.Millisecond))
}
```

**Expected output:**
```
=== Sequential Health Check ===
  OK: postgres
  OK: redis
FAIL: rabbitmq: connection refused on port 5672
Total time: 230ms
```

The sequential approach takes the sum of all check durations up to the first failure: 120 + 30 + 80 = 230ms. If all services were healthy, it would take 120 + 30 + 80 + 150 = 380ms. In production with 10+ services and network latency, this adds seconds to every deployment.

## Step 2 -- Parallel Health Checks with the Manual Errgroup Pattern

Now build the errgroup pattern from scratch. You need three standard library primitives working together:

1. `sync.WaitGroup` -- wait for all goroutines to finish
2. `sync.Once` -- capture only the first error (thread-safe)
3. An `error` variable -- store the captured error

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type ServiceName string

const (
	Postgres ServiceName = "postgres"
	Redis    ServiceName = "redis"
	RabbitMQ ServiceName = "rabbitmq"
	S3       ServiceName = "s3"
)

type HealthChecker struct {
	services []ServiceName
}

func NewHealthChecker(services []ServiceName) *HealthChecker {
	return &HealthChecker{services: services}
}

func (hc *HealthChecker) RunParallel() error {
	start := time.Now()

	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error

	for _, svc := range hc.services {
		svc := svc
		wg.Add(1)
		go func() {
			defer wg.Done()

			if err := hc.checkHealth(svc); err != nil {
				once.Do(func() { firstErr = err })
				return
			}
			fmt.Printf("  OK: %s (%v)\n", svc, time.Since(start).Round(time.Millisecond))
		}()
	}

	wg.Wait()
	return firstErr
}

func (hc *HealthChecker) checkHealth(service ServiceName) error {
	switch service {
	case Postgres:
		time.Sleep(120 * time.Millisecond)
		return nil
	case Redis:
		time.Sleep(30 * time.Millisecond)
		return nil
	case RabbitMQ:
		time.Sleep(80 * time.Millisecond)
		return fmt.Errorf("rabbitmq: connection refused on port 5672")
	case S3:
		time.Sleep(150 * time.Millisecond)
		return nil
	default:
		return fmt.Errorf("unknown service: %s", service)
	}
}

func main() {
	checker := NewHealthChecker([]ServiceName{Postgres, Redis, RabbitMQ, S3})

	fmt.Println("=== Parallel Health Check (manual errgroup) ===")
	start := time.Now()

	if err := checker.RunParallel(); err != nil {
		fmt.Printf("FAIL: %v\n", err)
		fmt.Printf("Total time: %v (all checks ran in parallel)\n", time.Since(start).Round(time.Millisecond))
	} else {
		fmt.Printf("All services healthy. Total time: %v\n", time.Since(start).Round(time.Millisecond))
	}
}
```

**Expected output:**
```
=== Parallel Health Check (manual errgroup) ===
  OK: redis (30ms)
  OK: postgres (120ms)
  OK: s3 (150ms)
FAIL: rabbitmq: connection refused on port 5672
Total time: 150ms (all checks ran in parallel)
```

Total time is now 150ms (the slowest check), not 230ms (sum of checks up to failure). All four checks ran concurrently. The `sync.Once` ensures that only the first error is captured -- if both rabbitmq and another service failed, you still get exactly one error.

Notice the boilerplate: `WaitGroup` (Add, Done, Wait), `sync.Once`, error variable, goroutine closure with captured variable. Five moving parts that must be wired correctly every time.

## Step 3 -- First-Error Semantics with Multiple Failures

When multiple services fail, only the first error is kept. The "first" error depends on which goroutine completes first, which is determined by timing:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type ServiceName string

const (
	Postgres ServiceName = "postgres"
	Redis    ServiceName = "redis"
	RabbitMQ ServiceName = "rabbitmq"
	S3       ServiceName = "s3"
)

type HealthChecker struct {
	services []ServiceName
}

func NewHealthChecker(services []ServiceName) *HealthChecker {
	return &HealthChecker{services: services}
}

func (hc *HealthChecker) RunParallelAllFailing() error {
	start := time.Now()

	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error

	for _, svc := range hc.services {
		svc := svc
		wg.Add(1)
		go func() {
			defer wg.Done()

			if err := hc.checkHealthMostFailing(svc); err != nil {
				once.Do(func() { firstErr = err })
				fmt.Printf("  FAIL: %s (%v)\n", svc, time.Since(start).Round(time.Millisecond))
				return
			}
			fmt.Printf("  OK: %s (%v)\n", svc, time.Since(start).Round(time.Millisecond))
		}()
	}

	wg.Wait()
	return firstErr
}

func (hc *HealthChecker) checkHealthMostFailing(service ServiceName) error {
	switch service {
	case Postgres:
		time.Sleep(120 * time.Millisecond)
		return fmt.Errorf("postgres: authentication failed")
	case Redis:
		time.Sleep(30 * time.Millisecond)
		return nil // redis is OK
	case RabbitMQ:
		time.Sleep(80 * time.Millisecond)
		return fmt.Errorf("rabbitmq: connection refused")
	case S3:
		time.Sleep(150 * time.Millisecond)
		return fmt.Errorf("s3: bucket not found")
	default:
		return fmt.Errorf("unknown service: %s", service)
	}
}

func main() {
	checker := NewHealthChecker([]ServiceName{Postgres, Redis, RabbitMQ, S3})

	fmt.Println("=== Multiple Failures ===")

	firstErr := checker.RunParallelAllFailing()
	fmt.Printf("\nWait returned: %v (only the first error is kept)\n", firstErr)
}
```

**Expected output:**
```
=== Multiple Failures ===
  OK: redis (30ms)
  FAIL: rabbitmq (80ms)
  FAIL: postgres (120ms)
  FAIL: s3 (150ms)

Wait returned: rabbitmq: connection refused (only the first error is kept)
```

Three services fail, but the error variable holds only `rabbitmq: connection refused` because rabbitmq fails at 80ms -- before postgres (120ms) and s3 (150ms). The `sync.Once` blocks the later errors from overwriting the first. If you need all errors, use a mutex-protected slice instead of `sync.Once`.

## Step 4 -- Reusable HealthChecker

Extract the pattern into a reusable function. This is essentially what `golang.org/x/sync/errgroup` does internally:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type HealthCheckFn func() error

type HealthCheck struct {
	Name string
	Fn   HealthCheckFn
}

type HealthChecker struct {
	checks []HealthCheck
}

func NewHealthChecker(checks []HealthCheck) *HealthChecker {
	return &HealthChecker{checks: checks}
}

func (hc *HealthChecker) RunAllParallel() error {
	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error

	for _, check := range hc.checks {
		check := check
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := check.Fn(); err != nil {
				once.Do(func() { firstErr = err })
			}
		}()
	}

	wg.Wait()
	return firstErr
}

func simulateService(latency time.Duration, errMsg string) HealthCheckFn {
	return func() error {
		time.Sleep(latency)
		if errMsg != "" {
			return fmt.Errorf("%s", errMsg)
		}
		return nil
	}
}

func main() {
	checker := NewHealthChecker([]HealthCheck{
		{Name: "postgres", Fn: simulateService(120*time.Millisecond, "")},
		{Name: "redis", Fn: simulateService(30*time.Millisecond, "")},
		{Name: "rabbitmq", Fn: simulateService(80*time.Millisecond, "rabbitmq: connection refused on port 5672")},
		{Name: "s3", Fn: simulateService(150*time.Millisecond, "")},
	})

	fmt.Println("=== Reusable Health Checker ===")
	start := time.Now()

	if err := checker.RunAllParallel(); err != nil {
		fmt.Printf("Deployment blocked: %v\n", err)
	} else {
		fmt.Println("All services healthy -- deploy!")
	}

	fmt.Printf("Total time: %v\n", time.Since(start).Round(time.Millisecond))

	// The golang.org/x/sync/errgroup package provides exactly this pattern:
	//
	//   var g errgroup.Group
	//   for _, check := range checks {
	//       check := check
	//       g.Go(check.Fn)
	//   }
	//   err := g.Wait()
	//
	// That is the entire implementation. g.Go() replaces wg.Add+go+defer wg.Done.
	// g.Wait() replaces wg.Wait and returns the first error.
	// No WaitGroup, no Once, no error variable -- one type does it all.
}
```

**Expected output:**
```
=== Reusable Health Checker ===
Deployment blocked: rabbitmq: connection refused on port 5672
Total time: 150ms
```

The `runChecksParallel` function is a minimal errgroup. The real `golang.org/x/sync/errgroup.Group` adds context integration (exercise 02), concurrency limits (exercise 03), and is battle-tested across thousands of Go services. But the core idea is exactly what you built here: WaitGroup + Once + first error.

## Intermediate Verification

At this point, verify:
1. The sequential version takes ~230ms (sum of checks up to failure)
2. The parallel version takes ~150ms (max of all checks)
3. Multiple failures result in only the first error being returned
4. The reusable function produces identical behavior

## Common Mistakes

### Forgetting to capture the loop variable

**Wrong:**
```go
for _, svc := range services {
    wg.Add(1)
    go func() {
        defer wg.Done()
        checkHealth(svc) // all goroutines see the last value of svc
    }()
}
```

**What happens:** All goroutines share the same `svc` variable by reference. By the time they execute, the loop has finished and `svc` holds the last element. In Go 1.22+ this is fixed for `for range` loops, but for clarity and backward compatibility, always shadow:

```go
for _, svc := range services {
    svc := svc // shadow the loop variable
    wg.Add(1)
    go func() {
        defer wg.Done()
        checkHealth(svc)
    }()
}
```

### Using a mutex instead of sync.Once for first-error capture

**Not wrong, but unnecessary complexity:**
```go
mu.Lock()
if firstErr == nil {
    firstErr = err
}
mu.Unlock()
```

**Better:** `sync.Once` is purpose-built for "do this exactly once." It communicates intent more clearly and avoids the if-nil check.

### Swallowing errors inside the goroutine

**Wrong:**
```go
go func() {
    defer wg.Done()
    if err := checkHealth(svc); err != nil {
        log.Println(err) // logged but not propagated
    }
}()
```

**What happens:** The caller of `wg.Wait()` never knows a check failed. The deployment proceeds with a broken dependency. In production, this means deploying to a cluster where the database is unreachable.

## Verify What You Learned

Run the full program and confirm:
1. Sequential checks take the sum of durations; parallel checks take the max
2. `sync.Once` captures exactly one error even when multiple goroutines fail
3. The reusable `runChecksParallel` function works identically to inline code
4. A zero-error run returns nil

```bash
go run main.go
```

## What's Next
Continue to [02-errgroup-with-context](../02-errgroup-with-context/02-errgroup-with-context.md) to learn how to cancel remaining health checks immediately when one fails -- so you do not waste resources checking services when the deployment is already blocked.

## Summary
- The errgroup pattern combines `sync.WaitGroup` + `sync.Once` + error variable for concurrent-with-errors work
- Launch goroutines that return errors, wait for all, get back the first failure
- All goroutines run to completion -- the pattern does NOT cancel siblings (that requires context, exercise 02)
- Only the first error is returned; subsequent errors are discarded by `sync.Once`
- The `golang.org/x/sync/errgroup` package provides this pattern ready-made as `errgroup.Group`
- Always capture loop variables in closures: `svc := svc` before the goroutine
- Never swallow errors inside goroutines -- propagate them to the caller

## Reference
- [sync.WaitGroup documentation](https://pkg.go.dev/sync#WaitGroup)
- [sync.Once documentation](https://pkg.go.dev/sync#Once)
- [errgroup package documentation](https://pkg.go.dev/golang.org/x/sync/errgroup)
- [Go Blog: Pipelines and cancellation](https://go.dev/blog/pipelines)
