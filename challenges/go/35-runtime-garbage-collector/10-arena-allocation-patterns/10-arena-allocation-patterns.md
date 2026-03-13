# 10. Arena Allocation Patterns

<!--
difficulty: insane
concepts: [memory-arena, bump-allocator, region-based-allocation, batch-deallocation, gc-bypass, unsafe-memory, arena-lifecycle]
tools: [go]
estimated_time: 60m
bloom_level: create
prerequisites: [reducing-gc-pressure, unsafe-and-cgo, gc-phases]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-09 in this section
- Understanding of GC pressure reduction techniques
- Familiarity with `unsafe` package and raw memory management

## Learning Objectives

- **Create** a memory arena allocator that bypasses the Go garbage collector for batch workloads
- **Analyze** the performance characteristics of arena allocation vs standard heap allocation
- **Evaluate** when arena allocation is appropriate and the safety tradeoffs involved

## The Challenge

Arena allocation is a technique where you allocate a large block of memory upfront and hand out slices of it for individual objects. When the entire batch of work is done, the whole arena is freed at once -- no GC scanning, no per-object sweeping. This trades the safety and convenience of garbage collection for dramatically lower allocation overhead in batch-processing scenarios.

Go's standard library does not include a public arena allocator (the experimental `arena` package was added in Go 1.20 behind GOEXPERIMENT but was never promoted). However, the pattern can be implemented using `make([]byte, size)` for the backing store and manual offset management for sub-allocations, or using `unsafe` for type-punned allocations from a raw byte slab.

Build a type-safe arena allocator that supports allocating structs of known types from a pre-allocated memory region. The arena should support Reset (free all objects at once) without triggering GC. Benchmark it against standard allocation for batch workloads.

## Requirements

1. Implement a `ByteArena` that allocates raw bytes from a pre-allocated slab using bump-pointer allocation:
   - `New(capacity int) *ByteArena` -- allocate the backing slab
   - `Alloc(size, align int) []byte` -- return an aligned slice from the slab
   - `Reset()` -- reset the bump pointer to zero (logically frees all allocations)
   - `Used() int` and `Remaining() int` -- report usage statistics
2. Implement a generic `TypedArena[T]` that allocates values of a specific type from an underlying `ByteArena`:
   - `NewTypedArena[T](count int) *TypedArena[T]` -- pre-allocate space for `count` objects
   - `Alloc() *T` -- return a pointer to a zero-initialized T from the arena
   - `Reset()` -- reset the arena for reuse
3. Implement an arena-backed slice builder that constructs a slice of structs without individual heap allocations
4. Build a benchmark comparing three allocation strategies for processing 1 million records:
   - Standard heap allocation (one `new(T)` per record)
   - `sync.Pool` based allocation
   - Arena allocation with batch reset
5. Measure: allocations/op, bytes/op, ns/op, GC cycles, and p99 latency for each strategy
6. Demonstrate the lifecycle pattern: allocate from arena, process batch, reset arena, repeat -- showing that GC work stays constant regardless of batch size
7. Implement safety checks: out-of-bounds detection, double-reset detection, and use-after-reset detection (in debug mode)
8. Show the key limitation: arena-allocated objects must not outlive the arena. Demonstrate what goes wrong if they do.

## Hints

- The simplest arena is a `[]byte` with an offset counter. `Alloc` advances the offset and returns a sub-slice. `Reset` sets the offset to zero.
- For typed allocation, use `unsafe.Pointer` to convert a sub-slice of the backing store to a typed pointer. Ensure proper alignment using `unsafe.Alignof`.
- Arena allocation eliminates GC scanning overhead because the backing `[]byte` contains no pointers from the GC's perspective (it is an opaque byte array).
- The experimental `arena` package in Go 1.20+ (GOEXPERIMENT=arenas) provides a runtime-integrated arena. Study its API for inspiration, but implement your own.
- In production, arenas are most useful for request-scoped allocations (allocate for one request, reset after response), batch processing (ETL pipelines), and protocol parsing.
- Keep arena-allocated objects off the heap graph: do not store them in long-lived data structures or pass them through channels.

## Success Criteria

1. The `ByteArena` correctly allocates aligned memory regions from a pre-allocated slab
2. The `TypedArena[T]` provides type-safe allocation with proper alignment
3. Arena allocation shows at least 5x fewer allocs/op than standard heap allocation for batch workloads
4. GC cycle count does not increase with arena batch size (constant GC overhead)
5. Reset completes in O(1) time regardless of how many objects were allocated
6. Safety checks detect misuse in debug mode
7. The benchmark clearly demonstrates the performance advantage and documents the safety tradeoffs

## Research Resources

- [Go arena experiment](https://github.com/golang/go/issues/51317) -- the proposal and discussion for arena support in Go
- [GOEXPERIMENT=arenas](https://pkg.go.dev/arena) -- the experimental arena package (Go 1.20+)
- [Region-based memory management](https://en.wikipedia.org/wiki/Region-based_memory_management) -- the general technique
- [Bump allocators](https://os.phil-opp.com/allocator-designs/#bump-allocator) -- the allocation strategy used in arenas
- [unsafe package](https://pkg.go.dev/unsafe) -- for type-punned memory access

## What's Next

Congratulations -- you have completed the Runtime: Garbage Collector section. You now understand Go's GC from first principles: the tri-color algorithm, phases, tuning knobs, write barriers, observation tools, the pacer, and advanced allocation strategies. Continue to [Section 36 - Runtime: Compiler and Assembly](../../36-runtime-compiler-and-assembly/01-reading-ssa-output/01-reading-ssa-output.md) to explore how Go compiles your code.

## Summary

- Arena allocation provides O(1) deallocation for batch workloads by freeing all objects at once
- Bump-pointer allocation is extremely fast: just increment an offset
- Arenas bypass GC scanning because the backing store contains no traceable pointers
- Type-safe arenas use generics and `unsafe` to provide ergonomic typed allocation
- The key constraint: arena-allocated objects must not outlive the arena
- Arenas are ideal for request-scoped, batch, and parsing workloads where object lifetimes are bounded
- Always benchmark to confirm the performance benefit justifies the safety tradeoff
