# 4. Benchmarking Methodology

<!--
difficulty: advanced
concepts: [benchmarking, b-resettimer, b-stoptimer, b-reportallocs, benchstat, sub-benchmarks]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [testing-basics, cpu-profiling-with-pprof, memory-profiling]
-->

## Prerequisites

- Go 1.22+ installed
- Experience writing Go benchmarks
- Familiarity with `go test -bench` output

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** `b.ResetTimer`, `b.StopTimer`, and `b.StartTimer` to isolate measured code
- **Interpret** `b.ReportAllocs` and `-benchmem` output correctly
- **Apply** sub-benchmarks to compare implementations across input sizes
- **Analyze** benchmark stability using multiple runs

## Why Benchmarking Methodology

A benchmark is only as good as its methodology. If setup code is included in the measurement, results are skewed. If you run the benchmark once, noise dominates the signal. Go's testing framework provides precise timer controls and the `benchstat` tool provides statistical analysis, but you need to use them correctly.

## The Problem

You have two implementations of a function and need to determine which is faster, by how much, and whether the difference is statistically significant. You must isolate setup from measured code and handle benchmarks that need pre-computed data.

## Requirements

1. Write a benchmark that uses `b.ResetTimer()` to exclude setup from measurement
2. Write a benchmark that uses `b.StopTimer()`/`b.StartTimer()` for per-iteration setup
3. Use `b.ReportAllocs()` to track allocations
4. Create sub-benchmarks to compare behavior across input sizes
5. Run benchmarks multiple times and compare results

## Step 1 -- Isolate Setup with ResetTimer

Create a project:

```bash
mkdir -p ~/go-exercises/bench-method && cd ~/go-exercises/bench-method
go mod init bench-method
```

Create `search.go`:

```go
package main

import "sort"

func LinearSearch(sorted []int, target int) bool {
	for _, v := range sorted {
		if v == target {
			return true
		}
	}
	return false
}

func BinarySearch(sorted []int, target int) bool {
	i := sort.SearchInts(sorted, target)
	return i < len(sorted) && sorted[i] == target
}
```

Create `search_test.go`:

```go
package main

import "testing"

func BenchmarkLinearSearch(b *testing.B) {
	// Setup: create a large sorted slice
	data := make([]int, 10_000)
	for i := range data {
		data[i] = i * 2
	}
	target := 9_999 // near the end

	b.ResetTimer() // exclude setup from measurement

	for i := 0; i < b.N; i++ {
		LinearSearch(data, target)
	}
}

func BenchmarkBinarySearch(b *testing.B) {
	data := make([]int, 10_000)
	for i := range data {
		data[i] = i * 2
	}
	target := 9_999

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		BinarySearch(data, target)
	}
}
```

```bash
go test -bench=. -count=5
```

## Step 2 -- Per-Iteration Setup with StopTimer/StartTimer

When each benchmark iteration needs fresh input (e.g., a shuffled slice), use `b.StopTimer()` and `b.StartTimer()`:

Add to `search_test.go`:

```go
import "math/rand"

func BenchmarkSortThenSearch(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		// Per-iteration setup: create unsorted data
		data := make([]int, 1_000)
		for j := range data {
			data[j] = rand.Intn(10_000)
		}
		b.StartTimer()

		// Only this part is measured
		sort.Ints(data)
		BinarySearch(data, 5_000)
	}
}
```

**Warning**: `StopTimer`/`StartTimer` have overhead. If the measured code takes less than ~100ns, the timer calls themselves distort results. Use `ResetTimer` with a single setup phase when possible.

## Step 3 -- Sub-Benchmarks for Multiple Sizes

Sub-benchmarks let you compare performance across input sizes:

```go
func BenchmarkSearch(b *testing.B) {
	sizes := []int{100, 1_000, 10_000, 100_000}

	for _, size := range sizes {
		data := make([]int, size)
		for i := range data {
			data[i] = i * 2
		}
		target := size - 1 // worst case for linear

		b.Run(fmt.Sprintf("Linear/%d", size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				LinearSearch(data, target)
			}
		})

		b.Run(fmt.Sprintf("Binary/%d", size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				BinarySearch(data, target)
			}
		})
	}
}
```

```bash
go test -bench=BenchmarkSearch -benchmem
```

## Step 4 -- Statistical Comparison with benchstat

Run benchmarks multiple times and save output:

```bash
go test -bench=BenchmarkSearch -count=10 > results.txt
```

Install and use `benchstat`:

```bash
go install golang.org/x/perf/cmd/benchstat@latest
benchstat results.txt
```

`benchstat` shows the median, the confidence interval, and whether differences are statistically significant.

To compare two implementations, save results before and after optimization:

```bash
go test -bench=BenchmarkSearch -count=10 > old.txt
# ... make optimization changes ...
go test -bench=BenchmarkSearch -count=10 > new.txt
benchstat old.txt new.txt
```

## Hints

- Always use `-count=N` with N >= 5 to get statistically meaningful results
- Close other programs while benchmarking to reduce noise
- Use `b.ReportAllocs()` or `-benchmem` to see allocation impact
- The `b.N` loop must contain only the code being measured
- Never use `b.N` as input to the function being benchmarked (this changes the workload)

## Verification

Run the full benchmark suite:

```bash
go test -bench=. -benchmem -count=5
```

Confirm that:
- Binary search is faster than linear search for large inputs
- The difference grows with input size (O(log n) vs O(n))
- `benchstat` shows the difference is statistically significant (p < 0.05)
- `ReportAllocs` shows 0 allocs/op for pure search operations

## What's Next

With solid benchmarking methodology, you're ready to learn escape analysis -- understanding when Go allocates on the heap vs the stack.

## Summary

Rigorous benchmarking requires isolating setup from measurement using `b.ResetTimer()`, `b.StopTimer()`, and `b.StartTimer()`. Sub-benchmarks organize comparisons across input sizes. Always run with `-count=N` (N >= 5) and use `benchstat` for statistical analysis. `b.ReportAllocs()` reveals allocation costs that may not be visible in raw timing.

## Reference

- [testing.B documentation](https://pkg.go.dev/testing#B)
- [benchstat tool](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat)
- [How to Write Benchmarks in Go](https://dave.cheney.net/2013/06/30/how-to-write-benchmarks-in-go)
