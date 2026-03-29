# 61. Slab Allocator for Fixed-Size Objects

<!--
difficulty: intermediate-advanced
category: systems-programming
languages: [rust]
concepts: [slab-allocation, free-lists, memory-alignment, size-classes, memory-layout, unsafe-rust, arena-allocation]
estimated_time: 8-12 hours
bloom_level: apply
prerequisites: [rust-ownership, unsafe-rust-basics, raw-pointers, memory-layout, std-alloc-layout]
-->

## Languages

- Rust (stable)

## Prerequisites

- Rust ownership model and borrowing rules
- Raw pointers (`*mut T`, `*const T`) and `unsafe` blocks
- `std::alloc::Layout`: size, alignment, and the `from_size_align` constructor
- `std::alloc::System` allocator for underlying memory acquisition
- Memory layout fundamentals: alignment, padding, power-of-two rules
- Basic understanding of `malloc`/`free` and memory fragmentation
- Familiarity with linked list data structures

## Learning Objectives

- **Implement** a slab allocator that pre-allocates memory regions and serves fixed-size allocations in O(1) time
- **Apply** intrusive free list techniques to manage available slots without per-object metadata overhead
- **Design** size-class routing logic that maps arbitrary allocation requests to the appropriate fixed-size pool
- **Analyze** internal fragmentation trade-offs between slot size granularity and memory waste
- **Implement** proper memory alignment guarantees for all allocations
- **Measure** allocator performance against the system allocator using targeted benchmarks

## The Challenge

General-purpose allocators handle every size and pattern, but that generality has a cost. When a program allocates millions of objects from a small set of sizes -- network packets, AST nodes, database rows -- a slab allocator eliminates fragmentation and reduces allocation to a free list pop.

The slab allocator, introduced by Jeff Bonwick for the SunOS kernel in 1994, organizes memory into pools of fixed-size slots. Each pool is a contiguous block divided into equal-sized chunks. A free list threads through unused chunks, making both allocation and deallocation O(1) with zero external fragmentation.

Build a slab allocator that manages multiple size classes, provides O(1) allocation and deallocation via intrusive free lists, respects memory alignment requirements, tracks utilization statistics, and demonstrates measurable performance advantages over the system allocator for fixed-size allocation patterns.

## Requirements

1. Implement a `Slab` structure that manages a contiguous memory region divided into fixed-size slots. Each slab tracks its slot size, total capacity, free list head, and free count
2. The free list must be intrusive: store the next-free pointer inside each unused slot. Minimum slot size is `size_of::<*mut u8>()` to hold the pointer
3. Implement a `SlabAllocator` that manages size classes: 8, 16, 32, 64, 128, 256, and 512 bytes. Route allocations to the smallest class that fits
4. When a slab is exhausted, allocate a new slab from the system allocator and chain it to the existing slabs for that size class
5. Allocations larger than 512 bytes fall through to the system allocator
6. All allocations must respect the requested alignment. If alignment exceeds the size class, fall through to the system allocator
7. Track statistics: total allocations/deallocations per size class, active slot count, slab count, and utilization percentage
8. Implement a `reset()` method that returns all slots to the free list without deallocating slab memory (useful for arena-style patterns)
9. Write benchmarks comparing allocation throughput (ops/sec) against `std::alloc::System` for single-size and mixed-size workloads
10. All `unsafe` blocks must have safety comments explaining the invariant

## Hints

<details>
<summary>Hint 1 -- Intrusive free list structure</summary>

Each unused slot stores a pointer to the next unused slot. The slab's `free_head` points to the first available slot. Allocation reads the next pointer from `free_head`, advances it, and returns the old head. Cast slot memory to `*mut *mut u8` to read/write the next pointer.

</details>

<details>
<summary>Hint 2 -- Size class selection</summary>

Round up the requested size to the next power of two using `usize::next_power_of_two()`. Then find the matching index in your size class array. For `Layout { size: 20, align: 4 }`, route to the 32-byte class.

</details>

<details>
<summary>Hint 3 -- Slab initialization</summary>

When creating a slab, allocate a contiguous block via `System.alloc()` with alignment equal to the slot size. Then iterate through slots, writing next-pointers into each one. The last slot gets a null pointer to terminate the list.

</details>

<details>
<summary>Hint 4 -- Deallocation routing</summary>

When deallocating, use the `Layout` parameter to determine the size class. Then search the slab chain for that class to find which slab contains the pointer (check if the address falls within `base..base + slab_size`). Push the slot back onto that slab's free list.

</details>

## Acceptance Criteria

- [ ] Allocations of sizes <= 512 bytes are served from slabs with O(1) time
- [ ] Deallocations return memory to the correct slab's free list in O(1)
- [ ] The intrusive free list uses no extra memory per slot
- [ ] Allocations larger than 512 bytes correctly fall through to the system allocator
- [ ] Slab growth works: exhausting a slab triggers a new slab allocation without data loss
- [ ] Alignment is respected: `Layout { size: 4, align: 16 }` returns a 16-byte-aligned pointer
- [ ] Statistics accurately report active allocations, slab utilization, and per-class counts
- [ ] `reset()` returns all slots to the free list and subsequent allocations reuse the same memory
- [ ] No memory corruption: allocating, writing, and reading back 10,000 objects produces correct data
- [ ] Deallocated slots are reusable: after alloc/free of 1,000 slots, the next 1,000 reuse memory without growing
- [ ] Benchmarks demonstrate measurable speedup over the system allocator for fixed-size patterns
- [ ] All `unsafe` blocks have documented safety invariants

## Research Resources

- [The Slab Allocator: An Object-Caching Kernel Memory Allocator (Bonwick, 1994)](https://www.usenix.org/legacy/publications/library/proceedings/bos94/bonwick.html) -- the original slab allocator paper
- [Magazines and Vmem: Extending the Slab Allocator (Bonwick & Adams, 2001)](https://www.usenix.org/legacy/event/usenix01/full_papers/bonwick/bonwick.pdf) -- magazine-based thread caching extension
- [Rust std::alloc module documentation](https://doc.rust-lang.org/std/alloc/index.html) -- Layout, System allocator, and GlobalAlloc
- [Writing a Memory Allocator (Dmitry Soshnikov)](http://dmitrysoshnikov.com/compilers/writing-a-memory-allocator/) -- step-by-step allocator internals
- [Operating Systems: Three Easy Pieces, Chapter 17 (Free Space Management)](https://pages.cs.wisc.edu/~remzi/OSTEP/vm-freespace.pdf) -- free list strategies and fragmentation
- [mimalloc: Free List Sharding in Action (Leijen et al., 2019)](https://www.microsoft.com/en-us/research/uploads/prod/2019/06/mimalloc-tr-v1.pdf) -- modern allocator design with sharded free lists
