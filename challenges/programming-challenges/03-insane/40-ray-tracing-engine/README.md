# 40. Ray Tracing Engine

<!--
difficulty: insane
category: systems-programming
languages: [rust]
concepts: [ray-tracing, vector-math, bvh, phong-shading, reflection, refraction, multi-threading, image-generation]
estimated_time: 20-30 hours
bloom_level: create
prerequisites: [linear-algebra-basics, rust-traits, generics, rayon, file-io, floating-point-arithmetic]
-->

## Languages

- Rust (stable)

## Prerequisites

- 3D vector math: dot product, cross product, normalization, reflection
- Floating-point precision issues and epsilon comparisons
- Rust trait objects and generic dispatch
- Rayon for data-parallel iteration
- File I/O for writing image formats

## Learning Objectives

- **Create** a physically-based ray tracing renderer from mathematical first principles
- **Implement** recursive ray tracing with reflection, refraction, and shadow computation
- **Design** a BVH acceleration structure that reduces intersection tests from O(n) to O(log n)
- **Evaluate** the trade-offs between image quality (samples per pixel, recursion depth) and render time
- **Architect** a scene description system that separates geometry, materials, and lighting from the rendering pipeline

## The Challenge

Ray tracing generates photorealistic images by simulating how light interacts with objects. For every pixel, cast a ray from the camera into the scene, find the closest intersection, compute lighting at that point, and recursively trace reflected and refracted rays. The result is correct shadows, reflections, and transparency -- effects that rasterization approximates but ray tracing computes exactly.

Build a ray tracing renderer from scratch. No graphics libraries, no GPU APIs. Just math, rays, and pixels. The renderer must handle spheres and planes, implement the Phong illumination model, support reflective and refractive materials, accelerate intersection testing with a BVH, and parallelize rendering across CPU cores. Scene descriptions come from JSON configuration files, and the output is a PPM or PNG image.

This is computational geometry, numerical methods, and systems programming combined. Every optimization matters when you are casting millions of rays.

## Requirements

1. Implement a `Vec3` type with vector arithmetic: add, subtract, scale, dot product, cross product, normalize, length, and reflect
2. Implement `Ray` (origin + direction) and ray-sphere and ray-plane intersection tests returning the closest hit point, surface normal, and distance along the ray
3. Implement the Phong illumination model: ambient, diffuse (Lambert), and specular (Blinn-Phong) components. Materials specify ambient, diffuse, specular coefficients and shininess exponent
4. Implement point lights and directional lights with configurable color and intensity
5. Implement shadow rays: before computing diffuse and specular contributions, cast a ray from the hit point toward each light source. If an object blocks the path, the point is in shadow for that light
6. Implement recursive ray tracing: reflective surfaces spawn reflected rays, transparent surfaces spawn refracted rays using Snell's law. Support Fresnel approximation (Schlick's formula) to blend reflection and refraction
7. Implement supersampling anti-aliasing: cast multiple rays per pixel (configurable, e.g., 4x, 16x) with jittered offsets, average the results
8. Implement a BVH (Bounding Volume Hierarchy) using axis-aligned bounding boxes. Construct the tree with surface area heuristic (SAH) for split decisions. Ray traversal must test the BVH instead of every object
9. Parallelize rendering with rayon: divide the image into scanlines or tiles and render them in parallel across all CPU cores
10. Parse scene descriptions from a JSON file: camera position/orientation/FOV, objects (type, position, radius, material), lights (type, position, color), and render settings (resolution, samples per pixel, max recursion depth)
11. Output the rendered image in PPM format (mandatory) and optionally PNG (via the `image` crate)
12. Document all `unsafe` blocks (if any) with safety invariants

## Acceptance Criteria

- [ ] A scene with 3 spheres and a ground plane renders correctly with visible shadows
- [ ] Reflective surfaces show other objects in their reflection
- [ ] A glass sphere demonstrates refraction with visible distortion of objects behind it
- [ ] Fresnel effect is visible: glass sphere edges are more reflective than the center
- [ ] Anti-aliasing visibly reduces jagged edges compared to single-sample rendering
- [ ] BVH acceleration renders a 1000-sphere scene at least 10x faster than brute force
- [ ] Multi-threaded rendering achieves near-linear speedup (measure 1 thread vs N threads)
- [ ] Scene is loaded from a JSON config file, not hardcoded
- [ ] Output image is a valid PPM file viewable in any image viewer
- [ ] The renderer handles edge cases: ray origins inside spheres, total internal reflection, lights behind objects
- [ ] Gamma correction is applied: rendered colors are in linear space internally, converted to sRGB on output
- [ ] No shadow acne: shadow rays do not produce speckled artifacts on flat surfaces

## Research Resources

- [Ray Tracing in One Weekend (Peter Shirley)](https://raytracing.github.io/books/RayTracingInOneWeekend.html) -- the definitive introductory tutorial, covers the complete rendering pipeline
- [Ray Tracing: The Next Week (Peter Shirley)](https://raytracing.github.io/books/RayTracingTheNextWeek.html) -- BVH construction, textures, motion blur
- [Physically Based Rendering: From Theory to Implementation (Pharr, Jakob, Humphreys)](https://www.pbr-book.org/) -- the comprehensive reference, freely available online
- [Scratchapixel: Introduction to Ray Tracing](https://www.scratchapixel.com/lessons/3d-basic-rendering/introduction-to-ray-tracing) -- mathematical foundations with derivations
- [An Introduction to Ray Tracing (Glassner, ed.)](https://www.realtimerendering.com/raytracing/An-Introduction-to-Ray-Tracing-The-Morgan-Kaufmann-Series-in-Computer-Graphics-.pdf) -- classic text covering intersection algorithms and shading models
- [BVH Construction with SAH (Wald, 2007)](https://www.sci.utah.edu/~wald/Publications/2007/ParallelBVHBuild/fastbuild.pdf) -- efficient BVH construction techniques
- [Schlick's Approximation](https://en.wikipedia.org/wiki/Schlick%27s_approximation) -- the Fresnel approximation used in most ray tracers
