---
difficulty: basic
concepts: [context.Background, context.TODO, root contexts, context tree, layered architecture]
tools: [go]
estimated_time: 20m
bloom_level: understand
---

# 1. Context Background and TODO

## Learning Objectives
After completing this exercise, you will be able to:
- **Create** root contexts using `context.Background()` and `context.TODO()`
- **Explain** where `Background()` belongs (entry points) and where `TODO()` belongs (incomplete code)
- **Propagate** context from `main` through service and repository layers
- **Identify** the anti-pattern of using `Background()` deep in the call stack

## Why Context

Every Go service that handles HTTP requests, processes queues, or talks to databases needs a way to carry deadlines, cancellation signals, and request-scoped metadata through the call stack. The `context` package provides this mechanism.

At the root of every context tree sits one of two functions:

- **`context.Background()`** is the standard root. Use it in `main()`, initialization code, tests, and as the top-level context for incoming requests. It signals: "this is the starting point of an operation."
- **`context.TODO()`** is a placeholder. Use it when you are writing new code that will eventually receive a context from a caller, but that caller does not pass one yet. It signals: "I know a context belongs here, but I have not wired it up yet."

Both return identical empty contexts that are never cancelled, have no deadline, and carry no values. The difference is purely semantic -- a signal to the reader (and to static analysis tools) about intent.

Understanding these roots matters because every `WithCancel`, `WithTimeout`, `WithDeadline`, and `WithValue` you will use in production derives from one of them. If you start the tree wrong, cancellation and deadlines will not propagate correctly.

## Step 1 -- The Correct Entry Point: main() Owns Background

In a real service, `main()` creates the root context and passes it down. This is the only place where `context.Background()` should appear in application code. Build a simple order processing service to see this pattern:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func processOrder(ctx context.Context, orderID string) error {
	if ctx.Err() != nil {
		return fmt.Errorf("context already cancelled: %w", ctx.Err())
	}
	fmt.Printf("[order-service] processing order %s\n", orderID)
	time.Sleep(50 * time.Millisecond)

	return saveOrder(ctx, orderID)
}

func saveOrder(ctx context.Context, orderID string) error {
	if ctx.Err() != nil {
		return fmt.Errorf("context already cancelled: %w", ctx.Err())
	}
	fmt.Printf("[repository]    saving order %s to database\n", orderID)
	time.Sleep(30 * time.Millisecond)
	fmt.Printf("[repository]    order %s saved\n", orderID)
	return nil
}

func main() {
	// main() is the ONLY place where context.Background() belongs.
	ctx := context.Background()

	fmt.Printf("Root context type:   %T\n", ctx)
	fmt.Printf("Root context string: %s\n", ctx)
	fmt.Printf("Root context Err:    %v\n", ctx.Err())
	fmt.Printf("Root context Done:   %v (nil = never cancelled)\n", ctx.Done())
	fmt.Println()

	err := processOrder(ctx, "ORD-2024-1001")
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
Root context type:   context.backgroundCtx
Root context string: context.Background
Root context Err:    <nil>
Root context Done:   <nil> (nil = never cancelled)

[order-service] processing order ORD-2024-1001
[repository]    saving order ORD-2024-1001 to database
[repository]    order ORD-2024-1001 saved
```

The background context has no deadline, no error, and a nil `Done()` channel. A nil `Done()` channel blocks forever on receive, which is correct because a root context should never be cancelled. The context flows from `main` -> `processOrder` -> `saveOrder`, establishing the chain that cancellation and deadlines will follow.

## Step 2 -- Where context.TODO() Belongs

Imagine you are adding a new notification feature to the order service. The caller does not pass a context yet because you have not refactored the API. `context.TODO()` marks this spot for future work:

```go
package main

import (
	"context"
	"fmt"
)

// sendNotification is new code. The caller (an event handler) does not
// pass a context yet. TODO() marks this as "needs proper context wiring."
func sendNotification(userID string, message string) error {
	ctx := context.TODO() // Placeholder: will receive ctx from caller after refactor
	return deliverEmail(ctx, userID, message)
}

func deliverEmail(ctx context.Context, userID string, message string) error {
	if ctx.Err() != nil {
		return fmt.Errorf("context cancelled: %w", ctx.Err())
	}
	fmt.Printf("[email] sending to user %s: %q (via %s)\n", userID, message, ctx)
	return nil
}

func main() {
	bg := context.Background()
	todo := context.TODO()

	fmt.Printf("Background: %s\n", bg)
	fmt.Printf("TODO:       %s\n", todo)
	fmt.Printf("Same Err:   %v\n", bg.Err() == todo.Err())
	fmt.Printf("Same Done:  %v\n", bg.Done() == todo.Done())
	fmt.Println()

	// In production, this would be called from an event handler
	// that will eventually pass its own context.
	err := sendNotification("user-42", "Your order has shipped")
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
Background: context.Background
TODO:       context.TODO
Same Err:   true
Same Done:  true

[email] sending to user user-42: "Your order has shipped" (via context.TODO)
```

`Background()` and `TODO()` are structurally identical. The only difference is the string representation. Static analysis tools like `go vet` and `staticcheck` can flag `TODO()` contexts that remain in production code, reminding you to finish the refactor.

## Step 3 -- The Anti-Pattern: Background() Deep in the Call Stack

This is the most common context mistake in production code. When a function creates its own `context.Background()` instead of accepting one from its caller, it breaks the cancellation chain. Deadlines, timeouts, and cancel signals from the caller silently stop working:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

// WRONG: creates its own root context, ignoring the caller's cancellation.
func fetchUserBroken(userID string) (string, error) {
	ctx := context.Background() // isolated from caller -- silent bug
	_ = ctx
	fmt.Printf("[broken-repo]  fetching user %s (ignores cancellation)\n", userID)
	time.Sleep(100 * time.Millisecond)
	return "Alice", nil
}

// CORRECT: accepts the caller's context, propagating cancellation.
func fetchUserCorrect(ctx context.Context, userID string) (string, error) {
	if ctx.Err() != nil {
		return "", fmt.Errorf("fetch user: %w", ctx.Err())
	}
	fmt.Printf("[correct-repo] fetching user %s (respects cancellation)\n", userID)
	time.Sleep(100 * time.Millisecond)
	return "Alice", nil
}

func main() {
	// Simulate a request with a tight deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Wait for the deadline to pass.
	time.Sleep(60 * time.Millisecond)
	fmt.Printf("Context state: %v\n\n", ctx.Err())

	// The broken function ignores that the deadline has passed.
	name, err := fetchUserBroken("user-42")
	fmt.Printf("Broken result:  name=%s, err=%v (wasted work!)\n\n", name, err)

	// The correct function detects the cancelled context immediately.
	name, err = fetchUserCorrect(ctx, "user-42")
	fmt.Printf("Correct result: name=%q, err=%v (failed fast)\n", name, err)
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
Context state: context deadline exceeded

[broken-repo]  fetching user user-42 (ignores cancellation)
Broken result:  name=Alice, err=<nil> (wasted work!)

Correct result: name="", err=fetch user: context deadline exceeded (failed fast)
```

The broken function does 100ms of work even though the deadline already expired. In a real service, this means database queries, HTTP calls, and CPU time are wasted on requests that nobody is waiting for. Multiply this by thousands of requests per second, and it becomes a serious resource leak.

## Step 4 -- Complete Layered Service with Proper Context Flow

Build the full pattern you will use in every Go service: `main` creates the root, and context flows through every layer. Each layer checks context before doing work:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func apiHandler(ctx context.Context, orderID string) (string, error) {
	fmt.Printf("[handler]    received request for order %s\n", orderID)
	if ctx.Err() != nil {
		return "", fmt.Errorf("handler: %w", ctx.Err())
	}
	return orderService(ctx, orderID)
}

func orderService(ctx context.Context, orderID string) (string, error) {
	fmt.Printf("[service]    validating order %s\n", orderID)
	time.Sleep(30 * time.Millisecond)
	if ctx.Err() != nil {
		return "", fmt.Errorf("service: %w", ctx.Err())
	}
	return orderRepository(ctx, orderID)
}

func orderRepository(ctx context.Context, orderID string) (string, error) {
	fmt.Printf("[repository] querying database for order %s\n", orderID)
	select {
	case <-time.After(50 * time.Millisecond):
		result := fmt.Sprintf("Order{id: %s, status: shipped}", orderID)
		fmt.Printf("[repository] query complete\n")
		return result, nil
	case <-ctx.Done():
		return "", fmt.Errorf("repository: %w", ctx.Err())
	}
}

func main() {
	// main() owns the root context. In a real server, the HTTP framework
	// creates a per-request context derived from this root.
	ctx := context.Background()

	// Add a timeout to simulate a real request budget.
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	result, err := apiHandler(ctx, "ORD-2024-1001")
	if err != nil {
		fmt.Printf("\nError: %v\n", err)
		return
	}
	fmt.Printf("\nResult: %s\n", result)
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
[handler]    received request for order ORD-2024-1001
[service]    validating order ORD-2024-1001
[repository] querying database for order ORD-2024-1001
[repository] query complete

Result: Order{id: ORD-2024-1001, status: shipped}
```

This is the pattern you will see in every well-structured Go service. Context flows `main -> handler -> service -> repository`. If the timeout fires at any point, every downstream layer detects it and stops. No wasted database queries, no wasted CPU.

## Common Mistakes

### Using context.TODO() Permanently
**Wrong:** Leaving `context.TODO()` in production code indefinitely.

**Why it matters:** `TODO()` signals "I need to figure out the right context later." If it stays, it means cancellation and deadlines are not propagated through that code path, which leads to resource leaks under load. A function using `TODO()` will keep running even when the caller has given up on the result.

**Fix:** Replace `TODO()` with a properly propagated context once the caller is refactored. Treat every `TODO()` like a `// TODO` comment -- it is technical debt that must be resolved.

### Creating Background() Inside a Helper
**Wrong:**
```go
package main

import (
	"context"
	"fmt"
)

func queryDatabase(query string) error {
	ctx := context.Background() // new root -- isolated from caller
	_ = ctx
	fmt.Println("querying...")
	return nil
}

func main() {
	queryDatabase("SELECT * FROM orders")
}
```
**Fix:**
```go
package main

import (
	"context"
	"fmt"
)

func queryDatabase(ctx context.Context, query string) error {
	_ = ctx // uses caller's context -- cancellation propagates
	fmt.Println("querying...")
	return nil
}

func main() {
	queryDatabase(context.Background(), "SELECT * FROM orders")
}
```

When a function creates its own `context.Background()`, the caller has no way to cancel or set a deadline on that operation. In a server handling thousands of requests, this leads to goroutine pileups when downstream services are slow.

### Storing Context in a Struct
**Wrong:**
```go
package main

import "context"

type OrderService struct {
	ctx context.Context // do not do this
}

func main() {
	_ = OrderService{ctx: context.Background()}
}
```
**Why it matters:** Contexts are request-scoped. The service outlives any individual request. A context stored in a struct becomes stale immediately after the request it was created for ends, leading to cancelled contexts being reused for new requests.

**Fix:** Pass context as the first parameter of each method:
```go
package main

import (
	"context"
	"fmt"
)

type OrderService struct{}

func (s *OrderService) GetOrder(ctx context.Context, id string) (string, error) {
	if ctx.Err() != nil {
		return "", fmt.Errorf("get order: %w", ctx.Err())
	}
	return fmt.Sprintf("Order{id: %s}", id), nil
}

func main() {
	svc := &OrderService{}
	result, _ := svc.GetOrder(context.Background(), "ORD-001")
	fmt.Println(result)
}
```

## Verify What You Learned

Build a three-layer service (handler -> validator -> storage) where each layer logs which context it received. Call it once with `Background()` and once with a cancelled context to verify that cancellation is detected at each layer:

```go
package main

import (
	"context"
	"fmt"
)

func handler(ctx context.Context, data string) error {
	fmt.Printf("[handler]   context=%s, err=%v\n", ctx, ctx.Err())
	if ctx.Err() != nil {
		return fmt.Errorf("handler: %w", ctx.Err())
	}
	return validator(ctx, data)
}

func validator(ctx context.Context, data string) error {
	fmt.Printf("[validator] context=%s, err=%v\n", ctx, ctx.Err())
	if ctx.Err() != nil {
		return fmt.Errorf("validator: %w", ctx.Err())
	}
	return storage(ctx, data)
}

func storage(ctx context.Context, data string) error {
	fmt.Printf("[storage]   context=%s, err=%v\n", ctx, ctx.Err())
	if ctx.Err() != nil {
		return fmt.Errorf("storage: %w", ctx.Err())
	}
	fmt.Printf("[storage]   saved: %s\n", data)
	return nil
}

func main() {
	fmt.Println("=== With Background (healthy) ===")
	err := handler(context.Background(), "order-data")
	fmt.Printf("Result: %v\n\n", err)

	fmt.Println("=== With cancelled context ===")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = handler(ctx, "order-data")
	fmt.Printf("Result: %v\n", err)
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
=== With Background (healthy) ===
[handler]   context=context.Background, err=<nil>
[validator] context=context.Background, err=<nil>
[storage]   context=context.Background, err=<nil>
[storage]   saved: order-data
Result: <nil>

=== With cancelled context ===
[handler]   context=context.Background.WithCancel, err=context canceled
Result: handler: context canceled
```

## What's Next
Continue to [02-context-withcancel](../02-context-withcancel/02-context-withcancel.md) to learn how to create cancellable contexts and signal goroutines to stop when users cancel operations.

## Summary
- `context.Background()` is the standard root context -- use it only in `main()`, initialization, and tests
- `context.TODO()` is a placeholder for code that will receive a proper context after refactoring
- Both return empty, never-cancelled contexts with no deadline and no values
- Context flows from entry points down through every layer: handler -> service -> repository
- Never create `context.Background()` inside helper functions -- it breaks the cancellation chain
- Never store contexts in structs -- they are request-scoped and become stale
- Convention: `context.Context` is always the first parameter, named `ctx`
- A nil `Done()` channel blocks forever on receive, which is correct for root contexts

## Reference
- [Go Blog: Go Concurrency Patterns: Context](https://go.dev/blog/context)
- [Package context](https://pkg.go.dev/context)
- [Go Proverb: Pass context.Context as the first argument](https://go-proverbs.github.io/)
