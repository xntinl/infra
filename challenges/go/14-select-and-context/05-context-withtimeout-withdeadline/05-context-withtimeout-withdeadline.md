# 5. Context WithTimeout and WithDeadline

<!--
difficulty: intermediate
concepts: [context-withtimeout, context-withdeadline, deadline, automatic-cancellation, ctx-err]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [context-withcancel, select-statement-basics, timeout-with-select]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [04 - Context WithCancel](../04-context-withcancel/04-context-withcancel.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** `context.WithTimeout` to set a relative time limit on operations
- **Apply** `context.WithDeadline` to set an absolute time limit
- **Distinguish** between `WithTimeout` and `WithDeadline` and choose the right one
- **Implement** context-aware functions that respect time limits

## Why WithTimeout and WithDeadline

`context.WithCancel` requires manual cancellation. In practice, most operations have a time budget: an HTTP handler has a request timeout, a database query has a deadline, a retry loop should give up after N seconds.

`context.WithTimeout(parent, duration)` creates a context that automatically cancels after `duration`. `context.WithDeadline(parent, time)` cancels at an absolute `time.Time`. They are functionally equivalent -- `WithTimeout(ctx, 5*time.Second)` is shorthand for `WithDeadline(ctx, time.Now().Add(5*time.Second))`.

When the deadline passes, `ctx.Done()` closes and `ctx.Err()` returns `context.DeadlineExceeded`. This integrates directly with `select`, making timeout handling clean and composable.

## Step 1 -- Basic WithTimeout

```bash
mkdir -p ~/go-exercises/context-timeout && cd ~/go-exercises/context-timeout
go mod init context-timeout
```

Create `main.go`:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func slowOperation(ctx context.Context) (string, error) {
	select {
	case <-time.After(2 * time.Second):
		return "result", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	result, err := slowOperation(ctx)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("result:", result)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
error: context deadline exceeded
```

The operation takes 2 seconds but the context cancels after 500ms.

## Step 2 -- WithDeadline with Absolute Time

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

	dl, ok := ctx.Deadline()
	fmt.Printf("has deadline: %v, deadline: %v\n", ok, dl.Format(time.RFC3339Nano))

	select {
	case <-time.After(1 * time.Second):
		fmt.Println("operation completed")
	case <-ctx.Done():
		fmt.Println("deadline exceeded:", ctx.Err())
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
has deadline: true, deadline: 2026-...
deadline exceeded: context deadline exceeded
```

## Step 3 -- Nested Timeouts (Shortest Wins)

When you nest timeouts, the shortest one wins:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func main() {
	// Outer: 1 second timeout
	outer, cancelOuter := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancelOuter()

	// Inner: 300ms timeout -- this one fires first
	inner, cancelInner := context.WithTimeout(outer, 300*time.Millisecond)
	defer cancelInner()

	select {
	case <-time.After(2 * time.Second):
		fmt.Println("completed")
	case <-inner.Done():
		fmt.Println("inner:", inner.Err())
	}

	// Outer is still active (for now)
	fmt.Println("outer err:", outer.Err())

	// Wait for outer to expire
	<-outer.Done()
	fmt.Println("outer expired:", outer.Err())
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
inner: context deadline exceeded
outer err: <nil>
outer expired: context deadline exceeded
```

## Step 4 -- Context-Aware Work Function

Write functions that check their context before each unit of work:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func processItems(ctx context.Context, items []string) (int, error) {
	processed := 0
	for _, item := range items {
		// Check context before starting each item
		select {
		case <-ctx.Done():
			return processed, fmt.Errorf("cancelled after %d items: %w", processed, ctx.Err())
		default:
		}

		// Simulate processing
		fmt.Printf("processing: %s\n", item)
		time.Sleep(150 * time.Millisecond)
		processed++
	}
	return processed, nil
}

func main() {
	items := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot"}

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	count, err := processItems(ctx, items)
	if err != nil {
		fmt.Printf("stopped: %v\n", err)
	}
	fmt.Printf("total processed: %d/%d\n", count, len(items))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (approximately):

```
processing: alpha
processing: bravo
processing: charlie
stopped: cancelled after 3 items: context deadline exceeded
total processed: 3/6
```

## Step 5 -- Checking Remaining Time

Use `ctx.Deadline()` to check how much time remains:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func adaptiveWork(ctx context.Context) {
	for i := 1; ; i++ {
		dl, ok := ctx.Deadline()
		if ok {
			remaining := time.Until(dl)
			if remaining < 50*time.Millisecond {
				fmt.Printf("step %d: only %v remaining, stopping early\n", i, remaining.Truncate(time.Millisecond))
				return
			}
			fmt.Printf("step %d: %v remaining\n", i, remaining.Truncate(time.Millisecond))
		}

		select {
		case <-ctx.Done():
			fmt.Printf("step %d: context done (%v)\n", i, ctx.Err())
			return
		case <-time.After(100 * time.Millisecond):
			// work done
		}
	}
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 450*time.Millisecond)
	defer cancel()

	adaptiveWork(ctx)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (approximately):

```
step 1: 449ms remaining
step 2: 349ms remaining
step 3: 249ms remaining
step 4: 149ms remaining
step 5: only 49ms remaining, stopping early
```

## Common Mistakes

### Setting a Longer Timeout Than the Parent

```go
parent, _ := context.WithTimeout(ctx, 1*time.Second)
child, _ := context.WithTimeout(parent, 5*time.Second) // useless: parent cancels first
```

The child context cannot outlive its parent. The effective timeout is always `min(parent, child)`.

### Forgetting defer cancel()

Even for `WithTimeout` and `WithDeadline`, always call `defer cancel()`. The timeout will cancel eventually, but calling `cancel()` releases resources immediately when you are done.

### Not Checking ctx.Err() After ctx.Done()

When `<-ctx.Done()` fires, check `ctx.Err()` to distinguish between `context.Canceled` (manual cancel) and `context.DeadlineExceeded` (timeout).

## Verify What You Learned

Write a function `retryWithTimeout(ctx context.Context, maxRetries int, fn func() error) error` that:
1. Calls `fn` up to `maxRetries` times
2. Returns `nil` on the first success
3. Returns the context error if the context expires during retries
4. Returns the last error from `fn` if all retries fail within the deadline
5. Test with a 1-second timeout and a function that randomly succeeds or fails

## What's Next

Continue to [06 - Context WithValue](../06-context-withvalue/06-context-withvalue.md) to learn how to attach request-scoped data to contexts.

## Summary

- `context.WithTimeout(parent, d)` cancels automatically after duration `d`
- `context.WithDeadline(parent, t)` cancels at absolute time `t`
- `WithTimeout` is shorthand for `WithDeadline(ctx, time.Now().Add(d))`
- Nested timeouts: the shortest always wins
- `ctx.Err()` returns `context.DeadlineExceeded` when the time limit is reached
- `ctx.Deadline()` lets you inspect how much time remains
- Always `defer cancel()` even for timeout contexts

## Reference

- [context.WithTimeout](https://pkg.go.dev/context#WithTimeout)
- [context.WithDeadline](https://pkg.go.dev/context#WithDeadline)
- [Go Blog: Go Concurrency Patterns: Context](https://go.dev/blog/context)
