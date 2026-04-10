<!--
type: reference
difficulty: advanced
section: [09-performance-and-optimization]
concepts: [PGO, LTO, BOLT, inliner-budget, devirtualization, monomorphization, profile-guided-optimization, link-time-optimization, post-link-optimization, go-pgo, rust-pgo, llvm-pgo]
languages: [go, rust]
estimated_reading_time: 75 min
bloom_level: evaluate
prerequisites: [go-build-system, cargo-build, LLVM-basics, profiling-methodology]
papers: [Chang et al. — Profile Guided Code Positioning (PLDI 1990), Lattner — LLVM: A Compilation Framework for Lifelong Program Analysis (CGO 2004)]
industry_use: [Chrome PGO, Firefox PGO, Go 1.21 PGO, LLVM BOLT, Linux kernel PGO, ClickHouse LTO+PGO]
language_contrast: high
-->

# Compiler Optimization Flags

> A compiler that has seen your program run knows which branches are taken, which
> functions are hot, and which loops run longest. Profile-Guided Optimization gives the
> compiler that knowledge — and it uses it to make decisions no static analysis could.

## Mental Model

Compilers make optimization decisions statically: "this function might be called often"
or "this branch is probably taken." These guesses are wrong often enough that they leave
significant performance on the table. Profile-Guided Optimization (PGO) replaces guesses
with measurements: the compiler instruments the binary, you run it with representative
workload, and the resulting profile data tells the compiler the truth about hot paths,
branch probabilities, and call frequencies.

The mental model: compiler optimization flags are a hierarchy from free to expensive and
from general to workload-specific:

```
Level 0: Basic optimization flags (no profiling)
  -O2/-O3 (C/C++), --release (Rust), go build (Go default)
  Free; already enabled. No further cost.

Level 1: Link-Time Optimization (LTO)
  Enables inlining across compilation unit boundaries.
  5–15% improvement. Longer link time (1.5–3×).

Level 2: Profile-Guided Optimization (PGO)
  Compiler uses real profile data to optimize hot paths.
  10–20% improvement. Requires representative workload.
  Two-step build: instrument → profile → optimized build.

Level 3: Post-Link Optimization (BOLT)
  Reorganizes machine code layout after linking based on profile.
  5–15% additional improvement on top of PGO (code layout).
  Requires sampling profile from production binary.
```

Each level has diminishing returns and increasing complexity. Apply them in order, and
only after you have exhausted code-level optimizations (algorithmic improvements,
data layout, SIMD). LTO + PGO on a poorly structured codebase is slower than a
well-structured codebase with no special flags.

The inliner is the most important single optimization the compiler performs: inlining
eliminates function call overhead, enables the caller to see the callee's body (enabling
constant propagation, dead code elimination, and further inlining), and improves
instruction cache locality. Every inlining decision is a tradeoff between binary size
(code cache pressure) and runtime performance. PGO improves inlining by letting the
compiler inline *only* the hot callsites, avoiding binary size growth in cold paths.

## Core Concepts

### Profile-Guided Optimization (PGO)

PGO workflow:
1. **Instrument build**: compile a special binary with profile counters embedded in every
   function and branch
2. **Profile run**: execute the instrumented binary with representative production workload
   (ideally a replay of real traffic)
3. **Optimized build**: compile using the collected profile data; the compiler now knows
   which branches are taken, call frequencies, and hot loops

What PGO changes compared to static optimization:
- **Inliner budget**: hot functions get more inlining budget; cold functions get less
- **Branch layout**: likely branches go in the fall-through path (faster on CPU pipeline)
- **Function ordering**: hot functions are placed adjacent in the binary (better instruction
  cache locality)
- **Loop unrolling**: hot loops are unrolled more aggressively
- **Register allocation**: more registers allocated to hot variables

PGO typically improves performance by 5–20% depending on workload characteristics. Branch-
heavy code (parsers, compilers, interpreters) benefits most. Highly vectorized numerical
code benefits least (the hot loops are already optimized by auto-vectorization).

### Link-Time Optimization (LTO)

Without LTO, each `.go` file or Rust crate compiles independently. A function `foo()` in
package A calling `bar()` in package B cannot be inlined — the compiler doesn't see both
at the same time. LTO defers final code generation to link time, giving the linker/optimizer
visibility into the entire program.

In Go, LTO is implicit — the gc compiler already has whole-program visibility (all packages
compile in one invocation). This is why Go doesn't have a separate LTO flag.

In Rust, each crate compiles to LLVM bitcode. LTO at link time merges all bitcode files
and runs LLVM's optimization passes on the whole program. This enables cross-crate inlining,
cross-crate constant propagation, and dead code elimination across crate boundaries.

Thin LTO is a practical middle ground: only the "hot" cross-crate calls are inlined
(determined by a call graph analysis), not all code. Build time with thin LTO is 20–50%
longer vs no LTO; fat LTO is 2–5× longer.

### BOLT (Binary Optimization and Layout Tool)

BOLT is a post-link optimizer that reorganizes the executable's code layout based on a
sampling profile from a running binary (via `perf record`). BOLT:
- Moves hot functions to the beginning of the `.text` section (improving instruction
  cache utilization)
- Splits hot and cold code within functions (keeping the hot path's instructions contiguous)
- Reorders basic blocks within functions to reduce branch mispredictions

BOLT runs on the final linked binary, making it independent of the source language and
compiler. It requires a `perf.data` file from a production run of the binary.

Facebook (Meta) reported 5–12% performance improvement on their production workloads from
BOLT, on top of existing PGO. Chrome ships PGO + BOLT in official builds.

### Inliner Budgets

Every inlining decision consumes the function's code budget. The compiler estimates the
size increase from inlining and compares it to a budget. Exceeding the budget means the
function is not inlined.

**Go**: The inliner budget is measured in "nodes" (AST complexity). The default budget
allows functions up to ~80 nodes. `-gcflags="-l=4"` raises the budget (level 4 = maximum).
`-gcflags="-l"` disables all inlining (useful for accurate profiling attribution).
`//go:noinline` prevents a specific function from being inlined. `//go:inline` is not
supported — Go has no inline hint, only `//go:noescape` and `//go:nosplit`.

**Rust**: `#[inline]` hints to LLVM to consider inlining. `#[inline(always)]` forces
inlining regardless of size (use sparingly — large inlined functions bloat call sites).
`#[inline(never)]` prevents inlining (useful for benchmarking or preventing code size growth).
LLVM's inliner has its own cost model; `#[inline]` hints are just that — hints.

### Devirtualization and Monomorphization

**Go interfaces**: A call through a Go interface (`var r io.Reader`) is an indirect call:
the vtable is looked up, then the function pointer is called. Two pointer dereferences.
If the concrete type is known at compile time (e.g., always a `*bytes.Reader`), the
compiler can devirtualize: replace the indirect call with a direct call to
`(*bytes.Reader).Read`. PGO enables speculative devirtualization — "this interface call
has been a `*bytes.Reader` in 98% of profile samples; compile a fast path for that type."

**Rust generics / monomorphization**: Rust generics are monomorphized at compile time —
each instantiation of `fn process<T: Trait>` generates a separate copy of the function
for each concrete type. This eliminates virtual dispatch entirely (no vtable at runtime)
but increases binary size. Large binaries may see increased instruction cache pressure
that partially offsets the devirtualization benefit. `Box<dyn Trait>` (trait objects) uses
dynamic dispatch like Go interfaces; `impl Trait` in function positions uses static
dispatch (monomorphized).

## Implementation: Go

```go
// go.mod for this example:
// module example.com/pgo-demo
// go 1.21

package main

// --- Profile-Guided Optimization in Go 1.21+ ---
//
// Step 1: Build the normal binary (no PGO)
//   go build -o myapp .
//
// Step 2: Run with pprof CPU profile enabled
//   ./myapp  (ensure it runs with pprof HTTP endpoint active)
//   # OR: use -cpuprofile flag
//   ./myapp -cpuprofile=cpu.pprof
//
//   # OR: collect from running server
//   curl -o cpu.pprof http://localhost:6060/debug/pprof/profile?seconds=30
//
// Step 3: Build with PGO using the profile
//   go build -pgo=cpu.pprof -o myapp-pgo .
//
// Step 4: Compare
//   go test -bench=. -benchtime=10s -count=5 ./... > before.txt
//   # build with -pgo and run benchmarks:
//   go test -pgo=cpu.pprof -bench=. -benchtime=10s -count=5 ./... > after.txt
//   benchstat before.txt after.txt
//
// go build -pgo=auto will search for a file named 'default.pgo' in the main package
// directory and use it if found. This is the recommended workflow for production builds:
// - Collect a production profile
// - Rename it to default.pgo and commit it to the repository
// - CI/CD builds automatically use it

import (
	"fmt"
	"io"
	"net/http"
	_ "net/http/pprof"
	"strings"
)

// --- Demonstrating interface devirtualization ---
//
// Go's PGO enables speculative devirtualization for interface calls.
// This function calls r.Read through an interface — normally an indirect call.
// After PGO, if 95% of calls use *strings.Reader, the compiler inlines that path.

func countLines(r io.Reader) (int, error) {
	buf := make([]byte, 4096)
	count := 0
	for {
		n, err := r.Read(buf) // interface call — PGO may devirtualize this
		for _, b := range buf[:n] {
			if b == '\n' {
				count++
			}
		}
		if err == io.EOF {
			return count, nil
		}
		if err != nil {
			return count, err
		}
	}
}

// This is the workload that generates the profile.
// Run this frequently during the profiling window.
func simulateWorkload() {
	text := strings.Repeat("hello world\n", 10000)
	r := strings.NewReader(text)
	n, err := countLines(r)
	if err != nil {
		panic(err)
	}
	_ = n
}

// --- Inliner behavior ---
//
// Check whether a function is being inlined:
//   go build -gcflags="-m" ./...
//   go build -gcflags="-m=2" ./...  (more detail)
//
// The output will show:
//   ./main.go:42:6: inlining call to hotFunction
//   ./main.go:55:6: hotFunction too complex: cost 150 exceeds budget 80
//
// To force inlining of a specific function (rarely needed — prefer profiling):
//   go build -gcflags="-l=4" ./...  (max budget)
//
// To disable all inlining (for profiler accuracy):
//   go build -gcflags="-l" ./...

// Small function — likely to be inlined (cost < 80):
func add(a, b int) int {
	return a + b // cost: ~2 nodes; always inlined
}

// Larger function — may or may not be inlined:
func processItem(item []byte) int {
	total := 0
	for _, b := range item {
		total += int(b)
	}
	return total
	// cost: ~30–40 nodes (loop + range); inlined at default budget
}

// Complex function — not inlined at default budget:
func complexProcessor(data [][]byte) []int {
	results := make([]int, len(data))
	for i, item := range data {
		if len(item) == 0 {
			continue
		}
		// Multiple control flow + function calls = high node cost
		results[i] = processItem(item)
	}
	return results
	// cost: > 80 nodes; not inlined; use -gcflags="-m" to confirm
}

// Check inliner output:
//   go build -gcflags="-m" .
// Expected:
//   ./main.go: inlining call to add
//   ./main.go: inlining call to processItem
//   ./main.go: complexProcessor: function too complex to inline

func main() {
	// Start pprof server for profile collection
	go func() {
		fmt.Println("pprof on :6060")
		_ = http.ListenAndServe(":6060", nil)
	}()

	// Simulate workload (this is what you'd run before collecting the PGO profile)
	for i := 0; i < 100; i++ {
		simulateWorkload()
	}

	fmt.Println("=== Go PGO Workflow ===")
	fmt.Println("1. Run binary with workload, collect: curl http://localhost:6060/debug/pprof/profile?seconds=30 > default.pgo")
	fmt.Println("2. Rebuild: go build -pgo=auto .")
	fmt.Println("3. Verify inliner decisions: go build -pgo=auto -gcflags=-m .")
}
```

### Go-specific Considerations

**`default.pgo` convention**: Go 1.21 introduced the convention that `go build -pgo=auto`
looks for `default.pgo` in the main package directory. The recommended workflow: commit
`default.pgo` to the repository and CI builds will use it automatically. Refresh it
periodically by collecting a new production profile.

**PGO scope**: Go's PGO is whole-program — it can inline across all packages, including
the standard library and third-party dependencies, when the profile shows a hot call to
an external function. This is one of Go's advantages over Rust PGO, which is typically
scoped to crate boundaries without LTO.

**`-gcflags` scope**: `-gcflags="pattern=flags"` applies flags selectively.
`-gcflags="./...=-m"` applies `-m` to all packages. `-gcflags="main=-l"` disables
inlining only in the `main` package. Use pattern syntax to target specific packages.

**PGO improvement magnitude**: The Go team reports 2–14% improvement from PGO on typical
HTTP server workloads. Branch-heavy workloads (parsers, query engines) see higher gains.
Tight numerical loops see smaller gains. Measure for your specific workload.

## Implementation: Rust

```rust
// Cargo.toml for this example:
// [profile.release]
// lto = "thin"        # thin LTO: cross-crate inlining of hot paths
// opt-level = 3       # default for release
// codegen-units = 1   # required for PGO; disables parallel codegen
// panic = "abort"     # reduces binary size, removes unwinding overhead
//
// For fat LTO (maximum optimization, longest link time):
// lto = true          # or lto = "fat"

// --- Rust LTO: Cargo.toml configurations ---
//
// Thin LTO (recommended for production):
//   [profile.release]
//   lto = "thin"
//   codegen-units = 16  # thin LTO works with parallel codegen
//
// Fat LTO (maximum, slowest build):
//   [profile.release]
//   lto = true
//   codegen-units = 1

// --- Rust PGO Workflow ---
//
// Step 1: Build with instrumentation
//   RUSTFLAGS="-Cprofile-generate=/tmp/pgo-data" cargo build --release
//
// Step 2: Run with representative workload
//   ./target/release/my_binary --workload production_trace.json
//   # Multiple runs accumulate profile data in /tmp/pgo-data/
//
// Step 3: Merge profile data (requires llvm-profdata)
//   llvm-profdata merge -output=/tmp/merged.profdata /tmp/pgo-data/*.profraw
//
// Step 4: Build with profile data
//   RUSTFLAGS="-Cprofile-use=/tmp/merged.profdata" cargo build --release
//
// Step 5: Verify (optional — check binary size and function ordering)
//   nm -S ./target/release/my_binary | head -20
//   # Hot functions should appear at low addresses (BOLT also does this)

// --- Inlining hints ---

// #[inline]: hint to LLVM to inline this function at call sites.
// LLVM may ignore the hint if it estimates the cost too high.
#[inline]
fn add(a: i32, b: i32) -> i32 {
    a + b
}

// #[inline(always)]: force inlining regardless of cost.
// Use sparingly — large functions will bloat every call site.
// Justified when: the function has a single simple fast path,
// or when profiling confirms the indirect call overhead dominates.
#[inline(always)]
fn fast_abs(x: i32) -> i32 {
    if x < 0 { -x } else { x }
}

// #[inline(never)]: prevent inlining.
// Useful when: measuring the call overhead itself, or preventing
// code duplication in binary size-sensitive builds.
#[inline(never)]
fn cold_path_handler(err: &str) -> ! {
    panic!("unexpected error: {}", err)
}

// --- Monomorphization vs dynamic dispatch ---

// Static dispatch (monomorphized): zero runtime overhead, larger binary.
// LLVM can inline and optimize specific to the concrete type.
fn process_static<T: std::io::Read>(reader: &mut T) -> usize {
    let mut buf = [0u8; 4096];
    let mut total = 0;
    loop {
        match reader.read(&mut buf) {
            Ok(0) => break,
            Ok(n) => total += n,
            Err(e) if e.kind() == std::io::ErrorKind::Interrupted => continue,
            Err(_) => break,
        }
    }
    total
    // Calling this with &mut File generates a copy optimized for File's read method.
    // Calling it with &mut TcpStream generates a different copy for TcpStream.
    // Binary size grows with each distinct T, but each copy is maximally optimized.
}

// Dynamic dispatch: single function, vtable at runtime, no monomorphization.
// Useful for heterogeneous collections or when binary size matters more than speed.
fn process_dynamic(reader: &mut dyn std::io::Read) -> usize {
    let mut buf = [0u8; 4096];
    let mut total = 0;
    loop {
        match reader.read(&mut buf) {
            Ok(0) => break,
            Ok(n) => total += n,
            Err(e) if e.kind() == std::io::ErrorKind::Interrupted => continue,
            Err(_) => break,
        }
    }
    total
    // Each call dereferences the vtable to find reader.read — two pointer indirections.
    // PGO can speculatively devirtualize if one concrete type dominates in the profile.
}

// --- BOLT integration ---
//
// BOLT requires a perf profile, not an LLVM profile. The workflow:
//
// Step 1: Build with debug symbols in release mode
//   RUSTFLAGS="-g" cargo build --release
//
// Step 2: Collect perf profile
//   perf record -e cycles:u -j any,u -a -o perf.data ./target/release/my_binary
//
// Step 3: Convert perf data to BOLT format
//   perf2bolt -p perf.data -o perf.fdata ./target/release/my_binary
//
// Step 4: Apply BOLT optimizations
//   llvm-bolt ./target/release/my_binary -o ./target/release/my_binary.bolt \
//     -data=perf.fdata -reorder-blocks=ext-tsp -reorder-functions=hfsort+ \
//     -split-functions -split-all-cold -split-eh -dyno-stats
//
// Step 5: Verify improvement
//   perf stat ./target/release/my_binary         # before BOLT
//   perf stat ./target/release/my_binary.bolt    # after BOLT

fn main() {
    // Demonstrate static vs dynamic dispatch
    let data = b"hello world\nhow are you\n";

    let mut reader1 = std::io::Cursor::new(data);
    let n1 = process_static(&mut reader1); // monomorphized for Cursor<&[u8]>

    let mut reader2 = std::io::Cursor::new(data);
    let n2 = process_dynamic(&mut reader2); // dynamic dispatch

    assert_eq!(n1, n2, "Both should read the same bytes");
    println!("Bytes read: {}", n1);

    // Inline hint demonstration
    let sum: i32 = (0..1000).map(|x| add(x, 1)).sum(); // add is inlined
    let abs_sum: i32 = (-500..500).map(|x| fast_abs(x)).sum(); // always inlined
    println!("Sum: {}, AbsSum: {}", sum, abs_sum);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_process_static_vs_dynamic() {
        let data = b"test\ndata\n";
        let mut r1 = std::io::Cursor::new(data);
        let mut r2 = std::io::Cursor::new(data);
        assert_eq!(process_static(&mut r1), process_dynamic(&mut r2));
    }
}
```

### Rust-specific Considerations

**`codegen-units = 1`**: By default, Rust splits each crate into multiple parallel
compilation units to speed up builds. This prevents cross-unit inlining within a crate.
For PGO and maximum optimization, set `codegen-units = 1` in the release profile.
This increases build time by 20–50% but enables LLVM to see the entire crate at once.

**`panic = "abort"`**: The default Rust panic behavior (`unwind`) generates stack unwind
tables that add ~5% to binary size and slight runtime overhead on stack operations.
`panic = "abort"` disables unwinding — panics abort immediately. This is appropriate for
servers where a panic is non-recoverable anyway. It also enables better inlining because
LLVM doesn't need to track unwind paths.

**`cargo-pgo` crate**: The `cargo-pgo` crate (install: `cargo install cargo-pgo`) automates
the PGO workflow: `cargo pgo build`, `cargo pgo run`, `cargo pgo optimize`. It handles
the `llvm-profdata merge` step automatically and simplifies the three-step build workflow.

**LTO and build times**: Fat LTO on a large Rust project can increase link time from
seconds to minutes. For incremental development, use `lto = "thin"` or `lto = false` and
only enable fat LTO in the final release build. Thin LTO in CI is a good practical
compromise: 50–80% of fat LTO's performance benefit at 2–3× the link time of no LTO.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| LTO equivalent | Implicit (whole-program compilation) | `lto = "thin"` or `lto = true` in Cargo.toml |
| PGO introduced | Go 1.20 (preview), 1.21 (production) | Rust 1.52 (stable via LLVM PGO flags) |
| PGO workflow | `go build -pgo=profile.pprof` | `RUSTFLAGS="-Cprofile-generate=..."` + `profile-use=...` |
| PGO profile format | Go pprof format | LLVM `.profraw` / `.profdata` format |
| Default PGO | `go build -pgo=auto` uses `default.pgo` | Manual; `cargo-pgo` automates |
| Inliner control | `-gcflags="-l=N"` (budget level) | `#[inline]`, `#[inline(always)]`, `#[inline(never)]` |
| Inline inspection | `go build -gcflags="-m"` | `RUSTFLAGS="--emit=llvm-ir"` + inspect IR |
| BOLT support | Yes (BOLT is language-agnostic) | Yes (BOLT is language-agnostic) |
| Dynamic dispatch devirt | PGO speculative devirtualization (Go 1.21+) | LLVM devirtualizes with PGO profile |
| Monomorphization | No (only interface dispatch) | Yes (generics generate per-type copies) |
| Reported PGO gain | 2–14% on HTTP server workloads | 5–20% on typical workloads |

## Production War Stories

**Go standard library's `default.pgo`**: The Go project has been publishing `default.pgo`
profiles for the Go standard library itself as part of the Go 1.21+ releases. These
profiles are generated from running the Go compiler's own test suite. This means that
building any Go program with Go 1.21+ gets PGO benefits on the standard library calls
(JSON parsing, HTTP handling, etc.) without any user action.

**Chrome and LLVM BOLT (Google, 2021)**: Chrome ships with both PGO and BOLT applied.
Google reported that BOLT on Chrome improved startup time by 3% and peak throughput by
8% on top of existing PGO. The BOLT improvement comes primarily from code layout — the
hot paths of V8 (Chrome's JavaScript engine) and the renderer are laid out contiguously,
reducing instruction cache misses.

**Rust in Firefox (Mozilla, 2021)**: Mozilla's Firefox uses PGO for its Rust components
(SpiderMonkey JavaScript engine's Rust bindings and various encoding libraries). They
reported 5–8% improvement in their JavaScript benchmark suite (JetStream2) from PGO.
The improvement was larger for their C++ components (10–15%), partly because the Rust
components already had monomorphization providing some of PGO's benefits.

**ClickHouse PGO + LTO (Yandex, 2020)**: ClickHouse enabled fat LTO and PGO for their
production builds. The combined improvement was 15% on analytical query throughput.
LTO enabled cross-crate inlining of their expression evaluation primitives (which are
implemented as templates). PGO then further optimized the inlining decisions for the
hot query patterns in their benchmark suite.

## Numbers That Matter

| Optimization | Typical Improvement | Build Time Cost |
|---|---|---|
| `opt-level=3` vs `opt-level=0` | 2–10× | Moderate |
| Thin LTO (Rust) | 5–15% over `opt-level=3` | 50–100% longer link |
| Fat LTO (Rust) | 8–20% over `opt-level=3` | 2–5× longer link |
| PGO (Go, HTTP server) | 2–14% | 2× (instrument + profile + build) |
| PGO (Rust, typical) | 5–20% | 2× (instrument + profile + build) |
| BOLT (on top of PGO) | 3–12% | Perf collection + BOLT pass |
| `#[inline(always)]` on a 5-line fn | 1–5% at hot call sites | 0 (compile time only) |
| `codegen-units=1` vs default | 2–8% | 20–50% longer compile |
| `panic=abort` | 0–3% | 0 |

## Common Pitfalls

**Using an unrepresentative profile for PGO**: PGO makes the binary optimal for the
workload in the profile. If the profile is from a load test with synthetic data and
production traffic has different access patterns, PGO may actually make production worse
by misaligning branch predictions and inlining decisions. Collect profiles from production
traffic or high-fidelity replay.

**Enabling LTO without measuring**: LTO increases link time significantly. For a service
that builds in 30 seconds, this is irrelevant. For a service that builds in 10 minutes,
fat LTO may be impractical for development. Use thin LTO as a reasonable default; reserve
fat LTO for release builds with clear performance requirements.

**Over-using `#[inline(always)]`**: Forcing inlining of large functions causes code bloat
at every call site. A 200-line function with `#[inline(always)]` that is called from 50
places creates 200 × 50 = 10,000 lines of duplicated machine code. This will trash the
instruction cache and cancel any benefit of eliminating the call overhead. Use `#[inline]`
(not `always`) for anything larger than 10 lines.

**Expecting PGO to fix algorithmic problems**: PGO optimizes branch layout, inlining
decisions, and loop unrolling. It does not change an O(n²) algorithm to O(n log n). If
the profiler shows 60% of time in a sort routine, PGO will inline the comparator more
aggressively — but you are still doing 60% of time in sorting. Fix the algorithm first.

**Not using `codegen-units=1` with Rust PGO**: Rust PGO without `codegen-units=1` may
produce fragmented profiles and miss cross-unit inlining opportunities. The PGO step
itself (the instrumented run) works fine with multiple codegen units, but the optimization
pass needs single-unit compilation to apply all inlining decisions correctly. Always set
`codegen-units=1` in the PGO-optimized build profile.

## Exercises

**Exercise 1** (30 min): Enable inliner diagnostics on a Go or Rust project you own.
In Go: `go build -gcflags="-m=1" ./...`. In Rust: check the LLVM IR for `@inline` hints
using `cargo rustc --release -- --emit=llvm-ir`. Find three functions that are not being
inlined but are called on hot paths. Determine whether the reason is size (cost) or
explicit `//go:noinline` / `#[inline(never)]`.

**Exercise 2** (2–4h): Apply PGO to a Go HTTP server. Use `net/http/pprof` to collect a
CPU profile while running a realistic load test (`hey -n 100000`). Build with
`go build -pgo=cpu.pprof`. Benchmark before and after with `testing.B` and `benchstat`.
Specifically test the code path that was hot in the profile. Verify you see improvement.

**Exercise 3** (4–8h): Apply Rust LTO to a project with multiple crates (or create one
with a library crate and a binary crate). Benchmark with `criterion` under: (a) default
settings (no LTO), (b) `lto = "thin"`, (c) `lto = true`. Compare build times and
benchmark results. Identify which cross-crate function calls got inlined by examining
the LLVM IR (`cargo rustc --release -- --emit=llvm-ir`).

**Exercise 4** (8–15h): Complete the full PGO workflow for a Rust binary: (1) instrument
build with `-Cprofile-generate`, (2) run realistic workload to collect raw profiles,
(3) merge with `llvm-profdata`, (4) optimize build with `-Cprofile-use`, (5) benchmark
before/after with criterion. Then apply BOLT on top: collect `perf.data`, run BOLT,
benchmark. Document the improvement at each step: base → PGO → PGO+BOLT. Identify
which optimization contributes more for your specific workload.

## Further Reading

### Foundational Papers

- Chang, Mahlke, Hwu — ["Using Profile Information to Assist Classic Code
  Optimizations"](https://dl.acm.org/doi/10.1002/spe.4380210204) (Software: Practice and
  Experience, 1991) — original paper establishing PGO as a principled optimization
- Pettis, Hansen — ["Profile Guided Code Positioning"](https://dl.acm.org/doi/10.1145/93542.93550)
  (PLDI 1990) — foundational paper on using profiles for function layout (BOLT is a modern
  industrial application of this idea)

### Books

- Cooper, Torczon — *Engineering a Compiler* (3rd ed., 2022) — Chapters 9–10 cover
  inlining, LTO, and profile-guided optimization from a compiler construction perspective
- Alfred V. Aho et al. — *Compilers: Principles, Techniques, and Tools* ("Dragon Book",
  2nd ed.) — Chapter 9 covers machine-independent optimizations including inlining

### Blog Posts

- [Profile-Guided Optimization in Go 1.21](https://go.dev/blog/pgo) — Go official blog;
  the canonical introduction with benchmarks
- [Rust PGO: How I Achieved a 30% Speedup on Rust Code](https://blog.logrocket.com/rust-performance-guide/)
  — practical PGO walkthrough with `cargo-pgo`
- [LLVM BOLT documentation](https://github.com/llvm/llvm-project/blob/main/bolt/README.md) —
  complete guide to BOLT's optimization passes and workflow
- [How PGO Works in LLVM](https://llvm.org/docs/HowToBuildWithPGO.html) — LLVM's official
  PGO guide; the authoritative reference for Rust's PGO workflow since it uses LLVM

### Tools Documentation

- [`cargo-pgo`](https://github.com/Kobzol/cargo-pgo) — automates the Rust PGO workflow
- [LLVM BOLT](https://github.com/llvm/llvm-project/tree/main/bolt) — post-link optimizer
- [`go build -pgo`](https://pkg.go.dev/cmd/go#hdr-Compile_packages_and_dependencies) —
  Go PGO build flag documentation
- [`llvm-profdata`](https://llvm.org/docs/CommandGuide/llvm-profdata.html) — tool for
  merging and inspecting LLVM profile data
