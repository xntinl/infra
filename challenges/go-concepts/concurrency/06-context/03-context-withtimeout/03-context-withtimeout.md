---
difficulty: intermediate
concepts: [context.WithTimeout, automatic cancellation, ctx.Done, ctx.Err, DeadlineExceeded, defer cancel]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [context.Background, context.WithCancel, goroutines, select]
---

# 3. Context WithTimeout


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

Create a context with a short timeout and simulate a slow operation that exceeds it:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	fmt.Println("Starting slow operation (needs 500ms, allowed 200ms)...")

	select {
	case <-time.After(500 * time.Millisecond):
		fmt.Println("Operation completed successfully")
	case <-ctx.Done():
		fmt.Printf("Operation aborted: %v\n", ctx.Err())
	}
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
Starting slow operation (needs 500ms, allowed 200ms)...
Operation aborted: context deadline exceeded
```

The operation needed 500ms but the context only allowed 200ms. The `ctx.Done()` channel closed first, and `ctx.Err()` returns `context.DeadlineExceeded`. This is a different error from `context.Canceled`, which is returned on manual cancellation.

## Step 2 -- Fast Operation Completes Before Timeout

Show that when the operation finishes before the timeout, everything proceeds normally:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel() // still required even though timeout will not fire

	fmt.Println("Starting fast operation (needs 100ms, allowed 500ms)...")

	select {
	case <-time.After(100 * time.Millisecond):
		fmt.Println("Operation completed successfully")
	case <-ctx.Done():
		fmt.Printf("Operation aborted: %v\n", ctx.Err())
	}

	fmt.Printf("Context error after success: %v\n", ctx.Err())
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
Starting fast operation (needs 100ms, allowed 500ms)...
Operation completed successfully
Context error after success: <nil>
```

The operation finished in 100ms, well within the 500ms timeout. `ctx.Err()` is nil because the timeout has not fired yet. The deferred `cancel()` is still important -- it stops the internal timer and frees resources immediately instead of waiting for garbage collection.

## Step 3 -- Timeout with Goroutine Worker

Pass the timeout context to a goroutine that simulates work in a loop, checking `ctx.Done()` between iterations:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()

	done := make(chan string)

	go func(ctx context.Context) {
		for i := 1; ; i++ {
			select {
			case <-ctx.Done():
				done <- fmt.Sprintf("worker stopped at item %d: %v", i, ctx.Err())
				return
			default:
				fmt.Printf("worker: processing item %d\n", i)
				time.Sleep(100 * time.Millisecond)
			}
		}
	}(ctx)

	result := <-done
	fmt.Println(result)
}
```

### Verification
```bash
go run main.go
```
Expected output (approximately):
```
worker: processing item 1
worker: processing item 2
worker: processing item 3
worker stopped at item 4: context deadline exceeded
```

The worker processes items until the 350ms timeout fires. The goroutine detects the cancellation via `ctx.Done()` and reports back through the `done` channel.

## Step 4 -- Manual Cancel Before Timeout

Show that calling `cancel()` before the timeout triggers `context.Canceled` instead of `context.DeadlineExceeded`:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	go func() {
		<-ctx.Done()
		fmt.Printf("goroutine: context ended: %v\n", ctx.Err())
	}()

	time.Sleep(100 * time.Millisecond)
	fmt.Println("main: calling cancel() manually (timeout was 5s)")
	cancel()

	time.Sleep(50 * time.Millisecond)
	fmt.Println("Key insight: Canceled (not DeadlineExceeded) because we cancelled manually")
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
main: calling cancel() manually (timeout was 5s)
goroutine: context ended: context canceled
Key insight: Canceled (not DeadlineExceeded) because we cancelled manually
```

When you cancel manually, `ctx.Err()` returns `context.Canceled`, not `context.DeadlineExceeded`. This distinction lets callers differentiate "we chose to stop" from "we ran out of time" -- useful for metrics, logging, and retry decisions.

## Step 5 -- Child Cannot Extend Parent Timeout

A child context cannot have a longer timeout than its parent. The shorter deadline always wins:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func main() {
	parent, cancelParent := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancelParent()

	child, cancelChild := context.WithTimeout(parent, 10*time.Second)
	defer cancelChild()

	parentDeadline, _ := parent.Deadline()
	childDeadline, _ := child.Deadline()

	fmt.Printf("Parent deadline remaining: ~%v\n", time.Until(parentDeadline).Round(time.Millisecond))
	fmt.Printf("Child requested: 10s\n")
	fmt.Printf("Child actual remaining:    ~%v  (parent's wins)\n",
		time.Until(childDeadline).Round(time.Millisecond))
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
Parent deadline remaining: ~1s
Child requested: 10s
Child actual remaining:    ~1s  (parent's wins)
```

The child inherits the parent's shorter deadline. This is a fundamental rule: you can tighten a timeout by creating a child with a shorter duration, but you can never loosen it.

## Common Mistakes

### Not Deferring Cancel on WithTimeout
**Wrong:**
```go
package main

import (
	"context"
	"fmt"
	"time"
)

func main() {
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	// forgot defer cancel() -- timer goroutine leaks!
	fmt.Printf("ctx.Err(): %v\n", ctx.Err())
}
```
**What happens:** Even if the timeout fires, internal resources (a timer goroutine) are not freed until the parent is cancelled or garbage collected. This leaks resources, especially in long-running servers.

**Fix:** Always `defer cancel()` immediately:
```go
package main

import (
	"context"
	"fmt"
	"time"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	fmt.Printf("ctx.Err(): %v\n", ctx.Err())
}
```

### Setting Timeout Longer Than Parent's
**Wrong:**
```go
package main

import (
	"context"
	"fmt"
	"time"
)

func main() {
	parent, cancelParent := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancelParent()

	child, cancelChild := context.WithTimeout(parent, 10*time.Second) // this 10s is useless
	defer cancelChild()

	childDeadline, _ := child.Deadline()
	fmt.Printf("Child deadline: ~%v (not 10s!)\n", time.Until(childDeadline).Round(time.Millisecond))
}
```
**What happens:** The child inherits the parent's 1-second deadline. The 10-second timeout on the child is never reached because the parent cancels first. This is not an error, but it is misleading code.

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

Always wrap and return `ctx.Err()` so callers know whether the operation timed out or was cancelled.

## Verify What You Learned

Simulate a "database query" function that takes a context and a simulated duration. Call it twice -- once with a timeout shorter than the query (should time out) and once with a timeout longer (should succeed):

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func simulateQuery(ctx context.Context, queryDuration time.Duration) error {
	select {
	case <-time.After(queryDuration):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func main() {
	ctx1, cancel1 := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel1()
	err1 := simulateQuery(ctx1, 100*time.Millisecond)
	fmt.Printf("Fast query (100ms, timeout 500ms): %v\n", err1)

	ctx2, cancel2 := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel2()
	err2 := simulateQuery(ctx2, 800*time.Millisecond)
	fmt.Printf("Slow query (800ms, timeout 200ms): %v\n", err2)
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
Fast query (100ms, timeout 500ms): <nil>
Slow query (800ms, timeout 200ms): context deadline exceeded
```

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
