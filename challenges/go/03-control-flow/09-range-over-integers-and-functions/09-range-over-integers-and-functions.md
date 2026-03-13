# 9. Range Over Integers and Functions

<!--
difficulty: intermediate
concepts: [range-over-int, range-over-func, iterator-protocol, yield, go-1-22, go-1-23, push-iterator]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [02-for-loops, 05-range-over-collections, 04-functions/01-function-declaration-and-signatures]
-->

## Prerequisites

- Go 1.23+ installed (range-over-func requires Go 1.23; range-over-int requires Go 1.22)
- A terminal and text editor
- Completed [02 - For Loops](../02-for-loops/02-for-loops.md) and [05 - Range Over Collections](../05-range-over-collections/05-range-over-collections.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** `range` over integers to replace `for i := 0; i < n; i++` (Go 1.22+)
- **Explain** the iterator function protocol and the role of the yield function (Go 1.23+)
- **Write** custom iterators using `func(yield func(T) bool)` and `func(yield func(K, V) bool)`
- **Compose** iterators for filtering, mapping, and chaining sequences

## Why Range Over Integers and Functions

Go 1.22 introduced `range` over integers, letting you write `for i := range 5` instead of `for i := 0; i < 5; i++`. This is cleaner for simple counting loops.

Go 1.23 took this further with range-over-func, establishing a standard iterator protocol. Any function matching `func(yield func(V) bool)` or `func(yield func(K, V) bool)` can be used in a `for range` loop. This gives Go a composable iteration mechanism without requiring interface implementations or channel-based generators.

These features bring Go closer to languages with built-in iterator support while maintaining Go's explicit style.

## Step 1 -- Range Over Integers (Go 1.22+)

```bash
mkdir -p ~/go-exercises/range-new
cd ~/go-exercises/range-new
go mod init range-new
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	// Basic range over integer
	fmt.Println("Range over 5:")
	for i := range 5 {
		fmt.Printf("  %d\n", i)
	}

	// Equivalent to: for i := 0; i < 5; i++
	// i goes from 0 to n-1

	// Discard the index
	fmt.Println("Repeat 3 times:")
	for range 3 {
		fmt.Println("  hello")
	}

	// Build a multiplication table
	fmt.Println("Multiplication table (1-4):")
	for i := range 4 {
		row := i + 1
		for j := range 4 {
			col := j + 1
			fmt.Printf("  %2d", row*col)
		}
		fmt.Println()
	}

	// Range over zero or negative: zero iterations
	fmt.Print("Range over 0: ")
	for range 0 {
		fmt.Print("never")
	}
	fmt.Println("(no output)")
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/range-new && go run main.go
```

Expected:

```
Range over 5:
  0
  1
  2
  3
  4
Repeat 3 times:
  hello
  hello
  hello
Multiplication table (1-4):
   1  2  3  4
   2  4  6  8
   3  6  9 12
   4  8 12 16
Range over 0: (no output)
```

## Step 2 -- Single-Value Iterator Functions (Go 1.23+)

Replace `main.go`:

```go
package main

import "fmt"

// An iterator function takes a yield callback.
// It calls yield for each value. If yield returns false, the iterator stops.
// Signature: func(func(V) bool)

// Fibonacci returns an iterator over the first n Fibonacci numbers
func Fibonacci(n int) func(func(int) bool) {
	return func(yield func(int) bool) {
		a, b := 0, 1
		for range n {
			if !yield(a) {
				return // caller broke out of the loop
			}
			a, b = b, a+b
		}
	}
}

// Countdown returns an iterator from n down to 1
func Countdown(n int) func(func(int) bool) {
	return func(yield func(int) bool) {
		for i := n; i >= 1; i-- {
			if !yield(i) {
				return
			}
		}
	}
}

func main() {
	// Use iterator with for-range
	fmt.Println("First 10 Fibonacci numbers:")
	for v := range Fibonacci(10) {
		fmt.Printf("  %d\n", v)
	}

	fmt.Println("Countdown from 5:")
	for v := range Countdown(5) {
		fmt.Printf("  %d\n", v)
	}

	// Early break works -- yield returns false
	fmt.Println("Fibonacci until > 20:")
	for v := range Fibonacci(20) {
		if v > 20 {
			break
		}
		fmt.Printf("  %d\n", v)
	}
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/range-new && go run main.go
```

Expected:

```
First 10 Fibonacci numbers:
  0
  1
  1
  2
  3
  5
  8
  13
  21
  34
Countdown from 5:
  5
  4
  3
  2
  1
Fibonacci until > 20:
  0
  1
  1
  2
  3
  5
  8
  13
```

The key contract: the iterator calls `yield(value)` for each element. If the caller uses `break`, `yield` returns `false` and the iterator must stop.

## Step 3 -- Two-Value Iterator Functions (Key-Value)

Replace `main.go`:

```go
package main

import "fmt"

// Two-value iterators use func(func(K, V) bool)
// This matches the pattern of map iteration: for k, v := range ...

// Enumerate wraps a slice iterator to yield index-value pairs
func Enumerate[T any](s []T) func(func(int, T) bool) {
	return func(yield func(int, T) bool) {
		for i, v := range s {
			if !yield(i, v) {
				return
			}
		}
	}
}

// Pairs iterates over consecutive pairs in a slice
func Pairs[T any](s []T) func(func(T, T) bool) {
	return func(yield func(T, T) bool) {
		for i := 0; i+1 < len(s); i++ {
			if !yield(s[i], s[i+1]) {
				return
			}
		}
	}
}

// Zip combines two slices into key-value pairs
func Zip[K, V any](keys []K, values []V) func(func(K, V) bool) {
	return func(yield func(K, V) bool) {
		n := len(keys)
		if len(values) < n {
			n = len(values)
		}
		for i := range n {
			if !yield(keys[i], values[i]) {
				return
			}
		}
	}
}

func main() {
	fruits := []string{"apple", "banana", "cherry"}

	fmt.Println("Enumerate:")
	for i, v := range Enumerate(fruits) {
		fmt.Printf("  [%d] %s\n", i, v)
	}

	numbers := []int{1, 4, 2, 8, 5, 7}
	fmt.Println("Consecutive pairs:")
	for a, b := range Pairs(numbers) {
		fmt.Printf("  (%d, %d) diff=%d\n", a, b, b-a)
	}

	names := []string{"Alice", "Bob", "Charlie"}
	scores := []int{95, 87, 92}
	fmt.Println("Zip names and scores:")
	for name, score := range Zip(names, scores) {
		fmt.Printf("  %s: %d\n", name, score)
	}
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/range-new && go run main.go
```

Expected:

```
Enumerate:
  [0] apple
  [1] banana
  [2] cherry
Consecutive pairs:
  (1, 4) diff=3
  (4, 2) diff=-2
  (2, 8) diff=6
  (8, 5) diff=-3
  (5, 7) diff=2
Zip names and scores:
  Alice: 95
  Bob: 87
  Charlie: 92
```

## Step 4 -- Composing Iterators

Replace `main.go`:

```go
package main

import "fmt"

// Filter returns an iterator that yields only values satisfying the predicate
func Filter[V any](seq func(func(V) bool), predicate func(V) bool) func(func(V) bool) {
	return func(yield func(V) bool) {
		seq(func(v V) bool {
			if predicate(v) {
				return yield(v)
			}
			return true // skip, but continue iterating
		})
	}
}

// Map transforms each value in an iterator
func Map[V, R any](seq func(func(V) bool), transform func(V) R) func(func(R) bool) {
	return func(yield func(R) bool) {
		seq(func(v V) bool {
			return yield(transform(v))
		})
	}
}

// Take returns an iterator that yields at most n values
func Take[V any](seq func(func(V) bool), n int) func(func(V) bool) {
	return func(yield func(V) bool) {
		count := 0
		seq(func(v V) bool {
			if count >= n {
				return false
			}
			count++
			return yield(v)
		})
	}
}

// Range generates integers from start to end (exclusive)
func Range(start, end int) func(func(int) bool) {
	return func(yield func(int) bool) {
		for i := start; i < end; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

func main() {
	// Filter even numbers
	fmt.Println("Even numbers from 0-9:")
	evens := Filter(Range(0, 10), func(n int) bool { return n%2 == 0 })
	for v := range evens {
		fmt.Printf("  %d\n", v)
	}

	// Map: square numbers
	fmt.Println("Squares of 1-5:")
	squares := Map(Range(1, 6), func(n int) int { return n * n })
	for v := range squares {
		fmt.Printf("  %d\n", v)
	}

	// Compose: first 3 even numbers from 0-20, doubled
	fmt.Println("First 3 even numbers doubled:")
	pipeline := Map(
		Take(
			Filter(Range(0, 20), func(n int) bool { return n%2 == 0 }),
			3,
		),
		func(n int) int { return n * 2 },
	)
	for v := range pipeline {
		fmt.Printf("  %d\n", v)
	}
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/range-new && go run main.go
```

Expected:

```
Even numbers from 0-9:
  0
  2
  4
  6
  8
Squares of 1-5:
  1
  4
  9
  16
  25
First 3 even numbers doubled:
  0
  4
  8
```

Iterator composition lets you build lazy pipelines. Values flow through the chain one at a time without allocating intermediate slices.

## Common Mistakes

### Forgetting to Check the Return Value of Yield

**Wrong:**

```go
func Bad(yield func(int) bool) {
    for i := 0; i < 100; i++ {
        yield(i) // ignores return value
    }
}
```

**What happens:** If the caller uses `break`, `yield` returns `false` but the iterator keeps running.

**Fix:** Always check: `if !yield(i) { return }`.

### Using Range Over Negative Integers

**Wrong assumption:** `for i := range -5` iterates 5 times.

**What happens:** `range` over a non-positive integer produces zero iterations. There is no error; the loop body simply never executes.

### Confusing Iterator Signatures

**Wrong:** Using `func(func(V))` instead of `func(func(V) bool)`.

**What happens:** Without the `bool` return, the yield function cannot signal that the caller broke out of the loop.

**Fix:** The yield function must return `bool`. Use `func(func(V) bool)` for single-value and `func(func(K, V) bool)` for two-value iterators.

## Verify What You Learned

```bash
cd ~/go-exercises/range-new && go run main.go
```

Write a `Reverse` iterator that takes a slice and yields elements from last to first. Compose it with `Filter` to get odd numbers from a reversed list.

## What's Next

Continue to [10 - Control Flow Debugging Challenge](../10-control-flow-debugging-challenge/10-control-flow-debugging-challenge.md) to apply everything you have learned in this section to diagnose and fix control flow bugs.

## Summary

- `for i := range n` iterates from 0 to n-1 (Go 1.22+), replacing `for i := 0; i < n; i++`
- `for range n` without a variable is valid for simple repetition
- Range-over-func iterators use `func(func(V) bool)` for single values and `func(func(K, V) bool)` for pairs (Go 1.23+)
- The yield function returns `false` when the caller breaks out of the loop
- Always check yield's return value and stop iterating when it returns `false`
- Iterators compose naturally: Filter, Map, Take can be chained into lazy pipelines
- The `iter` package in the standard library defines `iter.Seq[V]` and `iter.Seq2[K, V]` as type aliases for these signatures

## Reference

- [Go 1.22 Release Notes: Range Over Integer](https://go.dev/doc/go1.22#language)
- [Go 1.23 Release Notes: Range Over Function Types](https://go.dev/doc/go1.23#language)
- [Go Wiki: Rangefunc](https://go.dev/wiki/RangefuncExperiment)
- [iter package](https://pkg.go.dev/iter)
- [Go Blog: Range Over Function Types](https://go.dev/blog/range-functions)
