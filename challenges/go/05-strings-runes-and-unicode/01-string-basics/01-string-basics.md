# 1. String Basics

<!--
difficulty: basic
concepts: [string-type, immutability, utf8-encoding, len-vs-rune-count, raw-strings]
tools: [go]
estimated_time: 15m
bloom_level: remember
prerequisites: [variables-and-types, functions]
-->

## Prerequisites

- Go 1.22+ installed
- Familiarity with Go variables and basic types
- A terminal and text editor

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** that Go strings are immutable sequences of bytes
- **Identify** the difference between `len()` (byte count) and `utf8.RuneCountInString()` (character count)
- **Explain** why Go uses UTF-8 encoding for strings by default

## Why String Basics

Strings are one of the most used types in any programming language. Go strings have specific properties that surprise developers coming from other languages: they are immutable, they are stored as UTF-8 encoded bytes, and `len()` returns the byte count rather than the character count. Understanding these fundamentals prevents subtle bugs when working with internationalized text, file I/O, and network protocols.

Go was co-created by Rob Pike and Ken Thompson, who also co-created UTF-8. The language treats UTF-8 as a first-class encoding, and the entire standard library assumes it. Getting comfortable with Go's string model early saves hours of debugging later.

## Step 1 -- Strings Are Immutable Byte Sequences

Create a new project and explore string fundamentals.

```bash
mkdir -p ~/go-exercises/string-basics
cd ~/go-exercises/string-basics
go mod init string-basics
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	s := "Hello, Go!"
	fmt.Println(s)
	fmt.Printf("Type: %T\n", s)
	fmt.Printf("Length (bytes): %d\n", len(s))

	// Strings are immutable -- this does NOT compile:
	// s[0] = 'h'  // error: cannot assign to s[0]

	// You can reassign the variable, but the original string is unchanged
	s = "Hello, World!"
	fmt.Println(s)
}
```

Reassigning `s` points the variable at a new string value. The original `"Hello, Go!"` is not modified -- it becomes garbage collected when nothing references it.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Hello, Go!
Type: string
Length (bytes): 10
Hello, World!
```

## Step 2 -- UTF-8 Encoding and len()

The `len()` function returns the number of bytes, not the number of characters:

```go
package main

import (
	"fmt"
	"unicode/utf8"
)

func main() {
	ascii := "Hello"
	japanese := "Go言語"
	emoji := "Go is fun! 🎉"

	fmt.Printf("%-15s bytes=%-3d runes=%d\n",
		ascii, len(ascii), utf8.RuneCountInString(ascii))
	fmt.Printf("%-15s bytes=%-3d runes=%d\n",
		japanese, len(japanese), utf8.RuneCountInString(japanese))
	fmt.Printf("%-15s bytes=%-3d runes=%d\n",
		emoji, len(emoji), utf8.RuneCountInString(emoji))
}
```

ASCII characters are 1 byte each. CJK characters typically use 3 bytes. Emoji use 4 bytes.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Hello           bytes=5   runes=5
Go言語          bytes=8   runes=4
Go is fun! 🎉  bytes=15  runes=12
```

## Step 3 -- Raw String Literals

Go has two string literal forms: interpreted strings (double quotes) and raw strings (backticks):

```go
package main

import "fmt"

func main() {
	// Interpreted string -- escape sequences are processed
	interpreted := "Line 1\nLine 2\tTabbed"
	fmt.Println(interpreted)

	fmt.Println("---")

	// Raw string -- everything between backticks is literal
	raw := `Line 1\nLine 2\tTabbed`
	fmt.Println(raw)

	fmt.Println("---")

	// Raw strings can span multiple lines
	multiline := `{
  "name": "gopher",
  "lang": "Go"
}`
	fmt.Println(multiline)
}
```

Raw strings are useful for regular expressions, JSON templates, SQL queries, and file paths on Windows.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Line 1
Line 2	Tabbed
---
Line 1\nLine 2\tTabbed
---
{
  "name": "gopher",
  "lang": "Go"
}
```

## Step 4 -- String Comparison and Concatenation

```go
package main

import "fmt"

func main() {
	a := "apple"
	b := "banana"
	c := "apple"

	// Comparison is lexicographic (byte-by-byte)
	fmt.Println(a == c)  // true
	fmt.Println(a == b)  // false
	fmt.Println(a < b)   // true (a comes before b)

	// Concatenation with +
	greeting := "Hello, " + "World!"
	fmt.Println(greeting)

	// Concatenation in a loop is inefficient (covered in later exercises)
	result := ""
	for i := 0; i < 3; i++ {
		result += fmt.Sprintf("item-%d ", i)
	}
	fmt.Println(result)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
true
false
true
Hello, World!
item-0 item-1 item-2
```

## Common Mistakes

### Assuming len() Returns Character Count

**Wrong:**

```go
s := "café"
fmt.Println(len(s)) // Expecting 4
```

**What happens:** `len(s)` returns `5` because the `é` character is encoded as 2 bytes in UTF-8.

**Fix:** Use `utf8.RuneCountInString(s)` to count characters.

### Trying to Modify a String In Place

**Wrong:**

```go
s := "hello"
s[0] = 'H' // compile error
```

**What happens:** Strings are immutable. You cannot change individual bytes.

**Fix:** Convert to `[]byte`, modify, and convert back: `b := []byte(s); b[0] = 'H'; s = string(b)`.

## Verify What You Learned

1. Create a string containing characters from three different scripts (Latin, Cyrillic, CJK) and print both its byte length and rune count
2. Write a raw string literal containing a Windows file path like `C:\Users\gopher\file.txt` without any escape sequences
3. Concatenate your first name and last name with a space between them using the `+` operator

## What's Next

Continue to [02 - Byte Slices vs Strings](../02-byte-slices-vs-strings/02-byte-slices-vs-strings.md) to learn about converting between strings and byte slices and when to use each.

## Summary

- Go strings are immutable sequences of bytes, not characters
- Strings are UTF-8 encoded by default
- `len(s)` returns the byte count; `utf8.RuneCountInString(s)` returns the character count
- Interpreted strings (`"..."`) process escape sequences; raw strings (`` `...` ``) do not
- String comparison uses lexicographic byte ordering
- Concatenation with `+` creates a new string each time

## Reference

- [Go Blog: Strings, bytes, runes, and characters in Go](https://go.dev/blog/strings)
- [Go Spec: String types](https://go.dev/ref/spec#String_types)
- [unicode/utf8 package](https://pkg.go.dev/unicode/utf8)
