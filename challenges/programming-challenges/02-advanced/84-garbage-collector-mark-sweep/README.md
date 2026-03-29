# 84. Garbage Collector Mark-Sweep

<!--
difficulty: advanced
category: compilers-runtime-systems
languages: [rust]
concepts: [mark-and-sweep, object-graph, root-set, tri-color-marking, memory-management, graph-traversal, unsafe-rust]
estimated_time: 14-18 hours
bloom_level: evaluate
prerequisites: [rust-ownership, unsafe-rust-basics, graph-traversal, smart-pointers, interior-mutability, trait-objects]
-->

## Languages

- Rust (stable)

## Prerequisites

- Rust ownership, borrowing, and lifetimes (the GC sits outside the borrow checker's guarantees)
- `unsafe` Rust basics: raw pointers, manual allocation with `std::alloc::Layout`
- Graph traversal algorithms (DFS/BFS for the mark phase)
- Smart pointers (`Rc`, `RefCell`) for understanding reference semantics the GC replaces
- Interior mutability patterns (`Cell`, `RefCell`, `UnsafeCell`)
- Trait objects for polymorphic object headers

## Learning Objectives

- **Implement** a tracing garbage collector using the mark-and-sweep algorithm with a managed heap
- **Design** an object header layout that supports tri-color marking, type metadata, and pointer enumeration
- **Apply** graph traversal to trace live objects from a root set through arbitrarily connected object graphs including cycles
- **Analyze** the distinction between precise and conservative collection and the trade-offs each imposes on the runtime
- **Evaluate** the impact of heap fragmentation on allocation performance and the limitations of non-compacting collection
- **Justify** design decisions around root set identification, object layout, and the boundary between safe and unsafe code

## The Challenge

Every language runtime that frees the programmer from manual memory management needs a garbage collector. The mark-and-sweep algorithm, first described by John McCarthy in 1960 for Lisp, is the foundation of all modern tracing collectors. Its principle is direct: starting from a set of root references (stack variables, global variables, registers), traverse the object graph and mark every reachable object. Then sweep the entire heap and reclaim every unmarked object. Any object not reachable from the roots is garbage, no matter how many other garbage objects point to it -- this is what makes tracing collectors handle cycles correctly, unlike reference counting.

The collector operates in two phases. The **mark phase** starts from the root set and performs a graph traversal (DFS or BFS). Each visited object is colored: white (unvisited, presumed dead), gray (discovered but not fully scanned), black (fully scanned, all children discovered). The traversal processes gray objects by scanning their fields for pointers, coloring discovered white objects gray, and then coloring the scanned object black. When no gray objects remain, every reachable object is black and every unreachable object is white. The **sweep phase** walks the heap linearly, freeing every white object and resetting every black object to white for the next collection cycle.

The hardest design problem is object layout. The collector must be able to: identify which fields of an object are pointers (versus integers or floats), find the size of each object to advance through the heap, and store per-object metadata (color, type tag). A common approach is an object header preceding the payload, containing a type descriptor pointer, the mark color, and the object size. The type descriptor tells the collector which offsets within the payload contain pointers.

You will build a mark-and-sweep garbage collector in Rust with a managed heap, a root set API, and tri-color marking. The collector must handle cyclic references correctly, support multiple object types, and provide a simple allocation and collection API suitable for integration with an interpreter or test harness.

## Requirements

1. Implement a managed heap as a contiguous byte array (or a list of allocated blocks). Allocation uses bump pointer or free list. Each allocated object is preceded by an object header containing: mark color (white/gray/black), object size, type descriptor index, and a next-pointer for the heap's object linked list
2. Implement at least three object types: `Integer` (no pointers), `Pair` (two pointer fields), and `Array` (variable number of pointer fields). Each type has a type descriptor that enumerates its pointer field offsets so the collector can trace them
3. Implement root set management: `add_root(obj)`, `remove_root(obj)`, `clear_roots()`. Roots are the entry points for the mark phase. The test harness or interpreter pushes and pops roots as variables come into and go out of scope
4. Implement the mark phase using tri-color marking: initialize all objects as white, color root objects gray, process gray objects by scanning pointer fields and coloring discovered white objects gray, color the scanned object black. Use an explicit worklist (Vec or VecDeque) rather than recursion to avoid stack overflow on deep object graphs
5. Implement the sweep phase: walk the heap's object list, free every white object (return its memory to the free list or mark the region as available), reset every black object to white for the next cycle
6. Handle cyclic references: two or more objects that point to each other but are unreachable from any root must all be collected. The mark phase naturally handles this because unreachable objects are never colored gray
7. Implement `collect()` that runs a full mark-sweep cycle and returns the number of objects freed and bytes reclaimed
8. Provide an `alloc<T: GcObject>(value: T) -> GcRef` API that allocates an object on the managed heap, writes its header and payload, and returns a reference. Trigger automatic collection when the heap exceeds a configurable threshold
9. Implement heap statistics: total allocated bytes, live object count, garbage collected count per cycle, total collections run, and current heap utilization percentage
10. All public APIs must be safe Rust. Encapsulate all `unsafe` operations (raw pointer arithmetic, manual allocation) within the heap and collector modules behind safe interfaces

## Hints

The object header is the critical data structure. A minimal header needs four fields packed into a struct:

```
+-------------------+
| color: u8         |  -- 0=white, 1=gray, 2=black
| type_id: u16      |  -- index into type descriptor table
| size: u32         |  -- total size including header
| next: *mut Header |  -- linked list of all heap objects
+-------------------+
| payload bytes...  |  -- the actual object data
+-------------------+
```

The type descriptor table is a `Vec<TypeDescriptor>` where each entry describes an object type: its name, payload size, and a list of offsets within the payload that contain pointer fields. When the marker scans an object, it reads the type descriptor to know which payload bytes are pointers to other GC-managed objects.

For the free list allocator, maintain a linked list of free blocks. On allocation, walk the list and find a block large enough (first-fit or best-fit). On deallocation during sweep, add the block back to the free list. Coalescing adjacent free blocks prevents fragmentation but is not required for a first implementation.

The worklist for the mark phase is straightforward: a `Vec<*mut Header>` acting as a stack (DFS) or queue (BFS). Push all root objects as gray, then loop: pop an object, scan its pointer fields using the type descriptor, push any white objects found (coloring them gray), and color the scanned object black. The loop terminates when the worklist is empty.

One subtle issue: the `GcRef` returned to user code is essentially a raw pointer into the managed heap. Since mark-sweep does not move objects, these pointers remain valid across collections. This is a key advantage over copying collectors (and a limitation -- no compaction means fragmentation accumulates).

## Acceptance Criteria

- [ ] Objects are allocated on the managed heap with proper headers and payload
- [ ] Root set API correctly tracks which objects are roots
- [ ] Mark phase traces from roots and correctly colors all reachable objects black
- [ ] Sweep phase frees all unreachable (white) objects and reclaims their memory
- [ ] Cyclic references between unreachable objects are collected (not leaked)
- [ ] Cyclic references between reachable objects are preserved (not collected)
- [ ] Tri-color invariant holds: no black object points directly to a white object during marking
- [ ] Automatic collection triggers when heap usage exceeds threshold
- [ ] Multiple collection cycles work correctly (colors reset, free list maintained)
- [ ] Heap statistics accurately report allocations, collections, and utilization
- [ ] No undefined behavior: all unsafe code is sound (Miri clean under `cargo +nightly miri test`)
- [ ] Allocating and collecting 100k objects completes in under 1 second
- [ ] Test harness demonstrates: simple collection, cycle collection, root protection, multi-cycle correctness

## Going Further

- Implement free list coalescing to reduce fragmentation after many allocation/deallocation cycles
- Add weak references that do not prevent collection but are nulled when the referent is swept
- Implement incremental marking using the tri-color invariant and a write barrier
- Add finalization: run a user-defined callback before an object is swept
- Integrate the collector with the interpreter from Challenge 36
- Measure and graph heap fragmentation over many allocation/collection cycles

## Research Resources

- [The Garbage Collection Handbook (Jones, Hosking, Moss)](https://gchandbook.org/) -- the definitive reference, Chapters 2-3 cover mark-sweep in depth
- [Crafting Interpreters, Chapter 26: Garbage Collection](https://craftinginterpreters.com/garbage-collection.html) -- practical mark-sweep implementation in C for a language VM
- [Baby's First Garbage Collector (Bob Nystrom)](https://journal.stuffwithstuff.com/2013/04/26/babys-first-garbage-collector/) -- minimal mark-sweep in ~200 lines of C, excellent conceptual introduction
- [Rust `std::alloc` module](https://doc.rust-lang.org/std/alloc/index.html) -- low-level allocation API for building custom heaps
- [The Tri-Color Abstraction (Dijkstra et al., 1978)](https://dl.acm.org/doi/10.1145/359642.359655) -- original paper introducing tri-color marking for on-the-fly collection
- [Unified Theory of Garbage Collection (Bacon et al., 2004)](https://www.cs.virginia.edu/~cs415/reading/bacon-garbage.pdf) -- shows tracing and reference counting are duals, provides theoretical foundation
- [Simple Mark-Sweep in Rust (GitHub examples)](https://github.com/pliniker/mo-gc) -- community implementations of GC in Rust for reference
- [Memory Management Reference](https://www.memorymanagement.org/) -- encyclopedic resource on GC algorithms, terminology, and history
