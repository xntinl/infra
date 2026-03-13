# 2. Lock-Free Data Structures

**Difficulty**: Insane

## The Challenge

Implement a lock-free multi-producer, single-consumer (MPSC) queue that is correct
under all possible thread interleavings. Then prove it.

Lock-free programming is where Rust's ownership model meets the bare metal of CPU
memory models. You will wrestle with atomic orderings that the compiler and CPU are
free to reorder, memory reclamation that cannot rely on garbage collection, and
correctness properties that are impossible to verify through normal testing alone.

Your queue must be linearizable: every operation must appear to take effect at a
single instant between its invocation and response. This is not a suggestion — it
is a formal property you will verify with the `loom` crate's exhaustive interleaving
explorer.

## Acceptance Criteria

- [ ] Implement a lock-free MPSC queue using linked list nodes and atomic `compare_exchange`
- [ ] Producers can enqueue concurrently from multiple threads without locks or mutexes
- [ ] Consumer can dequeue without blocking producers
- [ ] Every memory ordering annotation (`Relaxed`, `Acquire`, `Release`, `AcqRel`,
      `SeqCst`) is chosen deliberately — document WHY each one is correct
- [ ] Implement epoch-based reclamation (EBR) for safe deallocation of dequeued nodes
- [ ] No memory leaks under any interleaving — verify with Miri and Valgrind
- [ ] No ABA bugs — demonstrate understanding of the ABA problem by writing a test
      that would trigger it if your solution were naive, then show your solution prevents it
- [ ] Pass a `loom` test that explores all interleavings for a 2-producer, 1-consumer
      scenario with at least 3 enqueue/dequeue operations per thread
- [ ] Benchmark against `std::sync::mpsc` and `crossbeam-channel` with criterion;
      measure throughput at 1, 2, 4, 8 producer threads
- [ ] Queue must be `Send + Sync` where appropriate — the compiler must agree your
      types are thread-safe

## Starting Points

- **Michael-Scott Queue**: The foundational lock-free queue algorithm. Read the
  original paper: "Simple, Fast, and Practical Non-Blocking and Blocking Concurrent
  Queue Algorithms" (Michael & Scott, 1996). Your implementation will be a variant
  of this.
- **crossbeam-epoch** (`crossbeam-rs/crossbeam`): Study `crossbeam-epoch/src/internal.rs`
  and `crossbeam-epoch/src/collector.rs`. This is the production-quality EBR
  implementation in Rust. Understand the three-epoch rotation and how `Guard`
  prevents reclamation of in-use memory.
- **crossbeam-queue**: Study `crossbeam-queue/src/seg_queue.rs` for a lock-free
  unbounded queue. Note how it uses segments (arrays) rather than individual nodes
  to reduce allocation pressure and improve cache behavior.
- **loom** (`tokio-rs/loom`): Study `loom/src/rt/mod.rs` for how it intercepts
  atomic operations and systematically explores interleavings via DPOR (dynamic
  partial-order reduction). Your `loom` tests replace `std::sync::atomic` with
  `loom::sync::atomic`.
- **Rust Reference: Atomics** (`std::sync::atomic` docs): The ordering guarantees
  are defined in terms of the C++20 memory model. Read the `Ordering` enum docs
  carefully — Rust inherits the C++ model verbatim.

## Hints

1. The Michael-Scott queue uses a sentinel (dummy) node so that enqueue and dequeue
   operate on different nodes and do not contend on the same pointer. Without the
   sentinel, the empty-queue case requires both producer and consumer to CAS the
   same pointer — destroying your lock-freedom.

2. Memory ordering cheat sheet for queues: the producer's CAS that links a new node
   needs `Release` so the node's data is visible; the consumer's load of `next`
   needs `Acquire` to see that data. `SeqCst` everywhere "works" but is a crutch
   that hides bugs — use the weakest correct ordering and justify it.

3. The ABA problem: thread A reads pointer P (value X), gets preempted. Thread B
   dequeues X, frees it, allocates a new node that happens to get address X, enqueues
   it. Thread A wakes up, CAS succeeds (P is still X), but the node is different.
   Epoch-based reclamation solves this by deferring deallocation until no thread can
   hold a reference to the old node.

4. For `loom` testing: keep the test small. Loom explores exponentially many
   interleavings. Two producers each doing 2 operations, one consumer doing 4
   dequeues, is already a meaningful test. Use `loom::model(|| { ... })` and replace
   all atomics with `loom::sync::atomic::*`.

5. Distinguish between lock-free (some thread always makes progress) and wait-free
   (every thread makes progress in bounded steps). Your MPSC queue will be lock-free
   but not wait-free — the CAS loop can fail arbitrarily many times under contention.
   Understand why this is acceptable for most real-world systems.

## Going Further

- Implement a lock-free MPMC (multi-producer, multi-consumer) bounded ring buffer
  using an array and atomic indices. This is fundamentally harder than the linked-list
  approach — study Dmitry Vyukov's bounded MPMC queue design.
- Replace epoch-based reclamation with hazard pointers. Compare the tradeoffs:
  EBR has amortized overhead but can delay reclamation under long-lived guards;
  hazard pointers provide prompt reclamation but have per-access overhead.
- Implement a `loom`-verified lock-free stack (Treiber stack) as a warm-up.
- Port your queue to `no_std` for embedded use — you will need a custom allocator
  or a pre-allocated node pool.
- Write a formal proof sketch of linearizability for your queue. Identify the
  linearization point for each operation (the atomic instruction where the operation
  "takes effect").

## Resources

- [Michael & Scott, 1996: "Simple, Fast, and Practical Non-Blocking and Blocking
  Concurrent Queue Algorithms"](https://www.cs.rochester.edu/~scott/papers/1996_PODC_queues.pdf) —
  The foundational paper for lock-free queues
- [Herlihy & Shavit: "The Art of Multiprocessor Programming"](https://www.elsevier.com/books/the-art-of-multiprocessor-programming/herlihy/978-0-12-415950-1) —
  Chapters 10-11 cover lock-free linked lists and queues, Chapter 7 covers memory
  reclamation
- [crossbeam source](https://github.com/crossbeam-rs/crossbeam) —
  `crossbeam-epoch/` for EBR, `crossbeam-queue/` for lock-free queues
- [loom source](https://github.com/tokio-rs/loom) — Deterministic concurrency
  testing for Rust
- [Mara Bos: "Rust Atomics and Locks"](https://marabos.nl/atomics/) (O'Reilly, 2023) —
  The definitive Rust-specific book on atomics and concurrency primitives. Chapters
  on memory ordering are essential reading.
- [Jon Gjengset: "Implementing Lock-Free Queues"](https://www.youtube.com/watch?v=s19G6n0UjsM) —
  Crust of Rust stream covering crossbeam's approach
- [Rust Nomicon: Atomics](https://doc.rust-lang.org/nomicon/atomics.html) — Unsafe
  Rust's perspective on atomic operations
- [CppReference: Memory Order](https://en.cppreference.com/w/cpp/atomic/memory_order) —
  Rust uses the same model; this reference is more detailed than the Rust docs
- [Dmitry Vyukov: "Bounded MPMC Queue"](https://www.1024cores.net/home/lock-free-algorithms/queues/bounded-mpmc-queue) —
  For the Going Further stretch goal
