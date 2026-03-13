# 9. Closure Gotchas — Loop Variable Capture

<!--
difficulty: intermediate
concepts: [closure-capture, loop-variable-bug, go-1.22-fix, goroutine-closures]
tools: [go]
estimated_time: 25m
bloom_level: analyze
prerequisites: [closures, anonymous-functions, goroutines-basics]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor

## Learning Objectives

- **Analyze** the classic loop variable capture bug in pre-Go 1.22 code
- **Apply** the fix patterns: shadowing, parameter passing, and Go 1.22 per-iteration scoping
- **Identify** situations where capture bugs can still occur in Go 1.22+

## Why This Matters

The loop variable capture bug was one of the most common mistakes in Go for over a decade. When a closure captures a loop variable, it captures the variable itself — not its current value. In pre-Go 1.22 code, all iterations of a `for` loop shared a single variable, so closures created in a loop would all see the final value of that variable.

Go 1.22 changed `for` loop semantics so that each iteration gets its own copy of the loop variable. This fixed the most common form of the bug. However, understanding the underlying mechanism is still important because (1) you will encounter pre-1.22 code, (2) similar capture issues exist with non-loop variables, and (3) the same concept applies in every language with closures.

## Step 1 — The Classic Bug (Pre-Go 1.22 Behavior)

Before Go 1.22, this code was buggy:

```go
// Pre-Go 1.22 behavior (DO NOT rely on this)
// All goroutines would print "2" because they share the same variable
funcs := []func(){}
for i := 0; i < 3; i++ {
    funcs = append(funcs, func() {
        fmt.Println(i) // captures the variable i, not the value
    })
}
for _, f := range funcs {
    f() // would print: 3, 3, 3 (pre-1.22)
}
```

The single variable `i` is shared by all closures. By the time the closures execute, `i` has reached its final value.

## Step 2 — Go 1.22 Fix: Per-Iteration Variables

Go 1.22 changed loop variable semantics. Each iteration now creates a new variable:

```go
package main

import "fmt"

func main() {
    funcs := []func(){}
    for i := range 3 {
        funcs = append(funcs, func() {
            fmt.Println(i)
        })
    }
    for _, f := range funcs {
        f()
    }
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output (Go 1.22+):

```
0
1
2
```

Each closure captures its own copy of `i` because Go 1.22 creates a new variable per iteration.

## Step 3 — Pre-1.22 Fix Patterns (Still Good to Know)

You will encounter legacy code that uses these fix patterns. They are still valid and useful:

**Pattern 1: Shadow the variable**

```go
package main

import "fmt"

func main() {
    funcs := []func(){}
    for i := range 3 {
        i := i // shadow: creates a new variable in this scope
        funcs = append(funcs, func() {
            fmt.Println(i)
        })
    }
    for _, f := range funcs {
        f()
    }
}
```

**Pattern 2: Pass as a function parameter**

```go
package main

import "fmt"

func main() {
    funcs := []func(){}
    for i := range 3 {
        funcs = append(funcs, func(n int) func() {
            return func() { fmt.Println(n) }
        }(i))
    }
    for _, f := range funcs {
        f()
    }
}
```

### Intermediate Verification

```bash
go run main.go
```

Both patterns produce:

```
0
1
2
```

## Step 4 — Goroutines and Loop Variables

The most dangerous form of this bug involves goroutines, because the timing makes the behavior nondeterministic:

```go
package main

import (
    "fmt"
    "sync"
)

func main() {
    var wg sync.WaitGroup

    // Go 1.22+: each iteration gets its own copy of i
    for i := range 5 {
        wg.Add(1)
        go func() {
            defer wg.Done()
            fmt.Println(i)
        }()
    }

    wg.Wait()
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output (order may vary, but all values 0-4 appear):

```
0
4
1
3
2
```

## Step 5 — Cases Where Capture Bugs Still Exist

Go 1.22 fixed `for` loop variables, but capture-by-reference issues can still occur with other mutable variables:

```go
package main

import "fmt"

func main() {
    // This is NOT a loop variable — still captured by reference
    x := 0
    funcs := []func(){}

    funcs = append(funcs, func() { fmt.Println(x) })
    x = 10
    funcs = append(funcs, func() { fmt.Println(x) })
    x = 20

    for _, f := range funcs {
        f() // both print 20, because x is shared
    }
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
20
20
```

Another case: closures over pointer-like values:

```go
package main

import "fmt"

type Config struct {
    Name string
}

func main() {
    configs := []Config{
        {Name: "alpha"},
        {Name: "beta"},
        {Name: "gamma"},
    }

    printers := []func(){}
    for _, cfg := range configs {
        printers = append(printers, func() {
            fmt.Println(cfg.Name)
        })
    }

    for _, p := range printers {
        p()
    }
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output (Go 1.22+):

```
alpha
beta
gamma
```

In Go 1.22+, `cfg` is per-iteration, so this works correctly. In pre-1.22, it would print `gamma` three times.

## Step 6 — Detecting Capture Bugs with `go vet`

The `go vet` tool (and `loopclosure` analyzer) can detect some loop variable capture bugs:

```bash
go vet ./...
```

For pre-1.22 modules, `go vet` warns about loop variable capture in goroutines. With Go 1.22+, the warning is no longer necessary for `for` loops since the language semantics were changed.

## Common Mistakes

| Mistake | Why It Fails |
|---|---|
| Assuming Go 1.22 fixes all capture issues | Only `for` loop variables are per-iteration; other shared variables are still captured by reference |
| Using `&item` inside a range loop (pre-1.22) | All pointers point to the same address |
| Ignoring `go vet` warnings | The tooling catches many capture bugs automatically |
| Not testing concurrent closures | Race conditions from capture bugs are timing-dependent and may not show in simple tests |

## Verify What You Learned

1. Write a program that creates 5 goroutines in a loop. Verify that each goroutine prints its own iteration value (0-4).
2. Create a scenario where a non-loop variable is captured by reference, causing unexpected behavior. Then fix it by copying the value before capturing.
3. Explain why `i := i` (shadowing) fixes the pre-1.22 capture bug.

## What's Next

Next you will learn about **higher-order functions** — implementing `map`, `filter`, and `reduce` patterns manually in Go.

## Summary

- Pre-Go 1.22: all iterations of a `for` loop shared one variable, causing capture bugs
- Go 1.22+: each loop iteration creates a new variable, fixing the most common capture bug
- Fix patterns for legacy code: shadow (`i := i`) or pass as a parameter
- Non-loop variables are still captured by reference — be careful with shared mutable state
- Use `go vet` to detect capture issues in older code

## Reference

- [Go wiki: Common Mistakes — Using goroutines on loop iterator variables](https://go.dev/wiki/CommonMistakes)
- [Go 1.22 Release Notes: Loopvar](https://go.dev/doc/go1.22#language)
- [Go blog: Fixing For Loops in Go 1.22](https://go.dev/blog/loopvar-preview)
