# 4. Bounds Check Elimination

<!--
difficulty: advanced
concepts: [bounds-check-elimination, bce, prove-pass, compiler-hints, slice-safety, index-bounds]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [reading-ssa-output, compiler-optimization-passes]
-->

## Prerequisites

- Go 1.22+ installed
- Ability to read SSA output from exercise 01
- Understanding of slices and array indexing

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** how the Go compiler proves array/slice accesses are within bounds
- **Write** code patterns that enable bounds check elimination (BCE)
- **Identify** when bounds checks remain using `-gcflags='-d=ssa/check_bce/debug=1'`
- **Measure** the performance impact of bounds checks in hot loops

## Why Bounds Check Elimination Matters

Go performs bounds checking on every array and slice access. This prevents buffer overflows but adds overhead -- a compare-and-branch on every index operation. The compiler's `prove` pass can eliminate bounds checks when it can statically prove the index is within bounds. In tight loops processing large arrays, eliminated bounds checks can improve performance by 10-30%.

## The Problem

Write functions that process slices and arrays, then use compiler diagnostics to observe which bounds checks are eliminated and which remain. Restructure the remaining checks to help the compiler eliminate them.

## Requirements

1. **Write a function with eliminable bounds checks** -- ascending loop with known length:

```go
func sumArray(arr [256]int) int {
    total := 0
    for i := 0; i < len(arr); i++ {
        total += arr[i] // BCE: compiler knows i < len(arr)
    }
    return total
}
```

2. **Write a function with non-obvious bounds checks** -- reversed or strided access:

```go
func reverseSum(s []int) int {
    total := 0
    for i := len(s) - 1; i >= 0; i-- {
        total += s[i]
    }
    return total
}

func stridedAccess(s []int) int {
    total := 0
    for i := 0; i < len(s)-3; i += 4 {
        total += s[i] + s[i+1] + s[i+2] + s[i+3]
    }
    return total
}
```

3. **Demonstrate the "prove by asserting length" pattern** that helps the compiler:

```go
func processQuad(s []int) int {
    // Without hint: each access needs a bounds check
    // With hint: one check proves all four accesses are safe
    if len(s) < 4 {
        return 0
    }
    s = s[:4] // Compiler hint: s is exactly length 4
    return s[0] + s[1] + s[2] + s[3]
}
```

4. **Write a benchmark** comparing bounds-checked vs eliminated versions:

```go
func BenchmarkWithBoundsCheck(b *testing.B) {
    data := make([]int, 1024)
    for i := 0; i < b.N; i++ {
        _ = sumWithBoundsChecks(data)
    }
}

func BenchmarkWithoutBoundsCheck(b *testing.B) {
    data := make([]int, 1024)
    for i := 0; i < b.N; i++ {
        _ = sumWithBCE(data)
    }
}
```

5. **Check bounds check elimination with compiler diagnostics**:

```bash
go build -gcflags='-d=ssa/check_bce/debug=1' main.go
```

## Hints

- `-gcflags='-d=ssa/check_bce/debug=1'` reports which index operations still have bounds checks
- The compiler eliminates bounds checks when it can prove `0 <= index < len(slice)` at compile time
- `for i := 0; i < len(s); i++` naturally proves bounds. `for i := range s` does too.
- Multi-element access (`s[i], s[i+1], s[i+2]`) needs the compiler to prove `i+2 < len(s)`. A guard clause `if len(s) < i+3` helps.
- Re-slicing (`s = s[:n]`) gives the compiler a tighter bound to work with
- The `prove` pass in SSA performs this analysis. Use `GOSSAFUNC` to see it in action.

## Verification

```bash
go build -gcflags='-d=ssa/check_bce/debug=1' main.go 2>&1
go test -bench=. -count=5
```

Confirm that:
1. Simple ascending loops show no remaining bounds checks
2. Strided access shows remaining bounds checks without the guard clause
3. Adding the guard clause or re-slice eliminates the remaining checks
4. The benchmark shows measurable improvement when bounds checks are eliminated

## What's Next

Continue to [05 - PGO: Profile-Guided Optimization](../05-pgo-profile-guided-optimization/05-pgo-profile-guided-optimization.md) to learn how runtime profiling data guides compiler optimization decisions.

## Summary

- Go inserts bounds checks on every slice/array access for safety
- The compiler's `prove` pass eliminates checks when it can statically prove the index is valid
- Simple `for i := 0; i < len(s); i++` loops get automatic BCE
- Multi-element access benefits from guard clauses that assert minimum length
- Re-slicing (`s[:n]`) helps the compiler narrow bounds
- `-gcflags='-d=ssa/check_bce/debug=1'` reveals remaining bounds checks
- In hot loops, BCE can yield 10-30% performance improvement

## Reference

- [Bounds Check Elimination in Go](https://go101.org/article/bounds-check-elimination.html)
- [SSA prove pass](https://github.com/golang/go/blob/master/src/cmd/compile/internal/ssa/prove.go)
- [Go Compiler Optimizations](https://go.dev/wiki/CompilerOptimizations)
