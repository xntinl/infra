# 22. Custom Smart Pointers and GC
**Difficulty**: Insane

## The Challenge

Rust's ownership system eliminates the need for garbage collection in most programs, but
certain domains — graph structures, interpreters, UI frameworks, game entity systems — create
webs of cyclic references that fight against the borrow checker. `Rc<RefCell<T>>` with `Weak`
references is the standard workaround, but it pushes cycle management onto the programmer and
offers no automatic reclamation of cycles. Your mission is to build a **tracing garbage collector**
that lives inside Rust's type system, providing automatic cycle collection while preserving
memory safety guarantees.

You will implement the core GC infrastructure from scratch: a `Trace` trait that objects implement
to describe their reference graph, a `GcRef<T>` smart pointer that participates in the GC heap,
a rooting API that prevents the collector from reclaiming objects reachable from the stack, and
the mark-and-sweep algorithm itself. The collector must handle interior mutability (since GC
references are inherently shared), must never create dangling pointers, and must correctly collect
cycles of arbitrary depth and shape. This is where Rust's guarantees meet their most interesting
stress test — you are building infrastructure that intentionally shares mutable state, something
Rust's type system normally forbids.

The deeper challenge is the **rooting problem**: the collector must know which GC objects are
reachable from the Rust stack so it never sweeps a live object. This requires a safe API that
makes it impossible to hold a `GcRef` across a collection without registering it as a root. You
will explore arena-based allocation, `Drop` implementations that deregister roots, and the
subtle interaction between Rust's destructor ordering and GC finalization. The final system
should be able to host a small interpreter with closures that capture their environment —
creating exactly the kind of cycles that motivated garbage collection in the first place.

## Acceptance Criteria

### Core Trait System
- [ ] Define a `Trace` trait with a `fn trace(&self, tracer: &mut Tracer)` method that visits all GC references held by the implementing type
- [ ] Implement `Trace` for all primitive types (`u8`..`u128`, `i8`..`i128`, `f32`, `f64`, `bool`, `char`, `String`, `()`) as no-ops
- [ ] Implement `Trace` for standard containers: `Vec<T: Trace>`, `HashMap<K: Trace, V: Trace>`, `Option<T: Trace>`, `Box<T: Trace>`
- [ ] Provide a derive macro `#[derive(Trace)]` that auto-implements `Trace` for structs by tracing all fields
- [ ] The `Trace` trait must be object-safe so that `dyn Trace` can be stored in the GC heap

### GC Heap and Allocation
- [ ] Implement a `GcHeap` struct that owns all GC-allocated objects and serves as the single entry point for allocation and collection
- [ ] `GcHeap::alloc<T: Trace + 'static>(&self, value: T) -> GcRef<T>` allocates an object on the GC heap and returns a tracked reference
- [ ] The heap stores objects as type-erased `GcBox` nodes in an intrusive linked list, each containing: the value, a mark bit, a `Trace` vtable pointer, and a next pointer
- [ ] Allocation must not trigger collection automatically — collection is explicit via `GcHeap::collect()`
- [ ] Track total allocated bytes and number of live objects, exposed via `GcHeap::stats() -> GcStats`

### GcRef Smart Pointer
- [ ] `GcRef<T>` is a Copy + Clone smart pointer (internally a pointer to a `GcBox<T>` on the GC heap)
- [ ] `GcRef<T>` implements `Deref<Target = T>` for ergonomic field access
- [ ] `GcRef<T>` implements `Trace` so that GC objects can reference other GC objects
- [ ] `GcRef<T>` does NOT implement `Drop` — the GC heap owns the memory, not the reference
- [ ] Provide `GcRef::ptr_eq(a, b) -> bool` for identity comparison (same allocation, not structural equality)

### Interior Mutability via GcCell
- [ ] Implement `GcCell<T>` — a `RefCell`-like wrapper designed for GC objects that allows mutation through shared references
- [ ] `GcCell<T>` implements `Trace` by tracing its inner value
- [ ] Provide `GcCell::borrow(&self) -> GcCellRef<T>` and `GcCell::borrow_mut(&self) -> GcCellRefMut<T>` with runtime borrow checking
- [ ] Borrowing a `GcCell` during a GC trace must not panic — the trace implementation must handle the case where the cell is already borrowed
- [ ] Document and handle the case where a `GcCellRefMut` is held during collection (either prevent collection while mutable borrows exist, or skip tracing borrowed cells with a clear safety argument)

### Rooting API
- [ ] Implement a `Root<T>` type that registers a `GcRef<T>` as a GC root, preventing it from being collected
- [ ] `Root<T>` implements `Drop` to deregister itself from the root set
- [ ] `Root<T>` implements `Deref<Target = T>` for transparent access
- [ ] Provide `GcHeap::root<T>(&self, gc_ref: GcRef<T>) -> Root<T>` as the only way to create roots
- [ ] The type system must make it impossible (or at least extremely difficult) to use a `GcRef` after collection without rooting it — consider using a `Mutation<'a>` token tied to the heap's lifetime
- [ ] Root set is maintained as a `Vec` of raw pointers to `GcBox` nodes, protected by the `Root` RAII guard

### Mark and Sweep Algorithm
- [ ] **Mark phase**: starting from all roots, recursively mark all reachable objects by setting their mark bit via the `Trace` trait
- [ ] **Sweep phase**: walk the entire heap linked list, free all unmarked objects, and clear mark bits on surviving objects
- [ ] Collection is triggered explicitly via `GcHeap::collect()` and must be safe to call at any point where no `GcCellRefMut` borrows are active
- [ ] After collection, `GcHeap::stats()` reflects the reduced object count and byte count
- [ ] The collector must handle self-referential objects (an object whose `Trace` impl visits itself)
- [ ] The collector must handle deep reference chains (1000+ objects deep) without stack overflow — use an explicit mark stack rather than recursion

### Cycle Collection
- [ ] Demonstrate collection of a simple two-node cycle: A -> B -> A, where neither A nor B is rooted
- [ ] Demonstrate collection of a three-node cycle with mixed types
- [ ] Demonstrate that a cycle where one node IS rooted keeps the entire cycle alive
- [ ] Demonstrate that unrooting the last root into a cycle causes the entire cycle to be collected on the next `GcHeap::collect()`
- [ ] Write a stress test that creates 10,000 cyclic structures and verifies they are all collected

### Interpreter Integration (Proof of Concept)
- [ ] Build a minimal Lisp-like interpreter that allocates cons cells, closures, and environments on the GC heap
- [ ] Closures capture their environment via `GcRef`, creating cycles (a closure stored in the environment it closes over)
- [ ] The interpreter can evaluate recursive functions without leaking memory
- [ ] Periodic collection during interpretation does not corrupt the interpreter state
- [ ] Demonstrate a program that would leak with `Rc<RefCell<T>>` but is correctly collected by your GC

### Safety and Correctness
- [ ] No `unsafe` code in the public API — all unsafety is encapsulated within the GC implementation
- [ ] The implementation uses `unsafe` only for: type erasure in `GcBox`, pointer arithmetic in the heap linked list, and raw pointer manipulation in the root set
- [ ] Every `unsafe` block has a `// SAFETY:` comment explaining the invariant
- [ ] Run all tests under Miri (`cargo +nightly miri test`) with no undefined behavior detected
- [ ] No use-after-free: once an object is swept, no `GcRef` to it can be dereferenced (enforced by the mutation token or by rooting requirements)

### Finalization
- [ ] Implement an optional `Finalize` trait: `trait Finalize { fn finalize(&self); }` that is called on objects just before they are swept
- [ ] Finalization runs after marking — only unreachable objects are finalized
- [ ] A finalized object may "resurrect" itself by storing a reference to itself in a rooted location — the collector must detect this and not free resurrected objects
- [ ] Provide a `GcRef::weak()` method that returns a `WeakGcRef<T>` which does not keep the object alive and returns `None` after the object is collected
- [ ] `WeakGcRef<T>` implements `Trace` as a no-op (it does not keep its target alive)
- [ ] Weak references are implemented via an indirection table: `WeakGcRef` points to a table entry, the table entry points to the `GcBox`, and the entry is nulled during sweep

### Debugging and Diagnostics
- [ ] Implement `GcHeap::dump_heap()` that prints all live objects with their type names, addresses, mark status, and reference counts (number of incoming GcRefs)
- [ ] Implement `GcHeap::find_roots_for(gc_ref: GcRef<T>) -> Vec<RootPath>` that traces backwards from an object to find which root(s) keep it alive — essential for debugging memory leaks
- [ ] Provide a `#[cfg(debug_assertions)]` allocation tracker that records the call site (file, line) of each allocation for leak diagnosis
- [ ] Log collection statistics: how many objects were marked, how many were swept, how long each phase took, peak mark stack depth

### Performance
- [ ] Benchmark allocation of 100,000 objects — must complete in under 100ms in release mode
- [ ] Benchmark collection of a 100,000-object heap with 10% survival rate — must complete in under 50ms
- [ ] Benchmark repeated alloc/collect cycles to verify no memory leaks in the GC infrastructure itself (RSS stays bounded)
- [ ] Compare performance against `Rc<RefCell<T>>` for the interpreter benchmark
- [ ] Profile mark phase cache behavior: objects allocated sequentially should be marked sequentially for cache locality
- [ ] Benchmark the derive macro overhead: compiling 100 `#[derive(Trace)]` structs should not noticeably increase compile time compared to 100 `#[derive(Clone)]` structs

## Starting Points

- Study the **`gc` crate** source code (https://github.com/manishearth/rust-gc) — this is the canonical implementation of tracing GC in Rust. Pay close attention to `Gc<T>`, `GcCell<T>`, the `Trace` trait, and how rooting is handled. Manish Goregaokar's design makes specific tradeoffs around thread safety and rooting that you should understand before making your own
- Study the **`bacon-rajan-cc` crate** (https://github.com/fitzgen/bacon-rajan-cc) — this implements the Bacon-Rajan cycle collection algorithm, which is a different approach from mark-and-sweep. Understanding both approaches will inform your design
- Read **"A Unified Theory of Garbage Collection"** by Bacon, Cheng, and Rajan (2004) — this paper shows that tracing and reference counting are duals of each other, which gives deep insight into what mark-and-sweep is actually doing
- Study the **SpiderMonkey GC rooting API** (https://searchfox.org/mozilla-central/source/js/public/RootingAPI.h) — Mozilla's JavaScript engine faces the exact same rooting problem in C++. Their `Rooted<T>` / `Handle<T>` design heavily influenced the Rust GC ecosystem
- Read Manish Goregaokar's blog post **"Prevent Child Access"** — it explains the `GhostCell` pattern and how to use branded lifetimes to enforce access discipline, which is directly applicable to the mutation token approach
- Study the **`typed-arena` crate** source code — arena allocation is a simpler form of bulk memory management, and understanding its `unsafe` patterns helps with implementing the GC heap
- Read **"Immix: A Mark-Region Garbage Collector with Space Efficiency, Fast Collection, and Mutator Performance"** by Blackburn and McKinley (2008) — if you want to go beyond basic mark-and-sweep, this paper describes a more sophisticated allocation and collection strategy
- Study the **OCaml runtime GC** source code (https://github.com/ocaml/ocaml/tree/trunk/runtime) — a production-grade generational GC that handles the same rooting problems with its `CAMLparam` / `CAMLlocal` macros
- Read **"Rust as a Language for High Performance GC Implementation"** by Lin et al. (2016) — this discusses using Rust to implement the Immix GC for the MMTk framework, revealing which Rust features help and which hinder GC implementation
- Study the **`shifgrethor` project** by withoutboats (https://github.com/withoutboats/shifgrethor) — an experimental GC for Rust that uses the `Pin` API and branded lifetimes for rooting safety. This is the most sophisticated approach to the rooting problem in the Rust ecosystem
- Read the **V8 GC documentation** (https://v8.dev/blog/trash-talk) — while V8 is in C++, its blog posts on concurrent marking, incremental sweeping, and generational collection provide the clearest explanations of these optimization strategies

## Hints

1. Start with the simplest possible design: a `GcHeap` that owns a `Vec<Box<dyn Trace>>` and allocates by pushing. Use indices as your "pointers" initially — you can optimize to raw pointers later once the algorithm is correct
2. The `Trace` trait is the linchpin. Every type on the GC heap must accurately report its references. A single missed reference means the collector will free a live object. A single phantom reference means a leak. Get this trait right before touching the collector
3. For type erasure in `GcBox`, you need to store a `Trace` vtable alongside the data. The cleanest approach is `GcBox { header: GcHeader, data: T }` where `GcHeader` stores the mark bit and a function pointer `fn(&dyn Any, &mut Tracer)` for tracing the erased type
4. The intrusive linked list for the heap is cleaner than a `Vec` because sweeping a `Vec` requires shifting elements or leaving tombstones. With a linked list, you can unlink and free dead nodes in O(1) during the sweep
5. For the rooting API, the key insight is: `GcRef<T>` by itself is **not safe to use across a collection boundary**. You need either (a) a branded lifetime that ties `GcRef` to a `Mutation<'gc>` scope, or (b) a runtime check that all live `GcRef`s are rooted. Option (a) is safer but more ergonomically costly
6. The mark phase is a graph traversal. Use an explicit worklist (a `Vec<*const GcBox>`) rather than recursive calls to `trace()`. Push newly discovered references onto the worklist, pop and mark until empty. This prevents stack overflow on deep graphs
7. For `GcCell`, you face a subtle issue: during the mark phase, the collector calls `trace()` on every object, but a mutator might hold a `GcCellRefMut`. The `gc` crate solves this by making collection only happen at safe points where no borrows are active. The simplest approach is to add a `fn can_collect(&self) -> bool` check to `GcHeap` that verifies no `GcCell` borrows are outstanding
8. The derive macro for `Trace` should expand `#[derive(Trace)] struct Foo { a: GcRef<Bar>, b: String }` into a `trace` implementation that calls `tracer.trace(&self.a)` and `tracer.trace(&self.b)` (the latter being a no-op). Use a proc-macro crate following standard Rust derive macro patterns
9. For the interpreter, a Lisp cons cell is the simplest cyclic structure: `struct Cons { car: Value, cdr: Value }` where `Value` is an enum containing `GcRef<Cons>`. Closures are `struct Closure { params: Vec<String>, body: Expr, env: GcRef<Env> }` where `Env` contains a `HashMap<String, Value>` that may contain the closure itself
10. To test cycle collection, create the cycle, drop all `Root`s into it, call `collect()`, and verify that `GcHeap::stats().live_objects` decreased. You can also track allocations with a global counter in `GcBox::new` / `GcBox::drop` to verify exact counts
11. For Miri compatibility, avoid `transmute` where possible — prefer `ptr::read`, `ptr::write`, and `ptr::cast`. Miri is very strict about pointer provenance, so raw pointer arithmetic must go through `ptr::add`/`ptr::sub` rather than casting to `usize` and back
12. The hardest bug you will encounter is a GC object being swept while a `GcRef` to it still exists somewhere on the Rust stack but is not rooted. This manifests as a use-after-free that Miri catches but is otherwise silent. The mutation token pattern prevents this at compile time by tying all `GcRef` access to a scope that cannot overlap with collection
13. Consider implementing a simple generational optimization: maintain a "young generation" list of recently allocated objects and collect only those first. If a young object survives N collections, promote it to the "old generation." This dramatically improves throughput for the interpreter benchmark where most allocations are short-lived
14. For the stress test, use a deterministic pseudo-random number generator to create cycles of varying sizes and shapes. Verify that after collection, only the rooted subgraph survives. Count allocations and deallocations to ensure zero leaks
15. Profile your collector with `perf` or `cargo flamegraph`. The mark phase is typically dominated by cache misses when chasing pointers. The sweep phase is dominated by deallocation. If sweep is slow, consider lazy sweeping (sweep a few objects per allocation rather than all at once)
16. For the `Finalize` trait, be aware of the "popping the GC stack" problem: if a finalizer stores a reference to the finalized object in a root (resurrecting it), you must not free the object. The standard approach is: run finalizers, then re-mark from roots to find resurrected objects, then sweep only the still-unmarked objects. This adds a second mark pass but is the only correct approach
17. For `WeakGcRef`, the indirection table approach works as follows: allocate a `WeakEntry { target: Option<*const GcBox> }` for each weak reference. During sweep, before freeing a `GcBox`, null out all `WeakEntry`s pointing to it. When dereferencing a `WeakGcRef`, check the entry — if it is null, the target was collected. The entries themselves must be freed separately (when the last `WeakGcRef` to an entry is dropped)
18. The `dump_heap` and `find_roots_for` debugging tools are invaluable during development. `find_roots_for` works by running a modified mark phase that records the path (parent -> child chain) for each marked object, then looking up the path for the target object. This is expensive but only used for debugging
19. For the interpreter proof of concept, implement at minimum: `(define (f x) (if (= x 0) 1 (* x (f (- x 1)))))` (recursive factorial). The environment that binds `f` contains the closure for `f`, and the closure captures the environment — this is the classic cycle. Verify that after evaluating `(f 10)`, collection reclaims the intermediate environments
20. Consider the `GhostCell`-inspired API where `GcRef<T>` requires a `&GcToken` to access its contents, and the `GcToken` is exclusively borrowed during collection. This makes it compile-time impossible to read a `GcRef` while collection is in progress, preventing the most dangerous class of bugs. The tradeoff is that every field access requires passing the token, which is ergonomically expensive
21. Test the edge case of an empty heap collection: `GcHeap::collect()` on an empty heap must not panic or corrupt state. Test the edge case of collecting a heap where every object is rooted (nothing to collect). Test the edge case of collecting a heap where nothing is rooted (everything collected). These are the boundaries where off-by-one errors hide
