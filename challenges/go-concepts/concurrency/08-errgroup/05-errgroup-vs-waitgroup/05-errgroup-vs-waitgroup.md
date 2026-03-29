# 5. Errgroup vs WaitGroup

<!--
difficulty: intermediate
concepts: [errgroup.Group, sync.WaitGroup, error handling patterns, decision criteria]
tools: [go]
estimated_time: 25m
bloom_level: analyze
prerequisites: [errgroup basics, sync.WaitGroup, error handling, channels]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercise 01 (errgroup basics)
- Solid understanding of `sync.WaitGroup` (Add, Done, Wait)
- Familiarity with channel-based error collection

## Learning Objectives
After completing this exercise, you will be able to:
- **Solve** the same concurrent problem with both WaitGroup and errgroup
- **Compare** the code complexity, safety, and ergonomics of each approach
- **Formulate** clear decision criteria for choosing between them
- **Identify** scenarios where WaitGroup is more appropriate than errgroup

## Why This Comparison Matters

Developers who learn errgroup often ask: "Should I always use errgroup instead of WaitGroup?" The answer is no. Each tool has its sweet spot:

- **errgroup**: When goroutines can fail and you need to propagate errors. The common case for I/O, network calls, and any fallible operation.
- **WaitGroup**: When goroutines are fire-and-forget or when you need manual control over goroutine lifecycle.

By solving the same problem with both approaches, you develop an intuition for which tool fits naturally and where the other creates friction.

## Step 1 -- The Problem: Parallel File Processing

Process 5 files concurrently. Some succeed, some fail. Report a summary of successes and the first error.

```bash
go mod tidy
go run main.go
```

## Step 2 -- WaitGroup Solution (Manual Error Handling)

The WaitGroup approach requires four primitives wired together:

```go
package main

import (
    "fmt"
    "math/rand"
    "sync"
    "time"
)

func main() {
    files := []string{"config.yaml", "data.csv", "CORRUPT", "readme.md", "MISSING"}

    var wg sync.WaitGroup
    var mu sync.Mutex
    var firstErr error
    successCount := 0

    for _, file := range files {
        file := file
        wg.Add(1)
        go func() {
            defer wg.Done()
            if err := processFile(file); err != nil {
                mu.Lock()
                if firstErr == nil {
                    firstErr = err
                }
                mu.Unlock()
                return
            }
            mu.Lock()
            successCount++
            mu.Unlock()
        }()
    }

    wg.Wait()

    fmt.Printf("Processed: %d/%d succeeded\n", successCount, len(files))
    if firstErr != nil {
        fmt.Printf("First error: %v\n", firstErr)
    }
}

func processFile(name string) error {
    time.Sleep(time.Duration(50+rand.Intn(100)) * time.Millisecond)
    switch name {
    case "CORRUPT":
        return fmt.Errorf("file %q is corrupted", name)
    case "MISSING":
        return fmt.Errorf("file %q not found", name)
    default:
        fmt.Printf("  Processed: %s\n", name)
        return nil
    }
}
```

**Expected output:**
```
  Processed: config.yaml
  Processed: data.csv
  Processed: readme.md
Processed: 3/5 succeeded
First error: file "CORRUPT" is corrupted
```

Five interleaved concerns: WaitGroup (Add, Done, Wait), mutex (Lock, Unlock x2), error variable (check-then-set), success counter (increment). Easy to forget Add before the goroutine, forget Done with defer, or forget to lock the mutex.

## Step 3 -- Errgroup Solution (Built-in Error Handling)

The same problem with errgroup -- no WaitGroup, no mutex for errors:

```go
package main

import (
    "fmt"
    "math/rand"
    "time"

    "golang.org/x/sync/errgroup"
)

func main() {
    files := []string{"config.yaml", "data.csv", "CORRUPT", "readme.md", "MISSING"}
    results := make([]bool, len(files))

    var g errgroup.Group
    for i, file := range files {
        i, file := i, file
        g.Go(func() error {
            if err := processFile(file); err != nil {
                return err
            }
            results[i] = true
            return nil
        })
    }

    err := g.Wait()

    successCount := 0
    for _, ok := range results {
        if ok {
            successCount++
        }
    }

    fmt.Printf("Processed: %d/%d succeeded\n", successCount, len(files))
    if err != nil {
        fmt.Printf("First error: %v\n", err)
    }
}

func processFile(name string) error {
    time.Sleep(time.Duration(50+rand.Intn(100)) * time.Millisecond)
    switch name {
    case "CORRUPT":
        return fmt.Errorf("file %q is corrupted", name)
    case "MISSING":
        return fmt.Errorf("file %q not found", name)
    default:
        fmt.Printf("  Processed: %s\n", name)
        return nil
    }
}
```

**Expected output:**
```
  Processed: config.yaml
  Processed: data.csv
  Processed: readme.md
Processed: 3/5 succeeded
First error: file "CORRUPT" is corrupted
```

Same result, but: no Add/Done, no mutex for error collection (errgroup handles it), and success tracking uses the index-based pattern (no mutex needed for that either).

## Step 4 -- When WaitGroup Wins

Fire-and-forget work where errors are meaningless:

```go
package main

import (
    "fmt"
    "sync"
    "time"
)

func main() {
    var wg sync.WaitGroup

    for i := 0; i < 5; i++ {
        i := i
        wg.Add(1)
        go func() {
            defer wg.Done()
            time.Sleep(time.Duration(i*30) * time.Millisecond)
            fmt.Printf("  Worker %d: sent notification\n", i)
        }()
    }

    wg.Wait()
    fmt.Println("All 5 notifications sent")
}
```

**Expected output:**
```
  Worker 0: sent notification
  Worker 1: sent notification
  Worker 2: sent notification
  Worker 3: sent notification
  Worker 4: sent notification
All 5 notifications sent
```

Using errgroup here would force every closure to return `nil` -- noise with no benefit. WaitGroup is the natural fit for infallible side-effects: logging, metrics emission, best-effort notifications.

## Step 5 -- Both Tools in One Program

In real programs, fallible and infallible work coexist. Use each tool where it fits:

```go
package main

import (
    "fmt"
    "math/rand"
    "sync"
    "time"

    "golang.org/x/sync/errgroup"
)

func main() {
    // Fallible: errgroup for HTTP fetches
    var g errgroup.Group
    urls := []string{"https://api.example.com/users", "https://api.example.com/orders", "https://api.example.com/BROKEN"}
    for _, url := range urls {
        url := url
        g.Go(func() error {
            time.Sleep(time.Duration(50+rand.Intn(50)) * time.Millisecond)
            if url == "https://api.example.com/BROKEN" {
                return fmt.Errorf("fetch %s: 500", url)
            }
            fmt.Printf("  Fetched: %s\n", url)
            return nil
        })
    }

    // Infallible: WaitGroup for background logging
    var wg sync.WaitGroup
    for i := 0; i < 3; i++ {
        i := i
        wg.Add(1)
        go func() {
            defer wg.Done()
            time.Sleep(time.Duration(30+i*20) * time.Millisecond)
            fmt.Printf("  Logger %d: wrote audit log\n", i)
        }()
    }

    if err := g.Wait(); err != nil {
        fmt.Printf("Fetch error: %v\n", err)
    }
    wg.Wait()
    fmt.Println("Background logging complete")
}
```

**Expected output:**
```
  Fetched: https://api.example.com/users
  Fetched: https://api.example.com/orders
  Logger 0: wrote audit log
  Logger 1: wrote audit log
Fetch error: fetch https://api.example.com/BROKEN: 500
  Logger 2: wrote audit log
Background logging complete
```

## Decision Table

| Criterion | sync.WaitGroup | errgroup.Group |
|-----------|---------------|----------------|
| Error propagation | Manual (mutex + variable) | Built-in (Wait returns first error) |
| Cancellation | Manual (context + plumbing) | Built-in with WithContext |
| Concurrency limit | Manual (semaphore channel) | Built-in with SetLimit |
| Add/Done tracking | Manual (easy to misuse) | Automatic (Go handles it) |
| Fire-and-forget | Natural fit | Unnecessary overhead |
| Best for | Infallible goroutines | Fallible goroutines |

**Rule of thumb:** If the goroutine can return an error, use errgroup. If it cannot, use WaitGroup.

## Common Mistakes

### Using WaitGroup when tasks can fail

**Wrong:**
```go
var wg sync.WaitGroup
wg.Add(1)
go func() {
    defer wg.Done()
    if err := riskyOperation(); err != nil {
        log.Println(err) // logged and forgotten
    }
}()
wg.Wait()
// caller has no idea if riskyOperation failed
```

**What happens:** Errors are silently swallowed. The caller proceeds as if everything succeeded.

**Fix:** Use errgroup:
```go
var g errgroup.Group
g.Go(func() error {
    return riskyOperation()
})
if err := g.Wait(); err != nil {
    // caller can handle the error properly
}
```

### Forgetting wg.Add before launching the goroutine

**Wrong:**
```go
go func() {
    wg.Add(1) // INSIDE the goroutine -- race with wg.Wait()
    defer wg.Done()
}()
wg.Wait() // might return before Add is called
```

**What happens:** `Wait()` can return before the goroutine even calls `Add(1)`. Errgroup avoids this entirely because `g.Go()` handles both launching and tracking atomically.

### Wrapping infallible work in errgroup

**Not wrong, but noisy:**
```go
g.Go(func() error {
    log.Println("sent metric")
    return nil // always nil -- errgroup adds nothing here
})
```

**Better:** Use WaitGroup for fire-and-forget work. Reserve errgroup for fallible operations.

## Verify What You Learned

Run the full program and confirm:
1. WaitGroup and errgroup solutions produce equivalent results for the same problem
2. WaitGroup requires more boilerplate for error handling
3. WaitGroup is cleaner for fire-and-forget work
4. Both tools coexist naturally in the same program

```bash
go run main.go
```

## What's Next
Continue to [06-errgroup-parallel-pipeline](../06-errgroup-parallel-pipeline/06-errgroup-parallel-pipeline.md) for a comprehensive exercise building a multi-stage parallel pipeline with errgroup.

## Summary
- Use **errgroup** when goroutines can fail and errors matter
- Use **WaitGroup** for fire-and-forget goroutines with no meaningful errors
- Errgroup eliminates the WaitGroup + mutex + error channel boilerplate
- Both can coexist in the same program -- use each where it fits
- The rule of thumb: if the goroutine returns `error`, use errgroup

## Reference
- [sync.WaitGroup documentation](https://pkg.go.dev/sync#WaitGroup)
- [errgroup package documentation](https://pkg.go.dev/golang.org/x/sync/errgroup)
- [Go Proverbs: Don't just check errors, handle them gracefully](https://go-proverbs.github.io/)
