---
difficulty: intermediate
concepts: [context.WithValue, type-safe keys, request-scoped data, middleware, key collision avoidance]
tools: [go]
estimated_time: 30m
bloom_level: apply
---

# 5. Context WithValue

## Learning Objectives
After completing this exercise, you will be able to:
- **Propagate** request-scoped data (request ID, user ID, trace ID) through middleware, handlers, and services
- **Define** type-safe context keys using unexported struct types to prevent collisions
- **Build** helper functions (`WithX` / `XFrom`) as the public API for context values
- **Identify** anti-patterns: using WithValue for optional parameters, dependency injection, or function arguments

## Why WithValue

In a real service, every request needs metadata that follows it through every layer: a request ID for correlating log lines across microservices, a user ID for authorization checks deep in the call stack, a trace ID for distributed tracing. This metadata crosses API boundaries -- from HTTP middleware to business logic to database queries to external API calls.

`context.WithValue` attaches a key-value pair to a context, creating a new derived context. Every function that receives this context can read the value. This is designed specifically for request-scoped metadata that transits process boundaries.

The critical rule: context values are for data that crosses API boundaries (request IDs, trace spans, auth tokens), not for passing function arguments. If a function needs data to operate, it should accept it as an explicit parameter. A function that pulls its inputs from context is impossible to understand without reading the entire call chain.

Keys must be carefully chosen. Using bare strings as keys is dangerous because two independent packages might use the same string key, silently overwriting each other's values. The Go idiom is to define an unexported struct type as the key, making cross-package collisions impossible.

## Step 1 -- Request Metadata Flowing Through Middleware

Build a request processing pipeline where middleware attaches a request ID and user ID, and every downstream layer can access them for logging:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

type requestIDKey struct{}
type userIDKey struct{}
type traceIDKey struct{}

func withRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

func withUserID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, userIDKey{}, id)
}

func userIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(userIDKey{}).(string)
	return id
}

func withTraceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceIDKey{}, id)
}

func traceIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(traceIDKey{}).(string)
	return id
}

type RequestLogger struct{}

func NewRequestLogger() *RequestLogger {
	return &RequestLogger{}
}

func (l *RequestLogger) Log(ctx context.Context, layer, message string) {
	fmt.Printf("[%-10s] req=%s user=%s trace=%s | %s\n",
		layer,
		requestIDFrom(ctx),
		userIDFrom(ctx),
		traceIDFrom(ctx),
		message,
	)
}

type RequestHandler struct {
	logger *RequestLogger
}

func NewRequestHandler(logger *RequestLogger) *RequestHandler {
	return &RequestHandler{logger: logger}
}

func (h *RequestHandler) buildContext(ctx context.Context) context.Context {
	ctx = withRequestID(ctx, "req-7f3a-bc21")
	fmt.Println("Middleware: added request ID")

	ctx = withUserID(ctx, "user-42")
	fmt.Println("Middleware: added user ID")

	ctx = withTraceID(ctx, "trace-abc-789")
	fmt.Println("Middleware: added trace ID")
	fmt.Println()

	return ctx
}

func (h *RequestHandler) ProcessOrder(ctx context.Context) {
	ctx = h.buildContext(ctx)

	h.logger.Log(ctx, "handler", "received order creation request")
	h.logger.Log(ctx, "service", "validating order data")
	h.logger.Log(ctx, "repository", "inserting order into database")
	h.logger.Log(ctx, "service", "sending confirmation email")
	h.logger.Log(ctx, "handler", fmt.Sprintf("completed in %v", 150*time.Millisecond))
}

func main() {
	logger := NewRequestLogger()
	handler := NewRequestHandler(logger)
	handler.ProcessOrder(context.Background())
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
Middleware: added request ID
Middleware: added user ID
Middleware: added trace ID

[handler   ] req=req-7f3a-bc21 user=user-42 trace=trace-abc-789 | received order creation request
[service   ] req=req-7f3a-bc21 user=user-42 trace=trace-abc-789 | validating order data
[repository] req=req-7f3a-bc21 user=user-42 trace=trace-abc-789 | inserting order into database
[service   ] req=req-7f3a-bc21 user=user-42 trace=trace-abc-789 | sending confirmation email
[handler   ] req=req-7f3a-bc21 user=user-42 trace=trace-abc-789 | completed in 150ms
```

The request ID, user ID, and trace ID flow through every layer without being passed as explicit parameters. Every log line from every layer contains the same correlation IDs, making it trivial to trace a single request across a distributed system.

## Step 2 -- Why Unexported Struct Keys (Not Strings)

Using plain strings as context keys is a production bug waiting to happen. Two independent packages that use the same string key silently overwrite each other's values:

```go
package main

import (
	"context"
	"fmt"
)

func demonstrateStringKeyCollision() {
	ctx := context.Background()

	ctx = context.WithValue(ctx, "userID", "admin-from-auth")
	fmt.Printf("After auth middleware:    userID = %s\n", ctx.Value("userID"))

	ctx = context.WithValue(ctx, "userID", "anonymous-from-logger")
	fmt.Printf("After logging middleware: userID = %s\n", ctx.Value("userID"))

	fmt.Printf("\nAuth check sees: %s\n", ctx.Value("userID"))
	fmt.Println("BUG: auth value was silently overwritten by the logger!")
}

func main() {
	demonstrateStringKeyCollision()
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
After auth middleware:    userID = admin-from-auth
After logging middleware: userID = anonymous-from-logger

Auth check sees: anonymous-from-logger
BUG: auth value was silently overwritten by the logger!
```

Now the safe version with typed keys:

```go
package main

import (
	"context"
	"fmt"
)

type authUserKey struct{}
type loggerUserKey struct{}

func demonstrateTypedKeySafety() {
	ctx := context.Background()

	ctx = context.WithValue(ctx, authUserKey{}, "admin-from-auth")
	ctx = context.WithValue(ctx, loggerUserKey{}, "anonymous-from-logger")

	authUser, _ := ctx.Value(authUserKey{}).(string)
	loggerUser, _ := ctx.Value(loggerUserKey{}).(string)

	fmt.Printf("Auth user:   %s\n", authUser)
	fmt.Printf("Logger user: %s\n", loggerUser)
	fmt.Println("No collision: different types, different keys.")
}

func main() {
	demonstrateTypedKeySafety()
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
Auth user:   admin-from-auth
Logger user: anonymous-from-logger
No collision: different types, different keys.
```

Each key type retrieves only its own value. The `go vet` tool warns about using string or integer keys. In production code, always use unexported struct types.

## Step 3 -- The Helper Functions Pattern

The production idiom used by gRPC, OpenTelemetry, and most Go libraries: the key type is unexported, and two exported functions provide the public API. This encapsulates the key completely:

```go
package main

import (
	"context"
	"fmt"
)

type authTokenKey struct{}

func WithAuthToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, authTokenKey{}, token)
}

func AuthTokenFrom(ctx context.Context) (string, bool) {
	token, ok := ctx.Value(authTokenKey{}).(string)
	return token, ok
}

type OrderHandler struct{}

func NewOrderHandler() *OrderHandler {
	return &OrderHandler{}
}

func (h *OrderHandler) HandleRequest(ctx context.Context) {
	token, ok := AuthTokenFrom(ctx)
	if !ok {
		fmt.Println("[handler] 401 Unauthorized: no auth token")
		return
	}
	fmt.Printf("[handler] authenticated, token prefix: %s...\n", token[:20])
	h.processOrder(ctx)
}

func (h *OrderHandler) processOrder(ctx context.Context) {
	token, ok := AuthTokenFrom(ctx)
	if !ok {
		fmt.Println("[service] ERROR: no auth token in context")
		return
	}
	fmt.Printf("[service] processing order for token: %s...\n", token[:20])
}

func main() {
	handler := NewOrderHandler()

	ctx := WithAuthToken(context.Background(), "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.payload")

	fmt.Println("=== With auth token ===")
	handler.HandleRequest(ctx)

	fmt.Println("\n=== Without auth token ===")
	handler.HandleRequest(context.Background())
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
=== With auth token ===
[handler] authenticated, token prefix: Bearer eyJhbGciOiJIU...
[service] processing order for token: Bearer eyJhbGciOiJIU...

=== Without auth token ===
[handler] 401 Unauthorized: no auth token
```

Callers never need to know the key type exists. They interact only with `WithAuthToken` and `AuthTokenFrom`. This is the pattern you should follow for every context value in production.

## Step 4 -- Anti-Patterns: What NOT to Put in Context

Context values have specific, narrow use cases. Here are the three most common misuses:

```go
package main

import (
	"context"
	"fmt"
)

type dbKey struct{}
type itemsKey struct{}
type logLevelKey struct{}

func demonstrateAntiPatterns() {
	fmt.Println("=== ANTI-PATTERN 1: Function Arguments in Context ===")
	ctx := context.WithValue(context.Background(), itemsKey{}, []string{"item-1", "item-2"})
	items, _ := ctx.Value(itemsKey{}).([]string)
	fmt.Printf("  Pulled from context: %v\n", items)
	fmt.Println("  FIX: pass items as a function parameter: calculateTotal(items []string)")

	fmt.Println("\n=== ANTI-PATTERN 2: Dependency Injection via Context ===")
	ctx = context.WithValue(context.Background(), dbKey{}, "postgres://db:5432")
	dsn, _ := ctx.Value(dbKey{}).(string)
	fmt.Printf("  Pulled from context: %s\n", dsn)
	fmt.Println("  FIX: inject the DB connection via struct field or constructor")

	fmt.Println("\n=== ANTI-PATTERN 3: Optional Parameters via Context ===")
	ctx = context.WithValue(context.Background(), logLevelKey{}, "debug")
	level, _ := ctx.Value(logLevelKey{}).(string)
	fmt.Printf("  Pulled from context: %s\n", level)
	fmt.Println("  FIX: pass as a parameter or use a logger configuration struct")

	fmt.Println("\n=== CORRECT USES OF WithValue ===")
	fmt.Println("  - Request ID (correlation across services)")
	fmt.Println("  - User ID from authentication (authorization decisions)")
	fmt.Println("  - Trace/span ID (distributed tracing)")
	fmt.Println("  - Tenant ID in multi-tenant systems")
}

func main() {
	demonstrateAntiPatterns()
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
=== ANTI-PATTERN 1: Function Arguments in Context ===
  Pulled from context: [item-1 item-2]
  FIX: pass items as a function parameter: calculateTotal(items []string)

=== ANTI-PATTERN 2: Dependency Injection via Context ===
  Pulled from context: postgres://db:5432
  FIX: inject the DB connection via struct field or constructor

=== ANTI-PATTERN 3: Optional Parameters via Context ===
  Pulled from context: debug
  FIX: pass as a parameter or use a logger configuration struct

=== CORRECT USES OF WithValue ===
  - Request ID (correlation across services)
  - User ID from authentication (authorization decisions)
  - Trace/span ID (distributed tracing)
  - Tenant ID in multi-tenant systems
```

The rule of thumb: if a function would break or produce wrong results without the value, it should be an explicit parameter. Context values are for metadata that enriches behavior (logging, tracing, authorization) but is not strictly required to compute the result.

## Common Mistakes

### Using String or Integer Keys
**Wrong:**
```go
ctx = context.WithValue(ctx, "requestID", "abc")     // string key -- collision risk
ctx = context.WithValue(ctx, 42, "some value")        // integer key -- collision risk
```
**Fix:** Define an unexported struct type as the key:
```go
type requestIDKey struct{}
ctx = context.WithValue(ctx, requestIDKey{}, "abc")
```

### Forgetting That WithValue Creates a New Context
**Wrong:**
```go
package main

import (
	"context"
	"fmt"
)

type myKey struct{}

func main() {
	ctx := context.Background()
	context.WithValue(ctx, myKey{}, "value") // return value discarded!
	fmt.Println(ctx.Value(myKey{}))           // nil -- ctx was not modified
}
```
**Fix:**
```go
package main

import (
	"context"
	"fmt"
)

type myKey struct{}

func main() {
	ctx := context.Background()
	ctx = context.WithValue(ctx, myKey{}, "value") // reassign!
	fmt.Println(ctx.Value(myKey{}))                  // "value"
}
```

Contexts are immutable. `WithValue` returns a new context -- always capture the return value.

### Storing Large Objects in Context
Context values are looked up by walking the parent chain linearly. Storing many values or large objects degrades performance. Keep context values small (strings, IDs) and few (3-5 values is typical).

## Verify What You Learned

Build a complete middleware chain that adds request metadata, then verify the metadata is accessible at every layer:

```go
package main

import (
	"context"
	"fmt"
)

type reqIDKey struct{}
type tenantKey struct{}

func withReqID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, reqIDKey{}, id)
}

func reqIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(reqIDKey{}).(string)
	return id
}

func withTenant(ctx context.Context, t string) context.Context {
	return context.WithValue(ctx, tenantKey{}, t)
}

func tenantFrom(ctx context.Context) string {
	t, _ := ctx.Value(tenantKey{}).(string)
	return t
}

type MiddlewareChain struct{}

func NewMiddlewareChain() *MiddlewareChain {
	return &MiddlewareChain{}
}

func (m *MiddlewareChain) logEntry(ctx context.Context, layer, msg string) {
	fmt.Printf("[%-10s] req=%s tenant=%s | %s\n", layer, reqIDFrom(ctx), tenantFrom(ctx), msg)
}

func (m *MiddlewareChain) ProcessRequest(ctx context.Context) {
	ctx = withReqID(ctx, "req-001")
	ctx = withTenant(ctx, "acme-corp")

	m.logEntry(ctx, "gateway", "request received")
	m.logEntry(ctx, "auth", "user authenticated")
	m.logEntry(ctx, "handler", "processing order")
	m.logEntry(ctx, "repository", "querying database")
	m.logEntry(ctx, "handler", "returning response")
}

func main() {
	chain := NewMiddlewareChain()
	chain.ProcessRequest(context.Background())
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
[gateway   ] req=req-001 tenant=acme-corp | request received
[auth      ] req=req-001 tenant=acme-corp | user authenticated
[handler   ] req=req-001 tenant=acme-corp | processing order
[repository] req=req-001 tenant=acme-corp | querying database
[handler   ] req=req-001 tenant=acme-corp | returning response
```

## What's Next
Continue to [06-context-propagation-chain](../06-context-propagation-chain/06-context-propagation-chain.md) to see how context flows through a complete request lifecycle with middleware, rate limiting, and database queries.

## Summary
- `context.WithValue(parent, key, val)` returns a new context carrying the key-value pair
- Use unexported struct types as keys to prevent cross-package collisions
- Provide exported helper functions (`WithX` / `XFrom`) as the public API for your context values
- Context values are for request-scoped metadata: request IDs, user IDs, trace IDs, tenant IDs
- Do NOT use context values for function arguments, dependency injection, or optional parameters
- `WithValue` creates a new context -- always capture the return value
- Keep context values small and few; lookup is a linear walk up the parent chain

## Reference
- [Package context: WithValue](https://pkg.go.dev/context#WithValue)
- [Go Blog: Context](https://go.dev/blog/context)
- [Go Wiki: Context Keys](https://go.dev/wiki/CodeReviewComments#contexts)
