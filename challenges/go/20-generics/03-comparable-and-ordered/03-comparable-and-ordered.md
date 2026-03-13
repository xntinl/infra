# 3. Comparable and Ordered

<!--
difficulty: intermediate
concepts: [comparable, cmp-ordered, constraint-hierarchy, type-sets]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [type-parameters, generic-functions]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [02 - Generic Functions](../02-generic-functions/02-generic-functions.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Distinguish** between `any`, `comparable`, and `cmp.Ordered` constraints
- **Apply** the correct constraint based on which operators are needed
- **Use** `cmp.Compare` and `cmp.Less` from the standard library

## Why Comparable and Ordered

Go generics use constraints to define which operations are legal on a type parameter. Choosing the wrong constraint either restricts your function unnecessarily or allows types that will not work.

The three most common constraints form a hierarchy: `any` (all types) > `comparable` (types with `==`) > `cmp.Ordered` (types with `<`). Understanding when to use each is essential for writing correct generic code.

## Step 1 -- Understand the Constraint Hierarchy

```bash
mkdir -p ~/go-exercises/comparable-ordered
cd ~/go-exercises/comparable-ordered
go mod init comparable-ordered
```

Create `main.go`:

```go
package main

import (
	"cmp"
	"fmt"
)

// any: can only store/pass, no operators
func Identity[T any](v T) T {
	return v
}

// comparable: can use == and !=
func Equal[T comparable](a, b T) bool {
	return a == b
}

// cmp.Ordered: can use <, >, <=, >=, == and !=
func Clamp[T cmp.Ordered](value, low, high T) T {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func main() {
	// Identity works with anything
	fmt.Println(Identity(42))
	fmt.Println(Identity([]int{1, 2, 3})) // slices are not comparable

	// Equal works with comparable types
	fmt.Println("Equal(1, 1):", Equal(1, 1))
	fmt.Println("Equal(1, 2):", Equal(1, 2))
	fmt.Println(`Equal("a", "a"):`, Equal("a", "a"))
	// Equal([]int{1}, []int{1}) would NOT compile -- slices are not comparable

	// Clamp works with ordered types
	fmt.Println("Clamp(5, 1, 10):", Clamp(5, 1, 10))
	fmt.Println("Clamp(-3, 0, 100):", Clamp(-3, 0, 100))
	fmt.Println("Clamp(200, 0, 100):", Clamp(200, 0, 100))
	fmt.Println(`Clamp("m", "a", "z"):`, Clamp("m", "a", "z"))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
42
[1 2 3]
Equal(1, 1): true
Equal(1, 2): false
Equal("a", "a"): true
Clamp(5, 1, 10): 5
Clamp(-3, 0, 100): 0
Clamp(200, 0, 100): 100
Clamp("m", "a", "z"): m
```

## Step 2 -- Use cmp.Compare and cmp.Less

The `cmp` package provides utility functions that work with `Ordered` types:

```go
func demonstrateCmp() {
	// cmp.Compare returns -1, 0, or +1
	fmt.Println("cmp.Compare(1, 2):", cmp.Compare(1, 2))   // -1
	fmt.Println("cmp.Compare(2, 2):", cmp.Compare(2, 2))   // 0
	fmt.Println("cmp.Compare(3, 2):", cmp.Compare(3, 2))   // +1

	// cmp.Less is shorthand for <
	fmt.Println("cmp.Less(1, 2):", cmp.Less(1, 2))         // true
	fmt.Println("cmp.Less(2, 1):", cmp.Less(2, 1))         // false

	// cmp.Or returns the first non-zero value
	fmt.Println("cmp.Or(0, 0, 3):", cmp.Or(0, 0, 3))       // 3
	fmt.Println(`cmp.Or("", "", "c"):`, cmp.Or("", "", "c")) // c
}
```

Add `demonstrateCmp()` to `main`.

### Intermediate Verification

```bash
go run main.go
```

Expected (appended):

```
cmp.Compare(1, 2): -1
cmp.Compare(2, 2): 0
cmp.Compare(3, 2): +1
cmp.Less(1, 2): true
cmp.Less(2, 1): false
cmp.Or(0, 0, 3): 3
cmp.Or("", "", "c"): c
```

## Step 3 -- Build a Generic MinMax Function

Combine `cmp.Compare` with a practical function that returns both min and max from a slice:

```go
func MinMax[T cmp.Ordered](items []T) (T, T) {
	if len(items) == 0 {
		var zero T
		return zero, zero
	}
	min, max := items[0], items[0]
	for _, v := range items[1:] {
		if cmp.Less(v, min) {
			min = v
		}
		if cmp.Compare(v, max) > 0 {
			max = v
		}
	}
	return min, max
}
```

Add to `main`:

```go
ints := []int{5, 2, 8, 1, 9, 3}
min, max := MinMax(ints)
fmt.Printf("MinMax(%v): min=%d, max=%d\n", ints, min, max)

strs := []string{"banana", "apple", "cherry"}
smin, smax := MinMax(strs)
fmt.Printf("MinMax(%v): min=%s, max=%s\n", strs, smin, smax)
```

### Intermediate Verification

```bash
go run main.go
```

Expected (appended):

```
MinMax([5 2 8 1 9 3]): min=1, max=9
MinMax([banana apple cherry]): min=apple, max=cherry
```

## Step 4 -- Structs with comparable

Structs whose fields are all comparable are themselves `comparable`:

```go
type Point struct {
	X, Y int
}

func demonstrateStructComparable() {
	p1 := Point{1, 2}
	p2 := Point{1, 2}
	p3 := Point{3, 4}

	fmt.Println("Equal(p1, p2):", Equal(p1, p2))
	fmt.Println("Equal(p1, p3):", Equal(p1, p3))

	// But Point is NOT cmp.Ordered -- you cannot use < on structs
	// Clamp(p1, p2, p3) would NOT compile
}
```

Add `demonstrateStructComparable()` to `main`.

### Intermediate Verification

```bash
go run main.go
```

Expected (appended):

```
Equal(p1, p2): true
Equal(p1, p3): false
```

## Common Mistakes

### Using `comparable` When You Need `cmp.Ordered`

**Wrong:**

```go
func Sort[T comparable](items []T) { // cannot use < with comparable
```

**Fix:** Use `cmp.Ordered` when sorting or comparing magnitude.

### Trying to Use `cmp.Ordered` with Structs

**Wrong:**

```go
type User struct{ Name string }
Clamp(User{"A"}, User{"B"}, User{"C"}) // compile error
```

**What happens:** Structs do not satisfy `cmp.Ordered` even if their fields do.

**Fix:** Extract the field you want to compare: `Clamp(u.Name, "B", "C")`.

## Verify What You Learned

```bash
go run main.go
```

Confirm all outputs match expectations.

## What's Next

Continue to [04 - Generic Data Structures](../04-generic-data-structures/04-generic-data-structures.md) to build type-safe `Stack[T]` and `Queue[T]`.

## Summary

- `any` allows all types but no operators
- `comparable` adds `==` and `!=` -- use for lookups and deduplication
- `cmp.Ordered` adds `<`, `>`, `<=`, `>=` -- use for sorting and clamping
- `cmp.Compare` returns -1, 0, +1 for three-way comparison
- Structs are `comparable` if all fields are, but never `cmp.Ordered`

## Reference

- [cmp package](https://pkg.go.dev/cmp)
- [Go spec: Comparison operators](https://go.dev/ref/spec#Comparison_operators)
- [Go spec: Type constraints](https://go.dev/ref/spec#Type_constraints)
