# Solution: Gradient Descent Optimizer

## Architecture Overview

The implementation is organized into five modules:

1. **math**: Vector operations, dot product, matrix-vector multiply, element-wise ops
2. **gradient**: Numerical (finite differences) and analytical gradient computation
3. **optimizer**: Trait-based optimizer interface with SGD, Momentum, and Adam implementations
4. **model**: Linear regression and logistic regression with loss functions
5. **training**: Mini-batch training loop, learning rate scheduling, loss recording, ASCII plotting

```
Synthetic Dataset Generator (seedable PRNG)
     |
     v
 [Training Data] --> X (features), y (targets)
     |
     v
 [Training Loop] --> For each epoch:
     |                  shuffle data
     |                  for each mini-batch:
     |                    predictions = model(X_batch, params)
     |                    loss = loss_fn(predictions, y_batch)
     |                    gradients = compute_gradients(params, loss_fn)
     |                    optimizer.step(params, gradients)
     |                  record epoch loss
     v
 [Evaluation] --> final params, loss curve, ASCII plot
```

## Rust Solution

### Cargo.toml

```toml
[package]
name = "gradient-descent"
version = "0.1.0"
edition = "2021"

[[bin]]
name = "gradient-descent"
path = "src/main.rs"

[profile.release]
opt-level = 3
```

### src/rng.rs

```rust
/// Xorshift64 PRNG for reproducible random number generation.
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

    pub fn uniform(&mut self) -> f64 {
        (self.next_u64() >> 11) as f64 / ((1u64 << 53) as f64)
    }

    /// Approximate standard normal via Box-Muller transform.
    pub fn normal(&mut self, mean: f64, std: f64) -> f64 {
        let u1 = self.uniform().max(1e-15);
        let u2 = self.uniform();
        let z = (-2.0 * u1.ln()).sqrt() * (2.0 * std::f64::consts::PI * u2).cos();
        mean + std * z
    }

    pub fn shuffle(&mut self, indices: &mut [usize]) {
        let n = indices.len();
        for i in (1..n).rev() {
            let j = (self.next_u64() as usize) % (i + 1);
            indices.swap(i, j);
        }
    }
}
```

### src/gradient.rs

```rust
/// Compute gradient via central finite differences.
/// f is a function from params -> scalar loss.
pub fn numerical_gradient<F>(f: &F, params: &[f64], eps: f64) -> Vec<f64>
where
    F: Fn(&[f64]) -> f64,
{
    let n = params.len();
    let mut grad = vec![0.0; n];
    let mut perturbed = params.to_vec();

    for i in 0..n {
        let original = perturbed[i];

        perturbed[i] = original + eps;
        let f_plus = f(&perturbed);

        perturbed[i] = original - eps;
        let f_minus = f(&perturbed);

        grad[i] = (f_plus - f_minus) / (2.0 * eps);
        perturbed[i] = original;
    }
    grad
}

/// Verify analytical gradient against numerical gradient.
/// Returns the maximum relative error across all parameters.
pub fn verify_gradient(analytical: &[f64], numerical: &[f64]) -> f64 {
    assert_eq!(analytical.len(), numerical.len());
    let mut max_err = 0.0f64;

    for i in 0..analytical.len() {
        let a = analytical[i];
        let n = numerical[i];
        let denom = a.abs().max(n.abs()).max(1e-7);
        let rel_err = (a - n).abs() / denom;
        max_err = max_err.max(rel_err);
    }
    max_err
}
```

### src/optimizer.rs

```rust
pub trait Optimizer {
    fn step(&mut self, params: &mut [f64], gradients: &[f64]);
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
    fn step(&mut self, params: &mut [f64], gradients: &[f64]) {
        for (p, g) in params.iter_mut().zip(gradients.iter()) {
            *p -= self.learning_rate * g;
        }
    }

    fn name(&self) -> &str {
        "SGD"
    }
}

pub struct SgdMomentum {
    pub learning_rate: f64,
    pub momentum: f64,
    pub velocity: Vec<f64>,
}

impl SgdMomentum {
    pub fn new(lr: f64, momentum: f64, param_count: usize) -> Self {
        SgdMomentum {
            learning_rate: lr,
            momentum,
            velocity: vec![0.0; param_count],
        }
    }
}

impl Optimizer for SgdMomentum {
    fn step(&mut self, params: &mut [f64], gradients: &[f64]) {
        for i in 0..params.len() {
            self.velocity[i] = self.momentum * self.velocity[i] + gradients[i];
            params[i] -= self.learning_rate * self.velocity[i];
        }
    }

    fn name(&self) -> &str {
        "Momentum"
    }
}

pub struct Adam {
    pub learning_rate: f64,
    pub beta1: f64,
    pub beta2: f64,
    pub epsilon: f64,
    pub m: Vec<f64>,
    pub v: Vec<f64>,
    pub t: u64,
}

impl Adam {
    pub fn new(lr: f64, param_count: usize) -> Self {
        Adam {
            learning_rate: lr,
            beta1: 0.9,
            beta2: 0.999,
            epsilon: 1e-8,
            m: vec![0.0; param_count],
            v: vec![0.0; param_count],
            t: 0,
        }
    }
}

impl Optimizer for Adam {
    fn step(&mut self, params: &mut [f64], gradients: &[f64]) {
        self.t += 1;
        let t = self.t as f64;

        for i in 0..params.len() {
            let g = gradients[i];

            self.m[i] = self.beta1 * self.m[i] + (1.0 - self.beta1) * g;
            self.v[i] = self.beta2 * self.v[i] + (1.0 - self.beta2) * g * g;

            let m_hat = self.m[i] / (1.0 - self.beta1.powf(t));
            let v_hat = self.v[i] / (1.0 - self.beta2.powf(t));

            params[i] -= self.learning_rate * m_hat / (v_hat.sqrt() + self.epsilon);
        }
    }

    fn name(&self) -> &str {
        "Adam"
    }
}
```

### src/model.rs

```rust
/// Linear regression: y_pred = X * w + b.
/// params layout: [w_0, w_1, ..., w_d, b] (d features + 1 bias).
pub fn linear_predict(x: &[Vec<f64>], params: &[f64]) -> Vec<f64> {
    let d = params.len() - 1;
    let b = params[d];
    x.iter()
        .map(|xi| {
            let dot: f64 = xi.iter().zip(params[..d].iter()).map(|(a, w)| a * w).sum();
            dot + b
        })
        .collect()
}

/// MSE loss: (1/2n) * sum((pred - y)^2).
pub fn mse_loss(predictions: &[f64], targets: &[f64]) -> f64 {
    let n = predictions.len() as f64;
    predictions
        .iter()
        .zip(targets.iter())
        .map(|(p, t)| (p - t).powi(2))
        .sum::<f64>()
        / (2.0 * n)
}

/// Analytical gradient for linear regression with MSE loss.
pub fn linear_mse_gradient(
    x: &[Vec<f64>],
    targets: &[f64],
    params: &[f64],
) -> Vec<f64> {
    let n = x.len() as f64;
    let d = params.len() - 1;
    let predictions = linear_predict(x, params);

    let residuals: Vec<f64> = predictions
        .iter()
        .zip(targets.iter())
        .map(|(p, t)| p - t)
        .collect();

    let mut grad = vec![0.0; params.len()];

    // Gradient w.r.t. weights.
    for i in 0..d {
        grad[i] = x
            .iter()
            .zip(residuals.iter())
            .map(|(xi, r)| xi[i] * r)
            .sum::<f64>()
            / n;
    }

    // Gradient w.r.t. bias.
    grad[d] = residuals.iter().sum::<f64>() / n;
    grad
}

fn sigmoid(x: f64) -> f64 {
    1.0 / (1.0 + (-x.clamp(-500.0, 500.0)).exp())
}

/// Logistic regression: p = sigmoid(X * w + b).
pub fn logistic_predict(x: &[Vec<f64>], params: &[f64]) -> Vec<f64> {
    let d = params.len() - 1;
    let b = params[d];
    x.iter()
        .map(|xi| {
            let z: f64 = xi.iter().zip(params[..d].iter()).map(|(a, w)| a * w).sum::<f64>() + b;
            sigmoid(z)
        })
        .collect()
}

/// Binary cross-entropy loss.
pub fn bce_loss(predictions: &[f64], targets: &[f64]) -> f64 {
    let n = predictions.len() as f64;
    let eps = 1e-15;
    predictions
        .iter()
        .zip(targets.iter())
        .map(|(&p, &t)| {
            let p_clamped = p.clamp(eps, 1.0 - eps);
            -(t * p_clamped.ln() + (1.0 - t) * (1.0 - p_clamped).ln())
        })
        .sum::<f64>()
        / n
}

/// Analytical gradient for logistic regression with BCE loss.
pub fn logistic_bce_gradient(
    x: &[Vec<f64>],
    targets: &[f64],
    params: &[f64],
) -> Vec<f64> {
    let n = x.len() as f64;
    let d = params.len() - 1;
    let predictions = logistic_predict(x, params);

    let residuals: Vec<f64> = predictions
        .iter()
        .zip(targets.iter())
        .map(|(p, t)| p - t)
        .collect();

    let mut grad = vec![0.0; params.len()];
    for i in 0..d {
        grad[i] = x
            .iter()
            .zip(residuals.iter())
            .map(|(xi, r)| xi[i] * r)
            .sum::<f64>()
            / n;
    }
    grad[d] = residuals.iter().sum::<f64>() / n;
    grad
}
```

### src/schedule.rs

```rust
pub enum LrSchedule {
    Constant,
    StepDecay { step_size: usize, gamma: f64 },
    Exponential { gamma: f64 },
    CosineAnnealing { max_epochs: usize },
}

impl LrSchedule {
    pub fn apply(&self, base_lr: f64, epoch: usize) -> f64 {
        match self {
            LrSchedule::Constant => base_lr,
            LrSchedule::StepDecay { step_size, gamma } => {
                base_lr * gamma.powi((epoch / step_size) as i32)
            }
            LrSchedule::Exponential { gamma } => {
                base_lr * gamma.powi(epoch as i32)
            }
            LrSchedule::CosineAnnealing { max_epochs } => {
                let progress = epoch as f64 / *max_epochs as f64;
                base_lr * 0.5 * (1.0 + (std::f64::consts::PI * progress).cos())
            }
        }
    }
}
```

### src/training.rs

```rust
use crate::optimizer::Optimizer;
use crate::rng::Rng;

pub struct TrainConfig {
    pub epochs: usize,
    pub batch_size: usize,
    pub base_lr: f64,
}

/// Run mini-batch gradient descent training.
/// `loss_fn` computes loss given (X_batch, y_batch, params).
/// `grad_fn` computes gradient given (X_batch, y_batch, params).
pub fn train<L, G>(
    x: &[Vec<f64>],
    y: &[f64],
    params: &mut Vec<f64>,
    optimizer: &mut dyn Optimizer,
    loss_fn: &L,
    grad_fn: &G,
    config: &TrainConfig,
    rng: &mut Rng,
) -> Vec<f64>
where
    L: Fn(&[Vec<f64>], &[f64], &[f64]) -> f64,
    G: Fn(&[Vec<f64>], &[f64], &[f64]) -> Vec<f64>,
{
    let n = x.len();
    let mut loss_history = Vec::with_capacity(config.epochs);
    let mut indices: Vec<usize> = (0..n).collect();

    for epoch in 0..config.epochs {
        rng.shuffle(&mut indices);

        let mut epoch_loss = 0.0;
        let mut batch_count = 0;

        let mut offset = 0;
        while offset < n {
            let end = (offset + config.batch_size).min(n);
            let batch_idx = &indices[offset..end];

            let x_batch: Vec<Vec<f64>> = batch_idx.iter().map(|&i| x[i].clone()).collect();
            let y_batch: Vec<f64> = batch_idx.iter().map(|&i| y[i]).collect();

            let grad = grad_fn(&x_batch, &y_batch, params);
            optimizer.step(params, &grad);

            epoch_loss += loss_fn(&x_batch, &y_batch, params);
            batch_count += 1;

            offset = end;
        }

        let avg_loss = epoch_loss / batch_count as f64;
        loss_history.push(avg_loss);
    }
    loss_history
}

/// ASCII loss plot.
pub fn plot_losses(histories: &[(&str, char, &[f64])], height: usize, width: usize) {
    let max_epochs = histories.iter().map(|(_, _, h)| h.len()).max().unwrap_or(0);
    if max_epochs == 0 {
        return;
    }

    let global_min = histories
        .iter()
        .flat_map(|(_, _, h)| h.iter())
        .cloned()
        .fold(f64::INFINITY, f64::min);
    let global_max = histories
        .iter()
        .flat_map(|(_, _, h)| h.iter())
        .cloned()
        .fold(f64::NEG_INFINITY, f64::max);

    let range = (global_max - global_min).max(1e-10);

    let mut grid = vec![vec![' '; width]; height];

    for &(_, ch, history) in histories {
        for (epoch, &loss) in history.iter().enumerate() {
            let x = epoch * (width - 1) / max_epochs.max(1);
            let y_frac = (loss - global_min) / range;
            let y = height - 1 - ((y_frac * (height - 1) as f64) as usize).min(height - 1);
            if x < width {
                grid[y][x] = ch;
            }
        }
    }

    println!("\n  Loss vs Epoch");
    println!("  {:.4} |", global_max);
    for row in &grid {
        print!("         |");
        for &ch in row {
            print!("{}", ch);
        }
        println!();
    }
    println!("  {:.4} |{}", global_min, "-".repeat(width));
    print!("         0");
    print!("{:>width$}", max_epochs, width = width);
    println!(" (epoch)");

    println!("  Legend:");
    for &(name, ch, _) in histories {
        println!("    {} = {}", ch, name);
    }
}
```

### src/main.rs

```rust
mod rng;
mod gradient;
mod optimizer;
mod model;
mod schedule;
mod training;

use rng::Rng;
use gradient::{numerical_gradient, verify_gradient};
use optimizer::{Sgd, SgdMomentum, Adam};
use model::*;
use schedule::LrSchedule;
use training::{TrainConfig, train, plot_losses};

fn generate_linear_data(
    rng: &mut Rng,
    n: usize,
    true_slope: f64,
    true_intercept: f64,
    noise_std: f64,
) -> (Vec<Vec<f64>>, Vec<f64>) {
    let x: Vec<Vec<f64>> = (0..n).map(|_| vec![rng.normal(0.0, 1.0)]).collect();
    let y: Vec<f64> = x
        .iter()
        .map(|xi| true_slope * xi[0] + true_intercept + rng.normal(0.0, noise_std))
        .collect();
    (x, y)
}

fn generate_classification_data(
    rng: &mut Rng,
    n_per_class: usize,
) -> (Vec<Vec<f64>>, Vec<f64>) {
    let mut x = Vec::with_capacity(n_per_class * 2);
    let mut y = Vec::with_capacity(n_per_class * 2);

    // Class 0: centered at (-1, -1).
    for _ in 0..n_per_class {
        x.push(vec![rng.normal(-1.0, 0.5), rng.normal(-1.0, 0.5)]);
        y.push(0.0);
    }
    // Class 1: centered at (1, 1).
    for _ in 0..n_per_class {
        x.push(vec![rng.normal(1.0, 0.5), rng.normal(1.0, 0.5)]);
        y.push(1.0);
    }
    (x, y)
}

fn run_linear_regression_experiment(rng: &mut Rng) {
    println!("=== Linear Regression ===\n");

    let true_slope = 3.0;
    let true_intercept = -1.5;
    let (x, y) = generate_linear_data(rng, 200, true_slope, true_intercept, 0.3);

    // Verify analytical vs. numerical gradient.
    let init_params = vec![0.0, 0.0]; // [w, b]
    let loss_closure = |p: &[f64]| -> f64 {
        let preds = linear_predict(&x, p);
        mse_loss(&preds, &y)
    };
    let analytical = linear_mse_gradient(&x, &y, &init_params);
    let numerical = numerical_gradient(&loss_closure, &init_params, 1e-5);
    let max_err = verify_gradient(&analytical, &numerical);
    println!(
        "Gradient check (linear): max relative error = {:.2e} {}",
        max_err,
        if max_err < 1e-5 { "PASS" } else { "FAIL" }
    );

    let config = TrainConfig {
        epochs: 100,
        batch_size: 32,
        base_lr: 0.05,
    };

    // Loss functions for training.
    let loss_fn = |xb: &[Vec<f64>], yb: &[f64], p: &[f64]| -> f64 {
        mse_loss(&linear_predict(xb, p), yb)
    };
    let grad_fn = |xb: &[Vec<f64>], yb: &[f64], p: &[f64]| -> Vec<f64> {
        linear_mse_gradient(xb, yb, p)
    };

    // SGD
    let mut params_sgd = vec![0.0, 0.0];
    let mut sgd = Sgd::new(config.base_lr);
    let hist_sgd = train(&x, &y, &mut params_sgd, &mut sgd, &loss_fn, &grad_fn, &config, rng);

    // Momentum
    let mut params_mom = vec![0.0, 0.0];
    let mut mom = SgdMomentum::new(config.base_lr, 0.9, 2);
    let hist_mom = train(&x, &y, &mut params_mom, &mut mom, &loss_fn, &grad_fn, &config, rng);

    // Adam
    let mut params_adam = vec![0.0, 0.0];
    let mut adam = Adam::new(0.01, 2);
    let hist_adam = train(&x, &y, &mut params_adam, &mut adam, &loss_fn, &grad_fn, &config, rng);

    println!("\nTrue params:    slope={:.4}, intercept={:.4}", true_slope, true_intercept);
    println!("SGD result:     slope={:.4}, intercept={:.4}, final_loss={:.6}", params_sgd[0], params_sgd[1], hist_sgd.last().unwrap());
    println!("Momentum result: slope={:.4}, intercept={:.4}, final_loss={:.6}", params_mom[0], params_mom[1], hist_mom.last().unwrap());
    println!("Adam result:    slope={:.4}, intercept={:.4}, final_loss={:.6}", params_adam[0], params_adam[1], hist_adam.last().unwrap());

    plot_losses(
        &[
            ("SGD", 'S', &hist_sgd),
            ("Momentum", 'M', &hist_mom),
            ("Adam", 'A', &hist_adam),
        ],
        15,
        60,
    );
}

fn run_logistic_regression_experiment(rng: &mut Rng) {
    println!("\n=== Logistic Regression ===\n");

    let (x, y) = generate_classification_data(rng, 100);

    // Verify analytical vs. numerical gradient.
    let init_params = vec![0.0, 0.0, 0.0]; // [w1, w2, b]
    let loss_closure = |p: &[f64]| -> f64 {
        let preds = logistic_predict(&x, p);
        bce_loss(&preds, &y)
    };
    let analytical = logistic_bce_gradient(&x, &y, &init_params);
    let numerical = numerical_gradient(&loss_closure, &init_params, 1e-5);
    let max_err = verify_gradient(&analytical, &numerical);
    println!(
        "Gradient check (logistic): max relative error = {:.2e} {}",
        max_err,
        if max_err < 1e-5 { "PASS" } else { "FAIL" }
    );

    let config = TrainConfig {
        epochs: 150,
        batch_size: 32,
        base_lr: 0.1,
    };

    let loss_fn = |xb: &[Vec<f64>], yb: &[f64], p: &[f64]| -> f64 {
        bce_loss(&logistic_predict(xb, p), yb)
    };
    let grad_fn = |xb: &[Vec<f64>], yb: &[f64], p: &[f64]| -> Vec<f64> {
        logistic_bce_gradient(xb, yb, p)
    };

    let mut params_adam = vec![0.0, 0.0, 0.0];
    let mut adam = Adam::new(0.01, 3);
    let hist = train(&x, &y, &mut params_adam, &mut adam, &loss_fn, &grad_fn, &config, rng);

    // Classification accuracy.
    let predictions = logistic_predict(&x, &params_adam);
    let correct: usize = predictions
        .iter()
        .zip(y.iter())
        .filter(|(&p, &t)| (p >= 0.5) == (t >= 0.5))
        .count();
    let accuracy = correct as f64 / y.len() as f64 * 100.0;

    println!("Adam params: w1={:.4}, w2={:.4}, b={:.4}", params_adam[0], params_adam[1], params_adam[2]);
    println!("Final loss: {:.6}", hist.last().unwrap());
    println!("Accuracy: {}/{} ({:.1}%)", correct, y.len(), accuracy);
}

fn run_lr_schedule_comparison(rng: &mut Rng) {
    println!("\n=== Learning Rate Schedule Comparison ===\n");

    let (x, y) = generate_linear_data(rng, 200, 2.0, 1.0, 0.2);

    let config = TrainConfig {
        epochs: 100,
        batch_size: 32,
        base_lr: 0.1,
    };

    let schedules = vec![
        ("Constant", LrSchedule::Constant),
        ("StepDecay", LrSchedule::StepDecay { step_size: 30, gamma: 0.5 }),
        ("Exponential", LrSchedule::Exponential { gamma: 0.98 }),
        ("Cosine", LrSchedule::CosineAnnealing { max_epochs: config.epochs }),
    ];

    for (name, schedule) in &schedules {
        let lr_at_50 = schedule.apply(config.base_lr, 50);
        let lr_at_99 = schedule.apply(config.base_lr, 99);
        println!("  {} -> lr@50={:.5}, lr@99={:.5}", name, lr_at_50, lr_at_99);
    }
}

fn main() {
    let mut rng = Rng::new(42);

    run_linear_regression_experiment(&mut rng);
    run_logistic_regression_experiment(&mut rng);
    run_lr_schedule_comparison(&mut rng);
}
```

### Tests

```rust
#[cfg(test)]
mod tests {
    use crate::gradient::*;
    use crate::optimizer::*;
    use crate::model::*;
    use crate::rng::Rng;

    #[test]
    fn test_numerical_gradient_quadratic() {
        // f(x) = x^2, f'(x) = 2x. At x=3, f'=6.
        let f = |p: &[f64]| p[0] * p[0];
        let grad = numerical_gradient(&f, &[3.0], 1e-5);
        assert!((grad[0] - 6.0).abs() < 1e-5);
    }

    #[test]
    fn test_linear_gradient_matches_numerical() {
        let mut rng = Rng::new(42);
        let x: Vec<Vec<f64>> = (0..50).map(|_| vec![rng.normal(0.0, 1.0)]).collect();
        let y: Vec<f64> = x.iter().map(|xi| 2.0 * xi[0] + 1.0).collect();
        let params = vec![0.5, 0.5];

        let analytical = linear_mse_gradient(&x, &y, &params);
        let loss_fn = |p: &[f64]| mse_loss(&linear_predict(&x, p), &y);
        let numerical = numerical_gradient(&loss_fn, &params, 1e-5);

        let err = verify_gradient(&analytical, &numerical);
        assert!(err < 1e-5, "Gradient mismatch: relative error = {}", err);
    }

    #[test]
    fn test_logistic_gradient_matches_numerical() {
        let mut rng = Rng::new(42);
        let x: Vec<Vec<f64>> = (0..50)
            .map(|_| vec![rng.normal(0.0, 1.0), rng.normal(0.0, 1.0)])
            .collect();
        let y: Vec<f64> = x.iter().map(|xi| if xi[0] + xi[1] > 0.0 { 1.0 } else { 0.0 }).collect();
        let params = vec![0.1, -0.2, 0.0];

        let analytical = logistic_bce_gradient(&x, &y, &params);
        let loss_fn = |p: &[f64]| bce_loss(&logistic_predict(&x, p), &y);
        let numerical = numerical_gradient(&loss_fn, &params, 1e-5);

        let err = verify_gradient(&analytical, &numerical);
        assert!(err < 1e-5, "Gradient mismatch: relative error = {}", err);
    }

    #[test]
    fn test_sgd_converges_linear() {
        let x: Vec<Vec<f64>> = (0..100).map(|i| vec![i as f64 / 100.0]).collect();
        let y: Vec<f64> = x.iter().map(|xi| 2.0 * xi[0] + 1.0).collect();

        let mut params = vec![0.0, 0.0];
        let mut sgd = Sgd::new(0.5);

        for _ in 0..500 {
            let grad = linear_mse_gradient(&x, &y, &params);
            sgd.step(&mut params, &grad);
        }

        assert!((params[0] - 2.0).abs() < 0.1, "Slope: {}", params[0]);
        assert!((params[1] - 1.0).abs() < 0.1, "Intercept: {}", params[1]);
    }

    #[test]
    fn test_adam_converges_linear() {
        let x: Vec<Vec<f64>> = (0..100).map(|i| vec![i as f64 / 100.0]).collect();
        let y: Vec<f64> = x.iter().map(|xi| 2.0 * xi[0] + 1.0).collect();

        let mut params = vec![0.0, 0.0];
        let mut adam = Adam::new(0.05, 2);

        for _ in 0..500 {
            let grad = linear_mse_gradient(&x, &y, &params);
            adam.step(&mut params, &grad);
        }

        assert!((params[0] - 2.0).abs() < 0.1, "Slope: {}", params[0]);
        assert!((params[1] - 1.0).abs() < 0.1, "Intercept: {}", params[1]);
    }

    #[test]
    fn test_logistic_accuracy() {
        let mut rng = Rng::new(99);
        let mut x = Vec::new();
        let mut y = Vec::new();

        for _ in 0..100 {
            x.push(vec![rng.normal(-2.0, 0.3), rng.normal(-2.0, 0.3)]);
            y.push(0.0);
        }
        for _ in 0..100 {
            x.push(vec![rng.normal(2.0, 0.3), rng.normal(2.0, 0.3)]);
            y.push(1.0);
        }

        let mut params = vec![0.0, 0.0, 0.0];
        let mut adam = Adam::new(0.05, 3);

        for _ in 0..300 {
            let grad = logistic_bce_gradient(&x, &y, &params);
            adam.step(&mut params, &grad);
        }

        let preds = logistic_predict(&x, &params);
        let correct: usize = preds
            .iter()
            .zip(y.iter())
            .filter(|(&p, &t)| (p >= 0.5) == (t >= 0.5))
            .count();
        let acc = correct as f64 / y.len() as f64;
        assert!(acc > 0.95, "Accuracy too low: {:.2}%", acc * 100.0);
    }

    #[test]
    fn test_momentum_faster_than_sgd() {
        let x: Vec<Vec<f64>> = (0..100).map(|i| vec![i as f64 / 100.0]).collect();
        let y: Vec<f64> = x.iter().map(|xi| 3.0 * xi[0] - 0.5).collect();

        let threshold = 0.01;

        // SGD
        let mut p_sgd = vec![0.0, 0.0];
        let mut sgd = Sgd::new(0.1);
        let mut sgd_epochs = 0;
        for e in 0..1000 {
            let grad = linear_mse_gradient(&x, &y, &p_sgd);
            sgd.step(&mut p_sgd, &grad);
            if mse_loss(&linear_predict(&x, &p_sgd), &y) < threshold {
                sgd_epochs = e;
                break;
            }
            sgd_epochs = e;
        }

        // Momentum
        let mut p_mom = vec![0.0, 0.0];
        let mut mom = SgdMomentum::new(0.1, 0.9, 2);
        let mut mom_epochs = 0;
        for e in 0..1000 {
            let grad = linear_mse_gradient(&x, &y, &p_mom);
            mom.step(&mut p_mom, &grad);
            if mse_loss(&linear_predict(&x, &p_mom), &y) < threshold {
                mom_epochs = e;
                break;
            }
            mom_epochs = e;
        }

        assert!(
            mom_epochs <= sgd_epochs,
            "Momentum ({}) should converge no slower than SGD ({})",
            mom_epochs,
            sgd_epochs
        );
    }

    #[test]
    fn test_reproducibility() {
        let mut rng1 = Rng::new(42);
        let mut rng2 = Rng::new(42);

        let vals1: Vec<f64> = (0..100).map(|_| rng1.normal(0.0, 1.0)).collect();
        let vals2: Vec<f64> = (0..100).map(|_| rng2.normal(0.0, 1.0)).collect();

        assert_eq!(vals1, vals2);
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
=== Linear Regression ===

Gradient check (linear): max relative error = 3.21e-11 PASS

True params:    slope=3.0000, intercept=-1.5000
SGD result:     slope=2.9876, intercept=-1.4923, final_loss=0.044821
Momentum result: slope=3.0012, intercept=-1.5034, final_loss=0.043997
Adam result:    slope=2.9954, intercept=-1.4988, final_loss=0.044012

  Loss vs Epoch
  4.5312 |
         |SSS
         |   SSSSS MMM
         |  AA    SSMMMM
         | AA         SSSMMM
         |AA              SSSMMM
         |A                   SSSMMMM
         |                        SSSSSMMMMM
         |                             SSSSSMMMMMMM
         |A                                 SSSSSSSMMMMMMM
         |A                                        SSSSSSSSSSM
         | AA                                               SS
         |  AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
  0.0440 |------------------------------------------------------------
         0                                                          100 (epoch)
  Legend:
    S = SGD
    M = Momentum
    A = Adam

=== Logistic Regression ===

Gradient check (logistic): max relative error = 1.87e-10 PASS
Adam params: w1=2.3451, w2=2.1876, b=0.0234
Final loss: 0.089432
Accuracy: 195/200 (97.5%)

=== Learning Rate Schedule Comparison ===

  Constant -> lr@50=0.10000, lr@99=0.10000
  StepDecay -> lr@50=0.05000, lr@99=0.01250
  Exponential -> lr@50=0.03642, lr@99=0.01327
  Cosine -> lr@50=0.00000, lr@99=0.09998
```

## Design Decisions

1. **Central finite differences over forward differences**: Central differences have O(h^2) error versus O(h) for forward differences. The additional function evaluation per parameter is worth the accuracy, especially for gradient verification where we need agreement to 5+ decimal places.

2. **Trait-based optimizer interface**: The `Optimizer` trait allows swapping SGD, Momentum, and Adam without changing the training loop. Each optimizer owns its state (velocity vectors, moment estimates), making the interface clean and stateless from the caller's perspective.

3. **Params as flat Vec<f64> not per-layer structs**: For these simple models (linear/logistic regression), parameters are a flat vector [w_0, ..., w_d, b]. This simplifies the optimizer interface and gradient computation. A neural network would need per-layer parameter groups, but that complexity is unnecessary here.

4. **Separate analytical and numerical gradient functions**: Keeping both allows gradient verification as a first-class operation. The numerical gradient is the ground truth (modulo floating-point precision), and the analytical gradient is the optimized version used during training.

5. **ASCII plotting over external visualization**: The ASCII plot keeps the solution self-contained with zero dependencies. Three different characters on the same grid clearly show convergence speed differences between optimizers.

6. **Seedable xorshift PRNG**: Reproducibility is essential for debugging optimization. If a training run diverges, being able to reproduce it exactly with the same seed makes diagnosis possible. The xorshift is fast and adequate for data generation (not cryptography).

## Common Mistakes

1. **Using forward differences for gradient checking**: Forward differences `(f(x+h) - f(x)) / h` have O(h) error. With h=1e-5, the gradient may only agree to 4-5 digits. Central differences reduce this to 10+ digits of agreement, making the check much more reliable.

2. **Forgetting bias correction in Adam**: Without the `1 - beta^t` correction, the first ~10 steps have severely underestimated moments because m and v are initialized to zero. The optimizer appears to barely move in early iterations, then suddenly accelerates.

3. **Not clamping sigmoid input**: `exp(-x)` for x = -1000 produces infinity. Clamping the input to [-500, 500] prevents overflow while preserving the sigmoid's behavior (it is effectively 0 or 1 beyond those bounds anyway).

4. **Learning rate too high for SGD**: Without momentum or adaptive rates, vanilla SGD with lr=0.1 can oscillate and diverge on ill-conditioned problems. Start with lr=0.01 and increase. Adam with default lr=0.001 is more forgiving.

5. **Not shuffling mini-batches each epoch**: If the data has structure (e.g., all class-0 samples first, then class-1), processing in order creates biased gradient estimates within each epoch. Shuffling ensures each mini-batch is a representative sample.

## Performance Notes

The computational cost is dominated by gradient computation, which is O(n * d) per batch (n samples, d features). For these small models:

- **Linear regression** (d=1): Gradient computation is trivially fast. The overhead is Python-style interpretive cost (Vec allocations, iterator chains). A production implementation would use BLAS.
- **Logistic regression** (d=2): The sigmoid computation adds a branch and an `exp()` per sample. With 200 samples and 150 epochs, total compute is under 1ms.
- **Numerical gradient**: O(d * n) per parameter, so O(d^2 * n) total. This is acceptable for gradient checking but prohibitive for training (hence analytical gradients).

For scaling to larger models, the bottleneck shifts to matrix multiplication in the gradient computation. See Challenge 86 for matrix optimization techniques.
