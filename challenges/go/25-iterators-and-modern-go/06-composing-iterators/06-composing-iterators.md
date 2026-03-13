# 6. Composing Iterators

<!--
difficulty: advanced
concepts: [iterator-composition, filter, map-transform, take, chain, pipeline]
tools: [go]
estimated_time: 35m
bloom_level: create
prerequisites: [range-over-func-push-iterators, designing-iterator-apis, generics]
-->

## Prerequisites

- Go 1.23+ installed
- Completed [05 - Designing Iterator APIs](../05-designing-iterator-apis/05-designing-iterator-apis.md)
- Understanding of generics

## Learning Objectives

After completing this exercise, you will be able to:

- **Build** generic iterator combinators: Filter, Map, Take, Skip, Chain
- **Compose** iterators into processing pipelines
- **Evaluate** the lazy evaluation model and its performance implications

## Why Composing Iterators

Individual iterators produce sequences. Composition transforms them: filter out unwanted elements, transform values, limit output, combine multiple sources. In functional programming, this is `map`, `filter`, `reduce`. In Go, iterator composition means writing functions that accept an `iter.Seq` and return a new `iter.Seq`, forming a pipeline where no intermediate slices are allocated.

## The Problem

Build a library of generic iterator combinators and demonstrate composing them into expressive data pipelines.

### Requirements

1. Implement `Filter[V](seq, predicate) iter.Seq[V]`
2. Implement `Map[A, B](seq, transform) iter.Seq[B]`
3. Implement `Take[V](seq, n) iter.Seq[V]`
4. Implement `Skip[V](seq, n) iter.Seq[V]`
5. Implement `Chain[V](seqs...) iter.Seq[V]`
6. Implement `Reduce[V, R](seq, initial, accumulator) R` (terminal operation)
7. Compose them into a multi-step pipeline

### Hints

<details>
<summary>Hint 1: Filter combinator</summary>

```go
func Filter[V any](seq iter.Seq[V], predicate func(V) bool) iter.Seq[V] {
    return func(yield func(V) bool) {
        for v := range seq {
            if predicate(v) {
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
<summary>Hint 2: Map combinator</summary>

```go
func Map[A, B any](seq iter.Seq[A], transform func(A) B) iter.Seq[B] {
    return func(yield func(B) bool) {
        for a := range seq {
            if !yield(transform(a)) {
                return
            }
        }
    }
}
```
</details>

<details>
<summary>Hint 3: Pipeline composition</summary>

```go
// Numbers 1 to 100, keep evens, square them, take first 5
result := Take(
    Map(
        Filter(
            Range(1, 100),
            func(n int) bool { return n%2 == 0 },
        ),
        func(n int) int { return n * n },
    ),
    5,
)

for v := range result {
    fmt.Println(v) // 4, 16, 36, 64, 100
}
```

The pipeline is lazy -- only 10 numbers (the first 5 evens) are actually generated, not all 100.
</details>

## Verification

Your program should demonstrate:

```
--- Filter ---
Even numbers: 2 4 6 8 10

--- Map ---
Squared: 1 4 9 16 25

--- Take ---
First 3: 1 2 3

--- Pipeline ---
First 5 even squares from 1-100:
  4 16 36 64 100

--- Chain ---
Combined: 1 2 3 10 20 30

--- Reduce ---
Sum of 1-10: 55
Product of 1-5: 120
```

```bash
go run main.go
```

Verify lazy evaluation by adding print statements in the source iterator to confirm only necessary elements are generated.

## What's Next

Continue to [07 - iter Package Usage](../07-iter-package-usage/07-iter-package-usage.md) to learn the standard library's `iter` package functions.

## Summary

- Iterator combinators accept and return `iter.Seq[V]`, enabling composition
- `Filter` skips elements that do not match a predicate
- `Map` transforms each element from type A to type B
- `Take`/`Skip` control how many elements pass through
- `Chain` concatenates multiple iterators into one
- `Reduce` is a terminal operation that collapses a sequence to a single value
- Pipelines are lazy -- elements flow through the chain one at a time, no intermediate allocations

## Reference

- [iter package](https://pkg.go.dev/iter)
- [Functional programming in Go](https://go.dev/blog/intro-generics)
- [Iterator pattern](https://refactoring.guru/design-patterns/iterator)
