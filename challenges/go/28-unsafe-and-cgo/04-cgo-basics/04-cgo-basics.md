# 4. cgo Basics

<!--
difficulty: advanced
concepts: [cgo, calling-c-from-go, import-c, cgo-comment, c-types, linking]
tools: [go, gcc]
estimated_time: 35m
bloom_level: apply
prerequisites: [unsafe-pointer-and-uintptr, c-basics]
-->

## Prerequisites

- Go 1.22+ installed
- A C compiler installed (gcc or clang -- `cc --version` should work)
- Basic knowledge of C syntax (functions, headers, types)
- Understanding of `unsafe.Pointer` from exercise 1

## Learning Objectives

After completing this exercise, you will be able to:

- **Call** C functions from Go using the `import "C"` pseudo-package
- **Use** the cgo preamble comment to define C code inline
- **Convert** between Go and C types (strings, ints, pointers)
- **Link** against system C libraries from Go

## Why cgo

Go cannot do everything. When you need to use a C library (OpenSSL, SQLite, libcurl, GPU drivers), cgo is the bridge. The `import "C"` pseudo-package lets Go code call C functions, access C types, and include C headers. Understanding cgo basics is essential for wrapping existing C libraries and for understanding performance-sensitive Go projects that drop into C for critical sections.

cgo works by generating C and Go glue code at compile time. When you `import "C"`, the Go compiler invokes a C compiler to build the C portions, then links everything together. The magic comment immediately above `import "C"` (the cgo preamble) is passed directly to the C compiler.

## Step 1 -- Hello cgo

```bash
mkdir -p ~/go-exercises/cgo-basics && cd ~/go-exercises/cgo-basics
go mod init cgo-basics
```

Create `main.go`:

```go
package main

/*
#include <stdio.h>
#include <stdlib.h>

void hello_from_c(const char* name) {
    printf("Hello from C, %s!\n", name);
}

int add(int a, int b) {
    return a + b;
}

double divide(double a, double b) {
    if (b == 0.0) return 0.0;
    return a / b;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func main() {
	// Call a void C function with a string argument
	name := C.CString("Gopher")
	defer C.free(unsafe.Pointer(name))
	C.hello_from_c(name)

	// Call a C function that returns a value
	result := C.add(C.int(3), C.int(4))
	fmt.Printf("C add(3, 4) = %d\n", int(result))

	// Call with floating-point types
	quotient := C.divide(C.double(22.0), C.double(7.0))
	fmt.Printf("C divide(22, 7) = %f\n", float64(quotient))
}
```

```bash
go run main.go
```

### Intermediate Verification

The output shows a printf from C and two computed results from C functions. The key observation: there must be NO blank line between the closing `*/` and `import "C"`. Any blank line breaks the cgo preamble.

## Step 2 -- C Types and Go Types

```go
package main

/*
#include <stdint.h>
#include <stdbool.h>

typedef struct {
    int32_t x;
    int32_t y;
} Point;

Point make_point(int32_t x, int32_t y) {
    Point p = {x, y};
    return p;
}

int32_t manhattan_distance(Point a, Point b) {
    int32_t dx = a.x - b.x;
    int32_t dy = a.y - b.y;
    if (dx < 0) dx = -dx;
    if (dy < 0) dy = -dy;
    return dx + dy;
}
*/
import "C"

import "fmt"

func main() {
	// C structs are accessible as C.Point
	p1 := C.make_point(1, 2)
	p2 := C.make_point(4, 6)

	fmt.Printf("Point 1: (%d, %d)\n", p1.x, p1.y)
	fmt.Printf("Point 2: (%d, %d)\n", p2.x, p2.y)

	dist := C.manhattan_distance(p1, p2)
	fmt.Printf("Manhattan distance: %d\n", int(dist))

	// Direct struct construction from Go
	p3 := C.Point{x: 10, y: 20}
	fmt.Printf("Point 3: (%d, %d)\n", p3.x, p3.y)

	// Type conversions
	fmt.Println("\n=== Type Sizes ===")
	fmt.Printf("C.int:     Go equivalent is int32 (on most platforms)\n")
	fmt.Printf("C.long:    platform-dependent\n")
	fmt.Printf("C.char:    Go equivalent is byte (C.char)\n")
	fmt.Printf("C.float:   Go equivalent is float32\n")
	fmt.Printf("C.double:  Go equivalent is float64\n")

	// C int <-> Go int conversion
	var goInt int = 42
	cInt := C.int(goInt)
	backToGo := int(cInt)
	fmt.Printf("Go %d -> C %d -> Go %d\n", goInt, cInt, backToGo)
}
```

## Step 3 -- String Conversion

String handling between Go and C requires explicit conversion and memory management:

```go
package main

/*
#include <stdlib.h>
#include <string.h>

// Returns length of a C string
int string_length(const char* s) {
    return (int)strlen(s);
}

// Modifies a C string in place
void to_upper(char* s) {
    for (int i = 0; s[i]; i++) {
        if (s[i] >= 'a' && s[i] <= 'z') {
            s[i] -= 32;
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
	// Go string -> C string (allocates C memory, MUST be freed)
	goStr := "hello, cgo"
	cStr := C.CString(goStr)
	defer C.free(unsafe.Pointer(cStr))

	length := C.string_length(cStr)
	fmt.Printf("C strlen(%q) = %d\n", goStr, int(length))

	// Modify the C string in place
	C.to_upper(cStr)

	// C string -> Go string (copies the bytes, safe to use after free)
	result := C.GoString(cStr)
	fmt.Printf("After to_upper: %q\n", result)

	// C.GoStringN for strings with known length (non-null-terminated)
	partial := C.GoStringN(cStr, 5)
	fmt.Printf("First 5 chars: %q\n", partial)

	// Go []byte -> C bytes
	goBytes := []byte{0x48, 0x65, 0x6C, 0x6C, 0x6F}
	cBytes := C.CBytes(goBytes)
	defer C.free(cBytes)
	fmt.Printf("C bytes from Go: length=%d\n", len(goBytes))

	// C bytes -> Go []byte
	goBytesCopy := C.GoBytes(cBytes, C.int(len(goBytes)))
	fmt.Printf("Go bytes from C: %v = %q\n", goBytesCopy, string(goBytesCopy))
}
```

## Step 4 -- Linking System Libraries

Link against the system math library:

```go
package main

/*
#cgo LDFLAGS: -lm
#include <math.h>
*/
import "C"

import "fmt"

func main() {
	// Call C math library functions
	fmt.Printf("C sqrt(144) = %f\n", float64(C.sqrt(C.double(144))))
	fmt.Printf("C sin(pi/2) = %f\n", float64(C.sin(C.double(3.14159265/2.0))))
	fmt.Printf("C pow(2, 10) = %f\n", float64(C.pow(C.double(2), C.double(10))))
	fmt.Printf("C log(e) = %f\n", float64(C.log(C.double(2.71828182845))))
}
```

### Intermediate Verification

The `#cgo LDFLAGS: -lm` directive tells the linker to include libm. Without it, linking fails with undefined symbol errors. On macOS, libm is included by default; on Linux, the explicit flag is required.

## Hints

- The cgo preamble (comment above `import "C"`) MUST have no blank line before `import "C"`
- `C.CString` allocates C memory -- you MUST call `C.free` to avoid memory leaks
- `C.GoString` copies C string data into Go-managed memory -- safe to use after the C string is freed
- C `int` is not Go `int`: use `C.int(x)` and `int(cResult)` for explicit conversion
- `#cgo LDFLAGS: -lfoo` links against `libfoo.so` or `libfoo.a`
- `#cgo CFLAGS: -I/path` adds include directories for headers
- `CGO_ENABLED=1` must be set (it is the default) -- cross-compilation with cgo requires a C cross-compiler
- Build with `go build -x` to see the C compiler invocations

## Verification

- Inline C functions callable from Go with correct results
- C structs accessible from Go with field read/write
- `C.CString`/`C.GoString` round-trip preserves string content
- Memory allocated by `C.CString` is freed with `C.free`
- System library linking with `#cgo LDFLAGS` works for libm functions
- Type conversions between C and Go numeric types produce correct values

## What's Next

With basic cgo working, the next exercise covers the complexities of passing data between Go and C -- including slices, structs with pointers, and the cgo pointer passing rules.

## Summary

cgo enables calling C from Go via `import "C"`. The preamble comment before `import "C"` can include C code, `#include` directives, and `#cgo` build flags. C types are accessed through the `C` pseudo-package (`C.int`, `C.double`, `C.Point`). Strings require explicit conversion: `C.CString` (Go to C, allocates), `C.GoString` (C to Go, copies). Always free C-allocated memory with `C.free`. Use `#cgo LDFLAGS` and `#cgo CFLAGS` to control linking and include paths. cgo adds build complexity and disables cross-compilation by default.

## Reference

- [cgo documentation](https://pkg.go.dev/cmd/cgo)
- [cgo wiki](https://go.dev/wiki/cgo)
- [C? Go? Cgo!](https://go.dev/blog/cgo) -- official blog post
