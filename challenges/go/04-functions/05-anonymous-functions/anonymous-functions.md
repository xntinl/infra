# 5. Anonymous Functions

<!--
difficulty: basic
concepts: [anonymous-functions, function-literals, iife-pattern, inline-functions]
tools: [go]
estimated_time: 15m
bloom_level: understand
prerequisites: [function-declaration, first-class-functions]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor

## Learning Objectives

- **Explain** what anonymous functions (function literals) are and when to use them
- **Identify** the IIFE (Immediately Invoked Function Expression) pattern in Go
- **Understand** common use cases for anonymous functions: goroutines, deferred calls, and inline callbacks

## Why Anonymous Functions

Anonymous functions — also called function literals — are functions defined without a name. You write them inline wherever a function value is needed. This avoids polluting the package namespace with one-off helper functions that are only used in a single place.

Anonymous functions are everywhere in Go. You use them with `go` to launch goroutines, with `defer` for cleanup, as arguments to `sort.Slice`, as HTTP handlers, and in test table setups. They keep related code close together and reduce the mental overhead of jumping between function definitions.

The IIFE pattern — defining and immediately calling a function — is useful for scoping variables or performing one-time initialization in a controlled scope.

## Step 1 — Basic Anonymous Function

An anonymous function is a `func` literal without a name:

```go
package main

import "fmt"

func main() {
    greet := func(name string) string {
        return "Hello, " + name
    }

    fmt.Println(greet("Gopher"))
    fmt.Println(greet("World"))
}
```

The variable `greet` holds a function value. It behaves identically to a named function but exists only within the scope of `main`.

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
Hello, Gopher
Hello, World
```

## Step 2 — Immediately Invoked Function Expression (IIFE)

You can define and call a function in a single expression by adding `()` after the function body:

```go
package main

import "fmt"

func main() {
    result := func(a, b int) int {
        return a * b
    }(6, 7)

    fmt.Println("Result:", result)
}
```

The IIFE pattern is useful for creating a limited scope. Variables declared inside the function do not leak into the surrounding scope:

```go
package main

import "fmt"

func main() {
    msg := func() string {
        x := "computed"
        y := "value"
        return x + " " + y
    }()

    fmt.Println(msg)
    // x and y are not accessible here
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
Result: 42
```

## Step 3 — Anonymous Functions with `go`

Anonymous functions are the most common way to launch goroutines:

```go
package main

import (
    "fmt"
    "sync"
)

func main() {
    var wg sync.WaitGroup

    for i := range 5 {
        wg.Add(1)
        go func() {
            defer wg.Done()
            fmt.Printf("goroutine %d\n", i)
        }()
    }

    wg.Wait()
}
```

In Go 1.22+, the loop variable `i` is scoped per iteration, so each goroutine captures its own copy.

### Intermediate Verification

```bash
go run main.go
```

Expected output (order may vary):

```
goroutine 0
goroutine 1
goroutine 2
goroutine 3
goroutine 4
```

## Step 4 — Anonymous Functions with `defer`

Anonymous functions work well with `defer` when you need cleanup logic that depends on local state:

```go
package main

import "fmt"

func process() {
    fmt.Println("Starting process")

    defer func() {
        fmt.Println("Cleanup complete")
    }()

    fmt.Println("Doing work...")
}

func main() {
    process()
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
Starting process
Doing work...
Cleanup complete
```

## Step 5 — Anonymous Functions as Sort Comparators

A practical use case is passing anonymous functions to sorting:

```go
package main

import (
    "fmt"
    "slices"
)

func main() {
    words := []string{"banana", "apple", "cherry", "date"}

    slices.SortFunc(words, func(a, b string) int {
        if len(a) < len(b) {
            return -1
        }
        if len(a) > len(b) {
            return 1
        }
        return 0
    })

    fmt.Println(words)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
[date apple banana cherry]
```

## Common Mistakes

| Mistake | Why It Fails |
|---|---|
| Forgetting `()` at the end of an IIFE | You get a function value, not the result of calling it |
| Using anonymous functions for complex reusable logic | Named functions are easier to test and document |
| Deeply nesting anonymous functions | Hard to read; extract to named functions when nesting exceeds two levels |
| Forgetting `defer` runs at function exit, not block exit | `defer` in a loop body runs when the enclosing function returns, not at the end of each iteration |

## Verify What You Learned

1. Write an IIFE that computes the factorial of 10 and prints it.
2. Use an anonymous function with `slices.SortFunc` to sort a `[]int` in descending order.
3. Write a function that returns an anonymous function which generates sequential IDs starting from 1.

## What's Next

Next you will learn about **function types and callbacks** — declaring named types for function signatures and using the callback pattern.

## Summary

- Anonymous functions are `func` literals defined without a name
- They are used inline as values, arguments, goroutine bodies, and deferred calls
- The IIFE pattern `func() { ... }()` runs a function immediately and scopes variables
- Anonymous functions can capture variables from their enclosing scope (closures)
- Keep anonymous functions short; extract complex logic into named functions

## Reference

- [Go spec: Function literals](https://go.dev/ref/spec#Function_literals)
- [Go tour: Function closures](https://go.dev/tour/moretypes/25)
