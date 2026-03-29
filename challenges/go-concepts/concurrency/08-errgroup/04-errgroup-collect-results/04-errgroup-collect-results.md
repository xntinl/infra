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
- **Collect** partial results when some tasks fail using errgroup.WithContext
- **Choose** the right collection pattern based on whether results map to known indices

## Why Result Collection Matters

Errgroup handles synchronization and error propagation, but it has no built-in mechanism for collecting results. `g.Go()` takes a `func() error` -- there is no return value for data. You need a pattern for goroutines to report their results back to the caller.

Three common patterns exist:

1. **Index-based** (preferred when possible): Pre-allocate a results slice of known size. Each goroutine writes to `results[i]`. Since no two goroutines write to the same index, no mutex is needed.

2. **Mutex-guarded**: When results do not map to predictable indices (e.g., filtering), protect a shared slice with a mutex.

3. **Map-based**: When results are keyed by strings or other non-sequential keys, use a mutex-protected map.

The index-based pattern is preferred because it avoids lock contention entirely and preserves ordering -- `results[i]` always corresponds to `tasks[i]`.

## Step 1 -- See the Race Condition

Run with the race detector:

```bash
go mod tidy
go run -race main.go
```

The `unsafeCollect` function appends to a shared slice without synchronization:

```go
package main

import (
    "fmt"
    "time"

    "golang.org/x/sync/errgroup"
)

func main() {
    var g errgroup.Group
    var results []string // shared, unprotected

    for i := 0; i < 10; i++ {
        i := i
        g.Go(func() error {
            time.Sleep(time.Duration(i*10) * time.Millisecond)
            results = append(results, fmt.Sprintf("result-%d", i)) // DATA RACE
            return nil
        })
    }

    _ = g.Wait()
    fmt.Printf("Got %d results (may be wrong due to race)\n", len(results))
}
```

**Expected output:**
```
WARNING: DATA RACE
...
Got N results (may be wrong due to race)
```

`append` modifies the slice header (length) and may reallocate the backing array. Multiple goroutines calling `append` concurrently corrupt the slice.

## Step 2 -- Index-Based Collection (No Mutex)

Pre-allocate the results slice to the exact size. Each goroutine writes to its own slot:

```go
package main

import (
    "fmt"
    "time"

    "golang.org/x/sync/errgroup"
)

func main() {
    tasks := []string{"alpha", "bravo", "charlie", "delta", "echo"}
    results := make([]string, len(tasks)) // pre-allocate exact size

    var g errgroup.Group
    for i, task := range tasks {
        i, task := i, task
        g.Go(func() error {
            time.Sleep(time.Duration(50+i*30) * time.Millisecond)
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

**Expected output:**
```
Results (ordered):
  [0] processed-alpha
  [1] processed-bravo
  [2] processed-charlie
  [3] processed-delta
  [4] processed-echo
```

Why this is safe: writing to `results[0]`, `results[1]`, etc. from different goroutines touches different memory locations. The slice header (length, capacity, pointer) is never modified -- only individual elements of the backing array are written.

Run with `-race` to confirm: no warnings.

## Step 3 -- Mutex-Guarded Collection (Filtered Results)

When the output count differs from the input count (filtering, dynamic discovery), use a mutex:

```go
package main

import (
    "fmt"
    "sync"
    "time"

    "golang.org/x/sync/errgroup"
)

func main() {
    var g errgroup.Group
    var mu sync.Mutex
    var results []string

    for i := 0; i < 10; i++ {
        i := i
        g.Go(func() error {
            time.Sleep(time.Duration(i*20) * time.Millisecond)
            if i%2 == 0 { // only collect even-numbered results
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

    fmt.Printf("Collected %d even results:\n", len(results))
    for _, r := range results {
        fmt.Printf("  %s\n", r)
    }
}
```

**Expected output:**
```
Collected 5 even results:
  result-0
  result-2
  result-4
  result-6
  result-8
```

Note: ordering depends on goroutine scheduling but is deterministic here because of the staggered timing. In production, results may appear in any order. If ordering matters, sort after collection or use the index-based pattern.

## Step 4 -- Partial Results on Error

When some tasks fail but you still want the results from successful ones, combine index-based collection with `WithContext`:

```go
package main

import (
    "context"
    "fmt"
    "time"

    "golang.org/x/sync/errgroup"
)

func main() {
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
                return fmt.Errorf("task %d (%q) failed", i, task)
            }
            results[i] = fmt.Sprintf("processed-%s", task)
            return nil
        })
    }

    err := g.Wait()
    if err != nil {
        fmt.Printf("Error: %v\n", err)
    }

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

**Expected output:**
```
Error: task 2 ("FAIL") failed
Partial results:
  [0] processed-alpha
  [1] processed-bravo
  [2] (empty -- task failed or was cancelled)
  [3] (empty -- task failed or was cancelled)
  [4] (empty -- task failed or was cancelled)
```

Empty strings indicate tasks that failed or were cancelled. Tasks that completed before the failure have their results.

## Step 5 -- Map-Based Collection

When results are keyed by non-sequential identifiers, use a mutex-protected map:

```go
package main

import (
    "fmt"
    "sync"
    "time"

    "golang.org/x/sync/errgroup"
)

type UserProfile struct {
    ID    int
    Name  string
    Score int
}

func main() {
    userIDs := []string{"user-1", "user-2", "user-3"}
    db := map[string]UserProfile{
        "user-1": {ID: 1, Name: "Alice", Score: 92},
        "user-2": {ID: 2, Name: "Bob", Score: 87},
        "user-3": {ID: 3, Name: "Charlie", Score: 95},
    }

    var g errgroup.Group
    var mu sync.Mutex
    results := make(map[string]UserProfile)

    for _, uid := range userIDs {
        uid := uid
        g.Go(func() error {
            time.Sleep(50 * time.Millisecond)
            profile, ok := db[uid]
            if !ok {
                return fmt.Errorf("user %s not found", uid)
            }
            mu.Lock()
            results[uid] = profile
            mu.Unlock()
            return nil
        })
    }

    if err := g.Wait(); err != nil {
        fmt.Printf("Error: %v\n", err)
        return
    }

    fmt.Println("Results:")
    for _, uid := range userIDs {
        fmt.Printf("  %s: %+v\n", uid, results[uid])
    }
}
```

**Expected output:**
```
Results:
  user-1: {ID:1 Name:Alice Score:92}
  user-2: {ID:2 Name:Bob Score:87}
  user-3: {ID:3 Name:Charlie Score:95}
```

## Common Mistakes

### Appending to a shared slice without a mutex

**Wrong:**
```go
g.Go(func() error {
    results = append(results, value) // DATA RACE
    return nil
})
```

**What happens:** `append` may reallocate the underlying array. Multiple goroutines calling append concurrently corrupt the slice.

**Fix:** Use a mutex or the index-based pattern.

### Using index-based pattern with a zero-length slice

**Wrong:**
```go
results := make([]string, 0) // length 0
g.Go(func() error {
    results[i] = value // index out of range: panics!
    return nil
})
```

**What happens:** The slice has length 0, so any index access panics.

**Fix:** Pre-allocate with `make([]string, len(tasks))`.

### Reading results before Wait returns

**Wrong:**
```go
g.Go(func() error {
    results[i] = "done"
    return nil
})
fmt.Println(results) // reading before Wait -- DATA RACE
g.Wait()
```

**What happens:** You read the results slice while goroutines may still be writing.

**Fix:** Always read results AFTER `g.Wait()` returns. The `Wait()` call establishes a happens-before relationship.

## Verify What You Learned

Run the full program and confirm:
1. The race detector catches the unsafe pattern: `go run -race main.go`
2. Index-based collection produces ordered results with no race
3. Mutex-guarded collection handles filtered results safely
4. Partial results show empty slots for failed/cancelled tasks

## What's Next
Continue to [05-errgroup-vs-waitgroup](../05-errgroup-vs-waitgroup/05-errgroup-vs-waitgroup.md) to understand when to choose errgroup over sync.WaitGroup.

## Summary
- `errgroup.Go()` takes `func() error` -- no built-in mechanism to return data
- **Index-based**: pre-allocate `results[len(tasks)]`, each goroutine writes to its own index -- no mutex needed, preserves order
- **Mutex-guarded**: protect `append` with a mutex when output size is unknown or results are filtered
- **Map-based**: protect a shared map with a mutex when results are keyed by non-sequential identifiers
- Writing to distinct slice indices from different goroutines is safe (no data race)
- Always read results AFTER `g.Wait()` returns -- never before
- For partial results on error, use `WithContext` and check for empty/zero-value slots after Wait

## Reference
- [errgroup package documentation](https://pkg.go.dev/golang.org/x/sync/errgroup)
- [Go Memory Model: happens-before](https://go.dev/ref/mem)
- [Go Race Detector](https://go.dev/doc/articles/race_detector)
