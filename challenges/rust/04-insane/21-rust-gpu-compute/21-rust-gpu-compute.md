# 21. Rust GPU Compute

**Difficulty**: Insane

## The Challenge

Write a non-trivial parallel algorithm that runs entirely on the GPU, with the shader
written in Rust — not WGSL, not GLSL, not HLSL. You will use rust-gpu to compile
Rust code to SPIR-V, then load that SPIR-V into a wgpu compute pipeline and dispatch
it from a host application also written in Rust. The end result is a single language
across both CPU and GPU, sharing types and logic between host and device.

Choose one of the following algorithms (or propose your own of equivalent complexity):

**Option A: Tiled Matrix Multiplication.** Implement GEMM (general matrix multiply)
for square NxN matrices using shared workgroup memory to tile the computation. This
is the canonical GPU compute benchmark and forces you to understand workgroup sizes,
shared memory, and synchronization barriers.

**Option B: N-Body Particle Simulation.** Implement a gravitational or electrostatic
particle simulation where every particle interacts with every other particle (O(N^2)
brute force). Each workgroup processes a tile of particles, accumulating forces using
shared memory. Visualize the result by reading positions back to the CPU each frame.

**Option C: Image Convolution Pipeline.** Implement a multi-pass image filter (e.g.,
Gaussian blur followed by Sobel edge detection) where each pass is a separate compute
dispatch. The output buffer of one pass becomes the input of the next, requiring
correct buffer aliasing and synchronization.

This matters because GPU compute is becoming the dominant programming model for
everything from machine learning to physics simulation. Rust-gpu eliminates the
language boundary between host and device code, enabling shared type definitions,
compile-time safety on the GPU, and standard Rust tooling. The project recently
transitioned to community ownership under the Rust-GPU GitHub organization and
shifted its primary focus to GPU compute and GPGPU programming.

## Acceptance Criteria

- [ ] Shader code is written in Rust and compiled to SPIR-V via `spirv-builder` in a `build.rs`
- [ ] The shader crate uses `#![no_std]` and the `spirv-std` crate for GPU intrinsics
- [ ] Entry point is annotated with `#[spirv(compute(threads(X, Y, Z)))]` with a justified workgroup size
- [ ] Host code uses wgpu to create the compute pipeline, bind groups, buffers, and dispatch workgroups
- [ ] Storage buffers are correctly created with `STORAGE | COPY_SRC` usage flags
- [ ] A staging buffer with `MAP_READ | COPY_DST` is used for GPU-to-CPU readback
- [ ] Workgroup dispatch count is calculated correctly: `(data_size + workgroup_size - 1) / workgroup_size`
- [ ] The algorithm produces numerically correct results verified against a CPU reference implementation
- [ ] Shared workgroup memory is used (via `spirv_std::arch::workgroup_memory`) to reduce global memory traffic
- [ ] Performance is measured: time the GPU dispatch (excluding buffer transfer) and compare to a naive CPU implementation
- [ ] The shader compiles without `unsafe` in the shader crate (except for required GPU intrinsics)
- [ ] Types shared between host and device are defined in a common `shared` crate with `#[repr(C)]`

## Background

Traditional GPU programming requires writing shaders in a completely different
language from the host code. rust-gpu changes this by acting as a backend for the
Rust compiler (`rustc_codegen_spirv`) that emits SPIR-V instead of machine code.
The `spirv-builder` crate orchestrates this: it invokes a nested `cargo build` of
your shader crate using the SPIR-V backend and produces a `.spv` file that you
load at runtime.

On the host side, wgpu provides a cross-platform GPU abstraction based on the WebGPU
specification. It runs on Vulkan, Metal, DX12, and WebGPU. You create a
`ComputePipeline` from the SPIR-V module, bind your data buffers via `BindGroup`s,
and submit a `CommandEncoder` that dispatches workgroups.

The GPU memory model is fundamentally different from the CPU. Device-local memory is
fast for the GPU but invisible to the CPU. Host-visible memory can be mapped for CPU
access but is slower for GPU computation. Staging buffers bridge this gap: you upload
data to a staging buffer, copy it to device-local storage, dispatch, then copy results
back to a staging buffer for readback.

## Architecture Hints

1. Structure your workspace as three crates: `shared` (types, `no_std` compatible),
   `shader` (the GPU code, depends on `spirv-std` and `shared`), and `host` (the
   CPU code, depends on `wgpu` and `shared`). The `host` crate's `build.rs` uses
   `spirv-builder` to compile the `shader` crate.

2. For tiled matrix multiplication, the workgroup size should match your tile size
   (e.g., 16x16 = 256 threads). Each thread loads one element into shared memory,
   calls `workgroup_barrier()`, then computes a partial dot product from the shared
   tile. The tile slides across the K dimension.

3. Buffer readback in wgpu is asynchronous. After `copy_buffer_to_buffer`, you must
   call `buffer.slice(..).map_async(MapMode::Read, callback)` and then
   `device.poll(Maintain::Wait)` before accessing the mapped data.

4. The `spirv-builder` produces a path to the compiled `.spv` file. Use
   `include_bytes!` or `wgpu::ShaderModuleDescriptor` with
   `ShaderSource::SpirV` to load it. The entry point name in wgpu must match the
   Rust function name in your shader crate.

5. Debugging GPU code is hard. Start with a trivial shader (double every element in
   a buffer) and verify the full pipeline before implementing the actual algorithm.
   Use `wgpu`'s validation layer and consider `renderdoc` for GPU-side debugging.

## Starting Points

- **rust-gpu repository**: [github.com/Rust-GPU/rust-gpu](https://github.com/Rust-GPU/rust-gpu) — study
  `examples/` for shader crate structure. The project transitioned from Embark Studios
  to community ownership under the Rust-GPU organization.
- **spirv-builder API**: [docs.rs/spirv-builder](https://docs.rs/spirv-builder) — `SpirvBuilder::new("my_shaders", "spirv-unknown-vulkan1.1")` in your `build.rs`.
  The entry point attribute `#[spirv(compute(threads(32, 16, 97)))]` defines local
  workgroup dimensions.
- **Minimal rust-gpu + wgpu compute example**: [github.com/andrusha/rust-gpu-wgpu-compute-minimal](https://github.com/andrusha/rust-gpu-wgpu-compute-minimal) — a working
  integration showing the full pipeline from Rust shader to wgpu dispatch.
- **wgpu compute examples**: [github.com/gfx-rs/wgpu-rs/blob/master/examples/hello-compute/main.rs](https://github.com/gfx-rs/wgpu-rs/blob/master/examples/hello-compute/main.rs) — the
  canonical wgpu compute example (uses WGSL, but the host-side API is identical).
- **spirv-std intrinsics**: Study `spirv_std::arch` for barrier functions, workgroup
  memory, and built-in variables (`global_invocation_id`, `local_invocation_id`,
  `workgroup_id`).
- **Rust-GPU blog — "Rust running on every GPU"**: [rust-gpu.github.io/blog/2025/07/25/rust-on-every-gpu/](https://rust-gpu.github.io/blog/2025/07/25/rust-on-every-gpu/) — covers the
  SPIR-V to naga translation path that enables running on Metal, DX12, and WebGPU.
- **Rust CUDA project updates**: [rust-gpu.github.io/blog/2025/08/11/rust-cuda-update/](https://rust-gpu.github.io/blog/2025/08/11/rust-cuda-update/) — the parallel
  effort for NVIDIA GPUs, useful for understanding the broader Rust GPU ecosystem.

## Going Further

- Implement a comparison backend: write the same algorithm in WGSL and benchmark
  against your Rust SPIR-V shader. Measure compilation time, dispatch performance,
  and binary size.
- Add a CPU fallback path using `rayon` for machines without GPU support. Use the
  `shared` crate's types so both paths operate on identical data layouts.
- Implement double buffering for the particle simulation: while the GPU computes
  frame N+1, the CPU reads back frame N. Measure the latency improvement.
- Explore the Rust CUDA backend ([github.com/Rust-GPU/Rust-CUDA](https://github.com/Rust-GPU/Rust-CUDA)) and port your
  shader to run on NVIDIA hardware via PTX. Compare performance against the
  Vulkan/SPIR-V path.
- Integrate `rust-gpu`'s SPIR-T intermediate representation
  ([github.com/Rust-GPU/spirt](https://github.com/Rust-GPU/spirt)) for shader
  optimization passes before submitting to the driver.

## Resources

**Source Code**
- [Rust-GPU/rust-gpu](https://github.com/Rust-GPU/rust-gpu) — the compiler backend, `spirv-builder`, and `spirv-std`
- [Rust-GPU/Rust-CUDA](https://github.com/Rust-GPU/Rust-CUDA) — CUDA/PTX backend for NVIDIA GPUs
- [Rust-GPU/spirt](https://github.com/Rust-GPU/spirt) — SPIR-T shader IR for transformation and optimization
- [gfx-rs/wgpu](https://github.com/gfx-rs/wgpu) — the WebGPU implementation in Rust
- [andrusha/rust-gpu-wgpu-compute-minimal](https://github.com/andrusha/rust-gpu-wgpu-compute-minimal) — minimal integration example

**Documentation**
- [wgpu docs](https://docs.rs/wgpu/latest/wgpu/) — `ComputePipeline`, `ComputePass`, `BindGroup`, buffer usage flags
- [spirv-builder docs](https://docs.rs/spirv-builder) — build-time shader compilation API
- [Rust GPU Dev Guide: Writing Shader Crates](https://rust-gpu.github.io/rust-gpu/book/writing-shader-crates.html) — attribute syntax, entry points, limitations
- [Learn WGPU](https://sotrh.github.io/learn-wgpu/) — comprehensive wgpu tutorial (graphics-focused but covers core API)

**Blog Posts**
- [Rust GPU: Transition to Community Ownership](https://rust-gpu.github.io/blog/transition-announcement/) — the shift from Embark Studios to community-driven development
- [Rust running on every GPU](https://rust-gpu.github.io/blog/2025/07/25/rust-on-every-gpu/) — cross-platform execution via naga translation
- [High Performance GPGPU with Rust and wgpu](https://dev.to/jaysmito101/high-performance-gpgpu-with-rust-and-wgpu-4l9i) — practical compute pipeline walkthrough
- [Rust wgpu Compute: Minimal Example, Buffer Readback, and Performance Tips](https://tillcode.com/rust-wgpu-compute-minimal-example-buffer-readback-and-performance-tips/) — common pitfalls with buffer flags and dispatch sizing
- [Rust for GPU Programming: wgpu and rust-gpu Complete Guide](https://tillcode.com/rust-for-gpu-programming-wgpu-and-rust-gpu/) — end-to-end guide covering both crates
- [Computing Image Filters with wgpu-rs](https://blog.redwarp.app/image-filters/) — image convolution pipeline implementation
- [Applying 5 Million Pixel Updates per Second with Rust and wgpu](https://maxisom.me/posts/applying-5-million-pixel-updates-per-second) — real-world GPU compute performance

**Specifications**
- [WebGPU Specification](https://www.w3.org/TR/webgpu/) — the API that wgpu implements
- [SPIR-V Specification](https://registry.khronos.org/SPIR-V/specs/unified1/SPIRV.html) — the shader binary format rust-gpu targets
- [Vulkan Compute](https://www.khronos.org/registry/vulkan/specs/1.3-extensions/html/vkspec.html#compute) — the underlying dispatch model
