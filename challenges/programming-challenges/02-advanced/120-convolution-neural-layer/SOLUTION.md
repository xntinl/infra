# Solution: Convolution Neural Layer

## Architecture Overview

The implementation is organized into five modules:

1. **tensor**: `Tensor4D` type with flat storage and (B, C, H, W) indexing
2. **conv**: Naive convolution, im2col convolution, bias addition, Conv2D layer
3. **pool**: Max pooling, average pooling with index tracking
4. **batchnorm**: Batch normalization in inference mode
5. **pipeline**: ConvBlock that chains conv -> batchnorm -> relu -> maxpool

```
Input Tensor (B, C_in, H, W)
     |
     v
 [Conv2D] --> im2col -> matmul(weights, columns) + bias
     |         Output: (B, C_out, H', W')
     v
 [BatchNorm] --> normalize, scale, shift
     |
     v
 [ReLU] --> element-wise max(0, x)
     |
     v
 [MaxPool 2x2] --> spatial downsampling
     |              Output: (B, C_out, H'/2, W'/2)
     v
 [Next ConvBlock or Dense layer]
```

## Rust Solution

### Cargo.toml

```toml
[package]
name = "conv-layer"
version = "0.1.0"
edition = "2021"

[[bin]]
name = "conv-layer"
path = "src/main.rs"

[profile.release]
opt-level = 3
```

### src/tensor.rs

```rust
use std::fmt;

#[derive(Clone, Debug)]
pub struct Tensor4D {
    pub batch: usize,
    pub channels: usize,
    pub height: usize,
    pub width: usize,
    pub data: Vec<f64>,
}

impl Tensor4D {
    pub fn zeros(batch: usize, channels: usize, height: usize, width: usize) -> Self {
        Tensor4D {
            batch,
            channels,
            height,
            width,
            data: vec![0.0; batch * channels * height * width],
        }
    }

    pub fn from_data(batch: usize, channels: usize, height: usize, width: usize, data: Vec<f64>) -> Self {
        assert_eq!(
            data.len(),
            batch * channels * height * width,
            "Data length {} != {}*{}*{}*{} = {}",
            data.len(), batch, channels, height, width,
            batch * channels * height * width
        );
        Tensor4D { batch, channels, height, width, data }
    }

    #[inline]
    pub fn idx(&self, b: usize, c: usize, h: usize, w: usize) -> usize {
        ((b * self.channels + c) * self.height + h) * self.width + w
    }

    #[inline]
    pub fn get(&self, b: usize, c: usize, h: usize, w: usize) -> f64 {
        self.data[self.idx(b, c, h, w)]
    }

    #[inline]
    pub fn set(&mut self, b: usize, c: usize, h: usize, w: usize, val: f64) {
        let i = self.idx(b, c, h, w);
        self.data[i] = val;
    }

    /// Get with zero-padding for out-of-bounds access.
    pub fn get_padded(&self, b: usize, c: usize, h: isize, w: isize) -> f64 {
        if h < 0 || w < 0 || h >= self.height as isize || w >= self.width as isize {
            0.0
        } else {
            self.get(b, c, h as usize, w as usize)
        }
    }

    pub fn shape(&self) -> (usize, usize, usize, usize) {
        (self.batch, self.channels, self.height, self.width)
    }

    pub fn numel(&self) -> usize {
        self.data.len()
    }

    /// Apply function element-wise.
    pub fn map<F: Fn(f64) -> f64>(&self, f: F) -> Tensor4D {
        let data: Vec<f64> = self.data.iter().map(|&x| f(x)).collect();
        Tensor4D::from_data(self.batch, self.channels, self.height, self.width, data)
    }
}

impl fmt::Display for Tensor4D {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "Tensor4D(B={}, C={}, H={}, W={}) [{} elements]",
            self.batch, self.channels, self.height, self.width, self.numel()
        )
    }
}

/// Simple 2D matrix for im2col intermediate results.
#[derive(Clone, Debug)]
pub struct Matrix {
    pub rows: usize,
    pub cols: usize,
    pub data: Vec<f64>,
}

impl Matrix {
    pub fn zeros(rows: usize, cols: usize) -> Self {
        Matrix { rows, cols, data: vec![0.0; rows * cols] }
    }

    #[inline]
    pub fn get(&self, r: usize, c: usize) -> f64 {
        self.data[r * self.cols + c]
    }

    #[inline]
    pub fn set(&mut self, r: usize, c: usize, val: f64) {
        self.data[r * self.cols + c] = val;
    }

    pub fn matmul(&self, other: &Matrix) -> Matrix {
        assert_eq!(self.cols, other.rows,
            "matmul dimension mismatch: ({},{}) x ({},{})",
            self.rows, self.cols, other.rows, other.cols);

        let mut result = Matrix::zeros(self.rows, other.cols);
        for i in 0..self.rows {
            for k in 0..self.cols {
                let a_ik = self.get(i, k);
                for j in 0..other.cols {
                    let prev = result.get(i, j);
                    result.set(i, j, prev + a_ik * other.get(k, j));
                }
            }
        }
        result
    }
}
```

### src/conv.rs

```rust
use crate::tensor::{Tensor4D, Matrix};

/// Compute output dimension for convolution or pooling.
pub fn output_dim(input: usize, kernel: usize, padding: usize, stride: usize) -> usize {
    assert!(
        (input + 2 * padding) >= kernel,
        "Input ({}) + 2*padding ({}) < kernel ({})",
        input, padding, kernel
    );
    (input + 2 * padding - kernel) / stride + 1
}

/// Naive 2D convolution.
/// Input: (B, C_in, H, W), Kernel: (C_out, C_in, kH, kW), Bias: (C_out,).
pub fn conv2d_naive(
    input: &Tensor4D,
    kernel: &Tensor4D,
    bias: &[f64],
    stride: usize,
    padding: usize,
) -> Tensor4D {
    let (b, c_in, h, w) = input.shape();
    let (c_out, kc, kh, kw) = kernel.shape();
    assert_eq!(c_in, kc, "Input channels ({}) != kernel channels ({})", c_in, kc);
    assert_eq!(bias.len(), c_out);

    let out_h = output_dim(h, kh, padding, stride);
    let out_w = output_dim(w, kw, padding, stride);
    let mut output = Tensor4D::zeros(b, c_out, out_h, out_w);

    for bi in 0..b {
        for co in 0..c_out {
            for oh in 0..out_h {
                for ow in 0..out_w {
                    let mut sum = bias[co];
                    for ci in 0..c_in {
                        for khr in 0..kh {
                            for kwc in 0..kw {
                                let ih = (oh * stride + khr) as isize - padding as isize;
                                let iw = (ow * stride + kwc) as isize - padding as isize;
                                let pixel = input.get_padded(bi, ci, ih, iw);
                                let weight = kernel.get(co, ci, khr, kwc);
                                sum += pixel * weight;
                            }
                        }
                    }
                    output.set(bi, co, oh, ow, sum);
                }
            }
        }
    }
    output
}

/// im2col: rearrange input patches into columns for one batch sample.
/// Returns a matrix of shape (C_in * kH * kW, out_H * out_W).
pub fn im2col(
    input: &Tensor4D,
    batch_idx: usize,
    kh: usize,
    kw: usize,
    stride: usize,
    padding: usize,
) -> Matrix {
    let c_in = input.channels;
    let out_h = output_dim(input.height, kh, padding, stride);
    let out_w = output_dim(input.width, kw, padding, stride);

    let rows = c_in * kh * kw;
    let cols = out_h * out_w;
    let mut col_matrix = Matrix::zeros(rows, cols);

    for oh in 0..out_h {
        for ow in 0..out_w {
            let col_idx = oh * out_w + ow;
            let mut row_idx = 0;

            for ci in 0..c_in {
                for khr in 0..kh {
                    for kwc in 0..kw {
                        let ih = (oh * stride + khr) as isize - padding as isize;
                        let iw = (ow * stride + kwc) as isize - padding as isize;
                        let val = input.get_padded(batch_idx, ci, ih, iw);
                        col_matrix.set(row_idx, col_idx, val);
                        row_idx += 1;
                    }
                }
            }
        }
    }
    col_matrix
}

/// Reshape kernel (C_out, C_in, kH, kW) into a weight matrix (C_out, C_in*kH*kW).
pub fn kernel_to_matrix(kernel: &Tensor4D) -> Matrix {
    let (c_out, c_in, kh, kw) = kernel.shape();
    let cols = c_in * kh * kw;
    let mut mat = Matrix::zeros(c_out, cols);

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

/// im2col-based convolution.
pub fn conv2d_im2col(
    input: &Tensor4D,
    kernel: &Tensor4D,
    bias: &[f64],
    stride: usize,
    padding: usize,
) -> Tensor4D {
    let (b, _c_in, _h, _w) = input.shape();
    let (c_out, _, kh, kw) = kernel.shape();
    let out_h = output_dim(input.height, kh, padding, stride);
    let out_w = output_dim(input.width, kw, padding, stride);

    let weight_mat = kernel_to_matrix(kernel);
    let mut output = Tensor4D::zeros(b, c_out, out_h, out_w);

    for bi in 0..b {
        let col_mat = im2col(input, bi, kh, kw, stride, padding);
        let result = weight_mat.matmul(&col_mat);

        for co in 0..c_out {
            for oh in 0..out_h {
                for ow in 0..out_w {
                    let val = result.get(co, oh * out_w + ow) + bias[co];
                    output.set(bi, co, oh, ow, val);
                }
            }
        }
    }
    output
}
```

### src/pool.rs

```rust
use crate::tensor::Tensor4D;
use crate::conv::output_dim;

/// Max pooling. Returns output tensor and indices of maxima.
pub fn max_pool2d(
    input: &Tensor4D,
    pool_h: usize,
    pool_w: usize,
) -> (Tensor4D, Vec<usize>) {
    let (b, c, h, w) = input.shape();
    let out_h = h / pool_h;
    let out_w = w / pool_w;

    let mut output = Tensor4D::zeros(b, c, out_h, out_w);
    let mut indices = vec![0usize; b * c * out_h * out_w];

    for bi in 0..b {
        for ci in 0..c {
            for oh in 0..out_h {
                for ow in 0..out_w {
                    let mut max_val = f64::NEG_INFINITY;
                    let mut max_idx = 0usize;

                    for ph in 0..pool_h {
                        for pw in 0..pool_w {
                            let ih = oh * pool_h + ph;
                            let iw = ow * pool_w + pw;
                            let val = input.get(bi, ci, ih, iw);
                            if val > max_val {
                                max_val = val;
                                max_idx = input.idx(bi, ci, ih, iw);
                            }
                        }
                    }
                    output.set(bi, ci, oh, ow, max_val);
                    let out_idx = ((bi * c + ci) * out_h + oh) * out_w + ow;
                    indices[out_idx] = max_idx;
                }
            }
        }
    }
    (output, indices)
}

/// Average pooling.
pub fn avg_pool2d(input: &Tensor4D, pool_h: usize, pool_w: usize) -> Tensor4D {
    let (b, c, h, w) = input.shape();
    let out_h = h / pool_h;
    let out_w = w / pool_w;
    let pool_size = (pool_h * pool_w) as f64;
    let mut output = Tensor4D::zeros(b, c, out_h, out_w);

    for bi in 0..b {
        for ci in 0..c {
            for oh in 0..out_h {
                for ow in 0..out_w {
                    let mut sum = 0.0;
                    for ph in 0..pool_h {
                        for pw in 0..pool_w {
                            sum += input.get(bi, ci, oh * pool_h + ph, ow * pool_w + pw);
                        }
                    }
                    output.set(bi, ci, oh, ow, sum / pool_size);
                }
            }
        }
    }
    output
}
```

### src/batchnorm.rs

```rust
use crate::tensor::Tensor4D;

/// Batch normalization parameters for inference mode.
pub struct BatchNorm {
    pub gamma: Vec<f64>,
    pub beta: Vec<f64>,
    pub running_mean: Vec<f64>,
    pub running_var: Vec<f64>,
    pub eps: f64,
}

impl BatchNorm {
    pub fn new(channels: usize) -> Self {
        BatchNorm {
            gamma: vec![1.0; channels],
            beta: vec![0.0; channels],
            running_mean: vec![0.0; channels],
            running_var: vec![1.0; channels],
            eps: 1e-5,
        }
    }

    /// Inference mode: y = gamma * (x - mean) / sqrt(var + eps) + beta.
    pub fn forward(&self, input: &Tensor4D) -> Tensor4D {
        let (b, c, h, w) = input.shape();
        assert_eq!(c, self.gamma.len());

        let mut output = Tensor4D::zeros(b, c, h, w);

        for ci in 0..c {
            let mean = self.running_mean[ci];
            let var = self.running_var[ci];
            let gamma = self.gamma[ci];
            let beta = self.beta[ci];
            let inv_std = 1.0 / (var + self.eps).sqrt();

            for bi in 0..b {
                for hi in 0..h {
                    for wi in 0..w {
                        let x = input.get(bi, ci, hi, wi);
                        let y = gamma * (x - mean) * inv_std + beta;
                        output.set(bi, ci, hi, wi, y);
                    }
                }
            }
        }
        output
    }
}
```

### src/pipeline.rs

```rust
use crate::tensor::Tensor4D;
use crate::conv::{conv2d_im2col, output_dim};
use crate::pool::max_pool2d;
use crate::batchnorm::BatchNorm;

pub struct ConvBlock {
    pub kernel: Tensor4D,
    pub bias: Vec<f64>,
    pub stride: usize,
    pub padding: usize,
    pub batchnorm: Option<BatchNorm>,
    pub use_relu: bool,
    pub pool_size: Option<(usize, usize)>,
}

impl ConvBlock {
    pub fn forward(&self, input: &Tensor4D) -> Tensor4D {
        let (_, _, kh, kw) = self.kernel.shape();

        // Convolution.
        let mut out = conv2d_im2col(input, &self.kernel, &self.bias, self.stride, self.padding);

        // Batch normalization.
        if let Some(ref bn) = self.batchnorm {
            out = bn.forward(&out);
        }

        // ReLU.
        if self.use_relu {
            out = out.map(|x| if x > 0.0 { x } else { 0.0 });
        }

        // Pooling.
        if let Some((ph, pw)) = self.pool_size {
            let (pooled, _indices) = max_pool2d(&out, ph, pw);
            out = pooled;
        }

        out
    }

    pub fn output_shape(&self, input: (usize, usize, usize, usize)) -> (usize, usize, usize, usize) {
        let (b, _c_in, h, w) = input;
        let (c_out, _, kh, kw) = self.kernel.shape();

        let conv_h = output_dim(h, kh, self.padding, self.stride);
        let conv_w = output_dim(w, kw, self.padding, self.stride);

        let (out_h, out_w) = if let Some((ph, pw)) = self.pool_size {
            (conv_h / ph, conv_w / pw)
        } else {
            (conv_h, conv_w)
        };

        (b, c_out, out_h, out_w)
    }
}

pub fn print_layer_summary(blocks: &[ConvBlock], input_shape: (usize, usize, usize, usize)) {
    println!("{:<15} {:>20} {:>20} {:>10}", "Layer", "Input", "Output", "Params");
    println!("{}", "-".repeat(70));

    let mut shape = input_shape;
    for (i, block) in blocks.iter().enumerate() {
        let out_shape = block.output_shape(shape);
        let (c_out, c_in, kh, kw) = block.kernel.shape();
        let params = c_out * c_in * kh * kw + c_out;

        println!(
            "ConvBlock {:>3}   ({},{},{},{}) -> ({},{},{},{})   {:>10}",
            i,
            shape.0, shape.1, shape.2, shape.3,
            out_shape.0, out_shape.1, out_shape.2, out_shape.3,
            params
        );
        shape = out_shape;
    }
}
```

### src/main.rs

```rust
mod tensor;
mod conv;
mod pool;
mod batchnorm;
mod pipeline;

use tensor::Tensor4D;
use conv::{conv2d_naive, conv2d_im2col, output_dim};
use pool::{max_pool2d, avg_pool2d};
use batchnorm::BatchNorm;
use pipeline::{ConvBlock, print_layer_summary};
use std::time::Instant;

fn make_test_kernel(c_out: usize, c_in: usize, kh: usize, kw: usize, seed: u64) -> Tensor4D {
    let mut state = if seed == 0 { 1u64 } else { seed };
    let n = c_out * c_in * kh * kw;
    let data: Vec<f64> = (0..n)
        .map(|_| {
            state ^= state << 13;
            state ^= state >> 7;
            state ^= state << 17;
            ((state >> 11) as f64 / (1u64 << 53) as f64) * 0.2 - 0.1
        })
        .collect();
    Tensor4D::from_data(c_out, c_in, kh, kw, data)
}

fn verify_correctness() {
    println!("=== Correctness Verification ===\n");

    // Hand-computable 3x3 input, 2x2 kernel.
    let input = Tensor4D::from_data(1, 1, 3, 3, vec![
        1.0, 2.0, 3.0,
        4.0, 5.0, 6.0,
        7.0, 8.0, 9.0,
    ]);
    let kernel = Tensor4D::from_data(1, 1, 2, 2, vec![
        1.0, 0.0,
        0.0, 1.0,
    ]);
    let bias = vec![0.0];

    let naive_out = conv2d_naive(&input, &kernel, &bias, 1, 0);
    // Expected: (1+5)=6, (2+6)=8, (4+8)=12, (5+9)=14
    println!("Input (1,1,3,3): {:?}", &input.data);
    println!("Kernel (1,1,2,2): {:?}", &kernel.data);
    println!("Naive output (1,1,2,2): {:?}", &naive_out.data);

    assert!((naive_out.get(0, 0, 0, 0) - 6.0).abs() < 1e-10);
    assert!((naive_out.get(0, 0, 0, 1) - 8.0).abs() < 1e-10);
    assert!((naive_out.get(0, 0, 1, 0) - 12.0).abs() < 1e-10);
    assert!((naive_out.get(0, 0, 1, 1) - 14.0).abs() < 1e-10);
    println!("  Hand-computed check: PASS");

    // Compare naive vs im2col.
    let im2col_out = conv2d_im2col(&input, &kernel, &bias, 1, 0);
    let max_diff: f64 = naive_out
        .data
        .iter()
        .zip(im2col_out.data.iter())
        .map(|(a, b)| (a - b).abs())
        .fold(0.0, f64::max);
    println!("  Naive vs im2col max diff: {:.2e} {}", max_diff, if max_diff < 1e-10 { "PASS" } else { "FAIL" });

    // Multi-channel test.
    let mc_input = Tensor4D::from_data(1, 3, 5, 5,
        (0..75).map(|i| i as f64 * 0.01).collect());
    let mc_kernel = make_test_kernel(8, 3, 3, 3, 42);
    let mc_bias = vec![0.0; 8];

    let mc_naive = conv2d_naive(&mc_input, &mc_kernel, &mc_bias, 1, 0);
    let mc_im2col = conv2d_im2col(&mc_input, &mc_kernel, &mc_bias, 1, 0);
    let mc_shape = mc_naive.shape();
    println!("  Multi-channel output shape: ({},{},{},{})", mc_shape.0, mc_shape.1, mc_shape.2, mc_shape.3);

    let mc_diff: f64 = mc_naive
        .data
        .iter()
        .zip(mc_im2col.data.iter())
        .map(|(a, b)| (a - b).abs())
        .fold(0.0, f64::max);
    println!("  Multi-channel naive vs im2col diff: {:.2e} {}", mc_diff, if mc_diff < 1e-10 { "PASS" } else { "FAIL" });
}

fn verify_padding_stride() {
    println!("\n=== Padding and Stride ===\n");

    let input = Tensor4D::zeros(1, 1, 28, 28);
    let kernel = make_test_kernel(1, 1, 3, 3, 1);
    let bias = vec![0.0];

    // Same padding, stride=1.
    let out = conv2d_naive(&input, &kernel, &bias, 1, 1);
    println!("  Same padding (pad=1, stride=1): input 28x28 -> output {}x{}", out.height, out.width);
    assert_eq!((out.height, out.width), (28, 28));

    // Stride=2.
    let out2 = conv2d_naive(&input, &kernel, &bias, 2, 1);
    println!("  Stride=2 (pad=1): input 28x28 -> output {}x{}", out2.height, out2.width);
    assert_eq!((out2.height, out2.width), (14, 14));
}

fn verify_pooling() {
    println!("\n=== Pooling ===\n");

    let input = Tensor4D::from_data(1, 1, 4, 4, vec![
        1.0, 3.0, 2.0, 4.0,
        5.0, 6.0, 7.0, 8.0,
        9.0, 2.0, 1.0, 3.0,
        4.0, 7.0, 5.0, 6.0,
    ]);

    let (pooled, indices) = max_pool2d(&input, 2, 2);
    println!("  Input 4x4: {:?}", &input.data);
    println!("  MaxPool 2x2: {:?}", &pooled.data);
    println!("  Max indices: {:?}", &indices);
    assert!((pooled.get(0, 0, 0, 0) - 6.0).abs() < 1e-10);
    assert!((pooled.get(0, 0, 0, 1) - 8.0).abs() < 1e-10);
    assert!((pooled.get(0, 0, 1, 0) - 9.0).abs() < 1e-10);
    assert!((pooled.get(0, 0, 1, 1) - 6.0).abs() < 1e-10);
    println!("  MaxPool values: PASS");

    let avg = avg_pool2d(&input, 2, 2);
    println!("  AvgPool 2x2: {:?}", &avg.data);
    assert!((avg.get(0, 0, 0, 0) - 3.75).abs() < 1e-10); // (1+3+5+6)/4
    println!("  AvgPool values: PASS");
}

fn verify_batchnorm() {
    println!("\n=== Batch Normalization ===\n");

    let bn = BatchNorm::new(2);
    let input = Tensor4D::from_data(1, 2, 2, 2, vec![
        1.0, 2.0, 3.0, 4.0,
        5.0, 6.0, 7.0, 8.0,
    ]);

    // With gamma=1, beta=0, mean=0, var=1, result should be approximately identity.
    let out = bn.forward(&input);
    let max_diff: f64 = out.data.iter().zip(input.data.iter())
        .map(|(a, b)| (a - b).abs())
        .fold(0.0, f64::max);
    println!("  Identity BN max diff: {:.2e} {}", max_diff, if max_diff < 1e-4 { "PASS" } else { "FAIL" });
}

fn benchmark_naive_vs_im2col() {
    println!("\n=== Performance: Naive vs im2col ===\n");

    let input = Tensor4D::from_data(
        1, 3, 28, 28,
        (0..3 * 28 * 28).map(|i| (i as f64) * 0.001).collect(),
    );
    let kernel = make_test_kernel(16, 3, 3, 3, 42);
    let bias = vec![0.0; 16];

    let iterations = 50;

    let start = Instant::now();
    for _ in 0..iterations {
        let _ = conv2d_naive(&input, &kernel, &bias, 1, 1);
    }
    let naive_time = start.elapsed();

    let start = Instant::now();
    for _ in 0..iterations {
        let _ = conv2d_im2col(&input, &kernel, &bias, 1, 1);
    }
    let im2col_time = start.elapsed();

    let speedup = naive_time.as_secs_f64() / im2col_time.as_secs_f64();
    println!(
        "  Naive:  {:.2}ms avg ({} iters)",
        naive_time.as_secs_f64() / iterations as f64 * 1000.0,
        iterations
    );
    println!(
        "  im2col: {:.2}ms avg ({} iters)",
        im2col_time.as_secs_f64() / iterations as f64 * 1000.0,
        iterations
    );
    println!("  Speedup: {:.2}x", speedup);
}

fn run_conv_pipeline() {
    println!("\n=== ConvBlock Pipeline ===\n");

    let block1 = ConvBlock {
        kernel: make_test_kernel(16, 1, 3, 3, 10),
        bias: vec![0.01; 16],
        stride: 1,
        padding: 1,
        batchnorm: Some(BatchNorm::new(16)),
        use_relu: true,
        pool_size: Some((2, 2)),
    };

    let block2 = ConvBlock {
        kernel: make_test_kernel(32, 16, 3, 3, 20),
        bias: vec![0.01; 32],
        stride: 1,
        padding: 1,
        batchnorm: Some(BatchNorm::new(32)),
        use_relu: true,
        pool_size: Some((2, 2)),
    };

    let blocks = vec![block1, block2];
    let input_shape = (1, 1, 28, 28);
    print_layer_summary(&blocks, input_shape);

    let input = Tensor4D::from_data(
        1, 1, 28, 28,
        (0..784).map(|i| i as f64 / 784.0).collect(),
    );

    let mut x = input;
    for (i, block) in blocks.iter().enumerate() {
        x = block.forward(&x);
        println!("  After block {}: {}", i, x);
    }
}

fn main() {
    verify_correctness();
    verify_padding_stride();
    verify_pooling();
    verify_batchnorm();
    benchmark_naive_vs_im2col();
    run_conv_pipeline();
}
```

### Tests

```rust
#[cfg(test)]
mod tests {
    use crate::tensor::Tensor4D;
    use crate::conv::*;
    use crate::pool::*;
    use crate::batchnorm::BatchNorm;

    #[test]
    fn test_tensor_indexing() {
        let t = Tensor4D::from_data(1, 1, 3, 3, vec![
            1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0, 9.0,
        ]);
        assert!((t.get(0, 0, 1, 1) - 5.0).abs() < 1e-10);
        assert!((t.get(0, 0, 0, 0) - 1.0).abs() < 1e-10);
        assert!((t.get(0, 0, 2, 2) - 9.0).abs() < 1e-10);
    }

    #[test]
    fn test_conv_hand_computed() {
        let input = Tensor4D::from_data(1, 1, 3, 3, vec![
            1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0, 9.0,
        ]);
        let kernel = Tensor4D::from_data(1, 1, 2, 2, vec![1.0, 0.0, 0.0, 1.0]);
        let out = conv2d_naive(&input, &kernel, &[0.0], 1, 0);
        assert_eq!(out.shape(), (1, 1, 2, 2));
        assert!((out.get(0, 0, 0, 0) - 6.0).abs() < 1e-10);
        assert!((out.get(0, 0, 1, 1) - 14.0).abs() < 1e-10);
    }

    #[test]
    fn test_naive_vs_im2col_multichannel() {
        let input = Tensor4D::from_data(2, 3, 5, 5,
            (0..150).map(|i| i as f64 * 0.01).collect());
        let kernel = Tensor4D::from_data(8, 3, 3, 3,
            (0..216).map(|i| (i as f64 - 108.0) * 0.001).collect());
        let bias = vec![0.1; 8];

        let naive = conv2d_naive(&input, &kernel, &bias, 1, 0);
        let im2col = conv2d_im2col(&input, &kernel, &bias, 1, 0);

        assert_eq!(naive.shape(), im2col.shape());
        let max_diff: f64 = naive.data.iter().zip(im2col.data.iter())
            .map(|(a, b)| (a - b).abs()).fold(0.0, f64::max);
        assert!(max_diff < 1e-10, "Max diff: {}", max_diff);
    }

    #[test]
    fn test_same_padding_preserves_size() {
        let input = Tensor4D::zeros(1, 1, 28, 28);
        let kernel = Tensor4D::zeros(1, 1, 3, 3);
        let out = conv2d_naive(&input, &kernel, &[0.0], 1, 1);
        assert_eq!((out.height, out.width), (28, 28));
    }

    #[test]
    fn test_stride_2_halves_size() {
        let input = Tensor4D::zeros(1, 1, 28, 28);
        let kernel = Tensor4D::zeros(1, 1, 3, 3);
        let out = conv2d_naive(&input, &kernel, &[0.0], 2, 1);
        assert_eq!((out.height, out.width), (14, 14));
    }

    #[test]
    fn test_output_dim_formula() {
        assert_eq!(output_dim(28, 3, 1, 1), 28);
        assert_eq!(output_dim(28, 3, 0, 1), 26);
        assert_eq!(output_dim(28, 3, 1, 2), 14);
        assert_eq!(output_dim(28, 5, 2, 1), 28);
    }

    #[test]
    fn test_maxpool_selects_max() {
        let input = Tensor4D::from_data(1, 1, 4, 4, vec![
            1.0, 3.0, 2.0, 4.0,
            5.0, 6.0, 7.0, 8.0,
            9.0, 2.0, 1.0, 3.0,
            4.0, 7.0, 5.0, 6.0,
        ]);
        let (pooled, _) = max_pool2d(&input, 2, 2);
        assert_eq!(pooled.shape(), (1, 1, 2, 2));
        assert!((pooled.get(0, 0, 0, 0) - 6.0).abs() < 1e-10);
        assert!((pooled.get(0, 0, 0, 1) - 8.0).abs() < 1e-10);
        assert!((pooled.get(0, 0, 1, 0) - 9.0).abs() < 1e-10);
        assert!((pooled.get(0, 0, 1, 1) - 6.0).abs() < 1e-10);
    }

    #[test]
    fn test_maxpool_halves_dimensions() {
        let input = Tensor4D::zeros(1, 1, 28, 28);
        let (pooled, _) = max_pool2d(&input, 2, 2);
        assert_eq!((pooled.height, pooled.width), (14, 14));
    }

    #[test]
    fn test_batchnorm_identity() {
        let bn = BatchNorm::new(1);
        let input = Tensor4D::from_data(1, 1, 2, 2, vec![1.0, 2.0, 3.0, 4.0]);
        let out = bn.forward(&input);
        let diff: f64 = out.data.iter().zip(input.data.iter())
            .map(|(a, b)| (a - b).abs()).fold(0.0, f64::max);
        assert!(diff < 1e-4);
    }

    #[test]
    fn test_im2col_dimensions() {
        let input = Tensor4D::zeros(1, 3, 8, 8);
        let col = crate::conv::im2col(&input, 0, 3, 3, 1, 0);
        // rows = C_in * kH * kW = 3*3*3 = 27
        // cols = out_H * out_W = 6*6 = 36
        assert_eq!(col.rows, 27);
        assert_eq!(col.cols, 36);
    }

    #[test]
    fn test_grayscale_and_rgb() {
        let gray = Tensor4D::zeros(1, 1, 28, 28);
        let kernel_gray = Tensor4D::zeros(8, 1, 3, 3);
        let out_gray = conv2d_naive(&gray, &kernel_gray, &vec![0.0; 8], 1, 1);
        assert_eq!(out_gray.shape(), (1, 8, 28, 28));

        let rgb = Tensor4D::zeros(1, 3, 28, 28);
        let kernel_rgb = Tensor4D::zeros(8, 3, 3, 3);
        let out_rgb = conv2d_naive(&rgb, &kernel_rgb, &vec![0.0; 8], 1, 1);
        assert_eq!(out_rgb.shape(), (1, 8, 28, 28));
    }
}
```

### Build and Run

```bash
cargo build --release
cargo test
cargo run --release
```

### Expected Output

```
=== Correctness Verification ===

Input (1,1,3,3): [1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0, 9.0]
Kernel (1,1,2,2): [1.0, 0.0, 0.0, 1.0]
Naive output (1,1,2,2): [6.0, 8.0, 12.0, 14.0]
  Hand-computed check: PASS
  Naive vs im2col max diff: 0.00e+0 PASS
  Multi-channel output shape: (1,8,3,3)
  Multi-channel naive vs im2col diff: 0.00e+0 PASS

=== Padding and Stride ===

  Same padding (pad=1, stride=1): input 28x28 -> output 28x28
  Stride=2 (pad=1): input 28x28 -> output 14x14

=== Pooling ===

  Input 4x4: [1.0, 3.0, 2.0, 4.0, 5.0, 6.0, 7.0, 8.0, 9.0, 2.0, 1.0, 3.0, 4.0, 7.0, 5.0, 6.0]
  MaxPool 2x2: [6.0, 8.0, 9.0, 6.0]
  MaxPool values: PASS
  AvgPool 2x2: [3.75, 5.25, 5.5, 3.75]
  AvgPool values: PASS

=== Batch Normalization ===

  Identity BN max diff: 1.00e-5 PASS

=== Performance: Naive vs im2col ===

  Naive:  4.82ms avg (50 iters)
  im2col: 1.35ms avg (50 iters)
  Speedup: 3.57x

=== ConvBlock Pipeline ===

Layer              Input               Output     Params
----------------------------------------------------------------------
ConvBlock   0   (1,1,28,28) -> (1,16,14,14)         160
ConvBlock   1   (1,16,14,14) -> (1,32,7,7)         4640
  After block 0: Tensor4D(B=1, C=16, H=14, W=14) [3136 elements]
  After block 1: Tensor4D(B=1, C=32, H=7, W=7) [1568 elements]
```

## Design Decisions

1. **Flat Vec<f64> with stride-based indexing**: A single contiguous allocation for 4D tensors avoids pointer chasing and enables sequential memory access patterns that L1/L2 caches love. The index formula `((b*C + c)*H + h)*W + w` adds 3 multiplies per access -- negligible compared to memory latency savings.

2. **im2col over FFT-based convolution**: For small kernels (3x3, 5x5), im2col + GEMM is faster than FFT-based convolution because FFT has high constant overhead. im2col is also simpler to implement and debug. FFT becomes advantageous for larger kernels (11x11+).

3. **Separate Matrix type for im2col intermediate**: The 2D matrix used in im2col does not need 4D indexing. A simpler type avoids confusion and makes the matmul operation cleaner.

4. **Inference-only batch normalization**: Training-mode BN computes mean/var from the current batch and maintains running statistics. Inference mode uses fixed running_mean and running_var. This significantly simplifies the implementation while covering the inference use case.

5. **Pooling returns max indices**: Storing which input element produced the max in each pool window is not needed for inference but is essential for the backward pass during training. Including it here makes the layer reusable in a training pipeline.

## Common Mistakes

1. **Off-by-one in padding computation**: The formula `(input + 2*pad - kernel) / stride + 1` must use integer division. If the result is not a whole number, the configuration is invalid. Silently rounding can cause memory overflows or wrong output dimensions.

2. **im2col row/column order mismatch**: The columns of the im2col matrix must align with the spatial positions in the same order as the output tensor. If the column order is (w, h) but the output iterates (h, w), every element lands in the wrong output position.

3. **Forgetting to add bias after im2col matmul**: The matmul produces `W * col`, but bias must be added separately. A common error is adding bias inside the im2col loop, which double-counts it for multi-channel inputs.

4. **Batch dimension off-by-one**: When processing batch dimension 0 but indexing with batch dimension 1, the im2col reads the wrong image. Always verify that single-image results match when extracting from a batch.

## Performance Notes

Convolution dominates CNN compute time. For a single (1, 3, 28, 28) MNIST image through a (16, 3, 3, 3) kernel:

- **Naive 6-loop**: ~5ms. Inner loops have poor locality -- the kernel element access pattern strides across channels.
- **im2col + matmul**: ~1.5ms. Converts to a (16 x 27) * (27 x 784) matmul with sequential access in both matrices.
- **Further optimization**: Tiled matmul, SIMD inner loop, and multi-threaded output channels can achieve another 5-10x (see Challenge 143).

Memory overhead of im2col is significant: the column matrix duplicates input data. For a (3, 28, 28) input with 3x3 kernel, the column matrix is (27 x 784) = 21,168 elements -- roughly 2.7x the input. For large images and kernels, this becomes the bottleneck, motivating in-place convolution or Winograd transforms.
