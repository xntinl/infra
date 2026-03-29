# 2. Context WithCancel

<!--
difficulty: basic
concepts: [context.WithCancel, cancel function, ctx.Done channel, cancellation propagation]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [context.Background, goroutines, channels basics]
-->

## Prerequisites
- Go 1.22+ installed
- Completed [01-context-background-and-todo](../01-context-background-and-todo/01-context-background-and-todo.md)
- Familiarity with goroutines and channel receive operations (`<-ch`)

## Learning Objectives
After completing this exercise, you will be able to:
- **Create** a cancellable context using `context.WithCancel`
- **Signal** goroutines to stop by calling the cancel function
- **Listen** for cancellation via the `ctx.Done()` channel
- **Observe** that cancellation propagates from parent to child contexts

## Why WithCancel

In real programs, goroutines must be stoppable. A goroutine that runs forever leaks memory and CPU. The `context.WithCancel` function creates a derived context paired with a `cancel` function. When you call `cancel()`, the context's `Done()` channel is closed, and every goroutine listening on that channel receives the signal simultaneously.

This is the most fundamental cancellation mechanism in Go. HTTP servers use it to cancel request processing when the client disconnects. CLI tools use it to stop background work when the user presses Ctrl+C. Pipelines use it to tear down all stages when one stage fails.

The key insight: cancellation is cooperative. The goroutine must explicitly check `ctx.Done()` and choose to stop. The context does not forcibly kill anything -- it sends a signal that the goroutine must honor.

## Step 1 -- Basic Cancel and Done

Edit `main.go` and implement `basicCancel`. Create a cancellable context, pass it to a goroutine that loops until cancelled, then cancel from main:

```go
func basicCancel() {
    fmt.Println("=== Basic WithCancel ===")

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel() // always defer cancel to avoid resource leaks

    go func(ctx context.Context) {
        for i := 0; ; i++ {
            select {
            case <-ctx.Done():
                fmt.Printf("  goroutine: stopped (reason: %v)\n", ctx.Err())
                return
            default:
                fmt.Printf("  goroutine: working... iteration %d\n", i)
                time.Sleep(100 * time.Millisecond)
            }
        }
    }(ctx)

    // Let the goroutine work for a bit
    time.Sleep(350 * time.Millisecond)

    fmt.Println("  main: calling cancel()")
    cancel()

    // Give goroutine time to receive the signal and print
    time.Sleep(50 * time.Millisecond)
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output (approximately):
```
=== Basic WithCancel ===
  goroutine: working... iteration 0
  goroutine: working... iteration 1
  goroutine: working... iteration 2
  main: calling cancel()
  goroutine: stopped (reason: context canceled)
```

The goroutine runs 3 iterations (~300ms), then main calls `cancel()`, closing the `Done()` channel. The goroutine's `select` picks up the signal and exits.

## Step 2 -- Cancellation Propagates to Children

Implement `cancellationPropagation`. Create a parent context, derive two child contexts from it, and show that cancelling the parent stops both children:

```go
func cancellationPropagation() {
    fmt.Println("=== Cancellation Propagation ===")

    parent, cancelParent := context.WithCancel(context.Background())
    child1, cancelChild1 := context.WithCancel(parent)
    child2, cancelChild2 := context.WithCancel(parent)
    defer cancelChild1()
    defer cancelChild2()

    // Launch a worker for each child context
    worker := func(name string, ctx context.Context) {
        <-ctx.Done()
        fmt.Printf("  %s: stopped (reason: %v)\n", name, ctx.Err())
    }

    go worker("child1", child1)
    go worker("child2", child2)

    fmt.Println("  Cancelling parent context...")
    cancelParent()

    // Give workers time to receive the signal
    time.Sleep(50 * time.Millisecond)
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output (order of children may vary):
```
=== Cancellation Propagation ===
  Cancelling parent context...
  child1: stopped (reason: context canceled)
  child2: stopped (reason: context canceled)
```

Both children are cancelled when the parent is cancelled. This is the tree structure of contexts in action: cancellation flows downward.

## Step 3 -- Cancel Only a Child

Implement `cancelOnlyChild`. Show that cancelling a child does not affect the parent or siblings:

```go
func cancelOnlyChild() {
    fmt.Println("=== Cancel Only Child ===")

    parent, cancelParent := context.WithCancel(context.Background())
    defer cancelParent()

    child1, cancelChild1 := context.WithCancel(parent)
    child2, cancelChild2 := context.WithCancel(parent)
    defer cancelChild2()

    fmt.Println("  Cancelling child1 only...")
    cancelChild1()

    // Check states
    time.Sleep(10 * time.Millisecond)

    fmt.Printf("  parent.Err(): %v\n", parent.Err())
    fmt.Printf("  child1.Err(): %v\n", child1.Err())
    fmt.Printf("  child2.Err(): %v\n", child2.Err())
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Cancel Only Child ===
  Cancelling child1 only...
  parent.Err(): <nil>
  child1.Err(): context canceled
  child2.Err(): <nil>
```

Cancellation flows down, never up. The parent and sibling remain active.

## Common Mistakes

### Forgetting to Call Cancel
**Wrong:**
```go
ctx, cancel := context.WithCancel(parentCtx)
_ = cancel // unused -- resource leak!
// use ctx...
```
**What happens:** The derived context and its internal goroutine are never cleaned up, causing a resource leak.

**Fix:** Always `defer cancel()` immediately after creating the context:
```go
ctx, cancel := context.WithCancel(parentCtx)
defer cancel()
```

### Not Checking ctx.Done() in the Goroutine
**Wrong:**
```go
go func(ctx context.Context) {
    for {
        doWork() // never checks ctx.Done() -- goroutine runs forever
    }
}(ctx)
```
**Fix:**
```go
go func(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        default:
            doWork()
        }
    }
}(ctx)
```

### Calling Cancel Multiple Times
This is actually safe. Calling `cancel()` more than once is a no-op after the first call. The Go documentation explicitly states this. However, relying on it as a pattern is confusing -- prefer a single, clear cancellation point.

## Verify What You Learned

Implement `verifyKnowledge`: create a root context, derive three levels of child contexts (grandparent -> parent -> child). Launch a goroutine on each that prints when it is cancelled. Cancel the middle (parent) context and verify that the grandparent is unaffected while both parent and child are cancelled.

## What's Next
Continue to [03-context-withtimeout](../03-context-withtimeout/03-context-withtimeout.md) to learn how to automatically cancel a context after a specified duration.

## Summary
- `context.WithCancel` returns a derived context and a `cancel` function
- Calling `cancel()` closes the `Done()` channel, signaling all listeners
- Cancellation propagates from parent to all descendant contexts
- Cancellation never propagates upward -- parent and siblings are unaffected
- Always `defer cancel()` to prevent resource leaks
- Goroutines must cooperatively check `ctx.Done()` to respond to cancellation

## Reference
- [Package context: WithCancel](https://pkg.go.dev/context#WithCancel)
- [Go Blog: Context](https://go.dev/blog/context)
- [Go Concurrency Patterns: Pipelines](https://go.dev/blog/pipelines)
