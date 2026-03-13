# 3. Custom Allocators

**Difficulty**: Insane

## The Challenge

Build three memory allocators from scratch, each addressing a different allocation
pattern, and integrate them into real Rust programs via the `GlobalAlloc` trait.

The default system allocator (glibc malloc, macOS libmalloc, Windows HeapAlloc)
is a general-purpose compromise. It is acceptable for most workloads and terrible
for specific ones. Games allocate and free thousands of short-lived objects per
frame. Embedded systems have no heap at all. High-frequency trading systems cannot
tolerate the latency jitter of malloc's free-list searches. Compilers allocate
millions of AST nodes that all die together at the end of a compilation phase.

You will build:

1. **Bump allocator** — the fastest possible allocator: increment a pointer. No
   individual deallocation. Reset all at once.
2. **Arena allocator** — bump allocation with named reset points and the ability to
   allocate different types. Extend to support `Drop` types.
3. **Slab allocator** — fixed-size block allocation with O(1) alloc and free via a
   free list. Ideal for allocating many instances of the same type.

## Acceptance Criteria

### Bump Allocator
- [ ] Implement `GlobalAlloc` (the `alloc` and `dealloc` methods) backed by a
      fixed-size `[u8; N]` buffer
- [ ] `alloc` bumps a pointer forward, respecting `Layout::align()` via pointer
      alignment arithmetic
- [ ] `dealloc` is a no-op (memory is reclaimed only on reset)
- [ ] Implement `reset()` that reclaims all memory at once
- [ ] Thread-safe: use `AtomicUsize` for the bump pointer, `fetch_add` with
      appropriate ordering
- [ ] Demonstrate it as `#[global_allocator]` for a simple program

### Arena Allocator
- [ ] Allocate from contiguous chunks; grow by allocating new chunks when current is
      exhausted
- [ ] Support `arena.alloc(value: T) -> &T` that moves a value into the arena and
      returns a reference with the arena's lifetime
- [ ] Handle `Drop` types: maintain a drop-list that runs destructors on arena reset
- [ ] Alignment-aware: different types with different alignments can coexist in the
      same chunk
- [ ] Demonstrate with a tree data structure where all nodes live in the arena

### Slab Allocator
- [ ] Pre-allocate a pool of fixed-size blocks
- [ ] `alloc` returns a block from the free list in O(1) using an intrusive linked
      list (the free block itself stores the next pointer)
- [ ] `dealloc` pushes the block back onto the free list in O(1)
- [ ] No fragmentation by construction — all blocks are the same size
- [ ] Implement a typed wrapper: `SlabAllocator<T>` that returns `&mut T` and
      handles layout automatically
- [ ] Demonstrate with 1 million allocate/deallocate cycles and compare latency
      against the system allocator using `criterion`

### Cross-Cutting
- [ ] Every allocator must be tested under Miri for undefined behavior
- [ ] Benchmark all three against the system allocator and jemalloc (`tikv-jemallocator`)
      using a realistic workload (not just microbenchmarks)
- [ ] Measure allocation pressure with DHAT (`dhat` crate) on a sample program,
      identify the hot allocation site, replace it with your custom allocator, and
      show the improvement

## Starting Points

- **bumpalo** (`fitzgen/bumpalo`): Study `src/lib.rs`. This is the production bump/arena
  allocator used by the Rust compiler's query system. Pay attention to
  `alloc_layout_slow` — the chunk growth strategy is non-trivial.
- **typed-arena** (`dropbox/rust-typed-arena`): Study `src/lib.rs`. Simpler than
  bumpalo, focused on single-type arenas. Instructive for understanding the lifetime
  relationship between the arena and its allocations.
- **slab** (`tokio-rs/slab`): Not technically a slab allocator in the OS sense, but
  study the free-list-in-an-array pattern in `src/lib.rs`. Your slab allocator
  extends this pattern to raw memory.
- **GlobalAlloc trait**: Read `alloc::alloc::GlobalAlloc` in the standard library
  source (`library/alloc/src/alloc.rs`). Note: `alloc` must never return null for
  zero-sized types and must return aligned pointers. `dealloc` receives the same
  `Layout` that was passed to `alloc`.
- **DHAT docs** (`nnethercote/dhat-rs`): The `dhat` crate provides heap profiling.
  Study the README for how to integrate it and interpret the output.

## Hints

1. Alignment math: given a bump pointer `ptr` and required alignment `align`, the
   next aligned address is `(ptr + align - 1) & !(align - 1)`. This rounds up to
   the nearest multiple of `align`. Get this wrong and you get segfaults on
   architectures that enforce alignment (ARM, not x86 — x86 will silently eat the
   performance cost, making the bug invisible until you deploy to ARM).

2. For the arena's drop-list, store a `Vec<(*mut u8, unsafe fn(*mut u8))>` where
   each entry is a pointer and a type-erased drop function. When you allocate a `T`
   that needs dropping, push `(ptr as *mut u8, |p| std::ptr::drop_in_place(p as *mut T))`.
   Run the list in reverse on reset.

3. The slab allocator's free list is intrusive: when a block is free, its first
   `size_of::<usize>()` bytes store the index/pointer of the next free block. When
   allocated, those bytes are the user's data. This dual-use is why the block size
   must be `>= size_of::<usize>()`.

4. `GlobalAlloc` requires `&self` — not `&mut self`. Your allocator must use interior
   mutability (`UnsafeCell`, atomics, or `Mutex`). For the bump allocator, atomics
   suffice. For the slab, a `Mutex<FreeList>` is simplest; a lock-free free list
   (Treiber stack) is the advanced path.

5. Do not confuse `GlobalAlloc` (stable, global scope) with the `Allocator` trait
   (nightly, per-collection). The `Allocator` trait lets you write
   `Vec::new_in(my_arena)` — experiment with it on nightly, but your core
   implementation should work on stable via `GlobalAlloc`.

## Going Further

- Implement a **pool allocator** that combines slab allocation with automatic size
  classes (8, 16, 32, 64, ... bytes), routing each allocation to the appropriate
  slab. This is how jemalloc and tcmalloc work at a high level.
- Build a **region-based allocator** for a compiler pass: parse an AST into an arena,
  run analysis passes that allocate into a second arena, drop the first arena while
  the second lives. Model the borrow checker's region inference problem.
- Make your bump allocator work in `no_std` for embedded targets. You will need to
  take a `&'static mut [u8]` from the linker script instead of a heap allocation.
- Integrate `tikv-jemallocator` or `mimalloc` as `#[global_allocator]` in a real
  application and benchmark against the system allocator. jemalloc typically wins on
  multi-threaded allocation-heavy workloads; mimalloc wins on allocation-light
  workloads with its smaller overhead.
- Study how the Rust compiler itself uses arenas: search for `DroplessArena` in
  `compiler/rustc_arena/src/lib.rs` in the rust-lang/rust repository.

## Resources

- [The Rust `GlobalAlloc` documentation](https://doc.rust-lang.org/std/alloc/trait.GlobalAlloc.html) —
  The trait you are implementing
- [bumpalo source](https://github.com/fitzgen/bumpalo) — Production bump allocator
- [typed-arena source](https://github.com/dropbox/rust-typed-arena) — Simple typed
  arena allocator
- [dhat-rs](https://github.com/nnethercote/dhat-rs) — Heap profiling for Rust
- [tikv-jemallocator](https://github.com/tikv/jemallocator) — jemalloc bindings
  for Rust
- [mimalloc](https://github.com/purpleprotocol/mimalloc_rust) — mimalloc bindings
- [rustc_arena source](https://github.com/rust-lang/rust/blob/master/compiler/rustc_arena/src/lib.rs) —
  How the Rust compiler itself does arena allocation
- [Bonwick, 1994: "The Slab Allocator: An Object-Caching Kernel Memory Allocator"](https://www.usenix.org/legacy/publications/library/proceedings/bos94/bonwick.html) —
  The original slab allocator paper from Solaris
- [Nicholas Nethercote: "How to speed up the Rust compiler"](https://blog.mozilla.org/nnethercote/) —
  Blog series on profiling and allocation optimization in rustc
- [Rust Allocator RFC 1398](https://rust-lang.github.io/rfcs/1398-kinds-of-allocators.html) —
  Design rationale for the allocator traits
