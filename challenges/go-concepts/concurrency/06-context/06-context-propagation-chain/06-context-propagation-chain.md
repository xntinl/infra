# 6. Context Propagation Chain

<!--
difficulty: intermediate
concepts: [context propagation, layered architecture, context as first parameter, multi-layer cancellation]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [context.WithCancel, context.WithTimeout, context.WithValue, goroutines]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01 through 05 in this section
- Understanding of layered application architecture (handler -> service -> repository)

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

Edit `main.go` and implement the three layers. Each layer accepts `ctx context.Context` as its first parameter, does some work, and calls the next layer:

```go
func handler(ctx context.Context, userID string) (string, error) {
    fmt.Println("  [handler] received request")

    // Simulate request processing time
    select {
    case <-ctx.Done():
        fmt.Printf("  [handler] cancelled: %v\n", ctx.Err())
        return "", ctx.Err()
    case <-time.After(50 * time.Millisecond):
        // proceed
    }

    result, err := service(ctx, userID)
    if err != nil {
        return "", fmt.Errorf("handler: %w", err)
    }

    fmt.Printf("  [handler] returning result: %s\n", result)
    return result, nil
}

func service(ctx context.Context, userID string) (string, error) {
    fmt.Printf("  [service] looking up user %s\n", userID)

    select {
    case <-ctx.Done():
        fmt.Printf("  [service] cancelled: %v\n", ctx.Err())
        return "", ctx.Err()
    case <-time.After(50 * time.Millisecond):
        // proceed
    }

    data, err := repository(ctx, userID)
    if err != nil {
        return "", fmt.Errorf("service: %w", err)
    }

    return fmt.Sprintf("profile(%s)", data), nil
}

func repository(ctx context.Context, userID string) (string, error) {
    fmt.Printf("  [repository] querying database for %s\n", userID)

    select {
    case <-ctx.Done():
        fmt.Printf("  [repository] cancelled: %v\n", ctx.Err())
        return "", ctx.Err()
    case <-time.After(100 * time.Millisecond):
        // simulate DB response
    }

    return fmt.Sprintf("data-for-%s", userID), nil
}
```

### Intermediate Verification

Implement `successfulRequest` to call the chain with enough time:

```go
func successfulRequest() {
    fmt.Println("=== Successful Request ===")

    ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
    defer cancel()

    result, err := handler(ctx, "user-42")
    if err != nil {
        fmt.Printf("  Error: %v\n", err)
    } else {
        fmt.Printf("  Final result: %s\n", result)
    }
    fmt.Println()
}
```

```bash
go run main.go
```
Expected output:
```
=== Successful Request ===
  [handler] received request
  [service] looking up user user-42
  [repository] querying database for user-42
  [handler] returning result: profile(data-for-user-42)
  Final result: profile(data-for-user-42)
```

## Step 2 -- Timeout Cancels All Layers

Implement `timeoutRequest`. Use a short timeout so the chain is interrupted at the repository layer:

```go
func timeoutRequest() {
    fmt.Println("=== Timeout Cancels All Layers ===")

    // 120ms timeout: handler (50ms) + service (50ms) = 100ms,
    // leaving only 20ms for repository (needs 100ms) -- will timeout
    ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
    defer cancel()

    result, err := handler(ctx, "user-42")
    if err != nil {
        fmt.Printf("  Error: %v\n", err)
    } else {
        fmt.Printf("  Final result: %s\n", result)
    }
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Timeout Cancels All Layers ===
  [handler] received request
  [service] looking up user user-42
  [repository] querying database for user-42
  [repository] cancelled: context deadline exceeded
  Error: handler: service: context deadline exceeded
```

The timeout fires while the repository is working. The error propagates back up through service and handler.

## Step 3 -- Manual Cancel from the Top

Implement `manualCancelRequest`. Cancel the context from a separate goroutine while the chain is running:

```go
func manualCancelRequest() {
    fmt.Println("=== Manual Cancel from Top ===")

    ctx, cancel := context.WithCancel(context.Background())

    // Cancel after 80ms (while service is processing)
    go func() {
        time.Sleep(80 * time.Millisecond)
        fmt.Println("  [caller] cancelling request")
        cancel()
    }()

    result, err := handler(ctx, "user-42")
    if err != nil {
        fmt.Printf("  Error: %v\n", err)
    } else {
        fmt.Printf("  Final result: %s\n", result)
    }
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Manual Cancel from Top ===
  [handler] received request
  [caller] cancelling request
  [service] looking up user user-42
  [service] cancelled: context canceled
  Error: handler: service: context canceled
```

## Step 4 -- Context Values Through the Chain

Implement `requestWithValues`. Attach a request ID to the context and show it being accessed at every layer:

```go
type requestIDKey struct{}

func requestWithValues() {
    fmt.Println("=== Context Values Through Chain ===")

    ctx := context.Background()
    ctx = context.WithValue(ctx, requestIDKey{}, "req-7f3a")
    ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
    defer cancel()

    logWithContext(ctx, "handler", "starting request")
    logWithContext(ctx, "service", "processing")
    logWithContext(ctx, "repository", "querying")
    fmt.Println()
}

func logWithContext(ctx context.Context, layer, message string) {
    reqID, _ := ctx.Value(requestIDKey{}).(string)
    fmt.Printf("  [%s] request=%s: %s\n", layer, reqID, message)
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Context Values Through Chain ===
  [handler] request=req-7f3a: starting request
  [service] request=req-7f3a: processing
  [repository] request=req-7f3a: querying
```

The request ID flows through every layer without being passed as an explicit parameter -- this is the intended use of context values.

## Common Mistakes

### Breaking the Chain with context.Background()
**Wrong:**
```go
func service(ctx context.Context, id string) {
    // Creates a new root -- cancellation from caller is lost
    newCtx := context.Background()
    repository(newCtx, id)
}
```
**Fix:** Always derive from the incoming context:
```go
func service(ctx context.Context, id string) {
    repository(ctx, id) // propagates caller's cancellation
}
```

### Not Checking Context in Each Layer
Each layer should check `ctx.Done()` before starting its own work. If the context is already cancelled when a layer is entered, there is no point in proceeding.

### Wrapping Errors Without Context
**Wrong:**
```go
return "", err // caller has no idea which layer failed
```
**Fix:**
```go
return "", fmt.Errorf("service: %w", err) // clear error chain
```

## Verify What You Learned

Implement `verifyKnowledge`: build a 4-layer chain (gateway -> auth -> compute -> store). Each layer takes 50ms. Attach a request ID via context value. Test with a 300ms timeout (all layers complete) and a 150ms timeout (compute or store should be cancelled). Print the request ID in each layer's log output.

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
