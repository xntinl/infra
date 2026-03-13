# 10. SIMD and Vectorization

**Difficulty**: Insane

## The Challenge

Build a high-performance byte processing library that implements three algorithms — byte search (like `memchr`), CRC32C computation, and 8-bit grayscale image convolution — using explicit SIMD intrinsics. Each algorithm must have three implementations: a scalar fallback, an SSE4.2/NEON version, and an AVX2/SVE version (or the best available for your target). Runtime feature detection must dispatch to the widest available instruction set automatically.

The goal is not just correctness but measurable performance. You will use `criterion` to benchmark each implementation against its scalar baseline, and you will use Compiler Explorer (godbolt.org) to verify that the compiler emits the vector instructions you expect. You must understand the difference between writing intrinsics by hand and coaxing the compiler into auto-vectorizing scalar code — and demonstrate where auto-vectorization fails and manual intrinsics are necessary.

You will work with both stable Rust (`std::arch` intrinsics and `#[target_feature]`) and nightly Rust (`std::simd` portable SIMD). The portable SIMD module (`std::simd`) has been nightly-only since its introduction and remains so — it provides a higher-level, architecture-agnostic abstraction over SIMD lanes, but you cannot use it in production stable code today. You must implement at least one algorithm using both approaches and compare the ergonomics and generated assembly.

## Acceptance Criteria

- [ ] Byte search (`simd_memchr`) finds first occurrence of a byte in a `&[u8]` — matches `memchr` crate output for all inputs
- [ ] Byte search has scalar, SSE4.2 (or NEON), and AVX2 implementations behind `#[target_feature(enable = "...")]`
- [ ] Runtime dispatch uses `is_x86_feature_detected!` (or equivalent) — no compile-time-only selection
- [ ] CRC32C computation uses `_mm_crc32_u64` intrinsics on x86 with SSE4.2, scalar fallback otherwise
- [ ] CRC32C matches the CRC32C polynomial (Castagnoli, 0x1EDC6F41) — validated against `crc32c` crate
- [ ] Image convolution applies a 3x3 kernel to an 8-bit grayscale image using 16-lane `u8` vectors (AVX2 `__m256i` or portable `Simd<u8, 32>`)
- [ ] Convolution handles image boundaries correctly (clamp or zero-pad)
- [ ] `criterion` benchmarks show speedup factors for each tier vs. scalar — include results in a comment or doc
- [ ] At least one algorithm implemented with both `std::arch` and `std::simd` — assembly compared via godbolt
- [ ] Auto-vectorization experiment: write a simple scalar loop, compile with `-C opt-level=3 -C target-cpu=native`, inspect output — document where auto-vectorization succeeds and where it does not
- [ ] All `unsafe` blocks for intrinsics are sound — `#[target_feature]` functions are `unsafe fn` or called within checked dispatch
- [ ] No undefined behavior: alignment requirements respected for `_mm_load_si128` vs `_mm_loadu_si128`

## Background

### `std::arch` (Stable)

The `std::arch` module exposes platform-specific SIMD intrinsics as `unsafe` functions. On x86-64, these live in `std::arch::x86_64` and map almost one-to-one to Intel intrinsic names. You call `_mm256_cmpeq_epi8` just as you would in C. Each function requires the appropriate target feature to be enabled — either via `#[target_feature(enable = "avx2")]` on the enclosing function, or by compiling the entire crate with `-C target-feature=+avx2`.

The `#[target_feature]` attribute makes the function `unsafe` to call because calling an AVX2 function on a CPU without AVX2 is immediate undefined behavior (illegal instruction). The safe pattern is to check at runtime:

```rust
if is_x86_feature_detected!("avx2") {
    unsafe { search_avx2(haystack, needle) }
} else if is_x86_feature_detected!("sse4.2") {
    unsafe { search_sse42(haystack, needle) }
} else {
    search_scalar(haystack, needle)
}
```

### `std::simd` (Nightly)

The portable SIMD API (`#![feature(portable_simd)]`) provides `Simd<T, N>` where `T` is a scalar type and `N` is the lane count. Operations like `simd_eq`, `simd_reduce_or`, and lane-wise arithmetic are expressed as methods. The compiler lowers these to the best available instructions for the target. This is far more ergonomic than raw intrinsics but remains unstable. See the tracking issue at `rust-lang/rust#86656` and the `rust-lang/portable-simd` repository.

### Auto-Vectorization

LLVM can auto-vectorize scalar loops under `-C opt-level=3`. However, auto-vectorization is fragile: it fails on loops with complex control flow, non-trivial reductions, or data-dependent iteration counts. The `memchr` crate exists precisely because auto-vectorization cannot reliably produce optimal byte-search code. Your experiment should demonstrate this gap.

## Architecture Hints

```
src/
  lib.rs          // Public API with runtime dispatch
  scalar.rs       // Scalar fallback implementations
  x86_sse42.rs    // SSE4.2 implementations (#[target_feature(enable = "sse4.2")])
  x86_avx2.rs     // AVX2 implementations (#[target_feature(enable = "avx2")])
  portable.rs     // std::simd implementations (nightly only, behind cfg)
  bench.rs        // criterion benchmarks (benches/bench.rs)
```

For the memchr-style search, study how the `memchr` crate (`BurntSushi/memchr`) structures its dispatch in `src/arch/x86_64/memchr.rs` and `src/arch/generic/memchr.rs`. The key insight is broadcasting the needle byte to fill a SIMD register, then using `pcmpeqb` + `pmovmskb` to find matches in 16/32-byte chunks.

For CRC32C, the x86 `crc32` instruction operates on 8/16/32/64-bit inputs. The trick for throughput is processing 8 bytes at a time with `_mm_crc32_u64`. Study `src/hw.rs` in the `crc32c` crate on crates.io.

For image convolution, the challenge is the gather/scatter pattern — neighboring pixels must be loaded, multiplied by kernel weights, and accumulated. With AVX2, you can process 32 pixels simultaneously if you structure the loads correctly. Study how `image` crate's `imageproc` handles convolution.

## Going Further

- Implement a SIMD-accelerated UTF-8 validator. Study `simdutf` (Daniel Lemire) and the approach in `simdjson`.
- Use `std::simd`'s `mask` types to implement branchless conditional operations.
- Port your byte search to AArch64 NEON intrinsics (`std::arch::aarch64`) and benchmark on an Apple Silicon machine.
- Implement SIMD-accelerated base64 encoding/decoding. Study the approach in `base64-simd` crate.
- Explore `#[repr(simd)]` for custom SIMD types (nightly, `#![feature(repr_simd)]`).
- Profile with `perf stat` to measure IPC (instructions per cycle) — SIMD code should have significantly higher IPC than scalar.

## Resources

- **Blog**: Shnatsel — "The state of SIMD in Rust in 2025" — [shnatsel.medium.com/the-state-of-simd-in-rust-in-2025](https://shnatsel.medium.com/the-state-of-simd-in-rust-in-2025-32c263e5f53d)
- **Blog**: Itamar Turner-Trauring — "Using portable SIMD in stable Rust" — [pythonspeed.com/articles/simd-stable-rust](https://pythonspeed.com/articles/simd-stable-rust/)
- **Blog**: Linebender — "Towards Fearless SIMD, 7 Years Later" — [linebender.org/blog/towards-fearless-simd](https://linebender.org/blog/towards-fearless-simd/)
- **Repo**: `rust-lang/portable-simd` — [github.com/rust-lang/portable-simd](https://github.com/rust-lang/portable-simd)
- **Goal**: Rust Project Goals 2025h1 — "Ergonomic SIMD multiversioning" — [rust-lang.github.io/rust-project-goals/2025h1/simd-multiversioning](https://rust-lang.github.io/rust-project-goals/2025h1/simd-multiversioning.html)
- **Source**: `BurntSushi/memchr` — `src/arch/x86_64/` — [github.com/BurntSushi/memchr](https://github.com/BurntSushi/memchr)
- **Source**: `crc32c` crate — `src/hw.rs` — [crates.io/crates/crc32c](https://crates.io/crates/crc32c)
- **Docs**: `std::arch::x86_64` — [doc.rust-lang.org/std/arch/x86_64](https://doc.rust-lang.org/std/arch/x86_64/index.html)
- **Docs**: `std::simd` — [doc.rust-lang.org/std/simd](https://doc.rust-lang.org/std/simd/index.html)
- **Tool**: Compiler Explorer — [godbolt.org](https://godbolt.org) — paste your functions and inspect the emitted vector instructions
- **Tool**: `criterion` — [github.com/bheisler/criterion.rs](https://github.com/bheisler/criterion.rs)
- **Reference**: Intel Intrinsics Guide — [intel.com/content/www/us/en/docs/intrinsics-guide](https://www.intel.com/content/www/us/en/docs/intrinsics-guide/index.html)
- **Crate**: `wide` — stable SIMD abstraction — [crates.io/crates/wide](https://crates.io/crates/wide)
- **Paper**: Daniel Lemire — "Parsing Gigabytes of JSON per Second" (VLDB 2019) — for SIMD algorithm design patterns
