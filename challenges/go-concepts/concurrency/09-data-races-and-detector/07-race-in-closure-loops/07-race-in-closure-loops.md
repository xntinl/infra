# 7. Race in Closure Loops

<!--
difficulty: intermediate
concepts: [closure capture, loop variable, goroutine scheduling, Go 1.22 loop semantics]
tools: [go]
estimated_time: 25m
bloom_level: analyze
prerequisites: [goroutines, closures, data race concept, race detector]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01-06 (data races and fixes)
- Understanding of Go closures

## Learning Objectives
After completing this exercise, you will be able to:
- **Identify** the classic closure-in-loop race bug
- **Explain** why closures capture variables by reference, not by value
- **Fix** the bug by passing the loop variable as a function parameter
- **Understand** how Go 1.22 changed loop variable semantics and why the concept still matters

## Why Closure Races Matter
One of the most common concurrency bugs in Go is launching goroutines inside a loop where the goroutine closure captures the loop variable. Because closures capture variables by reference, all goroutines share the same loop variable. By the time the goroutines execute, the loop has often finished, and they all see the final value.

This bug is subtle because it is not always about data races in the strict sense (unsynchronized concurrent access). It is about a misunderstanding of how closures work: the goroutine does not get a snapshot of the variable at launch time; it gets a reference to the variable that continues to change as the loop iterates.

Starting with Go 1.22, the `for` loop creates a new variable for each iteration, which fixes the most common manifestation of this bug. However, understanding the underlying mechanism is essential because the same pattern can appear with non-loop variables, and because much existing code was written before Go 1.22.

## Step 1 -- The Classic Bug

Edit `main.go` and implement `closureBug`. This demonstrates the pre-Go-1.22 behavior using a variable declared outside the loop:

```go
func closureBug() {
    fmt.Println("=== Closure Bug ===")
    var wg sync.WaitGroup
    values := []string{"alpha", "beta", "gamma", "delta", "epsilon"}

    // BUG: variable captured by reference, not by value
    for _, v := range values {
        wg.Add(1)
        val := v // capture in a NEW variable each iteration
        // Remove the line above and use v directly to see the bug:
        go func() {
            defer wg.Done()
            // If we used v instead of val, all goroutines would likely print "epsilon"
            fmt.Printf("  goroutine sees: %s\n", val)
        }()
    }

    wg.Wait()
}
```

To see the actual bug, we simulate pre-1.22 behavior by declaring the variable outside the loop:

```go
func closureBugSimulated() {
    fmt.Println("\n=== Simulated Pre-1.22 Bug ===")
    var wg sync.WaitGroup
    values := []string{"alpha", "beta", "gamma", "delta", "epsilon"}

    var current string // single variable shared by all goroutines
    for _, v := range values {
        current = v // all goroutines see this same variable
        wg.Add(1)
        go func() {
            defer wg.Done()
            fmt.Printf("  goroutine sees: %s\n", current)
        }()
    }

    wg.Wait()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected for `closureBugSimulated`: most or all goroutines print "epsilon" (the last value).
```
=== Simulated Pre-1.22 Bug ===
  goroutine sees: epsilon
  goroutine sees: epsilon
  goroutine sees: epsilon
  goroutine sees: epsilon
  goroutine sees: epsilon
```

## Step 2 -- Fix by Passing as Parameter

Implement `closureFixParameter` to fix the bug by passing the value as a function parameter:

```go
func closureFixParameter() {
    fmt.Println("\n=== Fix: Pass as Parameter ===")
    var wg sync.WaitGroup
    values := []string{"alpha", "beta", "gamma", "delta", "epsilon"}

    var current string
    for _, v := range values {
        current = v
        wg.Add(1)
        go func(val string) { // val is a COPY, independent per goroutine
            defer wg.Done()
            fmt.Printf("  goroutine sees: %s\n", val)
        }(current) // current is copied into val at launch time
    }

    wg.Wait()
}
```

When you pass `current` as an argument to the goroutine function, Go copies the value at the point of the `go` call. Each goroutine gets its own independent copy.

### Intermediate Verification
```bash
go run main.go
```
All five values should appear (in any order), each exactly once:
```
=== Fix: Pass as Parameter ===
  goroutine sees: gamma
  goroutine sees: alpha
  goroutine sees: beta
  goroutine sees: epsilon
  goroutine sees: delta
```

## Step 3 -- Fix by Local Variable

Implement `closureFixLocalVar` showing the alternative fix using a local variable inside the loop body:

```go
func closureFixLocalVar() {
    fmt.Println("\n=== Fix: Local Variable ===")
    var wg sync.WaitGroup
    values := []string{"alpha", "beta", "gamma", "delta", "epsilon"}

    var current string
    for _, v := range values {
        current = v
        val := current // new variable per iteration, captures current value
        wg.Add(1)
        go func() {
            defer wg.Done()
            fmt.Printf("  goroutine sees: %s\n", val)
        }()
    }

    wg.Wait()
}
```

`val := current` creates a new variable on each iteration. The closure captures this new variable, which does not change.

### Intermediate Verification
```bash
go run main.go
```
Same correct output: all five values, each once.

## Step 4 -- Detect with Race Detector

Run the entire program with the race detector:

```bash
go run -race main.go
```

The `closureBugSimulated` function will trigger a data race warning because `current` is written by the loop and read by the goroutines concurrently. The fixed versions will not.

### Intermediate Verification
Confirm race warnings only from `closureBugSimulated`, not from the fixed versions.

## Step 5 -- Go 1.22 Loop Variable Change

In Go 1.22, each iteration of a `for` loop creates a new loop variable. This means:

```go
// Go 1.22+: this is now safe (each iteration has its own i and v)
for i, v := range values {
    go func() {
        fmt.Println(i, v) // i and v are unique per iteration
    }()
}
```

However, the simulated bug (variable declared outside the loop) is NOT fixed by Go 1.22 because the variable is not a loop variable. The underlying lesson -- closures capture references, not values -- remains critical.

## Common Mistakes

### Assuming All Variables in a Loop Are Per-Iteration
Only the loop variables declared in the `for` statement itself get per-iteration semantics in Go 1.22. Variables declared before the loop and modified inside it are still shared.

### Race Detector Not Catching All Closure Bugs
If all goroutines happen to read the variable after the loop finishes (no concurrent write), the race detector may not report it. The bug (all goroutines seeing the same value) still exists -- it is a logic bug, not just a data race.

### Thinking time.Sleep Fixes It
Adding sleep between goroutine launches does not fix the problem. The goroutine captures a reference to the variable, not a snapshot. Even if the goroutine starts immediately, the next loop iteration can change the variable before the goroutine reads it.

## Verify What You Learned

1. Run `go run -race main.go` and confirm which functions trigger race warnings
2. Why does the closure capture a reference and not a value?
3. What changed in Go 1.22 regarding loop variables?
4. Is the "pass as parameter" fix still useful in Go 1.22+? When?

## What's Next
Continue to [08-race-free-design-patterns](../08-race-free-design-patterns/08-race-free-design-patterns.md) to learn design patterns that make races impossible by construction.

## Summary
- Closures capture variables by reference, not by value
- In a loop, all goroutine closures share the same loop variable (pre-1.22 for loop vars, always for external vars)
- Fix 1: pass the variable as a function parameter (creates a copy)
- Fix 2: declare a new local variable inside the loop body
- Go 1.22 creates a new variable per loop iteration, fixing the most common case
- The underlying concept (capture by reference) still matters for non-loop variables
- The race detector catches concurrent read/write, but may not flag the logic bug if timing aligns

## Reference
- [Go Wiki: Common Mistakes -- Using Goroutines on Loop Iterator Variables](https://go.dev/wiki/CommonMistakes)
- [Go 1.22 Release Notes: Loopvar](https://go.dev/doc/go1.22#language)
- [Go Blog: Fixing For Loops in Go 1.22](https://go.dev/blog/loopvar-preview)
