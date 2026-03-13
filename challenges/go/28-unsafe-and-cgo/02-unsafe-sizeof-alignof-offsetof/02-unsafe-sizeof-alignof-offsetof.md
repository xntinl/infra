# 2. unsafe.Sizeof, Alignof, and Offsetof

<!--
difficulty: advanced
concepts: [sizeof, alignof, offsetof, struct-padding, memory-layout, alignment-rules]
tools: [go]
estimated_time: 25m
bloom_level: analyze
prerequisites: [unsafe-pointer-and-uintptr, structs-and-methods, memory-layout]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of `unsafe.Pointer` and `uintptr` from exercise 1
- Basic knowledge of how CPUs access aligned memory

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** `unsafe.Sizeof`, `unsafe.Alignof`, and `unsafe.Offsetof` to inspect memory layout
- **Explain** struct padding and alignment rules in Go
- **Optimize** struct field ordering to minimize memory waste
- **Predict** the size of a struct given its fields and alignment requirements

## Why Sizeof, Alignof, and Offsetof

Go structs have hidden gaps. A struct with a `bool` followed by an `int64` does not occupy 9 bytes -- it occupies 16, because the `int64` must be aligned to an 8-byte boundary, and 7 bytes of padding are inserted after the `bool`. Understanding these layout rules is essential when working with `unsafe` operations, designing cache-friendly data structures, or interoperating with C.

`unsafe.Sizeof` returns the size of a value's type in bytes (not including data pointed to). `unsafe.Alignof` returns the alignment requirement. `unsafe.Offsetof` returns a field's byte offset from the start of the struct. Together they let you reason about exactly where each byte lives in memory.

## Step 1 -- Inspect Basic Type Sizes

```bash
mkdir -p ~/go-exercises/unsafe-layout && cd ~/go-exercises/unsafe-layout
go mod init unsafe-layout
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"unsafe"
)

func main() {
	fmt.Println("=== Basic Type Sizes ===")
	fmt.Printf("bool:       size=%d  align=%d\n", unsafe.Sizeof(bool(false)), unsafe.Alignof(bool(false)))
	fmt.Printf("int8:       size=%d  align=%d\n", unsafe.Sizeof(int8(0)), unsafe.Alignof(int8(0)))
	fmt.Printf("int16:      size=%d  align=%d\n", unsafe.Sizeof(int16(0)), unsafe.Alignof(int16(0)))
	fmt.Printf("int32:      size=%d  align=%d\n", unsafe.Sizeof(int32(0)), unsafe.Alignof(int32(0)))
	fmt.Printf("int64:      size=%d  align=%d\n", unsafe.Sizeof(int64(0)), unsafe.Alignof(int64(0)))
	fmt.Printf("float32:    size=%d  align=%d\n", unsafe.Sizeof(float32(0)), unsafe.Alignof(float32(0)))
	fmt.Printf("float64:    size=%d  align=%d\n", unsafe.Sizeof(float64(0)), unsafe.Alignof(float64(0)))
	fmt.Printf("complex128: size=%d  align=%d\n", unsafe.Sizeof(complex128(0)), unsafe.Alignof(complex128(0)))
	fmt.Printf("string:     size=%d  align=%d\n", unsafe.Sizeof(""), unsafe.Alignof(""))
	fmt.Printf("[]byte:     size=%d  align=%d\n", unsafe.Sizeof([]byte{}), unsafe.Alignof([]byte{}))
	fmt.Printf("*int:       size=%d  align=%d\n", unsafe.Sizeof((*int)(nil)), unsafe.Alignof((*int)(nil)))
	fmt.Printf("int (arch): size=%d  align=%d\n", unsafe.Sizeof(int(0)), unsafe.Alignof(int(0)))

	fmt.Println("\n=== Composite Types ===")
	fmt.Printf("interface{}:   size=%d  align=%d\n", unsafe.Sizeof(interface{}(nil)), unsafe.Alignof(interface{}(nil)))
	fmt.Printf("map[string]int: size=%d  align=%d\n", unsafe.Sizeof(map[string]int{}), unsafe.Alignof(map[string]int{}))
	fmt.Printf("chan int:       size=%d  align=%d\n", unsafe.Sizeof(make(chan int)), unsafe.Alignof(make(chan int)))
}
```

```bash
go run main.go
```

### Intermediate Verification

On a 64-bit system: `string` is 16 bytes (pointer + length), `[]byte` is 24 bytes (pointer + length + capacity), pointers are 8 bytes, `interface{}` is 16 bytes (type pointer + data pointer). Maps and channels are pointers internally, so they are 8 bytes.

## Step 2 -- Struct Padding and Field Ordering

```go
package main

import (
	"fmt"
	"unsafe"
)

// Wasteful layout: lots of padding
type Wasteful struct {
	A bool    // 1 byte + 7 padding
	B int64   // 8 bytes
	C bool    // 1 byte + 3 padding
	D int32   // 4 bytes
	E bool    // 1 byte + 7 padding
	F int64   // 8 bytes
}

// Compact layout: same fields, reordered
type Compact struct {
	B int64   // 8 bytes
	F int64   // 8 bytes
	D int32   // 4 bytes
	A bool    // 1 byte
	C bool    // 1 byte
	E bool    // 1 byte + 1 padding
}

func printLayout(name string, size uintptr, fields []struct{ name string; offset, size uintptr }) {
	fmt.Printf("\n%s (total size: %d bytes)\n", name, size)
	fmt.Println("  Offset  Size  Field")
	fmt.Println("  ------  ----  -----")
	for _, f := range fields {
		padding := ""
		if len(fields) > 0 {
			// detect padding before this field
		}
		fmt.Printf("  %6d  %4d  %s %s\n", f.offset, f.size, f.name, padding)
	}
}

func main() {
	var w Wasteful
	fmt.Printf("Wasteful: size=%d, align=%d\n", unsafe.Sizeof(w), unsafe.Alignof(w))
	fmt.Printf("  A (bool):  offset=%d\n", unsafe.Offsetof(w.A))
	fmt.Printf("  B (int64): offset=%d\n", unsafe.Offsetof(w.B))
	fmt.Printf("  C (bool):  offset=%d\n", unsafe.Offsetof(w.C))
	fmt.Printf("  D (int32): offset=%d\n", unsafe.Offsetof(w.D))
	fmt.Printf("  E (bool):  offset=%d\n", unsafe.Offsetof(w.E))
	fmt.Printf("  F (int64): offset=%d\n", unsafe.Offsetof(w.F))

	var c Compact
	fmt.Printf("\nCompact: size=%d, align=%d\n", unsafe.Sizeof(c), unsafe.Alignof(c))
	fmt.Printf("  B (int64): offset=%d\n", unsafe.Offsetof(c.B))
	fmt.Printf("  F (int64): offset=%d\n", unsafe.Offsetof(c.F))
	fmt.Printf("  D (int32): offset=%d\n", unsafe.Offsetof(c.D))
	fmt.Printf("  A (bool):  offset=%d\n", unsafe.Offsetof(c.A))
	fmt.Printf("  C (bool):  offset=%d\n", unsafe.Offsetof(c.C))
	fmt.Printf("  E (bool):  offset=%d\n", unsafe.Offsetof(c.E))

	saved := unsafe.Sizeof(w) - unsafe.Sizeof(c)
	fmt.Printf("\nBytes saved by reordering: %d (%.0f%% reduction)\n",
		saved, float64(saved)/float64(unsafe.Sizeof(w))*100)
}
```

```bash
go run main.go
```

### Intermediate Verification

`Wasteful` is 40 bytes (with 10 bytes of padding). `Compact` is 24 bytes (with only 1 byte of padding). Same data, 40% less memory.

## Step 3 -- Visualize Memory Layout

Build a function that dumps the raw bytes of a struct to show padding:

```go
package main

import (
	"fmt"
	"unsafe"
)

func dumpBytes(name string, ptr unsafe.Pointer, size uintptr) {
	fmt.Printf("\n%s raw bytes (%d):\n", name, size)
	bytes := unsafe.Slice((*byte)(ptr), size)
	for i, b := range bytes {
		if i > 0 && i%8 == 0 {
			fmt.Println()
		}
		fmt.Printf("%02x ", b)
	}
	fmt.Println()
}

type Example struct {
	A uint8   // 1 byte
	B uint64  // 8 bytes (offset 8 after 7 padding)
	C uint8   // 1 byte
	D uint32  // 4 bytes (offset 12 after 3 padding)
}

func main() {
	e := Example{A: 0xAA, B: 0xBBBBBBBBBBBBBBBB, C: 0xCC, D: 0xDDDDDDDD}
	dumpBytes("Example", unsafe.Pointer(&e), unsafe.Sizeof(e))

	fmt.Println("\nField map:")
	fmt.Printf("  A at offset %d (byte 0xAA)\n", unsafe.Offsetof(e.A))
	fmt.Printf("  B at offset %d (bytes 0xBB...)\n", unsafe.Offsetof(e.B))
	fmt.Printf("  C at offset %d (byte 0xCC)\n", unsafe.Offsetof(e.C))
	fmt.Printf("  D at offset %d (bytes 0xDD...)\n", unsafe.Offsetof(e.D))
	fmt.Printf("  Total: %d bytes\n", unsafe.Sizeof(e))
}
```

## Step 4 -- The Alignment Rule

```go
package main

import (
	"fmt"
	"unsafe"
)

// Rule: a field with alignment N must start at an offset that is a multiple of N.
// The struct's total size must be a multiple of its largest alignment.

type AlignDemo struct {
	A int8    // align=1, offset=0
	B int16   // align=2, offset=2 (1 byte padding after A)
	C int8    // align=1, offset=4
	D int32   // align=4, offset=8 (3 bytes padding after C)
	E int8    // align=1, offset=12
	// Total must be multiple of max(align) = 4 -> padded to 16
}

func main() {
	var d AlignDemo
	fmt.Printf("AlignDemo: size=%d align=%d\n", unsafe.Sizeof(d), unsafe.Alignof(d))
	fmt.Printf("  A: offset=%d, size=%d, align=%d\n", unsafe.Offsetof(d.A), unsafe.Sizeof(d.A), unsafe.Alignof(d.A))
	fmt.Printf("  B: offset=%d, size=%d, align=%d\n", unsafe.Offsetof(d.B), unsafe.Sizeof(d.B), unsafe.Alignof(d.B))
	fmt.Printf("  C: offset=%d, size=%d, align=%d\n", unsafe.Offsetof(d.C), unsafe.Sizeof(d.C), unsafe.Alignof(d.C))
	fmt.Printf("  D: offset=%d, size=%d, align=%d\n", unsafe.Offsetof(d.D), unsafe.Sizeof(d.D), unsafe.Alignof(d.D))
	fmt.Printf("  E: offset=%d, size=%d, align=%d\n", unsafe.Offsetof(d.E), unsafe.Sizeof(d.E), unsafe.Alignof(d.E))

	// Verify: total size is rounded up to alignment
	maxAlign := unsafe.Alignof(d)
	lastField := unsafe.Offsetof(d.E) + unsafe.Sizeof(d.E)
	expectedSize := (lastField + maxAlign - 1) &^ (maxAlign - 1)
	fmt.Printf("\n  Last field ends at byte %d\n", lastField)
	fmt.Printf("  Rounded to align=%d: %d bytes\n", maxAlign, expectedSize)
}
```

## Hints

- `unsafe.Sizeof` returns compile-time constant size -- it does not follow pointers or measure slice/map contents
- Alignment rule: field at offset O requires O % alignof(field) == 0
- Struct total size is padded to a multiple of its largest field's alignment
- Sort fields from largest to smallest alignment to minimize padding
- `string` is 16 bytes (pointer + int), `[]T` is 24 bytes (pointer + int + int)
- Empty struct `struct{}` has size 0 but alignment 1 -- Go may share its address with adjacent fields
- The `fieldalignment` tool from `golang.org/x/tools` automatically suggests optimal field ordering

## Verification

- `Wasteful` struct is measurably larger than `Compact` with identical fields
- `unsafe.Offsetof` shows padding gaps between fields
- Raw byte dump reveals zero-valued padding bytes between field data
- Reordering fields by decreasing alignment eliminates most padding
- Total struct size equals last field's offset + size, rounded up to struct alignment

## What's Next

With memory layout understood, the next exercise uses `unsafe.Pointer` to perform type punning -- reinterpreting the bytes of one type as another.

## Summary

`unsafe.Sizeof` returns the in-memory size of a type (not following pointers). `unsafe.Alignof` returns the alignment requirement. `unsafe.Offsetof` returns a struct field's offset from the struct start. Go inserts padding between fields to satisfy alignment constraints, and pads the struct's total size to a multiple of the largest alignment. Reordering fields from largest to smallest alignment minimizes wasted padding. Use the `fieldalignment` tool to automate this optimization.

## Reference

- [unsafe.Sizeof](https://pkg.go.dev/unsafe#Sizeof)
- [unsafe.Alignof](https://pkg.go.dev/unsafe#Alignof)
- [unsafe.Offsetof](https://pkg.go.dev/unsafe#Offsetof)
- [fieldalignment analyzer](https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/fieldalignment)
- [Go spec: Size and alignment guarantees](https://go.dev/ref/spec#Size_and_alignment_guarantees)
