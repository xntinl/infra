# 2. Slices: Creation, Append, and Capacity

<!--
difficulty: basic
concepts: [slices, make, append, len, cap, slice-literal, nil-slice, dynamic-growth]
tools: [go]
estimated_time: 20m
bloom_level: understand
prerequisites: [arrays-fixed-size-value-semantics, variables-and-types, control-flow]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercise 01 (arrays)
- Understanding of arrays and value semantics
- A terminal and text editor

## Learning Objectives

After completing this exercise, you will be able to:

- **Describe** how slices differ from arrays (dynamic length, reference semantics)
- **Use** `make`, slice literals, and `append` to create and grow slices
- **Explain** the relationship between length, capacity, and underlying array

## Why Slices

Slices are the workhorse collection type in Go. While arrays are fixed-size and copied on assignment, slices are dynamically sized views into underlying arrays. Almost every Go function that operates on a sequence of values uses slices rather than arrays. Understanding how `len`, `cap`, and `append` work together is essential for writing correct and efficient Go code.

A slice header is a small struct with three fields: a pointer to the underlying array, a length, and a capacity. When you pass a slice to a function, only this header is copied -- the underlying data is shared. This makes slices cheap to pass around while still allowing mutation.

## Step 1 -- Creating Slices

```bash
mkdir -p ~/go-exercises/slices-basics
cd ~/go-exercises/slices-basics
go mod init slices-basics
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	// Slice literal (most common)
	fruits := []string{"apple", "banana", "cherry"}
	fmt.Println("Literal:", fruits)
	fmt.Printf("  len=%d cap=%d\n", len(fruits), cap(fruits))

	// make(type, length, capacity)
	scores := make([]int, 3, 10)
	fmt.Println("Make:", scores)
	fmt.Printf("  len=%d cap=%d\n", len(scores), cap(scores))

	// make with length only (capacity == length)
	zeros := make([]float64, 5)
	fmt.Println("Zeros:", zeros)
	fmt.Printf("  len=%d cap=%d\n", len(zeros), cap(zeros))

	// Nil slice -- has no underlying array
	var names []string
	fmt.Println("Nil slice:", names, names == nil)
	fmt.Printf("  len=%d cap=%d\n", len(names), cap(names))

	// Slice from an array
	arr := [5]int{10, 20, 30, 40, 50}
	slc := arr[1:4]
	fmt.Println("From array:", slc)
	fmt.Printf("  len=%d cap=%d\n", len(slc), cap(slc))
}
```

The `make` function allocates the underlying array and returns a slice pointing to it. The capacity argument is optional -- if omitted, capacity equals length.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Literal: [apple banana cherry]
  len=3 cap=3
Make: [0 0 0]
  len=3 cap=10
Zeros: [0 0 0 0 0]
  len=5 cap=5
Nil slice: [] true
  len=0 cap=0
From array: [20 30 40]
  len=3 cap=4
```

## Step 2 -- Append and Growth

```go
package main

import "fmt"

func main() {
	var s []int
	fmt.Printf("Start:  len=%d cap=%d %v\n", len(s), cap(s), s)

	for i := 1; i <= 10; i++ {
		s = append(s, i)
		fmt.Printf("After append(%d): len=%d cap=%d\n", i, len(s), cap(s))
	}

	fmt.Println("Final:", s)
}
```

When `append` needs more space than the current capacity, it allocates a new, larger underlying array and copies the existing elements. The growth factor is roughly 2x for small slices and less for larger ones.

### Intermediate Verification

```bash
go run main.go
```

Expected (capacity growth pattern):

```
Start:  len=0 cap=0 []
After append(1): len=1 cap=1
After append(2): len=2 cap=2
After append(3): len=3 cap=4
After append(4): len=4 cap=4
After append(5): len=5 cap=8
After append(6): len=6 cap=8
After append(7): len=7 cap=8
After append(8): len=8 cap=8
After append(9): len=9 cap=16
After append(10): len=10 cap=16
Final: [1 2 3 4 5 6 7 8 9 10]
```

## Step 3 -- Append Multiple Elements and Slices

```go
package main

import "fmt"

func main() {
	s := []int{1, 2, 3}

	// Append multiple elements
	s = append(s, 4, 5, 6)
	fmt.Println("Multi-append:", s)

	// Append one slice to another using ...
	extra := []int{7, 8, 9}
	s = append(s, extra...)
	fmt.Println("Slice append:", s)
}
```

The `...` operator unpacks a slice into individual arguments for `append`.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Multi-append: [1 2 3 4 5 6]
Slice append: [1 2 3 4 5 6 7 8 9]
```

## Step 4 -- Pre-Allocating with make for Performance

```go
package main

import (
	"fmt"
	"time"
)

func withoutPrealloc(n int) []int {
	var s []int
	for i := 0; i < n; i++ {
		s = append(s, i)
	}
	return s
}

func withPrealloc(n int) []int {
	s := make([]int, 0, n)
	for i := 0; i < n; i++ {
		s = append(s, i)
	}
	return s
}

func main() {
	n := 10_000_000

	start := time.Now()
	_ = withoutPrealloc(n)
	fmt.Printf("Without prealloc: %v\n", time.Since(start))

	start = time.Now()
	_ = withPrealloc(n)
	fmt.Printf("With prealloc:    %v\n", time.Since(start))
}
```

When you know the final size, `make([]T, 0, n)` avoids repeated allocations. This is one of the most impactful performance optimizations in Go.

### Intermediate Verification

```bash
go run main.go
```

The pre-allocated version should be noticeably faster (exact times depend on hardware).

## Step 5 -- Reference Semantics: Slices Share Underlying Data

```go
package main

import "fmt"

func addOne(s []int) {
	for i := range s {
		s[i]++
	}
}

func main() {
	original := []int{10, 20, 30}
	addOne(original)

	fmt.Println("After addOne:", original) // [11 21 31]

	// Both slices see the same data
	a := []int{1, 2, 3, 4, 5}
	b := a[1:4]
	b[0] = 999

	fmt.Println("a:", a) // [1 999 3 4 5]
	fmt.Println("b:", b) // [999 3 4]
}
```

Unlike arrays, modifying a slice in a function affects the original data because the underlying array is shared.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
After addOne: [11 21 31]
a: [1 999 3 4 5]
b: [999 3 4]
```

## Common Mistakes

### Forgetting to Capture the Return Value of append

**Wrong:**

```go
s := []int{1, 2, 3}
append(s, 4) // return value discarded
```

**What happens:** `append` may return a new slice header pointing to a new underlying array. Discarding the return value means losing the appended data.

**Fix:** Always reassign: `s = append(s, 4)`.

### Using make with Non-Zero Length When You Plan to Append

**Wrong:**

```go
s := make([]int, 5)
s = append(s, 1, 2, 3)
fmt.Println(s) // [0 0 0 0 0 1 2 3]
```

**What happens:** `make([]int, 5)` creates a slice of 5 zeros. Append adds after the existing elements.

**Fix:** Use `make([]int, 0, 5)` if you want to append into pre-allocated capacity.

### Assuming Append Always Returns the Same Backing Array

**Wrong:**

```go
a := []int{1, 2, 3}
b := a
b = append(b, 4) // may or may not share memory with a
```

**What happens:** If the underlying array has capacity, `b` still shares memory with `a`. If not, `b` points to a new array.

**Fix:** Be explicit about independence. Use `copy` or full slice expressions when you need to decouple slices.

## Verify What You Learned

1. Create a nil slice of strings, append 5 city names, and print the length and capacity after each append
2. Use `make` to pre-allocate a slice of 100 integers with capacity 100, fill it using a loop with index assignment (not append), and print the sum
3. Create two slices that share the same backing array and demonstrate that modifying one affects the other

## What's Next

Continue to [03 - Slice Expressions and Sub-Slicing](../03-slice-expressions-and-sub-slicing/03-slice-expressions-and-sub-slicing.md) to learn how to create sub-slices and understand the half-open interval syntax.

## Summary

- Slices are dynamically sized views into underlying arrays
- A slice header contains a pointer, length, and capacity
- Create slices with literals (`[]int{1, 2, 3}`), `make`, or by slicing an array
- `append` adds elements and may allocate a new backing array when capacity is exceeded
- Always reassign the result of `append`: `s = append(s, elem)`
- Pre-allocate with `make([]T, 0, n)` when the final size is known
- Slices have reference semantics: modifying a slice can affect other slices sharing the same array
- `len()` returns current length; `cap()` returns capacity of the underlying array

## Reference

- [Go Blog: Go Slices: usage and internals](https://go.dev/blog/slices-intro)
- [Go Spec: Slice types](https://go.dev/ref/spec#Slice_types)
- [Effective Go: Slices](https://go.dev/doc/effective_go#slices)
- [Go Wiki: SliceTricks](https://go.dev/wiki/SliceTricks)
