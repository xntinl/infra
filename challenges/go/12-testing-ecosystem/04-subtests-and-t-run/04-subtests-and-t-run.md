<!-- difficulty: intermediate -->
<!-- concepts: t.Run(), named subtests, go test -run -->
<!-- tools: go test -->
<!-- estimated_time: 20m -->
<!-- bloom_level: apply -->
<!-- prerequisites: 01-your-first-test, 02-table-driven-tests -->

# Subtests and t.Run

## Prerequisites

Before starting this exercise, you should be comfortable with:
- Table-driven tests
- Test helpers with `t.Helper()`
- Running tests with `go test -v` and `-run`

## Learning Objectives

By the end of this exercise, you will be able to:
1. Use `t.Run()` to create named subtests
2. Run individual subtests with `go test -run`
3. Combine table-driven tests with subtests
4. Use `t.Fatal` safely inside subtests without skipping other cases

## Why This Matters

Subtests give each test case its own identity in the test output. Instead of a single `TestReverse` with an error message mentioning a name, you get `TestReverse/empty_string`, `TestReverse/unicode`, etc. This enables running a single failing case in isolation, using `t.Fatal` without skipping other cases, and eventually running cases in parallel.

## Instructions

You will refactor a table-driven test to use subtests, then explore filtering and isolation.

### Scaffold

Create the module:

```bash
mkdir -p converter && cd converter
go mod init converter
```

`converter.go`:

```go
package converter

import "fmt"

// CelsiusToFahrenheit converts Celsius to Fahrenheit.
func CelsiusToFahrenheit(c float64) float64 {
	return c*9/5 + 32
}

// FahrenheitToCelsius converts Fahrenheit to Celsius.
func FahrenheitToCelsius(f float64) float64 {
	return (f - 32) * 5 / 9
}

// Grade returns "A", "B", "C", "D", or "F" for a numeric score.
func Grade(score int) (string, error) {
	if score < 0 || score > 100 {
		return "", fmt.Errorf("score %d out of range [0, 100]", score)
	}
	switch {
	case score >= 90:
		return "A", nil
	case score >= 80:
		return "B", nil
	case score >= 70:
		return "C", nil
	case score >= 60:
		return "D", nil
	default:
		return "F", nil
	}
}
```

### Your Task

Create `converter_test.go` with the following:

**1. Refactor to subtests**: Write `TestCelsiusToFahrenheit` using `t.Run()`:

```go
func TestCelsiusToFahrenheit(t *testing.T) {
	tests := []struct {
		name    string
		celsius float64
		want    float64
	}{
		// Add cases: freezing (0 -> 32), boiling (100 -> 212),
		// body temp (37 -> 98.6), negative (-40 -> -40), zero crossing
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CelsiusToFahrenheit(tt.celsius)
			if got != tt.want {
				t.Errorf("CelsiusToFahrenheit(%v) = %v, want %v", tt.celsius, got, tt.want)
			}
		})
	}
}
```

**2. Write `TestGrade` with subtests** that test both successful grades and error cases. Use `t.Fatal` inside subtests when the error check is a precondition:

```go
t.Run(tt.name, func(t *testing.T) {
    got, err := Grade(tt.score)
    if tt.wantErr {
        if err == nil {
            t.Fatal("expected error, got nil")
        }
        return
    }
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if got != tt.want {
        t.Errorf("Grade(%d) = %q, want %q", tt.score, got, tt.want)
    }
})
```

**3. Run a single subtest** by name:

```bash
go test -v -run "TestGrade/score_90"
```

**4. Run subtests matching a pattern**:

```bash
go test -v -run "TestGrade/score"
```

### Verification

```bash
go test -v
```

You should see each subtest listed individually:

```
=== RUN   TestCelsiusToFahrenheit
=== RUN   TestCelsiusToFahrenheit/freezing
=== RUN   TestCelsiusToFahrenheit/boiling
...
--- PASS: TestCelsiusToFahrenheit (0.00s)
    --- PASS: TestCelsiusToFahrenheit/freezing (0.00s)
    --- PASS: TestCelsiusToFahrenheit/boiling (0.00s)
```

## Common Mistakes

1. **Spaces in subtest names**: `t.Run("my test", ...)` becomes `TestFoo/my_test` (spaces replaced with underscores) when filtering with `-run`. Use underscores in names to avoid confusion.

2. **Capturing loop variable in Go < 1.22**: In older Go versions, the loop variable `tt` is shared across iterations. If running subtests in parallel (covered later), you must shadow it: `tt := tt`. Go 1.22+ fixed this.

3. **Using `-run` without anchoring**: `-run TestGrade` matches `TestGrade` and `TestGradeExtra`. Use `-run "^TestGrade$"` for exact matches.

4. **Confusing parent/subtest output**: When a subtest fails, both the subtest and its parent are marked FAIL. This is normal.

## Verify What You Learned

1. What does `t.Run()` return?
2. How does `-run` filter subtests? What is the separator?
3. Why is it safe to use `t.Fatal` inside a `t.Run` callback but not in a table-driven loop?
4. How are spaces in subtest names handled?

## What's Next

The next exercise covers **benchmarks** -- measuring how fast your code runs using `testing.B` and `go test -bench`.

## Summary

- `t.Run(name, func(t *testing.T) {...})` creates a named subtest
- Subtests appear as `TestParent/subtest_name` in output
- `go test -run "TestParent/subtest"` runs specific subtests
- `t.Fatal` inside a subtest only stops that subtest, not the parent
- Subtests combine naturally with table-driven tests

## Reference

- [testing.T.Run](https://pkg.go.dev/testing#T.Run)
- [Go blog: Using Subtests and Sub-benchmarks](https://go.dev/blog/subtests)
- [Go testing flags](https://pkg.go.dev/cmd/go/internal/test)
