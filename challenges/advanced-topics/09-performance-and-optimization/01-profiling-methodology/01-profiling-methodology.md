<!--
type: reference
difficulty: advanced
section: [09-performance-and-optimization]
concepts: [pprof, flamegraphs, perf, PMU-counters, sampling-profiler, differential-flamegraph, CPU-profiling, memory-profiling, goroutine-profiling]
languages: [go, rust]
estimated_reading_time: 75 min
bloom_level: evaluate
prerequisites: [go-http-server, goroutines, cargo-basics, linux-cli]
papers: [Brendan Gregg — The Flame Graph (ACMQ 2016)]
industry_use: [pprof, go tool pprof, perf, cargo-flamegraph, FlameScope, Brendan Gregg's FlameGraph scripts]
language_contrast: medium
-->

# Profiling Methodology

> You cannot optimize what you have not measured: profiling is the discipline of turning
> a vague complaint about slowness into a precise, falsifiable statement about where time
> goes.

## Mental Model

A profiler is an observer that periodically interrupts the running program (sampling
profiler) or records every entry and exit of instrumented functions (instrumentation
profiler). The interrupt-based sampling profiler is the workhorse for production systems:
it has predictable, low overhead (~1–3% CPU), can be enabled at runtime without
recompilation, and produces call stacks that aggregate into flamegraphs.

The key mental model: **time is attributed to call stacks, not individual functions**.
A function that appears wide on a flamegraph is either slow itself (deep, narrow children)
or called many times from many places (wide, shallow children). These require different
fixes. Wide-and-shallow means the algorithm calls it too often. Narrow-and-deep means the
function itself is the bottleneck.

The second mental model: **the profiling loop is a scientific method**. You form a
hypothesis ("the bottleneck is JSON serialization in the request handler"), you make one
change, you measure again with identical conditions, and you compare. Changing multiple
things between measurements makes it impossible to attribute improvements to causes.
The profiling loop is: measure baseline → read flamegraph → identify hottest path →
hypothesize root cause → change one thing → measure again → compare.

Sampling bias is the most common mistake. A sampling profiler will completely miss
functions that are called infrequently but take a long time (e.g., a cold startup path
that runs once). It will also attribute time to the wrong call site when Go inlines
aggressively. Verify surprising profiler output by adding explicit `time.Since` or
`std::time::Instant` measurements in the code path you suspect.

## Core Concepts

### CPU Profiling (Sampling)

A CPU profile answers: where is the CPU spending time? The Go runtime's CPU profiler
sends `SIGPROF` to the process at 100 Hz (by default), captures the goroutine stack, and
writes the sample to the profile buffer. The Rust equivalent uses Linux `perf` or
`cargo flamegraph`, both of which use the `perf_event_open` syscall to capture hardware
PMU (Performance Monitoring Unit) events — either cycles or on-CPU time.

The output is a weighted call graph. Each node's self-time is cycles spent in that
function's own instructions. Each node's cumulative time includes all children. Hot path
identification: find the widest path from root to a leaf with high self-time.

### Memory Profiling (Allocation)

A memory profile answers: what is allocating on the heap, and how much? Go's heap
profiler samples allocations (1 in every 512 KB by default, tunable via
`runtime.MemProfileRate`). It records the call stack at the time of allocation, the
number of objects, and bytes in-use vs. cumulative. The difference between in-use and
cumulative reveals allocation churn — high cumulative with low in-use means short-lived
objects that stress the GC.

In Rust, heap allocation profiling is done with `heaptrack` (Linux) or `Instruments`
(macOS). Since Rust programs control allocation explicitly, heap profiles are less
common — the more useful question is which allocations can be eliminated entirely.

### Block and Mutex Profiling (Contention)

Go's block profiler records goroutines blocked on channel operations and
`sync.Mutex`/`sync.RWMutex` waits. The mutex profiler specifically records contention
on mutexes. Both require explicit enablement at runtime:

```go
runtime.SetBlockProfileRate(1)   // record every blocking event
runtime.SetMutexProfileFraction(1) // record every mutex contention event
```

Use these to diagnose: "the service is slow under load but CPU utilization is low."
That is the signature of lock contention, not CPU bottleneck.

### Differential Flamegraphs

A differential flamegraph shows the difference between two profiles — before and after
a change. Red frames indicate more time spent in the new profile; blue indicates less.
This is the correct tool for confirming optimization results: you collect a profile
before the change, apply the change, collect a profile of identical workload, and
produce the diff. Without the diff, you are comparing flamegraphs by eye — unreliable.

### Hardware PMU Counters

Beyond CPU time, Linux `perf stat` can read hardware counters directly from the CPU's
Performance Monitoring Unit: instructions retired, cycles, cache misses (L1, L2, LLC),
branch mispredictions, TLB misses, and more. These counters reveal *why* a hot function
is slow: a function that is hot on CPU time but also has high L3 cache misses is
memory-bound, not compute-bound. The fix is data layout, not algorithmic.

```
perf stat -e cycles,instructions,cache-misses,cache-references ./your_binary
```

The ratio `instructions / cycles` is the IPC (instructions per cycle). A modern x86-64
CPU can retire 4 instructions per cycle. An IPC of 0.5 with high cache misses indicates
the CPU is stalling waiting for memory — the optimization target is data layout, not code.

## Implementation: Go

```go
package main

import (
	"log"
	"net/http"
	_ "net/http/pprof" // registers /debug/pprof handlers as a side effect
	"runtime"
	"runtime/pprof"
	"os"
	"time"
)

// ExampleServer demonstrates the two profiling approaches:
// 1. HTTP endpoint (for long-running servers) — prefer this for production
// 2. File-based (for benchmarks and short-lived programs)

func main() {
	// --- Approach 1: HTTP pprof endpoint (attach at any time without restart) ---
	//
	// Import _ "net/http/pprof" registers these handlers automatically:
	//   GET /debug/pprof/              — index of available profiles
	//   GET /debug/pprof/profile?seconds=30 — 30-second CPU profile
	//   GET /debug/pprof/heap          — heap allocation profile
	//   GET /debug/pprof/goroutine     — all goroutine stacks
	//   GET /debug/pprof/block         — goroutine blocking events
	//   GET /debug/pprof/mutex         — mutex contention
	//   GET /debug/pprof/trace?seconds=5 — execution trace (different from profile)
	//
	// Usage after starting server:
	//   go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30
	//   go tool pprof -http=:8080 http://localhost:6060/debug/pprof/heap

	// Enable block and mutex profiling (disabled by default — non-zero overhead)
	runtime.SetBlockProfileRate(1)
	runtime.SetMutexProfileFraction(1)

	go func() {
		log.Println("pprof server on :6060")
		log.Fatal(http.ListenAndServe(":6060", nil))
	}()

	// --- Approach 2: File-based CPU profile (for benchmarks or CLI tools) ---
	if err := writeCPUProfile("cpu.prof"); err != nil {
		log.Fatal(err)
	}
	defer writeHeapProfile("heap.prof")

	// Simulate work
	runWorkload()
}

func writeCPUProfile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	// pprof.StartCPUProfile sends SIGPROF at 100 Hz and records stacks.
	// The overhead is ~1-3% CPU.
	if err := pprof.StartCPUProfile(f); err != nil {
		f.Close()
		return err
	}
	// StopCPUProfile is deferred by the caller
	go func() {
		time.Sleep(30 * time.Second)
		pprof.StopCPUProfile()
		f.Close()
	}()
	return nil
}

func writeHeapProfile(path string) {
	f, err := os.Create(path)
	if err != nil {
		log.Printf("heap profile: %v", err)
		return
	}
	defer f.Close()
	// runtime.GC() forces a GC before profiling so in-use numbers are accurate.
	// Without this, the heap profile may show objects that are actually garbage.
	runtime.GC()
	if err := pprof.WriteHeapProfile(f); err != nil {
		log.Printf("heap profile write: %v", err)
	}
}

// runWorkload is a placeholder — replace with your application logic.
func runWorkload() {
	// Deliberate allocation churn to make a visible heap profile
	for i := 0; i < 1_000_000; i++ {
		_ = make([]byte, 256) // short-lived allocation
	}
}
```

### Reading a Flamegraph

```
# Collect a 30-second CPU profile from a running server
go tool pprof -http=:8080 http://localhost:6060/debug/pprof/profile?seconds=30

# Or from a file
go tool pprof -http=:8080 cpu.prof

# For differential flamegraphs (requires two profiles):
# Step 1: collect baseline.prof while running with old code
# Step 2: collect new.prof while running with new code (same workload, same duration)
go tool pprof -diff_base=baseline.prof -http=:8080 new.prof
```

In the flamegraph view:
- **X-axis**: percentage of sample time (width = time, not wall-clock order)
- **Y-axis**: call stack depth (root at bottom, leaves at top)
- **Wide frame near top**: this function is the bottleneck — most CPU time terminates here
- **Wide frame in middle with narrow children**: this function is called often but each
  call is cheap — reduce call frequency (algorithmic fix)

### Go-specific Considerations

**Inlining and profiling**: Go's inliner removes function call overhead but can make
flamegraphs confusing — an inlined function appears attributed to its caller. Build with
`-gcflags="-l"` to disable inlining during profiling if you need exact attribution.
Re-enable for production.

**GC interference**: A CPU profile taken during a GC pause will show `runtime.gcBgMarkWorker`
and related frames. This is expected. If GC takes >10% of CPU in your profiles, investigate
allocation rate with the heap profiler before optimizing application code.

**Goroutine profiles** (`/debug/pprof/goroutine`) are not sampling profiles — they capture
every goroutine's current stack at the moment of the request. This is the tool for
diagnosing goroutine leaks (monotonically growing goroutine count) and deadlocks (all
goroutines blocked on the same mutex or channel).

**`go test -bench` + pprof**: The most reproducible profiling setup is a benchmark:

```go
// Run benchmark and generate profile:
// go test -bench=BenchmarkHotPath -cpuprofile=cpu.prof -memprofile=heap.prof -benchtime=10s
func BenchmarkHotPath(b *testing.B) {
	// setup outside the loop
	data := generateTestData()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		processData(data)
	}
}
```

## Implementation: Rust

```rust
// Cargo.toml:
// [profile.release]
// debug = true        # keep debug symbols for profiling; stripped in final build
//
// [dev-dependencies]
// criterion = { version = "0.5", features = ["html_reports"] }

// cargo-flamegraph usage:
//   cargo install flamegraph
//   cargo flamegraph --bin my_binary -- --args
// On Linux this uses perf_event_open; on macOS it uses DTrace.
//
// For benchmarks:
//   cargo flamegraph --bench my_bench -- --bench

use std::time::Instant;

fn main() {
    // For short-lived profiling, wrap the section you care about in a timer
    // to confirm the flamegraph is covering the right time window.
    let start = Instant::now();
    run_workload();
    eprintln!("workload took: {:?}", start.elapsed());
}

fn run_workload() {
    // Allocation churn visible in heaptrack
    let mut v: Vec<Vec<u8>> = Vec::new();
    for _ in 0..1_000_000 {
        v.push(vec![0u8; 256]);
        if v.len() > 1000 {
            v.clear(); // short-lived — this shows up in heaptrack as churn
        }
    }
}

// Criterion benchmark with profiling integration:
// The profiler runs while criterion's measurement loop is executing.
// cargo flamegraph --bench my_bench produces a flamegraph of the bench loop.
#[cfg(test)]
mod benches {
    use criterion::{criterion_group, criterion_main, Criterion};

    fn bench_workload(c: &mut Criterion) {
        c.bench_function("run_workload", |b| {
            b.iter(|| {
                // criterion::black_box prevents the compiler from eliminating
                // the computation as dead code
                criterion::black_box(super::run_workload());
            })
        });
    }

    criterion_group!(benches, bench_workload);
    criterion_main!(benches);
}
```

### Using perf for Hardware PMU Counters

```bash
# Basic counter statistics
perf stat -e cycles,instructions,cache-misses,cache-references,branch-misses \
    ./target/release/my_binary

# Sample with call stacks, generate flamegraph
perf record -F 99 -g ./target/release/my_binary
perf script | stackcollapse-perf.pl | flamegraph.pl > flamegraph.svg

# Annotate hot instructions (requires debug symbols in release build)
perf annotate --symbol=hot_function
```

### Rust-specific Considerations

**Debug symbols in release builds**: Add `debug = true` to `[profile.release]` in
`Cargo.toml` during profiling sessions. This does not affect optimization level (`opt-level`)
but enables profiler symbol resolution. Strip before shipping: `cargo build --release`
with the default profile strips them.

**`cargo-flamegraph` vs `perf` directly**: `cargo flamegraph` is a convenient wrapper
that handles the `perf record` / `perf script` / stack collapse pipeline. Use it for
most cases. Use `perf` directly when you need hardware counter annotation or when
profiling a specific section of a long-running process.

**`criterion` and profiler integration**: Criterion's `iter_custom` allows injecting
custom timing, which can be combined with `pprof-rs` for in-process CPU profiling:

```rust
// Cargo.toml: pprof = { version = "0.13", features = ["flamegraph", "criterion"] }
use criterion::{Criterion, profiler::Profiler};
use pprof::criterion::{Output, PProfProfiler};

pub fn criterion_benchmark(c: &mut Criterion) {
    // PProfProfiler generates a flamegraph for the criterion measurement window
    let mut c = Criterion::default()
        .with_profiler(PProfProfiler::new(100, Output::Flamegraph(None)));
    // ...
}
```

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Built-in profiler | Yes — `net/http/pprof`, zero dependencies | No — external tools: `cargo-flamegraph`, `pprof-rs` |
| Runtime overhead | ~1–3% CPU for sampling at 100 Hz | Negligible (`perf` runs in kernel; no in-process overhead) |
| Memory profile | Heap profiler built into runtime | `heaptrack` (Linux), Instruments (macOS), `dhat` |
| GC visibility | GC frames appear in CPU profile automatically | N/A — no GC; allocator profiles via `dhat` or `heaptrack` |
| PMU counter access | Via `perf` externally; no stdlib API | Via `perf` externally; `perf-event` crate for in-process |
| Differential flamegraph | `go tool pprof -diff_base=` | Brendan Gregg's `difffolded.pl` from folded stacks |
| Inlining visibility | Inlined frames may be attributed to caller | Same; LLVM inlines aggressively in `--release` |
| Hot path accuracy | Block and mutex profiles are separate | All contention visible via `perf lock` or `mutrace` |
| Profile format | pprof protobuf (compatible with upstream `pprof`) | `perf.data` or `pprof` format via `pprof-rs` |

## Production War Stories

**Cloudflare: Go HTTP/2 handler bottleneck (2017)** — A CPU profile of Cloudflare's
Go-based TLS terminator showed 23% of CPU in `crypto/tls.(*halfConn).decrypt`. The
flamegraph revealed the inner hot path was a `copy()` call on every TLS record. Replacing
the in-place decryption buffer with a pooled `sync.Pool` reduced GC pressure by 40% and
overall CPU by 18%. The fix was found in one profiling session; the entire optimization
took 2 hours of engineering time.

**Prometheus: label allocation churn (2019)** — The Prometheus team found via heap
profiling that the label handling code created a new `map[string]string` for every metric
query. This produced 2 million allocations per second at scale, causing GC pauses of
20–40 ms. Switching to a sorted `[]label` slice with binary search for lookups eliminated
the map allocation entirely. P99 query latency dropped from 80 ms to 8 ms.

**Rust replacing Python in a data pipeline (Shopify, 2022)** — A data pipeline step
was rewritten from Python to Rust. Before profiling the Rust version, the authors assumed
the bottleneck would be I/O. A `cargo flamegraph` run revealed that 60% of CPU was in
`serde_json::from_str`, not I/O. Switching to `simd-json` (AVX2-accelerated JSON parsing)
brought parsing time down by 4x. The profiler prevented an unnecessary I/O optimization.

## Numbers That Matter

| Operation | Approximate Cycles | Approximate Latency (3 GHz CPU) |
|-----------|-------------------|--------------------------------|
| L1 cache hit | 4 cycles | ~1.3 ns |
| L2 cache hit | 12 cycles | ~4 ns |
| L3 cache hit | 40 cycles | ~13 ns |
| DRAM access | 200+ cycles | ~65 ns |
| Go GC STW pause (current) | — | ~0.1 ms (P99 in most workloads) |
| Go GC scan time per GB heap | — | ~1–2 ms |
| `pprof` sampling interrupt overhead | ~1–3% CPU | at 100 Hz sample rate |
| `perf record -F 99` overhead | ~1–2% CPU | at 99 Hz sample rate |

## Common Pitfalls

**Profiling in debug mode**: Both `go test` (without `-bench`) and `cargo build` (without
`--release`) produce unoptimized code. A CPU profile of debug code tells you nothing about
production performance. Always profile release/optimized builds.

**Sampling a cold workload**: If the profiled binary starts from cold cache, the first
few seconds of a CPU profile are dominated by page faults and loader overhead. Warm up
the workload for 10–30 seconds before collecting the profile, or use the `-seconds` flag
to push the profile window past the cold start.

**Confusing cumulative and self-time**: A function with 80% cumulative time but 1% self-time
is a passthrough — its children are the bottleneck, not itself. Optimization target is
the leaf with high self-time, not the root with high cumulative time.

**Block profile noise**: At `SetBlockProfileRate(1)`, every channel operation is recorded.
This includes scheduled sleeps and idle goroutines. The block profile is high-signal only
when you have identified a latency problem that cannot be explained by CPU usage. Filter
the output: look for call stacks involving `sync.Mutex.Lock` or `sync.RWMutex.Lock`, not
`time.Sleep` or `select {}`.

**Not establishing a baseline before optimizing**: "My change made it faster" is
unfalsifiable without a before-and-after benchmark on identical hardware with identical
workloads. Even thermal throttling between runs can produce a 5–10% difference. See
[Benchmarking and Statistical Rigor](../06-benchmarking-and-statistical-rigor/06-benchmarking-and-statistical-rigor.md).

## Exercises

**Exercise 1** (30 min): Instrument a Go HTTP server with the `net/http/pprof` handler.
Use `go tool pprof -http=:8080` to collect a 30-second CPU profile while running `ab`
or `hey` against it. Identify the three widest frames in the flamegraph and explain what
each represents.

**Exercise 2** (2–4h): Write a Go program that deliberately demonstrates three profiling
scenarios: (a) CPU-bound — a tight loop, visible as a wide leaf in the CPU flamegraph;
(b) allocation-bound — a function that creates many short-lived objects, visible in the
heap profile; (c) contention-bound — multiple goroutines fighting over a single mutex,
visible in the mutex profile. Confirm each scenario appears distinctly in the appropriate
profile type.

**Exercise 3** (4–8h): Take an existing service or benchmark and collect a full profiling
suite: CPU, heap, block, and mutex profiles. Write a one-page analysis identifying the
top bottleneck in each profile type and proposing a fix for each. For the top CPU
bottleneck, implement the fix and collect a differential flamegraph to confirm the
improvement.

**Exercise 4** (8–15h): Profile a Rust binary with both `cargo flamegraph` and `perf stat`.
Use `perf stat -e cache-misses,cache-references` to determine whether the hot path is
compute-bound or memory-bound. If memory-bound, apply a data layout change (see [CPU Cache
Optimization](../02-cpu-cache-optimization/02-cpu-cache-optimization.md)) and confirm the
cache miss rate drops in a follow-up `perf stat` run.

## Further Reading

### Foundational Papers

- Brendan Gregg — ["The Flame Graph"](https://dl.acm.org/doi/10.1145/2949064.2949114)
  (ACM Queue, 2016) — the definitive description of the flamegraph visualization, including
  how to read it correctly and what it cannot show

### Books

- Brendan Gregg — *Systems Performance: Enterprise and the Cloud* (2nd ed., 2020) —
  chapters 5–6 cover CPU profiling methodology and PMU counters in depth; the most
  thorough treatment of Linux `perf` available
- Brendan Gregg — *BPF Performance Tools* (2019) — eBPF-based profiling for production
  systems without sampling overhead

### Blog Posts

- [Profiling Go Programs](https://go.dev/blog/pprof) — Go official blog, the canonical
  introduction to `go tool pprof`
- [Brendan Gregg's Flame Graph page](https://www.brendangregg.com/flamegraphs.html) —
  all flamegraph tools, variants, and tutorials
- [Julia Evans: "How do profilers work?"](https://jvns.ca/blog/2017/12/17/how-do-ruby---python-profilers-work/)
  — demystifies sampling vs. instrumentation profilers
- [Amos Wenger: "cargo-flamegraph tutorial"](https://fasterthanli.me/articles/profiling-rust-with-flamegraph)

### Tools Documentation

- [`go tool pprof` reference](https://pkg.go.dev/runtime/pprof)
- [`perf` tutorial (Brendan Gregg)](https://www.brendangregg.com/perf.html)
- [`cargo-flamegraph`](https://github.com/flamegraph-rs/flamegraph)
- [`pprof-rs`](https://github.com/tikv/pprof-rs) — in-process CPU profiler for Rust
- [`heaptrack`](https://github.com/KDE/heaptrack) — heap allocation profiler for Linux
- [`dhat`](https://docs.rs/dhat) — Valgrind-based heap profiler for Rust
