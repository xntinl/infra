# 13. Implementing a Custom Memory Allocator

<!--
difficulty: insane
concepts: [memory-allocator, free-list, slab-allocator, buddy-system, mmap, page-allocation, fragmentation, thread-local-cache]
tools: [go]
estimated_time: 4h
bloom_level: create
prerequisites: [unsafe-and-cgo, arena-allocation-patterns, gc-phases]
-->

## Prerequisites

- Go 1.22+ installed
- Completed the GC section (exercises 35-01 through 35-10)
- Understanding of `unsafe` package and raw memory management
- Familiarity with operating system memory concepts (virtual memory, pages, mmap)

## Learning Objectives

- **Create** a fully functional memory allocator with allocation, deallocation, and coalescing
- **Analyze** fragmentation patterns and allocator performance characteristics
- **Evaluate** different allocator designs (free-list, slab, buddy system) for different workload patterns

## The Challenge

Go's built-in memory allocator (based on TCMalloc) is sophisticated: it uses per-P caches, size-class spans, and a page heap. But to truly understand memory allocation, you need to build one yourself.

Implement a custom memory allocator in Go. It should manage a large pre-allocated memory region (obtained via `mmap` or a large `make([]byte)` slab) and support `Alloc(size)` and `Free(ptr)` operations. Start with a simple free-list allocator, then evolve it into a slab allocator with size classes for small objects and a buddy system for large objects.

This is a capstone-level exercise that synthesizes understanding of memory layout, pointer arithmetic, concurrency, and systems programming.

## Requirements

1. Implement a `FreeListAllocator` that manages a contiguous memory region:
   - `Alloc(size int) unsafe.Pointer` -- find a free block, split if necessary, return a pointer
   - `Free(ptr unsafe.Pointer)` -- return a block to the free list and coalesce with adjacent free blocks
   - Block header: size, in-use flag, next-free pointer
   - First-fit, best-fit, or next-fit search strategy (implement at least two and compare)
2. Implement a `SlabAllocator` with size classes for small allocations:
   - Size classes: 8, 16, 32, 64, 128, 256, 512, 1024 bytes
   - Each size class maintains a free list of pre-divided blocks within a slab (page-sized region)
   - New slabs are allocated from the backing region when a size class is exhausted
   - `Alloc(size)` rounds up to the nearest size class and pops from the free list (O(1))
   - `Free(ptr)` pushes back onto the free list (O(1))
3. Implement a `BuddyAllocator` for large allocations:
   - Power-of-two block sizes from 4KB to the total region size
   - Split larger blocks to satisfy smaller requests
   - Coalesce buddy pairs on free to reduce fragmentation
4. Combine all three into a `HybridAllocator`:
   - Small allocations (<=1024 bytes): slab allocator
   - Medium allocations (1KB-64KB): buddy allocator
   - Large allocations (>64KB): direct allocation from the backing region
5. Add thread safety: protect the allocator with fine-grained locking (per-size-class locks for the slab allocator, a single lock for the buddy allocator)
6. Implement diagnostics: total allocated, total free, fragmentation ratio, allocation count, free count
7. Write comprehensive tests: random alloc/free sequences, stress tests with concurrent goroutines, fragmentation measurement
8. Benchmark against Go's built-in allocator (`make([]byte, size)`) for various workload patterns

## Hints

- Use `make([]byte, regionSize)` for the backing store. Use `unsafe.Pointer` and `unsafe.Add` for pointer arithmetic within the region.
- Block headers can be stored inline: the first N bytes of each block contain metadata (size, flags), and the returned pointer is offset past the header.
- Coalescing: when freeing a block, check if the adjacent blocks (by address) are also free. If so, merge them into a larger free block.
- Slab allocator: divide a page into N equal-sized slots. The free list is a linked list through the slots themselves (store the next pointer in the unused slot data).
- Buddy allocator: two blocks are "buddies" if they were split from the same parent. A block at offset `O` with size `S` has its buddy at offset `O XOR S`.
- For thread safety, per-size-class locks in the slab allocator allow concurrent allocations of different sizes without contention.
- Fragmentation ratio: `1 - (largest_free_block / total_free_memory)`. Higher means more fragmented.

## Success Criteria

1. The free-list allocator correctly handles allocation, deallocation, and coalescing
2. The slab allocator provides O(1) alloc/free for small objects
3. The buddy allocator correctly splits and coalesces power-of-two blocks
4. The hybrid allocator routes allocations to the appropriate sub-allocator
5. Concurrent stress tests pass without data races (`go test -race`)
6. Diagnostics accurately report memory usage and fragmentation
7. The fragmentation test shows that the slab allocator has near-zero fragmentation for uniform sizes
8. Benchmarks document the performance characteristics vs Go's built-in allocator

## Research Resources

- [TCMalloc Design](https://google.github.io/tcmalloc/design.html) -- the allocator Go's is based on
- [Go Memory Allocator (runtime/malloc.go)](https://github.com/golang/go/blob/master/src/runtime/malloc.go) -- the actual Go allocator source
- [Go Memory Management Internals](https://go.dev/src/runtime/HACKING.md) -- runtime developer guide
- [The Slab Allocator (Bonwick)](https://www.usenix.org/legacy/publications/library/proceedings/bos94/bonwick.html) -- original slab allocator paper
- [Buddy Memory Allocation](https://en.wikipedia.org/wiki/Buddy_memory_allocation) -- buddy system overview
- [Writing a Memory Allocator](http://dmitrysoshnikov.com/compilers/writing-a-memory-allocator/) -- tutorial walkthrough

## What's Next

Continue to [14 - Writing a Goroutine-Aware Profiler](../14-writing-a-goroutine-aware-profiler/14-writing-a-goroutine-aware-profiler.md) to build a profiling tool that understands Go's concurrency model.

## Summary

- A memory allocator manages a region of memory, providing alloc/free operations
- Free-list allocators are simple but suffer from fragmentation; coalescing helps
- Slab allocators provide O(1) alloc/free for fixed-size objects using pre-divided pages
- Buddy allocators handle variable-size large allocations with efficient coalescing
- A hybrid approach combines all three for different size ranges
- Thread safety requires per-size-class locking for low contention
- Building an allocator from scratch provides deep understanding of Go's runtime memory management
