# 7. Slice Internals

<!--
difficulty: intermediate
concepts: [slice-header, reflect-sliceheader, unsafe-pointer, backing-array, growth-algorithm, runtime-growslice]
tools: [go]
estimated_time: 25m
bloom_level: analyze
prerequisites: [slices-creation-append-capacity, slice-expressions-and-sub-slicing, copy-and-full-slice-expression]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-06 in this section
- Solid understanding of slice length, capacity, and backing arrays
- Willingness to use `unsafe` for educational purposes

## Learning Objectives

After completing this exercise, you will be able to:

- **Analyze** the three-field slice header (pointer, length, capacity)
- **Explain** when two slices share or diverge from the same backing array
- **Describe** the growth algorithm used by `append`

## Why Slice Internals

Most Go developers use slices daily without understanding their implementation. This works until something surprising happens: an append silently corrupts data, a slice passed to a function seems to ignore changes to its length, or a sub-slice holds an unexpectedly large allocation in memory. Understanding the slice header -- a struct with a pointer, length, and capacity -- turns these surprises into predictable behavior. This exercise bridges the gap between "using slices" and "reasoning about slices."

## Step 1 -- The Slice Header

```bash
mkdir -p ~/go-exercises/slice-internals
cd ~/go-exercises/slice-internals
go mod init slice-internals
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"unsafe"
)

func main() {
	s := []int{10, 20, 30, 40, 50}

	// A slice is a struct: { ptr *T, len int, cap int }
	// Its size is 3 words (24 bytes on 64-bit systems)
	fmt.Printf("Size of slice header: %d bytes\n", unsafe.Sizeof(s))
	fmt.Printf("Size of [5]int array: %d bytes\n", unsafe.Sizeof([5]int{}))

	// The slice header is what gets copied on assignment
	t := s
	fmt.Printf("\ns ptr: %p\n", &s[0])
	fmt.Printf("t ptr: %p\n", &t[0])
	fmt.Println("Same backing array:", &s[0] == &t[0])

	// Modifying through t affects s
	t[0] = 999
	fmt.Println("s[0]:", s[0]) // 999
}
```

A slice variable is a 24-byte struct (on 64-bit systems). Assigning a slice copies only this header, not the underlying data.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Size of slice header: 24 bytes
Size of [5]int array: 40 bytes

s ptr: 0x<address>
t ptr: 0x<address>
Same backing array: true
s[0]: 999
```

## Step 2 -- Visualizing Pointer, Length, and Capacity

```go
package main

import (
	"fmt"
	"unsafe"
)

type sliceHeader struct {
	ptr unsafe.Pointer
	len int
	cap int
}

func inspectSlice(name string, s []int) {
	hdr := (*sliceHeader)(unsafe.Pointer(&s))
	fmt.Printf("%-12s ptr=%-14p len=%-4d cap=%-4d data=%v\n",
		name, hdr.ptr, hdr.len, hdr.cap, s)
}

func main() {
	data := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}

	inspectSlice("data", data)
	inspectSlice("data[2:5]", data[2:5])
	inspectSlice("data[5:]", data[5:])
	inspectSlice("data[:3]", data[:3])
	inspectSlice("data[2:5:5]", data[2:5:5])
}
```

Notice how sub-slices have different pointer offsets but may share the same underlying memory region.

### Intermediate Verification

```bash
go run main.go
```

Expected (addresses will differ but offsets between them are consistent):

```
data         ptr=0x<base>       len=10   cap=10   data=[0 1 2 3 4 5 6 7 8 9]
data[2:5]    ptr=0x<base+16>    len=3    cap=8    data=[2 3 4]
data[5:]     ptr=0x<base+40>    len=5    cap=5    data=[5 6 7 8 9]
data[:3]     ptr=0x<base>       len=3    cap=10   data=[0 1 2]
data[2:5:5]  ptr=0x<base+16>    len=3    cap=3    data=[2 3 4]
```

## Step 3 -- Why Functions Cannot Change a Slice's Length

```go
package main

import "fmt"

func appendInFunction(s []int) {
	s = append(s, 999)
	fmt.Println("Inside function:", s)
}

func main() {
	data := make([]int, 3, 10) // plenty of capacity
	data[0], data[1], data[2] = 1, 2, 3

	appendInFunction(data)
	fmt.Println("After function:", data)
	fmt.Println("Length:", len(data)) // still 3!

	// The value WAS written to the backing array
	// but the caller's slice header still has len=3
	extended := data[:4]
	fmt.Println("Extended view:", extended) // [1 2 3 999]
}
```

Because the slice header is passed by value, the function's `append` updates the local copy's length. The caller's length is unchanged. The data IS in the backing array, but the caller's header does not know about it.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Inside function: [1 2 3 999]
After function: [1 2 3]
Length: 3
Extended view: [1 2 3 999]
```

## Step 4 -- Growth Algorithm

```go
package main

import "fmt"

func main() {
	var s []int
	prevCap := cap(s)

	fmt.Printf("%-8s %-8s %-8s %-8s\n", "Len", "Cap", "Growth", "Factor")
	for i := 0; i < 100_000; i++ {
		s = append(s, i)
		if cap(s) != prevCap {
			factor := "N/A"
			if prevCap > 0 {
				factor = fmt.Sprintf("%.2fx", float64(cap(s))/float64(prevCap))
			}
			fmt.Printf("%-8d %-8d %-8d %s\n",
				len(s), cap(s), cap(s)-prevCap, factor)
			prevCap = cap(s)
		}
	}
}
```

Run this to see the actual growth pattern. For small slices (< 256 elements as of Go 1.21), the capacity roughly doubles. For larger slices, growth is approximately 1.25x plus some smoothing. The exact algorithm is in `runtime.growslice`.

### Intermediate Verification

```bash
go run main.go
```

The output shows the growth pattern transitioning from ~2x to ~1.25x as the slice grows.

## Step 5 -- Detecting Shared vs Independent Backing Arrays

```go
package main

import (
	"fmt"
	"unsafe"
)

func sharesBackingArray(a, b []int) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	aStart := uintptr(unsafe.Pointer(&a[0]))
	aEnd := aStart + uintptr(cap(a))*unsafe.Sizeof(a[0])
	bStart := uintptr(unsafe.Pointer(&b[0]))
	return bStart >= aStart && bStart < aEnd
}

func main() {
	data := []int{1, 2, 3, 4, 5}
	sub := data[1:3]
	clone := append([]int(nil), data...)

	fmt.Println("data & sub share:", sharesBackingArray(data, sub))
	fmt.Println("data & clone share:", sharesBackingArray(data, clone))

	// After append that triggers reallocation
	big := data
	for i := 0; i < 100; i++ {
		big = append(big, i)
	}
	fmt.Println("data & big share:", sharesBackingArray(data, big))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
data & sub share: true
data & clone share: false
data & big share: false
```

## Common Mistakes

### Assuming append Always Modifies the Original

**Wrong:**

```go
func addElement(s []int, val int) {
    s = append(s, val) // caller never sees the updated length
}
```

**What happens:** The function receives a copy of the slice header. Append updates the local copy.

**Fix:** Return the new slice: `func addElement(s []int, val int) []int { return append(s, val) }`. Or pass a pointer to the slice.

### Using reflect.SliceHeader (Deprecated)

**Wrong:**

```go
hdr := (*reflect.SliceHeader)(unsafe.Pointer(&s))
```

**What happens:** `reflect.SliceHeader` is deprecated in Go 1.21+. The runtime does not guarantee its layout matches the actual slice header.

**Fix:** Use `unsafe.SliceData`, `unsafe.Slice`, or define your own struct for educational purposes.

## Verify What You Learned

1. Write a program that demonstrates a function modifying slice elements (visible to caller) vs appending (invisible to caller)
2. Create a slice, take two sub-slices, and prove they share the same backing array by checking element addresses
3. Measure the growth factor at several points (len=10, 100, 1000, 10000) and note the transition

## What's Next

Continue to [08 - Map Internals and Iteration Order](../08-map-internals-and-iteration-order/08-map-internals-and-iteration-order.md) to examine how maps work under the hood.

## Summary

- A slice is a 24-byte header: `{ pointer *T, length int, capacity int }`
- Assigning a slice copies only the header, not the backing data
- Functions receive a copy of the header: they can modify elements but cannot change the caller's length
- Sub-slices share the backing array; the pointer field points into the parent's data
- The full slice expression `s[low:high:max]` modifies only the capacity field
- Growth algorithm: ~2x for small slices, ~1.25x for large slices (Go 1.21+)
- Use `unsafe.Pointer` only for educational inspection; never in production code

## Reference

- [Go Blog: Go Slices: usage and internals](https://go.dev/blog/slices-intro)
- [Go Source: runtime/slice.go growslice](https://github.com/golang/go/blob/master/src/runtime/slice.go)
- [Go Spec: Slice types](https://go.dev/ref/spec#Slice_types)
