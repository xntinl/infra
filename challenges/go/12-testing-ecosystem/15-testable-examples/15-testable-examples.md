# 15. Testable Examples

<!--
difficulty: intermediate
concepts: [example-functions, output-comments, godoc-examples, example-naming]
tools: [go test, go doc]
estimated_time: 20m
bloom_level: apply
prerequisites: [01-your-first-test, 04-subtests-and-t-run]
-->

## Prerequisites

Before starting this exercise, you should be comfortable with:
- Writing and running Go tests
- Package documentation basics
- `fmt.Println` and standard output

## Learning Objectives

By the end of this exercise, you will be able to:
1. Write testable example functions with `// Output:` comments
2. Apply correct naming conventions for examples
3. Verify that examples run as tests with `go test`
4. Use examples as living documentation in `go doc`

## Why This Matters

Examples serve three purposes simultaneously: they are tests (verified by `go test`), documentation (displayed by `go doc` and pkg.go.dev), and working code samples (copy-pasteable). An example with an `// Output:` comment is executed during testing and fails if the output does not match. This means your documentation never goes stale.

## Instructions

You will write testable examples for a string utility package.

### Scaffold

```bash
mkdir -p ~/go-exercises/examples && cd ~/go-exercises/examples
go mod init examples
```

`strutil.go`:

```go
package strutil

import (
	"strings"
	"unicode"
)

// Truncate shortens s to maxLen characters, adding "..." if truncated.
func Truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

// Initials returns the uppercase first letter of each word.
func Initials(name string) string {
	words := strings.Fields(name)
	var result []rune
	for _, word := range words {
		for _, r := range word {
			result = append(result, unicode.ToUpper(r))
			break
		}
	}
	return string(result)
}

// Wrap wraps text at the given width, breaking at spaces.
func Wrap(text string, width int) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}
	var lines []string
	current := words[0]
	for _, word := range words[1:] {
		if len(current)+1+len(word) > width {
			lines = append(lines, current)
			current = word
		} else {
			current += " " + word
		}
	}
	lines = append(lines, current)
	return strings.Join(lines, "\n")
}
```

### Your Task

Create `example_test.go`:

```go
package strutil_test

import (
	"fmt"

	"examples"
)

func ExampleTruncate() {
	fmt.Println(strutil.Truncate("Hello, World!", 10))
	fmt.Println(strutil.Truncate("Short", 10))
	fmt.Println(strutil.Truncate("Hi", 2))
	// Output:
	// Hello, ...
	// Short
	// Hi
}

func ExampleTruncate_short() {
	fmt.Println(strutil.Truncate("Go", 5))
	// Output: Go
}

func ExampleInitials() {
	fmt.Println(strutil.Initials("John Doe"))
	fmt.Println(strutil.Initials("Alice Bob Charlie"))
	fmt.Println(strutil.Initials("go"))
	// Output:
	// JD
	// ABC
	// G
}

func ExampleWrap() {
	text := "The quick brown fox jumps over the lazy dog"
	fmt.Println(strutil.Wrap(text, 20))
	// Output:
	// The quick brown fox
	// jumps over the lazy
	// dog
}

func ExampleWrap_narrow() {
	fmt.Println(strutil.Wrap("hello world", 5))
	// Output:
	// hello
	// world
}
```

### Verification

Run examples as tests:

```bash
go test -v
```

Expected output shows each example running:

```
=== RUN   ExampleTruncate
--- PASS: ExampleTruncate (0.00s)
=== RUN   ExampleTruncate_short
--- PASS: ExampleTruncate_short (0.00s)
=== RUN   ExampleInitials
--- PASS: ExampleInitials (0.00s)
=== RUN   ExampleWrap
--- PASS: ExampleWrap (0.00s)
=== RUN   ExampleWrap_narrow
--- PASS: ExampleWrap_narrow (0.00s)
```

View the documentation:

```bash
go doc -all .
```

Examples appear under the functions they document.

## Common Mistakes

1. **Missing `// Output:` comment**: Without the comment, the example is not executed as a test. It still appears in documentation but is not verified.

2. **Wrong naming convention**: Example functions must follow strict naming rules:
   - `ExampleFunctionName` -- example for a function
   - `ExampleTypeName` -- example for a type
   - `ExampleTypeName_MethodName` -- example for a method
   - `ExampleFunctionName_suffix` -- additional example (suffix must start lowercase)
   - `Example` -- package-level example

3. **Non-deterministic output**: Examples must produce the same output every time. Do not use random numbers, timestamps, or map iteration (unordered).

4. **Using `// Unordered output:` incorrectly**: Use `// Unordered output:` only when the output lines can appear in any order (e.g., map iteration).

## Verify What You Learned

1. What makes an example function a test?
2. What naming convention links an example to a specific function?
3. When would you use `// Unordered output:`?
4. Where do examples appear in `go doc` output?

## What's Next

The next exercise covers **testing time-dependent code** -- techniques for testing code that uses `time.Now()`, timers, and durations.

## Summary

- Example functions start with `Example` and live in `_test.go` files
- The `// Output:` comment makes the example a test
- Examples appear in `go doc` and pkg.go.dev as documentation
- Naming: `ExampleFunc`, `ExampleType_Method`, `ExampleFunc_suffix`
- Use `_test` package names for black-box examples
- `// Unordered output:` for non-deterministic line order
- Examples without `// Output:` are compiled but not executed as tests

## Reference

- [Testable examples in Go](https://go.dev/blog/examples)
- [testing package: Examples](https://pkg.go.dev/testing#hdr-Examples)
- [Go Code Review Comments: Examples](https://go.dev/wiki/CodeReviewComments#examples)
