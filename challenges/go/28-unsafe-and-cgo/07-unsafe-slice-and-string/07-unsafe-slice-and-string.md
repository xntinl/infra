# 7. unsafe.Slice and unsafe.String

<!--
difficulty: advanced
concepts: [unsafe-slice, unsafe-string, unsafe-stringdata, unsafe-slicedata, go117-unsafe, zero-copy-conversion]
tools: [go]
estimated_time: 25m
bloom_level: analyze
prerequisites: [unsafe-pointer-and-uintptr, type-punning, memory-layout]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of `unsafe.Pointer`, slice headers, and string headers
- Familiarity with how Go represents slices (pointer + length + capacity) and strings (pointer + length) internally

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** `unsafe.Slice` and `unsafe.String` (Go 1.17+/1.20+) to create slices and strings from raw pointers
- **Use** `unsafe.SliceData` and `unsafe.StringData` (Go 1.20+) to extract the underlying data pointer
- **Replace** deprecated `reflect.SliceHeader`/`reflect.StringHeader` patterns with the modern equivalents
- **Implement** zero-copy `[]byte` to `string` conversion and back

## Why unsafe.Slice and unsafe.String

Before Go 1.17, creating a slice from a raw pointer required manually constructing a `reflect.SliceHeader` -- a fragile pattern the Go team explicitly deprecated. Go 1.17 introduced `unsafe.Slice(ptr, len)` as the safe(r) replacement. Go 1.20 added `unsafe.String`, `unsafe.StringData`, and `unsafe.SliceData` to complete the set.

These functions are essential for cgo interop (turning a C array pointer into a Go slice), zero-copy byte-to-string conversion, and memory-mapped file access. They are the official, supported way to perform these operations.

## Step 1 -- unsafe.Slice: Pointer to Slice

```bash
mkdir -p ~/go-exercises/unsafe-slice-string && cd ~/go-exercises/unsafe-slice-string
go mod init unsafe-slice-string
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"unsafe"
)

func main() {
	// Create a slice from a raw pointer and length
	array := [5]int32{10, 20, 30, 40, 50}
	ptr := &array[0]

	// unsafe.Slice creates a slice from a pointer and length
	// The resulting slice's backing array IS the original memory
	slice := unsafe.Slice(ptr, 5)
	fmt.Printf("Slice from array pointer: %v\n", slice)
	fmt.Printf("Type: %T, len=%d, cap=%d\n", slice, len(slice), cap(slice))

	// Modifying the slice modifies the original array (zero-copy)
	slice[2] = 999
	fmt.Printf("After modification: array=%v, slice=%v\n", array, slice)

	// Create a sub-slice starting from the middle
	midPtr := &array[2]
	subSlice := unsafe.Slice(midPtr, 3) // elements [2], [3], [4]
	fmt.Printf("Sub-slice from middle: %v\n", subSlice)

	// Common cgo pattern: C returns a pointer and length
	// You would do: goSlice := unsafe.Slice((*byte)(cPtr), cLen)
}
```

```bash
go run main.go
```

### Intermediate Verification

`unsafe.Slice` creates a Go slice whose backing array is the memory at `ptr` with the given length. The capacity equals the length. Modifications through the slice are visible through the original pointer.

## Step 2 -- unsafe.String and unsafe.StringData

```go
package main

import (
	"fmt"
	"unsafe"
)

func main() {
	// unsafe.String: create a string from a byte pointer and length
	data := [5]byte{'H', 'e', 'l', 'l', 'o'}
	str := unsafe.String(&data[0], 5)
	fmt.Printf("String from bytes: %q\n", str)

	// The string points to the SAME memory -- no copy
	// WARNING: modifying data after creating the string violates
	// Go's string immutability contract
	fmt.Printf("String data address: %p\n", unsafe.StringData(str))
	fmt.Printf("Array address:       %p\n", &data[0])

	// unsafe.StringData: extract the byte pointer from a string
	s := "Go unsafe"
	ptr := unsafe.StringData(s)
	fmt.Printf("\nString %q data pointer: %p\n", s, ptr)

	// Create a read-only byte slice from a string (zero-copy)
	// WARNING: Do NOT write to this slice -- strings are immutable
	readOnlyBytes := unsafe.Slice(ptr, len(s))
	fmt.Printf("Read-only bytes: %v = %q\n", readOnlyBytes, string(readOnlyBytes))

	// unsafe.SliceData: extract the data pointer from a slice
	slice := []byte{1, 2, 3, 4, 5}
	slicePtr := unsafe.SliceData(slice)
	fmt.Printf("\nSlice data pointer: %p\n", slicePtr)
	fmt.Printf("First element via pointer: %d\n", *slicePtr)
}
```

```bash
go run main.go
```

## Step 3 -- Zero-Copy String/Bytes Conversion

The most common use case: converting between `[]byte` and `string` without copying.

```go
package main

import (
	"fmt"
	"testing"
	"unsafe"
)

// Zero-copy []byte -> string (Go 1.20+)
func bytesToString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(b), len(b))
}

// Zero-copy string -> []byte (Go 1.20+)
// WARNING: The returned slice MUST NOT be modified
func stringToBytes(s string) []byte {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

// The old way (deprecated, do not use):
// func oldBytesToString(b []byte) string {
//     return *(*string)(unsafe.Pointer(&b))
// }

func main() {
	// Zero-copy string from bytes
	original := []byte("hello, unsafe world")
	s := bytesToString(original)
	fmt.Printf("String: %q\n", s)

	// Zero-copy bytes from string
	s2 := "convert me"
	b := stringToBytes(s2)
	fmt.Printf("Bytes: %v = %q\n", b, string(b))

	// Demonstrate they share memory
	fmt.Printf("String data: %p\n", unsafe.StringData(s))
	fmt.Printf("Bytes data:  %p\n", unsafe.SliceData(original))
	fmt.Printf("Same memory: %v\n",
		unsafe.StringData(s) == unsafe.SliceData(original))
}

// Benchmarks comparing copy vs zero-copy
func BenchmarkStringCopy(b *testing.B) {
	data := make([]byte, 1024)
	var s string
	for i := 0; i < b.N; i++ {
		s = string(data) // allocates and copies
	}
	_ = s
}

func BenchmarkStringZeroCopy(b *testing.B) {
	data := make([]byte, 1024)
	var s string
	for i := 0; i < b.N; i++ {
		s = bytesToString(data) // no allocation
	}
	_ = s
}

func BenchmarkBytesCopy(b *testing.B) {
	s := string(make([]byte, 1024))
	var data []byte
	for i := 0; i < b.N; i++ {
		data = []byte(s) // allocates and copies
	}
	_ = data
}

func BenchmarkBytesZeroCopy(b *testing.B) {
	s := string(make([]byte, 1024))
	var data []byte
	for i := 0; i < b.N; i++ {
		data = stringToBytes(s) // no allocation
	}
	_ = data
}
```

```bash
go test -bench=Benchmark -benchmem
```

## Step 4 -- Practical Use: cgo Array to Go Slice

```go
package main

/*
#include <stdlib.h>
#include <stdint.h>

// Simulate a C function that returns an array and its length
int32_t* create_array(int* out_len) {
    *out_len = 5;
    int32_t* arr = (int32_t*)malloc(5 * sizeof(int32_t));
    for (int i = 0; i < 5; i++) {
        arr[i] = (i + 1) * 10;
    }
    return arr;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func main() {
	var length C.int
	cArray := C.create_array(&length)
	defer C.free(unsafe.Pointer(cArray))

	// Modern way: unsafe.Slice (Go 1.17+)
	goSlice := unsafe.Slice((*int32)(unsafe.Pointer(cArray)), int(length))
	fmt.Printf("C array as Go slice: %v\n", goSlice)

	// goSlice is valid only as long as cArray is not freed
	// Copy if you need the data to outlive the C memory:
	owned := make([]int32, len(goSlice))
	copy(owned, goSlice)
	fmt.Printf("Owned copy: %v\n", owned)
}
```

## Hints

- `unsafe.Slice(ptr, len)` replaces the deprecated `reflect.SliceHeader` pattern -- always use it in Go 1.17+
- `unsafe.String(ptr, len)` replaces `*(*string)(unsafe.Pointer(&sliceHeader))` -- always use it in Go 1.20+
- Zero-copy `[]byte` to `string` is safe IF you never modify the `[]byte` afterward
- Zero-copy `string` to `[]byte` is safe IF you never write through the returned slice
- `unsafe.Slice` with a nil pointer and zero length returns a nil slice
- `unsafe.Slice` panics if length is negative
- For cgo: `unsafe.Slice((*T)(unsafe.Pointer(cPtr)), length)` is the idiomatic way to create a Go slice from a C array

## Verification

- `unsafe.Slice` creates a slice that shares memory with the source pointer
- `unsafe.String` creates a string that shares memory with the source bytes
- Zero-copy benchmarks show 0 `allocs/op`, while copy benchmarks show 1 `allocs/op`
- `unsafe.SliceData` and `unsafe.StringData` return the correct underlying pointers
- cgo array conversion works correctly and the slice is valid while the C memory lives

## What's Next

With modern unsafe utilities mastered, the next exercise takes on a full capstone: wrapping an entire C library with a Go-idiomatic API.

## Summary

Go 1.17 introduced `unsafe.Slice(ptr, len)` to create a slice from a raw pointer, replacing the fragile `reflect.SliceHeader` pattern. Go 1.20 added `unsafe.String(ptr, len)`, `unsafe.StringData(s)`, and `unsafe.SliceData(s)` for zero-copy string/byte conversions and pointer extraction. These are the official, supported APIs for cgo interop (C array to Go slice), zero-copy string conversion (for read-only use), and memory-mapped data access. Zero-copy conversion eliminates allocation overhead but requires discipline: never modify shared backing memory unexpectedly.

## Reference

- [unsafe.Slice](https://pkg.go.dev/unsafe#Slice) (Go 1.17+)
- [unsafe.String](https://pkg.go.dev/unsafe#String) (Go 1.20+)
- [unsafe.SliceData](https://pkg.go.dev/unsafe#SliceData) (Go 1.20+)
- [unsafe.StringData](https://pkg.go.dev/unsafe#StringData) (Go 1.20+)
- [Go 1.17 release notes: unsafe additions](https://go.dev/doc/go1.17#unsafe)
- [Go 1.20 release notes: unsafe additions](https://go.dev/doc/go1.20#unsafe)
