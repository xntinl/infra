# 6. Context WithValue

<!--
difficulty: intermediate
concepts: [context-withvalue, request-scoped-data, context-key-type, middleware-values]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [context-withcancel, context-withtimeout-withdeadline]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [05 - Context WithTimeout and WithDeadline](../05-context-withtimeout-withdeadline/05-context-withtimeout-withdeadline.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** `context.WithValue` to attach request-scoped data to a context
- **Implement** type-safe context keys using unexported types
- **Evaluate** when to use context values versus function parameters

## Why context.WithValue

Contexts carry two things: cancellation signals and request-scoped values. `context.WithValue(parent, key, val)` creates a new context that carries a key-value pair. Downstream functions can retrieve the value with `ctx.Value(key)`.

Context values are designed for data that crosses API boundaries and process boundaries -- request IDs, authentication tokens, trace spans, and similar metadata. They are NOT for passing optional parameters or replacing function arguments.

The key distinction: if removing a context value changes program correctness, it should be a function parameter instead. Context values should be invisible to the core logic and only used by cross-cutting concerns like logging, tracing, and auth.

## Step 1 -- Basic WithValue

```bash
mkdir -p ~/go-exercises/context-value && cd ~/go-exercises/context-value
go mod init context-value
```

Create `main.go`:

```go
package main

import (
	"context"
	"fmt"
)

func main() {
	ctx := context.Background()
	ctx = context.WithValue(ctx, "user", "alice")
	ctx = context.WithValue(ctx, "requestID", "abc-123")

	processRequest(ctx)
}

func processRequest(ctx context.Context) {
	user := ctx.Value("user")
	reqID := ctx.Value("requestID")

	fmt.Printf("processing request %v for user %v\n", reqID, user)

	// Missing key returns nil
	role := ctx.Value("role")
	fmt.Printf("role: %v (nil means not set)\n", role)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
processing request abc-123 for user alice
role: <nil> (nil means not set)
```

## Step 2 -- Type-Safe Keys

Using string keys is fragile -- different packages might use the same string. The idiomatic approach uses unexported types as keys:

```go
package main

import (
	"context"
	"fmt"
)

// Unexported type prevents collisions with other packages
type contextKey string

const (
	userKey      contextKey = "user"
	requestIDKey contextKey = "requestID"
)

func withUser(ctx context.Context, user string) context.Context {
	return context.WithValue(ctx, userKey, user)
}

func userFrom(ctx context.Context) (string, bool) {
	user, ok := ctx.Value(userKey).(string)
	return user, ok
}

func withRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	if id == "" {
		return "unknown"
	}
	return id
}

func main() {
	ctx := context.Background()
	ctx = withUser(ctx, "alice")
	ctx = withRequestID(ctx, "req-42")

	handleRequest(ctx)
}

func handleRequest(ctx context.Context) {
	user, ok := userFrom(ctx)
	if !ok {
		fmt.Println("no user in context")
		return
	}
	reqID := requestIDFrom(ctx)
	fmt.Printf("[%s] handling request for %s\n", reqID, user)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
[req-42] handling request for alice
```

## Step 3 -- Context Values Are Immutable and Layered

Each `WithValue` creates a new context. The original is unchanged:

```go
package main

import (
	"context"
	"fmt"
)

type key string

func main() {
	ctx1 := context.WithValue(context.Background(), key("lang"), "Go")
	ctx2 := context.WithValue(ctx1, key("version"), "1.22")
	ctx3 := context.WithValue(ctx2, key("lang"), "Rust") // shadows ctx1's "lang"

	fmt.Println("ctx1 lang:", ctx1.Value(key("lang")))
	fmt.Println("ctx1 version:", ctx1.Value(key("version"))) // nil -- not set yet

	fmt.Println("ctx2 lang:", ctx2.Value(key("lang")))       // inherited from ctx1
	fmt.Println("ctx2 version:", ctx2.Value(key("version")))

	fmt.Println("ctx3 lang:", ctx3.Value(key("lang")))       // shadowed
	fmt.Println("ctx3 version:", ctx3.Value(key("version"))) // inherited from ctx2
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
ctx1 lang: Go
ctx1 version: <nil>
ctx2 lang: Go
ctx2 version: 1.22
ctx3 lang: Rust
ctx3 version: 1.22
```

## Step 4 -- Context Values in a Middleware Chain

The most common real-world use: HTTP middleware attaches values that handlers read:

```go
package main

import (
	"context"
	"fmt"
)

type ctxKey int

const (
	traceIDKey ctxKey = iota
	userIDKey
)

// Simulate middleware that adds a trace ID
func withTracing(ctx context.Context) context.Context {
	return context.WithValue(ctx, traceIDKey, "trace-abc-123")
}

// Simulate auth middleware that adds user ID
func withAuth(ctx context.Context) context.Context {
	return context.WithValue(ctx, userIDKey, "user-42")
}

func handleOrder(ctx context.Context, orderID string) {
	traceID, _ := ctx.Value(traceIDKey).(string)
	userID, _ := ctx.Value(userIDKey).(string)

	fmt.Printf("[trace=%s] user %s placed order %s\n", traceID, userID, orderID)
}

func main() {
	// Simulate request flow through middleware
	ctx := context.Background()
	ctx = withTracing(ctx)
	ctx = withAuth(ctx)

	handleOrder(ctx, "ORD-001")
	handleOrder(ctx, "ORD-002")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
[trace=trace-abc-123] user user-42 placed order ORD-001
[trace=trace-abc-123] user user-42 placed order ORD-002
```

## Step 5 -- When NOT to Use Context Values

Context values are frequently misused. Here are the rules:

```go
package main

import (
	"context"
	"fmt"
)

// BAD: using context to pass required business data
func badCreateOrder(ctx context.Context) {
	productID := ctx.Value("productID").(string) // panic if missing!
	quantity := ctx.Value("quantity").(int)
	fmt.Printf("creating order: %s x%d\n", productID, quantity)
}

// GOOD: use function parameters for required data
func goodCreateOrder(ctx context.Context, productID string, quantity int) {
	fmt.Printf("creating order: %s x%d\n", productID, quantity)
}

// GOOD: context for cross-cutting concerns (tracing, auth)
type traceKey struct{}

func logWithTrace(ctx context.Context, msg string) {
	traceID, _ := ctx.Value(traceKey{}).(string)
	if traceID != "" {
		fmt.Printf("[%s] %s\n", traceID, msg)
	} else {
		fmt.Println(msg)
	}
}

func main() {
	ctx := context.WithValue(context.Background(), traceKey{}, "t-999")

	// Good: explicit parameters for business logic, context for tracing
	goodCreateOrder(ctx, "SKU-42", 3)
	logWithTrace(ctx, "order created successfully")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
creating order: SKU-42 x3
[t-999] order created successfully
```

## Common Mistakes

### Using String Keys

**Wrong:**

```go
ctx = context.WithValue(ctx, "user", "alice")
```

**Fix:** Define an unexported key type to prevent collisions across packages:

```go
type contextKey struct{}
ctx = context.WithValue(ctx, contextKey{}, "alice")
```

### Storing Mutable Values in Context

Context values should be immutable. Storing a pointer to a struct that gets modified concurrently creates data races.

### Using Context Values for Configuration

Database connection strings, feature flags, and configuration belong in structs passed explicitly, not in context values.

## Verify What You Learned

Write a package with:
1. An unexported key type and exported `WithRequestMeta` / `RequestMetaFrom` functions
2. A `RequestMeta` struct containing `TraceID`, `UserID`, and `Locale`
3. A handler function that extracts the metadata and formats a log message
4. Demonstrate that the handler works correctly with and without metadata in the context

## What's Next

Continue to [07 - Context Propagation](../07-context-propagation/07-context-propagation.md) to learn how context flows through layers of a real application.

## Summary

- `context.WithValue(parent, key, val)` attaches request-scoped data
- Use unexported types for keys to prevent package collisions
- Provide helper functions (`withX` / `xFrom`) for type-safe access
- Context values are immutable -- each `WithValue` creates a new context
- Use context values for cross-cutting concerns only: trace IDs, auth tokens, request IDs
- Never use context values for required function parameters or business data

## Reference

- [context.WithValue](https://pkg.go.dev/context#WithValue)
- [Go Blog: Contexts and structs](https://go.dev/blog/context-and-structs)
- [Go Blog: Go Concurrency Patterns: Context](https://go.dev/blog/context)
