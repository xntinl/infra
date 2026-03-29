# 119. Arena Allocator with Generational Indices

<!--
difficulty: advanced
category: systems-programming
languages: [rust]
concepts: [generational-indices, arena-allocator, aba-problem, entity-management, cache-friendly-layout, unsafe-rust, iterator-design]
estimated_time: 10-15 hours
bloom_level: evaluate, create
prerequisites: [rust-generics, option-enum, unsafe-rust-basics, memory-layout, iterator-trait, index-based-data-structures]
-->

## Languages

- Rust (stable)

## Prerequisites

- Solid understanding of Rust generics, `Option<T>`, and enum-based discriminated unions
- Familiarity with `unsafe` Rust: raw indexing, `MaybeUninit`, and manual memory management
- Understanding of the ABA problem in concurrent and reuse scenarios
- Knowledge of entity-component systems (ECS) or object pool patterns
- Experience implementing Rust iterators (`Iterator` trait, `next()`, `size_hint()`)
- Basic understanding of cache locality and data-oriented design

## Learning Objectives

- **Implement** a generational arena where each slot has a generation counter that increments on removal, invalidating stale handles
- **Evaluate** the trade-offs between generational indices and raw pointers/references for entity management
- **Design** a free list that recycles slots without fragmentation, maintaining O(1) insert and remove
- **Analyze** how generational indices prevent the ABA problem that plagues index-based and pointer-based reuse schemes
- **Create** efficient iteration over live entries using a dense iteration strategy or skip-based scanning
- **Implement** arena compaction that relocates entries to eliminate gaps, returning a remapping table for updating external references

## The Challenge

Games, simulations, network servers, and ECS architectures all manage large collections of entities that are created and destroyed dynamically. The naive approach -- storing entities in a `Vec` and removing by index -- has a critical flaw: after removing entity at index 5 and inserting a new entity at index 5, any code still holding "index 5" now silently refers to the wrong entity. This is the ABA problem applied to indices.

A generational arena solves this with a two-part handle: `(index, generation)`. Each slot in the arena tracks a generation counter. When an entity is removed, the generation increments. When code tries to access index 5 with generation 3, but the slot is now at generation 4, the lookup returns `None` -- a safe, detectable "dangling reference" without any unsafe pointer mechanics.

Build a generational arena allocator in Rust. The arena stores elements of type `T` in a contiguous backing array. Insertion returns a `Handle` containing the index and generation. Removal increments the generation and adds the slot to a free list. Access via handle is O(1) with a generation check. Provide iteration over all live entries, and implement a compaction operation that eliminates gaps in the backing array for improved cache performance.

This pattern is the foundation of entity storage in game engines (Bevy, specs, hecs), connection managers in servers, and resource handle systems in graphics APIs.

## Requirements

1. Implement `Handle` as a lightweight copyable struct containing a `u32` index and a `u32` generation. Implement `PartialEq`, `Eq`, `Hash`, `Debug`, and `Display`
2. Implement `Arena<T>` with a backing store of `Entry<T>` slots. Each `Entry` is either `Occupied { value: T, generation: u32 }` or `Vacant { next_free: Option<u32>, generation: u32 }`
3. `insert(value: T) -> Handle`: Place the value in the first free slot (from the free list head). If no free slots, grow the backing array. Return a handle with the slot's current generation
4. `remove(handle: Handle) -> Option<T>`: If the handle's generation matches the slot's generation and the slot is occupied, remove the value, increment the generation, add the slot to the free list, and return the value. Otherwise return `None`
5. `get(handle: Handle) -> Option<&T>` and `get_mut(handle: Handle) -> Option<&mut T>`: Return a reference only if the handle's generation matches. Stale handles return `None`
6. `contains(handle: Handle) -> bool`: Check if a handle still refers to a live entry
7. Implement `iter()` returning an iterator over `(Handle, &T)` for all live entries, and `iter_mut()` for `(Handle, &mut T)`
8. Implement `len()` (live count), `capacity()` (total slots), and `is_empty()`
9. Implement `compact() -> Vec<(Handle, Handle)>`: Relocate all live entries to the front of the array, eliminating gaps. Return a list of `(old_handle, new_handle)` pairs so callers can update external references. After compaction, the free list covers only the tail
10. Implement `retain(predicate: impl FnMut(&T) -> bool)`: Remove all entries for which the predicate returns false, similar to `Vec::retain`
11. Write tests demonstrating: stale handle detection, free list reuse, generation overflow behavior, iteration correctness, compaction with remapping, and use as an entity store (create 1000 entities, remove 500 randomly, verify remaining 500 are accessible)

## Hints

**Hint 1 -- Entry enum layout**: Use a Rust enum to distinguish occupied and vacant slots. The generation lives in both variants so it survives transitions:

```rust
enum Entry<T> {
    Occupied { value: T, generation: u32 },
    Vacant { next_free: Option<u32>, generation: u32 },
}
```

**Hint 2 -- Free list as intrusive linked list**: The `next_free` field in vacant entries forms a singly-linked list. `free_head: Option<u32>` points to the first free slot. On `insert`, pop from head. On `remove`, push to head. Both are O(1).

**Hint 3 -- Generation overflow**: When the generation counter wraps around to 0, there is a theoretical risk of ABA. In practice, `u32` gives 4 billion generations per slot -- a slot would need to be reused 4 billion times for a collision. Document this as an accepted trade-off. For critical systems, use `u64` generations.

**Hint 4 -- Compaction strategy**: Walk the array from both ends. Move live entries from high indices to vacant slots at low indices. Update the free list to cover only the tail. Return old-to-new handle mappings so external code can update its references.

**Hint 5 -- Dense iteration**: The simplest approach scans all slots and skips vacant ones. For arenas with many gaps, this wastes time. An alternative is to maintain a separate dense array of live indices, but this adds bookkeeping overhead. For most use cases (< 90% vacancy), linear scanning is faster due to cache locality.

## Acceptance Criteria

- [ ] `insert` returns a handle that can be used with `get` to retrieve the value
- [ ] `remove` invalidates the handle: subsequent `get` with the same handle returns `None`
- [ ] A new `insert` after `remove` reuses the freed slot with an incremented generation
- [ ] The same index with different generations produces different handles that do not alias
- [ ] Free list correctly chains vacant slots: inserting N items into an arena with N free slots reuses all of them without growing
- [ ] `iter()` yields exactly the live entries with correct handles
- [ ] `compact()` moves all live entries to the front and returns correct remapping
- [ ] After compaction, old handles return `None` and new handles return the correct values
- [ ] `retain()` correctly removes entries and maintains free list integrity
- [ ] Arena with 10,000 inserts and 5,000 random removes: all remaining entries accessible, no false positives, no panics
- [ ] Handle implements `Eq + Hash` so it can be used as a `HashMap` key
- [ ] Memory usage is proportional to capacity (high-water mark), not cumulative inserts
- [ ] No undefined behavior: all `unsafe` blocks (if any) have documented safety invariants

## Key Concepts

**Generational indices vs raw pointers**: Raw pointers (`*const T`) are invalidated by reallocation and require unsafe access. Rust references (`&T`) have lifetime constraints that prevent storage in long-lived data structures. Generational indices combine the flexibility of indices (copyable, storable, no lifetime) with safety (stale access returns None instead of aliasing a wrong entity).

**The ABA problem in index reuse**: If entity A occupies slot 5, is removed, and entity B takes slot 5, any code holding "index 5" now refers to B instead of A. Without generations, this is undetectable. The name comes from lock-free programming: a CAS sees value A, another thread changes it to B then back to A, and the CAS succeeds incorrectly. Generations break this cycle because the slot is now at generation N+1.

**Free list recycling**: Instead of compacting the array on every removal, freed slots are chained into a linked list using intrusive pointers (the next-free index is stored in the slot itself). This gives O(1) allocation and deallocation with zero external memory overhead.

**Cache-friendly iteration**: A contiguous array of `Entry<T>` has better cache behavior than a linked list of heap-allocated nodes, even with gaps from removed entries. The CPU prefetcher handles sequential array access efficiently. Compaction further improves this by eliminating gaps, making iteration dense.

## Research Resources

- [Generational Indices (Catherine West, RustConf 2018)](https://kyren.github.io/2018/09/14/rustconf-talk.html) -- the foundational talk on using generational indices for game entity management in Rust
- [thunderdome: A generational arena crate](https://github.com/LPGhatguy/thunderdome) -- production-quality generational arena with excellent API design
- [slotmap: Slot map data structure](https://github.com/orlp/slotmap) -- another arena crate with secondary maps and dense storage options
- [The ABA Problem (Wikipedia)](https://en.wikipedia.org/wiki/ABA_problem) -- background on the fundamental problem generational indices solve
- [Data-Oriented Design (Richard Fabian)](https://www.dataorienteddesign.com/dodbook/) -- the design philosophy behind contiguous array storage for entity systems
- [ECS Architecture in Bevy](https://bevyengine.org/learn/quick-start/getting-started/ecs/) -- how a production game engine uses arena-like storage for entities
- [Rust Iterator trait documentation](https://doc.rust-lang.org/std/iter/trait.Iterator.html) -- implementing custom iterators
- [specs: Parallel ECS](https://github.com/amethyst/specs) -- another ECS using generational indices for entity storage
