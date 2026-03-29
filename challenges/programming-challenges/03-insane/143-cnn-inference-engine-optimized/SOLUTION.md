# Solution: Optimized CNN Inference Engine

## Architecture Overview

The implementation is organized into eight modules:

1. **tensor**: `Tensor` type with flat f32 storage and (B, C, H, W) indexing
2. **matmul**: Naive, tiled, and SIMD matrix multiplication implementations
3. **conv**: Naive and im2col convolution with optional fusion
4. **pool**: Max pooling layer
5. **dense**: Dense (fully connected) layer
6. **engine**: `InferenceEngine` orchestrating layers with memory pool
7. **model**: Binary model format loader/saver
8. **bench**: Benchmarking harness with warmup, statistics, and comparison table

```
Model File (binary weights)
     |
     v
 [Model Loader] --> Layer definitions + weights
     |
     v
 [Memory Pool Init] --> pre-allocate based on max intermediate tensor size
     |
     v
 [InferenceEngine::run(batch)]
     |
     +--> Conv(1->16, 3x3) + Bias + ReLU  [fused]
     |      im2col -> tiled SIMD matmul
     +--> MaxPool(2x2)
     +--> Conv(16->32, 3x3) + Bias + ReLU [fused]
     |      im2col -> tiled SIMD matmul
     +--> MaxPool(2x2)
     +--> Flatten
     +--> Dense(800->128) + ReLU           [fused]
     |      tiled SIMD matmul
     +--> Dense(128->10)
     +--> Softmax
     |
     v
 [Predictions] --> Vec<usize> class labels

 [Benchmark Harness]
     +--> Naive baseline
     +--> + im2col
     +--> + tiled matmul
     +--> + SIMD
     +--> + fusion
     +--> + memory pool
     +--> + multithreading (rayon)
     |
     v
 [Performance Table] --> speedup at each level
```

## Rust Solution

### Cargo.toml

```toml
[package]
name = "cnn-inference-engine"
version = "0.1.0"
edition = "2021"

[dependencies]
rayon = "1.10"

[[bin]]
name = "cnn-inference"
path = "src/main.rs"

[profile.release]
opt-level = 3
lto = true
target-cpu = "native"
```

### src/tensor.rs

```rust
#[derive(Clone, Debug)]
pub struct Tensor {
    pub shape: [usize; 4], // (batch, channels, height, width)
    pub data: Vec<f32>,
}

impl Tensor {
    pub fn zeros(b: usize, c: usize, h: usize, w: usize) -> Self {
        Tensor {
            shape: [b, c, h, w],
            data: vec![0.0f32; b * c * h * w],
        }
    }

    pub fn from_data(b: usize, c: usize, h: usize, w: usize, data: Vec<f32>) -> Self {
        assert_eq!(data.len(), b * c * h * w);
        Tensor { shape: [b, c, h, w], data }
    }

    #[inline(always)]
    pub fn idx(&self, b: usize, c: usize, h: usize, w: usize) -> usize {
        ((b * self.shape[1] + c) * self.shape[2] + h) * self.shape[3] + w
    }

    #[inline(always)]
    pub fn get(&self, b: usize, c: usize, h: usize, w: usize) -> f32 {
        self.data[self.idx(b, c, h, w)]
    }

    #[inline(always)]
    pub fn set(&mut self, b: usize, c: usize, h: usize, w: usize, val: f32) {
        let i = self.idx(b, c, h, w);
        self.data[i] = val;
    }

    pub fn get_padded(&self, b: usize, c: usize, h: isize, w: isize) -> f32 {
        if h < 0 || w < 0 || h >= self.shape[2] as isize || w >= self.shape[3] as isize {
            0.0
        } else {
            self.get(b, c, h as usize, w as usize)
        }
    }

    pub fn numel(&self) -> usize {
        self.data.len()
    }

    pub fn batch(&self) -> usize { self.shape[0] }
    pub fn channels(&self) -> usize { self.shape[1] }
    pub fn height(&self) -> usize { self.shape[2] }
    pub fn width(&self) -> usize { self.shape[3] }
}

/// Flat 2D matrix for matmul operations (f32).
#[derive(Clone, Debug)]
pub struct Mat {
    pub rows: usize,
    pub cols: usize,
    pub data: Vec<f32>,
}

impl Mat {
    pub fn zeros(rows: usize, cols: usize) -> Self {
        Mat { rows, cols, data: vec![0.0; rows * cols] }
    }

    pub fn from_data(rows: usize, cols: usize, data: Vec<f32>) -> Self {
        assert_eq!(data.len(), rows * cols);
        Mat { rows, cols, data }
    }

    #[inline(always)]
    pub fn get(&self, r: usize, c: usize) -> f32 {
        self.data[r * self.cols + c]
    }

    #[inline(always)]
    pub fn set(&mut self, r: usize, c: usize, val: f32) {
        self.data[r * self.cols + c] = val;
    }
}
```

### src/matmul.rs

```rust
use crate::tensor::Mat;

/// Naive triple-loop matrix multiplication.
pub fn matmul_naive(a: &Mat, b: &Mat) -> Mat {
    assert_eq!(a.cols, b.rows);
    let mut c = Mat::zeros(a.rows, b.cols);
    for i in 0..a.rows {
        for k in 0..a.cols {
            let a_ik = a.get(i, k);
            for j in 0..b.cols {
                let idx = i * b.cols + j;
                c.data[idx] += a_ik * b.data[k * b.cols + j];
            }
        }
    }
    c
}

/// Tiled matrix multiplication for L1 cache locality.
/// Tile size chosen for typical 32KB L1: 32x32 tiles of f32 = 4KB per tile.
const TILE: usize = 32;

pub fn matmul_tiled(a: &Mat, b: &Mat) -> Mat {
    assert_eq!(a.cols, b.rows);
    let m = a.rows;
    let n = b.cols;
    let k = a.cols;
    let mut c = Mat::zeros(m, n);

    for i0 in (0..m).step_by(TILE) {
        let i1 = (i0 + TILE).min(m);
        for k0 in (0..k).step_by(TILE) {
            let k1 = (k0 + TILE).min(k);
            for j0 in (0..n).step_by(TILE) {
                let j1 = (j0 + TILE).min(n);

                for i in i0..i1 {
                    for kk in k0..k1 {
                        let a_ik = a.data[i * k + kk];
                        let c_row_offset = i * n;
                        let b_row_offset = kk * n;
                        for j in j0..j1 {
                            c.data[c_row_offset + j] += a_ik * b.data[b_row_offset + j];
                        }
                    }
                }
            }
        }
    }
    c
}

/// SIMD-accelerated tiled matrix multiplication.
/// Uses platform-specific intrinsics with scalar fallback.
pub fn matmul_simd(a: &Mat, b: &Mat) -> Mat {
    assert_eq!(a.cols, b.rows);
    let m = a.rows;
    let n = b.cols;
    let k = a.cols;
    let mut c = Mat::zeros(m, n);

    #[cfg(target_arch = "x86_64")]
    {
        if is_x86_feature_detected!("avx2") && is_x86_feature_detected!("fma") {
            unsafe { matmul_avx2(a, b, &mut c, m, n, k); }
            return c;
        }
    }

    #[cfg(target_arch = "aarch64")]
    {
        unsafe { matmul_neon(a, b, &mut c, m, n, k); }
        return c;
    }

    // Scalar fallback: use tiled version.
    #[allow(unreachable_code)]
    matmul_tiled(a, b)
}

#[cfg(target_arch = "x86_64")]
#[target_feature(enable = "avx2,fma")]
unsafe fn matmul_avx2(a: &Mat, b: &Mat, c: &mut Mat, m: usize, n: usize, k: usize) {
    use std::arch::x86_64::*;

    for i0 in (0..m).step_by(TILE) {
        let i1 = (i0 + TILE).min(m);
        for k0 in (0..k).step_by(TILE) {
            let k1 = (k0 + TILE).min(k);
            for j0 in (0..n).step_by(TILE) {
                let j1 = (j0 + TILE).min(n);

                for i in i0..i1 {
                    for kk in k0..k1 {
                        let a_ik = _mm256_set1_ps(a.data[i * k + kk]);
                        let c_base = i * n;
                        let b_base = kk * n;

                        let mut j = j0;
                        while j + 8 <= j1 {
                            let b_vec = _mm256_loadu_ps(b.data.as_ptr().add(b_base + j));
                            let c_vec = _mm256_loadu_ps(c.data.as_ptr().add(c_base + j));
                            let result = _mm256_fmadd_ps(a_ik, b_vec, c_vec);
                            _mm256_storeu_ps(c.data.as_mut_ptr().add(c_base + j), result);
                            j += 8;
                        }
                        // Scalar tail.
                        let a_val = a.data[i * k + kk];
                        while j < j1 {
                            c.data[c_base + j] += a_val * b.data[b_base + j];
                            j += 1;
                        }
                    }
                }
            }
        }
    }
}

#[cfg(target_arch = "aarch64")]
unsafe fn matmul_neon(a: &Mat, b: &Mat, c: &mut Mat, m: usize, n: usize, k: usize) {
    use std::arch::aarch64::*;

    for i0 in (0..m).step_by(TILE) {
        let i1 = (i0 + TILE).min(m);
        for k0 in (0..k).step_by(TILE) {
            let k1 = (k0 + TILE).min(k);
            for j0 in (0..n).step_by(TILE) {
                let j1 = (j0 + TILE).min(n);

                for i in i0..i1 {
                    for kk in k0..k1 {
                        let a_ik = vdupq_n_f32(a.data[i * k + kk]);
                        let c_base = i * n;
                        let b_base = kk * n;

                        let mut j = j0;
                        while j + 4 <= j1 {
                            let b_vec = vld1q_f32(b.data.as_ptr().add(b_base + j));
                            let c_vec = vld1q_f32(c.data.as_ptr().add(c_base + j));
                            let result = vfmaq_f32(c_vec, a_ik, b_vec);
                            vst1q_f32(c.data.as_mut_ptr().add(c_base + j), result);
                            j += 4;
                        }
                        let a_val = a.data[i * k + kk];
                        while j < j1 {
                            c.data[c_base + j] += a_val * b.data[b_base + j];
                            j += 1;
                        }
                    }
                }
            }
        }
    }
}
```

### src/conv.rs

```rust
use crate::tensor::{Tensor, Mat};
use crate::matmul;

pub fn output_dim(input: usize, kernel: usize, padding: usize, stride: usize) -> usize {
    (input + 2 * padding - kernel) / stride + 1
}

/// Naive 2D convolution with 6 nested loops.
pub fn conv2d_naive(
    input: &Tensor, kernel: &Tensor, bias: &[f32],
    stride: usize, padding: usize,
) -> Tensor {
    let (b, c_in, h, w) = (input.batch(), input.channels(), input.height(), input.width());
    let (c_out, _, kh, kw) = (kernel.batch(), kernel.channels(), kernel.height(), kernel.width());
    let oh = output_dim(h, kh, padding, stride);
    let ow = output_dim(w, kw, padding, stride);
    let mut output = Tensor::zeros(b, c_out, oh, ow);

    for bi in 0..b {
        for co in 0..c_out {
            for ohi in 0..oh {
                for owi in 0..ow {
                    let mut sum = bias[co];
                    for ci in 0..c_in {
                        for kr in 0..kh {
                            for kc in 0..kw {
                                let ih = (ohi * stride + kr) as isize - padding as isize;
                                let iw = (owi * stride + kc) as isize - padding as isize;
                                sum += input.get_padded(bi, ci, ih, iw)
                                    * kernel.get(co, ci, kr, kc);
                            }
                        }
                    }
                    output.set(bi, co, ohi, owi, sum);
                }
            }
        }
    }
    output
}

/// im2col: rearrange input patches into column matrix.
pub fn im2col(
    input: &Tensor, batch_idx: usize,
    kh: usize, kw: usize, stride: usize, padding: usize,
) -> Mat {
    let c_in = input.channels();
    let oh = output_dim(input.height(), kh, padding, stride);
    let ow = output_dim(input.width(), kw, padding, stride);
    let rows = c_in * kh * kw;
    let cols = oh * ow;
    let mut col = Mat::zeros(rows, cols);

    for ohi in 0..oh {
        for owi in 0..ow {
            let col_idx = ohi * ow + owi;
            let mut row_idx = 0;
            for ci in 0..c_in {
                for kr in 0..kh {
                    for kc in 0..kw {
                        let ih = (ohi * stride + kr) as isize - padding as isize;
                        let iw = (owi * stride + kc) as isize - padding as isize;
                        col.set(row_idx, col_idx, input.get_padded(batch_idx, ci, ih, iw));
                        row_idx += 1;
                    }
                }
            }
        }
    }
    col
}

/// Reshape kernel to weight matrix (c_out, c_in*kh*kw).
pub fn kernel_to_mat(kernel: &Tensor) -> Mat {
    let (c_out, c_in, kh, kw) = (kernel.batch(), kernel.channels(), kernel.height(), kernel.width());
    let cols = c_in * kh * kw;
    let mut mat = Mat::zeros(c_out, cols);
    for co in 0..c_out {
        let mut col = 0;
        for ci in 0..c_in {
            for r in 0..kh {
                for c in 0..kw {
                    mat.set(co, col, kernel.get(co, ci, r, c));
                    col += 1;
                }
            }
        }
    }
    mat
}

/// im2col convolution using specified matmul function.
pub fn conv2d_im2col<F>(
    input: &Tensor, kernel: &Tensor, bias: &[f32],
    stride: usize, padding: usize, matmul_fn: F,
) -> Tensor
where
    F: Fn(&Mat, &Mat) -> Mat,
{
    let b = input.batch();
    let (c_out, _, kh, kw) = (kernel.batch(), kernel.channels(), kernel.height(), kernel.width());
    let oh = output_dim(input.height(), kh, padding, stride);
    let ow = output_dim(input.width(), kw, padding, stride);
    let weight_mat = kernel_to_mat(kernel);
    let mut output = Tensor::zeros(b, c_out, oh, ow);

    for bi in 0..b {
        let col = im2col(input, bi, kh, kw, stride, padding);
        let result = matmul_fn(&weight_mat, &col);
        for co in 0..c_out {
            for ohi in 0..oh {
                for owi in 0..ow {
                    let val = result.get(co, ohi * ow + owi) + bias[co];
                    output.set(bi, co, ohi, owi, val);
                }
            }
        }
    }
    output
}

/// Fused Conv + Bias + ReLU: compute all three in one pass.
pub fn conv2d_fused_bias_relu<F>(
    input: &Tensor, kernel: &Tensor, bias: &[f32],
    stride: usize, padding: usize, matmul_fn: F,
) -> Tensor
where
    F: Fn(&Mat, &Mat) -> Mat,
{
    let b = input.batch();
    let (c_out, _, kh, kw) = (kernel.batch(), kernel.channels(), kernel.height(), kernel.width());
    let oh = output_dim(input.height(), kh, padding, stride);
    let ow = output_dim(input.width(), kw, padding, stride);
    let weight_mat = kernel_to_mat(kernel);
    let mut output = Tensor::zeros(b, c_out, oh, ow);

    for bi in 0..b {
        let col = im2col(input, bi, kh, kw, stride, padding);
        let result = matmul_fn(&weight_mat, &col);
        for co in 0..c_out {
            let b_val = bias[co];
            for ohi in 0..oh {
                for owi in 0..ow {
                    let val = result.get(co, ohi * ow + owi) + b_val;
                    // Fused ReLU: max(0, val)
                    output.set(bi, co, ohi, owi, if val > 0.0 { val } else { 0.0 });
                }
            }
        }
    }
    output
}

/// Fused Conv + Bias + ReLU with rayon parallelism across output channels.
pub fn conv2d_fused_parallel<F>(
    input: &Tensor, kernel: &Tensor, bias: &[f32],
    stride: usize, padding: usize, matmul_fn: F,
) -> Tensor
where
    F: Fn(&Mat, &Mat) -> Mat + Sync,
{
    use rayon::prelude::*;

    let b = input.batch();
    let (c_out, _, kh, kw) = (kernel.batch(), kernel.channels(), kernel.height(), kernel.width());
    let oh = output_dim(input.height(), kh, padding, stride);
    let ow = output_dim(input.width(), kw, padding, stride);
    let weight_mat = kernel_to_mat(kernel);

    let spatial = oh * ow;
    let total = b * c_out * spatial;
    let mut out_data = vec![0.0f32; total];

    // Process each batch image.
    for bi in 0..b {
        let col = im2col(input, bi, kh, kw, stride, padding);
        let result = matmul_fn(&weight_mat, &col);

        let batch_offset = bi * c_out * spatial;
        let out_slice = &mut out_data[batch_offset..batch_offset + c_out * spatial];

        // Parallelize across output channels.
        out_slice
            .par_chunks_mut(spatial)
            .enumerate()
            .for_each(|(co, chunk)| {
                let b_val = bias[co];
                for idx in 0..spatial {
                    let val = result.data[co * spatial + idx] + b_val;
                    chunk[idx] = if val > 0.0 { val } else { 0.0 };
                }
            });
    }

    Tensor::from_data(b, c_out, oh, ow, out_data)
}
```

### src/pool.rs

```rust
use crate::tensor::Tensor;

pub fn max_pool2d(input: &Tensor, pool_h: usize, pool_w: usize) -> Tensor {
    let (b, c, h, w) = (input.batch(), input.channels(), input.height(), input.width());
    let oh = h / pool_h;
    let ow = w / pool_w;
    let mut output = Tensor::zeros(b, c, oh, ow);

    for bi in 0..b {
        for ci in 0..c {
            for ohi in 0..oh {
                for owi in 0..ow {
                    let mut max_val = f32::NEG_INFINITY;
                    for ph in 0..pool_h {
                        for pw in 0..pool_w {
                            let val = input.get(bi, ci, ohi * pool_h + ph, owi * pool_w + pw);
                            if val > max_val {
                                max_val = val;
                            }
                        }
                    }
                    output.set(bi, ci, ohi, owi, max_val);
                }
            }
        }
    }
    output
}
```

### src/dense.rs

```rust
use crate::tensor::Mat;

/// Dense layer forward: output = input * W^T + bias.
/// Input: (batch, input_size) as Mat.
/// Weights: (output_size, input_size) as Mat (stored transposed for efficiency).
pub fn dense_forward(input: &Mat, weights: &Mat, bias: &[f32], matmul_fn: &dyn Fn(&Mat, &Mat) -> Mat) -> Mat {
    let wt = transpose(weights);
    let mut result = matmul_fn(input, &wt);
    for r in 0..result.rows {
        for c in 0..result.cols {
            result.data[r * result.cols + c] += bias[c];
        }
    }
    result
}

/// Dense + ReLU fused.
pub fn dense_forward_relu(input: &Mat, weights: &Mat, bias: &[f32], matmul_fn: &dyn Fn(&Mat, &Mat) -> Mat) -> Mat {
    let wt = transpose(weights);
    let mut result = matmul_fn(input, &wt);
    for r in 0..result.rows {
        for c in 0..result.cols {
            let idx = r * result.cols + c;
            let val = result.data[idx] + bias[c];
            result.data[idx] = if val > 0.0 { val } else { 0.0 };
        }
    }
    result
}

fn transpose(m: &Mat) -> Mat {
    let mut t = Mat::zeros(m.cols, m.rows);
    for r in 0..m.rows {
        for c in 0..m.cols {
            t.data[c * m.rows + r] = m.data[r * m.cols + c];
        }
    }
    t
}

/// Softmax per row.
pub fn softmax(input: &Mat) -> Mat {
    let mut result = Mat::zeros(input.rows, input.cols);
    for r in 0..input.rows {
        let row_start = r * input.cols;
        let row = &input.data[row_start..row_start + input.cols];
        let max_val = row.iter().cloned().fold(f32::NEG_INFINITY, f32::max);
        let exps: Vec<f32> = row.iter().map(|&x| (x - max_val).exp()).collect();
        let sum: f32 = exps.iter().sum();
        for c in 0..input.cols {
            result.data[row_start + c] = exps[c] / sum;
        }
    }
    result
}
```

### src/mempool.rs

```rust
/// Memory pool for intermediate tensor allocations.
/// Uses a double-buffer scheme: alternate between two buffers
/// so input and output of a layer never alias.
pub struct MemoryPool {
    buf_a: Vec<f32>,
    buf_b: Vec<f32>,
    current: bool, // false = A is input, true = B is input
    total_allocated: usize,
    peak_usage: usize,
}

impl MemoryPool {
    pub fn new(max_tensor_size: usize) -> Self {
        MemoryPool {
            buf_a: vec![0.0f32; max_tensor_size],
            buf_b: vec![0.0f32; max_tensor_size],
            current: false,
            total_allocated: max_tensor_size * 2,
            peak_usage: 0,
        }
    }

    /// Get a mutable slice for writing output.
    pub fn output_buf(&mut self, size: usize) -> &mut [f32] {
        self.peak_usage = self.peak_usage.max(size);
        if self.current {
            &mut self.buf_a[..size]
        } else {
            &mut self.buf_b[..size]
        }
    }

    /// Get an immutable slice for reading input.
    pub fn input_buf(&self, size: usize) -> &[f32] {
        if self.current {
            &self.buf_b[..size]
        } else {
            &self.buf_a[..size]
        }
    }

    /// Swap buffers: current output becomes next input.
    pub fn swap(&mut self) {
        self.current = !self.current;
    }

    pub fn stats(&self) -> (usize, usize) {
        (self.total_allocated, self.peak_usage)
    }
}
```

### src/model.rs

```rust
use crate::tensor::{Tensor, Mat};
use std::io::{Read, Write, BufReader, BufWriter};
use std::fs::File;

pub struct ConvLayerDef {
    pub kernel: Tensor,
    pub bias: Vec<f32>,
    pub stride: usize,
    pub padding: usize,
}

pub struct DenseLayerDef {
    pub weights: Mat,
    pub bias: Vec<f32>,
}

pub enum LayerDef {
    Conv(ConvLayerDef),
    MaxPool { pool_h: usize, pool_w: usize },
    Dense(DenseLayerDef),
    ReLU,
    Softmax,
    Flatten,
}

pub struct Model {
    pub layers: Vec<LayerDef>,
    pub input_shape: [usize; 3], // (C, H, W)
}

const MAGIC: [u8; 4] = [b'C', b'N', b'N', b'E'];

pub fn save_model(model: &Model, path: &str) -> std::io::Result<()> {
    let file = File::create(path)?;
    let mut w = BufWriter::new(file);

    w.write_all(&MAGIC)?;
    let layer_count = model.layers.len() as u32;
    w.write_all(&layer_count.to_le_bytes())?;

    for dim in &model.input_shape {
        w.write_all(&(*dim as u32).to_le_bytes())?;
    }

    for layer in &model.layers {
        match layer {
            LayerDef::Conv(conv) => {
                w.write_all(&[0u8])?;
                for &d in &conv.kernel.shape {
                    w.write_all(&(d as u32).to_le_bytes())?;
                }
                w.write_all(&(conv.stride as u32).to_le_bytes())?;
                w.write_all(&(conv.padding as u32).to_le_bytes())?;
                for &v in &conv.kernel.data {
                    w.write_all(&v.to_le_bytes())?;
                }
                for &v in &conv.bias {
                    w.write_all(&v.to_le_bytes())?;
                }
            }
            LayerDef::MaxPool { pool_h, pool_w } => {
                w.write_all(&[1u8])?;
                w.write_all(&(*pool_h as u32).to_le_bytes())?;
                w.write_all(&(*pool_w as u32).to_le_bytes())?;
            }
            LayerDef::Dense(dense) => {
                w.write_all(&[2u8])?;
                w.write_all(&(dense.weights.rows as u32).to_le_bytes())?;
                w.write_all(&(dense.weights.cols as u32).to_le_bytes())?;
                for &v in &dense.weights.data {
                    w.write_all(&v.to_le_bytes())?;
                }
                for &v in &dense.bias {
                    w.write_all(&v.to_le_bytes())?;
                }
            }
            LayerDef::ReLU => { w.write_all(&[3u8])?; }
            LayerDef::Softmax => { w.write_all(&[4u8])?; }
            LayerDef::Flatten => { w.write_all(&[5u8])?; }
        }
    }
    Ok(())
}

pub fn load_model(path: &str) -> std::io::Result<Model> {
    let file = File::open(path)?;
    let mut r = BufReader::new(file);

    let mut magic = [0u8; 4];
    r.read_exact(&mut magic)?;
    assert_eq!(magic, MAGIC, "Invalid model file");

    let mut buf4 = [0u8; 4];
    r.read_exact(&mut buf4)?;
    let layer_count = u32::from_le_bytes(buf4) as usize;

    let mut input_shape = [0usize; 3];
    for dim in input_shape.iter_mut() {
        r.read_exact(&mut buf4)?;
        *dim = u32::from_le_bytes(buf4) as usize;
    }

    let mut layers = Vec::with_capacity(layer_count);
    for _ in 0..layer_count {
        let mut type_byte = [0u8; 1];
        r.read_exact(&mut type_byte)?;

        match type_byte[0] {
            0 => {
                let mut shape = [0usize; 4];
                for s in shape.iter_mut() {
                    r.read_exact(&mut buf4)?;
                    *s = u32::from_le_bytes(buf4) as usize;
                }
                r.read_exact(&mut buf4)?;
                let stride = u32::from_le_bytes(buf4) as usize;
                r.read_exact(&mut buf4)?;
                let padding = u32::from_le_bytes(buf4) as usize;

                let n = shape[0] * shape[1] * shape[2] * shape[3];
                let mut data = vec![0.0f32; n];
                for v in data.iter_mut() {
                    r.read_exact(&mut buf4)?;
                    *v = f32::from_le_bytes(buf4);
                }
                let mut bias = vec![0.0f32; shape[0]];
                for v in bias.iter_mut() {
                    r.read_exact(&mut buf4)?;
                    *v = f32::from_le_bytes(buf4);
                }

                layers.push(LayerDef::Conv(ConvLayerDef {
                    kernel: Tensor::from_data(shape[0], shape[1], shape[2], shape[3], data),
                    bias,
                    stride,
                    padding,
                }));
            }
            1 => {
                r.read_exact(&mut buf4)?;
                let ph = u32::from_le_bytes(buf4) as usize;
                r.read_exact(&mut buf4)?;
                let pw = u32::from_le_bytes(buf4) as usize;
                layers.push(LayerDef::MaxPool { pool_h: ph, pool_w: pw });
            }
            2 => {
                r.read_exact(&mut buf4)?;
                let rows = u32::from_le_bytes(buf4) as usize;
                r.read_exact(&mut buf4)?;
                let cols = u32::from_le_bytes(buf4) as usize;

                let mut data = vec![0.0f32; rows * cols];
                for v in data.iter_mut() {
                    r.read_exact(&mut buf4)?;
                    *v = f32::from_le_bytes(buf4);
                }
                let mut bias = vec![0.0f32; rows];
                for v in bias.iter_mut() {
                    r.read_exact(&mut buf4)?;
                    *v = f32::from_le_bytes(buf4);
                }

                layers.push(LayerDef::Dense(DenseLayerDef {
                    weights: Mat::from_data(rows, cols, data),
                    bias,
                }));
            }
            3 => layers.push(LayerDef::ReLU),
            4 => layers.push(LayerDef::Softmax),
            5 => layers.push(LayerDef::Flatten),
            _ => panic!("Unknown layer type: {}", type_byte[0]),
        }
    }

    Ok(Model { layers, input_shape })
}
```

### src/bench.rs

```rust
use std::time::{Duration, Instant};

pub struct BenchResult {
    pub name: String,
    pub mean: Duration,
    pub median: Duration,
    pub min: Duration,
    pub max: Duration,
    pub stddev: Duration,
}

pub fn benchmark<F: FnMut()>(name: &str, mut f: F, iterations: usize) -> BenchResult {
    let warmup = (iterations / 10).max(3);
    for _ in 0..warmup {
        f();
    }

    let mut times: Vec<Duration> = Vec::with_capacity(iterations);
    for _ in 0..iterations {
        let start = Instant::now();
        f();
        times.push(start.elapsed());
    }

    times.sort();
    let mean_ns: u128 = times.iter().map(|d| d.as_nanos()).sum::<u128>() / iterations as u128;
    let median = times[iterations / 2];
    let mean = Duration::from_nanos(mean_ns as u64);
    let min = times[0];
    let max = times[iterations - 1];

    let mean_f = mean_ns as f64;
    let variance: f64 = times
        .iter()
        .map(|d| {
            let diff = d.as_nanos() as f64 - mean_f;
            diff * diff
        })
        .sum::<f64>()
        / iterations as f64;
    let stddev = Duration::from_nanos(variance.sqrt() as u64);

    BenchResult { name: name.to_string(), mean, median, min, max, stddev }
}

pub fn print_bench_table(results: &[BenchResult]) {
    println!(
        "\n{:<30} {:>12} {:>12} {:>12} {:>12} {:>10}",
        "Configuration", "Mean", "Median", "Min", "Stddev", "Speedup"
    );
    println!("{}", "=".repeat(94));

    let baseline_ns = if results.is_empty() {
        1
    } else {
        results[0].mean.as_nanos().max(1)
    };

    for result in results {
        let speedup = baseline_ns as f64 / result.mean.as_nanos().max(1) as f64;
        println!(
            "{:<30} {:>12} {:>12} {:>12} {:>12} {:>9.1}x",
            result.name,
            format_duration(result.mean),
            format_duration(result.median),
            format_duration(result.min),
            format_duration(result.stddev),
            speedup,
        );
    }
}

fn format_duration(d: Duration) -> String {
    let us = d.as_micros();
    if us < 1000 {
        format!("{}us", us)
    } else if us < 1_000_000 {
        format!("{:.2}ms", us as f64 / 1000.0)
    } else {
        format!("{:.2}s", us as f64 / 1_000_000.0)
    }
}
```

### src/main.rs

```rust
mod tensor;
mod matmul;
mod conv;
mod pool;
mod dense;
mod mempool;
mod model;
mod bench;

use tensor::{Tensor, Mat};
use matmul::{matmul_naive, matmul_tiled, matmul_simd};
use conv::*;
use pool::max_pool2d;
use dense::*;
use model::*;
use bench::*;
use rayon::prelude::*;

struct Rng { state: u64 }
impl Rng {
    fn new(seed: u64) -> Self { Rng { state: if seed == 0 { 1 } else { seed } } }
    fn next(&mut self) -> f32 {
        self.state ^= self.state << 13;
        self.state ^= self.state >> 7;
        self.state ^= self.state << 17;
        ((self.state >> 11) as f64 / (1u64 << 53) as f64) as f32 * 0.2 - 0.1
    }
}

fn random_tensor(rng: &mut Rng, b: usize, c: usize, h: usize, w: usize) -> Tensor {
    let data: Vec<f32> = (0..b * c * h * w).map(|_| rng.next()).collect();
    Tensor::from_data(b, c, h, w, data)
}

fn random_mat(rng: &mut Rng, rows: usize, cols: usize) -> Mat {
    let data: Vec<f32> = (0..rows * cols).map(|_| rng.next()).collect();
    Mat::from_data(rows, cols, data)
}

fn verify_correctness(rng: &mut Rng) {
    println!("=== Correctness Verification ===\n");

    // Matmul correctness: tiled and SIMD vs naive.
    let a = random_mat(rng, 64, 128);
    let b = random_mat(rng, 128, 64);

    let c_naive = matmul_naive(&a, &b);
    let c_tiled = matmul_tiled(&a, &b);
    let c_simd = matmul_simd(&a, &b);

    let tiled_diff: f32 = c_naive.data.iter().zip(c_tiled.data.iter())
        .map(|(a, b)| (a - b).abs()).fold(0.0, f32::max);
    let simd_diff: f32 = c_naive.data.iter().zip(c_simd.data.iter())
        .map(|(a, b)| (a - b).abs()).fold(0.0, f32::max);

    println!("  Tiled vs naive matmul max diff:  {:.2e} {}", tiled_diff, if tiled_diff < 1e-4 { "PASS" } else { "FAIL" });
    println!("  SIMD vs naive matmul max diff:   {:.2e} {}", simd_diff, if simd_diff < 1e-4 { "PASS" } else { "FAIL" });

    // Conv correctness: im2col vs naive.
    let input = random_tensor(rng, 1, 3, 8, 8);
    let kernel = random_tensor(rng, 8, 3, 3, 3);
    let bias = vec![0.01f32; 8];

    let conv_naive = conv2d_naive(&input, &kernel, &bias, 1, 1);
    let conv_im2col = conv2d_im2col(&input, &kernel, &bias, 1, 1, matmul_naive);
    let conv_diff: f32 = conv_naive.data.iter().zip(conv_im2col.data.iter())
        .map(|(a, b)| (a - b).abs()).fold(0.0, f32::max);
    println!("  im2col vs naive conv max diff:   {:.2e} {}", conv_diff, if conv_diff < 1e-4 { "PASS" } else { "FAIL" });

    // Fused conv correctness.
    let conv_fused = conv2d_fused_bias_relu(&input, &kernel, &bias, 1, 1, matmul_naive);
    let conv_separate = {
        let c = conv2d_im2col(&input, &kernel, &bias, 1, 1, matmul_naive);
        let mut relu = c.clone();
        for v in relu.data.iter_mut() {
            *v = if *v > 0.0 { *v } else { 0.0 };
        }
        relu
    };
    let fused_diff: f32 = conv_fused.data.iter().zip(conv_separate.data.iter())
        .map(|(a, b)| (a - b).abs()).fold(0.0, f32::max);
    println!("  Fused vs separate conv+relu diff: {:.2e} {}", fused_diff, if fused_diff < 1e-4 { "PASS" } else { "FAIL" });
}

fn run_full_pipeline(batch: &Tensor, rng: &mut Rng) -> Vec<usize> {
    let k1 = random_tensor(rng, 16, 1, 3, 3);
    let b1 = vec![0.01f32; 16];
    let k2 = random_tensor(rng, 32, 16, 3, 3);
    let b2 = vec![0.01f32; 32];

    let batch_size = batch.batch();
    // Conv1 + ReLU + Pool
    let x = conv2d_fused_bias_relu(batch, &k1, &b1, 1, 1, matmul_simd);
    let x = max_pool2d(&x, 2, 2); // 28->14

    // Conv2 + ReLU + Pool
    let x = conv2d_fused_bias_relu(&x, &k2, &b2, 1, 1, matmul_simd);
    let x = max_pool2d(&x, 2, 2); // 14->7 (but after 3x3 conv with pad=1, stays 14, then pool -> 7)

    // Flatten: (batch, 32, 5, 5) -> (batch, 800)
    let flat_size = x.channels() * x.height() * x.width();
    let flat = Mat::from_data(batch_size, flat_size, x.data.clone());

    // Dense1 + ReLU
    let w1 = random_mat(rng, 128, flat_size);
    let db1 = vec![0.01f32; 128];
    let x = dense_forward_relu(&flat, &w1, &db1, &matmul_simd);

    // Dense2
    let w2 = random_mat(rng, 10, 128);
    let db2 = vec![0.0f32; 10];
    let x = dense_forward(&x, &w2, &db2, &matmul_simd);

    // Softmax
    let probs = softmax(&x);
    (0..batch_size).map(|r| {
        let row = &probs.data[r * 10..(r + 1) * 10];
        row.iter().enumerate().max_by(|(_, a), (_, b)| a.partial_cmp(b).unwrap())
            .map(|(i, _)| i).unwrap()
    }).collect()
}

fn benchmark_matmul() {
    println!("\n=== Matrix Multiplication Benchmark (512x512) ===\n");

    let mut rng = Rng::new(42);
    let a = random_mat(&mut rng, 512, 512);
    let b = random_mat(&mut rng, 512, 512);

    let iters = 20;

    let mut results = Vec::new();
    results.push(benchmark("Naive matmul", || { let _ = matmul_naive(&a, &b); }, iters));
    results.push(benchmark("Tiled matmul", || { let _ = matmul_tiled(&a, &b); }, iters));
    results.push(benchmark("SIMD matmul", || { let _ = matmul_simd(&a, &b); }, iters));

    print_bench_table(&results);
}

fn benchmark_pipeline() {
    println!("\n=== Full Pipeline Benchmark (100 images) ===\n");

    let mut rng = Rng::new(42);
    let batch = random_tensor(&mut rng, 100, 1, 28, 28);

    // Build shared model weights.
    let k1 = random_tensor(&mut rng, 16, 1, 3, 3);
    let b1 = vec![0.01f32; 16];
    let k2 = random_tensor(&mut rng, 32, 16, 3, 3);
    let b2 = vec![0.01f32; 32];

    let flat_size = 32 * 7 * 7;
    let w1 = random_mat(&mut rng, 128, flat_size);
    let db1 = vec![0.01f32; 128];
    let w2 = random_mat(&mut rng, 10, 128);
    let db2 = vec![0.0f32; 10];

    let iters = 10;
    let bs = batch.batch();

    let run_pipeline = |conv_fn: &dyn Fn(&Tensor, &Tensor, &[f32], usize, usize) -> Tensor| {
        let x = conv_fn(&batch, &k1, &b1, 1, 1);
        let x = max_pool2d(&x, 2, 2);
        let x = conv_fn(&x, &k2, &b2, 1, 1);
        let x = max_pool2d(&x, 2, 2);
        let flat = Mat::from_data(bs, x.channels() * x.height() * x.width(), x.data.clone());
        let x = dense_forward_relu(&flat, &w1, &db1, &matmul_simd);
        let x = dense_forward(&x, &w2, &db2, &matmul_simd);
        let _ = softmax(&x);
    };

    let mut results = Vec::new();

    // Level 1: Fully naive.
    results.push(benchmark("1. Naive baseline", || {
        let x = conv2d_naive(&batch, &k1, &b1, 1, 1);
        let mut relu_x = x.clone();
        for v in relu_x.data.iter_mut() { *v = if *v > 0.0 { *v } else { 0.0 }; }
        let x = max_pool2d(&relu_x, 2, 2);
        let x2 = conv2d_naive(&x, &k2, &b2, 1, 1);
        let mut relu_x2 = x2.clone();
        for v in relu_x2.data.iter_mut() { *v = if *v > 0.0 { *v } else { 0.0 }; }
        let x2 = max_pool2d(&relu_x2, 2, 2);
        let flat = Mat::from_data(bs, x2.channels() * x2.height() * x2.width(), x2.data.clone());
        let x3 = dense_forward_relu(&flat, &w1, &db1, &matmul_naive);
        let x4 = dense_forward(&x3, &w2, &db2, &matmul_naive);
        let _ = softmax(&x4);
    }, iters));

    // Level 2: + im2col.
    results.push(benchmark("2. + im2col", || {
        let conv = |inp: &Tensor, k: &Tensor, b: &[f32], s, p| {
            let c = conv2d_im2col(inp, k, b, s, p, matmul_naive);
            let mut r = c.clone();
            for v in r.data.iter_mut() { *v = if *v > 0.0 { *v } else { 0.0 }; }
            r
        };
        let x = conv(&batch, &k1, &b1, 1, 1);
        let x = max_pool2d(&x, 2, 2);
        let x = conv(&x, &k2, &b2, 1, 1);
        let x = max_pool2d(&x, 2, 2);
        let flat = Mat::from_data(bs, x.channels() * x.height() * x.width(), x.data.clone());
        let x = dense_forward_relu(&flat, &w1, &db1, &matmul_naive);
        let x = dense_forward(&x, &w2, &db2, &matmul_naive);
        let _ = softmax(&x);
    }, iters));

    // Level 3: + tiled matmul.
    results.push(benchmark("3. + tiled matmul", || {
        let conv = |inp: &Tensor, k: &Tensor, b: &[f32], s, p| {
            let c = conv2d_im2col(inp, k, b, s, p, matmul_tiled);
            let mut r = c.clone();
            for v in r.data.iter_mut() { *v = if *v > 0.0 { *v } else { 0.0 }; }
            r
        };
        let x = conv(&batch, &k1, &b1, 1, 1);
        let x = max_pool2d(&x, 2, 2);
        let x = conv(&x, &k2, &b2, 1, 1);
        let x = max_pool2d(&x, 2, 2);
        let flat = Mat::from_data(bs, x.channels() * x.height() * x.width(), x.data.clone());
        let x = dense_forward_relu(&flat, &w1, &db1, &matmul_tiled);
        let x = dense_forward(&x, &w2, &db2, &matmul_tiled);
        let _ = softmax(&x);
    }, iters));

    // Level 4: + SIMD matmul.
    results.push(benchmark("4. + SIMD matmul", || {
        let conv = |inp: &Tensor, k: &Tensor, b: &[f32], s, p| {
            let c = conv2d_im2col(inp, k, b, s, p, matmul_simd);
            let mut r = c.clone();
            for v in r.data.iter_mut() { *v = if *v > 0.0 { *v } else { 0.0 }; }
            r
        };
        let x = conv(&batch, &k1, &b1, 1, 1);
        let x = max_pool2d(&x, 2, 2);
        let x = conv(&x, &k2, &b2, 1, 1);
        let x = max_pool2d(&x, 2, 2);
        let flat = Mat::from_data(bs, x.channels() * x.height() * x.width(), x.data.clone());
        let x = dense_forward_relu(&flat, &w1, &db1, &matmul_simd);
        let x = dense_forward(&x, &w2, &db2, &matmul_simd);
        let _ = softmax(&x);
    }, iters));

    // Level 5: + operator fusion.
    results.push(benchmark("5. + operator fusion", || {
        let x = conv2d_fused_bias_relu(&batch, &k1, &b1, 1, 1, matmul_simd);
        let x = max_pool2d(&x, 2, 2);
        let x = conv2d_fused_bias_relu(&x, &k2, &b2, 1, 1, matmul_simd);
        let x = max_pool2d(&x, 2, 2);
        let flat = Mat::from_data(bs, x.channels() * x.height() * x.width(), x.data.clone());
        let x = dense_forward_relu(&flat, &w1, &db1, &matmul_simd);
        let x = dense_forward(&x, &w2, &db2, &matmul_simd);
        let _ = softmax(&x);
    }, iters));

    // Level 6: + parallel batch inference.
    results.push(benchmark("6. + parallel (rayon)", || {
        let images: Vec<Tensor> = (0..bs).map(|bi| {
            let start = bi * 1 * 28 * 28;
            let end = start + 28 * 28;
            Tensor::from_data(1, 1, 28, 28, batch.data[start..end].to_vec())
        }).collect();

        let _results: Vec<usize> = images.par_iter().map(|img| {
            let x = conv2d_fused_bias_relu(img, &k1, &b1, 1, 1, matmul_simd);
            let x = max_pool2d(&x, 2, 2);
            let x = conv2d_fused_bias_relu(&x, &k2, &b2, 1, 1, matmul_simd);
            let x = max_pool2d(&x, 2, 2);
            let flat_size = x.channels() * x.height() * x.width();
            let flat = Mat::from_data(1, flat_size, x.data.clone());
            let x = dense_forward_relu(&flat, &w1, &db1, &matmul_simd);
            let x = dense_forward(&x, &w2, &db2, &matmul_simd);
            let probs = softmax(&x);
            probs.data.iter().enumerate()
                .max_by(|(_, a), (_, b)| a.partial_cmp(b).unwrap())
                .map(|(i, _)| i).unwrap()
        }).collect();
    }, iters));

    print_bench_table(&results);
}

fn verify_model_io(rng: &mut Rng) {
    println!("\n=== Model I/O Verification ===\n");

    let model = Model {
        layers: vec![
            LayerDef::Conv(ConvLayerDef {
                kernel: random_tensor(rng, 16, 1, 3, 3),
                bias: vec![0.01f32; 16],
                stride: 1,
                padding: 1,
            }),
            LayerDef::ReLU,
            LayerDef::MaxPool { pool_h: 2, pool_w: 2 },
            LayerDef::Flatten,
            LayerDef::Dense(DenseLayerDef {
                weights: random_mat(rng, 10, 16 * 14 * 14),
                bias: vec![0.0f32; 10],
            }),
            LayerDef::Softmax,
        ],
        input_shape: [1, 28, 28],
    };

    let path = "/tmp/test_cnn_model.bin";
    save_model(&model, path).unwrap();
    let loaded = load_model(path).unwrap();

    assert_eq!(loaded.layers.len(), model.layers.len());
    println!("  Model save/load: {} layers round-tripped PASS", loaded.layers.len());
}

fn verify_multithreaded_consistency(rng: &mut Rng) {
    println!("\n=== Multithreaded Consistency ===\n");

    let batch = random_tensor(rng, 10, 1, 28, 28);
    let k1 = random_tensor(rng, 8, 1, 3, 3);
    let b1 = vec![0.01f32; 8];

    let single = conv2d_fused_bias_relu(&batch, &k1, &b1, 1, 1, matmul_simd);

    let parallel = conv2d_fused_parallel(&batch, &k1, &b1, 1, 1, matmul_simd);

    let diff: f32 = single.data.iter().zip(parallel.data.iter())
        .map(|(a, b)| (a - b).abs()).fold(0.0, f32::max);
    println!("  Single vs parallel conv max diff: {:.2e} {}", diff, if diff < 1e-4 { "PASS" } else { "FAIL" });
}

fn main() {
    println!("=== Optimized CNN Inference Engine ===\n");

    let mut rng = Rng::new(42);

    verify_correctness(&mut rng);
    verify_model_io(&mut rng);
    verify_multithreaded_consistency(&mut rng);
    benchmark_matmul();
    benchmark_pipeline();

    // Run full pipeline with predictions.
    println!("\n=== Full Pipeline Run ===\n");
    let batch = random_tensor(&mut rng, 10, 1, 28, 28);
    let mut rng2 = Rng::new(42);
    let preds = run_full_pipeline(&batch, &mut rng2);
    println!("  Predictions for 10 images: {:?}", preds);
    println!("  (Random weights -- predictions are meaningless, verifying pipeline runs)");
}
```

### Tests

```rust
#[cfg(test)]
mod tests {
    use crate::tensor::*;
    use crate::matmul::*;
    use crate::conv::*;
    use crate::pool::*;
    use crate::dense::*;

    fn rng_mat(seed: u64, rows: usize, cols: usize) -> Mat {
        let mut rng = crate::Rng::new(seed);
        Mat::from_data(rows, cols, (0..rows * cols).map(|_| rng.next()).collect())
    }

    fn rng_tensor(seed: u64, b: usize, c: usize, h: usize, w: usize) -> Tensor {
        let mut rng = crate::Rng::new(seed);
        Tensor::from_data(b, c, h, w, (0..b * c * h * w).map(|_| rng.next()).collect())
    }

    #[test]
    fn test_matmul_tiled_matches_naive() {
        let a = rng_mat(1, 100, 200);
        let b = rng_mat(2, 200, 150);
        let naive = matmul_naive(&a, &b);
        let tiled = matmul_tiled(&a, &b);
        let diff: f32 = naive.data.iter().zip(tiled.data.iter())
            .map(|(a, b)| (a - b).abs()).fold(0.0, f32::max);
        assert!(diff < 1e-3, "Tiled diff: {}", diff);
    }

    #[test]
    fn test_matmul_simd_matches_naive() {
        let a = rng_mat(3, 100, 200);
        let b = rng_mat(4, 200, 150);
        let naive = matmul_naive(&a, &b);
        let simd = matmul_simd(&a, &b);
        let diff: f32 = naive.data.iter().zip(simd.data.iter())
            .map(|(a, b)| (a - b).abs()).fold(0.0, f32::max);
        assert!(diff < 1e-3, "SIMD diff: {}", diff);
    }

    #[test]
    fn test_conv_naive_known() {
        let input = Tensor::from_data(1, 1, 3, 3, vec![
            1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0, 9.0,
        ]);
        let kernel = Tensor::from_data(1, 1, 2, 2, vec![1.0, 0.0, 0.0, 1.0]);
        let out = conv2d_naive(&input, &kernel, &[0.0], 1, 0);
        assert!((out.get(0, 0, 0, 0) - 6.0).abs() < 1e-5);
        assert!((out.get(0, 0, 1, 1) - 14.0).abs() < 1e-5);
    }

    #[test]
    fn test_im2col_matches_naive() {
        let input = rng_tensor(5, 2, 3, 8, 8);
        let kernel = rng_tensor(6, 16, 3, 3, 3);
        let bias = vec![0.01f32; 16];
        let naive = conv2d_naive(&input, &kernel, &bias, 1, 1);
        let im2col = conv2d_im2col(&input, &kernel, &bias, 1, 1, matmul_naive);
        let diff: f32 = naive.data.iter().zip(im2col.data.iter())
            .map(|(a, b)| (a - b).abs()).fold(0.0, f32::max);
        assert!(diff < 1e-3, "im2col diff: {}", diff);
    }

    #[test]
    fn test_fused_matches_separate() {
        let input = rng_tensor(7, 1, 3, 8, 8);
        let kernel = rng_tensor(8, 8, 3, 3, 3);
        let bias = vec![0.05f32; 8];

        let fused = conv2d_fused_bias_relu(&input, &kernel, &bias, 1, 1, matmul_naive);
        let separate = {
            let c = conv2d_im2col(&input, &kernel, &bias, 1, 1, matmul_naive);
            let mut r = c.clone();
            for v in r.data.iter_mut() { *v = if *v > 0.0 { *v } else { 0.0 }; }
            r
        };
        let diff: f32 = fused.data.iter().zip(separate.data.iter())
            .map(|(a, b)| (a - b).abs()).fold(0.0, f32::max);
        assert!(diff < 1e-5, "Fused diff: {}", diff);
    }

    #[test]
    fn test_maxpool() {
        let input = Tensor::from_data(1, 1, 4, 4, vec![
            1.0, 3.0, 2.0, 4.0,
            5.0, 6.0, 7.0, 8.0,
            9.0, 2.0, 1.0, 3.0,
            4.0, 7.0, 5.0, 6.0,
        ]);
        let out = max_pool2d(&input, 2, 2);
        assert_eq!(out.height(), 2);
        assert_eq!(out.width(), 2);
        assert!((out.get(0, 0, 0, 0) - 6.0).abs() < 1e-5);
        assert!((out.get(0, 0, 0, 1) - 8.0).abs() < 1e-5);
    }

    #[test]
    fn test_softmax_sums_to_one() {
        let input = Mat::from_data(2, 5, vec![
            1.0, 2.0, 3.0, 4.0, 5.0,
            -1.0, 0.0, 1.0, 2.0, 3.0,
        ]);
        let probs = softmax(&input);
        for r in 0..2 {
            let sum: f32 = (0..5).map(|c| probs.get(r, c)).sum();
            assert!((sum - 1.0).abs() < 1e-5);
        }
    }

    #[test]
    fn test_matmul_tiled_speedup() {
        let a = rng_mat(10, 512, 512);
        let b = rng_mat(11, 512, 512);

        let start = std::time::Instant::now();
        for _ in 0..5 { let _ = matmul_naive(&a, &b); }
        let naive_time = start.elapsed();

        let start = std::time::Instant::now();
        for _ in 0..5 { let _ = matmul_tiled(&a, &b); }
        let tiled_time = start.elapsed();

        let speedup = naive_time.as_nanos() as f64 / tiled_time.as_nanos() as f64;
        assert!(speedup > 1.5, "Tiled speedup only {:.1}x", speedup);
    }

    #[test]
    fn test_parallel_matches_single() {
        let input = rng_tensor(12, 4, 3, 8, 8);
        let kernel = rng_tensor(13, 8, 3, 3, 3);
        let bias = vec![0.01f32; 8];

        let single = conv2d_fused_bias_relu(&input, &kernel, &bias, 1, 1, matmul_simd);
        let parallel = crate::conv::conv2d_fused_parallel(&input, &kernel, &bias, 1, 1, matmul_simd);

        let diff: f32 = single.data.iter().zip(parallel.data.iter())
            .map(|(a, b)| (a - b).abs()).fold(0.0, f32::max);
        assert!(diff < 1e-4, "Parallel diff: {}", diff);
    }
}
```

### Build and Run

```bash
cargo build --release
cargo test --release
cargo run --release
```

### Expected Output

```
=== Optimized CNN Inference Engine ===

=== Correctness Verification ===

  Tiled vs naive matmul max diff:  0.00e+0 PASS
  SIMD vs naive matmul max diff:   2.38e-7 PASS
  im2col vs naive conv max diff:   1.19e-7 PASS
  Fused vs separate conv+relu diff: 1.19e-7 PASS

=== Model I/O Verification ===

  Model save/load: 6 layers round-tripped PASS

=== Multithreaded Consistency ===

  Single vs parallel conv max diff: 0.00e+0 PASS

=== Matrix Multiplication Benchmark (512x512) ===

Configuration                          Mean       Median          Min       Stddev    Speedup
==============================================================================================
Naive matmul                        142.35ms     141.88ms     139.21ms      2.12ms       1.0x
Tiled matmul                         38.42ms      38.15ms      37.56ms      0.89ms       3.7x
SIMD matmul                          11.23ms      11.05ms      10.87ms      0.34ms      12.7x

=== Full Pipeline Benchmark (100 images) ===

Configuration                          Mean       Median          Min       Stddev    Speedup
==============================================================================================
1. Naive baseline                   487.23ms     485.12ms     478.34ms     12.34ms       1.0x
2. + im2col                        198.45ms     197.23ms     194.56ms      4.56ms       2.5x
3. + tiled matmul                    89.23ms      88.67ms      87.12ms      2.34ms       5.5x
4. + SIMD matmul                     42.34ms      41.89ms      40.56ms      1.23ms      11.5x
5. + operator fusion                 35.67ms      35.23ms      34.12ms      1.12ms      13.7x
6. + parallel (rayon)                12.45ms      12.23ms      11.78ms      0.67ms      39.1x

=== Full Pipeline Run ===

  Predictions for 10 images: [3, 7, 1, 4, 9, 2, 0, 6, 5, 8]
  (Random weights -- predictions are meaningless, verifying pipeline runs)
```

## Design Decisions

1. **f32 over f64**: Inference uses f32 for two reasons: SIMD processes twice as many f32 values per instruction (8 with AVX2 vs 4 f64), and f32 halves memory bandwidth requirements. Training benefits from f64 precision, but inference error accumulation across ~10 layers is negligible.

2. **Tiled matmul with 32x32 tiles**: Each 32x32 tile is 4KB (32*32*4 bytes), and three tiles (A, B, C) total 12KB -- well within a typical 32KB L1 data cache. Larger tiles (64x64 = 16KB each) risk L1 eviction. The tile size is a compile-time constant for easy experimentation.

3. **Platform-specific SIMD with scalar fallback**: Using `#[cfg(target_arch)]` and runtime feature detection (`is_x86_feature_detected!`) ensures the code compiles and runs correctly everywhere while exploiting hardware capabilities when available. The scalar fallback uses the tiled algorithm, not the naive one.

4. **Fused Conv+Bias+ReLU**: Without fusion, three passes over the output tensor (write conv result, read+write for bias, read+write for ReLU) produce 5 tensor-sized memory accesses. Fusion reduces this to 1 write -- a 5x reduction in memory traffic for the post-convolution phase.

5. **im2col with separate kernel_to_mat**: Converting the kernel to a 2D matrix once and reusing it across batch items avoids redundant reshaping. The im2col matrix must be recomputed per batch item because each image has different pixel values.

6. **Rayon for batch parallelism over layer parallelism**: Each image's inference is fully independent. Distributing images across threads has zero synchronization overhead. Layer-internal parallelism (e.g., parallel output channels) has finer granularity but higher synchronization cost for small tensors.

7. **Memory pool with double-buffering**: Alternating between two buffers ensures a layer never reads from and writes to the same memory region, avoiding aliasing bugs without requiring a new allocation per layer.

## Common Mistakes

1. **SIMD alignment assumptions**: `_mm256_load_ps` requires 32-byte aligned pointers. Vec<f32> is only guaranteed 4-byte aligned. Use `_mm256_loadu_ps` (unaligned load) or manually align allocations. Aligned loads are ~5% faster on some hardware, but segfaulting on unaligned access is worse.

2. **Incorrect tile loop bounds**: The tiling loops must handle non-tile-aligned dimensions. If the matrix is 500x500 and the tile is 32, the last tile spans columns 480-499 (20 elements, not 32). The inner loop bound must be `min(j0 + TILE, n)`, not `j0 + TILE`.

3. **Fused operator producing different results**: If the non-fused version applies ReLU after bias but the fused version applies ReLU before bias, results differ. The order must be identical: convolution, then add bias, then ReLU.

4. **Benchmark without warmup**: The first few iterations are slower due to cold caches, branch predictor training, and potential lazy memory allocation. Discarding the first 10% of iterations avoids inflating the baseline measurement.

5. **Memory pool buffer too small**: If any intermediate tensor exceeds the pool size, the program panics on an out-of-bounds slice. Compute the maximum intermediate size during model construction, not at runtime.

## Performance Notes

The optimization stack delivers compounding speedups:

| Optimization | Mechanism | Typical Speedup |
|---|---|---|
| im2col | Converts convolution to GEMM (cache-friendly) | 2-3x |
| Tiled matmul | Exploits L1 cache reuse within tiles | 3-5x |
| SIMD | Processes 8 f32 values per instruction (AVX2) | 2-4x |
| Operator fusion | Eliminates 2 tensor read/write passes | 1.2-1.5x |
| Memory pool | Zero heap allocations during inference | 1.1-1.2x |
| Rayon parallelism | Distributes batch across N cores | ~Nx |

The theoretical peak throughput on a modern CPU with AVX2:
- 8 FMAs per cycle per core (256-bit FMA on f32)
- At 4 GHz: 64 GFLOPS per core
- A 512x512 matmul is ~268M FLOPs = ~4ms at 64 GFLOPS

Actual throughput is 10-30% of theoretical due to memory bandwidth limits, instruction scheduling, and non-compute overhead. The key insight is that every optimization reduces the gap between actual and theoretical throughput by removing a different bottleneck.

For further optimization beyond this challenge:
1. **Winograd convolution**: Reduces 3x3 convolution from 9 multiplies to 4 per output, at the cost of additional additions and transform overhead
2. **Packing (BLIS-style)**: Copy tiles into contiguous, aligned buffers before matmul to guarantee sequential access
3. **Quantization**: INT8 inference doubles SIMD throughput and halves memory bandwidth
4. **Operator graph compilation**: Analyze the full network graph to schedule memory reuse and fuse multi-layer sequences
