# 22. Custom Smart Pointers and Garbage Collection

**Difficulty**: Insane

## The Challenge

Build a tracing garbage collector as a Rust library. Your GC must provide a `Gc<T>`
smart pointer that supports arbitrary reference graphs including cycles — something
that `Rc<T>` fundamentally cannot handle. You will implement mark-and-sweep collection,
a root set, interior mutability via `GcCell<T>`, and weak references.

This is not an academic exercise. Garbage collection in Rust is a solved problem in
production: the Ruffle Flash emulator uses `gc-arena` for its ActionScript VM, the
`dumpster` crate provides concurrent cycle collection, and `rust-gc` demonstrates
the fundamental mark-and-sweep pattern. Your job is to build one from scratch,
understanding every safety invariant along the way.

The core tension is this: Rust's ownership model assumes a single owner or reference-
counted sharing. A tracing GC breaks this assumption — multiple `Gc<T>` handles can
point to the same allocation, and the GC decides when to free it, not the `Drop`
implementation. Making this sound in Rust requires careful use of `unsafe`, a custom
`Trace` trait, and specific invariants around root set management. Getting any of
these wrong produces use-after-free, double-free, or memory leaks — and the compiler
will not save you.

## Acceptance Criteria

- [ ] Implement `Gc<T>` that can be cloned to create multiple handles to the same allocation
- [ ] Implement the `Trace` trait with an `unsafe` method `trace(&self, tracer: &mut Tracer)` that visits all contained `Gc` pointers
- [ ] Provide a derive macro `#[derive(Trace)]` that auto-implements `Trace` for structs and enums
- [ ] Implement `GcCell<T>` providing interior mutability compatible with the GC (not `RefCell` — the GC must be able to trace through a borrowed `GcCell`)
- [ ] Implement mark-and-sweep collection using tri-color marking (white/gray/black)
- [ ] The collector correctly identifies and frees cyclic garbage (e.g., A -> B -> A where both are unreachable)
- [ ] Implement `GcWeak<T>` weak references that do not prevent collection
- [ ] Root set management: GC roots are explicitly registered and deregistered; only objects reachable from roots survive collection
- [ ] Collection can be triggered manually via `gc::collect()` and automatically when allocation count exceeds a threshold
- [ ] Finalization: types implementing `Finalize` have their finalizer called before deallocation (but finalizers must not access other GC'd objects that may already be freed)
- [ ] Demonstrate cycle collection: create a doubly-linked list using `Gc<GcCell<Node>>`, drop all external references, call `collect()`, verify the nodes are freed
- [ ] Write a stress test: allocate 100,000 objects with random reference graphs, drop half the roots, collect, verify no leaks using a custom allocator that counts allocations

## Background

A tracing garbage collector works by starting from a set of known-live references
(the root set), following all pointers reachable from those roots, and freeing
everything that was not visited. The standard algorithm is tri-color marking:

- **White**: not yet visited (presumed dead)
- **Gray**: visited but children not yet traced (on the worklist)
- **Black**: visited and all children traced (definitely alive)

Collection begins by coloring everything white, then coloring roots gray and adding
them to a worklist. The collector pops items from the worklist, traces their children
(coloring them gray if white), and colors the processed item black. When the worklist
is empty, all white objects are unreachable and can be freed.

The `Trace` trait is the critical unsafe contract. Every type stored in a `Gc<T>`
must correctly report all contained `Gc` pointers during tracing. If a `Trace`
implementation fails to report a pointer, the collector may free a reachable object.
If it reports a pointer that does not exist, the collector may follow garbage memory.
Both are unsound.

In `rust-gc`, the collector maintains a thread-local linked list of all `Gc`
allocations (the "all objects" list) and a separate list of root objects. Each
`GcBox` stores the mark bit inline. The implementation is in
`Manishearth/rust-gc/gc/src/gc.rs` — study the `mark()`, `sweep()`, and
`collect_garbage()` functions.

## Architecture Hints

1. Each `Gc<T>` is a pointer to a `GcBox<T>` which contains: the value, a mark bit
   (or color), a `roots` counter (how many times this object appears on the stack as
   a direct root), pointers to form an intrusive linked list of all allocations, and
   optionally a finalizer function pointer. The root count is incremented when a `Gc`
   is created on the stack and decremented when dropped — but cloning a `Gc` that is
   stored inside another `Gc` does NOT increment the root count.

2. The distinction between "rooted" and "non-rooted" `Gc` handles is the key insight
   from `rust-gc`. A rooted `Gc` is one that lives on the stack or in a global. A
   non-rooted one lives inside another `Gc`'d allocation. Only rooted handles prevent
   collection. This is what makes cycle detection possible: if two objects point to
   each other but neither is rooted, both are garbage.

3. `GcCell<T>` must track borrow state AND cooperate with tracing. During the mark
   phase, the collector must be able to trace through a `GcCell` even if user code
   has not borrowed it. This means `GcCell` cannot use `RefCell` internally — it
   needs a custom borrow flag that the GC can bypass during tracing (but user code
   cannot).

4. Finalization order is a notorious problem. If object A references object B and
   both are garbage, running A's finalizer after B has been freed causes a
   use-after-free. The standard solution: run all finalizers first (in an arbitrary
   order), then deallocate. Finalizers must not assume their referents are still valid.
   This is why `rust-gc` separates `Finalize` from `Drop`.

5. For the derive macro, you will walk the fields of structs and enum variants.
   For each field that is `Gc<T>`, emit `field.trace(tracer)`. For fields that are
   not `Gc`, emit nothing (or call `Trace::trace` on them, which bottoms out at a
   no-op impl for primitive types). The derive logic is very similar to
   `#[derive(Clone)]` but with a different method.

## Starting Points

- **rust-gc source**: [github.com/Manishearth/rust-gc](https://github.com/Manishearth/rust-gc) — study `gc/src/gc.rs` for the
  core collector. The `GcBox` struct layout, the `mark()` function, and the `sweep()`
  function are the essential code. The derive macro is in `gc_derive/`.
- **Manish Goregaokar's blog**: ["A Tour of Safe Tracing GC Designs in Rust"](https://manishearth.github.io/blog/2021/04/05/a-tour-of-safe-tracing-gc-designs-in-rust/) — comprehensive survey of
  every approach to GC in Rust, with analysis of safety invariants. Read this first.
- **Manish Goregaokar's blog**: ["Designing a GC in Rust"](https://manishearth.github.io/blog/2015/09/01/designing-a-gc-in-rust/) — the original design
  rationale for `rust-gc`, including why `Trace` is unsafe and why `GcCell` exists.
- **gc-arena source**: [github.com/kyren/gc-arena](https://github.com/kyren/gc-arena) — used by the Ruffle Flash emulator. Studies
  a different design: arena-based GC where all `Gc` pointers are confined to a
  closure scope, enabling safe incremental collection without `unsafe` in user code.
  The collection algorithm is similar to PUC-Rio Lua, optimized for low pause time.
- **bacon-rajan-cc source**: [github.com/fitzgen/bacon-rajan-cc](https://github.com/fitzgen/bacon-rajan-cc) — reference counting
  with cycle collection based on the Bacon-Rajan algorithm. Study `src/collect.rs`
  for the three-phase cycle detection: `mark_roots`, `scan_roots`, `collect_roots`.
- **dumpster source**: [github.com/claytonwramsey/dumpster](https://github.com/claytonwramsey/dumpster) — cycle-tracking GC that
  extends reference counting. Provides both thread-local (`unsync`) and thread-safe
  (`sync`) collectors. Version 2.0 adds `dyn Trait` support.
- **shredder source**: [github.com/Others/shredder](https://github.com/Others/shredder) — concurrent GC with background
  collection and destruction. Study how `GcGuard` provides safe access without
  stopping the world.
- **kyju.org**: ["Techniques for Safe Garbage Collection in Rust"](https://kyju.org/blog/rust-safe-garbage-collection/) — analysis of
  gc-arena's safety model and the role of branded lifetimes.

## Going Further

- Implement generational collection: divide the heap into young and old generations.
  New objects go to the young generation, which is collected frequently. Objects that
  survive multiple collections are promoted to the old generation, which is collected
  rarely. This requires a write barrier to track cross-generational pointers.
- Implement incremental collection based on the gc-arena model: instead of stopping
  the world, process a fixed number of gray objects per step. The challenge is
  maintaining the tri-color invariant when user code can mutate the graph between
  collection steps (the "snapshot-at-the-beginning" write barrier).
- Make the collector thread-safe: multiple threads can allocate `Gc<T>` objects and
  trigger collection. Study `shredder`'s approach for concurrent collection.
- Implement the Bacon-Rajan cycle collection algorithm as an alternative to mark-and-
  sweep. This hybrid approach uses reference counting for acyclic objects (which is
  most objects) and only engages cycle detection for objects whose reference count
  drops to a suspicious threshold.
- Benchmark your GC against `Rc<RefCell<T>>` (no cycle support) and `dumpster::Gc`
  for a workload that creates and destroys many small cyclic structures (e.g.,
  building and tearing down a graph database).

## Resources

**Source Code**
- [Manishearth/rust-gc](https://github.com/Manishearth/rust-gc) — simple tracing mark-and-sweep GC, `gc/src/gc.rs` is the core
- [kyren/gc-arena](https://github.com/kyren/gc-arena) — arena-based incremental GC, used by Ruffle
- [fitzgen/bacon-rajan-cc](https://github.com/fitzgen/bacon-rajan-cc) — reference counting with cycle collection, `src/collect.rs`
- [claytonwramsey/dumpster](https://github.com/claytonwramsey/dumpster) — cycle-tracking GC with `unsync` and `sync` modules
- [Others/shredder](https://github.com/Others/shredder) — concurrent GC with background collection

**Blog Posts**
- [A Tour of Safe Tracing GC Designs in Rust](https://manishearth.github.io/blog/2021/04/05/a-tour-of-safe-tracing-gc-designs-in-rust/) — Manish Goregaokar's comprehensive survey
- [Designing a GC in Rust](https://manishearth.github.io/blog/2015/09/01/designing-a-gc-in-rust/) — original rust-gc design rationale
- [Techniques for Safe Garbage Collection in Rust](https://kyju.org/blog/rust-safe-garbage-collection/) — gc-arena's safety model analysis
- [Shredder: Garbage Collection as a Library for Rust](https://blog.typingtheory.com/shredder-garbage-collection-as-a-library-for-rust/) — design of concurrent GC

**Papers**
- David F. Bacon, V.T. Rajan — "Concurrent Cycle Collection in Reference Counted Systems" (ECOOP 2001) — the algorithm behind `bacon-rajan-cc`
- Edsger W. Dijkstra et al. — "On-the-Fly Garbage Collection: An Exercise in Cooperation" (1978) — the original tri-color marking paper
- Richard Jones, Antony Hosking, Eliot Moss — *The Garbage Collection Handbook* (2nd ed., CRC Press) — the definitive reference on GC algorithms
- "Breadth-first Cycle Collection Reference Counting" (SAC 2025, ACM) — recent academic work on cycle collection in Rust

**Documentation**
- [gc 0.5.1 docs](https://docs.rs/crate/gc/latest) — the `Gc`, `GcCell`, `Trace`, and `Finalize` APIs
- [gc-arena 0.5.3 docs](https://docs.rs/crate/gc-arena/latest) — arena-based GC with `Collect` trait
- [dumpster docs](https://docs.rs/dumpster/latest/dumpster/) — `Gc`, `Collectable` trait, `unsync` and `sync` modules
