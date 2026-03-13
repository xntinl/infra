<!-- difficulty: basic -->
<!-- concepts: test cases slice, struct literals, loop -->
<!-- tools: go test -->
<!-- estimated_time: 20m -->
<!-- bloom_level: understand -->
<!-- prerequisites: 01-your-first-test -->

# Table-Driven Tests

## Prerequisites

Before starting this exercise, you should be comfortable with:
- Writing basic Go tests with `testing.T`
- Structs and slice literals
- For-range loops

## Learning Objectives

By the end of this exercise, you will be able to:
1. Structure tests as a slice of test cases
2. Use anonymous structs for concise test case definitions
3. Name test cases for clear failure messages
4. Apply the table-driven pattern to any function

## Why This Matters

Table-driven tests are the most common testing pattern in Go. You will find them in the standard library, in every major open-source project, and in production codebases everywhere. The pattern separates test data from test logic, making it trivial to add new cases -- just add a row to the table. When a test fails, the case name tells you exactly which scenario broke.

## Step-by-Step Instructions

### Step 1: Start with the function to test

Create a new module:

```bash
mkdir -p stringutil && cd stringutil
go mod init stringutil
```

Create `stringutil.go`:

```go
package stringutil

import "strings"

// Reverse returns the reverse of a string.
func Reverse(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

// IsPalindrome checks if a string reads the same forwards and backwards.
func IsPalindrome(s string) bool {
	s = strings.ToLower(s)
	return s == Reverse(s)
}
```

### Intermediate Verification

```bash
go build ./...
```

### Step 2: Write a table-driven test

Create `stringutil_test.go`:

```go
package stringutil

import "testing"

func TestReverse(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty string", input: "", want: ""},
		{name: "single char", input: "a", want: "a"},
		{name: "two chars", input: "ab", want: "ba"},
		{name: "word", input: "hello", want: "olleh"},
		{name: "with spaces", input: "go lang", want: "gnal og"},
		{name: "unicode", input: "cafe\u0301", want: "\u0301efac"},
	}

	for _, tt := range tests {
		if got := Reverse(tt.input); got != tt.want {
			t.Errorf("%s: Reverse(%q) = %q, want %q", tt.name, tt.input, got, tt.want)
		}
	}
}
```

The pattern:
1. Define a slice of anonymous structs with `name`, inputs, and expected output
2. Loop over each case
3. Call the function and compare

### Intermediate Verification

```bash
go test -v
```

```
=== RUN   TestReverse
--- PASS: TestReverse (0.00s)
PASS
```

### Step 3: Add a table-driven test for IsPalindrome

Add to `stringutil_test.go`:

```go
func TestIsPalindrome(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "empty", input: "", want: true},
		{name: "single char", input: "a", want: true},
		{name: "palindrome", input: "racecar", want: true},
		{name: "mixed case palindrome", input: "Racecar", want: true},
		{name: "not palindrome", input: "hello", want: false},
		{name: "two chars same", input: "aa", want: true},
		{name: "two chars different", input: "ab", want: false},
	}

	for _, tt := range tests {
		got := IsPalindrome(tt.input)
		if got != tt.want {
			t.Errorf("%s: IsPalindrome(%q) = %v, want %v", tt.name, tt.input, got, tt.want)
		}
	}
}
```

### Intermediate Verification

```bash
go test -v
```

Both tests should pass. Notice how easy it is to add a new case -- just append another struct literal.

### Step 4: See clear failure output

Temporarily add a broken case to understand the output:

```go
{name: "intentional failure", input: "test", want: true},
```

Run the test:

```bash
go test -v
```

You will see output like:

```
--- FAIL: TestIsPalindrome (0.00s)
    stringutil_test.go:42: intentional failure: IsPalindrome("test") = false, want true
```

The case name immediately tells you which scenario failed. Remove the broken case before continuing.

## Common Mistakes

1. **Forgetting the `name` field**: Without names, failure messages just show values, making it hard to identify which case failed in a long table.

2. **Mutating shared state between cases**: Each test case should be independent. If one case modifies shared data, later cases may produce incorrect results.

3. **Using `t.Fatal` in table loops**: `t.Fatal` stops the entire test function. In a table loop, this means remaining cases are skipped. Use `t.Error` instead, or use subtests (covered in exercise 04).

4. **Not testing edge cases**: The table format makes it cheap to add edge cases. Always include empty inputs, zero values, and boundary conditions.

## Verify What You Learned

1. What are the three parts of a typical table-driven test case struct?
2. Why should you include a `name` field in each test case?
3. Why is `t.Error` preferred over `t.Fatal` in table-driven loops?
4. How do you add a new test case to a table-driven test?

## What's Next

The next exercise covers **test helpers** -- reusable functions that reduce repetition across tests while keeping failure messages clear with `t.Helper()`.

## Summary

- Table-driven tests use a slice of structs to define test cases
- Each struct has a name, inputs, and expected outputs
- A for-range loop runs each case against the function under test
- The `name` field makes failure messages immediately actionable
- Adding cases is as simple as adding a struct literal to the slice

## Reference

- [Go wiki: Table-Driven Tests](https://go.dev/wiki/TableDrivenTests)
- [testing package](https://pkg.go.dev/testing)
- [Go blog: Using Subtests and Sub-benchmarks](https://go.dev/blog/subtests)
