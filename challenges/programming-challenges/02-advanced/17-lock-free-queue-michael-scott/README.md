<!-- difficulty: advanced -->
<!-- category: data-structures -->
<!-- languages: [rust] -->
<!-- concepts: [lock-free, michael-scott-queue, cas, sentinel-node, epoch-reclamation, linearizability] -->
<!-- estimated_time: 10-14 hours -->
<!-- bloom_level: analyze, evaluate -->
<!-- prerequisites: [treiber-stack-or-equivalent, atomic-operations, memory-ordering, unsafe-rust, epoch-reclamation-concept] -->

# Challenge 17: Lock-Free Queue (Michael-Scott Algorithm)

## Languages

Rust (stable, latest edition)

## Prerequisites

- Completed or equivalent experience with Treiber's lock-free stack (Challenge 16)
- Deep understanding of atomic compare-and-swap and memory ordering
- Experience with epoch-based or hazard-pointer memory reclamation
- Comfort with `unsafe` Rust, raw pointers, and manual memory management
- Understanding of linearizability and concurrent correctness reasoning

## Learning Objectives

- **Implement** the Michael-Scott two-pointer lock-free FIFO queue algorithm
- **Analyze** the sentinel node pattern and its role in simplifying CAS operations on head and tail
- **Evaluate** the "helping" mechanism where enqueuers fix a lagging tail pointer
- **Design** an epoch-based memory reclamation strategy that prevents use-after-free
- **Compare** throughput against `crossbeam::queue::SegQueue` and `Mutex<VecDeque<T>>`

## Background

A lock-free queue is fundamentally harder than a lock-free stack. A stack has one access point (the top), so a single CAS suffices. A queue has two access points (head for dequeue, tail for enqueue) that must remain consistent relative to each other. If head advances past tail, or tail falls behind the actual last node, the structure is broken.

Michael and Scott's 1996 paper solved this with two key insights. First, use a sentinel (dummy) node so that the queue is never truly empty at the pointer level -- head and tail always point to a valid node, eliminating null-pointer special cases. Second, introduce a "helping" mechanism: if a thread detects that tail is lagging behind the actual last node, it advances tail on behalf of the stalled thread. This cooperative advancement is what makes the algorithm lock-free rather than just optimistic.

The algorithm has been the foundation of concurrent queues in Java's `ConcurrentLinkedQueue`, .NET's `ConcurrentQueue`, and numerous other runtime libraries. Understanding it deeply gives you the conceptual tools to analyze any lock-free linked structure.

## The Challenge

Implement the Michael-Scott lock-free FIFO queue. Unlike a stack (one pointer), the queue requires managing two atomic pointers (head and tail) that must remain consistent without locks. The enqueue operation requires two CAS steps that can independently succeed or fail, creating interleavings that a stack never encounters.

The algorithm uses a sentinel (dummy) node: the head always points to a sentinel whose `next` is the actual first element. This eliminates the empty-queue special case in dequeue. Enqueue appends to the node currently pointed to by tail, then swings the tail pointer forward. The critical subtlety: another thread may have appended a node but not yet updated tail. An enqueuer must detect this "lagging tail" and help advance it before retrying its own append.

Memory reclamation is harder than in a stack because both head and tail may reference a node being dequeued. A dequeuer removes the sentinel from the front, making the first real node the new sentinel. But if another thread's tail still points to the old sentinel, freeing it causes use-after-free. You must implement epoch-based reclamation (either from scratch or using `crossbeam-epoch`) to defer deallocation until it is safe.

Prove correctness by testing linearizability: record a concurrent history of enqueue/dequeue operations and verify that the results are consistent with some sequential FIFO execution. Per-producer ordering must be preserved -- if producer P enqueues A before B, every consumer must observe A before B.

The dequeue operation has a subtle correctness concern around value reading. You must read the value from `head.next` before CAS-ing head forward, because after the CAS succeeds, another thread might immediately dequeue the next element and trigger reclamation of the node you just read from. If you read the value after the CAS, the node's memory might already be reused. This read-before-CAS pattern appears simple but is the source of most bugs in Michael-Scott queue implementations.

This challenge builds directly on the Treiber stack (Challenge 16). If you have not implemented a single-pointer lock-free structure with epoch-based reclamation, complete that first. The two-pointer coordination here adds a qualitative increase in complexity that is difficult to debug without the single-pointer foundation.

## Requirements

1. Implement `MSQueue<T>` with `enqueue(&self, value: T)` and `dequeue(&self) -> Option<T>`
2. Use a sentinel node so that head and tail are never null at any point during execution
3. Implement the two-CAS enqueue: first CAS the tail node's `next` from null to the new node, then CAS the tail pointer to the new node
4. Implement the helping mechanism: if `tail.next` is not null, advance tail to `tail.next` before retrying your own enqueue
5. Implement dequeue with the swing-head-forward pattern, reading the value from `head.next` and CAS-ing head to `head.next`
6. After a successful dequeue, the old head (former sentinel) becomes garbage and must be retired through epoch-based reclamation
7. Implement epoch-based memory reclamation to defer node deallocation until no thread holds a reference (use `crossbeam-epoch` or implement a simplified scheme)
8. Choose and document memory ordering for every atomic operation, explaining what would break with a weaker ordering
9. Ensure `MSQueue<T>` is `Send + Sync` with correct bounds on `T`
10. Implement `is_empty()` with documentation about its racy nature
11. Implement `Drop` to drain all elements and free the remaining sentinel
12. Write a linearizability test: N producer threads enqueue values, M consumer threads dequeue, verify FIFO ordering is preserved per-producer
13. Write an element accounting test: verify the multiset of dequeued values equals the multiset of enqueued values (no lost or duplicated elements)
14. Benchmark against `crossbeam::queue::SegQueue` and `Mutex<VecDeque<T>>` at 1, 4, 8, and 16 threads
15. Document all `unsafe` blocks with safety invariants

## Hints

1. The sentinel simplifies everything. At initialization, create one dummy node with no value. Head and tail both point to it. An empty queue is: head and tail point to the same sentinel, and sentinel.next is null. Never let head or tail be null. This invariant eliminates the need for null checks in the CAS operations, which significantly simplifies the algorithm's control flow.

2. The enqueue algorithm has two CAS operations that can independently fail. First, try to CAS `tail.next` from null to the new node. If this fails, another enqueuer won. Second, CAS `tail` from the current tail to the new node. If this second CAS fails, it is fine -- either you or a helper already advanced it. The key insight: the first CAS is the linearization point (it makes the node visible). The second CAS is a housekeeping operation that can be performed by any thread.

3. The helping mechanism in enqueue: before attempting your own CAS on `tail.next`, check if `tail.next` is not null. If so, tail is lagging behind the actual last node -- CAS `tail` forward to `tail.next` and retry. This ensures progress even if the thread that appended a node stalls between its first CAS (linking the node) and its second CAS (advancing tail). Without helping, a stalled thread could prevent all other enqueuers from making progress indefinitely.

4. For epoch-based reclamation: maintain a global epoch counter and per-thread epoch records. When a thread enters a critical section, it publishes the current global epoch via `pin()`. A retired node is safe to free when the global epoch has advanced at least twice since retirement AND all threads have observed the new epoch. The `crossbeam-epoch` crate encapsulates this. Key detail: the guard returned by `pin()` must be dropped as soon as you no longer need it, because long-lived guards delay garbage collection for all threads.

5. For the linearizability test: assign each enqueue a unique ID composed of `(producer_id, sequence_number)`. For each producer, its values must be dequeued in FIFO order relative to that producer. Collect all dequeued values and verify three properties: no duplicates (every value dequeued exactly once), no missing values (every enqueued value is eventually dequeued), and per-producer ordering is preserved (sequence numbers are monotonically increasing within each producer's stream).

## Key Concepts

- **Sentinel Node**: A dummy node with no value that serves as the head of the queue. The actual first element is `sentinel.next`. This eliminates the special case where head and tail must both be updated for enqueue-into-empty and dequeue-to-empty operations. Without the sentinel, these operations require multi-pointer CAS, which is not available on most hardware.

- **Helping (Cooperative Advancement)**: When a thread detects that another thread's operation is incomplete (tail is lagging), it completes the operation on behalf of the stalled thread. This is the mechanism that provides the lock-free guarantee: even if a thread is suspended mid-operation, other threads can make progress by helping it finish.

- **Two-CAS Enqueue**: The enqueue linearization point is the CAS on `tail.next` (making the new node reachable). The CAS on `tail` itself is a performance optimization (keeping tail close to the actual end). If the second CAS fails, correctness is not affected -- the next operation will help advance tail.

- **Dequeue Read-Before-CAS**: The value must be read from `head.next` before CAS-ing head forward. After the CAS succeeds, the node pointed to by `head.next` might be immediately dequeued by another thread and its memory reclaimed. Reading after the CAS risks accessing freed memory.

## Acceptance Criteria

- [ ] `MSQueue<T>` implements lock-free `enqueue` and `dequeue` with FIFO semantics
- [ ] Sentinel node is always present; head and tail are never null
- [ ] Helping mechanism correctly advances a lagging tail pointer
- [ ] Epoch-based reclamation prevents use-after-free of dequeued nodes
- [ ] Every atomic operation has documented and correct memory ordering
- [ ] Queue is `Send + Sync` and works correctly from multiple threads
- [ ] All `unsafe` blocks have documented safety invariants
- [ ] Stress test with 16+ threads (8 producers, 8 consumers) passes reliably
- [ ] Linearizability test verifies FIFO ordering and no lost/duplicated elements
- [ ] Benchmarks compare throughput against `SegQueue` and `Mutex<VecDeque>` at multiple thread counts
- [ ] `Drop` drains all elements and frees the sentinel without leaking
- [ ] No memory leaks (validate with Miri if possible: `MIRIFLAGS="-Zmiri-disable-isolation" cargo +nightly miri test`)
- [ ] All tests pass with `cargo test`
- [ ] Code compiles with no warnings under `#[deny(unsafe_op_in_unsafe_fn)]`

## Starting Points

- **Michael & Scott's original paper (1996)**: Read Figures 1 and 2 carefully. The pseudocode maps almost directly to Rust code with `crossbeam-epoch`. Pay attention to the comments about when each CAS can fail and why that failure is benign.
- **Rust Atomics and Locks (Mara Bos), Chapter 6**: Covers building a concurrent queue. While not the exact Michael-Scott algorithm, the patterns (sentinel, two-pointer coordination) are the same.
- **crossbeam-epoch examples**: The `crossbeam-epoch` crate's documentation includes examples of building linked structures. Study how `Shared`, `Owned`, and `Guard` interact to understand the epoch lifecycle.
- **crossbeam SegQueue source**: `crossbeam-queue/src/seg_queue.rs` implements a segmented queue. Study how it amortizes allocation costs by batching nodes into arrays. This is the production-grade alternative you will benchmark against.

## Going Further

- Implement a bounded variant that blocks (or returns an error) when the queue is full. Study how this changes the helping mechanism -- a full queue means enqueue cannot always make progress, weakening the lock-free guarantee to lock-free-when-not-full.
- Add batch enqueue/dequeue methods that insert or remove N elements with fewer CAS operations. Measure the throughput improvement at various batch sizes and explain why batching reduces contention.
- Implement the LCRQ (Linked Concurrent Ring Queue) by Morrison and Afek (2013): a wait-free queue that achieves higher throughput by using CRQ (Concurrent Ring Queue) nodes linked in a list. Compare its performance and complexity against your Michael-Scott implementation.
- Profile with `perf stat` to measure CAS retry rates, L1/L2 cache miss rates, and branch mispredictions. Use this data to explain the non-linear throughput degradation as thread count increases.
- Implement a hazard pointer scheme instead of epoch-based reclamation. Compare the implementation complexity, per-operation overhead, and memory reclamation latency against the epoch approach. Hazard pointers provide bounded garbage accumulation while epochs can accumulate unbounded garbage if one thread stalls.

## Research Resources

- [Simple, Fast, and Practical Non-Blocking and Blocking Concurrent Queue Algorithms (Michael & Scott, 1996)](https://www.cs.rochester.edu/~scott/papers/1996_PODC_queues.pdf) -- the original paper, essential reading
- [Rust Atomics and Locks (Mara Bos)](https://marabos.nl/atomics/) -- the best Rust-specific reference for lock-free programming
- [Crossbeam Epoch documentation](https://docs.rs/crossbeam-epoch/latest/crossbeam_epoch/) -- production-grade epoch reclamation in Rust
- [Crossbeam SegQueue source](https://github.com/crossbeam-rs/crossbeam/tree/master/crossbeam-queue/src) -- a segmented queue to benchmark against
- [The Art of Multiprocessor Programming (Herlihy & Shavit), Chapter 10](https://cs.ipm.ac.ir/asoc2016/Resources/Theartofmultiprocessorprogramming.pdf) -- concurrent queues chapter with correctness proofs
- [Memory Ordering in Rust (nomicon)](https://doc.rust-lang.org/nomicon/atomics.html) -- Rust's atomic memory model
- [Hazard Pointers vs. Epoch-Based Reclamation (comparison)](https://concurrencyfreaks.blogspot.com/2017/08/why-is-memory-reclamation-so-important.html) -- trade-offs between reclamation schemes
