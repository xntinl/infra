# 5. Strings Package

<!--
difficulty: intermediate
concepts: [strings-package, builder, split, join, replace, contains, trim, fields]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [string-basics, byte-slices-vs-strings, runes-and-unicode-code-points]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-04 in this section
- Understanding of string immutability and UTF-8 encoding

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** common `strings` package functions for searching, splitting, and joining
- **Use** `strings.Builder` for efficient string concatenation
- **Select** the appropriate function for a given string manipulation task

## Why Strings Package

The `strings` package is one of the most frequently imported packages in Go. It provides battle-tested functions for every common string operation: searching, splitting, joining, replacing, trimming, and case conversion. Using these functions instead of writing your own avoids subtle Unicode bugs and takes advantage of optimized implementations that handle edge cases correctly.

`strings.Builder` is particularly important. Naive string concatenation with `+` in a loop creates a new string on every iteration, resulting in O(n^2) behavior. `Builder` provides O(n) concatenation.

## Step 1 -- Searching Functions

```bash
mkdir -p ~/go-exercises/strings-pkg
cd ~/go-exercises/strings-pkg
go mod init strings-pkg
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"strings"
)

func main() {
	s := "The quick brown fox jumps over the lazy dog"

	fmt.Println("Contains 'fox':", strings.Contains(s, "fox"))
	fmt.Println("Contains 'cat':", strings.Contains(s, "cat"))
	fmt.Println("HasPrefix 'The':", strings.HasPrefix(s, "The"))
	fmt.Println("HasSuffix 'dog':", strings.HasSuffix(s, "dog"))
	fmt.Println("Index of 'fox':", strings.Index(s, "fox"))
	fmt.Println("Count of 'the':", strings.Count(s, "the"))
	fmt.Println("ContainsRune '🦊':", strings.ContainsRune(s, '🦊'))
	fmt.Println("ContainsAny vowels:", strings.ContainsAny(s, "aeiou"))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Contains 'fox': true
Contains 'cat': false
HasPrefix 'The': true
HasSuffix 'dog': true
Index of 'fox': 16
Count of 'the': 1
ContainsRune '🦊': false
ContainsAny vowels: true
```

## Step 2 -- Split and Join

```go
package main

import (
	"fmt"
	"strings"
)

func main() {
	csv := "alice,bob,charlie,diana"

	// Split into a slice
	names := strings.Split(csv, ",")
	fmt.Printf("Split: %v (len=%d)\n", names, len(names))

	// Join back together
	joined := strings.Join(names, " | ")
	fmt.Println("Joined:", joined)

	// SplitN limits the number of splits
	parts := strings.SplitN("a:b:c:d:e", ":", 3)
	fmt.Printf("SplitN: %v\n", parts)

	// Fields splits on whitespace (any amount)
	text := "  hello   world   go  "
	words := strings.Fields(text)
	fmt.Printf("Fields: %v\n", words)

	// SplitAfter keeps the separator
	after := strings.SplitAfter("one.two.three", ".")
	fmt.Printf("SplitAfter: %v\n", after)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Split: [alice bob charlie diana] (len=4)
Joined: alice | bob | charlie | diana
SplitN: [a b c:d:e]
Fields: [hello world go]
SplitAfter: [one. two. three]
```

## Step 3 -- Replace and Transform

```go
package main

import (
	"fmt"
	"strings"
)

func main() {
	s := "foo bar foo baz foo"

	// Replace all occurrences
	fmt.Println(strings.ReplaceAll(s, "foo", "qux"))

	// Replace first N occurrences (-1 means all)
	fmt.Println(strings.Replace(s, "foo", "QUX", 2))

	// Case conversion
	fmt.Println(strings.ToUpper("hello"))
	fmt.Println(strings.ToLower("HELLO"))
	fmt.Println(strings.Title("hello world")) //nolint: deprecated but illustrative

	// Trimming
	fmt.Println(strings.TrimSpace("  hello  "))
	fmt.Println(strings.Trim("***hello***", "*"))
	fmt.Println(strings.TrimLeft("000123", "0"))
	fmt.Println(strings.TrimPrefix("test_file.go", "test_"))
	fmt.Println(strings.TrimSuffix("file.go", ".go"))

	// Map: apply a function to every rune
	shout := strings.Map(func(r rune) rune {
		if r == ' ' {
			return '-'
		}
		return r
	}, "hello world go")
	fmt.Println(shout)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
qux bar qux baz qux
QUX bar QUX baz foo
HELLO
hello
Hello World
hello
hello
123
file.go
file
hello-world-go
```

## Step 4 -- strings.Builder

```go
package main

import (
	"fmt"
	"strings"
)

func buildCSV(headers []string, rows [][]string) string {
	var b strings.Builder

	// Write header
	b.WriteString(strings.Join(headers, ","))
	b.WriteByte('\n')

	// Write rows
	for _, row := range rows {
		b.WriteString(strings.Join(row, ","))
		b.WriteByte('\n')
	}

	return b.String()
}

func main() {
	headers := []string{"Name", "Age", "City"}
	rows := [][]string{
		{"Alice", "30", "Portland"},
		{"Bob", "25", "Seattle"},
		{"Charlie", "35", "Denver"},
	}

	csv := buildCSV(headers, rows)
	fmt.Print(csv)

	// Builder can also report its length
	var b strings.Builder
	b.WriteString("hello")
	fmt.Printf("\nBuilder len: %d, cap: %d\n", b.Len(), b.Cap())
	b.WriteString(", world!")
	fmt.Printf("Builder len: %d, cap: %d\n", b.Len(), b.Cap())
	fmt.Println("Result:", b.String())
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Name,Age,City
Alice,30,Portland
Bob,25,Seattle
Charlie,35,Denver

Builder len: 5, cap: 8
Builder len: 13, cap: 16
Result: hello, world!
```

## Step 5 -- strings.NewReader and strings.NewReplacer

```go
package main

import (
	"fmt"
	"io"
	"strings"
)

func main() {
	// NewReader creates an io.Reader from a string (no copy)
	r := strings.NewReader("Hello from Reader")
	buf := make([]byte, 5)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			fmt.Printf("%s", buf[:n])
		}
		if err == io.EOF {
			break
		}
	}
	fmt.Println()

	// NewReplacer performs multiple replacements efficiently
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
	)
	escaped := replacer.Replace(`<div class="main">Hello & "World"</div>`)
	fmt.Println(escaped)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Hello from Reader
&lt;div class=&quot;main&quot;&gt;Hello &amp; &quot;World&quot;&lt;/div&gt;
```

## Common Mistakes

### Using + Concatenation in a Loop

**Wrong:**

```go
result := ""
for _, word := range words {
    result += word + " "
}
```

**What happens:** Each `+=` allocates a new string and copies all previous content. This is O(n^2) for n words.

**Fix:** Use `strings.Builder` or `strings.Join`.

### Confusing Trim and TrimPrefix

**Wrong:**

```go
strings.Trim("testing", "test") // Returns "ing"? No!
```

**What happens:** `Trim` removes individual characters from the cutset, not the substring. It removes `t`, `e`, `s` from both ends, leaving `"in"`.

**Fix:** Use `strings.TrimPrefix` or `strings.TrimSuffix` to remove a specific prefix or suffix.

## Verify What You Learned

1. Split the string `"key1=val1&key2=val2&key3=val3"` into key-value pairs
2. Use `strings.Builder` to build a string that repeats `"Go"` 100 times separated by commas
3. Write an HTML escaper using `strings.NewReplacer` that handles `&`, `<`, `>`, and `"`

## What's Next

Continue to [06 - String Formatting with fmt](../06-string-formatting-with-fmt/06-string-formatting-with-fmt.md) to learn how to format strings with verbs and control output precision.

## Summary

- `strings.Contains`, `HasPrefix`, `HasSuffix`, `Index` search within strings
- `strings.Split` and `strings.Join` convert between strings and slices
- `strings.Fields` splits on whitespace, handling multiple spaces
- `strings.Replace` and `strings.Map` transform string content
- `strings.Trim`, `TrimPrefix`, `TrimSuffix` remove unwanted edges
- `strings.Builder` concatenates strings efficiently in O(n) time
- `strings.NewReplacer` handles multiple replacements in one pass

## Reference

- [strings package](https://pkg.go.dev/strings)
- [strings.Builder](https://pkg.go.dev/strings#Builder)
- [Go Blog: Strings, bytes, runes, and characters in Go](https://go.dev/blog/strings)
