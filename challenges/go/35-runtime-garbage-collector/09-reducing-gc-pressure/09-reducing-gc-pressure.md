# 9. Reducing GC Pressure

<!--
difficulty: insane
concepts: [gc-pressure, allocation-reduction, sync-pool, stack-allocation, escape-analysis, slice-reuse, string-interning, zero-allocation]
tools: [go, pprof, benchstat]
estimated_time: 60m
bloom_level: create
prerequisites: [gc-phases, gogc-and-gomemlimit, gc-impact-tail-latency]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-08 in this section
- Understanding of GC impact on latency
- Familiarity with `go test -bench`, pprof, and escape analysis (`-gcflags='-m'`)

## Learning Objectives

- **Create** optimized versions of allocation-heavy code using systematic GC pressure reduction techniques
- **Analyze** heap profiles to identify the largest sources of GC pressure
- **Evaluate** the effectiveness of each technique using benchmarks with allocation tracking

## The Challenge

The most effective way to reduce GC overhead is to allocate less. Every heap allocation eventually becomes work for the garbage collector -- scanning, marking, sweeping. A program that allocates less runs faster, pauses less, and uses less memory.

This exercise covers the full toolkit for reducing GC pressure in Go: escape analysis awareness, stack allocation tricks, `sync.Pool`, slice and buffer reuse, string interning, value types vs pointer types, and zero-allocation idioms. You will start with a deliberately allocation-heavy program, profile it to identify hotspots, then systematically apply optimizations and measure the improvement.

## Requirements

1. Build an allocation-heavy data processing pipeline that reads records, transforms them, filters them, and produces output. The baseline version should allocate freely (new slices, string concatenation, interface boxing, closures capturing variables).
2. Profile the baseline with `go test -bench -benchmem` and `go tool pprof` (alloc_objects profile) to identify the top 5 allocation sites
3. Apply escape analysis optimization: restructure code so the compiler keeps values on the stack. Use `go build -gcflags='-m'` to verify escape decisions. Target at least 3 allocations that currently escape but could stay on the stack.
4. Apply `sync.Pool` for frequently allocated short-lived objects (buffers, temporary structs). Measure the reduction in allocations per operation.
5. Apply slice reuse: pre-allocate slices with known capacity, reuse backing arrays across iterations with `slice[:0]` reset, use `append` judiciously
6. Apply string interning for repeated string values: build a string intern table that deduplicates identical strings, reducing both allocation count and memory usage
7. Apply value-type optimization: replace pointer-heavy struct designs with value embeddings where object lifetime is contained
8. Implement a zero-allocation JSON encoder for a specific struct type (hand-written, no reflection) and benchmark it against `encoding/json`
9. Produce a before/after comparison table showing: allocations/op, bytes/op, ns/op, and GC cycles for each optimization applied incrementally
10. The final optimized version should achieve at least 80% fewer allocations per operation than the baseline

## Hints

- `go test -bench=. -benchmem` reports allocs/op and B/op -- your primary metrics.
- `go build -gcflags='-m=2'` shows detailed escape analysis decisions. Look for "escapes to heap" messages.
- Common escape triggers: returning a pointer to a local, storing a local in an interface, sending a value through a channel, closures capturing by reference.
- `sync.Pool` objects may be collected at any GC cycle. Never store critical state in a pool -- use it only for transient buffers.
- `strings.Builder` reuses its buffer. `bytes.Buffer` can be pooled. Both are preferable to `+` concatenation.
- Zero-allocation JSON encoding: write directly to an `io.Writer` using `strconv.AppendInt`, `strconv.AppendFloat`, and manual quoting.
- Value types (embedded structs, arrays) avoid pointer indirection and reduce GC scan work because the GC does not need to follow pointers within scalar-only types.

## Success Criteria

1. The heap profile identifies clear allocation hotspots in the baseline
2. Escape analysis output shows at least 3 variables moved from heap to stack after optimization
3. `sync.Pool` usage reduces repeated buffer allocations to near zero
4. Slice reuse eliminates per-iteration slice growth allocations
5. String interning reduces string allocations for repeated values
6. The zero-allocation JSON encoder reports 0 allocs/op for the target struct
7. The final benchmark shows at least 80% reduction in allocs/op compared to baseline
8. GC cycles during the benchmark are measurably reduced

## Research Resources

- [Go escape analysis](https://go.dev/wiki/CompilerOptimizations#escape-analysis) -- how the compiler decides stack vs heap
- [sync.Pool](https://pkg.go.dev/sync#Pool) -- object pooling for short-lived allocations
- [pprof alloc profiling](https://pkg.go.dev/runtime/pprof) -- heap allocation profiling
- [strings.Builder](https://pkg.go.dev/strings#Builder) -- efficient string construction
- [Dave Cheney: High Performance Go](https://dave.cheney.net/high-performance-go-workshop/gophercon-2019.html) -- comprehensive optimization guide
- [Bryan Boreham: An Introduction to Reducing Allocations in Go](https://www.youtube.com/watch?v=yfrJFfCGHG0) -- practical allocation reduction talk

## What's Next

Continue to [10 - Arena Allocation Patterns](../10-arena-allocation-patterns/10-arena-allocation-patterns.md) to explore memory arena patterns for batch allocation and deallocation.

## Summary

- Allocating less is the most effective way to reduce GC overhead
- Escape analysis determines stack vs heap placement -- understanding it is key to optimization
- `sync.Pool` eliminates repeated allocation of short-lived objects
- Slice reuse with `slice[:0]` avoids repeated backing array allocation
- String interning deduplicates repeated string values
- Value types reduce GC scan work by eliminating pointer chasing
- Zero-allocation patterns (manual encoding, pre-allocated buffers) can eliminate allocations entirely
- Always profile first, then optimize the highest-impact allocation sites
