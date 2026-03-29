# 1. Context Background and TODO

<!--
difficulty: basic
concepts: [context.Background, context.TODO, root contexts, context tree]
tools: [go]
estimated_time: 15m
bloom_level: understand
prerequisites: [Go basics, interfaces]
-->

## Prerequisites
- Go 1.22+ installed
- Familiarity with Go interfaces
- Basic understanding of function signatures and parameter passing

## Learning Objectives
After completing this exercise, you will be able to:
- **Create** root contexts using `context.Background()` and `context.TODO()`
- **Explain** the role of context as a tree structure in Go programs
- **Distinguish** when to use `Background()` versus `TODO()`
- **Inspect** the properties of a root context

## Why Context

The `context` package is the standard mechanism in Go for carrying deadlines, cancellation signals, and request-scoped values across API boundaries and between goroutines. Every context forms a tree: each derived context has a parent, and cancellation flows downward from parent to children.

At the root of every context tree sits one of two functions: `context.Background()` or `context.TODO()`. They return identical, empty contexts that are never cancelled, have no deadline, and carry no values. The difference is purely semantic -- a signal to the reader about intent:

- **`context.Background()`** is the default root. Use it in `main`, initialization code, tests, and as the top-level context for incoming requests.
- **`context.TODO()`** is a placeholder. Use it when you know a context is needed but are unsure which one to propagate, or when the surrounding code has not yet been updated to pass context.

Understanding these root contexts is the foundation. Every `WithCancel`, `WithTimeout`, `WithDeadline`, and `WithValue` call you will see in later exercises derives from one of these roots.

## Step 1 -- Create and Inspect Background Context

The `exploreBackground` function creates a background context and prints every observable property. Run it to see that a root context is completely empty:

```go
package main

import (
	"context"
	"fmt"
)

func main() {
	ctx := context.Background()

	fmt.Printf("Type:     %T\n", ctx)
	fmt.Printf("String:   %s\n", ctx)
	fmt.Printf("Err:      %v\n", ctx.Err())
	fmt.Printf("Done:     %v\n", ctx.Done())

	deadline, ok := ctx.Deadline()
	if ok {
		fmt.Printf("Deadline: %v\n", deadline)
	} else {
		fmt.Println("Deadline: none (no deadline set)")
	}
	fmt.Printf("Value(\"key\"): %v\n", ctx.Value("key"))
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
Type:     *context.emptyCtx
String:   context.Background
Err:      <nil>
Done:     <nil>
Deadline: none (no deadline set)
Value("key"): <nil>
```

The background context has no deadline, no error, a nil `Done()` channel (meaning it can never be cancelled), and no values. The nil `Done()` channel is significant: a receive on a nil channel blocks forever, which is correct because a root context should never be cancelled.

## Step 2 -- Create and Inspect TODO Context

`context.TODO()` returns a context structurally identical to `Background()`. The only difference is the string representation, which serves as documentation of intent:

```go
package main

import (
	"context"
	"fmt"
)

func main() {
	bg := context.Background()
	todo := context.TODO()

	fmt.Printf("Background string: %s\n", bg)
	fmt.Printf("TODO string:       %s\n", todo)

	// Prove they are behaviorally identical.
	fmt.Printf("Both nil Err:   %v\n", bg.Err() == todo.Err())
	fmt.Printf("Both nil Done:  %v\n", bg.Done() == todo.Done())

	_, bgOk := bg.Deadline()
	_, todoOk := todo.Deadline()
	fmt.Printf("Both no deadline: %v\n", bgOk == todoOk)
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
Background string: context.Background
TODO string:       context.TODO
Both nil Err:   true
Both nil Done:  true
Both no deadline: true
```

Notice that `Background()` and `TODO()` are structurally identical. The only difference is the string representation. Static analysis tools like `go vet` can flag `TODO()` contexts that remain in production code.

## Step 3 -- Passing Context to a Function

The universal Go convention: `context.Context` is always the **first** parameter, named `ctx`. This is not optional style -- the entire standard library and ecosystem follows it. Linters like `revive` and `contextcheck` enforce it.

```go
package main

import (
	"context"
	"fmt"
)

func greet(ctx context.Context, name string) {
	if ctx.Err() != nil {
		fmt.Printf("greet: context already cancelled, skipping\n")
		return
	}
	fmt.Printf("Hello, %s! (via %s)\n", name, ctx)
}

func main() {
	greet(context.Background(), "Alice")
	greet(context.TODO(), "Bob")

	// Show behavior with a cancelled context.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	greet(cancelled, "Charlie")
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
Hello, Alice! (via context.Background)
Hello, Bob! (via context.TODO)
greet: context already cancelled, skipping
```

Checking `ctx.Err()` before doing work is a defensive pattern that avoids wasting resources when the context is already cancelled (e.g., the HTTP client disconnected before the handler started processing).

## Step 4 -- Context Tree Visualization

Every context in Go forms a tree. Root contexts sit at the top. Derived contexts are children. Cancellation flows DOWN: cancelling a parent cancels all descendants. It never flows UP -- parents and siblings are unaffected.

```go
package main

import (
	"context"
	"fmt"
)

func main() {
	root := context.Background()
	child, cancelChild := context.WithCancel(root)
	grandchild, cancelGrandchild := context.WithCancel(child)
	defer cancelGrandchild()

	fmt.Printf("Before cancel:\n")
	fmt.Printf("  root.Err():       %v\n", root.Err())
	fmt.Printf("  child.Err():      %v\n", child.Err())
	fmt.Printf("  grandchild.Err(): %v\n", grandchild.Err())

	cancelChild()

	fmt.Printf("After cancelling child:\n")
	fmt.Printf("  root.Err():       %v  (unaffected)\n", root.Err())
	fmt.Printf("  child.Err():      %v\n", child.Err())
	fmt.Printf("  grandchild.Err(): %v  (cascaded from child)\n", grandchild.Err())
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
Before cancel:
  root.Err():       <nil>
  child.Err():      <nil>
  grandchild.Err(): <nil>
After cancelling child:
  root.Err():       <nil>  (unaffected)
  child.Err():      context canceled
  grandchild.Err(): context canceled  (cascaded from child)
```

## Common Mistakes

### Using context.TODO() Permanently
**Wrong:** Leaving `context.TODO()` in production code indefinitely.

**Why it matters:** `TODO()` signals "I need to figure out the right context later." If it stays, it means cancellation and deadlines are not propagated through that code path, which can lead to resource leaks.

**Fix:** Replace `TODO()` with a properly derived context once the surrounding design is clear.

### Creating Context Inside a Helper Instead of Receiving It
**Wrong:**
```go
package main

import (
	"context"
	"fmt"
)

func fetchData() {
	ctx := context.Background() // creates a new root -- isolated from caller
	_ = ctx
	fmt.Println("fetching data...")
}

func main() {
	fetchData()
}
```
**Fix:**
```go
package main

import (
	"context"
	"fmt"
)

func fetchData(ctx context.Context) {
	_ = ctx // uses the caller's context -- cancellation propagates
	fmt.Println("fetching data...")
}

func main() {
	fetchData(context.Background())
}
```

When a function creates its own `context.Background()`, it breaks the cancellation chain. The caller has no way to cancel or set a deadline on that operation. Always accept context as a parameter.

### Storing Context in a Struct
**Wrong:**
```go
package main

import "context"

type Server struct {
	ctx context.Context // do not do this
}

func main() {
	_ = Server{ctx: context.Background()}
}
```
**Why it matters:** Contexts are request-scoped. Storing them in a struct ties a short-lived value to a long-lived object, leading to stale contexts and subtle bugs. The context would outlive the request it was meant for.

**Fix:** Pass context as the first parameter of each method call:
```go
package main

import (
	"context"
	"fmt"
)

type Server struct{}

func (s *Server) HandleRequest(ctx context.Context) {
	fmt.Printf("handling request with context: %s\n", ctx)
}

func main() {
	s := &Server{}
	s.HandleRequest(context.Background())
}
```

## Verify What You Learned

Write a function `describeContext(ctx context.Context)` that accepts any context and prints whether it has a deadline, whether its `Done` channel is nil, and its string representation. Call it with both `Background` and `TODO` contexts and confirm they behave identically aside from their string output.

```go
package main

import (
	"context"
	"fmt"
)

func describeContext(ctx context.Context) {
	_, hasDeadline := ctx.Deadline()
	doneIsNil := ctx.Done() == nil

	fmt.Printf("  Has deadline: %v\n", hasDeadline)
	fmt.Printf("  Done is nil:  %v\n", doneIsNil)
	fmt.Printf("  String:       %s\n", ctx)
}

func main() {
	fmt.Println("describe(context.Background):")
	describeContext(context.Background())

	fmt.Println("describe(context.TODO):")
	describeContext(context.TODO())
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
describe(context.Background):
  Has deadline: false
  Done is nil:  true
  String:       context.Background
describe(context.TODO):
  Has deadline: false
  Done is nil:  true
  String:       context.TODO
```

## What's Next
Continue to [02-context-withcancel](../02-context-withcancel/02-context-withcancel.md) to learn how to create cancellable contexts and signal goroutines to stop.

## Summary
- `context.Background()` is the standard root context for `main`, init, and top-level request handling
- `context.TODO()` is a placeholder root for code that does not yet have a proper context to propagate
- Both return empty, never-cancelled contexts with no deadline and no values
- Context in Go forms a tree: root contexts are the starting point for all derived contexts
- Convention: `context.Context` is always the first parameter, named `ctx`
- Never store contexts in structs; pass them through function parameters
- A nil `Done()` channel blocks forever, which is correct for root contexts

## Reference
- [Go Blog: Go Concurrency Patterns: Context](https://go.dev/blog/context)
- [Package context](https://pkg.go.dev/context)
- [Go Proverb: Pass context.Context as the first argument](https://go-proverbs.github.io/)
