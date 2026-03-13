# 8. Accept Interfaces, Return Structs

<!--
difficulty: intermediate
concepts: [api-design, accept-interfaces, return-structs, decoupling, testability]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [interface-segregation, implicit-interface-satisfaction]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-06 in this section
- Understanding of interface segregation

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** the "accept interfaces, return structs" guideline to function signatures
- **Explain** why this pattern improves flexibility on the input side and clarity on the output side
- **Identify** when to deviate from this guideline

## Why Accept Interfaces, Return Structs

This is one of Go's most quoted design guidelines. Accepting interfaces as parameters makes your functions flexible -- callers can pass any type that satisfies the interface. Returning concrete structs makes your return values clear -- callers know exactly what they get and can access all its fields and methods.

The principle creates a healthy asymmetry: inputs are abstract (flexible), outputs are concrete (precise). This pattern naturally emerges from interface segregation and implicit satisfaction.

## Step 1 -- Accept an Interface

Create a new project:

```bash
mkdir -p ~/go-exercises/accept-return
cd ~/go-exercises/accept-return
go mod init accept-return
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"io"
	"strings"
)

// Accepts io.Reader -- works with files, HTTP bodies, strings, etc.
func countWords(r io.Reader) (int, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return 0, err
	}
	words := strings.Fields(string(data))
	return len(words), nil
}

func main() {
	// Pass a string reader
	r := strings.NewReader("the quick brown fox jumps over the lazy dog")
	count, _ := countWords(r)
	fmt.Printf("Word count: %d\n", count)
}
```

Because `countWords` accepts `io.Reader`, it works with strings, files, network connections, or any other reader without modification.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Word count: 9
```

## Step 2 -- Return a Struct

Return concrete types so callers have full access:

```go
package main

import "fmt"

type Stats struct {
	Words      int
	Characters int
	Lines      int
}

func (s Stats) String() string {
	return fmt.Sprintf("%d words, %d chars, %d lines", s.Words, s.Characters, s.Lines)
}

func analyze(text string) Stats {
	lines := 1
	for _, ch := range text {
		if ch == '\n' {
			lines++
		}
	}
	words := len(splitWords(text))
	return Stats{
		Words:      words,
		Characters: len(text),
		Lines:      lines,
	}
}

func splitWords(s string) []string {
	var words []string
	word := []rune{}
	for _, ch := range s {
		if ch == ' ' || ch == '\n' || ch == '\t' {
			if len(word) > 0 {
				words = append(words, string(word))
				word = nil
			}
		} else {
			word = append(word, ch)
		}
	}
	if len(word) > 0 {
		words = append(words, string(word))
	}
	return words
}

func main() {
	text := "hello world\nfoo bar baz"
	stats := analyze(text)
	fmt.Println(stats)
	fmt.Printf("Average word length: %.1f\n", float64(stats.Characters)/float64(stats.Words))
}
```

Because `analyze` returns `Stats` (not an interface), callers can access `.Words`, `.Characters`, and `.Lines` directly.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
5 words, 23 chars, 2 lines
Average word length: 4.6
```

## Step 3 -- Combine Both Patterns

Build a function that accepts an interface and returns a struct:

```go
package main

import (
	"fmt"
	"io"
	"strings"
)

type ParseResult struct {
	Lines []string
	Count int
}

// Accept interface, return struct
func parseLines(r io.Reader) (*ParseResult, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading input: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	return &ParseResult{
		Lines: lines,
		Count: len(lines),
	}, nil
}

func main() {
	input := "line one\nline two\nline three"
	result, err := parseLines(strings.NewReader(input))
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Printf("Parsed %d lines:\n", result.Count)
	for i, line := range result.Lines {
		fmt.Printf("  [%d] %s\n", i, line)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Parsed 3 lines:
  [0] line one
  [1] line two
  [2] line three
```

## Step 4 -- When to Deviate

There are valid reasons to return an interface:

```go
package main

import (
	"fmt"
	"io"
	"strings"
)

// Returning io.Reader is acceptable here because
// the caller should not know or care about the implementation
func newSource(kind string) io.Reader {
	switch kind {
	case "greeting":
		return strings.NewReader("Hello, World!")
	case "numbers":
		return strings.NewReader("1 2 3 4 5")
	default:
		return strings.NewReader("")
	}
}

func main() {
	r := newSource("greeting")
	data, _ := io.ReadAll(r)
	fmt.Println(string(data))
}
```

Factory functions and constructor patterns may return interfaces when the caller should not depend on the concrete type.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Hello, World!
```

## Common Mistakes

### Accepting a Concrete Type When an Interface Would Do

**Wrong:**

```go
func process(buf *bytes.Buffer) { ... }
```

**Fix:** If you only call `Read`, accept `io.Reader`. This lets callers pass any reader.

### Returning an Interface When There Is Only One Implementation

**Wrong:**

```go
func NewUserService() UserServiceInterface { ... }
```

**Fix:** Return `*UserService`. Callers who need an interface can define one at the consumption site.

## Verify What You Learned

1. Write a function that accepts `io.Writer` and writes formatted output to it
2. Write a function that returns a concrete struct with multiple fields
3. Combine both: accept `io.Reader`, process the data, return a result struct

## What's Next

Continue to [09 - Interface Internals](../09-interface-internals/09-interface-internals.md) to understand how Go represents interfaces at the machine level.

## Summary

- Accept interfaces as function parameters to maximize flexibility
- Return concrete structs from functions to provide full type information
- This creates healthy asymmetry: abstract inputs, concrete outputs
- The pattern makes functions testable (callers can pass mocks) and composable
- Deviate when factory functions need to hide implementation details
- Do not return interfaces just to create an "abstraction" -- let consumers define their own

## Reference

- [Jack Lindamood: Accept Interfaces, Return Structs](https://bryanftan.medium.com/accept-interfaces-return-structs-in-go-d4cab29a301b)
- [Go Wiki: CodeReviewComments - Interfaces](https://go.dev/wiki/CodeReviewComments#interfaces)
- [Effective Go: Interfaces](https://go.dev/doc/effective_go#interfaces)
