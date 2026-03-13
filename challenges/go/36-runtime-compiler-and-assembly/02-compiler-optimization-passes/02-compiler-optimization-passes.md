# 2. Compiler Optimization Passes

<!--
difficulty: advanced
concepts: [compiler-passes, constant-folding, constant-propagation, copy-elimination, common-subexpression-elimination, strength-reduction, loop-optimization]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [reading-ssa-output]
-->

## Prerequisites

- Go 1.22+ installed
- Ability to generate and read SSA output from exercise 01
- Basic understanding of compiler optimization concepts

## Learning Objectives

After completing this exercise, you will be able to:

- **Identify** the major optimization passes in the Go compiler's SSA pipeline
- **Demonstrate** constant folding, propagation, copy elimination, and CSE through targeted examples
- **Analyze** how each pass transforms the SSA representation
- **Compare** optimized vs unoptimized output to understand the impact of each pass

## Why Understanding Compiler Passes Matters

The Go compiler applies dozens of optimization passes to your code. Understanding which optimizations exist helps you write code that the compiler can optimize effectively, avoid patterns that defeat optimization, and interpret performance differences. It also demystifies what happens between `go build` and the running binary.

## The Problem

Write Go functions that specifically trigger different compiler optimization passes. For each function, generate SSA output and compare the "before" and "after" states of the relevant pass to confirm the optimization fired.

## Requirements

1. **Constant folding and propagation** -- write a function where the compiler can compute the result at compile time:

```go
func constantFolding() int {
    a := 10
    b := 20
    c := a + b    // Folded to 30 at compile time
    d := c * 2    // Folded to 60
    return d + 1  // Folded to 61
}
```

Generate SSA and verify the entire computation is replaced by a single constant.

2. **Common subexpression elimination (CSE)** -- write a function with repeated computations:

```go
func cse(a, b int) int {
    x := a*b + 1
    y := a*b + 2  // a*b is a common subexpression
    return x + y
}
```

3. **Dead code elimination** -- write a function with unreachable code:

```go
func deadCode(x int) int {
    if true {
        return x + 1
    }
    return x * 100  // Dead code -- never reached
}
```

4. **Strength reduction** -- write code using operations the compiler can replace with cheaper ones:

```go
func strengthReduction(x int) int {
    a := x * 2    // Can be replaced with x << 1
    b := x * 8    // Can be replaced with x << 3
    c := x / 4    // Can be replaced with x >> 2 (for unsigned)
    return a + b + c
}
```

5. **Nil check elimination** -- write code where the compiler can prove a pointer is non-nil:

```go
func nilCheckElim(p *int) int {
    _ = *p        // First dereference proves p != nil
    return *p + 1 // Second dereference does not need a nil check
}
```

6. **Write a `main` function** and build with optimization disabled vs enabled to compare:

```bash
# Normal (optimized)
GOSSAFUNC=constantFolding go build -o /dev/null main.go

# Disabled optimizations
GOSSAFUNC=constantFolding go build -gcflags='-N -l' -o /dev/null main.go
```

## Hints

- Use `GOSSAFUNC` to generate SSA HTML and compare phases. The "opt" phase is where most high-level optimizations happen.
- `-gcflags='-N'` disables optimizations and `-gcflags='-l'` disables inlining. Use both to see the unoptimized baseline.
- Constant folding happens very early. By the "opt" phase, constant expressions are already resolved.
- CSE is visible when two SSA values with the same operation and operands are merged into one.
- The compiler's dead code elimination removes basic blocks with no predecessors.
- Strength reduction replaces expensive operations (multiply, divide) with shifts and adds. Look for `Lsh` (left shift) and `Rsh` (right shift) operations in the lowered SSA.

## Verification

```bash
GOSSAFUNC=constantFolding go build -o /dev/null main.go
GOSSAFUNC=cse go build -o /dev/null main.go
GOSSAFUNC=deadCode go build -o /dev/null main.go
GOSSAFUNC=strengthReduction go build -o /dev/null main.go
GOSSAFUNC=nilCheckElim go build -o /dev/null main.go
```

Confirm that:
1. `constantFolding` is reduced to returning a single constant in the final SSA
2. `cse` computes `a*b` only once
3. `deadCode` eliminates the unreachable `return x * 100` block
4. `strengthReduction` replaces multiplications with shifts
5. `nilCheckElim` has only one nil check, not two

## What's Next

Continue to [03 - Inlining Heuristics](../03-inlining-heuristics/03-inlining-heuristics.md) to understand when and why the Go compiler inlines function calls.

## Summary

- The Go compiler applies many optimization passes: constant folding, CSE, dead code elimination, strength reduction, nil check elimination, and more
- Each pass transforms the SSA representation, and the effect is visible in `GOSSAFUNC` HTML output
- `-gcflags='-N -l'` disables optimizations and inlining for comparison
- Writing code that the compiler can optimize is better than hand-optimizing -- the compiler knows the target architecture
- Understanding passes helps diagnose why expected performance gains do not materialize

## Reference

- [Go Compiler SSA Passes](https://github.com/golang/go/blob/master/src/cmd/compile/internal/ssa/compile.go) -- list of all SSA passes
- [Go Compiler README](https://go.dev/src/cmd/compile/README)
- [SSA optimization rules](https://github.com/golang/go/tree/master/src/cmd/compile/internal/ssa/_gen) -- the rewrite rules that drive optimization
