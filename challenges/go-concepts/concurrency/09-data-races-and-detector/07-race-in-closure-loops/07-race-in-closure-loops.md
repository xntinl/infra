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
- **Fix** the bug using two different techniques (parameter passing, local variable)
- **Understand** how Go 1.22 changed loop variable semantics and why the concept still matters

## Why Closure Races Matter

One of the most common concurrency bugs in Go is launching goroutines inside a loop where the goroutine closure captures the loop variable. Because closures capture variables **by reference**, all goroutines share the same variable. By the time the goroutines execute, the loop has often finished, and they all see the final value.

This bug is subtle because:
- It is not always a data race in the strict sense (it can also be a logic bug)
- The program compiles and runs without errors
- It sometimes appears to work (if goroutines execute fast enough)
- The fix is simple once you know the pattern

Starting with **Go 1.22**, the `for` loop creates a new variable for each iteration, which fixes the most common manifestation. However, understanding the underlying mechanism is essential because:
1. The same pattern appears with non-loop variables
2. Much existing code was written before Go 1.22
3. The concept of "capture by reference" applies everywhere closures are used

## Step 1 -- The Classic Bug

The `main.go` demonstrates the bug using a variable declared outside the loop (simulating pre-1.22 behavior):

```go
package main

import (
    "fmt"
    "sync"
)

func closureBugSimulated() {
    var wg sync.WaitGroup
    values := []string{"alpha", "beta", "gamma", "delta", "epsilon"}

    // Declaring current OUTSIDE the loop means all goroutines share it.
    var current string
    for _, v := range values {
        current = v // all goroutines point to this single variable
        wg.Add(1)
        go func() {
            defer wg.Done()
            // DATA RACE: current is written by the loop and read by this
            // goroutine concurrently.
            fmt.Printf("  goroutine sees: %s\n", current)
        }()
    }

    wg.Wait()
}

func main() {
    closureBugSimulated()
}
```

### Verification
```bash
go run main.go
```
Expected: most or all goroutines print "epsilon" (the last value):
```
--- Demo 1: The Classic Bug (simulated pre-1.22) ---
All goroutines see the LAST value because they capture a shared variable:
  goroutine sees: epsilon
  goroutine sees: epsilon
  goroutine sees: epsilon
  goroutine sees: epsilon
  goroutine sees: epsilon
```

```bash
go run -race main.go
```
Expected: `WARNING: DATA RACE` because `current` is written by the loop and read by goroutines concurrently:
```
==================
WARNING: DATA RACE
Read at 0x00c00011c120 by goroutine 7:
  main.closureBugSimulated.func1()
      /path/to/main.go:76 +0x7c

Previous write at 0x00c00011c120 by main goroutine:
  main.closureBugSimulated()
      /path/to/main.go:69 +0x230
==================
```

## Step 2 -- Fix by Passing as Parameter

Pass the value as a function parameter. Go copies the argument at the call site:

```go
package main

import (
    "fmt"
    "sync"
)

func closureFixParameter() {
    var wg sync.WaitGroup
    values := []string{"alpha", "beta", "gamma", "delta", "epsilon"}

    var current string
    for _, v := range values {
        current = v
        wg.Add(1)
        // val is a PARAMETER: Go copies current's value into val here.
        go func(val string) {
            defer wg.Done()
            fmt.Printf("  goroutine sees: %s\n", val)
        }(current) // copy happens at the go call
    }

    wg.Wait()
}

func main() {
    closureFixParameter()
}
```

### Verification
```bash
go run -race main.go
```
Expected: all five values appear (in any order), each exactly once, with zero race warnings:
```
--- Demo 2: Fix with Function Parameter ---
  goroutine sees: gamma
  goroutine sees: alpha
  goroutine sees: beta
  goroutine sees: epsilon
  goroutine sees: delta
```

## Step 3 -- Fix by Local Variable

Create a new local variable inside each loop iteration:

```go
package main

import (
    "fmt"
    "sync"
)

func closureFixLocalVar() {
    var wg sync.WaitGroup
    values := []string{"alpha", "beta", "gamma", "delta", "epsilon"}

    var current string
    for _, v := range values {
        current = v
        val := current // NEW variable per iteration
        wg.Add(1)
        go func() {
            defer wg.Done()
            fmt.Printf("  goroutine sees: %s\n", val)
        }()
    }

    wg.Wait()
}

func main() {
    closureFixLocalVar()
}
```

`val := current` creates a new variable on each iteration. The closure captures this new variable, which does not change after creation.

### Verification
```bash
go run -race main.go
```
Expected: same correct output, zero race warnings.

## Step 4 -- Go 1.22 Loop Variable Change

In Go 1.22+, each iteration of a `for` loop creates a new loop variable:

```go
// Go 1.22+: this is now safe (each iteration has its own v)
for _, v := range values {
    go func() {
        fmt.Println(v) // v is unique per iteration in Go 1.22+
    }()
}
```

**However**, variables declared OUTSIDE the loop are NOT affected by this change:

```go
var current string // declared outside -- still shared
for _, v := range values {
    current = v // still a single shared variable
    go func() {
        fmt.Println(current) // STILL A BUG even in Go 1.22+
    }()
}
```

The underlying lesson -- closures capture references, not values -- remains critical regardless of Go version.

## Step 5 -- Index Capture Bug

The same bug applies to integer indices, not just string values. The `main.go` demonstrates both the bug and the fix for integer loop variables:

### Verification
```bash
go run -race main.go
```
Expected:
```
--- Demo 5: Index Capture Bug ---
  BUG (shared index):
    goroutine sees index 4
    goroutine sees index 4
    goroutine sees index 4
    goroutine sees index 4
    goroutine sees index 4
  FIX (parameter copy):
    goroutine sees index 0
    goroutine sees index 3
    goroutine sees index 1
    goroutine sees index 4
    goroutine sees index 2
```

The BUG version shows all goroutines seeing index 4 (the last value). The FIX version shows all unique indices.

## Common Mistakes

### Assuming All Variables in a Loop Are Per-Iteration
Only the loop variables declared in the `for` statement itself get per-iteration semantics in Go 1.22. Variables declared before the loop and modified inside it are still shared.

### Race Detector Not Catching All Closure Bugs
If all goroutines happen to read the variable after the loop finishes (no concurrent write), the race detector may not report it. The bug (all goroutines seeing the same value) still exists -- it is a **logic bug**, not just a data race.

### Thinking time.Sleep Fixes It
Adding sleep between goroutine launches does not fix the problem. The goroutine captures a **reference** to the variable, not a snapshot. Even if the goroutine starts immediately, the next loop iteration can change the variable before the goroutine reads it.

### Forgetting Integer Indices
The bug is not limited to range values. Integer loop counters (`for i := 0; i < n; i++`) declared outside the loop are equally affected.

## Verify What You Learned

```bash
go run -race main.go
```

1. Confirm which functions trigger race warnings (Demo 1 and Demo 5 BUG only)
2. Why does the closure capture a reference and not a value?
3. What changed in Go 1.22 regarding loop variables?
4. Is the "pass as parameter" fix still useful in Go 1.22+? When?

## What's Next
Continue to [08-race-free-design-patterns](../08-race-free-design-patterns/08-race-free-design-patterns.md) to learn design patterns that make races impossible by construction.

## Summary
- Closures capture variables **by reference**, not by value
- In a loop, all goroutine closures share the same outer variable (always true for non-loop variables, pre-1.22 for loop variables)
- **Fix 1**: pass the variable as a function parameter (creates a copy at the go call)
- **Fix 2**: declare a new local variable inside the loop body (`val := current`)
- Go 1.22 creates a new variable per loop iteration, fixing the most common case
- The underlying concept (capture by reference) still matters for non-loop variables
- The race detector catches concurrent read/write, but may not flag the logic bug if timing aligns
- The bug applies equally to string values and integer indices

## Reference
- [Go Wiki: Common Mistakes -- Using Goroutines on Loop Iterator Variables](https://go.dev/wiki/CommonMistakes)
- [Go 1.22 Release Notes: Loopvar](https://go.dev/doc/go1.22#language)
- [Go Blog: Fixing For Loops in Go 1.22](https://go.dev/blog/loopvar-preview)
