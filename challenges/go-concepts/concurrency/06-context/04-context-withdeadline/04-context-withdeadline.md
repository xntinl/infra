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

Set a deadline 300ms in the future and simulate a 500ms operation:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func main() {
	deadline := time.Now().Add(300 * time.Millisecond)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	fmt.Printf("Deadline set to: %v\n", deadline.Format("15:04:05.000"))
	fmt.Printf("Current time:    %v\n", time.Now().Format("15:04:05.000"))

	select {
	case <-time.After(500 * time.Millisecond):
		fmt.Println("Operation completed")
	case <-ctx.Done():
		fmt.Printf("Deadline hit at: %v\n", time.Now().Format("15:04:05.000"))
		fmt.Printf("Error: %v\n", ctx.Err())
	}
}
```

### Verification
```bash
go run main.go
```
Expected output (times will vary):
```
Deadline set to: 14:30:01.300
Current time:    14:30:01.000
Deadline hit at: 14:30:01.300
Error: context deadline exceeded
```

The key difference from `WithTimeout`: you pass a `time.Time`, not a `time.Duration`. This matters when propagating deadlines from upstream callers.

## Step 2 -- Inspecting the Deadline

Use `ctx.Deadline()` to read back the deadline from a context. This method returns `(time.Time, bool)` -- the boolean is `false` for contexts without a deadline:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func main() {
	// WithDeadline context has a deadline.
	deadline := time.Now().Add(2 * time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	if d, ok := ctx.Deadline(); ok {
		fmt.Printf("WithDeadline context:  deadline=%v, remaining=~%v\n",
			d.Format("15:04:05.000"),
			time.Until(d).Round(time.Millisecond))
	}

	// Background context has NO deadline.
	bgCtx := context.Background()
	if _, ok := bgCtx.Deadline(); !ok {
		fmt.Println("Background context:    no deadline")
	}

	// WithTimeout also sets a deadline internally.
	timeoutCtx, cancelTimeout := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancelTimeout()

	if d, ok := timeoutCtx.Deadline(); ok {
		fmt.Printf("WithTimeout(1s):       deadline=%v\n", d.Format("15:04:05.000"))
	}
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
WithDeadline context:  deadline=14:30:03.000, remaining=~2s
Background context:    no deadline
WithTimeout(1s):       deadline=14:30:02.000
```

Both `WithDeadline` and `WithTimeout` set a deadline that is visible through `ctx.Deadline()`. This allows downstream code to check how much time remains and make decisions accordingly.

## Step 3 -- WithTimeout Is WithDeadline in Disguise

Show that `WithTimeout(parent, d)` behaves identically to `WithDeadline(parent, time.Now().Add(d))`:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func main() {
	now := time.Now()
	duration := 500 * time.Millisecond

	ctxTimeout, cancelTimeout := context.WithTimeout(context.Background(), duration)
	defer cancelTimeout()

	ctxDeadline, cancelDeadline := context.WithDeadline(context.Background(), now.Add(duration))
	defer cancelDeadline()

	deadlineFromTimeout, _ := ctxTimeout.Deadline()
	deadlineFromDeadline, _ := ctxDeadline.Deadline()

	diff := deadlineFromTimeout.Sub(deadlineFromDeadline).Abs()
	fmt.Printf("WithTimeout deadline:  %v\n", deadlineFromTimeout.Format("15:04:05.000000"))
	fmt.Printf("WithDeadline deadline: %v\n", deadlineFromDeadline.Format("15:04:05.000000"))
	fmt.Printf("Difference: %v (should be < 1ms)\n", diff)
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
WithTimeout deadline:  14:30:01.500000
WithDeadline deadline: 14:30:01.500000
Difference: 50us (should be < 1ms)
```

The tiny difference is the time elapsed between the two calls. They are functionally equivalent.

## Step 4 -- Shorter Deadline Wins

A child context cannot extend its parent's deadline. This is a fundamental invariant of the context tree:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func main() {
	parent, cancelParent := context.WithDeadline(
		context.Background(),
		time.Now().Add(200*time.Millisecond),
	)
	defer cancelParent()

	// Attempt to set a much longer deadline on the child.
	child, cancelChild := context.WithDeadline(parent, time.Now().Add(5*time.Second))
	defer cancelChild()

	parentDeadline, _ := parent.Deadline()
	childDeadline, _ := child.Deadline()

	fmt.Printf("Parent deadline: %v (200ms from now)\n", parentDeadline.Format("15:04:05.000"))
	fmt.Printf("Child deadline:  %v (same as parent!)\n", childDeadline.Format("15:04:05.000"))
	fmt.Println("(Child inherits parent's shorter deadline)")

	<-child.Done()
	fmt.Printf("Child cancelled: %v\n", child.Err())
	fmt.Println("Actual wait: ~200ms (parent's deadline, not child's 5s)")
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
Parent deadline: 14:30:01.200 (200ms from now)
Child deadline:  14:30:01.200 (same as parent!)
(Child inherits parent's shorter deadline)
Child cancelled: context deadline exceeded
Actual wait: ~200ms (parent's deadline, not child's 5s)
```

The child's `Deadline()` returns the parent's deadline because it is earlier. You can tighten a deadline by deriving a child with a shorter one, but you can never extend beyond the parent.

## Step 5 -- Pipeline with Deadline Budget

A single deadline context shared across multiple pipeline stages. Each stage consumes part of the budget, and later stages can check how much time remains:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func pipelineStage(ctx context.Context, name string, work time.Duration) (string, error) {
	deadline, hasDeadline := ctx.Deadline()
	if hasDeadline {
		remaining := time.Until(deadline).Round(time.Millisecond)
		fmt.Printf("[%s] starting (budget remaining: ~%v)\n", name, remaining)
	}

	select {
	case <-time.After(work):
		fmt.Printf("[%s] completed in %v\n", name, work)
		return name, nil
	case <-ctx.Done():
		return "", fmt.Errorf("%s: %w", name, ctx.Err())
	}
}

func main() {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(500*time.Millisecond))
	defer cancel()

	stages := []struct {
		name string
		work time.Duration
	}{
		{"stage-1", 100 * time.Millisecond},
		{"stage-2", 100 * time.Millisecond},
		{"stage-3", 100 * time.Millisecond},
	}

	result := ""
	for _, s := range stages {
		name, err := pipelineStage(ctx, s.name, s.work)
		if err != nil {
			fmt.Printf("Pipeline failed: %v\n", err)
			return
		}
		if result != "" {
			result += " -> "
		}
		result += name
	}
	fmt.Printf("Pipeline result: %s -> done\n", result)
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
[stage-1] starting (budget remaining: ~500ms)
[stage-1] completed in 100ms
[stage-2] starting (budget remaining: ~400ms)
[stage-2] completed in 100ms
[stage-3] starting (budget remaining: ~300ms)
[stage-3] completed in 100ms
Pipeline result: stage-1 -> stage-2 -> stage-3 -> done
```

This pattern is common in request pipelines where middleware, business logic, and data access all share a single request deadline.

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
func process(ctx context.Context) error {
    // starts expensive work without checking if deadline already passed
    return expensiveOperation()
}
```
**Fix:**
```go
func process(ctx context.Context) error {
    if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < minimumRequired {
        return fmt.Errorf("insufficient time: need %v, have %v",
            minimumRequired, time.Until(deadline))
    }
    return expensiveOperation()
}
```

This fail-fast check avoids starting work that will certainly be cancelled.

## Verify What You Learned

Simulate a "request pipeline" where an incoming request has a deadline. Pass the context through three stages (each taking 100ms). Test with a 500ms deadline (all pass) and a 250ms deadline (observe which stage gets cut off):

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func runPipeline(label string, budget time.Duration) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(budget))
	defer cancel()

	stages := []string{"stage-1", "stage-2", "stage-3"}
	result := ""

	for _, name := range stages {
		select {
		case <-time.After(100 * time.Millisecond):
			if result != "" {
				result += " -> "
			}
			result += name
		case <-ctx.Done():
			fmt.Printf("%s: failed at %s: %v\n", label, name, ctx.Err())
			return
		}
	}
	fmt.Printf("%s: %s -> done\n", label, result)
}

func main() {
	runPipeline("Generous (500ms)", 500*time.Millisecond)
	runPipeline("Tight (250ms)", 250*time.Millisecond)
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
Generous (500ms): stage-1 -> stage-2 -> stage-3 -> done
Tight (250ms): failed at stage-3: context deadline exceeded
```

## What's Next
Continue to [05-context-withvalue](../05-context-withvalue/05-context-withvalue.md) to learn how to attach request-scoped data to contexts.

## Summary
- `context.WithDeadline(parent, time)` cancels the context at an absolute point in time
- `WithTimeout(parent, d)` is equivalent to `WithDeadline(parent, time.Now().Add(d))`
- `ctx.Deadline()` returns the deadline and whether one is set
- A child context inherits the shorter of its own and its parent's deadline
- Use `WithDeadline` when the deadline is an absolute time (propagated from upstream)
- Use `WithTimeout` when you want a relative duration from "now"
- Check remaining time with `time.Until(deadline)` before starting expensive work

## Reference
- [Package context: WithDeadline](https://pkg.go.dev/context#WithDeadline)
- [Package context: WithTimeout](https://pkg.go.dev/context#WithTimeout)
- [time.Until](https://pkg.go.dev/time#Until)
