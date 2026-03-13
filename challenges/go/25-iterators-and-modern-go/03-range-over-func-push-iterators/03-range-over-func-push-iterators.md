# 3. Range Over Func -- Push Iterators

<!--
difficulty: intermediate
concepts: [range-over-func, push-iterator, iter-seq, yield-function, go-1-23]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [range-over-integers, closures, functions]
-->

## Prerequisites

- Go 1.23+ installed
- Understanding of closures and higher-order functions

## Learning Objectives

After completing this exercise, you will be able to:

- **Write** push iterator functions compatible with `for range`
- **Explain** the `iter.Seq[V]` and `iter.Seq2[K, V]` function signatures
- **Build** custom iterators for data structures and transformations

## Why Range Over Func (Push Iterators)

Go 1.23 introduced `range` over functions, allowing any function with the right signature to be used in a `for range` loop. A push iterator is a function that calls a `yield` callback for each element. If `yield` returns `false`, iteration stops (the caller broke out of the loop). This lets you write custom iterators for trees, generators, filtered sequences, and more -- all usable with the familiar `for range` syntax.

## The Problem

Build custom iterators for common patterns: generating sequences, filtering collections, traversing data structures, and transforming values. Use the push iterator signature so they work directly with `for range`.

## Requirements

1. Write a single-value iterator (`func(yield func(V) bool)`) for a sequence generator
2. Write a two-value iterator (`func(yield func(K, V) bool)`) for key-value pairs
3. Demonstrate early termination via `break`
4. Build an iterator over a linked list

## Step 1 -- Your First Push Iterator

```bash
mkdir -p ~/go-exercises/push-iterators
cd ~/go-exercises/push-iterators
go mod init push-iterators
```

Create `main.go`:

```go
package main

import "fmt"

// Countdown yields values from n down to 1.
// Signature: func(yield func(int) bool)
// This is iter.Seq[int] without importing the iter package.
func Countdown(n int) func(yield func(int) bool) {
	return func(yield func(int) bool) {
		for i := n; i >= 1; i-- {
			if !yield(i) {
				return // caller broke out of the loop
			}
		}
	}
}

func main() {
	fmt.Println("Countdown from 5:")
	for v := range Countdown(5) {
		fmt.Println(v)
	}
}
```

The function returns a closure matching the push iterator signature. When used with `for range`, Go calls the closure and the loop body becomes the `yield` function. Returning `false` from `yield` (via `break`) signals the iterator to stop.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Countdown from 5:
5
4
3
2
1
```

## Step 2 -- Early Termination with Break

```go
func main() {
	fmt.Println("First 3 of countdown from 10:")
	for v := range Countdown(10) {
		if v <= 7 {
			break // yield returns false, iterator stops
		}
		fmt.Println(v)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
First 3 of countdown from 10:
10
9
8
```

## Step 3 -- Two-Value Iterator (Seq2)

```go
// Enumerate yields index-value pairs from a slice.
// Signature matches iter.Seq2[int, V].
func Enumerate[V any](s []V) func(yield func(int, V) bool) {
	return func(yield func(int, V) bool) {
		for i, v := range s {
			if !yield(i, v) {
				return
			}
		}
	}
}

func main() {
	names := []string{"Alice", "Bob", "Charlie"}
	for i, name := range Enumerate(names) {
		fmt.Printf("%d: %s\n", i, name)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
0: Alice
1: Bob
2: Charlie
```

## Step 4 -- Linked List Iterator

Build an iterator over a custom data structure:

```go
type Node[T any] struct {
	Value T
	Next  *Node[T]
}

func (n *Node[T]) All() func(yield func(T) bool) {
	return func(yield func(T) bool) {
		for curr := n; curr != nil; curr = curr.Next {
			if !yield(curr.Value) {
				return
			}
		}
	}
}

func main() {
	// Build linked list: 10 -> 20 -> 30
	list := &Node[int]{Value: 10, Next: &Node[int]{Value: 20, Next: &Node[int]{Value: 30}}}

	fmt.Println("Linked list values:")
	for v := range list.All() {
		fmt.Println(v)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Linked list values:
10
20
30
```

## Step 5 -- Fibonacci Generator

An infinite sequence that yields on demand:

```go
func Fibonacci() func(yield func(int) bool) {
	return func(yield func(int) bool) {
		a, b := 0, 1
		for {
			if !yield(a) {
				return
			}
			a, b = b, a+b
		}
	}
}

func main() {
	fmt.Println("First 10 Fibonacci numbers:")
	count := 0
	for v := range Fibonacci() {
		fmt.Printf("%d ", v)
		count++
		if count >= 10 {
			break
		}
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
First 10 Fibonacci numbers:
0 1 1 2 3 5 8 13 21 34
```

## Common Mistakes

### Ignoring the Yield Return Value

**Wrong:**

```go
func BadIterator() func(yield func(int) bool) {
    return func(yield func(int) bool) {
        for i := 0; i < 1000; i++ {
            yield(i) // ignoring return value!
        }
    }
}
```

**What happens:** If the caller uses `break`, the iterator keeps running until completion.

**Fix:** Always check: `if !yield(v) { return }`.

### Modifying Shared State During Iteration

**Wrong:**

```go
for v := range mySlice.All() {
    mySlice.Remove(v) // modifying the collection during iteration
}
```

**Fix:** Collect values to remove, then remove after the loop.

## Verification

```bash
go run main.go
```

Confirm all iterators produce correct output, including early termination.

## What's Next

Continue to [04 - Range Over Func (Pull Iterators)](../04-range-over-func-pull-iterators/04-range-over-func-pull-iterators.md) to learn how to convert push iterators to pull-style iteration.

## Summary

- Push iterators have the signature `func(yield func(V) bool)` (single value) or `func(yield func(K, V) bool)` (key-value)
- They work directly with `for range` in Go 1.23+
- Always check `yield`'s return value -- `false` means the caller used `break`
- Use generics to make iterators type-safe and reusable
- Push iterators can represent infinite sequences (Fibonacci, counters)
- Data structures can expose iterators as methods (linked list, tree)

## Reference

- [Go 1.23 release notes: range over func](https://go.dev/doc/go1.23#language)
- [iter package](https://pkg.go.dev/iter)
- [Range over function types spec](https://go.dev/ref/spec#For_range)
