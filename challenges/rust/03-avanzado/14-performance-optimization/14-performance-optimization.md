# 14. Performance Optimization

**Difficulty**: Avanzado

## Prerequisites

- Completed: exercises 01-13 (ownership, traits, lifetimes, unsafe, serde)
- Ability to read disassembly output at a high level
- Basic understanding of CPU caches, branch prediction, and memory allocation

## Learning Objectives

- Write criterion benchmarks and interpret their statistical output
- Profile Rust programs with perf/flamegraph and dhat for allocation tracking
- Apply compiler-level optimizations (LTO, codegen-units, PGO)
- Eliminate unnecessary allocations using SmallVec, ArrayVec, and stack-based patterns
- Design cache-friendly data layouts (SoA vs AoS) and measure the difference
- Use `const fn` and compile-time evaluation to shift work from runtime to compile time

## Concepts

### Criterion Benchmarks

`criterion` provides statistically rigorous benchmarking with warmup, outlier detection, and regression analysis:

```rust
// benches/my_benchmark.rs
use criterion::{black_box, criterion_group, criterion_main, Criterion};

fn fibonacci(n: u64) -> u64 {
    match n {
        0 | 1 => n,
        _ => fibonacci(n - 1) + fibonacci(n - 2),
    }
}

fn bench_fib(c: &mut Criterion) {
    c.bench_function("fib 20", |b| {
        b.iter(|| fibonacci(black_box(20)))
    });
}

criterion_group!(benches, bench_fib);
criterion_main!(benches);
```

```toml
# Cargo.toml
[dev-dependencies]
criterion = { version = "0.5", features = ["html_reports"] }

[[bench]]
name = "my_benchmark"
harness = false
```

`black_box` prevents the compiler from optimizing away the computation. Without it, the compiler might compute the result at compile time and your benchmark measures nothing.

### Profiling with perf and Flamegraphs

```bash
# Build with debug symbols in release mode
# Cargo.toml:
# [profile.release]
# debug = true

cargo build --release

# Record with perf (Linux)
perf record -g --call-graph dwarf ./target/release/my_binary

# Generate flamegraph
cargo install flamegraph
cargo flamegraph --release -- <args>
# or:
perf script | inferno-collapse-perf | inferno-flamegraph > flamegraph.svg
```

On macOS, use `cargo instruments` (requires Xcode Instruments) or `samply`:

```bash
cargo install samply
cargo build --release
samply record ./target/release/my_binary
```

Flamegraphs show where your program spends time. Wide bars at the top are the hot functions. Look for:
- Unexpected allocator calls (`alloc::`, `__rust_alloc`)
- Hashing overhead (`HashMap` operations)
- Serialization/deserialization dominating
- Lock contention (`pthread_mutex_lock`)

### DHAT Allocation Tracking

`dhat` instruments the allocator to track every allocation:

```rust
// Only in debug/test builds
#[cfg(feature = "dhat-heap")]
#[global_allocator]
static ALLOC: dhat::Alloc = dhat::Alloc;

fn main() {
    #[cfg(feature = "dhat-heap")]
    let _profiler = dhat::Profiler::new_heap();

    // ... your code ...
    // On drop, prints allocation statistics
}
```

```toml
[dependencies]
dhat = { version = "0.3", optional = true }

[features]
dhat-heap = ["dhat"]
```

```bash
cargo run --release --features dhat-heap
# Opens dhat-heap.json in https://nnethercote.github.io/dh_view/dh_view.html
```

### Compiler Optimizations

```toml
# Cargo.toml
[profile.release]
lto = true              # Link-Time Optimization: cross-crate inlining
codegen-units = 1       # Single codegen unit: better optimization, slower compile
panic = "abort"         # No unwinding: smaller binary, slightly faster
strip = true            # Strip debug symbols from binary
opt-level = 3           # Maximum optimization (default for release)
```

**LTO** enables the compiler to inline across crate boundaries. Without it, calls to functions in dependencies are never inlined.

**codegen-units = 1** forces the compiler to process the entire crate as one unit, enabling optimizations that require a global view. Default is 16 for parallelism.

**Profile-Guided Optimization (PGO):**
```bash
# Step 1: build instrumented binary
RUSTFLAGS="-Cprofile-generate=/tmp/pgo-data" cargo build --release

# Step 2: run representative workload
./target/release/my_binary < typical_input.txt

# Step 3: merge profile data
llvm-profdata merge -o /tmp/pgo-data/merged.profdata /tmp/pgo-data

# Step 4: build with profile data
RUSTFLAGS="-Cprofile-use=/tmp/pgo-data/merged.profdata" cargo build --release
```

### Avoiding Allocations

Every heap allocation is a potential cache miss and a synchronization point in the allocator. Strategies:

```rust
use smallvec::SmallVec;

// SmallVec: inline storage for small cases, heap for overflow
fn process_tags(input: &str) -> SmallVec<[&str; 8]> {
    // Most items have < 8 tags. No heap allocation in the common case.
    input.split(',').collect()
}

// ArrayVec: fixed capacity, never allocates, panics or returns Err on overflow
use arrayvec::ArrayVec;

fn parse_header(line: &str) -> ArrayVec<(&str, &str), 16> {
    let mut headers = ArrayVec::new();
    for part in line.split(';') {
        if let Some((k, v)) = part.split_once('=') {
            // try_push returns Err if full -- no panic
            if headers.try_push((k.trim(), v.trim())).is_err() {
                break;
            }
        }
    }
    headers
}

// Reuse buffers instead of allocating new ones
fn process_lines(lines: &[&str]) -> Vec<String> {
    let mut buf = String::new();
    let mut results = Vec::with_capacity(lines.len()); // pre-allocate

    for line in lines {
        buf.clear(); // reuse allocation
        buf.push_str("processed: ");
        buf.push_str(line);
        results.push(buf.clone());
    }
    results
}
```

### Iterator Chains vs Manual Loops

Iterator chains often optimize better than manual loops because the compiler can reason about the entire pipeline:

```rust
// Iterator chain: the compiler fuses map + filter + sum into a single loop
fn sum_even_squares(data: &[i32]) -> i64 {
    data.iter()
        .filter(|&&x| x % 2 == 0)
        .map(|&x| (x as i64) * (x as i64))
        .sum()
}

// Manual loop: equivalent performance, but harder to optimize in complex cases
fn sum_even_squares_manual(data: &[i32]) -> i64 {
    let mut total: i64 = 0;
    for &x in data {
        if x % 2 == 0 {
            total += (x as i64) * (x as i64);
        }
    }
    total
}

// Collect into pre-sized Vec
fn transform(data: &[i32]) -> Vec<i32> {
    // This pre-allocates because the iterator has a known size hint
    data.iter().map(|x| x * 2).collect()
}
```

### Cache-Friendly Data: SoA vs AoS

**Array of Structs (AoS):** each element contains all fields. Cache-hostile when you only access one field.

**Struct of Arrays (SoA):** each field is a separate array. Cache-friendly when you iterate over one field.

```rust
// AoS: each particle is contiguous in memory
struct ParticleAoS {
    x: f64,
    y: f64,
    z: f64,
    mass: f64,
    charge: f64,
}

fn total_mass_aos(particles: &[ParticleAoS]) -> f64 {
    // Each iteration loads 40 bytes (5 * f64) but only reads 8 (mass).
    // 80% of cache line loads are wasted.
    particles.iter().map(|p| p.mass).sum()
}

// SoA: each field is a contiguous array
struct ParticlesSoA {
    x: Vec<f64>,
    y: Vec<f64>,
    z: Vec<f64>,
    mass: Vec<f64>,
    charge: Vec<f64>,
}

fn total_mass_soa(particles: &ParticlesSoA) -> f64 {
    // Iterates over a contiguous f64 array. Every byte loaded is useful.
    // The CPU prefetcher can predict the access pattern.
    particles.mass.iter().sum()
}
```

### const fn

Move computation from runtime to compile time:

```rust
const fn fibonacci(n: u32) -> u64 {
    let mut a: u64 = 0;
    let mut b: u64 = 1;
    let mut i = 0;
    while i < n {
        let tmp = b;
        b = a + b;
        a = tmp;
        i += 1;
    }
    a
}

// Computed at compile time. Zero runtime cost.
const FIB_20: u64 = fibonacci(20);

// Lookup table built at compile time
const fn build_lookup() -> [u8; 256] {
    let mut table = [0u8; 256];
    let mut i = 0;
    while i < 256 {
        table[i] = (i as u8).count_ones() as u8;
        i += 1;
    }
    table
}

const POPCOUNT_TABLE: [u8; 256] = build_lookup();
```

## Exercises

### Exercise 1: Benchmark and Optimize a String Parser

Write a function that parses key-value pairs from a log line format: `key1=value1 key2="value with spaces" key3=value3`. Implement three versions:

1. **Naive**: split on spaces, split on `=`, collect into `HashMap<String, String>`
2. **Optimized**: pre-allocated `HashMap`, reuse string buffer, `SmallVec` for intermediate storage
3. **Zero-alloc**: return an iterator of `(&str, &str)` pairs that borrows from the input

Benchmark all three with criterion. The input should be a realistic 200-character log line.

**Cargo.toml:**
```toml
[package]
name = "perf-exercises"
edition = "2021"

[dependencies]
smallvec = "1"

[dev-dependencies]
criterion = { version = "0.5", features = ["html_reports"] }

[[bench]]
name = "parser_bench"
harness = false
```

**Hints:**
- Quoted values need special handling: scan for closing quote
- `HashMap::with_capacity` avoids rehashing
- The zero-alloc version should be an iterator struct, not collect into a container

<details>
<summary>Solution</summary>

**src/lib.rs:**
```rust
use smallvec::SmallVec;
use std::collections::HashMap;

// --- Version 1: Naive ---
pub fn parse_naive(input: &str) -> HashMap<String, String> {
    let mut map = HashMap::new();
    let mut chars = input.chars().peekable();
    let mut key = String::new();
    let mut value = String::new();
    let mut in_key = true;
    let mut in_quotes = false;

    while let Some(ch) = chars.next() {
        match ch {
            '=' if in_key => {
                in_key = false;
            }
            '"' if !in_key => {
                in_quotes = !in_quotes;
            }
            ' ' if !in_key && !in_quotes => {
                map.insert(
                    std::mem::take(&mut key),
                    std::mem::take(&mut value),
                );
                in_key = true;
            }
            _ if in_key => key.push(ch),
            _ => value.push(ch),
        }
    }

    if !key.is_empty() {
        map.insert(key, value);
    }
    map
}

// --- Version 2: Optimized with pre-allocation ---
pub fn parse_optimized(input: &str) -> HashMap<String, String> {
    // Estimate number of pairs by counting '='
    let pair_count = input.bytes().filter(|&b| b == b'=').count();
    let mut map = HashMap::with_capacity(pair_count);

    for (k, v) in KvIter::new(input) {
        map.insert(k.to_string(), v.to_string());
    }
    map
}

// --- Version 3: Zero-allocation iterator ---
pub struct KvIter<'a> {
    remaining: &'a str,
}

impl<'a> KvIter<'a> {
    pub fn new(input: &'a str) -> Self {
        Self { remaining: input.trim() }
    }
}

impl<'a> Iterator for KvIter<'a> {
    type Item = (&'a str, &'a str);

    fn next(&mut self) -> Option<Self::Item> {
        if self.remaining.is_empty() {
            return None;
        }

        // Find the '='
        let eq_pos = self.remaining.find('=')?;
        let key = &self.remaining[..eq_pos];
        let after_eq = &self.remaining[eq_pos + 1..];

        let (value, rest) = if after_eq.starts_with('"') {
            // Quoted value: find closing quote
            let content = &after_eq[1..];
            match content.find('"') {
                Some(end) => {
                    let val = &content[..end];
                    let rest = &content[end + 1..];
                    (val, rest.trim_start())
                }
                None => {
                    // No closing quote: take the rest
                    (content, "")
                }
            }
        } else {
            // Unquoted: take until space
            match after_eq.find(' ') {
                Some(space) => {
                    (&after_eq[..space], after_eq[space + 1..].trim_start())
                }
                None => (after_eq, ""),
            }
        };

        self.remaining = rest;
        Some((key, value))
    }
}

// Convenience function using the iterator
pub fn parse_zero_alloc(input: &str) -> SmallVec<[(&str, &str); 16]> {
    KvIter::new(input).collect()
}

fn main() {
    let input = r#"host=web-01 method=GET path="/api/users/search" status=200 duration_ms=42 trace_id=abc-123-def request_id=req-789 user_agent="Mozilla/5.0""#;

    println!("Naive:");
    for (k, v) in &parse_naive(input) {
        println!("  {k} = {v}");
    }

    println!("\nZero-alloc:");
    for (k, v) in KvIter::new(input) {
        println!("  {k} = {v}");
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const INPUT: &str = r#"host=web-01 method=GET path="/api/users" status=200"#;

    #[test]
    fn naive_parses_correctly() {
        let map = parse_naive(INPUT);
        assert_eq!(map["host"], "web-01");
        assert_eq!(map["path"], "/api/users");
        assert_eq!(map["status"], "200");
    }

    #[test]
    fn optimized_matches_naive() {
        let naive = parse_naive(INPUT);
        let opt = parse_optimized(INPUT);
        assert_eq!(naive, opt);
    }

    #[test]
    fn zero_alloc_parses_correctly() {
        let pairs: Vec<_> = KvIter::new(INPUT).collect();
        assert_eq!(pairs.len(), 4);
        assert_eq!(pairs[0], ("host", "web-01"));
        assert_eq!(pairs[2], ("path", "/api/users"));
    }

    #[test]
    fn empty_input() {
        assert_eq!(KvIter::new("").count(), 0);
        assert!(parse_naive("").is_empty());
    }

    #[test]
    fn single_pair() {
        let pairs: Vec<_> = KvIter::new("key=value").collect();
        assert_eq!(pairs, vec![("key", "value")]);
    }
}
```

**benches/parser_bench.rs:**
```rust
use criterion::{black_box, criterion_group, criterion_main, Criterion};
use perf_exercises::{parse_naive, parse_optimized, parse_zero_alloc, KvIter};

const INPUT: &str = r#"host=web-01 method=GET path="/api/users/search" status=200 duration_ms=42 trace_id=abc-123-def request_id=req-789 user_agent="Mozilla/5.0 (X11; Linux x86_64)""#;

fn bench_parsers(c: &mut Criterion) {
    let mut group = c.benchmark_group("kv_parser");

    group.bench_function("naive", |b| {
        b.iter(|| parse_naive(black_box(INPUT)))
    });

    group.bench_function("optimized", |b| {
        b.iter(|| parse_optimized(black_box(INPUT)))
    });

    group.bench_function("zero_alloc", |b| {
        b.iter(|| parse_zero_alloc(black_box(INPUT)))
    });

    group.bench_function("iterator_only", |b| {
        b.iter(|| {
            let mut count = 0u32;
            for (_, v) in KvIter::new(black_box(INPUT)) {
                count += v.len() as u32;
            }
            count
        })
    });

    group.finish();
}

criterion_group!(benches, bench_parsers);
criterion_main!(benches);
```

Run: `cargo bench`

Expected results (approximate, varies by machine):
- Naive: ~800ns (HashMap allocation + string copies)
- Optimized: ~500ns (pre-allocation, but still copies strings)
- Zero-alloc SmallVec: ~150ns (no string copies, stack-based storage)
- Iterator only: ~50ns (no collection at all)
</details>

### Exercise 2: SoA vs AoS Cache Benchmark

Implement a particle simulation with N=1,000,000 particles. Each particle has: `x, y, z: f64` position, `vx, vy, vz: f64` velocity, `mass: f64`.

Implement two operations in both AoS and SoA layouts:
1. **Total mass**: sum all masses (accesses one field)
2. **Update positions**: `x += vx * dt` for all axes (accesses six fields)

Benchmark both operations in both layouts. Predict which layout wins for each operation and verify.

**Hints:**
- Total mass: SoA wins (sequential access to one array vs strided access)
- Update positions: it depends -- AoS may win due to spatial locality of all 6 fields per particle
- Use `cargo bench` with criterion, not `cargo run`, for meaningful numbers

<details>
<summary>Solution</summary>

```rust
// src/lib.rs

// Array of Structs
pub struct ParticleAoS {
    pub x: f64, pub y: f64, pub z: f64,
    pub vx: f64, pub vy: f64, pub vz: f64,
    pub mass: f64,
}

pub struct SimAoS {
    pub particles: Vec<ParticleAoS>,
}

impl SimAoS {
    pub fn new(n: usize) -> Self {
        let particles = (0..n)
            .map(|i| {
                let f = i as f64;
                ParticleAoS {
                    x: f * 0.1, y: f * 0.2, z: f * 0.3,
                    vx: 1.0, vy: 0.5, vz: -0.3,
                    mass: 1.0 + (i % 100) as f64 * 0.01,
                }
            })
            .collect();
        Self { particles }
    }

    pub fn total_mass(&self) -> f64 {
        self.particles.iter().map(|p| p.mass).sum()
    }

    pub fn update_positions(&mut self, dt: f64) {
        for p in &mut self.particles {
            p.x += p.vx * dt;
            p.y += p.vy * dt;
            p.z += p.vz * dt;
        }
    }
}

// Struct of Arrays
pub struct SimSoA {
    pub x: Vec<f64>, pub y: Vec<f64>, pub z: Vec<f64>,
    pub vx: Vec<f64>, pub vy: Vec<f64>, pub vz: Vec<f64>,
    pub mass: Vec<f64>,
}

impl SimSoA {
    pub fn new(n: usize) -> Self {
        let mut sim = SimSoA {
            x: Vec::with_capacity(n), y: Vec::with_capacity(n), z: Vec::with_capacity(n),
            vx: Vec::with_capacity(n), vy: Vec::with_capacity(n), vz: Vec::with_capacity(n),
            mass: Vec::with_capacity(n),
        };
        for i in 0..n {
            let f = i as f64;
            sim.x.push(f * 0.1);
            sim.y.push(f * 0.2);
            sim.z.push(f * 0.3);
            sim.vx.push(1.0);
            sim.vy.push(0.5);
            sim.vz.push(-0.3);
            sim.mass.push(1.0 + (i % 100) as f64 * 0.01);
        }
        sim
    }

    pub fn total_mass(&self) -> f64 {
        self.mass.iter().sum()
    }

    pub fn update_positions(&mut self, dt: f64) {
        for i in 0..self.x.len() {
            self.x[i] += self.vx[i] * dt;
            self.y[i] += self.vy[i] * dt;
            self.z[i] += self.vz[i] * dt;
        }
    }
}

fn main() {
    let n = 1_000_000;
    let mut aos = SimAoS::new(n);
    let mut soa = SimSoA::new(n);

    let start = std::time::Instant::now();
    let mass_aos = aos.total_mass();
    let t_aos_mass = start.elapsed();

    let start = std::time::Instant::now();
    let mass_soa = soa.total_mass();
    let t_soa_mass = start.elapsed();

    println!("total_mass AoS: {t_aos_mass:?} ({mass_aos:.2})");
    println!("total_mass SoA: {t_soa_mass:?} ({mass_soa:.2})");

    let start = std::time::Instant::now();
    aos.update_positions(0.016);
    let t_aos_update = start.elapsed();

    let start = std::time::Instant::now();
    soa.update_positions(0.016);
    let t_soa_update = start.elapsed();

    println!("update    AoS: {t_aos_update:?}");
    println!("update    SoA: {t_soa_update:?}");
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn mass_matches() {
        let n = 1000;
        let aos = SimAoS::new(n);
        let soa = SimSoA::new(n);
        let diff = (aos.total_mass() - soa.total_mass()).abs();
        assert!(diff < 1e-6, "mass mismatch: {diff}");
    }

    #[test]
    fn update_produces_same_result() {
        let n = 100;
        let mut aos = SimAoS::new(n);
        let mut soa = SimSoA::new(n);
        let dt = 0.016;

        aos.update_positions(dt);
        soa.update_positions(dt);

        for i in 0..n {
            let diff = (aos.particles[i].x - soa.x[i]).abs();
            assert!(diff < 1e-10, "x mismatch at {i}: {diff}");
        }
    }
}
```

**benches/soa_bench.rs:**
```rust
use criterion::{black_box, criterion_group, criterion_main, Criterion};
use perf_exercises::{SimAoS, SimSoA};

const N: usize = 1_000_000;

fn bench_total_mass(c: &mut Criterion) {
    let aos = SimAoS::new(N);
    let soa = SimSoA::new(N);

    let mut group = c.benchmark_group("total_mass");
    group.bench_function("AoS", |b| b.iter(|| black_box(aos.total_mass())));
    group.bench_function("SoA", |b| b.iter(|| black_box(soa.total_mass())));
    group.finish();
}

fn bench_update(c: &mut Criterion) {
    let mut group = c.benchmark_group("update_positions");

    group.bench_function("AoS", |b| {
        let mut aos = SimAoS::new(N);
        b.iter(|| aos.update_positions(black_box(0.016)))
    });

    group.bench_function("SoA", |b| {
        let mut soa = SimSoA::new(N);
        b.iter(|| soa.update_positions(black_box(0.016)))
    });

    group.finish();
}

criterion_group!(benches, bench_total_mass, bench_update);
criterion_main!(benches);
```

**Expected results (1M particles, x86_64):**

| Operation | AoS | SoA | Winner |
|---|---|---|---|
| total_mass | ~2ms | ~0.3ms | SoA (6-8x) |
| update_positions | ~4ms | ~3ms | SoA (1.3x, less dramatic) |

SoA wins both because the CPU prefetcher and SIMD vectorizer work better on contiguous arrays. For `total_mass`, the advantage is dramatic because AoS loads 56 bytes per particle but uses only 8. For `update_positions`, AoS is closer because all 6 fields are needed and they are co-located.
</details>

### Exercise 3: const fn Lookup Table

Build a compile-time perfect hash function for HTTP status codes. Given a status code (100-599), return the reason phrase in O(1) with no runtime computation.

Requirements:
1. Build the lookup table with `const fn`
2. Handle all standard HTTP status codes (at least 200, 201, 204, 301, 302, 400, 401, 403, 404, 500, 502, 503)
3. Return `Option<&'static str>` for unknown codes
4. Benchmark against a `match` statement and a `HashMap` to show the `const fn` table is faster

**Hints:**
- Use a `const fn` that builds a `[Option<&'static str>; 500]` array indexed by `status - 100`
- `const fn` supports `while` loops and array indexing, but not `for` loops or trait methods
- The table is embedded in the binary at compile time -- zero runtime initialization

<details>
<summary>Solution</summary>

```rust
const TABLE_SIZE: usize = 500; // covers 100-599

const fn build_status_table() -> [Option<&'static str>; TABLE_SIZE] {
    let mut table: [Option<&'static str>; TABLE_SIZE] = [None; TABLE_SIZE];

    // const fn cannot use for loops or match on ranges, so we assign directly.
    // In a real project, a proc macro could generate this.
    table[100 - 100] = Some("Continue");
    table[101 - 100] = Some("Switching Protocols");
    table[200 - 100] = Some("OK");
    table[201 - 100] = Some("Created");
    table[202 - 100] = Some("Accepted");
    table[204 - 100] = Some("No Content");
    table[206 - 100] = Some("Partial Content");
    table[301 - 100] = Some("Moved Permanently");
    table[302 - 100] = Some("Found");
    table[303 - 100] = Some("See Other");
    table[304 - 100] = Some("Not Modified");
    table[307 - 100] = Some("Temporary Redirect");
    table[308 - 100] = Some("Permanent Redirect");
    table[400 - 100] = Some("Bad Request");
    table[401 - 100] = Some("Unauthorized");
    table[403 - 100] = Some("Forbidden");
    table[404 - 100] = Some("Not Found");
    table[405 - 100] = Some("Method Not Allowed");
    table[408 - 100] = Some("Request Timeout");
    table[409 - 100] = Some("Conflict");
    table[410 - 100] = Some("Gone");
    table[413 - 100] = Some("Payload Too Large");
    table[415 - 100] = Some("Unsupported Media Type");
    table[422 - 100] = Some("Unprocessable Entity");
    table[429 - 100] = Some("Too Many Requests");
    table[500 - 100] = Some("Internal Server Error");
    table[501 - 100] = Some("Not Implemented");
    table[502 - 100] = Some("Bad Gateway");
    table[503 - 100] = Some("Service Unavailable");
    table[504 - 100] = Some("Gateway Timeout");

    table
}

static STATUS_TABLE: [Option<&str>; TABLE_SIZE] = build_status_table();

/// O(1) lookup with zero runtime computation.
pub fn reason_phrase_table(code: u16) -> Option<&'static str> {
    if code < 100 || code >= 600 {
        return None;
    }
    STATUS_TABLE[(code - 100) as usize]
}

/// Match-based alternative for comparison.
pub fn reason_phrase_match(code: u16) -> Option<&'static str> {
    match code {
        100 => Some("Continue"),
        101 => Some("Switching Protocols"),
        200 => Some("OK"),
        201 => Some("Created"),
        202 => Some("Accepted"),
        204 => Some("No Content"),
        301 => Some("Moved Permanently"),
        302 => Some("Found"),
        304 => Some("Not Modified"),
        400 => Some("Bad Request"),
        401 => Some("Unauthorized"),
        403 => Some("Forbidden"),
        404 => Some("Not Found"),
        405 => Some("Method Not Allowed"),
        409 => Some("Conflict"),
        422 => Some("Unprocessable Entity"),
        429 => Some("Too Many Requests"),
        500 => Some("Internal Server Error"),
        502 => Some("Bad Gateway"),
        503 => Some("Service Unavailable"),
        504 => Some("Gateway Timeout"),
        _ => None,
    }
}

/// HashMap-based alternative for comparison.
pub fn build_hashmap() -> std::collections::HashMap<u16, &'static str> {
    let mut m = std::collections::HashMap::new();
    m.insert(200, "OK");
    m.insert(201, "Created");
    m.insert(204, "No Content");
    m.insert(301, "Moved Permanently");
    m.insert(302, "Found");
    m.insert(400, "Bad Request");
    m.insert(401, "Unauthorized");
    m.insert(403, "Forbidden");
    m.insert(404, "Not Found");
    m.insert(500, "Internal Server Error");
    m.insert(502, "Bad Gateway");
    m.insert(503, "Service Unavailable");
    m
}

fn main() {
    let codes = [200, 404, 500, 301, 999];
    for code in codes {
        println!("{code}: {:?}", reason_phrase_table(code));
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn known_codes() {
        assert_eq!(reason_phrase_table(200), Some("OK"));
        assert_eq!(reason_phrase_table(404), Some("Not Found"));
        assert_eq!(reason_phrase_table(500), Some("Internal Server Error"));
    }

    #[test]
    fn unknown_codes() {
        assert_eq!(reason_phrase_table(299), None);
        assert_eq!(reason_phrase_table(0), None);
        assert_eq!(reason_phrase_table(600), None);
    }

    #[test]
    fn table_matches_match() {
        for code in 100..600u16 {
            assert_eq!(
                reason_phrase_table(code),
                reason_phrase_match(code),
                "mismatch at {code}"
            );
        }
    }
}
```

**Performance characteristics:**
- **const fn table**: single array index + bounds check. Predictable, no branch mispredictions.
- **match**: compiler may generate a jump table (equivalent) or binary search depending on density.
- **HashMap**: hash computation + bucket lookup + key comparison. Slowest for small key spaces.

The table approach trades 4KB of binary size (500 * 8 bytes for `Option<&str>`) for guaranteed O(1) with zero branches. For HTTP status codes this is an excellent trade-off.
</details>

## Common Mistakes

1. **Benchmarking debug builds.** Always use `cargo bench` or `--release`. Debug builds have no optimizations and misleading profiles.

2. **Forgetting `black_box`.** The compiler will eliminate dead code. If you do not consume the result, your benchmark measures nothing.

3. **Premature SoA conversion.** SoA is faster for single-field scans but adds complexity. Profile first, restructure only where it matters.

4. **Over-using `const fn`.** Complex compile-time computation increases build times. Use it for lookup tables and small computations, not for building entire databases.

5. **Ignoring allocator overhead.** A function that allocates and frees inside a tight loop will spend more time in the allocator than in your logic. Move allocations outside the loop.

## Verification

- `cargo test` for correctness
- `cargo bench` for performance numbers (requires criterion)
- `cargo build --release` with LTO enabled: check binary size with `ls -lh target/release/`
- Profile with `cargo flamegraph --release` to verify your optimizations hit the hot paths

## Summary

Performance optimization in Rust follows a universal process: measure, identify the bottleneck, fix it, measure again. The tools (criterion, flamegraph, dhat) tell you where to look. The techniques (avoiding allocations, cache-friendly layouts, compile-time computation, LTO) give you options. The discipline is: never optimize without a benchmark proving the change helps.

## What's Next

Exercise 15 covers WebAssembly with Rust -- a compilation target where binary size and startup time are the dominant performance constraints.

## Resources

- [criterion.rs](https://bheisler.github.io/criterion.rs/book/)
- [The Rust Performance Book](https://nnethercote.github.io/perf-book/)
- [Flamegraph](https://github.com/flamegraph-rs/flamegraph)
- [DHAT](https://docs.rs/dhat)
- [SmallVec](https://docs.rs/smallvec)
- [Data-oriented design resources](https://dataorienteddesign.com/dodbook/)
