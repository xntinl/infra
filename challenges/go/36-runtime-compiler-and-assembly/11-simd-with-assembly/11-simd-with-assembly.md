# 11. SIMD with Assembly

<!--
difficulty: insane
concepts: [simd, sse, avx, avx2, neon, vector-instructions, data-parallel, xmm-registers, ymm-registers]
tools: [go, objdump]
estimated_time: 90m
bloom_level: create
prerequisites: [go-assembly-basics, writing-assembly-functions]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 09-10 (assembly basics and writing assembly functions)
- Understanding of SIMD concepts (processing multiple data elements per instruction)
- Access to a machine with SSE2/AVX2 (amd64) or NEON (arm64)

## Learning Objectives

- **Create** SIMD-accelerated functions using Go assembly with vector registers
- **Analyze** data alignment requirements and their impact on SIMD performance
- **Evaluate** the speedup from SIMD for different data sizes and operations

## The Challenge

SIMD (Single Instruction, Multiple Data) instructions process multiple data elements in parallel using wide registers (128-bit XMM, 256-bit YMM on x86, 128-bit V registers on ARM). The Go compiler does not auto-vectorize loops, so using SIMD requires hand-written assembly. This is one of the few cases where assembly consistently and dramatically outperforms compiler-generated code.

Write SIMD-accelerated versions of common operations: vector addition, dot product, byte searching, and minimum/maximum finding. Provide pure Go fallbacks for architectures without SIMD support, and benchmark to quantify the speedup.

## Requirements

1. Write an SSE2/AVX2 assembly function `VectorAddFloat32(dst, a, b []float32)` that adds two float32 slices element-wise using 128-bit (SSE) or 256-bit (AVX2) operations. Process 4 or 8 floats per instruction.
2. Write a SIMD function `DotProductFloat32(a, b []float32) float32` that computes the dot product using horizontal add instructions (HADDPS on SSE3 or VFMADD on FMA).
3. Write a SIMD function `FindByteAVX2(s []byte, c byte) int` that searches for a byte in a slice using VPCMPEQB and VPMOVMSKB to check 32 bytes per iteration.
4. Write a SIMD function `MaxFloat32(s []float32) float32` that finds the maximum value using VMAXPS to compare 8 floats per iteration.
5. Provide pure Go fallback implementations for each function. Use build tags or runtime feature detection to select the SIMD version when available.
6. Handle alignment: demonstrate the performance difference between aligned and unaligned SIMD loads (VMOVAPS vs VMOVUPS).
7. Handle tails: when the slice length is not a multiple of the vector width, process remaining elements with scalar code.
8. Benchmark each function at multiple data sizes (64, 1K, 64K, 1M elements) showing SIMD speedup vs scalar.

## Hints

- XMM registers (X0-X15) are 128-bit. YMM registers (Y0-Y15) are 256-bit. Use `MOVUPS` / `VMOVUPS` for unaligned loads, `MOVAPS` / `VMOVAPS` for aligned.
- SSE2 operates on 4 float32s or 2 float64s. AVX2 doubles this to 8 float32s or 4 float64s.
- Go assembly uses uppercase instruction mnemonics: `VMOVUPS`, `VADDPS`, `VMULPS`, `VMAXPS`, `VPCMPEQB`.
- Feature detection: check `internal/cpu` or use `CPUID` to detect AVX2 support before using 256-bit instructions.
- Data alignment matters: `VMOVAPS` requires 32-byte alignment for YMM registers. Go's allocator does not guarantee alignment beyond pointer size. Use `VMOVUPS` for safety.
- The horizontal add pattern for dot product: multiply (`VMULPS`), then sum within the register using `VHADDPS` or shuffle + add.
- For byte search, `VPCMPEQB` compares 32 bytes at once, `VPMOVMSKB` extracts a bit mask of matches, then `TZCNTL` finds the first match.

## Success Criteria

1. All SIMD functions produce identical results to their pure Go equivalents for all inputs
2. Tail handling correctly processes remaining elements when length is not a multiple of vector width
3. Benchmarks show at least 3x speedup for SIMD vs scalar on data sizes >= 1K elements
4. Aligned vs unaligned load comparison shows the (small or negligible on modern CPUs) difference
5. Build tags or runtime detection correctly select SIMD vs fallback implementations
6. Tests cover edge cases: empty slices, single element, exactly one vector width, odd lengths

## Research Resources

- [Intel Intrinsics Guide](https://www.intel.com/content/www/us/en/docs/intrinsics-guide/) -- searchable reference for SSE/AVX instructions
- [Go assembly SIMD examples](https://github.com/golang/go/blob/master/src/crypto/aes/gcm_amd64.s) -- real SIMD in the Go standard library
- [Agner Fog's optimization manuals](https://www.agner.org/optimize/) -- definitive x86 performance reference
- [SIMD for Go (blog)](https://sourcegraph.com/blog/slow-to-simd) -- practical guide to SIMD in Go
- [ARM NEON intrinsics](https://developer.arm.com/architectures/instruction-sets/intrinsics/) -- for arm64 implementations

## What's Next

Continue to [12 - Analyzing Compiler Output](../12-analyzing-compiler-output/12-analyzing-compiler-output.md) to learn systematic techniques for analyzing and understanding compiler-generated code.

## Summary

- SIMD processes multiple data elements per instruction using wide registers (128-bit XMM, 256-bit YMM)
- The Go compiler does not auto-vectorize -- SIMD requires hand-written assembly
- SSE2 processes 4 float32s per instruction; AVX2 processes 8
- Always handle tail elements that do not fill a complete vector
- Use unaligned loads (VMOVUPS) unless you can guarantee alignment
- SIMD typically provides 3-8x speedup for data-parallel operations
- Provide pure Go fallbacks for portability and correctness testing
- Real-world SIMD is used in Go's crypto, hash, and string packages
