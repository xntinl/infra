# 134. Garbage Collector Generational

<!--
difficulty: insane
category: compilers-runtime-systems
languages: [rust]
concepts: [generational-gc, copying-collector, semi-space, write-barrier, remembered-set, bump-allocation, mark-compact, object-promotion]
estimated_time: 25-35 hours
bloom_level: create
prerequisites: [mark-sweep-gc, unsafe-rust, pointer-arithmetic, memory-layout, graph-traversal, interior-mutability]
-->

## Languages

- Rust (1.75+ stable)

## The Challenge

Build a generational garbage collector with a young generation using semi-space copying collection and an old generation using mark-sweep. The generational hypothesis -- the empirical observation that most objects die young -- drives the design: collect the young generation frequently and cheaply, and collect the old generation rarely. Objects that survive multiple young collections are promoted to the old generation. A write barrier tracks old-to-young pointers so that young collections do not need to scan the entire old generation.

The young generation is divided into two equal semi-spaces: from-space (where objects are allocated) and to-space (the evacuation target during collection). Allocation is a bump pointer increment -- O(1) and cache-friendly. When from-space is full, a minor collection copies all reachable objects from from-space to to-space using Cheney's algorithm (BFS with a scan pointer), then swaps the spaces. Objects that have survived a configurable number of minor collections are promoted (copied) to the old generation instead of to-space.

The old generation uses mark-sweep (or optionally mark-compact). Major collections are triggered when the old generation exceeds a size threshold. Since major collections are expensive (proportional to total live objects), the design must minimize their frequency.

The critical mechanism is the write barrier. When a mutator writes a pointer from an old-generation object to a young-generation object, that old object must be added to a remembered set. During minor collection, the remembered set acts as additional roots -- without it, young objects referenced only from old objects would be incorrectly collected. The write barrier intercepts every pointer store and checks whether it crosses the generational boundary.

## Acceptance Criteria

- [ ] Young generation uses semi-space copying with bump pointer allocation (O(1) per allocation)
- [ ] Minor collection copies reachable young objects to to-space using Cheney's algorithm, then swaps spaces
- [ ] Objects surviving N minor collections (configurable, default 3) are promoted to old generation
- [ ] Old generation uses mark-sweep with a free list allocator
- [ ] Write barrier detects old-to-young pointer stores and adds the source object to the remembered set
- [ ] Minor collection uses remembered set as additional roots (does not scan entire old generation)
- [ ] Major collection marks from both root set and old-generation objects, sweeps old generation
- [ ] Cyclic references within the young generation are collected during minor GC
- [ ] Cyclic references spanning generations are collected during major GC
- [ ] Forwarding pointers correctly update all references to relocated young objects
- [ ] After promotion, old objects referencing promoted objects have updated pointers
- [ ] Heap statistics report: minor/major collection counts, promotion rate, remembered set size, generation sizes
- [ ] No undefined behavior: all unsafe code is sound (Miri clean for non-performance tests)
- [ ] Stress test: allocate 1 million short-lived objects with 1% survival rate, verify minor GC keeps pause times under 5ms

## Starting Points

- **Cheney's algorithm** is a BFS copying collector using two pointers: a scan pointer and a free pointer. Objects are copied to to-space at the free pointer. The scan pointer advances through to-space, examining each copied object's fields for pointers to from-space objects. When scan catches up to free, all reachable objects have been copied. The elegance is that to-space itself serves as the BFS queue -- no separate worklist is needed.

- **Forwarding pointers**: when an object is copied from from-space to to-space, a forwarding pointer is written at the object's old location (overwriting the header). Any subsequent reference to the old address finds the forwarding pointer and follows it to the new location. This ensures that each object is copied exactly once, even if multiple references point to it.

- **Write barrier granularity**: a card table divides old generation memory into fixed-size cards (e.g., 512 bytes). Instead of tracking individual objects in the remembered set, the barrier marks the card containing the store as dirty. During minor collection, all objects in dirty cards are scanned as roots. Card tables are cheaper per barrier invocation but scan more objects per collection. A precise remembered set tracks individual objects but has higher per-barrier cost.

## Hints

1. Represent the young semi-spaces as two `Vec<u8>` of equal size. The bump pointer is an offset into the current from-space. Allocation increments the bump pointer by header size + payload size. When it reaches the end, trigger a minor collection.

2. During Cheney's copy, when you encounter a pointer to a from-space object: if it already has a forwarding pointer (check a flag in the header), use the forwarded address. Otherwise, copy the object to the free pointer in to-space, write a forwarding pointer at the old location, and use the new address. This handles diamonds (multiple references to the same object) and cycles.

3. For the write barrier, the simplest approach is a `Vec<*mut ObjHeader>` remembered set. On every pointer store from old to young, push the source object. Before minor collection, deduplicate the set. After minor collection, rebuild the set (some entries may no longer point to young objects after promotion).

## Resources

- [The Garbage Collection Handbook (Jones, Hosking, Moss)](https://gchandbook.org/) -- Chapters 4 (copying collection), 9 (generational GC), authoritative reference
- [Crafting Interpreters, Chapter 26: Garbage Collection](https://craftinginterpreters.com/garbage-collection.html) -- context for integrating GC with interpreters
- [A Unified Theory of Garbage Collection (Bacon et al., 2004)](https://www.cs.virginia.edu/~cs415/reading/bacon-garbage.pdf) -- theoretical foundation connecting tracing and counting
- [Cheney's Algorithm (C.J. Cheney, 1970)](https://dl.acm.org/doi/10.1145/362790.362798) -- original semi-space copying collector paper
- [Generational Garbage Collection (Ungar, 1984)](https://dl.acm.org/doi/10.1145/800020.808261) -- the original generational GC paper from Smalltalk-80
- [V8 Blog: Trash Talk (the Orinoco garbage collector)](https://v8.dev/blog/trash-talk) -- how V8 implements generational GC with write barriers and concurrent marking
- [GC FAQ (Hans Boehm)](https://www.hboehm.info/gc/gc_faq.html) -- practical answers to common GC implementation questions
- [Memory Management Reference](https://www.memorymanagement.org/) -- encyclopedic resource on collector algorithms and terminology
