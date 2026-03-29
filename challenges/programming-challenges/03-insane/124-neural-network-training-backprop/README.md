<!-- difficulty: insane -->
<!-- category: machine-learning -->
<!-- languages: [rust] -->
<!-- concepts: [backpropagation, automatic-differentiation, computation-graph, chain-rule, gradient-flow, cross-entropy, mse, mini-batch, learning-rate-scheduling, mnist-training] -->
<!-- estimated_time: 20-30 hours -->
<!-- bloom_level: create, evaluate -->
<!-- prerequisites: [linear-algebra, matrix-multiplication, calculus-chain-rule, forward-pass-inference, activation-functions, gradient-descent, rust-traits, rust-generics, file-io] -->

# Challenge 124: Neural Network Training with Backpropagation

## Languages

Rust (stable, latest edition)

## Prerequisites

- Matrix multiplication, transposition, and element-wise operations on 2D arrays
- Calculus: chain rule for composite functions, partial derivatives
- Forward pass mechanics: layers, weights, biases, activation functions
- Gradient descent: parameter updates using computed gradients
- Rust ownership model, trait objects or enums for polymorphic layer types
- File I/O for loading MNIST data and saving model checkpoints

## Learning Objectives

- **Create** a complete neural network training system with forward pass, loss computation, and backward pass
- **Implement** backpropagation using the chain rule to propagate gradients from loss to every parameter in the network
- **Design** a computation graph where each operation stores the information needed for its backward pass
- **Evaluate** training effectiveness by tracking loss convergence and classification accuracy on MNIST
- **Analyze** gradient flow pathology: vanishing gradients with sigmoid, exploding gradients without proper initialization

## The Challenge

Training a neural network means finding the weights that minimize a loss function. Backpropagation computes the gradient of the loss with respect to every weight in the network by applying the chain rule of calculus backward through the computation graph. Each layer must compute two things during the backward pass: the gradient of the loss with respect to its inputs (to pass to the previous layer) and the gradient with respect to its own parameters (to update its weights).

Build a complete neural network training system from scratch. No autograd libraries, no ML frameworks. Implement forward pass, loss computation, backward pass, and parameter updates. Train on MNIST and achieve at least 95% accuracy on the test set.

The computation flows like this: input data enters the network, passes forward through layers (matrix multiply, add bias, apply activation), reaches the loss function, then gradients flow backward through every layer in reverse order. Each layer's backward method receives the gradient from the layer above and computes: (1) the gradient to pass down, and (2) the gradient for its own weights and biases.

This is the foundation of all deep learning. Every framework (PyTorch, TensorFlow, JAX) implements exactly this mechanism, just with GPU acceleration and automatic graph construction. Building it yourself reveals every detail that frameworks hide.

## Requirements

1. Implement a `Layer` trait with `forward(&mut self, input: &Matrix) -> Matrix` and `backward(&mut self, grad_output: &Matrix) -> Matrix`. Forward stores intermediate values needed by backward. Backward returns the gradient with respect to the layer's input
2. Implement `DenseLayer`: forward computes `output = input * W^T + bias`. Backward computes `grad_input = grad_output * W`, `grad_W = grad_output^T * input`, `grad_bias = sum(grad_output, axis=0)`. Store input during forward for use in backward
3. Implement activation layers (ReLU, Sigmoid, Softmax) as separate layers with their own backward methods. ReLU backward: multiply gradient by 0 where input was negative, 1 where positive. Sigmoid backward: `grad * sigmoid_output * (1 - sigmoid_output)`
4. Implement MSE loss: `L = (1/2n) * sum((pred - target)^2)`, gradient: `dL/dpred = (1/n) * (pred - target)`
5. Implement cross-entropy loss with softmax: `L = -(1/n) * sum(target * ln(pred))`. When softmax is the last layer and cross-entropy is the loss, the combined gradient simplifies to `pred - target` (one-hot encoded). Implement this fused version for numerical stability
6. Implement a `Network` struct that owns an ordered list of layers. Forward passes input through all layers. Backward passes gradient through all layers in reverse. Each layer accumulates parameter gradients
7. Implement the training loop: for each epoch, shuffle data, iterate over mini-batches, run forward pass, compute loss, run backward pass, update parameters with the optimizer, record metrics
8. Implement SGD and Adam optimizers that operate on all network parameters. Each layer exposes its parameters and accumulated gradients through a trait method
9. Implement one-hot encoding for MNIST labels: label 3 becomes [0,0,0,1,0,0,0,0,0,0]
10. Implement learning rate scheduling: step decay and cosine annealing
11. Implement gradient clipping: cap the norm of the gradient vector to a maximum value to prevent exploding gradients
12. Implement model checkpointing: save all weights and biases to a binary file after each epoch, load the best model (lowest validation loss) at the end of training
13. Train a network with architecture 784-128-64-10 (two hidden layers with ReLU, output with softmax+cross-entropy) on MNIST to >95% test accuracy
14. Track and print per-epoch: training loss, training accuracy, test loss, test accuracy, learning rate, epoch time

## Acceptance Criteria

- [ ] Forward pass of a 2-layer network with known weights produces the expected output (verified by hand)
- [ ] Backward pass gradient for DenseLayer matches numerical gradient (finite differences) within relative error 1e-5
- [ ] Backward pass gradient for ReLU matches numerical gradient within relative error 1e-5
- [ ] Backward pass gradient for sigmoid matches numerical gradient within relative error 1e-5
- [ ] Cross-entropy + softmax fused gradient matches numerical gradient within 1e-5
- [ ] MSE loss gradient matches numerical gradient within 1e-5
- [ ] Training on MNIST achieves >95% test accuracy within 20 epochs
- [ ] Training loss decreases monotonically (averaged over epochs) for the first 10 epochs
- [ ] Gradient clipping prevents NaN/infinity in gradients when tested with a large learning rate
- [ ] Model checkpoint can be saved and loaded, producing identical inference results
- [ ] Adam optimizer converges faster than vanilla SGD (fewer epochs to 90% accuracy)
- [ ] No dependencies beyond `std`

## Research Resources

- [Backpropagation -- Wikipedia](https://en.wikipedia.org/wiki/Backpropagation) -- mathematical derivation of the backpropagation algorithm
- [Neural Networks and Deep Learning, Chapter 2 (Michael Nielsen)](http://neuralnetworksanddeeplearning.com/chap2.html) -- the four fundamental equations of backpropagation with clear derivations
- [CS231n: Backpropagation (Stanford)](https://cs231n.github.io/optimization-2/) -- computational graph perspective, chain rule intuition, and gradient patterns
- [3Blue1Brown: Backpropagation Calculus](https://www.youtube.com/watch?v=tIeHLnjs5U8) -- visual derivation of backpropagation through a simple network
- [Adam: A Method for Stochastic Optimization (Kingma & Ba, 2015)](https://arxiv.org/abs/1412.6980) -- the Adam optimizer algorithm
- [MNIST Database (Yann LeCun)](http://yann.lecun.com/exdb/mnist/) -- training and test data in IDX format
- [Calculus on Computational Graphs: Backpropagation (Colah, 2015)](https://colah.github.io/posts/2015-08-Backprop/) -- intuitive explanation of reverse-mode automatic differentiation
