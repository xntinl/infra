<!-- difficulty: advanced -->
<!-- category: machine-learning -->
<!-- languages: [rust] -->
<!-- concepts: [gradient-descent, sgd, momentum, adam-optimizer, numerical-differentiation, linear-regression, logistic-regression, loss-functions, convergence, learning-rate] -->
<!-- estimated_time: 10-14 hours -->
<!-- bloom_level: apply, evaluate -->
<!-- prerequisites: [rust-basics, calculus-derivatives, linear-algebra, matrix-operations, floating-point-arithmetic] -->

# Challenge 109: Gradient Descent Optimizer

## Languages

Rust (stable, latest edition)

## Prerequisites

- Understanding of derivatives: what a derivative represents, partial derivatives of multivariable functions
- Basic linear algebra: vectors, dot products, matrix-vector multiplication
- Familiarity with the concept of a loss function (a scalar measure of how wrong a prediction is)
- Rust structs, traits, enums, and closures (`Fn` trait for passing functions)
- Comfortable with f64 arithmetic and potential numerical issues (very small gradients, NaN propagation)

## Learning Objectives

- **Implement** three gradient descent variants (vanilla SGD, SGD with momentum, Adam) and understand what problem each one solves
- **Apply** numerical differentiation (finite differences) and analytical gradients to compute the direction of steepest descent
- **Evaluate** convergence behavior by comparing loss curves across optimizers and hyperparameter settings
- **Analyze** why momentum helps escape shallow local minima and why Adam adapts the learning rate per parameter
- **Design** a training loop that applies gradient updates to model parameters across multiple epochs with mini-batches

## The Challenge

Gradient descent is the engine behind all neural network training. The idea is simple: compute how wrong you are (loss), figure out which direction to move each parameter to reduce the loss (gradient), and take a small step in that direction. Repeat until the loss converges.

Implement three gradient descent optimizers from scratch and apply them to two classical ML problems: linear regression and logistic regression.

**Vanilla SGD** updates parameters by `w = w - lr * gradient`. It works but oscillates in ravines and converges slowly on ill-conditioned problems.

**SGD with Momentum** maintains a velocity vector that accumulates past gradients: `v = beta * v + gradient; w = w - lr * v`. The velocity acts like a ball rolling downhill -- it accelerates in consistent directions and dampens oscillations. Typical beta is 0.9.

**Adam** (Adaptive Moment Estimation) maintains both a first moment estimate (like momentum) and a second moment estimate (moving average of squared gradients). It adapts the learning rate per parameter: parameters with large gradients get smaller effective learning rates, and vice versa. This makes Adam robust to hyperparameter choices.

For computing gradients, implement two approaches: **numerical differentiation** using finite differences (perturb each parameter by epsilon, measure the change in loss) and **analytical gradients** (derive the gradient formula by hand for linear and logistic regression). Compare them to verify correctness -- they should agree to several decimal places.

Gradient checking is one of the most important debugging tools in machine learning. The idea is simple: the analytical gradient is a formula you derived by hand (fast but error-prone), and the numerical gradient is a brute-force computation (slow but almost certainly correct). If they agree to 5+ decimal places for every parameter, your analytical gradient is correct. If they disagree, your formula has a bug. Every major ML framework developer uses this technique.

The landscape of a loss function is a high-dimensional surface. Gradient descent walks downhill on this surface. Vanilla SGD follows the local slope, which can zigzag in narrow valleys. Momentum adds inertia, smoothing the path. Adam combines momentum with per-parameter adaptive learning rates, making it the default choice for most deep learning tasks because it is less sensitive to hyperparameter tuning.

The learning rate is the most important hyperparameter. Too large and the optimizer overshoots the minimum, oscillating or diverging. Too small and convergence is painfully slow. Learning rate scheduling addresses this by starting with a large rate (fast initial progress) and reducing it over time (precise final convergence). Step decay, exponential decay, and cosine annealing each offer different decay profiles suited to different training dynamics.

## Requirements

1. Implement numerical gradient computation via central finite differences: `df/dw_i = (f(w + eps*e_i) - f(w - eps*e_i)) / (2 * eps)` where `e_i` is the unit vector in dimension `i` and eps is typically 1e-5
2. Implement analytical gradient for linear regression: for loss `L = (1/2n) * sum((X*w + b - y)^2)`, the gradient is `dL/dw = (1/n) * X^T * (X*w + b - y)` and `dL/db = (1/n) * sum(X*w + b - y)`
3. Implement analytical gradient for logistic regression: sigmoid output `p = 1/(1 + exp(-z))`, binary cross-entropy loss `L = -(1/n) * sum(y*ln(p) + (1-y)*ln(1-p))`, gradient `dL/dw = (1/n) * X^T * (p - y)`
4. Implement gradient verification: compare numerical and analytical gradients element-wise, assert relative difference is below 1e-5 for each parameter
5. Implement vanilla SGD optimizer: `w = w - learning_rate * gradient`. Configurable learning rate
6. Implement SGD with momentum: maintain velocity `v`, update `v = momentum * v + gradient`, then `w = w - learning_rate * v`. Configurable momentum coefficient (default 0.9)
7. Implement Adam optimizer: maintain first moment `m` and second moment `v` (both vectors), with bias correction. Update rule: `m = beta1*m + (1-beta1)*g`, `v = beta2*v + (1-beta2)*g^2`, `m_hat = m/(1-beta1^t)`, `v_hat = v/(1-beta2^t)`, `w = w - lr * m_hat / (sqrt(v_hat) + eps)`. Default: beta1=0.9, beta2=0.999, eps=1e-8
8. Implement an `Optimizer` trait with `fn step(&mut self, params: &mut Vec<f64>, gradients: &[f64])` so different optimizers are interchangeable
9. Implement mini-batch iteration: shuffle the dataset, split into batches of configurable size, compute gradient on each batch. One pass through all batches is one epoch
10. Implement a training loop: for each epoch, for each mini-batch, compute gradients, call optimizer step, record loss. Return the loss history as `Vec<f64>`
11. Generate synthetic datasets: linear data with Gaussian noise for linear regression, two separable Gaussian clusters for logistic regression. Use a seedable PRNG (xorshift) for reproducibility
12. Implement learning rate scheduling: step decay (halve every N epochs), exponential decay (`lr * gamma^epoch`), and cosine annealing (`lr * 0.5 * (1 + cos(pi * epoch / max_epochs))`)
13. Implement loss curve visualization: output a text-based plot of loss vs. epoch to stdout using ASCII characters. Mark the y-axis with loss values, x-axis with epoch numbers. Plot multiple optimizers on the same chart using different characters (e.g., `S` for SGD, `M` for momentum, `A` for Adam)
14. Report final loss, number of epochs to converge (loss below threshold), and parameter values for each optimizer

## Hints

1. Central finite differences (`(f(x+h) - f(x-h)) / 2h`) are more accurate than forward
   differences (`(f(x+h) - f(x)) / h`) because the error term is O(h^2) instead of O(h). Use
   h = 1e-5 for a good balance between truncation error (large h) and floating-point
   cancellation error (small h). Too small (1e-15) and the subtraction in the numerator loses
   all significant digits.

2. Adam's bias correction matters in the early steps. Without it, the first moment is biased
   toward zero because `m` is initialized to zero and only partially updated. The correction
   `m_hat = m / (1 - beta1^t)` compensates. At `t=1` with `beta1=0.9`, the correction factor
   is `1/0.1 = 10x`. By `t=100`, `beta1^100` is negligible and the correction is ~1.0x.

3. For logistic regression, clamp the sigmoid output to `[1e-15, 1-1e-15]` before computing
   `ln(p)` and `ln(1-p)`. Without clamping, a perfect prediction (p=1.0 for y=1) produces
   `ln(1-1.0) = ln(0) = -infinity`, which poisons the entire loss computation.

4. When verifying numerical vs. analytical gradients, use relative error:
   `|numerical - analytical| / max(|numerical|, |analytical|, 1e-7)`. The denominator avoids
   division by zero when both gradients are near zero. A relative error below 1e-5 is
   acceptable for most parameters.

5. The mini-batch size creates a bias-variance trade-off in gradient estimation. A batch of 1
   sample gives a very noisy gradient estimate (high variance, low computation). The full
   dataset gives the exact gradient (zero variance, high computation). Typical batch sizes
   of 32-128 balance noise (which helps escape shallow local minima) with stability
   (convergence to a reasonable solution). To implement mini-batches, shuffle the dataset at
   the start of each epoch, then split indices into chunks of `batch_size`. Compute the
   gradient on each chunk separately and apply the optimizer step after each chunk.

## Acceptance Criteria

- [ ] Numerical gradient matches analytical gradient for linear regression (relative error < 1e-5 per parameter)
- [ ] Numerical gradient matches analytical gradient for logistic regression (relative error < 1e-5 per parameter)
- [ ] Vanilla SGD converges on linear regression with appropriate learning rate (loss decreases monotonically)
- [ ] SGD with momentum converges faster than vanilla SGD (fewer epochs to reach the same loss threshold)
- [ ] Adam converges on both linear and logistic regression without manual learning rate tuning
- [ ] Adam with default hyperparameters (lr=0.001, beta1=0.9, beta2=0.999) converges on all test problems
- [ ] Logistic regression achieves >95% classification accuracy on the synthetic two-cluster dataset
- [ ] Linear regression recovers the true slope and intercept within 5% relative error on the synthetic dataset
- [ ] Mini-batch training produces the same converged result as full-batch (within tolerance) for small datasets
- [ ] Learning rate scheduling (step decay, exponential, cosine) each produce measurably different loss curves
- [ ] ASCII loss plot displays loss vs. epoch for all three optimizers with distinguishable curves
- [ ] Training is reproducible: same seed produces identical loss curves across runs
- [ ] Cosine annealing reaches near-zero learning rate at the final epoch and starts at the full rate
- [ ] Step decay reduces the learning rate by the correct factor at the configured step boundary
- [ ] The optimizer trait allows swapping SGD, Momentum, and Adam without changing the training loop
- [ ] No dependencies beyond `std`
- [ ] All tests pass with `cargo test`

## Research Resources

- [An overview of gradient descent optimization algorithms (Ruder, 2016)](https://arxiv.org/abs/1609.04747) -- comprehensive survey of SGD, momentum, Adam, and other optimizers with convergence analysis
- [Adam: A Method for Stochastic Optimization (Kingma & Ba, 2015)](https://arxiv.org/abs/1412.6980) -- the original Adam paper with the full algorithm and default hyperparameters
- [Neural Networks and Deep Learning, Chapter 1 (Michael Nielsen)](http://neuralnetworksanddeeplearning.com/chap1.html) -- gradient descent explained with MNIST context
- [CS231n: Optimization (Stanford)](https://cs231n.github.io/optimization-1/) -- gradient computation, numerical gradients, and gradient checking techniques
- [Numerical Differentiation -- Wikipedia](https://en.wikipedia.org/wiki/Numerical_differentiation) -- finite difference formulas and error analysis
- [Why Momentum Really Works (Distill, Goh 2017)](https://distill.pub/2017/momentum/) -- interactive visualizations of momentum dynamics on loss surfaces
- [Logistic Regression -- Wikipedia](https://en.wikipedia.org/wiki/Logistic_regression) -- mathematical formulation, gradient derivation, and the connection to cross-entropy loss
- [Linear Regression -- Wikipedia](https://en.wikipedia.org/wiki/Linear_regression) -- least squares formulation and the closed-form solution for comparison
- [Cosine Annealing Learning Rate Schedule (Loshchilov & Hutter, 2017)](https://arxiv.org/abs/1608.03983) -- SGDR paper introducing cosine annealing with warm restarts
- [Binary Cross-Entropy Loss -- Wikipedia](https://en.wikipedia.org/wiki/Cross-entropy#Cross-entropy_loss_function_and_logistic_regression) -- derivation of BCE loss and its gradient for logistic regression
