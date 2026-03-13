# 5. Escape Analysis

<!--
difficulty: advanced
concepts: [escape-analysis, stack-vs-heap, gcflags, compiler-optimizations, allocation-reduction]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [memory-profiling, benchmarking-methodology, pointers]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of stack vs heap memory
- Familiarity with Go pointers and benchmarking

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** `go build -gcflags="-m"` to inspect escape analysis decisions
- **Explain** why a variable escapes to the heap
- **Refactor** code to reduce heap allocations by keeping values on the stack
- **Measure** the performance impact of escape vs no-escape

## Why Escape Analysis

Go's compiler decides at compile time whether a variable can live on the stack (cheap, automatic cleanup) or must escape to the heap (requires GC). Understanding escape analysis lets you write code that stays on the stack when possible, reducing GC pressure.

Common reasons a variable escapes:
- Returned as a pointer from a function
- Captured by a goroutine's closure
- Stored in an interface value (sometimes)
- Too large for the stack
- Passed to a function that might retain it

## The Problem

You have several functions with different allocation patterns. Determine which allocations escape to the heap, understand why, and refactor to eliminate unnecessary escapes.

## Requirements

1. Use `-gcflags="-m"` to see escape analysis output
2. Identify at least three different reasons for heap escape
3. Refactor functions to reduce escapes and benchmark the difference

## Step 1 -- Observe Escape Decisions

Create a project:

```bash
mkdir -p ~/go-exercises/escape-analysis && cd ~/go-exercises/escape-analysis
go mod init escape-analysis
```

Create `escape.go`:

```go
package main

// Returns a pointer -- the value MUST escape to the heap.
func newInt(x int) *int {
	v := x // v escapes because its address is returned
	return &v
}

// Returns a value -- stays on the stack.
func copyInt(x int) int {
	v := x
	return v
}

// Interface assignment may cause escape.
func toInterface(x int) interface{} {
	return x // x escapes because interface values are heap-allocated
}

// Large struct on stack vs heap.
type SmallStruct struct {
	A, B int
}

type LargeStruct struct {
	Data [1024 * 1024]byte // 1MB
}

func makeSmall() SmallStruct {
	return SmallStruct{A: 1, B: 2} // stays on stack
}

func makeLargePointer() *LargeStruct {
	s := LargeStruct{} // escapes: too large + returned as pointer
	return &s
}

// Closure capture.
func closureCapture() func() int {
	x := 42
	return func() int {
		return x // x escapes: captured by closure that outlives the function
	}
}

func main() {
	_ = newInt(10)
	_ = copyInt(10)
	_ = toInterface(10)
	_ = makeSmall()
	_ = makeLargePointer()
	_ = closureCapture()
}
```

Run escape analysis:

```bash
go build -gcflags="-m" escape.go
```

For more detail:

```bash
go build -gcflags="-m -m" escape.go
```

## Step 2 -- Interpret the Output

The compiler prints lines like:

```
./escape.go:5:2: moved to heap: v
./escape.go:15:9: x escapes to heap
./escape.go:30:9: &s escapes to heap
```

Map each escape to its cause:
- `newInt`: returns pointer to local variable
- `toInterface`: interface conversion allocates
- `makeLargePointer`: pointer to large struct returned
- `closureCapture`: variable captured by escaping closure

## Step 3 -- Refactor to Reduce Escapes

Create `escape_fixed.go`:

```go
package main

// Instead of returning a pointer, return the value.
func newIntFixed(x int) int {
	return x // stays on stack
}

// Accept a pointer to fill instead of returning one.
func fillLargeStruct(s *LargeStruct) {
	s.Data[0] = 1 // caller controls allocation
}

// Avoid interface when concrete type suffices.
func doubleInt(x int) int {
	return x * 2 // no interface, no escape
}
```

## Step 4 -- Benchmark the Difference

Create `escape_test.go`:

```go
package main

import "testing"

func BenchmarkNewInt(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		p := newInt(i)
		_ = p
	}
}

func BenchmarkCopyInt(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		v := copyInt(i)
		_ = v
	}
}

func BenchmarkToInterface(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		v := toInterface(i)
		_ = v
	}
}

func BenchmarkDoubleInt(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		v := doubleInt(i)
		_ = v
	}
}

func BenchmarkMakeLargePointer(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		p := makeLargePointer()
		_ = p
	}
}

func BenchmarkFillLargeStruct(b *testing.B) {
	b.ReportAllocs()
	s := &LargeStruct{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fillLargeStruct(s)
	}
}
```

```bash
go test -bench=. -benchmem
```

## Hints

- `-gcflags="-m"` shows escape decisions; `-m -m` shows the reasoning
- The compiler inlines small functions, which can prevent escapes
- `//go:noinline` directive prevents inlining (useful for benchmarking)
- A value only escapes if the compiler cannot prove it doesn't outlive its stack frame

## Verification

- `go build -gcflags="-m"` shows specific escape reasons for each function
- `BenchmarkCopyInt` shows 0 allocs/op while `BenchmarkNewInt` shows 1 alloc/op
- `BenchmarkFillLargeStruct` shows 0 allocs/op while `BenchmarkMakeLargePointer` shows 1 alloc/op
- `BenchmarkDoubleInt` shows 0 allocs/op while `BenchmarkToInterface` shows 1 alloc/op

## What's Next

Escape analysis tells you what escapes. The next exercise covers struct field ordering to optimize cache line usage for the data that stays in memory.

## Summary

Go's escape analysis determines at compile time whether variables live on the stack or heap. Use `go build -gcflags="-m"` to inspect these decisions. Variables escape when returned as pointers, captured by closures, stored in interfaces, or are too large for the stack. Refactoring to return values instead of pointers, pre-allocating buffers, and avoiding unnecessary interface conversions keeps data on the stack, reducing GC pressure.

## Reference

- [Go FAQ: Stack or Heap](https://go.dev/doc/faq#stack_or_heap)
- [Go Compiler Directives](https://pkg.go.dev/cmd/compile)
- [Allocation Efficiency in High-Performance Go Services](https://segment.com/blog/allocation-efficiency-in-high-performance-go-services/)
