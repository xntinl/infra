# 144. 3D Software Rasterizer

<!--
difficulty: insane
category: game-development-and-graphics
languages: [rust]
concepts: [3d-rasterization, vertex-transformation, z-buffer, perspective-projection, texture-mapping, shading, backface-culling, frustum-clipping, obj-loading]
estimated_time: 25-40 hours
bloom_level: create
prerequisites: [linear-algebra-matrices, 3d-coordinate-systems, homogeneous-coordinates, trigonometry, image-output-formats, rust-file-io]
-->

## Languages

- Rust (stable)

## Prerequisites

- 4x4 matrix math: multiplication, transpose, inverse, transformation construction
- Homogeneous coordinates and perspective division
- 3D coordinate systems: model space, world space, view space, clip space, screen space
- Trigonometry for rotation matrices and camera orientation
- PPM/PNG image writing for frame output
- Rust file I/O and string parsing (for OBJ model loading)

## Learning Objectives

- **Create** a complete 3D rasterization pipeline from vertex input to pixel output, with no GPU or graphics API dependencies
- **Implement** the full vertex transformation chain: model, view, projection, perspective divide, viewport transform
- **Design** a triangle rasterizer with z-buffer, perspective-correct texture interpolation, and multiple shading models
- **Evaluate** the visual quality and performance trade-offs between flat shading, Gouraud shading, and per-pixel operations

## The Challenge

Every GPU executes the same fundamental algorithm: transform vertices from 3D space to screen coordinates, assemble them into triangles, rasterize each triangle into fragments, shade each fragment, and write the result to a framebuffer with depth testing. This is the rasterization pipeline that renders every 3D game and application.

Build this entire pipeline in software. No OpenGL, no Vulkan, no wgpu. Transform vertices through model, view, and projection matrices. Clip triangles against the view frustum. Rasterize triangles using scanline conversion or the edge function method. Implement a z-buffer for correct depth ordering. Apply flat shading and Gouraud (per-vertex) shading. Implement perspective-correct texture mapping. Load OBJ models and render them to PPM or PNG images.

The output is a static rendered image (or sequence of frames). This is not a real-time renderer -- the goal is correctness and understanding of every stage in the pipeline.

## Requirements

1. Implement `Vec3`, `Vec4`, and `Mat4` types with full arithmetic: vector add/sub/scale/dot/cross/normalize, matrix multiply, matrix-vector multiply, transpose
2. Construct transformation matrices: translation, rotation (X, Y, Z), uniform scale, look-at (view matrix), perspective projection (with FOV, aspect ratio, near/far planes)
3. Implement the full vertex transformation pipeline: model matrix * vertex -> world space, view matrix -> view space, projection matrix -> clip space, perspective divide -> NDC, viewport transform -> screen space
4. Implement triangle rasterization: for each triangle, determine which pixels it covers and compute barycentric coordinates for interpolation. Use either scanline (top-bottom edge walking) or the edge function method (test all pixels in bounding box)
5. Implement a z-buffer (depth buffer): a `Vec<f64>` the size of the image, initialized to infinity. For each fragment, compare its interpolated depth against the z-buffer. Write only if closer
6. Implement backface culling: compute the triangle normal in screen space (or view space). If it faces away from the camera, skip the triangle
7. Implement flat shading: compute one normal per triangle face, apply a directional light (N dot L), output a single color for the entire triangle
8. Implement Gouraud shading: compute lighting at each vertex, interpolate the resulting color across the triangle using barycentric coordinates
9. Implement perspective-correct texture mapping: interpolate texture coordinates divided by clip-space W, then divide by the interpolated 1/W at each pixel, to produce correct UV coordinates
10. Implement view frustum clipping: clip triangles against the near plane (minimum). Triangles partially outside the frustum are split into smaller triangles. Fully outside triangles are discarded
11. Implement an OBJ model loader: parse vertex positions (`v`), texture coordinates (`vt`), normals (`vn`), and face definitions (`f`). Handle triangulated meshes
12. Output rendered frames as PPM images (mandatory) and optionally PNG via the `image` crate

## Acceptance Criteria

- [ ] A simple scene (cube or Utah teapot) renders with correct 3D perspective
- [ ] Depth buffer resolves overlapping triangles: closer surfaces occlude farther ones
- [ ] Backface culling removes back-facing triangles, reducing rendered triangle count by approximately half for closed meshes
- [ ] Flat shading produces visible light/shadow boundaries on curved surfaces
- [ ] Gouraud shading produces smooth color gradients across triangle boundaries
- [ ] Texture-mapped model displays the texture without visible warping (perspective-correct)
- [ ] View frustum clipping handles triangles crossing the near plane without visual artifacts
- [ ] OBJ model (e.g., teapot, Suzanne) loads and renders correctly
- [ ] Output image is a valid PPM file viewable in standard image viewers
- [ ] Wireframe mode renders triangle edges for debugging
- [ ] The renderer handles degenerate triangles (zero area) without crashing

## Research Resources

- [Scratchapixel: Rasterization](https://www.scratchapixel.com/lessons/3d-basic-rendering/rasterization-practical-implementation) -- comprehensive tutorial on software rasterization with code
- [tinyrenderer (Dmitry Sokolov)](https://github.com/ssloy/tinyrenderer/wiki) -- build a renderer in 500 lines, excellent learning resource
- [Fundamentals of Computer Graphics (Marschner & Shirley)](https://www.cs.cornell.edu/~srm/fcg4/) -- the standard textbook for rasterization pipeline
- [Learn OpenGL: Coordinate Systems](https://learnopengl.com/Getting-started/Coordinate-Systems) -- visual explanation of the transformation chain
- [Perspective-Correct Interpolation](https://www.comp.nus.edu.sg/~lowkl/publications/lowk_persp_interp_techrep.pdf) -- the math behind perspective-correct texture mapping
- [OBJ File Format Specification](https://paulbourke.net/dataformats/obj/) -- the OBJ format reference
- [A Parallel Algorithm for Polygon Rasterization (Pineda, 1988)](https://www.cs.drexel.edu/~david/Classes/Papers/comp175-06-pineda.pdf) -- the edge function rasterization method used by GPUs
- [Real-Time Rendering (Akenine-Moller et al.)](https://www.realtimerendering.com/) -- comprehensive reference covering the entire rendering pipeline
- [3D Math Primer for Graphics and Game Development (Dunn & Parberry)](https://gamemath.com/) -- matrices, transforms, and coordinate systems explained for programmers
