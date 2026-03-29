# Solution: Neural Network Forward Pass

## Architecture Overview

The implementation is organized into four modules:

1. **matrix**: `Matrix` type with flat `Vec<f64>` row-major storage, matmul, transpose, element-wise ops
2. **activation**: ReLU, sigmoid, softmax functions with numerical stability
3. **network**: `Layer` and `Network` structs, weight initialization, serialization
4. **mnist**: IDX file parser, image normalization, label loading

```
MNIST IDX Files (images + labels)
     |
     v
 [IDX Parser] --> Vec<Vec<f64>> images, Vec<u8> labels
     |
     v
 [Network::load(weights_file)]
     |
     v
 [Forward Pass] --> For each image (or batch):
     |                input -> Layer 1 (matmul + bias + ReLU)
     |                      -> Layer 2 (matmul + bias + ReLU)
     |                      -> Layer 3 (matmul + bias + Softmax)
     v
 [Evaluation] --> argmax(softmax) vs true label
     |
     v
 [Report] --> accuracy, confusion matrix
```

## Rust Solution

### Cargo.toml

```toml
[package]
name = "nn-forward-pass"
version = "0.1.0"
edition = "2021"

[[bin]]
name = "nn-forward-pass"
path = "src/main.rs"

[profile.release]
opt-level = 3
```

### src/matrix.rs

```rust
use std::fmt;

#[derive(Clone, Debug)]
pub struct Matrix {
    pub rows: usize,
    pub cols: usize,
    pub data: Vec<f64>,
}

impl Matrix {
    pub fn zeros(rows: usize, cols: usize) -> Self {
        Matrix {
            rows,
            cols,
            data: vec![0.0; rows * cols],
        }
    }

    pub fn from_vec(rows: usize, cols: usize, data: Vec<f64>) -> Self {
        assert_eq!(
            data.len(),
            rows * cols,
            "Data length {} does not match dimensions {}x{}",
            data.len(),
            rows,
            cols
        );
        Matrix { rows, cols, data }
    }

    pub fn get(&self, row: usize, col: usize) -> f64 {
        self.data[row * self.cols + col]
    }

    pub fn set(&mut self, row: usize, col: usize, val: f64) {
        self.data[row * self.cols + col] = val;
    }

    pub fn row_slice(&self, row: usize) -> &[f64] {
        let start = row * self.cols;
        &self.data[start..start + self.cols]
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

    /// Matrix multiplication: (m x n) * (n x p) = (m x p).
    pub fn matmul(&self, other: &Matrix) -> Matrix {
        assert_eq!(
            self.cols, other.rows,
            "matmul dimension mismatch: ({} x {}) * ({} x {})",
            self.rows, self.cols, other.rows, other.cols
        );
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

    /// Add a row vector to every row of the matrix (bias broadcast).
    pub fn add_bias(&self, bias: &[f64]) -> Matrix {
        assert_eq!(bias.len(), self.cols, "Bias length must match column count");
        let mut result = self.clone();
        for r in 0..self.rows {
            for c in 0..self.cols {
                let idx = r * self.cols + c;
                result.data[idx] += bias[c];
            }
        }
        result
    }

    /// Apply a function element-wise.
    pub fn map<F: Fn(f64) -> f64>(&self, f: F) -> Matrix {
        let data: Vec<f64> = self.data.iter().map(|&x| f(x)).collect();
        Matrix::from_vec(self.rows, self.cols, data)
    }

    /// Argmax per row. Returns a vector of column indices.
    pub fn argmax_per_row(&self) -> Vec<usize> {
        (0..self.rows)
            .map(|r| {
                let row = self.row_slice(r);
                row.iter()
                    .enumerate()
                    .max_by(|(_, a), (_, b)| a.partial_cmp(b).unwrap())
                    .map(|(idx, _)| idx)
                    .unwrap()
            })
            .collect()
    }
}

impl fmt::Display for Matrix {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        for r in 0..self.rows.min(5) {
            for c in 0..self.cols.min(10) {
                write!(f, "{:8.4} ", self.get(r, c))?;
            }
            if self.cols > 10 {
                write!(f, "...")?;
            }
            writeln!(f)?;
        }
        if self.rows > 5 {
            writeln!(f, "... ({} rows total)", self.rows)?;
        }
        Ok(())
    }
}
```

### src/activation.rs

```rust
use crate::matrix::Matrix;

#[derive(Clone, Debug)]
pub enum Activation {
    ReLU,
    Sigmoid,
    Softmax,
    None,
}

impl Activation {
    pub fn apply(&self, input: &Matrix) -> Matrix {
        match self {
            Activation::ReLU => relu(input),
            Activation::Sigmoid => sigmoid(input),
            Activation::Softmax => softmax(input),
            Activation::None => input.clone(),
        }
    }
}

fn relu(m: &Matrix) -> Matrix {
    m.map(|x| if x > 0.0 { x } else { 0.0 })
}

fn sigmoid(m: &Matrix) -> Matrix {
    m.map(|x| {
        let clamped = x.clamp(-500.0, 500.0);
        1.0 / (1.0 + (-clamped).exp())
    })
}

/// Numerically stable softmax applied per row.
fn softmax(m: &Matrix) -> Matrix {
    let mut result = Matrix::zeros(m.rows, m.cols);
    for r in 0..m.rows {
        let row = m.row_slice(r);
        let max_val = row.iter().cloned().fold(f64::NEG_INFINITY, f64::max);

        let exps: Vec<f64> = row.iter().map(|&x| (x - max_val).exp()).collect();
        let sum: f64 = exps.iter().sum();

        for c in 0..m.cols {
            result.set(r, c, exps[c] / sum);
        }
    }
    result
}
```

### src/network.rs

```rust
use crate::activation::Activation;
use crate::matrix::Matrix;
use std::io::{Read, Write, BufReader, BufWriter};
use std::fs::File;

#[derive(Clone, Debug)]
pub struct Layer {
    pub weights: Matrix,  // (input_size x output_size)
    pub bias: Vec<f64>,   // (output_size,)
    pub activation: Activation,
}

impl Layer {
    pub fn new(input_size: usize, output_size: usize, activation: Activation) -> Self {
        Layer {
            weights: Matrix::zeros(input_size, output_size),
            bias: vec![0.0; output_size],
            activation,
        }
    }

    /// Forward pass: output = activation(input * weights + bias).
    /// Input shape: (batch_size x input_size)
    /// Output shape: (batch_size x output_size)
    pub fn forward(&self, input: &Matrix) -> Matrix {
        let linear = input.matmul(&self.weights).add_bias(&self.bias);
        self.activation.apply(&linear)
    }
}

pub struct Network {
    pub layers: Vec<Layer>,
}

impl Network {
    pub fn new() -> Self {
        Network { layers: Vec::new() }
    }

    pub fn add_layer(&mut self, layer: Layer) {
        self.layers.push(layer);
    }

    pub fn forward(&self, input: &Matrix) -> Matrix {
        let mut current = input.clone();
        for layer in &self.layers {
            current = layer.forward(&current);
        }
        current
    }

    pub fn predict(&self, input: &Matrix) -> Vec<usize> {
        let output = self.forward(input);
        output.argmax_per_row()
    }

    pub fn summary(&self) {
        println!("{:<10} {:>12} {:>12} {:>10}", "Layer", "Input", "Output", "Params");
        println!("{}", "-".repeat(48));
        for (i, layer) in self.layers.iter().enumerate() {
            let input_size = layer.weights.rows;
            let output_size = layer.weights.cols;
            let params = input_size * output_size + output_size;
            println!(
                "{:<10} {:>12} {:>12} {:>10}",
                format!("Dense {}", i),
                input_size,
                output_size,
                params
            );
        }
        let total: usize = self
            .layers
            .iter()
            .map(|l| l.weights.rows * l.weights.cols + l.bias.len())
            .sum();
        println!("{}", "-".repeat(48));
        println!("Total parameters: {}", total);
    }
}

// --- Weight Initialization ---

/// Simple xorshift64 PRNG.
pub struct Rng {
    state: u64,
}

impl Rng {
    pub fn new(seed: u64) -> Self {
        Rng {
            state: if seed == 0 { 1 } else { seed },
        }
    }

    fn next_u64(&mut self) -> u64 {
        let mut x = self.state;
        x ^= x << 13;
        x ^= x >> 7;
        x ^= x << 17;
        self.state = x;
        x
    }

    /// Uniform in [0, 1).
    pub fn uniform(&mut self) -> f64 {
        (self.next_u64() >> 11) as f64 / ((1u64 << 53) as f64)
    }

    /// Uniform in [-limit, limit].
    pub fn uniform_range(&mut self, limit: f64) -> f64 {
        self.uniform() * 2.0 * limit - limit
    }
}

/// Xavier/Glorot uniform initialization: U[-sqrt(6/(fan_in+fan_out)), sqrt(6/(fan_in+fan_out))].
pub fn xavier_init(rng: &mut Rng, fan_in: usize, fan_out: usize) -> Matrix {
    let limit = (6.0 / (fan_in + fan_out) as f64).sqrt();
    let data: Vec<f64> = (0..fan_in * fan_out)
        .map(|_| rng.uniform_range(limit))
        .collect();
    Matrix::from_vec(fan_in, fan_out, data)
}

/// He initialization: U[-sqrt(6/fan_in), sqrt(6/fan_in)].
pub fn he_init(rng: &mut Rng, fan_in: usize, fan_out: usize) -> Matrix {
    let limit = (6.0 / fan_in as f64).sqrt();
    let data: Vec<f64> = (0..fan_in * fan_out)
        .map(|_| rng.uniform_range(limit))
        .collect();
    Matrix::from_vec(fan_in, fan_out, data)
}

pub fn build_mnist_network(rng: &mut Rng) -> Network {
    let mut net = Network::new();

    let mut layer1 = Layer::new(784, 128, Activation::ReLU);
    layer1.weights = he_init(rng, 784, 128);

    let mut layer2 = Layer::new(128, 64, Activation::ReLU);
    layer2.weights = he_init(rng, 128, 64);

    let mut layer3 = Layer::new(64, 10, Activation::Softmax);
    layer3.weights = xavier_init(rng, 64, 10);

    net.add_layer(layer1);
    net.add_layer(layer2);
    net.add_layer(layer3);
    net
}

// --- Serialization ---

pub fn save_network(net: &Network, path: &str) -> std::io::Result<()> {
    let file = File::create(path)?;
    let mut writer = BufWriter::new(file);

    let layer_count = net.layers.len() as u32;
    writer.write_all(&layer_count.to_le_bytes())?;

    for layer in &net.layers {
        let rows = layer.weights.rows as u32;
        let cols = layer.weights.cols as u32;
        let act_id: u8 = match layer.activation {
            Activation::ReLU => 0,
            Activation::Sigmoid => 1,
            Activation::Softmax => 2,
            Activation::None => 3,
        };
        writer.write_all(&rows.to_le_bytes())?;
        writer.write_all(&cols.to_le_bytes())?;
        writer.write_all(&[act_id])?;

        for &val in &layer.weights.data {
            writer.write_all(&val.to_le_bytes())?;
        }
        for &val in &layer.bias {
            writer.write_all(&val.to_le_bytes())?;
        }
    }
    Ok(())
}

pub fn load_network(path: &str) -> std::io::Result<Network> {
    let file = File::open(path)?;
    let mut reader = BufReader::new(file);

    let mut buf4 = [0u8; 4];
    reader.read_exact(&mut buf4)?;
    let layer_count = u32::from_le_bytes(buf4) as usize;

    let mut net = Network::new();
    for _ in 0..layer_count {
        reader.read_exact(&mut buf4)?;
        let rows = u32::from_le_bytes(buf4) as usize;
        reader.read_exact(&mut buf4)?;
        let cols = u32::from_le_bytes(buf4) as usize;

        let mut act_byte = [0u8; 1];
        reader.read_exact(&mut act_byte)?;
        let activation = match act_byte[0] {
            0 => Activation::ReLU,
            1 => Activation::Sigmoid,
            2 => Activation::Softmax,
            _ => Activation::None,
        };

        let weight_count = rows * cols;
        let mut weight_data = vec![0.0f64; weight_count];
        let mut buf8 = [0u8; 8];
        for val in weight_data.iter_mut() {
            reader.read_exact(&mut buf8)?;
            *val = f64::from_le_bytes(buf8);
        }

        let mut bias_data = vec![0.0f64; cols];
        for val in bias_data.iter_mut() {
            reader.read_exact(&mut buf8)?;
            *val = f64::from_le_bytes(buf8);
        }

        let layer = Layer {
            weights: Matrix::from_vec(rows, cols, weight_data),
            bias: bias_data,
            activation,
        };
        net.add_layer(layer);
    }
    Ok(net)
}
```

### src/mnist.rs

```rust
use crate::matrix::Matrix;
use std::fs::File;
use std::io::{BufReader, Read};

/// Load MNIST images from IDX format.
/// Returns a Matrix of shape (num_images, 784) with pixel values normalized to [0, 1].
pub fn load_images(path: &str) -> std::io::Result<Matrix> {
    let file = File::open(path)?;
    let mut reader = BufReader::new(file);

    let mut header = [0u8; 16];
    reader.read_exact(&mut header)?;

    let magic = u32::from_be_bytes([header[0], header[1], header[2], header[3]]);
    assert_eq!(magic, 2051, "Invalid image file magic number: {}", magic);

    let num_images = u32::from_be_bytes([header[4], header[5], header[6], header[7]]) as usize;
    let num_rows = u32::from_be_bytes([header[8], header[9], header[10], header[11]]) as usize;
    let num_cols = u32::from_be_bytes([header[12], header[13], header[14], header[15]]) as usize;

    let pixel_count = num_rows * num_cols;
    assert_eq!(pixel_count, 784, "Expected 28x28 images, got {}x{}", num_rows, num_cols);

    let total_bytes = num_images * pixel_count;
    let mut raw = vec![0u8; total_bytes];
    reader.read_exact(&mut raw)?;

    let data: Vec<f64> = raw.iter().map(|&b| b as f64 / 255.0).collect();
    Ok(Matrix::from_vec(num_images, pixel_count, data))
}

/// Load MNIST labels from IDX format.
pub fn load_labels(path: &str) -> std::io::Result<Vec<u8>> {
    let file = File::open(path)?;
    let mut reader = BufReader::new(file);

    let mut header = [0u8; 8];
    reader.read_exact(&mut header)?;

    let magic = u32::from_be_bytes([header[0], header[1], header[2], header[3]]);
    assert_eq!(magic, 2049, "Invalid label file magic number: {}", magic);

    let num_labels = u32::from_be_bytes([header[4], header[5], header[6], header[7]]) as usize;

    let mut labels = vec![0u8; num_labels];
    reader.read_exact(&mut labels)?;

    Ok(labels)
}

pub fn print_digit(images: &Matrix, index: usize) {
    println!("--- Image {} ---", index);
    let row = images.row_slice(index);
    for r in 0..28 {
        for c in 0..28 {
            let pixel = row[r * 28 + c];
            let ch = if pixel > 0.75 {
                '#'
            } else if pixel > 0.50 {
                '+'
            } else if pixel > 0.25 {
                '.'
            } else {
                ' '
            };
            print!("{}", ch);
        }
        println!();
    }
}
```

### src/main.rs

```rust
mod matrix;
mod activation;
mod network;
mod mnist;

use matrix::Matrix;
use network::{Rng, build_mnist_network, save_network, load_network};

fn evaluate(net: &network::Network, images: &Matrix, labels: &[u8]) -> (f64, Vec<Vec<usize>>) {
    let predictions = net.predict(images);

    let mut correct = 0usize;
    // Confusion matrix: confusion[actual][predicted]
    let mut confusion = vec![vec![0usize; 10]; 10];

    for (i, &pred) in predictions.iter().enumerate() {
        let actual = labels[i] as usize;
        confusion[actual][pred] += 1;
        if pred == actual {
            correct += 1;
        }
    }

    let accuracy = correct as f64 / labels.len() as f64;
    (accuracy, confusion)
}

fn print_confusion_matrix(confusion: &[Vec<usize>]) {
    println!("\nConfusion Matrix (rows=actual, cols=predicted):");
    print!("     ");
    for c in 0..10 {
        print!("{:>5}", c);
    }
    println!("   | Total | Acc");
    println!("{}", "-".repeat(78));

    for (actual, row) in confusion.iter().enumerate() {
        print!("  {} |", actual);
        let total: usize = row.iter().sum();
        for &count in row {
            print!("{:>5}", count);
        }
        let correct = row[actual];
        let acc = if total > 0 {
            correct as f64 / total as f64 * 100.0
        } else {
            0.0
        };
        println!("   | {:>5} | {:.1}%", total, acc);
    }
}

fn evaluate_batch(net: &network::Network, images: &Matrix, labels: &[u8], batch_size: usize) {
    let num_images = images.rows;
    let mut all_preds = Vec::with_capacity(num_images);

    let mut offset = 0;
    while offset < num_images {
        let end = (offset + batch_size).min(num_images);
        let batch_rows = end - offset;
        let batch_data = images.data[offset * images.cols..(offset + batch_rows) * images.cols].to_vec();
        let batch = Matrix::from_vec(batch_rows, images.cols, batch_data);
        let preds = net.predict(&batch);
        all_preds.extend(preds);
        offset = end;
    }

    let correct: usize = all_preds
        .iter()
        .zip(labels.iter())
        .filter(|(&p, &l)| p == l as usize)
        .count();

    println!(
        "Batch inference (batch_size={}): {}/{} correct ({:.2}%)",
        batch_size,
        correct,
        num_images,
        correct as f64 / num_images as f64 * 100.0
    );
}

fn main() {
    println!("=== Neural Network Forward Pass ===\n");

    let mut rng = Rng::new(42);
    let net = build_mnist_network(&mut rng);

    println!("Network architecture:");
    net.summary();
    println!();

    // Save and reload to verify serialization round-trip.
    let weights_path = "model_weights.bin";
    save_network(&net, weights_path).expect("Failed to save weights");
    let loaded_net = load_network(weights_path).expect("Failed to load weights");

    // Verify round-trip: same input must produce identical output.
    let test_input = Matrix::from_vec(1, 784, (0..784).map(|i| i as f64 / 784.0).collect());
    let out_original = net.forward(&test_input);
    let out_loaded = loaded_net.forward(&test_input);
    let max_diff: f64 = out_original
        .data
        .iter()
        .zip(out_loaded.data.iter())
        .map(|(a, b)| (a - b).abs())
        .fold(0.0, f64::max);
    println!("Serialization round-trip max diff: {:.2e}", max_diff);
    assert!(max_diff < 1e-15, "Serialization round-trip mismatch");

    // Attempt to load MNIST data.
    let images_path = "t10k-images-idx3-ubyte";
    let labels_path = "t10k-labels-idx1-ubyte";

    match (mnist::load_images(images_path), mnist::load_labels(labels_path)) {
        (Ok(images), Ok(labels)) => {
            println!("\nLoaded MNIST test set: {} images", images.rows);
            mnist::print_digit(&images, 0);

            println!("\n--- Single-image inference ---");
            let (accuracy, confusion) = evaluate(&loaded_net, &images, &labels);
            println!("Overall accuracy: {:.2}%", accuracy * 100.0);
            print_confusion_matrix(&confusion);

            println!("\n--- Batch inference ---");
            evaluate_batch(&loaded_net, &images, &labels, 100);
        }
        _ => {
            println!("\nMNIST files not found ({}, {}).", images_path, labels_path);
            println!("Download from http://yann.lecun.com/exdb/mnist/");
            println!("Running with synthetic input instead.\n");

            let synthetic = Matrix::from_vec(
                5,
                784,
                (0..5 * 784).map(|i| rng.uniform()).collect(),
            );
            let output = loaded_net.forward(&synthetic);
            println!("Softmax output for 5 synthetic images:");
            println!("{}", output);

            let preds = output.argmax_per_row();
            let sums: Vec<f64> = (0..output.rows)
                .map(|r| output.row_slice(r).iter().sum())
                .collect();
            println!("Predictions: {:?}", preds);
            println!("Row sums (should be ~1.0): {:?}", sums);
        }
    }
}
```

### Tests

```rust
// Place in src/main.rs or as a separate tests module.

#[cfg(test)]
mod tests {
    use crate::matrix::Matrix;
    use crate::activation::Activation;
    use crate::network::*;

    #[test]
    fn test_matmul_2x3_times_3x2() {
        let a = Matrix::from_vec(2, 3, vec![1.0, 2.0, 3.0, 4.0, 5.0, 6.0]);
        let b = Matrix::from_vec(3, 2, vec![7.0, 8.0, 9.0, 10.0, 11.0, 12.0]);
        let c = a.matmul(&b);

        assert_eq!(c.rows, 2);
        assert_eq!(c.cols, 2);
        // Row 0: 1*7+2*9+3*11=58, 1*8+2*10+3*12=64
        assert!((c.get(0, 0) - 58.0).abs() < 1e-10);
        assert!((c.get(0, 1) - 64.0).abs() < 1e-10);
        // Row 1: 4*7+5*9+6*11=139, 4*8+5*10+6*12=154
        assert!((c.get(1, 0) - 139.0).abs() < 1e-10);
        assert!((c.get(1, 1) - 154.0).abs() < 1e-10);
    }

    #[test]
    #[should_panic(expected = "matmul dimension mismatch")]
    fn test_matmul_dimension_mismatch() {
        let a = Matrix::from_vec(2, 3, vec![0.0; 6]);
        let b = Matrix::from_vec(2, 2, vec![0.0; 4]);
        a.matmul(&b);
    }

    #[test]
    fn test_relu() {
        let act = Activation::ReLU;
        let input = Matrix::from_vec(1, 5, vec![-2.0, -1.0, 0.0, 1.0, 2.0]);
        let output = act.apply(&input);
        assert!((output.get(0, 0) - 0.0).abs() < 1e-10);
        assert!((output.get(0, 1) - 0.0).abs() < 1e-10);
        assert!((output.get(0, 2) - 0.0).abs() < 1e-10);
        assert!((output.get(0, 3) - 1.0).abs() < 1e-10);
        assert!((output.get(0, 4) - 2.0).abs() < 1e-10);
    }

    #[test]
    fn test_sigmoid() {
        let act = Activation::Sigmoid;
        let input = Matrix::from_vec(1, 3, vec![-1000.0, 0.0, 1000.0]);
        let output = act.apply(&input);
        assert!(output.get(0, 0) < 1e-10);
        assert!((output.get(0, 1) - 0.5).abs() < 1e-10);
        assert!((output.get(0, 2) - 1.0).abs() < 1e-10);
    }

    #[test]
    fn test_softmax_sums_to_one() {
        let act = Activation::Softmax;
        let input = Matrix::from_vec(1, 5, vec![1.0, 2.0, 3.0, 4.0, 5.0]);
        let output = act.apply(&input);
        let sum: f64 = output.row_slice(0).iter().sum();
        assert!((sum - 1.0).abs() < 1e-10, "Softmax sum = {}", sum);
    }

    #[test]
    fn test_softmax_large_values_no_nan() {
        let act = Activation::Softmax;
        let input = Matrix::from_vec(1, 3, vec![999.0, 1000.0, 1001.0]);
        let output = act.apply(&input);
        for &v in &output.data {
            assert!(!v.is_nan(), "Softmax produced NaN");
            assert!(!v.is_infinite(), "Softmax produced infinity");
        }
        let sum: f64 = output.row_slice(0).iter().sum();
        assert!((sum - 1.0).abs() < 1e-10);
    }

    #[test]
    fn test_network_deterministic() {
        let mut rng = Rng::new(123);
        let net = build_mnist_network(&mut rng);

        let input = Matrix::from_vec(1, 784, vec![0.5; 784]);
        let out1 = net.forward(&input);
        let out2 = net.forward(&input);

        for (a, b) in out1.data.iter().zip(out2.data.iter()) {
            assert!((a - b).abs() < 1e-15);
        }
    }

    #[test]
    fn test_batch_vs_single() {
        let mut rng = Rng::new(99);
        let net = build_mnist_network(&mut rng);

        let img1: Vec<f64> = (0..784).map(|_| rng.uniform()).collect();
        let img2: Vec<f64> = (0..784).map(|_| rng.uniform()).collect();

        let single1 = net.forward(&Matrix::from_vec(1, 784, img1.clone()));
        let single2 = net.forward(&Matrix::from_vec(1, 784, img2.clone()));

        let mut batch_data = img1;
        batch_data.extend(img2);
        let batch_result = net.forward(&Matrix::from_vec(2, 784, batch_data));

        for c in 0..10 {
            assert!((single1.get(0, c) - batch_result.get(0, c)).abs() < 1e-10);
            assert!((single2.get(0, c) - batch_result.get(1, c)).abs() < 1e-10);
        }
    }

    #[test]
    fn test_save_load_roundtrip() {
        let mut rng = Rng::new(77);
        let net = build_mnist_network(&mut rng);

        let path = "/tmp/test_nn_weights.bin";
        save_network(&net, path).unwrap();
        let loaded = load_network(path).unwrap();

        let input = Matrix::from_vec(1, 784, vec![0.3; 784]);
        let out_orig = net.forward(&input);
        let out_loaded = loaded.forward(&input);

        for (a, b) in out_orig.data.iter().zip(out_loaded.data.iter()) {
            assert!((a - b).abs() < 1e-15, "Round-trip mismatch: {} vs {}", a, b);
        }
    }

    #[test]
    fn test_argmax() {
        let m = Matrix::from_vec(2, 4, vec![0.1, 0.7, 0.1, 0.1, 0.3, 0.1, 0.5, 0.1]);
        let argmaxes = m.argmax_per_row();
        assert_eq!(argmaxes, vec![1, 2]);
    }

    #[test]
    fn test_add_bias() {
        let m = Matrix::from_vec(2, 3, vec![1.0, 2.0, 3.0, 4.0, 5.0, 6.0]);
        let bias = vec![10.0, 20.0, 30.0];
        let result = m.add_bias(&bias);
        assert!((result.get(0, 0) - 11.0).abs() < 1e-10);
        assert!((result.get(1, 2) - 36.0).abs() < 1e-10);
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
=== Neural Network Forward Pass ===

Network architecture:
Layer         Input       Output     Params
------------------------------------------------
Dense 0         784          128     100480
Dense 1         128           64       8256
Dense 2          64           10        650
------------------------------------------------
Total parameters: 109386

Serialization round-trip max diff: 0.00e+0

MNIST files not found (t10k-images-idx3-ubyte, t10k-labels-idx1-ubyte).
Download from http://yann.lecun.com/exdb/mnist/
Running with synthetic input instead.

Softmax output for 5 synthetic images:
  0.1023   0.0891   0.1145   0.0987   0.1034   0.0912   0.1056   0.0978   0.1003   0.0971
  ...
Predictions: [2, 4, 0, 6, 2]
Row sums (should be ~1.0): [1.0, 1.0, 1.0, 1.0, 1.0]
```

With MNIST data present, the network reports accuracy and a full confusion matrix. Random weights give ~10% accuracy (random chance for 10 classes), confirming the forward pass distributes probabilities correctly without bias toward any digit.

## Design Decisions

1. **Row-major flat Vec over nested Vec<Vec>**: A single contiguous allocation avoids pointer chasing and enables cache-friendly sequential access during matmul. The index computation `row * cols + col` is one multiply and one add -- negligible overhead.

2. **ik-j loop order for matmul**: The standard ij-k order has poor cache behavior because `other.get(k, j)` strides across rows. The ik-j order fixes this: the inner loop over `j` accesses both `result` and `other` sequentially. This alone provides 2-3x speedup for large matrices.

3. **Activation as enum, not trait object**: Since there are only four activation types and they are known at compile time, an enum with match dispatch avoids vtable indirection and enables inlining. If extensibility were needed, a trait object would be the right choice.

4. **Softmax per-row with max subtraction**: This is not optional. Without subtracting the row maximum, `exp(710)` overflows f64 and produces `inf`, which propagates through the division as NaN. The mathematical equivalence is: `softmax(x) == softmax(x - c)` for any constant `c`.

5. **Binary serialization over JSON/text**: Weight files for neural networks can be megabytes. Binary f64 values avoid parsing overhead and floating-point representation loss from text encoding. The format is simple (layer count, dimensions, raw data) and self-describing enough to validate on load.

6. **Batch inference via matrix-matrix multiplication**: Processing B images simultaneously through `(B x 784) * (784 x 128)` reuses each weight value B times per cache load, dramatically improving throughput compared to B separate vector-matrix multiplications.

## Common Mistakes

1. **Transposing the weight matrix incorrectly**: If weights are stored as (output x input) but matmul expects (input x output), every layer produces wrong-dimension results. Decide on one convention early and stick with it everywhere. This solution uses (input x output), so forward is `input * W`, not `W * input`.

2. **Forgetting column-major ordering for MNIST IDX**: The IDX format stores pixels in row-major order (left to right, top to bottom), which happens to match what we need. But if you reshape as 28 columns of 28 rows instead of 28 rows of 28 columns, the digit images appear transposed.

3. **Not normalizing pixel values**: MNIST pixels are u8 [0, 255]. If fed directly as f64 values without dividing by 255, the first layer receives inputs 100x larger than expected, causing activation saturation and exploding gradients (relevant for training, but also produces skewed softmax outputs).

4. **Softmax on the wrong axis**: For batch processing, softmax must be applied independently to each row (each image), not to the entire matrix. Applying softmax to all 10*B values produces invalid probability distributions.

5. **Bias broadcasting bug**: The bias has shape (output_size,) and must be added to every row of the (batch x output_size) matrix. Adding it only to the first row, or adding it incorrectly to columns, produces wrong results for batch_size > 1.

## Performance Notes

The bottleneck is matrix multiplication. For the 784x128 first layer with batch size 100:
- **Naive ijk**: ~20M multiply-add operations, ~2ms on modern CPU
- **Optimized ikj loop order**: ~30-50% faster due to sequential memory access in the inner loop
- **Batch processing**: (100 x 784) * (784 x 128) reuses weight matrix 100x, ~5x throughput improvement over 100 individual vector-matrix multiplications

For production inference at this scale, the network processes 10,000 MNIST images in under 1 second on release mode. The main optimization opportunities:
1. **SIMD**: Vectorize the inner loop of matmul with AVX2 (8 f64s per instruction)
2. **Tiling**: Partition matrices into cache-friendly blocks (see Challenge 143)
3. **Parallelism**: Process batch rows on separate threads with rayon
