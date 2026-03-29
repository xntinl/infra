<!-- difficulty: advanced -->
<!-- category: concurrency-patterns -->
<!-- languages: [rust] -->
<!-- concepts: [hazard-pointers, memory-reclamation, lock-free, atomic-operations, safe-memory-reclamation] -->
<!-- estimated_time: 10-16 hours -->
<!-- bloom_level: analyze, evaluate -->
<!-- prerequisites: [atomic-operations, memory-ordering-model, unsafe-rust, lock-free-data-structures, epoch-based-reclamation] -->

# Challenge 108: Hazard Pointer Reclamation

## Languages

Rust (stable, latest edition)

## Prerequisites

- Deep understanding of atomic operations (`AtomicPtr`, `compare_exchange`, `fence`) and all five memory ordering levels
- Experience implementing lock-free data structures (Treiber stack, Michael-Scott queue, or similar)
- Knowledge of the memory reclamation problem: why you cannot simply free nodes in lock-free structures
- Familiarity with epoch-based reclamation (crossbeam-epoch) to compare trade-offs
- Proficiency with `unsafe` Rust, raw pointer arithmetic, and manual memory management

## Learning Objectives

- **Implement** the hazard pointer protocol for safe memory reclamation in lock-free data structures
- **Analyze** the scan-and-reclaim algorithm: how protected pointers prevent premature deallocation
- **Evaluate** hazard pointers versus epoch-based reclamation in terms of memory overhead, throughput, and worst-case garbage bounds
- **Design** the integration between hazard pointers and a lock-free stack (or queue) so the data structure does not need to know the reclamation internals
- **Evaluate** memory ordering requirements for publishing hazard pointers, reading shared state, and reclaiming memory

## Background

Every lock-free data structure faces the same fundamental problem: when can you free a node that has been logically removed? In a garbage-collected language (Java, Go), the runtime handles it. In Rust or C++, you must solve it yourself. If you free a node while another thread still holds a raw pointer to it, that thread will read freed memory -- use-after-free, the most dangerous class of concurrency bug.

There are three main solutions: epoch-based reclamation (EBR), hazard pointers (HP), and reference counting. Each has different trade-offs:

**Epoch-based reclamation** (used by crossbeam-epoch) is fast but has unbounded worst-case garbage. If one thread enters a critical section and stalls (gets descheduled by the OS for seconds), no garbage can be freed across the entire system until that thread advances. This is usually acceptable but not always.

**Hazard pointers** (Maged Michael, 2004) provide bounded garbage. Each thread publishes the pointers it is currently accessing in a thread-local array (the "hazard pointers"). Before freeing a retired node, the reclaimer scans all threads' hazard pointer arrays. If any thread is protecting the node, the reclaimer defers it. If no thread is protecting it, the reclaimer frees it. The worst-case garbage is O(H * T) where H is the number of hazard pointers per thread and T is the number of threads -- bounded and predictable.

**The trade-off**: hazard pointers require a write (store) to the hazard array on every access to a shared node, plus a scan of all threads' arrays on reclamation. This makes reads slightly slower than EBR (where the read-side cost is a single epoch increment) but guarantees bounded memory. For systems where memory must be tightly controlled (embedded, real-time, memory-constrained), hazard pointers are the right choice.

The interaction with Rust is interesting. The hazard pointer protocol is inherently unsafe -- you are managing raw pointers and asserting that protected pointers will not be freed. But you can wrap it in a safe API: the user calls `protect(ptr)` to get a guard, accesses the data through the guard, and the guard unpublishes the hazard pointer on drop. This is analogous to `crossbeam-epoch`'s `pin()` returning a `Guard`.

## The Challenge

Implement a complete hazard pointer library and integrate it with a lock-free data structure.

**Part 1: Hazard Pointer Domain**. Build the core hazard pointer infrastructure:
- A global registry of thread-local hazard pointer arrays
- `protect(ptr)` to publish a pointer as hazard-protected, returning a guard
- `retire(ptr)` to mark a node as logically deleted and awaiting reclamation
- `scan_and_reclaim()` to check retired nodes against all hazard pointers and free unprotected ones

**Part 2: Lock-Free Stack with HP**. Integrate hazard pointers into a Treiber stack (or Michael-Scott queue). The data structure uses the HP domain to protect nodes during traversal and retires nodes after removal. This demonstrates the HP protocol in a real data structure, not just in isolation.

**Part 3: Comparison**. Benchmark the HP-protected stack against an epoch-based stack (using crossbeam-epoch) and measure: read/write throughput, memory overhead (peak garbage), and latency distribution. Then introduce a pathological scenario: one thread sleeps for 100ms during a critical section. Observe how EBR's garbage grows unbounded while HP's garbage stays within O(H * T).

The key implementation challenge is the memory ordering of hazard pointer publication. When a thread protects a pointer, it must ensure the store to the hazard array is visible to the reclaimer before the thread dereferences the pointer. This requires careful ordering: store the hazard pointer, fence, then re-read the shared pointer to verify it has not changed. If it changed, the protected pointer is stale and the process must restart. This double-check pattern (protect, fence, validate) is the core of the HP protocol and getting the ordering wrong causes use-after-free.

## Requirements

1. Implement `HazardDomain` as the global registry of hazard pointer arrays
2. Implement per-thread hazard pointer arrays with a configurable number of slots (typically 2-4 per thread)
3. Implement `protect<T>(ptr: *const T, slot: usize) -> HazardGuard<T>` that publishes the pointer and returns a guard
4. The protect protocol: store pointer to HP slot, `SeqCst` fence, re-read the source pointer, verify match. If mismatch, retry
5. `HazardGuard<T>` implements `Deref<Target = T>` and clears the HP slot on `Drop`
6. Implement `retire(ptr: *mut T)` that adds the pointer to a thread-local retired list
7. Implement `scan_and_reclaim()`: collect all hazard pointers from all threads, free retired nodes not in the hazard set
8. Trigger reclamation when the retired list exceeds a threshold (e.g., 2 * H * T)
9. Integrate HP into a `TreiberStack<T>` -- push/pop use `protect` for node access and `retire` for removed nodes
10. Implement `Drop` for `HazardDomain` that frees all retired nodes (shutdown cleanup)
11. Stress test: 8 threads, 500k push/pop operations, no use-after-free, no memory leaks
12. Element accounting test: multiset of popped values equals multiset of pushed values
13. Bounded garbage test: verify retired node count never exceeds O(H * T * batch_threshold)
14. Benchmark against crossbeam-epoch-based stack at 2, 8, 16 threads
15. Pathological test: one thread sleeps 100ms mid-operation, compare garbage accumulation HP vs EBR

## Hints

1. The hazard pointer array for each thread can be a fixed-size array of `AtomicPtr<()>` (type-erased). Use `null` to indicate an empty slot. When a thread first accesses the domain, it claims an array from a global list (or allocates one). Retiring threads return their arrays. The reclaimer iterates all arrays, collecting all non-null pointers into a `HashSet` for fast lookup.

2. The protect-fence-validate sequence is critical:
   ```
   loop {
       let ptr = source.load(Acquire);
       hp_slot.store(ptr, Release);
       fence(SeqCst);  // ensure HP store visible before re-read
       let ptr2 = source.load(Acquire);
       if ptr == ptr2 { return HazardGuard(ptr); }
       // ptr changed between reads -- another thread modified source.
       // Our HP protects a stale pointer. Retry.
   }
   ```
   Without the fence, the compiler or CPU may reorder the HP store after the second load, creating a window where the reclaimer does not see the protection.

3. The retired list should be thread-local (each thread retires its own nodes). Reclamation can also be thread-local: each thread scans hazard pointers and frees its own unprotected retired nodes. This avoids global synchronization during reclamation. Trigger reclamation when the retired list exceeds `R` nodes (a tunable threshold, typically `H * T * 2`).

4. For the Treiber stack integration: `pop` needs one hazard pointer slot (to protect the top node while reading its `next` field). `push` does not need HP (the new node is not yet shared). After a successful CAS in `pop`, the removed node is retired. The HP guard is dropped after extracting the value, which unpublishes the pointer.

5. For the benchmark comparison, use a wrapper trait so both the HP stack and the epoch stack implement the same interface. Run identical workloads against both. The pathological test pins one thread in a critical section (for EBR, hold the guard; for HP, just hold a hazard guard) and measures total memory of unreclaimed nodes. EBR cannot reclaim anything while the guard is held; HP only retains nodes protected by that specific guard.

## Key Concepts

- **Hazard Pointer**: A thread-local pointer that announces "I am currently accessing this memory address -- do not free it." The reclaimer must check all hazard pointers before freeing any retired node.

- **Protect-Fence-Validate**: The three-step protocol for safely accessing a shared pointer. Store the pointer as hazard-protected, issue a memory fence to ensure visibility, then re-read the shared pointer to confirm it has not changed. Without the fence, a reclaimer might not see the hazard pointer and free the node between steps 1 and 3.

- **Retired List**: A per-thread list of nodes that have been logically removed from the data structure but cannot yet be freed because some thread might still hold a reference. Periodically scanned against hazard pointers to determine which nodes are safe to free.

- **Bounded Garbage**: Unlike EBR, hazard pointers guarantee that the total number of unreclaimed nodes is at most O(H * T * R_threshold). This is a worst-case bound, not an amortized bound -- it holds even if threads stall.

- **Scan**: The reclamation operation. Collect all hazard pointers from all threads into a set. For each retired node, check if its address is in the set. If not, free it. If yes, keep it in the retired list.

## Acceptance Criteria

- [ ] `HazardDomain` manages per-thread hazard pointer arrays
- [ ] `protect` implements the protect-fence-validate protocol correctly
- [ ] `HazardGuard` provides safe access and clears the HP slot on drop
- [ ] `retire` adds nodes to the thread-local retired list
- [ ] `scan_and_reclaim` correctly identifies and frees unprotected retired nodes
- [ ] Treiber stack integrated with HP passes concurrent correctness tests
- [ ] Stress test: 8 threads, 500k ops, no use-after-free
- [ ] Element accounting: no lost or duplicated elements
- [ ] Bounded garbage: retired list size never exceeds the theoretical bound
- [ ] Benchmark comparison against epoch-based reclamation at multiple thread counts
- [ ] Pathological test demonstrates HP's bounded garbage vs EBR's unbounded accumulation
- [ ] All `unsafe` blocks have safety comments
- [ ] All tests pass with `cargo test`
- [ ] Stress tests pass reliably in release mode (run 50+ times)
- [ ] Code compiles with no warnings under `#[deny(unsafe_op_in_unsafe_fn)]`

## Starting Points

- **Maged Michael, "Hazard Pointers: Safe Memory Reclamation for Lock-Free Objects" (2004)**: The original paper. Defines the protocol, proves correctness, and benchmarks against other schemes. Read sections 1-4 before writing code.
- **Rust Atomics and Locks (Mara Bos), Chapter 6**: Discusses the memory reclamation problem and several solutions, including hazard pointers.
- **Folly HazPtr (Facebook)**: Facebook's production C++ hazard pointer library. Study the API design and the batched reclamation strategy.
- **haphazard crate**: A Rust hazard pointer library. Read the source to understand one approach, but implement your own.

## Research Resources

- [Michael, "Hazard Pointers" (IEEE TPDS, 2004)](https://ieeexplore.ieee.org/document/1291819) -- the original paper
- [Rust Atomics and Locks (Mara Bos)](https://marabos.nl/atomics/) -- Chapters 4-6 on atomics and reclamation
- [Folly HazPtr source](https://github.com/facebook/folly/blob/main/folly/synchronization/HazptrDomain.h) -- production C++ implementation
- [haphazard crate (Rust)](https://github.com/jonhoo/haphazard) -- Rust HP implementation by Jon Gjengset
- [Preshing: Memory Ordering at Compile Time](https://preshing.com/20120625/memory-ordering-at-compile-time/) -- understanding why fences are needed
- [Anderson et al.: "Universal Constructions for Large Objects" (2014)](https://dl.acm.org/doi/10.1145/2611462.2611482) -- broader context of safe memory reclamation
- [P2530R3: Hazard Pointers for C++](https://www.open-std.org/jtc1/sc22/wg21/docs/papers/2023/p2530r3.pdf) -- the C++ standard proposal, with precise definitions
