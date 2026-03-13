# 9. Memory Model and Atomics

**Difficulty**: Insane

## The Challenge

Build three concurrent data structures from scratch using only `std::sync::atomic` and `std::sync::atomic::fence` — no `Mutex`, no `RwLock`, no `crossbeam`. Each structure exercises a different subset of the memory ordering model:

### Structure 1: Spinlock

A mutual exclusion lock using `AtomicBool` and a compare-and-swap loop. Must provide a RAII guard. Must be correct on both x86 (TSO) and ARM (weak ordering). Must use the minimum ordering strength that preserves correctness — if you use `SeqCst` everywhere, you have not completed the exercise.

### Structure 2: Seqlock

A sequence lock optimized for read-heavy workloads on `Copy` types. Writers take exclusive access, increment a sequence counter, write data, increment again. Readers spin until they observe a consistent (even) sequence before and after reading. This requires careful use of `Acquire`/`Release` fences to prevent the compiler and CPU from reordering reads past the sequence check.

### Structure 3: Lock-Free MPSC Queue

A multi-producer, single-consumer queue based on a linked list. Producers push nodes using `compare_exchange` on the head pointer. The consumer pops from the tail. Memory reclamation is the hard part — you must handle it (epoch-based or hazard pointers, your choice).

For each structure, you must:
- Argue why your choice of memory ordering is correct (not just "it works on my machine")
- Write a test suite using the `loom` crate that explores possible thread interleavings
- Demonstrate a bug that would occur if you weakened an ordering by one level

## Acceptance Criteria

- [ ] `Spinlock<T>` passes `loom` tests with at least 2 threads contending
- [ ] `Spinlock` uses `Acquire` on lock, `Release` on unlock — not `SeqCst`
- [ ] `Spinlock` guard implements `Deref` and `DerefMut`; lock is released on drop
- [ ] `Seqlock<T: Copy>` allows concurrent reads while a write is in progress (reads retry, not block)
- [ ] `Seqlock` reader observes only values that were fully written (no torn reads)
- [ ] `Seqlock` uses `fence(Acquire)` and `fence(Release)` correctly — explain why atomic loads alone are insufficient
- [ ] `Seqlock` passes `loom` test with 1 writer + 2 readers
- [ ] MPSC queue is linearizable: items pushed by any producer are eventually observed by the consumer in per-producer FIFO order
- [ ] MPSC queue does not leak memory — reclamation strategy is implemented and tested
- [ ] MPSC queue passes `loom` test with 2 producers + 1 consumer
- [ ] Each structure includes a doc comment explaining the ordering choices with references to the happens-before relation
- [ ] Demonstrate at least one ordering bug per structure: show a `loom` test that fails when you weaken an ordering (e.g., `Release` to `Relaxed`)
- [ ] All structures are `Send + Sync` where appropriate

## Starting Points

- **Book**: Mara Bos — *Rust Atomics and Locks* (O'Reilly, 2023). Chapters 4-7 cover every ordering in detail with hardware context. This is your primary reference. Read it before writing a single line of code.
- **C++ standard**: Section [atomics.order] (N4860, 31.4) — Rust's model is intentionally identical. Understanding the C++ formalism helps you reason about happens-before.
- **Paper**: Hans-J. Boehm and Sarita V. Adve — "Foundations of the C++ Concurrency Memory Model" (PLDI 2008) — the theoretical basis.
- **Paper**: Herb Sutter — "atomic<> Weapons" (C++ and Beyond 2012 talk, two parts) — the most accessible explanation of why relaxed atomics exist.
- **Source**: `crossbeam-epoch/src/` — [github.com/crossbeam-rs/crossbeam](https://github.com/crossbeam-rs/crossbeam) — study epoch-based reclamation for the MPSC queue.
- **Source**: Linux kernel `include/linux/seqlock.h` — the canonical seqlock implementation (C, but the algorithm is what matters).

## Hints

1. **Spinlock ordering**: `Acquire` on the `compare_exchange` that acquires the lock, `Release` on the store that releases it. This creates a happens-before edge: everything the previous holder wrote before releasing is visible to the next acquirer. `Relaxed` on the failed `compare_exchange` is fine — a failed CAS does not establish synchronization. For the spin loop, consider `std::hint::spin_loop()` to emit a `PAUSE` instruction on x86.

2. **Seqlock fences**: The reader must do `load(Relaxed)` on the sequence, then `fence(Acquire)`, then read the data, then `fence(Acquire)`, then load sequence again. The fences prevent the CPU from speculating data reads before the first sequence check or after the second. An `Acquire` load on the sequence is **not** sufficient because `Acquire` only orders loads/stores **after** the atomic load — it does not prevent the data reads from being reordered **before** the sequence load on weakly-ordered hardware.

3. **MPSC push operation**: Use `compare_exchange` with `Release` ordering to swing the head pointer from the old head to the new node (whose `next` already points to old head). The `Release` ensures the new node's contents are visible before it appears in the list. The consumer uses `Acquire` on the head load.

4. **Loom**: Replace `std::sync::atomic::*` with `loom::sync::atomic::*` behind a `#[cfg(loom)]` feature flag. Loom exhaustively explores interleavings (within bounds). Keep your loom tests small — 2-3 threads, 2-3 operations each. Loom's state space grows exponentially.

5. **The torn read problem in seqlocks**: On a 64-bit system, writing a 128-bit struct requires two stores. Without fences, a reader can observe half the old value and half the new one. The seqlock's sequence counter detects this — but only if the ordering prevents the compiler from reordering the data reads outside the sequence check window. This is the essential subtlety.

## Going Further

- Implement a lock-free MPMC (multi-producer, multi-consumer) bounded ring buffer using `AtomicUsize` head and tail pointers.
- Port your spinlock to `no_std` — it should work on bare metal with no allocator.
- Benchmark your seqlock against `RwLock` for a read-heavy workload (99% reads, 1% writes). Measure on both x86 and ARM (or use QEMU for ARM).
- Implement backoff strategies for the spinlock: exponential backoff, `thread::yield_now()`, and parking. Measure contention behavior.
- Read about `compiler_fence` vs `fence` — implement a case where `compiler_fence` is sufficient and `fence` is overkill, and explain why (single-core embedded context).

## Resources

- **Book**: Mara Bos — *Rust Atomics and Locks* (O'Reilly, 2023) — [marabos.nl/atomics](https://marabos.nl/atomics/)
- **Paper**: Boehm, Adve — "Foundations of the C++ Concurrency Memory Model" (PLDI 2008)
- **Talk**: Herb Sutter — "atomic<> Weapons" (C++ and Beyond 2012) — [youtube search: Herb Sutter atomic weapons]
- **Crate**: `loom` — [github.com/tokio-rs/loom](https://github.com/tokio-rs/loom)
- **Source**: `crossbeam-epoch` — [github.com/crossbeam-rs/crossbeam/tree/master/crossbeam-epoch](https://github.com/crossbeam-rs/crossbeam/tree/master/crossbeam-epoch)
- **Source**: Linux kernel `include/linux/seqlock.h` — [elixir.bootlin.com](https://elixir.bootlin.com/linux/latest/source/include/linux/seqlock.h)
- **Docs**: `std::sync::atomic::Ordering` — read the stdlib docs, they are precise
- **Blog**: Preshing on Programming — "Acquire and Release Semantics", "Memory Barriers Are Like Source Control Operations" — [preshing.com](https://preshing.com)
- **Blog**: Ralf Jung — "Relaxed Memory Concurrency is Not a Dirty Trick" and other posts on the Rust memory model at [ralfj.de](https://www.ralfj.de/blog/)
- **Reference**: Intel 64 Architecture Memory Ordering White Paper — for x86-TSO specifics
- **Reference**: ARM Architecture Reference Manual — for weak ordering examples
