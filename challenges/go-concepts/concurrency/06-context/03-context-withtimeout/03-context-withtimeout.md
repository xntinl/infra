---
difficulty: intermediate
concepts: [context.WithTimeout, automatic cancellation, ctx.Done, ctx.Err, DeadlineExceeded, defer cancel, API client]
tools: [go]
estimated_time: 25m
bloom_level: apply
---

# 3. Context WithTimeout

## Learning Objectives
After completing this exercise, you will be able to:
- **Create** a context that automatically cancels after a specified duration
- **Build** an API client with timeout protection against slow external services
- **Distinguish** between manual cancellation (`context.Canceled`) and timeout (`context.DeadlineExceeded`)
- **Detect** resource leaks caused by forgetting to call the cancel function

## Why WithTimeout

Every call to an external service -- a database, a REST API, a gRPC endpoint -- can hang. Network partitions, overloaded servers, and DNS failures can cause a simple HTTP call to block for minutes. Without a timeout, that goroutine holds a connection, memory, and a spot in your worker pool indefinitely. When hundreds of requests pile up waiting for a dead service, your entire system stops responding. This is a cascading failure.

`context.WithTimeout` creates a context that automatically cancels after a specified duration, even if nobody calls `cancel()` explicitly. This is the backbone of resilient systems. When you set a 2-second timeout on a database query, you guarantee that no matter what happens downstream, the goroutine will be freed within 2 seconds.

The cancel function returned by `WithTimeout` must still be deferred. Even if the timeout fires first, calling `cancel()` releases internal timer resources immediately instead of waiting for garbage collection.

## Step 1 -- API Client with Timeout

Build a client that calls an external payment verification service. If the service does not respond in 2 seconds, give up and return an error:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func verifyPayment(ctx context.Context, transactionID string) (string, error) {
	fmt.Printf("[payment-api] verifying transaction %s...\n", transactionID)

	// Simulate an external service that takes variable time.
	serviceLatency := 3 * time.Second // service is slow today

	select {
	case <-time.After(serviceLatency):
		return "verified", nil
	case <-ctx.Done():
		return "", fmt.Errorf("payment verification failed: %w", ctx.Err())
	}
}

func main() {
	// Timeout: if payment service does not respond in 2 seconds, give up.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	fmt.Println("Calling payment verification service (timeout: 2s)...")
	start := time.Now()

	result, err := verifyPayment(ctx, "TXN-2024-98765")
	elapsed := time.Since(start).Round(time.Millisecond)

	if err != nil {
		fmt.Printf("[error] %v (after %v)\n", err, elapsed)
		fmt.Println("[action] falling back to manual review queue")
	} else {
		fmt.Printf("[success] %s (after %v)\n", result, elapsed)
	}
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
Calling payment verification service (timeout: 2s)...
[payment-api] verifying transaction TXN-2024-98765...
[error] payment verification failed: context deadline exceeded (after 2s)
[action] falling back to manual review queue
```

The service needed 3 seconds but the context only allowed 2. After 2 seconds, `ctx.Done()` closed, the select picked up the cancellation, and `ctx.Err()` returned `context.DeadlineExceeded`. Without this timeout, the goroutine would block for the full 3 seconds -- or forever if the service is completely down.

## Step 2 -- Fast Response Completes Before Timeout

When the service responds within the timeout, everything proceeds normally. The deferred `cancel()` is still required to free internal timer resources:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func fetchUserProfile(ctx context.Context, userID string) (string, error) {
	serviceLatency := 200 * time.Millisecond // service is fast

	select {
	case <-time.After(serviceLatency):
		return fmt.Sprintf("User{id: %s, name: Alice, plan: premium}", userID), nil
	case <-ctx.Done():
		return "", fmt.Errorf("fetch user profile: %w", ctx.Err())
	}
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel() // Required even when timeout will not fire -- frees the timer.

	fmt.Println("Fetching user profile (timeout: 2s, expected latency: 200ms)...")
	start := time.Now()

	profile, err := fetchUserProfile(ctx, "user-42")
	elapsed := time.Since(start).Round(time.Millisecond)

	if err != nil {
		fmt.Printf("[error] %v (after %v)\n", err, elapsed)
	} else {
		fmt.Printf("[success] %s (after %v)\n", profile, elapsed)
	}

	// The context has not expired yet.
	fmt.Printf("Context error after success: %v (nil means timeout has not fired)\n", ctx.Err())
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
Fetching user profile (timeout: 2s, expected latency: 200ms)...
[success] User{id: user-42, name: Alice, plan: premium} (after 200ms)
Context error after success: <nil> (nil means timeout has not fired)
```

The operation finished in 200ms, well within the 2-second timeout. `ctx.Err()` is nil because the timeout has not fired yet. The deferred `cancel()` stops the internal timer immediately on function return.

## Step 3 -- Resource Leak When You Forget Cancel

This demonstrates what happens when you do not call `cancel()`. The internal timer goroutine leaks:

```go
package main

import (
	"context"
	"fmt"
	"runtime"
	"time"
)

func leakyTimeout() {
	// BAD: ignoring the cancel function.
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	_ = ctx
	// The timer goroutine runs for 10 seconds even though we are done.
}

func properTimeout() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel() // Stops the timer immediately.
	_ = ctx
}

func main() {
	baseline := runtime.NumGoroutine()
	fmt.Printf("Baseline goroutines: %d\n\n", baseline)

	fmt.Println("Creating 100 timeouts WITHOUT cancel...")
	for i := 0; i < 100; i++ {
		leakyTimeout()
	}
	leaked := runtime.NumGoroutine()
	fmt.Printf("Goroutines after leaky calls: %d (leaked: %d)\n\n", leaked, leaked-baseline)

	fmt.Println("Creating 100 timeouts WITH proper cancel...")
	for i := 0; i < 100; i++ {
		properTimeout()
	}
	proper := runtime.NumGoroutine()
	fmt.Printf("Goroutines after proper calls: %d (leaked from those: %d)\n", proper, proper-leaked)
	fmt.Println("\nThe leaky calls left timer goroutines running for 10 seconds each.")
	fmt.Println("In a server handling 1000 req/s, this consumes gigabytes of memory.")
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
Baseline goroutines: 1

Creating 100 timeouts WITHOUT cancel...
Goroutines after leaky calls: 101 (leaked: 100)

Creating 100 timeouts WITH proper cancel...
Goroutines after proper calls: 101 (leaked from those: 0)

The leaky calls left timer goroutines running for 10 seconds each.
In a server handling 1000 req/s, this consumes gigabytes of memory.
```

Each forgotten `cancel()` leaves a timer goroutine running until the timeout expires. In a long-running server, these accumulate and cause memory exhaustion.

## Step 4 -- Distinguishing Timeout vs Manual Cancellation

When diagnosing issues, you need to know whether an operation was cancelled by the caller or timed out on its own. `ctx.Err()` tells you which:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"time"
)

func callService(ctx context.Context, name string) error {
	select {
	case <-time.After(5 * time.Second): // Service takes 5s.
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func classifyError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "TIMEOUT: service too slow, consider increasing timeout or adding cache"
	}
	if errors.Is(err, context.Canceled) {
		return "CANCELLED: caller gave up (client disconnect, user abort)"
	}
	return "UNKNOWN"
}

func main() {
	// Case 1: Timeout fires.
	fmt.Println("=== Case 1: Timeout ===")
	ctx1, cancel1 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel1()

	err1 := callService(ctx1, "inventory")
	fmt.Printf("Error: %v\n", err1)
	fmt.Printf("Diagnosis: %s\n\n", classifyError(err1))

	// Case 2: Manual cancellation before timeout.
	fmt.Println("=== Case 2: Manual Cancel ===")
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)

	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel2() // User disconnected.
	}()

	err2 := callService(ctx2, "inventory")
	fmt.Printf("Error: %v\n", err2)
	fmt.Printf("Diagnosis: %s\n", classifyError(err2))
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
=== Case 1: Timeout ===
Error: context deadline exceeded
Diagnosis: TIMEOUT: service too slow, consider increasing timeout or adding cache

=== Case 2: Manual Cancel ===
Error: context canceled
Diagnosis: CANCELLED: caller gave up (client disconnect, user abort)
```

This distinction drives real decisions: timeouts trigger alerts about slow dependencies; cancellations are usually normal (clients disconnecting) and should not page the on-call engineer. Use `errors.Is(err, context.DeadlineExceeded)` vs `errors.Is(err, context.Canceled)` to classify them in your logging and metrics.

## Step 5 -- Child Cannot Extend Parent Timeout

A fundamental rule: a child context cannot have a longer timeout than its parent. The shorter deadline always wins. This prevents a downstream layer from circumventing the caller's budget:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func main() {
	// API gateway sets a 1-second budget for the entire request.
	gateway, cancelGateway := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancelGateway()

	// Service layer tries to give the database 10 seconds. It will not work.
	dbQuery, cancelDB := context.WithTimeout(gateway, 10*time.Second)
	defer cancelDB()

	gatewayDeadline, _ := gateway.Deadline()
	dbDeadline, _ := dbQuery.Deadline()

	fmt.Printf("Gateway deadline: %v (1s from now)\n",
		time.Until(gatewayDeadline).Round(time.Millisecond))
	fmt.Printf("DB query requested: 10s\n")
	fmt.Printf("DB query actual:    %v (inherits gateway's shorter deadline)\n",
		time.Until(dbDeadline).Round(time.Millisecond))
	fmt.Println("\nYou can tighten a timeout (shorter) but never loosen it (longer).")
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
Gateway deadline: 1s (1s from now)
DB query requested: 10s
DB query actual:    1s (inherits gateway's shorter deadline)

You can tighten a timeout (shorter) but never loosen it (longer).
```

## Common Mistakes

### Not Deferring Cancel on WithTimeout
**Wrong:**
```go
package main

import (
	"context"
	"fmt"
	"time"
)

func main() {
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	// forgot defer cancel() -- timer goroutine leaks until timeout fires!
	fmt.Printf("ctx.Err(): %v\n", ctx.Err())
}
```
**What happens:** The internal timer runs for the full 5 seconds even if the operation finishes in 10 milliseconds. In a server handling thousands of requests, timer goroutines pile up.

**Fix:** Always `defer cancel()` immediately:
```go
package main

import (
	"context"
	"fmt"
	"time"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	fmt.Printf("ctx.Err(): %v\n", ctx.Err())
}
```

### Ignoring ctx.Err() After Timeout
**Wrong:**
```go
select {
case <-ctx.Done():
    return nil // caller has no idea what went wrong
}
```
**Fix:**
```go
select {
case <-ctx.Done():
    return fmt.Errorf("operation failed: %w", ctx.Err())
}
```

Always wrap and return `ctx.Err()` so callers can distinguish timeout from cancellation and make appropriate retry or fallback decisions.

### Setting Timeout Longer Than Parent's
As shown in Step 5, setting a child timeout longer than the parent's is not an error, but it is misleading code. The child inherits the parent's shorter deadline, and the longer timeout has no effect. This confuses developers reading the code.

## Verify What You Learned

Build an API client that calls two services: a fast user service and a slow recommendation service. Set appropriate timeouts for each. Verify that the fast service succeeds and the slow one times out:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func callService(ctx context.Context, name string, latency time.Duration) (string, error) {
	select {
	case <-time.After(latency):
		return fmt.Sprintf("%s: OK", name), nil
	case <-ctx.Done():
		return "", fmt.Errorf("%s: %w", name, ctx.Err())
	}
}

func main() {
	// Fast service: 100ms latency, 500ms timeout. Should succeed.
	ctx1, cancel1 := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel1()
	result, err := callService(ctx1, "user-service", 100*time.Millisecond)
	if err != nil {
		fmt.Printf("[FAIL] %v\n", err)
	} else {
		fmt.Printf("[OK]   %s\n", result)
	}

	// Slow service: 2s latency, 300ms timeout. Should timeout.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel2()
	result, err = callService(ctx2, "recommendation-service", 2*time.Second)
	if err != nil {
		fmt.Printf("[FAIL] %v\n", err)
	} else {
		fmt.Printf("[OK]   %s\n", result)
	}
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
[OK]   user-service: OK
[FAIL] recommendation-service: context deadline exceeded
```

## What's Next
Continue to [04-context-withdeadline](../04-context-withdeadline/04-context-withdeadline.md) to learn about absolute deadlines and how to enforce SLA requirements across multiple processing stages.

## Summary
- `context.WithTimeout(parent, duration)` creates a context that auto-cancels after the duration
- The `Done()` channel closes when the timeout fires or when `cancel()` is called manually
- `ctx.Err()` returns `context.DeadlineExceeded` for timeouts, `context.Canceled` for manual cancellation
- Always `defer cancel()` even with `WithTimeout` -- it frees the internal timer immediately
- Forgetting `cancel()` leaks a timer goroutine per call -- catastrophic at scale
- A child context cannot extend its parent's deadline -- the shorter deadline always wins
- Use timeouts on every call to external systems (databases, APIs, RPCs)

## Reference
- [Package context: WithTimeout](https://pkg.go.dev/context#WithTimeout)
- [Go Blog: Context](https://go.dev/blog/context)
- [Dave Cheney: Context is for Cancellation](https://dave.cheney.net/2017/08/20/context-isnt-for-cancellation)
