<!-- difficulty: advanced -->
<!-- category: concurrency-patterns -->
<!-- languages: [rust] -->
<!-- concepts: [rcu, epoch-reclamation, lock-free-reads, atomic-swap, grace-period] -->
<!-- estimated_time: 10-16 hours -->
<!-- bloom_level: analyze, evaluate -->
<!-- prerequisites: [atomic-operations, memory-ordering-model, unsafe-rust, arc-internals, epoch-based-reclamation] -->

# Challenge 92: Read-Copy-Update (RCU)

## Languages

Rust (stable, latest edition)

## Prerequisites

- Deep understanding of atomic operations (`AtomicPtr`, `compare_exchange`, `swap`) and memory ordering
- Experience with `unsafe` Rust, raw pointers, `Box::into_raw`, `Box::from_raw`
- Knowledge of epoch-based memory reclamation (how grace periods work, deferred deallocation)
- Familiarity with `Arc`, `RwLock`, and their performance characteristics under read-heavy workloads
- Understanding of concurrent data structure correctness (linearizability, quiescent consistency)

## Learning Objectives

- **Implement** RCU (Read-Copy-Update) allowing readers to access shared data without any locks or atomic instructions
- **Analyze** grace period tracking using epoch-based reclamation to determine when old data versions can be freed
- **Evaluate** the trade-off: RCU favors readers at the expense of writers, making it ideal for read-dominated workloads
- **Design** RCU-protected containers (HashMap, linked list) that expose safe reader and writer APIs
- **Compare** read throughput against `RwLock` and `Arc<Mutex>` to quantify the read-side advantage

## Background

RCU is the most important synchronization mechanism you have never heard of. It is the backbone of the Linux kernel's scalability -- used in routing tables, filesystem caches, module lists, and hundreds of other structures. The idea is radical: readers pay zero synchronization cost. No locks, no atomics, no barriers. Readers simply dereference a pointer and read the data. This makes read-side critical sections as fast as single-threaded code.

The trick is on the write side. When a writer wants to modify shared data, it does not mutate in place. Instead, it copies the current data, modifies the copy, and atomically swaps the pointer to the new version. Old readers still see the old version -- that is fine, they have a consistent snapshot. New readers see the new version. The writer must then wait for a grace period -- the time until all readers that could possibly hold a reference to the old version have finished -- before freeing the old data.

Grace period tracking is the hard part. In the Linux kernel, RCU leverages context switches: once every CPU has scheduled at least once, no reader can hold an old reference. In userspace Rust, you track grace periods with epochs. Each reader announces entry/exit from a read-side critical section by incrementing a per-thread epoch counter. The writer waits until all threads have advanced past the epoch at which the old data was retired.

This is fundamentally different from `RwLock`. With `RwLock`, readers still acquire a shared lock (an atomic increment), which causes cache-line bouncing on multi-core systems. With RCU, readers touch only thread-local state (or nothing at all). On a 64-core machine, the difference between one atomic per read (`RwLock`) and zero atomics per read (RCU) is dramatic.

The cost is paid by writers: they must allocate, copy, swap, and wait. RCU is not a general-purpose lock replacement -- it is specifically for read-dominated, infrequently-updated data. Configuration tables, routing rules, feature flags, cached metadata: these are RCU's domain.

## The Challenge

Implement a userspace RCU library in Rust. The library provides:

1. **`RcuCell<T>`**: A container holding a single `T` that can be read without locks and updated atomically. Readers call `read()` to get an epoch-protected reference to the current data. Writers call `update(new_value)` to swap in new data and schedule the old data for deferred deallocation.

2. **Epoch-based grace period tracking**: A global epoch counter and per-thread epoch announcements. Readers pin the current epoch on entry and unpin on exit. Writers advance the epoch and wait until all readers have unpinned from the old epoch before freeing retired data.

3. **`RcuHashMap<K, V>`**: An RCU-protected HashMap where reads are lock-free and writes copy-on-write the entire map (or use a more granular bucket-level strategy). Demonstrate the classic RCU use case: a lookup table updated rarely but read on every request.

4. **`RcuList<T>`**: An RCU-protected singly-linked list where readers traverse without locks and writers splice in/out nodes, deferring deallocation of removed nodes until the grace period expires.

The central tension is the gap between Rust's ownership model and RCU's design. RCU assumes readers can hold raw references to data that the writer wants to free. Rust does not allow this without `unsafe`. Your `read()` method must return a guard that keeps the reader pinned to the current epoch, preventing the writer from freeing the data until the guard is dropped. The guard's lifetime must be tied to the returned reference. This is where Rust's borrow checker works for you -- it prevents the user from keeping a reference past the guard's lifetime.

Every `unsafe` block must have a safety comment. The invariants are:
- A reader with an active guard sees data that will not be freed until the guard drops
- A writer does not free old data until all guards from the previous epoch are dropped
- The epoch advancement protocol ensures bounded garbage accumulation

## Requirements

1. Implement the epoch system: global epoch counter, per-thread epoch slots, pin/unpin operations
2. Implement `RcuCell<T>` with `read(&self) -> ReadGuard<T>` that returns a reference valid for the guard's lifetime
3. Implement `RcuCell<T>::update(&self, value: T)` that swaps in new data and defers deallocation of old data
4. `ReadGuard<T>` implements `Deref<Target = T>` and unpins the epoch on `Drop`
5. Grace period tracking: writers call a synchronize or defer function that waits until all pre-existing readers have finished
6. Implement `RcuHashMap<K, V>` with `get(key) -> Option<ReadGuard<V>>` and `insert(key, value)` / `remove(key)`
7. Implement `RcuList<T>` with `iter() -> RcuListIter<T>` for lock-free traversal and `push_front(value)` / `remove(predicate)`
8. `RcuCell<T>`, `RcuHashMap`, and `RcuList` must be `Send + Sync`
9. All `unsafe` blocks must have a safety comment documenting the invariant
10. Write a concurrent read-heavy benchmark: 15 reader threads, 1 writer thread. Measure read throughput and compare against `RwLock<T>` and `Arc<Mutex<T>>`
11. Write a correctness test: readers never see partially-updated or freed data, writer updates are eventually visible
12. Write a grace period test: verify that old data is freed after all readers from the old epoch unpin
13. Run stress test: 16 threads (14 readers, 2 writers), 1M operations, no use-after-free, no memory leaks

## Hints

1. Start with the simplest possible epoch scheme: one global `AtomicU64` epoch, and a thread-local (or per-reader) `AtomicU64` slot. `pin()` stores the current global epoch into the thread's slot. `unpin()` stores a sentinel (e.g., `u64::MAX`). The writer advances the global epoch and waits until no thread's slot holds an epoch older than the current. This is a blocking wait, which is acceptable for RCU (writers are expected to be slow).

2. For `RcuCell<T>`, store the data as `AtomicPtr<T>`. `read()` pins the epoch, loads the pointer, and returns a `ReadGuard` that holds the reference and unpins on drop. `update()` allocates a new `T`, swaps the pointer (with `swap` or `store`), and adds the old pointer to a retired list. A background task or the next `update` call scans the retired list and frees entries whose grace period has passed.

3. For `RcuHashMap`, the simplest approach is copy-on-write: `insert` clones the entire `HashMap`, modifies the clone, and swaps the pointer. This is expensive for large maps but simple and correct. A more advanced approach partitions the map into buckets, each protected separately. Start with full-copy and optimize only if benchmarks demand it.

4. For `RcuList`, insertion at the head is simple: create a new node pointing to the current head, swap the head pointer. Removal is trickier: to remove a middle node, the writer must update the predecessor's `next` pointer. Readers traversing the list may be at the removed node -- they can safely follow its `next` pointer because the node is not freed until the grace period ends. This is the classic RCU list removal pattern from the Linux kernel.

5. Thread-local epoch slots can be implemented with a global `Vec<AtomicU64>` where each thread claims a slot on first use (via `thread_local!` storing the index). Alternatively, use a linked list of thread registrations. The Linux kernel uses per-CPU slots; in userspace, per-thread slots are the analog.

## Key Concepts

- **Grace Period**: The interval during which all pre-existing readers finish their read-side critical sections. After a grace period, it is safe to free data that was removed before the period began. The duration is bounded by the longest read-side critical section.

- **Quiescent State**: A point at which a thread is guaranteed to not hold any RCU references. In the kernel, a context switch is a quiescent state. In userspace, unpinning the epoch is the explicit quiescent state.

- **Copy-on-Write**: Writers never mutate shared data in place. They copy, modify the copy, and swap the pointer. This is expensive (O(n) for a copy) but guarantees readers always see consistent data without synchronization.

- **Deferred Reclamation**: Old data is not freed immediately after the pointer swap. It is added to a retired list and freed later, after a grace period. This is the same concept as epoch-based reclamation in lock-free data structures -- RCU simply applies it to the data itself rather than to internal nodes.

## Acceptance Criteria

- [ ] Epoch system correctly tracks reader registration and grace periods
- [ ] `RcuCell<T>::read()` returns a reference that is valid for the guard's lifetime
- [ ] `RcuCell<T>::update()` atomically swaps data and defers old data deallocation
- [ ] Grace period test: old data freed only after all pre-existing readers unpin
- [ ] `RcuHashMap` supports concurrent reads and writes without reader-side locks
- [ ] `RcuList` supports lock-free traversal and safe node removal with deferred free
- [ ] All `unsafe` blocks have documented safety invariants
- [ ] Benchmark: RCU read throughput significantly exceeds `RwLock` at 8+ reader threads
- [ ] Stress test: 16 threads, 1M ops, no use-after-free, no memory leaks
- [ ] `Send + Sync` bounds are correct and enforced by the compiler
- [ ] All tests pass with `cargo test`
- [ ] Stress tests pass reliably in release mode (run 50+ times)
- [ ] Code compiles with no warnings under `#[deny(unsafe_op_in_unsafe_fn)]`

## Starting Points

- **Paul McKenney, "Is Parallel Programming Hard, And, If So, What Can You Do About It?" (perfbook)**: Chapters 9-10 are the definitive RCU reference, written by RCU's inventor. Available free at [kernel.org](https://mirrors.edge.kernel.org/pub/linux/kernel/people/paulmck/perfbook/perfbook.html)
- **Linux kernel RCU documentation**: `Documentation/RCU/` in the kernel source tree. Start with `whatisRCU.rst`
- **crossbeam-epoch**: Study how crossbeam implements epoch-based reclamation in userspace. Your RCU epoch system is conceptually similar
- **Rust Atomics and Locks (Mara Bos), Chapter 4-6**: Covers the atomic operations and memory ordering you will use extensively

## Research Resources

- [Paul McKenney's perfbook (free)](https://mirrors.edge.kernel.org/pub/linux/kernel/people/paulmck/perfbook/perfbook.html) -- chapters 9-10 on RCU
- [whatisRCU.rst (Linux kernel docs)](https://www.kernel.org/doc/Documentation/RCU/whatisRCU.rst) -- concise official explanation
- [McKenney, Slingwine: "Read-Copy Update: Using Execution History to Solve Concurrency Problems"](https://www.rdrop.com/users/paulmck/RCU/rclockpdcsproof.pdf) -- original RCU paper
- [crossbeam-epoch source](https://github.com/crossbeam-rs/crossbeam/tree/master/crossbeam-epoch) -- userspace epoch reclamation
- [Rust Atomics and Locks](https://marabos.nl/atomics/) -- essential for the atomic operations and memory model
- [Jon Gjengset: "RCU in Rust" (blog/talk)](https://www.youtube.com/watch?v=fSv_nmmFxGo) -- practical considerations for userspace RCU in Rust
