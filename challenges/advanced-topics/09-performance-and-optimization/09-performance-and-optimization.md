# Performance Engineering and Optimization — Reference Overview

> The fastest code is the code that lets the hardware do what it was designed to do. Every
> optimization technique in this section is ultimately about removing barriers between your
> intent and the CPU's ability to execute it.

## Why This Section Matters

Performance engineering is the discipline of making systems faster through measurement,
modeling, and systematic change. It is distinct from premature optimization — the ad hoc
guessing that makes code harder to read and slower to change — and from the folklore of
"fast tricks" that haven't been benchmarked against a modern compiler and a modern CPU.

The engineers who build high-performance systems — the ones maintaining ClickHouse's
vectorized query engine, Cloudflare's edge networking stack, or the Rust standard library's
sort implementation — share a common foundation: they know the hardware constraints first,
they measure before they change, and they understand why each optimization works at the
machine level.

This section provides that foundation. It covers the complete optimization workflow from
profiling methodology through CPU cache effects, memory bandwidth, SIMD vectorization,
latency/throughput tradeoffs, and the statistical rigor required to distinguish real
improvement from measurement noise. Both Go and Rust are treated as first-class languages
throughout — not because they are interchangeable (they are not), but because the
constraints are identical and the comparison illuminates both.

Go's garbage collector is a real performance boundary that experienced Go engineers navigate
rather than fight. Rust's zero-cost abstractions and LLVM backend provide a higher
optimization ceiling, but that ceiling requires understanding what the compiler can and
cannot do on its own. Both languages reward developers who understand the hardware model
underneath the runtime.

---

## Subtopics

| # | Topic | Key Concepts | Reading Time | Difficulty |
|---|-------|-------------|-------------|-----------|
| 01 | [Profiling Methodology](./01-profiling-methodology/01-profiling-methodology.md) | pprof, flamegraphs, perf PMU counters, differential profiles, sampling bias | 75 min | Advanced |
| 02 | [CPU Cache Optimization](./02-cpu-cache-optimization/02-cpu-cache-optimization.md) | cache lines, false sharing, AoS vs SoA, struct padding, data-oriented design | 80 min | Advanced |
| 03 | [Memory Bandwidth Optimization](./03-memory-bandwidth-optimization/03-memory-bandwidth-optimization.md) | DRAM bandwidth limits, prefetching, sequential vs random access, NUMA | 70 min | Advanced |
| 04 | [SIMD Optimization](./04-simd-optimization/04-simd-optimization.md) | auto-vectorization, portable SIMD, AVX2 intrinsics, vectorization barriers | 90 min | Expert |
| 05 | [Latency vs Throughput Tradeoffs](./05-latency-vs-throughput-tradeoffs/05-latency-vs-throughput-tradeoffs.md) | Little's Law, M/M/1 queues, batching, Nagle's algorithm, mechanical sympathy | 75 min | Advanced |
| 06 | [Benchmarking and Statistical Rigor](./06-benchmarking-and-statistical-rigor/06-benchmarking-and-statistical-rigor.md) | testing.B, benchstat, compiler elision, noise sources, CPU pinning | 70 min | Advanced |
| 07 | [Compiler Optimization Flags](./07-compiler-optimization-flags/07-compiler-optimization-flags.md) | PGO, LTO, BOLT, inliner budgets, devirtualization, monomorphization | 75 min | Advanced |

---

## Optimization Hierarchy

Follow this order. Violating it wastes time and produces fragile systems.

```
1. Measure first
   ↳ Profile with pprof / perf / cargo-flamegraph
   ↳ Identify the actual hot path — it is almost never where you expect
   ↳ Establish a reproducible benchmark BEFORE changing anything

2. Algorithm and data structure (10x–1000x potential gain)
   ↳ Replacing O(n²) with O(n log n) beats any micro-optimization
   ↳ Better data structure = fewer instructions + better cache behavior

3. Data layout (2x–10x potential gain)
   ↳ AoS → SoA for SIMD-friendly access
   ↳ Struct field reordering to eliminate padding
   ↳ Cache-line alignment for hot shared data

4. Memory access patterns (2x–5x potential gain)
   ↳ Sequential > random for hardware prefetcher
   ↳ Eliminate false sharing in concurrent paths
   ↳ NUMA-aware allocation for large multi-socket workloads

5. SIMD and vectorization (4x–8x potential gain for eligible loops)
   ↳ Enable auto-vectorization first (profile to confirm it fires)
   ↳ Portable SIMD (std::simd in Rust, assembly in Go) next
   ↳ Hand-rolled AVX2/AVX-512 intrinsics last

6. Compiler hints (1.1x–2x potential gain)
   ↳ Profile-Guided Optimization for branch prediction
   ↳ LTO for cross-crate inlining
   ↳ Inline annotations for hot paths the compiler misses

7. Micro-optimization (last, smallest gains)
   ↳ Branch elimination
   ↳ Loop unrolling
   ↳ Instruction scheduling
```

The hierarchy is not a waterfall — you iterate within it. But jumping to step 7 before
step 2 is how engineers spend a week saving 3 ns on a hot path that gets called twice a
second.

---

## Dependency Map

```
Profiling Methodology ──────────────────────────► All other subtopics
  (you need to measure before you can optimize anything)

CPU Cache Optimization ─────────────────────────► Memory Bandwidth Optimization
  (cache behavior drives bandwidth behavior)        (bandwidth is what happens when
                                                     you exhaust the cache)

CPU Cache Optimization ─────────────────────────► SIMD Optimization
  (SIMD only matters if data is in cache)

Benchmarking and Statistical Rigor ─────────────► All other subtopics
  (you cannot confirm any optimization without valid benchmarks)

Latency vs Throughput Tradeoffs ────────────────► (standalone; applies to all layers)

Compiler Optimization Flags ────────────────────► (applies after all code-level
                                                     optimizations are in place)
```

**Recommended read order for a first pass:**

1. Profiling Methodology — the foundation; nothing else matters without measurement
2. Benchmarking and Statistical Rigor — understand how to confirm what you measure
3. CPU Cache Optimization — the single highest-impact hardware insight
4. Memory Bandwidth Optimization — what happens after cache runs out
5. Latency vs Throughput Tradeoffs — system-level reasoning about performance goals
6. SIMD Optimization — compute-bound optimization for eligible workloads
7. Compiler Optimization Flags — squeeze the last gains from mature, measured code

---

## Time Investment

- **Survey** (Mental Model + Go vs Rust comparison for all 7 subtopics): ~7h
- **Working knowledge** (read fully + run both implementations): ~18h
- **Mastery** (all exercises + further reading per subtopic): ~80–110h

---

## Prerequisites

Before starting this section you should be comfortable with:

- **Hardware basics**: What a CPU pipeline is; the memory hierarchy (L1/L2/L3 cache,
  DRAM); what a cache line is (64 bytes on x86-64); the difference between latency and
  throughput at the instruction level
- **Go**: goroutines and the scheduler; `sync/atomic`; `testing.B`; escape analysis and
  the difference between stack and heap allocation; basic `pprof` usage
- **Rust**: ownership and borrowing; the difference between `Box<T>` and stack allocation;
  `unsafe` blocks and why they exist; the `criterion` crate for benchmarking; basic LLVM
  IR understanding is helpful but not required
- **Systems**: Process virtual memory; the difference between user-space and kernel-space
  profiling; what `perf stat` shows; how CPU frequency scaling affects benchmarks
- **Recommended prior sections**: Lock-Free Data Structures (covers `sync/atomic` and
  memory ordering, which are prerequisite for the false-sharing examples in CPU Cache
  Optimization) — [Rust: Lock-Free Data Structures](../../rust/04-insane/02-lock-free-data-structures/02-lock-free-data-structures.md)
