# 68. Matrix Multiplication: Strassen's Algorithm

<!--
difficulty: intermediate-advanced
category: game-development-and-graphics
languages: [rust]
concepts: [strassen-algorithm, divide-and-conquer, matrix-algebra, cache-optimization, benchmarking, operator-overloading]
estimated_time: 5-7 hours
bloom_level: apply, analyze
prerequisites: [rust-generics, operator-overloading, recursion, basic-linear-algebra, benchmarking-criterion]
-->

## Languages

- Rust (stable)

## Prerequisites

- Matrix multiplication math: row-by-column dot product
- Rust operator overloading (`std::ops` traits)
- Recursive algorithms and divide-and-conquer strategy
- Basic understanding of CPU cache behavior and memory access patterns
- Familiarity with `criterion` for benchmarking

## Learning Objectives

- **Implement** Strassen's algorithm that reduces matrix multiplication from O(n^3) to O(n^2.807) through recursive decomposition
- **Analyze** the crossover point where Strassen outperforms naive multiplication and explain why it exists
- **Apply** padding strategies to handle matrices whose dimensions are not powers of two
- **Evaluate** the cache behavior differences between naive row-major iteration and block-recursive decomposition

## The Challenge

Matrix multiplication is the most performance-critical operation in graphics, physics, and machine learning. The naive algorithm uses three nested loops and runs in O(n^3). Strassen's algorithm (1969) was the first to break this barrier, achieving O(n^2.807) by replacing 8 recursive multiplications with 7 at each level, at the cost of more additions.

The insight is that multiplying two 2x2 block matrices normally requires 8 multiplications. Strassen showed you can compute the same result with only 7 multiplications and 18 additions/subtractions. Since multiplication dominates for large matrices, this trade-off wins asymptotically.

Implement both naive and Strassen matrix multiplication. Pad non-power-of-2 matrices to the next power of 2. Build a hybrid that falls back to naive for small submatrices (the crossover point). Benchmark to find the optimal crossover on your hardware.

This matters for game development because every frame involves hundreds of matrix multiplications: model transforms, view projection, normal matrices, skeletal animation. Understanding what happens underneath the BLAS call makes you a better systems programmer.

## Requirements

1. Implement a `Matrix` struct storing elements in row-major order as a flat `Vec<f64>` with dimensions `rows x cols`
2. Implement naive O(n^3) matrix multiplication with three nested loops
3. Implement matrix addition and subtraction with operator overloading (`Add`, `Sub` for `&Matrix`)
4. Implement Strassen's algorithm: split each matrix into four quadrants, compute 7 products (M1-M7), combine results with additions/subtractions
5. Handle non-square and non-power-of-2 matrices by padding with zeros to the next power of 2, then trimming the result
6. Implement a hybrid algorithm that falls back to naive multiplication when the submatrix size drops below a configurable threshold
7. Verify correctness: `strassen(A, B)` must equal `naive(A, B)` within floating-point tolerance (epsilon = 1e-9) for random matrices
8. Benchmark with `criterion` at sizes 64, 128, 256, 512, 1024: naive vs pure Strassen vs hybrid at crossover points of 32, 64, 128
9. Implement `Display` for `Matrix` for debug printing (show small matrices formatted, large matrices show dimensions only)
10. Write tests covering: identity multiplication, zero matrix, non-square padding, known small-matrix results, Strassen vs naive equivalence

## Hints

<details>
<summary>Hint 1: Strassen's seven products</summary>

Given block decomposition A = [[A11, A12], [A21, A22]] and B = [[B11, B12], [B21, B22]]:

```
M1 = (A11 + A22) * (B11 + B22)
M2 = (A21 + A22) * B11
M3 = A11 * (B12 - B22)
M4 = A22 * (B21 - B11)
M5 = (A11 + A12) * B22
M6 = (A21 - A11) * (B11 + B12)
M7 = (A12 - A22) * (B21 + B22)
```

Then: C11 = M1 + M4 - M5 + M7, C12 = M3 + M5, C21 = M2 + M4, C22 = M1 - M2 + M3 + M6.
</details>

<details>
<summary>Hint 2: Submatrix extraction</summary>

Rather than copying quadrants, implement a `submatrix(row_start, col_start, size)` method that creates a new `Matrix` from a region. For the recursive step, split an n x n matrix into four n/2 x n/2 quadrants. After computing the result quadrants, assemble them back into the full result matrix with a `set_submatrix` method.
</details>

<details>
<summary>Hint 3: Finding the crossover point</summary>

Strassen has higher constant factors due to the extra additions and allocations. For small matrices (typically below 32-128 elements per side), naive is faster. The hybrid approach recursively applies Strassen down to the crossover size, then switches to naive. Benchmark different thresholds to find the optimal crossover for your machine. The crossover is hardware-dependent due to cache sizes.
</details>

<details>
<summary>Hint 4: Avoiding excessive allocation</summary>

Each Strassen recursion level creates intermediate matrices for M1-M7 and the submatrix sums. For deep recursion, this is thousands of allocations. Pre-allocate scratch buffers or use an arena allocator. Alternatively, the crossover threshold prevents deep recursion, which limits the total allocation count.
</details>

## Acceptance Criteria

- [ ] Naive multiplication produces correct results for known test cases
- [ ] Strassen produces results matching naive within f64 epsilon for random 128x128 matrices
- [ ] Non-power-of-2 matrices (e.g., 100x100) are handled correctly via padding
- [ ] Hybrid algorithm switches to naive below the configured threshold
- [ ] Benchmarks show Strassen outperforming naive for matrices 256x256 and larger
- [ ] Identity matrix multiplication returns the original matrix
- [ ] Non-square input matrices are padded and results trimmed correctly
- [ ] All tests pass including floating-point tolerance comparisons
- [ ] Benchmark results are printed showing crossover analysis

## Research Resources

- [Strassen's Algorithm (Wikipedia)](https://en.wikipedia.org/wiki/Strassen_algorithm) -- the algorithm definition and complexity analysis
- [Matrix Multiplication: A Little Bit of History](https://www.ams.org/journals/bull/2023-60-04/S0273-0979-2023-01811-8/S0273-0979-2023-01811-8.pdf) -- context for matrix multiplication complexity research
- [Criterion.rs Documentation](https://bheisler.github.io/criterion.rs/book/) -- Rust benchmarking framework
- [What Every Programmer Should Know About Memory (Drepper)](https://people.freebsd.org/~lstewart/articles/cpumemory.pdf) -- cache effects on matrix operations
- [BLAS and Cache-Oblivious Algorithms](https://en.algorithmica.org/hpc/algorithms/matmul/) -- how production implementations optimize matrix multiplication
- [Introduction to Algorithms (CLRS), Chapter 4](https://mitpress.mit.edu/9780262046305/introduction-to-algorithms/) -- divide-and-conquer and Strassen analysis
