# 13. Build Tags for Test Separation

<!--
difficulty: intermediate
concepts: [build-tags, build-constraints, go-build-constraint, test-separation, conditional-compilation]
tools: [go test]
estimated_time: 25m
bloom_level: apply
prerequisites: [01-your-first-test, 04-subtests-and-t-run]
-->

## Prerequisites

Before starting this exercise, you should be comfortable with:
- Writing and running Go tests
- Package organization
- `go test` flags and options

## Learning Objectives

By the end of this exercise, you will be able to:
1. Use `//go:build` constraints to separate test categories
2. Run specific test groups using `-tags` flag
3. Separate unit tests from slow, integration, or special tests
4. Apply the `!` operator to exclude tests by default

## Why This Matters

Not all tests should run every time. Unit tests should run on every save. Integration tests that hit databases should run only in CI or when explicitly requested. Expensive benchmarks should be opt-in. Build tags let you compile tests conditionally, so `go test ./...` runs only fast unit tests by default, and `go test -tags=integration ./...` includes slower tests.

## Instructions

You will create a package with unit tests, slow tests, and integration tests separated by build tags.

### Scaffold

```bash
mkdir -p ~/go-exercises/build-tags && cd ~/go-exercises/build-tags
go mod init build-tags
```

`calc.go`:

```go
package calc

import "math"

// Add returns the sum of two numbers.
func Add(a, b float64) float64 {
	return a + b
}

// Sqrt returns the square root of n.
// Returns an error string if n is negative.
func Sqrt(n float64) (float64, string) {
	if n < 0 {
		return 0, "negative input"
	}
	return math.Sqrt(n), ""
}

// IsPrime checks whether n is a prime number.
func IsPrime(n int) bool {
	if n < 2 {
		return false
	}
	for i := 2; i*i <= n; i++ {
		if n%i == 0 {
			return false
		}
	}
	return true
}
```

### Your Task

**1. Unit tests (always run)** -- `calc_test.go`:

```go
package calc

import "testing"

func TestAdd(t *testing.T) {
	tests := []struct {
		a, b, want float64
	}{
		{1, 2, 3},
		{-1, 1, 0},
		{0, 0, 0},
		{0.1, 0.2, 0.3},
	}

	for _, tt := range tests {
		got := Add(tt.a, tt.b)
		if math.Abs(got-tt.want) > 1e-9 {
			t.Errorf("Add(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestSqrt(t *testing.T) {
	got, errMsg := Sqrt(9)
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if got != 3 {
		t.Errorf("Sqrt(9) = %v, want 3", got)
	}
}

func TestSqrt_Negative(t *testing.T) {
	_, errMsg := Sqrt(-1)
	if errMsg == "" {
		t.Error("expected error for negative input")
	}
}
```

Add `import "math"` to the imports.

**2. Slow tests (opt-in with `-tags=slow`)** -- `calc_slow_test.go`:

```go
//go:build slow

package calc

import "testing"

func TestIsPrime_LargeNumbers(t *testing.T) {
	// These tests are slow because they check large primes
	primes := []int{
		104729, 224737, 350377, 479909, 611953,
	}

	for _, p := range primes {
		if !IsPrime(p) {
			t.Errorf("IsPrime(%d) = false, want true", p)
		}
	}
}

func TestIsPrime_Exhaustive(t *testing.T) {
	// Check all numbers up to 10000
	knownPrimeCount := 0
	for i := 2; i <= 10000; i++ {
		if IsPrime(i) {
			knownPrimeCount++
		}
	}

	// There are 1229 primes below 10000
	if knownPrimeCount != 1229 {
		t.Errorf("found %d primes below 10000, want 1229", knownPrimeCount)
	}
}
```

**3. Integration tests (opt-in with `-tags=integration`)** -- `calc_integration_test.go`:

```go
//go:build integration

package calc

import (
	"os"
	"testing"
)

func TestCalc_WithEnvironment(t *testing.T) {
	// This test requires specific environment setup
	precision := os.Getenv("CALC_PRECISION")
	if precision == "" {
		t.Skip("CALC_PRECISION not set")
	}
	t.Logf("Running with precision: %s", precision)

	got := Add(1.0, 2.0)
	if got != 3.0 {
		t.Errorf("Add(1, 2) = %v, want 3", got)
	}
}
```

### Verification

Run only unit tests (default):

```bash
go test -v
```

Expected: only `TestAdd`, `TestSqrt`, `TestSqrt_Negative` run. Slow and integration tests are excluded.

Run unit tests + slow tests:

```bash
go test -v -tags=slow
```

Expected: unit tests plus `TestIsPrime_LargeNumbers` and `TestIsPrime_Exhaustive` run.

Run unit tests + integration tests:

```bash
go test -v -tags=integration
```

Run everything:

```bash
go test -v -tags="slow,integration"
```

## Common Mistakes

1. **Using the old `// +build` syntax**: Go 1.17+ uses `//go:build` (no space between `//` and `go:build`). The old syntax still works but is deprecated.

2. **Putting the build tag after the package declaration**: The `//go:build` line must appear before the `package` line, with a blank line between the tag and the package.

3. **Forgetting that untagged tests always run**: Tests without build tags run regardless of `-tags` flags. Only tagged tests are conditional.

4. **Using `testing.Short()` when build tags are better**: `testing.Short()` requires `if testing.Short() { t.Skip() }` in every test. Build tags exclude files at compile time -- cleaner and harder to forget.

## Verify What You Learned

1. What is the difference between `//go:build slow` and `//go:build !slow`?
2. How do you run tests with multiple build tags?
3. Where must the `//go:build` directive appear in a file?
4. When would you use `testing.Short()` instead of build tags?

## What's Next

The next exercise covers **parallel tests** -- running tests concurrently with `t.Parallel()` for faster test suites.

## Summary

- `//go:build tag` compiles the file only when `-tags=tag` is passed
- `//go:build !tag` compiles the file only when the tag is NOT passed
- Use tags to separate unit, slow, and integration tests
- Default `go test ./...` runs only untagged tests
- `go test -tags="slow,integration" ./...` includes tagged tests
- Build tags apply to all code in the file, not just tests

## Reference

- [Build constraints](https://pkg.go.dev/cmd/go#hdr-Build_constraints)
- [Go specification: Build constraints](https://go.dev/ref/spec#Build_constraints)
- [Go 1.17 release notes: Build constraints](https://go.dev/doc/go1.17#go-build-constraint)
