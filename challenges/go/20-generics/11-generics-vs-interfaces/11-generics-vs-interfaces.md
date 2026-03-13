# 11. Generics vs Interfaces

<!--
difficulty: advanced
concepts: [generics-tradeoffs, interfaces, type-assertions, performance, design-decisions]
tools: [go]
estimated_time: 30m
bloom_level: evaluate
prerequisites: [type-parameters, interface-constraints-with-methods, generic-data-structures]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [10 - Generic Repository Pattern](../10-generic-repository-pattern/10-generic-repository-pattern.md)
- Solid understanding of both interfaces and generics

## Learning Objectives

After completing this exercise, you will be able to:

- **Evaluate** when generics improve code versus when interfaces are simpler
- **Compare** the runtime behavior of generic code versus interface-based code
- **Choose** the right abstraction by applying concrete decision criteria

## Why Generics vs Interfaces

Generics and interfaces both enable polymorphism, but they solve different problems. Interfaces abstract behavior -- "anything that can Read." Generics abstract types -- "this container works with any type." Using the wrong one leads to either unnecessary complexity (generics where an interface suffices) or lost type safety (interfaces where generics would catch bugs).

The Go community converges on a guideline: use interfaces for behavior abstraction and dependency injection; use generics for type-safe data structures and algorithms that operate on the type itself (sorting, collecting, mapping). Understanding the boundary helps you write idiomatic Go.

## The Problem

Implement the same functionality three ways -- with interfaces, with generics, and with both combined -- then compare the tradeoffs.

### Requirements

1. Implement a `Sorter` three ways:
   - **Interface-based**: define a `Sortable` interface, implement it for multiple types
   - **Generic**: write a `Sort[T cmp.Ordered](s []T)` function
   - **Combined**: write a generic function constrained by an interface

2. Implement a `Cache` two ways:
   - **Interface-based**: `Cache` storing `any` values with type assertions on retrieval
   - **Generic**: `Cache[T any]` storing typed values with no assertions needed

3. Write benchmarks comparing the interface-based and generic approaches

4. Document when you would choose each approach

### Hints

<details>
<summary>Hint 1: Interface-based sort</summary>

```go
type Sortable interface {
    Len() int
    Less(i, j int) bool
    Swap(i, j int)
}

func SortInterface(s Sortable) {
    // simple bubble sort for demonstration
    for i := 0; i < s.Len(); i++ {
        for j := i + 1; j < s.Len(); j++ {
            if s.Less(j, i) {
                s.Swap(i, j)
            }
        }
    }
}

type IntSlice []int
func (s IntSlice) Len() int           { return len(s) }
func (s IntSlice) Less(i, j int) bool { return s[i] < s[j] }
func (s IntSlice) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
```
</details>

<details>
<summary>Hint 2: Generic sort</summary>

```go
func SortGeneric[T cmp.Ordered](s []T) {
    for i := 0; i < len(s); i++ {
        for j := i + 1; j < len(s); j++ {
            if s[j] < s[i] {
                s[i], s[j] = s[j], s[i]
            }
        }
    }
}
```

No wrapper types, no method implementations. Works for any ordered type.
</details>

<details>
<summary>Hint 3: Interface cache vs generic cache</summary>

```go
// Interface-based
type AnyCache struct {
    mu    sync.RWMutex
    items map[string]any
}

func (c *AnyCache) Get(key string) (any, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    v, ok := c.items[key]
    return v, ok
}

// Generic
type TypedCache[T any] struct {
    mu    sync.RWMutex
    items map[string]T
}

func (c *TypedCache[T]) Get(key string) (T, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    v, ok := c.items[key]
    return v, ok
}
```

The generic version returns `T` directly -- no type assertion needed at the call site.
</details>

<details>
<summary>Hint 4: Decision criteria</summary>

Choose **interfaces** when:
- You need runtime polymorphism (different types in the same collection)
- You are defining a behavioral contract (io.Reader, http.Handler)
- The caller decides the implementation (dependency injection)

Choose **generics** when:
- You need type-safe containers or data structures
- You are writing algorithms that work on the type itself (sort, map, filter)
- You want to avoid type assertions and runtime panics
- Performance matters and you want to avoid interface boxing overhead
</details>

## Verification

Your program should produce output similar to:

```
--- Interface Sort ---
Sorted ints: [1 2 3 5 8]
Sorted strings: [alice bob charlie]

--- Generic Sort ---
Sorted ints: [1 2 3 5 8]
Sorted strings: [alice bob charlie]

--- Interface Cache ---
Got int: 42 (type assertion required)
Got string: hello (type assertion required)
Wrong type assertion: interface conversion panic avoided

--- Generic Cache ---
Got int: 42 (no assertion needed)
String cache get: hello (compile-time safe)
// cache.Set("key", 42) would not compile on TypedCache[string]

--- Decision Guide ---
io.Reader: use interface (behavioral contract)
[]T sort: use generics (type-safe algorithm)
HTTP handler: use interface (runtime dispatch)
Stack[T]: use generics (type-safe container)
Mixed-type collection: use interface (runtime polymorphism needed)
```

```bash
go run main.go
```

## What's Next

Continue to [12 - Type Constraint Composition](../12-type-constraint-composition/12-type-constraint-composition.md) to learn how to compose complex constraints from simpler ones.

## Summary

- Interfaces abstract behavior; generics abstract types
- Use interfaces for dependency injection, behavioral contracts, and runtime polymorphism
- Use generics for type-safe data structures, algorithms, and avoiding type assertions
- Generic code avoids boxing overhead and produces specialized machine code per type
- The two approaches compose well -- generic functions constrained by interfaces
- When in doubt, start with interfaces; refactor to generics when type safety or performance demands it

## Reference

- [Go blog: When to Use Generics](https://go.dev/blog/when-generics)
- [Go Proverbs: interface{} says nothing](https://go-proverbs.github.io/)
- [Ian Lance Taylor: Generics Discussion](https://github.com/golang/go/issues/43651)
