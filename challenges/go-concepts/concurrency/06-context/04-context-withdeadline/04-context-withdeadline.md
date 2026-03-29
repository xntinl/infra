# 4. Context WithDeadline

<!--
difficulty: intermediate
concepts: [context.WithDeadline, absolute deadline, time.Now, DeadlineExceeded, WithTimeout equivalence]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [context.Background, context.WithCancel, context.WithTimeout, time package]
-->

## Prerequisites
- Go 1.22+ installed
- Completed [03-context-withtimeout](../03-context-withtimeout/03-context-withtimeout.md)
- Understanding of `time.Time` and `time.Now()`

## Learning Objectives
After completing this exercise, you will be able to:
- **Create** a context with an absolute deadline using `context.WithDeadline`
- **Inspect** a context's deadline with `ctx.Deadline()`
- **Explain** that `WithTimeout` is shorthand for `WithDeadline(parent, time.Now().Add(duration))`
- **Choose** between `WithDeadline` and `WithTimeout` based on the situation

## Why WithDeadline

While `WithTimeout` specifies "cancel after this duration from now," `WithDeadline` specifies "cancel at this exact point in time." The distinction matters when deadlines are computed externally.

Consider a request arriving at 14:00:00 with a deadline of 14:00:05 (set by the upstream caller). You receive it at 14:00:02 after network and middleware processing. You should propagate the original deadline (14:00:05), not create a new 5-second timeout from 14:00:02 -- that would extend the deadline to 14:00:07, violating the contract.

`WithDeadline` is the lower-level primitive. In fact, `WithTimeout(parent, d)` is implemented internally as `WithDeadline(parent, time.Now().Add(d))`. Understanding both lets you choose the right tool: `WithTimeout` for relative durations, `WithDeadline` for absolute points in time.

## Step 1 -- Basic Deadline

Edit `main.go` and implement `basicDeadline`. Set a deadline 300ms in the future and simulate a 500ms operation:

```go
func basicDeadline() {
    fmt.Println("=== Basic WithDeadline ===")

    deadline := time.Now().Add(300 * time.Millisecond)
    ctx, cancel := context.WithDeadline(context.Background(), deadline)
    defer cancel()

    fmt.Printf("  Deadline set to: %v\n", deadline.Format("15:04:05.000"))
    fmt.Printf("  Current time:    %v\n", time.Now().Format("15:04:05.000"))

    select {
    case <-time.After(500 * time.Millisecond):
        fmt.Println("  Operation completed")
    case <-ctx.Done():
        fmt.Printf("  Deadline hit at: %v\n", time.Now().Format("15:04:05.000"))
        fmt.Printf("  Error: %v\n", ctx.Err())
    }

    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output (times will vary):
```
=== Basic WithDeadline ===
  Deadline set to: 14:30:01.300
  Current time:    14:30:01.000
  Deadline hit at: 14:30:01.300
  Error: context deadline exceeded
```

## Step 2 -- Inspecting the Deadline

Implement `inspectDeadline`. Use `ctx.Deadline()` to read back the deadline from a context:

```go
func inspectDeadline() {
    fmt.Println("=== Inspecting Deadline ===")

    deadline := time.Now().Add(2 * time.Second)
    ctx, cancel := context.WithDeadline(context.Background(), deadline)
    defer cancel()

    if d, ok := ctx.Deadline(); ok {
        fmt.Printf("  Context has deadline: %v\n", d.Format("15:04:05.000"))
        fmt.Printf("  Time remaining: %v\n", time.Until(d).Round(time.Millisecond))
    }

    // Compare with a plain Background context
    bgCtx := context.Background()
    if _, ok := bgCtx.Deadline(); !ok {
        fmt.Println("  Background context has no deadline")
    }

    // WithTimeout also sets a deadline
    timeoutCtx, cancelTimeout := context.WithTimeout(context.Background(), 1*time.Second)
    defer cancelTimeout()

    if d, ok := timeoutCtx.Deadline(); ok {
        fmt.Printf("  WithTimeout(1s) deadline: %v\n", d.Format("15:04:05.000"))
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
=== Inspecting Deadline ===
  Context has deadline: 14:30:03.000
  Time remaining: 1.999s
  Background context has no deadline
  WithTimeout(1s) deadline: 14:30:02.000
```

Both `WithDeadline` and `WithTimeout` set a deadline that is visible through `ctx.Deadline()`.

## Step 3 -- WithTimeout Is WithDeadline in Disguise

Implement `equivalenceDemo`. Show that `WithTimeout(parent, d)` behaves identically to `WithDeadline(parent, time.Now().Add(d))`:

```go
func equivalenceDemo() {
    fmt.Println("=== WithTimeout == WithDeadline(now + d) ===")

    now := time.Now()
    duration := 500 * time.Millisecond

    ctxTimeout, cancelTimeout := context.WithTimeout(context.Background(), duration)
    defer cancelTimeout()

    ctxDeadline, cancelDeadline := context.WithDeadline(context.Background(), now.Add(duration))
    defer cancelDeadline()

    deadlineFromTimeout, _ := ctxTimeout.Deadline()
    deadlineFromDeadline, _ := ctxDeadline.Deadline()

    diff := deadlineFromTimeout.Sub(deadlineFromDeadline).Abs()
    fmt.Printf("  WithTimeout deadline:  %v\n", deadlineFromTimeout.Format("15:04:05.000000"))
    fmt.Printf("  WithDeadline deadline: %v\n", deadlineFromDeadline.Format("15:04:05.000000"))
    fmt.Printf("  Difference: %v (should be < 1ms)\n", diff)
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== WithTimeout == WithDeadline(now + d) ===
  WithTimeout deadline:  14:30:01.500000
  WithDeadline deadline: 14:30:01.500000
  Difference: 50us (should be < 1ms)
```

## Step 4 -- Shorter Deadline Wins

Implement `shorterDeadlineWins`. Show that a child context cannot extend its parent's deadline:

```go
func shorterDeadlineWins() {
    fmt.Println("=== Shorter Deadline Always Wins ===")

    parent, cancelParent := context.WithDeadline(
        context.Background(),
        time.Now().Add(200*time.Millisecond),
    )
    defer cancelParent()

    // Attempt to set a longer deadline on the child
    child, cancelChild := context.WithDeadline(parent, time.Now().Add(5*time.Second))
    defer cancelChild()

    parentDeadline, _ := parent.Deadline()
    childDeadline, _ := child.Deadline()

    fmt.Printf("  Parent deadline: %v\n", parentDeadline.Format("15:04:05.000"))
    fmt.Printf("  Child deadline:  %v\n", childDeadline.Format("15:04:05.000"))
    fmt.Println("  (Child inherits parent's shorter deadline)")

    <-child.Done()
    fmt.Printf("  Child cancelled: %v\n", child.Err())
    fmt.Printf("  Actual wait: ~200ms (parent's deadline, not child's 5s)\n\n")
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Shorter Deadline Always Wins ===
  Parent deadline: 14:30:01.200
  Child deadline:  14:30:01.200
  (Child inherits parent's shorter deadline)
  Child cancelled: context deadline exceeded
  Actual wait: ~200ms (parent's deadline, not child's 5s)
```

The child's `Deadline()` returns the parent's deadline because it is earlier. The child cannot extend beyond its parent.

## Common Mistakes

### Confusing Deadline with Timeout
**Wrong:** Using `WithDeadline` with a duration instead of an absolute time:
```go
ctx, cancel := context.WithDeadline(parent, 5*time.Second) // compile error: wrong type
```
**Fix:** `WithDeadline` takes a `time.Time`, not a `time.Duration`:
```go
ctx, cancel := context.WithDeadline(parent, time.Now().Add(5*time.Second))
// or simply:
ctx, cancel := context.WithTimeout(parent, 5*time.Second)
```

### Assuming Child Can Extend Parent Deadline
As shown in Step 4, a child context always gets the minimum of its own deadline and its parent's. You cannot use `WithDeadline` to grant more time than the parent allows.

### Not Checking Deadline Before Starting Expensive Work
**Wrong:**
```go
func process(ctx context.Context) {
    // starts expensive work without checking if deadline already passed
    expensiveOperation()
}
```
**Fix:**
```go
func process(ctx context.Context) {
    if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < minimumRequired {
        return fmt.Errorf("insufficient time: need %v, have %v", minimumRequired, time.Until(deadline))
    }
    expensiveOperation()
}
```

## Verify What You Learned

Implement `verifyKnowledge`: simulate a "request pipeline" where an incoming request has a deadline 500ms from now. Pass the context through three stages (each taking 100ms). After each stage, print the remaining time. Then repeat with a 250ms deadline and observe which stage gets cut off.

## What's Next
Continue to [05-context-withvalue](../05-context-withvalue/05-context-withvalue.md) to learn how to attach request-scoped data to contexts.

## Summary
- `context.WithDeadline(parent, time)` cancels the context at an absolute point in time
- `WithTimeout(parent, d)` is equivalent to `WithDeadline(parent, time.Now().Add(d))`
- `ctx.Deadline()` returns the deadline and whether one is set
- A child context inherits the shorter of its own and its parent's deadline
- Use `WithDeadline` when the deadline is an absolute time (propagated from upstream)
- Use `WithTimeout` when you want a relative duration from "now"

## Reference
- [Package context: WithDeadline](https://pkg.go.dev/context#WithDeadline)
- [Package context: WithTimeout](https://pkg.go.dev/context#WithTimeout)
- [time.Until](https://pkg.go.dev/time#Until)
