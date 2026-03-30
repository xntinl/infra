---
difficulty: intermediate
concepts: [context.WithCancel, cooperative cancellation, goroutine leak, fail-fast, concurrent API calls]
tools: [go]
estimated_time: 30m
bloom_level: apply
---

# 2. Errgroup with Context -- Dashboard Data Loader

## Learning Objectives
After completing this exercise, you will be able to:
- **Build** a dashboard data loader that fetches from multiple APIs concurrently with cancellation
- **Implement** context-based cancellation so one failed API call stops all remaining requests
- **Detect** goroutine leaks caused by missing cancellation
- **Explain** why cooperative cancellation requires goroutines to actively check `ctx.Done()`

## Why Context Cancellation Matters

Your dashboard page loads data from 4 internal APIs concurrently: user profile, recent orders, notifications, and recommendations. If the orders API is down and returns an error after 50ms, the naive approach keeps the other 3 requests running for another 2 seconds -- even though you already know the dashboard cannot render completely.

In production, this means:
- Wasted HTTP connections to services that are under load
- Goroutines stuck waiting for responses that nobody will use
- Memory held by those goroutines until they eventually finish
- If the failing service is slow instead of fast, you get goroutine leaks that accumulate over time

The fix: when any API fails, cancel all remaining requests immediately. This is what `context.WithCancel` provides. The errgroup-with-context pattern automatically cancels a derived context when the first goroutine returns an error.

## Step 1 -- Without Cancellation (Wasted Resources)

First, observe the problem. Four API calls run concurrently. The orders API fails at 50ms, but the other three keep running until they finish:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	fmt.Println("=== Without Cancellation ===")
	start := time.Now()

	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error

	apis := []struct {
		name    string
		latency time.Duration
		fail    bool
	}{
		{"user-profile", 200 * time.Millisecond, false},
		{"recent-orders", 50 * time.Millisecond, true},
		{"notifications", 300 * time.Millisecond, false},
		{"recommendations", 400 * time.Millisecond, false},
	}

	for _, api := range apis {
		api := api
		wg.Add(1)
		go func() {
			defer wg.Done()

			time.Sleep(api.latency)
			if api.fail {
				once.Do(func() {
					firstErr = fmt.Errorf("%s: 503 service unavailable", api.name)
				})
				fmt.Printf("  [%v] %s: FAILED\n", time.Since(start).Round(time.Millisecond), api.name)
				return
			}
			fmt.Printf("  [%v] %s: completed (wasted work!)\n", time.Since(start).Round(time.Millisecond), api.name)
		}()
	}

	wg.Wait()
	elapsed := time.Since(start).Round(time.Millisecond)
	fmt.Printf("\nError: %v\n", firstErr)
	fmt.Printf("Total time: %v -- waited for ALL goroutines despite knowing error at 50ms\n", elapsed)
}
```

**Expected output:**
```
=== Without Cancellation ===
  [50ms] recent-orders: FAILED
  [200ms] user-profile: completed (wasted work!)
  [300ms] notifications: completed (wasted work!)
  [400ms] recommendations: completed (wasted work!)

Error: recent-orders: 503 service unavailable
Total time: 400ms -- waited for ALL goroutines despite knowing error at 50ms
```

The error was known at 50ms, but the program waited 400ms for all goroutines to finish. Those 350ms of extra work are pure waste -- the dashboard will show an error page regardless.

## Step 2 -- With Context Cancellation (Fail Fast)

Now add `context.WithCancel`. When the first error occurs, cancel the context. All other goroutines check `ctx.Done()` and bail out early:

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

func main() {
	fmt.Println("=== With Context Cancellation ===")
	start := time.Now()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error

	apis := []struct {
		name    string
		latency time.Duration
		fail    bool
	}{
		{"user-profile", 200 * time.Millisecond, false},
		{"recent-orders", 50 * time.Millisecond, true},
		{"notifications", 300 * time.Millisecond, false},
		{"recommendations", 400 * time.Millisecond, false},
	}

	for _, api := range apis {
		api := api
		wg.Add(1)
		go func() {
			defer wg.Done()

			select {
			case <-ctx.Done():
				fmt.Printf("  [%v] %s: cancelled before starting\n",
					time.Since(start).Round(time.Millisecond), api.name)
				return
			default:
			}

			select {
			case <-ctx.Done():
				fmt.Printf("  [%v] %s: cancelled while waiting\n",
					time.Since(start).Round(time.Millisecond), api.name)
				return
			case <-time.After(api.latency):
			}

			if api.fail {
				once.Do(func() {
					firstErr = fmt.Errorf("%s: 503 service unavailable", api.name)
					cancel() // cancel the context for all siblings
				})
				fmt.Printf("  [%v] %s: FAILED -- cancelling siblings\n",
					time.Since(start).Round(time.Millisecond), api.name)
				return
			}

			fmt.Printf("  [%v] %s: completed\n",
				time.Since(start).Round(time.Millisecond), api.name)
		}()
	}

	wg.Wait()
	elapsed := time.Since(start).Round(time.Millisecond)
	fmt.Printf("\nError: %v\n", firstErr)
	fmt.Printf("Total time: %v -- siblings cancelled shortly after the failure\n", elapsed)
}
```

**Expected output:**
```
=== With Context Cancellation ===
  [50ms] recent-orders: FAILED -- cancelling siblings
  [50ms] user-profile: cancelled while waiting
  [50ms] notifications: cancelled while waiting
  [50ms] recommendations: cancelled while waiting

Error: recent-orders: 503 service unavailable
Total time: 50ms -- siblings cancelled shortly after the failure
```

Total time dropped from 400ms to 50ms. The moment `recent-orders` fails and calls `cancel()`, the `select` statement in every other goroutine detects `ctx.Done()` and exits. No wasted connections, no lingering goroutines.

## Step 3 -- Goroutine Leak Without Cancellation

Goroutine leaks are a real production problem. When a goroutine blocks on a channel send or sleep and nobody cancels it, it stays alive forever, consuming memory:

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
	"time"
)

func main() {
	fmt.Println("=== Goroutine Leak Demo ===")
	fmt.Printf("Goroutines before: %d\n", runtime.NumGoroutine())

	leakyDashboardLoad()

	time.Sleep(50 * time.Millisecond)
	fmt.Printf("Goroutines after (leaky): %d -- leaked goroutines are still alive!\n", runtime.NumGoroutine())

	// In a real server, this happens on every request.
	// 1000 requests/sec * 3 leaked goroutines = 3000 leaked goroutines/sec.
	// The process eventually runs out of memory and crashes.
}

func leakyDashboardLoad() {
	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error

	results := make(chan string) // unbuffered channel -- receivers might never read

	apis := []string{"user-profile", "recent-orders", "notifications"}

	for _, api := range apis {
		api := api
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(200 * time.Millisecond) // simulate API call
			results <- fmt.Sprintf("data from %s", api) // BLOCKS if nobody reads
		}()
	}

	// Only read one result, then "give up" due to an error
	go func() {
		firstResult := <-results
		fmt.Printf("  Got: %s\n", firstResult)
		once.Do(func() {
			firstErr = fmt.Errorf("decided to abort after first result")
		})
	}()

	time.Sleep(300 * time.Millisecond) // simulate waiting
	_ = firstErr
	// The other 2 goroutines are stuck on `results <- ...` forever.
	// They cannot be garbage collected because the channel is still referenced.
}
```

**Expected output:**
```
=== Goroutine Leak Demo ===
Goroutines before: 1
  Got: data from user-profile
Goroutines after (leaky): 4 -- leaked goroutines are still alive!
```

The goroutine count increased by 3 (the API goroutines) and 2 of them are stuck forever trying to send on a channel that nobody reads. With context cancellation, each goroutine would check `ctx.Done()` before the channel send and exit cleanly.

## Step 4 -- Complete Dashboard Loader with Cancellation

Put it all together into a production-style dashboard loader. Each API fetch respects context cancellation. Pass `ctx` to any function that accepts it (like `http.NewRequestWithContext` in real code):

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type DashboardData struct {
	UserName       string
	OrderCount     int
	Notifications  int
	Recommendations []string
}

func main() {
	fmt.Println("=== Dashboard Data Loader ===")

	fmt.Println("\n--- Scenario 1: All APIs healthy ---")
	start := time.Now()
	data, err := loadDashboard(false)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Dashboard: user=%s, orders=%d, notifications=%d, recs=%d\n",
			data.UserName, data.OrderCount, data.Notifications, len(data.Recommendations))
	}
	fmt.Printf("Time: %v\n", time.Since(start).Round(time.Millisecond))

	fmt.Println("\n--- Scenario 2: Orders API failing ---")
	start = time.Now()
	data, err = loadDashboard(true)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}
	fmt.Printf("Time: %v (fast failure, no wasted work)\n", time.Since(start).Round(time.Millisecond))
}

func loadDashboard(ordersDown bool) (*DashboardData, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error

	var data DashboardData
	var mu sync.Mutex

	type apiCall struct {
		name string
		fn   func(context.Context) error
	}

	calls := []apiCall{
		{"user-profile", func(ctx context.Context) error {
			if err := simulateAPI(ctx, 100*time.Millisecond); err != nil {
				return fmt.Errorf("user-profile: %w", err)
			}
			mu.Lock()
			data.UserName = "alice"
			mu.Unlock()
			return nil
		}},
		{"recent-orders", func(ctx context.Context) error {
			if ordersDown {
				time.Sleep(40 * time.Millisecond)
				return fmt.Errorf("recent-orders: 503 service unavailable")
			}
			if err := simulateAPI(ctx, 80*time.Millisecond); err != nil {
				return fmt.Errorf("recent-orders: %w", err)
			}
			mu.Lock()
			data.OrderCount = 42
			mu.Unlock()
			return nil
		}},
		{"notifications", func(ctx context.Context) error {
			if err := simulateAPI(ctx, 120*time.Millisecond); err != nil {
				return fmt.Errorf("notifications: %w", err)
			}
			mu.Lock()
			data.Notifications = 7
			mu.Unlock()
			return nil
		}},
		{"recommendations", func(ctx context.Context) error {
			if err := simulateAPI(ctx, 150*time.Millisecond); err != nil {
				return fmt.Errorf("recommendations: %w", err)
			}
			mu.Lock()
			data.Recommendations = []string{"item-1", "item-2", "item-3"}
			mu.Unlock()
			return nil
		}},
	}

	for _, call := range calls {
		call := call
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := call.fn(ctx); err != nil {
				once.Do(func() {
					firstErr = err
					cancel()
				})
			}
		}()
	}

	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}
	return &data, nil
}

func simulateAPI(ctx context.Context, latency time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(latency):
		return nil
	}
}
```

**Expected output:**
```
=== Dashboard Data Loader ===

--- Scenario 1: All APIs healthy ---
Dashboard: user=alice, orders=42, notifications=7, recs=3
Time: 150ms

--- Scenario 2: Orders API failing ---
Error: recent-orders: 503 service unavailable
Time: 40ms (fast failure, no wasted work)
```

The `simulateAPI` function uses the same `select` pattern you would use with `http.NewRequestWithContext` in real code. When the context is cancelled, the simulated API call returns immediately with `context.Canceled`.

The `golang.org/x/sync/errgroup` package provides exactly this pattern via `errgroup.WithContext`:

```go
// With errgroup.WithContext, the same code becomes:
//   g, ctx := errgroup.WithContext(context.Background())
//   g.Go(func() error { return fetchUserProfile(ctx) })
//   g.Go(func() error { return fetchOrders(ctx) })
//   g.Go(func() error { return fetchNotifications(ctx) })
//   g.Go(func() error { return fetchRecommendations(ctx) })
//   err := g.Wait()
//
// No WaitGroup, no Once, no manual cancel(), no mutex for error capture.
// The context is automatically cancelled when the first Go() returns an error.
```

## Intermediate Verification

At this point, verify:
1. Without cancellation, total time equals the slowest API (~400ms)
2. With cancellation, total time equals the time to first failure (~50ms)
3. The goroutine leak demo shows goroutine count increasing
4. The complete dashboard loader returns in ~40ms when an API is down

## Common Mistakes

### Not checking ctx.Done() in goroutines

**Wrong:**
```go
go func() {
    defer wg.Done()
    time.Sleep(10 * time.Second) // blocks regardless of cancellation
    results <- data
}()
```

**What happens:** The context is cancelled but the goroutine does not notice. It runs the full 10 seconds, then tries to send on a channel that may already be abandoned. Context cancellation is cooperative -- goroutines must check.

**Fix:** Use `select` with `ctx.Done()`:
```go
go func() {
    defer wg.Done()
    select {
    case <-ctx.Done():
        return
    case <-time.After(10 * time.Second):
        results <- data
    }
}()
```

### Returning ctx.Err() when your task is the first to fail

**Wrong:**
```go
if somethingFailed {
    return ctx.Err() // might be nil if you are the first to fail!
}
```

**What happens:** If your goroutine is the first to fail, the context has not been cancelled yet. `ctx.Err()` returns nil. Your error is silently lost.

**Fix:** Return your own descriptive error. Only return `ctx.Err()` when reacting to a sibling's cancellation:
```go
if somethingFailed {
    return fmt.Errorf("orders API returned 503")
}
```

### Forgetting defer cancel() on the parent

**Wrong:**
```go
ctx, cancel := context.WithCancel(context.Background())
// no defer cancel()
// if loadDashboard returns early on error, cancel is never called
```

**What happens:** The context and its resources are never freed. The Go vet tool will warn about this.

**Fix:** Always `defer cancel()` immediately after creating the context.

## Verify What You Learned

Run the full program and confirm:
1. Without cancellation, all API calls complete even after a failure
2. With cancellation, sibling goroutines exit within milliseconds of the first failure
3. Goroutine leaks are visible in the goroutine count
4. The dashboard loader handles both success and failure scenarios correctly

```bash
go run main.go
```

## What's Next
Continue to [03-errgroup-setlimit](../03-errgroup-setlimit/03-errgroup-setlimit.md) to learn how to limit concurrency -- because checking 1000 services simultaneously would overwhelm your network.

## Summary
- Context cancellation prevents wasted work when one concurrent task fails
- Without cancellation, all goroutines run to completion even if the result is already known to be an error
- Cancellation is cooperative: goroutines must check `ctx.Done()` via `select` to respond
- Goroutine leaks happen when goroutines block on channel operations without context checks
- The pattern: `context.WithCancel` + `cancel()` in the error handler + `select` on `ctx.Done()` in each goroutine
- `golang.org/x/sync/errgroup.WithContext` provides this pattern built-in: the context is automatically cancelled when the first `Go()` returns a non-nil error
- Always `defer cancel()` to release context resources
- Return your own error for failures; return `ctx.Err()` only when reacting to sibling cancellation

## Reference
- [context.WithCancel documentation](https://pkg.go.dev/context#WithCancel)
- [Go Blog: Context](https://go.dev/blog/context)
- [errgroup.WithContext documentation](https://pkg.go.dev/golang.org/x/sync/errgroup#WithContext)
- [Go Concurrency Patterns: Context](https://go.dev/blog/context)
