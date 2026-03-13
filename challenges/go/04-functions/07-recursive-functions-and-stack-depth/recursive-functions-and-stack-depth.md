# 7. Recursive Functions and Stack Depth

<!--
difficulty: intermediate
concepts: [recursion, base-case, stack-overflow, tail-recursion, iterative-conversion]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [function-declaration, control-flow]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor

## Learning Objectives

- **Apply** recursive solutions to problems with self-similar substructure
- **Analyze** when recursion is appropriate versus iteration
- **Identify** stack overflow risks and Go's lack of tail-call optimization

## Why Recursive Functions

Recursion is a technique where a function calls itself to solve smaller instances of the same problem. Some problems — tree traversal, directory walking, mathematical sequences — are naturally recursive. Go supports recursion, and its goroutine stacks grow dynamically (starting at a few KB and growing as needed), so you can recurse deeper than in languages with fixed-size stacks.

However, Go does not perform tail-call optimization (TCO). This means every recursive call adds a frame to the stack. For very deep recursion (hundreds of thousands of calls), you will eventually hit a stack overflow. Understanding this tradeoff helps you decide when to recurse and when to convert to iteration.

## Step 1 — Basic Recursion: Factorial

Every recursive function needs a base case (when to stop) and a recursive case (how to break down the problem):

```go
package main

import "fmt"

func factorial(n int) int {
    if n <= 1 {
        return 1 // base case
    }
    return n * factorial(n-1) // recursive case
}

func main() {
    for i := range 8 {
        fmt.Printf("%d! = %d\n", i, factorial(i))
    }
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
0! = 1
1! = 1
2! = 2
3! = 6
4! = 24
5! = 120
6! = 720
7! = 5040
```

## Step 2 — Recursion with Slices: Binary Search

Recursive binary search demonstrates how to reduce the problem size on each call:

```go
package main

import "fmt"

func binarySearch(sorted []int, target int) int {
    return search(sorted, target, 0, len(sorted)-1)
}

func search(sorted []int, target, low, high int) int {
    if low > high {
        return -1 // not found
    }
    mid := low + (high-low)/2
    switch {
    case sorted[mid] == target:
        return mid
    case sorted[mid] < target:
        return search(sorted, target, mid+1, high)
    default:
        return search(sorted, target, low, mid-1)
    }
}

func main() {
    data := []int{2, 5, 8, 12, 16, 23, 38, 56, 72, 91}

    fmt.Println("Index of 23:", binarySearch(data, 23))
    fmt.Println("Index of 99:", binarySearch(data, 99))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
Index of 23: 5
Index of 99: -1
```

## Step 3 — Recursion for Tree-Like Structures

Recursion shines when processing nested or tree-like data:

```go
package main

import "fmt"

type Node struct {
    Value    int
    Children []*Node
}

func sumTree(n *Node) int {
    if n == nil {
        return 0
    }
    total := n.Value
    for _, child := range n.Children {
        total += sumTree(child)
    }
    return total
}

func main() {
    tree := &Node{
        Value: 1,
        Children: []*Node{
            {Value: 2, Children: []*Node{
                {Value: 4},
                {Value: 5},
            }},
            {Value: 3, Children: []*Node{
                {Value: 6},
            }},
        },
    }

    fmt.Println("Sum:", sumTree(tree))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
Sum: 21
```

## Step 4 — Stack Overflow and Go's Stack Growth

Go goroutines start with a small stack (a few KB) that grows dynamically. However, there is still an upper limit (default 1 GB). Extremely deep recursion will crash:

```go
package main

import "fmt"

func deepRecurse(n int) int {
    if n == 0 {
        return 0
    }
    return 1 + deepRecurse(n-1)
}

func main() {
    // This will work fine — stack grows as needed
    fmt.Println(deepRecurse(10000))

    // Uncomment the next line to see a stack overflow:
    // fmt.Println(deepRecurse(100_000_000))
}
```

You can check the maximum stack size:

```go
package main

import (
    "fmt"
    "runtime/debug"
)

func main() {
    fmt.Println("Max stack size:", debug.SetMaxStack(0))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
10000
```

## Step 5 — Converting Recursion to Iteration

When recursion is too deep, convert to iteration. The factorial function converts directly:

```go
package main

import "fmt"

func factorialIterative(n int) int {
    result := 1
    for i := 2; i <= n; i++ {
        result *= i
    }
    return result
}

func main() {
    fmt.Println(factorialIterative(7))
    fmt.Println(factorialIterative(20))
}
```

For tree traversal, use an explicit stack:

```go
package main

import "fmt"

type Node struct {
    Value    int
    Children []*Node
}

func sumTreeIterative(root *Node) int {
    if root == nil {
        return 0
    }
    total := 0
    stack := []*Node{root}
    for len(stack) > 0 {
        n := stack[len(stack)-1]
        stack = stack[:len(stack)-1]
        total += n.Value
        stack = append(stack, n.Children...)
    }
    return total
}

func main() {
    tree := &Node{
        Value: 1,
        Children: []*Node{
            {Value: 2, Children: []*Node{
                {Value: 4},
                {Value: 5},
            }},
            {Value: 3},
        },
    }
    fmt.Println("Sum:", sumTreeIterative(tree))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
Sum: 15
```

## Common Mistakes

| Mistake | Why It Fails |
|---|---|
| Missing the base case | Infinite recursion leading to stack overflow |
| Not reducing the problem size | Each recursive call must move toward the base case |
| Assuming Go optimizes tail calls | Go does **not** perform TCO; every call adds a stack frame |
| Using recursion for simple linear problems | A loop is clearer and uses constant stack space |

## Verify What You Learned

1. Write a recursive function `fibonacci(n int) int` and observe how slow it becomes for `n > 40`. Then write an iterative version and compare.
2. Write a recursive function `flatten(nested []any) []int` that flattens nested slices of integers (use type assertions).
3. Convert the recursive binary search from Step 2 into an iterative version.

## What's Next

Next you will learn about **init functions and package initialization** — special functions that run automatically when a package is loaded.

## Summary

- Recursive functions call themselves with smaller inputs until a base case is reached
- Go goroutine stacks grow dynamically but have an upper limit
- Go does not optimize tail calls — deep recursion will overflow the stack
- Tree-like structures are natural fits for recursion
- Convert to iteration (with an explicit stack if needed) when recursion depth is unbounded

## Reference

- [Go spec: Calls](https://go.dev/ref/spec#Calls)
- [Go blog: Stack traces in Go](https://www.ardanlabs.com/blog/2015/01/stack-traces-in-go.html)
