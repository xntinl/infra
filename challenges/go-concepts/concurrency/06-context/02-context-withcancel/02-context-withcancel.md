---
difficulty: basic
concepts: [context.WithCancel, cancel function, ctx.Done channel, cancellation propagation]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [context.Background, goroutines, channels basics]
---

# 2. Context WithCancel


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

Create a cancellable context, pass it to a goroutine that loops until cancelled, then cancel from main:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // always defer cancel to avoid resource leaks

	go func(ctx context.Context) {
		for i := 0; ; i++ {
			select {
			case <-ctx.Done():
				fmt.Printf("goroutine: stopped (reason: %v)\n", ctx.Err())
				return
			default:
				fmt.Printf("goroutine: working... iteration %d\n", i)
				time.Sleep(100 * time.Millisecond)
			}
		}
	}(ctx)

	// Let the goroutine work for a bit.
	time.Sleep(350 * time.Millisecond)

	fmt.Println("main: calling cancel()")
	cancel()

	// Give goroutine time to receive the signal and print.
	time.Sleep(50 * time.Millisecond)
}
```

### Verification
```bash
go run main.go
```
Expected output (approximately):
```
goroutine: working... iteration 0
goroutine: working... iteration 1
goroutine: working... iteration 2
main: calling cancel()
goroutine: stopped (reason: context canceled)
```

The goroutine runs 3 iterations (~300ms), then main calls `cancel()`, closing the `Done()` channel. The goroutine's `select` picks up the signal and exits. Note that `ctx.Err()` returns `context.Canceled` -- this is how you know cancellation happened (as opposed to a timeout).

## Step 2 -- Cancellation Propagates to Children

Create a parent context, derive two child contexts from it, and show that cancelling the parent stops both children:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func main() {
	parent, cancelParent := context.WithCancel(context.Background())
	child1, cancelChild1 := context.WithCancel(parent)
	child2, cancelChild2 := context.WithCancel(parent)
	defer cancelChild1()
	defer cancelChild2()

	worker := func(name string, ctx context.Context) {
		<-ctx.Done()
		fmt.Printf("%s: stopped (reason: %v)\n", name, ctx.Err())
	}

	go worker("child1", child1)
	go worker("child2", child2)

	fmt.Println("Cancelling parent context...")
	cancelParent()

	time.Sleep(50 * time.Millisecond)
}
```

### Verification
```bash
go run main.go
```
Expected output (order of children may vary):
```
Cancelling parent context...
child1: stopped (reason: context canceled)
child2: stopped (reason: context canceled)
```

Both children are cancelled when the parent is cancelled. This is the tree structure of contexts in action: cancellation flows downward through the entire subtree.

## Step 3 -- Cancel Only a Child

Show that cancelling a child does not affect the parent or siblings:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func main() {
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()

	child1, cancelChild1 := context.WithCancel(parent)
	child2, cancelChild2 := context.WithCancel(parent)
	defer cancelChild2()

	fmt.Println("Cancelling child1 only...")
	cancelChild1()

	time.Sleep(10 * time.Millisecond)

	fmt.Printf("parent.Err(): %v\n", parent.Err())
	fmt.Printf("child1.Err(): %v\n", child1.Err())
	fmt.Printf("child2.Err(): %v\n", child2.Err())
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
Cancelling child1 only...
parent.Err(): <nil>
child1.Err(): context canceled
child2.Err(): <nil>
```

Cancellation flows down, never up. The parent and sibling remain active. This is critical: a failing sub-operation should not tear down unrelated parts of the system.

## Step 4 -- Cancel Is Idempotent

Calling `cancel()` more than once is safe. The Go documentation explicitly states this. This matters because `defer cancel()` and an explicit `cancel()` call may both execute:

```go
package main

import (
	"context"
	"fmt"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())

	cancel()
	fmt.Printf("First cancel:  %v\n", ctx.Err())

	cancel() // no panic
	fmt.Printf("Second cancel: %v  (no panic, same error)\n", ctx.Err())

	cancel() // still safe
	fmt.Printf("Third cancel:  %v  (still safe)\n", ctx.Err())
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
First cancel:  context canceled
Second cancel: context canceled  (no panic, same error)
Third cancel:  context canceled  (still safe)
```

## Common Mistakes

### Forgetting to Call Cancel
**Wrong:**
```go
package main

import (
	"context"
	"fmt"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	_ = cancel // unused -- resource leak!
	fmt.Printf("ctx.Err(): %v\n", ctx.Err())
}
```
**What happens:** The derived context and its internal goroutine are never cleaned up, causing a resource leak. The Go runtime cannot garbage-collect the context's internal resources until cancel is called.

**Fix:** Always `defer cancel()` immediately after creating the context:
```go
package main

import (
	"context"
	"fmt"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fmt.Printf("ctx.Err(): %v\n", ctx.Err())
}
```

### Not Checking ctx.Done() in the Goroutine
**Wrong:**
```go
package main

import (
	"context"
	"fmt"
	"time"
)

func doWork() {
	time.Sleep(100 * time.Millisecond)
	fmt.Println("working...")
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func(ctx context.Context) {
		for {
			doWork() // never checks ctx.Done() -- goroutine runs forever
		}
	}(ctx)

	time.Sleep(300 * time.Millisecond)
	cancel()
	time.Sleep(200 * time.Millisecond) // goroutine is STILL running
}
```
**What happens:** The goroutine ignores the cancellation signal and continues consuming CPU and memory.

**Fix:**
```go
package main

import (
	"context"
	"fmt"
	"time"
)

func doWork() {
	time.Sleep(100 * time.Millisecond)
	fmt.Println("working...")
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				fmt.Println("stopped")
				return
			default:
				doWork()
			}
		}
	}(ctx)

	time.Sleep(300 * time.Millisecond)
	cancel()
	time.Sleep(200 * time.Millisecond)
}
```

### Passing cancel Function to Other Goroutines
Prefer keeping the cancel function close to where the context was created. Passing it to multiple goroutines makes it unclear who is responsible for cancellation, leading to premature or accidental cancellation. If a goroutine needs to stop the operation, signal through a separate channel and let the owner call cancel.

## Verify What You Learned

Create a context tree with branching: root -> branch1, branch2, and leaf under branch1. Cancel branch1 and verify that root and branch2 are unaffected while branch1 and leaf are cancelled:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func main() {
	//         root
	//        /    \
	//   branch1  branch2
	//      |
	//     leaf

	root, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()

	branch1, cancelBranch1 := context.WithCancel(root)
	branch2, cancelBranch2 := context.WithCancel(root)
	defer cancelBranch2()

	leaf, cancelLeaf := context.WithCancel(branch1)
	defer cancelLeaf()

	fmt.Println("Before any cancellation:")
	fmt.Printf("  root.Err():    %v\n", root.Err())
	fmt.Printf("  branch1.Err(): %v\n", branch1.Err())
	fmt.Printf("  branch2.Err(): %v\n", branch2.Err())
	fmt.Printf("  leaf.Err():    %v\n", leaf.Err())

	cancelBranch1()
	time.Sleep(10 * time.Millisecond)

	fmt.Println("After cancelling branch1:")
	fmt.Printf("  root.Err():    %v\n", root.Err())
	fmt.Printf("  branch1.Err(): %v\n", branch1.Err())
	fmt.Printf("  branch2.Err(): %v\n", branch2.Err())
	fmt.Printf("  leaf.Err():    %v\n", leaf.Err())
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
Before any cancellation:
  root.Err():    <nil>
  branch1.Err(): <nil>
  branch2.Err(): <nil>
  leaf.Err():    <nil>
After cancelling branch1:
  root.Err():    <nil>
  branch1.Err(): context canceled
  branch2.Err(): <nil>
  leaf.Err():    context canceled
```

## What's Next
Continue to [03-context-withtimeout](../03-context-withtimeout/03-context-withtimeout.md) to learn how to automatically cancel a context after a specified duration.

## Summary
- `context.WithCancel` returns a derived context and a `cancel` function
- Calling `cancel()` closes the `Done()` channel, signaling all listeners simultaneously
- Cancellation propagates from parent to all descendant contexts (the entire subtree)
- Cancellation never propagates upward -- parent and siblings are unaffected
- Always `defer cancel()` to prevent resource leaks
- Calling cancel multiple times is safe (idempotent)
- Goroutines must cooperatively check `ctx.Done()` to respond to cancellation

## Reference
- [Package context: WithCancel](https://pkg.go.dev/context#WithCancel)
- [Go Blog: Context](https://go.dev/blog/context)
- [Go Concurrency Patterns: Pipelines](https://go.dev/blog/pipelines)
