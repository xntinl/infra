# 9. Generic Iterator Patterns

<!--
difficulty: advanced
concepts: [iterators, yield-functions, iter-package, seq, pull-iterators, push-iterators]
tools: [go]
estimated_time: 35m
bloom_level: apply
prerequisites: [type-parameters, generic-functions, closures, channels]
-->

## Prerequisites

- Go 1.23+ installed (for `iter` package and range-over-function)
- Completed [08 - Generic Tree Structures](../08-generic-tree-structures/08-generic-tree-structures.md)
- Familiarity with closures and first-class functions

## Learning Objectives

After completing this exercise, you will be able to:

- **Build** generic push iterators using `iter.Seq[V]` and `iter.Seq2[K, V]`
- **Chain** iterator operations (map, filter, take) without intermediate allocations
- **Convert** between push iterators and pull iterators using `iter.Pull`

## Why Generic Iterator Patterns

Go 1.23 introduced range-over-function and the `iter` package, giving Go a standard iterator protocol. Before this, you had two options: collect everything into a slice (wasting memory on large datasets) or use channels (expensive goroutine overhead for simple iteration).

Generic iterators let you compose lazy operations -- filter, map, take, skip -- without allocating intermediate slices. A pipeline like `Take(Filter(Map(seq, fn), pred), 10)` processes elements one at a time, stopping as soon as 10 matching elements are found. This matters when working with large datasets, database result sets, or infinite sequences.

## The Problem

Build a library of generic iterator combinators that work with Go 1.23's `iter.Seq` type. Then use them to process data lazily.

### Requirements

1. Implement `SliceIter[T any](s []T) iter.Seq[T]` -- create an iterator from a slice
2. Implement `Map[T, U any](seq iter.Seq[T], fn func(T) U) iter.Seq[U]` -- transform each element
3. Implement `Filter[T any](seq iter.Seq[T], pred func(T) bool) iter.Seq[T]` -- keep matching elements
4. Implement `Take[T any](seq iter.Seq[T], n int) iter.Seq[T]` -- yield at most n elements
5. Implement `Enumerate[T any](seq iter.Seq[T]) iter.Seq2[int, T]` -- yield index-value pairs
6. Implement `Reduce[T, U any](seq iter.Seq[T], initial U, fn func(U, T) U) U` -- fold elements
7. Implement `RangeIter(start, end int) iter.Seq[int]` -- an iterator over a numeric range
8. Demonstrate composing these into a pipeline and using `iter.Pull` for pull-based iteration

### Hints

<details>
<summary>Hint 1: Push iterator signature</summary>

An `iter.Seq[T]` is a function type: `func(yield func(T) bool)`. Your iterator calls `yield` with each element. If `yield` returns `false`, stop iteration early.

```go
func SliceIter[T any](s []T) iter.Seq[T] {
    return func(yield func(T) bool) {
        for _, v := range s {
            if !yield(v) {
                return
            }
        }
    }
}
```
</details>

<details>
<summary>Hint 2: Map combinator</summary>

```go
func Map[T, U any](seq iter.Seq[T], fn func(T) U) iter.Seq[U] {
    return func(yield func(U) bool) {
        for v := range seq {
            if !yield(fn(v)) {
                return
            }
        }
    }
}
```

Note: `for v := range seq` works because `seq` is `iter.Seq[T]`, which Go 1.23 supports in range.
</details>

<details>
<summary>Hint 3: Filter combinator</summary>

```go
func Filter[T any](seq iter.Seq[T], pred func(T) bool) iter.Seq[T] {
    return func(yield func(T) bool) {
        for v := range seq {
            if pred(v) {
                if !yield(v) {
                    return
                }
            }
        }
    }
}
```
</details>

<details>
<summary>Hint 4: Pull iterator conversion</summary>

```go
next, stop := iter.Pull(mySeq)
defer stop()
for {
    val, ok := next()
    if !ok {
        break
    }
    fmt.Println(val)
}
```

`iter.Pull` converts a push iterator into a pull iterator with `next()`/`stop()` functions.
</details>

## Verification

Your program should produce output similar to:

```
--- Basic Iteration ---
Slice: 1 2 3 4 5

--- Map + Filter Pipeline ---
Squared evens from 1-10: 4 16 36 64 100

--- Take ---
First 3 squared evens: 4 16 36

--- Enumerate ---
0: apple
1: banana
2: cherry

--- Reduce ---
Sum of 1-10: 55
Product of 1-5: 120

--- Range Iterator ---
Range 5-10: 5 6 7 8 9

--- Pull Iterator ---
Pulled: 4
Pulled: 16
Pulled: 36
(stopped early)

--- Composition ---
Pipeline result: [4 16 36 64 100]
```

```bash
go run main.go
```

## What's Next

Continue to [10 - Generic Repository Pattern](../10-generic-repository-pattern/10-generic-repository-pattern.md) to apply generics to a real-world repository abstraction.

## Summary

- `iter.Seq[T]` is Go 1.23's standard push iterator: `func(yield func(T) bool)`
- Combinators (Map, Filter, Take) wrap one iterator in another without allocating slices
- `iter.Pull` converts a push iterator into a pull iterator for manual stepping
- Early termination propagates through the chain when `yield` returns `false`
- Compose small combinators into pipelines for lazy, memory-efficient data processing

## Reference

- [iter package](https://pkg.go.dev/iter) -- Go 1.23 iterator types
- [Go wiki: Rangefunc](https://go.dev/wiki/RangefuncExperiment)
- [Go blog: Range Over Function Types](https://go.dev/blog/range-functions)
