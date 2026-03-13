# 1. Function Declaration and Multiple Return Values

<!--
difficulty: basic
concepts: [function-declaration, multiple-return-values, error-return-pattern]
tools: [go]
estimated_time: 15m
bloom_level: remember
prerequisites: [variables, types, control-flow]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor

## Learning Objectives

- **Recall** the syntax for declaring functions in Go
- **Identify** how Go functions can return multiple values
- **Recall** the idiomatic error return pattern using multiple returns

## Why Functions

Functions are the fundamental building blocks of any Go program. Every Go program starts with `func main()`, and from there you decompose your logic into smaller, reusable functions.

Unlike many languages that restrict you to a single return value, Go functions can return multiple values. This is not just a convenience feature — it is central to Go's error handling philosophy. Instead of throwing exceptions, Go functions return an error value alongside the result, and the caller decides what to do with it.

Understanding function declaration and multiple return values is the foundation for writing idiomatic Go. Every Go developer writes dozens of functions daily, and the patterns you learn here appear in the standard library, third-party packages, and production codebases everywhere.

## Step 1 — Declaring a Simple Function

Create a file called `main.go`:

```go
package main

import "fmt"

func greet(name string) string {
    return "Hello, " + name + "!"
}

func main() {
    message := greet("Gopher")
    fmt.Println(message)
}
```

A function declaration has four parts:
- The `func` keyword
- A name (`greet`)
- Parameters in parentheses (`name string`)
- A return type (`string`)

When multiple parameters share the same type, you can group them:

```go
func add(a, b int) int {
    return a + b
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
Hello, Gopher!
```

## Step 2 — Returning Multiple Values

Go functions can return more than one value. Wrap the return types in parentheses:

```go
package main

import "fmt"

func swap(a, b string) (string, string) {
    return b, a
}

func main() {
    first, second := swap("hello", "world")
    fmt.Println(first, second)
}
```

The caller receives both values and assigns them with `:=`. You must capture all returned values — Go does not let you silently ignore them (unless you use the blank identifier `_`).

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
world hello
```

## Step 3 — The Error Return Pattern

The most important use of multiple returns is the `(result, error)` pattern. By convention, the error is the last return value:

```go
package main

import (
    "errors"
    "fmt"
    "math"
)

func sqrt(x float64) (float64, error) {
    if x < 0 {
        return 0, errors.New("cannot take square root of negative number")
    }
    return math.Sqrt(x), nil
}

func main() {
    result, err := sqrt(16)
    if err != nil {
        fmt.Println("Error:", err)
        return
    }
    fmt.Printf("sqrt(16) = %.1f\n", result)

    result, err = sqrt(-4)
    if err != nil {
        fmt.Println("Error:", err)
        return
    }
    fmt.Printf("sqrt(-4) = %.1f\n", result)
}
```

Key points:
- Return `nil` for the error when the operation succeeds
- Return a zero value for the result when the operation fails
- Always check the error before using the result

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
sqrt(16) = 4.0
Error: cannot take square root of negative number
```

## Step 4 — Functions with No Return Value

Functions that perform side effects (printing, writing files) often return nothing:

```go
package main

import "fmt"

func printHeader(title string) {
    fmt.Println("====================")
    fmt.Println(title)
    fmt.Println("====================")
}

func main() {
    printHeader("My Report")
}
```

You can also use an early `return` (without a value) to exit a void function:

```go
func logMessage(msg string) {
    if msg == "" {
        return
    }
    fmt.Println("[LOG]", msg)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
====================
My Report
====================
```

## Step 5 — Discarding Return Values with the Blank Identifier

When a function returns multiple values and you only need some of them, use `_`:

```go
package main

import "fmt"

func divide(a, b float64) (float64, float64) {
    quotient := a / b
    remainder := float64(int(a) % int(b))
    return quotient, remainder
}

func main() {
    quotient, _ := divide(17, 5)
    fmt.Printf("17 / 5 = %.1f\n", quotient)

    _, remainder := divide(17, 5)
    fmt.Printf("17 %% 5 = %.0f\n", remainder)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
17 / 5 = 3.4
17 % 5 = 2
```

## Common Mistakes

| Mistake | Why It Fails |
|---|---|
| Ignoring a returned error | The compiler may allow it but the program can panic on `nil` results |
| Returning the wrong number of values | Compiler error — must match the declared return signature |
| Forgetting parentheses around multiple return types | `func f() string, error` is a syntax error; use `(string, error)` |
| Using `:=` when the variable already exists | Use `=` for reassignment; `:=` only for new declarations |

## Verify What You Learned

1. Write a function `minMax(numbers []int) (int, int)` that returns both the minimum and maximum of a slice.
2. Write a function `safeDivide(a, b int) (int, error)` that returns an error when `b` is zero.
3. Call both functions from `main`, handling all errors.

## What's Next

In the next exercise you will learn about **named return values**, which let you give names to your return parameters and use naked returns.

## Summary

- Functions are declared with `func name(params) returnType`
- Go functions can return multiple values by wrapping return types in parentheses
- The `(result, error)` pattern is idiomatic Go — always check errors
- Use `_` to discard values you do not need

## Reference

- [Go spec: Function declarations](https://go.dev/ref/spec#Function_declarations)
- [Effective Go: Multiple return values](https://go.dev/doc/effective_go#multiple-returns)
- [Go blog: Error handling and Go](https://go.dev/blog/error-handling-and-go)
