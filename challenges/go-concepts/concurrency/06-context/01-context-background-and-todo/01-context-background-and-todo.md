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

Edit `main.go` and implement the `exploreBackground` function. Create a background context and print its properties:

```go
func exploreBackground() {
    fmt.Println("=== context.Background() ===")

    ctx := context.Background()

    fmt.Printf("Type:     %T\n", ctx)
    fmt.Printf("String:   %s\n", ctx)
    fmt.Printf("Err:      %v\n", ctx.Err())
    fmt.Printf("Done:     %v\n", ctx.Done())
    fmt.Printf("Deadline: ")

    deadline, ok := ctx.Deadline()
    if ok {
        fmt.Printf("%v\n", deadline)
    } else {
        fmt.Println("none (no deadline set)")
    }
    fmt.Printf("Value(\"key\"): %v\n\n", ctx.Value("key"))
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== context.Background() ===
Type:     *context.emptyCtx
String:   context.Background
Err:      <nil>
Done:     <nil>
Deadline: none (no deadline set)
Value("key"): <nil>
```

The background context has no deadline, no error, a nil `Done()` channel (meaning it can never be cancelled), and no values.

## Step 2 -- Create and Inspect TODO Context

Implement `exploreTODO`. Create a TODO context and print the same properties:

```go
func exploreTODO() {
    fmt.Println("=== context.TODO() ===")

    ctx := context.TODO()

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
    fmt.Printf("Value(\"key\"): %v\n\n", ctx.Value("key"))
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== context.TODO() ===
Type:     *context.emptyCtx
String:   context.TODO
Err:      <nil>
Done:     <nil>
Deadline: none (no deadline set)
Value("key"): <nil>
```

Notice that `Background()` and `TODO()` are structurally identical. The only difference is the string representation, which serves as documentation of intent.

## Step 3 -- Passing Context to a Function

Implement `greet` and call it from `demonstratePassingContext`. This establishes the convention that `context.Context` is always the first parameter:

```go
func greet(ctx context.Context, name string) {
    if ctx.Err() != nil {
        fmt.Println("context already cancelled, skipping greet")
        return
    }
    fmt.Printf("Hello, %s! (context: %s)\n", name, ctx)
}

func demonstratePassingContext() {
    fmt.Println("=== Passing Context ===")

    bgCtx := context.Background()
    greet(bgCtx, "Background User")

    todoCtx := context.TODO()
    greet(todoCtx, "TODO User")

    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Passing Context ===
Hello, Background User! (context: context.Background)
Hello, TODO User! (context: context.TODO)
```

## Common Mistakes

### Using context.TODO() Permanently
**Wrong:** Leaving `context.TODO()` in production code indefinitely.

**Why it matters:** `TODO()` signals "I need to figure out the right context later." If it stays, it means cancellation and deadlines are not propagated through that code path, which can lead to resource leaks.

**Fix:** Replace `TODO()` with a properly derived context once the surrounding design is clear.

### Creating Context Inside a Helper Instead of Receiving It
**Wrong:**
```go
func fetchData() {
    ctx := context.Background() // creates a new root -- isolated from caller
    // ...
}
```
**Fix:**
```go
func fetchData(ctx context.Context) {
    // uses the caller's context -- cancellation propagates
    // ...
}
```

### Storing Context in a Struct
**Wrong:**
```go
type Server struct {
    ctx context.Context // do not do this
}
```
**Why it matters:** Contexts are request-scoped. Storing them in a struct ties a short-lived value to a long-lived object, leading to stale contexts and subtle bugs.

**Fix:** Pass context as the first parameter of each method call.

## Verify What You Learned

Implement `verifyKnowledge`: create both a `Background` and a `TODO` context, then write a function `describeContext` that accepts a `context.Context` and prints whether it has a deadline, whether its `Done` channel is nil, and its string representation. Call it with both contexts and confirm they behave identically aside from their string output.

## What's Next
Continue to [02-context-withcancel](../02-context-withcancel/02-context-withcancel.md) to learn how to create cancellable contexts and signal goroutines to stop.

## Summary
- `context.Background()` is the standard root context for `main`, init, and top-level request handling
- `context.TODO()` is a placeholder root for code that does not yet have a proper context to propagate
- Both return empty, never-cancelled contexts with no deadline and no values
- Context in Go forms a tree: root contexts are the starting point for all derived contexts
- Convention: `context.Context` is always the first parameter, named `ctx`
- Never store contexts in structs; pass them through function parameters

## Reference
- [Go Blog: Go Concurrency Patterns: Context](https://go.dev/blog/context)
- [Package context](https://pkg.go.dev/context)
- [Go Proverb: Pass context.Context as the first argument](https://go-proverbs.github.io/)
