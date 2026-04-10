<!--
type: reference
difficulty: advanced
section: [09-performance-and-optimization]
concepts: [SIMD, auto-vectorization, SSE, AVX2, AVX-512, portable-SIMD, vectorization-barriers, data-dependencies, aliasing, Go-assembly, std-simd]
languages: [go, rust]
estimated_reading_time: 90 min
bloom_level: evaluate
prerequisites: [cpu-cache-optimization, memory-bandwidth-optimization, Go-assembly-basics, unsafe-rust]
papers: [Intel AVX Programming Reference, ARM NEON Programmer's Guide]
industry_use: [zstd SIMD compression, BoringSSL AES-NI, image processing, genomics BLAST search, ClickHouse vectorized execution]
language_contrast: high
-->

# SIMD Optimization

> A single AVX2 instruction processes 32 bytes — 8 floats, 4 doubles, or 32 chars — in
> the same cycle budget as a scalar instruction processing 4 bytes. SIMD is not a micro-
> optimization; it is a multiplier on every correctly structured loop.

## Mental Model

SIMD (Single Instruction, Multiple Data) executes one instruction on a vector of data
elements simultaneously. An `ADDPS` (add packed single-precision float) on an AVX2
register adds 8 pairs of `float32` values in one instruction. For a loop summing an
array of floats, this means 8x more useful work per cycle — or equivalently, 8x more
bandwidth efficiency, since you pay the same cache miss penalty per cache line whether
you process 1 or 8 elements from it.

The key mental model: **the CPU's SIMD units are always present and always idle unless
used**. The question is not whether to use SIMD but whether the code structure allows it.
Three conditions must hold for SIMD to fire:

1. **No data dependencies between iterations**: `a[i] = a[i] + a[i-1]` cannot be
   vectorized because each iteration depends on the previous result. `a[i] = b[i] + c[i]`
   can be vectorized because all iterations are independent.

2. **No aliasing**: If the compiler cannot prove that `a` and `b` do not overlap in
   memory, it cannot vectorize `a[i] = b[i] + c[i]`. Rust's ownership rules eliminate
   aliasing by construction. In Go, the compiler uses alias analysis; pass separate slices,
   not sub-slices of the same backing array, to vectorizable functions.

3. **No branches (or predictable branches)**: A loop with `if condition { a[i] = ... }
   else { a[i] = ... }` may not vectorize. Replace conditional assignment with branchless
   alternatives (multiply by 0 or 1, mask operations) or use SIMD blend instructions.

When these conditions are met, the compiler (Go's gc compiler with SSE/AVX support, or
LLVM via Rust) will generate SIMD instructions automatically. This is auto-vectorization.
It is free, and it should be the first SIMD approach. Manual SIMD via intrinsics comes
only when auto-vectorization fails or produces suboptimal output.

The SIMD register width determines the multiplier:
- SSE2 (128 bits): 4× float32, 2× float64, 16× int8
- AVX2 (256 bits): 8× float32, 4× float64, 32× int8 — available on Intel Haswell (2013+),
  AMD Zen (2017+)
- AVX-512 (512 bits): 16× float32 — available on Intel Skylake-X, Ice Lake server (2019+)

## Core Concepts

### Auto-Vectorization Conditions

Auto-vectorization succeeds when:
- Loop has a fixed or countable iteration count
- No loop-carried dependencies across vector width
- Arrays are not aliased
- No function calls that the compiler cannot inline
- No early exits (`break` inside the vectorizable part)

Auto-vectorization fails (silently!) when any condition is violated. Always confirm
vectorization by: (a) examining generated assembly for `VMOVDQU`/`VADDPS` instructions,
or (b) running `perf stat -e fp_arith_inst_retired.256b_packed_single` and observing
non-zero count.

In Go: `go build -gcflags="-d=ssa/loop vectorize/debug=1"` shows vectorization decisions.
In Rust: `RUSTFLAGS="-C opt-level=3" cargo rustc -- --emit=asm` then search for `vmovdqu`
or `vaddps` in the `.s` file. Or use the [Compiler Explorer (godbolt.org)](https://godbolt.org).

### Vectorization Barriers

**Loop-carried dependency** (the most common barrier):
```go
// NOT vectorizable: a[i] depends on a[i-1]
for i := 1; i < n; i++ {
    a[i] = a[i] + a[i-1]
}

// Vectorizable: each element is independent
for i := 0; i < n; i++ {
    c[i] = a[i] + b[i]
}
```

**Conditional store** (may prevent vectorization):
```go
// May NOT vectorize due to conditional store
for i := 0; i < n; i++ {
    if a[i] > 0 {
        b[i] = a[i] * 2
    }
}

// Branchless — vectorizable
for i := 0; i < n; i++ {
    mask := int32(0)
    if a[i] > 0 { mask = -1 } // all ones
    b[i] = a[i] * 2 & mask
}
```

**Unknown loop count or aliasing** (prevents vectorization):
```go
// If the compiler cannot determine len(a) at compile time AND cannot prove
// a and b don't alias, it may not vectorize. Prefer slices over pointers.
```

### Go Assembly for SIMD

Go's compiler does not expose SSE/AVX intrinsics from Go code. SIMD in Go requires either:
1. Assembly files (`.s`) with the plan9 assembly syntax used by Go
2. Using `//go:linkname` to call assembly functions from Go code
3. Using CGo to call C functions that use compiler intrinsics (higher overhead)

The `golang.org/x/sys/cpu` package detects CPU feature support at runtime. The pattern:
```
if cpu.X86.HasAVX2 {
    processorAVX2(data)
} else {
    processorScalar(data)
}
```

### Rust std::simd (Portable SIMD)

Rust's `std::simd` module (stabilized in Rust 1.x, check `portable-simd` on crates.io
for stable fallback) provides portable vector types: `f32x8`, `i32x8`, `u8x32`, etc.
The Rust compiler maps these to the appropriate ISA instructions. Writing `f32x8::from_array`
and `f32x8::reduce_add` will produce AVX2 instructions on AVX2-capable CPUs and SSE4.1
instructions on older CPUs — without `unsafe` code.

For maximum control, `std::arch::x86_64` provides direct access to Intel intrinsics
(`_mm256_add_ps`, `_mm256_loadu_ps`, etc.) behind `unsafe` blocks.

## Implementation: Go

```go
package main

import (
	"fmt"
	"golang.org/x/sys/cpu"
	"testing"
	"unsafe"
)

// --- Auto-vectorization: conditions and counter-examples ---

// VECTORIZABLE: independent iterations, no aliasing (separate slices)
func addSlices(dst, a, b []float32) {
	for i := range dst {
		dst[i] = a[i] + b[i]
	}
	// go build -gcflags="-d=ssa/loop vectorize/debug=1" will report:
	// "loop vectorized" for this loop on amd64 with SSE2 support
}

// NOT VECTORIZABLE: loop-carried dependency
func prefixSum(a []float64) {
	for i := 1; i < len(a); i++ {
		a[i] += a[i-1] // a[i] depends on a[i-1]: cannot parallelize
	}
}

// NOT VECTORIZABLE (reliably): complex branch inside loop
func conditionalOp(dst, src []int32) {
	for i := range dst {
		if src[i] > 100 {
			dst[i] = src[i] * 3
		} else {
			dst[i] = src[i] + 7
		}
	}
}

// BRANCHLESS version — more likely to vectorize using blend/select instructions:
func conditionalOpBranchless(dst, src []int32) {
	for i := range dst {
		// Use Go's conditional expression idiom — still may not vectorize in gc
		// compiler, but expresses intent clearly.
		// For guaranteed SIMD, this requires assembly.
		hi := src[i] * 3
		lo := src[i] + 7
		mask := int32(0)
		if src[i] > 100 {
			mask = -1
		}
		dst[i] = (hi & mask) | (lo & ^mask)
	}
}

// --- Benchmarks: scalar vs vectorized ---

const BenchN = 1 << 20 // 1 million elements

func BenchmarkAddSlicesScalar(b *testing.B) {
	a := make([]float32, BenchN)
	bSlice := make([]float32, BenchN)
	dst := make([]float32, BenchN)
	for i := range a {
		a[i] = float32(i)
		bSlice[i] = float32(i * 2)
	}
	b.SetBytes(int64(BenchN) * 4)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		addSlices(dst, a, bSlice)
	}
	// The gc compiler will vectorize addSlices on amd64 with SSE2.
	// Expected throughput: ~20–40 GB/s on modern hardware (bandwidth-limited).
}

// --- Assembly-backed SIMD for cases the compiler won't auto-vectorize ---
// The following shows the pattern; the actual .s file is shown conceptually.

// dot_product_avx2.s (plan9 assembly — illustrative, not complete):
//
// TEXT ·dotProductAVX2(SB),NOSPLIT,$0-56
//     MOVQ    a_ptr+0(FP), SI
//     MOVQ    b_ptr+8(FP), DI
//     MOVQ    n+16(FP), CX
//     VXORPS  Y0, Y0, Y0           // accumulator = 0
// loop:
//     VMOVUPS (SI), Y1             // load 8 float32s from a
//     VMOVUPS (DI), Y2             // load 8 float32s from b
//     VFMADD231PS Y1, Y2, Y0       // Y0 += Y1 * Y2 (fused multiply-add)
//     ADDQ    $32, SI
//     ADDQ    $32, DI
//     SUBQ    $8, CX
//     JNZ     loop
//     VEXTRACTF128 $1, Y0, X1
//     VADDPS  X0, X1, X0           // reduce upper+lower 128-bit halves
//     // ... horizontal sum of X0 ...
//     VMOVSS  X0, ret+24(FP)
//     VZEROUPPER                   // required before calling non-AVX code
//     RET

// Go declaration for the assembly function:
// func dotProductAVX2(a, b []float32) float32

// Runtime dispatch based on CPU features:
func dotProduct(a, b []float32) float32 {
	if len(a) != len(b) {
		panic("length mismatch")
	}
	if cpu.X86.HasAVX2 && len(a) >= 8 {
		// In a real implementation, call the .s assembly function here
		return dotProductScalar(a, b) // placeholder
	}
	return dotProductScalar(a, b)
}

// Scalar fallback (may still be auto-vectorized by gc):
func dotProductScalar(a, b []float32) float32 {
	sum := float32(0)
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}

func BenchmarkDotProductScalar(b *testing.B) {
	n := 1 << 16
	a := make([]float32, n)
	bSlice := make([]float32, n)
	for i := range a {
		a[i] = float32(i)
		bSlice[i] = float32(i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = dotProductScalar(a, bSlice)
	}
}

// --- Verifying vectorization via disassembly ---
func main() {
	fmt.Printf("CPU AVX2 support: %v\n", cpu.X86.HasAVX2)
	fmt.Printf("CPU AVX-512F support: %v\n", cpu.X86.HasAVX512F)
	fmt.Printf("float32 size: %d bytes\n", unsafe.Sizeof(float32(0)))
	fmt.Printf("AVX2 processes %d float32s per instruction\n", 32/int(unsafe.Sizeof(float32(0))))

	// Confirm the loop produces correct results
	a := []float32{1, 2, 3, 4, 5, 6, 7, 8}
	b := []float32{1, 2, 3, 4, 5, 6, 7, 8}
	result := dotProductScalar(a, b)
	fmt.Printf("Dot product [1..8] · [1..8] = %.0f (expected 204)\n", result)
}
```

### Go-specific Considerations

**gc compiler vectorization**: Go's built-in compiler (`gc`) does vectorize loops on
amd64 targets using SSE2 (128-bit) and, in recent versions (Go 1.22+), AVX2 (256-bit)
for some patterns. The vectorizer is less aggressive than LLVM — it requires simpler
loop structures. `gccgo` (Go frontend for GCC) produces better vectorized code but is
rarely used in production.

**`golang.org/x/sys/cpu`**: This package (part of the Go extended standard library)
provides runtime CPU feature detection. It reads CPUID at init time and exports flags like
`cpu.X86.HasAVX2`, `cpu.ARM64.HasASIMD`. Use this for runtime dispatch between scalar,
SSE2, and AVX2 code paths.

**Plan9 assembly and `//go:generate`**: Writing Go assembly is feasible but verbose.
The `avo` library generates Go assembly from Go code — it uses Go types and loops to
describe SIMD operations and emits the `.s` file. This is the recommended approach for
new Go SIMD code: write in `avo`, commit both the generator and the generated `.s`.

**Third-party SIMD libraries**: `github.com/klauspost/cpuid` extends `golang.org/x/sys/cpu`.
`github.com/minio/sha256-simd` is a reference implementation of AVX2-accelerated SHA-256
that shows the full assembly+dispatch pattern at production quality.

## Implementation: Rust

```rust
// portable-simd feature (stable via std in Rust 1.86+, or use the
// `packed_simd_2` / `wide` crates on stable Rust prior to that)
//
// Check current status: https://doc.rust-lang.org/std/simd/index.html

// For maximum portability without nightly, use the `wide` crate:
// Cargo.toml: wide = "0.7"

// This example shows three levels:
// 1. Auto-vectorization (free, zero code changes)
// 2. std::simd portable API (safe, portable)
// 3. std::arch AVX2 intrinsics (unsafe, maximum control)

// --- Level 1: Auto-vectorization ---

// This loop auto-vectorizes with --release because:
// - Independent iterations
// - Rust proves no aliasing (slices are non-overlapping by type system)
// - No branches in the critical path
fn add_slices_auto(dst: &mut [f32], a: &[f32], b: &[f32]) {
    assert_eq!(dst.len(), a.len());
    assert_eq!(dst.len(), b.len());
    for ((d, &ai), &bi) in dst.iter_mut().zip(a.iter()).zip(b.iter()) {
        *d = ai + bi;
    }
    // cargo rustc --release -- --emit=asm | grep -A5 'vaddps'
    // will show AVX2 instructions on x86_64 with AVX2 target feature
}

// Branchless conditional — enables vectorization vs conditional store:
fn threshold_clip(data: &mut [f32], threshold: f32) {
    for v in data.iter_mut() {
        // Instead of: if *v > threshold { *v = threshold; }
        // Use min() — LLVM maps this to VMINPS (vectorized min)
        *v = v.min(threshold);
    }
}

// --- Level 2: std::simd Portable SIMD ---
// Requires nightly or the `wide` crate on stable. Shown with std::simd syntax.

#[cfg(feature = "nightly")]
fn dot_product_simd(a: &[f32], b: &[f32]) -> f32 {
    use std::simd::f32x8;
    use std::simd::num::SimdFloat;

    assert_eq!(a.len(), b.len());
    let chunks = a.len() / 8;
    let mut acc = f32x8::splat(0.0); // broadcast 0.0 to all 8 lanes

    for i in 0..chunks {
        let va = f32x8::from_slice(&a[i * 8..]);
        let vb = f32x8::from_slice(&b[i * 8..]);
        acc += va * vb; // VFMADD231PS on AVX2
    }

    let mut sum = acc.reduce_sum(); // horizontal sum across lanes
    // Handle remaining elements (len % 8 != 0)
    for i in chunks * 8..a.len() {
        sum += a[i] * b[i];
    }
    sum
}

// Stable alternative using the `wide` crate:
fn dot_product_wide(a: &[f32], b: &[f32]) -> f32 {
    // Without wide crate, fall through to scalar for demonstration
    // With wide crate: use wide::f32x8 same as above
    a.iter().zip(b.iter()).map(|(&x, &y)| x * y).sum()
}

// --- Level 3: AVX2 Intrinsics (unsafe) ---

#[cfg(target_arch = "x86_64")]
#[target_feature(enable = "avx2,fma")]
unsafe fn dot_product_avx2(a: &[f32], b: &[f32]) -> f32 {
    use std::arch::x86_64::*;

    let n = a.len();
    let mut acc = _mm256_setzero_ps(); // 256-bit register, all zeros
    let chunks = n / 8;

    for i in 0..chunks {
        // _mm256_loadu_ps: load 8 f32s from unaligned address
        // Use _mm256_load_ps for 32-byte aligned arrays (faster on some CPUs)
        let va = _mm256_loadu_ps(a.as_ptr().add(i * 8));
        let vb = _mm256_loadu_ps(b.as_ptr().add(i * 8));
        // Fused multiply-add: acc += va * vb (one instruction, better precision)
        acc = _mm256_fmadd_ps(va, vb, acc);
    }

    // Horizontal reduction: sum 8 lanes into one f32
    // Technique: add upper 128 bits to lower 128 bits, then reduce 4 lanes
    let lo = _mm256_castps256_ps128(acc);
    let hi = _mm256_extractf128_ps(acc, 1);
    let sum128 = _mm_add_ps(lo, hi);
    let shuf = _mm_movehdup_ps(sum128);
    let sum64 = _mm_add_ps(sum128, shuf);
    let shuf2 = _mm_movehl_ps(sum64, sum64);
    let sum32 = _mm_add_ss(sum64, shuf2);
    let result = _mm_cvtss_f32(sum32);

    // Handle remaining elements
    let mut tail = result;
    for i in chunks * 8..n {
        tail += a[i] * b[i];
    }
    tail
}

// Runtime dispatch with safe wrapper
fn dot_product(a: &[f32], b: &[f32]) -> f32 {
    assert_eq!(a.len(), b.len());
    #[cfg(target_arch = "x86_64")]
    {
        if is_x86_feature_detected!("avx2") && is_x86_feature_detected!("fma") {
            // SAFETY: we just checked AVX2+FMA support at runtime
            return unsafe { dot_product_avx2(a, b) };
        }
    }
    dot_product_wide(a, b)
}

fn main() {
    let a: Vec<f32> = (1..=16).map(|x| x as f32).collect();
    let b: Vec<f32> = (1..=16).map(|x| x as f32).collect();

    let result = dot_product(&a, &b);
    // 1²+2²+...+16² = 1496
    println!("Dot product: {:.0} (expected 1496)", result);
    assert!((result - 1496.0).abs() < 0.1);

    #[cfg(target_arch = "x86_64")]
    {
        println!("AVX2 support: {}", is_x86_feature_detected!("avx2"));
        println!("FMA support:  {}", is_x86_feature_detected!("fma"));
    }

    // Demonstrate threshold_clip (auto-vectorized min)
    let mut data: Vec<f32> = (0..16).map(|x| x as f32).collect();
    threshold_clip(&mut data, 10.0);
    println!("Clipped: {:?}", &data[8..16]); // [8,9,10,10,10,10,10,10]
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_dot_product_correctness() {
        let a = vec![1.0f32, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0,
                     1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0];
        let b = a.clone();
        let expected: f32 = a.iter().zip(b.iter()).map(|(&x, &y)| x * y).sum();
        let actual = dot_product(&a, &b);
        assert!((actual - expected).abs() < 0.01,
                "AVX2 result {actual} != scalar {expected}");
    }
}
```

### Rust-specific Considerations

**`#[target_feature(enable = "avx2")]`**: This attribute enables AVX2 code generation for
a single function without requiring the entire binary to target AVX2. Combined with
`is_x86_feature_detected!` for runtime dispatch, it is the idiomatic Rust SIMD pattern.
The function is compiled twice: once for the detected feature level, once for the fallback.

**`VZEROUPPER` and performance**: AVX2 code that uses 256-bit registers and then calls
non-AVX code without a `VZEROUPPER` instruction can cause a state transition penalty on
Intel CPUs (up to 100+ cycles). Rust and LLVM emit `VZEROUPPER` at the end of `#[target_feature]`
functions automatically. When writing raw assembly, insert it manually.

**`aligned_alloc` for SIMD data**: `_mm256_load_ps` (aligned load) requires the address
to be 32-byte aligned. Rust's standard allocator aligns to `size_of::<T>()` or 8 bytes,
whichever is larger. For 32-byte or 64-byte aligned allocations, use `std::alloc::alloc`
with `Layout::from_size_align(n * 4, 32).unwrap()`, or use the `aligned` crate.

**`multiversion` crate**: Provides a `#[multiversion]` proc-macro that automatically
generates multiple versions of a function (SSE2, AVX2, AVX-512) with compile-time
dispatch. Eliminates the boilerplate of manual feature detection:
```rust
#[multiversion::multiversion(targets("x86_64+avx2+fma", "x86_64+sse4.1", "x86_64"))]
fn dot_product(a: &[f32], b: &[f32]) -> f32 { ... }
```

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Auto-vectorization quality | Moderate (SSE2, some AVX2 in Go 1.22+) | High (LLVM generates AVX2/AVX-512) |
| Safe portable SIMD API | None (requires assembly) | `std::simd` / `wide` crate |
| Manual intrinsics | Via plan9 assembly (`.s` files) | `std::arch::x86_64` (unsafe) |
| Runtime dispatch | `golang.org/x/sys/cpu` + if/else | `is_x86_feature_detected!` + `#[target_feature]` |
| Assembly ergonomics | Plan9 assembly, verbose syntax | Direct intrinsics + LLVM IR, idiomatic |
| AVX2 access | Assembly only | Safe via `wide` crate, unsafe via `std::arch` |
| Correctness guarantee | Manual (assembly correctness is on the author) | Rust type system handles alignment + bounds |
| Cross-platform SIMD | Requires per-platform assembly | `std::simd` / `multiversion` handle it |
| Key libraries | `minio/sha256-simd`, `klauspost` libraries | `packed_simd_2`, `wide`, `simdeez` |

## Production War Stories

**zstd compression (Meta, 2018)**: The zstd compressor uses AVX2 for its entropy coding
(Huffman and ANS/FSE decoding). The AVX2 path processes 64 symbols simultaneously in the
decode loop, compared to 8 in the SSE2 path and 1 in the scalar path. The AVX2 decode
throughput is approximately 5–6 GB/s vs 1.2 GB/s for scalar — a 4.5x improvement from
SIMD alone, on identical algorithmic logic.

**BoringSSL AES-GCM (Google, 2017)**: Google's BoringSSL uses AES-NI hardware
instructions (a form of SIMD for AES) for AES-GCM encryption. The AES-NI path achieves
~3.5 GB/s single-core throughput on modern Intel CPUs, compared to ~300 MB/s for the
pure software AES implementation. HTTPS termination at Google scale would require roughly
10x more CPU cores without AES-NI.

**ClickHouse vectorized execution engine**: ClickHouse's expression evaluation engine
operates on columns (arrays) rather than rows. Each primitive operation (add, compare,
string hash) is implemented as a SIMD loop. The AVX2 filter loop (`WHERE col > threshold`)
processes 32 bytes per instruction using `_mm256_cmp_epi8`. This is why ClickHouse can
scan 10 billion rows per second on a single server for simple aggregations — every
comparison is 32-wide.

**Rust vs C for SIMD (Cloudflare, 2021)**: Cloudflare rewrote their base64 decoder in
Rust using `std::arch` AVX2 intrinsics. The Rust version matched the throughput of the
C version (both using `_mm256_shuffle_epi8`) while eliminating buffer overflow risks from
the type system. The key finding: LLVM produces equivalent code to GCC for identical
intrinsic calls; the performance ceiling is the same.

## Numbers That Matter

| Operation | Throughput (approximate, single core, 3 GHz) |
|-----------|---------------------------------------------|
| Scalar float32 add | ~3 billion/second (1 per cycle, pipelined) |
| SSE2 float32 add (4-wide) | ~10 billion/second |
| AVX2 float32 add (8-wide) | ~20 billion/second |
| AVX-512 float32 add (16-wide) | ~30–40 billion/second |
| AVX2 FMA (fused multiply-add, 8-wide) | ~20 billion FMA/second = 40B flop/s |
| Scalar int8 comparison | ~3 billion/second |
| AVX2 int8 comparison (32-wide, `_mm256_cmpgt_epi8`) | ~90 billion/second |
| Auto-vectorization overhead vs manual SIMD | 0–10% slower (LLVM is very good) |

## Common Pitfalls

**Assuming auto-vectorization fired**: The most common mistake. Writing a clean loop and
assuming the compiler vectorized it. Verify with `objdump -d binary | grep -E 'vmovdqu|vaddps'`
or `perf stat -e fp_arith_inst_retired.256b_packed_single`. If the count is zero, the loop
ran scalar.

**Mixing AVX and non-AVX code without VZEROUPPER**: On Intel Haswell through Skylake CPUs,
calling a non-AVX function after using 256-bit registers without a `VZEROUPPER` instruction
causes a 200+ cycle stall at the function boundary. This stall disappears silently in
microbenchmarks (no mixing) but appears in real code. LLVM and GCC emit `VZEROUPPER`
automatically; Go assembly authors must handle this manually.

**Unaligned loads with `_mm256_load_ps`**: `_mm256_load_ps` requires 32-byte alignment.
`_mm256_loadu_ps` handles unaligned addresses. On modern CPUs (Haswell+), the performance
difference is negligible for naturally aligned data. Always use the `u` (unaligned) variant
unless you have verified alignment and profiled an actual difference.

**SIMD on data that doesn't fit in registers**: Processing a 1 TB dataset with AVX2 is
bandwidth-limited, not compute-limited. Once you saturate DRAM bandwidth (~50 GB/s on
dual-channel DDR4), adding more SIMD width doesn't help — you're waiting for data, not
computation. Always confirm whether the bottleneck is compute or bandwidth before adding
SIMD.

**Ignoring SIMD on the critical path for correctness**: IEEE 754 floating-point operations
have defined rounding behavior. AVX2 FMA (`VFMADD231PS`) has different rounding than
`(a * b) + c` computed separately. If your code has strict numerical reproducibility
requirements (financial, scientific computing with fixed expected outputs), document this
difference and use scalar paths for correctness-sensitive tests.

## Exercises

**Exercise 1** (30 min): Write a Go function that computes the dot product of two
`[]float32` slices. Compile with `go build -gcflags="-d=ssa/loop vectorize/debug=1"`.
Identify whether the compiler vectorized it. Then deliberately break vectorization by
introducing a loop-carried dependency and verify the compiler reports the failure.

**Exercise 2** (2–4h): In Rust, implement a horizontal byte histogram (count occurrences
of each byte value 0–255 in a large byte slice) in three versions: scalar, auto-vectorized
(`iter().fold`), and AVX2 intrinsics using `_mm256_sad_epu8`. Benchmark all three with
criterion. The AVX2 version should be 4–8x faster than scalar for large inputs. Verify
results are identical for all three.

**Exercise 3** (4–8h): Port an existing Go string processing function (e.g., a JSON
field parser or a CSV column extractor) to use AVX2 via Go assembly. The function should:
detect newline characters using `VPCMPEQB`, create a bitmask, and use `TZCNT` to find
the position of the next newline. Compare throughput to the scalar version using
`testing.B`. Target: 2x+ improvement.

**Exercise 4** (8–15h): Implement a SIMD-accelerated image channel split: given an
RGBA pixel array (interleaved bytes: R,G,B,A,R,G,B,A,...), split into four separate
R/G/B/A planar arrays using AVX2 shuffle instructions. Use `_mm256_shuffle_epi8` to
deinterleave 32 bytes at a time. Write criterion benchmarks. Then integrate runtime
dispatch so the binary falls back to scalar on non-AVX2 hardware.

## Further Reading

### Foundational Papers

- Intel — ["Intel AVX-512 Programming Reference"](https://www.intel.com/content/www/us/en/developer/articles/technical/intel-avx512-programming-reference.html) —
  the authoritative reference for all AVX-512 instructions with latency tables
- ARM — ["ARM NEON Programmer's Guide"](https://developer.arm.com/documentation/den0018/a/) —
  ARM's SIMD documentation for server-class ARM processors (Graviton, Ampere)

### Books

- Daniel Lemire — ["Code like a Pro in C"](https://www.manning.com/books/code-like-a-pro-in-c)
  — chapters on SIMD apply directly to C-equivalent Rust intrinsic code
- Agner Fog — ["Optimizing software in C++"](https://www.agner.org/optimize/optimizing_cpp.pdf)
  (free PDF) — Chapter 11 covers SIMD optimization with extensive intrinsic examples;
  directly applicable to Rust `std::arch` code

### Blog Posts

- [Agner Fog's instruction tables](https://www.agner.org/optimize/instruction_tables.pdf) —
  latency and throughput for every x86 instruction by microarchitecture
- [How to Write Fast Numerical Code](https://users.ece.cmu.edu/~franzf/papers/hpec17.pdf)
  (Franz, Puschel) — systematic methodology for writing SIMD-optimized numerical code
- [Rust SIMD performance guide](https://rust-lang.github.io/packed_simd/perf-guide/) —
  patterns and pitfalls for SIMD in Rust
- [minio/sha256-simd](https://github.com/minio/sha256-simd) — production AVX512/AVX2
  SHA-256 in Go assembly, reference for the assembly+dispatch pattern

### Tools Documentation

- [Compiler Explorer (godbolt.org)](https://godbolt.org) — inspect generated assembly
  for any Rust, C, C++, or Go code without a local build
- [`cargo-asm`](https://github.com/gnzlbg/cargo-asm) — show assembly for a specific
  Rust function: `cargo asm my_crate::my_func`
- [`avo`](https://github.com/mmcloughlin/avo) — Go assembly generator; write SIMD in Go
- [`multiversion`](https://docs.rs/multiversion) — Rust function multiversioning
