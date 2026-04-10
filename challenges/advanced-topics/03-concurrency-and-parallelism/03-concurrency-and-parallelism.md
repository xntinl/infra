# Concurrency and Parallelism — Reference Overview

> The difference between a program that works and one that is provably correct under the memory model is the distance between "it passed the race detector" and "I can reason about every possible interleaving." This section covers the latter.

## Why This Section Matters

Concurrency bugs are the hardest category of production incidents. They are non-deterministic, they disappear under observation (Heisenbugs), and they can lie dormant in a codebase for years before a specific CPU topology or OS scheduler decision surfaces them. The engineers who debug these incidents are not smarter than the ones who cannot — they have a working mental model of what the hardware and language runtime actually guarantee.

This section exists because most engineers who write concurrent code have an intuitive model that is subtly wrong. They believe that if a program passes a race detector, it is correct. They believe that `sync/atomic` operations in Go are "safe" without understanding which orderings are established. They use `Arc<Mutex<T>>` in Rust because "that's how you do it" without understanding why the compiler rejects their alternative and whether there is a faster one. They reach for a mutex when a lock-free structure would be three times faster — or worse, reach for a lock-free structure when contention on a single cache line makes it slower than a mutex.

The subtopics here span the full vertical: from the hardware memory model that your CPU actually implements, through the programming language abstractions that expose it, up to high-level parallel algorithm design that determines whether your 32-core machine finishes in 1/32 of the time or 1/4.

Go and Rust approach concurrency from opposite directions. Go's philosophy is "share by communicating" — channels establish happens-before relationships, and the race detector catches violations at runtime. Rust's philosophy is "the type system prevents data races" — `Send` and `Sync` traits, ownership, and `Ordering` semantics eliminate entire classes of bugs at compile time. Both languages have work-stealing schedulers, both target high-throughput concurrent systems, and both expose the same underlying hardware primitives. Understanding the contrast reveals which approach is better for which problem class.

## Subtopics

| # | Topic | Key Concepts | Est. Reading | Difficulty |
|---|-------|-------------|-------------|------------|
| 01 | [Memory Models and Happens-Before](./01-memory-models-and-happens-before/01-memory-models-and-happens-before.md) | Go memory model (2022), C++20 memory model, synchronized-before, DRF-SC, initialization ordering | 75 min | advanced |
| 02 | [Lock-Free Programming](./02-lock-free-programming/02-lock-free-programming.md) | CAS instruction, ABA problem, tagged pointers, epoch-based reclamation, hazard pointers | 80 min | advanced |
| 03 | [Wait-Free Algorithms](./03-wait-free-algorithms/03-wait-free-algorithms.md) | Wait-free vs lock-free, Kogan-Petrank queue, fetch-and-add counters, universal construction, JVM safepoints | 70 min | advanced |
| 04 | [Work-Stealing Scheduler](./04-work-stealing-scheduler/04-work-stealing-scheduler.md) | Chase-Lev deque, Go GMP model, Rayon, near-optimal parallel speedup, queue-per-P design | 75 min | advanced |
| 05 | [Software Transactional Memory](./05-software-transactional-memory/05-software-transactional-memory.md) | Optimistic concurrency, MVCC in-memory, read-set/write-set, commit validation, HTM (Intel TSX) | 80 min | advanced |
| 06 | [SIMD and Data Parallelism](./06-simd-and-data-parallelism/06-simd-and-data-parallelism.md) | Data parallelism vs task parallelism, auto-vectorization, `std::simd`, SSE/AVX, parallel scan | 70 min | advanced |
| 07 | [Async Runtimes Internals](./07-async-runtimes-internals/07-async-runtimes-internals.md) | Go GMP scheduler, goroutine parking, netpoller, Tokio future/waker model, io_uring | 90 min | advanced |
| 08 | [NUMA and Memory Topology](./08-numa-and-memory-topology/08-numa-and-memory-topology.md) | NUMA nodes, cross-socket latency, false sharing, NUMA-aware allocation, `mbind`, `hwloc` | 65 min | advanced |
| 09 | [Parallel Algorithms](./09-parallel-algorithms/09-parallel-algorithms.md) | Blelloch scan, parallel merge sort, work-span model, Amdahl's law, critical path analysis | 75 min | advanced |

## Concurrency Model Comparison: Go vs Rust

```
                      GO                              RUST
                      ──────────────────────────────────────
Philosophy        "Share by communicating"        "Fearless concurrency"
                  Channels establish              Ownership prevents
                  happens-before                  data races at compile time

Race detection    Runtime (go -race)              Compile-time (Send/Sync)
                  ~5-20x overhead                 Zero overhead at runtime

Memory model      Go memory model 2022            C++20 memory model
                  synchronized-before             sequentially consistent,
                  relation via channels,           acquire-release, relaxed
                  mutexes, once                   (explicit Ordering enum)

Scheduler         GMP: work-stealing,             OS threads + optional
                  preemptive (async-safe           Tokio/async-std
                  points), ~1µs goroutines        (cooperative, waker-based)

Lock-free         sync/atomic (limited            std::sync::atomic +
primitives        Ordering exposure)              crossbeam-epoch for EBR

Memory reclaim    GC (stop-the-world STW          Ownership + RAII +
                  reduced via concurrent          optional EBR/hazard ptrs
                  mark-and-sweep since 1.5)       (no GC pauses)

Channel           Built-in, first-class           crossbeam-channel or
                  (buffered + unbuffered)         std::sync::mpsc (limited)

Parallelism       GOMAXPROCS, manual              Rayon (data parallelism),
                  goroutine pools                 explicit thread pools
```

### When to choose Go
- Services with many concurrent I/O-bound operations (network proxies, API gateways)
- Teams where the cognitive cost of ownership rules creates velocity drag
- When the GC pauses (typically < 1ms in modern Go) are acceptable
- When channel-based pipeline architectures are a natural fit for the problem

### When to choose Rust
- Systems where GC pauses are unacceptable (real-time, game engines, low-latency trading)
- Lock-free data structures that require manual memory reclamation
- SIMD-intensive workloads where you need fine-grained control over vectorization
- Embedded or `no_std` environments
- When compile-time proof of absence of data races is more valuable than development speed

## Dependency Map

```
01 Memory Models ──────────────────────────────────────────────────────────►
                                                                All other subtopics
                                                                depend on this one

02 Lock-Free ─────────────────────────────────────────────────► 03 Wait-Free
                                                                 (wait-free is
                                                                  strictly stronger)

04 Work-Stealing ─────────────────────────────────────────────► 07 Async Runtimes
                                                                 (Tokio uses
                                                                  work-stealing)

09 Parallel Algorithms ──────────────────────────────────────── reads from
                                                                 04 + 06 (SIMD +
                                                                  scheduling)

08 NUMA ──────────────────────────────────────────────────────► 04 Work-Stealing
                                                                 (NUMA-aware pools)
```

**Recommended reading order:**

1. **01 — Memory Models** (required first; everything else uses happens-before reasoning)
2. **02 — Lock-Free** (CAS fundamentals used in all non-blocking structures)
3. **03 — Wait-Free** (progression from lock-free guarantees)
4. **07 — Async Runtimes** (Go and Rust schedulers explained; essential context)
5. **04 — Work-Stealing** (scheduler internals; reads naturally after 07)
6. **08 — NUMA** (hardware topology; relevant for both lock-free and scheduler design)
7. **05 — STM** (orthogonal; reads well after lock-free)
8. **06 — SIMD** (data parallelism; independent of the above)
9. **09 — Parallel Algorithms** (applies all prior knowledge)

## Time Investment

| Topic | Reading | Exercises (all 4) | Total |
|-------|---------|-------------------|-------|
| 01 — Memory Models | 75 min | 14–28 h | ~16 h |
| 02 — Lock-Free | 80 min | 18–33 h | ~20 h |
| 03 — Wait-Free | 70 min | 16–30 h | ~18 h |
| 04 — Work-Stealing | 75 min | 18–33 h | ~20 h |
| 05 — STM | 80 min | 18–33 h | ~20 h |
| 06 — SIMD | 70 min | 14–24 h | ~16 h |
| 07 — Async Runtimes | 90 min | 20–38 h | ~22 h |
| 08 — NUMA | 65 min | 14–24 h | ~16 h |
| 09 — Parallel Algorithms | 75 min | 18–33 h | ~20 h |
| **Section total** | **~11–12 h** | **~150–276 h** | **~168 h** |

- **Survey pass** (Mental Model + comparison tables only): ~8 h
- **Working knowledge** (full read + run both implementations): ~22 h
- **Mastery** (all exercises + further reading + build the stretch goals): ~168 h

## Prerequisites

Before entering this section you should be comfortable with:

- **Go fundamentals**: goroutines, `sync.Mutex`, `sync.WaitGroup`, `sync/atomic` (basic usage), channels (buffered and unbuffered), `context.Context`
- **Rust fundamentals**: ownership, borrowing, lifetimes, `Arc<Mutex<T>>`, `std::thread::spawn`, trait objects, why `Send` and `Sync` are marker traits
- **Systems knowledge**: what a cache line is (64 bytes on x86); what a CPU pipeline is; the difference between L1/L2/L3 cache and main memory latency; what a system call is and why it is expensive
- **Data structures**: linked lists (pointer-based), ring buffers, hash tables — you will implement concurrent variants of all three
- **Algorithms**: Big-O analysis including amortized complexity; why O(1) amortized differs from O(1) worst-case

If the lock-free challenge from the Rust section (`rust/04-insane/02-lock-free-data-structures/`) is unfamiliar, skim it before starting subtopic 02. It covers the same CAS fundamentals from a challenge perspective; this section covers them from a reference perspective.
