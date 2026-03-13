# 6. Struct Field Ordering and Cache Lines

<!--
difficulty: advanced
concepts: [cache-lines, struct-padding, memory-alignment, field-ordering, cpu-cache]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [escape-analysis, benchmarking-methodology, structs-and-methods]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of struct memory layout
- Familiarity with benchmarking in Go

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** how CPU cache lines affect struct access performance
- **Calculate** struct size including padding using `unsafe.Sizeof`
- **Reorder** struct fields to minimize padding and improve cache locality
- **Measure** the performance impact of field ordering

## Why Struct Field Ordering

Modern CPUs load memory in cache lines (typically 64 bytes). When you access a struct field, the entire cache line containing that field is loaded. If a struct has poor field ordering, padding bytes are inserted between fields to satisfy alignment requirements, wasting cache space and potentially causing a struct to span more cache lines than necessary.

Go's compiler does not automatically reorder struct fields (unlike some C compilers). The programmer controls the layout.

## The Problem

Analyze structs with different field orderings, calculate their sizes with padding, and benchmark access patterns to observe the performance difference.

## Requirements

1. Create structs with suboptimal and optimal field ordering
2. Use `unsafe.Sizeof` and `unsafe.Alignof` to measure the difference
3. Benchmark iteration over slices of each struct variant
4. Explain why the optimized ordering is faster

## Step 1 -- Measure Struct Sizes

```bash
mkdir -p ~/go-exercises/cache-lines && cd ~/go-exercises/cache-lines
go mod init cache-lines
```

Create `structs.go`:

```go
package main

import (
	"fmt"
	"unsafe"
)

// BadOrder: fields arranged for maximum padding.
// bool(1) + padding(7) + float64(8) + bool(1) + padding(3) + int32(4) +
// int64(8) + bool(1) + padding(7) = 40 bytes
type BadOrder struct {
	Active  bool
	Balance float64
	IsAdmin bool
	Age     int32
	ID      int64
	Deleted bool
}

// GoodOrder: fields arranged largest to smallest.
// float64(8) + int64(8) + int32(4) + bool(1) + bool(1) + bool(1) +
// padding(1) = 24 bytes
type GoodOrder struct {
	Balance float64
	ID      int64
	Age     int32
	Active  bool
	IsAdmin bool
	Deleted bool
}

func main() {
	fmt.Printf("BadOrder:  size=%d  align=%d\n",
		unsafe.Sizeof(BadOrder{}), unsafe.Alignof(BadOrder{}))
	fmt.Printf("GoodOrder: size=%d  align=%d\n",
		unsafe.Sizeof(GoodOrder{}), unsafe.Alignof(GoodOrder{}))

	// Show individual field offsets
	var b BadOrder
	fmt.Printf("\nBadOrder field offsets:\n")
	fmt.Printf("  Active:  %d\n", unsafe.Offsetof(b.Active))
	fmt.Printf("  Balance: %d\n", unsafe.Offsetof(b.Balance))
	fmt.Printf("  IsAdmin: %d\n", unsafe.Offsetof(b.IsAdmin))
	fmt.Printf("  Age:     %d\n", unsafe.Offsetof(b.Age))
	fmt.Printf("  ID:      %d\n", unsafe.Offsetof(b.ID))
	fmt.Printf("  Deleted: %d\n", unsafe.Offsetof(b.Deleted))

	var g GoodOrder
	fmt.Printf("\nGoodOrder field offsets:\n")
	fmt.Printf("  Balance: %d\n", unsafe.Offsetof(g.Balance))
	fmt.Printf("  ID:      %d\n", unsafe.Offsetof(g.ID))
	fmt.Printf("  Age:     %d\n", unsafe.Offsetof(g.Age))
	fmt.Printf("  Active:  %d\n", unsafe.Offsetof(g.Active))
	fmt.Printf("  IsAdmin: %d\n", unsafe.Offsetof(g.IsAdmin))
	fmt.Printf("  Deleted: %d\n", unsafe.Offsetof(g.Deleted))
}
```

```bash
go run structs.go
```

## Step 2 -- Benchmark Cache Effects

Create `structs_test.go`:

```go
package main

import "testing"

const N = 1_000_000

func BenchmarkBadOrderIterate(b *testing.B) {
	items := make([]BadOrder, N)
	for i := range items {
		items[i] = BadOrder{
			ID: int64(i), Balance: float64(i), Age: int32(i),
			Active: true, IsAdmin: false, Deleted: false,
		}
	}
	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		sum := int64(0)
		for i := range items {
			sum += items[i].ID
		}
		_ = sum
	}
}

func BenchmarkGoodOrderIterate(b *testing.B) {
	items := make([]GoodOrder, N)
	for i := range items {
		items[i] = GoodOrder{
			ID: int64(i), Balance: float64(i), Age: int32(i),
			Active: true, IsAdmin: false, Deleted: false,
		}
	}
	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		sum := int64(0)
		for i := range items {
			sum += items[i].ID
		}
		_ = sum
	}
}
```

```bash
go test -bench=. -count=5 -benchmem
```

## Step 3 -- Use fieldalignment Tool

Go provides a vet analyzer to find struct padding issues:

```bash
go install golang.org/x/tools/go/analysis/passes/fieldalignment/cmd/fieldalignment@latest
fieldalignment structs.go
```

This tool reports structs that could be made smaller by reordering fields.

## Hints

- The rule of thumb: order fields from largest alignment to smallest
- `bool` = 1 byte, `int32` = 4 bytes, `int64`/`float64`/`pointer` = 8 bytes
- The struct's alignment equals its largest field's alignment
- A struct's size is rounded up to a multiple of its alignment
- For hot loops over large slices, the size difference compounds

## Verification

- `BadOrder` should be 40 bytes, `GoodOrder` should be 24 bytes
- The `GoodOrder` benchmark should be measurably faster for large slice iteration due to better cache utilization (fewer cache misses per element)
- `fieldalignment` reports `BadOrder` as having suboptimal field ordering

## What's Next

With struct layout optimized, the next exercise explores string interning to reduce repeated string allocations.

## Summary

Struct field ordering affects memory layout, padding, and cache performance. Go does not reorder struct fields automatically. Arrange fields from largest to smallest alignment to minimize padding. Use `unsafe.Sizeof`, `unsafe.Offsetof`, and the `fieldalignment` tool to analyze layouts. For performance-critical code with large slices, the size difference between well-ordered and poorly-ordered structs directly affects cache efficiency.

## Reference

- [Go spec: Size and alignment guarantees](https://go.dev/ref/spec#Size_and_alignment_guarantees)
- [unsafe package](https://pkg.go.dev/unsafe)
- [fieldalignment analyzer](https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/fieldalignment)
