# 5. PGO: Profile-Guided Optimization

<!--
difficulty: advanced
concepts: [pgo, profile-guided-optimization, cpu-profile, hot-path, devirtualization, inlining-budget, pprof]
tools: [go, pprof]
estimated_time: 40m
bloom_level: analyze
prerequisites: [inlining-heuristics, compiler-optimization-passes]
-->

## Prerequisites

- Go 1.22+ installed (PGO enabled by default since Go 1.21)
- Understanding of inlining and compiler optimization passes
- Familiarity with `runtime/pprof` or `net/http/pprof`

## Learning Objectives

After completing this exercise, you will be able to:

- **Generate** a CPU profile suitable for PGO-guided compilation
- **Build** a Go binary with PGO enabled using a `default.pgo` profile
- **Measure** the performance improvement from PGO on a realistic workload
- **Explain** which optimizations PGO enhances (inlining, devirtualization)

## Why PGO Matters

Profile-Guided Optimization (PGO) uses runtime profiling data from production (or representative benchmarks) to guide compiler decisions. The compiler can inline functions that are hot in practice (even if they exceed the normal budget), devirtualize interface calls that almost always resolve to one concrete type, and optimize branch layout for the common path. PGO typically improves throughput by 2-7% with zero code changes.

## The Problem

Build a program with meaningful work (JSON parsing, sorting, interface dispatching), profile it under a representative workload, then rebuild with PGO and measure the improvement.

## Requirements

1. **Build a workload program** that exercises multiple code paths: JSON decoding, sorting, string processing, and interface method dispatch:

```go
type Processor interface {
    Process(data []byte) (int, error)
}

type JSONProcessor struct{}
type CSVProcessor struct{}

func (j JSONProcessor) Process(data []byte) (int, error) { /* ... */ }
func (c CSVProcessor) Process(data []byte) (int, error) { /* ... */ }

func runWorkload(processors []Processor, inputs [][]byte) int {
    total := 0
    for _, p := range processors {
        for _, input := range inputs {
            n, _ := p.Process(input)
            total += n
        }
    }
    return total
}
```

2. **Write a benchmark** that exercises the workload:

```go
func BenchmarkWorkload(b *testing.B) {
    processors, inputs := setupWorkload()
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        runWorkload(processors, inputs)
    }
}
```

3. **Generate a CPU profile**:

```bash
go test -bench=BenchmarkWorkload -cpuprofile=default.pgo -count=10
```

4. **Rebuild with PGO** and benchmark again:

```bash
# PGO is auto-detected when default.pgo exists in the main package directory
go test -bench=BenchmarkWorkload -count=10
```

5. **Compare results** using `benchstat`:

```bash
go test -bench=BenchmarkWorkload -count=10 > nopgo.txt
# Move default.pgo into place
go test -bench=BenchmarkWorkload -count=10 > withpgo.txt
benchstat nopgo.txt withpgo.txt
```

6. **Verify PGO is active** using build info:

```bash
go version -m ./binary | grep pgo
```

## Hints

- Place the profile as `default.pgo` in the main package directory. The Go toolchain picks it up automatically since Go 1.21.
- PGO primarily helps with: (a) more aggressive inlining of hot functions, (b) devirtualization of interface calls where one concrete type dominates, (c) better code layout.
- The profile does not need to be from the exact same binary. A profile from production or a representative benchmark works.
- Use `-pgo=off` to explicitly disable PGO for comparison: `go test -pgo=off -bench=...`
- PGO improvements compound with other optimizations. A devirtualized interface call can then be inlined, which enables further escape analysis improvements.
- `go build -gcflags='-m -m'` with and without PGO shows different inlining decisions.

## Verification

```bash
# Generate profile
go test -bench=BenchmarkWorkload -cpuprofile=default.pgo -count=1

# Benchmark without PGO
go test -pgo=off -bench=BenchmarkWorkload -count=10 > nopgo.txt

# Benchmark with PGO
go test -bench=BenchmarkWorkload -count=10 > withpgo.txt

# Compare
benchstat nopgo.txt withpgo.txt
```

Confirm that:
1. A `default.pgo` file is generated from the profiling run
2. The PGO build detects and uses the profile
3. `benchstat` shows a measurable improvement (typically 2-7%)
4. `go version -m` on the PGO binary shows the profile was applied

## What's Next

Continue to [06 - Compiler Devirtualization](../06-compiler-devirtualization/06-compiler-devirtualization.md) to understand how the compiler converts interface calls to direct calls.

## Summary

- PGO uses runtime CPU profiles to guide compiler optimization decisions
- Place a `default.pgo` file in the main package directory for automatic PGO
- PGO enhances inlining (hot functions get higher budget), devirtualization, and code layout
- Typical improvement is 2-7% throughput with zero code changes
- The profile can come from production or representative benchmarks
- Use `benchstat` to measure the improvement across multiple runs
- PGO is a free performance win that every production Go service should use

## Reference

- [Profile-guided optimization in Go](https://go.dev/doc/pgo) -- official PGO documentation
- [PGO proposal](https://github.com/golang/go/issues/55022) -- design discussion and rationale
- [Go 1.21 PGO](https://go.dev/blog/pgo) -- announcement blog post
