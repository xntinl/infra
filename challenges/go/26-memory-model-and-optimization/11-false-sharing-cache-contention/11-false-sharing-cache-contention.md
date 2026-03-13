# 11. False Sharing and Cache Contention

<!--
difficulty: insane
concepts: [false-sharing, cache-lines, cpu-cache-contention, padding, per-cpu-data-structures]
tools: [go]
estimated_time: 60m
bloom_level: evaluate
prerequisites: [struct-field-ordering-cache-lines, benchmarking-methodology, goroutines, sync-primitives]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of CPU cache lines (64 bytes)
- Experience with concurrent programming and benchmarking
- Completed the struct field ordering exercise

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** false sharing and why it destroys parallel scalability
- **Detect** false sharing through benchmarks and performance counters
- **Eliminate** false sharing with cache line padding
- **Design** per-CPU data structures that avoid contention

## The Challenge

False sharing occurs when goroutines on different CPU cores modify variables that live on the same cache line. Even though they access different variables, the CPU's cache coherence protocol (MESI) forces the cache line to bounce between cores, serializing what should be parallel work.

This problem is invisible to Go's profiler and race detector. It looks correct and race-free but performs dramatically worse than expected.

## Requirements

1. Create a multi-goroutine counter benchmark that suffers from false sharing
2. Demonstrate the performance degradation as core count increases
3. Fix the false sharing with cache line padding
4. Implement a per-CPU counter that scales linearly
5. Benchmark all approaches and explain the scaling behavior

## Hints

<details>
<summary>Hint 1: Creating False Sharing</summary>

Pack multiple counters into a struct or array where adjacent elements share a cache line:

```go
type Counters struct {
    c0 int64
    c1 int64
    c2 int64
    c3 int64
}
```

Since `int64` is 8 bytes, all four counters fit in a single 64-byte cache line.
</details>

<details>
<summary>Hint 2: Padding to Eliminate False Sharing</summary>

Add padding to force each counter onto its own cache line:

```go
type PaddedCounter struct {
    value int64
    _pad  [56]byte // pad to 64 bytes total
}
```

Or use `[8]int64` where only index 0 is used.
</details>

<details>
<summary>Hint 3: Per-CPU Design</summary>

Use `runtime.GOMAXPROCS(0)` to get the number of logical CPUs and create one counter per CPU. Use `atomic.AddInt64` for the per-CPU counter and sum all counters on read.
</details>

<details>
<summary>Hint 4: Measuring the Effect</summary>

Use sub-benchmarks with varying goroutine counts (`b.Run(fmt.Sprintf("goroutines-%d", n), ...)`) to show how false sharing prevents scaling.
</details>

## Success Criteria

- False-sharing benchmark shows performance that degrades or plateaus as goroutine count increases
- Padded benchmark shows near-linear scaling with goroutine count
- The padded struct is exactly 64 bytes per counter (verify with `unsafe.Sizeof`)
- Per-CPU counter achieves the best throughput in the multi-goroutine case
- Written explanation of why false sharing occurs at the hardware level

## Research Resources

- [Mechanical Sympathy: False Sharing](https://mechanical-sympathy.blogspot.com/2011/07/false-sharing.html)
- [Go struct alignment and padding](https://go.dev/ref/spec#Size_and_alignment_guarantees)
- [CPU Cache Architecture](https://en.wikipedia.org/wiki/CPU_cache)
- [MESI Protocol](https://en.wikipedia.org/wiki/MESI_protocol)
- [perf stat](https://perf.wiki.kernel.org/index.php/Tutorial) (Linux only) for measuring cache misses

## What's Next

With false sharing understood, the next exercise covers zero-allocation patterns for eliminating GC pressure entirely on hot paths.

## Summary

False sharing occurs when multiple CPU cores modify variables on the same cache line, causing the coherence protocol to serialize access. It is invisible to the race detector and profiler. Fix it by padding structs to cache line boundaries (64 bytes) or using per-CPU data structures. Always benchmark with increasing goroutine counts to detect scaling problems caused by cache contention.
