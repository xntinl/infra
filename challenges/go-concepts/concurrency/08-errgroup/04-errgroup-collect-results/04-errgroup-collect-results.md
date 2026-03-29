# 4. Errgroup Collect Results

<!--
difficulty: intermediate
concepts: [errgroup result collection, index-based results, mutex-protected slice, pre-allocated arrays]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [errgroup basics, sync.Mutex, slices, goroutine safety]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercise 01 (errgroup basics)
- Understanding of `sync.Mutex` for protecting shared state
- Knowledge of why concurrent writes to a slice are unsafe

## Learning Objectives
After completing this exercise, you will be able to:
- **Identify** why naive result collection from goroutines causes data races
- **Apply** the index-based pattern: pre-allocate a slice and let each goroutine write to its own index
- **Apply** the mutex-guarded pattern: protect a shared slice with `sync.Mutex`
- **Choose** the right collection pattern based on whether task results map to known indices

## Why Result Collection Matters
Errgroup handles synchronization and error propagation, but it has no built-in mechanism for collecting results. `g.Go()` takes a `func() error` -- there is no return value for data. You need a pattern for goroutines to report their results back to the caller.

Two common patterns exist:

1. **Index-based** (preferred when possible): Pre-allocate a results slice of known size. Each goroutine writes to `results[i]` where `i` is its unique index. Since no two goroutines write to the same index, no mutex is needed. This is safe because writing to distinct slice indices is not a data race.

2. **Mutex-guarded**: When results do not map to predictable indices (e.g., you are filtering or transforming data and the output size is unknown), protect a shared slice with a mutex.

The index-based pattern is preferred because it avoids lock contention entirely. It also preserves ordering -- `results[i]` corresponds to `tasks[i]`.

## Step 1 -- See the Race Condition

Run the starter code:

```bash
go mod tidy
go run -race main.go
```

The `unsafeCollect` function appends results to a shared slice without synchronization. The race detector will flag this.

### Intermediate Verification
You see `WARNING: DATA RACE` output. The final results slice may have incorrect length or corrupted entries because `append` is not goroutine-safe.

## Step 2 -- Index-Based Collection (No Mutex Needed)

Implement `collectByIndex`. Pre-allocate the results slice to the exact size and let each goroutine write to its own slot:

```go
func collectByIndex() {
    fmt.Println("\n=== Collect by Index (no mutex) ===")
    tasks := []string{"alpha", "bravo", "charlie", "delta", "echo"}
    results := make([]string, len(tasks)) // pre-allocate exact size

    var g errgroup.Group
    for i, task := range tasks {
        i, task := i, task // capture
        g.Go(func() error {
            time.Sleep(time.Duration(50+i*30) * time.Millisecond) // stagger
            results[i] = fmt.Sprintf("processed-%s", task) // safe: unique index
            return nil
        })
    }

    if err := g.Wait(); err != nil {
        fmt.Printf("Error: %v\n", err)
        return
    }

    fmt.Println("Results (ordered):")
    for i, r := range results {
        fmt.Printf("  [%d] %s\n", i, r)
    }
}
```

Key insight: writing to `results[0]`, `results[1]`, etc. from different goroutines is safe because each goroutine writes to a distinct memory location. The slice header (length, capacity, pointer) is never modified -- only the backing array elements are written.

### Intermediate Verification
```bash
go run -race main.go
```
No race warnings for `collectByIndex`. Results are ordered -- `results[0]` always corresponds to `tasks[0]`, regardless of which goroutine finishes first.

## Step 3 -- Mutex-Guarded Collection

Implement `collectWithMutex` for cases where results do not map to a fixed index (e.g., filtering):

```go
func collectWithMutex() {
    fmt.Println("\n=== Collect with Mutex ===")
    var g errgroup.Group
    var mu sync.Mutex
    var results []string

    for i := 0; i < 10; i++ {
        i := i
        g.Go(func() error {
            time.Sleep(time.Duration(i*20) * time.Millisecond)

            // Only collect even-numbered results (filtering)
            if i%2 == 0 {
                result := fmt.Sprintf("result-%d", i)
                mu.Lock()
                results = append(results, result)
                mu.Unlock()
            }
            return nil
        })
    }

    if err := g.Wait(); err != nil {
        fmt.Printf("Error: %v\n", err)
        return
    }

    fmt.Printf("Collected %d results (order may vary):\n", len(results))
    for _, r := range results {
        fmt.Printf("  %s\n", r)
    }
}
```

The mutex protects the `append` call. Note that results may appear in any order since goroutine scheduling is non-deterministic. If ordering matters, sort the results after collection or use the index-based pattern.

### Intermediate Verification
```bash
go run -race main.go
```
No race warnings. The results slice contains 5 entries (even numbers 0-8). The order may differ between runs.

## Step 4 -- Handle Errors with Partial Results

Implement `collectWithErrors` to handle the case where some tasks fail but you still want the results from successful ones:

```go
func collectWithErrors() {
    fmt.Println("\n=== Collect with Partial Results on Error ===")
    tasks := []string{"alpha", "bravo", "FAIL", "delta", "echo"}
    results := make([]string, len(tasks))

    g, ctx := errgroup.WithContext(context.Background())

    for i, task := range tasks {
        i, task := i, task
        g.Go(func() error {
            select {
            case <-ctx.Done():
                return ctx.Err()
            case <-time.After(time.Duration(50+i*30) * time.Millisecond):
            }

            if task == "FAIL" {
                return fmt.Errorf("task %d (%s) failed", i, task)
            }
            results[i] = fmt.Sprintf("processed-%s", task)
            return nil
        })
    }

    err := g.Wait()
    fmt.Printf("Error: %v\n", err)
    fmt.Println("Partial results:")
    for i, r := range results {
        if r != "" {
            fmt.Printf("  [%d] %s\n", i, r)
        } else {
            fmt.Printf("  [%d] (empty -- task failed or was cancelled)\n", i)
        }
    }
}
```

### Intermediate Verification
```bash
go run main.go
```
You see partial results: tasks that completed before the failure have their results, while the failed task and cancelled tasks show as empty.

## Common Mistakes

### Appending to a shared slice without a mutex
**Wrong:**
```go
g.Go(func() error {
    results = append(results, value) // DATA RACE
    return nil
})
```
**What happens:** `append` may reallocate the underlying array. Multiple goroutines calling `append` concurrently corrupt the slice.

**Fix:** Use a mutex or the index-based pattern.

### Using index-based pattern with dynamically-sized results
**Wrong:**
```go
results := make([]string, 0)
g.Go(func() error {
    results[i] = value // index out of range!
    return nil
})
```
**What happens:** The slice has length 0, so indexing panics.

**Fix:** Pre-allocate with `make([]string, len(tasks))` when using the index pattern.

### Reading results before Wait returns
**Wrong:**
```go
g.Go(func() error {
    results[i] = "done"
    return nil
})
fmt.Println(results) // reading before Wait -- data race!
g.Wait()
```
**What happens:** You read the results slice while goroutines may still be writing. This is a data race.

**Fix:** Always read results after `g.Wait()` returns.

## Verify What You Learned

Build a parallel URL validator: given 8 URLs, fetch each in parallel (simulated), collect the HTTP status code for each, and print a summary table showing URL, status, and whether it was successful. Use the index-based pattern to preserve ordering.

## What's Next
Continue to [05-errgroup-vs-waitgroup](../05-errgroup-vs-waitgroup/05-errgroup-vs-waitgroup.md) to understand when to choose errgroup over sync.WaitGroup and vice versa.

## Summary
- `errgroup.Go()` takes `func() error` -- there is no built-in mechanism to return data
- Index-based collection: pre-allocate `results[len(tasks)]`, each goroutine writes to its own index -- no mutex needed
- Mutex-guarded collection: protect `append` with a mutex when output size is unknown or results are filtered
- Writing to distinct slice indices from different goroutines is safe (no data race)
- Always read results AFTER `g.Wait()` returns -- never before
- For partial results on error, use `WithContext` and check for empty slots after Wait

## Reference
- [errgroup package documentation](https://pkg.go.dev/golang.org/x/sync/errgroup)
- [Go Memory Model: happens-before](https://go.dev/ref/mem)
- [Go Race Detector](https://go.dev/doc/articles/race_detector)
