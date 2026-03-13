# 10. Higher-Order Functions

<!--
difficulty: intermediate
concepts: [higher-order-functions, map, filter, reduce, function-composition]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [function-types, closures, slices-basics]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor

## Learning Objectives

- **Apply** the map, filter, and reduce patterns in Go
- **Apply** function composition to chain transformations
- **Analyze** the tradeoffs between higher-order functions and simple loops

## Why Higher-Order Functions

A higher-order function is a function that takes another function as an argument or returns a function as a result. You have already seen examples: `sort.Slice` takes a comparison function, and `http.HandleFunc` takes a handler function.

Go does not have built-in `map`, `filter`, or `reduce` functions like Python or JavaScript. The Go philosophy favors explicit loops for clarity. However, understanding how to implement these patterns teaches you function composition and prepares you for Go's generics, which make type-safe higher-order functions practical.

Implementing these patterns manually deepens your understanding of how functions, slices, and types interact.

## Step 1 — Implementing Map

The map pattern applies a function to every element of a slice:

```go
package main

import (
    "fmt"
    "strings"
)

func mapStrings(input []string, transform func(string) string) []string {
    result := make([]string, len(input))
    for i, s := range input {
        result[i] = transform(s)
    }
    return result
}

func main() {
    names := []string{"alice", "bob", "charlie"}

    upper := mapStrings(names, strings.ToUpper)
    fmt.Println(upper)

    lengths := mapStrings(names, func(s string) string {
        return fmt.Sprintf("%s(%d)", s, len(s))
    })
    fmt.Println(lengths)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
[ALICE BOB CHARLIE]
[alice(5) bob(3) charlie(7)]
```

## Step 2 — Implementing Filter

The filter pattern selects elements that satisfy a predicate:

```go
package main

import "fmt"

func filter(input []int, predicate func(int) bool) []int {
    var result []int
    for _, v := range input {
        if predicate(v) {
            result = append(result, v)
        }
    }
    return result
}

func main() {
    numbers := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

    evens := filter(numbers, func(n int) bool {
        return n%2 == 0
    })
    fmt.Println("Evens:", evens)

    greaterThan5 := filter(numbers, func(n int) bool {
        return n > 5
    })
    fmt.Println("Greater than 5:", greaterThan5)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
Evens: [2 4 6 8 10]
Greater than 5: [6 7 8 9 10]
```

## Step 3 — Implementing Reduce

Reduce (also called fold) combines all elements into a single value:

```go
package main

import "fmt"

func reduce(input []int, initial int, accumulator func(int, int) int) int {
    result := initial
    for _, v := range input {
        result = accumulator(result, v)
    }
    return result
}

func main() {
    numbers := []int{1, 2, 3, 4, 5}

    sum := reduce(numbers, 0, func(acc, n int) int {
        return acc + n
    })
    fmt.Println("Sum:", sum)

    product := reduce(numbers, 1, func(acc, n int) int {
        return acc * n
    })
    fmt.Println("Product:", product)

    max := reduce(numbers, numbers[0], func(acc, n int) int {
        if n > acc {
            return n
        }
        return acc
    })
    fmt.Println("Max:", max)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
Sum: 15
Product: 120
Max: 5
```

## Step 4 — Composing Functions

You can chain higher-order functions together to build data pipelines:

```go
package main

import "fmt"

func filterInts(input []int, pred func(int) bool) []int {
    var result []int
    for _, v := range input {
        if pred(v) {
            result = append(result, v)
        }
    }
    return result
}

func mapInts(input []int, transform func(int) int) []int {
    result := make([]int, len(input))
    for i, v := range input {
        result[i] = transform(v)
    }
    return result
}

func reduceInts(input []int, initial int, acc func(int, int) int) int {
    result := initial
    for _, v := range input {
        result = acc(result, v)
    }
    return result
}

func main() {
    numbers := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

    // Pipeline: filter evens -> square them -> sum
    evens := filterInts(numbers, func(n int) bool { return n%2 == 0 })
    squared := mapInts(evens, func(n int) int { return n * n })
    total := reduceInts(squared, 0, func(a, b int) int { return a + b })

    fmt.Println("Sum of squares of evens:", total)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
Sum of squares of evens: 220
```

## Step 5 — Generic Higher-Order Functions (Go 1.18+)

With generics, you can write type-safe versions that work with any type:

```go
package main

import "fmt"

func Map[T, U any](input []T, transform func(T) U) []U {
    result := make([]U, len(input))
    for i, v := range input {
        result[i] = transform(v)
    }
    return result
}

func Filter[T any](input []T, predicate func(T) bool) []T {
    var result []T
    for _, v := range input {
        if predicate(v) {
            result = append(result, v)
        }
    }
    return result
}

func Reduce[T, U any](input []T, initial U, accumulator func(U, T) U) U {
    result := initial
    for _, v := range input {
        result = accumulator(result, v)
    }
    return result
}

func main() {
    words := []string{"hello", "world", "go", "is", "great"}

    // Filter words longer than 2 chars, map to lengths, sum
    long := Filter(words, func(s string) bool { return len(s) > 2 })
    lengths := Map(long, func(s string) int { return len(s) })
    total := Reduce(lengths, 0, func(acc, n int) int { return acc + n })

    fmt.Println("Long words:", long)
    fmt.Println("Lengths:", lengths)
    fmt.Println("Total chars:", total)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
Long words: [hello world great]
Lengths: [5 5 5]
Total chars: 15
```

## Common Mistakes

| Mistake | Why It Fails |
|---|---|
| Overusing higher-order functions where a loop is clearer | Go favors readability; a simple loop is often better |
| Creating intermediate slices wastefully | Each `Map`/`Filter` allocates a new slice; a single loop avoids this |
| Not handling empty slices in `Reduce` | Calling `Reduce` on an empty slice with no sensible initial value can produce wrong results |
| Using non-generic versions when generics are available | Type-specific versions (`mapInts`, `filterStrings`) lead to code duplication |

## Verify What You Learned

1. Implement a generic `ForEach[T any](input []T, action func(T))` function.
2. Implement `Any[T any](input []T, pred func(T) bool) bool` and `All[T any](input []T, pred func(T) bool) bool`.
3. Build a pipeline that takes a `[]string` of sentences, filters those containing "Go", maps them to word counts, and reduces to the total word count.

## What's Next

Next you will learn about **defer stacking and resource cleanup** — how `defer` manages cleanup and how deferred calls stack.

## Summary

- Higher-order functions take or return functions
- Map transforms each element, Filter selects elements, Reduce combines elements
- Go favors explicit loops, but higher-order functions are useful for composable pipelines
- Generics (Go 1.18+) make higher-order functions type-safe across any type
- Prefer a simple loop when a pipeline creates unnecessary intermediate allocations

## Reference

- [Go spec: Function types](https://go.dev/ref/spec#Function_types)
- [Go generics tutorial](https://go.dev/doc/tutorial/generics)
