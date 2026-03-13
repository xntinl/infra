# 1. unsafe.Pointer and uintptr

<!--
difficulty: advanced
concepts: [unsafe-pointer, uintptr, pointer-arithmetic, type-conversion, gc-safety]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [pointers, memory-layout, type-system]
-->

## Prerequisites

- Go 1.22+ installed
- Solid understanding of Go pointers and the type system
- Familiarity with how memory is laid out for structs and arrays

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** `unsafe.Pointer` to convert between incompatible pointer types
- **Distinguish** between `unsafe.Pointer` and `uintptr` and their GC implications
- **Apply** the six legal `unsafe.Pointer` conversion patterns from the Go specification
- **Identify** unsafe pointer operations that are incorrect and why they break

## Why unsafe.Pointer and uintptr

Go's type system prevents you from converting between unrelated pointer types. `unsafe.Pointer` is the escape hatch: it is a pointer type that can be converted to and from any other pointer type, and to and from `uintptr`. This is the foundation of all `unsafe` operations in Go -- type punning, pointer arithmetic, and C interop all flow through `unsafe.Pointer`.

The critical distinction is between `unsafe.Pointer` and `uintptr`. An `unsafe.Pointer` is a real pointer that the garbage collector tracks. A `uintptr` is just an integer -- the GC does not treat it as a reference, so the object it points to can be moved or collected. This means that storing a `uintptr` and converting it back to a pointer later is unsafe: the object may have moved. You must perform pointer arithmetic in a single expression.

## Step 1 -- Basic Pointer Conversion

```bash
mkdir -p ~/go-exercises/unsafe-pointer && cd ~/go-exercises/unsafe-pointer
go mod init unsafe-pointer
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"unsafe"
)

func main() {
	// Convert *int to *float64 (same size, reinterpret bits)
	x := uint64(0x4059000000000000) // IEEE 754 encoding of 100.0
	f := *(*float64)(unsafe.Pointer(&x))
	fmt.Printf("uint64 0x%x as float64: %f\n", x, f)

	// Convert *float64 back to *uint64
	y := 3.14
	bits := *(*uint64)(unsafe.Pointer(&y))
	fmt.Printf("float64 %f as uint64: 0x%x\n", y, bits)

	// Convert between struct pointer types
	type RGB struct{ R, G, B uint8 }
	type BGR struct{ B, G, R uint8 }

	rgb := RGB{R: 255, G: 128, B: 0}
	bgr := *(*BGR)(unsafe.Pointer(&rgb))
	fmt.Printf("RGB{%d,%d,%d} as BGR: {B:%d, G:%d, R:%d}\n",
		rgb.R, rgb.G, rgb.B, bgr.B, bgr.G, bgr.R)
}
```

```bash
go run main.go
```

### Intermediate Verification

The `uint64` bit pattern `0x4059000000000000` reinterprets as `100.0` when read as `float64`. The RGB-to-BGR conversion swaps the field interpretation because both structs have the same memory layout (three contiguous bytes) but different field names.

## Step 2 -- The Six Legal Patterns

The Go specification defines exactly six legal uses of `unsafe.Pointer`. Create `patterns.go`:

```go
package main

import (
	"fmt"
	"math"
	"unsafe"
)

// Pattern 1: Conversion of *T1 to Pointer to *T2
// T1 and T2 must share the same memory layout
func pattern1() {
	i := int64(42)
	u := (*uint64)(unsafe.Pointer(&i))
	fmt.Printf("Pattern 1: int64(%d) as uint64(%d)\n", i, *u)
}

// Pattern 2: Conversion of Pointer to uintptr (but not back!)
// Only valid for printing or passing to syscalls
func pattern2() {
	x := 42
	addr := uintptr(unsafe.Pointer(&x))
	fmt.Printf("Pattern 2: address of x = 0x%x\n", addr)
	// WARNING: do NOT convert addr back to a pointer in a separate statement
}

// Pattern 3: Conversion of Pointer to uintptr and back, with arithmetic
// Must be in a SINGLE expression
func pattern3() {
	type Pair struct {
		A int64
		B int64
	}
	p := Pair{A: 10, B: 20}

	// Get pointer to B by adding the offset of B to the base pointer
	bPtr := (*int64)(unsafe.Pointer(
		uintptr(unsafe.Pointer(&p)) + unsafe.Offsetof(p.B),
	))
	fmt.Printf("Pattern 3: p.B via pointer arithmetic = %d\n", *bPtr)
}

// Pattern 4: Syscall-related (Syscall passes uintptr args as pointers)
// Demonstrated conceptually -- actual syscalls are OS-dependent.

// Pattern 5: reflect.Value.Pointer / reflect.Value.UnsafeAddr to Pointer
// Demonstrated conceptually.

// Pattern 6: Conversion of reflect.SliceHeader/StringHeader Data to Pointer
// Deprecated in Go 1.20+ in favor of unsafe.Slice and unsafe.String

func main() {
	pattern1()
	pattern2()
	pattern3()

	// Demonstrate math.Float64bits/Float64frombits -- the safe alternative
	f := 3.14
	bits := math.Float64bits(f)
	back := math.Float64frombits(bits)
	fmt.Printf("Safe alternative: %f -> 0x%x -> %f\n", f, bits, back)
}
```

```bash
go run patterns.go main.go
```

## Step 3 -- The uintptr Danger

Create `danger_test.go` to demonstrate why splitting uintptr conversion is wrong:

```go
package main

import (
	"runtime"
	"testing"
	"unsafe"
)

func TestUintptrDanger(t *testing.T) {
	// CORRECT: single expression (GC cannot move object mid-expression)
	type Data struct{ Value int }
	d := &Data{Value: 42}
	vPtr := (*int)(unsafe.Pointer(
		uintptr(unsafe.Pointer(d)) + unsafe.Offsetof(d.Value),
	))
	t.Logf("Correct: Value = %d", *vPtr)

	// INCORRECT (shown for education -- do NOT do this in real code):
	// addr := uintptr(unsafe.Pointer(d)) // d could be moved after this line
	// runtime.GC()                        // GC could relocate d
	// badPtr := (*int)(unsafe.Pointer(addr)) // addr may be stale
	// The above sequence is UNDEFINED BEHAVIOR

	// Demonstrate that uintptr does not keep objects alive
	var addr uintptr
	func() {
		local := &Data{Value: 99}
		addr = uintptr(unsafe.Pointer(local))
		// local is no longer referenced after this function returns
	}()
	runtime.GC() // local's memory may be reclaimed
	t.Logf("Stale uintptr: 0x%x (object may be collected)", addr)
	// Dereferencing addr here would be UNDEFINED BEHAVIOR
}
```

```bash
go test -v -run TestUintptr
```

## Step 4 -- Accessing Unexported Fields

One practical use of `unsafe.Pointer` is accessing unexported struct fields (for debugging, testing, or interop):

```go
package main

import (
	"fmt"
	"reflect"
	"unsafe"
)

type secret struct {
	public  int
	private int // unexported
}

func main() {
	s := secret{public: 1, private: 42}

	// reflect cannot read unexported fields normally
	v := reflect.ValueOf(s)
	field := v.Field(1)
	fmt.Println("CanInterface:", field.CanInterface()) // false

	// unsafe can access any field via offset
	privatePtr := (*int)(unsafe.Pointer(
		uintptr(unsafe.Pointer(&s)) + unsafe.Offsetof(s.private),
	))
	fmt.Println("private field via unsafe:", *privatePtr) // 42

	// Modify the unexported field
	*privatePtr = 100
	fmt.Println("modified private:", s)
}
```

## Hints

- `unsafe.Pointer` is tracked by the GC; `uintptr` is not -- never store a `uintptr` and convert back later
- Pointer arithmetic must happen in a single expression: `(*T)(unsafe.Pointer(uintptr(unsafe.Pointer(&x)) + offset))`
- `unsafe.Offsetof` returns the offset of a struct field from the struct's start address
- `unsafe.Pointer(nil)` is valid and converts to any `*T` as nil
- `go vet` catches some (but not all) misuse of `unsafe.Pointer` -- always run it

## Verification

- `*(*float64)(unsafe.Pointer(&uint64val))` correctly reinterprets the bit pattern
- `unsafe.Offsetof` produces correct offsets for accessing struct fields via pointer arithmetic
- Storing a `uintptr` and converting back in a separate statement is identified as dangerous
- `unsafe.Pointer` conversions between same-size types work without data corruption
- Accessing unexported struct fields via `unsafe.Pointer` + `Offsetof` works correctly

## What's Next

Now that you understand `unsafe.Pointer` conversions, the next exercise explores `unsafe.Sizeof`, `unsafe.Alignof`, and `unsafe.Offsetof` to understand Go's memory layout rules.

## Summary

`unsafe.Pointer` is Go's universal pointer type: it can be converted to/from any `*T` and to/from `uintptr`. The GC tracks `unsafe.Pointer` but not `uintptr`, so pointer arithmetic must happen in a single expression to prevent the GC from invalidating the address. The Go spec defines six legal usage patterns. Common uses include type punning (reinterpreting bits), pointer arithmetic to access struct fields at offsets, and accessing unexported fields. Always prefer safe alternatives (`math.Float64bits`, `encoding/binary`) when they exist.

## Reference

- [unsafe package](https://pkg.go.dev/unsafe)
- [unsafe.Pointer rules](https://pkg.go.dev/unsafe#Pointer)
- [Go spec: Package unsafe](https://go.dev/ref/spec#Package_unsafe)
