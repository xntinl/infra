# 12. Sorted Collections and Binary Search

<!--
difficulty: advanced
concepts: [sorted-slice, binary-search, insertion-sort, sort-stability, custom-ordering, sorted-set, sorted-map-alternative]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [slices-package, copy-and-full-slice-expression, slice-internals]
-->

## Prerequisites

- Go 1.21+ installed
- Completed exercises 01-11 in this section
- Proficiency with the `slices` package (`Sort`, `BinarySearch`, `Insert`)
- Understanding of algorithmic complexity (O(log n) vs O(n))

## The Problem

Go does not have a built-in sorted set or sorted map type. When you need to maintain a collection in sorted order with efficient lookup, you have two choices: sort after every insertion (expensive) or maintain sorted order during insertion using binary search (efficient). This exercise asks you to build sorted collection abstractions on top of sorted slices and binary search, providing O(log n) lookups and O(n) insertions that are cache-friendly and allocation-efficient.

Your task: implement a `SortedSet[T]` and a `SortedMap[K, V]` using sorted slices and binary search.

## Requirements

1. Implement `SortedSet[T cmp.Ordered]` with:
   - `Add(item T) bool` -- insert maintaining sorted order, return false if duplicate
   - `Remove(item T) bool` -- remove if present, return false if not found
   - `Contains(item T) bool` -- O(log n) lookup
   - `Items() []T` -- return a copy of all items in sorted order
   - `Len() int`
2. Implement `SortedMap[K cmp.Ordered, V any]` with:
   - `Set(key K, value V)` -- insert or update
   - `Get(key K) (V, bool)` -- O(log n) lookup
   - `Delete(key K) bool`
   - `Keys() []K` -- return all keys in sorted order
   - `Range(fn func(K, V) bool)` -- iterate in sorted order, stop if fn returns false
3. Both structures should use `slices.BinarySearch` for lookups
4. Both should use `slices.Insert` for maintaining sorted order
5. Write tests that verify correctness and demonstrate O(log n) lookup performance

## Hints

<details>
<summary>Hint 1: SortedSet insertion pattern</summary>

```go
func (s *SortedSet[T]) Add(item T) bool {
    idx, found := slices.BinarySearch(s.items, item)
    if found {
        return false // duplicate
    }
    s.items = slices.Insert(s.items, idx, item)
    return true
}
```

`BinarySearch` returns the insertion point even when the item is not found.
</details>

<details>
<summary>Hint 2: SortedMap key-value pair</summary>

```go
type entry[K cmp.Ordered, V any] struct {
    key   K
    value V
}

type SortedMap[K cmp.Ordered, V any] struct {
    entries []entry[K, V]
}
```

Use `slices.BinarySearchFunc` with a comparison on the key field.
</details>

<details>
<summary>Hint 3: Deletion with slices.Delete</summary>

```go
func (s *SortedSet[T]) Remove(item T) bool {
    idx, found := slices.BinarySearch(s.items, item)
    if !found {
        return false
    }
    s.items = slices.Delete(s.items, idx, idx+1)
    return true
}
```
</details>

<details>
<summary>Hint 4: Performance comparison test</summary>

```go
func BenchmarkLookup(b *testing.B) {
    set := NewSortedSet[int]()
    for i := 0; i < 100_000; i++ {
        set.Add(i)
    }
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        set.Contains(i % 100_000)
    }
}
```

Compare against a linear scan to demonstrate the O(log n) advantage.
</details>

## Verification

Your implementation should pass these tests:

1. Adding N items in random order produces a sorted `Items()` slice
2. Adding a duplicate returns `false` and does not modify the set
3. Contains returns `true` for present items and `false` for absent
4. Remove returns `true` and removes the item; subsequent Contains returns `false`
5. SortedMap `Set` followed by `Get` returns the correct value
6. SortedMap `Keys()` is always sorted regardless of insertion order
7. Range iterates in sorted key order and stops when the callback returns `false`
8. Benchmark shows O(log n) lookup for `Contains` vs O(n) for a plain slice `slices.Contains`

Check your understanding:
- When would you use a sorted slice vs a `map[K]V` for lookups?
- What is the trade-off between insertion cost (O(n) due to shifting) and lookup cost (O(log n))?
- For what data patterns is a sorted slice more cache-friendly than a hash map?

## What's Next

Continue to [13 - Implementing a Ring Buffer](../13-implementing-a-ring-buffer/13-implementing-a-ring-buffer.md) to build a fixed-size circular buffer using slices.

## Summary

- Sorted slices provide O(log n) lookups via binary search
- Insertion is O(n) due to element shifting, but cache-friendly for moderate sizes
- `slices.BinarySearch` returns both the index and whether the item was found
- The returned index doubles as the insertion point for maintaining sorted order
- `slices.Insert` and `slices.Delete` handle the shifting mechanics
- Sorted slices outperform hash maps for small-to-medium collections and range queries
- For large collections with frequent mutations, consider `btree` packages instead

## Reference

- [slices package](https://pkg.go.dev/slices)
- [cmp package](https://pkg.go.dev/cmp)
- [Go Wiki: SliceTricks](https://go.dev/wiki/SliceTricks)
- [Google btree package](https://pkg.go.dev/github.com/google/btree)
