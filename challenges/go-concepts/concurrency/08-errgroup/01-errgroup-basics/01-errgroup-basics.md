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

When you use `sync.WaitGroup` to coordinate goroutines, there is no built-in way to collect errors. You end up writing boilerplate: a WaitGroup for synchronization, a mutex-protected variable for the error, and logic to pick one error from potentially many. This pattern repeats so often that the Go team created `golang.org/x/sync/errgroup` to encapsulate it.

An `errgroup.Group` combines WaitGroup-style synchronization with first-error propagation in a single type. You launch goroutines with `g.Go(func() error { ... })` instead of the `go` keyword. When you call `g.Wait()`, it blocks until all goroutines finish and returns the first non-nil error encountered (or nil if all succeeded). No channels, no mutexes, no manual Add/Done bookkeeping.

The key insight: `g.Go()` accepts a `func() error`, not a `func()`. This forces you to handle errors at the point of creation rather than silently ignoring them inside goroutines -- a common source of bugs with raw `go` statements.

## Step 1 -- The Manual Pattern (What Errgroup Replaces)

Run the program:

```bash
go mod tidy
go run main.go
```

The `manualWaitGroupErrors` function shows the pattern that errgroup replaces. It requires four separate primitives:

```go
package main

import (
    "fmt"
    "sync"
    "time"
)

func main() {
    urls := []string{"https://example.com", "https://example.org", "INVALID", "https://example.net"}

    var wg sync.WaitGroup
    var mu sync.Mutex
    var firstErr error

    for _, url := range urls {
        url := url
        wg.Add(1)
        go func() {
            defer wg.Done()
            if err := fetchURL(url); err != nil {
                mu.Lock()
                if firstErr == nil {
                    firstErr = err
                }
                mu.Unlock()
            }
        }()
    }

    wg.Wait()
    if firstErr != nil {
        fmt.Printf("First error: %v\n", firstErr)
    }
}

func fetchURL(url string) error {
    time.Sleep(100 * time.Millisecond)
    if url == "INVALID" {
        return fmt.Errorf("failed to fetch %q: invalid URL", url)
    }
    fmt.Printf("  Fetched: %s\n", url)
    return nil
}
```

**Expected output:**
```
  Fetched: https://example.com
  Fetched: https://example.org
  Fetched: https://example.net
First error: failed to fetch "INVALID": invalid URL
```

That is a lot of ceremony for "run these things concurrently and tell me if one failed." WaitGroup, mutex, error variable, Add, Done -- five moving parts that must be wired correctly.

## Step 2 -- The Same Thing with Errgroup

Now compare with errgroup:

```go
package main

import (
    "fmt"
    "time"

    "golang.org/x/sync/errgroup"
)

func main() {
    urls := []string{"https://example.com", "https://example.org", "INVALID", "https://example.net"}

    var g errgroup.Group

    for _, url := range urls {
        url := url
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

func fetchURL(url string) error {
    time.Sleep(100 * time.Millisecond)
    if url == "INVALID" {
        return fmt.Errorf("failed to fetch %q: invalid URL", url)
    }
    fmt.Printf("  Fetched: %s\n", url)
    return nil
}
```

**Expected output:**
```
  Fetched: https://example.com
  Fetched: https://example.org
  Fetched: https://example.net
First error: failed to fetch "INVALID": invalid URL
```

Three things to notice:
1. **No Add/Done** -- `g.Go()` handles both internally
2. **No mutex or error variable** -- `Wait()` returns the first error
3. **The closure must return `error`** -- this forces you to propagate errors rather than silently discarding them

All goroutines still run to completion. The "INVALID" task fails, but the other three fetch successfully. Errgroup does not cancel siblings -- for that, you need `WithContext` (exercise 02).

## Step 3 -- First-Error Semantics

When multiple tasks fail, `Wait()` returns only the first error. The rest are silently discarded:

```go
package main

import (
    "fmt"
    "time"

    "golang.org/x/sync/errgroup"
)

func main() {
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
        fmt.Printf("Wait returned: %v (only first error is kept)\n", err)
    }
}
```

**Expected output:**
```
  Task 1 succeeded
  Task 3 succeeded
Wait returned: task 0 failed (only first error is kept)
```

Tasks 0, 2, and 4 all fail, but Wait returns only task 0's error (it fails first due to the staggered timing). If you need all errors, use a mutex-protected slice or a library like `go.uber.org/multierr`.

## Step 4 -- Zero Value and New Group Per Batch

The zero value of `errgroup.Group` is ready to use -- no constructor needed. For sequential batches of work, create a new group for each batch:

```go
package main

import (
    "fmt"

    "golang.org/x/sync/errgroup"
)

func main() {
    // Batch 1: one task fails
    var g1 errgroup.Group
    for i := 0; i < 3; i++ {
        i := i
        g1.Go(func() error {
            if i == 1 {
                return fmt.Errorf("batch-1 task %d failed", i)
            }
            return nil
        })
    }
    fmt.Printf("Batch 1 error: %v\n", g1.Wait())

    // Batch 2: fresh group, all succeed
    var g2 errgroup.Group
    for i := 0; i < 3; i++ {
        g2.Go(func() error { return nil })
    }
    fmt.Printf("Batch 2 error: %v\n", g2.Wait())
}
```

**Expected output:**
```
Batch 1 error: batch-1 task 1 failed
Batch 2 error: <nil>
```

Always create a new group for each independent batch. Do not reuse a group after `Wait()` returns.

## Common Mistakes

### Forgetting to capture the loop variable

**Wrong:**
```go
package main

import (
    "fmt"

    "golang.org/x/sync/errgroup"
)

func main() {
    var g errgroup.Group
    urls := []string{"a.com", "b.com", "c.com"}

    for _, url := range urls {
        g.Go(func() error {
            fmt.Println(url) // captures the loop variable by reference
            return nil
        })
    }
    g.Wait()
}
```

**What happens:** All goroutines might print "c.com" because closures capture the variable by reference. In Go 1.22+ with `GOEXPERIMENT=loopvar` (default in 1.22+), this is fixed for `for range` loops, but for clarity and backward compatibility, always shadow:

```go
for _, url := range urls {
    url := url // shadow the loop variable
    g.Go(func() error {
        fmt.Println(url) // safe: captures the shadowed copy
        return nil
    })
}
```

### Swallowing the error inside g.Go

**Wrong:**
```go
g.Go(func() error {
    if err := doWork(); err != nil {
        log.Println(err) // logged but not returned
    }
    return nil // always nil -- error is lost
})
```

**What happens:** The caller of `Wait()` never sees the error. The whole point of errgroup is error propagation.

**Fix:** Return the error:
```go
g.Go(func() error {
    return doWork()
})
```

### Mixing `go` keyword with errgroup

**Wrong:**
```go
var g errgroup.Group
go func() {
    g.Go(func() error { return doWork() })
}()
g.Wait() // might return before the outer goroutine calls g.Go
```

**What happens:** The `go` keyword launches a goroutine that errgroup does not track. `Wait()` might return before that goroutine registers its task.

**Fix:** Use only `g.Go()` to launch work. Never combine `go` and errgroup.

## Verify What You Learned

Run the full program and confirm:
1. The manual WaitGroup pattern and errgroup produce the same result
2. Multiple errors from different tasks result in only one error from `Wait()`
3. A zero-value group with no tasks returns nil from `Wait()`

```bash
go run main.go
```

## What's Next
Continue to [02-errgroup-with-context](../02-errgroup-with-context/02-errgroup-with-context.md) to learn how `errgroup.WithContext` automatically cancels sibling goroutines when one fails.

## Summary
- `errgroup.Group` combines WaitGroup synchronization with first-error propagation
- Launch goroutines with `g.Go(func() error { ... })` -- no Add/Done needed
- `g.Wait()` blocks until all goroutines complete and returns the first non-nil error
- Only the first error is returned; subsequent errors are discarded
- The zero value is ready to use -- no constructor
- Create a new group for each batch of work
- Always capture loop variables when passing closures to `g.Go()`
- Never mix the `go` keyword with errgroup -- use `g.Go()` exclusively

## Reference
- [errgroup package documentation](https://pkg.go.dev/golang.org/x/sync/errgroup)
- [golang.org/x/sync repository](https://github.com/golang/sync)
- [Go Blog: Pipelines and cancellation](https://go.dev/blog/pipelines)
