# 3. Variadic Functions

<!--
difficulty: basic
concepts: [variadic-functions, spread-operator, slice-passing]
tools: [go]
estimated_time: 15m
bloom_level: remember
prerequisites: [function-declaration, slices-basics]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor

## Learning Objectives

- **Recall** the `...T` syntax for declaring variadic parameters
- **Identify** how variadic arguments are received as a slice inside the function
- **Recall** how to pass a slice to a variadic function using `...`

## Why Variadic Functions

Variadic functions accept a variable number of arguments. You have already used one — `fmt.Println` can take any number of arguments. This pattern is useful when you do not know at compile time how many values the caller will pass.

Go implements variadic parameters elegantly: the caller passes individual values, and the function receives them as a slice. This means you get the convenience of a flexible API with the safety and simplicity of slice operations inside the function body.

Variadic functions appear throughout Go's standard library: `fmt.Println`, `fmt.Sprintf`, `append`, `log.Printf`, and many more. Understanding how they work lets you write similarly flexible APIs.

## Step 1 — Declaring a Variadic Function

The last parameter in a function signature can use `...T` to accept zero or more values of type `T`:

```go
package main

import "fmt"

func sum(numbers ...int) int {
    total := 0
    for _, n := range numbers {
        total += n
    }
    return total
}

func main() {
    fmt.Println(sum(1, 2, 3))
    fmt.Println(sum(10, 20))
    fmt.Println(sum())
}
```

Inside the function, `numbers` has type `[]int`. You can call `len()`, iterate with `range`, and use it like any other slice.

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
6
30
0
```

## Step 2 — Mixing Regular and Variadic Parameters

You can have regular parameters before the variadic one, but the variadic parameter must always be last:

```go
package main

import "fmt"

func greetAll(greeting string, names ...string) {
    for _, name := range names {
        fmt.Printf("%s, %s!\n", greeting, name)
    }
}

func main() {
    greetAll("Hello", "Alice", "Bob", "Charlie")
    greetAll("Goodbye")
}
```

When no variadic arguments are passed, the slice is empty (not nil in practice for the iteration).

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
Hello, Alice!
Hello, Bob!
Hello, Charlie!
```

## Step 3 — Passing a Slice to a Variadic Function

If you already have a slice and want to pass it to a variadic function, use the `...` operator after the slice:

```go
package main

import "fmt"

func sum(numbers ...int) int {
    total := 0
    for _, n := range numbers {
        total += n
    }
    return total
}

func main() {
    nums := []int{5, 10, 15, 20}
    result := sum(nums...)
    fmt.Println("Sum:", result)
}
```

The `...` after the slice unpacks it into individual arguments. You cannot mix individual values and a spread slice in the same call.

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
Sum: 50
```

## Step 4 — How `fmt.Println` Uses Variadic Parameters

The signature of `fmt.Println` is:

```go
func Println(a ...any) (n int, err error)
```

The type `any` (alias for `interface{}`) means it accepts values of any type. This is why you can write:

```go
package main

import "fmt"

func main() {
    fmt.Println("Name:", "Gopher", "Age:", 10, "Active:", true)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
Name: Gopher Age: 10 Active: true
```

## Step 5 — The Built-in `append` Is Variadic

The built-in `append` function is variadic, which is why you can append multiple elements or spread another slice:

```go
package main

import "fmt"

func main() {
    a := []int{1, 2, 3}
    a = append(a, 4, 5, 6)
    fmt.Println(a)

    b := []int{7, 8, 9}
    a = append(a, b...)
    fmt.Println(a)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
[1 2 3 4 5 6]
[1 2 3 4 5 6 7 8 9]
```

## Common Mistakes

| Mistake | Why It Fails |
|---|---|
| Putting the variadic parameter first | Only the last parameter can be variadic |
| Mixing individual args with `...` spread | `sum(1, nums...)` is a compile error |
| Modifying the variadic slice inside the function | The underlying array may be shared with the caller's slice when using `...` |
| Having two variadic parameters | A function can have at most one variadic parameter |

## Verify What You Learned

1. Write a function `max(first int, rest ...int) int` that returns the maximum value. Requiring at least one argument prevents calling `max()` with no values.
2. Write a function `joinWords(sep string, words ...string) string` that joins words with a separator (without using `strings.Join`).
3. Call both functions from `main` using both individual arguments and slice spreading.

## What's Next

Next you will learn about **first-class functions and closures** — passing functions as values and capturing variables from enclosing scopes.

## Summary

- Variadic functions use `...T` as the last parameter to accept zero or more values
- Inside the function, the variadic parameter is a `[]T` slice
- Pass a slice to a variadic function with `slice...`
- You cannot mix individual arguments with a spread slice in the same call
- Standard library functions like `fmt.Println`, `fmt.Sprintf`, and `append` are variadic

## Reference

- [Go spec: Passing arguments to ... parameters](https://go.dev/ref/spec#Passing_arguments_to_..._parameters)
- [Effective Go: Append](https://go.dev/doc/effective_go#append)
