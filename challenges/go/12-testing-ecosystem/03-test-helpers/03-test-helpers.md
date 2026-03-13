<!-- difficulty: intermediate -->
<!-- concepts: t.Helper(), helper functions, clean output -->
<!-- tools: go test -->
<!-- estimated_time: 20m -->
<!-- bloom_level: apply -->
<!-- prerequisites: 01-your-first-test, 02-table-driven-tests -->

# Test Helpers

## Prerequisites

Before starting this exercise, you should be comfortable with:
- Writing basic tests and table-driven tests
- Functions as first-class values
- `testing.T` methods: `t.Error`, `t.Fatal`

## Learning Objectives

By the end of this exercise, you will be able to:
1. Extract common test logic into helper functions
2. Use `t.Helper()` to produce clean failure output
3. Write assertion helpers that report failures at the caller's line
4. Recognize when to use helpers vs. inline assertions

## Why This Matters

As your test suite grows, you will repeat the same assertions and setup patterns across many tests. Helper functions eliminate this duplication, but without `t.Helper()`, failure messages point to the wrong line -- inside the helper instead of the caller. Marking functions with `t.Helper()` fixes the stack trace so you can immediately find the failing test case.

## Instructions

You are building a small `validator` package that checks user input. Your task is to write tests using helper functions that produce clean, actionable output.

### Scaffold

Create the module and source file:

```bash
mkdir -p validator && cd validator
go mod init validator
```

`validator.go`:

```go
package validator

import (
	"fmt"
	"regexp"
	"strings"
)

// ValidationError holds a field name and an error message.
type ValidationError struct {
	Field   string
	Message string
}

func (v ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", v.Field, v.Message)
}

// ValidateEmail checks if an email address is valid.
func ValidateEmail(email string) error {
	if email == "" {
		return &ValidationError{Field: "email", Message: "required"}
	}
	pattern := `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`
	if matched, _ := regexp.MatchString(pattern, email); !matched {
		return &ValidationError{Field: "email", Message: "invalid format"}
	}
	return nil
}

// ValidateUsername checks if a username meets requirements.
func ValidateUsername(name string) error {
	if name == "" {
		return &ValidationError{Field: "username", Message: "required"}
	}
	if len(name) < 3 {
		return &ValidationError{Field: "username", Message: "too short (min 3)"}
	}
	if len(name) > 20 {
		return &ValidationError{Field: "username", Message: "too long (max 20)"}
	}
	if strings.Contains(name, " ") {
		return &ValidationError{Field: "username", Message: "must not contain spaces"}
	}
	return nil
}
```

### Your Task

Create `validator_test.go` with the following:

1. **A `assertNoError` helper** that calls `t.Helper()` and fails the test if an error is non-nil.

2. **A `assertError` helper** that calls `t.Helper()` and fails the test if an error is nil. It should also check that the error message contains an expected substring.

3. **Table-driven tests for `ValidateEmail`** using your helpers -- test valid emails return no error and invalid emails return errors with expected messages.

4. **Table-driven tests for `ValidateUsername`** using your helpers.

### Hints

Here is the signature for one helper:

```go
func assertNoError(t *testing.T, err error) {
	t.Helper()
	// your code here
}
```

Without `t.Helper()`, a failure inside `assertNoError` would report the line number inside the helper function. With `t.Helper()`, Go reports the line in the test that called the helper.

Your test for `ValidateEmail` should look something like:

```go
func TestValidateEmail(t *testing.T) {
	tests := []struct {
		name      string
		email     string
		wantErr   bool
		errContains string
	}{
		// fill in cases
	}

	for _, tt := range tests {
		err := ValidateEmail(tt.email)
		if tt.wantErr {
			assertError(t, err, tt.errContains)
		} else {
			assertNoError(t, err)
		}
	}
}
```

Test cases to cover:
- Valid: `"user@example.com"`, `"a.b+c@domain.co"`
- Invalid: `""` (required), `"noatsign"` (invalid format), `"@missing.local"` (invalid format)
- Username valid: `"gopher"`, `"abc"` (exactly 3)
- Username invalid: `""` (required), `"ab"` (too short), `"has space"` (spaces), a 21-char string (too long)

### Verification

```bash
go test -v
```

All tests should pass. To see the benefit of `t.Helper()`, temporarily break a test case (change an expected value) and observe that the failure line points to the test function, not the helper.

## Common Mistakes

1. **Forgetting `t.Helper()`**: Without it, every failure points to the helper function's line, making debugging painful.

2. **Helpers that do too much**: A helper should assert one thing. Avoid writing a single helper that validates multiple conditions.

3. **Not passing `*testing.T`**: Helpers need `t` to report failures. Always pass it as the first parameter by convention.

4. **Using helpers for setup instead of assertions**: Setup helpers (creating temp dirs, starting servers) typically use `t.Fatal` since the test cannot proceed without them. Assertion helpers typically use `t.Error` so remaining checks still run.

## Verify What You Learned

1. What does `t.Helper()` change about failure reporting?
2. Where should `t.Helper()` be called in a helper function?
3. When should a helper use `t.Fatal` vs `t.Error`?
4. What is the conventional first parameter of a test helper?

## What's Next

The next exercise introduces **subtests with `t.Run()`** -- a way to give each table-driven case its own test identity, enabling parallel execution and selective running.

## Summary

- `t.Helper()` marks a function as a test helper so failures report the caller's line
- Call `t.Helper()` as the first line in any helper function
- Helpers reduce duplication while keeping output actionable
- Pass `*testing.T` as the first parameter by convention
- Use `t.Error` in assertion helpers, `t.Fatal` in setup helpers

## Reference

- [testing.T.Helper](https://pkg.go.dev/testing#T.Helper)
- [Go wiki: Table-Driven Tests](https://go.dev/wiki/TableDrivenTests)
- [Go blog: Using Subtests and Sub-benchmarks](https://go.dev/blog/subtests)
