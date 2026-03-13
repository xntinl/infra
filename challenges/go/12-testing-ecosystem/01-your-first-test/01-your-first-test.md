<!-- difficulty: basic -->
<!-- concepts: testing.T, t.Error, t.Fatal, go test, _test.go -->
<!-- tools: go test -->
<!-- estimated_time: 15m -->
<!-- bloom_level: understand -->
<!-- prerequisites: 01-fundamentals, 02-type-system -->

# Your First Test

## Prerequisites

Before starting this exercise, you should be comfortable with:
- Writing and running Go programs
- Functions and return values
- Package structure basics

## Learning Objectives

By the end of this exercise, you will be able to:
1. Create a test file following Go naming conventions
2. Write a test function using `testing.T`
3. Use `t.Error` and `t.Fatal` to report failures
4. Run tests with `go test`

## Why This Matters

Testing is not optional in professional Go development. Go ships with a powerful built-in testing framework in the `testing` package -- no third-party libraries needed. Every Go project you encounter will have `_test.go` files, and understanding them is essential for contributing to any Go codebase. The convention-driven approach means once you learn it, you can read and write tests in any Go project.

## Step-by-Step Instructions

### Step 1: Create the module and source file

Create a new directory and initialize a module:

```bash
mkdir -p mathutil && cd mathutil
go mod init mathutil
```

Create `mathutil.go`:

```go
package mathutil

// Add returns the sum of two integers.
func Add(a, b int) int {
	return a + b
}

// IsPositive returns true if n is greater than zero.
func IsPositive(n int) bool {
	return n > 0
}
```

### Intermediate Verification

```bash
go build ./...
```

You should see no output -- this means it compiled successfully.

### Step 2: Create your first test file

Create `mathutil_test.go`:

```go
package mathutil

import "testing"

func TestAdd(t *testing.T) {
	result := Add(2, 3)
	if result != 5 {
		t.Errorf("Add(2, 3) = %d; want 5", result)
	}
}
```

Key conventions to notice:
- The file name ends with `_test.go` -- Go only compiles these during `go test`
- The function name starts with `Test` followed by an uppercase letter
- The function takes exactly one parameter: `*testing.T`

### Intermediate Verification

```bash
go test
```

You should see:

```
PASS
ok  	mathutil	0.001s
```

### Step 3: Add a failing test to see output

Add another test to `mathutil_test.go`:

```go
func TestAddNegative(t *testing.T) {
	result := Add(-1, -1)
	if result != -2 {
		t.Errorf("Add(-1, -1) = %d; want -2", result)
	}
}
```

### Intermediate Verification

```bash
go test -v
```

The `-v` flag enables verbose output so you can see each test name:

```
=== RUN   TestAdd
--- PASS: TestAdd (0.00s)
=== RUN   TestAddNegative
--- PASS: TestAddNegative (0.00s)
PASS
ok  	mathutil	0.001s
```

### Step 4: Understand t.Error vs t.Fatal

Add a test that demonstrates the difference:

```go
func TestIsPositive(t *testing.T) {
	// t.Error reports failure but continues executing
	if !IsPositive(1) {
		t.Error("IsPositive(1) should be true")
	}

	// This line runs even if the check above fails
	if IsPositive(-1) {
		t.Error("IsPositive(-1) should be false")
	}

	// t.Fatal reports failure and stops this test immediately
	if IsPositive(0) {
		t.Fatal("IsPositive(0) should be false -- stopping test")
	}

	// This line only runs if the above check passes
	if !IsPositive(42) {
		t.Error("IsPositive(42) should be true")
	}
}
```

Use `t.Error` when you want to report multiple failures in a single test run. Use `t.Fatal` when continuing the test would be meaningless or would panic.

### Intermediate Verification

```bash
go test -v
```

All tests should pass:

```
=== RUN   TestIsPositive
--- PASS: TestIsPositive (0.00s)
```

### Step 5: Run a specific test

You can run a single test by name:

```bash
go test -v -run TestAdd
```

This runs all tests whose names match the pattern `TestAdd` (including `TestAddNegative`).

To run only `TestAdd` exactly:

```bash
go test -v -run "^TestAdd$"
```

## Common Mistakes

1. **Forgetting `_test.go` suffix**: Regular `.go` files with test functions will not be recognized by `go test`.

2. **Wrong function signature**: Test functions must be `func TestXxx(t *testing.T)` where `Xxx` starts with an uppercase letter. `func Testadd(t *testing.T)` will be silently ignored.

3. **Using `fmt.Println` instead of `t.Error`**: Printing does not mark the test as failed. Always use `t.Error`, `t.Errorf`, `t.Fatal`, or `t.Fatalf`.

4. **Confusing `t.Error` and `t.Fatal`**: `t.Error` continues execution; `t.Fatal` stops the current test. Use `t.Fatal` when subsequent code depends on the check passing.

## Verify What You Learned

1. What suffix must a Go test file have?
2. What is the required signature for a test function?
3. What is the difference between `t.Error` and `t.Fatal`?
4. How do you run tests in verbose mode?
5. How do you run a specific test by name?

## What's Next

Now that you can write individual tests, the next exercise covers **table-driven tests** -- Go's idiomatic pattern for testing a function with many inputs.

## Summary

- Test files end with `_test.go`
- Test functions start with `Test` and accept `*testing.T`
- `t.Error`/`t.Errorf` report failure but continue
- `t.Fatal`/`t.Fatalf` report failure and stop the test
- `go test` runs tests; `-v` shows verbose output; `-run` filters by name

## Reference

- [testing package](https://pkg.go.dev/testing)
- [Go testing tutorial](https://go.dev/doc/tutorial/add-a-test)
- [How to Write Go Code: Testing](https://go.dev/doc/code#Testing)
