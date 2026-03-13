# 2. Generic Functions

<!--
difficulty: basic
concepts: [generic-functions, min-max, contains, constraints-ordered, cmp-package]
tools: [go]
estimated_time: 20m
bloom_level: remember
prerequisites: [type-parameters, any-constraint, slices]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [01 - Type Parameters and Constraints](../01-type-parameters-and-constraints/01-type-parameters-and-constraints.md)
- Familiarity with slices

## Learning Objectives

After completing this exercise, you will be able to:

- **Write** generic `Min` and `Max` functions using `cmp.Ordered`
- **Implement** a generic `Contains` function for slices
- **Use** the `cmp` package for ordered type constraints

## Why Generic Functions

Without generics, implementing `Min` for integers and floats requires separate functions: `MinInt`, `MinFloat64`, and so on. This leads to code duplication that grows with every new type.

Generic functions let you write `Min` once and use it with any ordered type. The `cmp.Ordered` constraint from the standard library represents all types that support `<`, `>`, `<=`, `>=` -- integers, floats, and strings.

These utility functions are the most common first use of generics and demonstrate how constraints enable operators beyond what `any` allows.

## Step 1 -- Generic Min and Max

```bash
mkdir -p ~/go-exercises/generic-funcs
cd ~/go-exercises/generic-funcs
go mod init generic-funcs
```

Create `main.go`:

```go
package main

import (
	"cmp"
	"fmt"
)

func Min[T cmp.Ordered](a, b T) T {
	if a < b {
		return a
	}
	return b
}

func Max[T cmp.Ordered](a, b T) T {
	if a > b {
		return a
	}
	return b
}

func main() {
	fmt.Println("Min(3, 7):", Min(3, 7))
	fmt.Println("Max(3, 7):", Max(3, 7))
	fmt.Println("Min(3.14, 2.71):", Min(3.14, 2.71))
	fmt.Println("Max(3.14, 2.71):", Max(3.14, 2.71))
	fmt.Println(`Min("apple", "banana"):`, Min("apple", "banana"))
	fmt.Println(`Max("apple", "banana"):`, Max("apple", "banana"))
}
```

The `cmp.Ordered` constraint allows `<` and `>` operators. Without it (using `any`), these comparisons would not compile.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Min(3, 7): 3
Max(3, 7): 7
Min(3.14, 2.71): 2.71
Max(3.14, 2.71): 3.14
Min("apple", "banana"): apple
Max("apple", "banana"): banana
```

## Step 2 -- Generic Contains

Write a function that checks if a slice contains a specific value:

```go
func Contains[T comparable](slice []T, target T) bool {
	for _, v := range slice {
		if v == target {
			return true
		}
	}
	return false
}
```

Add to `main`:

```go
nums := []int{1, 2, 3, 4, 5}
fmt.Println("Contains(nums, 3):", Contains(nums, 3))
fmt.Println("Contains(nums, 9):", Contains(nums, 9))

words := []string{"go", "rust", "python"}
fmt.Println(`Contains(words, "go"):`, Contains(words, "go"))
fmt.Println(`Contains(words, "java"):`, Contains(words, "java"))
```

Note that `Contains` uses `comparable` instead of `cmp.Ordered` because it only needs `==`, not `<` or `>`.

### Intermediate Verification

```bash
go run main.go
```

Expected (appended):

```
Contains(nums, 3): true
Contains(nums, 9): false
Contains(words, "go"): true
Contains(words, "java"): false
```

## Step 3 -- Generic Index

Write a function that returns the index of a value in a slice, or -1 if not found:

```go
func Index[T comparable](slice []T, target T) int {
	for i, v := range slice {
		if v == target {
			return i
		}
	}
	return -1
}
```

Add to `main`:

```go
fmt.Println("Index(nums, 4):", Index(nums, 4))
fmt.Println("Index(nums, 9):", Index(nums, 9))
fmt.Println(`Index(words, "rust"):`, Index(words, "rust"))
```

### Intermediate Verification

```bash
go run main.go
```

Expected (appended):

```
Index(nums, 4): 3
Index(nums, 9): -1
Index(words, "rust"): 1
```

## Step 4 -- Generic Filter

Write a function that filters a slice using a predicate:

```go
func Filter[T any](slice []T, predicate func(T) bool) []T {
	var result []T
	for _, v := range slice {
		if predicate(v) {
			result = append(result, v)
		}
	}
	return result
}
```

Add to `main`:

```go
evens := Filter(nums, func(n int) bool { return n%2 == 0 })
fmt.Println("Evens:", evens)

long := Filter(words, func(s string) bool { return len(s) > 3 })
fmt.Println("Long words:", long)
```

### Intermediate Verification

```bash
go run main.go
```

Expected (appended):

```
Evens: [2 4]
Long words: [rust python]
```

## Step 5 -- Generic Map (Transform)

Write a function that transforms each element:

```go
func Map[T any, U any](slice []T, transform func(T) U) []U {
	result := make([]U, len(slice))
	for i, v := range slice {
		result[i] = transform(v)
	}
	return result
}
```

Add to `main`:

```go
doubled := Map(nums, func(n int) int { return n * 2 })
fmt.Println("Doubled:", doubled)

lengths := Map(words, func(s string) int { return len(s) })
fmt.Println("Lengths:", lengths)
```

### Intermediate Verification

```bash
go run main.go
```

Expected (appended):

```
Doubled: [2 4 6 8 10]
Lengths: [2 4 6]
```

## Common Mistakes

### Using `any` When You Need `comparable`

**Wrong:**

```go
func Contains[T any](slice []T, target T) bool {
	if v == target { // compile error: == not defined on any
```

**What happens:** The `any` constraint does not guarantee `==`. Use `comparable`.

**Fix:** `func Contains[T comparable](...)`.

### Confusing `comparable` and `cmp.Ordered`

- `comparable` allows `==` and `!=` -- use for equality checks
- `cmp.Ordered` allows `<`, `>`, `<=`, `>=` and also `==` -- use when you need ordering

### Returning the Wrong Zero Value

**Wrong:**

```go
func First[T any](slice []T) T {
	return nil // compile error: nil is not a valid T
}
```

**Fix:** Use `var zero T; return zero` to get the zero value for any type.

## Verify What You Learned

Run the complete program:

```bash
go run main.go
```

Confirm all utility functions produce correct output.

## What's Next

Continue to [03 - Comparable and Ordered](../03-comparable-and-ordered/03-comparable-and-ordered.md) to understand the `comparable` constraint and `cmp.Ordered` in depth.

## Summary

- `cmp.Ordered` constrains to types that support comparison operators
- `comparable` constrains to types that support `==` and `!=`
- Generic utility functions like `Min`, `Max`, `Contains`, `Filter`, and `Map` eliminate type-specific duplicates
- `any` is sufficient when you only need to store or pass values, not compare them

## Reference

- [cmp package documentation](https://pkg.go.dev/cmp)
- [slices package](https://pkg.go.dev/slices) -- standard library generic slice functions
- [maps package](https://pkg.go.dev/maps) -- standard library generic map functions
