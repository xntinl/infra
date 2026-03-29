<!-- difficulty: advanced -->
<!-- category: machine-learning -->
<!-- languages: [rust] -->
<!-- concepts: [convolution, cnn, max-pooling, batch-normalization, im2col, padding, stride, multi-channel, tensor-operations, image-processing] -->
<!-- estimated_time: 12-16 hours -->
<!-- bloom_level: apply, analyze -->
<!-- prerequisites: [rust-basics, matrix-operations, linear-algebra, neural-network-basics, image-representation] -->

# Challenge 120: Convolution Neural Layer

## Languages

Rust (stable, latest edition)

## Prerequisites

- Matrix and tensor operations: 2D and 3D array indexing, element-wise operations
- Understanding of what convolution means in signal processing (sliding a kernel over input)
- Familiarity with how images are represented: pixels as values, channels (grayscale = 1 channel, RGB = 3 channels)
- Rust multi-dimensional array handling using flat Vec with stride-based indexing
- Basic understanding of neural network layers (input, weights, output, activation)

## Learning Objectives

- **Implement** 2D convolution with multi-channel inputs and outputs, configurable stride and padding
- **Apply** the im2col optimization to convert convolution into efficient matrix multiplication
- **Design** a pooling layer (max pool) that reduces spatial dimensions while preserving the strongest features
- **Implement** batch normalization as a normalization layer that stabilizes intermediate activations
- **Analyze** how kernel size, stride, padding, and pooling interact to determine output spatial dimensions

## The Challenge

Convolutional neural networks (CNNs) revolutionized computer vision because convolution exploits spatial structure: a feature detector (kernel) is slid across the image, producing a feature map that indicates where that feature appears. Stacking multiple kernels detects multiple features. Stacking multiple layers builds a hierarchy from edges to textures to object parts.

Implement a 2D convolution layer from scratch. The core operation slides a small kernel (e.g., 3x3) across a 2D input, computing the dot product at each position. For multi-channel inputs (RGB), the kernel has the same depth as the input, and the dot product spans all channels. Multiple output channels means multiple kernels, each producing one output feature map.

The naive implementation is simple but slow: four nested loops (output height, output width, kernel height, kernel width, times input channels). The im2col trick transforms convolution into a single large matrix multiplication by rearranging input patches into columns of a matrix. This is how every major ML framework (PyTorch, TensorFlow) implements convolution internally -- matrix multiplication is highly optimized on every platform.

Also implement max pooling (take the maximum value in each non-overlapping window, typically 2x2, reducing spatial dimensions by half) and batch normalization (normalize activations to zero mean and unit variance, then apply a learnable scale and shift). Together, these three operations form the building blocks of every CNN architecture from LeNet to ResNet.

The im2col trick is worth understanding in detail. Consider a 5x5 input with a 3x3 kernel and stride 1. The output is 3x3 (9 positions). At each position, the kernel sees a 3x3 patch of the input. im2col takes each of these 9 patches, flattens them into 9-element column vectors, and stacks them into a (9 x 9) matrix. Then convolution becomes: `weights_reshaped (C_out x 9) * im2col_matrix (9 x 9) = output_reshaped (C_out x 9)`. For multi-channel inputs, the patch includes all channels, so the column length is `C_in * kH * kW`. This transformation trades memory (the im2col matrix duplicates input data) for compute efficiency (matrix multiplication is heavily optimized).

The output spatial dimension formula -- `floor((input + 2*padding - kernel) / stride) + 1` -- is the single most important formula in CNN implementation. Every dimension error traces back to getting this wrong. Validate it at layer construction time, not during computation.

Batch normalization stabilizes training by normalizing intermediate activations. Without it, each layer sees a shifting distribution of inputs as earlier layers update their weights -- the "internal covariate shift" problem. During inference (this challenge), batch normalization uses pre-computed running mean and variance rather than computing statistics from the current batch.

## Requirements

1. Implement a `Tensor4D` type representing a 4D tensor with dimensions (batch, channels, height, width). Store data in a flat `Vec<f64>` with row-major ordering. Implement index computation: `index(b, c, h, w) = ((b * C + c) * H + h) * W + w`
2. Implement naive 2D convolution: for each output position, compute the dot product of the kernel with the corresponding input patch. Support multi-channel input (C_in) and multi-channel output (C_out). Kernel shape is (C_out, C_in, kH, kW)
3. Implement configurable stride: the kernel moves by `stride` pixels instead of 1. Output size becomes `(H - kH + 2*padding) / stride + 1`
4. Implement zero-padding: pad the input with zeros on all sides by the specified amount. "Same" padding preserves spatial dimensions when stride=1: `padding = (kH - 1) / 2`
5. Implement a bias vector (one bias per output channel) added to the convolution output
6. Implement im2col: transform each receptive field (input patch that one output element sees) into a column of a 2D matrix. The resulting matrix has shape (C_in * kH * kW, out_H * out_W). Then convolution becomes: `output = weights_matrix * im2col_matrix + bias`, where weights_matrix has shape (C_out, C_in * kH * kW)
7. Implement col2im: the inverse of im2col, reconstructing the spatial layout from columnar form. This is needed for the backward pass but useful for verification: `col2im(im2col(input)) == input` when stride=1 and full overlap
8. Implement max pooling: for each non-overlapping window of size (pool_H, pool_W), output the maximum value. Default is 2x2. Also store the indices of maxima (needed for backward pass in training). Output dimensions: `(H / pool_H, W / pool_W)`
9. Implement average pooling as an alternative: output the mean of each window instead of the maximum
10. Implement batch normalization in inference mode: `y = gamma * (x - mean) / sqrt(var + eps) + beta`, where mean and var are running statistics (not batch statistics), and gamma/beta are learned parameters. Use eps=1e-5
11. Implement ReLU activation as a post-convolution step (element-wise max(0, x) on the tensor)
12. Implement a `ConvBlock` that chains: Conv2D -> BatchNorm -> ReLU -> MaxPool. Configurable: which components are included, kernel size, stride, padding, pool size
13. Implement output dimension computation: given input (B, C, H, W), kernel size, stride, padding, and pool size, compute the output dimensions without running the convolution. Print a layer summary similar to PyTorch's model summary
14. Process both grayscale (1 channel) and RGB (3 channel) images. Load raw pixel data from a flat binary file or from MNIST IDX format
15. Verify correctness: for a known 3x3 input and 2x2 kernel, compute convolution by hand and assert the code matches

## Hints

1. Index arithmetic is the main source of bugs. For a tensor (B, C, H, W) stored flat, the
   element at (b, c, h, w) is at offset `((b * C + c) * H + h) * W + w`. Get this wrong and
   every operation produces garbage. Write a test that creates a small tensor (1, 1, 3, 3)
   with values 1-9, verify that get(0, 0, 1, 1) returns 5 (center element), and that iterating
   over all indices in order matches the flat storage.

2. im2col rearranges input patches into columns. For a (1, 1, 4, 4) input with a 3x3 kernel
   and stride 1, the output positions form a 2x2 grid (4 positions). Each position sees a
   3x3=9 element patch. The im2col matrix is therefore 9 rows by 4 columns. Each column is
   one flattened patch. For multi-channel inputs, the patch includes all channels:
   (C_in * kH * kW) rows. Verify dimensions before implementing the data copy.

3. The output spatial dimension formula is: `floor((input + 2*padding - kernel) / stride) + 1`.
   This must be an integer. If it is not, your combination of input size, kernel size, padding,
   and stride is invalid. Check this at construction time, not during the convolution, to
   fail fast with a clear error message.

4. Batch normalization has two modes: training (compute mean/var from the current batch) and
   inference (use running mean/var accumulated during training). Implement inference mode only.
   The formula is straightforward: normalize, then scale and shift. Keep gamma and beta as
   learnable parameters (load from file or initialize to 1.0 and 0.0 respectively).

5. Max pooling is conceptually simple but has a subtlety: for the backward pass (training),
   you need to know which input element produced the maximum in each window. Store these
   indices during the forward pass. For inference only, you can skip index storage, but
   including it makes the layer reusable in a training context. Average pooling does not need
   indices because the gradient distributes equally to all elements in the window.

6. When testing, always start with a 1-channel, 1-batch input to eliminate batch and channel
   bugs. Then test 1-batch multi-channel. Then multi-batch multi-channel. Each dimension
   adds a new way for indexing to go wrong. A single off-by-one in the channel loop can
   produce output that looks almost correct but is subtly shifted.

## Acceptance Criteria

- [ ] Convolution of a known 3x3 input with a known 2x2 kernel matches hand-computed result
- [ ] Multi-channel convolution: (1, 3, 5, 5) input with (8, 3, 3, 3) kernel produces output shape (1, 8, 3, 3) with stride=1, no padding
- [ ] Same padding with stride=1 preserves spatial dimensions: (1, 1, 28, 28) with 3x3 kernel and padding=1 produces (1, 1, 28, 28)
- [ ] Stride=2 halves spatial dimensions: (1, 1, 28, 28) with 3x3 kernel, padding=1, stride=2 produces (1, 1, 14, 14)
- [ ] im2col produces a matrix with correct dimensions: (C_in * kH * kW) rows and (out_H * out_W) columns
- [ ] im2col convolution produces identical results to naive convolution for the same input and kernel
- [ ] im2col is at least 2x faster than naive convolution on a (1, 3, 28, 28) input with (16, 3, 3, 3) kernel (measure with `std::time::Instant`)
- [ ] Max pooling with 2x2 window halves spatial dimensions: (1, 1, 28, 28) -> (1, 1, 14, 14)
- [ ] Max pooling correctly selects the maximum value in each window
- [ ] Batch normalization with gamma=1, beta=0, mean=0, var=1 is identity
- [ ] A ConvBlock (conv -> batchnorm -> relu -> maxpool) chains correctly with matching intermediate dimensions
- [ ] Dimension summary prints correct shapes for a multi-layer CNN applied to 28x28 input
- [ ] Grayscale and RGB inputs are both handled correctly (1 and 3 input channels)
- [ ] col2im(im2col(input)) reconstructs the original input for stride=1 non-overlapping patches
- [ ] Average pooling produces the correct mean value for each window
- [ ] Output dimension formula correctly rejects invalid configurations (e.g., kernel larger than padded input)
- [ ] Pipeline processes a batch of 10 images (10, 1, 28, 28) through two ConvBlocks without shape errors
- [ ] No dependencies beyond `std`
- [ ] All tests pass with `cargo test`

## Research Resources

- [CS231n: Convolutional Neural Networks (Stanford)](https://cs231n.github.io/convolutional-networks/) -- visual explanation of convolution, pooling, stride, padding, and output dimension formulas
- [A guide to convolution arithmetic for deep learning (Dumoulin & Visin, 2016)](https://arxiv.org/abs/1603.07285) -- animated GIFs and comprehensive reference for all convolution configurations
- [im2col: The Trick Behind Convolution (Petewarden's blog)](https://petewarden.com/2015/04/20/why-gemm-is-at-the-heart-of-deep-learning/) -- explains why matrix multiplication (GEMM) is the core of CNN computation
- [Batch Normalization: Accelerating Deep Network Training (Ioffe & Szegedy, 2015)](https://arxiv.org/abs/1502.03167) -- the original batch normalization paper
- [High Performance Convolutional Neural Networks (Chetlur et al., cuDNN paper)](https://arxiv.org/abs/1410.0759) -- how convolution is implemented in production GPU libraries (im2col, FFT, Winograd)
- [MNIST Database of Handwritten Digits (Yann LeCun)](http://yann.lecun.com/exdb/mnist/) -- test images for verifying the pipeline on real data
- [Max Pooling -- Wikipedia](https://en.wikipedia.org/wiki/Convolutional_neural_network#Pooling_layers) -- overview of pooling operations and their role in CNN architectures
