<!-- difficulty: advanced -->
<!-- category: data-structures -->
<!-- languages: [rust] -->
<!-- concepts: [lock-free, atomic-cas, memory-ordering, aba-problem, epoch-reclamation] -->
<!-- estimated_time: 8-12 hours -->
<!-- bloom_level: analyze, evaluate -->
<!-- prerequisites: [atomic-operations, memory-ordering-model, unsafe-rust, arc-internals] -->

# Challenge 16: Lock-Free Stack (Treiber's Algorithm)

## Languages

Rust (stable, latest edition)

## Prerequisites

- Solid understanding of atomic operations (`AtomicPtr`, `compare_exchange`)
- Knowledge of the C++11/Rust memory ordering model (Relaxed, Acquire, Release, SeqCst)
- Experience with `unsafe` Rust and raw pointer manipulation
- Familiarity with `Arc`, `Box::into_raw`, and `Box::from_raw`
- Understanding of concurrent data structure correctness properties (linearizability)

## Learning Objectives

- **Implement** Treiber's lock-free stack algorithm using atomic compare-and-swap
- **Analyze** the ABA problem and evaluate mitigation strategies (tagged pointers, epoch-based reclamation)
- **Evaluate** the correctness of memory ordering choices for each atomic operation
- **Design** stress tests that expose concurrency bugs under high contention
- **Compare** throughput and latency characteristics against a `Mutex`-based stack

## Background

Lock-free data structures guarantee system-wide progress: at least one thread completes its operation in a finite number of steps, regardless of what other threads do (stall, crash, get preempted). This is a stronger guarantee than mutex-based structures, where a thread holding a lock can block everyone else indefinitely.

The fundamental building block is the compare-and-swap (CAS) instruction: atomically read a memory location, compare it to an expected value, and write a new value only if the comparison succeeds. In Rust, this is `AtomicPtr::compare_exchange`. The entire Treiber stack algorithm reduces to two CAS loops -- one for push, one for pop.

However, lock-free programming in a language with manual memory management introduces a problem that garbage-collected languages avoid entirely: when can you free a node that was removed from the structure? In Java or Go, the garbage collector handles it. In Rust, you must solve it yourself. This is where the ABA problem and memory reclamation schemes become essential.

## The Challenge

Implement Treiber's stack -- one of the simplest and most foundational lock-free data structures. The algorithm uses a single atomic pointer to the top of a singly-linked list. `push` prepends a node by CAS-ing the top pointer, and `pop` removes the top node by CAS-ing it to the next node. When the CAS fails (another thread modified the top), the operation retries.

The simplicity is deceptive. The real challenge is memory safety. When thread A reads the top node and prepares to pop it, thread B might pop it and push a new node at the same address. Thread A's CAS succeeds because the pointer matches, but the node is different -- this is the ABA problem. Solving it requires either tagged pointers (incrementing a version counter packed into the pointer) or epoch-based memory reclamation (deferring deallocation until no thread holds a reference).

You must reason about every atomic operation's memory ordering. A wrong choice does not cause a compile error -- it causes data races that manifest as once-in-a-million corruption on x86 (which has a strong memory model) and routinely on ARM/RISC-V (which have weaker models). Your stress tests must be aggressive enough to catch these bugs, and you should understand why even passing stress tests do not prove correctness -- they only increase confidence.

This challenge is a stepping stone to the Michael-Scott queue (Challenge 17). Master the single-pointer CAS pattern here before tackling two-pointer coordination.

Rust adds a unique dimension to this problem. The borrow checker prevents the most common lock-free bugs (use-after-free, data races on non-atomic data) at compile time -- but only if you stay in safe Rust. Lock-free data structures require `unsafe` because you must manipulate raw pointers and make guarantees that the compiler cannot verify. The discipline is: use `unsafe` for the minimum necessary operations, and document the invariant that makes each use sound. Every `unsafe` block is a contract between you and the compiler -- break it, and the resulting bugs will be silent and intermittent.

## Requirements

1. Implement `TreiberStack<T>` with `push(&self, value: T)` and `pop(&self) -> Option<T>`
2. Use `AtomicPtr<Node<T>>` for the top pointer and `compare_exchange` for CAS operations
3. Choose and document the memory ordering for every atomic operation with a comment explaining why that specific ordering is necessary and what would break with a weaker ordering
4. Implement ABA mitigation using at least one strategy:
   - Tagged pointers: pack a monotonic counter with the pointer using `AtomicU128` or pointer tagging in the lower alignment bits
   - Epoch-based reclamation: implement a simplified epoch scheme, or use `crossbeam-epoch` and write a detailed explanation of how its guard/pin/defer model prevents use-after-free
5. Ensure the stack is `Send + Sync` with correct trait bounds on `T`
6. Implement `is_empty()` with a doc comment explaining why this method is inherently racy and should not be used for control flow in concurrent contexts
7. Implement `Drop` for `TreiberStack<T>` to drain remaining elements and prevent memory leaks
8. Write a stress test: 8+ threads performing random push/pop operations for at least 100k operations per thread, verifying no panics, no lost elements, no double-frees
9. Write an element accounting test: all threads push known values, all threads pop, verify the multiset of popped values equals the multiset of pushed values
10. Benchmark against `Mutex<Vec<T>>` using `criterion` at three contention levels: 2 threads (low), 8 threads (medium), 16 threads (high)
11. Document all `unsafe` blocks with a safety comment explaining which invariant justifies the unsafe operation

## Hints

1. Start with the basic algorithm ignoring memory reclamation. Use `Box::into_raw` to allocate nodes and `Box::from_raw` to reclaim them in `pop`. This has a use-after-free bug under concurrency -- but getting the CAS logic right first simplifies debugging. Once push and pop work correctly in single-threaded tests, add reclamation as a separate concern.

2. For the tagged pointer approach, you can pack a tag into the lower bits of a pointer if your nodes are aligned (which they are, since `Box` guarantees alignment to at least the size of the type). Alternatively, use a `(usize, usize)` pair stored in an `AtomicU128` or two adjacent `AtomicUsize` values with a seqlock pattern. The tag is a monotonically increasing counter that ensures no two CAS operations see the same (pointer, tag) pair even if the pointer is reused.

3. For epoch-based reclamation, the core idea is: each thread announces when it enters a critical section (reads shared state). Garbage is not freed until all threads have left the critical section. The simplest implementation uses three epochs and a global counter. `crossbeam-epoch` implements this with a `pin()` call that returns a `Guard`. As long as the guard is alive, no deferred destruction can run for the current epoch.

4. The ordering for `push` CAS is `Release` on success (publishing the new node so other threads see its contents) and `Relaxed` on failure (just retrying). The ordering for `pop` CAS is `Acquire` on success (reading the node's data that was written by the pushing thread) and `Relaxed` on failure. The initial load of the top pointer in both `push` and `pop` must be `Acquire` to see the data written by previous operations. Using `SeqCst` everywhere works but adds unnecessary overhead.

5. Your stress test should not just check for panics. Count every pushed element and every popped element. The multiset of popped values must equal the multiset of pushed values. Any discrepancy means lost or duplicated data. Use unique IDs (e.g., `thread_id * ops_per_thread + sequence`) so every value is distinct and verifiable. Run the test at least 50 times in release mode.

## Key Concepts

- **Compare-and-Swap (CAS)**: The atomic operation `compare_exchange(expected, desired)` reads the current value, compares it to `expected`, and writes `desired` only if they match. Returns `Ok(expected)` on success or `Err(actual)` on failure. The entire operation is atomic -- no other thread can intervene between the read and write.

- **ABA Problem**: Thread A reads pointer P. Thread B pops P, frees it, allocates a new node that happens to get the same address, and pushes it. Thread A's CAS on P succeeds because the address matches, but the node is different. Tagged pointers (version counters) or deferred reclamation (never reuse memory while readers exist) solve this.

- **Memory Ordering**: Atomic operations can be reordered by the compiler and CPU unless you specify ordering constraints. `Acquire` prevents reads from being moved before the atomic load. `Release` prevents writes from being moved after the atomic store. Together, they form a synchronization pair: a `Release` store is visible to a subsequent `Acquire` load on the same variable.

- **Epoch-Based Reclamation**: Threads announce when they enter and exit critical sections. Memory is freed only when all threads have exited. Three-epoch rotation ensures bounded garbage accumulation. `crossbeam-epoch` implements this as `pin()` (enter) and `Guard::drop()` (exit).

## Acceptance Criteria

- [ ] `TreiberStack<T>` implements lock-free `push` and `pop` using CAS
- [ ] Every atomic operation has documented and correct memory ordering
- [ ] ABA problem is addressed with tagged pointers or epoch-based reclamation
- [ ] Stack is `Send + Sync` and works correctly from multiple threads
- [ ] All `unsafe` blocks have documented safety invariants
- [ ] Stress test with 8+ threads passes reliably (run 100+ times)
- [ ] Element accounting test verifies zero lost or duplicated elements
- [ ] Benchmarks show throughput comparison against `Mutex<Vec<T>>` at multiple contention levels
- [ ] `Drop` implementation drains the stack completely
- [ ] No memory leaks (validate with `cargo test` under Miri if possible: `MIRIFLAGS="-Zmiri-disable-isolation" cargo +nightly miri test`)
- [ ] All tests pass with `cargo test`
- [ ] Code compiles with no warnings under `#[deny(unsafe_op_in_unsafe_fn)]`

## Starting Points

- **Mara Bos, "Rust Atomics and Locks", Chapters 4-6**: This covers AtomicPtr, compare_exchange, memory ordering, and builds a lock-free stack step by step. Read this before writing code.
- **crossbeam-epoch source code**: Study `crossbeam-epoch/src/internal.rs` to understand how pinning, guards, and deferred destruction work. Your solution can use crossbeam-epoch as a dependency, but you should understand the mechanism.
- **The Rustonomicon, atomics chapter**: Explains the Rust memory model's relationship to C++11 atomics and why each ordering level exists.
- **Treiber's original paper (1986)**: Short and readable. The pseudocode is in a C-like language. Map each operation to Rust's `AtomicPtr` API and identify where Rust's ownership model adds constraints that the original did not address.

## Going Further

- Implement the stack twice: once with `crossbeam-epoch` and once with a hand-rolled tagged pointer approach. Compare code complexity, performance, and ease of reasoning about correctness. Write a comparative analysis of the trade-offs.
- Add an `into_iter()` method that consumes the stack and yields all elements without atomic operations (since `into_iter` takes ownership, no concurrent access is possible).
- Implement an elimination backoff stack (Hendler, Shavit, Yerushalmi 2004): when push and pop operations collide under high contention, pair them together to cancel out without touching the shared top pointer. This transforms the worst case (high contention) into the best case.
- Run your implementation under Miri to detect undefined behavior (`MIRIFLAGS="-Zmiri-disable-isolation" cargo +nightly miri test`), then under ThreadSanitizer (`RUSTFLAGS="-Z sanitizer=thread" cargo +nightly test`). Document which classes of bugs each tool catches and which it misses.
- Implement a lock-free stack pool: pre-allocate a fixed number of nodes and recycle them instead of calling `Box::new`/`Box::from_raw` per operation. Measure the throughput improvement from eliminating allocator contention.

## Research Resources

- [Treiber's Stack (original IBM Research Report, 1986)](https://dominoweb.draco.res.ibm.com/58319a2ed2b1078985257003004617ef.html) -- the original algorithm
- [Rust Atomics and Locks (Mara Bos)](https://marabos.nl/atomics/) -- Chapters 4-6 cover exactly this territory
- [Crossbeam Epoch documentation](https://docs.rs/crossbeam-epoch/latest/crossbeam_epoch/) -- production epoch-based reclamation
- [The ABA Problem (Wikipedia)](https://en.wikipedia.org/wiki/ABA_problem) -- overview of the problem and solutions
- [Memory Ordering in Rust (nomicon)](https://doc.rust-lang.org/nomicon/atomics.html) -- the Rustonomicon chapter on atomics
- [Miri: An Interpreter for Rust's MIR](https://github.com/rust-lang/miri) -- detects undefined behavior in unsafe code
- [Preshing on Programming: Memory Ordering](https://preshing.com/20120913/acquire-and-release-semantics/) -- the clearest explanations of acquire/release semantics
