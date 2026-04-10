<!--
type: reference
difficulty: advanced
section: [09-performance-and-optimization]
concepts: [testing.B, benchstat, compiler-elision, blackbox, noise-sources, CPU-pinning, thermal-throttling, ASLR, statistical-significance, geometric-mean, t-test, criterion-rs]
languages: [go, rust]
estimated_reading_time: 70 min
bloom_level: evaluate
prerequisites: [go-testing, cargo-test, statistics-basics, linux-cli]
papers: [Mytkowicz et al. — Producing Wrong Data Without Doing Anything Obviously Wrong (ASPLOS 2009)]
industry_use: [Go benchstat, Rust criterion.rs, Google Perfkit Benchmarker, wrk2, Netflix Hollow benchmarking]
language_contrast: high
-->

# Benchmarking and Statistical Rigor

> A benchmark that measures the wrong thing with high precision is worse than no benchmark:
> it gives false confidence and trains your intuition on noise rather than signal.

## Mental Model

Microbenchmarks are controlled experiments. Like any experiment, they can have
confounding variables that corrupt the result. The most common confounders in CPU
benchmarking are: (1) the compiler eliminating the work you're measuring, (2) thermal
throttling changing CPU frequency between runs, (3) branch predictor and cache state
carrying over between runs, and (4) insufficient samples to distinguish signal from
measurement noise.

The statistical framing: you have a before-distribution and an after-distribution of
execution times. You want to know if they are different, and if so, by how much. A single
measurement tells you nothing — it's one sample from a distribution. Ten measurements
tell you slightly more. The `benchstat` tool and criterion.rs automate the statistical
analysis: they compute the geometric mean (not arithmetic mean — time ratios are
multiplicative), the confidence interval, and whether the difference is statistically
significant (p-value < 0.05 by default).

The key insight: **a benchmark showing 5% improvement is meaningless unless the noise
floor is below 2%**. Hardware noise (thermal throttling, CPU frequency scaling, ASLR,
shared cache state) routinely produces 5–15% variation between runs on uncontrolled
hardware. Reducing noise to <2% requires: CPU frequency pinning, CPU affinity, disabling
simultaneous multi-threading, and running enough iterations for the law of large numbers
to suppress noise.

The second key insight: **microbenchmarks measure the wrong thing**. A function that is
10% faster in isolation may produce no measurable improvement in a production workload
because: the function is not on the critical path, the bottleneck moved elsewhere, or
cache/branch predictor state in the benchmark differs from production. Always profile
the production workload to identify the actual bottleneck before benchmarking optimizations.

## Core Concepts

### Compiler Elision

The compiler can eliminate benchmark work entirely if it can prove the result is unused.
Go and Rust both perform this optimization:

```go
// WRONG: the compiler may eliminate the loop entirely
for i := 0; i < b.N; i++ {
    result := expensiveFunction(data)
    _ = result // STILL may be eliminated — underscore assignment is a hint, not a guarantee
}

// CORRECT: use runtime.KeepAlive or return from the benchmark
var sink int
for i := 0; i < b.N; i++ {
    sink = expensiveFunction(data)
}
runtime.KeepAlive(sink)
```

In Rust, criterion's `black_box` is the standard solution — it inserts a memory barrier
that prevents the compiler from reasoning about the value's usage:

```rust
b.iter(|| {
    criterion::black_box(expensive_function(&data))
});
```

`black_box` is not magic — it is an optimization fence. The function still runs, but the
compiler cannot assume the result goes nowhere and eliminate it. Use it for return values.

### Loop Overhead in testing.B

Go's `testing.B` runs the benchmark loop `b.N` times. The runtime starts with N=1 and
increases N until the benchmark runs for at least 1 second (configurable with `-benchtime`).
The reported `ns/op` is `total_time / N`.

Overhead sources:
- **Function call overhead**: For functions taking <10 ns, the loop overhead itself (a few
  ns) represents a significant fraction of the measurement. Solution: call the function
  multiple times inside the loop and divide by the count.
- **Setup cost in the loop**: If setup runs every iteration, the benchmark measures setup,
  not the function under test. Solution: put setup before `b.ResetTimer()`.
- **Allocation counting**: `-benchmem` reports allocations per operation. A benchmark
  showing unexpected allocations has a problem in the code under test, not the benchmark.

### Noise Sources

| Noise Source | Magnitude | Mitigation |
|---|---|---|
| Thermal throttling | 5–30% | Cool down between runs, use thermal management |
| CPU frequency scaling | 5–20% | `cpupower frequency-set --governor performance` |
| Background processes | 2–15% | `nice -n -20` or dedicated benchmark machine |
| ASLR (address randomization) | 1–5% | `echo 0 > /proc/sys/kernel/randomize_va_space` (test machines only) |
| SMT/Hyperthreading | 3–10% | `echo off > /sys/devices/system/cpu/smt/control` |
| CPU cache state | 1–10% | Run enough iterations for warm-cache state to dominate |
| NUMA effects | 5–50% | Pin to a single NUMA node with `numactl --cpunodebind=0 --membind=0` |

For scientific benchmarking on Linux, the full setup:
```bash
# Disable frequency scaling (requires root)
cpupower frequency-set --governor performance
# Pin to a single CPU core
taskset -c 0 ./benchmark
# Or with Go:
taskset -c 0 go test -bench=. -count=10
```

### Statistical Analysis with benchstat

`benchstat` compares two sets of benchmark results and reports the geometric mean
(correct for ratios), confidence interval, and p-value. It implements Welch's t-test
for the statistical test:

```bash
# Run each benchmark 10 times (more is better; 10 is minimum for benchstat)
go test -bench=. -count=10 > old.txt
# apply change
go test -bench=. -count=10 > new.txt
benchstat old.txt new.txt
```

Output:
```
name             old time/op    new time/op    delta
ProcessData/1K   1.21µs ± 3%    0.89µs ± 2%   -26.2%   (p=0.000 n=10+10)
ProcessData/10K  12.3µs ± 5%    9.1µs ± 3%    -26.1%   (p=0.000 n=10+10)
```

The `±` percentage is the relative standard deviation (coefficient of variation). If it
is >5%, your results are noisy and conclusions may be unreliable. The `p=0.000` means
the null hypothesis (no difference) is rejected with p < 0.001. If p > 0.05, the
difference is not statistically significant.

### The Benchmark Lie — Microbenchmark vs Production

Microbenchmarks measure isolated functions with warm caches, consistent branch predictor
state, and no concurrent load. Production workloads have: cold caches (function runs
after unrelated work that evicts it), polymorphic call sites (branch predictor trained
on mixed types), concurrent GC or background tasks, and variable input sizes.

A function that benchmarks 10% faster may not improve production P99 latency because:
- The function represents 2% of total wall time; 10% of 2% = 0.2% total improvement
- The optimization shifted the bottleneck elsewhere (Amdahl's Law: you reduced a
  non-dominant term)
- Production inputs trigger the worst-case path that the microbenchmark avoided

Always validate microbenchmark improvements with production profiling or realistic
end-to-end benchmarks (realistic input data, realistic concurrency, realistic cache state).

## Implementation: Go

```go
package main

import (
	"math/rand"
	"runtime"
	"testing"
)

// --- Demonstrating compiler elision ---

func computeChecksum(data []byte) uint64 {
	var h uint64
	for _, b := range data {
		h = h*31 + uint64(b)
	}
	return h
}

// WRONG: result may be elided — the compiler sees _ = result and removes the call
func BenchmarkChecksumWrong(b *testing.B) {
	data := make([]byte, 4096)
	rand.Read(data)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := computeChecksum(data) // compiler may prove result unused
		_ = result                      // this does NOT reliably prevent elision
	}
	// Expected: may show suspiciously low ns/op (benchmark is measuring nothing)
}

// CORRECT: use runtime.KeepAlive on a package-level sink
var globalSink uint64 // package-level variable forces the result to be "used"

func BenchmarkChecksumCorrect(b *testing.B) {
	data := make([]byte, 4096)
	rand.Read(data)
	b.ResetTimer()
	var localSink uint64
	for i := 0; i < b.N; i++ {
		localSink = computeChecksum(data)
	}
	// KeepAlive prevents the compiler from eliminating localSink.
	// It must be outside the loop to not add loop overhead.
	runtime.KeepAlive(localSink)
}

// --- Correct b.ResetTimer() placement ---

func BenchmarkWithSetup(b *testing.B) {
	// Setup: do not include in benchmark timing
	data := make([]byte, 1<<20) // 1 MB
	rand.Read(data)

	b.ResetTimer() // reset timer AFTER setup is complete

	for i := 0; i < b.N; i++ {
		_ = computeChecksum(data)
		runtime.KeepAlive(data)
	}
}

// WRONG: timer includes allocation in every iteration
func BenchmarkWithSetupWrong(b *testing.B) {
	for i := 0; i < b.N; i++ {
		data := make([]byte, 1<<20) // allocation in the loop — not what we're benchmarking
		rand.Read(data)
		_ = computeChecksum(data)
	}
	// This benchmarks "allocation + random fill + checksum", not just checksum
}

// --- Sub-benchmarks for scaling behavior ---

func BenchmarkChecksumSizes(b *testing.B) {
	for _, size := range []int{64, 256, 1024, 4096, 65536, 1 << 20} {
		data := make([]byte, size)
		rand.Read(data)
		b.Run(
			// Use human-readable names for benchstat comparison
			formatSize(size),
			func(b *testing.B) {
				b.SetBytes(int64(size)) // enables benchstat to report MB/s
				var sink uint64
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					sink = computeChecksum(data)
				}
				runtime.KeepAlive(sink)
			},
		)
	}
}

func formatSize(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1<<20:
		return fmt.Sprintf("%dKB", n/1024)
	default:
		return fmt.Sprintf("%dMB", n/(1<<20))
	}
}

// --- -benchmem: tracking allocations ---

type Request struct {
	ID   int
	Data string
}

// BEFORE: allocates a new string per request
func processRequestAllocating(r Request) string {
	return fmt.Sprintf("processed-%d-%s", r.ID, r.Data) // allocates
}

// AFTER: returns a pre-sized buffer (no allocation for small strings)
func processRequestNoAlloc(r Request, buf []byte) []byte {
	buf = buf[:0]
	buf = strconv.AppendInt(buf, int64(r.ID), 10)
	buf = append(buf, '-')
	buf = append(buf, r.Data...)
	return buf
}

func BenchmarkProcessRequestAllocating(b *testing.B) {
	req := Request{ID: 42, Data: "hello-world"}
	b.ReportAllocs() // same as -benchmem on the command line
	for i := 0; i < b.N; i++ {
		result := processRequestAllocating(req)
		runtime.KeepAlive(result)
	}
	// Expected: 1 alloc/op, ~64 B/op
}

func BenchmarkProcessRequestNoAlloc(b *testing.B) {
	req := Request{ID: 42, Data: "hello-world"}
	buf := make([]byte, 0, 64)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf = processRequestNoAlloc(req, buf)
		runtime.KeepAlive(buf)
	}
	// Expected: 0 allocs/op, 0 B/op (after warmup)
}

// --- benchstat workflow (command-line instructions) ---
//
// Step 1: measure before change
//   go test -bench=BenchmarkProcessRequest -count=10 ./... > before.txt
//
// Step 2: apply your change
//
// Step 3: measure after change
//   go test -bench=BenchmarkProcessRequest -count=10 ./... > after.txt
//
// Step 4: compare
//   benchstat before.txt after.txt
//
// Step 5: interpret
//   - If delta column shows improvement: check p-value (must be < 0.05)
//   - If ± percentage > 5%: results are too noisy; try taskset, performance governor
//   - If n < 10: results are statistically weak; run -count=20

import (
	"fmt"
	"math/rand"
	"runtime"
	"strconv"
	"testing"
)

func main() {
	fmt.Println("Run: go test -bench=. -benchmem -count=10 ./...")
	fmt.Println("Then: benchstat before.txt after.txt")
}
```

### Go-specific Considerations

**`b.N` is determined by the testing framework**: Never set `b.N` manually and never use
a fixed loop count. The framework will run the loop more times for fast functions. For
a function that takes 1 ns, `b.N` may reach 10 billion. For a function that takes 1 ms,
`b.N` may be 1000. The framework ensures at least 1 second of measurement.

**`testing.B.ReportAllocs()`**: Reports allocations per operation in the benchmark output.
Zero allocations in a hot path is often achievable and worth targeting. Each allocation
in a hot path is a potential GC trigger, and GC pauses add non-deterministic latency.

**`benchstat` installation**: `go install golang.org/x/perf/cmd/benchstat@latest`. The
output format has changed in recent versions — version 0.6+ produces different output from
0.5. Use consistent versions between before/after runs.

**`-benchtime=Ns` for slow functions**: For functions taking >1 ms, use `-benchtime=10s`
to collect enough samples. The default 1 second produces very few iterations for slow
functions, leading to high noise.

## Implementation: Rust

```rust
// Cargo.toml:
// [dev-dependencies]
// criterion = { version = "0.5", features = ["html_reports"] }
//
// [[bench]]
// name = "benchmarks"
// harness = false

use criterion::{
    criterion_group, criterion_main,
    measurement::WallTime,
    BenchmarkId, Criterion, Throughput,
};
use std::hint::black_box; // std::hint::black_box (stable since Rust 1.66)

// --- The function under test ---
fn checksum(data: &[u8]) -> u64 {
    data.iter().fold(0u64, |h, &b| h.wrapping_mul(31).wrapping_add(b as u64))
}

// --- Incorrect: compiler eliminates unused result ---
// This would be wrong if using a less careful API:
// for _ in 0..N { checksum(&data); } // result dropped — may be compiled away

// --- Correct: criterion's black_box ---
fn bench_checksum(c: &mut Criterion) {
    // Input sizes as a group for scaling analysis
    let mut group = c.benchmark_group("checksum");

    for size in [64usize, 256, 1024, 4096, 65536, 1 << 20] {
        let data: Vec<u8> = (0..size).map(|i| i as u8).collect();
        // Tell criterion how many bytes we're processing — enables throughput reporting
        group.throughput(Throughput::Bytes(size as u64));

        group.bench_with_input(
            BenchmarkId::from_parameter(size),
            &data,
            |b, data| {
                // black_box prevents the compiler from hoisting the computation
                // out of the loop or eliminating it as dead code
                b.iter(|| black_box(checksum(black_box(data))))
            },
        );
    }
    group.finish();
}

// --- Setup cost: iter_with_setup ---

fn bench_with_setup(c: &mut Criterion) {
    c.bench_function("checksum_with_fresh_data", |b| {
        // iter_batched runs setup before each batch of iterations.
        // Use this when the function consumes or mutates its input.
        b.iter_batched(
            || -> Vec<u8> {
                // Setup: runs once per batch, NOT timed
                let mut data = vec![0u8; 4096];
                // Fill with random-ish data
                for (i, v) in data.iter_mut().enumerate() {
                    *v = (i * 7 + 13) as u8;
                }
                data
            },
            |data| {
                // Benchmark: runs many times per batch, IS timed
                black_box(checksum(black_box(&data)))
            },
            criterion::BatchSize::SmallInput, // data fits in L1 cache
        );
    });
}

// --- Comparing BEFORE and AFTER ---
// criterion automatically compares against the previous run if you use the same
// benchmark name. Run `cargo bench`, change the implementation, run `cargo bench`
// again — criterion will show the delta and confidence interval.
//
// For explicit before/after comparison with HTML reports:
// 1. cargo bench --bench benchmarks -- --save-baseline before
// 2. (apply change)
// 3. cargo bench --bench benchmarks -- --baseline before
// This shows regression/improvement relative to the saved baseline.

fn bench_allocation_comparison(c: &mut Criterion) {
    let mut group = c.benchmark_group("string_building");
    let id = 42usize;
    let data = "hello-world";

    // BEFORE: allocating version
    group.bench_function("with_format", |b| {
        b.iter(|| {
            let s = format!("processed-{}-{}", black_box(id), black_box(data));
            black_box(s)
        })
    });

    // AFTER: zero-allocation version using a pre-allocated buffer
    group.bench_function("with_write", |b| {
        use std::io::Write;
        let mut buf = Vec::with_capacity(64);
        b.iter(|| {
            buf.clear();
            write!(buf, "processed-{}-{}", id, data).unwrap();
            black_box(&buf);
        })
    });

    group.finish();
}

criterion_group!(
    benches,
    bench_checksum,
    bench_with_setup,
    bench_allocation_comparison
);
criterion_main!(benches);
```

### Rust-specific Considerations

**criterion's warm-up**: criterion runs a warm-up phase before measurement to put the CPU
into a steady thermal state. The default is 3 seconds. For latency-sensitive benchmarks
where cache state matters, increase warm-up: `c.warm_up_time(Duration::from_secs(10))`.

**`black_box` in std**: `std::hint::black_box` was stabilized in Rust 1.66. Before that,
criterion provided its own. Both are equivalent — they insert a memory barrier preventing
LLVM from reasoning about the value's usage. Use `std::hint::black_box` in new code.

**Benchmark comparison and `--save-baseline`**: criterion saves benchmark results in
`target/criterion/<bench_name>/`. The `--save-baseline NAME` flag saves a named baseline.
`--baseline NAME` compares the current run against the named baseline. For systematic
before/after comparison matching Go's `benchstat` workflow, use saved baselines.

**`cargo criterion` (deprecated)**: The `cargo-criterion` wrapper tool added HTML reports
and interactive flamegraph integration. It has been superseded by criterion 0.5's built-in
HTML reports. Use `cargo bench` directly.

**Measuring allocations**: Rust's `dhat` crate tracks heap allocations during benchmarks.
Add it as a profiler and call `dhat::init()` before the benchmark loop. For zero-allocation
assertions in tests, use `dhat::stats()` and assert `stats.total_bytes == 0`.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Benchmark framework | `testing.B` (stdlib) | `criterion` (crate, widely standard) |
| Compiler elision prevention | `runtime.KeepAlive(sink)` + package-level sink | `std::hint::black_box` |
| Statistical comparison | `benchstat` (external tool) | criterion built-in (reports CI, p-value) |
| Statistical test | Welch's t-test (benchstat) | Bootstrap confidence interval (criterion) |
| Report format | Text table | Text + HTML with plots |
| Allocation tracking | `-benchmem` / `b.ReportAllocs()` | `dhat` crate |
| Warm-up | Not explicit (b.N grows automatically) | Explicit warm-up phase (configurable) |
| Setup without timing | `b.ResetTimer()` | `b.iter_batched(setup_fn, bench_fn, ...)` |
| Sub-benchmarks | `b.Run("name", func(b *testing.B) {...})` | `bench_group.bench_with_input(id, &data, ...)` |
| CPU pinning | External `taskset` | External `taskset` |
| Noise reduction | External `cpupower`, `taskset` | External `cpupower`, `taskset` |

## Production War Stories

**Mytkowicz et al. — "Producing Wrong Data" (ASPLOS 2009)**: This paper showed that
changing the size of environment variables on a Linux system changes the stack address,
which changes cache alignment, which changes benchmark results by up to 40% — without
any change to the code. This is the ASLR and memory alignment effect. The paper is the
canonical argument for why single-run benchmark results are unreliable.

**Go's `encoding/json` benchmark discovery**: The Go team discovered in 2019 that their
`json.Marshal` benchmark was showing a 15% speedup from a refactor. Closer inspection
with `benchstat` (10 runs, with p-value) showed p=0.3 — the result was not statistically
significant. The actual improvement after noise reduction turned out to be 3%. The
benchmark framework prevented a false claim in the release notes.

**Criterion and Rust's sort algorithm selection**: The Rust standard library's sort
implementation was changed from introsort to a Timsort variant in Rust 1.59. The
decision was based on criterion benchmarks run on a range of input distributions: random,
sorted, reverse-sorted, partially sorted, and random with duplicates. The Timsort variant
was faster for 4 of 5 distributions. Without systematic benchmarking across distributions,
the worst-case regression on random input (which was not significantly slower) would have
been missed.

## Numbers That Matter

| Metric | Value / Range |
|--------|--------------|
| Minimum runs for statistical significance | 10 (benchstat / criterion) |
| Thermal throttling variation (uncooled) | 5–30% between runs |
| Acceptable noise floor for valid results | <3% relative standard deviation |
| ASLR-induced variation (Linux) | 1–5% |
| Minimum benchmark duration (Go `-benchtime`) | 1 second (default) |
| `benchstat` significance threshold | p < 0.05 |
| Criterion default warm-up time | 3 seconds |
| Criterion default measurement time | 5 seconds |
| `black_box` overhead | ~0 ns (it is an optimization barrier, not a function call) |
| Memory barrier overhead from `KeepAlive` | ~1–2 ns |

## Common Pitfalls

**Single-run benchmarks**: A benchmark run once is not a measurement — it is an anecdote.
Always use `-count=10` (Go) or criterion's multiple-sample approach (Rust). Single runs
can vary 20%+ from run to run on uncontrolled hardware.

**Not resetting the timer after expensive setup**: If setup takes 100 ms and the benchmark
function takes 1 µs, the reported `ns/op` will be dominated by setup. Call `b.ResetTimer()`
in Go after setup. In Rust, use `iter_batched` to separate setup from measurement.

**Comparing benchmarks across machines**: Different hardware has different frequencies,
cache sizes, and microarchitectures. A 10% improvement on your laptop may be 3% or 25%
on the production server. Benchmark on hardware that matches production, or use relative
comparisons (before vs after on the same machine).

**Warm-cache benchmarks that don't reflect production**: A function benchmarked in tight
loop will always have warm L1 cache. If the production code path calls the function once
per request with hundreds of other function calls in between, the cache state will be cold.
Use `iter_batched` with a large batch size or introduce artificial cache pollution to
simulate cold-cache conditions.

**Ignoring the p-value from benchstat**: `benchstat` will print a delta even for results
where p > 0.05. A delta marked with `~` (not significant) should be ignored. The `~`
means the null hypothesis (no difference) cannot be rejected. Reporting a `~` result as
an improvement is scientific dishonesty.

## Exercises

**Exercise 1** (30 min): Write a Go benchmark with a deliberate compiler elision bug
(discarding the result into `_`). Run it and observe the suspiciously fast `ns/op`. Fix
it with `runtime.KeepAlive`. Verify the `ns/op` increases to a realistic value. Then add
a setup cost (100 ms `time.Sleep`) before `b.ResetTimer()` and verify the setup is not
counted in the benchmark time.

**Exercise 2** (2–4h): Write a checksum function in Go with three implementations: naive
byte loop, loop unrolled 8×, and using `encoding/binary`. Benchmark all three with
`-count=10`. Use `benchstat` to compare. Confirm p < 0.05 for any differences. Run on
three different input sizes. Observe whether the speedup is consistent across sizes.

**Exercise 3** (4–8h): Reproduce the noise sources experimentally. On a Linux machine,
benchmark the same function under: (a) default settings, (b) performance CPU governor,
(c) CPU pinning with `taskset -c 0`, (d) both (b) and (c). For each configuration, report
the relative standard deviation from `benchstat`. Confirm that (d) achieves <3% noise
floor. Document the commands and results.

**Exercise 4** (8–15h): Implement the full benchmarking workflow for a realistic change.
Take an existing function in a Go or Rust project. Identify an optimization opportunity
from profiling (use the methodology from [Profiling Methodology](../01-profiling-methodology/01-profiling-methodology.md)).
Write the optimized version. Run 20-sample before/after benchmarks. Use `benchstat`/
criterion to confirm statistical significance. Validate the improvement with an end-to-end
integration benchmark. Write a one-page report: hypothesis, measurement methodology, result,
p-value, and whether the improvement is production-relevant.

## Further Reading

### Foundational Papers

- Mytkowicz, Diwan, Hauswirth, Sweeney — ["Producing Wrong Data Without Doing Anything
  Obviously Wrong"](https://dl.acm.org/doi/10.1145/1508284.1508275) (ASPLOS 2009) —
  the definitive paper on measurement artifacts in microbenchmarks; required reading before
  trusting any single benchmark result

### Books

- Daniel J. Barrett — *Efficient Linux at the Command Line* (2022) — chapter on process
  management covers `nice`, `taskset`, and CPU affinity for benchmark isolation
- Brendan Gregg — *Systems Performance* (2nd ed.) — Chapter 12 covers benchmarking
  methodology, error sources, and statistical analysis

### Blog Posts

- [How to Write Benchmarks in Go](https://dave.cheney.net/2013/06/30/how-to-write-benchmarks-in-go)
  — Dave Cheney's canonical guide; covers ResetTimer, KeepAlive, and -benchmem
- [Benchmarking in Rust with Criterion](https://bheisler.github.io/post/benchmarking-with-criterion-rs/)
  — bheisler (criterion author) on proper Criterion usage
- [Statistical Rigour for Benchmarks](https://www.sigplan.org/sites/default/files/empirical_evaluation_guidelines_2013.pdf)
  — SIGPLAN Empirical Evaluation Guidelines; the standard for academic performance papers

### Tools Documentation

- [`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat) — install:
  `go install golang.org/x/perf/cmd/benchstat@latest`
- [`criterion.rs`](https://bheisler.github.io/criterion.rs/book/) — full criterion documentation
- [`dhat`](https://docs.rs/dhat) — Rust heap allocation profiler for use in benchmarks
- [`cpupower`](https://linux.die.net/man/1/cpupower) — Linux CPU frequency management:
  `cpupower frequency-set --governor performance`
