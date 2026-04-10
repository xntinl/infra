<!--
type: reference
difficulty: advanced
section: [03-concurrency-and-parallelism]
concepts: [SIMD, data-parallelism, vectorization, SSE, AVX, AVX-512, auto-vectorization, std-simd, parallel-scan, prefix-sum, throughput-vs-latency]
languages: [go, rust]
estimated_reading_time: 60-90 min
bloom_level: analyze
prerequisites: [cache-lines, memory-alignment, CPU-pipeline-basics]
papers: [Blelloch 1990 "Prefix Sums and Their Applications", Larsen & Procter 2019 "OpenCL SYCL and std::simd"]
industry_use: [NumPy, TensorFlow, BLAS, ClickHouse, DuckDB, Polars, Go-compiler-vectorizer, LLVM-SLP-vectorizer]
language_contrast: high
-->

# SIMD and Data Parallelism

> SIMD turns a single CPU core into a mini-parallel-computer: one instruction processes 4, 8, or 16 values simultaneously, achieving data parallelism without thread synchronization.

## Mental Model

Task parallelism assigns different *tasks* to different threads. Data parallelism applies the *same operation* to different *data elements* simultaneously. SIMD (Single Instruction Multiple Data) is data parallelism within a single CPU core. Where a scalar add takes one instruction to produce one result, a 256-bit AVX2 `vpaddd` takes one instruction to produce eight 32-bit results simultaneously — a potential 8x throughput improvement with no threads, no synchronization, and no coherency overhead.

SIMD's concurrency significance: it multiplies single-core throughput without any of the complexity of multi-threading. Before reaching for goroutines or Rayon to parallelize an array computation, ask whether SIMD can achieve the target throughput on a single core. A 256-bit AVX2 float32 add throughput is ~6 GFlops/s on a modern Intel core. Eight such cores gives ~48 GFlops/s. A naive scalar loop on the same 8 cores achieves ~6 GFlops/s total. SIMD first; threading second.

The path to SIMD in production: **auto-vectorization** is the zero-effort path — the compiler (LLVM for Rust, the Go compiler for simple patterns) detects loops over arrays and emits SIMD instructions automatically, subject to alignment, aliasing, and dependency constraints. **Explicit SIMD** (using `std::simd` in Rust or assembly in Go) is the escape hatch when the compiler fails to vectorize or produces suboptimal code. The decision rule: always check compiler output (via Compiler Explorer / `objdump -d`) before writing explicit SIMD; compilers are often better than human-written SIMD on complex patterns.

The **parallel scan** (prefix sum) is the canonical parallel algorithm that benefits from both SIMD and multi-threading: it is the building block for parallel sort (radix sort uses prefix scans to compute output positions), stream compaction, and histogram computation. Understanding parallel scan means understanding how to compose SIMD data parallelism with inter-core parallelism.

## Core Concepts

### SIMD Register Width and Instruction Sets

| ISA | Width | Integer | Float32 | Float64 |
|-----|-------|---------|---------|---------|
| SSE2 (x86, ~2001) | 128-bit | 4×i32 / 16×i8 | 4×f32 | 2×f64 |
| AVX2 (x86, Haswell 2013) | 256-bit | 8×i32 / 32×i8 | 8×f32 | 4×f64 |
| AVX-512 (x86, Skylake-X 2017) | 512-bit | 16×i32 | 16×f32 | 8×f64 |
| NEON (ARM, ARMv7+) | 128-bit | 4×i32 / 16×i8 | 4×f32 | — |
| SVE (ARM, ARMv8.2+) | variable width | scalable | scalable | scalable |

AVX-512 has a complex history: thermal throttling on desktop CPUs (using AVX-512 lowers clock speed on some Intel chips), instruction frequency (Linux kernel avoids AVX-512 in some paths), and inconsistent hardware support made it less universally applicable than AVX2. For portable code, AVX2 is the practical maximum on x86.

### Auto-Vectorization Prerequisites

A loop is auto-vectorizable when the compiler can prove:
1. **No loop-carried dependencies**: each iteration's output does not depend on the previous iteration's output through non-associative operations.
2. **No aliasing**: the input and output arrays do not overlap (use `restrict` in C; no equivalent in Go/Rust, but Rust's borrow checker proves non-aliasing at compile time).
3. **Sufficient trip count**: vectorization has overhead; short loops (< ~4 iterations) are not worth vectorizing.
4. **Supportable data types**: the operation and type must have a SIMD analog in the target ISA.

Common auto-vectorization killers: function calls inside the loop (inline first), `if` inside the loop (use branchless min/max patterns), complex iterator chains with non-trivial state.

### Parallel Scan (Prefix Sum)

The prefix scan of an array `a` produces output `s` where `s[i] = a[0] + a[1] + ... + a[i-1]` (exclusive) or including `a[i]` (inclusive). Sequential implementation is O(N) work. The Blelloch parallel scan achieves O(N) work, O(log N) span:

**Up-sweep (reduce phase)**: Build a reduction tree. At each level, `a[2k+1] += a[2k]`. After log(N) levels, `a[N-1]` is the total sum.

**Down-sweep phase**: Set `a[N-1] = 0`. Propagate down: at each level, `temp = a[left]; a[left] = a[right]; a[right] = temp + a[right]`. After log(N) levels, `a[i]` contains the exclusive prefix sum.

Combined with SIMD within each level (each level processes N/2 independent pairs), the parallel scan achieves near-peak throughput on both SIMD and multi-core parallelism simultaneously.

## Implementation: Go

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

// --- Auto-vectorization: the compiler's SIMD ---
//
// The Go compiler can auto-vectorize simple loops over []float32 / []float64.
// This loop will be compiled to SSE2/AVX instructions on x86-64 by the
// Go compiler (check with: GOAMD64=v3 go build -gcflags="-d=ssa/lower/debug=1").
//
// Race detector: clean (single-goroutine, purely data-parallel).
// No synchronization needed — each element is independent.

func scalarSum(a []float32) float32 {
	var sum float32
	for _, v := range a {
		sum += v
	}
	return sum
}

// The Go compiler vectorizes this loop on supported targets.
// The key: no loop-carried dependencies beyond the accumulator,
// which the compiler handles via vectorized horizontal reduction.
func vectorizedSumHint(a []float32) float32 {
	// Ensure alignment (Go allocates slices 8-byte aligned by default;
	// for AVX we want 32-byte alignment).
	// In practice, the Go compiler handles unaligned loads via VMOVUPS.
	var sum float32
	for i := 0; i < len(a); i++ {
		sum += a[i] // compiler will emit VADDSS/VADDPS
	}
	return sum
}

// --- Explicit SIMD via assembly for critical paths ---
//
// For the highest performance on Go, critical SIMD paths can be written
// in Go assembly (.s files). This example shows the function signature
// and the expected assembly interface. The actual assembly is below as a comment
// to illustrate the pattern without requiring an .s file in this reference.
//
// In a real package, this would be split into:
//   sum_amd64.go:   func dotProductAVX(a, b []float32) float32
//   sum_amd64.s:    TEXT ·dotProductAVX(SB), ...

// dotProductScalar is the portable fallback.
func dotProductScalar(a, b []float32) float32 {
	if len(a) != len(b) {
		panic("length mismatch")
	}
	var sum float32
	for i := range a {
		sum += a[i] * b[i] // compiler may vectorize with FMA (fused multiply-add)
	}
	return sum
}

// Assembly for AVX-based dot product (illustrative; requires _amd64.s file):
//
// TEXT ·dotProductAVX(SB),NOSPLIT,$0
//     MOVQ    a_base+0(FP), SI    // a.ptr
//     MOVQ    b_base+24(FP), DI   // b.ptr
//     MOVQ    a_len+8(FP), CX     // len
//     VXORPS  Y0, Y0, Y0          // accumulator = 0
//     SHRQ    $3, CX              // CX /= 8 (process 8 float32 per iteration)
// loop:
//     VMOVUPS (SI), Y1            // load 8 float32 from a
//     VMOVUPS (DI), Y2            // load 8 float32 from b
//     VFMADD231PS Y1, Y2, Y0      // Y0 += Y1 * Y2 (FMA)
//     ADDQ    $32, SI
//     ADDQ    $32, DI
//     DECQ    CX
//     JNZ     loop
//     VEXTRACTF128 $1, Y0, X1
//     VADDPS  X0, X1, X0          // horizontal sum ...
//     MOVSS   X0, ret+48(FP)
//     VZEROUPPER
//     RET

// --- Parallel scan (prefix sum) using goroutines + SIMD accumulation ---
//
// Step 1: partition the array into P chunks (P = GOMAXPROCS)
// Step 2: compute local prefix sum within each chunk (SIMD-friendly)
// Step 3: compute prefix sum of chunk totals (sequential, tiny)
// Step 4: add chunk offsets to each chunk's local sums (SIMD-friendly)
//
// Total work: O(N). Span: O(N/P + P). Speedup: P for large N.

func parallelPrefixSum(input []int64) []int64 {
	n := len(input)
	output := make([]int64, n)
	nProcs := runtime.GOMAXPROCS(0)

	if n < nProcs*4 {
		// Sequential for small inputs — parallelism overhead exceeds benefit.
		return sequentialPrefixSum(input)
	}

	chunkSize := (n + nProcs - 1) / nProcs
	chunkSums := make([]int64, nProcs)

	// Phase 1: parallel local prefix sums.
	var wg sync.WaitGroup
	for p := 0; p < nProcs; p++ {
		start := p * chunkSize
		end := min(start+chunkSize, n)
		if start >= n {
			break
		}
		wg.Add(1)
		go func(p, start, end int) {
			defer wg.Done()
			var localSum int64
			for i := start; i < end; i++ {
				localSum += input[i]
				output[i] = localSum // local prefix sum (0-based per chunk)
			}
			chunkSums[p] = localSum // total sum of this chunk
		}(p, start, end)
	}
	wg.Wait()

	// Phase 2: prefix sum of chunk totals (sequential, nProcs elements).
	for p := 1; p < nProcs; p++ {
		chunkSums[p] += chunkSums[p-1]
	}

	// Phase 3: add chunk offsets to local prefix sums (parallel).
	for p := 1; p < nProcs; p++ {
		start := p * chunkSize
		end := min(start+chunkSize, n)
		if start >= n {
			break
		}
		offset := chunkSums[p-1]
		wg.Add(1)
		go func(start, end int, offset int64) {
			defer wg.Done()
			for i := start; i < end; i++ {
				output[i] += offset // the compiler may vectorize this loop
			}
		}(start, end, offset)
	}
	wg.Wait()

	return output
}

func sequentialPrefixSum(input []int64) []int64 {
	output := make([]int64, len(input))
	var running int64
	for i, v := range input {
		running += v
		output[i] = running
	}
	return output
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// --- Memory alignment for SIMD ---
//
// SIMD instructions work on aligned memory (32-byte alignment for AVX,
// 16-byte for SSE). Go's allocator provides 8-byte alignment by default.
// For maximum SIMD efficiency, allocate with custom alignment.

func alignedAllocFloat32(n int) []float32 {
	// Allocate with 32-byte alignment for AVX2.
	// Go's unsafe allows this but is not idiomatic; use in tight hot paths only.
	size := n*4 + 32
	buf := make([]byte, size)
	ptr := uintptr(unsafe.Pointer(&buf[0]))
	aligned := (ptr + 31) &^ 31 // round up to 32-byte boundary
	return unsafe.Slice((*float32)(unsafe.Pointer(aligned)), n)
}

func main() {
	// Demonstrate scalar vs compiler-vectorized sum
	const N = 1_000_000
	data := make([]float32, N)
	for i := range data {
		data[i] = float32(i) * 0.001
	}

	s1 := scalarSum(data)
	s2 := vectorizedSumHint(data)
	fmt.Printf("Scalar sum: %.2f, Vectorized hint sum: %.2f\n", s1, s2)

	// Dot product
	b := make([]float32, N)
	for i := range b {
		b[i] = 1.0
	}
	dp := dotProductScalar(data, b)
	fmt.Printf("Dot product: %.2f\n", dp)

	// Parallel prefix sum
	inputInts := make([]int64, 1_000_000)
	for i := range inputInts {
		inputInts[i] = int64(i + 1)
	}
	result := parallelPrefixSum(inputInts)
	// Verify: result[999999] should be 1+2+...+1000000 = 500000500000
	expected := int64(1000000) * int64(1000001) / 2
	fmt.Printf("Prefix sum[last] = %d, expected = %d\n", result[len(result)-1], expected)
}
```

### Go-specific considerations

**Go compiler auto-vectorization**: The Go compiler (gc toolchain) performs auto-vectorization for simple loops on `[]float32`, `[]float64`, and integer slice operations. Check with `GOAMD64=v3 go build -gcflags="-d=ssa/lower/debug=1" .`. The `GOAMD64` environment variable controls the minimum ISA: `v1` (baseline x86-64), `v2` (SSE4.2), `v3` (AVX2), `v4` (AVX-512). Setting `GOAMD64=v3` enables AVX2 emission for loops that the compiler vectorizes.

**Go assembly for explicit SIMD**: For operations the compiler does not vectorize, Go assembly (`.s` files) allows hand-written SIMD. The function signature is declared in Go (as a forward declaration without a body), and the implementation is in the `.s` file using Go's assembler syntax. This approach is used in Go's `crypto/aes`, `math/big`, and `internal/cpu` packages. The downside: architecture-specific `.s` files for each target (amd64, arm64).

**`math/bits` and SIMD-friendliness**: Go's `math/bits.OnesCount64` and similar functions compile to single CPU instructions (`POPCNT` on x86) — effectively single-element SIMD. The compiler recognizes these patterns and emits the hardware instruction directly, achieving hardware-optimal performance without explicit SIMD code.

## Implementation: Rust

```rust
// Production note: for portable SIMD, add to Cargo.toml:
//   [features]
//   nightly = []
// and use #![feature(portable_simd)] on nightly.
// The stable alternative is the `wide` crate or target-specific intrinsics
// via `std::arch`.

// --- Auto-vectorization: the LLVM approach ---
//
// Rust/LLVM auto-vectorizes more aggressively than Go's compiler.
// The key: Rust's borrow checker proves non-aliasing at compile time,
// removing a major vectorization barrier (no need for restrict-equivalent).
// LLVM's SLP (Superword Level Parallelism) vectorizer handles both
// loop vectorization and straight-line code patterns.

fn scalar_sum(a: &[f32]) -> f32 {
    a.iter().sum() // LLVM will vectorize this with AVX2 if available
}

// Hint for LLVM: use target_feature attribute to require AVX2.
// This allows the compiler to emit AVX2 instructions unconditionally
// for this function (requires AVX2 at runtime or UB).
#[target_feature(enable = "avx2")]
unsafe fn sum_avx2_hint(a: &[f32]) -> f32 {
    // Safety: caller must ensure AVX2 is available (checked at runtime below).
    // LLVM will emit VADDPS in 256-bit registers for this loop.
    a.iter().sum()
}

// Runtime dispatch: check CPU features once, dispatch to optimal implementation.
fn sum_with_dispatch(a: &[f32]) -> f32 {
    // is_x86_feature_detected! checks CPUID at runtime.
    #[cfg(target_arch = "x86_64")]
    if is_x86_feature_detected!("avx2") {
        // Safety: AVX2 is available on this CPU.
        return unsafe { sum_avx2_hint(a) };
    }
    scalar_sum(a)
}

// --- std::simd portable SIMD (nightly only) ---
//
// std::simd provides a portable SIMD API that compiles to the best available
// ISA for each target. The same code compiles to SSE on x86 without AVX2,
// AVX2 on Haswell+, and NEON on ARM.
//
// Example with explicit SIMD types (requires #![feature(portable_simd)]):
//
// use std::simd::prelude::*;
//
// fn simd_dot_product(a: &[f32], b: &[f32]) -> f32 {
//     assert_eq!(a.len(), b.len());
//     let lanes = f32x8::LEN; // 8 lanes for f32x8
//     let chunks = a.len() / lanes;
//
//     // Process SIMD-width chunks.
//     let mut sum = f32x8::splat(0.0);
//     for i in 0..chunks {
//         let a_chunk = f32x8::from_slice(&a[i * lanes..]);
//         let b_chunk = f32x8::from_slice(&b[i * lanes..]);
//         sum += a_chunk * b_chunk; // element-wise multiply + accumulate
//     }
//
//     // Horizontal sum of SIMD accumulator.
//     let mut total = sum.reduce_sum();
//
//     // Handle remaining elements (tail).
//     for i in (chunks * lanes)..a.len() {
//         total += a[i] * b[i];
//     }
//     total
// }

// --- Explicit intrinsics for maximum control ---
//
// For AVX2 dot product using std::arch intrinsics.
// This is portable within x86_64 + AVX2; use runtime dispatch for other targets.

#[cfg(target_arch = "x86_64")]
mod avx2 {
    #[cfg(target_feature = "avx2")]
    use std::arch::x86_64::*;

    // SIMD dot product using AVX2 FMA (Fused Multiply-Add).
    // FMA: a * b + c in a single hardware instruction (lower rounding error, higher throughput).
    //
    // Safety: requires AVX2 + FMA support. Use is_x86_feature_detected! at call site.
    #[cfg(target_feature = "avx2")]
    pub unsafe fn dot_product_avx2(a: &[f32], b: &[f32]) -> f32 {
        debug_assert_eq!(a.len(), b.len());
        let n = a.len();
        let mut sum = _mm256_setzero_ps(); // 256-bit accumulator = 0.0×8

        let chunks = n / 8;
        for i in 0..chunks {
            // Load 8 float32 from a and b (may be unaligned; VMOVUPS handles it).
            let a8 = _mm256_loadu_ps(a.as_ptr().add(i * 8));
            let b8 = _mm256_loadu_ps(b.as_ptr().add(i * 8));
            // FMA: sum += a8 * b8 (single instruction on Haswell+)
            // _mm256_fmadd_ps requires the "fma" target feature in addition to "avx2".
            // For AVX2-only, use separate mul + add:
            let prod = _mm256_mul_ps(a8, b8);
            sum = _mm256_add_ps(sum, prod);
        }

        // Horizontal reduction: sum 8 lanes into 1 scalar.
        // _mm256_extractf128_ps: extract high 128 bits
        let sum128hi = _mm256_extractf128_ps::<1>(sum);
        let sum128lo = _mm256_castps256_ps128(sum);
        let sum128 = _mm_add_ps(sum128lo, sum128hi);
        // Now sum128 has 4 lanes. Add pairs.
        let sum64 = _mm_hadd_ps(sum128, sum128);
        let sum32 = _mm_hadd_ps(sum64, sum64);
        let total = _mm_cvtss_f32(sum32);

        // Tail: remaining elements not covered by 8-wide loop.
        let mut tail_sum = total;
        for i in (chunks * 8)..n {
            tail_sum += a[i] * b[i];
        }
        tail_sum
    }
}

// --- Parallel scan (prefix sum) with SIMD inner loop ---

fn parallel_prefix_sum(input: &[i64]) -> Vec<i64> {
    let n = input.len();
    let n_threads = num_cpus();
    let chunk_size = (n + n_threads - 1) / n_threads;

    if n < n_threads * 4 {
        return sequential_prefix_sum(input);
    }

    let mut output = vec![0i64; n];
    let mut chunk_sums = vec![0i64; n_threads];

    // Phase 1: parallel local prefix sums.
    // Each thread computes local prefix sum for its chunk.
    // Rayon would be the idiomatic choice here:
    // input.par_chunks(chunk_size).zip(output.par_chunks_mut(chunk_size)).enumerate()
    //      .for_each(|(p, (in_chunk, out_chunk))| { ... });
    //
    // Manual thread version for illustration:
    use std::sync::Arc;
    use std::thread;

    let input_arc = Arc::new(input.to_vec());
    let output_ptr = output.as_mut_ptr() as usize; // raw pointer for sharing

    let handles: Vec<_> = (0..n_threads).map(|p| {
        let inp = Arc::clone(&input_arc);
        let start = p * chunk_size;
        let end = (start + chunk_size).min(n);
        let out_ptr = output_ptr;

        thread::spawn(move || -> i64 {
            if start >= inp.len() {
                return 0;
            }
            let end = end.min(inp.len());
            let mut local_sum = 0i64;
            // Safety: each thread writes to a disjoint range of output.
            let out_slice = unsafe {
                std::slice::from_raw_parts_mut(out_ptr as *mut i64, inp.len())
            };
            for i in start..end {
                local_sum += inp[i];
                out_slice[i] = local_sum; // local (0-based) prefix sum
            }
            local_sum
        })
    }).collect();

    for (p, h) in handles.into_iter().enumerate() {
        chunk_sums[p] = h.join().unwrap();
    }

    // Phase 2: prefix sum of chunk totals (sequential, tiny).
    for p in 1..n_threads {
        chunk_sums[p] += chunk_sums[p - 1];
    }

    // Phase 3: add offsets (parallel, SIMD-vectorizable inner loop).
    // LLVM will vectorize the inner `output[i] += offset` loop with VPADDD.
    let chunk_sums_arc = Arc::new(chunk_sums);
    let out_ptr = output.as_mut_ptr() as usize;

    let handles: Vec<_> = (1..n_threads).map(|p| {
        let cs = Arc::clone(&chunk_sums_arc);
        let out_ptr = out_ptr;
        let n = n;
        let start = p * chunk_size;
        let end = (start + chunk_size).min(n);

        thread::spawn(move || {
            let offset = cs[p - 1];
            // Safety: disjoint range from phase 1 writes.
            let out_slice = unsafe {
                std::slice::from_raw_parts_mut(out_ptr as *mut i64, n)
            };
            for i in start..end {
                out_slice[i] += offset; // LLVM vectorizes: VPADDQ ymm
            }
        })
    }).collect();

    for h in handles { h.join().unwrap(); }

    output
}

fn sequential_prefix_sum(input: &[i64]) -> Vec<i64> {
    let mut output = Vec::with_capacity(input.len());
    let mut running = 0i64;
    for &v in input {
        running += v;
        output.push(running);
    }
    output
}

fn num_cpus() -> usize {
    // std::thread::available_parallelism returns the number of hardware threads.
    std::thread::available_parallelism()
        .map(|n| n.get())
        .unwrap_or(4)
}

fn main() {
    // Sum comparison
    let data: Vec<f32> = (0..1_000_000).map(|i| i as f32 * 0.001).collect();
    let s1 = scalar_sum(&data);
    let s2 = sum_with_dispatch(&data);
    println!("Scalar: {s1:.2}, Dispatched: {s2:.2}");

    // Prefix sum correctness check
    let input: Vec<i64> = (1..=1_000_000).collect();
    let result = parallel_prefix_sum(&input);
    let expected = 1_000_000i64 * 1_000_001 / 2;
    println!("Prefix sum[last] = {}, expected = {expected}", result[result.len() - 1]);

    // Throughput comparison concept (actual benchmark uses criterion):
    // scalar sum:     ~800 MB/s on a single core
    // AVX2 sum:       ~6400 MB/s on a single core (8x)
    // AVX2 + 8 cores: ~48000 MB/s (with memory bandwidth as the limit)
    println!("SIMD throughput: ~8x scalar for f32 sum on AVX2");
}
```

### Rust-specific considerations

**`#[target_feature]` vs `#[cfg(target_feature)]`**: `#[target_feature(enable = "avx2")]` is a function-level attribute that tells LLVM to assume AVX2 is available when compiling that function, enabling AVX2 code emission. The function itself is `unsafe` because calling it on a CPU without AVX2 is undefined behavior. `#[cfg(target_feature = "avx2")]` is a compile-time attribute that conditionally includes the function only when the entire compilation target has AVX2. For runtime dispatch, use `is_x86_feature_detected!` to check at runtime and call the `target_feature`-annotated function behind a safety check.

**`std::simd` (portable SIMD)**: Available on nightly Rust as `std::simd`. Provides `f32x4`, `f32x8`, `i64x4`, etc. types that compile to the best available SIMD for the target. On x86 with AVX2, `f32x8` maps to a 256-bit YMM register. On ARM with NEON, `f32x4` maps to a 128-bit SIMD register. The `wide` crate on stable provides similar functionality: `f32x8`, `i32x8`, etc. with operations that degrade gracefully when the target ISA does not support the full width.

**LLVM auto-vectorization superiority**: LLVM's auto-vectorizer is significantly more aggressive than Go's. In benchmarks, idiomatic Rust iterator chains (`iter().map(...).sum()`) are more reliably vectorized than equivalent Go range loops. The reason: Rust's borrow checker eliminates aliasing ambiguity at compile time, removing the most common vectorization barrier. Go's compiler must prove non-aliasing via alias analysis, which is less effective for complex access patterns.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Auto-vectorization | Moderate; `GOAMD64=v3` unlocks AVX2 | Aggressive; LLVM SLP vectorizer; borrow checker helps |
| Explicit SIMD API | Go assembly (`.s` files) + `unsafe` | `std::arch` intrinsics (unsafe); `std::simd` portable API (nightly) |
| Runtime ISA dispatch | `cpu.X86.HasAVX2` from `internal/cpu` | `is_x86_feature_detected!` macro |
| Portable SIMD | No standard API; assembly required | `std::simd` (nightly) or `wide` crate (stable) |
| SIMD for cryptography | `crypto/aes` uses amd64 assembly | `aes` crate uses `std::arch` intrinsics |
| Vectorization checking | `-gcflags="-d=ssa/lower/debug=1"` | `RUSTFLAGS="-C opt-level=3 -C target-cpu=native"` + Compiler Explorer |

## Production War Stories

**NumPy's SIMD transition (2020)**: NumPy introduced runtime-dispatched SIMD in 1.20 via its "SIMD" subsystem. Before this, NumPy used compile-time target features (requiring users to compile NumPy themselves for their specific CPU). The runtime dispatch approach — checking CPUID and selecting among SSE2, AVX2, and AVX-512 implementations — increased throughput by 2-4x for array operations on modern hardware without changing the Python API. The architecture mirrors the `sum_with_dispatch` pattern above. This is the standard industrial approach for SIMD in library code.

**ClickHouse and AVX-512 controversies (2022)**: ClickHouse (the OLAP database) uses AVX-512 for vectorized query processing. When running on AWS instances with AVX-512 (Skylake-X, Ice Lake), ClickHouse showed 2-3x throughput improvement for column scan operations. However, on some Intel desktop CPUs, AVX-512 instructions trigger thermal throttling that reduces clock speed by 10-15%, making AVX-512 slower than AVX2 for mixed workloads. The lesson: SIMD instructions have thermal effects. For cloud instances with a dedicated frequency governor, AVX-512 benefits are consistent; for shared or burst-capable instances, the thermal interaction requires benchmarking on the specific hardware.

**DuckDB's SIMD vectorized execution engine**: DuckDB, the in-process OLAP database, achieves its performance advantage over SQLite and PostgreSQL largely through SIMD-vectorized execution: column values are stored in fixed-size vectors (1024 elements), and each operator (filter, project, aggregate) processes an entire vector with SIMD instructions. The prefix scan is used for filtered aggregates: compute a boolean selection vector (SIMD comparison), compute its prefix sum to get output positions, then scatter results. This pattern (filter → prefix scan → scatter/gather) is the fundamental SIMD algorithm for database column operations.

**Go's `bytes.Index` and SIMD**: Go's `bytes.IndexByte` uses `PCMPISTRI` (SSE4.2 string comparison instruction) to search for a byte in a slice. This single instruction compares 16 bytes against a target in one cycle, achieving 16x throughput over a scalar byte comparison loop. The implementation is in `go/src/internal/bytealg/index_amd64.s` — a good example of how Go uses assembly for standard library hot paths while keeping the API Go-idiomatic.

## Complexity Analysis

- **SIMD width W** (typically 4-16 for f32 on AVX2/AVX-512): reduces operation count by W for perfectly vectorizable loops. Practical speedup is typically 0.5W to W due to load/store overhead, alignment costs, and tail handling.

- **Auto-vectorization conditions**: A loop of N elements is vectorized in N/W iterations (+ up to W-1 scalar iterations for the tail). Overhead: 1-2 setup instructions. Break-even: typically N ≥ 8-16 for auto-vectorization to be profitable.

- **Parallel scan work-span**: W₁ = O(N) (total operations), T∞ = O(log N) (depth). On P processors with SIMD width W: expected time O(N/(P*W) + log N). For N=10⁷, P=8, W=8: O(10⁷/64 + 23) ≈ 156,273 operations. Sequential: O(10⁷). Speedup: ~64x theoretical.

- **Memory bandwidth limit**: For memory-bound operations (array sum, prefix sum with large N), the throughput is limited by memory bandwidth (~50-100 GB/s on modern CPUs), not by SIMD width. Beyond a certain SIMD width, adding more vector width gives diminishing returns because the bottleneck shifts from computation to memory. For L1-resident data (< 32KB), SIMD width improvements translate to throughput improvements; for DRAM-resident data, SIMD helps only if it reduces the number of cache lines loaded (compression, filtering).

## Common Pitfalls

**1. Assuming auto-vectorization always works.** Function calls inside a loop prevent vectorization. `if` branches prevent vectorization unless they compile to predicated instructions (VCMPPS + VBLENDPS). Iterator chains with `map(f)` where `f` has complex control flow prevent vectorization. Always verify with compiler output (`objdump -d binary | grep ymm` or Compiler Explorer) that the loop you expected to be vectorized actually is.

**2. Mixing aligned and unaligned access.** AVX2 instructions have both aligned (`VMOVAPS`, requires 32-byte alignment) and unaligned (`VMOVUPS`, any alignment) variants. `VMOVUPS` is free on modern Intel hardware (Haswell+, Sandy Bridge+), so alignment is less critical than it once was. However, alignment that spans cache line boundaries (e.g., a 32-byte vector load starting at offset 48 within a 64-byte cache line) still has a penalty. For large arrays, allocate with 64-byte alignment (one full cache line).

**3. SIMD for short arrays.** The overhead of SIMD setup and tail handling is ~5-10 instructions. For arrays shorter than ~16 elements, scalar code is typically faster. Auto-vectorization guards with a minimum trip count; explicit SIMD must include its own minimum-length check.

**4. Horizontal reduction cost.** Summing 8 f32 lanes into a single f32 scalar requires ~3-4 SIMD instructions (extract, add, shuffle). This is cheap for one reduction at the end of a loop but expensive if done inside the loop (e.g., computing a running sum with a horizontal reduction on each iteration). Accumulate in SIMD registers and reduce once at the end.

**5. False vectorization via `f32::sum()`.** Floating-point addition is not associative; reordering additions changes results. Auto-vectorization reorders additions for SIMD. This means `array.iter().sum::<f32>()` on a vectorized implementation may return a different result than a sequential loop, particularly for arrays with values of very different magnitudes. For reproducible numerical results, use Kahan summation or ensure inputs are sorted by magnitude. For performance, accept the slight numerical difference.

## Exercises

**Exercise 1** (30 min): Write three implementations of array sum in Go: scalar loop, auto-vectorized hint (range loop with GOAMD64=v3), and a goroutine-parallel version. Benchmark at N = 1K, 10K, 100K, 1M, 10M. For each N, identify whether the bottleneck is compute (SIMD) or memory bandwidth. Report the L1/L2/L3/DRAM crossover points.

**Exercise 2** (2-4h): Implement a parallel Blelloch prefix scan in Rust that uses `rayon::join` for the inter-thread parallelism and explicit SIMD (using the `wide` crate's `i64x4`) for the intra-chunk work. The SIMD is used in the "add offset to chunk" phase (Phase 3): process 4 `i64` values per instruction. Benchmark against a sequential scan and a parallel scan without SIMD. Compute the achieved GFLOPS and compare against the peak memory bandwidth limit.

**Exercise 3** (4-8h): Implement an AVX2 `memchr` (find first occurrence of a byte in a slice) in Rust using `std::arch::x86_64::_mm256_cmpeq_epi8` and `_mm256_movemask_epi8`. The algorithm: load 32 bytes, compare all 32 against the target byte in a single instruction, get a 32-bit mask of matches, use `u32::trailing_zeros()` to find the first match position. Handle the tail (last < 32 bytes) separately. Benchmark against `memchr::memchr` from the `memchr` crate. Include runtime dispatch for non-AVX2 targets.

**Exercise 4** (8-15h): Implement a vectorized histogram computation in both Go and Rust. Input: a slice of u8 values (0-255). Output: a 256-element count array. The naive approach (loop + `counts[v]++`) is not vectorizable because of random-write conflicts. The SIMD approach: use 4 partial histograms (to avoid conflicts), compute partial histograms in parallel with SIMD scatter, then merge. Benchmark at N=10M bytes. Compare against `sort`+`group_by`+`count` and against a naive loop. Document the expected throughput ceiling from memory bandwidth.

## Further Reading

### Foundational Papers

- Blelloch, G. (1990). "Prefix Sums and Their Applications." *Technical Report CMU-CS-90-190* — The parallel scan paper; prefix sum as a fundamental building block.
- Fog, A. (continuously updated). "Optimizing Software in C++" — Chapter 11-13: SIMD programming. The most comprehensive practical SIMD reference. Free at agner.org.

### Books

- Fog, A. *Instruction Tables: Lists of Instruction Latencies, Throughputs, and Micro-Operation Breakdowns for Intel, AMD, and VIA CPUs* — Essential reference for understanding SIMD instruction costs. Free at agner.org.
- Patterson, D. & Hennessy, J. *Computer Organization and Design: ARM Edition* (2017) — Chapters on SIMD and vector processing.

### Production Code to Read

- `go/src/internal/bytealg/index_amd64.s` — Go standard library SSE4.2-based string search.
- `go/src/crypto/aes/asm_amd64.s` — AES-NI usage in Go's crypto package.
- DuckDB `src/execution/operator/` — Vectorized query processing; the filter-scan-aggregate pipeline.
- Polars `crates/polars-core/src/` — Rust SIMD-accelerated DataFrame operations via Arrow2.

### Talks

- "SIMD for C++ Developers" — Peter Cordes (StackOverflow famous answers) — Comprehensive SIMD mental model.
- "How to Write Fast Numerical Code" — Markus Püschel (ETH Zürich) — Systematic approach to vectorization and memory hierarchy optimization.
- "Performance Matters" — Emery Berger (Strange Loop 2019) — Profiling and optimization methodology; when SIMD is and is not the answer.
