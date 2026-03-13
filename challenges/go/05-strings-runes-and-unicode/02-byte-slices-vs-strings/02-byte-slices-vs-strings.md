# 2. Byte Slices vs Strings

<!--
difficulty: basic
concepts: [byte-slice, string-conversion, mutability, io-operations, zero-copy]
tools: [go]
estimated_time: 20m
bloom_level: understand
prerequisites: [string-basics, variables-and-types]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercise 01 (String Basics)
- Understanding of slices at a conceptual level

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the relationship between `string` and `[]byte` in Go
- **Convert** between strings and byte slices and understand the cost
- **Identify** when to use `[]byte` instead of `string`

## Why Byte Slices vs Strings

Go's I/O interfaces (`io.Reader`, `io.Writer`) work with `[]byte`, not `string`. Network sockets, files, and HTTP bodies all produce and consume byte slices. Meanwhile, most text-processing functions operate on `string`. You constantly bridge these two worlds, so understanding the conversion cost and the semantic difference is essential.

The key distinction: strings are immutable and safe to share across goroutines. Byte slices are mutable and can be modified in place. Converting between them copies the data, which matters in performance-critical code.

## Step 1 -- Converting Between String and []byte

```bash
mkdir -p ~/go-exercises/bytes-vs-strings
cd ~/go-exercises/bytes-vs-strings
go mod init bytes-vs-strings
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	// String to []byte
	s := "Hello, Go!"
	b := []byte(s)
	fmt.Printf("string: %q\n", s)
	fmt.Printf("[]byte: %v\n", b)
	fmt.Printf("[]byte as string: %q\n", string(b))

	// Modifying the byte slice does NOT affect the original string
	b[0] = 'h'
	fmt.Printf("Modified []byte: %q\n", string(b))
	fmt.Printf("Original string: %q\n", s)
}
```

The conversion `[]byte(s)` copies the string's bytes into a new slice. Modifying the slice has no effect on the original string.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
string: "Hello, Go!"
[]byte: [72 101 108 108 111 44 32 71 111 33]
[]byte as string: "Hello, Go!"
Modified []byte: "hello, Go!"
Original string: "Hello, Go!"
```

## Step 2 -- Individual Bytes and Characters

```go
package main

import "fmt"

func main() {
	s := "ABC"

	// Indexing a string gives a byte, not a character
	fmt.Printf("s[0] = %d (type: %T)\n", s[0], s[0])
	fmt.Printf("s[0] as char = %c\n", s[0])

	// This matters for multi-byte characters
	multi := "é"
	fmt.Printf("len(%q) = %d\n", multi, len(multi))
	fmt.Printf("bytes: ")
	for i := 0; i < len(multi); i++ {
		fmt.Printf("%02x ", multi[i])
	}
	fmt.Println()
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
s[0] = 65 (type: uint8)
s[0] as char = A
len("é") = 2
bytes: c3 a9
```

## Step 3 -- []byte for In-Place Modification

When you need to modify text character by character, work with `[]byte`:

```go
package main

import "fmt"

func toUpperASCII(b []byte) {
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32 // ASCII lowercase to uppercase offset
		}
	}
}

func main() {
	data := []byte("hello, world!")
	toUpperASCII(data)
	fmt.Println(string(data))

	// Compare: with strings, you would need to create a new string
	s := "hello again!"
	b := []byte(s)
	toUpperASCII(b)
	result := string(b) // another copy
	fmt.Println(result)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
HELLO, WORLD!
HELLO AGAIN!
```

## Step 4 -- When to Use Which

```go
package main

import (
	"bytes"
	"fmt"
	"strings"
)

func main() {
	// strings package works with string
	fmt.Println(strings.ToUpper("hello"))
	fmt.Println(strings.Contains("seafood", "foo"))

	// bytes package provides the same functions for []byte
	b := []byte("hello")
	fmt.Println(string(bytes.ToUpper(b)))
	fmt.Println(bytes.Contains([]byte("seafood"), []byte("foo")))

	// bytes.Buffer builds byte sequences efficiently
	var buf bytes.Buffer
	buf.WriteString("Hello, ")
	buf.WriteString("World!")
	fmt.Println(buf.String())
}
```

Use `string` when you need text that will not change: map keys, function parameters, return values. Use `[]byte` when you need to mutate data, work with I/O, or build content incrementally.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
HELLO
true
HELLO
true
Hello, World!
```

## Step 5 -- Conversion Cost

```go
package main

import (
	"fmt"
	"unsafe"
)

func main() {
	s := "Hello"

	// Each conversion allocates and copies
	b1 := []byte(s)
	b2 := []byte(s)

	// b1 and b2 are independent copies
	b1[0] = 'J'
	fmt.Printf("b1=%q b2=%q s=%q\n", b1, b2, s)

	// String header: pointer + length (no capacity)
	// Slice header: pointer + length + capacity
	fmt.Printf("Size of string header: %d bytes\n", unsafe.Sizeof(s))
	fmt.Printf("Size of []byte header: %d bytes\n", unsafe.Sizeof(b1))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
b1="Jello" b2="Hello" s="Hello"
Size of string header: 16 bytes
Size of []byte header: 24 bytes
```

## Common Mistakes

### Assuming String Indexing Returns a Character

**Wrong:**

```go
s := "café"
fmt.Println(string(s[3])) // Expecting "é"
```

**What happens:** `s[3]` returns byte `0xc3`, which is the first byte of the two-byte UTF-8 encoding of `é`. You get a garbled character.

**Fix:** Use `[]rune(s)[3]` or iterate with `for _, r := range s`.

### Repeatedly Converting in a Loop

**Wrong:**

```go
for i := 0; i < 1000; i++ {
    b := []byte(s)
    // process b
    s = string(b)
}
```

**What happens:** Each conversion copies the entire string. In tight loops this causes excessive allocations.

**Fix:** Work with `[]byte` throughout the loop and convert once at the end.

## Verify What You Learned

1. Convert the string `"Go 🚀"` to `[]byte` and print each byte in hexadecimal
2. Create a function that takes a `[]byte` and reverses it in place (for ASCII text)
3. Use `bytes.Buffer` to build a comma-separated list from a slice of strings

## What's Next

Continue to [03 - Runes and Unicode Code Points](../03-runes-and-unicode-code-points/03-runes-and-unicode-code-points.md) to learn how Go represents individual Unicode characters with the `rune` type.

## Summary

- `string` is an immutable sequence of bytes; `[]byte` is a mutable slice of bytes
- Converting between them copies the underlying data
- `len(s)` returns bytes for both types
- Use `string` for text that does not change; use `[]byte` for mutable data and I/O
- The `strings` and `bytes` packages offer parallel APIs for each type
- A string header is 16 bytes (pointer + length); a slice header is 24 bytes (pointer + length + capacity)

## Reference

- [Go Blog: Strings, bytes, runes, and characters in Go](https://go.dev/blog/strings)
- [bytes package](https://pkg.go.dev/bytes)
- [strings package](https://pkg.go.dev/strings)
