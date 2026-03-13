<!-- difficulty: intermediate -->
<!-- concepts: testing.B, b.N, go test -bench, -benchmem -->
<!-- tools: go test -->
<!-- estimated_time: 25m -->
<!-- bloom_level: apply -->
<!-- prerequisites: 01-your-first-test, 02-table-driven-tests -->

# Benchmarks

## Prerequisites

Before starting this exercise, you should be comfortable with:
- Writing and running Go tests
- String manipulation and slices
- Command-line flags

## Learning Objectives

By the end of this exercise, you will be able to:
1. Write benchmark functions using `testing.B`
2. Understand and interpret `b.N` and benchmark output
3. Run benchmarks with `go test -bench` and `-benchmem`
4. Compare performance of different implementations

## Why This Matters

When performance matters, opinions are worthless -- only measurements count. Go's built-in benchmark framework lets you measure execution time and memory allocations with a single command. Before optimizing, benchmark to establish a baseline. After optimizing, benchmark to prove the improvement is real. This discipline prevents premature optimization and validates real gains.

## Instructions

You will benchmark two approaches to string concatenation and observe measurable differences.

### Scaffold

```bash
mkdir -p concat && cd concat
go mod init concat
```

`concat.go`:

```go
package concat

import "strings"

// ConcatPlus joins strings using the + operator.
func ConcatPlus(parts []string) string {
	result := ""
	for _, p := range parts {
		result += p
	}
	return result
}

// ConcatBuilder joins strings using strings.Builder.
func ConcatBuilder(parts []string) string {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p)
	}
	return b.String()
}

// ConcatJoin uses strings.Join.
func ConcatJoin(parts []string) string {
	return strings.Join(parts, "")
}
```

### Your Task

Create `concat_test.go` with:

**1. A basic benchmark for `ConcatPlus`**:

```go
package concat

import "testing"

func BenchmarkConcatPlus(b *testing.B) {
	parts := []string{"hello", " ", "world", " ", "from", " ", "go"}
	for i := 0; i < b.N; i++ {
		ConcatPlus(parts)
	}
}
```

Key points:
- Benchmark functions start with `Benchmark` (not `Test`)
- They take `*testing.B` (not `*testing.T`)
- The loop runs `b.N` times -- the framework adjusts `b.N` to get stable measurements

**2. Matching benchmarks for `ConcatBuilder` and `ConcatJoin`** following the same pattern.

**3. A benchmark with larger input** to see how performance diverges:

```go
func BenchmarkConcatPlus100(b *testing.B) {
	parts := make([]string, 100)
	for i := range parts {
		parts[i] = "word"
	}
	for i := 0; i < b.N; i++ {
		ConcatPlus(parts)
	}
}
```

Create matching `BenchmarkConcatBuilder100` and `BenchmarkConcatJoin100`.

**4. A sub-benchmark** using `b.Run`:

```go
func BenchmarkConcat(b *testing.B) {
	sizes := []int{10, 100, 1000}
	for _, size := range sizes {
		parts := make([]string, size)
		for i := range parts {
			parts[i] = "word"
		}
		b.Run(fmt.Sprintf("Plus/%d", size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				ConcatPlus(parts)
			}
		})
		b.Run(fmt.Sprintf("Builder/%d", size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				ConcatBuilder(parts)
			}
		})
	}
}
```

### Verification

Run benchmarks (the `.` runs all, `-run=^$` skips regular tests):

```bash
go test -bench=. -run=^$ -benchmem
```

You should see output like:

```
BenchmarkConcatPlus-8        5000000    230 ns/op    56 B/op    3 allocs/op
BenchmarkConcatBuilder-8    10000000    120 ns/op    64 B/op    1 allocs/op
BenchmarkConcatJoin-8       15000000     85 ns/op    32 B/op    1 allocs/op
```

The columns mean:
- **iterations**: how many times the loop ran
- **ns/op**: nanoseconds per operation
- **B/op**: bytes allocated per operation
- **allocs/op**: allocations per operation

Run only builder benchmarks:

```bash
go test -bench=Builder -run=^$ -benchmem
```

## Common Mistakes

1. **Not using `b.N`**: The benchmark loop must use `b.N`, not a hardcoded number. The framework calibrates `b.N` for stable results.

2. **Setup inside the loop**: Setup code (creating test data) should run before the `b.N` loop, or use `b.ResetTimer()` if setup is expensive.

3. **Compiler optimizing away results**: If the result of the function is not used, the compiler may eliminate the call. Assign to a package-level variable:
   ```go
   var result string
   func BenchmarkX(b *testing.B) {
       for i := 0; i < b.N; i++ {
           result = ConcatPlus(parts)
       }
   }
   ```

4. **Running benchmarks alongside tests**: Use `-run=^$` to skip tests when benchmarking to avoid noise.

## Verify What You Learned

1. What is `b.N` and who controls its value?
2. What flag enables memory allocation statistics?
3. How do you run only benchmarks matching a pattern?
4. Why should setup code be outside the `b.N` loop?

## What's Next

The next exercise covers **fuzz testing** -- letting Go generate random inputs to find edge cases you never thought of.

## Summary

- Benchmark functions: `func BenchmarkXxx(b *testing.B)` with a `b.N` loop
- `go test -bench=.` runs all benchmarks; `-benchmem` adds memory stats
- `b.Run()` creates sub-benchmarks for comparing variants
- Put setup before the loop; use `b.ResetTimer()` if needed
- Always benchmark before and after optimizations

## Reference

- [testing.B](https://pkg.go.dev/testing#B)
- [Go blog: Profiling Go Programs](https://go.dev/blog/pprof)
- [benchstat tool](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat)
