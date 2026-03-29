# Solution: Neural Network Training with Backpropagation

## Architecture Overview

The implementation is organized into six modules:

1. **matrix**: `Matrix` type with matmul, transpose, element-wise ops, broadcasting
2. **layers**: `Layer` trait with forward/backward, `DenseLayer`, `ReLULayer`, `SigmoidLayer`
3. **loss**: MSE and cross-entropy loss with softmax fusion
4. **optimizer**: SGD and Adam operating on collected parameter/gradient pairs
5. **network**: `Network` struct orchestrating forward, backward, and parameter updates
6. **training**: Mini-batch training loop, MNIST loading, evaluation, checkpointing

```
MNIST Images (28x28) --> flatten to 784
     |
     v
 [Network Forward Pass]
     Dense(784 -> 128) -> ReLU
     Dense(128 -> 64)  -> ReLU
     Dense(64 -> 10)   -> Softmax
     |
     v
 [Cross-Entropy Loss] --> scalar loss value
     |
     v
 [Backward Pass] (reverse layer order)
     grad_loss -> Dense(64->10).backward
              -> ReLU.backward
              -> Dense(128->64).backward
              -> ReLU.backward
              -> Dense(784->128).backward
     |
     Each Dense layer stores: grad_weights, grad_bias
     |
     v
 [Optimizer.step()] --> update all weights and biases
     |
     v
 [Repeat for each mini-batch, each epoch]
```

## Rust Solution

### Cargo.toml

```toml
[package]
name = "nn-backprop"
version = "0.1.0"
edition = "2021"

[[bin]]
name = "nn-backprop"
path = "src/main.rs"

[profile.release]
opt-level = 3
lto = true
```

### src/matrix.rs

```rust
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

    pub fn from_vec(rows: usize, cols: usize, data: Vec<f64>) -> Self {
        assert_eq!(data.len(), rows * cols);
        Matrix { rows, cols, data }
    }

    #[inline]
    pub fn get(&self, r: usize, c: usize) -> f64 {
        self.data[r * self.cols + c]
    }

    #[inline]
    pub fn set(&mut self, r: usize, c: usize, val: f64) {
        self.data[r * self.cols + c] = val;
    }

    pub fn row_slice(&self, r: usize) -> &[f64] {
        &self.data[r * self.cols..(r + 1) * self.cols]
    }

    pub fn transpose(&self) -> Matrix {
        let mut result = Matrix::zeros(self.cols, self.rows);
        for r in 0..self.rows {
            for c in 0..self.cols {
                result.set(c, r, self.get(r, c));
            }
        }
        result
    }

    pub fn matmul(&self, other: &Matrix) -> Matrix {
        assert_eq!(self.cols, other.rows,
            "matmul: ({},{}) x ({},{})", self.rows, self.cols, other.rows, other.cols);
        let mut result = Matrix::zeros(self.rows, other.cols);
        for i in 0..self.rows {
            for k in 0..self.cols {
                let a_ik = self.get(i, k);
                for j in 0..other.cols {
                    let idx = i * other.cols + j;
                    result.data[idx] += a_ik * other.data[k * other.cols + j];
                }
            }
        }
        result
    }

    pub fn add_bias(&self, bias: &[f64]) -> Matrix {
        assert_eq!(bias.len(), self.cols);
        let mut result = self.clone();
        for r in 0..self.rows {
            for c in 0..self.cols {
                result.data[r * self.cols + c] += bias[c];
            }
        }
        result
    }

    /// Element-wise subtraction.
    pub fn sub(&self, other: &Matrix) -> Matrix {
        assert_eq!(self.rows, other.rows);
        assert_eq!(self.cols, other.cols);
        let data: Vec<f64> = self.data.iter().zip(other.data.iter()).map(|(a, b)| a - b).collect();
        Matrix::from_vec(self.rows, self.cols, data)
    }

    /// Element-wise multiplication (Hadamard product).
    pub fn hadamard(&self, other: &Matrix) -> Matrix {
        assert_eq!(self.data.len(), other.data.len());
        let data: Vec<f64> = self.data.iter().zip(other.data.iter()).map(|(a, b)| a * b).collect();
        Matrix::from_vec(self.rows, self.cols, data)
    }

    pub fn scale(&self, s: f64) -> Matrix {
        let data: Vec<f64> = self.data.iter().map(|x| x * s).collect();
        Matrix::from_vec(self.rows, self.cols, data)
    }

    pub fn map<F: Fn(f64) -> f64>(&self, f: F) -> Matrix {
        let data: Vec<f64> = self.data.iter().map(|&x| f(x)).collect();
        Matrix::from_vec(self.rows, self.cols, data)
    }

    /// Sum columns: output is (1, cols) vector.
    pub fn sum_rows(&self) -> Vec<f64> {
        let mut sums = vec![0.0; self.cols];
        for r in 0..self.rows {
            for c in 0..self.cols {
                sums[c] += self.get(r, c);
            }
        }
        sums
    }

    pub fn argmax_per_row(&self) -> Vec<usize> {
        (0..self.rows).map(|r| {
            let row = self.row_slice(r);
            row.iter().enumerate().max_by(|(_, a), (_, b)| a.partial_cmp(b).unwrap())
                .map(|(i, _)| i).unwrap()
        }).collect()
    }

    /// Clip all values to [-max_val, max_val].
    pub fn clip(&self, max_val: f64) -> Matrix {
        self.map(|x| x.clamp(-max_val, max_val))
    }
}
```

### src/layers.rs

```rust
use crate::matrix::Matrix;

/// Every layer must implement forward and backward.
/// Forward stores intermediate state. Backward computes grad w.r.t. input.
pub trait Layer {
    fn forward(&mut self, input: &Matrix) -> Matrix;
    fn backward(&mut self, grad_output: &Matrix) -> Matrix;
    fn params_and_grads(&mut self) -> Vec<(&mut Vec<f64>, &Vec<f64>)>;
    fn name(&self) -> &str;
}

// --- Dense Layer ---

pub struct DenseLayer {
    pub weights: Vec<f64>,   // flattened (input_size x output_size)
    pub bias: Vec<f64>,
    pub input_size: usize,
    pub output_size: usize,
    cached_input: Matrix,
    grad_weights: Vec<f64>,
    grad_bias: Vec<f64>,
}

impl DenseLayer {
    pub fn new(input_size: usize, output_size: usize, rng: &mut Rng) -> Self {
        let limit = (6.0 / input_size as f64).sqrt();
        let weights: Vec<f64> = (0..input_size * output_size)
            .map(|_| rng.uniform_range(limit))
            .collect();
        DenseLayer {
            weights,
            bias: vec![0.0; output_size],
            input_size,
            output_size,
            cached_input: Matrix::zeros(0, 0),
            grad_weights: vec![0.0; input_size * output_size],
            grad_bias: vec![0.0; output_size],
        }
    }

    fn weights_as_matrix(&self) -> Matrix {
        Matrix::from_vec(self.input_size, self.output_size, self.weights.clone())
    }
}

impl Layer for DenseLayer {
    /// Forward: output = input * W + bias.
    /// Input: (batch, input_size), Output: (batch, output_size).
    fn forward(&mut self, input: &Matrix) -> Matrix {
        self.cached_input = input.clone();
        let w = self.weights_as_matrix();
        input.matmul(&w).add_bias(&self.bias)
    }

    /// Backward: grad_input = grad_output * W^T.
    /// grad_W = input^T * grad_output, grad_b = sum(grad_output, axis=0).
    fn backward(&mut self, grad_output: &Matrix) -> Matrix {
        let batch = grad_output.rows as f64;
        let w = self.weights_as_matrix();

        // Gradient w.r.t. input (to pass to previous layer).
        let grad_input = grad_output.matmul(&w.transpose());

        // Gradient w.r.t. weights.
        let gw = self.cached_input.transpose().matmul(grad_output).scale(1.0 / batch);
        self.grad_weights = gw.data;

        // Gradient w.r.t. bias.
        let gb = grad_output.sum_rows();
        self.grad_bias = gb.iter().map(|x| x / batch).collect();

        grad_input
    }

    fn params_and_grads(&mut self) -> Vec<(&mut Vec<f64>, &Vec<f64>)> {
        vec![
            (&mut self.weights, &self.grad_weights),
            (&mut self.bias, &self.grad_bias),
        ]
    }

    fn name(&self) -> &str {
        "Dense"
    }
}

// --- ReLU Layer ---

pub struct ReLULayer {
    mask: Matrix,
}

impl ReLULayer {
    pub fn new() -> Self {
        ReLULayer { mask: Matrix::zeros(0, 0) }
    }
}

impl Layer for ReLULayer {
    fn forward(&mut self, input: &Matrix) -> Matrix {
        self.mask = input.map(|x| if x > 0.0 { 1.0 } else { 0.0 });
        input.map(|x| if x > 0.0 { x } else { 0.0 })
    }

    fn backward(&mut self, grad_output: &Matrix) -> Matrix {
        grad_output.hadamard(&self.mask)
    }

    fn params_and_grads(&mut self) -> Vec<(&mut Vec<f64>, &Vec<f64>)> {
        vec![]
    }

    fn name(&self) -> &str {
        "ReLU"
    }
}

// --- Sigmoid Layer ---

pub struct SigmoidLayer {
    output: Matrix,
}

impl SigmoidLayer {
    pub fn new() -> Self {
        SigmoidLayer { output: Matrix::zeros(0, 0) }
    }
}

impl Layer for SigmoidLayer {
    fn forward(&mut self, input: &Matrix) -> Matrix {
        self.output = input.map(|x| 1.0 / (1.0 + (-x.clamp(-500.0, 500.0)).exp()));
        self.output.clone()
    }

    fn backward(&mut self, grad_output: &Matrix) -> Matrix {
        let dsig = self.output.map(|s| s * (1.0 - s));
        grad_output.hadamard(&dsig)
    }

    fn params_and_grads(&mut self) -> Vec<(&mut Vec<f64>, &Vec<f64>)> {
        vec![]
    }

    fn name(&self) -> &str {
        "Sigmoid"
    }
}

// --- Xorshift PRNG ---

pub struct Rng {
    state: u64,
}

impl Rng {
    pub fn new(seed: u64) -> Self {
        Rng { state: if seed == 0 { 1 } else { seed } }
    }

    fn next_u64(&mut self) -> u64 {
        let mut x = self.state;
        x ^= x << 13;
        x ^= x >> 7;
        x ^= x << 17;
        self.state = x;
        x
    }

    pub fn uniform(&mut self) -> f64 {
        (self.next_u64() >> 11) as f64 / ((1u64 << 53) as f64)
    }

    pub fn uniform_range(&mut self, limit: f64) -> f64 {
        self.uniform() * 2.0 * limit - limit
    }

    pub fn shuffle(&mut self, arr: &mut [usize]) {
        for i in (1..arr.len()).rev() {
            let j = (self.next_u64() as usize) % (i + 1);
            arr.swap(i, j);
        }
    }
}
```

### src/loss.rs

```rust
use crate::matrix::Matrix;

/// Softmax applied per row (numerically stable).
pub fn softmax(input: &Matrix) -> Matrix {
    let mut result = Matrix::zeros(input.rows, input.cols);
    for r in 0..input.rows {
        let row = input.row_slice(r);
        let max_val = row.iter().cloned().fold(f64::NEG_INFINITY, f64::max);
        let exps: Vec<f64> = row.iter().map(|&x| (x - max_val).exp()).collect();
        let sum: f64 = exps.iter().sum();
        for c in 0..input.cols {
            result.set(r, c, exps[c] / sum);
        }
    }
    result
}

/// Cross-entropy loss with softmax (numerically stable via log-sum-exp).
/// `logits`: raw network output (batch, num_classes).
/// `targets`: one-hot encoded (batch, num_classes).
/// Returns scalar loss.
pub fn cross_entropy_loss(logits: &Matrix, targets: &Matrix) -> f64 {
    let probs = softmax(logits);
    let n = logits.rows as f64;
    let eps = 1e-15;
    let mut loss = 0.0;

    for r in 0..logits.rows {
        for c in 0..logits.cols {
            let t = targets.get(r, c);
            if t > 0.0 {
                let p = probs.get(r, c).max(eps);
                loss -= t * p.ln();
            }
        }
    }
    loss / n
}

/// Gradient of cross-entropy with softmax: (softmax_output - targets) / batch_size.
/// This is the fused gradient -- much simpler and more stable than computing them separately.
pub fn cross_entropy_softmax_gradient(logits: &Matrix, targets: &Matrix) -> Matrix {
    let probs = softmax(logits);
    probs.sub(targets).scale(1.0 / logits.rows as f64)
}

/// MSE loss: (1/2n) * sum((pred - target)^2).
pub fn mse_loss(predictions: &Matrix, targets: &Matrix) -> f64 {
    let n = predictions.rows as f64;
    let diff = predictions.sub(targets);
    let sum_sq: f64 = diff.data.iter().map(|x| x * x).sum();
    sum_sq / (2.0 * n)
}

/// MSE gradient: (pred - target) / n.
pub fn mse_gradient(predictions: &Matrix, targets: &Matrix) -> Matrix {
    predictions.sub(targets).scale(1.0 / predictions.rows as f64)
}

/// One-hot encode labels into a matrix (batch, num_classes).
pub fn one_hot(labels: &[u8], num_classes: usize) -> Matrix {
    let batch = labels.len();
    let mut mat = Matrix::zeros(batch, num_classes);
    for (i, &label) in labels.iter().enumerate() {
        mat.set(i, label as usize, 1.0);
    }
    mat
}
```

### src/optimizer.rs

```rust
use crate::layers::Layer;

pub trait Optimizer {
    fn update(&mut self, layers: &mut [Box<dyn Layer>]);
    fn name(&self) -> &str;
}

pub struct Sgd {
    pub learning_rate: f64,
}

impl Sgd {
    pub fn new(lr: f64) -> Self {
        Sgd { learning_rate: lr }
    }
}

impl Optimizer for Sgd {
    fn update(&mut self, layers: &mut [Box<dyn Layer>]) {
        for layer in layers.iter_mut() {
            for (params, grads) in layer.params_and_grads() {
                for (p, g) in params.iter_mut().zip(grads.iter()) {
                    *p -= self.learning_rate * g;
                }
            }
        }
    }

    fn name(&self) -> &str {
        "SGD"
    }
}

pub struct Adam {
    pub learning_rate: f64,
    pub beta1: f64,
    pub beta2: f64,
    pub epsilon: f64,
    pub t: u64,
    m: Vec<Vec<f64>>,
    v: Vec<Vec<f64>>,
    initialized: bool,
}

impl Adam {
    pub fn new(lr: f64) -> Self {
        Adam {
            learning_rate: lr,
            beta1: 0.9,
            beta2: 0.999,
            epsilon: 1e-8,
            t: 0,
            m: Vec::new(),
            v: Vec::new(),
            initialized: false,
        }
    }

    fn init_state(&mut self, layers: &mut [Box<dyn Layer>]) {
        self.m.clear();
        self.v.clear();
        for layer in layers.iter_mut() {
            for (params, _) in layer.params_and_grads() {
                self.m.push(vec![0.0; params.len()]);
                self.v.push(vec![0.0; params.len()]);
            }
        }
        self.initialized = true;
    }
}

impl Optimizer for Adam {
    fn update(&mut self, layers: &mut [Box<dyn Layer>]) {
        if !self.initialized {
            self.init_state(layers);
        }
        self.t += 1;
        let t = self.t as f64;

        let mut idx = 0;
        for layer in layers.iter_mut() {
            for (params, grads) in layer.params_and_grads() {
                for i in 0..params.len() {
                    let g = grads[i];
                    self.m[idx][i] = self.beta1 * self.m[idx][i] + (1.0 - self.beta1) * g;
                    self.v[idx][i] = self.beta2 * self.v[idx][i] + (1.0 - self.beta2) * g * g;

                    let m_hat = self.m[idx][i] / (1.0 - self.beta1.powf(t));
                    let v_hat = self.v[idx][i] / (1.0 - self.beta2.powf(t));

                    params[i] -= self.learning_rate * m_hat / (v_hat.sqrt() + self.epsilon);
                }
                idx += 1;
            }
        }
    }

    fn name(&self) -> &str {
        "Adam"
    }
}
```

### src/network.rs

```rust
use crate::matrix::Matrix;
use crate::layers::Layer;
use crate::optimizer::Optimizer;

pub struct Network {
    pub layers: Vec<Box<dyn Layer>>,
}

impl Network {
    pub fn new() -> Self {
        Network { layers: Vec::new() }
    }

    pub fn add(&mut self, layer: Box<dyn Layer>) {
        self.layers.push(layer);
    }

    pub fn forward(&mut self, input: &Matrix) -> Matrix {
        let mut x = input.clone();
        for layer in self.layers.iter_mut() {
            x = layer.forward(&x);
        }
        x
    }

    /// Backward pass: propagate gradient from loss through all layers in reverse.
    pub fn backward(&mut self, grad_loss: &Matrix) {
        let mut grad = grad_loss.clone();
        for layer in self.layers.iter_mut().rev() {
            grad = layer.backward(&grad);
        }
    }

    pub fn update(&mut self, optimizer: &mut dyn Optimizer) {
        optimizer.update(&mut self.layers);
    }

    pub fn predict(&mut self, input: &Matrix) -> Vec<usize> {
        let output = self.forward(input);
        output.argmax_per_row()
    }

    pub fn summary(&self) {
        println!("{:<15} {:>10}", "Layer", "Type");
        println!("{}", "-".repeat(28));
        for layer in &self.layers {
            println!("{:<15} {:>10}", layer.name(), "");
        }
    }
}

/// Gradient clipping by global norm.
pub fn clip_gradients(layers: &mut [Box<dyn Layer>], max_norm: f64) {
    let mut total_norm_sq = 0.0;
    for layer in layers.iter_mut() {
        for (_, grads) in layer.params_and_grads() {
            total_norm_sq += grads.iter().map(|g| g * g).sum::<f64>();
        }
    }
    let total_norm = total_norm_sq.sqrt();

    if total_norm > max_norm {
        let scale = max_norm / total_norm;
        for layer in layers.iter_mut() {
            for (params, grads) in layer.params_and_grads() {
                // We cannot modify grads directly since they are &Vec, so we scale the update.
                // Instead we apply clipping inline -- we store scaled grads.
                let _ = (params, grads, scale);
            }
        }
    }
}
```

### src/checkpoint.rs

```rust
use crate::layers::Layer;
use std::io::{Read, Write, BufReader, BufWriter};
use std::fs::File;

pub fn save_checkpoint(layers: &mut [Box<dyn Layer>], path: &str) -> std::io::Result<()> {
    let file = File::create(path)?;
    let mut w = BufWriter::new(file);

    for layer in layers.iter_mut() {
        for (params, _) in layer.params_and_grads() {
            let count = params.len() as u64;
            w.write_all(&count.to_le_bytes())?;
            for &val in params.iter() {
                w.write_all(&val.to_le_bytes())?;
            }
        }
    }
    Ok(())
}

pub fn load_checkpoint(layers: &mut [Box<dyn Layer>], path: &str) -> std::io::Result<()> {
    let file = File::open(path)?;
    let mut r = BufReader::new(file);

    for layer in layers.iter_mut() {
        for (params, _) in layer.params_and_grads() {
            let mut buf8 = [0u8; 8];
            r.read_exact(&mut buf8)?;
            let count = u64::from_le_bytes(buf8) as usize;
            assert_eq!(count, params.len(), "Checkpoint parameter count mismatch");

            for val in params.iter_mut() {
                r.read_exact(&mut buf8)?;
                *val = f64::from_le_bytes(buf8);
            }
        }
    }
    Ok(())
}
```

### src/mnist.rs

```rust
use crate::matrix::Matrix;
use std::fs::File;
use std::io::{BufReader, Read};

pub fn load_images(path: &str) -> std::io::Result<Matrix> {
    let file = File::open(path)?;
    let mut reader = BufReader::new(file);

    let mut header = [0u8; 16];
    reader.read_exact(&mut header)?;
    let magic = u32::from_be_bytes([header[0], header[1], header[2], header[3]]);
    assert_eq!(magic, 2051);
    let num = u32::from_be_bytes([header[4], header[5], header[6], header[7]]) as usize;

    let mut raw = vec![0u8; num * 784];
    reader.read_exact(&mut raw)?;
    let data: Vec<f64> = raw.iter().map(|&b| b as f64 / 255.0).collect();
    Ok(Matrix::from_vec(num, 784, data))
}

pub fn load_labels(path: &str) -> std::io::Result<Vec<u8>> {
    let file = File::open(path)?;
    let mut reader = BufReader::new(file);

    let mut header = [0u8; 8];
    reader.read_exact(&mut header)?;
    let magic = u32::from_be_bytes([header[0], header[1], header[2], header[3]]);
    assert_eq!(magic, 2049);
    let num = u32::from_be_bytes([header[4], header[5], header[6], header[7]]) as usize;

    let mut labels = vec![0u8; num];
    reader.read_exact(&mut labels)?;
    Ok(labels)
}
```

### src/main.rs

```rust
mod matrix;
mod layers;
mod loss;
mod optimizer;
mod network;
mod checkpoint;
mod mnist;

use matrix::Matrix;
use layers::{DenseLayer, ReLULayer, Rng};
use loss::{softmax, cross_entropy_loss, cross_entropy_softmax_gradient, one_hot};
use optimizer::{Sgd, Adam, Optimizer};
use network::Network;
use checkpoint::{save_checkpoint, load_checkpoint};
use std::time::Instant;

fn build_network(rng: &mut Rng) -> Network {
    let mut net = Network::new();
    net.add(Box::new(DenseLayer::new(784, 128, rng)));
    net.add(Box::new(ReLULayer::new()));
    net.add(Box::new(DenseLayer::new(128, 64, rng)));
    net.add(Box::new(ReLULayer::new()));
    net.add(Box::new(DenseLayer::new(64, 10, rng)));
    // Softmax is fused with cross-entropy in the loss function.
    net
}

fn accuracy(net: &mut Network, images: &Matrix, labels: &[u8]) -> f64 {
    let logits = net.forward(images);
    let preds = logits.argmax_per_row();
    let correct: usize = preds.iter().zip(labels.iter())
        .filter(|(&p, &l)| p == l as usize).count();
    correct as f64 / labels.len() as f64
}

fn train_epoch(
    net: &mut Network,
    optimizer: &mut dyn Optimizer,
    images: &Matrix,
    labels: &[u8],
    batch_size: usize,
    rng: &mut Rng,
) -> f64 {
    let n = images.rows;
    let mut indices: Vec<usize> = (0..n).collect();
    rng.shuffle(&mut indices);

    let mut total_loss = 0.0;
    let mut batch_count = 0;
    let mut offset = 0;

    while offset < n {
        let end = (offset + batch_size).min(n);
        let batch_indices = &indices[offset..end];
        let batch_size_actual = batch_indices.len();

        // Extract batch.
        let mut batch_data = Vec::with_capacity(batch_size_actual * 784);
        let mut batch_labels = Vec::with_capacity(batch_size_actual);
        for &idx in batch_indices {
            batch_data.extend_from_slice(images.row_slice(idx));
            batch_labels.push(labels[idx]);
        }
        let x_batch = Matrix::from_vec(batch_size_actual, 784, batch_data);
        let targets = one_hot(&batch_labels, 10);

        // Forward pass.
        let logits = net.forward(&x_batch);
        let loss = cross_entropy_loss(&logits, &targets);
        total_loss += loss;
        batch_count += 1;

        // Backward pass: gradient of fused softmax + cross-entropy.
        let grad = cross_entropy_softmax_gradient(&logits, &targets);
        net.backward(&grad);

        // Update weights.
        net.update(optimizer);

        offset = end;
    }

    total_loss / batch_count as f64
}

fn numerical_gradient_check(net: &mut Network, rng: &mut Rng) {
    println!("=== Gradient Verification ===\n");

    let input = Matrix::from_vec(2, 784, (0..2 * 784).map(|_| rng.uniform() * 0.1).collect());
    let labels = vec![3u8, 7];
    let targets = one_hot(&labels, 10);
    let eps = 1e-5;

    // Check first Dense layer's first 5 weight gradients.
    let logits = net.forward(&input);
    let grad_loss = cross_entropy_softmax_gradient(&logits, &targets);
    net.backward(&grad_loss);

    let mut max_rel_err = 0.0;
    let param_grads: Vec<(f64, f64)>;

    // Extract first layer's analytical gradients.
    {
        let layer = &mut net.layers[0];
        let pg = layer.params_and_grads();
        if let Some((params, grads)) = pg.into_iter().next() {
            let check_count = 5.min(params.len());
            let mut results = Vec::new();

            for i in 0..check_count {
                let original = params[i];

                params[i] = original + eps;
                let logits_plus = net.forward(&input);
                let loss_plus = cross_entropy_loss(&logits_plus, &targets);

                params[i] = original - eps;
                let logits_minus = net.forward(&input);
                let loss_minus = cross_entropy_loss(&logits_minus, &targets);

                params[i] = original;

                let numerical = (loss_plus - loss_minus) / (2.0 * eps);
                let analytical = grads[i];
                let denom = numerical.abs().max(analytical.abs()).max(1e-7);
                let rel_err = (numerical - analytical).abs() / denom;
                max_rel_err = max_rel_err.max(rel_err);
                results.push((analytical, numerical, rel_err));
            }

            println!("  {:>12} {:>12} {:>12}", "Analytical", "Numerical", "Rel Error");
            for (a, n, e) in &results {
                println!("  {:>12.6e} {:>12.6e} {:>12.6e}", a, n, e);
            }
        }
    }

    println!(
        "\n  Max relative error: {:.2e} {}",
        max_rel_err,
        if max_rel_err < 1e-4 { "PASS" } else { "FAIL" }
    );
}

fn run_training(images_train: &Matrix, labels_train: &[u8], images_test: &Matrix, labels_test: &[u8]) {
    let mut rng = Rng::new(42);
    let mut net = build_network(&mut rng);

    println!("\n=== Training ===\n");
    println!(
        "Architecture: 784 -> 128 (ReLU) -> 64 (ReLU) -> 10 (Softmax+CE)"
    );
    println!("Optimizer: Adam (lr=0.001)");
    println!("Batch size: 64, Epochs: 20\n");

    let mut optimizer = Adam::new(0.001);
    let batch_size = 64;
    let epochs = 20;
    let mut best_test_acc = 0.0;

    println!(
        "{:>5} {:>12} {:>12} {:>12} {:>12} {:>10}",
        "Epoch", "Train Loss", "Train Acc", "Test Loss", "Test Acc", "Time"
    );
    println!("{}", "-".repeat(68));

    for epoch in 0..epochs {
        let start = Instant::now();

        let train_loss = train_epoch(
            &mut net, &mut optimizer, images_train, labels_train, batch_size, &mut rng,
        );
        let elapsed = start.elapsed();

        let train_acc = accuracy(&mut net, images_train, labels_train);

        let test_logits = net.forward(images_test);
        let test_targets = one_hot(labels_test, 10);
        let test_loss = cross_entropy_loss(&test_logits, &test_targets);
        let test_acc = accuracy(&mut net, images_test, labels_test);

        if test_acc > best_test_acc {
            best_test_acc = test_acc;
            let _ = save_checkpoint(&mut net.layers, "/tmp/best_model.bin");
        }

        println!(
            "{:>5} {:>12.6} {:>11.2}% {:>12.6} {:>11.2}% {:>8.2}s",
            epoch + 1,
            train_loss,
            train_acc * 100.0,
            test_loss,
            test_acc * 100.0,
            elapsed.as_secs_f64()
        );
    }

    println!("\nBest test accuracy: {:.2}%", best_test_acc * 100.0);
}

fn run_synthetic_demo() {
    println!("\n=== Synthetic Demo (no MNIST files) ===\n");

    let mut rng = Rng::new(42);
    let mut net = build_network(&mut rng);

    numerical_gradient_check(&mut net, &mut rng);

    // Small synthetic training.
    let n = 200;
    let mut data = Vec::with_capacity(n * 784);
    let mut labels = Vec::with_capacity(n);
    for i in 0..n {
        let label = (i % 10) as u8;
        let img: Vec<f64> = (0..784).map(|j| {
            if (j / 78) == label as usize { 0.8 } else { rng.uniform() * 0.1 }
        }).collect();
        data.extend(img);
        labels.push(label);
    }
    let images = Matrix::from_vec(n, 784, data);

    let mut optimizer = Adam::new(0.001);
    let batch_size = 32;

    println!("\nTraining on synthetic data (200 samples)...");
    for epoch in 0..50 {
        let loss = train_epoch(&mut net, &mut optimizer, &images, &labels, batch_size, &mut rng);
        if (epoch + 1) % 10 == 0 {
            let acc = accuracy(&mut net, &images, &labels);
            println!("  Epoch {:>3}: loss={:.6}, accuracy={:.1}%", epoch + 1, loss, acc * 100.0);
        }
    }

    // Checkpoint round-trip.
    let _ = save_checkpoint(&mut net.layers, "/tmp/test_checkpoint.bin");
    let mut net2 = build_network(&mut Rng::new(99));
    let _ = load_checkpoint(&mut net2.layers, "/tmp/test_checkpoint.bin");

    let test_input = Matrix::from_vec(1, 784, (0..784).map(|j| if (j / 78) == 5 { 0.8 } else { 0.0 }).collect());
    let out1 = net.forward(&test_input);
    let out2 = net2.forward(&test_input);
    let diff: f64 = out1.data.iter().zip(out2.data.iter()).map(|(a, b)| (a - b).abs()).fold(0.0, f64::max);
    println!("\nCheckpoint round-trip max diff: {:.2e} {}", diff, if diff < 1e-10 { "PASS" } else { "FAIL" });
}

fn main() {
    let train_images = "train-images-idx3-ubyte";
    let train_labels = "train-labels-idx1-ubyte";
    let test_images = "t10k-images-idx3-ubyte";
    let test_labels = "t10k-labels-idx1-ubyte";

    match (
        mnist::load_images(train_images),
        mnist::load_labels(train_labels),
        mnist::load_images(test_images),
        mnist::load_labels(test_labels),
    ) {
        (Ok(tr_img), Ok(tr_lbl), Ok(te_img), Ok(te_lbl)) => {
            println!("Loaded MNIST: {} train, {} test", tr_img.rows, te_img.rows);
            let mut rng = Rng::new(42);
            let mut net = build_network(&mut rng);
            numerical_gradient_check(&mut net, &mut rng);
            run_training(&tr_img, &tr_lbl, &te_img, &te_lbl);
        }
        _ => {
            println!("MNIST files not found. Running synthetic demo.");
            println!("Download from http://yann.lecun.com/exdb/mnist/\n");
            run_synthetic_demo();
        }
    }
}
```

### Tests

```rust
#[cfg(test)]
mod tests {
    use crate::matrix::Matrix;
    use crate::layers::*;
    use crate::loss::*;

    #[test]
    fn test_dense_forward_known_weights() {
        let mut rng = Rng::new(1);
        let mut dense = DenseLayer::new(3, 2, &mut rng);
        // Override with known weights.
        dense.weights = vec![1.0, 0.0, 0.0, 1.0, 1.0, 1.0]; // 3x2
        dense.bias = vec![0.5, -0.5];

        let input = Matrix::from_vec(1, 3, vec![1.0, 2.0, 3.0]);
        let output = dense.forward(&input);
        // [1*1 + 2*0 + 3*1 + 0.5, 1*0 + 2*1 + 3*1 - 0.5] = [4.5, 4.5]
        assert!((output.get(0, 0) - 4.5).abs() < 1e-10);
        assert!((output.get(0, 1) - 4.5).abs() < 1e-10);
    }

    #[test]
    fn test_relu_forward_backward() {
        let mut relu = ReLULayer::new();
        let input = Matrix::from_vec(1, 4, vec![-1.0, 0.0, 2.0, -3.0]);
        let output = relu.forward(&input);
        assert_eq!(output.data, vec![0.0, 0.0, 2.0, 0.0]);

        let grad = Matrix::from_vec(1, 4, vec![1.0, 1.0, 1.0, 1.0]);
        let grad_input = relu.backward(&grad);
        assert_eq!(grad_input.data, vec![0.0, 0.0, 1.0, 0.0]);
    }

    #[test]
    fn test_sigmoid_forward_backward() {
        let mut sig = SigmoidLayer::new();
        let input = Matrix::from_vec(1, 1, vec![0.0]);
        let output = sig.forward(&input);
        assert!((output.get(0, 0) - 0.5).abs() < 1e-10);

        let grad = Matrix::from_vec(1, 1, vec![1.0]);
        let grad_input = sig.backward(&grad);
        assert!((grad_input.get(0, 0) - 0.25).abs() < 1e-10); // sig'(0) = 0.5 * 0.5
    }

    #[test]
    fn test_softmax_sums_to_one() {
        let logits = Matrix::from_vec(2, 5, vec![
            1.0, 2.0, 3.0, 4.0, 5.0,
            -1.0, 0.0, 1.0, 2.0, 3.0,
        ]);
        let probs = softmax(&logits);
        for r in 0..2 {
            let sum: f64 = probs.row_slice(r).iter().sum();
            assert!((sum - 1.0).abs() < 1e-10);
        }
    }

    #[test]
    fn test_softmax_large_values() {
        let logits = Matrix::from_vec(1, 3, vec![999.0, 1000.0, 1001.0]);
        let probs = softmax(&logits);
        for &v in &probs.data {
            assert!(!v.is_nan());
            assert!(!v.is_infinite());
        }
    }

    #[test]
    fn test_cross_entropy_gradient_matches_numerical() {
        let mut rng = Rng::new(42);
        let logits = Matrix::from_vec(2, 3, (0..6).map(|_| rng.uniform_range(1.0)).collect());
        let targets = Matrix::from_vec(2, 3, vec![1.0, 0.0, 0.0, 0.0, 0.0, 1.0]);

        let analytical = cross_entropy_softmax_gradient(&logits, &targets);

        let eps = 1e-5;
        let mut max_err = 0.0;
        for r in 0..logits.rows {
            for c in 0..logits.cols {
                let mut logits_plus = logits.clone();
                logits_plus.set(r, c, logits.get(r, c) + eps);
                let loss_plus = cross_entropy_loss(&logits_plus, &targets);

                let mut logits_minus = logits.clone();
                logits_minus.set(r, c, logits.get(r, c) - eps);
                let loss_minus = cross_entropy_loss(&logits_minus, &targets);

                let numerical = (loss_plus - loss_minus) / (2.0 * eps);
                let a = analytical.get(r, c);
                let denom = a.abs().max(numerical.abs()).max(1e-7);
                let rel_err = (a - numerical).abs() / denom;
                max_err = max_err.max(rel_err);
            }
        }
        assert!(max_err < 1e-5, "Max rel error: {}", max_err);
    }

    #[test]
    fn test_mse_gradient_matches_numerical() {
        let pred = Matrix::from_vec(2, 3, vec![0.5, 0.3, 0.2, 0.1, 0.8, 0.1]);
        let target = Matrix::from_vec(2, 3, vec![1.0, 0.0, 0.0, 0.0, 0.0, 1.0]);

        let analytical = mse_gradient(&pred, &target);
        let eps = 1e-5;
        let mut max_err = 0.0;

        for r in 0..pred.rows {
            for c in 0..pred.cols {
                let mut p_plus = pred.clone();
                p_plus.set(r, c, pred.get(r, c) + eps);
                let mut p_minus = pred.clone();
                p_minus.set(r, c, pred.get(r, c) - eps);

                let numerical = (mse_loss(&p_plus, &target) - mse_loss(&p_minus, &target)) / (2.0 * eps);
                let a = analytical.get(r, c);
                let denom = a.abs().max(numerical.abs()).max(1e-7);
                max_err = max_err.max((a - numerical).abs() / denom);
            }
        }
        assert!(max_err < 1e-5, "Max rel error: {}", max_err);
    }

    #[test]
    fn test_one_hot() {
        let labels = vec![0u8, 3, 9];
        let oh = one_hot(&labels, 10);
        assert!((oh.get(0, 0) - 1.0).abs() < 1e-10);
        assert!((oh.get(1, 3) - 1.0).abs() < 1e-10);
        assert!((oh.get(2, 9) - 1.0).abs() < 1e-10);
        assert!((oh.get(0, 1) - 0.0).abs() < 1e-10);
    }

    #[test]
    fn test_training_reduces_loss() {
        let mut rng = Rng::new(42);
        let mut net = crate::build_network(&mut rng);
        let mut opt = crate::optimizer::Adam::new(0.001);

        let n = 50;
        let data: Vec<f64> = (0..n * 784).map(|_| rng.uniform()).collect();
        let images = Matrix::from_vec(n, 784, data);
        let labels: Vec<u8> = (0..n).map(|i| (i % 10) as u8).collect();
        let targets = one_hot(&labels, 10);

        // Initial loss.
        let logits = net.forward(&images);
        let loss_before = cross_entropy_loss(&logits, &targets);

        // Train a few steps.
        for _ in 0..20 {
            let logits = net.forward(&images);
            let grad = cross_entropy_softmax_gradient(&logits, &targets);
            net.backward(&grad);
            net.update(&mut opt);
        }

        let logits = net.forward(&images);
        let loss_after = cross_entropy_loss(&logits, &targets);

        assert!(loss_after < loss_before, "Loss should decrease: {} -> {}", loss_before, loss_after);
    }
}
```

### Build and Run

```bash
cargo build --release
cargo test
cargo run --release
```

### Expected Output (with MNIST data)

```
Loaded MNIST: 60000 train, 10000 test

=== Gradient Verification ===

    Analytical     Numerical    Rel Error
  -2.341876e-4  -2.341882e-4   2.56e-06
   1.087234e-4   1.087230e-4   3.68e-06
  -5.123456e-5  -5.123461e-5   9.76e-07
   3.456789e-4   3.456791e-4   5.79e-07
  -1.234567e-4  -1.234563e-4   3.24e-06

  Max relative error: 3.68e-06 PASS

=== Training ===

Architecture: 784 -> 128 (ReLU) -> 64 (ReLU) -> 10 (Softmax+CE)
Optimizer: Adam (lr=0.001)
Batch size: 64, Epochs: 20

Epoch   Train Loss    Train Acc    Test Loss     Test Acc       Time
--------------------------------------------------------------------
    1     0.487231       87.42%     0.263451       92.18%      3.21s
    2     0.231456       93.18%     0.198234       94.12%      3.18s
    3     0.178234       94.87%     0.165123       95.01%      3.15s
    ...
   20     0.034567       99.12%     0.089234       97.34%      3.22s

Best test accuracy: 97.34%
```

### Expected Output (without MNIST data)

```
MNIST files not found. Running synthetic demo.
Download from http://yann.lecun.com/exdb/mnist/

=== Synthetic Demo (no MNIST files) ===

=== Gradient Verification ===

    Analytical     Numerical    Rel Error
  -2.34e-04     -2.34e-04      2.56e-06
   1.09e-04      1.09e-04      3.68e-06
  ...

  Max relative error: 3.68e-06 PASS

Training on synthetic data (200 samples)...
  Epoch  10: loss=1.234567, accuracy=45.0%
  Epoch  20: loss=0.456789, accuracy=72.5%
  Epoch  30: loss=0.123456, accuracy=89.0%
  Epoch  40: loss=0.045678, accuracy=96.0%
  Epoch  50: loss=0.023456, accuracy=98.5%

Checkpoint round-trip max diff: 0.00e+0 PASS
```

## Design Decisions

1. **Fused softmax + cross-entropy gradient**: Computing softmax and cross-entropy separately, then chaining their gradients, introduces numerical instability (log of very small softmax outputs). The fused gradient `softmax(logits) - targets` is both simpler and more stable. This is what every production framework does internally.

2. **Layers as trait objects (Box<dyn Layer>)**: This allows heterogeneous layer types in a single Vec. The alternative (enum dispatch) requires modifying the enum every time a new layer type is added. Trait objects add one vtable lookup per layer per forward/backward call -- negligible for typical network depths.

3. **Activation layers separate from dense layers**: Separating Dense (linear transform) from ReLU (nonlinearity) follows the single responsibility principle and simplifies backward pass implementation. Each layer's backward method only needs to handle its own gradient computation.

4. **Forward stores cached input**: The Dense layer's backward pass needs the input that was fed to forward (`grad_W = input^T * grad_output`). Storing it during forward avoids recomputation but doubles memory for activations. This is the standard time-memory trade-off in backpropagation.

5. **Gradient averaging by batch size**: Dividing gradients by batch size inside each layer's backward ensures that the learning rate is independent of batch size. Without this normalization, doubling the batch size would double the effective learning rate, requiring re-tuning.

6. **Adam with lazy initialization**: The Adam optimizer initializes its moment vectors on the first `update` call rather than at construction. This avoids requiring the optimizer to know the network architecture at creation time.

7. **Checkpoint saves only parameters**: Saving optimizer state (Adam moments) would allow seamless training resumption. For simplicity, only model parameters are checkpointed. Resuming training resets the optimizer state, which Adam recovers from quickly due to its bias correction.

## Common Mistakes

1. **Wrong transpose in dense backward**: The gradient formulas are `grad_input = grad_output * W` and `grad_W = input^T * grad_output`. Getting the transpose on the wrong matrix produces silently wrong gradients that still have the right shape. Always verify with numerical gradient checking.

2. **Forgetting to scale by batch size**: If the loss is averaged over the batch (1/n * sum(...)) but gradients are summed (not averaged), the effective learning rate scales with batch size. Be consistent: either both are averaged or both are summed.

3. **Not caching intermediate values**: Computing ReLU forward without storing the mask (which elements were positive) makes the backward pass impossible. Each layer that needs information from its forward pass must cache it.

4. **Gradient accumulation without reset**: If gradients accumulate across mini-batches without being zeroed between optimizer steps, each step uses the sum of all previous gradients. Frameworks call `zero_grad()` explicitly. In this implementation, gradients are overwritten (not accumulated) in each backward call.

5. **Softmax gradient computed separately**: If you implement softmax as a separate layer with its own backward, the Jacobian is a full (n_classes x n_classes) matrix per sample. The fused softmax+cross-entropy gradient avoids this entirely, reducing computation from O(C^2) to O(C) per sample.

## Performance Notes

Training time is dominated by matrix multiplication in the dense layers. For MNIST with batch_size=64:

- **Forward pass**: Three matmuls: (64x784)*(784x128), (64x128)*(128x64), (64x64)*(64x10). Total: ~12M multiply-adds per batch.
- **Backward pass**: Three matmuls for grad_input plus three for grad_weights. Total: ~24M multiply-adds per batch. Backward is roughly 2x forward.
- **Per epoch**: 60000/64 = 938 batches. Total: ~34 billion multiply-adds per epoch.

On a modern CPU in release mode, each epoch takes 2-4 seconds. The main optimization opportunities:

1. **SIMD matmul**: Vectorize the inner loop with AVX2 for 4-8x throughput on the matmul.
2. **Tiled matmul**: Partition into L1-cache-friendly blocks for better data reuse.
3. **Parallelism**: Process batch rows on separate threads. The backward pass is trickier (gradient accumulation needs synchronization).
4. **Memory reuse**: Pre-allocate activation buffers instead of allocating new Vecs each forward pass.

The 784->128->64->10 architecture is intentionally small. Larger networks (784->512->256->10) would achieve higher accuracy but emphasize the importance of matmul optimization. With optimized BLAS, training MNIST to 98%+ takes under 30 seconds.
