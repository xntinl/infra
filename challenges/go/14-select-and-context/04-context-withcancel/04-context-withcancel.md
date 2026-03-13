# 4. Context WithCancel

<!--
difficulty: intermediate
concepts: [context, context-withcancel, cancellation, cancel-func, context-done]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [select-statement-basics, done-channel-pattern, goroutines]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [03 - Timeout with Select](../03-timeout-with-select/03-timeout-with-select.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** `context.WithCancel` to cancel goroutines cooperatively
- **Implement** functions that accept and respect `context.Context`
- **Design** cancellation hierarchies where cancelling a parent cancels all children

## Why context.WithCancel

In Section 13 you learned the done channel pattern: create a `chan struct{}`, pass it to goroutines, and close it to signal cancellation. The `context` package standardizes this pattern into an interface that the entire Go ecosystem uses.

`context.WithCancel(parent)` returns a new context and a `cancel` function. When you call `cancel()`, the context's `Done()` channel is closed, signaling all goroutines watching it. Contexts form a tree: cancelling a parent automatically cancels all its children.

Every function that does I/O, runs long, or spawns goroutines should accept a `context.Context` as its first parameter. This is not just convention -- it is enforced by the standard library (`net/http`, `database/sql`, `os/exec`).

## Step 1 -- Basic Cancellation

```bash
mkdir -p ~/go-exercises/context-cancel && cd ~/go-exercises/context-cancel
go mod init context-cancel
```

Create `main.go`:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func worker(ctx context.Context, id int) {
	for {
		select {
		case <-ctx.Done():
			fmt.Printf("worker %d: stopped (%v)\n", id, ctx.Err())
			return
		default:
			fmt.Printf("worker %d: working\n", id)
			time.Sleep(200 * time.Millisecond)
		}
	}
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())

	for i := 1; i <= 3; i++ {
		go worker(ctx, i)
	}

	time.Sleep(600 * time.Millisecond)
	fmt.Println("main: cancelling all workers")
	cancel()
	time.Sleep(100 * time.Millisecond)
}
```

`context.Background()` is the root context -- it is never cancelled. `WithCancel` derives a child that can be cancelled explicitly.

### Intermediate Verification

```bash
go run main.go
```

Expected (worker order varies):

```
worker 1: working
worker 2: working
worker 3: working
worker 1: working
worker 2: working
worker 3: working
worker 1: working
worker 2: working
worker 3: working
main: cancelling all workers
worker 2: stopped (context canceled)
worker 1: stopped (context canceled)
worker 3: stopped (context canceled)
```

## Step 2 -- Cancel a Producer Pipeline

```go
package main

import (
	"context"
	"fmt"
)

func generate(ctx context.Context) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		n := 0
		for {
			select {
			case <-ctx.Done():
				fmt.Println("generator: cancelled")
				return
			case out <- n:
				n++
			}
		}
	}()
	return out
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	nums := generate(ctx)

	for i := 0; i < 5; i++ {
		fmt.Println(<-nums)
	}

	cancel()
	// Try to read one more -- channel may be closed
	v, ok := <-nums
	fmt.Printf("after cancel: %d, ok=%v\n", v, ok)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
0
1
2
3
4
generator: cancelled
after cancel: 0, ok=false
```

## Step 3 -- Parent-Child Cancellation

Cancelling a parent context automatically cancels all derived children:

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

func task(ctx context.Context, name string, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			fmt.Printf("%s: cancelled (%v)\n", name, ctx.Err())
			return
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func main() {
	parent, cancelParent := context.WithCancel(context.Background())

	childA, cancelA := context.WithCancel(parent)
	childB, _ := context.WithCancel(parent) // no need to call cancelB directly
	_ = cancelA                              // we will use this below

	var wg sync.WaitGroup
	wg.Add(3)
	go task(parent, "parent-task", &wg)
	go task(childA, "child-A", &wg)
	go task(childB, "child-B", &wg)

	time.Sleep(300 * time.Millisecond)

	// Cancel only child A
	fmt.Println("--- cancelling child A ---")
	cancelA()
	time.Sleep(150 * time.Millisecond)

	// Cancel parent -- also cancels child B and parent-task
	fmt.Println("--- cancelling parent ---")
	cancelParent()

	wg.Wait()
	fmt.Println("all tasks stopped")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
--- cancelling child A ---
child-A: cancelled (context canceled)
--- cancelling parent ---
parent-task: cancelled (context canceled)
child-B: cancelled (context canceled)
all tasks stopped
```

## Step 4 -- Checking ctx.Err()

`ctx.Err()` returns `nil` while the context is active, `context.Canceled` after `cancel()` is called, and `context.DeadlineExceeded` after a timeout (covered in the next exercise):

```go
package main

import (
	"context"
	"errors"
	"fmt"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())

	fmt.Println("before cancel:", ctx.Err())

	cancel()

	fmt.Println("after cancel:", ctx.Err())
	fmt.Println("is Canceled?", errors.Is(ctx.Err(), context.Canceled))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
before cancel: <nil>
after cancel: context canceled
is Canceled? true
```

## Step 5 -- Always Defer Cancel

The `cancel` function must always be called to release resources associated with the context. The idiomatic pattern is `defer cancel()`:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func doWork(ctx context.Context) error {
	select {
	case <-time.After(100 * time.Millisecond):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // always call cancel -- even if you cancel explicitly later

	if err := doWork(ctx); err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Println("work completed")
}
```

Even if the function returns normally, `defer cancel()` ensures the context tree is cleaned up.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
work completed
```

## Common Mistakes

### Forgetting to Call cancel()

Every `context.WithCancel` returns a `cancel` function that MUST be called. Failure to do so leaks the context and its goroutine until the parent is cancelled.

```go
// ALWAYS do this:
ctx, cancel := context.WithCancel(parent)
defer cancel()
```

### Passing context.Background() Everywhere

`context.Background()` should only appear at the top of a call chain (in `main`, `init`, or test functions). All other functions should receive their context as a parameter.

### Ignoring ctx.Done() in Long Operations

A function that accepts `context.Context` but never checks `ctx.Done()` defeats the purpose. Always check for cancellation in loops and before expensive operations.

## Verify What You Learned

Write a program that:
1. Creates a parent context with cancel
2. Launches a "fetcher" goroutine that generates values on a channel
3. Launches a "processor" goroutine that reads from the channel and transforms values
4. After receiving 10 values from the processor, cancel the parent context
5. Verify both goroutines stop cleanly

## What's Next

Continue to [05 - Context WithTimeout and WithDeadline](../05-context-withtimeout-withdeadline/05-context-withtimeout-withdeadline.md) to learn how to add automatic time-based cancellation.

## Summary

- `context.WithCancel(parent)` returns a new context and a `cancel` function
- Calling `cancel()` closes the context's `Done()` channel
- Cancelling a parent context cancels all children automatically
- `ctx.Err()` returns `context.Canceled` after cancellation
- Always `defer cancel()` to prevent resource leaks
- Accept `context.Context` as the first parameter of any function that does I/O or may be long-running

## Reference

- [context package documentation](https://pkg.go.dev/context)
- [Go Blog: Go Concurrency Patterns: Context](https://go.dev/blog/context)
- [Go Blog: Contexts and structs](https://go.dev/blog/context-and-structs)
