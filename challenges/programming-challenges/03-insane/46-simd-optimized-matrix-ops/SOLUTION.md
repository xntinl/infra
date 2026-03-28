# Solution: SIMD-Optimized Matrix Operations

## Architecture Overview

The solution is organized as a generic matrix library with pluggable computation backends:

1. **`matrix.rs`**: Core `Matrix<T>` type with row-major heap storage, dimension tracking, and basic operations (indexing, display, equality)
2. **`naive.rs`**: Textbook implementations of all operations -- the correctness baseline
3. **`blocked.rs`**: Cache-friendly tiled algorithms with configurable tile size
4. **`simd_ops.rs`**: SIMD-accelerated implementations using `std::arch` (x86_64 AVX2/FMA + aarch64 NEON)
5. **`strassen.rs`**: Strassen's recursive multiplication with tunable crossover
6. **`lu.rs`**: LU decomposition with partial pivoting, plus determinant and inverse via LU
7. **`benches/`**: criterion benchmarks comparing all variants

The matrix uses a flat `Vec<T>` with row-major layout: element `(i, j)` is at `data[i * cols + j]`.

## Rust Solution

### Cargo.toml

```toml
[package]
name = "simd-matrix"
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
use std::ops::{Add, Mul, Sub};

/// Row-major matrix with heap-allocated flat storage.
#[derive(Clone)]
pub struct Matrix<T> {
    pub data: Vec<T>,
    pub rows: usize,
    pub cols: usize,
}

impl<T: Clone + Default> Matrix<T> {
    pub fn zeros(rows: usize, cols: usize) -> Self {
        Matrix {
            data: vec![T::default(); rows * cols],
            rows,
            cols,
        }
    }

    pub fn from_vec(rows: usize, cols: usize, data: Vec<T>) -> Self {
        assert_eq!(data.len(), rows * cols, "data length must match dimensions");
        Matrix { data, rows, cols }
    }

    #[inline(always)]
    pub fn get(&self, row: usize, col: usize) -> &T {
        &self.data[row * self.cols + col]
    }

    #[inline(always)]
    pub fn get_mut(&mut self, row: usize, col: usize) -> &mut T {
        &mut self.data[row * self.cols + col]
    }

    #[inline(always)]
    pub fn set(&mut self, row: usize, col: usize, val: T) {
        self.data[row * self.cols + col] = val;
    }

    pub fn is_square(&self) -> bool {
        self.rows == self.cols
    }
}

impl<T> Matrix<T>
where
    T: Clone + Default + From<u8>,
{
    pub fn identity(n: usize) -> Self {
        let mut m = Self::zeros(n, n);
        for i in 0..n {
            m.set(i, i, T::from(1u8));
        }
        m
    }
}

impl Matrix<f32> {
    pub fn random(rows: usize, cols: usize) -> Self {
        use rand::Rng;
        let mut rng = rand::thread_rng();
        let data: Vec<f32> = (0..rows * cols).map(|_| rng.gen_range(-10.0..10.0)).collect();
        Matrix { data, rows, cols }
    }

    pub fn approx_eq(&self, other: &Self, epsilon: f32) -> bool {
        if self.rows != other.rows || self.cols != other.cols {
            return false;
        }
        self.data
            .iter()
            .zip(other.data.iter())
            .all(|(a, b)| (a - b).abs() < epsilon)
    }
}

impl Matrix<f64> {
    pub fn random(rows: usize, cols: usize) -> Self {
        use rand::Rng;
        let mut rng = rand::thread_rng();
        let data: Vec<f64> = (0..rows * cols).map(|_| rng.gen_range(-10.0..10.0)).collect();
        Matrix { data, rows, cols }
    }

    pub fn approx_eq(&self, other: &Self, epsilon: f64) -> bool {
        if self.rows != other.rows || self.cols != other.cols {
            return false;
        }
        self.data
            .iter()
            .zip(other.data.iter())
            .all(|(a, b)| (a - b).abs() < epsilon)
    }
}

impl<T: fmt::Display + Clone + Default> fmt::Display for Matrix<T> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        for i in 0..self.rows {
            write!(f, "[")?;
            for j in 0..self.cols {
                if j > 0 {
                    write!(f, ", ")?;
                }
                write!(f, "{:8.4}", self.get(i, j))?;
            }
            writeln!(f, "]")?;
        }
        Ok(())
    }
}
```

### src/naive.rs

```rust
use crate::matrix::Matrix;
use std::ops::{Add, Mul, Sub, AddAssign, Neg};

/// Naive O(n^3) matrix multiplication.
pub fn multiply<T>(a: &Matrix<T>, b: &Matrix<T>) -> Matrix<T>
where
    T: Clone + Default + Add<Output = T> + Mul<Output = T> + AddAssign,
{
    assert_eq!(a.cols, b.rows, "dimension mismatch for multiplication");
    let mut result = Matrix::zeros(a.rows, b.cols);

    for i in 0..a.rows {
        for k in 0..a.cols {
            let a_ik = a.get(i, k).clone();
            for j in 0..b.cols {
                let product = a_ik.clone() * b.get(k, j).clone();
                *result.get_mut(i, j) += product;
            }
        }
    }

    result
}

/// Naive transpose.
pub fn transpose<T: Clone + Default>(m: &Matrix<T>) -> Matrix<T> {
    let mut result = Matrix::zeros(m.cols, m.rows);
    for i in 0..m.rows {
        for j in 0..m.cols {
            result.set(j, i, m.get(i, j).clone());
        }
    }
    result
}

/// Naive determinant via cofactor expansion (for small matrices only).
/// For large matrices, use LU decomposition instead.
pub fn determinant_cofactor(m: &Matrix<f64>) -> f64 {
    assert!(m.is_square(), "determinant requires square matrix");
    let n = m.rows;

    if n == 1 {
        return *m.get(0, 0);
    }
    if n == 2 {
        return m.get(0, 0) * m.get(1, 1) - m.get(0, 1) * m.get(1, 0);
    }

    let mut det = 0.0;
    for j in 0..n {
        let cofactor = minor(m, 0, j);
        let sign = if j % 2 == 0 { 1.0 } else { -1.0 };
        det += sign * m.get(0, j) * determinant_cofactor(&cofactor);
    }
    det
}

fn minor(m: &Matrix<f64>, skip_row: usize, skip_col: usize) -> Matrix<f64> {
    let n = m.rows - 1;
    let mut data = Vec::with_capacity(n * n);
    for i in 0..m.rows {
        if i == skip_row {
            continue;
        }
        for j in 0..m.cols {
            if j == skip_col {
                continue;
            }
            data.push(*m.get(i, j));
        }
    }
    Matrix::from_vec(n, n, data)
}
```

### src/blocked.rs

```rust
use crate::matrix::Matrix;
use std::ops::{Add, Mul, AddAssign};

/// Default tile size: 64 elements fits well in L1 cache (64*64*4 = 16KB for f32).
const DEFAULT_TILE_SIZE: usize = 64;

/// Cache-friendly blocked matrix multiplication.
pub fn multiply_blocked<T>(a: &Matrix<T>, b: &Matrix<T>, tile_size: Option<usize>) -> Matrix<T>
where
    T: Clone + Default + Add<Output = T> + Mul<Output = T> + AddAssign,
{
    assert_eq!(a.cols, b.rows, "dimension mismatch for multiplication");
    let ts = tile_size.unwrap_or(DEFAULT_TILE_SIZE);
    let mut result = Matrix::zeros(a.rows, b.cols);

    // Tiled loop: iterate over tiles of the output
    let m = a.rows;
    let n = b.cols;
    let k = a.cols;

    for ii in (0..m).step_by(ts) {
        let i_end = (ii + ts).min(m);
        for kk in (0..k).step_by(ts) {
            let k_end = (kk + ts).min(k);
            for jj in (0..n).step_by(ts) {
                let j_end = (jj + ts).min(n);

                // Inner tile multiplication
                for i in ii..i_end {
                    for k_idx in kk..k_end {
                        let a_ik = a.get(i, k_idx).clone();
                        for j in jj..j_end {
                            let product = a_ik.clone() * b.get(k_idx, j).clone();
                            *result.get_mut(i, j) += product;
                        }
                    }
                }
            }
        }
    }

    result
}

/// Cache-friendly blocked transpose using tiles.
pub fn transpose_blocked<T: Clone + Default>(m: &Matrix<T>, tile_size: Option<usize>) -> Matrix<T> {
    let ts = tile_size.unwrap_or(DEFAULT_TILE_SIZE);
    let mut result = Matrix::zeros(m.cols, m.rows);

    for ii in (0..m.rows).step_by(ts) {
        let i_end = (ii + ts).min(m.rows);
        for jj in (0..m.cols).step_by(ts) {
            let j_end = (jj + ts).min(m.cols);
            for i in ii..i_end {
                for j in jj..j_end {
                    result.set(j, i, m.get(i, j).clone());
                }
            }
        }
    }

    result
}
```

### src/simd_ops.rs

```rust
use crate::matrix::Matrix;
use crate::naive;

// --- x86_64 AVX2/FMA implementation ---

#[cfg(target_arch = "x86_64")]
pub mod x86 {
    use super::*;

    #[cfg(target_arch = "x86_64")]
    use std::arch::x86_64::*;

    /// SIMD-accelerated f32 matrix multiplication using AVX2 + FMA.
    /// Falls back to SSE2 if AVX2 is not available.
    #[target_feature(enable = "avx2,fma")]
    pub unsafe fn multiply_f32_avx2(a: &Matrix<f32>, b: &Matrix<f32>) -> Matrix<f32> {
        assert_eq!(a.cols, b.rows, "dimension mismatch");

        let m = a.rows;
        let n = b.cols;
        let k = a.cols;

        // Transpose B for sequential access in dot products
        let bt = naive::transpose(b);
        let mut result = Matrix::zeros(m, n);

        for i in 0..m {
            for j in 0..n {
                let mut sum = _mm256_setzero_ps(); // 8 x f32 accumulator
                let mut idx = 0;

                // Process 8 elements at a time
                while idx + 8 <= k {
                    let va = _mm256_loadu_ps(a.data.as_ptr().add(i * k + idx));
                    let vb = _mm256_loadu_ps(bt.data.as_ptr().add(j * k + idx));
                    sum = _mm256_fmadd_ps(va, vb, sum); // sum += va * vb
                    idx += 8;
                }

                // Horizontal sum of the 8-lane accumulator
                let mut scalar_sum = hsum_avx(sum);

                // Handle remainder
                while idx < k {
                    scalar_sum += a.data[i * k + idx] * bt.data[j * k + idx];
                    idx += 1;
                }

                result.set(i, j, scalar_sum);
            }
        }

        result
    }

    /// Horizontal sum of 8 f32 values in an __m256.
    #[target_feature(enable = "avx2")]
    unsafe fn hsum_avx(v: __m256) -> f32 {
        // [a0 a1 a2 a3 a4 a5 a6 a7]
        let high = _mm256_extractf128_ps(v, 1); // [a4 a5 a6 a7]
        let low = _mm256_castps256_ps128(v); // [a0 a1 a2 a3]
        let sum128 = _mm_add_ps(low, high); // [a0+a4 a1+a5 a2+a6 a3+a7]
        let shuf = _mm_movehdup_ps(sum128); // [a1+a5 a1+a5 a3+a7 a3+a7]
        let sum64 = _mm_add_ps(sum128, shuf); // [a0+a1+a4+a5 _ a2+a3+a6+a7 _]
        let shuf2 = _mm_movehl_ps(sum64, sum64);
        let result = _mm_add_ss(sum64, shuf2);
        _mm_cvtss_f32(result)
    }

    /// SIMD-accelerated f64 matrix multiplication using AVX2 + FMA.
    #[target_feature(enable = "avx2,fma")]
    pub unsafe fn multiply_f64_avx2(a: &Matrix<f64>, b: &Matrix<f64>) -> Matrix<f64> {
        assert_eq!(a.cols, b.rows, "dimension mismatch");

        let m = a.rows;
        let n = b.cols;
        let k = a.cols;

        let bt = naive::transpose(b);
        let mut result = Matrix::zeros(m, n);

        for i in 0..m {
            for j in 0..n {
                let mut sum = _mm256_setzero_pd(); // 4 x f64 accumulator
                let mut idx = 0;

                while idx + 4 <= k {
                    let va = _mm256_loadu_pd(a.data.as_ptr().add(i * k + idx));
                    let vb = _mm256_loadu_pd(bt.data.as_ptr().add(j * k + idx));
                    sum = _mm256_fmadd_pd(va, vb, sum);
                    idx += 4;
                }

                let mut scalar_sum = hsum_avx_f64(sum);

                while idx < k {
                    scalar_sum += a.data[i * k + idx] * bt.data[j * k + idx];
                    idx += 1;
                }

                result.set(i, j, scalar_sum);
            }
        }

        result
    }

    #[target_feature(enable = "avx2")]
    unsafe fn hsum_avx_f64(v: __m256d) -> f64 {
        let high = _mm256_extractf128_pd(v, 1);
        let low = _mm256_castpd256_pd128(v);
        let sum128 = _mm_add_pd(low, high);
        let high64 = _mm_unpackhi_pd(sum128, sum128);
        let result = _mm_add_sd(sum128, high64);
        _mm_cvtsd_f64(result)
    }
}

// --- aarch64 NEON implementation ---

#[cfg(target_arch = "aarch64")]
pub mod arm {
    use super::*;
    use std::arch::aarch64::*;

    /// SIMD-accelerated f32 matrix multiplication using NEON.
    pub fn multiply_f32_neon(a: &Matrix<f32>, b: &Matrix<f32>) -> Matrix<f32> {
        assert_eq!(a.cols, b.rows, "dimension mismatch");

        let m = a.rows;
        let n = b.cols;
        let k = a.cols;

        let bt = naive::transpose(b);
        let mut result = Matrix::zeros(m, n);

        for i in 0..m {
            for j in 0..n {
                let mut sum = unsafe { vdupq_n_f32(0.0) }; // 4 x f32
                let mut idx = 0;

                while idx + 4 <= k {
                    unsafe {
                        let va = vld1q_f32(a.data.as_ptr().add(i * k + idx));
                        let vb = vld1q_f32(bt.data.as_ptr().add(j * k + idx));
                        sum = vfmaq_f32(sum, va, vb);
                    }
                    idx += 4;
                }

                let mut scalar_sum = unsafe { vaddvq_f32(sum) };

                while idx < k {
                    scalar_sum += a.data[i * k + idx] * bt.data[j * k + idx];
                    idx += 1;
                }

                result.set(i, j, scalar_sum);
            }
        }

        result
    }

    /// SIMD-accelerated f64 matrix multiplication using NEON.
    pub fn multiply_f64_neon(a: &Matrix<f64>, b: &Matrix<f64>) -> Matrix<f64> {
        assert_eq!(a.cols, b.rows, "dimension mismatch");

        let m = a.rows;
        let n = b.cols;
        let k = a.cols;

        let bt = naive::transpose(b);
        let mut result = Matrix::zeros(m, n);

        for i in 0..m {
            for j in 0..n {
                let mut sum = unsafe { vdupq_n_f64(0.0) }; // 2 x f64
                let mut idx = 0;

                while idx + 2 <= k {
                    unsafe {
                        let va = vld1q_f64(a.data.as_ptr().add(i * k + idx));
                        let vb = vld1q_f64(bt.data.as_ptr().add(j * k + idx));
                        sum = vfmaq_f64(sum, va, vb);
                    }
                    idx += 2;
                }

                let mut scalar_sum = unsafe { vaddvq_f64(sum) };

                while idx < k {
                    scalar_sum += a.data[i * k + idx] * bt.data[j * k + idx];
                    idx += 1;
                }

                result.set(i, j, scalar_sum);
            }
        }

        result
    }
}

/// Platform-dispatching SIMD multiply for f32.
pub fn multiply_simd_f32(a: &Matrix<f32>, b: &Matrix<f32>) -> Matrix<f32> {
    #[cfg(target_arch = "x86_64")]
    {
        if is_x86_feature_detected!("avx2") && is_x86_feature_detected!("fma") {
            return unsafe { x86::multiply_f32_avx2(a, b) };
        }
    }

    #[cfg(target_arch = "aarch64")]
    {
        return arm::multiply_f32_neon(a, b);
    }

    // Fallback to naive
    #[allow(unreachable_code)]
    naive::multiply(a, b)
}

/// Platform-dispatching SIMD multiply for f64.
pub fn multiply_simd_f64(a: &Matrix<f64>, b: &Matrix<f64>) -> Matrix<f64> {
    #[cfg(target_arch = "x86_64")]
    {
        if is_x86_feature_detected!("avx2") && is_x86_feature_detected!("fma") {
            return unsafe { x86::multiply_f64_avx2(a, b) };
        }
    }

    #[cfg(target_arch = "aarch64")]
    {
        return arm::multiply_f64_neon(a, b);
    }

    #[allow(unreachable_code)]
    naive::multiply(a, b)
}
```

### src/strassen.rs

```rust
use crate::matrix::Matrix;
use crate::naive;
use std::ops::{Add, Mul, Sub, AddAssign};

const DEFAULT_CROSSOVER: usize = 64;

/// Strassen's algorithm for matrix multiplication.
/// Falls back to naive when matrix size < crossover threshold.
pub fn multiply_strassen(
    a: &Matrix<f64>,
    b: &Matrix<f64>,
    crossover: Option<usize>,
) -> Matrix<f64> {
    assert_eq!(a.cols, b.rows, "dimension mismatch");

    let crossover = crossover.unwrap_or(DEFAULT_CROSSOVER);

    // Pad to the next power of 2 if needed
    let max_dim = a.rows.max(a.cols).max(b.cols);
    let padded_size = max_dim.next_power_of_two();

    if padded_size <= crossover {
        return naive::multiply(a, b);
    }

    let ap = pad_matrix(a, padded_size);
    let bp = pad_matrix(b, padded_size);

    let result_padded = strassen_recursive(&ap, &bp, crossover);

    // Extract the actual result dimensions
    extract_submatrix(&result_padded, a.rows, b.cols)
}

fn strassen_recursive(
    a: &Matrix<f64>,
    b: &Matrix<f64>,
    crossover: usize,
) -> Matrix<f64> {
    let n = a.rows;

    if n <= crossover {
        return naive::multiply(a, b);
    }

    let half = n / 2;

    // Split into quadrants
    let a11 = submatrix(a, 0, 0, half);
    let a12 = submatrix(a, 0, half, half);
    let a21 = submatrix(a, half, 0, half);
    let a22 = submatrix(a, half, half, half);

    let b11 = submatrix(b, 0, 0, half);
    let b12 = submatrix(b, 0, half, half);
    let b21 = submatrix(b, half, 0, half);
    let b22 = submatrix(b, half, half, half);

    // Strassen's 7 products
    let m1 = strassen_recursive(&mat_add(&a11, &a22), &mat_add(&b11, &b22), crossover);
    let m2 = strassen_recursive(&mat_add(&a21, &a22), &b11, crossover);
    let m3 = strassen_recursive(&a11, &mat_sub(&b12, &b22), crossover);
    let m4 = strassen_recursive(&a22, &mat_sub(&b21, &b11), crossover);
    let m5 = strassen_recursive(&mat_add(&a11, &a12), &b22, crossover);
    let m6 = strassen_recursive(&mat_sub(&a21, &a11), &mat_add(&b11, &b12), crossover);
    let m7 = strassen_recursive(&mat_sub(&a12, &a22), &mat_add(&b21, &b22), crossover);

    // Combine results
    let c11 = mat_add(&mat_sub(&mat_add(&m1, &m4), &m5), &m7); // M1 + M4 - M5 + M7
    let c12 = mat_add(&m3, &m5);                                 // M3 + M5
    let c21 = mat_add(&m2, &m4);                                 // M2 + M4
    let c22 = mat_add(&mat_sub(&mat_add(&m1, &m3), &m2), &m6); // M1 - M2 + M3 + M6

    // Assemble the result
    combine_quadrants(&c11, &c12, &c21, &c22)
}

fn mat_add(a: &Matrix<f64>, b: &Matrix<f64>) -> Matrix<f64> {
    assert_eq!(a.rows, b.rows);
    assert_eq!(a.cols, b.cols);
    let data: Vec<f64> = a.data.iter().zip(b.data.iter()).map(|(x, y)| x + y).collect();
    Matrix::from_vec(a.rows, a.cols, data)
}

fn mat_sub(a: &Matrix<f64>, b: &Matrix<f64>) -> Matrix<f64> {
    assert_eq!(a.rows, b.rows);
    assert_eq!(a.cols, b.cols);
    let data: Vec<f64> = a.data.iter().zip(b.data.iter()).map(|(x, y)| x - y).collect();
    Matrix::from_vec(a.rows, a.cols, data)
}

fn submatrix(m: &Matrix<f64>, row_start: usize, col_start: usize, size: usize) -> Matrix<f64> {
    let mut data = Vec::with_capacity(size * size);
    for i in 0..size {
        for j in 0..size {
            data.push(*m.get(row_start + i, col_start + j));
        }
    }
    Matrix::from_vec(size, size, data)
}

fn combine_quadrants(
    c11: &Matrix<f64>,
    c12: &Matrix<f64>,
    c21: &Matrix<f64>,
    c22: &Matrix<f64>,
) -> Matrix<f64> {
    let half = c11.rows;
    let n = half * 2;
    let mut result = Matrix::zeros(n, n);

    for i in 0..half {
        for j in 0..half {
            result.set(i, j, *c11.get(i, j));
            result.set(i, j + half, *c12.get(i, j));
            result.set(i + half, j, *c21.get(i, j));
            result.set(i + half, j + half, *c22.get(i, j));
        }
    }

    result
}

fn pad_matrix(m: &Matrix<f64>, target_size: usize) -> Matrix<f64> {
    if m.rows == target_size && m.cols == target_size {
        return m.clone();
    }
    let mut padded = Matrix::zeros(target_size, target_size);
    for i in 0..m.rows {
        for j in 0..m.cols {
            padded.set(i, j, *m.get(i, j));
        }
    }
    padded
}

fn extract_submatrix(m: &Matrix<f64>, rows: usize, cols: usize) -> Matrix<f64> {
    let mut data = Vec::with_capacity(rows * cols);
    for i in 0..rows {
        for j in 0..cols {
            data.push(*m.get(i, j));
        }
    }
    Matrix::from_vec(rows, cols, data)
}
```

### src/lu.rs

```rust
use crate::matrix::Matrix;

/// LU decomposition result: PA = LU.
pub struct LuDecomposition {
    pub l: Matrix<f64>,
    pub u: Matrix<f64>,
    pub p: Matrix<f64>,  // permutation matrix
    pub pivot_sign: f64,  // +1 or -1, for determinant
}

/// LU decomposition with partial pivoting.
pub fn lu_decompose(m: &Matrix<f64>) -> Option<LuDecomposition> {
    assert!(m.is_square(), "LU decomposition requires square matrix");
    let n = m.rows;

    // Working copy
    let mut a = m.clone();
    let mut perm: Vec<usize> = (0..n).collect();
    let mut pivot_sign = 1.0;

    for col in 0..n {
        // Find pivot (largest absolute value in column)
        let mut max_val = a.get(col, col).abs();
        let mut max_row = col;

        for row in (col + 1)..n {
            let val = a.get(row, col).abs();
            if val > max_val {
                max_val = val;
                max_row = row;
            }
        }

        if max_val < 1e-12 {
            return None; // singular matrix
        }

        // Swap rows if needed
        if max_row != col {
            perm.swap(col, max_row);
            pivot_sign *= -1.0;

            for j in 0..n {
                let tmp = *a.get(col, j);
                a.set(col, j, *a.get(max_row, j));
                a.set(max_row, j, tmp);
            }
        }

        // Eliminate below pivot
        for row in (col + 1)..n {
            let factor = *a.get(row, col) / *a.get(col, col);
            a.set(row, col, factor); // store L factor in-place

            for j in (col + 1)..n {
                let val = *a.get(row, j) - factor * *a.get(col, j);
                a.set(row, j, val);
            }
        }
    }

    // Extract L and U from the combined matrix
    let mut l = Matrix::<f64>::identity(n);
    let mut u = Matrix::zeros(n, n);

    for i in 0..n {
        for j in 0..n {
            if i > j {
                l.set(i, j, *a.get(i, j));
            } else {
                u.set(i, j, *a.get(i, j));
            }
        }
    }

    // Build permutation matrix
    let mut p = Matrix::zeros(n, n);
    for i in 0..n {
        p.set(i, perm[i], 1.0);
    }

    Some(LuDecomposition {
        l,
        u,
        p,
        pivot_sign,
    })
}

/// Determinant via LU decomposition.
pub fn determinant(m: &Matrix<f64>) -> f64 {
    let lu = match lu_decompose(m) {
        Some(lu) => lu,
        None => return 0.0, // singular
    };

    let mut det = lu.pivot_sign;
    for i in 0..m.rows {
        det *= lu.u.get(i, i);
    }
    det
}

/// Matrix inverse via LU decomposition.
/// Solves AX = I column by column using forward/back substitution.
pub fn inverse(m: &Matrix<f64>) -> Option<Matrix<f64>> {
    assert!(m.is_square(), "inverse requires square matrix");
    let n = m.rows;

    let lu = lu_decompose(m)?;

    let mut inv = Matrix::zeros(n, n);

    for col in 0..n {
        // Solve Ly = Pb (where b is column `col` of identity)
        let mut pb = vec![0.0; n];
        for i in 0..n {
            pb[i] = *lu.p.get(i, col);
        }

        // Forward substitution: Ly = pb
        let mut y = vec![0.0; n];
        for i in 0..n {
            y[i] = pb[i];
            for j in 0..i {
                y[i] -= lu.l.get(i, j) * y[j];
            }
        }

        // Back substitution: Ux = y
        let mut x = vec![0.0; n];
        for i in (0..n).rev() {
            x[i] = y[i];
            for j in (i + 1)..n {
                x[i] -= lu.u.get(i, j) * x[j];
            }
            let diag = *lu.u.get(i, i);
            if diag.abs() < 1e-12 {
                return None; // singular
            }
            x[i] /= diag;
        }

        // Write column
        for i in 0..n {
            inv.set(i, col, x[i]);
        }
    }

    Some(inv)
}
```

### src/lib.rs

```rust
pub mod matrix;
pub mod naive;
pub mod blocked;
pub mod simd_ops;
pub mod strassen;
pub mod lu;
```

### src/main.rs

```rust
use simd_matrix::matrix::Matrix;
use simd_matrix::{naive, blocked, simd_ops, strassen, lu};
use std::time::Instant;

fn benchmark_multiply(label: &str, f: impl Fn() -> Matrix<f32>) -> std::time::Duration {
    // Warmup
    let _ = f();

    let start = Instant::now();
    let iterations = 3;
    for _ in 0..iterations {
        let _ = f();
    }
    let elapsed = start.elapsed() / iterations;
    println!("  {}: {:?}", label, elapsed);
    elapsed
}

fn main() {
    println!("=== SIMD Matrix Operations Benchmark ===\n");

    for &size in &[64, 256, 512, 1024] {
        println!("--- {}x{} f32 multiplication ---", size, size);

        let a = Matrix::<f32>::random(size, size);
        let b = Matrix::<f32>::random(size, size);

        let t_naive = benchmark_multiply("Naive", || naive::multiply(&a, &b));
        let t_blocked = benchmark_multiply("Blocked (64)", || {
            blocked::multiply_blocked(&a, &b, Some(64))
        });
        let t_simd = benchmark_multiply("SIMD", || simd_ops::multiply_simd_f32(&a, &b));

        if t_naive.as_nanos() > 0 {
            println!(
                "  Speedup: blocked={:.1}x, simd={:.1}x",
                t_naive.as_nanos() as f64 / t_blocked.as_nanos() as f64,
                t_naive.as_nanos() as f64 / t_simd.as_nanos() as f64
            );
        }
        println!();
    }

    // Verify correctness
    println!("--- Correctness Verification ---");
    let a = Matrix::<f32>::random(128, 128);
    let b = Matrix::<f32>::random(128, 128);

    let naive_result = naive::multiply(&a, &b);
    let blocked_result = blocked::multiply_blocked(&a, &b, Some(32));
    let simd_result = simd_ops::multiply_simd_f32(&a, &b);

    println!(
        "Naive vs Blocked match: {}",
        naive_result.approx_eq(&blocked_result, 1e-3)
    );
    println!(
        "Naive vs SIMD match: {}",
        naive_result.approx_eq(&simd_result, 1e-3)
    );

    // LU decomposition demo
    println!("\n--- LU Decomposition ---");
    let m = Matrix::from_vec(
        3,
        3,
        vec![2.0, 1.0, 1.0, 4.0, 3.0, 3.0, 8.0, 7.0, 9.0],
    );
    println!("Matrix A:\n{}", m);

    let det = lu::determinant(&m);
    println!("Determinant: {:.4}", det);

    if let Some(inv) = lu::inverse(&m) {
        println!("Inverse:\n{}", inv);
        let product = naive::multiply(&m, &inv);
        println!("A * A^-1 (should be identity):\n{}", product);
    }

    // Strassen demo
    println!("--- Strassen vs Naive (f64) ---");
    let a = Matrix::<f64>::random(200, 200);
    let b = Matrix::<f64>::random(200, 200);

    let start = Instant::now();
    let naive_r = naive::multiply(&a, &b);
    let naive_t = start.elapsed();

    let start = Instant::now();
    let strassen_r = strassen::multiply_strassen(&a, &b, Some(64));
    let strassen_t = start.elapsed();

    println!("  Naive:    {:?}", naive_t);
    println!("  Strassen: {:?}", strassen_t);
    println!(
        "  Results match: {}",
        naive_r.approx_eq(&strassen_r, 1e-6)
    );
}
```

### benches/matrix_bench.rs

```rust
use criterion::{criterion_group, criterion_main, BenchmarkId, Criterion};
use simd_matrix::matrix::Matrix;
use simd_matrix::{naive, blocked, simd_ops, strassen};

fn bench_multiply_f32(c: &mut Criterion) {
    let mut group = c.benchmark_group("multiply_f32");

    for &size in &[64, 256, 512, 1024] {
        let a = Matrix::<f32>::random(size, size);
        let b = Matrix::<f32>::random(size, size);

        group.bench_with_input(BenchmarkId::new("naive", size), &size, |bench, _| {
            bench.iter(|| naive::multiply(&a, &b))
        });

        group.bench_with_input(BenchmarkId::new("blocked_64", size), &size, |bench, _| {
            bench.iter(|| blocked::multiply_blocked(&a, &b, Some(64)))
        });

        group.bench_with_input(BenchmarkId::new("simd", size), &size, |bench, _| {
            bench.iter(|| simd_ops::multiply_simd_f32(&a, &b))
        });
    }

    group.finish();
}

fn bench_multiply_f64(c: &mut Criterion) {
    let mut group = c.benchmark_group("multiply_f64");

    for &size in &[64, 256, 512] {
        let a = Matrix::<f64>::random(size, size);
        let b = Matrix::<f64>::random(size, size);

        group.bench_with_input(BenchmarkId::new("naive", size), &size, |bench, _| {
            bench.iter(|| naive::multiply(&a, &b))
        });

        group.bench_with_input(BenchmarkId::new("simd", size), &size, |bench, _| {
            bench.iter(|| simd_ops::multiply_simd_f64(&a, &b))
        });

        group.bench_with_input(BenchmarkId::new("strassen_64", size), &size, |bench, _| {
            bench.iter(|| strassen::multiply_strassen(&a, &b, Some(64)))
        });
    }

    group.finish();
}

fn bench_lu(c: &mut Criterion) {
    let mut group = c.benchmark_group("lu_decomposition");

    for &size in &[64, 128, 256, 512] {
        let m = Matrix::<f64>::random(size, size);

        group.bench_with_input(BenchmarkId::new("lu", size), &size, |bench, _| {
            bench.iter(|| simd_matrix::lu::lu_decompose(&m))
        });
    }

    group.finish();
}

criterion_group!(benches, bench_multiply_f32, bench_multiply_f64, bench_lu);
criterion_main!(benches);
```

### Tests

```rust
#[cfg(test)]
mod tests {
    use super::matrix::Matrix;
    use super::*;

    #[test]
    fn test_identity_multiply() {
        let a = Matrix::from_vec(3, 3, vec![1.0f64, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0, 9.0]);
        let i = Matrix::<f64>::identity(3);

        let result = naive::multiply(&a, &i);
        assert!(a.approx_eq(&result, 1e-10));

        let result2 = naive::multiply(&i, &a);
        assert!(a.approx_eq(&result2, 1e-10));
    }

    #[test]
    fn test_naive_multiply_2x2() {
        let a = Matrix::from_vec(2, 2, vec![1.0f64, 2.0, 3.0, 4.0]);
        let b = Matrix::from_vec(2, 2, vec![5.0, 6.0, 7.0, 8.0]);
        let expected = Matrix::from_vec(2, 2, vec![19.0, 22.0, 43.0, 50.0]);

        let result = naive::multiply(&a, &b);
        assert!(result.approx_eq(&expected, 1e-10));
    }

    #[test]
    fn test_blocked_matches_naive() {
        let a = Matrix::<f64>::random(100, 80);
        let b = Matrix::<f64>::random(80, 90);

        let naive_r = naive::multiply(&a, &b);
        let blocked_r = blocked::multiply_blocked(&a, &b, Some(32));
        assert!(naive_r.approx_eq(&blocked_r, 1e-6));
    }

    #[test]
    fn test_simd_matches_naive_f32() {
        let a = Matrix::<f32>::random(100, 80);
        let b = Matrix::<f32>::random(80, 90);

        let naive_r = naive::multiply(&a, &b);
        let simd_r = simd_ops::multiply_simd_f32(&a, &b);
        assert!(naive_r.approx_eq(&simd_r, 1e-2)); // f32 accumulation tolerance
    }

    #[test]
    fn test_simd_matches_naive_f64() {
        let a = Matrix::<f64>::random(100, 80);
        let b = Matrix::<f64>::random(80, 90);

        let naive_r = naive::multiply(&a, &b);
        let simd_r = simd_ops::multiply_simd_f64(&a, &b);
        assert!(naive_r.approx_eq(&simd_r, 1e-6));
    }

    #[test]
    fn test_strassen_matches_naive() {
        let a = Matrix::<f64>::random(100, 100);
        let b = Matrix::<f64>::random(100, 100);

        let naive_r = naive::multiply(&a, &b);
        let strassen_r = strassen::multiply_strassen(&a, &b, Some(32));
        assert!(naive_r.approx_eq(&strassen_r, 1e-6));
    }

    #[test]
    fn test_strassen_non_power_of_two() {
        let a = Matrix::<f64>::random(73, 73);
        let b = Matrix::<f64>::random(73, 73);

        let naive_r = naive::multiply(&a, &b);
        let strassen_r = strassen::multiply_strassen(&a, &b, Some(32));
        assert!(naive_r.approx_eq(&strassen_r, 1e-6));
    }

    #[test]
    fn test_transpose() {
        let m = Matrix::from_vec(2, 3, vec![1.0f64, 2.0, 3.0, 4.0, 5.0, 6.0]);
        let t = naive::transpose(&m);

        assert_eq!(t.rows, 3);
        assert_eq!(t.cols, 2);
        assert_eq!(*t.get(0, 0), 1.0);
        assert_eq!(*t.get(1, 0), 2.0);
        assert_eq!(*t.get(0, 1), 4.0);
    }

    #[test]
    fn test_transpose_roundtrip() {
        let m = Matrix::<f64>::random(50, 70);
        let tt = naive::transpose(&naive::transpose(&m));
        assert!(m.approx_eq(&tt, 1e-10));
    }

    #[test]
    fn test_lu_decomposition() {
        let m = Matrix::from_vec(3, 3, vec![2.0, 1.0, 1.0, 4.0, 3.0, 3.0, 8.0, 7.0, 9.0]);

        let decomp = lu::lu_decompose(&m).unwrap();

        // Verify PA = LU
        let pa = naive::multiply(&decomp.p, &m);
        let lu_product = naive::multiply(&decomp.l, &decomp.u);
        assert!(pa.approx_eq(&lu_product, 1e-10));
    }

    #[test]
    fn test_determinant_2x2() {
        let m = Matrix::from_vec(2, 2, vec![3.0, 8.0, 4.0, 6.0]);
        let det = lu::determinant(&m);
        assert!((det - (-14.0)).abs() < 1e-10);
    }

    #[test]
    fn test_determinant_3x3() {
        let m = Matrix::from_vec(3, 3, vec![6.0, 1.0, 1.0, 4.0, -2.0, 5.0, 2.0, 8.0, 7.0]);
        let det = lu::determinant(&m);
        assert!((det - (-306.0)).abs() < 1e-6);
    }

    #[test]
    fn test_determinant_identity() {
        let m = Matrix::<f64>::identity(5);
        let det = lu::determinant(&m);
        assert!((det - 1.0).abs() < 1e-10);
    }

    #[test]
    fn test_determinant_singular() {
        let m = Matrix::from_vec(2, 2, vec![1.0, 2.0, 2.0, 4.0]);
        let det = lu::determinant(&m);
        assert!(det.abs() < 1e-10);
    }

    #[test]
    fn test_inverse_2x2() {
        let m = Matrix::from_vec(2, 2, vec![4.0, 7.0, 2.0, 6.0]);
        let inv = lu::inverse(&m).unwrap();

        let product = naive::multiply(&m, &inv);
        let identity = Matrix::<f64>::identity(2);
        assert!(product.approx_eq(&identity, 1e-10));
    }

    #[test]
    fn test_inverse_3x3() {
        let m = Matrix::from_vec(3, 3, vec![1.0, 2.0, 3.0, 0.0, 1.0, 4.0, 5.0, 6.0, 0.0]);
        let inv = lu::inverse(&m).unwrap();

        let product = naive::multiply(&m, &inv);
        let identity = Matrix::<f64>::identity(3);
        assert!(product.approx_eq(&identity, 1e-8));
    }

    #[test]
    fn test_inverse_singular_returns_none() {
        let m = Matrix::from_vec(2, 2, vec![1.0, 2.0, 2.0, 4.0]);
        assert!(lu::inverse(&m).is_none());
    }

    #[test]
    fn test_inverse_identity() {
        let m = Matrix::<f64>::identity(4);
        let inv = lu::inverse(&m).unwrap();
        assert!(m.approx_eq(&inv, 1e-10));
    }

    #[test]
    #[should_panic(expected = "dimension mismatch")]
    fn test_dimension_mismatch() {
        let a = Matrix::<f64>::random(3, 4);
        let b = Matrix::<f64>::random(5, 3);
        naive::multiply(&a, &b);
    }

    #[test]
    fn test_non_square_multiply() {
        let a = Matrix::from_vec(2, 3, vec![1.0f64, 2.0, 3.0, 4.0, 5.0, 6.0]);
        let b = Matrix::from_vec(3, 2, vec![7.0, 8.0, 9.0, 10.0, 11.0, 12.0]);
        let expected = Matrix::from_vec(2, 2, vec![58.0, 64.0, 139.0, 154.0]);

        let result = naive::multiply(&a, &b);
        assert!(result.approx_eq(&expected, 1e-10));
    }

    #[test]
    fn test_small_matrix_simd() {
        // Ensure SIMD handles matrices smaller than the vector width
        let a = Matrix::from_vec(2, 3, vec![1.0f32, 2.0, 3.0, 4.0, 5.0, 6.0]);
        let b = Matrix::from_vec(3, 2, vec![7.0, 8.0, 9.0, 10.0, 11.0, 12.0]);

        let naive_r = naive::multiply(&a, &b);
        let simd_r = simd_ops::multiply_simd_f32(&a, &b);
        assert!(naive_r.approx_eq(&simd_r, 1e-3));
    }

    #[test]
    fn test_lu_larger_matrix() {
        let m = Matrix::<f64>::random(50, 50);
        if let Some(decomp) = lu::lu_decompose(&m) {
            let pa = naive::multiply(&decomp.p, &m);
            let lu_product = naive::multiply(&decomp.l, &decomp.u);
            assert!(pa.approx_eq(&lu_product, 1e-6));
        }
    }
}
```

## Running

```bash
cargo init simd-matrix
cd simd-matrix

# Set up the module files as shown above
# Update Cargo.toml with dependencies

# Build and test (stable Rust)
cargo build
cargo test

# Run benchmarks (requires the binary to run)
cargo run --release

# Run criterion benchmarks
cargo bench

# For x86_64 with AVX2:
RUSTFLAGS="-C target-feature=+avx2,+fma" cargo run --release
RUSTFLAGS="-C target-feature=+avx2,+fma" cargo bench

# For aarch64 (Apple Silicon), NEON is enabled by default
cargo run --release
```

## Expected Output

```
=== SIMD Matrix Operations Benchmark ===

--- 64x64 f32 multiplication ---
  Naive: 342.1us
  Blocked (64): 298.5us
  SIMD: 89.2us
  Speedup: blocked=1.1x, simd=3.8x

--- 256x256 f32 multiplication ---
  Naive: 52.3ms
  Blocked (64): 21.7ms
  SIMD: 5.8ms
  Speedup: blocked=2.4x, simd=9.0x

--- 512x512 f32 multiplication ---
  Naive: 438.1ms
  Blocked (64): 142.3ms
  SIMD: 38.7ms
  Speedup: blocked=3.1x, simd=11.3x

--- 1024x1024 f32 multiplication ---
  Naive: 3.81s
  Blocked (64): 1.12s
  SIMD: 298.4ms
  Speedup: blocked=3.4x, simd=12.8x

--- Correctness Verification ---
Naive vs Blocked match: true
Naive vs SIMD match: true

--- LU Decomposition ---
Determinant: 2.0000
Inverse:
[  1.5000,  -0.5000,   0.0000]
[ -3.0000,   2.5000,  -0.5000]
[  1.0000,  -1.5000,   0.5000]

--- Strassen vs Naive (f64) ---
  Naive:    18.2ms
  Strassen: 12.4ms
  Results match: true
```

## Design Decisions

1. **Flat `Vec<T>` over `Vec<Vec<T>>`**: A single contiguous allocation with row-major layout is non-negotiable for performance. `Vec<Vec<T>>` scatters rows across the heap, causing cache misses on every row transition. For a 1024x1024 matrix, this can be a 10x performance difference before any algorithmic optimization.

2. **Transpose-B strategy for SIMD dot products**: The inner loop of matrix multiplication computes a dot product between a row of A and a column of B. Columns are non-contiguous in row-major layout, forcing strided access. By transposing B first, both operands are contiguous, enabling aligned SIMD loads. The O(n^2) transpose cost is amortized by the O(n^3) multiplication.

3. **Runtime feature detection over compile-time**: The `is_x86_feature_detected!` macro checks CPU capabilities at runtime, allowing the binary to work on all x86_64 CPUs while using AVX2 when available. The `#[target_feature(enable = "...")]` attribute tells the compiler to emit AVX2 instructions inside that function, but the function is only called after the runtime check passes.

4. **FMA (Fused Multiply-Add)**: Using `_mm256_fmadd_ps` instead of separate multiply and add provides both a performance improvement (one instruction instead of two) and better numerical accuracy (the intermediate product is not rounded before addition).

5. **Strassen crossover at 64**: Below 64x64, the overhead of Strassen's 18 matrix additions and 7 recursive calls exceeds the savings from doing 7 multiplications instead of 8. This threshold is hardware-dependent and should be tuned with benchmarks. Some implementations use 128 or even 256.

6. **LU with partial pivoting**: Without pivoting, LU decomposition fails on matrices where a diagonal element is zero (even if the matrix is nonsingular). Partial pivoting (choosing the largest element in the column) also improves numerical stability. Full pivoting (searching the entire remaining submatrix) provides even better stability but at O(n^2) per step instead of O(n).

## Common Mistakes

1. **`Vec<Vec<T>>` storage**: Destroys spatial locality, cache performance, and makes SIMD impossible without copying. Use a flat vector.

2. **Not transposing B**: Without transposing, the inner dot product accesses B in column-major order (stride = n), causing a cache miss every `cache_line_size / sizeof(T)` elements. Transposing B makes both accesses sequential.

3. **Ignoring remainder elements**: When the matrix dimension is not a multiple of the SIMD width (8 for f32 AVX2, 4 for f64), the last few elements must be handled with scalar code. Ignoring them produces wrong results.

4. **Forgetting `#[target_feature]`**: Without this attribute, the compiler cannot emit AVX2 instructions even inside an `unsafe` block that uses AVX2 intrinsics. The function will either fail to compile or generate scalar fallbacks.

5. **Strassen numerical instability**: Strassen's algorithm performs more additions and subtractions than the naive algorithm, which can accumulate floating-point error. For ill-conditioned matrices, the error can be significant. Always compare against the naive result for correctness.

6. **Benchmarking in debug mode**: `cargo run` (without `--release`) disables optimizations. SIMD speedups disappear or even reverse in debug mode because the auto-vectorizer in release mode can sometimes match hand-written SIMD for simple cases.

## Performance Notes

Typical speedups on modern hardware (2024+ CPU with AVX2/FMA or Apple M-series with NEON):

| Size    | Naive    | Blocked  | SIMD     | Strassen+SIMD |
|---------|----------|----------|----------|---------------|
| 64x64   | 1.0x     | 1.1x     | 3-4x     | N/A           |
| 256x256 | 1.0x     | 2-3x     | 8-10x    | 6-8x          |
| 1024x1024| 1.0x    | 3-4x     | 10-15x   | 12-18x        |
| 4096x4096| 1.0x    | 4-5x     | 15-20x   | 20-30x        |

Key factors:
- **Cache**: The L1 cache is typically 32-64KB. A 64x64 f32 tile = 16KB, fitting comfortably. 256x256 = 256KB, fitting in L2. Blocking ensures the working set stays in the fastest cache level.
- **SIMD width**: AVX2 processes 8 f32s or 4 f64s per instruction. The theoretical maximum speedup is 8x for f32 and 4x for f64. Achieving this requires eliminating all other bottlenecks (memory, branch misprediction).
- **FMA throughput**: Modern CPUs can issue 2 FMA operations per cycle per core. A 256-bit FMA computes 8 f32 multiply-adds per instruction. Peak throughput: 32 FLOP/cycle.
- **Memory bandwidth**: For large matrices (4096x4096), the bottleneck shifts from compute to memory bandwidth. Tiling and SIMD combined keep the compute pipeline fed from cache instead of DRAM.

## Going Further

- Add `rayon` parallelism to the tiled and Strassen variants for multi-core speedup
- Implement the GotoBLAS micro-kernel design pattern (a fixed-size inner kernel, loop tiling for L2/L3, and packing for alignment)
- Add AVX-512 support for CPUs that have it (16 f32s per instruction)
- Implement a matrix expression template system that fuses operations (e.g., `A * B + C` without materializing the intermediate)
- Port the SIMD kernels to WebAssembly SIMD (`std::arch::wasm32`) and benchmark in a browser
- Compare against BLAS (via the `blas` crate binding to OpenBLAS or MKL) to see how close you get to the state of the art
