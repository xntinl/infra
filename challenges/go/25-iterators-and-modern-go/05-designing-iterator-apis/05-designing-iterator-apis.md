# 5. Designing Iterator APIs

<!--
difficulty: advanced
concepts: [iterator-api-design, method-naming, iter-seq, lazy-evaluation, error-handling-iterators]
tools: [go]
estimated_time: 35m
bloom_level: create
prerequisites: [range-over-func-push-iterators, range-over-func-pull-iterators, interfaces]
-->

## Prerequisites

- Go 1.23+ installed
- Completed exercises 03 and 04 in this section

## Learning Objectives

After completing this exercise, you will be able to:

- **Design** iterator methods that follow Go standard library conventions
- **Handle** errors in iterator sequences
- **Choose** between returning `iter.Seq`, channels, and slices for collection APIs

## Why Designing Iterator APIs

Now that Go has iterator support, library authors face design decisions: should `Users()` return `[]User`, `chan User`, or `iter.Seq[User]`? Each has trade-offs. Iterators are lazy (no upfront allocation), composable, and work with `for range`. But they cannot be re-iterated without re-calling the function, and error handling needs careful thought. Following conventions from the standard library ensures your APIs feel idiomatic.

## The Problem

Design the API for a collection library that exposes its data through iterators. Handle common challenges: error propagation, re-iterability, and naming conventions.

### Requirements

1. Follow standard library naming conventions (`All`, `Values`, `Keys`, `Backward`)
2. Design an iterator that can produce errors (e.g., reading from a file)
3. Build a collection type with multiple iterator methods
4. Demonstrate when to return `iter.Seq` vs `[]T` vs callback
5. Handle the re-iteration pattern (calling the iterator function again)

### Hints

<details>
<summary>Hint 1: Standard library naming conventions</summary>

The standard library uses these method names:
- `All()` -- iterate all elements (slices.All, maps.All)
- `Values()` -- iterate values only (maps.Values)
- `Keys()` -- iterate keys only (maps.Keys)
- `Backward()` -- iterate in reverse (slices.Backward)
- `Sorted()` / `SortedFunc()` -- iterate in sorted order

Follow these conventions in your own types.
</details>

<details>
<summary>Hint 2: Error handling in iterators</summary>

Option A: Use `Seq2[V, error]` where the second value is an error:

```go
func ReadLines(path string) iter.Seq2[string, error] {
    return func(yield func(string, error) bool) {
        f, err := os.Open(path)
        if err != nil {
            yield("", err)
            return
        }
        defer f.Close()
        scanner := bufio.NewScanner(f)
        for scanner.Scan() {
            if !yield(scanner.Text(), nil) {
                return
            }
        }
        if err := scanner.Err(); err != nil {
            yield("", err)
        }
    }
}
```

Option B: Store the error and check after iteration via a method.
</details>

<details>
<summary>Hint 3: Re-iteration</summary>

Each call to the iterator function starts fresh:

```go
seq := collection.All() // returns iter.Seq[T]

// First iteration
for v := range seq { ... }

// Second iteration -- works, starts from the beginning
for v := range seq { ... }
```

This works because `seq` is a function. Calling it again runs the function body from the start.
</details>

## Verification

Your program should demonstrate:

```
--- Collection API ---
All: Alice Bob Charlie Dave
Values: Alice Bob Charlie Dave
Backward: Dave Charlie Bob Alice

--- Error Iterator ---
Line 1: package main
Line 2: import "fmt"
...
Error reading nonexistent.txt: open nonexistent.txt: no such file or directory

--- Re-iteration ---
First pass: 1 2 3 4 5
Second pass: 1 2 3 4 5 (same iterator, fresh start)
```

```bash
go run main.go
```

## What's Next

Continue to [06 - Composing Iterators](../06-composing-iterators/06-composing-iterators.md) to build filter, map, take, and other iterator combinators.

## Summary

- Follow standard library naming: `All`, `Values`, `Keys`, `Backward`, `Sorted`
- Return `iter.Seq[V]` for single values, `iter.Seq2[K, V]` for key-value or value-error pairs
- Error handling: use `Seq2[V, error]` or store errors for post-iteration checking
- Iterators are re-iterable -- each call to the function starts a fresh iteration
- Prefer `iter.Seq` over channels (no goroutine needed) and over slices (lazy, no upfront allocation)
- Return `[]T` when the caller needs random access, length, or multiple passes with mutation

## Reference

- [iter package](https://pkg.go.dev/iter)
- [slices package iterators](https://pkg.go.dev/slices)
- [maps package iterators](https://pkg.go.dev/maps)
