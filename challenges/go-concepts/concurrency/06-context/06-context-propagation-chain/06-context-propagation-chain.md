---
difficulty: intermediate
concepts: [context propagation, layered architecture, context as first parameter, multi-layer cancellation]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [context.WithCancel, context.WithTimeout, context.WithValue, goroutines]
---

# 6. Context Propagation Chain


## Learning Objectives
After completing this exercise, you will be able to:
- **Propagate** context through multiple application layers
- **Apply** the `ctx context.Context` first-parameter convention consistently
- **Observe** that cancellation at any layer stops all downstream layers
- **Combine** context values and cancellation across a multi-layer chain

## Why Context Propagation

In production Go applications, a single user request flows through multiple layers: HTTP handler, business logic (service), data access (repository), and potentially external API calls. Each layer must respect the caller's context -- if the client disconnects or a timeout fires, all layers must stop promptly.

The Go convention is universal: `context.Context` is always the first parameter of any function that might block, do I/O, or call other functions that do. This is not optional style -- it is the standard that the entire Go ecosystem follows, from the standard library's `database/sql` to `net/http` to gRPC.

When context is propagated correctly, a single cancel call at the top tears down the entire operation tree. When it is not -- when a function creates its own `context.Background()` instead of using the caller's context -- cancellation stops at that boundary, and downstream work continues uselessly, wasting resources.

## Step 1 -- Define the Three Layers

Build a handler -> service -> repository chain. Each layer accepts `ctx context.Context` as its first parameter, checks for cancellation, does some work, and calls the next layer:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func handler(ctx context.Context, userID string) (string, error) {
	fmt.Println("[handler] received request")

	select {
	case <-ctx.Done():
		fmt.Printf("[handler] cancelled: %v\n", ctx.Err())
		return "", fmt.Errorf("handler: %w", ctx.Err())
	case <-time.After(50 * time.Millisecond):
	}

	result, err := service(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("handler: %w", err)
	}

	fmt.Printf("[handler] returning result: %s\n", result)
	return result, nil
}

func service(ctx context.Context, userID string) (string, error) {
	fmt.Printf("[service] looking up user %s\n", userID)

	select {
	case <-ctx.Done():
		fmt.Printf("[service] cancelled: %v\n", ctx.Err())
		return "", fmt.Errorf("service: %w", ctx.Err())
	case <-time.After(50 * time.Millisecond):
	}

	data, err := repository(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("service: %w", err)
	}

	return fmt.Sprintf("profile(%s)", data), nil
}

func repository(ctx context.Context, userID string) (string, error) {
	fmt.Printf("[repository] querying database for %s\n", userID)

	select {
	case <-ctx.Done():
		fmt.Printf("[repository] cancelled: %v\n", ctx.Err())
		return "", fmt.Errorf("repository: %w", ctx.Err())
	case <-time.After(100 * time.Millisecond):
	}

	return fmt.Sprintf("data-for-%s", userID), nil
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	result, err := handler(ctx, "user-42")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
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
[handler] received request
[service] looking up user user-42
[repository] querying database for user-42
[handler] returning result: profile(data-for-user-42)
Result: profile(data-for-user-42)
```

Total work: handler(50ms) + service(50ms) + repository(100ms) = 200ms. With a 1-second budget, everything completes.

## Step 2 -- Timeout Cancels All Layers

Use a short timeout so the chain is interrupted at the repository layer:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func handler(ctx context.Context, userID string) (string, error) {
	fmt.Println("[handler] received request")
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("handler: %w", ctx.Err())
	case <-time.After(50 * time.Millisecond):
	}
	result, err := service(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("handler: %w", err)
	}
	return result, nil
}

func service(ctx context.Context, userID string) (string, error) {
	fmt.Printf("[service] looking up %s\n", userID)
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("service: %w", ctx.Err())
	case <-time.After(50 * time.Millisecond):
	}
	data, err := repository(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("service: %w", err)
	}
	return fmt.Sprintf("profile(%s)", data), nil
}

func repository(ctx context.Context, userID string) (string, error) {
	fmt.Printf("[repository] querying for %s\n", userID)
	select {
	case <-ctx.Done():
		fmt.Printf("[repository] cancelled: %v\n", ctx.Err())
		return "", fmt.Errorf("repository: %w", ctx.Err())
	case <-time.After(100 * time.Millisecond):
	}
	return fmt.Sprintf("data-for-%s", userID), nil
}

func main() {
	// 120ms budget: handler(50ms) + service(50ms) = 100ms,
	// leaving only 20ms for repository(100ms).
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()

	_, err := handler(ctx, "user-42")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
[handler] received request
[service] looking up user-42
[repository] querying for user-42
[repository] cancelled: context deadline exceeded
Error: handler: service: repository: context deadline exceeded
```

The timeout fires while the repository is working. The error propagates back up through service and handler, creating a clear error chain that tells you exactly which layer was interrupted and why.

## Step 3 -- Manual Cancel from the Top

Cancel the context from a separate goroutine while the chain is running, simulating a client disconnect:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func handler(ctx context.Context, userID string) (string, error) {
	fmt.Println("[handler] received request")
	select {
	case <-ctx.Done():
		fmt.Printf("[handler] cancelled: %v\n", ctx.Err())
		return "", fmt.Errorf("handler: %w", ctx.Err())
	case <-time.After(50 * time.Millisecond):
	}
	return "done", nil // simplified for this example
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())

	// Simulate a client disconnecting after 80ms.
	go func() {
		time.Sleep(80 * time.Millisecond)
		fmt.Println("[caller] cancelling request")
		cancel()
	}()

	_, err := handler(ctx, "user-42")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
[handler] received request
[caller] cancelling request
[handler] cancelled: context canceled
Error: handler: context canceled
```

Notice the error is `context canceled` (not `deadline exceeded`), correctly indicating manual cancellation rather than a timeout.

## Step 4 -- Context Values Through the Chain

Attach a request ID at the entry point and show it being accessed at every layer:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

type requestIDKey struct{}

func withRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

func logWithContext(ctx context.Context, layer, message string) {
	reqID := requestIDFrom(ctx)
	fmt.Printf("[%-10s] req=%s: %s\n", layer, reqID, message)
}

func main() {
	ctx := withRequestID(context.Background(), "req-7f3a")
	ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	logWithContext(ctx, "handler", "processing request")
	logWithContext(ctx, "service", "applying business logic")
	logWithContext(ctx, "repository", "executing query")
	logWithContext(ctx, "handler", "completed successfully")
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
[handler   ] req=req-7f3a: processing request
[service   ] req=req-7f3a: applying business logic
[repository] req=req-7f3a: executing query
[handler   ] req=req-7f3a: completed successfully
```

The request ID flows through every layer without being passed as an explicit parameter -- this is the intended use of context values for cross-cutting concerns like logging and tracing.

## Common Mistakes

### Breaking the Chain with context.Background()
**Wrong:**
```go
func service(ctx context.Context, id string) (string, error) {
    // Creates a new root -- cancellation from caller is lost!
    newCtx := context.Background()
    return repository(newCtx, id)
}
```
**What happens:** If the caller cancels or the timeout fires, the repository continues running uselessly. It has no connection to the caller's context tree.

**Fix:** Always derive from the incoming context:
```go
func service(ctx context.Context, id string) (string, error) {
    return repository(ctx, id) // propagates caller's cancellation
}
```

### Not Checking Context in Each Layer
Each layer should check `ctx.Done()` before starting its own work. If the context is already cancelled when a layer is entered, there is no point in proceeding. This is the fail-fast principle applied to context:

```go
func service(ctx context.Context, id string) (string, error) {
    if ctx.Err() != nil {
        return "", fmt.Errorf("service: %w", ctx.Err())
    }
    // proceed with work...
}
```

### Wrapping Errors Without Context
**Wrong:**
```go
return "", err // caller has no idea which layer failed
```
**Fix:**
```go
return "", fmt.Errorf("service: %w", err) // clear error chain
```

Use `%w` to wrap errors so callers can use `errors.Is` and `errors.As` to inspect the chain.

## Verify What You Learned

Build a 4-layer chain (gateway -> auth -> compute -> store). Each layer takes 50ms. Attach a request ID. Test with a 300ms timeout (all complete) and a 130ms timeout (partial cancellation):

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

func runChain(ctx context.Context, layers []string, workPerLayer time.Duration) (string, error) {
	result := ""
	for _, name := range layers {
		fmt.Printf("[%-8s] req=%s: processing\n", name, requestIDFrom(ctx))
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("failed at %s: %w", name, ctx.Err())
		case <-time.After(workPerLayer):
			if result != "" {
				result += " -> "
			}
			result += name
		}
	}
	return result, nil
}

func main() {
	layers := []string{"gateway", "auth", "compute", "store"}

	// 300ms for 4x50ms = success.
	ctx1 := context.WithValue(context.Background(), requestIDKey{}, "req-001")
	ctx1, cancel1 := context.WithTimeout(ctx1, 300*time.Millisecond)
	defer cancel1()

	result, err := runChain(ctx1, layers, 50*time.Millisecond)
	if err != nil {
		fmt.Printf("Case 1: %v\n", err)
	} else {
		fmt.Printf("Case 1 success: %s -> done\n", result)
	}

	// 130ms for 4x50ms = fails around layer 3.
	ctx2 := context.WithValue(context.Background(), requestIDKey{}, "req-002")
	ctx2, cancel2 := context.WithTimeout(ctx2, 130*time.Millisecond)
	defer cancel2()

	result, err = runChain(ctx2, layers, 50*time.Millisecond)
	if err != nil {
		fmt.Printf("Case 2: %v\n", err)
	} else {
		fmt.Printf("Case 2 success: %s -> done\n", result)
	}
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
[gateway ] req=req-001: processing
[auth    ] req=req-001: processing
[compute ] req=req-001: processing
[store   ] req=req-001: processing
Case 1 success: gateway -> auth -> compute -> store -> done
[gateway ] req=req-002: processing
[auth    ] req=req-002: processing
[compute ] req=req-002: processing
Case 2: failed at compute: context deadline exceeded
```

## What's Next
Continue to [07-context-aware-long-worker](../07-context-aware-long-worker/07-context-aware-long-worker.md) to learn how to make long-running loops and workers respect context cancellation.

## Summary
- `context.Context` is always the first parameter: `func Foo(ctx context.Context, ...)`
- Cancellation propagates through the entire call chain when context is passed correctly
- Breaking the chain with `context.Background()` silently disables cancellation for downstream layers
- Context values (request IDs, trace spans) flow through all layers automatically
- Each layer should check `ctx.Done()` before starting expensive work
- Wrap errors with layer identification: `fmt.Errorf("layer: %w", err)`

## Reference
- [Go Blog: Context](https://go.dev/blog/context)
- [Go Code Review: Context](https://go.dev/wiki/CodeReviewComments#contexts)
- [Package context](https://pkg.go.dev/context)
