# 8. Standard Library Iterators

<!--
difficulty: intermediate
concepts: [slices-iterators, maps-iterators, strings-iterators, bytes-iterators, stdlib-integration]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [range-over-func-push-iterators, iter-package-usage]
-->

## Prerequisites

- Go 1.23+ installed
- Completed [07 - iter Package Usage](../07-iter-package-usage/07-iter-package-usage.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** iterator functions from `slices`, `maps`, `strings`, and other standard library packages
- **Combine** standard library iterators with custom iterators
- **Identify** which standard library packages have gained iterator support

## Why Standard Library Iterators

Go 1.23 added iterator support to core packages: `slices`, `maps`, `strings`, and `bytes`. These functions return `iter.Seq` or `iter.Seq2`, enabling lazy iteration and composition with custom iterators. Instead of `slices.Contains(s, v)` with its O(n) full scan, you can compose `slices.Values` with your own `Filter` and `Take` for early termination.

Understanding what the standard library provides avoids reinventing the wheel and ensures your custom iterators compose seamlessly with built-in ones.

## The Problem

Build a program that exercises the iterator functions from the standard library. Combine them with custom iterators to solve practical problems.

## Requirements

1. Use `slices.All`, `slices.Values`, `slices.Backward`, `slices.Sorted`, `slices.Collect`
2. Use `maps.Keys`, `maps.Values`, `maps.All`
3. Use `strings.Lines` (if available) or build an equivalent
4. Combine standard iterators with custom Filter/Map/Take
5. Demonstrate `slices.SortedFunc` for custom sort orders

## Step 1 -- Slices Package Iterators

```bash
mkdir -p ~/go-exercises/stdlib-iterators
cd ~/go-exercises/stdlib-iterators
go mod init stdlib-iterators
```

Create `main.go`:

```go
package main

import (
	"cmp"
	"fmt"
	"iter"
	"maps"
	"slices"
)

func main() {
	fruits := []string{"banana", "apple", "cherry", "date", "elderberry"}

	// slices.All: index-value pairs
	fmt.Println("--- slices.All ---")
	for i, v := range slices.All(fruits) {
		fmt.Printf("  %d: %s\n", i, v)
	}

	// slices.Values: values only
	fmt.Println("\n--- slices.Values ---")
	for v := range slices.Values(fruits) {
		fmt.Printf("  %s\n", v)
	}

	// slices.Backward: reverse iteration
	fmt.Println("\n--- slices.Backward ---")
	for _, v := range slices.Backward(fruits) {
		fmt.Printf("  %s\n", v)
	}

	// slices.Sorted: sorted iteration from a sequence
	fmt.Println("\n--- Sorted values ---")
	sorted := slices.Sorted(slices.Values(fruits))
	for _, v := range sorted {
		fmt.Printf("  %s\n", v)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: fruits printed in original order, values only, reversed, and sorted.

## Step 2 -- Maps Package Iterators

```go
func main() {
	scores := map[string]int{
		"Alice":   95,
		"Bob":     87,
		"Charlie": 92,
		"Dave":    78,
	}

	// maps.Keys: iterate keys
	fmt.Println("--- Sorted keys ---")
	for k := range slices.Sorted(maps.Keys(scores)) {
		fmt.Printf("  %s: %d\n", k, scores[k])
	}

	// maps.Values: iterate values
	fmt.Println("\n--- All values ---")
	for v := range maps.Values(scores) {
		fmt.Printf("  %d\n", v)
	}

	// maps.All: key-value pairs
	fmt.Println("\n--- All pairs ---")
	for k, v := range maps.All(scores) {
		fmt.Printf("  %s=%d\n", k, v)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Keys printed in sorted order; values and pairs in map order.

## Step 3 -- Combining Standard and Custom Iterators

```go
func Filter[V any](seq iter.Seq[V], pred func(V) bool) iter.Seq[V] {
	return func(yield func(V) bool) {
		for v := range seq {
			if pred(v) {
				if !yield(v) {
					return
				}
			}
		}
	}
}

func Map[A, B any](seq iter.Seq[A], fn func(A) B) iter.Seq[B] {
	return func(yield func(B) bool) {
		for a := range seq {
			if !yield(fn(a)) {
				return
			}
		}
	}
}

func main() {
	scores := map[string]int{
		"Alice": 95, "Bob": 87, "Charlie": 92, "Dave": 78, "Eve": 91,
	}

	// Pipeline: get names of students with score >= 90, sorted
	highScorers := slices.Sorted(
		Filter(maps.Keys(scores), func(name string) bool {
			return scores[name] >= 90
		}),
	)

	fmt.Println("High scorers (>= 90):")
	for _, name := range highScorers {
		fmt.Printf("  %s: %d\n", name, scores[name])
	}

	// Pipeline: get sorted scores above 85
	nums := []int{3, 1, 4, 1, 5, 9, 2, 6, 5, 3, 5}
	unique := slices.Sorted(slices.Values(nums))
	fmt.Printf("\nSorted: %v\n", unique)
}
```

### Intermediate Verification

```bash
go run main.go
```

## Step 4 -- SortedFunc for Custom Orders

```go
func main() {
	type Person struct {
		Name string
		Age  int
	}

	people := []Person{
		{"Alice", 30}, {"Bob", 25}, {"Charlie", 35}, {"Dave", 28},
	}

	// Sort by age using SortedFunc
	byAge := slices.SortedFunc(slices.Values(people), func(a, b Person) int {
		return cmp.Compare(a.Age, b.Age)
	})

	fmt.Println("Sorted by age:")
	for _, p := range byAge {
		fmt.Printf("  %s (age %d)\n", p.Name, p.Age)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Sorted by age:
  Bob (age 25)
  Dave (age 28)
  Alice (age 30)
  Charlie (age 35)
```

## Common Mistakes

### Assuming Sorted Returns an Iterator

**Wrong assumption:**

```go
for v := range slices.Sorted(seq) { // Sorted returns []T, not iter.Seq[T]
```

**Clarification:** `slices.Sorted` returns a `[]T` (a collected, sorted slice), not an iterator. This is intentional -- sorting requires all elements upfront.

### Iterating Maps and Expecting Order

**Wrong assumption:**

```go
for k := range maps.Keys(m) { // order is NOT guaranteed
```

**Fix:** Use `slices.Sorted(maps.Keys(m))` if you need deterministic order.

## Verification

```bash
go run main.go
```

## What's Next

You have completed the iterators and modern Go section. These patterns compose with everything you have learned: iterators over database results, config entries, API responses, and data structures all follow the same `iter.Seq` signature.

## Summary

- `slices.All`, `Values`, `Backward` provide iterator access to slices
- `maps.Keys`, `Values`, `All` provide iterator access to maps
- `slices.Sorted` and `SortedFunc` collect and sort an iterator's output
- `slices.Collect` materializes an iterator into a slice
- Standard library iterators compose with custom Filter, Map, Take combinators
- The `iter.Seq[V]` type is the universal currency for Go iteration

## Reference

- [slices package](https://pkg.go.dev/slices)
- [maps package](https://pkg.go.dev/maps)
- [iter package](https://pkg.go.dev/iter)
- [Go 1.23 release notes](https://go.dev/doc/go1.23)
