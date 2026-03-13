# 3. Basic Types

<!--
difficulty: basic
concepts: [int, float64, bool, string, byte, rune, numeric-types, type-sizes]
tools: [go]
estimated_time: 20m
bloom_level: understand
prerequisites: [02-zero-values-and-default-initialization]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor
- Completed [02 - Zero Values and Default Initialization](../02-zero-values-and-default-initialization/02-zero-values-and-default-initialization.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **List** Go's basic types and their sizes
- **Choose** the appropriate numeric type for a given use case
- **Distinguish** between `byte` and `rune` and explain when to use each
- **Use** `string` as a read-only byte slice and understand its immutability

## Why Basic Types

Go has a small set of built-in types that cover the vast majority of use cases. Unlike languages with implicit coercion, Go requires explicit type conversions between numeric types. This prevents subtle bugs where a float silently truncates to an int or an overflow goes unnoticed.

Understanding the exact set of types -- their sizes, their defaults, and their relationships -- lets you write correct, efficient code. The `byte` and `rune` aliases exist for clarity: `byte` signals you are working with raw data, and `rune` signals you are working with a Unicode code point.

## Step 1 -- Integer Types

```bash
mkdir -p ~/go-exercises/basic-types
cd ~/go-exercises/basic-types
go mod init basic-types
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"math"
	"unsafe"
)

func main() {
	// Sized integers
	var i8 int8 = 127
	var i16 int16 = 32767
	var i32 int32 = 2147483647
	var i64 int64 = 9223372036854775807

	// Unsigned integers
	var u8 uint8 = 255
	var u16 uint16 = 65535

	// Platform-dependent int (32 or 64 bit)
	var i int = 42

	fmt.Printf("int8:   %d  (size: %d bytes, max: %d)\n", i8, unsafe.Sizeof(i8), math.MaxInt8)
	fmt.Printf("int16:  %d  (size: %d bytes, max: %d)\n", i16, unsafe.Sizeof(i16), math.MaxInt16)
	fmt.Printf("int32:  %d  (size: %d bytes)\n", i32, unsafe.Sizeof(i32))
	fmt.Printf("int64:  %d  (size: %d bytes)\n", i64, unsafe.Sizeof(i64))
	fmt.Printf("uint8:  %d  (size: %d bytes, max: %d)\n", u8, unsafe.Sizeof(u8), math.MaxUint8)
	fmt.Printf("uint16: %d  (size: %d bytes, max: %d)\n", u16, unsafe.Sizeof(u16), math.MaxUint16)
	fmt.Printf("int:    %d  (size: %d bytes)\n", i, unsafe.Sizeof(i))
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/basic-types && go run main.go
```

Expected (on a 64-bit system):

```
int8:   127  (size: 1 bytes, max: 127)
int16:  32767  (size: 2 bytes, max: 32767)
int32:  2147483647  (size: 4 bytes)
int64:  9223372036854775807  (size: 8 bytes)
uint8:  255  (size: 1 bytes, max: 255)
uint16: 65535  (size: 2 bytes, max: 65535)
int:    42  (size: 8 bytes)
```

## Step 2 -- Float and Bool Types

Replace `main.go`:

```go
package main

import (
	"fmt"
	"math"
)

func main() {
	// Floating point
	var f32 float32 = 3.14
	var f64 float64 = 3.141592653589793

	fmt.Printf("float32: %.10f (max: %e)\n", f32, math.MaxFloat32)
	fmt.Printf("float64: %.15f (max: %e)\n", f64, math.MaxFloat64)

	// Boolean
	var ready bool = true
	var done bool = false

	fmt.Printf("ready: %t, done: %t\n", ready, done)
	fmt.Printf("AND: %t, OR: %t, NOT: %t\n", ready && done, ready || done, !ready)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/basic-types && go run main.go
```

Expected:

```
float32: 3.1400001049 (max: 3.402823e+38)
float64: 3.141592653589793 (max: 1.797693e+308)
ready: true, done: false
AND: false, OR: true, NOT: false
```

## Step 3 -- Strings, Bytes, and Runes

Replace `main.go`:

```go
package main

import "fmt"

func main() {
	// Strings are immutable sequences of bytes
	s := "Hello, Go!"
	fmt.Printf("string: %s (len: %d bytes)\n", s, len(s))

	// byte is an alias for uint8
	var b byte = 'A'
	fmt.Printf("byte: %c (value: %d, type: %T)\n", b, b, b)

	// rune is an alias for int32 -- represents a Unicode code point
	var r rune = 'G'
	fmt.Printf("rune: %c (value: %d, Unicode: U+%04X, type: %T)\n", r, r, r, r)

	// Multi-byte characters
	emoji := "Go es genial"
	fmt.Printf("string: %s\n", emoji)
	fmt.Printf("bytes:  %d\n", len(emoji))
	fmt.Printf("runes:  %d\n", len([]rune(emoji)))

	// Iterating by byte vs by rune
	word := "cafe\u0301" // "cafe" + combining acute accent = "cafe" (5 runes, 6 bytes)
	fmt.Printf("\nword: %s\n", word)
	fmt.Println("Bytes:")
	for i := 0; i < len(word); i++ {
		fmt.Printf("  [%d] %x\n", i, word[i])
	}
	fmt.Println("Runes:")
	for i, r := range word {
		fmt.Printf("  [%d] %c (U+%04X)\n", i, r, r)
	}
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/basic-types && go run main.go
```

Expected:

```
string: Hello, Go! (len: 10 bytes)
byte: A (value: 65, type: uint8)
rune: G (value: 71, Unicode: U+0047, type: int32)
string: Go es genial
bytes:  12
runes:  12
```

The `cafe\u0301` example shows that byte count and rune count can differ for multi-byte Unicode.

## Step 4 -- Raw Strings and String Operations

Replace `main.go`:

```go
package main

import "fmt"

func main() {
	// Interpreted string literal (escape sequences processed)
	interpreted := "line1\nline2\ttab"
	fmt.Println("Interpreted:")
	fmt.Println(interpreted)

	// Raw string literal (backslash is literal, can span lines)
	raw := `line1\nline2\ttab`
	fmt.Println("\nRaw:")
	fmt.Println(raw)

	// String concatenation
	first := "Hello"
	second := "World"
	full := first + ", " + second + "!"
	fmt.Println("\nConcatenated:", full)

	// Strings are immutable
	s := "hello"
	// s[0] = 'H' // compile error: cannot assign to s[0]
	upper := "H" + s[1:]
	fmt.Println("Modified copy:", upper)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/basic-types && go run main.go
```

Expected:

```
Interpreted:
line1
line2	tab

Raw:
line1\nline2\ttab

Concatenated: Hello, World!
Modified copy: Hello
```

## Common Mistakes

### Confusing `byte` and `rune`

**Wrong:** Using `byte` to process Unicode text character by character.

**What happens:** Multi-byte characters like `e` span multiple bytes. Indexing by byte splits them incorrectly.

**Fix:** Use `rune` (or `range` over a string) when you need to process characters. Use `byte` only for raw binary data or ASCII-only text.

### Assuming `len(s)` Returns Character Count

**Wrong:** `len("cafe\u0301")` expecting 5.

**What happens:** `len` returns the byte count, not the rune count. Multi-byte UTF-8 characters increase the byte count beyond the rune count.

**Fix:** Use `len([]rune(s))` or `utf8.RuneCountInString(s)` for character count.

## Verify What You Learned

```bash
cd ~/go-exercises/basic-types && go run main.go
```

Experiment by declaring variables of each type and printing them with `%T` to confirm their types.

## What's Next

Continue to [04 - Constants and Iota](../04-constants-and-iota/04-constants-and-iota.md) to learn how Go handles compile-time constants and enumerated values.

## Summary

- Go's integer types: `int8`, `int16`, `int32`, `int64`, and unsigned variants; `int` is platform-dependent
- `float32` has ~7 digits of precision; `float64` has ~15 digits
- `bool` has two values: `true` and `false`
- `string` is an immutable sequence of bytes, typically UTF-8
- `byte` is an alias for `uint8`; `rune` is an alias for `int32`
- `len(s)` returns bytes, not characters -- use `[]rune(s)` for rune count
- Raw string literals use backticks and preserve literal content

## Reference

- [Go Specification: Types](https://go.dev/ref/spec#Types)
- [Go Specification: Numeric Types](https://go.dev/ref/spec#Numeric_types)
- [Go Blog: Strings, bytes, runes and characters in Go](https://go.dev/blog/strings)
