# 3. WaitGroup: Wait for All

<!--
difficulty: basic
concepts: [sync.WaitGroup, Add, Done, Wait, goroutine synchronization]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [goroutines, go keyword]
-->

## Prerequisites
- Go 1.22+ installed
- Ability to launch goroutines with the `go` keyword
- Understanding that `main` exits when it returns, killing all goroutines

## Learning Objectives
After completing this exercise, you will be able to:
- **Use** `sync.WaitGroup` to wait for multiple goroutines to complete
- **Apply** the correct pattern: `Add` before `go`, `Done` inside the goroutine
- **Identify** common mistakes such as calling `Add` inside the goroutine or producing a negative counter

## Why WaitGroup
In the goroutines exercises, you used `time.Sleep` to wait for goroutines to finish. This is fragile: sleep too little and goroutines are killed mid-execution; sleep too much and you waste time. You need a way to say "wait until all N goroutines have finished" without guessing how long they take.

`sync.WaitGroup` is a counter-based synchronization primitive. You increment the counter with `Add(n)` before launching goroutines, each goroutine decrements it with `Done()` when it finishes, and the main goroutine blocks on `Wait()` until the counter reaches zero. It is the simplest and most common way to synchronize goroutine completion in Go.

The critical rule is: **call `Add` before the `go` statement, not inside the goroutine**. If you call `Add` inside the goroutine, the main goroutine might reach `Wait()` before the goroutine has called `Add`, causing it to return immediately with work still running.

## Step 1 -- Replace time.Sleep with WaitGroup

Open `main.go`. The `withSleep` function uses `time.Sleep` -- observe how it is unreliable. Then implement `withWaitGroup`:

```go
func withWaitGroup() {
    fmt.Println("\n=== With WaitGroup ===")
    var wg sync.WaitGroup
    start := time.Now()

    for i := 0; i < 5; i++ {
        wg.Add(1) // Add BEFORE the go statement
        go func(id int) {
            defer wg.Done() // Done when goroutine completes
            duration := time.Duration(100*(id+1)) * time.Millisecond
            time.Sleep(duration)
            fmt.Printf("Worker %d finished (took %v)\n", id, duration)
        }(i)
    }

    wg.Wait() // Blocks until counter reaches 0
    fmt.Printf("All workers done in %v\n", time.Since(start).Round(time.Millisecond))
}
```

### Intermediate Verification
```bash
go run main.go
```
All 5 workers should finish, and the total time should be approximately 500ms (the slowest worker determines total time since they run concurrently).

## Step 2 -- Add Before Go, Not Inside

Implement `demonstrateCorrectAdd` to show why `Add` must come before `go`:

```go
func demonstrateCorrectAdd() {
    fmt.Println("\n=== Correct: Add Before Go ===")
    var wg sync.WaitGroup

    tasks := []string{"fetch-users", "fetch-orders", "fetch-products"}

    for _, task := range tasks {
        wg.Add(1) // CORRECT: Add is called in the launching goroutine
        go func(name string) {
            defer wg.Done()
            time.Sleep(50 * time.Millisecond)
            fmt.Printf("Task %q completed\n", name)
        }(task)
    }

    wg.Wait()
    fmt.Println("All tasks completed.")
}
```

### Intermediate Verification
```bash
go run main.go
```
All three tasks should print their completion message before "All tasks completed."

## Step 3 -- Batch Add for Known Count

When you know the number of goroutines upfront, you can call `Add` once with the total count:

```go
func batchAdd() {
    fmt.Println("\n=== Batch Add ===")
    const numWorkers = 10
    var wg sync.WaitGroup
    results := make([]int, numWorkers)

    wg.Add(numWorkers) // Add all at once
    for i := 0; i < numWorkers; i++ {
        go func(id int) {
            defer wg.Done()
            results[id] = id * id // each goroutine writes to its own index
        }(i)
    }

    wg.Wait()
    fmt.Printf("Results: %v\n", results)
}
```

Note: each goroutine writes to a unique index in the slice, so no mutex is needed. This is a common and safe pattern.

### Intermediate Verification
```bash
go run main.go
```
Results should contain the squares: `[0 1 4 9 16 25 36 49 64 81]`.

## Step 4 -- Dynamic Work with WaitGroup

Implement `dynamicWork` where the number of goroutines is determined at runtime:

```go
func dynamicWork() {
    fmt.Println("\n=== Dynamic Work ===")
    var wg sync.WaitGroup

    urls := []string{
        "https://api.example.com/users",
        "https://api.example.com/orders",
        "https://api.example.com/products",
        "https://api.example.com/inventory",
    }

    for _, url := range urls {
        wg.Add(1)
        go func(u string) {
            defer wg.Done()
            // Simulate HTTP request
            time.Sleep(time.Duration(50+len(u)) * time.Millisecond)
            fmt.Printf("Fetched: %s\n", u)
        }(url)
    }

    wg.Wait()
    fmt.Println("All URLs fetched.")
}
```

### Intermediate Verification
```bash
go run main.go
```
All four URLs should be printed before the "All URLs fetched." message.

## Common Mistakes

### Add Inside the Goroutine
**Wrong:**
```go
var wg sync.WaitGroup
for i := 0; i < 5; i++ {
    go func(id int) {
        wg.Add(1) // RACE: main might reach Wait() before this executes
        defer wg.Done()
        fmt.Println(id)
    }(i)
}
wg.Wait() // might return immediately with goroutines still running
```
**What happens:** `Wait()` can return before all goroutines have called `Add`, so some goroutines may not be waited for.

**Fix:** Always call `Add` before the `go` statement.

### Negative WaitGroup Counter
**Wrong:**
```go
var wg sync.WaitGroup
wg.Add(1)
go func() {
    wg.Done()
    wg.Done() // panic: negative WaitGroup counter
}()
wg.Wait()
```
**What happens:** Runtime panic. Each goroutine must call `Done` exactly once.

**Fix:** Use `defer wg.Done()` as the first line inside the goroutine to guarantee it is called exactly once.

### Forgetting Done
**Wrong:**
```go
var wg sync.WaitGroup
wg.Add(1)
go func() {
    if someCondition {
        return // Done is never called
    }
    wg.Done()
}()
wg.Wait() // blocks forever
```
**What happens:** Deadlock. The counter never reaches zero.

**Fix:** Use `defer wg.Done()` so it runs regardless of how the goroutine exits.

### Passing WaitGroup by Value
**Wrong:**
```go
func worker(wg sync.WaitGroup) { // receives a COPY
    defer wg.Done() // decrements the copy, not the original
    // ...
}
```
**What happens:** The original WaitGroup counter is never decremented. Deadlock.

**Fix:** Pass `*sync.WaitGroup` (pointer):
```go
func worker(wg *sync.WaitGroup) {
    defer wg.Done()
}
```

## Verify What You Learned

Create a `parallelSum` function that:
1. Splits a slice of 1,000,000 integers into 10 chunks
2. Launches a goroutine for each chunk to compute its partial sum
3. Uses WaitGroup to wait for all goroutines
4. Combines the partial sums into a total

Verify the result matches a sequential sum.

## What's Next
Continue to [04-once-singleton-init](../04-once-singleton-init/04-once-singleton-init.md) to learn how `sync.Once` ensures code runs exactly once, even under concurrent access.

## Summary
- `sync.WaitGroup` is a counter: `Add` increments, `Done` decrements, `Wait` blocks until zero
- Always call `Add` before the `go` statement, never inside the goroutine
- Use `defer wg.Done()` to guarantee the counter is decremented on all exit paths
- Pass WaitGroup by pointer (`*sync.WaitGroup`), never by value
- For a known count, call `Add(n)` once before the loop
- WaitGroup replaces fragile `time.Sleep` synchronization with deterministic completion waiting

## Reference
- [sync.WaitGroup documentation](https://pkg.go.dev/sync#WaitGroup)
- [Go by Example: WaitGroups](https://gobyexample.com/waitgroups)
- [Effective Go: Concurrency](https://go.dev/doc/effective_go#concurrency)
