# 4. Range Over Func -- Pull Iterators

<!--
difficulty: intermediate
concepts: [pull-iterator, iter-pull, iter-pull2, push-to-pull-conversion, manual-iteration]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [range-over-func-push-iterators, goroutines-and-channels]
-->

## Prerequisites

- Go 1.23+ installed
- Completed [03 - Range Over Func (Push Iterators)](../03-range-over-func-push-iterators/03-range-over-func-push-iterators.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Convert** push iterators to pull iterators using `iter.Pull`
- **Explain** when pull iteration is preferable to push iteration
- **Use** pull iterators for parallel zip, interleaving, and manual stepping

## Why Pull Iterators

Push iterators drive the loop -- the iterator calls `yield` and the loop body executes. This is convenient for simple iteration but awkward when you need to consume two iterators in lockstep (zip), peek ahead, or integrate with code that expects manual `next()`-style iteration. `iter.Pull` converts a push iterator into a pull iterator: a `next` function you call to get the next value, and a `stop` function to clean up.

## The Problem

Build programs that use pull iterators for patterns that are difficult or impossible with push iterators alone: zipping two sequences, merging sorted iterators, and manual stepping through a sequence.

## Requirements

1. Convert a push iterator to a pull iterator using `iter.Pull`
2. Build a `Zip` function that combines two iterators element-by-element
3. Build a `Merge` function that merges two sorted iterators into one sorted sequence
4. Demonstrate manual stepping where you advance an iterator conditionally

## Step 1 -- Basic Pull Iterator

```bash
mkdir -p ~/go-exercises/pull-iterators
cd ~/go-exercises/pull-iterators
go mod init pull-iterators
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"iter"
)

func Naturals(limit int) iter.Seq[int] {
	return func(yield func(int) bool) {
		for i := 1; i <= limit; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

func main() {
	// Convert push to pull
	next, stop := iter.Pull(Naturals(10))
	defer stop()

	// Manually pull values
	for range 5 {
		v, ok := next()
		if !ok {
			break
		}
		fmt.Printf("pulled: %d\n", v)
	}
	// stop() cleans up -- remaining values are discarded
}
```

`iter.Pull` returns two functions: `next()` returns the next value and a boolean (false when exhausted), and `stop()` cleans up the underlying goroutine.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
pulled: 1
pulled: 2
pulled: 3
pulled: 4
pulled: 5
```

## Step 2 -- Zip Two Iterators

```go
func Zip[A, B any](seqA iter.Seq[A], seqB iter.Seq[B]) iter.Seq2[A, B] {
	return func(yield func(A, B) bool) {
		nextA, stopA := iter.Pull(seqA)
		defer stopA()
		nextB, stopB := iter.Pull(seqB)
		defer stopB()

		for {
			a, okA := nextA()
			b, okB := nextB()
			if !okA || !okB {
				return
			}
			if !yield(a, b) {
				return
			}
		}
	}
}

func Letters(s string) iter.Seq[rune] {
	return func(yield func(rune) bool) {
		for _, r := range s {
			if !yield(r) {
				return
			}
		}
	}
}

func main() {
	fmt.Println("Zipped:")
	for num, letter := range Zip(Naturals(5), Letters("ABCDE")) {
		fmt.Printf("  %d -> %c\n", num, letter)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Zipped:
  1 -> A
  2 -> B
  3 -> C
  4 -> D
  5 -> E
```

## Step 3 -- Merge Sorted Iterators

```go
func MergeSorted(seqA, seqB iter.Seq[int]) iter.Seq[int] {
	return func(yield func(int) bool) {
		nextA, stopA := iter.Pull(seqA)
		defer stopA()
		nextB, stopB := iter.Pull(seqB)
		defer stopB()

		a, okA := nextA()
		b, okB := nextB()

		for okA && okB {
			if a <= b {
				if !yield(a) { return }
				a, okA = nextA()
			} else {
				if !yield(b) { return }
				b, okB = nextB()
			}
		}

		for okA {
			if !yield(a) { return }
			a, okA = nextA()
		}
		for okB {
			if !yield(b) { return }
			b, okB = nextB()
		}
	}
}

func FromSlice[T any](s []T) iter.Seq[T] {
	return func(yield func(T) bool) {
		for _, v := range s {
			if !yield(v) { return }
		}
	}
}

func main() {
	a := FromSlice([]int{1, 3, 5, 7, 9})
	b := FromSlice([]int{2, 4, 6, 8, 10})

	fmt.Print("Merged: ")
	for v := range MergeSorted(a, b) {
		fmt.Printf("%d ", v)
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
Merged: 1 2 3 4 5 6 7 8 9 10
```

## Step 4 -- Pull2 for Key-Value Iterators

```go
func main() {
	m := map[string]int{"a": 1, "b": 2, "c": 3}

	// Create a Seq2 iterator
	mapIter := func(yield func(string, int) bool) {
		for k, v := range m {
			if !yield(k, v) { return }
		}
	}

	next, stop := iter.Pull2(mapIter)
	defer stop()

	// Pull key-value pairs manually
	for {
		k, v, ok := next()
		if !ok {
			break
		}
		fmt.Printf("  %s=%d\n", k, v)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

## Common Mistakes

### Forgetting to Call stop()

**Wrong:**

```go
next, stop := iter.Pull(seq)
// forgot to call stop() -- goroutine leak
```

**Fix:** Always `defer stop()` immediately after `iter.Pull`.

### Using Pull When Push Suffices

**Wrong:** Converting to pull just to use in a `for` loop -- push iterators already work with `for range`.

**Fix:** Use `iter.Pull` only when you need manual control, zipping, or interleaving.

## Verification

```bash
go run main.go
```

## What's Next

Continue to [05 - Designing Iterator APIs](../05-designing-iterator-apis/05-designing-iterator-apis.md) to learn best practices for exposing iterators from your packages.

## Summary

- `iter.Pull(seq)` converts a push iterator to a pull iterator: `next, stop`
- `next()` returns `(value, ok)` -- `ok` is false when exhausted
- `stop()` must always be called (use `defer`) to prevent goroutine leaks
- Pull iterators enable zip, merge, interleave, and manual stepping
- `iter.Pull2` works with `Seq2` iterators for key-value pairs
- Prefer push iterators for simple loops; use pull when you need two iterators in lockstep

## Reference

- [iter.Pull](https://pkg.go.dev/iter#Pull)
- [iter.Pull2](https://pkg.go.dev/iter#Pull2)
- [Go 1.23 release notes](https://go.dev/doc/go1.23)
