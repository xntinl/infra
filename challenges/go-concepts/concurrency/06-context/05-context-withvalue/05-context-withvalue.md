# 5. Context WithValue

<!--
difficulty: intermediate
concepts: [context.WithValue, type-safe keys, request-scoped data, key collision avoidance]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [context.Background, context.WithCancel, custom types]
-->

## Prerequisites
- Go 1.22+ installed
- Completed [02-context-withcancel](../02-context-withcancel/02-context-withcancel.md)
- Understanding of Go custom types and type assertions

## Learning Objectives
After completing this exercise, you will be able to:
- **Store** request-scoped data in a context using `context.WithValue`
- **Define** type-safe context keys to avoid collisions
- **Retrieve** values from context using type assertions
- **Distinguish** appropriate uses of context values from anti-patterns

## Why WithValue

Contexts carry more than cancellation signals. The `context.WithValue` function attaches a key-value pair to a context, creating a new derived context. This is designed for request-scoped data that crosses API boundaries: request IDs, authentication tokens, tracing spans, and similar metadata.

The critical rule: context values are for data that transits processes and APIs, not for passing optional function parameters. If a function needs data to operate, it should accept it as an explicit parameter. Context values are for metadata that the function does not directly use but needs to propagate downstream (logging correlation IDs, for example).

Keys must be carefully chosen to avoid collisions. Using bare strings or integers as keys is dangerous because two independent packages might use the same key. The idiomatic Go approach is to define an unexported type as the key, making it impossible for code outside your package to collide.

## Step 1 -- Store and Retrieve a Value

Edit `main.go` and implement `basicWithValue`. Store a request ID in the context and retrieve it in a downstream function:

```go
type contextKey string

const requestIDKey contextKey = "requestID"

func basicWithValue() {
    fmt.Println("=== Basic WithValue ===")

    ctx := context.Background()
    ctx = context.WithValue(ctx, requestIDKey, "req-abc-123")

    processRequest(ctx)
    fmt.Println()
}

func processRequest(ctx context.Context) {
    reqID, ok := ctx.Value(requestIDKey).(string)
    if !ok {
        fmt.Println("  no request ID found in context")
        return
    }
    fmt.Printf("  Processing request: %s\n", reqID)
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Basic WithValue ===
  Processing request: req-abc-123
```

## Step 2 -- Type-Safe Keys (The Right Way)

Implement `typeSafeKeys`. Demonstrate why unexported custom types are essential for key safety:

```go
// unexported type -- only this package can create values of this type
type userKey struct{}
type traceKey struct{}

func typeSafeKeys() {
    fmt.Println("=== Type-Safe Keys ===")

    ctx := context.Background()
    ctx = context.WithValue(ctx, userKey{}, "alice")
    ctx = context.WithValue(ctx, traceKey{}, "trace-xyz-789")

    user, _ := ctx.Value(userKey{}).(string)
    trace, _ := ctx.Value(traceKey{}).(string)

    fmt.Printf("  User:  %s\n", user)
    fmt.Printf("  Trace: %s\n", trace)

    // Different type keys never collide, even with same underlying value
    fmt.Printf("  userKey lookup with traceKey: %v\n", ctx.Value(traceKey{}))
    fmt.Printf("  traceKey lookup with userKey: %v\n", ctx.Value(userKey{}))
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Type-Safe Keys ===
  User:  alice
  Trace: trace-xyz-789
  userKey lookup with traceKey: trace-xyz-789
  traceKey lookup with userKey: alice
```

Each key type retrieves only its own value. There is no collision even though both values are strings.

## Step 3 -- String Key Collision Problem

Implement `stringKeyCollision`. Show why using plain strings as keys is dangerous:

```go
func stringKeyCollision() {
    fmt.Println("=== String Key Collision Problem ===")

    ctx := context.Background()

    // Package A stores a value
    ctx = context.WithValue(ctx, "userID", "user-from-package-A")

    // Package B (independently) stores a value with the same key
    ctx = context.WithValue(ctx, "userID", "user-from-package-B")

    // Package A tries to read its value -- gets Package B's instead
    value := ctx.Value("userID")
    fmt.Printf("  Value for 'userID': %s\n", value)
    fmt.Println("  Package A's value was silently overwritten!")
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== String Key Collision Problem ===
  Value for 'userID': user-from-package-B
  Package A's value was silently overwritten!
```

## Step 4 -- Helper Functions Pattern

Implement helper functions that encapsulate the key type and provide a clean API for storing and retrieving values:

```go
type authTokenKey struct{}

func withAuthToken(ctx context.Context, token string) context.Context {
    return context.WithValue(ctx, authTokenKey{}, token)
}

func authTokenFrom(ctx context.Context) (string, bool) {
    token, ok := ctx.Value(authTokenKey{}).(string)
    return token, ok
}

func helperFunctionsPattern() {
    fmt.Println("=== Helper Functions Pattern ===")

    ctx := context.Background()
    ctx = withAuthToken(ctx, "Bearer eyJhbG...")

    handleRequest(ctx)
    fmt.Println()
}

func handleRequest(ctx context.Context) {
    if token, ok := authTokenFrom(ctx); ok {
        fmt.Printf("  Authenticated with token: %s...\n", token[:15])
    } else {
        fmt.Println("  No auth token -- rejecting request")
    }
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Helper Functions Pattern ===
  Authenticated with token: Bearer eyJhbG...
```

This pattern keeps the key type unexported while providing a clean, documented API. Callers never need to know the key type exists.

## Common Mistakes

### Using String or Integer Keys
**Wrong:**
```go
ctx = context.WithValue(ctx, "requestID", "abc")     // string key
ctx = context.WithValue(ctx, 42, "some value")        // integer key
```
**What happens:** Any package can accidentally use the same string or integer, silently overwriting your value.

**Fix:** Define an unexported struct type as the key:
```go
type requestIDKey struct{}
ctx = context.WithValue(ctx, requestIDKey{}, "abc")
```

### Using Context Values Instead of Function Parameters
**Wrong:**
```go
func calculateTotal(ctx context.Context) float64 {
    items := ctx.Value(itemsKey{}).([]Item) // this should be a parameter
    return sum(items)
}
```
**Fix:**
```go
func calculateTotal(items []Item) float64 {
    return sum(items)
}
```

Context values are for metadata (request IDs, trace spans, auth tokens) that crosses API boundaries -- not for function inputs.

### Forgetting That WithValue Creates a New Context
**Wrong:**
```go
ctx := context.Background()
context.WithValue(ctx, myKey{}, "value") // return value discarded!
fmt.Println(ctx.Value(myKey{}))          // nil -- ctx was not modified
```
**Fix:**
```go
ctx := context.Background()
ctx = context.WithValue(ctx, myKey{}, "value") // reassign
fmt.Println(ctx.Value(myKey{}))                 // "value"
```

### Storing Large Objects in Context
Context values are looked up by walking the parent chain. Storing many values or large objects degrades performance. Keep context values small and few.

## Verify What You Learned

Implement `verifyKnowledge`: define a `correlationID` key type and helper functions `withCorrelationID` / `correlationIDFrom`. Build a three-function chain (entry -> middleware -> handler) that:
1. Entry sets the correlation ID
2. Middleware reads and logs it, then forwards the context
3. Handler reads the correlation ID for its own logging

## What's Next
Continue to [06-context-propagation-chain](../06-context-propagation-chain/06-context-propagation-chain.md) to see how context flows through a realistic multi-layer application.

## Summary
- `context.WithValue(parent, key, val)` returns a new context carrying the key-value pair
- Use unexported struct types as keys to prevent cross-package collisions
- Provide exported helper functions (`WithX` / `XFrom`) as the public API for your context values
- Context values are for request-scoped metadata, not function parameters
- `WithValue` creates a new context -- always capture the return value
- Keep context values small and few; lookup is a linear walk up the parent chain

## Reference
- [Package context: WithValue](https://pkg.go.dev/context#WithValue)
- [Go Blog: Context](https://go.dev/blog/context)
- [Go Wiki: Context Keys](https://go.dev/wiki/CodeReviewComments#contexts)
