# 12. Type Constraint Composition

<!--
difficulty: advanced
concepts: [constraint-composition, embedded-interfaces, type-sets, method-constraints, structural-constraints]
tools: [go]
estimated_time: 35m
bloom_level: apply
prerequisites: [type-parameters, union-type-constraints, interface-constraints-with-methods]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [11 - Generics vs Interfaces](../11-generics-vs-interfaces/11-generics-vs-interfaces.md)
- Solid understanding of union constraints and interface constraints

## Learning Objectives

After completing this exercise, you will be able to:

- **Compose** constraints by embedding multiple interfaces into a single constraint
- **Combine** method requirements with type element requirements in one constraint
- **Design** reusable constraint hierarchies for domain-specific libraries

## Why Type Constraint Composition

Real-world generic code often needs types that satisfy multiple requirements simultaneously. A sorted-map needs keys that are both `comparable` (for map lookups) and `cmp.Ordered` (for sorting). A serializable cache needs values that implement `encoding.BinaryMarshaler` and are `comparable` for deduplication.

Go lets you compose constraints by embedding interfaces inside other interfaces. This produces precise type sets that describe exactly what your function needs -- no more, no less. Composing small, focused constraints into larger ones keeps each piece reusable.

## The Problem

Build a set of composable constraints and generic functions that demonstrate constraint composition patterns.

### Requirements

1. Compose `Numeric` from `Integer | Float` type unions
2. Create a `Stringer` constraint combining `fmt.Stringer` with `comparable`
3. Build a `MapKey` constraint requiring both `comparable` and `fmt.Stringer`
4. Implement a `SortedUniqueMap[K MapKey, V any]` using the composed constraint
5. Create a `Serializable` constraint combining a method requirement with a type union
6. Write functions that leverage each composed constraint

### Hints

<details>
<summary>Hint 1: Composing type unions</summary>

```go
type Signed interface {
    ~int | ~int8 | ~int16 | ~int32 | ~int64
}

type Unsigned interface {
    ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

type Integer interface {
    Signed | Unsigned
}

type Float interface {
    ~float32 | ~float64
}

type Numeric interface {
    Integer | Float
}
```
</details>

<details>
<summary>Hint 2: Combining methods and comparable</summary>

```go
type MapKey interface {
    comparable
    fmt.Stringer  // requires String() string
}
```

Any type used as `K MapKey` must be usable as a map key (`comparable`) and implement `String() string`.
</details>

<details>
<summary>Hint 3: Combining methods and type elements</summary>

```go
type OrderedStringer interface {
    cmp.Ordered
    fmt.Stringer
}
```

This constraint accepts types that are both ordered (support `<`, `>`) AND implement `String()`. Note: built-in types like `int` do NOT satisfy this because they do not have a `String()` method. Only named types with methods do.
</details>

<details>
<summary>Hint 4: SortedUniqueMap skeleton</summary>

```go
type SortedUniqueMap[K MapKey, V any] struct {
    items map[K]V
    keys  []K
}

func (m *SortedUniqueMap[K, V]) Set(key K, value V) {
    if _, exists := m.items[key]; !exists {
        m.keys = append(m.keys, key)
        slices.SortFunc(m.keys, func(a, b K) int {
            return strings.Compare(a.String(), b.String())
        })
    }
    m.items[key] = value
}
```
</details>

## Verification

Your program should produce output similar to:

```
--- Numeric Constraint ---
Sum ints: 15
Sum floats: 7.50
Sum uint8s: 6
Clamp(15, 0, 10): 10
Clamp(-5, 0, 10): 0

--- MapKey Constraint ---
SortedUniqueMap keys: [dept:engineering dept:marketing dept:sales]
Get dept:engineering: 42 (found)

--- Composed Method + Type ---
PrintAll: [ID-1 ID-2 ID-3]

--- Constraint Hierarchy ---
Number operations work with all numeric types
Stats(ints): min=1, max=5, sum=15
Stats(floats): min=1.10, max=5.50, sum=16.50
```

```bash
go run main.go
```

## What's Next

Continue to [13 - Generic Middleware and Decorator](../13-generic-middleware-and-decorator/13-generic-middleware-and-decorator.md) to apply generics to middleware and decorator patterns.

## Summary

- Compose constraints by embedding interfaces inside other interfaces
- Combine `comparable` with method requirements for map-key constraints
- Combine type unions (`~int | ~float64`) with method requirements in a single constraint
- Built-in types do not have methods, so constraints with method requirements only match named types
- Build small, focused constraints and compose them into larger ones for reusability
- Use `cmp.Ordered` and `comparable` as building blocks for custom constraints

## Reference

- [Go spec: Interface types](https://go.dev/ref/spec#Interface_types)
- [Go spec: Type constraints](https://go.dev/ref/spec#Type_constraints)
- [Go blog: An Introduction to Generics](https://go.dev/blog/intro-generics)
