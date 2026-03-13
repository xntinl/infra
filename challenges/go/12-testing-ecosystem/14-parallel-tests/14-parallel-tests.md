# 14. Parallel Tests

<!--
difficulty: intermediate
concepts: [t-parallel, parallel-subtests, test-isolation, race-conditions, shared-state]
tools: [go test]
estimated_time: 30m
bloom_level: apply
prerequisites: [01-your-first-test, 04-subtests-and-t-run, 12-t-cleanup-patterns]
-->

## Prerequisites

Before starting this exercise, you should be comfortable with:
- Subtests and `t.Run`
- `t.Cleanup` for resource management
- Basic concurrency concepts

## Learning Objectives

By the end of this exercise, you will be able to:
1. Use `t.Parallel()` to run tests concurrently
2. Identify and fix shared state problems in parallel tests
3. Apply the loop variable capture pattern for parallel table-driven tests
4. Control parallelism with `-parallel` flag

## Why This Matters

A large test suite can take minutes to run sequentially. `t.Parallel()` tells the test runner that a test can run concurrently with other parallel tests, reducing wall-clock time significantly. But parallel tests must not share mutable state. The most common bug is capturing a loop variable by reference in a parallel subtest -- this causes all subtests to use the last value.

## Instructions

You will parallelize a test suite and fix common concurrency bugs.

### Scaffold

```bash
mkdir -p ~/go-exercises/parallel && cd ~/go-exercises/parallel
go mod init parallel
```

`transform.go`:

```go
package transform

import (
	"strings"
	"unicode"
)

// Reverse returns the string reversed.
func Reverse(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

// CamelToSnake converts camelCase to snake_case.
func CamelToSnake(s string) string {
	var result []rune
	for i, r := range s {
		if unicode.IsUpper(r) && i > 0 {
			result = append(result, '_')
		}
		result = append(result, unicode.ToLower(r))
	}
	return string(result)
}

// WordCount returns the number of words in a string.
func WordCount(s string) int {
	return len(strings.Fields(s))
}
```

### Your Task

Create `transform_test.go`:

```go
package transform

import "testing"

func TestReverse_Parallel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"single", "a", "a"},
		{"word", "hello", "olleh"},
		{"sentence", "Go is fast", "tsaf si oG"},
		{"unicode", "cafe\u0301", "\u0301efac"},
		{"palindrome", "racecar", "racecar"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel() // Mark as parallel

			got := Reverse(tt.input)
			if got != tt.want {
				t.Errorf("Reverse(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCamelToSnake_Parallel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple", "helloWorld", "hello_world"},
		{"multiple", "thisIsCamelCase", "this_is_camel_case"},
		{"single", "hello", "hello"},
		{"allCaps", "URL", "u_r_l"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := CamelToSnake(tt.input)
			if got != tt.want {
				t.Errorf("CamelToSnake(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestWordCount_Parallel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"empty", "", 0},
		{"single", "hello", 1},
		{"two", "hello world", 2},
		{"extra spaces", "  hello   world  ", 2},
		{"tabs", "hello\tworld", 2},
		{"newlines", "hello\nworld", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := WordCount(tt.input)
			if got != tt.want {
				t.Errorf("WordCount(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestParallel_WithCleanup(t *testing.T) {
	// Parallel tests can use t.Cleanup
	t.Run("sub1", func(t *testing.T) {
		t.Parallel()
		t.Cleanup(func() {
			// Runs when this subtest finishes
		})
		got := Reverse("abc")
		if got != "cba" {
			t.Errorf("got %q, want cba", got)
		}
	})

	t.Run("sub2", func(t *testing.T) {
		t.Parallel()
		t.Cleanup(func() {
			// Runs when this subtest finishes
		})
		got := Reverse("xyz")
		if got != "zyx" {
			t.Errorf("got %q, want zyx", got)
		}
	})
}
```

### Verification

Run tests with verbose output:

```bash
go test -v
```

Run with race detector to catch shared state bugs:

```bash
go test -race -v
```

Control parallelism:

```bash
go test -v -parallel=2
```

The `-parallel` flag sets the maximum number of parallel tests. Default is `GOMAXPROCS`.

## Common Mistakes

1. **Loop variable capture (pre-Go 1.22)**: In Go versions before 1.22, the loop variable is shared across iterations. Parallel subtests that capture it by closure all see the last value.

**Wrong (Go < 1.22):**
```go
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        t.Parallel()
        // tt refers to the loop variable -- shared!
        got := Reverse(tt.input)
    })
}
```

**Fix (Go < 1.22):**
```go
for _, tt := range tests {
    tt := tt // shadow the loop variable
    t.Run(tt.name, func(t *testing.T) {
        t.Parallel()
        got := Reverse(tt.input)
    })
}
```

In Go 1.22+, loop variables are scoped per iteration, so this is no longer needed.

2. **Shared mutable state**: Parallel tests must not modify shared variables without synchronization.

3. **Calling `t.Parallel()` after using `t`**: Call `t.Parallel()` as the first line in the subtest function, before any setup that uses `t`.

4. **Forgetting that the parent test waits**: A parent test with parallel subtests waits for all subtests to finish before its cleanup runs.

## Verify What You Learned

1. What happens when you call `t.Parallel()` inside a subtest?
2. How does `-parallel` flag affect test execution?
3. Why was `tt := tt` needed before Go 1.22?
4. Can you mix parallel and sequential subtests in the same parent test?

## What's Next

The next exercise covers **testable examples** -- writing examples that serve as documentation and tests simultaneously.

## Summary

- `t.Parallel()` marks a test or subtest to run concurrently
- Parallel tests must not share mutable state
- In Go < 1.22, shadow loop variables with `tt := tt` for parallel subtests
- In Go 1.22+, loop variable scoping eliminates this bug
- `-parallel=N` controls maximum concurrent parallel tests
- `-race` flag detects data races in parallel tests
- Parent tests wait for all parallel subtests to complete

## Reference

- [testing.T.Parallel](https://pkg.go.dev/testing#T.Parallel)
- [Go Blog: Subtests and sub-benchmarks](https://go.dev/blog/subtests)
- [Go 1.22: Loop variable scoping](https://go.dev/blog/loopvar-preview)
