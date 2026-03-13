# 1. Range Over Integers

<!--
difficulty: basic
concepts: [range-over-int, for-range, go-1-22, loop-syntax, iteration]
tools: [go]
estimated_time: 15m
bloom_level: remember
prerequisites: [control-flow, variables-types-and-constants]
-->

## Prerequisites

- Go 1.22+ installed
- Familiarity with `for` loops

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** the `range N` syntax introduced in Go 1.22
- **Recall** how `range` over an integer differs from `range` over a slice
- **Identify** common use cases for integer range

## Why Range Over Integers

Before Go 1.22, repeating something N times required the classic C-style loop: `for i := 0; i < n; i++`. This syntax is verbose and offers multiple places for off-by-one errors. Go 1.22 introduced `range` over integers: `for i := range n` iterates from 0 to n-1. It is shorter, clearer, and consistent with `range` over slices and maps.

## Step 1 -- Basic Range Over Integer

Create a project and write a program that uses integer range:

```bash
mkdir -p ~/go-exercises/range-int
cd ~/go-exercises/range-int
go mod init range-int
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	fmt.Println("Counting to 5:")
	for i := range 5 {
		fmt.Println(i)
	}
}
```

`range 5` yields values 0, 1, 2, 3, 4 -- five iterations, zero-indexed.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Counting to 5:
0
1
2
3
4
```

## Step 2 -- Ignoring the Index

If you do not need the index, use the blank identifier:

```go
func main() {
	fmt.Println("Three stars:")
	for range 3 {
		fmt.Print("* ")
	}
	fmt.Println()
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Three stars:
* * *
```

## Step 3 -- Practical Use Cases

Replace common C-style loops with range:

```go
package main

import (
	"fmt"
	"strings"
)

func repeat(s string, n int) string {
	var b strings.Builder
	for range n {
		b.WriteString(s)
	}
	return b.String()
}

func main() {
	// Repeat a string
	fmt.Println(repeat("Go! ", 3))

	// Generate a sequence
	squares := make([]int, 0, 10)
	for i := range 10 {
		squares = append(squares, i*i)
	}
	fmt.Println("Squares:", squares)

	// Multiplication table row
	n := 7
	fmt.Printf("\n%d times table:\n", n)
	for i := range 13 {
		fmt.Printf("  %2d x %d = %2d\n", i, n, i*n)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Go! Go! Go!
Squares: [0 1 4 9 16 25 36 49 64 81]

7 times table:
   0 x 7 =  0
   1 x 7 =  7
   2 x 7 = 14
  ...
  12 x 7 = 84
```

## Step 4 -- Range Zero and Negative

Understand edge cases:

```go
package main

import "fmt"

func main() {
	fmt.Print("range 0: ")
	for range 0 {
		fmt.Print("never runs")
	}
	fmt.Println("(no iterations)")

	// range over a variable
	n := 3
	fmt.Print("range n=3: ")
	for i := range n {
		fmt.Printf("%d ", i)
	}
	fmt.Println()
}
```

`range 0` produces zero iterations. Negative integers cause a compile error when used as a constant, or zero iterations when stored in an `int` variable with a negative value (Go 1.22 treats it as zero iterations).

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
range 0: (no iterations)
range n=3: 0 1 2
```

## Common Mistakes

### Expecting 1-Based Indexing

**Wrong assumption:**

```go
for i := range 5 {
	fmt.Println(i) // prints 0-4, not 1-5
}
```

**Fix:** If you need 1-based: `for i := range 5 { fmt.Println(i + 1) }`.

### Using range with Non-Integer Types

**Wrong:**

```go
for i := range 3.14 { // compile error
```

**Fix:** `range` over numbers only works with integer types.

## Verify What You Learned

Run the final program and confirm:

- `range N` iterates from 0 to N-1
- `range 0` produces no iterations
- The index variable is optional

```bash
go run main.go
```

## What's Next

Continue to [02 - Loopvar Semantic Change](../02-loopvar-semantic-change/02-loopvar-semantic-change.md) to understand the loop variable capture fix in Go 1.22.

## Summary

- `for i := range N` iterates from 0 to N-1 (Go 1.22+)
- `for range N` iterates N times without using the index
- Replaces the verbose `for i := 0; i < N; i++` pattern
- `range 0` and negative runtime values produce zero iterations
- Only integer types are supported (not float or string)

## Reference

- [Go 1.22 release notes: range over int](https://go.dev/doc/go1.22#language)
- [Range over integer specification](https://go.dev/ref/spec#For_range)
- [Proposal: range over integers](https://github.com/golang/go/issues/61405)
