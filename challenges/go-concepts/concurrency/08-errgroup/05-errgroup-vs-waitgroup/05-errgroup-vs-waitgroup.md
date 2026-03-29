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
- **WaitGroup**: When goroutines are fire-and-forget or when you need manual control over goroutine lifecycle that errgroup does not provide (e.g., spawning goroutines conditionally in a loop, or goroutines that outlive the group).

By solving the same problem with both approaches, you develop an intuition for which tool fits naturally and where the other creates friction.

## Step 1 -- The Problem: Parallel File Processing

The task is to process 5 files concurrently. Each file can succeed or fail. You need to:
1. Process all files in parallel
2. Know if any processing failed
3. Print a summary of successes and failures

Run the starter code:

```bash
go mod tidy
go run main.go
```

The `processFile` helper simulates file processing -- some files succeed, some fail.

### Intermediate Verification
The program compiles and runs. The WaitGroup version shows the boilerplate required for error handling.

## Step 2 -- WaitGroup Solution (Manual Error Handling)

Study and complete the `solveWithWaitGroup` function. This requires manual orchestration:

```go
func solveWithWaitGroup() {
    fmt.Println("=== WaitGroup Solution ===")
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
```

Notice the ceremony: WaitGroup + mutex + error variable + success counter. Five concerns interleaved in one function.

### Intermediate Verification
```bash
go run main.go
```
The function reports the number of successes and the first error. It works, but the code is dense.

## Step 3 -- Errgroup Solution (Built-in Error Handling)

Implement the same logic using `solveWithErrgroup`:

```go
func solveWithErrgroup() {
    fmt.Println("\n=== Errgroup Solution ===")
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
```

No WaitGroup, no mutex for error collection, no manual Add/Done. Error propagation is built in. The only manual part is counting successes, which uses the index-based pattern (no mutex needed).

### Intermediate Verification
```bash
go run main.go
```
Both versions produce equivalent results. The errgroup version is shorter and has fewer opportunities for bugs.

## Step 4 -- When WaitGroup Wins

Implement `waitgroupForFireAndForget` to show a case where WaitGroup is the better choice:

```go
func waitgroupForFireAndForget() {
    fmt.Println("\n=== WaitGroup: Fire-and-Forget (best fit) ===")
    var wg sync.WaitGroup

    // Launch background workers that never fail
    for i := 0; i < 5; i++ {
        i := i
        wg.Add(1)
        go func() {
            defer wg.Done()
            // Pure side-effect work: logging, metrics, notifications
            time.Sleep(time.Duration(i*50) * time.Millisecond)
            fmt.Printf("  Worker %d: sent notification\n", i)
        }()
    }

    wg.Wait()
    fmt.Println("All notifications sent")
}
```

When goroutines perform infallible operations (logging, metrics emission, notifications to best-effort systems), there is no error to propagate. Using errgroup here adds unnecessary `func() error` wrappers with `return nil` boilerplate.

### Intermediate Verification
```bash
go run main.go
```
Clean, simple code. No errors to handle, no error return values cluttering the logic.

## Common Mistakes

### Using WaitGroup when tasks can fail
**Wrong:**
```go
var wg sync.WaitGroup
wg.Add(1)
go func() {
    defer wg.Done()
    if err := riskyOperation(); err != nil {
        log.Println(err) // error is logged and lost
    }
}()
wg.Wait()
// caller has no idea if riskyOperation failed
```
**What happens:** Errors are silently swallowed or only logged. The caller proceeds as if everything succeeded.

**Fix:** Use errgroup when tasks can fail:
```go
var g errgroup.Group
g.Go(func() error {
    return riskyOperation()
})
if err := g.Wait(); err != nil {
    // caller can handle the error
}
```

### Wrapping infallible work in errgroup
**Not wrong, but noisy:**
```go
g.Go(func() error {
    log.Println("sent metric")
    return nil // always nil -- errgroup adds nothing here
})
```
**Better:** Use WaitGroup for fire-and-forget work. Reserve errgroup for fallible operations.

### Forgetting wg.Add before launching the goroutine
**Wrong:**
```go
go func() {
    wg.Add(1) // INSIDE the goroutine -- race with wg.Wait()
    defer wg.Done()
}()
wg.Wait() // might return before Add is called
```
**What happens:** `Wait()` can return before the goroutine even calls `Add(1)`. Errgroup avoids this entirely because `g.Go()` handles both launching and tracking.

## Verify What You Learned

Create a scenario with 10 tasks where:
- 5 tasks are fallible (HTTP fetches) -- use errgroup
- 5 tasks are infallible (metric emissions) -- use WaitGroup
- Both groups run concurrently, and you wait for both to complete

This demonstrates using both tools in the same program, each for its appropriate use case.

## What's Next
Continue to [06-errgroup-parallel-pipeline](../06-errgroup-parallel-pipeline/06-errgroup-parallel-pipeline.md) for a comprehensive exercise building a multi-stage parallel pipeline with errgroup.

## Summary

| Criterion | sync.WaitGroup | errgroup.Group |
|-----------|---------------|----------------|
| Error propagation | Manual (mutex + variable) | Built-in (Wait returns first error) |
| Cancellation | Manual (context + plumbing) | Built-in with WithContext |
| Concurrency limit | Manual (semaphore channel) | Built-in with SetLimit |
| Add/Done tracking | Manual (easy to misuse) | Automatic (Go handles it) |
| Fire-and-forget | Natural fit | Unnecessary overhead |
| Best for | Infallible goroutines | Fallible goroutines |

- Use **errgroup** when goroutines can fail and errors matter
- Use **WaitGroup** for fire-and-forget goroutines with no meaningful errors
- Errgroup eliminates the WaitGroup + mutex + error channel boilerplate
- Both can coexist in the same program -- use each where it fits

## Reference
- [sync.WaitGroup documentation](https://pkg.go.dev/sync#WaitGroup)
- [errgroup package documentation](https://pkg.go.dev/golang.org/x/sync/errgroup)
- [Go Proverbs: Don't just check errors, handle them gracefully](https://go-proverbs.github.io/)
