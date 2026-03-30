---
difficulty: intermediate
concepts: [context propagation, middleware chain, layered architecture, multi-layer cancellation, request lifecycle]
tools: [go]
estimated_time: 35m
bloom_level: apply
---

# 6. Context Propagation Chain

## Learning Objectives
After completing this exercise, you will be able to:
- **Build** a complete request flow through middleware layers: auth, rate limiter, handler, service, database
- **Propagate** context through every layer so cancellation at any point stops all downstream operations
- **Combine** context values and cancellation across a realistic multi-layer chain
- **Observe** the real consequence of breaking the chain with `context.Background()`

## Why Context Propagation

In a production Go web server, a single HTTP request flows through multiple middleware layers before reaching business logic:

```
Client -> Auth Middleware -> Rate Limiter -> Handler -> Service -> Database
```

Each layer may add context values (request ID, authenticated user), check context (rate limit exceeded?), or derive new contexts (add a per-query timeout). When the client disconnects or a timeout fires, the cancellation signal must propagate through the ENTIRE chain. If any layer breaks the chain by creating its own `context.Background()`, all downstream work continues uselessly -- wasting database connections, CPU, and memory.

This exercise builds the pattern you will use in every Go web server: middleware enriches context, handlers pass it to services, services pass it to repositories, and cancellation flows from top to bottom.

## Step 1 -- Define the Middleware and Service Layers

Build a complete request flow with five layers. Each layer accepts context, does its work, and passes context to the next layer:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

type requestIDKey struct{}
type userIDKey struct{}
type rateLimitKey struct{}

func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

func log(ctx context.Context, layer, msg string) {
	fmt.Printf("[%-12s] req=%s | %s\n", layer, requestIDFrom(ctx), msg)
}

// Layer 1: Auth middleware -- validates token and adds user ID to context.
func authMiddleware(ctx context.Context, token string) (context.Context, error) {
	log(ctx, "auth", "validating token")
	time.Sleep(30 * time.Millisecond)

	if token == "" {
		return ctx, fmt.Errorf("auth: missing token")
	}
	if token != "valid-token-xyz" {
		return ctx, fmt.Errorf("auth: invalid token")
	}

	ctx = context.WithValue(ctx, userIDKey{}, "user-42")
	log(ctx, "auth", "authenticated as user-42")
	return ctx, nil
}

// Layer 2: Rate limiter -- checks if user has exceeded rate limit.
func rateLimiter(ctx context.Context) (context.Context, error) {
	userID, _ := ctx.Value(userIDKey{}).(string)
	log(ctx, "rate-limiter", fmt.Sprintf("checking rate for %s", userID))
	time.Sleep(10 * time.Millisecond)

	ctx = context.WithValue(ctx, rateLimitKey{}, "50/min")
	log(ctx, "rate-limiter", "within limits (50/min)")
	return ctx, nil
}

// Layer 3: Handler -- orchestrates business logic.
func handler(ctx context.Context, orderID string) (string, error) {
	log(ctx, "handler", fmt.Sprintf("processing order %s", orderID))

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("handler: %w", ctx.Err())
	default:
	}

	return orderService(ctx, orderID)
}

// Layer 4: Service -- business logic.
func orderService(ctx context.Context, orderID string) (string, error) {
	log(ctx, "service", "validating business rules")
	time.Sleep(40 * time.Millisecond)

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("service: %w", ctx.Err())
	default:
	}

	return queryDatabase(ctx, orderID)
}

// Layer 5: Database query.
func queryDatabase(ctx context.Context, orderID string) (string, error) {
	log(ctx, "database", fmt.Sprintf("SELECT * FROM orders WHERE id = '%s'", orderID))

	select {
	case <-time.After(80 * time.Millisecond):
		result := fmt.Sprintf("Order{id: %s, status: processing, user: %s}",
			orderID, ctx.Value(userIDKey{}))
		log(ctx, "database", "query complete")
		return result, nil
	case <-ctx.Done():
		log(ctx, "database", fmt.Sprintf("query cancelled: %v", ctx.Err()))
		return "", fmt.Errorf("database: %w", ctx.Err())
	}
}

func main() {
	// Root context with request ID and 1-second timeout.
	ctx := context.WithValue(context.Background(), requestIDKey{}, "req-7f3a")
	ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	fmt.Println("=== Request Flow (budget: 1s) ===\n")

	// Flow through all layers.
	ctx, err := authMiddleware(ctx, "valid-token-xyz")
	if err != nil {
		fmt.Printf("REJECTED: %v\n", err)
		return
	}

	ctx, err = rateLimiter(ctx)
	if err != nil {
		fmt.Printf("REJECTED: %v\n", err)
		return
	}

	result, err := handler(ctx, "ORD-2024-5678")
	if err != nil {
		fmt.Printf("\nFAILED: %v\n", err)
		return
	}

	fmt.Printf("\nSUCCESS: %s\n", result)
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
=== Request Flow (budget: 1s) ===

[auth        ] req=req-7f3a | validating token
[auth        ] req=req-7f3a | authenticated as user-42
[rate-limiter] req=req-7f3a | checking rate for user-42
[rate-limiter] req=req-7f3a | within limits (50/min)
[handler     ] req=req-7f3a | processing order ORD-2024-5678
[service     ] req=req-7f3a | validating business rules
[database    ] req=req-7f3a | SELECT * FROM orders WHERE id = 'ORD-2024-5678'
[database    ] req=req-7f3a | query complete

SUCCESS: Order{id: ORD-2024-5678, status: processing, user: user-42}
```

Total: auth(30ms) + rate-limiter(10ms) + service(40ms) + database(80ms) = 160ms, well within the 1-second budget. The request ID is visible at every layer, and context flows through the entire chain.

## Step 2 -- Timeout Cancels All Downstream Layers

Reduce the timeout so cancellation happens during the database query, and observe how it propagates back through every layer:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

type requestIDKey struct{}

func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

func authMiddleware(ctx context.Context) (context.Context, error) {
	fmt.Printf("[auth]       req=%s processing\n", requestIDFrom(ctx))
	time.Sleep(30 * time.Millisecond)
	return ctx, nil
}

func rateLimiter(ctx context.Context) error {
	fmt.Printf("[rate-limit] req=%s checking\n", requestIDFrom(ctx))
	time.Sleep(10 * time.Millisecond)
	return nil
}

func handler(ctx context.Context) (string, error) {
	fmt.Printf("[handler]    req=%s starting\n", requestIDFrom(ctx))
	return service(ctx)
}

func service(ctx context.Context) (string, error) {
	fmt.Printf("[service]    req=%s business logic\n", requestIDFrom(ctx))
	time.Sleep(40 * time.Millisecond)
	return database(ctx)
}

func database(ctx context.Context) (string, error) {
	fmt.Printf("[database]   req=%s executing query (needs 200ms)\n", requestIDFrom(ctx))
	select {
	case <-time.After(200 * time.Millisecond):
		return "data", nil
	case <-ctx.Done():
		fmt.Printf("[database]   req=%s CANCELLED: %v\n", requestIDFrom(ctx), ctx.Err())
		return "", fmt.Errorf("database: %w", ctx.Err())
	}
}

func main() {
	// Only 100ms budget for: auth(30ms) + rate(10ms) + service(40ms) + db(200ms) = 280ms
	ctx := context.WithValue(context.Background(), requestIDKey{}, "req-timeout-demo")
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	fmt.Println("=== Request Flow (budget: 100ms, needs: 280ms) ===\n")

	ctx, _ = authMiddleware(ctx)
	_ = rateLimiter(ctx)
	result, err := handler(ctx)
	if err != nil {
		fmt.Printf("\nRequest failed: %v\n", err)
		fmt.Println("The timeout propagated through handler -> service -> database")
	} else {
		fmt.Printf("Result: %s\n", result)
	}
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
=== Request Flow (budget: 100ms, needs: 280ms) ===

[auth]       req=req-timeout-demo processing
[rate-limit] req=req-timeout-demo checking
[handler]    req=req-timeout-demo starting
[service]    req=req-timeout-demo business logic
[database]   req=req-timeout-demo executing query (needs 200ms)
[database]   req=req-timeout-demo CANCELLED: context deadline exceeded

Request failed: database: context deadline exceeded
The timeout propagated through handler -> service -> database
```

The timeout fires during the database query. Because every layer uses the same context, the cancellation signal reaches the deepest layer immediately. The error propagates back up through the chain.

## Step 3 -- What Happens When the Chain Breaks

This is the anti-pattern that causes real production incidents. One layer creates its own `context.Background()`, disconnecting downstream layers from the caller's context:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

type requestIDKey struct{}

func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	if id == "" {
		return "MISSING"
	}
	return id
}

func handler(ctx context.Context) (string, error) {
	fmt.Printf("[handler]  req=%s starting\n", requestIDFrom(ctx))
	return brokenService(ctx)
}

// BROKEN: creates its own context instead of using the caller's.
func brokenService(ctx context.Context) (string, error) {
	fmt.Printf("[service]  req=%s (BROKEN: creating new Background)\n", requestIDFrom(ctx))

	// This breaks the chain. The database has no connection to the caller.
	newCtx := context.Background()
	return database(newCtx)
}

func database(ctx context.Context) (string, error) {
	fmt.Printf("[database] req=%s executing slow query...\n", requestIDFrom(ctx))
	select {
	case <-time.After(500 * time.Millisecond):
		fmt.Printf("[database] req=%s query finished (but caller already gave up!)\n",
			requestIDFrom(ctx))
		return "data", nil
	case <-ctx.Done():
		fmt.Printf("[database] req=%s cancelled\n", requestIDFrom(ctx))
		return "", ctx.Err()
	}
}

func main() {
	ctx := context.WithValue(context.Background(), requestIDKey{}, "req-broken-chain")
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	fmt.Println("=== Broken Chain: service creates new Background() ===\n")

	done := make(chan struct{})
	go func() {
		result, err := handler(ctx)
		if err != nil {
			fmt.Printf("\nResult: error=%v\n", err)
		} else {
			fmt.Printf("\nResult: %s\n", result)
		}
		close(done)
	}()

	// The caller's timeout fires, but the database keeps running.
	<-ctx.Done()
	fmt.Printf("\n[caller] timeout fired at 100ms, but database is STILL running...\n")

	<-done // Wait for the database to finish its wasted work.
	fmt.Println("\n[caller] database finally finished 400ms AFTER the caller gave up.")
	fmt.Println("[caller] That was a wasted database connection, CPU, and 400ms of work.")
	fmt.Println("[caller] FIX: pass ctx through the service instead of creating Background().")
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
=== Broken Chain: service creates new Background() ===

[handler]  req=req-broken-chain starting
[service]  req=req-broken-chain (BROKEN: creating new Background)
[database] req=MISSING executing slow query...

[caller] timeout fired at 100ms, but database is STILL running...
[database] req=MISSING query finished (but caller already gave up!)

Result: data

[caller] database finally finished 400ms AFTER the caller gave up.
[caller] That was a wasted database connection, CPU, and 400ms of work.
[caller] FIX: pass ctx through the service instead of creating Background().
```

Two problems: (1) the request ID is lost because `Background()` has no values, and (2) the database keeps running for 500ms even though the caller gave up after 100ms. In a real system, this wastes a database connection that could serve another request.

## Step 4 -- Complete Middleware Chain with Auth Rejection

Show the full pattern including early rejection. If auth fails, no downstream layer runs:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

type requestIDKey struct{}
type userIDKey struct{}

func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

func log(ctx context.Context, layer, msg string) {
	fmt.Printf("[%-12s] req=%s | %s\n", layer, requestIDFrom(ctx), msg)
}

func authMiddleware(ctx context.Context, token string) (context.Context, error) {
	log(ctx, "auth", fmt.Sprintf("checking token: %s...", token[:10]))
	if token != "valid-token" {
		log(ctx, "auth", "REJECTED: invalid token")
		return ctx, fmt.Errorf("auth: invalid token")
	}
	ctx = context.WithValue(ctx, userIDKey{}, "user-42")
	log(ctx, "auth", "OK")
	return ctx, nil
}

func rateLimiter(ctx context.Context) error {
	log(ctx, "rate-limiter", "checking quota")
	time.Sleep(5 * time.Millisecond)
	log(ctx, "rate-limiter", "OK (45/50 remaining)")
	return nil
}

func businessHandler(ctx context.Context) (string, error) {
	log(ctx, "handler", "processing")
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("handler: %w", ctx.Err())
	case <-time.After(50 * time.Millisecond):
	}
	return databaseQuery(ctx)
}

func databaseQuery(ctx context.Context) (string, error) {
	log(ctx, "database", "executing query")
	select {
	case <-time.After(60 * time.Millisecond):
		log(ctx, "database", "complete")
		return "Order{status: confirmed}", nil
	case <-ctx.Done():
		return "", fmt.Errorf("database: %w", ctx.Err())
	}
}

func processRequest(reqID string, token string, timeout time.Duration) {
	ctx := context.WithValue(context.Background(), requestIDKey{}, reqID)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ctx, err := authMiddleware(ctx, token)
	if err != nil {
		log(ctx, "response", fmt.Sprintf("401: %v", err))
		return
	}

	if err := rateLimiter(ctx); err != nil {
		log(ctx, "response", fmt.Sprintf("429: %v", err))
		return
	}

	result, err := businessHandler(ctx)
	if err != nil {
		log(ctx, "response", fmt.Sprintf("500: %v", err))
		return
	}

	log(ctx, "response", fmt.Sprintf("200: %s", result))
}

func main() {
	fmt.Println("=== Request 1: Happy path ===")
	processRequest("req-001", "valid-token", 1*time.Second)

	fmt.Println("\n=== Request 2: Auth failure ===")
	processRequest("req-002", "bad-token!!", 1*time.Second)

	fmt.Println("\n=== Request 3: Timeout ===")
	processRequest("req-003", "valid-token", 80*time.Millisecond)
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
=== Request 1: Happy path ===
[auth        ] req=req-001 | checking token: valid-toke...
[auth        ] req=req-001 | OK
[rate-limiter] req=req-001 | checking quota
[rate-limiter] req=req-001 | OK (45/50 remaining)
[handler     ] req=req-001 | processing
[database    ] req=req-001 | executing query
[database    ] req=req-001 | complete
[response    ] req=req-001 | 200: Order{status: confirmed}

=== Request 2: Auth failure ===
[auth        ] req=req-002 | checking token: bad-token!...
[auth        ] req=req-002 | REJECTED: invalid token
[response    ] req=req-002 | 401: auth: invalid token

=== Request 3: Timeout ===
[auth        ] req=req-003 | checking token: valid-toke...
[auth        ] req=req-003 | OK
[rate-limiter] req=req-003 | checking quota
[rate-limiter] req=req-003 | OK (45/50 remaining)
[handler     ] req=req-003 | processing
[response    ] req=req-003 | 500: handler: context deadline exceeded
```

Three scenarios: success, early rejection at auth, and timeout during processing. In all cases, context carries the request ID through every layer, and no downstream work runs after a failure.

## Common Mistakes

### Breaking the Chain with context.Background()
**Wrong:**
```go
func service(ctx context.Context, id string) (string, error) {
    newCtx := context.Background() // breaks the chain
    return repository(newCtx, id)
}
```
**Fix:** Always derive from the incoming context:
```go
func service(ctx context.Context, id string) (string, error) {
    return repository(ctx, id) // propagates caller's cancellation and values
}
```

### Not Checking Context in Each Layer
Each layer should check `ctx.Err()` or `ctx.Done()` before starting work. If the context is already cancelled when a layer is entered, proceeding wastes resources:
```go
func service(ctx context.Context) (string, error) {
    if ctx.Err() != nil {
        return "", fmt.Errorf("service: %w", ctx.Err())
    }
    // proceed...
}
```

### Wrapping Errors Without Layer Identification
**Wrong:**
```go
return "", err // caller has no idea which layer failed
```
**Fix:**
```go
return "", fmt.Errorf("service: %w", err) // clear error chain
```

## Verify What You Learned

Build a 4-layer chain (gateway -> auth -> handler -> storage) and test it with two scenarios: a 500ms budget (all layers complete) and a 150ms budget (times out at storage):

```go
package main

import (
	"context"
	"fmt"
	"time"
)

type reqIDKey struct{}

func reqIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(reqIDKey{}).(string)
	return id
}

func runRequest(reqID string, budget time.Duration) {
	ctx := context.WithValue(context.Background(), reqIDKey{}, reqID)
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	layers := []struct {
		name string
		work time.Duration
	}{
		{"gateway", 30 * time.Millisecond},
		{"auth", 20 * time.Millisecond},
		{"handler", 40 * time.Millisecond},
		{"storage", 100 * time.Millisecond},
	}

	for _, l := range layers {
		fmt.Printf("[%-8s] req=%s processing\n", l.name, reqIDFrom(ctx))
		select {
		case <-time.After(l.work):
		case <-ctx.Done():
			fmt.Printf("[%-8s] req=%s CANCELLED: %v\n", l.name, reqIDFrom(ctx), ctx.Err())
			return
		}
	}
	fmt.Printf("[done]    req=%s all layers complete\n", reqIDFrom(ctx))
}

func main() {
	fmt.Println("=== 500ms budget (needs 190ms) ===")
	runRequest("req-ok", 500*time.Millisecond)

	fmt.Println("\n=== 100ms budget (needs 190ms) ===")
	runRequest("req-tight", 100*time.Millisecond)
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
=== 500ms budget (needs 190ms) ===
[gateway ] req=req-ok processing
[auth    ] req=req-ok processing
[handler ] req=req-ok processing
[storage ] req=req-ok processing
[done]    req=req-ok all layers complete

=== 100ms budget (needs 190ms) ===
[gateway ] req=req-tight processing
[auth    ] req=req-tight processing
[handler ] req=req-tight processing
[storage ] req=req-tight CANCELLED: context deadline exceeded
```

## What's Next
Continue to [07-context-aware-long-worker](../07-context-aware-long-worker/07-context-aware-long-worker.md) to learn how to build report generators and file processors that respect context cancellation mid-operation.

## Summary
- Context must propagate through every layer: middleware -> handler -> service -> repository
- Breaking the chain with `context.Background()` silently disables cancellation and loses context values
- Middleware enriches context (auth token, request ID, rate limit info); handlers and services consume it
- Cancellation at any layer propagates to all downstream layers through the shared context
- Early rejection (auth failure, rate limit) prevents downstream work from starting
- Each layer should check context before doing work and wrap errors with layer identification
- This pattern (middleware chain -> handler -> service -> repository) is the standard in production Go web servers

## Reference
- [Go Blog: Context](https://go.dev/blog/context)
- [Go Code Review: Context](https://go.dev/wiki/CodeReviewComments#contexts)
- [Package context](https://pkg.go.dev/context)
