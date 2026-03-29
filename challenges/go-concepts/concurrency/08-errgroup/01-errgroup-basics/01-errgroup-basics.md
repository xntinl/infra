# 1. Errgroup Basics

<!--
difficulty: basic
concepts: [errgroup.Group, Go, Wait, error propagation, golang.org/x/sync]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [goroutines, sync.WaitGroup, error handling]
-->

## Prerequisites
- Go 1.22+ installed
- Understanding of `sync.WaitGroup` (Add, Done, Wait)
- Familiarity with Go error handling (`error` interface, `fmt.Errorf`)
- Basic goroutine usage

## Learning Objectives
After completing this exercise, you will be able to:
- **Create** an `errgroup.Group` and launch concurrent tasks with `g.Go()`
- **Collect** the first non-nil error from a set of concurrent goroutines via `Wait()`
- **Explain** how errgroup simplifies the WaitGroup + error channel pattern
- **Compare** manual error collection with errgroup's built-in propagation

## Why Errgroup
When you use `sync.WaitGroup` to coordinate goroutines, there is no built-in way to collect errors. You end up writing boilerplate: a WaitGroup for synchronization, a channel or mutex-protected variable for errors, and logic to pick one error from potentially many. This pattern repeats so often that the Go team created `golang.org/x/sync/errgroup` to encapsulate it.

An `errgroup.Group` combines WaitGroup-style synchronization with first-error propagation in a single type. You launch goroutines with `g.Go(func() error { ... })` instead of the `go` keyword. When you call `g.Wait()`, it blocks until all goroutines finish and returns the first non-nil error encountered (or nil if all succeeded). No channels, no mutexes, no manual Add/Done bookkeeping.

The key insight: `g.Go()` accepts a `func() error`, not a `func()`. This forces you to handle errors at the point of creation rather than silently ignoring them inside goroutines -- a common source of bugs with raw `go` statements.

## Step 1 -- Set Up the Module

Open `main.go` and `go.mod`. The module already declares the dependency on `golang.org/x/sync`. Run:

```bash
go mod tidy
```

This downloads the dependency and generates `go.sum`. Verify it compiled:

```bash
go run main.go
```

You should see the output from `manualWaitGroupErrors()` demonstrating the manual pattern with WaitGroup + error collection.

### Intermediate Verification
The program compiles and runs. The manual approach prints task results and reports any error that occurred.

## Step 2 -- Launch Tasks with errgroup

Implement the `errgroupBasic` function. Create a new `errgroup.Group`, launch several tasks with `g.Go()`, and call `g.Wait()` to collect the result:

```go
func errgroupBasic() {
    fmt.Println("\n=== Errgroup Basic ===")
    var g errgroup.Group

    urls := []string{
        "https://example.com",
        "https://example.org",
        "INVALID",
        "https://example.net",
    }

    for _, url := range urls {
        url := url // capture loop variable
        g.Go(func() error {
            return fetchURL(url)
        })
    }

    if err := g.Wait(); err != nil {
        fmt.Printf("First error: %v\n", err)
    } else {
        fmt.Println("All tasks succeeded")
    }
}
```

Notice that `g.Go()` takes a `func() error`. There is no `Add()` or `Done()` -- errgroup handles that internally. The `Wait()` call blocks until every goroutine launched by `Go()` completes, then returns the first error.

### Intermediate Verification
```bash
go run main.go
```
The errgroup version should report the error from the "INVALID" URL. All other goroutines still run to completion -- errgroup does not cancel siblings (that requires `WithContext`, covered in exercise 02).

## Step 3 -- Observe First-Error Semantics

Implement `errgroupMultipleErrors` to confirm that `Wait()` returns only the first error even when multiple tasks fail:

```go
func errgroupMultipleErrors() {
    fmt.Println("\n=== Errgroup Multiple Errors ===")
    var g errgroup.Group

    for i := 0; i < 5; i++ {
        i := i
        g.Go(func() error {
            time.Sleep(time.Duration(i) * 50 * time.Millisecond)
            if i%2 == 0 {
                return fmt.Errorf("task %d failed", i)
            }
            fmt.Printf("  Task %d succeeded\n", i)
            return nil
        })
    }

    if err := g.Wait(); err != nil {
        fmt.Printf("Wait returned: %v\n", err)
        fmt.Println("(only the first error is returned)")
    }
}
```

Tasks 0, 2, and 4 all fail, but `Wait()` returns only one of them. Which one depends on goroutine scheduling -- typically the fastest to fail.

### Intermediate Verification
```bash
go run main.go
```
You see a single error from `Wait()`, plus the success messages from tasks 1 and 3. The other failures are silently discarded by errgroup. If you need all errors, use a different strategy (e.g., `multierr` or collecting into a slice with a mutex).

## Common Mistakes

### Forgetting to capture the loop variable
**Wrong:**
```go
for _, url := range urls {
    g.Go(func() error {
        return fetchURL(url) // captures the loop variable by reference
    })
}
```
**What happens:** All goroutines see the last value of `url` because closures capture by reference. In Go 1.22+ with `GOEXPERIMENT=loopvar` (default), this is fixed, but for clarity and backward compatibility, always shadow the variable.

**Fix:**
```go
for _, url := range urls {
    url := url // shadow the loop variable
    g.Go(func() error {
        return fetchURL(url)
    })
}
```

### Passing a function that ignores errors
**Wrong:**
```go
g.Go(func() error {
    doWork() // doWork returns an error but it's ignored
    return nil
})
```
**What happens:** Errors are silently swallowed. The whole point of errgroup is error propagation.

**Fix:**
```go
g.Go(func() error {
    return doWork()
})
```

### Using the `go` keyword alongside errgroup
**Wrong:**
```go
var g errgroup.Group
go func() {
    g.Go(func() error { ... }) // launched in a separate goroutine
}()
g.Wait() // might return before the go statement even executes g.Go
```
**What happens:** The `go` keyword launches a goroutine that errgroup does not track. `Wait()` might return before that goroutine calls `g.Go()`.

**Fix:** Only use `g.Go()` to launch work. Do not mix `go` and errgroup.

## Verify What You Learned

Modify the program to simulate 10 concurrent "file processing" tasks where tasks 3 and 7 fail with different errors. Confirm that:
1. `Wait()` returns exactly one error
2. All non-failing tasks still run to completion
3. The program does not deadlock or panic

## What's Next
Continue to [02-errgroup-with-context](../02-errgroup-with-context/02-errgroup-with-context.md) to learn how `errgroup.WithContext` automatically cancels sibling goroutines when one fails.

## Summary
- `errgroup.Group` combines WaitGroup synchronization with first-error propagation
- Launch goroutines with `g.Go(func() error { ... })` -- no Add/Done needed
- `g.Wait()` blocks until all goroutines complete and returns the first non-nil error
- Only the first error is returned; subsequent errors are discarded
- Always capture loop variables when passing closures to `g.Go()`
- Never mix the `go` keyword with errgroup -- use `g.Go()` exclusively

## Reference
- [errgroup package documentation](https://pkg.go.dev/golang.org/x/sync/errgroup)
- [golang.org/x/sync repository](https://github.com/golang/sync)
- [Go Blog: Pipelines and cancellation](https://go.dev/blog/pipelines)
