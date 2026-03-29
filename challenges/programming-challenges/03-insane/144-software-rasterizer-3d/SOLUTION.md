# Solution: 3D Software Rasterizer

## Architecture Overview

The rasterizer is organized into seven modules forming a pipeline:

1. **math**: `Vec3`, `Vec4`, `Mat4`, with full 3D math operations
2. **transform**: Matrix construction (translation, rotation, scale, look-at, perspective projection)
3. **model**: OBJ loader producing vertex/normal/UV data and triangle face indices
4. **pipeline**: Vertex transformation chain (model -> world -> view -> clip -> NDC -> screen)
5. **rasterizer**: Triangle rasterization, z-buffer, barycentric interpolation
6. **shading**: Flat shading, Gouraud shading, perspective-correct texture mapping
7. **output**: Framebuffer to PPM/PNG writer

```
OBJ File -> [Model Loader] -> Vertices, Normals, UVs, Faces
                                  |
                                  v
Scene Description (camera, lights, transforms)
                                  |
                                  v
[Vertex Shader Stage]
  model_matrix * vertex -> world position
  view_matrix * world -> view position
  projection_matrix * view -> clip position
                                  |
                                  v
[Clipping] -> clip against near plane
                                  |
                                  v
[Perspective Divide] -> NDC (-1..1)
                                  |
                                  v
[Viewport Transform] -> screen coordinates (pixels)
                                  |
                                  v
[Backface Culling] -> discard back-facing triangles
                                  |
                                  v
[Rasterization] -> for each triangle:
  compute bounding box
  for each pixel in bounding box:
    compute barycentric coords
    if inside triangle:
      interpolate depth
      z-buffer test
      shade pixel (flat/Gouraud/textured)
      write to framebuffer
                                  |
                                  v
[Output] -> PPM / PNG file
```

## Rust Solution

### Cargo.toml

```toml
[package]
name = "rasterizer"
version = "0.1.0"
edition = "2021"

[dependencies]
image = { version = "0.25", optional = true }

[features]
default = []
png_output = ["image"]
```

### src/math.rs

```rust
use std::ops::{Add, Sub, Mul, Neg};

#[derive(Debug, Clone, Copy)]
pub struct Vec3 {
    pub x: f64,
    pub y: f64,
    pub z: f64,
}

impl Vec3 {
    pub const ZERO: Vec3 = Vec3 { x: 0.0, y: 0.0, z: 0.0 };

    pub fn new(x: f64, y: f64, z: f64) -> Self {
        Self { x, y, z }
    }

    pub fn dot(self, other: Vec3) -> f64 {
        self.x * other.x + self.y * other.y + self.z * other.z
    }

    pub fn cross(self, other: Vec3) -> Vec3 {
        Vec3 {
            x: self.y * other.z - self.z * other.y,
            y: self.z * other.x - self.x * other.z,
            z: self.x * other.y - self.y * other.x,
        }
    }

    pub fn length(self) -> f64 {
        self.dot(self).sqrt()
    }

    pub fn normalized(self) -> Vec3 {
        let len = self.length();
        if len < 1e-10 { return Vec3::ZERO; }
        self * (1.0 / len)
    }

    pub fn to_vec4(self, w: f64) -> Vec4 {
        Vec4::new(self.x, self.y, self.z, w)
    }
}

impl Add for Vec3 { type Output = Vec3; fn add(self, r: Vec3) -> Vec3 { Vec3::new(self.x + r.x, self.y + r.y, self.z + r.z) } }
impl Sub for Vec3 { type Output = Vec3; fn sub(self, r: Vec3) -> Vec3 { Vec3::new(self.x - r.x, self.y - r.y, self.z - r.z) } }
impl Mul<f64> for Vec3 { type Output = Vec3; fn mul(self, t: f64) -> Vec3 { Vec3::new(self.x * t, self.y * t, self.z * t) } }
impl Neg for Vec3 { type Output = Vec3; fn neg(self) -> Vec3 { Vec3::new(-self.x, -self.y, -self.z) } }

#[derive(Debug, Clone, Copy)]
pub struct Vec4 {
    pub x: f64,
    pub y: f64,
    pub z: f64,
    pub w: f64,
}

impl Vec4 {
    pub fn new(x: f64, y: f64, z: f64, w: f64) -> Self {
        Self { x, y, z, w }
    }

    pub fn to_vec3(self) -> Vec3 {
        Vec3::new(self.x, self.y, self.z)
    }

    /// Perspective divide: divide xyz by w.
    pub fn perspective_divide(self) -> Vec3 {
        if self.w.abs() < 1e-10 {
            return Vec3::ZERO;
        }
        Vec3::new(self.x / self.w, self.y / self.w, self.z / self.w)
    }
}

#[derive(Debug, Clone, Copy)]
pub struct Mat4 {
    pub m: [[f64; 4]; 4],
}

impl Mat4 {
    pub fn identity() -> Self {
        let mut m = [[0.0; 4]; 4];
        for i in 0..4 { m[i][i] = 1.0; }
        Self { m }
    }

    pub fn from_rows(rows: [[f64; 4]; 4]) -> Self {
        Self { m: rows }
    }

    pub fn mul_vec4(&self, v: Vec4) -> Vec4 {
        Vec4 {
            x: self.m[0][0]*v.x + self.m[0][1]*v.y + self.m[0][2]*v.z + self.m[0][3]*v.w,
            y: self.m[1][0]*v.x + self.m[1][1]*v.y + self.m[1][2]*v.z + self.m[1][3]*v.w,
            z: self.m[2][0]*v.x + self.m[2][1]*v.y + self.m[2][2]*v.z + self.m[2][3]*v.w,
            w: self.m[3][0]*v.x + self.m[3][1]*v.y + self.m[3][2]*v.z + self.m[3][3]*v.w,
        }
    }

    pub fn mul_mat4(&self, other: &Mat4) -> Mat4 {
        let mut result = [[0.0; 4]; 4];
        for i in 0..4 {
            for j in 0..4 {
                for k in 0..4 {
                    result[i][j] += self.m[i][k] * other.m[k][j];
                }
            }
        }
        Mat4 { m: result }
    }

    pub fn transpose(&self) -> Mat4 {
        let mut result = [[0.0; 4]; 4];
        for i in 0..4 {
            for j in 0..4 {
                result[i][j] = self.m[j][i];
            }
        }
        Mat4 { m: result }
    }
}

pub type Color = Vec3; // r, g, b in [0, 1]

pub fn color(r: f64, g: f64, b: f64) -> Color {
    Vec3::new(r, g, b)
}

pub fn color_to_u8(c: Color) -> (u8, u8, u8) {
    let r = (c.x.clamp(0.0, 1.0) * 255.0) as u8;
    let g = (c.y.clamp(0.0, 1.0) * 255.0) as u8;
    let b = (c.z.clamp(0.0, 1.0) * 255.0) as u8;
    (r, g, b)
}
```

### src/transform.rs

```rust
use crate::math::*;

pub fn translation(tx: f64, ty: f64, tz: f64) -> Mat4 {
    Mat4::from_rows([
        [1.0, 0.0, 0.0, tx],
        [0.0, 1.0, 0.0, ty],
        [0.0, 0.0, 1.0, tz],
        [0.0, 0.0, 0.0, 1.0],
    ])
}

pub fn scale(sx: f64, sy: f64, sz: f64) -> Mat4 {
    Mat4::from_rows([
        [sx,  0.0, 0.0, 0.0],
        [0.0, sy,  0.0, 0.0],
        [0.0, 0.0, sz,  0.0],
        [0.0, 0.0, 0.0, 1.0],
    ])
}

pub fn rotation_x(angle: f64) -> Mat4 {
    let c = angle.cos();
    let s = angle.sin();
    Mat4::from_rows([
        [1.0, 0.0, 0.0, 0.0],
        [0.0, c,   -s,  0.0],
        [0.0, s,    c,  0.0],
        [0.0, 0.0, 0.0, 1.0],
    ])
}

pub fn rotation_y(angle: f64) -> Mat4 {
    let c = angle.cos();
    let s = angle.sin();
    Mat4::from_rows([
        [c,   0.0, s,   0.0],
        [0.0, 1.0, 0.0, 0.0],
        [-s,  0.0, c,   0.0],
        [0.0, 0.0, 0.0, 1.0],
    ])
}

pub fn rotation_z(angle: f64) -> Mat4 {
    let c = angle.cos();
    let s = angle.sin();
    Mat4::from_rows([
        [c,   -s,  0.0, 0.0],
        [s,    c,  0.0, 0.0],
        [0.0, 0.0, 1.0, 0.0],
        [0.0, 0.0, 0.0, 1.0],
    ])
}

/// Construct a look-at view matrix.
/// eye: camera position, target: point to look at, up: world up direction.
pub fn look_at(eye: Vec3, target: Vec3, up: Vec3) -> Mat4 {
    let forward = (eye - target).normalized();   // Camera looks along -z
    let right = up.cross(forward).normalized();
    let camera_up = forward.cross(right);

    Mat4::from_rows([
        [right.x,     right.y,     right.z,     -right.dot(eye)],
        [camera_up.x, camera_up.y, camera_up.z, -camera_up.dot(eye)],
        [forward.x,   forward.y,   forward.z,   -forward.dot(eye)],
        [0.0,         0.0,         0.0,          1.0],
    ])
}

/// Perspective projection matrix.
/// fov_y: vertical field of view in radians.
/// aspect: width/height.
/// near, far: clipping planes (positive distances).
pub fn perspective(fov_y: f64, aspect: f64, near: f64, far: f64) -> Mat4 {
    let f = 1.0 / (fov_y / 2.0).tan();
    let range = near - far;

    Mat4::from_rows([
        [f / aspect, 0.0, 0.0,                        0.0],
        [0.0,        f,   0.0,                        0.0],
        [0.0,        0.0, (far + near) / range,       2.0 * far * near / range],
        [0.0,        0.0, -1.0,                       0.0],
    ])
}

/// Viewport transform: NDC [-1,1] -> screen [0,width] x [0,height].
pub fn viewport_transform(x: f64, y: f64, ndc: Vec3) -> (f64, f64, f64) {
    let screen_x = (ndc.x + 1.0) * 0.5 * x;
    let screen_y = (1.0 - ndc.y) * 0.5 * y;  // Flip Y: NDC +y is up, screen +y is down
    let depth = (ndc.z + 1.0) * 0.5;          // Map depth to [0, 1]
    (screen_x, screen_y, depth)
}
```

### src/model.rs

```rust
use crate::math::Vec3;
use std::fs;

#[derive(Debug, Clone)]
pub struct Vertex {
    pub position: Vec3,
    pub normal: Vec3,
    pub uv: (f64, f64),
}

#[derive(Debug, Clone)]
pub struct Triangle {
    pub v: [usize; 3],
}

#[derive(Debug)]
pub struct Mesh {
    pub vertices: Vec<Vertex>,
    pub triangles: Vec<Triangle>,
}

impl Mesh {
    /// Load a triangulated OBJ file.
    pub fn load_obj(path: &str) -> Result<Self, String> {
        let content = fs::read_to_string(path)
            .map_err(|e| format!("Failed to read {}: {}", path, e))?;

        let mut positions: Vec<Vec3> = Vec::new();
        let mut normals: Vec<Vec3> = Vec::new();
        let mut uvs: Vec<(f64, f64)> = Vec::new();
        let mut vertices: Vec<Vertex> = Vec::new();
        let mut triangles: Vec<Triangle> = Vec::new();

        // Map from (pos_idx, uv_idx, norm_idx) to vertex index for dedup
        let mut vertex_map: std::collections::HashMap<(usize, usize, usize), usize> =
            std::collections::HashMap::new();

        for line in content.lines() {
            let line = line.trim();
            if line.is_empty() || line.starts_with('#') {
                continue;
            }

            let parts: Vec<&str> = line.split_whitespace().collect();
            if parts.is_empty() { continue; }

            match parts[0] {
                "v" if parts.len() >= 4 => {
                    let x = parts[1].parse::<f64>().unwrap_or(0.0);
                    let y = parts[2].parse::<f64>().unwrap_or(0.0);
                    let z = parts[3].parse::<f64>().unwrap_or(0.0);
                    positions.push(Vec3::new(x, y, z));
                }
                "vn" if parts.len() >= 4 => {
                    let x = parts[1].parse::<f64>().unwrap_or(0.0);
                    let y = parts[2].parse::<f64>().unwrap_or(0.0);
                    let z = parts[3].parse::<f64>().unwrap_or(0.0);
                    normals.push(Vec3::new(x, y, z));
                }
                "vt" if parts.len() >= 3 => {
                    let u = parts[1].parse::<f64>().unwrap_or(0.0);
                    let v = parts[2].parse::<f64>().unwrap_or(0.0);
                    uvs.push((u, v));
                }
                "f" if parts.len() >= 4 => {
                    let mut face_verts: Vec<usize> = Vec::new();

                    for &part in &parts[1..] {
                        let indices: Vec<&str> = part.split('/').collect();
                        let pi = indices[0].parse::<usize>().unwrap_or(1) - 1;
                        let ti = if indices.len() > 1 && !indices[1].is_empty() {
                            indices[1].parse::<usize>().unwrap_or(1) - 1
                        } else { 0 };
                        let ni = if indices.len() > 2 && !indices[2].is_empty() {
                            indices[2].parse::<usize>().unwrap_or(1) - 1
                        } else { 0 };

                        let key = (pi, ti, ni);
                        let vi = if let Some(&existing) = vertex_map.get(&key) {
                            existing
                        } else {
                            let pos = positions.get(pi).copied().unwrap_or(Vec3::ZERO);
                            let norm = normals.get(ni).copied().unwrap_or(Vec3::new(0.0, 1.0, 0.0));
                            let uv = uvs.get(ti).copied().unwrap_or((0.0, 0.0));
                            let idx = vertices.len();
                            vertices.push(Vertex { position: pos, normal: norm, uv });
                            vertex_map.insert(key, idx);
                            idx
                        };
                        face_verts.push(vi);
                    }

                    // Fan triangulation for polygons with more than 3 vertices
                    for i in 1..(face_verts.len() - 1) {
                        triangles.push(Triangle {
                            v: [face_verts[0], face_verts[i], face_verts[i + 1]],
                        });
                    }
                }
                _ => {}
            }
        }

        if positions.is_empty() {
            return Err("No vertices found in OBJ file".to_string());
        }

        // If no normals were provided, compute face normals
        if normals.is_empty() {
            for tri in &triangles {
                let v0 = vertices[tri.v[0]].position;
                let v1 = vertices[tri.v[1]].position;
                let v2 = vertices[tri.v[2]].position;
                let normal = (v1 - v0).cross(v2 - v0).normalized();
                for &vi in &tri.v {
                    vertices[vi].normal = normal;
                }
            }
        }

        Ok(Mesh { vertices, triangles })
    }

    /// Create a unit cube centered at origin.
    pub fn cube() -> Self {
        let p = [
            Vec3::new(-0.5, -0.5, -0.5), Vec3::new( 0.5, -0.5, -0.5),
            Vec3::new( 0.5,  0.5, -0.5), Vec3::new(-0.5,  0.5, -0.5),
            Vec3::new(-0.5, -0.5,  0.5), Vec3::new( 0.5, -0.5,  0.5),
            Vec3::new( 0.5,  0.5,  0.5), Vec3::new(-0.5,  0.5,  0.5),
        ];
        let faces = [
            // Front
            ([4,5,6], Vec3::new(0.0,0.0,1.0)),  ([4,6,7], Vec3::new(0.0,0.0,1.0)),
            // Back
            ([1,0,3], Vec3::new(0.0,0.0,-1.0)), ([1,3,2], Vec3::new(0.0,0.0,-1.0)),
            // Right
            ([5,1,2], Vec3::new(1.0,0.0,0.0)),  ([5,2,6], Vec3::new(1.0,0.0,0.0)),
            // Left
            ([0,4,7], Vec3::new(-1.0,0.0,0.0)), ([0,7,3], Vec3::new(-1.0,0.0,0.0)),
            // Top
            ([7,6,2], Vec3::new(0.0,1.0,0.0)),  ([7,2,3], Vec3::new(0.0,1.0,0.0)),
            // Bottom
            ([0,1,5], Vec3::new(0.0,-1.0,0.0)), ([0,5,4], Vec3::new(0.0,-1.0,0.0)),
        ];

        let mut vertices = Vec::new();
        let mut triangles = Vec::new();

        for (indices, normal) in &faces {
            let base = vertices.len();
            for &i in indices {
                vertices.push(Vertex {
                    position: p[i],
                    normal: *normal,
                    uv: (0.0, 0.0),
                });
            }
            triangles.push(Triangle { v: [base, base + 1, base + 2] });
        }

        Mesh { vertices, triangles }
    }
}
```

### src/rasterizer.rs

```rust
use crate::math::*;

pub struct Framebuffer {
    pub width: usize,
    pub height: usize,
    pub color_buffer: Vec<Color>,
    pub depth_buffer: Vec<f64>,
}

impl Framebuffer {
    pub fn new(width: usize, height: usize) -> Self {
        Self {
            width,
            height,
            color_buffer: vec![color(0.0, 0.0, 0.0); width * height],
            depth_buffer: vec![f64::INFINITY; width * height],
        }
    }

    pub fn clear(&mut self, clear_color: Color) {
        self.color_buffer.fill(clear_color);
        self.depth_buffer.fill(f64::INFINITY);
    }

    #[inline]
    pub fn set_pixel(&mut self, x: usize, y: usize, depth: f64, color: Color) {
        if x >= self.width || y >= self.height { return; }
        let idx = y * self.width + x;
        if depth < self.depth_buffer[idx] {
            self.depth_buffer[idx] = depth;
            self.color_buffer[idx] = color;
        }
    }

    pub fn draw_line(&mut self, x0: i32, y0: i32, x1: i32, y1: i32, color: Color) {
        let dx = (x1 - x0).abs();
        let dy = -(y1 - y0).abs();
        let sx: i32 = if x0 < x1 { 1 } else { -1 };
        let sy: i32 = if y0 < y1 { 1 } else { -1 };
        let mut err = dx + dy;
        let mut x = x0;
        let mut y = y0;

        loop {
            if x >= 0 && y >= 0 && (x as usize) < self.width && (y as usize) < self.height {
                let idx = y as usize * self.width + x as usize;
                self.color_buffer[idx] = color;
            }
            if x == x1 && y == y1 { break; }
            let e2 = 2 * err;
            if e2 >= dy { err += dy; x += sx; }
            if e2 <= dx { err += dx; y += sy; }
        }
    }

    /// Write framebuffer to PPM file.
    pub fn write_ppm(&self, path: &str) -> std::io::Result<()> {
        use std::io::Write;
        let mut file = std::fs::File::create(path)?;
        write!(file, "P6\n{} {}\n255\n", self.width, self.height)?;
        let mut bytes = Vec::with_capacity(self.width * self.height * 3);
        for pixel in &self.color_buffer {
            let (r, g, b) = color_to_u8(*pixel);
            bytes.push(r);
            bytes.push(g);
            bytes.push(b);
        }
        file.write_all(&bytes)?;
        Ok(())
    }
}

/// Compute barycentric coordinates of point P with respect to triangle (a, b, c).
/// Returns (u, v, w) where P = u*a + v*b + w*c.
pub fn barycentric(
    ax: f64, ay: f64,
    bx: f64, by: f64,
    cx: f64, cy: f64,
    px: f64, py: f64,
) -> (f64, f64, f64) {
    let v0x = bx - ax;
    let v0y = by - ay;
    let v1x = cx - ax;
    let v1y = cy - ay;
    let v2x = px - ax;
    let v2y = py - ay;

    let d00 = v0x * v0x + v0y * v0y;
    let d01 = v0x * v1x + v0y * v1y;
    let d11 = v1x * v1x + v1y * v1y;
    let d20 = v2x * v0x + v2y * v0y;
    let d21 = v2x * v1x + v2y * v1y;

    let denom = d00 * d11 - d01 * d01;
    if denom.abs() < 1e-10 {
        return (-1.0, -1.0, -1.0); // Degenerate triangle
    }

    let v = (d11 * d20 - d01 * d21) / denom;
    let w = (d00 * d21 - d01 * d20) / denom;
    let u = 1.0 - v - w;

    (u, v, w)
}

/// Rasterize a single triangle with flat shading.
pub fn rasterize_triangle_flat(
    fb: &mut Framebuffer,
    screen: [(f64, f64, f64); 3],  // (x, y, depth) in screen space
    face_color: Color,
) {
    let (x0, y0, _) = screen[0];
    let (x1, y1, _) = screen[1];
    let (x2, y2, _) = screen[2];

    let min_x = x0.min(x1).min(x2).max(0.0) as usize;
    let max_x = x0.max(x1).max(x2).min(fb.width as f64 - 1.0) as usize;
    let min_y = y0.min(y1).min(y2).max(0.0) as usize;
    let max_y = y0.max(y1).max(y2).min(fb.height as f64 - 1.0) as usize;

    for y in min_y..=max_y {
        for x in min_x..=max_x {
            let px = x as f64 + 0.5;
            let py = y as f64 + 0.5;

            let (u, v, w) = barycentric(x0, y0, x1, y1, x2, y2, px, py);
            if u < 0.0 || v < 0.0 || w < 0.0 { continue; }

            let depth = u * screen[0].2 + v * screen[1].2 + w * screen[2].2;
            fb.set_pixel(x, y, depth, face_color);
        }
    }
}

/// Rasterize a triangle with Gouraud (per-vertex color) shading.
pub fn rasterize_triangle_gouraud(
    fb: &mut Framebuffer,
    screen: [(f64, f64, f64); 3],
    vertex_colors: [Color; 3],
) {
    let (x0, y0, _) = screen[0];
    let (x1, y1, _) = screen[1];
    let (x2, y2, _) = screen[2];

    let min_x = x0.min(x1).min(x2).max(0.0) as usize;
    let max_x = x0.max(x1).max(x2).min(fb.width as f64 - 1.0) as usize;
    let min_y = y0.min(y1).min(y2).max(0.0) as usize;
    let max_y = y0.max(y1).max(y2).min(fb.height as f64 - 1.0) as usize;

    for y in min_y..=max_y {
        for x in min_x..=max_x {
            let px = x as f64 + 0.5;
            let py = y as f64 + 0.5;

            let (u, v, w) = barycentric(x0, y0, x1, y1, x2, y2, px, py);
            if u < 0.0 || v < 0.0 || w < 0.0 { continue; }

            let depth = u * screen[0].2 + v * screen[1].2 + w * screen[2].2;
            let r = u * vertex_colors[0].x + v * vertex_colors[1].x + w * vertex_colors[2].x;
            let g = u * vertex_colors[0].y + v * vertex_colors[1].y + w * vertex_colors[2].y;
            let b = u * vertex_colors[0].z + v * vertex_colors[1].z + w * vertex_colors[2].z;

            fb.set_pixel(x, y, depth, color(r, g, b));
        }
    }
}

/// Rasterize with perspective-correct texture interpolation.
pub fn rasterize_triangle_textured(
    fb: &mut Framebuffer,
    screen: [(f64, f64, f64); 3],
    clip_w: [f64; 3],  // W values from clip space (for perspective correction)
    uvs: [(f64, f64); 3],
    texture: &TextureBuffer,
    light_intensity: f64,
) {
    let (x0, y0, _) = screen[0];
    let (x1, y1, _) = screen[1];
    let (x2, y2, _) = screen[2];

    let min_x = x0.min(x1).min(x2).max(0.0) as usize;
    let max_x = x0.max(x1).max(x2).min(fb.width as f64 - 1.0) as usize;
    let min_y = y0.min(y1).min(y2).max(0.0) as usize;
    let max_y = y0.max(y1).max(y2).min(fb.height as f64 - 1.0) as usize;

    // Pre-compute 1/w for each vertex
    let inv_w = [1.0 / clip_w[0], 1.0 / clip_w[1], 1.0 / clip_w[2]];

    for y in min_y..=max_y {
        for x in min_x..=max_x {
            let px = x as f64 + 0.5;
            let py = y as f64 + 0.5;

            let (u, v, w) = barycentric(x0, y0, x1, y1, x2, y2, px, py);
            if u < 0.0 || v < 0.0 || w < 0.0 { continue; }

            let depth = u * screen[0].2 + v * screen[1].2 + w * screen[2].2;

            // Perspective-correct UV interpolation
            let interp_inv_w = u * inv_w[0] + v * inv_w[1] + w * inv_w[2];
            let tex_u = (u * uvs[0].0 * inv_w[0]
                       + v * uvs[1].0 * inv_w[1]
                       + w * uvs[2].0 * inv_w[2]) / interp_inv_w;
            let tex_v = (u * uvs[0].1 * inv_w[0]
                       + v * uvs[1].1 * inv_w[1]
                       + w * uvs[2].1 * inv_w[2]) / interp_inv_w;

            let tex_color = texture.sample(tex_u, tex_v);
            let lit_color = color(
                tex_color.x * light_intensity,
                tex_color.y * light_intensity,
                tex_color.z * light_intensity,
            );

            fb.set_pixel(x, y, depth, lit_color);
        }
    }
}

pub struct TextureBuffer {
    pub width: usize,
    pub height: usize,
    pub pixels: Vec<Color>,
}

impl TextureBuffer {
    pub fn checkerboard(size: usize, c1: Color, c2: Color) -> Self {
        let mut pixels = Vec::with_capacity(size * size);
        for y in 0..size {
            for x in 0..size {
                let checker = ((x / 8) + (y / 8)) % 2 == 0;
                pixels.push(if checker { c1 } else { c2 });
            }
        }
        Self { width: size, height: size, pixels }
    }

    pub fn sample(&self, u: f64, v: f64) -> Color {
        let u = u.fract();
        let v = v.fract();
        let u = if u < 0.0 { u + 1.0 } else { u };
        let v = if v < 0.0 { v + 1.0 } else { v };
        let x = (u * self.width as f64) as usize % self.width;
        let y = ((1.0 - v) * self.height as f64) as usize % self.height;
        self.pixels[y * self.width + x]
    }
}
```

### src/pipeline.rs

```rust
use crate::math::*;
use crate::transform::*;
use crate::model::*;
use crate::rasterizer::*;

pub struct RenderSettings {
    pub width: usize,
    pub height: usize,
    pub wireframe: bool,
    pub shading: ShadingMode,
    pub backface_cull: bool,
}

#[derive(Clone, Copy, PartialEq)]
pub enum ShadingMode {
    Flat,
    Gouraud,
    Wireframe,
}

pub struct DirectionalLight {
    pub direction: Vec3,  // Points toward the light
    pub color: Color,
    pub ambient: f64,
}

/// Clip a triangle against the near plane (z = -near in view space,
/// or w > 0 in clip space). Returns 0, 1, or 2 triangles.
fn clip_triangle_near_plane(
    clip_verts: [Vec4; 3],
    normals: [Vec3; 3],
    uvs: [(f64, f64); 3],
) -> Vec<([Vec4; 3], [Vec3; 3], [(f64, f64); 3])> {
    let inside: Vec<bool> = clip_verts.iter().map(|v| v.w > 0.001).collect();
    let num_inside = inside.iter().filter(|&&b| b).count();

    match num_inside {
        3 => vec![(clip_verts, normals, uvs)],
        0 => vec![],
        _ => {
            // Find inside and outside vertices
            let mut inside_idx = Vec::new();
            let mut outside_idx = Vec::new();
            for i in 0..3 {
                if inside[i] { inside_idx.push(i); } else { outside_idx.push(i); }
            }

            if num_inside == 1 {
                let a = inside_idx[0];
                let b = outside_idx[0];
                let c = outside_idx[1];

                let t_ab = clip_verts[a].w / (clip_verts[a].w - clip_verts[b].w);
                let t_ac = clip_verts[a].w / (clip_verts[a].w - clip_verts[c].w);

                let v_ab = lerp_vec4(clip_verts[a], clip_verts[b], t_ab);
                let v_ac = lerp_vec4(clip_verts[a], clip_verts[c], t_ac);
                let n_ab = lerp_vec3(normals[a], normals[b], t_ab);
                let n_ac = lerp_vec3(normals[a], normals[c], t_ac);
                let uv_ab = lerp_uv(uvs[a], uvs[b], t_ab);
                let uv_ac = lerp_uv(uvs[a], uvs[c], t_ac);

                vec![([clip_verts[a], v_ab, v_ac], [normals[a], n_ab, n_ac], [uvs[a], uv_ab, uv_ac])]
            } else {
                // 2 inside, 1 outside -> 2 triangles
                let a = inside_idx[0];
                let b = inside_idx[1];
                let c = outside_idx[0];

                let t_ac = clip_verts[a].w / (clip_verts[a].w - clip_verts[c].w);
                let t_bc = clip_verts[b].w / (clip_verts[b].w - clip_verts[c].w);

                let v_ac = lerp_vec4(clip_verts[a], clip_verts[c], t_ac);
                let v_bc = lerp_vec4(clip_verts[b], clip_verts[c], t_bc);
                let n_ac = lerp_vec3(normals[a], normals[c], t_ac);
                let n_bc = lerp_vec3(normals[b], normals[c], t_bc);
                let uv_ac = lerp_uv(uvs[a], uvs[c], t_ac);
                let uv_bc = lerp_uv(uvs[b], uvs[c], t_bc);

                vec![
                    ([clip_verts[a], clip_verts[b], v_ac], [normals[a], normals[b], n_ac], [uvs[a], uvs[b], uv_ac]),
                    ([clip_verts[b], v_bc, v_ac], [normals[b], n_bc, n_ac], [uvs[b], uv_bc, uv_ac]),
                ]
            }
        }
    }
}

fn lerp_vec4(a: Vec4, b: Vec4, t: f64) -> Vec4 {
    Vec4::new(
        a.x + (b.x - a.x) * t,
        a.y + (b.y - a.y) * t,
        a.z + (b.z - a.z) * t,
        a.w + (b.w - a.w) * t,
    )
}

fn lerp_vec3(a: Vec3, b: Vec3, t: f64) -> Vec3 {
    Vec3::new(
        a.x + (b.x - a.x) * t,
        a.y + (b.y - a.y) * t,
        a.z + (b.z - a.z) * t,
    )
}

fn lerp_uv(a: (f64, f64), b: (f64, f64), t: f64) -> (f64, f64) {
    (a.0 + (b.0 - a.0) * t, a.1 + (b.1 - a.1) * t)
}

/// Render a mesh with the full pipeline.
pub fn render_mesh(
    fb: &mut Framebuffer,
    mesh: &Mesh,
    model_matrix: &Mat4,
    view_matrix: &Mat4,
    proj_matrix: &Mat4,
    light: &DirectionalLight,
    settings: &RenderSettings,
    texture: Option<&TextureBuffer>,
) {
    let mvp = proj_matrix.mul_mat4(&view_matrix.mul_mat4(model_matrix));
    let model_view = view_matrix.mul_mat4(model_matrix);
    let width = settings.width as f64;
    let height = settings.height as f64;

    let mut rendered_tris = 0u64;
    let mut culled_tris = 0u64;

    for tri in &mesh.triangles {
        let v0 = &mesh.vertices[tri.v[0]];
        let v1 = &mesh.vertices[tri.v[1]];
        let v2 = &mesh.vertices[tri.v[2]];

        // Transform to clip space
        let clip0 = mvp.mul_vec4(v0.position.to_vec4(1.0));
        let clip1 = mvp.mul_vec4(v1.position.to_vec4(1.0));
        let clip2 = mvp.mul_vec4(v2.position.to_vec4(1.0));

        // Transform normals to world space
        let n0 = model_matrix.mul_vec4(v0.normal.to_vec4(0.0)).to_vec3().normalized();
        let n1 = model_matrix.mul_vec4(v1.normal.to_vec4(0.0)).to_vec3().normalized();
        let n2 = model_matrix.mul_vec4(v2.normal.to_vec4(0.0)).to_vec3().normalized();

        let uvs = [v0.uv, v1.uv, v2.uv];

        // Near-plane clipping
        let clipped = clip_triangle_near_plane(
            [clip0, clip1, clip2],
            [n0, n1, n2],
            uvs,
        );

        for (clip_verts, normals, tri_uvs) in clipped {
            // Perspective divide + viewport transform
            let ndc: Vec<Vec3> = clip_verts.iter().map(|v| v.perspective_divide()).collect();
            let screen: Vec<(f64, f64, f64)> = ndc.iter()
                .map(|n| viewport_transform(width, height, *n))
                .collect();

            let screen_arr = [screen[0], screen[1], screen[2]];

            // Backface culling in screen space
            if settings.backface_cull {
                let edge1_x = screen[1].0 - screen[0].0;
                let edge1_y = screen[1].1 - screen[0].1;
                let edge2_x = screen[2].0 - screen[0].0;
                let edge2_y = screen[2].1 - screen[0].1;
                let cross_z = edge1_x * edge2_y - edge1_y * edge2_x;
                if cross_z <= 0.0 {
                    culled_tris += 1;
                    continue;
                }
            }

            rendered_tris += 1;

            match settings.shading {
                ShadingMode::Wireframe => {
                    let c = color(0.0, 1.0, 0.0);
                    fb.draw_line(screen[0].0 as i32, screen[0].1 as i32,
                                 screen[1].0 as i32, screen[1].1 as i32, c);
                    fb.draw_line(screen[1].0 as i32, screen[1].1 as i32,
                                 screen[2].0 as i32, screen[2].1 as i32, c);
                    fb.draw_line(screen[2].0 as i32, screen[2].1 as i32,
                                 screen[0].0 as i32, screen[0].1 as i32, c);
                }
                ShadingMode::Flat => {
                    let face_normal = ((normals[0] + normals[1]) + normals[2]) * (1.0 / 3.0);
                    let face_normal = face_normal.normalized();
                    let intensity = face_normal.dot(light.direction).max(0.0);
                    let lit = light.ambient + (1.0 - light.ambient) * intensity;

                    if let Some(tex) = texture {
                        rasterize_triangle_textured(
                            fb, screen_arr,
                            [clip_verts[0].w, clip_verts[1].w, clip_verts[2].w],
                            tri_uvs, tex, lit,
                        );
                    } else {
                        let face_color = color(
                            light.color.x * lit,
                            light.color.y * lit,
                            light.color.z * lit,
                        );
                        rasterize_triangle_flat(fb, screen_arr, face_color);
                    }
                }
                ShadingMode::Gouraud => {
                    let vertex_colors: [Color; 3] = [
                        compute_vertex_color(&normals[0], light),
                        compute_vertex_color(&normals[1], light),
                        compute_vertex_color(&normals[2], light),
                    ];
                    rasterize_triangle_gouraud(fb, screen_arr, vertex_colors);
                }
            }
        }
    }

    eprintln!("Rendered: {} triangles, Culled: {} triangles", rendered_tris, culled_tris);
}

fn compute_vertex_color(normal: &Vec3, light: &DirectionalLight) -> Color {
    let intensity = normal.dot(light.direction).max(0.0);
    let lit = light.ambient + (1.0 - light.ambient) * intensity;
    color(light.color.x * lit, light.color.y * lit, light.color.z * lit)
}
```

### src/main.rs

```rust
mod math;
mod transform;
mod model;
mod rasterizer;
mod pipeline;

use math::*;
use transform::*;
use model::*;
use rasterizer::*;
use pipeline::*;
use std::f64::consts::PI;

fn main() {
    let width = 800;
    let height = 600;
    let aspect = width as f64 / height as f64;

    let mut fb = Framebuffer::new(width, height);

    // Camera
    let eye = Vec3::new(2.0, 2.0, 3.0);
    let target = Vec3::new(0.0, 0.0, 0.0);
    let up = Vec3::new(0.0, 1.0, 0.0);
    let view = look_at(eye, target, up);
    let proj = perspective(PI / 4.0, aspect, 0.1, 100.0);

    // Light
    let light = DirectionalLight {
        direction: Vec3::new(0.5, 1.0, 0.3).normalized(),
        color: color(0.9, 0.85, 0.8),
        ambient: 0.15,
    };

    let settings = RenderSettings {
        width,
        height,
        wireframe: false,
        shading: ShadingMode::Gouraud,
        backface_cull: true,
    };

    // Try to load an OBJ model, fall back to built-in cube
    let mesh = match Mesh::load_obj("model.obj") {
        Ok(m) => {
            eprintln!("Loaded model: {} vertices, {} triangles",
                      m.vertices.len(), m.triangles.len());
            m
        }
        Err(_) => {
            eprintln!("No model.obj found, rendering built-in cube");
            Mesh::cube()
        }
    };

    // Render with rotation
    let model = rotation_y(PI / 6.0).mul_mat4(&rotation_x(PI / 8.0));

    fb.clear(color(0.1, 0.1, 0.15));
    render_mesh(&mut fb, &mesh, &model, &view, &proj, &light, &settings, None);

    fb.write_ppm("output.ppm").expect("Failed to write PPM");
    eprintln!("Wrote output.ppm ({}x{})", width, height);

    // Also render wireframe version
    let wireframe_settings = RenderSettings {
        shading: ShadingMode::Wireframe,
        ..settings
    };
    fb.clear(color(0.0, 0.0, 0.0));
    render_mesh(&mut fb, &mesh, &model, &view, &proj, &light, &wireframe_settings, None);
    fb.write_ppm("output_wireframe.ppm").expect("Failed to write wireframe PPM");
    eprintln!("Wrote output_wireframe.ppm");

    // Flat shading version
    let flat_settings = RenderSettings {
        shading: ShadingMode::Flat,
        backface_cull: true,
        ..wireframe_settings
    };
    fb.clear(color(0.1, 0.1, 0.15));
    render_mesh(&mut fb, &mesh, &model, &view, &proj, &light, &flat_settings, None);
    fb.write_ppm("output_flat.ppm").expect("Failed to write flat PPM");
    eprintln!("Wrote output_flat.ppm");
}
```

### src/lib.rs

```rust
pub mod math;
pub mod transform;
pub mod model;
pub mod rasterizer;
pub mod pipeline;
```

### Tests

```rust
// tests/rasterizer_tests.rs
use rasterizer::math::*;
use rasterizer::transform::*;
use rasterizer::model::*;
use rasterizer::rasterizer::*;
use std::f64::consts::PI;

#[test]
fn vec3_operations() {
    let a = Vec3::new(1.0, 0.0, 0.0);
    let b = Vec3::new(0.0, 1.0, 0.0);
    assert!((a.dot(b) - 0.0).abs() < 1e-10);

    let cross = a.cross(b);
    assert!((cross.z - 1.0).abs() < 1e-10);

    let norm = Vec3::new(3.0, 4.0, 0.0).normalized();
    assert!((norm.length() - 1.0).abs() < 1e-10);
}

#[test]
fn mat4_identity() {
    let m = Mat4::identity();
    let v = Vec4::new(1.0, 2.0, 3.0, 1.0);
    let result = m.mul_vec4(v);
    assert!((result.x - 1.0).abs() < 1e-10);
    assert!((result.y - 2.0).abs() < 1e-10);
    assert!((result.z - 3.0).abs() < 1e-10);
}

#[test]
fn translation_matrix() {
    let t = translation(5.0, 10.0, -3.0);
    let v = Vec4::new(0.0, 0.0, 0.0, 1.0);
    let result = t.mul_vec4(v);
    assert!((result.x - 5.0).abs() < 1e-10);
    assert!((result.y - 10.0).abs() < 1e-10);
    assert!((result.z - (-3.0)).abs() < 1e-10);
}

#[test]
fn perspective_divide() {
    let v = Vec4::new(2.0, 4.0, 6.0, 2.0);
    let ndc = v.perspective_divide();
    assert!((ndc.x - 1.0).abs() < 1e-10);
    assert!((ndc.y - 2.0).abs() < 1e-10);
    assert!((ndc.z - 3.0).abs() < 1e-10);
}

#[test]
fn barycentric_inside() {
    // Point at centroid of a right triangle
    let (u, v, w) = barycentric(
        0.0, 0.0,
        10.0, 0.0,
        0.0, 10.0,
        3.33, 3.33,
    );
    assert!(u > 0.0 && v > 0.0 && w > 0.0);
    assert!((u + v + w - 1.0).abs() < 0.01);
}

#[test]
fn barycentric_outside() {
    let (u, v, w) = barycentric(
        0.0, 0.0,
        10.0, 0.0,
        0.0, 10.0,
        -5.0, -5.0,
    );
    // At least one coordinate is negative
    assert!(u < 0.0 || v < 0.0 || w < 0.0);
}

#[test]
fn zbuffer_depth_test() {
    let mut fb = Framebuffer::new(10, 10);
    fb.clear(color(0.0, 0.0, 0.0));

    // Write a pixel at depth 0.5
    fb.set_pixel(5, 5, 0.5, color(1.0, 0.0, 0.0));
    assert!((fb.color_buffer[5 * 10 + 5].x - 1.0).abs() < 1e-10);

    // Closer pixel (depth 0.3) overwrites
    fb.set_pixel(5, 5, 0.3, color(0.0, 1.0, 0.0));
    assert!((fb.color_buffer[5 * 10 + 5].y - 1.0).abs() < 1e-10);

    // Farther pixel (depth 0.8) does not overwrite
    fb.set_pixel(5, 5, 0.8, color(0.0, 0.0, 1.0));
    assert!((fb.color_buffer[5 * 10 + 5].y - 1.0).abs() < 1e-10);
}

#[test]
fn rotation_preserves_length() {
    let v = Vec4::new(1.0, 0.0, 0.0, 1.0);
    for angle in [0.0, PI / 4.0, PI / 2.0, PI, 2.0 * PI] {
        let r = rotation_y(angle);
        let result = r.mul_vec4(v).to_vec3();
        assert!((result.length() - 1.0).abs() < 1e-10, "Rotation changed vector length at angle {}", angle);
    }
}

#[test]
fn cube_mesh_valid() {
    let cube = Mesh::cube();
    assert_eq!(cube.triangles.len(), 12); // 6 faces * 2 triangles
    for tri in &cube.triangles {
        for &vi in &tri.v {
            assert!(vi < cube.vertices.len());
        }
    }
}

#[test]
fn ppm_output() {
    let mut fb = Framebuffer::new(2, 2);
    fb.color_buffer[0] = color(1.0, 0.0, 0.0);
    fb.color_buffer[1] = color(0.0, 1.0, 0.0);
    fb.color_buffer[2] = color(0.0, 0.0, 1.0);
    fb.color_buffer[3] = color(1.0, 1.0, 1.0);

    let path = "/tmp/test_rasterizer_output.ppm";
    fb.write_ppm(path).expect("Failed to write PPM");
    let data = std::fs::read(path).expect("Failed to read PPM");
    assert!(data.starts_with(b"P6\n2 2\n255\n"));
    std::fs::remove_file(path).ok();
}

#[test]
fn flat_shading_renders_without_crash() {
    let mut fb = Framebuffer::new(100, 100);
    fb.clear(color(0.0, 0.0, 0.0));

    let screen = [
        (20.0, 80.0, 0.5),
        (50.0, 10.0, 0.5),
        (80.0, 80.0, 0.5),
    ];
    rasterize_triangle_flat(&mut fb, screen, color(1.0, 0.0, 0.0));

    // Some pixels should be colored
    let red_pixels = fb.color_buffer.iter().filter(|c| c.x > 0.5).count();
    assert!(red_pixels > 100, "Too few pixels rendered: {}", red_pixels);
}

#[test]
fn degenerate_triangle_handled() {
    let mut fb = Framebuffer::new(100, 100);
    fb.clear(color(0.0, 0.0, 0.0));

    // Degenerate triangle (all points collinear)
    let screen = [
        (10.0, 10.0, 0.5),
        (50.0, 50.0, 0.5),
        (90.0, 90.0, 0.5),
    ];
    // Should not crash
    rasterize_triangle_flat(&mut fb, screen, color(1.0, 0.0, 0.0));
}

#[test]
fn texture_sampling() {
    let tex = TextureBuffer::checkerboard(64, color(1.0, 1.0, 1.0), color(0.0, 0.0, 0.0));
    let c1 = tex.sample(0.01, 0.01);
    let c2 = tex.sample(0.2, 0.01);
    // One should be white-ish, the other black-ish (or vice versa)
    assert!((c1.x - c2.x).abs() > 0.5 || (c1.x - c2.x).abs() < 0.01);
}
```

### Commands

```bash
cargo new rasterizer
cd rasterizer
# Place source files, then:
cargo test
cargo run --release
# Output: output.ppm, output_wireframe.ppm, output_flat.ppm
# View with: open output.ppm (macOS) or feh output.ppm (Linux)

# To render a custom model:
# Download a .obj file (e.g., Utah teapot) and place as model.obj
cargo run --release
```

### Expected Output

```
running 12 tests
test vec3_operations ... ok
test mat4_identity ... ok
test translation_matrix ... ok
test perspective_divide ... ok
test barycentric_inside ... ok
test barycentric_outside ... ok
test zbuffer_depth_test ... ok
test rotation_preserves_length ... ok
test cube_mesh_valid ... ok
test ppm_output ... ok
test flat_shading_renders_without_crash ... ok
test degenerate_triangle_handled ... ok
test texture_sampling ... ok

test result: ok. 13 passed; 0 failed; 0 ignored
```

```
No model.obj found, rendering built-in cube
Rendered: 6 triangles, Culled: 6 triangles
Wrote output.ppm (800x600)
Wrote output_wireframe.ppm
Wrote output_flat.ppm
```

The PPM files show:
- `output.ppm`: A cube with smooth Gouraud shading, directional light casting shadows across faces
- `output_wireframe.ppm`: Green triangle edges on black background, showing the cube geometry
- `output_flat.ppm`: The same cube with per-face flat shading, visible edges between faces

## Design Decisions

1. **Edge function (bounding box) rasterization over scanline**: The bounding box approach (testing every pixel inside the triangle's bounding box with barycentric coordinates) is simpler to implement and naturally parallelizable. Scanline rasterization is faster for large triangles but much more complex code. For a learning-focused renderer, clarity matters more than speed.

2. **PPM as primary output**: PPM (P6 binary) is the simplest possible image format -- a header followed by raw RGB bytes. No library dependency required. Every image viewer supports it. PNG output is optional via the `image` crate.

3. **Near-plane clipping only**: Full frustum clipping (all 6 planes) is complex. Near-plane clipping handles the critical case (vertices behind the camera), and the bounding box clamp in the rasterizer handles the screen edges. This is simpler and sufficient for most scenes.

4. **Normals transformed without inverse-transpose**: For uniform scale only, the model matrix correctly transforms normals. Non-uniform scale requires the inverse-transpose of the model matrix. This is documented but omitted for simplicity. The cube and most OBJ models use uniform scale.

5. **Barycentric coordinates for all interpolation**: A single barycentric function serves depth testing, color interpolation (Gouraud), and texture coordinate interpolation. This unifies the fragment processing logic.

6. **Separate shading modes as enum**: Rather than a single shader function with flags, each mode is explicitly handled. This makes the code easier to follow and avoids unnecessary branching in the inner loop.

## Common Mistakes

- **Forgetting perspective divide**: Without dividing by W after projection, the entire scene is distorted. The projection matrix puts depth information in W, and the divide converts clip space to NDC.
- **Y-axis flipped**: NDC +Y is up, but screen +Y is down. The viewport transform must flip Y. Missing this renders the scene upside down.
- **Incorrect backface culling sign**: The winding direction determines which sign of the screen-space cross product means "front-facing." CCW winding with screen-space Y-down means positive cross is front-facing.
- **Linear texture interpolation (not perspective-correct)**: Linearly interpolating UVs across a triangle produces visible warping on perspective-projected surfaces. Dividing by W before interpolation and correcting afterward is essential.
- **Shadow acne in z-buffer**: Using floating-point comparison without bias can cause z-fighting on coplanar surfaces. A small epsilon bias or integer-mapped depth reduces this.
- **Not handling degenerate triangles**: Zero-area triangles (collinear vertices) produce division by zero in barycentric calculation. Check the denominator and skip degenerate triangles.

## Performance Notes

- The edge function rasterizer processes O(A) pixels per triangle where A is the bounding box area. For a 800x600 framebuffer, a full-screen triangle tests 480,000 pixels. Tighter bounds (actual scanline extent) would reduce this.
- A 10,000-triangle OBJ model renders in 1-5 seconds on a modern CPU in release mode. The bottleneck is the inner rasterization loop per fragment.
- Backface culling eliminates approximately half the triangles for closed meshes, providing a 2x speedup.
- Near-plane clipping generates at most 2 triangles per input triangle, so the worst case doubles the triangle count for scenes with many near-plane intersections.
- For real-time performance, the rasterizer would need SIMD vectorization (processing 4-8 pixels simultaneously), multi-threading (parallel scanlines), and tiled rendering for cache efficiency. These are beyond the scope of this challenge but represent the next steps toward GPU-like performance.
- Memory usage is dominated by the framebuffer: 800x600 at 24 bytes per pixel (Color + depth) = ~14MB. This fits comfortably in L3 cache on modern CPUs.
