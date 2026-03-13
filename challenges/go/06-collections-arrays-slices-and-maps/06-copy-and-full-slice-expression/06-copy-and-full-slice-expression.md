# 6. Copy and Full Slice Expression

<!--
difficulty: intermediate
concepts: [copy-builtin, full-slice-expression, three-index-slice, decoupling-slices, defensive-copy, append-safety]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [slices-creation-append-capacity, slice-expressions-and-sub-slicing, nil-slices-vs-empty-slices]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-05 in this section
- Understanding of slice backing arrays, capacity, and sub-slicing
- Familiarity with how append can overwrite shared backing data

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** the `copy` built-in to create independent slices
- **Use** the full slice expression `s[low:high:max]` to limit capacity and prevent append overwrites
- **Design** functions that safely return slices without leaking internal state

## Why Copy and Full Slice Expressions

Exercises 02 and 03 demonstrated that sub-slices share backing arrays, and `append` on a sub-slice can overwrite the parent's data. In production code, this sharing leads to data corruption bugs that are notoriously difficult to reproduce and diagnose. The `copy` built-in and the three-index slice expression are your two tools for defensive programming with slices. `copy` creates a fully independent slice. The full slice expression `s[low:high:max]` restricts capacity so that any `append` is forced to allocate a new backing array, preventing silent overwrites.

## Step 1 -- The copy Built-in

```bash
mkdir -p ~/go-exercises/copy-and-fse
cd ~/go-exercises/copy-and-fse
go mod init copy-and-fse
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	src := []int{10, 20, 30, 40, 50}

	// copy(dst, src) -- copies min(len(dst), len(src)) elements
	dst := make([]int, len(src))
	n := copy(dst, src)
	fmt.Printf("Copied %d elements: %v\n", n, dst)

	// Modify dst -- src is unaffected
	dst[0] = 999
	fmt.Println("src:", src)
	fmt.Println("dst:", dst)
}
```

`copy` returns the number of elements copied, which is the minimum of `len(dst)` and `len(src)`. The destination must already be allocated with sufficient length.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Copied 5 elements: [10 20 30 40 50]
src: [10 20 30 40 50]
dst: [999 20 30 40 50]
```

## Step 2 -- Partial Copy and Overlapping Slices

```go
package main

import "fmt"

func main() {
	// Partial copy: dst is shorter than src
	src := []int{1, 2, 3, 4, 5}
	partial := make([]int, 3)
	n := copy(partial, src)
	fmt.Printf("Partial: copied %d -> %v\n", n, partial)

	// Copy into a sub-slice
	data := []int{0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	copy(data[3:], []int{7, 8, 9})
	fmt.Println("Into sub-slice:", data)

	// Overlapping: shift elements left
	s := []int{1, 2, 3, 4, 5}
	copy(s, s[2:]) // copy s[2:] to s[0:]
	fmt.Println("Shift left:", s) // [3 4 5 4 5]

	// Overlapping: shift elements right
	r := []int{1, 2, 3, 4, 5}
	copy(r[2:], r) // copy r[0:] to r[2:]
	fmt.Println("Shift right:", r) // [1 2 1 2 3]
}
```

`copy` handles overlapping source and destination correctly, similar to C's `memmove`.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Partial: copied 3 -> [1 2 3]
Into sub-slice: [0 0 0 7 8 9 0 0 0 0]
Shift left: [3 4 5 4 5]
Shift right: [1 2 1 2 3]
```

## Step 3 -- The Full Slice Expression (Three-Index Slice)

```go
package main

import "fmt"

func main() {
	data := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}

	// Standard sub-slice: inherits remaining capacity
	standard := data[2:5]
	fmt.Printf("standard: %v len=%d cap=%d\n", standard, len(standard), cap(standard))

	// Full slice expression: cap is limited to max-low
	restricted := data[2:5:5]
	fmt.Printf("restricted: %v len=%d cap=%d\n", restricted, len(restricted), cap(restricted))

	// Demonstrate the safety difference
	fmt.Println("\nBefore append:")
	fmt.Println("  data:", data)

	// Append to standard -- overwrites data[5]!
	s1 := append(standard, 999)
	fmt.Println("After append(standard, 999):")
	fmt.Println("  data:", data)
	fmt.Println("  s1:  ", s1)

	// Reset data
	data = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	restricted = data[2:5:5]

	// Append to restricted -- forced to allocate new array
	s2 := append(restricted, 888)
	fmt.Println("\nAfter append(restricted, 888):")
	fmt.Println("  data:", data) // unchanged
	fmt.Println("  s2:  ", s2)
}
```

The full slice expression `s[low:high:max]` sets the capacity to `max - low`. When capacity equals length, any `append` must allocate.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
standard: [2 3 4] len=3 cap=8
restricted: [2 3 4] len=3 cap=3

Before append:
  data: [0 1 2 3 4 5 6 7 8 9]
After append(standard, 999):
  data: [0 1 2 3 4 999 6 7 8 9]
  s1:   [2 3 4 999]

After append(restricted, 888):
  data: [0 1 2 3 4 5 6 7 8 9]
  s2:   [2 3 4 888]
```

## Step 4 -- Defensive Copy Pattern

```go
package main

import "fmt"

type Inventory struct {
	items []string
}

func NewInventory(items []string) *Inventory {
	// Defensive copy on input: caller can't modify our internal state
	internal := make([]string, len(items))
	copy(internal, items)
	return &Inventory{items: internal}
}

func (inv *Inventory) Items() []string {
	// Defensive copy on output: caller can't modify our internal state
	result := make([]string, len(inv.items))
	copy(result, inv.items)
	return result
}

func main() {
	source := []string{"sword", "shield", "potion"}
	inv := NewInventory(source)

	// Modify the original source
	source[0] = "CORRUPTED"
	fmt.Println("Source:", source)
	fmt.Println("Inventory:", inv.Items())

	// Modify the returned slice
	items := inv.Items()
	items[0] = "ALSO CORRUPTED"
	fmt.Println("Modified return:", items)
	fmt.Println("Inventory still:", inv.Items())
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Source: [CORRUPTED shield potion]
Inventory: [sword shield potion]
Modified return: [ALSO CORRUPTED shield potion]
Inventory still: [sword shield potion]
```

## Step 5 -- Idiomatic Clone with append

```go
package main

import "fmt"

func main() {
	original := []int{1, 2, 3, 4, 5}

	// One-liner clone using append
	// append to a nil slice forces allocation of a new backing array
	clone := append([]int(nil), original...)
	// Alternatively: clone := append(original[:0:0], original...)

	clone[0] = 999
	fmt.Println("original:", original)
	fmt.Println("clone:   ", clone)
}
```

The `append([]int(nil), original...)` idiom is a concise way to clone a slice. It allocates a new backing array and copies all elements in one expression.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
original: [1 2 3 4 5]
clone:    [999 2 3 4 5]
```

## Common Mistakes

### Forgetting to Allocate the Destination for copy

**Wrong:**

```go
var dst []int
copy(dst, src) // copies 0 elements because len(dst) == 0
```

**What happens:** `copy` copies `min(len(dst), len(src))` elements. A nil/empty destination means zero copies.

**Fix:** `dst := make([]int, len(src))` before calling `copy`.

### Using Full Slice Expression with Wrong max

**Wrong:**

```go
s := data[2:5:4] // panic: max < high
```

**What happens:** The constraint is `low <= high <= max <= cap(data)`. Violating this panics.

**Fix:** Ensure `max >= high`. For maximum safety, use `max == high`.

## Verify What You Learned

1. Write a function `Clone[T any](s []T) []T` that returns a fully independent copy of any slice
2. Given a large slice, extract elements at indices 10-20 using the full slice expression so that append on the result cannot corrupt the original
3. Implement `InsertAt(s []int, index int, value int) []int` using `copy` and `append`

## What's Next

Continue to [07 - Slice Internals](../07-slice-internals/07-slice-internals.md) to examine the runtime representation of slices and understand the slice header struct.

## Summary

- `copy(dst, src)` copies `min(len(dst), len(src))` elements; the destination must be pre-allocated
- `copy` handles overlapping slices correctly
- The full slice expression `s[low:high:max]` limits capacity to `max - low`
- When capacity equals length, `append` is forced to allocate a new backing array
- Use defensive copies in constructors and getters to prevent external mutation of internal state
- `append([]T(nil), original...)` is a concise clone idiom
- Always use the full slice expression when returning sub-slices from functions

## Reference

- [Go Spec: Appending to and copying slices](https://go.dev/ref/spec#Appending_and_copying_slices)
- [Go Spec: Full slice expressions](https://go.dev/ref/spec#Slice_expressions)
- [Go Blog: Go Slices: usage and internals](https://go.dev/blog/slices-intro)
- [Go Wiki: SliceTricks](https://go.dev/wiki/SliceTricks)
