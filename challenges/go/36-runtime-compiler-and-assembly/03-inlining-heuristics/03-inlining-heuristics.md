# 3. Inlining Heuristics

<!--
difficulty: advanced
concepts: [inlining, inline-budget, gcflags-m, call-overhead, mid-stack-inlining, inline-directives]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [reading-ssa-output, compiler-optimization-passes]
-->

## Prerequisites

- Go 1.22+ installed
- Ability to read SSA output and understand compiler passes
- Understanding of function call overhead

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** Go's inlining cost model and budget system
- **Predict** whether a function will be inlined using the cost heuristics
- **Use** `-gcflags='-m'` to observe inlining decisions
- **Demonstrate** how inlining enables further optimizations (escape analysis, constant propagation)

## Why Inlining Heuristics Matter

Function inlining replaces a call site with the body of the called function. This eliminates call overhead (argument setup, stack frame, return) and -- more importantly -- enables cross-function optimizations. An inlined function's variables can be stack-allocated, its constants can be propagated, and its branches can be eliminated. Understanding when Go inlines helps you write small, composable functions without fear of call overhead.

## The Problem

Write a series of functions that test the boundaries of Go's inlining heuristics. Use `-gcflags='-m'` to observe which functions are inlined and which are not, then modify the non-inlined functions to make them inlineable.

## Requirements

1. **Write functions of increasing complexity** and check which are inlined:

```go
// Simple -- should inline
func add(a, b int) int { return a + b }

// Slightly larger -- should still inline
func clamp(val, min, max int) int {
    if val < min { return min }
    if val > max { return max }
    return val
}

// Contains a loop -- may not inline
func sumTo(n int) int {
    s := 0
    for i := 0; i <= n; i++ {
        s += i
    }
    return s
}

// Contains a panic -- affects inlining budget
func mustPositive(n int) int {
    if n <= 0 {
        panic("must be positive")
    }
    return n
}
```

Check inlining decisions:

```bash
go build -gcflags='-m' -o /dev/null main.go
go build -gcflags='-m -m' -o /dev/null main.go  # More verbose
```

2. **Demonstrate mid-stack inlining** where an inlined function calls another inlineable function:

```go
func double(x int) int { return x * 2 }
func quadruple(x int) int { return double(double(x)) }

func main() {
    fmt.Println(quadruple(5))
}
```

3. **Demonstrate how inlining enables escape analysis improvements**:

```go
func newPoint(x, y int) *Point {
    return &Point{x, y}  // Escapes if not inlined, may stay on stack if inlined
}
```

4. **Demonstrate the `//go:noinline` directive** and measure the performance difference:

```go
//go:noinline
func addNoInline(a, b int) int { return a + b }

func BenchmarkInlined(b *testing.B) {
    for i := 0; i < b.N; i++ {
        _ = add(1, 2)
    }
}

func BenchmarkNotInlined(b *testing.B) {
    for i := 0; i < b.N; i++ {
        _ = addNoInline(1, 2)
    }
}
```

5. **Document the inlining budget** by writing functions that are just barely inlineable and just barely not, showing the cost threshold.

## Hints

- Go uses an "inline budget" (cost model) measured in AST node units. Functions exceeding the budget (typically 80 nodes) are not inlined.
- `-gcflags='-m'` shows "can inline" and "inlining call to" messages. `-gcflags='-m -m'` shows the cost of each function.
- Loops, `select`, `go`, `defer`, and type switches increase the inlining cost significantly.
- `panic` calls are handled specially -- the compiler may still inline functions containing panic on the cold path.
- Mid-stack inlining (since Go 1.12) means functions that call other functions can be inlined, not just leaf functions.
- `//go:noinline` prevents inlining of a specific function. Useful for benchmarking and debugging.

## Verification

```bash
go build -gcflags='-m -m' -o /dev/null main.go 2>&1 | grep -E "can inline|cannot inline|inlining"
go test -bench=. -count=5
```

Confirm that:
1. Simple functions (`add`, `clamp`) are inlined
2. Functions with loops may or may not inline depending on complexity
3. Mid-stack inlining chains work (`quadruple` inlines `double`)
4. `//go:noinline` prevents inlining and shows a measurable performance difference
5. `-gcflags='-m -m'` reports the cost of each function

## What's Next

Continue to [04 - Bounds Check Elimination](../04-bounds-check-elimination/04-bounds-check-elimination.md) to learn how the compiler removes unnecessary array bounds checks.

## Summary

- Go inlines functions whose cost is below the budget threshold (~80 AST nodes)
- Inlining eliminates call overhead and enables cross-function optimizations (escape analysis, constant propagation)
- `-gcflags='-m'` reveals inlining decisions; `-gcflags='-m -m'` shows costs
- Loops, closures, `select`, `go`, and `defer` increase inlining cost
- Mid-stack inlining (Go 1.12+) allows non-leaf functions to be inlined
- `//go:noinline` prevents inlining for testing and debugging purposes
- Small, focused functions are both readable and efficient thanks to inlining

## Reference

- [Go Compiler Inlining](https://go.dev/wiki/CompilerOptimizations#function-inlining)
- [Mid-stack inlining proposal](https://github.com/golang/proposal/blob/master/design/19348-midstack-inlining.md)
- [cmd/compile: inlining](https://github.com/golang/go/blob/master/src/cmd/compile/internal/inline/)
