# 3. Context WithTimeout

<!--
difficulty: intermediate
concepts: [context.WithTimeout, automatic cancellation, ctx.Done, ctx.Err, DeadlineExceeded, defer cancel]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [context.Background, context.WithCancel, goroutines, select]
-->

## Prerequisites
- Go 1.22+ installed
- Completed [02-context-withcancel](../02-context-withcancel/02-context-withcancel.md)
- Understanding of `select` with channels

## Learning Objectives
After completing this exercise, you will be able to:
- **Create** a context that automatically cancels after a specified duration
- **Handle** timeout signals using `ctx.Done()` and `ctx.Err()`
- **Distinguish** between manual cancellation and timeout (context.Canceled vs context.DeadlineExceeded)
- **Avoid** resource leaks by always deferring the cancel function

## Why WithTimeout

Many operations must complete within a time limit. Database queries, HTTP requests, RPC calls -- if they hang, they hold goroutines and connections open indefinitely. `context.WithTimeout` creates a context that automatically cancels itself after the specified duration, even if no one calls `cancel()` explicitly.

This is the backbone of resilient systems. When you set a timeout, you guarantee that no matter what happens downstream, resources will be freed within a bounded time. Without it, a single slow dependency can cascade into a system-wide outage as goroutines pile up waiting for responses that never come.

The cancel function returned by `WithTimeout` must still be deferred. Even if the timeout fires first, calling `cancel()` releases internal resources immediately rather than waiting for garbage collection.

## Step 1 -- Basic Timeout

Edit `main.go` and implement `basicTimeout`. Create a context with a short timeout and simulate a slow operation that exceeds it:

```go
func basicTimeout() {
    fmt.Println("=== Basic WithTimeout ===")

    ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
    defer cancel()

    fmt.Println("  Starting slow operation (will take 500ms)...")

    select {
    case <-time.After(500 * time.Millisecond):
        fmt.Println("  Operation completed successfully")
    case <-ctx.Done():
        fmt.Printf("  Operation aborted: %v\n", ctx.Err())
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
=== Basic WithTimeout ===
  Starting slow operation (will take 500ms)...
  Operation aborted: context deadline exceeded
```

The operation needed 500ms but the context only allowed 200ms. The `ctx.Done()` channel closed first, and `ctx.Err()` returns `context.DeadlineExceeded`.

## Step 2 -- Fast Operation Completes Before Timeout

Implement `fastOperation`. Show that when the operation finishes before the timeout, everything proceeds normally:

```go
func fastOperation() {
    fmt.Println("=== Fast Operation (within timeout) ===")

    ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
    defer cancel() // still required even though timeout won't fire

    fmt.Println("  Starting fast operation (will take 100ms)...")

    select {
    case <-time.After(100 * time.Millisecond):
        fmt.Println("  Operation completed successfully")
    case <-ctx.Done():
        fmt.Printf("  Operation aborted: %v\n", ctx.Err())
    }

    fmt.Printf("  Context error after completion: %v\n\n", ctx.Err())
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Fast Operation (within timeout) ===
  Starting fast operation (will take 100ms)...
  Operation completed successfully
  Context error after completion: <nil>
```

The operation finished in 100ms, well within the 500ms timeout. The context error is still nil because the timeout has not fired yet.

## Step 3 -- Timeout with Goroutine Worker

Implement `timeoutWithWorker`. Pass the timeout context to a goroutine that simulates work in a loop, checking `ctx.Done()` between iterations:

```go
func timeoutWithWorker() {
    fmt.Println("=== Timeout with Worker Goroutine ===")

    ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
    defer cancel()

    done := make(chan string)

    go func(ctx context.Context) {
        for i := 1; ; i++ {
            select {
            case <-ctx.Done():
                done <- fmt.Sprintf("worker stopped at iteration %d: %v", i, ctx.Err())
                return
            default:
                fmt.Printf("  worker: processing item %d\n", i)
                time.Sleep(100 * time.Millisecond)
            }
        }
    }(ctx)

    result := <-done
    fmt.Printf("  %s\n\n", result)
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output (approximately):
```
=== Timeout with Worker Goroutine ===
  worker: processing item 1
  worker: processing item 2
  worker: processing item 3
  worker stopped at iteration 4: context deadline exceeded
```

The worker processes items until the 350ms timeout fires. The goroutine detects the cancellation via `ctx.Done()` and reports back through the `done` channel.

## Step 4 -- Manual Cancel Before Timeout

Implement `earlyCancel`. Show that calling `cancel()` before the timeout triggers `context.Canceled` instead of `context.DeadlineExceeded`:

```go
func earlyCancel() {
    fmt.Println("=== Early Cancel (before timeout) ===")

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    // Note: not deferring -- we will call it manually

    go func() {
        <-ctx.Done()
        fmt.Printf("  goroutine: context ended: %v\n", ctx.Err())
    }()

    time.Sleep(100 * time.Millisecond)
    fmt.Println("  main: calling cancel() manually (timeout was 5s)")
    cancel()

    time.Sleep(50 * time.Millisecond)
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Early Cancel (before timeout) ===
  main: calling cancel() manually (timeout was 5s)
  goroutine: context ended: context canceled
```

When you cancel manually, `ctx.Err()` returns `context.Canceled`, not `context.DeadlineExceeded`. This distinction lets you differentiate "we chose to stop" from "we ran out of time."

## Common Mistakes

### Not Deferring Cancel on WithTimeout
**Wrong:**
```go
ctx, cancel := context.WithTimeout(parent, 5*time.Second)
// forgot defer cancel()
```
**What happens:** Even if the timeout fires, internal resources (a timer goroutine) are not freed until the parent is cancelled or garbage collected. This leaks resources.

**Fix:** Always `defer cancel()` immediately:
```go
ctx, cancel := context.WithTimeout(parent, 5*time.Second)
defer cancel()
```

### Setting Timeout Longer Than Parent's
**Wrong:**
```go
parent, _ := context.WithTimeout(bg, 1*time.Second)
child, _ := context.WithTimeout(parent, 10*time.Second) // this 10s is useless
```
**What happens:** The child inherits the parent's 1-second deadline. The 10-second timeout on the child is never reached because the parent cancels first. This is not an error, but it is misleading.

### Ignoring ctx.Err() After Timeout
**Wrong:**
```go
select {
case <-ctx.Done():
    return nil // what went wrong? caller has no idea
}
```
**Fix:**
```go
select {
case <-ctx.Done():
    return fmt.Errorf("operation failed: %w", ctx.Err())
}
```

## Verify What You Learned

Implement `verifyKnowledge`: simulate a "database query" function that takes a `context.Context` and a simulated duration. Call it twice -- once with a timeout shorter than the query (should time out) and once with a timeout longer (should succeed). Print which error type you get in each case.

## What's Next
Continue to [04-context-withdeadline](../04-context-withdeadline/04-context-withdeadline.md) to learn about absolute deadlines and how `WithTimeout` is really a shorthand for `WithDeadline`.

## Summary
- `context.WithTimeout(parent, duration)` creates a context that auto-cancels after the duration
- The `Done()` channel closes when the timeout fires or when `cancel()` is called manually
- `ctx.Err()` returns `context.DeadlineExceeded` for timeouts, `context.Canceled` for manual cancellation
- Always `defer cancel()` even with `WithTimeout` -- it frees the internal timer immediately
- A child context cannot extend its parent's deadline -- the shorter deadline always wins
- Use timeouts to bound all operations that depend on external systems

## Reference
- [Package context: WithTimeout](https://pkg.go.dev/context#WithTimeout)
- [Go Blog: Context](https://go.dev/blog/context)
- [Dave Cheney: Context is for Cancellation](https://dave.cheney.net/2017/08/20/context-isnt-for-cancellation)
