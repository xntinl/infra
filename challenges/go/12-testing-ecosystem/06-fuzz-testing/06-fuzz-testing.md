<!-- difficulty: intermediate -->
<!-- concepts: testing.F, f.Fuzz, corpus, go test -fuzz -->
<!-- tools: go test -->
<!-- estimated_time: 30m -->
<!-- bloom_level: apply -->
<!-- prerequisites: 01-your-first-test, 02-table-driven-tests -->

# Fuzz Testing

## Prerequisites

Before starting this exercise, you should be comfortable with:
- Writing and running Go tests
- String and byte manipulation
- Error handling

## Learning Objectives

By the end of this exercise, you will be able to:
1. Write fuzz tests using `testing.F`
2. Add seed corpus entries with `f.Add()`
3. Run fuzz tests with `go test -fuzz`
4. Understand how the fuzzer discovers bugs through generated inputs

## Why This Matters

You write tests for cases you think of. The fuzzer generates cases you never imagined. Fuzz testing automatically creates random inputs and feeds them to your function, looking for panics, crashes, and logical errors. Go 1.18 added native fuzzing support -- no external tools needed. Fuzz testing is particularly effective for parsers, validators, encoders/decoders, and any function that handles untrusted input.

## Instructions

You will write a fuzz test that discovers a bug in a URL slug generator.

### Scaffold

```bash
mkdir -p slug && cd slug
go mod init slug
```

`slug.go`:

```go
package slug

import (
	"strings"
	"unicode"
)

// Slugify converts a string into a URL-safe slug.
// It lowercases, replaces spaces with hyphens, and removes non-alphanumeric characters.
func Slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.TrimSpace(s)

	var result []rune
	prevHyphen := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			result = append(result, r)
			prevHyphen = false
		} else if r == ' ' || r == '-' || r == '_' {
			if !prevHyphen {
				result = append(result, '-')
				prevHyphen = true
			}
		}
	}

	return strings.Trim(string(result), "-")
}

// ReverseSlug converts a slug back to a title-case string.
// This is a simplified inverse: "hello-world" -> "Hello World"
func ReverseSlug(slug string) string {
	parts := strings.Split(slug, "-")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}
```

### Your Task

Create `slug_test.go` with:

**1. A regular test to verify basic behavior**:

```go
package slug

import "testing"

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Hello World", "hello-world"},
		{"  spaces  ", "spaces"},
		{"Go 1.21 Release!", "go-121-release"},
		{"already-a-slug", "already-a-slug"},
		{"UPPERCASE", "uppercase"},
		{"multiple   spaces", "multiple-spaces"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := Slugify(tt.input)
			if got != tt.want {
				t.Errorf("Slugify(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
```

**2. A fuzz test for Slugify**:

```go
func FuzzSlugify(f *testing.F) {
	// Seed corpus: provide initial inputs for the fuzzer
	f.Add("Hello World")
	f.Add("")
	f.Add("   ")
	f.Add("a-b-c")
	f.Add("emoji 🎉 party")
	f.Add("café résumé")

	f.Fuzz(func(t *testing.T, input string) {
		result := Slugify(input)

		// Property 1: result should not start or end with hyphen
		if len(result) > 0 {
			if result[0] == '-' {
				t.Errorf("slug starts with hyphen: Slugify(%q) = %q", input, result)
			}
			if result[len(result)-1] == '-' {
				t.Errorf("slug ends with hyphen: Slugify(%q) = %q", input, result)
			}
		}

		// Property 2: result should not contain consecutive hyphens
		if strings.Contains(result, "--") {
			t.Errorf("slug contains consecutive hyphens: Slugify(%q) = %q", input, result)
		}

		// Property 3: result should only contain lowercase letters, digits, hyphens
		for _, r := range result {
			if !unicode.IsLower(r) && !unicode.IsDigit(r) && r != '-' {
				t.Errorf("slug contains invalid char %q: Slugify(%q) = %q", r, input, result)
			}
		}
	})
}
```

**3. A roundtrip fuzz test** that checks the relationship between `Slugify` and `ReverseSlug`:

```go
func FuzzSlugifyRoundtrip(f *testing.F) {
	f.Add("hello world")
	f.Add("go programming")

	f.Fuzz(func(t *testing.T, input string) {
		slug := Slugify(input)
		if slug == "" {
			return // skip empty slugs
		}
		reversed := ReverseSlug(slug)
		reslug := Slugify(reversed)

		// Slugifying the reversed slug should give the same slug
		if reslug != slug {
			t.Errorf("roundtrip mismatch: %q -> %q -> %q -> %q", input, slug, reversed, reslug)
		}
	})
}
```

### Verification

Run the seed corpus (without fuzzing):

```bash
go test -v
```

Run the fuzzer for 10 seconds:

```bash
go test -fuzz=FuzzSlugify -fuzztime=10s
```

If the fuzzer finds a failure, it saves the input in `testdata/fuzz/FuzzSlugify/` and the test will fail on subsequent `go test` runs until you fix the bug.

Run with a specific fuzz target:

```bash
go test -fuzz=FuzzSlugifyRoundtrip -fuzztime=10s
```

## Common Mistakes

1. **No seed corpus**: Always call `f.Add()` with representative inputs. The fuzzer uses these as starting points for mutation.

2. **Testing exact outputs in fuzz tests**: Fuzz tests check properties (invariants), not exact values. You do not know what input the fuzzer will generate, so you cannot predict the exact output.

3. **Ignoring saved failures**: When the fuzzer finds a bug, it saves the input in `testdata/fuzz/`. Commit these files -- they serve as regression tests.

4. **Running forever**: Use `-fuzztime` to limit duration. Start with 10-30 seconds; increase for critical code.

## Verify What You Learned

1. What is the difference between `testing.T` and `testing.F`?
2. What does `f.Add()` do?
3. Where does the fuzzer save failing inputs?
4. What kinds of properties should fuzz tests check?

## What's Next

The next exercise covers **test fixtures and testdata** -- organizing test files and loading test data from the filesystem.

## Summary

- Fuzz functions: `func FuzzXxx(f *testing.F)` with `f.Fuzz(func(t *testing.T, ...) {...})`
- `f.Add()` provides seed corpus entries
- `go test -fuzz=FuzzXxx -fuzztime=30s` runs the fuzzer
- Fuzz tests check invariants/properties, not exact outputs
- Failing inputs are saved in `testdata/fuzz/` as regression tests

## Reference

- [Go fuzzing tutorial](https://go.dev/doc/tutorial/fuzz)
- [testing.F](https://pkg.go.dev/testing#F)
- [Go blog: Fuzzing is Beta-Ready](https://go.dev/blog/fuzz-beta)
