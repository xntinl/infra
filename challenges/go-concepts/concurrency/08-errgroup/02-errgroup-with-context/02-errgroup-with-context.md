---
difficulty: intermediate
concepts: [errgroup.WithContext, context.Context, automatic cancellation, ctx.Done, ctx.Err]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [errgroup basics, context.Context, context.WithCancel]
---

# 2. Errgroup with Context


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

## Step 1 -- Observe Without Context (Wasted Work)

Run the program:

```bash
go mod tidy
go run main.go
```

The `withoutContext` function launches 4 tasks. Task 1 fails immediately, but tasks 0, 2, and 3 run all three steps to completion:

```go
package main

import (
    "fmt"
    "time"

    "golang.org/x/sync/errgroup"
)

func main() {
    fmt.Println("=== Without Context ===")
    var g errgroup.Group

    for i := 0; i < 4; i++ {
        i := i
        g.Go(func() error {
            if i == 1 {
                fmt.Printf("  Task %d: failing immediately\n", i)
                return fmt.Errorf("task %d failed", i)
            }
            for step := 0; step < 3; step++ {
                time.Sleep(80 * time.Millisecond)
                fmt.Printf("  Task %d: step %d (still working despite failure)\n", i, step)
            }
            return nil
        })
    }

    if err := g.Wait(); err != nil {
        fmt.Printf("Group error: %v\n", err)
    }
}
```

**Expected output:**
```
=== Without Context ===
  Task 1: failing immediately
  Task 0: step 0 (still working despite failure)
  Task 2: step 0 (still working despite failure)
  Task 3: step 0 (still working despite failure)
  ...all tasks complete all 3 steps...
Group error: task 1 failed
```

The problem: tasks 0, 2, and 3 perform 240ms of useless work after task 1 has already failed. In production, this could be 100 HTTP requests hitting a server that is already returning errors.

## Step 2 -- Add WithContext for Automatic Cancellation

Replace the plain errgroup with `errgroup.WithContext`. Make goroutines check `ctx.Done()` between work steps:

```go
package main

import (
    "context"
    "fmt"
    "time"

    "golang.org/x/sync/errgroup"
)

func main() {
    fmt.Println("=== With Context ===")
    g, ctx := errgroup.WithContext(context.Background())

    for i := 0; i < 4; i++ {
        i := i
        g.Go(func() error {
            // Check if context is already cancelled before starting
            select {
            case <-ctx.Done():
                fmt.Printf("  Task %d: cancelled before starting\n", i)
                return ctx.Err()
            default:
            }

            if i == 1 {
                fmt.Printf("  Task %d: failing -- this cancels the context\n", i)
                return fmt.Errorf("task %d failed", i)
            }

            for step := 0; step < 3; step++ {
                select {
                case <-ctx.Done():
                    fmt.Printf("  Task %d: cancelled at step %d\n", i, step)
                    return ctx.Err()
                case <-time.After(80 * time.Millisecond):
                    fmt.Printf("  Task %d: step %d done\n", i, step)
                }
            }
            return nil
        })
    }

    if err := g.Wait(); err != nil {
        fmt.Printf("Group error: %v\n", err)
    }
}
```

**Expected output:**
```
=== With Context ===
  Task 1: failing -- this cancels the context
  Task 0: cancelled at step 0
  Task 2: cancelled at step 0
  Task 3: cancelled at step 0
Group error: task 1 failed
```

The key points:
1. `errgroup.WithContext` returns both a group and a derived context
2. When task 1 returns an error, the context is cancelled automatically
3. Other tasks detect cancellation via `select` on `ctx.Done()`
4. Cancellation is **cooperative** -- tasks must check the context themselves

## Step 3 -- Observe the Cancellation Timeline

This example shows precise timing. A fast task fails at ~100ms; a slow task (checking every ~50ms) detects the cancellation shortly after:

```go
package main

import (
    "context"
    "fmt"
    "time"

    "golang.org/x/sync/errgroup"
)

func main() {
    fmt.Println("=== Cancellation Timeline ===")
    start := time.Now()
    g, ctx := errgroup.WithContext(context.Background())

    g.Go(func() error {
        time.Sleep(100 * time.Millisecond)
        fmt.Printf("  [%v] Fast task: returning error\n", time.Since(start).Round(time.Millisecond))
        return fmt.Errorf("fast task failed")
    })

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

**Expected output:**
```
=== Cancellation Timeline ===
  [50ms]  Slow task: iteration 0
  [100ms] Fast task: returning error
  [100ms] Slow task: cancelled at iteration 1
  [100ms] Wait returned: fast task failed
```

Without context, the slow task would run for 500ms. With context, it stops at ~100ms.

## Step 4 -- Context Is Also Cancelled on Success

An important detail: the derived context is **always** cancelled when `Wait()` returns, even if all tasks succeeded. Do not use the derived context for work that outlives the group:

```go
package main

import (
    "context"
    "fmt"

    "golang.org/x/sync/errgroup"
)

func main() {
    g, ctx := errgroup.WithContext(context.Background())

    g.Go(func() error {
        return nil // succeeds
    })

    _ = g.Wait()
    fmt.Printf("ctx.Err() after Wait: %v\n", ctx.Err())
}
```

**Expected output:**
```
ctx.Err() after Wait: context canceled
```

## Step 5 -- Passing the Context to Library Functions

The real power of errgroup+context: pass `ctx` to standard library functions that accept `context.Context`. When a sibling fails, HTTP requests, database queries, and other I/O get cancelled automatically:

```go
package main

import (
    "context"
    "fmt"
    "time"

    "golang.org/x/sync/errgroup"
)

func main() {
    g, ctx := errgroup.WithContext(context.Background())

    for i := 0; i < 3; i++ {
        i := i
        g.Go(func() error {
            if i == 1 {
                time.Sleep(50 * time.Millisecond)
                return fmt.Errorf("task %d deliberate failure", i)
            }
            // In real code: http.NewRequestWithContext(ctx, ...)
            err := simulateHTTPFetch(ctx, 200*time.Millisecond)
            if err != nil {
                fmt.Printf("  Task %d: fetch cancelled: %v\n", i, err)
                return err
            }
            fmt.Printf("  Task %d: fetch completed\n", i)
            return nil
        })
    }

    if err := g.Wait(); err != nil {
        fmt.Printf("Group error: %v\n", err)
    }
}

func simulateHTTPFetch(ctx context.Context, duration time.Duration) error {
    select {
    case <-ctx.Done():
        return ctx.Err()
    case <-time.After(duration):
        return nil
    }
}
```

**Expected output:**
```
  Task 1: deliberate failure
  Task 0: fetch cancelled: context canceled
  Task 2: fetch cancelled: context canceled
Group error: task 1 deliberate failure
```

## Common Mistakes

### Not checking ctx.Done() in goroutines

**Wrong:**
```go
g, ctx := errgroup.WithContext(context.Background())
_ = ctx // created but never used
g.Go(func() error {
    time.Sleep(10 * time.Second) // blocks regardless of cancellation
    return nil
})
```

**What happens:** The context is cancelled but goroutines do not notice. You get no benefit from `WithContext`.

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

### Using the parent context instead of the derived one

**Wrong:**
```go
parentCtx := context.Background()
g, _ := errgroup.WithContext(parentCtx)
g.Go(func() error {
    <-parentCtx.Done() // parentCtx is NEVER cancelled by errgroup
    return nil
})
```

**What happens:** `parentCtx` is never cancelled. The goroutine blocks forever.

**Fix:** Always use the context returned by `WithContext`:
```go
g, ctx := errgroup.WithContext(parentCtx)
g.Go(func() error {
    <-ctx.Done() // this IS cancelled when a sibling fails
    return ctx.Err()
})
```

### Returning ctx.Err() for your own failure

**Wrong:**
```go
g.Go(func() error {
    if somethingFailed {
        return ctx.Err() // might be nil if you are the first to fail!
    }
    return nil
})
```

**What happens:** If your task is the first to fail, the context has not been cancelled yet, so `ctx.Err()` returns nil. Your error is lost.

**Fix:** Return your own error. Only return `ctx.Err()` when reacting to cancellation by a sibling:
```go
g.Go(func() error {
    if somethingFailed {
        return fmt.Errorf("my task failed: %w", err) // your own error
    }
    return nil
})
```

## Verify What You Learned

Run the full program and confirm:
1. Without context, all tasks complete despite the failure
2. With context, sibling tasks cancel early
3. The timeline shows cancellation propagating within one check interval
4. The derived context is cancelled even on success

```bash
go run main.go
```

## What's Next
Continue to [03-errgroup-setlimit](../03-errgroup-setlimit/03-errgroup-setlimit.md) to learn how `g.SetLimit(n)` controls the maximum number of concurrent goroutines.

## Summary
- `errgroup.WithContext` returns a group and a derived context that is cancelled on first error
- Goroutines must cooperatively check `ctx.Done()` to respond to cancellation
- Use `select` with `ctx.Done()` in loops and before long operations
- The derived context is also cancelled when `Wait()` returns (even without errors)
- Return your own descriptive error when your task fails; return `ctx.Err()` only when reacting to sibling cancellation
- Pass the derived context to library functions (`http.NewRequestWithContext`, etc.) for automatic cancellation

## Reference
- [errgroup.WithContext documentation](https://pkg.go.dev/golang.org/x/sync/errgroup#WithContext)
- [Go Blog: Context](https://go.dev/blog/context)
- [Context and Cancellation patterns](https://pkg.go.dev/context)
