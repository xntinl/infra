# 22. Custom Memory Pool Allocator

<!--
difficulty: advanced
category: systems-programming
languages: [rust]
concepts: [memory-allocation, slab-allocator, free-lists, thread-local-storage, global-alloc-trait, memory-fragmentation, unsafe-rust]
estimated_time: 10-14 hours
bloom_level: evaluate
prerequisites: [rust-ownership, unsafe-rust-basics, raw-pointers, memory-layout, concurrency-primitives, trait-implementation]
-->

## Languages

- Rust (stable)

## Prerequisites

- Rust ownership model, lifetime annotations, and interior mutability (`RefCell`)
- Raw pointers (`*mut T`, `*const T`), pointer arithmetic (`ptr.add()`, `ptr.sub()`), and `unsafe` blocks
- `std::alloc::Layout`: size, alignment, and the `from_size_align` constructor
- `std::alloc::GlobalAlloc` trait: the `alloc` and `dealloc` methods and their safety requirements
- Memory layout: size, alignment, padding between fields, power-of-two alignment rules
- Concurrency primitives: `Mutex`, `AtomicUsize`, thread-local storage (`thread_local!`)
- Trait implementation including unsafe traits (`unsafe impl GlobalAlloc`, `unsafe impl Send + Sync`)
- Basic understanding of how the system allocator works (malloc/free, memory fragmentation)
- Familiarity with cache locality and its impact on allocation-heavy workloads

## Learning Objectives

- **Implement** a slab/pool allocator that pre-allocates memory and serves fixed-size allocations in O(1) time complexity
- **Evaluate** the trade-offs between pool allocation and general-purpose allocation in terms of fragmentation, throughput, and memory overhead
- **Analyze** how intrusive free list data structures enable constant-time allocation and deallocation without per-object metadata overhead
- **Design** a multi-size-class allocator that routes allocation requests to the appropriate pool based on requested size and alignment
- **Apply** thread-local caching to reduce lock contention in multi-threaded allocation workloads
- **Implement** the `GlobalAlloc` trait to replace the system allocator for an entire Rust program
- **Measure** allocator performance through targeted benchmarks comparing throughput, fragmentation, and scalability across thread counts

## The Challenge

General-purpose allocators like glibc's malloc handle every allocation size and pattern, but that generality costs performance. When your program allocates millions of objects of the same few sizes -- network packets, AST nodes, ECS components, database rows -- a pool allocator eliminates fragmentation entirely and reduces allocation to a pointer bump or free list pop.

The slab allocator, introduced by Jeff Bonwick for the SunOS kernel in 1994, organizes memory into pools of fixed-size slots. Each pool (slab) is a contiguous block divided into equal-sized chunks. A free list threads through unused chunks, so both allocation and deallocation are O(1) with zero fragmentation within a size class. Modern allocators like jemalloc and mimalloc still use this principle at their core.

The central insight is that most programs allocate objects from a small number of distinct sizes. A web server allocates thousands of request buffers (say, 128 bytes each), thousands of header maps (64 bytes), and thousands of response bodies (256 bytes). A general-purpose allocator must search for a block that fits, split large blocks, coalesce freed blocks, and manage fragmentation across all sizes. A pool allocator sidesteps all of this: it pre-allocates contiguous memory divided into fixed-size slots. Allocation pops a slot from a free list. Deallocation pushes it back. No splitting, no coalescing, no fragmentation.

The cost of this simplicity is inflexibility. A 20-byte object in a 32-byte slot wastes 12 bytes. If you do not know your size distribution in advance, you waste memory on wrong-sized pools. And you still need a fallback for sizes that do not fit any pool. This is the fundamental trade-off you will explore.

Build a memory pool allocator that manages multiple size classes, provides O(1) allocation and deallocation, tracks utilization statistics, and integrates with Rust's `GlobalAlloc` trait so it can replace the system allocator. Then benchmark it against the default allocator to see where pool allocation wins and where it loses.

This challenge is a stepping stone toward understanding production allocators. jemalloc, mimalloc, and TCMalloc all use slab/pool techniques at their core, layered with thread caching, arena isolation, and OS memory management. By building a simpler version, you internalize the principles that make those allocators fast.

Working on this challenge will require writing `unsafe` Rust extensively. Raw pointer manipulation, casting between pointer types, and manual memory management are inherent to allocator implementation. This is one of the few domains where `unsafe` is not a smell but a requirement -- the allocator provides the safety guarantee that safe Rust relies on. Every `unsafe` block must carry a safety comment explaining why the operation is sound, because there is no borrow checker to catch mistakes at this level.

## Constraints and Considerations

- All size classes must be powers of two. This ensures that any slot in a size class is naturally aligned to its size, which satisfies most alignment requirements without additional logic.
- The `GlobalAlloc` trait requires that `alloc` and `dealloc` are safe to call from any thread. Your implementation must be `Send + Sync`. Using `Mutex` for the shared pool and `thread_local!` for the cache achieves this.
- Rust's `GlobalAlloc::dealloc` receives the original `Layout`, so you know the size class of the freed allocation. You do not need to store the size class in the allocation itself, unlike C's `free()` which gets only a pointer.
- Slab memory should be acquired from the system allocator (`std::alloc::System`), not from `mmap` directly. This keeps the implementation platform-independent and avoids the complexity of managing virtual memory pages.
- When the allocator is installed as `#[global_allocator]`, it must not use any standard library types that allocate (like `Vec` or `String`) during allocation -- this causes infinite recursion. Use fixed-size arrays or raw memory for internal data structures.
- The thread-local cache must be flushed when a thread exits. If slots are cached in a dead thread's TLS, they are leaked. Rust's `thread_local!` invokes `Drop` on thread exit (for normal exits), which is the mechanism to flush cached slots back to the shared pool.

## Requirements

1. Implement a `Slab` structure that manages a contiguous memory region divided into fixed-size slots. Each slab tracks its slot size, total slot count, and a free list of available slots
2. The free list must be intrusive: store the next-free pointer inside each unused slot itself, eliminating per-slot metadata overhead. Slots must be at least `size_of::<*mut u8>()` bytes to hold the pointer
3. Implement a `PoolAllocator` that manages multiple size classes: 8, 16, 32, 64, 128, and 256 bytes. Each size class has one or more slabs. Allocations are routed to the smallest size class that fits the requested layout
4. When a slab in a size class is exhausted, allocate a new slab from the system allocator (via `std::alloc::alloc`) and add it to the chain for that size class
5. Allocation requests larger than 256 bytes must fall through to the system allocator directly
6. Implement thread-local caching: each thread maintains a small per-size-class free list (magazine/cache of 32 slots). Threads allocate from their local cache first. When the cache is empty, it refills from the shared slab under a lock. When the cache is full on deallocation, it flushes back to the shared slab
7. Track allocation statistics: total allocations, total deallocations, active allocations per size class, slab utilization (used slots / total slots), and number of slabs allocated per size class
8. Implement `GlobalAlloc` for the `PoolAllocator` so it can be installed as the program-wide allocator via `#[global_allocator]`
9. Write benchmarks comparing your allocator against the system allocator for: single-threaded allocation/deallocation throughput of fixed-size objects, multi-threaded contention with 8 threads, and mixed-size allocation patterns
10. All `unsafe` blocks must have a safety comment explaining the invariant that makes the operation sound

## Hints

**Hint 1 -- Intrusive free list**: Each unused slot in a slab contains a pointer to the next unused slot. The slab's free list head points to the first available slot. Allocation pops the head, deallocation pushes the freed slot back. Cast the slot's memory to `*mut *mut u8` to read/write the next pointer. This works because unused slots are not holding user data.

**Hint 2 -- Size class routing**: Use a lookup table or a simple match on the requested size rounded up to the next power of two. For `Layout { size: 20, align: 4 }`, route to the 32-byte class. For alignment requirements larger than the size class, you may need to over-allocate or fall through to the system allocator.

**Hint 3 -- Thread-local caching**: Use `thread_local!` with `RefCell<[Vec<*mut u8>; NUM_SIZE_CLASSES]>`. The `Vec` acts as a stack of cached free slots. This avoids locking on the hot path entirely. The tricky part is flushing the cache back to the shared slab on thread exit -- `Drop` on the thread-local value handles this.

**Hint 4 -- Slab metadata placement**: Store slab metadata (slot size, capacity, free count, pointer to next slab) in a separate allocation, not at the start of the slab memory. This keeps slab memory properly aligned for all slot sizes and simplifies the math for slot-to-slab lookups.

## Acceptance Criteria

- [ ] Allocations of sizes <= 256 bytes are served from the pool with O(1) allocation time
- [ ] Deallocations return memory to the correct slab and size class in O(1)
- [ ] The intrusive free list uses no extra memory per slot beyond the slot itself
- [ ] Thread-local caching reduces contention: 8-thread benchmark shows less than 2x overhead compared to single-threaded
- [ ] Allocations larger than 256 bytes correctly fall through to the system allocator
- [ ] Slab growth works correctly: exhausting a slab triggers allocation of a new slab without data loss
- [ ] Statistics accurately report active allocations, slab utilization, and per-class allocation counts
- [ ] The allocator can be installed as `#[global_allocator]` and run a non-trivial program (e.g., build a HashMap with 100,000 entries)
- [ ] All unsafe blocks have documented safety invariants
- [ ] Benchmarks demonstrate measurable speedup over the system allocator for fixed-size allocation patterns
- [ ] Alignment is respected: `Layout { size: 4, align: 16 }` returns a 16-byte aligned pointer
- [ ] No memory corruption: allocating, writing, and reading back 10,000 objects produces correct data
- [ ] Deallocated slots are reusable: after allocating and freeing 1,000 slots, the next 1,000 allocations reuse the same memory without growing the slab

## Key Concepts

**Intrusive free list**: Instead of maintaining a separate data structure to track which slots are free, the free list is embedded in the slots themselves. Each unused slot's first 8 bytes hold a pointer to the next unused slot. This eliminates per-slot metadata overhead entirely -- the cost is that the minimum slot size equals pointer size (8 bytes on 64-bit).

**Size class routing**: When a request arrives for N bytes with alignment A, the allocator computes `max(N, A)` and rounds up to the nearest size class. This ensures alignment is satisfied (since each size class is a power of two and slabs are aligned to their slot size) and maps every request to a fixed-size pool.

**Thread-local caching**: The hot path (allocate or deallocate) should never touch a shared lock. Each thread maintains a small stack of free slots per size class. Allocations pop from the local stack (zero synchronization). Only when the local stack is empty does the thread lock the shared pool to refill. Deallocations push to the local stack; only when it overflows does it flush back. This pattern is used by jemalloc (tcache), mimalloc (thread-local pages), and TCMalloc (per-thread cache).

**Slab growth**: When all slots in a size class are allocated, the pool must grow. Allocate a new contiguous region from the system allocator, divide it into slots, thread the free list, and add it to the size class's chain. The key decision is slab size: too small means frequent growth, too large means wasted memory for rarely-used classes.

**Internal fragmentation**: A pool allocator trades external fragmentation (scattered free blocks too small to use) for internal fragmentation (wasted space within slots). A 20-byte allocation in a 32-byte slot wastes 37.5% of the slot. Choosing size classes that match your workload's allocation distribution minimizes this waste. The six classes (8, 16, 32, 64, 128, 256) cover the common case; real allocators use finer granularity (jemalloc has 40+ size classes below 16 KB).

**Deallocation and the pointer-to-slab problem**: When `dealloc` receives a pointer, the allocator must determine which slab (and therefore which size class) the pointer belongs to. Two approaches: store the size class in the allocation header (adds overhead), or compute it from the pointer address and slab base addresses (requires a mapping structure or address-based lookup). Since `GlobalAlloc::dealloc` provides the `Layout`, you know the size class from the layout's size -- but production allocators that implement C's `free(void*)` cannot rely on this and must solve the reverse mapping problem.

**Benchmarking methodology**: Measure allocation throughput (ops/sec) by timing a tight loop of alloc/dealloc. Measure fragmentation by tracking peak memory usage vs. useful memory. Measure contention by running the same benchmark on 1, 2, 4, and 8 threads and plotting the throughput curve. A well-designed pool allocator should show near-constant throughput up to the point where lock contention dominates, then flatten. Thread-local caching pushes that inflection point to higher thread counts.

## Research Resources

- [The Slab Allocator: An Object-Caching Kernel Memory Allocator (Bonwick, 1994)](https://www.usenix.org/legacy/publications/library/proceedings/bos94/bonwick.html) -- the original slab allocator paper from SunOS, the foundational design
- [Magazines and Vmem: Extending the Slab Allocator (Bonwick & Adams, 2001)](https://www.usenix.org/legacy/event/usenix01/full_papers/bonwick/bonwick.pdf) -- the follow-up paper introducing magazine-based thread caching
- [A Scalable Concurrent malloc(3) Implementation for FreeBSD (jemalloc)](https://people.freebsd.org/~jasone/jemalloc/bsdcan2006/jemalloc.pdf) -- Jason Evans' jemalloc paper covering arenas, thread caches, and size classes
- [mimalloc: Free List Sharding in Action (Leijen et al., 2019)](https://www.microsoft.com/en-us/research/uploads/prod/2019/06/mimalloc-tr-v1.pdf) -- Microsoft Research allocator using sharded free lists for performance
- [Rust GlobalAlloc documentation](https://doc.rust-lang.org/std/alloc/trait.GlobalAlloc.html) -- the trait you must implement, including safety requirements
- [Writing a Memory Allocator (Dmitry Soshnikov)](http://dmitrysoshnikov.com/compilers/writing-a-memory-allocator/) -- step-by-step walkthrough of allocator internals
- [Rust Allocator API RFC](https://github.com/rust-lang/rfcs/blob/master/text/1398-kinds-of-allocators.md) -- RFC covering the allocator trait design and rationale
- [Operating Systems: Three Easy Pieces, Chapter 17 (Free Space Management)](https://pages.cs.wisc.edu/~remzi/OSTEP/vm-freespace.pdf) -- free list strategies, splitting, coalescing, and fragmentation analysis
- [TCMalloc: Thread-Caching Malloc (Google)](https://google.github.io/tcmalloc/design.html) -- Google's allocator design with per-thread caches and central free lists
