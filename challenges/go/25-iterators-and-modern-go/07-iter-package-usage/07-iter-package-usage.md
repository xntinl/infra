# 7. iter Package Usage

<!--
difficulty: advanced
concepts: [iter-package, iter-seq, iter-seq2, iter-pull, iter-pull2, standard-iterators]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [range-over-func-push-iterators, range-over-func-pull-iterators]
-->

## Prerequisites

- Go 1.23+ installed
- Completed exercises 03 and 04 in this section

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** the `iter` package types and functions correctly
- **Apply** `iter.Seq`, `iter.Seq2`, `iter.Pull`, and `iter.Pull2` in practice
- **Integrate** standard library iterators with custom code

## Why the iter Package

The `iter` package (Go 1.23) provides the foundational types for Go's iterator ecosystem. `iter.Seq[V]` and `iter.Seq2[K, V]` are type aliases for the iterator function signatures. `iter.Pull` and `iter.Pull2` convert push iterators to pull iterators. Understanding these types is essential for writing and consuming iterators across the Go ecosystem.

## The Problem

Build a program that exercises every function in the `iter` package. Combine standard library iterators from `slices` and `maps` with custom iterators, and convert between push and pull styles.

### Requirements

1. Use `iter.Seq[V]` as a return type for custom iterators
2. Use `iter.Seq2[K, V]` for key-value iterators
3. Convert push to pull with `iter.Pull` and demonstrate cleanup
4. Convert push to pull with `iter.Pull2` for key-value iterators
5. Combine with `slices.All`, `slices.Values`, `maps.Keys`, `maps.Values`

### Hints

<details>
<summary>Hint 1: iter.Seq as a type alias</summary>

```go
// These are equivalent:
func Primes() iter.Seq[int] { ... }
func Primes() func(yield func(int) bool) { ... }

// iter.Seq[V] is defined as:
// type Seq[V any] func(yield func(V) bool)
```

Using `iter.Seq` makes your intent clear and improves documentation.
</details>

<details>
<summary>Hint 2: Combining standard library iterators</summary>

```go
import (
    "maps"
    "slices"
)

// Iterate slice indices and values
for i, v := range slices.All(mySlice) { ... }

// Iterate slice values only
for v := range slices.Values(mySlice) { ... }

// Iterate slice in reverse
for i, v := range slices.Backward(mySlice) { ... }

// Iterate map keys
for k := range maps.Keys(myMap) { ... }

// Iterate sorted map keys
for k := range slices.Sorted(maps.Keys(myMap)) { ... }
```
</details>

<details>
<summary>Hint 3: Collecting iterator results</summary>

```go
// Collect an iterator into a slice
func Collect[V any](seq iter.Seq[V]) []V {
    var result []V
    for v := range seq {
        result = append(result, v)
    }
    return result
}

// Or use slices.Collect (Go 1.23+):
result := slices.Collect(myIterator)
```
</details>

## Verification

Your program should demonstrate:

```
--- iter.Seq ---
Primes under 30: [2 3 5 7 11 13 17 19 23 29]

--- iter.Seq2 ---
Indexed: 0=Alice 1=Bob 2=Charlie

--- iter.Pull ---
Pulled first 3 primes: 2, 3, 5

--- iter.Pull2 ---
Pulled pairs: (0, Alice) (1, Bob)

--- Standard library integration ---
Sorted map keys: [age city name]
Backward slice: Charlie Bob Alice
Collected values: [Alice Bob Charlie]
```

```bash
go run main.go
```

## What's Next

Continue to [08 - Standard Library Iterators](../08-standard-library-iterators/08-standard-library-iterators.md) to explore iterator support across the standard library.

## Summary

- `iter.Seq[V]` and `iter.Seq2[K, V]` are the canonical iterator types
- `iter.Pull` converts push `Seq[V]` to pull: returns `(next func() (V, bool), stop func())`
- `iter.Pull2` converts push `Seq2[K, V]` to pull: returns `(next func() (K, V, bool), stop func())`
- Always `defer stop()` when using Pull/Pull2 to prevent goroutine leaks
- `slices.Collect` converts an iterator back to a slice
- The `slices`, `maps`, and `strings` packages provide iterator-aware functions

## Reference

- [iter package](https://pkg.go.dev/iter)
- [iter.Seq](https://pkg.go.dev/iter#Seq)
- [iter.Pull](https://pkg.go.dev/iter#Pull)
- [slices.Collect](https://pkg.go.dev/slices#Collect)
