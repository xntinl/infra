# 13. Performance Regression Testing

<!--
difficulty: insane
concepts: [benchstat, ci-benchmarks, performance-regression, benchmark-comparison, automation]
tools: [go]
estimated_time: 45m
bloom_level: evaluate
prerequisites: [benchmarking-methodology, cpu-profiling-with-pprof, memory-profiling]
-->

## Prerequisites

- Go 1.22+ installed
- Strong benchmarking skills (timer controls, sub-benchmarks, `-count` flag)
- Familiarity with `benchstat`
- Basic CI/CD understanding

## Learning Objectives

After completing this exercise, you will be able to:

- **Design** a benchmark suite that detects meaningful performance regressions
- **Automate** benchmark comparison between Git revisions
- **Evaluate** statistical significance using `benchstat`
- **Create** a CI-compatible performance regression workflow

## The Challenge

Build a performance regression testing system for a Go project. The system must detect when a code change causes a statistically significant performance regression and report the results in a CI-friendly format.

## Requirements

1. Write stable, repeatable benchmarks for a non-trivial package
2. Create a script that compares benchmarks between the current branch and a base revision
3. Use `benchstat` to determine statistical significance
4. Define regression thresholds (e.g., >5% slowdown = failure)
5. Output CI-friendly results (exit code, human-readable summary)

## Hints

<details>
<summary>Hint 1: Stable Benchmarks</summary>

Benchmarks must be deterministic. Use fixed seeds for random data, pre-compute input, and avoid I/O in the measured loop. Lock OS thread if necessary:

```go
func BenchmarkStable(b *testing.B) {
    runtime.LockOSThread()
    defer runtime.UnlockOSThread()
    // ...
}
```
</details>

<details>
<summary>Hint 2: Comparing Git Revisions</summary>

```bash
#!/bin/bash
BASELINE_REF=${1:-main}
BENCH_COUNT=10

# Save current benchmarks
go test -bench=. -count=$BENCH_COUNT -benchmem ./... > new.txt

# Stash changes and benchmark baseline
git stash
git checkout $BASELINE_REF
go test -bench=. -count=$BENCH_COUNT -benchmem ./... > old.txt
git checkout -

# Compare
benchstat old.txt new.txt
```
</details>

<details>
<summary>Hint 3: Parsing benchstat Output</summary>

`benchstat` outputs a table with `~` (no change), `+` (regression), or `-` (improvement). Parse for `+` lines and check the percentage to determine if it exceeds your threshold.
</details>

<details>
<summary>Hint 4: CI Integration Pattern</summary>

Store baseline benchmark results as a CI artifact. On each PR, run benchmarks and compare against the stored baseline. Use exit codes to fail the build on regression:

```bash
benchstat baseline.txt current.txt | grep -E '\+[0-9]+\.[0-9]+%' && exit 1
```
</details>

## Success Criteria

- Benchmark suite covers at least 3 critical paths with sub-benchmarks for different input sizes
- Comparison script correctly identifies regressions vs improvements vs noise
- `benchstat` is used with `-count=10` or higher for statistical validity
- The script returns non-zero exit code on regression above threshold
- Results are human-readable and include the percentage change and p-value
- The system handles the case where benchmark names change between revisions

## Research Resources

- [benchstat documentation](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat)
- [Testing flags: -bench, -count, -benchtime](https://pkg.go.dev/cmd/go#hdr-Testing_flags)
- [Continuous Benchmarking in Go](https://dave.cheney.net/2013/06/30/how-to-write-benchmarks-in-go)
- [GitHub Actions for Go benchmarks](https://github.com/benchmark-action/github-action-benchmark)

## What's Next

With performance testing automated, the capstone exercise challenges you to optimize a real-world hot path end-to-end.

## Summary

Performance regression testing automates benchmark comparison between code revisions using `benchstat` for statistical analysis. Stable benchmarks require deterministic input, sufficient iteration counts (`-count=10+`), and isolation from setup. CI integration compares current benchmarks against a baseline and fails builds when regressions exceed a defined threshold. This catches performance problems before they reach production.
