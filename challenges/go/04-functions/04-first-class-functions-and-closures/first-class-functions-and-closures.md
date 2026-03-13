# 4. First-Class Functions and Closures

<!--
difficulty: basic
concepts: [first-class-functions, closures, captured-variables, function-values]
tools: [go]
estimated_time: 20m
bloom_level: understand
prerequisites: [function-declaration, variables]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor

## Learning Objectives

- **Explain** what it means for functions to be first-class values in Go
- **Identify** how closures capture variables from their enclosing scope
- **Understand** that closures hold references to variables, not copies of values

## Why First-Class Functions and Closures

In Go, functions are first-class citizens. This means you can assign a function to a variable, pass it as an argument, return it from another function, and store it in data structures. Functions are values, just like integers or strings.

A closure is a function value that references variables from outside its body. The function "closes over" those variables — it can read and modify them even after the enclosing function has returned. This is a powerful pattern for creating stateful functions without using structs.

First-class functions and closures enable patterns like callbacks, middleware, iterators, and functional transformations. They are foundational to writing clean, composable Go code.

## Step 1 — Functions as Values

You can assign a function to a variable and call it through that variable:

```go
package main

import "fmt"

func add(a, b int) int {
    return a + b
}

func multiply(a, b int) int {
    return a * b
}

func main() {
    op := add
    fmt.Println(op(3, 4))

    op = multiply
    fmt.Println(op(3, 4))

    fmt.Printf("Type of op: %T\n", op)
}
```

The variable `op` has type `func(int, int) int`. You can reassign it to any function with the same signature.

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
7
12
Type of op: func(int, int) int
```

## Step 2 — Passing Functions as Arguments

Functions can be passed to other functions as arguments:

```go
package main

import "fmt"

func apply(a, b int, operation func(int, int) int) int {
    return operation(a, b)
}

func main() {
    sum := apply(10, 20, func(a, b int) int {
        return a + b
    })
    fmt.Println("Sum:", sum)

    product := apply(10, 20, func(a, b int) int {
        return a * b
    })
    fmt.Println("Product:", product)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
Sum: 30
Product: 200
```

## Step 3 — Returning Functions

A function can return another function. This is the basis of closures:

```go
package main

import "fmt"

func multiplier(factor int) func(int) int {
    return func(x int) int {
        return x * factor
    }
}

func main() {
    double := multiplier(2)
    triple := multiplier(3)

    fmt.Println(double(5))
    fmt.Println(triple(5))
}
```

The returned function captures `factor` from its enclosing scope. Each call to `multiplier` creates a new closure with its own copy of `factor`.

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
10
15
```

## Step 4 — Closures Capture by Reference

Closures hold a reference to the captured variable, not a copy. This means modifications to the variable inside the closure affect the original, and vice versa:

```go
package main

import "fmt"

func counter() func() int {
    count := 0
    return func() int {
        count++
        return count
    }
}

func main() {
    next := counter()

    fmt.Println(next())
    fmt.Println(next())
    fmt.Println(next())

    // A new counter starts from zero
    another := counter()
    fmt.Println(another())
}
```

Each call to `counter()` creates a new `count` variable. The returned closure holds a reference to that specific variable.

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
1
2
3
1
```

## Step 5 — Storing Functions in Data Structures

Because functions are values, you can store them in maps, slices, and struct fields:

```go
package main

import "fmt"

func main() {
    operations := map[string]func(int, int) int{
        "add":      func(a, b int) int { return a + b },
        "subtract": func(a, b int) int { return a - b },
        "multiply": func(a, b int) int { return a * b },
    }

    for name, op := range operations {
        fmt.Printf("%s(6, 3) = %d\n", name, op(6, 3))
    }
}
```

### Intermediate Verification

```bash
go run main.go
```

Output will contain (order may vary due to map iteration):

```
add(6, 3) = 9
subtract(6, 3) = 3
multiply(6, 3) = 18
```

## Common Mistakes

| Mistake | Why It Fails |
|---|---|
| Assuming closures copy captured values | Closures hold references; mutations are shared |
| Comparing function values with `==` | Functions can only be compared to `nil`, not to each other |
| Ignoring that each closure invocation shares state | Two calls to `next()` in the counter example share the same `count` |
| Using a closure when a method on a struct would be clearer | Closures with complex state are harder to test; prefer structs |

## Verify What You Learned

1. Write a function `adder(initial int) func(int) int` that returns a closure. Each call to the closure should add the argument to a running total and return the new total.
2. Create a map of string-to-function that maps `"upper"` and `"lower"` to `strings.ToUpper` and `strings.ToLower`. Look up a function by key and apply it to a string.

## What's Next

Next you will explore **anonymous functions** — function literals that are defined inline without a name, including the IIFE (Immediately Invoked Function Expression) pattern.

## Summary

- Functions in Go are first-class values with a type like `func(int) int`
- You can assign, pass, return, and store functions just like any other value
- A closure captures variables from its enclosing scope by reference
- Each closure invocation shares its captured state
- Use closures for counters, factories, callbacks, and configuration

## Reference

- [Go spec: Function literals](https://go.dev/ref/spec#Function_literals)
- [Go tour: Function closures](https://go.dev/tour/moretypes/25)
- [Go blog: First Class Functions in Go](https://go.dev/blog/functions-codewalk)
