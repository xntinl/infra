# 3. Runes and Unicode Code Points

<!--
difficulty: basic
concepts: [rune-type, unicode-code-points, utf8-package, rune-literals, int32-alias]
tools: [go]
estimated_time: 20m
bloom_level: understand
prerequisites: [string-basics, byte-slices-vs-strings]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01 and 02 in this section
- Basic understanding of UTF-8 encoding

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** that `rune` is an alias for `int32` representing a Unicode code point
- **Use** the `unicode/utf8` package to decode and encode runes
- **Distinguish** between bytes, runes, and strings in Go

## Why Runes and Unicode Code Points

A Unicode code point is a unique number assigned to every character in every writing system. The letter `A` is code point U+0041, the Chinese character `語` is U+8A9E, and the rocket emoji `🚀` is U+1F680. Go represents code points with the `rune` type, which is an alias for `int32` -- large enough to hold any Unicode code point.

Understanding runes is critical because Go strings are byte sequences, not character sequences. When you need to work with individual characters -- counting them, comparing them, or transforming them -- you work with runes. Without this understanding, string manipulation code breaks silently on non-ASCII text.

## Step 1 -- The Rune Type

```bash
mkdir -p ~/go-exercises/runes
cd ~/go-exercises/runes
go mod init runes
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	// A rune is an alias for int32
	var r rune = 'A'
	fmt.Printf("rune: %c, code point: U+%04X, type: %T, value: %d\n",
		r, r, r, r)

	// Rune literals use single quotes
	chinese := '語'
	fmt.Printf("rune: %c, code point: U+%04X, value: %d\n",
		chinese, chinese, chinese)

	rocket := '🚀'
	fmt.Printf("rune: %c, code point: U+%04X, value: %d\n",
		rocket, rocket, rocket)
}
```

Single quotes create a `rune` (int32). Double quotes create a `string`. This distinction matters.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
rune: A, code point: U+0041, type: int32, value: 65
rune: 語, code point: U+8A9E, value: 35486
rune: 🚀, code point: U+1F680, value: 128640
```

## Step 2 -- Converting Strings to Rune Slices

```go
package main

import "fmt"

func main() {
	s := "Go言語"

	// Convert string to []rune
	runes := []rune(s)
	fmt.Printf("String: %q\n", s)
	fmt.Printf("Bytes:  %d\n", len(s))
	fmt.Printf("Runes:  %d\n", len(runes))
	fmt.Println()

	for i, r := range runes {
		fmt.Printf("runes[%d] = %c (U+%04X)\n", i, r, r)
	}
}
```

Converting to `[]rune` gives you a slice where each element is one Unicode character, regardless of how many bytes it takes in UTF-8.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
String: "Go言語"
Bytes:  8
Runes:  4

runes[0] = G (U+0047)
runes[1] = o (U+006F)
runes[2] = 言 (U+8A00)
runes[3] = 語 (U+8A9E)
```

## Step 3 -- The unicode/utf8 Package

```go
package main

import (
	"fmt"
	"unicode/utf8"
)

func main() {
	s := "café"

	// Count runes without allocating a []rune
	fmt.Printf("Rune count: %d\n", utf8.RuneCountInString(s))

	// Decode runes one at a time
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		fmt.Printf("byte offset %d: %c (U+%04X, %d bytes)\n", i, r, r, size)
		i += size
	}

	fmt.Println()

	// Check if bytes form valid UTF-8
	fmt.Println("Valid UTF-8:", utf8.ValidString(s))
	fmt.Println("Valid UTF-8:", utf8.Valid([]byte{0xff, 0xfe}))
}
```

`DecodeRuneInString` returns the rune and the number of bytes it consumed. This is how you iterate without allocating a `[]rune` slice.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Rune count: 4
byte offset 0: c (U+0063, 1 bytes)
byte offset 1: a (U+0061, 1 bytes)
byte offset 2: f (U+0066, 1 bytes)
byte offset 3: é (U+00E9, 2 bytes)

Valid UTF-8: true
Valid UTF-8: false
```

## Step 4 -- Encoding Runes to Bytes

```go
package main

import (
	"fmt"
	"unicode/utf8"
)

func main() {
	r := '世'

	// Encode a single rune to UTF-8 bytes
	buf := make([]byte, utf8.UTFMax) // UTFMax is 4
	n := utf8.EncodeRune(buf, r)

	fmt.Printf("Rune %c encoded to %d bytes: %v\n", r, n, buf[:n])
	fmt.Printf("Hex: %x\n", buf[:n])

	// RuneLen tells you how many bytes a rune needs
	fmt.Printf("RuneLen('A') = %d\n", utf8.RuneLen('A'))
	fmt.Printf("RuneLen('é') = %d\n", utf8.RuneLen('é'))
	fmt.Printf("RuneLen('世') = %d\n", utf8.RuneLen('世'))
	fmt.Printf("RuneLen('🚀') = %d\n", utf8.RuneLen('🚀'))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Rune 世 encoded to 3 bytes: [228 184 150]
Hex: e4b896
RuneLen('A') = 1
RuneLen('é') = 2
RuneLen('世') = 3
RuneLen('🚀') = 4
```

## Step 5 -- The unicode Package

```go
package main

import (
	"fmt"
	"unicode"
)

func main() {
	chars := []rune{'A', 'a', '3', ' ', '!', '語', 'é'}

	for _, c := range chars {
		fmt.Printf("%c: Letter=%t Digit=%t Space=%t Upper=%t Lower=%t\n",
			c,
			unicode.IsLetter(c),
			unicode.IsDigit(c),
			unicode.IsSpace(c),
			unicode.IsUpper(c),
			unicode.IsLower(c),
		)
	}

	fmt.Println()

	// Case conversion
	fmt.Printf("ToUpper('a') = %c\n", unicode.ToUpper('a'))
	fmt.Printf("ToLower('Z') = %c\n", unicode.ToLower('Z'))
	fmt.Printf("ToTitle('a') = %c\n", unicode.ToTitle('a'))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
A: Letter=true Digit=false Space=false Upper=true Lower=false
a: Letter=true Digit=false Space=false Upper=false Lower=true
3: Letter=false Digit=true Space=false Upper=false Lower=false
 : Letter=false Digit=false Space=true Upper=false Lower=false
!: Letter=false Digit=false Space=false Upper=false Lower=false
語: Letter=true Digit=false Space=false Upper=false Lower=false
é: Letter=true Digit=false Space=false Upper=false Lower=true

ToUpper('a') = A
ToLower('Z') = z
ToTitle('a') = A
```

## Common Mistakes

### Confusing byte and rune

**Wrong:**

```go
s := "café"
fmt.Println(s[4]) // Expecting 'é'
```

**What happens:** `s[4]` accesses byte index 4. Since `é` starts at byte index 3 and is 2 bytes wide, `s[4]` is the second byte of `é` -- not a valid character on its own.

**Fix:** Use `[]rune(s)[3]` or iterate with `range`.

### Using len() to Count Characters

**Wrong:**

```go
if len(username) > 20 { // Intended: max 20 characters
```

**What happens:** A 10-character Japanese username uses 30 bytes and would be rejected.

**Fix:** Use `utf8.RuneCountInString(username) > 20`.

## Verify What You Learned

1. Write a function that counts the number of uppercase letters in a string using `unicode.IsUpper`
2. Convert the string `"Hello, 世界"` to a `[]rune`, reverse the slice, and convert back to a string
3. Use `utf8.DecodeRuneInString` to print each character of `"göpher"` with its byte offset

## What's Next

Continue to [04 - String Iteration: Bytes vs Runes](../04-string-iteration-bytes-vs-runes/04-string-iteration-bytes-vs-runes.md) to learn the two ways Go iterates over strings and when to use each.

## Summary

- `rune` is an alias for `int32` and represents a Unicode code point
- Single quotes create rune literals: `'A'`, `'語'`, `'🚀'`
- `[]rune(s)` converts a string to a slice of code points (allocates)
- The `unicode/utf8` package decodes and encodes runes without full slice conversion
- The `unicode` package classifies and transforms individual runes
- UTF-8 uses 1-4 bytes per rune: ASCII=1, Latin=2, CJK=3, Emoji=4

## Reference

- [Go Blog: Strings, bytes, runes, and characters in Go](https://go.dev/blog/strings)
- [unicode/utf8 package](https://pkg.go.dev/unicode/utf8)
- [unicode package](https://pkg.go.dev/unicode)
- [Go Spec: Rune literals](https://go.dev/ref/spec#Rune_literals)
