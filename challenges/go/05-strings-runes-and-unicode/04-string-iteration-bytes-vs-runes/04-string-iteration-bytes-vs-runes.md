# 4. String Iteration: Bytes vs Runes

<!--
difficulty: basic
concepts: [range-over-string, byte-iteration, rune-iteration, index-semantics, replacement-character]
tools: [go]
estimated_time: 20m
bloom_level: understand
prerequisites: [string-basics, runes-and-unicode-code-points]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-03 in this section
- Understanding of runes and UTF-8 encoding

## Learning Objectives

After completing this exercise, you will be able to:

- **Distinguish** between byte-level and rune-level iteration over strings
- **Explain** what `for i, r := range s` produces (byte index + rune value)
- **Identify** how Go handles invalid UTF-8 sequences during iteration

## Why String Iteration: Bytes vs Runes

Go provides two fundamentally different ways to iterate over a string. A classic `for` loop with index gives you bytes. A `for range` loop gives you runes. Choosing the wrong one leads to bugs: splitting multi-byte characters, miscounting positions, or corrupting text. This exercise makes the difference concrete so you can always pick the correct iteration strategy.

## Step 1 -- Byte-Level Iteration

```bash
mkdir -p ~/go-exercises/string-iteration
cd ~/go-exercises/string-iteration
go mod init string-iteration
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	s := "Goé🚀"

	fmt.Println("=== Byte iteration ===")
	for i := 0; i < len(s); i++ {
		fmt.Printf("s[%d] = 0x%02x (%c)\n", i, s[i], s[i])
	}
}
```

Byte-level iteration visits every byte. Multi-byte characters produce multiple entries, and printing individual bytes with `%c` produces garbled output for non-ASCII characters.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
=== Byte iteration ===
s[0] = 0x47 (G)
s[1] = 0x6f (o)
s[2] = 0xc3 (Ã)
s[3] = 0xa9 (©)
s[4] = 0xf0 (ð)
s[5] = 0x9f ()
s[6] = 0x9a ()
s[7] = 0x80 ()
```

## Step 2 -- Rune-Level Iteration with range

```go
package main

import "fmt"

func main() {
	s := "Goé🚀"

	fmt.Println("=== Rune iteration (for range) ===")
	for i, r := range s {
		fmt.Printf("index=%d rune=%c (U+%04X, %d bytes)\n",
			i, r, r, len(string(r)))
	}
}
```

The `for range` loop over a string decodes one rune per iteration. The index `i` is the byte offset where that rune starts, not the rune index.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
=== Rune iteration (for range) ===
index=0 rune=G (U+0047, 1 bytes)
index=1 rune=o (U+006F, 1 bytes)
index=2 rune=é (U+00E9, 2 bytes)
index=4 rune=🚀 (U+1F680, 4 bytes)
```

Notice the index jumps from 2 to 4 (skipping byte 3, the second byte of `é`) and there is no index 5, 6, or 7.

## Step 3 -- Comparing Both Approaches Side by Side

```go
package main

import (
	"fmt"
	"unicode/utf8"
)

func main() {
	s := "Hello, 世界!"

	fmt.Printf("String:      %q\n", s)
	fmt.Printf("Byte count:  %d\n", len(s))
	fmt.Printf("Rune count:  %d\n", utf8.RuneCountInString(s))
	fmt.Println()

	// Byte iteration: len(s) iterations
	byteCount := 0
	for i := 0; i < len(s); i++ {
		byteCount++
	}

	// Rune iteration: rune count iterations
	runeCount := 0
	for range s {
		runeCount++
	}

	fmt.Printf("Byte loop iterations: %d\n", byteCount)
	fmt.Printf("Range loop iterations: %d\n", runeCount)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
String:      "Hello, 世界!"
Byte count:  13
Rune count:  9

Byte loop iterations: 13
Range loop iterations: 9
```

## Step 4 -- Invalid UTF-8 and the Replacement Character

```go
package main

import "fmt"

func main() {
	// Create a string with invalid UTF-8 bytes
	invalid := string([]byte{0x48, 0x65, 0xff, 0x6c, 0x6f})

	fmt.Printf("String bytes: %x\n", []byte(invalid))
	fmt.Println("=== Range over invalid UTF-8 ===")
	for i, r := range invalid {
		fmt.Printf("index=%d rune=%c (U+%04X)\n", i, r, r)
	}
}
```

When `range` encounters an invalid UTF-8 byte, it produces the Unicode replacement character `U+FFFD` and advances one byte.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
String bytes: 4865ff6c6f
=== Range over invalid UTF-8 ===
index=0 rune=H (U+0048)
index=1 rune=e (U+0065)
index=2 rune=\ufffd (U+FFFD)
index=3 rune=l (U+006C)
index=4 rune=o (U+006F)
```

## Step 5 -- Practical Example: Counting Character Types

```go
package main

import (
	"fmt"
	"unicode"
)

func charStats(s string) (letters, digits, spaces, other int) {
	for _, r := range s {
		switch {
		case unicode.IsLetter(r):
			letters++
		case unicode.IsDigit(r):
			digits++
		case unicode.IsSpace(r):
			spaces++
		default:
			other++
		}
	}
	return
}

func main() {
	s := "Go 1.22 is great! 🎉"
	l, d, sp, o := charStats(s)
	fmt.Printf("Input: %q\n", s)
	fmt.Printf("Letters: %d, Digits: %d, Spaces: %d, Other: %d\n",
		l, d, sp, o)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Input: "Go 1.22 is great! 🎉"
Letters: 9, Digits: 3, Spaces: 4, Other: 4
```

## Common Mistakes

### Using Byte Index as Character Position

**Wrong:**

```go
s := "café"
// Trying to get the 4th character
fmt.Println(string(s[3])) // Gets a partial byte of 'é'
```

**What happens:** Byte index 3 is the first byte of `é`, not a complete character.

**Fix:** Convert to `[]rune` for positional access: `fmt.Println(string([]rune(s)[3]))`.

### Assuming range Index Is Sequential

**Wrong:**

```go
for i, r := range "Go語" {
    fmt.Printf("Character %d: %c\n", i, r)
}
// Prints indices 0, 1, 2 -- but index 2 is byte offset, not position 2
```

**What happens:** The index skips values when multi-byte runes are encountered. If you need sequential character positions, use a separate counter.

**Fix:**

```go
pos := 0
for _, r := range "Go語" {
    fmt.Printf("Character %d: %c\n", pos, r)
    pos++
}
```

## Verify What You Learned

1. Write a function that returns the byte offset of the Nth rune in a string (0-indexed)
2. Iterate over `"Hello, 世界! 🌍"` using both byte and rune iteration, and count how many iterations each produces
3. Create a string containing one invalid UTF-8 byte and verify that `range` emits `U+FFFD` for it

## What's Next

Continue to [05 - Strings Package](../05-strings-package/05-strings-package.md) to learn the standard library functions for searching, splitting, joining, and building strings.

## Summary

- `for i := 0; i < len(s); i++` iterates over bytes
- `for i, r := range s` iterates over runes, where `i` is the byte offset
- The `range` index skips values for multi-byte characters
- Invalid UTF-8 bytes produce the replacement character `U+FFFD` during range iteration
- Use rune iteration for text processing; use byte iteration only when you need raw byte access
- For positional character access, convert to `[]rune` or maintain a separate counter

## Reference

- [Go Spec: For statements with range clause](https://go.dev/ref/spec#For_range)
- [Go Blog: Strings, bytes, runes, and characters in Go](https://go.dev/blog/strings)
- [Unicode Replacement Character](https://en.wikipedia.org/wiki/Specials_(Unicode_block)#Replacement_character)
