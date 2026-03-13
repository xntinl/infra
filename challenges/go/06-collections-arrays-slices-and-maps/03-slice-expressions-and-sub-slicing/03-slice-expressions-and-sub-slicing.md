# 3. Slice Expressions and Sub-Slicing

<!--
difficulty: basic
concepts: [slice-expressions, half-open-interval, sub-slicing, shared-backing-array, bounds, three-index-slice]
tools: [go]
estimated_time: 20m
bloom_level: understand
prerequisites: [arrays-fixed-size-value-semantics, slices-creation-append-capacity]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01 (arrays) and 02 (slices basics)
- Understanding of slice length and capacity
- A terminal and text editor

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the half-open interval `[low:high)` used in slice expressions
- **Create** sub-slices from arrays and existing slices
- **Predict** how sub-slices share memory with their parent

## Why Slice Expressions

Slice expressions let you create a window into an existing array or slice without copying data. This is both powerful and dangerous: powerful because it avoids allocation and copying, dangerous because modifications through one slice are visible through the other. Mastering slice expressions is essential for writing efficient Go code that manipulates portions of data in place -- parsing protocols, processing buffers, and filtering collections all rely on sub-slicing.

## Step 1 -- Basic Slice Expressions

```bash
mkdir -p ~/go-exercises/slice-expressions
cd ~/go-exercises/slice-expressions
go mod init slice-expressions
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	data := []int{10, 20, 30, 40, 50, 60, 70}

	// s[low:high] -- elements from low to high-1 (half-open interval)
	fmt.Println("data[2:5]:", data[2:5])   // [30 40 50]
	fmt.Println("data[:3]:", data[:3])      // [10 20 30]
	fmt.Println("data[4:]:", data[4:])      // [50 60 70]
	fmt.Println("data[:]:", data[:])        // [10 20 30 40 50 60 70]

	// The same works on arrays
	arr := [5]string{"a", "b", "c", "d", "e"}
	slc := arr[1:4]
	fmt.Println("arr[1:4]:", slc) // [b c d]
	fmt.Printf("Type: %T\n", slc) // []string (a slice, not an array)
}
```

The expression `s[low:high]` selects elements at indices `low`, `low+1`, ..., `high-1`. The number of elements is `high - low`.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
data[2:5]: [30 40 50]
data[:3]: [10 20 30]
data[4:]: [50 60 70]
data[:]: [10 20 30 40 50 60 70]
arr[1:4]: [b c d]
Type: []string
```

## Step 2 -- Sub-Slices Share the Backing Array

```go
package main

import "fmt"

func main() {
	original := []int{1, 2, 3, 4, 5}
	sub := original[1:4] // [2 3 4]

	fmt.Println("Before mutation:")
	fmt.Println("  original:", original)
	fmt.Println("  sub:     ", sub)

	// Modify through the sub-slice
	sub[0] = 999

	fmt.Println("After sub[0] = 999:")
	fmt.Println("  original:", original) // [1 999 3 4 5]
	fmt.Println("  sub:     ", sub)      // [999 3 4]
}
```

The sub-slice does not own its own memory. It points into the same backing array as `original`.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Before mutation:
  original: [1 2 3 4 5]
  sub:      [2 3 4]
After sub[0] = 999:
  original: [1 999 3 4 5]
  sub:      [999 3 4]
```

## Step 3 -- Capacity of Sub-Slices

```go
package main

import "fmt"

func main() {
	data := []int{10, 20, 30, 40, 50, 60, 70}

	s1 := data[2:5]
	fmt.Printf("s1 = data[2:5]: %v  len=%d cap=%d\n", s1, len(s1), cap(s1))

	s2 := data[2:5:6]
	fmt.Printf("s2 = data[2:5:6]: %v  len=%d cap=%d\n", s2, len(s2), cap(s2))

	s3 := data[:3]
	fmt.Printf("s3 = data[:3]: %v  len=%d cap=%d\n", s3, len(s3), cap(s3))

	s4 := data[5:]
	fmt.Printf("s4 = data[5:]: %v  len=%d cap=%d\n", s4, len(s4), cap(s4))
}
```

The capacity of `s[low:high]` is `cap(original) - low`. This means a sub-slice starting at a later index has less capacity. The three-index form `s[low:high:max]` explicitly limits the capacity to `max - low`.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
s1 = data[2:5]: [30 40 50]  len=3 cap=5
s2 = data[2:5:6]: [30 40 50]  len=3 cap=4
s3 = data[:3]: [10 20 30]  len=3 cap=7
s4 = data[5:]: [60 70]  len=2 cap=2
```

## Step 4 -- Append on a Sub-Slice Can Overwrite Parent Data

```go
package main

import "fmt"

func main() {
	original := []int{1, 2, 3, 4, 5}
	sub := original[1:3] // [2 3], cap=4

	fmt.Println("Before append:")
	fmt.Println("  original:", original)
	fmt.Println("  sub:     ", sub, "cap:", cap(sub))

	// Append fits within capacity -- overwrites original[3]!
	sub = append(sub, 999)

	fmt.Println("After append(sub, 999):")
	fmt.Println("  original:", original) // [1 2 3 999 5]
	fmt.Println("  sub:     ", sub)      // [2 3 999]
}
```

This is one of the most common sources of bugs with sub-slices. Because `sub` has remaining capacity from the parent array, `append` writes into the parent's memory.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Before append:
  original: [1 2 3 4 5]
  sub:      [2 3] cap: 4
After append(sub, 999):
  original: [1 2 3 999 5]
  sub:      [2 3 999]
```

## Step 5 -- Practical Example: Filtering Without Allocation

```go
package main

import "fmt"

func filterEven(data []int) []int {
	// Reuse the same backing array to avoid allocation
	result := data[:0]
	for _, v := range data {
		if v%2 == 0 {
			result = append(result, v)
		}
	}
	return result
}

func main() {
	numbers := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	evens := filterEven(numbers)
	fmt.Println("Evens:", evens)
	fmt.Println("Original (modified):", numbers)
}
```

The `data[:0]` expression creates an empty slice with the same backing array. Appending reuses the existing memory. This pattern is common in performance-sensitive code, but be aware that it modifies the original slice's data.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Evens: [2 4 6 8 10]
Original (modified): [2 4 6 8 10 6 7 8 9 10]
```

## Common Mistakes

### Off-by-One with Half-Open Intervals

**Wrong:**

```go
data := []int{10, 20, 30, 40, 50}
last3 := data[3:6] // panic: index out of range
```

**What happens:** `data` has indices 0-4. Index 6 is out of bounds.

**Fix:** Use `data[2:5]` to get the last 3 elements.

### Forgetting Sub-Slices Share Memory

**Wrong:**

```go
config := []string{"host=localhost", "port=8080", "mode=debug"}
subset := config[1:]
subset[0] = "port=9090"
// config is now ["host=localhost", "port=9090", "mode=debug"]
```

**What happens:** `subset` shares the backing array with `config`.

**Fix:** Use `copy` or the three-index slice expression to decouple.

## Verify What You Learned

1. Given `s := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}`, write slice expressions to extract `[3 4 5]`, `[0 1 2]`, and `[7 8 9]`
2. Create a sub-slice and demonstrate that appending to it can overwrite the parent, then use the three-index form to prevent this
3. Write a function that removes the element at index `i` from a slice using sub-slicing and `append`

## What's Next

Continue to [04 - Maps: Creation, Access, and Iteration](../04-maps-creation-access-iteration/04-maps-creation-access-iteration.md) to learn Go's built-in hash map type.

## Summary

- Slice expressions use half-open intervals: `s[low:high]` includes `low`, excludes `high`
- Omitted indices default to `0` (low) and `len(s)` (high)
- Sub-slices share the same backing array as the parent
- Capacity of `s[low:high]` is `cap(parent) - low`
- The three-index form `s[low:high:max]` limits capacity to `max - low`
- Appending to a sub-slice within its capacity overwrites the parent's data
- The `s[:0]` pattern enables zero-allocation filtering

## Reference

- [Go Spec: Slice expressions](https://go.dev/ref/spec#Slice_expressions)
- [Go Blog: Go Slices: usage and internals](https://go.dev/blog/slices-intro)
- [Go Wiki: SliceTricks](https://go.dev/wiki/SliceTricks)
