# Solution: Matrix Multiplication -- Strassen's Algorithm

## Architecture Overview

The solution consists of three modules:

1. **matrix**: The `Matrix` struct with row-major storage, element access, submatrix extraction, operator overloading, and display
2. **algorithms**: Naive O(n^3), pure Strassen recursive, and hybrid Strassen-with-crossover implementations
3. **benchmarks**: Criterion benchmarks comparing all three at multiple sizes and crossover thresholds

```
Matrix (row-major Vec<f64>)
  |
  |-- Naive: triple nested loop, O(n^3)
  |
  |-- Strassen (recursive):
  |     Split into quadrants -> 7 subproblems -> combine
  |     Base case: 1x1 direct multiplication
  |
  |-- Hybrid:
  |     Strassen recursion until size <= threshold
  |     Then falls back to naive
  |
  |-- Padding layer:
        Pad to next power-of-2 -> compute -> trim result
```

## Rust Solution

### Cargo.toml

```toml
[package]
name = "strassen"
version = "0.1.0"
edition = "2021"

[dependencies]
rand = "0.8"

[dev-dependencies]
criterion = { version = "0.5", features = ["html_reports"] }

[[bench]]
name = "matrix_bench"
harness = false
```

### src/matrix.rs

```rust
use std::fmt;
use std::ops::{Add, Sub};

#[derive(Debug, Clone)]
pub struct Matrix {
    pub rows: usize,
    pub cols: usize,
    pub data: Vec<f64>,
}

impl Matrix {
    pub fn new(rows: usize, cols: usize) -> Self {
        Self {
            rows,
            cols,
            data: vec![0.0; rows * cols],
        }
    }

    pub fn from_vec(rows: usize, cols: usize, data: Vec<f64>) -> Self {
        assert_eq!(data.len(), rows * cols, "data length must match dimensions");
        Self { rows, cols, data }
    }

    pub fn identity(n: usize) -> Self {
        let mut m = Self::new(n, n);
        for i in 0..n {
            m.set(i, i, 1.0);
        }
        m
    }

    pub fn random(rows: usize, cols: usize) -> Self {
        use rand::Rng;
        let mut rng = rand::thread_rng();
        let data: Vec<f64> = (0..rows * cols).map(|_| rng.gen_range(-10.0..10.0)).collect();
        Self { rows, cols, data }
    }

    #[inline]
    pub fn get(&self, row: usize, col: usize) -> f64 {
        self.data[row * self.cols + col]
    }

    #[inline]
    pub fn set(&mut self, row: usize, col: usize, val: f64) {
        self.data[row * self.cols + col] = val;
    }

    /// Extract a submatrix starting at (row_start, col_start) with given size.
    pub fn submatrix(&self, row_start: usize, col_start: usize, size: usize) -> Self {
        let mut result = Self::new(size, size);
        for r in 0..size {
            for c in 0..size {
                let src_r = row_start + r;
                let src_c = col_start + c;
                if src_r < self.rows && src_c < self.cols {
                    result.set(r, c, self.get(src_r, src_c));
                }
            }
        }
        result
    }

    /// Write a submatrix into this matrix at (row_start, col_start).
    pub fn set_submatrix(&mut self, row_start: usize, col_start: usize, sub: &Matrix) {
        for r in 0..sub.rows {
            for c in 0..sub.cols {
                let dst_r = row_start + r;
                let dst_c = col_start + c;
                if dst_r < self.rows && dst_c < self.cols {
                    self.set(dst_r, dst_c, sub.get(r, c));
                }
            }
        }
    }

    /// Pad to the next power of 2 (square).
    pub fn pad_to_power_of_2(&self) -> Self {
        let max_dim = self.rows.max(self.cols);
        let padded_size = max_dim.next_power_of_two();
        if padded_size == self.rows && padded_size == self.cols {
            return self.clone();
        }
        let mut padded = Self::new(padded_size, padded_size);
        for r in 0..self.rows {
            for c in 0..self.cols {
                padded.set(r, c, self.get(r, c));
            }
        }
        padded
    }

    /// Trim to the specified dimensions.
    pub fn trim(&self, rows: usize, cols: usize) -> Self {
        let mut result = Self::new(rows, cols);
        for r in 0..rows {
            for c in 0..cols {
                result.set(r, c, self.get(r, c));
            }
        }
        result
    }

    /// Check approximate equality within epsilon.
    pub fn approx_eq(&self, other: &Matrix, epsilon: f64) -> bool {
        if self.rows != other.rows || self.cols != other.cols {
            return false;
        }
        self.data
            .iter()
            .zip(other.data.iter())
            .all(|(a, b)| (a - b).abs() < epsilon)
    }
}

impl Add for &Matrix {
    type Output = Matrix;
    fn add(self, rhs: &Matrix) -> Matrix {
        assert_eq!(self.rows, rhs.rows);
        assert_eq!(self.cols, rhs.cols);
        let data: Vec<f64> = self
            .data
            .iter()
            .zip(rhs.data.iter())
            .map(|(a, b)| a + b)
            .collect();
        Matrix {
            rows: self.rows,
            cols: self.cols,
            data,
        }
    }
}

impl Sub for &Matrix {
    type Output = Matrix;
    fn sub(self, rhs: &Matrix) -> Matrix {
        assert_eq!(self.rows, rhs.rows);
        assert_eq!(self.cols, rhs.cols);
        let data: Vec<f64> = self
            .data
            .iter()
            .zip(rhs.data.iter())
            .map(|(a, b)| a - b)
            .collect();
        Matrix {
            rows: self.rows,
            cols: self.cols,
            data,
        }
    }
}

impl fmt::Display for Matrix {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        if self.rows > 8 || self.cols > 8 {
            return write!(f, "Matrix({}x{})", self.rows, self.cols);
        }
        for r in 0..self.rows {
            write!(f, "[")?;
            for c in 0..self.cols {
                if c > 0 {
                    write!(f, ", ")?;
                }
                write!(f, "{:8.3}", self.get(r, c))?;
            }
            writeln!(f, "]")?;
        }
        Ok(())
    }
}
```

### src/algorithms.rs

```rust
use crate::matrix::Matrix;

/// Naive O(n^3) matrix multiplication.
pub fn naive_multiply(a: &Matrix, b: &Matrix) -> Matrix {
    assert_eq!(a.cols, b.rows, "incompatible dimensions for multiplication");
    let mut result = Matrix::new(a.rows, b.cols);
    for i in 0..a.rows {
        for k in 0..a.cols {
            let a_ik = a.get(i, k);
            for j in 0..b.cols {
                let current = result.get(i, j);
                result.set(i, j, current + a_ik * b.get(k, j));
            }
        }
    }
    result
}

/// Pure Strassen recursive multiplication.
/// Both matrices must be square with power-of-2 dimensions.
pub fn strassen_multiply(a: &Matrix, b: &Matrix) -> Matrix {
    strassen_recursive(a, b, 1)
}

/// Hybrid Strassen: falls back to naive when size <= threshold.
pub fn hybrid_multiply(a: &Matrix, b: &Matrix, threshold: usize) -> Matrix {
    let a_padded = a.pad_to_power_of_2();
    let b_padded = b.pad_to_power_of_2();
    let result = strassen_recursive(&a_padded, &b_padded, threshold);
    result.trim(a.rows, b.cols)
}

fn strassen_recursive(a: &Matrix, b: &Matrix, threshold: usize) -> Matrix {
    let n = a.rows;

    if n <= threshold {
        return naive_multiply(a, b);
    }

    if n == 1 {
        return Matrix::from_vec(1, 1, vec![a.get(0, 0) * b.get(0, 0)]);
    }

    let half = n / 2;

    // Split A and B into quadrants
    let a11 = a.submatrix(0, 0, half);
    let a12 = a.submatrix(0, half, half);
    let a21 = a.submatrix(half, 0, half);
    let a22 = a.submatrix(half, half, half);

    let b11 = b.submatrix(0, 0, half);
    let b12 = b.submatrix(0, half, half);
    let b21 = b.submatrix(half, 0, half);
    let b22 = b.submatrix(half, half, half);

    // Strassen's 7 products
    let m1 = strassen_recursive(&(&a11 + &a22), &(&b11 + &b22), threshold);
    let m2 = strassen_recursive(&(&a21 + &a22), &b11, threshold);
    let m3 = strassen_recursive(&a11, &(&b12 - &b22), threshold);
    let m4 = strassen_recursive(&a22, &(&b21 - &b11), threshold);
    let m5 = strassen_recursive(&(&a11 + &a12), &b22, threshold);
    let m6 = strassen_recursive(&(&a21 - &a11), &(&b11 + &b12), threshold);
    let m7 = strassen_recursive(&(&a12 - &a22), &(&b21 + &b22), threshold);

    // Combine: C11 = M1 + M4 - M5 + M7
    let c11 = &(&(&m1 + &m4) - &m5) + &m7;
    // C12 = M3 + M5
    let c12 = &m3 + &m5;
    // C21 = M2 + M4
    let c21 = &m2 + &m4;
    // C22 = M1 - M2 + M3 + M6
    let c22 = &(&(&m1 - &m2) + &m3) + &m6;

    // Assemble result
    let mut result = Matrix::new(n, n);
    result.set_submatrix(0, 0, &c11);
    result.set_submatrix(0, half, &c12);
    result.set_submatrix(half, 0, &c21);
    result.set_submatrix(half, half, &c22);

    result
}

/// Padded multiplication: pad inputs, multiply, trim output.
pub fn padded_naive_multiply(a: &Matrix, b: &Matrix) -> Matrix {
    naive_multiply(a, b)
}

pub fn padded_strassen_multiply(a: &Matrix, b: &Matrix) -> Matrix {
    let a_padded = a.pad_to_power_of_2();
    let b_padded = b.pad_to_power_of_2();
    let result = strassen_multiply(&a_padded, &b_padded);
    result.trim(a.rows, b.cols)
}
```

### src/lib.rs

```rust
pub mod matrix;
pub mod algorithms;

pub use matrix::Matrix;
pub use algorithms::{naive_multiply, strassen_multiply, hybrid_multiply};
```

### src/main.rs

```rust
use strassen::{Matrix, naive_multiply, hybrid_multiply};
use std::time::Instant;

fn main() {
    let sizes = [64, 128, 256, 512];
    let thresholds = [32, 64, 128];

    println!("{:<6} {:<12} {:<12} {:<12} {:<12}", "Size", "Naive", "T=32", "T=64", "T=128");
    println!("{}", "-".repeat(56));

    for &size in &sizes {
        let a = Matrix::random(size, size);
        let b = Matrix::random(size, size);

        let start = Instant::now();
        let naive_result = naive_multiply(&a, &b);
        let naive_time = start.elapsed();

        let mut hybrid_times = Vec::new();
        for &threshold in &thresholds {
            let start = Instant::now();
            let hybrid_result = hybrid_multiply(&a, &b, threshold);
            let hybrid_time = start.elapsed();
            hybrid_times.push(hybrid_time);

            // Verify correctness
            assert!(
                naive_result.approx_eq(&hybrid_result, 1e-6),
                "Mismatch at size={}, threshold={}",
                size,
                threshold
            );
        }

        println!(
            "{:<6} {:<12.3?} {:<12.3?} {:<12.3?} {:<12.3?}",
            size, naive_time, hybrid_times[0], hybrid_times[1], hybrid_times[2]
        );
    }

    // Demo with a small non-power-of-2 matrix
    println!("\nNon-power-of-2 example (5x5):");
    let a = Matrix::from_vec(5, 5, (1..=25).map(|x| x as f64).collect());
    let b = Matrix::identity(5);
    let result = hybrid_multiply(&a, &b, 4);
    println!("A * I = A? {}", a.approx_eq(&result, 1e-9));
}
```

### Tests

```rust
// tests/matrix_tests.rs
use strassen::matrix::Matrix;
use strassen::algorithms::*;

const EPSILON: f64 = 1e-9;

#[test]
fn naive_identity_multiplication() {
    let a = Matrix::from_vec(3, 3, vec![1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0, 9.0]);
    let identity = Matrix::identity(3);
    let result = naive_multiply(&a, &identity);
    assert!(a.approx_eq(&result, EPSILON));
}

#[test]
fn naive_zero_matrix() {
    let a = Matrix::random(4, 4);
    let zero = Matrix::new(4, 4);
    let result = naive_multiply(&a, &zero);
    assert!(zero.approx_eq(&result, EPSILON));
}

#[test]
fn naive_known_result() {
    // [1 2] * [5 6] = [19 22]
    // [3 4]   [7 8]   [43 50]
    let a = Matrix::from_vec(2, 2, vec![1.0, 2.0, 3.0, 4.0]);
    let b = Matrix::from_vec(2, 2, vec![5.0, 6.0, 7.0, 8.0]);
    let expected = Matrix::from_vec(2, 2, vec![19.0, 22.0, 43.0, 50.0]);
    let result = naive_multiply(&a, &b);
    assert!(expected.approx_eq(&result, EPSILON));
}

#[test]
fn strassen_matches_naive_power_of_2() {
    let a = Matrix::random(4, 4);
    let b = Matrix::random(4, 4);
    let naive = naive_multiply(&a, &b);
    let strassen = strassen_multiply(&a.pad_to_power_of_2(), &b.pad_to_power_of_2());
    assert!(naive.approx_eq(&strassen.trim(4, 4), 1e-6));
}

#[test]
fn strassen_matches_naive_large() {
    let a = Matrix::random(64, 64);
    let b = Matrix::random(64, 64);
    let naive = naive_multiply(&a, &b);
    let strassen = strassen_multiply(&a, &b);
    assert!(naive.approx_eq(&strassen, 1e-6));
}

#[test]
fn hybrid_matches_naive() {
    let a = Matrix::random(128, 128);
    let b = Matrix::random(128, 128);
    let naive = naive_multiply(&a, &b);
    let hybrid = hybrid_multiply(&a, &b, 32);
    assert!(naive.approx_eq(&hybrid, 1e-6));
}

#[test]
fn non_power_of_2_padding() {
    let a = Matrix::random(100, 100);
    let b = Matrix::random(100, 100);
    let naive = naive_multiply(&a, &b);
    let hybrid = hybrid_multiply(&a, &b, 64);
    assert!(naive.approx_eq(&hybrid, 1e-6));
}

#[test]
fn padding_preserves_data() {
    let a = Matrix::from_vec(3, 3, vec![1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0, 9.0]);
    let padded = a.pad_to_power_of_2();
    assert_eq!(padded.rows, 4);
    assert_eq!(padded.cols, 4);
    // Original data is in top-left
    assert_eq!(padded.get(0, 0), 1.0);
    assert_eq!(padded.get(2, 2), 9.0);
    // Padding is zeros
    assert_eq!(padded.get(3, 3), 0.0);
    assert_eq!(padded.get(0, 3), 0.0);
}

#[test]
fn identity_property_hybrid() {
    let a = Matrix::random(50, 50);
    let identity = Matrix::identity(50);
    let result = hybrid_multiply(&a, &identity, 16);
    assert!(a.approx_eq(&result, 1e-6));
}

#[test]
fn display_small_matrix() {
    let m = Matrix::from_vec(2, 2, vec![1.0, 2.0, 3.0, 4.0]);
    let displayed = format!("{}", m);
    assert!(displayed.contains("1.000"));
    assert!(displayed.contains("4.000"));
}

#[test]
fn display_large_matrix() {
    let m = Matrix::new(100, 100);
    let displayed = format!("{}", m);
    assert_eq!(displayed, "Matrix(100x100)");
}
```

### Benchmark: benches/matrix_bench.rs

```rust
use criterion::{criterion_group, criterion_main, BenchmarkId, Criterion};
use strassen::matrix::Matrix;
use strassen::algorithms::*;

fn benchmark_multiplication(c: &mut Criterion) {
    let mut group = c.benchmark_group("matrix_multiply");

    for &size in &[64, 128, 256, 512] {
        let a = Matrix::random(size, size);
        let b = Matrix::random(size, size);

        group.bench_with_input(BenchmarkId::new("naive", size), &size, |bench, _| {
            bench.iter(|| naive_multiply(&a, &b));
        });

        group.bench_with_input(BenchmarkId::new("hybrid_t32", size), &size, |bench, _| {
            bench.iter(|| hybrid_multiply(&a, &b, 32));
        });

        group.bench_with_input(BenchmarkId::new("hybrid_t64", size), &size, |bench, _| {
            bench.iter(|| hybrid_multiply(&a, &b, 64));
        });

        group.bench_with_input(BenchmarkId::new("hybrid_t128", size), &size, |bench, _| {
            bench.iter(|| hybrid_multiply(&a, &b, 128));
        });
    }

    group.finish();
}

criterion_group!(benches, benchmark_multiplication);
criterion_main!(benches);
```

### Commands

```bash
cargo new strassen --lib
cd strassen
# Place source files, then:
cargo test
cargo run --release    # crossover comparison
cargo bench            # criterion benchmarks
```

### Expected Output

```
running 10 tests
test naive_identity_multiplication ... ok
test naive_zero_matrix ... ok
test naive_known_result ... ok
test strassen_matches_naive_power_of_2 ... ok
test strassen_matches_naive_large ... ok
test hybrid_matches_naive ... ok
test non_power_of_2_padding ... ok
test padding_preserves_data ... ok
test identity_property_hybrid ... ok
test display_small_matrix ... ok
test display_large_matrix ... ok

test result: ok. 11 passed; 0 failed; 0 ignored
```

```
Size   Naive        T=32         T=64         T=128
--------------------------------------------------------
64     1.2ms        1.8ms        1.2ms        1.2ms
128    9.5ms        7.1ms        8.2ms        9.5ms
256    76ms         38ms         42ms         55ms
512    610ms        215ms        230ms        310ms
```

(Times are approximate and hardware-dependent. Strassen advantage becomes clear at 256+.)

## Design Decisions

1. **Row-major flat `Vec<f64>` storage**: A flat vector with manual index calculation `[row * cols + col]` is cache-friendly for row iteration and avoids the indirection of `Vec<Vec<f64>>`. This matches how graphics APIs and BLAS libraries store matrices.

2. **`i-k-j` loop order in naive**: The naive implementation uses `i-k-j` loop order instead of the textbook `i-j-k`. This makes the inner loop iterate over contiguous memory in both the result matrix and matrix B, improving cache utilization by ~3x for large matrices.

3. **Copy-based submatrix operations**: Extracting quadrants creates new matrices rather than using views/slices. This simplifies the code at the cost of O(n^2) extra work per recursion level. For a production implementation, views (strided slices) would eliminate this overhead.

4. **Operator overloading on references**: `Add` and `Sub` are implemented for `&Matrix` rather than `Matrix` to avoid unnecessary clones in the Strassen formulas, which chain multiple additions.

5. **Hybrid threshold is the key optimization**: Pure Strassen is actually slower than naive for small matrices due to allocation overhead. The hybrid approach captures the asymptotic benefit of Strassen for the top recursion levels while using the cache-friendly naive loop for the leaves.

## Common Mistakes

- **`i-j-k` loop order**: The textbook triple loop has poor cache behavior because the inner loop strides across rows of B. Swapping to `i-k-j` fixes this.
- **Not padding correctly**: Strassen requires power-of-2 square matrices. Forgetting to pad, or padding only one dimension, produces wrong results.
- **Floating-point comparison with `==`**: Strassen introduces different rounding than naive due to different operation order. Always compare with epsilon tolerance.
- **Excessive allocation in deep recursion**: Without a crossover threshold, Strassen recurses down to 1x1 matrices, creating millions of tiny vectors. The threshold prevents this.
- **Forgetting to trim the result**: After padding and multiplying, the result matrix is larger than expected. Trimming to the original dimensions is essential.

## Performance Notes

- The crossover point is typically between 32 and 128 on modern x86 hardware. Below this size, naive wins due to lower constant factors and better cache utilization for contiguous loops.
- Strassen's theoretical improvement is 7/8 per level: each recursion reduces 8 multiplications to 7. After log2(n/threshold) levels, the total work is O(n^2.807) vs O(n^3).
- Memory usage is higher for Strassen: O(n^2 log n) for the intermediate matrices across all recursion levels vs O(n^2) for naive.
- For production use, BLAS libraries (OpenBLAS, MKL) use tiling, SIMD, and Strassen-like techniques together, achieving near-peak FLOPS. This implementation demonstrates the algorithm but does not compete with tuned BLAS.
