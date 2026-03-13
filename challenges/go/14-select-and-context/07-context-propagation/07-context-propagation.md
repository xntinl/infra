# 7. Context Propagation

<!--
difficulty: intermediate
concepts: [context-propagation, context-chain, request-lifecycle, layered-architecture, context-best-practices]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [context-withcancel, context-withtimeout-withdeadline, context-withvalue]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [06 - Context WithValue](../06-context-withvalue/06-context-withvalue.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** context propagation through multiple layers of an application
- **Implement** functions that correctly pass context to downstream calls
- **Design** request lifecycles where cancellation flows through the entire call chain

## Why Context Propagation

Context is only useful if it flows through the entire call chain. When an HTTP handler creates a context with a 5-second timeout, every function it calls -- service layer, repository, external API client -- must receive and respect that context. If any layer ignores the context, cancellation does not propagate, and the timeout is ineffective.

The rule is simple: every function that calls another function that accepts `context.Context` must pass its own context forward. Never create a new `context.Background()` in the middle of a call chain.

## Step 1 -- Context Flows Through Layers

```bash
mkdir -p ~/go-exercises/context-propagation && cd ~/go-exercises/context-propagation
go mod init context-propagation
```

Create `main.go`:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

// Repository layer
func fetchFromDB(ctx context.Context, query string) (string, error) {
	fmt.Println("  db: executing query:", query)
	select {
	case <-time.After(200 * time.Millisecond):
		return "db-result", nil
	case <-ctx.Done():
		fmt.Println("  db: query cancelled")
		return "", ctx.Err()
	}
}

// Service layer
func getUserProfile(ctx context.Context, userID string) (string, error) {
	fmt.Println(" service: fetching profile for", userID)
	result, err := fetchFromDB(ctx, "SELECT * FROM users WHERE id="+userID)
	if err != nil {
		return "", fmt.Errorf("getUserProfile: %w", err)
	}
	return "profile:" + result, nil
}

// Handler layer
func handleRequest(ctx context.Context, userID string) {
	fmt.Println("handler: processing request")
	profile, err := getUserProfile(ctx, userID)
	if err != nil {
		fmt.Println("handler: error:", err)
		return
	}
	fmt.Println("handler: got", profile)
}

func main() {
	// Simulate a request with a 500ms timeout
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	handleRequest(ctx, "42")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
handler: processing request
 service: fetching profile for 42
  db: executing query: SELECT * FROM users WHERE id=42
handler: got profile:db-result
```

## Step 2 -- Cancellation Propagates Down

Reduce the timeout to see cancellation flow through all layers:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func fetchFromDB(ctx context.Context, query string) (string, error) {
	fmt.Println("  db: starting query")
	select {
	case <-time.After(500 * time.Millisecond): // slow query
		return "result", nil
	case <-ctx.Done():
		fmt.Println("  db: cancelled:", ctx.Err())
		return "", ctx.Err()
	}
}

func callExternalAPI(ctx context.Context, endpoint string) (string, error) {
	fmt.Println("  api: calling", endpoint)
	select {
	case <-time.After(300 * time.Millisecond):
		return "api-data", nil
	case <-ctx.Done():
		fmt.Println("  api: cancelled:", ctx.Err())
		return "", ctx.Err()
	}
}

func buildResponse(ctx context.Context) (string, error) {
	fmt.Println(" service: building response")

	dbResult, err := fetchFromDB(ctx, "SELECT ...")
	if err != nil {
		return "", fmt.Errorf("db failed: %w", err)
	}

	apiResult, err := callExternalAPI(ctx, "/enrichment")
	if err != nil {
		return "", fmt.Errorf("api failed: %w", err)
	}

	return dbResult + " + " + apiResult, nil
}

func main() {
	// Only 400ms -- not enough for DB (500ms) + API (300ms)
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	result, err := buildResponse(ctx)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("result:", result)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
 service: building response
  db: starting query
  db: cancelled: context deadline exceeded
error: db failed: context deadline exceeded
```

The DB query takes 500ms but the context expires at 400ms. The cancellation propagates up through the error return.

## Step 3 -- Parallel Calls with Shared Context

When making concurrent calls, all share the same context for coordinated cancellation:

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

func fetchService(ctx context.Context, name string, delay time.Duration) (string, error) {
	select {
	case <-time.After(delay):
		return name + "-data", nil
	case <-ctx.Done():
		return "", fmt.Errorf("%s: %w", name, ctx.Err())
	}
}

func aggregateData(ctx context.Context) (map[string]string, error) {
	type result struct {
		name string
		data string
		err  error
	}

	services := map[string]time.Duration{
		"users":    100 * time.Millisecond,
		"orders":   200 * time.Millisecond,
		"products": 150 * time.Millisecond,
	}

	results := make(chan result, len(services))
	var wg sync.WaitGroup

	for name, delay := range services {
		wg.Add(1)
		go func(n string, d time.Duration) {
			defer wg.Done()
			data, err := fetchService(ctx, n, d)
			results <- result{name: n, data: data, err: err}
		}(name, delay)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	aggregated := make(map[string]string)
	for r := range results {
		if r.err != nil {
			return nil, r.err
		}
		aggregated[r.name] = r.data
	}
	return aggregated, nil
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	data, err := aggregateData(ctx)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	for k, v := range data {
		fmt.Printf("%s: %s\n", k, v)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (order varies):

```
users: users-data
orders: orders-data
products: products-data
```

## Step 4 -- Breaking the Chain (Anti-Pattern)

See what happens when a layer creates a new background context instead of propagating:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func brokenLayer(ctx context.Context) (string, error) {
	// BUG: creates new context, ignoring parent's cancellation
	newCtx := context.Background()
	_ = ctx // parent context ignored!
	return slowWork(newCtx)
}

func correctLayer(ctx context.Context) (string, error) {
	// CORRECT: passes parent context through
	return slowWork(ctx)
}

func slowWork(ctx context.Context) (string, error) {
	select {
	case <-time.After(2 * time.Second):
		return "done", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// The broken layer ignores the timeout -- it will NOT cancel
	fmt.Println("broken layer (should timeout but won't):")
	start := time.Now()
	result, err := brokenLayer(ctx)
	fmt.Printf("  result=%s, err=%v, took=%v\n", result, err, time.Since(start).Truncate(time.Millisecond))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
broken layer (should timeout but won't):
  result=done, err=<nil>, took=2000ms
```

The 200ms timeout was completely ignored because the chain was broken. This is a common and dangerous bug.

## Step 5 -- context.WithoutCancel (Go 1.21+)

Sometimes you need to break the cancellation chain intentionally -- for example, to perform cleanup after a request is cancelled:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func cleanup(ctx context.Context) {
	// This runs even after the parent is cancelled
	select {
	case <-time.After(100 * time.Millisecond):
		fmt.Println("cleanup: completed")
	case <-ctx.Done():
		fmt.Println("cleanup: interrupted (should not happen)")
	}
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)

	// Wait for timeout
	<-ctx.Done()
	fmt.Println("request timed out:", ctx.Err())

	// Use WithoutCancel so cleanup is not affected by parent cancellation
	cleanupCtx := context.WithoutCancel(ctx)
	cleanup(cleanupCtx)

	cancel()
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
request timed out: context deadline exceeded
cleanup: completed
```

## Common Mistakes

### Creating context.Background() Mid-Chain

Never create a new root context inside a service or repository function. Always propagate the context received from the caller.

### Storing Context in a Struct

**Wrong:**

```go
type Service struct {
    ctx context.Context // anti-pattern
}
```

**Fix:** Pass context as the first parameter of each method:

```go
func (s *Service) Do(ctx context.Context) error { ... }
```

### Ignoring Context in Loops

If a function loops, check `ctx.Done()` on each iteration. Otherwise a cancelled context is only noticed after the next I/O call.

## Verify What You Learned

Build a three-layer application (handler, service, repository) where:
1. The handler sets a 1-second timeout
2. The service makes two sequential repository calls (each takes 400ms)
3. Run it and observe that both calls succeed (800ms total, under 1s)
4. Change the timeout to 600ms and observe the second call gets cancelled
5. Verify the error message includes the layer that detected the cancellation

## What's Next

Continue to [08 - Select Priority and Starvation](../08-select-priority-and-starvation/08-select-priority-and-starvation.md) to explore how `select` handles priority between channels and how to prevent starvation.

## Summary

- Context must flow through every layer of the application
- Never create `context.Background()` in the middle of a call chain
- Cancellation propagates downward automatically through derived contexts
- Parallel calls sharing a context are all cancelled together
- Pass context as the first function parameter, never store it in a struct
- Use `context.WithoutCancel` (Go 1.21+) only for intentional decoupling like cleanup work

## Reference

- [Go Blog: Go Concurrency Patterns: Context](https://go.dev/blog/context)
- [Go Blog: Contexts and structs](https://go.dev/blog/context-and-structs)
- [context.WithoutCancel](https://pkg.go.dev/context#WithoutCancel)
