# 9. Slices Package

<!--
difficulty: intermediate
concepts: [slices-package, sort, binary-search, contains, compact, clip, grow, generic-functions]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [slices-creation-append-capacity, copy-and-full-slice-expression, slice-internals]
note: Go 1.21+
-->

## Prerequisites

- Go 1.21+ installed (the `slices` package was added in Go 1.21)
- Completed exercises 01-08 in this section
- Understanding of slices, append, and copy
- Familiarity with generics syntax (basic)

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** standard library `slices` functions for sorting, searching, and transforming slices
- **Select** the appropriate function for common slice operations instead of writing manual loops
- **Use** `slices.SortFunc` and `slices.BinarySearchFunc` with custom comparison functions

## Why the Slices Package

Before Go 1.21, common slice operations like sorting, searching, and removing duplicates required either manual implementations or the older `sort` package with its interface-based design. The `slices` package provides generic, type-safe functions that work with any slice type. These functions are optimized, well-tested, and eliminate entire categories of off-by-one bugs that come from hand-rolled implementations.

## Step 1 -- Sorting Slices

```bash
mkdir -p ~/go-exercises/slices-pkg
cd ~/go-exercises/slices-pkg
go mod init slices-pkg
```

Create `main.go`:

```go
package main

import (
	"cmp"
	"fmt"
	"slices"
)

func main() {
	// Sort integers
	numbers := []int{5, 3, 8, 1, 9, 2, 7}
	slices.Sort(numbers)
	fmt.Println("Sorted:", numbers)

	// Sort strings
	names := []string{"Charlie", "Alice", "Bob", "Diana"}
	slices.Sort(names)
	fmt.Println("Names:", names)

	// Check if sorted
	fmt.Println("Is sorted:", slices.IsSorted(numbers))

	// Sort with custom function -- descending order
	desc := []int{5, 3, 8, 1, 9, 2, 7}
	slices.SortFunc(desc, func(a, b int) int {
		return cmp.Compare(b, a) // reversed
	})
	fmt.Println("Descending:", desc)

	// SortStableFunc preserves order of equal elements
	type Person struct {
		Name string
		Age  int
	}
	people := []Person{
		{"Alice", 30}, {"Bob", 25}, {"Charlie", 30}, {"Diana", 25},
	}
	slices.SortStableFunc(people, func(a, b Person) int {
		return cmp.Compare(a.Age, b.Age)
	})
	fmt.Println("By age (stable):", people)
}
```

`slices.Sort` works with any `cmp.Ordered` type. `slices.SortFunc` accepts a comparison function returning negative, zero, or positive.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Sorted: [1 2 3 5 7 8 9]
Names: [Alice Bob Charlie Diana]
Is sorted: true
Descending: [9 8 7 5 3 2 1]
By age (stable): [{Bob 25} {Diana 25} {Alice 30} {Charlie 30}]
```

## Step 2 -- Searching

```go
package main

import (
	"cmp"
	"fmt"
	"slices"
)

func main() {
	sorted := []int{1, 3, 5, 7, 9, 11, 13, 15}

	// BinarySearch on a sorted slice
	idx, found := slices.BinarySearch(sorted, 7)
	fmt.Printf("BinarySearch(7): idx=%d found=%v\n", idx, found)

	idx, found = slices.BinarySearch(sorted, 6)
	fmt.Printf("BinarySearch(6): idx=%d found=%v\n", idx, found)

	// Contains (linear search -- does not require sorted)
	names := []string{"alice", "bob", "charlie"}
	fmt.Println("Contains bob:", slices.Contains(names, "bob"))
	fmt.Println("Contains dave:", slices.Contains(names, "dave"))

	// Index
	fmt.Println("Index of charlie:", slices.Index(names, "charlie"))
	fmt.Println("Index of dave:", slices.Index(names, "dave"))

	// BinarySearchFunc with custom comparison
	type Item struct {
		Name  string
		Price float64
	}
	items := []Item{
		{"Apple", 1.20}, {"Banana", 0.50}, {"Cherry", 2.50}, {"Date", 3.00},
	}
	// Already sorted by Name
	idx, found = slices.BinarySearchFunc(items, "Cherry", func(item Item, target string) int {
		return cmp.Compare(item.Name, target)
	})
	fmt.Printf("BinarySearchFunc(Cherry): idx=%d found=%v\n", idx, found)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
BinarySearch(7): idx=3 found=true
BinarySearch(6): idx=3 found=false
Contains bob: true
Contains dave: false
Index of charlie: 2
Index of dave: -1
BinarySearchFunc(Cherry): idx=2 found=true
```

## Step 3 -- Modifying Slices

```go
package main

import (
	"fmt"
	"slices"
)

func main() {
	// Compact: remove consecutive duplicates (slice must be sorted for full dedup)
	sorted := []int{1, 1, 2, 3, 3, 3, 4, 5, 5}
	compacted := slices.Compact(sorted)
	fmt.Println("Compact:", compacted)

	// Insert at index
	s := []int{1, 2, 5, 6}
	s = slices.Insert(s, 2, 3, 4) // insert 3,4 at index 2
	fmt.Println("Insert:", s)

	// Delete range [i, j)
	s = slices.Delete(s, 1, 3) // delete indices 1, 2
	fmt.Println("Delete:", s)

	// Replace range
	r := []string{"a", "b", "c", "d", "e"}
	r = slices.Replace(r, 1, 3, "X", "Y", "Z") // replace b,c with X,Y,Z
	fmt.Println("Replace:", r)

	// Reverse in place
	rev := []int{1, 2, 3, 4, 5}
	slices.Reverse(rev)
	fmt.Println("Reverse:", rev)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Compact: [1 2 3 4 5]
Insert: [1 2 3 4 5 6]
Delete: [1 4 5 6]
Replace: [a X Y Z d e]
Reverse: [5 4 3 2 1]
```

## Step 4 -- Capacity Management

```go
package main

import (
	"fmt"
	"slices"
)

func main() {
	// Clip: reduce capacity to length
	s := make([]int, 3, 100)
	s[0], s[1], s[2] = 1, 2, 3
	fmt.Printf("Before Clip: len=%d cap=%d\n", len(s), cap(s))
	s = slices.Clip(s)
	fmt.Printf("After Clip:  len=%d cap=%d\n", len(s), cap(s))

	// Grow: ensure additional capacity
	g := []int{1, 2, 3}
	fmt.Printf("Before Grow: len=%d cap=%d\n", len(g), cap(g))
	g = slices.Grow(g, 100) // ensure room for 100 more elements
	fmt.Printf("After Grow:  len=%d cap=%d\n", len(g), cap(g))

	// Clone: create an independent copy
	original := []int{10, 20, 30}
	clone := slices.Clone(original)
	clone[0] = 999
	fmt.Println("Original:", original)
	fmt.Println("Clone:", clone)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Before Clip: len=3 cap=100
After Clip:  len=3 cap=3
Before Grow: len=3 cap=3
After Grow:  len=3 cap=103
Original: [10 20 30]
Clone: [999 20 30]
```

## Step 5 -- Comparison and Aggregation

```go
package main

import (
	"fmt"
	"slices"
)

func main() {
	a := []int{1, 2, 3}
	b := []int{1, 2, 3}
	c := []int{1, 2, 4}

	// Compare returns 0 if equal, -1 if a < c, +1 if a > c
	fmt.Println("Compare(a, b):", slices.Compare(a, b))
	fmt.Println("Compare(a, c):", slices.Compare(a, c))

	// Equal
	fmt.Println("Equal(a, b):", slices.Equal(a, b))
	fmt.Println("Equal(a, c):", slices.Equal(a, c))

	// Min and Max
	data := []int{42, 17, 93, 8, 55}
	fmt.Println("Min:", slices.Min(data))
	fmt.Println("Max:", slices.Max(data))

	// Concat (Go 1.22+)
	x := []int{1, 2}
	y := []int{3, 4}
	z := []int{5, 6}
	combined := slices.Concat(x, y, z)
	fmt.Println("Concat:", combined)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Compare(a, b): 0
Compare(a, c): -1
Equal(a, b): true
Equal(a, c): false
Min: 8
Max: 93
Concat: [1 2 3 4 5 6]
```

## Common Mistakes

### Using BinarySearch on an Unsorted Slice

**Wrong:**

```go
unsorted := []int{5, 3, 1, 4, 2}
idx, found := slices.BinarySearch(unsorted, 3)
```

**What happens:** Binary search assumes sorted input. Results are undefined on unsorted data.

**Fix:** Call `slices.Sort(unsorted)` first, or use `slices.Contains` for unsorted data.

### Forgetting Compact Requires Sorted Input for Full Dedup

**Wrong:**

```go
data := []int{1, 3, 1, 2, 3}
deduped := slices.Compact(data) // [1 3 1 2 3] -- no change!
```

**What happens:** `Compact` only removes consecutive duplicates. Non-adjacent duplicates survive.

**Fix:** Sort first: `slices.Sort(data); deduped := slices.Compact(data)`.

## Verify What You Learned

1. Sort a slice of structs by two fields (primary: age ascending, secondary: name alphabetical)
2. Deduplicate a slice of strings using `Sort` + `Compact`
3. Use `BinarySearch` to implement a function that checks membership in a large sorted dataset

## What's Next

Continue to [10 - Maps Package](../10-maps-package/10-maps-package.md) to learn the standard library functions for map manipulation introduced in Go 1.21.

## Summary

- `slices.Sort` sorts any `cmp.Ordered` slice; `SortFunc` accepts custom comparisons
- `slices.BinarySearch` finds elements in sorted slices in O(log n)
- `slices.Contains` and `slices.Index` provide linear search
- `slices.Compact` removes consecutive duplicates (sort first for full dedup)
- `slices.Insert`, `Delete`, `Replace`, `Reverse` modify slices in place
- `slices.Clip` reduces capacity to length; `Grow` ensures additional capacity
- `slices.Clone` creates a fully independent copy
- `slices.Equal`, `Compare`, `Min`, `Max` provide comparison and aggregation

## Reference

- [slices package](https://pkg.go.dev/slices)
- [cmp package](https://pkg.go.dev/cmp)
- [Go 1.21 Release Notes](https://go.dev/doc/go1.21)
