# 2. Errgroup with Context

<!--
difficulty: intermediate
concepts: [errgroup.WithContext, context.Context, automatic cancellation, ctx.Done, ctx.Err]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [errgroup basics, context.Context, context.WithCancel]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercise 01 (errgroup basics)
- Understanding of `context.Context`, `ctx.Done()`, and `ctx.Err()`
- Familiarity with `context.WithCancel` and cancellation propagation

## Learning Objectives
After completing this exercise, you will be able to:
- **Create** an errgroup with `errgroup.WithContext` to get automatic cancellation
- **Implement** goroutines that respect context cancellation via `ctx.Done()`
- **Explain** the cancellation semantics: when one task fails, siblings are notified
- **Design** cooperative cancellation where goroutines check context before proceeding

## Why Errgroup with Context
A plain `errgroup.Group` runs all goroutines to completion even when one fails. If you launch 100 HTTP requests and the first one returns a 500 error, the other 99 still run -- wasting time, bandwidth, and server resources.

`errgroup.WithContext` solves this by associating a `context.Context` with the group. When any goroutine returns a non-nil error, the context is automatically cancelled. Sibling goroutines that check `ctx.Done()` can detect this and bail out early. The derived context is cancelled when the first error occurs OR when `Wait()` returns -- whichever happens first.

This is the standard pattern for "fail fast" concurrent operations: launch N tasks, cancel the rest as soon as one fails, collect the error.

## Step 1 -- Set Up and Observe Without Context

Run the starter code:

```bash
go mod tidy
go run main.go
```

The `withoutContext` function launches 5 tasks. Task 1 fails immediately, but all other tasks run to completion because there is no cancellation mechanism. Observe that all tasks print their "working" message even though the group already has an error.

### Intermediate Verification
You see output from all 5 tasks. The error from task 1 is reported by `Wait()`, but tasks 0, 2, 3, and 4 all complete their full work -- wasteful when you already know the operation failed.

## Step 2 -- Add WithContext for Automatic Cancellation

Implement the `withContext` function using `errgroup.WithContext`:

```go
func withContext() {
    fmt.Println("\n=== Errgroup WithContext ===")
    g, ctx := errgroup.WithContext(context.Background())

    for i := 0; i < 5; i++ {
        i := i
        g.Go(func() error {
            // Check if context is already cancelled before starting work
            select {
            case <-ctx.Done():
                fmt.Printf("  Task %d: cancelled before starting\n", i)
                return ctx.Err()
            default:
            }

            // Simulate work with periodic cancellation checks
            for step := 0; step < 3; step++ {
                select {
                case <-ctx.Done():
                    fmt.Printf("  Task %d: cancelled at step %d\n", i, step)
                    return ctx.Err()
                case <-time.After(100 * time.Millisecond):
                    fmt.Printf("  Task %d: step %d done\n", i, step)
                }
            }

            // Task 1 fails after completing its work
            if i == 1 {
                return fmt.Errorf("task %d: simulated failure", i)
            }

            return nil
        })
    }

    if err := g.Wait(); err != nil {
        fmt.Printf("Group error: %v\n", err)
    }
}
```

The key points:
1. `errgroup.WithContext` returns both a group and a derived context
2. When task 1 returns an error, the context is cancelled
3. Other tasks detect cancellation via `select` on `ctx.Done()`
4. Tasks must cooperatively check the context -- cancellation is not forced

### Intermediate Verification
```bash
go run main.go
```
Tasks that have not yet completed their work will print "cancelled" messages. The total execution time is shorter because work stops early after the failure.

## Step 3 -- Demonstrate the Cancellation Timeline

Implement `cancellationTimeline` to clearly show the timing of cancellation:

```go
func cancellationTimeline() {
    fmt.Println("\n=== Cancellation Timeline ===")
    start := time.Now()
    g, ctx := errgroup.WithContext(context.Background())

    // Fast task -- fails quickly
    g.Go(func() error {
        time.Sleep(100 * time.Millisecond)
        fmt.Printf("  [%v] Fast task: returning error\n", time.Since(start).Round(time.Millisecond))
        return fmt.Errorf("fast task failed")
    })

    // Slow task -- should get cancelled
    g.Go(func() error {
        for i := 0; i < 10; i++ {
            select {
            case <-ctx.Done():
                fmt.Printf("  [%v] Slow task: cancelled at iteration %d\n", time.Since(start).Round(time.Millisecond), i)
                return ctx.Err()
            case <-time.After(50 * time.Millisecond):
                fmt.Printf("  [%v] Slow task: iteration %d\n", time.Since(start).Round(time.Millisecond), i)
            }
        }
        return nil
    })

    if err := g.Wait(); err != nil {
        fmt.Printf("  [%v] Wait returned: %v\n", time.Since(start).Round(time.Millisecond), err)
    }
}
```

### Intermediate Verification
```bash
go run main.go
```
The timeline shows the slow task being cancelled shortly after the fast task fails at ~100ms. Without context, the slow task would run for ~500ms.

## Common Mistakes

### Not checking ctx.Done() in goroutines
**Wrong:**
```go
g, ctx := errgroup.WithContext(context.Background())
_ = ctx // context created but never used in goroutines
g.Go(func() error {
    time.Sleep(10 * time.Second) // blocks regardless of cancellation
    return nil
})
```
**What happens:** The context is cancelled but goroutines do not notice. You get no benefit from `WithContext` -- tasks still run to completion.

**Fix:** Always use `select` with `ctx.Done()` inside long-running goroutines:
```go
g.Go(func() error {
    select {
    case <-ctx.Done():
        return ctx.Err()
    case <-time.After(10 * time.Second):
        return nil
    }
})
```

### Using the parent context instead of the derived context
**Wrong:**
```go
parentCtx := context.Background()
g, _ := errgroup.WithContext(parentCtx)
g.Go(func() error {
    <-parentCtx.Done() // this context is NEVER cancelled by errgroup
    return nil
})
```
**What happens:** `parentCtx` is never cancelled because errgroup cancels only the derived context. The goroutine blocks forever.

**Fix:** Always use the context returned by `WithContext`:
```go
g, ctx := errgroup.WithContext(parentCtx)
g.Go(func() error {
    <-ctx.Done() // this IS cancelled when a sibling fails
    return ctx.Err()
})
```

### Returning ctx.Err() when your own task fails
**Wrong:**
```go
g.Go(func() error {
    if somethingFailed {
        return ctx.Err() // might be nil if you're the first to fail!
    }
    return nil
})
```
**What happens:** If your task is the first to fail, the context has not been cancelled yet, so `ctx.Err()` returns nil. Your error is lost.

**Fix:** Return your own error. Only return `ctx.Err()` when reacting to cancellation from a sibling:
```go
g.Go(func() error {
    if somethingFailed {
        return fmt.Errorf("my task failed: %w", err) // your own error
    }
    return nil
})
```

## Verify What You Learned

Modify the program to simulate 10 HTTP fetches where fetch 3 fails after 200ms. Measure total execution time with and without `WithContext`. The version with context should complete significantly faster because remaining fetches are cancelled.

## What's Next
Continue to [03-errgroup-setlimit](../03-errgroup-setlimit/03-errgroup-setlimit.md) to learn how `g.SetLimit(n)` controls the maximum number of concurrent goroutines in an errgroup.

## Summary
- `errgroup.WithContext` returns a group and a derived context that is cancelled on first error
- Goroutines must cooperatively check `ctx.Done()` to respond to cancellation
- Use `select` with `ctx.Done()` in loops and before long operations
- The derived context is also cancelled when `Wait()` returns (even without errors)
- Return your own descriptive error when your task fails; return `ctx.Err()` only when reacting to sibling cancellation
- This pattern implements "fail fast" semantics for concurrent operations

## Reference
- [errgroup.WithContext documentation](https://pkg.go.dev/golang.org/x/sync/errgroup#WithContext)
- [Go Blog: Context](https://go.dev/blog/context)
- [Context and Cancellation patterns](https://pkg.go.dev/context)
