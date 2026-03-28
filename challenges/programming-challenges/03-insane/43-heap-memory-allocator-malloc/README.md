# 43. Heap Memory Allocator (malloc implementation)

<!--
difficulty: insane
category: systems-programming
languages: [rust]
concepts: [heap-allocation, free-list, coalescing, boundary-tags, segregated-lists, mmap, thread-safety, global-alloc-trait, unsafe-rust]
estimated_time: 25-35 hours
bloom_level: create
prerequisites: [unsafe-rust, raw-pointers, memory-layout, alignment, concurrency-primitives, system-calls]
-->

## Languages

- Rust (stable, nightly for benchmarks with allocator API)

## Prerequisites

- Deep understanding of raw pointers, pointer arithmetic, and `unsafe` Rust
- Memory alignment rules: natural alignment, over-alignment, power-of-two constraints
- System call basics: `mmap`/`munmap` for acquiring memory from the OS
- Concurrency: `Mutex`, atomics, or lock-free data structures
- Rust's `GlobalAlloc` trait and the `#[global_allocator]` attribute

## Learning Objectives

- **Create** a general-purpose heap allocator that handles arbitrary allocation sizes and patterns
- **Implement** multiple free list policies (first-fit, best-fit, next-fit) and evaluate their fragmentation characteristics
- **Design** boundary tags for O(1) coalescing of adjacent free blocks
- **Architect** segregated free lists that route allocations to size-appropriate bins
- **Evaluate** thread-safety strategies: global lock, per-thread arenas, and lock-free approaches
- **Implement** the `GlobalAlloc` trait to replace the system allocator for an entire Rust program

## The Challenge

Every call to `Box::new`, `Vec::push`, or `String::from` in Rust ultimately calls a memory allocator. The default allocator (usually the system's malloc) is a sophisticated piece of systems software that manages a heap, serves allocation requests of arbitrary sizes, reclaims freed memory, and does all of this with minimal fragmentation and contention.

Build a heap allocator from the ground up. Start with a simple free list allocator that acquires memory from the OS via `mmap`, manages it with explicit free lists, and returns it through `alloc`/`dealloc`. Then add coalescing (merging adjacent free blocks), boundary tags (metadata at both ends of each block for O(1) coalescing), segregated free lists (multiple lists for different size classes), and thread safety.

The result is a functional replacement for malloc that can serve as the global allocator for a Rust program. Benchmark it against the system allocator, jemalloc, and mimalloc to understand where your design wins and where production allocators pull ahead.

## Requirements

1. Acquire heap memory from the OS using `mmap` (Unix) or `VirtualAlloc` (Windows). Never use `std::alloc::System` internally -- your allocator IS the system allocator
2. Implement block headers: each allocated or free block has a header containing the block size, an in-use flag, and pointers for the free list
3. Implement three allocation policies that search the free list for a suitable block: **first-fit** (first block that fits), **best-fit** (smallest block that fits), **next-fit** (first-fit resuming from the last allocation point)
4. Implement block splitting: when a found block is significantly larger than requested (e.g., >32 bytes excess), split it into an allocated block and a smaller free block
5. Implement coalescing: when a block is freed, merge it with adjacent free blocks (previous and next) to form a larger free block. Use boundary tags (a footer at the end of each block containing the block size) to find the previous block in O(1)
6. Implement memory alignment: all returned pointers must be aligned to at least 8 bytes. Support explicit alignment requests up to 4096 bytes by over-allocating and adjusting the returned pointer
7. Implement segregated free lists: maintain separate free lists for size classes (e.g., 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, >4096). Allocations are routed to the appropriate list. If a list is empty, search larger lists and split
8. Implement mmap fallback for large allocations: requests above a threshold (e.g., 128 KB) are served directly by `mmap` with their own virtual memory mapping, bypassing the free list entirely
9. Implement thread safety using one of: a global mutex, per-thread arenas (each thread has its own heap segment), or lock-free free lists using CAS operations
10. Implement the `GlobalAlloc` trait so the allocator can be installed via `#[global_allocator]`
11. Write benchmarks comparing allocation throughput, deallocation throughput, and fragmentation against the system allocator. Optionally benchmark against jemalloc and mimalloc
12. All `unsafe` blocks must have a safety comment documenting the invariant that makes the operation sound

## Acceptance Criteria

- [ ] The allocator can serve as `#[global_allocator]` and run a full Rust program (HashMap with 1M entries, Vec grow/shrink cycles, String concatenation)
- [ ] First-fit, best-fit, and next-fit policies produce correct results for the same workload
- [ ] Block splitting creates correctly-sized blocks with valid headers and free list linkage
- [ ] Coalescing merges adjacent free blocks: after alloc(A), alloc(B), alloc(C), free(A), free(C), free(B), the heap has a single merged free block
- [ ] Boundary tags enable O(1) backward coalescing without scanning the heap
- [ ] Alignment requests up to 4096 bytes return correctly-aligned pointers
- [ ] Large allocations (>128 KB) use mmap directly and munmap on free
- [ ] Thread safety: 8 threads performing concurrent alloc/free produce no corruption or panics
- [ ] Segregated free lists reduce fragmentation compared to a single free list (measure and report)
- [ ] All unsafe blocks are documented with safety invariants
- [ ] No memory leaks: after freeing all allocations, all heap memory is on the free list or returned to the OS
- [ ] Allocator statistics are accurate: total bytes allocated matches total bytes freed after a complete alloc/free cycle
- [ ] Splitting threshold prevents creation of uselessly small free blocks (minimum block size enforced)

## Research Resources

- [A Scalable Concurrent malloc(3) Implementation for FreeBSD (jemalloc)](https://people.freebsd.org/~jasone/jemalloc/bsdcan2006/jemalloc.pdf) -- Jason Evans' jemalloc paper: arenas, thread caches, run-based allocation
- [mimalloc: Free List Sharding in Action (Leijen et al., 2019)](https://www.microsoft.com/en-us/research/uploads/prod/2019/06/mimalloc-tr-v1.pdf) -- Microsoft's high-performance allocator with novel free list sharding
- [Dynamic Storage Allocation: A Survey and Critical Review (Wilson et al., 1995)](https://www.cs.northwestern.edu/~pdinda/ics-s05/doc/dsa.pdf) -- the comprehensive survey of allocator techniques: coalescing, segregated lists, buddy systems
- [Operating Systems: Three Easy Pieces, Chapter 17 (Free Space Management)](https://pages.cs.wisc.edu/~remzi/OSTEP/vm-freespace.pdf) -- OSTEP's treatment of free list management, splitting, and coalescing
- [Doug Lea's malloc (dlmalloc)](https://gee.cs.oswego.edu/dl/html/malloc.html) -- the design document for the most widely deployed malloc implementation
- [glibc malloc internals](https://sourceware.org/glibc/wiki/MallocInternals) -- how glibc ptmalloc2 manages arenas, bins, and thread contention
- [Rust GlobalAlloc documentation](https://doc.rust-lang.org/std/alloc/trait.GlobalAlloc.html) -- trait requirements and safety constraints
