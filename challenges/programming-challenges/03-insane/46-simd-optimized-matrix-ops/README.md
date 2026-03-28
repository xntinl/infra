<!-- difficulty: insane -->
<!-- category: algorithms -->
<!-- languages: [rust] -->
<!-- concepts: [simd, matrix-operations, cache-optimization, benchmarking, strassen-algorithm, intrinsics] -->
<!-- estimated_time: 20-30 hours -->
<!-- bloom_level: evaluate, create -->
<!-- prerequisites: [rust-unsafe, linear-algebra, cpu-cache-hierarchy, benchmarking, simd-concepts] -->

# Challenge 46: SIMD-Optimized Matrix Operations

## Languages

Rust (stable for baseline + blocked, nightly for explicit SIMD intrinsics)

## The Challenge

Matrix multiplication is the canonical benchmark for computational optimization. A naive implementation of 1024x1024 matrix multiply takes seconds; an optimized one takes milliseconds. The difference is not algorithmic -- both are O(n^3) -- but architectural: cache hierarchy, instruction-level parallelism, and SIMD vectorization. Understanding this gap is understanding how modern CPUs actually execute code.

This challenge takes you through the entire optimization spectrum. You will implement the same operations at three levels of sophistication and measure the speedup at each level. The numbers will surprise you: cache-aware blocking alone provides 3-5x improvement, and SIMD on top of that pushes to 10-20x over the naive baseline.

Implement a matrix library supporting five operations: multiplication, transpose, determinant, inverse, and LU decomposition. For multiplication, implement three versions:

1. **Naive**: straightforward textbook algorithm. This is your correctness reference and performance baseline.
2. **Cache-friendly (blocked/tiled)**: reorganize memory access patterns to exploit CPU cache hierarchy. Use tiled/blocked algorithms where the inner loops operate on tiles that fit in L1 cache.
3. **SIMD-accelerated**: use `std::arch` intrinsics (SSE2/AVX2 on x86_64, NEON on ARM) to process 4 or 8 floats per instruction. This requires `unsafe` and target-feature gates.

Support both `f32` and `f64` element types. Handle arbitrary matrix dimensions, not just powers of 2 -- this means your tiled and SIMD code must handle remainder elements at tile and vector boundaries cleanly without segfaults or wrong results.

Implement Strassen's algorithm for large matrix multiplication. Strassen reduces the 8 recursive multiplications in standard divide-and-conquer to 7, changing the complexity from O(n^3) to O(n^2.807). The algorithm requires padding non-power-of-2 matrices to the next power of 2, and has a crossover point below which the overhead exceeds the savings. Tune this threshold empirically with benchmarks.

Use `criterion` for benchmarking. Produce comparison data at matrix sizes 64x64, 256x256, 1024x1024, and 4096x4096. The goal is measurable, reproducible speedup. If your SIMD version is not at least 3x faster than naive for 1024x1024 f32 matrices, your memory access pattern or vectorization has a problem.

## Acceptance Criteria

- [ ] `Matrix<f32>` and `Matrix<f64>` with runtime-determined dimensions, heap-allocated
      flat `Vec<T>` row-major storage, and `get(row, col)` / `set(row, col, val)` accessors
- [ ] Naive multiplication, transpose, determinant, inverse, and LU decomposition produce
      correct results verified against known test cases
- [ ] Blocked multiplication with configurable tile size demonstrates measurable cache
      improvement over naive (at least 2x for 1024x1024)
- [ ] SIMD multiplication using `std::arch` intrinsics: `_mm256_fmadd_ps` (AVX2/FMA) or
      `_mm_mul_ps` (SSE2) on x86_64, `vfmaq_f32` (NEON) on aarch64
- [ ] SIMD code uses `#[target_feature(enable = "...")]` with runtime feature detection
      via `is_x86_feature_detected!` or equivalent
- [ ] Strassen's algorithm with tunable crossover, correct for non-power-of-2 dimensions
      (pad to next power of 2, then extract the result submatrix)
- [ ] LU decomposition with partial pivoting producing P, L, U where PA = LU within
      floating-point tolerance
- [ ] Determinant via LU: product of U's diagonal, adjusted for pivot sign
- [ ] Inverse via LU: forward and back substitution column by column
- [ ] `criterion` benchmarks for all multiplication variants at sizes 64, 256, 1024, 4096
- [ ] SIMD version achieves at least 3x speedup over naive for 1024x1024 f32 multiplication
- [ ] All tests pass with `cargo test`: identity multiply, known 2x2 and 3x3 products,
      inverse * original = identity, known determinants, PA=LU verification, non-square
      multiply, dimension mismatch panics, remainder handling for non-SIMD-aligned dimensions

## Hints

1. Row-major storage with a single `Vec<T>` (not `Vec<Vec<T>>`) is essential for both cache performance and SIMD. Element `(i, j)` lives at index `i * cols + j`. The `Vec<Vec<T>>` layout scatters rows across the heap, destroying spatial locality.

2. For SIMD matrix multiplication, the inner loop processes one row of A against a tile of B. Transpose B first so that columns of B become rows -- this makes the dot product of A's row with B's column a sequential memory access pattern, which SIMD can vectorize without gather instructions.

3. Strassen's algorithm recursively splits matrices into 4 quadrants and computes 7 multiplications instead of 8. The crossover point where you fall back to naive/SIMD multiplication is typically around 64x64 to 128x128 -- below that, the overhead of allocation and addition exceeds the saved multiplication. Tune this empirically.

## Going Further

- Implement the Winograd variant of Strassen (fewer additions at the cost of more memory)
- Add parallel execution using `rayon` for the tiled and Strassen variants
- Implement a matrix multiplication kernel that matches BLAS-level performance (study GotoBLAS micro-kernel design)
- Port the SIMD kernels to WebAssembly SIMD and benchmark in a browser
- Implement mixed-precision: accumulate f32 multiplications in f64 for better numerical stability

## Resources

- [Intel Intrinsics Guide](https://www.intel.com/content/www/us/en/docs/intrinsics-guide/index.html) -- searchable reference for all x86 SIMD intrinsics
- [Rust `std::arch` documentation](https://doc.rust-lang.org/std/arch/index.html) -- platform-specific SIMD intrinsics in Rust
- [Rust `std::simd` portable SIMD (nightly)](https://doc.rust-lang.org/std/simd/index.html) -- portable SIMD abstractions, currently nightly-only
- [What Every Programmer Should Know About Memory (Drepper)](https://people.freebsd.org/~lstewart/articles/cpumemory.pdf) -- sections 3 and 6 on cache hierarchy and optimization
- [Strassen's Algorithm -- Wikipedia](https://en.wikipedia.org/wiki/Strassen_algorithm) -- the recursive matrix multiplication algorithm
- [criterion.rs documentation](https://bheisler.github.io/criterion.rs/book/) -- benchmarking framework for Rust
- [Anatomy of High-Performance Matrix Multiplication (Goto, van de Geijn)](https://www.cs.utexas.edu/~flame/pubs/GotoTOMS_final.pdf) -- the paper behind GotoBLAS, the gold standard for matrix multiply optimization
- [ARM NEON Intrinsics Reference](https://developer.arm.com/architectures/instruction-sets/intrinsics/) -- ARM's equivalent of Intel's guide, for aarch64 targets
