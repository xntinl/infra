---
difficulty: intermediate
concepts: [context.WithValue, type-safe keys, request-scoped data, key collision avoidance]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [context.Background, context.WithCancel, custom types]
---

# 5. Context WithValue


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

Store a request ID in the context and retrieve it in a downstream function:

```go
package main

import (
	"context"
	"fmt"
)

type contextKey string

const requestIDKey contextKey = "requestID"

func processRequest(ctx context.Context) {
	reqID, ok := ctx.Value(requestIDKey).(string)
	if !ok {
		fmt.Println("no request ID found in context")
		return
	}
	fmt.Printf("Processing request: %s\n", reqID)
}

func main() {
	ctx := context.Background()
	ctx = context.WithValue(ctx, requestIDKey, "req-abc-123")

	processRequest(ctx)

	// A key that was never set returns nil.
	fmt.Printf("Missing key: %v\n", ctx.Value(contextKey("nonexistent")))
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
Processing request: req-abc-123
Missing key: <nil>
```

Note the comma-ok idiom for the type assertion: `reqID, ok := ctx.Value(key).(string)`. This safely distinguishes "key not found" (ok=false) from "key found with zero value."

## Step 2 -- Type-Safe Keys (The Right Way)

Unexported custom types are essential for key safety. Different struct types are never equal, even if their structure is identical, so they can never collide:

```go
package main

import (
	"context"
	"fmt"
)

// Unexported types -- only this package can create values of these types.
type userKey struct{}
type traceKey struct{}

func main() {
	ctx := context.Background()
	ctx = context.WithValue(ctx, userKey{}, "alice")
	ctx = context.WithValue(ctx, traceKey{}, "trace-xyz-789")

	user, _ := ctx.Value(userKey{}).(string)
	trace, _ := ctx.Value(traceKey{}).(string)

	fmt.Printf("User:  %s\n", user)
	fmt.Printf("Trace: %s\n", trace)

	// Using the wrong key type returns nil, which type-asserts to zero value.
	wrongType, _ := ctx.Value(struct{}{}).(string)
	fmt.Printf("Wrong key type: %q (empty, not a collision)\n", wrongType)
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
User:  alice
Trace: trace-xyz-789
Wrong key type: "" (empty, not a collision)
```

Each key type retrieves only its own value. There is no collision even though both values are strings. The struct{} approach uses zero bytes of memory.

## Step 3 -- String Key Collision Problem

Why using plain strings as keys is dangerous:

```go
package main

import (
	"context"
	"fmt"
)

func main() {
	ctx := context.Background()

	// "Package A" stores a value.
	ctx = context.WithValue(ctx, "userID", "user-from-package-A")

	// "Package B" independently stores a value with the same key.
	ctx = context.WithValue(ctx, "userID", "user-from-package-B")

	// "Package A" tries to read its value -- gets B's instead.
	value := ctx.Value("userID")
	fmt.Printf("Value for 'userID': %s\n", value)
	fmt.Println("Package A's value was silently overwritten!")
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
Value for 'userID': user-from-package-B
Package A's value was silently overwritten!
```

With typed keys (Step 2), this collision is impossible because each package defines its own unexported type. The `go vet` tool even warns about using string or integer keys.

## Step 4 -- Helper Functions Pattern

The production idiom: the key type is unexported, and two exported functions provide the public API. This is the pattern used by gRPC metadata, OpenTelemetry spans, and most Go libraries:

```go
package main

import (
	"context"
	"fmt"
)

// Unexported key -- callers never see it.
type authTokenKey struct{}

// Public API: store a token.
func withAuthToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, authTokenKey{}, token)
}

// Public API: retrieve a token.
func authTokenFrom(ctx context.Context) (string, bool) {
	token, ok := ctx.Value(authTokenKey{}).(string)
	return token, ok
}

func handleRequest(ctx context.Context) {
	token, ok := authTokenFrom(ctx)
	if !ok {
		fmt.Println("No auth token -- rejecting request")
		return
	}
	// Only show a prefix to avoid leaking the full token.
	fmt.Printf("Authenticated with token: %s...\n", token[:15])
}

func main() {
	ctx := withAuthToken(context.Background(), "Bearer eyJhbGciOiJIUzI1NiJ9.payload.signature")
	handleRequest(ctx)

	// Call without a token to see the rejection.
	handleRequest(context.Background())
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
Authenticated with token: Bearer eyJhbGciO...
No auth token -- rejecting request
```

This pattern keeps the key type unexported while providing a clean, documented API. Callers never need to know the key type exists.

## Step 5 -- Values Are Inherited Down the Tree

A child context sees all values from its ancestors, but a parent does NOT see values added by its children. Values flow downward:

```go
package main

import (
	"context"
	"fmt"
)

type reqIDKey struct{}
type uidKey struct{}

func main() {
	root := context.WithValue(context.Background(), reqIDKey{}, "req-001")
	child := context.WithValue(root, uidKey{}, "alice")

	// Root's value is visible from root.
	rootReqID, _ := root.Value(reqIDKey{}).(string)
	fmt.Printf("root: requestID=%s\n", rootReqID)

	// Child sees root's value (inherited).
	childReqID, _ := child.Value(reqIDKey{}).(string)
	fmt.Printf("child sees parent's value: requestID=%s\n", childReqID)

	// Child's own value.
	childUID, _ := child.Value(uidKey{}).(string)
	fmt.Printf("child's own value: userID=%s\n", childUID)

	// Root does NOT see child's value.
	rootUID, _ := root.Value(uidKey{}).(string)
	fmt.Printf("root does NOT see child's value: userID=%q\n", rootUID)
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
root: requestID=req-001
child sees parent's value: requestID=req-001
child's own value: userID=alice
root does NOT see child's value: userID=""
```

This is consistent with the context tree model: information flows down from parent to child, never up.

## Common Mistakes

### Using String or Integer Keys
**Wrong:**
```go
ctx = context.WithValue(ctx, "requestID", "abc")     // string key -- collision risk
ctx = context.WithValue(ctx, 42, "some value")        // integer key -- collision risk
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
    items := ctx.Value(itemsKey{}).([]Item) // this should be a parameter!
    return sum(items)
}
```
**Fix:**
```go
func calculateTotal(items []Item) float64 {
    return sum(items)
}
```
Context values are for metadata (request IDs, trace spans, auth tokens) that crosses API boundaries -- not for function inputs. If a function *needs* data to operate, it should accept it as an explicit parameter.

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

Contexts are immutable. `WithValue` returns a *new* context -- always capture the return value.

### Storing Large Objects in Context
Context values are looked up by walking the parent chain. Storing many values or large objects degrades performance. Keep context values small and few.

## Verify What You Learned

Define a `correlationID` key type and helper functions. Build a three-function chain (entry -> middleware -> handler) where the correlation ID flows through every layer:

```go
package main

import (
	"context"
	"fmt"
)

type correlationIDKey struct{}

func withCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationIDKey{}, id)
}

func correlationIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(correlationIDKey{}).(string)
	return id
}

func logWithCorrelation(ctx context.Context, layer, message string) {
	corrID := correlationIDFrom(ctx)
	fmt.Printf("[%-10s] corrID=%s: %s\n", layer, corrID, message)
}

func main() {
	ctx := withCorrelationID(context.Background(), "corr-98765")

	logWithCorrelation(ctx, "gateway", "received request")
	logWithCorrelation(ctx, "middleware", "validating auth")
	logWithCorrelation(ctx, "handler", "processing business logic")
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
[gateway   ] corrID=corr-98765: received request
[middleware] corrID=corr-98765: validating auth
[handler   ] corrID=corr-98765: processing business logic
```

## What's Next
Continue to [06-context-propagation-chain](../06-context-propagation-chain/06-context-propagation-chain.md) to see how context flows through a realistic multi-layer application.

## Summary
- `context.WithValue(parent, key, val)` returns a new context carrying the key-value pair
- Use unexported struct types as keys to prevent cross-package collisions
- Provide exported helper functions (`WithX` / `XFrom`) as the public API for your context values
- Context values are for request-scoped metadata, not function parameters
- `WithValue` creates a new context -- always capture the return value
- Keep context values small and few; lookup is a linear walk up the parent chain
- Values are inherited downward: children see parent values, parents do not see child values

## Reference
- [Package context: WithValue](https://pkg.go.dev/context#WithValue)
- [Go Blog: Context](https://go.dev/blog/context)
- [Go Wiki: Context Keys](https://go.dev/wiki/CodeReviewComments#contexts)
