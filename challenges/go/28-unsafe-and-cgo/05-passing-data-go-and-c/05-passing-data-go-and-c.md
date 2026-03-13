# 5. Passing Data Between Go and C

<!--
difficulty: advanced
concepts: [cgo-pointer-passing, go-to-c-data, c-to-go-data, cgo-rules, pinning, slice-to-c]
tools: [go, gcc]
estimated_time: 35m
bloom_level: analyze
prerequisites: [cgo-basics, unsafe-pointer-and-uintptr, memory-layout]
-->

## Prerequisites

- Go 1.22+ installed with a working C compiler
- Completed cgo basics (exercise 4)
- Understanding of Go's garbage collector and memory management

## Learning Objectives

After completing this exercise, you will be able to:

- **Pass** Go slices, structs, and arrays to C functions safely
- **Explain** the cgo pointer-passing rules and why they exist
- **Handle** C-allocated memory that contains results for Go to consume
- **Avoid** the common pitfalls that cause crashes or GC corruption

## Why Passing Data Between Go and C Matters

The hard part of cgo is not calling a C function -- it is getting data across the boundary without crashing, leaking memory, or corrupting the garbage collector. Go's GC can move objects at any time, so passing a Go pointer to C that the GC later relocates causes a dangling pointer. The cgo pointer-passing rules exist to prevent this: Go code may pass a Go pointer to C only if the Go memory it points to does not itself contain any Go pointers. Violating this rule causes a runtime panic (with `GOEXPERIMENT=cgocheck2`) or silent corruption.

## Step 1 -- Passing Primitive Arrays to C

```bash
mkdir -p ~/go-exercises/cgo-passing && cd ~/go-exercises/cgo-passing
go mod init cgo-passing
```

Create `main.go`:

```go
package main

/*
#include <stdint.h>

// Sum an array of int32s
int32_t sum_array(const int32_t* arr, int len) {
    int32_t total = 0;
    for (int i = 0; i < len; i++) {
        total += arr[i];
    }
    return total;
}

// Fill an array with squares
void fill_squares(int32_t* arr, int len) {
    for (int i = 0; i < len; i++) {
        arr[i] = (i + 1) * (i + 1);
    }
}

// Sort an array in place (bubble sort for simplicity)
void sort_array(int32_t* arr, int len) {
    for (int i = 0; i < len - 1; i++) {
        for (int j = 0; j < len - i - 1; j++) {
            if (arr[j] > arr[j+1]) {
                int32_t tmp = arr[j];
                arr[j] = arr[j+1];
                arr[j+1] = tmp;
            }
        }
    }
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func main() {
	// Pass a Go slice to C (safe: int32 contains no Go pointers)
	data := []int32{5, 3, 8, 1, 9, 2, 7}
	sum := C.sum_array(
		(*C.int32_t)(unsafe.Pointer(&data[0])),
		C.int(len(data)),
	)
	fmt.Printf("Sum of %v = %d\n", data, int(sum))

	// Let C fill a Go slice
	squares := make([]int32, 10)
	C.fill_squares(
		(*C.int32_t)(unsafe.Pointer(&squares[0])),
		C.int(len(squares)),
	)
	fmt.Printf("Squares: %v\n", squares)

	// Let C sort a Go slice in place
	unsorted := []int32{42, 17, 93, 5, 67, 31}
	fmt.Printf("Before sort: %v\n", unsorted)
	C.sort_array(
		(*C.int32_t)(unsafe.Pointer(&unsorted[0])),
		C.int(len(unsorted)),
	)
	fmt.Printf("After sort:  %v\n", unsorted)
}
```

```bash
go run main.go
```

### Intermediate Verification

Passing `&data[0]` to C gives C a pointer to the Go slice's backing array. This is safe because `int32` contains no Go pointers. C reads and writes the Go memory directly -- no copying.

## Step 2 -- The Pointer-Passing Rules

Create `rules_test.go`:

```go
package main

import (
	"testing"
	"unsafe"
)

/*
#include <stdint.h>

// Accepts a pointer to a struct
typedef struct {
    int32_t x;
    int32_t y;
} CPoint;

int32_t point_sum(const CPoint* p) {
    return p->x + p->y;
}

// Accepts a pointer to a pointer (double pointer)
void set_value(int32_t** out, int32_t* val) {
    *out = val;
}
*/
import "C"

func TestPassStructToC(t *testing.T) {
	// SAFE: struct contains only C-compatible types (no Go pointers)
	p := C.CPoint{x: 10, y: 20}
	result := C.point_sum(&p)
	if int(result) != 30 {
		t.Errorf("expected 30, got %d", int(result))
	}
	t.Logf("point_sum({10, 20}) = %d", int(result))
}

func TestPassGoStructToC(t *testing.T) {
	// SAFE: Go struct with only primitive fields
	type GoPoint struct {
		X, Y int32
	}
	gp := GoPoint{X: 5, Y: 7}
	result := C.point_sum((*C.CPoint)(unsafe.Pointer(&gp)))
	if int(result) != 12 {
		t.Errorf("expected 12, got %d", int(result))
	}
	t.Logf("Go struct passed to C: sum = %d", int(result))
}

// UNSAFE EXAMPLE (commented out -- would panic with cgo pointer checks):
//
// func TestPassGoPointerInStruct(t *testing.T) {
//     // This would VIOLATE the rules: Go memory containing Go pointers
//     type Bad struct {
//         Data *int32  // Go pointer inside struct
//     }
//     x := int32(42)
//     b := Bad{Data: &x}
//     // Passing &b to C is ILLEGAL because b contains a Go pointer (&x)
//     // Runtime will panic: "cgo argument has Go pointer to Go pointer"
// }

func TestCAllocatedMemory(t *testing.T) {
	// C-allocated memory can contain anything -- no GC tracking
	p := (*C.CPoint)(C.malloc(C.ulong(unsafe.Sizeof(C.CPoint{}))))
	if p == nil {
		t.Fatal("malloc failed")
	}
	defer C.free(unsafe.Pointer(p))

	p.x = 100
	p.y = 200
	result := C.point_sum(p)
	t.Logf("C-allocated point: sum = %d", int(result))
}
```

```bash
go test -v
```

## Step 3 -- Passing Strings and Byte Slices

```go
package main

/*
#include <stdlib.h>
#include <string.h>

// Process a byte buffer: count occurrences of a byte
int count_byte(const unsigned char* data, int len, unsigned char target) {
    int count = 0;
    for (int i = 0; i < len; i++) {
        if (data[i] == target) count++;
    }
    return count;
}

// Write result into a caller-provided buffer
int format_result(char* buf, int buflen, int value) {
    return snprintf(buf, buflen, "Result: %d", value);
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func main() {
	// Pass Go []byte to C
	data := []byte("hello, world! hello, cgo!")
	count := C.count_byte(
		(*C.uchar)(unsafe.Pointer(&data[0])),
		C.int(len(data)),
		C.uchar('l'),
	)
	fmt.Printf("Count of 'l' in %q: %d\n", string(data), int(count))

	// Let C write into a Go byte slice
	buf := make([]byte, 64)
	n := C.format_result(
		(*C.char)(unsafe.Pointer(&buf[0])),
		C.int(len(buf)),
		C.int(42),
	)
	result := string(buf[:n])
	fmt.Printf("C formatted: %q\n", result)

	// Alternative: using C.CString for null-terminated strings
	// (allocates C memory -- must free)
	cstr := C.CString("search in this string")
	defer C.free(unsafe.Pointer(cstr))
	cstrLen := C.int(C.strlen(cstr))
	fmt.Printf("C strlen: %d\n", int(cstrLen))
}
```

## Step 4 -- Returning Allocated Data from C

When C allocates memory that Go needs to consume:

```go
package main

/*
#include <stdlib.h>
#include <string.h>

// C function that allocates and returns data
// Caller is responsible for freeing the returned pointer
char* generate_greeting(const char* name) {
    const char* prefix = "Hello, ";
    const char* suffix = "!";
    int len = strlen(prefix) + strlen(name) + strlen(suffix) + 1;
    char* result = (char*)malloc(len);
    if (!result) return NULL;
    snprintf(result, len, "%s%s%s", prefix, name, suffix);
    return result;
}

// C function that fills a caller-provided struct
typedef struct {
    int32_t values[4];
    int32_t count;
} Result;

void compute(Result* out) {
    out->values[0] = 10;
    out->values[1] = 20;
    out->values[2] = 30;
    out->values[3] = 40;
    out->count = 4;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func main() {
	// C allocates, Go consumes and frees
	name := C.CString("Gopher")
	greeting := C.generate_greeting(name)
	C.free(unsafe.Pointer(name))

	if greeting == nil {
		panic("C allocation failed")
	}
	defer C.free(unsafe.Pointer(greeting))

	goGreeting := C.GoString(greeting)
	fmt.Println(goGreeting)

	// C fills a Go-allocated struct (no Go pointers in struct)
	var result C.Result
	C.compute(&result)
	fmt.Printf("Result: count=%d, values=[", int(result.count))
	for i := 0; i < int(result.count); i++ {
		if i > 0 {
			fmt.Print(", ")
		}
		fmt.Print(int(result.values[i]))
	}
	fmt.Println("]")
}
```

## Hints

- Go slices of primitive types (`[]int32`, `[]byte`, `[]float64`) can be passed to C via `&slice[0]`
- NEVER pass `&slice[0]` for an empty slice -- check `len(slice) > 0` first
- Go structs containing only primitive types can be passed to C if the layout matches
- Go structs containing Go pointers (strings, slices, interfaces, maps, channels, function values) CANNOT be passed to C
- C-allocated memory (`C.malloc`) is invisible to the GC -- you must free it manually
- Use `GOEXPERIMENT=cgocheck2` to enable strict pointer-passing checks during development
- `C.CBytes(goSlice)` allocates C memory and copies Go bytes into it -- always safe, but has copy overhead
- When C returns a pointer, document whether the caller or callee owns the memory

## Verification

- `sum_array` correctly sums a Go `[]int32` passed to C
- `fill_squares` writes values into a Go slice from C -- the Go slice reflects the changes
- Sorting a Go slice via C modifies the original slice in place
- Go struct with matching layout passes to C function correctly
- C-allocated memory is freed via `defer C.free(unsafe.Pointer(p))`
- Byte counting and string formatting across the boundary produce correct results

## What's Next

With data passing understood, the next exercise measures the performance overhead of cgo calls to determine when the boundary-crossing cost justifies using pure Go instead.

## Summary

Passing data between Go and C requires understanding the pointer-passing rules: Go memory passed to C must not contain Go pointers. Primitive slices (`[]int32`, `[]byte`) are safe to pass via `&slice[0]`. Strings require `C.CString` (allocates) or `C.GoString` (copies). C-allocated memory must be manually freed. Go structs with compatible layouts can be cast to C struct pointers. Use `C.CBytes` for safe (copying) byte transfers. Always check slice length before passing `&slice[0]` to avoid nil pointer dereferences.

## Reference

- [cgo pointer-passing rules](https://pkg.go.dev/cmd/cgo#hdr-Passing_pointers)
- [cgo documentation](https://pkg.go.dev/cmd/cgo)
- [Go wiki: cgo](https://go.dev/wiki/cgo)
