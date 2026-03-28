# Solution: Ray Tracing Engine

## Architecture Overview

The renderer is split into five modules:

1. **math**: `Vec3`, `Ray`, color operations, epsilon constants
2. **geometry**: `Hittable` trait, `Sphere`, `Plane`, `AABB`, `HitRecord`
3. **material**: `Material` enum (Lambertian, Metal, Dielectric), Phong shading, Fresnel
4. **scene**: JSON scene loader, camera, lights, BVH construction
5. **render**: Pixel sampling, recursive ray color, parallel scanline dispatch

```
JSON Scene File
     |
     v
 [Scene Loader] --> Camera, Objects, Lights, Settings
     |
     v
 [BVH Builder] --> Tree of AABBs over objects
     |
     v
 [Renderer (rayon)] --> For each pixel:
     |                    - Generate primary ray(s) with jitter
     |                    - Traverse BVH for closest hit
     |                    - Compute Phong shading + shadows
     |                    - Recursively trace reflection/refraction
     |                    - Average samples
     v
 [Image Writer] --> PPM / PNG
```

## Rust Solution

### Cargo.toml

```toml
[package]
name = "raytracer"
version = "0.1.0"
edition = "2021"

[dependencies]
rayon = "1.10"
serde = { version = "1", features = ["derive"] }
serde_json = "1"
image = { version = "0.25", optional = true }
rand = "0.8"

[features]
default = []
png_output = ["image"]

[[bin]]
name = "raytracer"
path = "src/main.rs"

[profile.release]
opt-level = 3
lto = true
```

### src/math.rs

```rust
use std::ops::{Add, Sub, Mul, Neg};

pub const EPSILON: f64 = 1e-8;
pub const INFINITY: f64 = f64::MAX;

#[derive(Debug, Clone, Copy)]
pub struct Vec3 {
    pub x: f64,
    pub y: f64,
    pub z: f64,
}

impl Vec3 {
    pub fn new(x: f64, y: f64, z: f64) -> Self {
        Vec3 { x, y, z }
    }

    pub fn zero() -> Self {
        Vec3::new(0.0, 0.0, 0.0)
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

    pub fn length_squared(self) -> f64 {
        self.dot(self)
    }

    pub fn length(self) -> f64 {
        self.length_squared().sqrt()
    }

    pub fn normalized(self) -> Vec3 {
        let len = self.length();
        if len < EPSILON {
            return Vec3::zero();
        }
        self * (1.0 / len)
    }

    pub fn reflect(self, normal: Vec3) -> Vec3 {
        self - normal * 2.0 * self.dot(normal)
    }

    pub fn refract(self, normal: Vec3, eta_ratio: f64) -> Option<Vec3> {
        let cos_i = (-self).dot(normal).min(1.0);
        let sin2_t = eta_ratio * eta_ratio * (1.0 - cos_i * cos_i);
        if sin2_t > 1.0 {
            return None; // Total internal reflection
        }
        let cos_t = (1.0 - sin2_t).sqrt();
        Some(self * eta_ratio + normal * (eta_ratio * cos_i - cos_t))
    }

    pub fn component_mul(self, other: Vec3) -> Vec3 {
        Vec3::new(self.x * other.x, self.y * other.y, self.z * other.z)
    }

    pub fn clamp(self, min: f64, max: f64) -> Vec3 {
        Vec3::new(
            self.x.clamp(min, max),
            self.y.clamp(min, max),
            self.z.clamp(min, max),
        )
    }
}

impl Add for Vec3 {
    type Output = Vec3;
    fn add(self, rhs: Vec3) -> Vec3 {
        Vec3::new(self.x + rhs.x, self.y + rhs.y, self.z + rhs.z)
    }
}

impl Sub for Vec3 {
    type Output = Vec3;
    fn sub(self, rhs: Vec3) -> Vec3 {
        Vec3::new(self.x - rhs.x, self.y - rhs.y, self.z - rhs.z)
    }
}

impl Mul<f64> for Vec3 {
    type Output = Vec3;
    fn mul(self, t: f64) -> Vec3 {
        Vec3::new(self.x * t, self.y * t, self.z * t)
    }
}

impl Neg for Vec3 {
    type Output = Vec3;
    fn neg(self) -> Vec3 {
        Vec3::new(-self.x, -self.y, -self.z)
    }
}

pub type Color = Vec3;

#[derive(Debug, Clone, Copy)]
pub struct Ray {
    pub origin: Vec3,
    pub direction: Vec3,
}

impl Ray {
    pub fn new(origin: Vec3, direction: Vec3) -> Self {
        Ray { origin, direction: direction.normalized() }
    }

    pub fn at(self, t: f64) -> Vec3 {
        self.origin + self.direction * t
    }
}
```

### src/geometry.rs

```rust
use crate::math::*;

#[derive(Debug, Clone, Copy)]
pub struct HitRecord {
    pub point: Vec3,
    pub normal: Vec3,
    pub t: f64,
    pub front_face: bool,
    pub material_index: usize,
}

impl HitRecord {
    pub fn with_face_normal(
        point: Vec3,
        t: f64,
        outward_normal: Vec3,
        ray_dir: Vec3,
        material_index: usize,
    ) -> Self {
        let front_face = ray_dir.dot(outward_normal) < 0.0;
        let normal = if front_face { outward_normal } else { -outward_normal };
        HitRecord { point, normal, t, front_face, material_index }
    }
}

#[derive(Debug, Clone, Copy)]
pub struct AABB {
    pub min: Vec3,
    pub max: Vec3,
}

impl AABB {
    pub fn new(min: Vec3, max: Vec3) -> Self {
        AABB { min, max }
    }

    pub fn hit(&self, ray: &Ray, mut t_min: f64, mut t_max: f64) -> bool {
        for axis in 0..3 {
            let (origin, dir, bmin, bmax) = match axis {
                0 => (ray.origin.x, ray.direction.x, self.min.x, self.max.x),
                1 => (ray.origin.y, ray.direction.y, self.min.y, self.max.y),
                _ => (ray.origin.z, ray.direction.z, self.min.z, self.max.z),
            };
            let inv_d = 1.0 / dir;
            let mut t0 = (bmin - origin) * inv_d;
            let mut t1 = (bmax - origin) * inv_d;
            if inv_d < 0.0 {
                std::mem::swap(&mut t0, &mut t1);
            }
            t_min = t_min.max(t0);
            t_max = t_max.min(t1);
            if t_max <= t_min {
                return false;
            }
        }
        true
    }

    pub fn surrounding(a: &AABB, b: &AABB) -> AABB {
        AABB {
            min: Vec3::new(
                a.min.x.min(b.min.x),
                a.min.y.min(b.min.y),
                a.min.z.min(b.min.z),
            ),
            max: Vec3::new(
                a.max.x.max(b.max.x),
                a.max.y.max(b.max.y),
                a.max.z.max(b.max.z),
            ),
        }
    }

    pub fn surface_area(&self) -> f64 {
        let d = self.max - self.min;
        2.0 * (d.x * d.y + d.y * d.z + d.z * d.x)
    }
}

pub trait Hittable: Send + Sync {
    fn hit(&self, ray: &Ray, t_min: f64, t_max: f64) -> Option<HitRecord>;
    fn bounding_box(&self) -> AABB;
}

pub struct Sphere {
    pub center: Vec3,
    pub radius: f64,
    pub material_index: usize,
}

impl Hittable for Sphere {
    fn hit(&self, ray: &Ray, t_min: f64, t_max: f64) -> Option<HitRecord> {
        let oc = ray.origin - self.center;
        let a = ray.direction.length_squared();
        let half_b = oc.dot(ray.direction);
        let c = oc.length_squared() - self.radius * self.radius;
        let discriminant = half_b * half_b - a * c;

        if discriminant < 0.0 {
            return None;
        }

        let sqrt_d = discriminant.sqrt();
        let mut root = (-half_b - sqrt_d) / a;
        if root < t_min || root > t_max {
            root = (-half_b + sqrt_d) / a;
            if root < t_min || root > t_max {
                return None;
            }
        }

        let point = ray.at(root);
        let outward_normal = (point - self.center) * (1.0 / self.radius);
        Some(HitRecord::with_face_normal(
            point, root, outward_normal, ray.direction, self.material_index,
        ))
    }

    fn bounding_box(&self) -> AABB {
        let r = Vec3::new(self.radius, self.radius, self.radius);
        AABB::new(self.center - r, self.center + r)
    }
}

pub struct Plane {
    pub point: Vec3,
    pub normal: Vec3,
    pub material_index: usize,
}

impl Hittable for Plane {
    fn hit(&self, ray: &Ray, t_min: f64, t_max: f64) -> Option<HitRecord> {
        let denom = self.normal.dot(ray.direction);
        if denom.abs() < EPSILON {
            return None; // Ray parallel to plane
        }
        let t = (self.point - ray.origin).dot(self.normal) / denom;
        if t < t_min || t > t_max {
            return None;
        }
        let point = ray.at(t);
        Some(HitRecord::with_face_normal(
            point, t, self.normal, ray.direction, self.material_index,
        ))
    }

    fn bounding_box(&self) -> AABB {
        // Infinite plane: use large bounds on the non-normal axes.
        let big = 1e6;
        let offset = Vec3::new(
            if self.normal.x.abs() > 0.9 { EPSILON } else { big },
            if self.normal.y.abs() > 0.9 { EPSILON } else { big },
            if self.normal.z.abs() > 0.9 { EPSILON } else { big },
        );
        AABB::new(self.point - offset, self.point + offset)
    }
}
```

### src/bvh.rs

```rust
use crate::geometry::*;
use crate::math::*;

pub enum BvhNode {
    Leaf {
        bbox: AABB,
        object_index: usize,
    },
    Interior {
        bbox: AABB,
        left: Box<BvhNode>,
        right: Box<BvhNode>,
    },
}

impl BvhNode {
    /// Build a BVH using the Surface Area Heuristic (SAH).
    pub fn build(objects: &[Box<dyn Hittable>], indices: &mut [usize]) -> BvhNode {
        if indices.len() == 1 {
            let idx = indices[0];
            return BvhNode::Leaf {
                bbox: objects[idx].bounding_box(),
                object_index: idx,
            };
        }

        // Compute bounding box of all objects in this node.
        let mut node_bbox = objects[indices[0]].bounding_box();
        for &idx in indices.iter().skip(1) {
            node_bbox = AABB::surrounding(&node_bbox, &objects[idx].bounding_box());
        }

        if indices.len() == 2 {
            let left = BvhNode::Leaf {
                bbox: objects[indices[0]].bounding_box(),
                object_index: indices[0],
            };
            let right = BvhNode::Leaf {
                bbox: objects[indices[1]].bounding_box(),
                object_index: indices[1],
            };
            return BvhNode::Interior {
                bbox: node_bbox,
                left: Box::new(left),
                right: Box::new(right),
            };
        }

        // SAH: try splits along each axis, pick the cheapest.
        let mut best_cost = f64::MAX;
        let mut best_axis = 0;
        let mut best_split = indices.len() / 2;

        for axis in 0..3 {
            indices.sort_by(|&a, &b| {
                let ca = centroid(&objects[a].bounding_box(), axis);
                let cb = centroid(&objects[b].bounding_box(), axis);
                ca.partial_cmp(&cb).unwrap()
            });

            // Evaluate SAH at each possible split position.
            for split in 1..indices.len() {
                let mut left_bbox = objects[indices[0]].bounding_box();
                for &idx in &indices[1..split] {
                    left_bbox = AABB::surrounding(&left_bbox, &objects[idx].bounding_box());
                }

                let mut right_bbox = objects[indices[split]].bounding_box();
                for &idx in &indices[split + 1..] {
                    right_bbox = AABB::surrounding(&right_bbox, &objects[idx].bounding_box());
                }

                let cost = (split as f64 * left_bbox.surface_area()
                    + (indices.len() - split) as f64 * right_bbox.surface_area())
                    / node_bbox.surface_area();

                if cost < best_cost {
                    best_cost = cost;
                    best_axis = axis;
                    best_split = split;
                }
            }
        }

        // Sort by best axis and split.
        indices.sort_by(|&a, &b| {
            let ca = centroid(&objects[a].bounding_box(), best_axis);
            let cb = centroid(&objects[b].bounding_box(), best_axis);
            ca.partial_cmp(&cb).unwrap()
        });

        let (left_indices, right_indices) = indices.split_at_mut(best_split);
        let left = BvhNode::build(objects, left_indices);
        let right = BvhNode::build(objects, right_indices);

        BvhNode::Interior {
            bbox: node_bbox,
            left: Box::new(left),
            right: Box::new(right),
        }
    }

    pub fn hit(
        &self,
        ray: &Ray,
        t_min: f64,
        t_max: f64,
        objects: &[Box<dyn Hittable>],
    ) -> Option<HitRecord> {
        match self {
            BvhNode::Leaf { bbox, object_index } => {
                if !bbox.hit(ray, t_min, t_max) {
                    return None;
                }
                objects[*object_index].hit(ray, t_min, t_max)
            }
            BvhNode::Interior { bbox, left, right } => {
                if !bbox.hit(ray, t_min, t_max) {
                    return None;
                }
                let left_hit = left.hit(ray, t_min, t_max, objects);
                let t_closest = left_hit.as_ref().map_or(t_max, |h| h.t);
                let right_hit = right.hit(ray, t_min, t_closest, objects);
                right_hit.or(left_hit)
            }
        }
    }
}

fn centroid(bbox: &AABB, axis: usize) -> f64 {
    match axis {
        0 => (bbox.min.x + bbox.max.x) * 0.5,
        1 => (bbox.min.y + bbox.max.y) * 0.5,
        _ => (bbox.min.z + bbox.max.z) * 0.5,
    }
}
```

### src/material.rs

```rust
use crate::math::*;

#[derive(Debug, Clone)]
pub struct PhongMaterial {
    pub ambient: Color,
    pub diffuse: Color,
    pub specular: Color,
    pub shininess: f64,
    pub reflectivity: f64,       // 0.0 = no reflection, 1.0 = mirror
    pub transparency: f64,       // 0.0 = opaque, 1.0 = fully transparent
    pub refractive_index: f64,   // glass ~1.5, water ~1.33
}

impl PhongMaterial {
    pub fn lambertian(color: Color) -> Self {
        PhongMaterial {
            ambient: color * 0.1,
            diffuse: color,
            specular: Color::new(0.3, 0.3, 0.3),
            shininess: 32.0,
            reflectivity: 0.0,
            transparency: 0.0,
            refractive_index: 1.0,
        }
    }

    pub fn metal(color: Color, shininess: f64) -> Self {
        PhongMaterial {
            ambient: color * 0.05,
            diffuse: color * 0.3,
            specular: Color::new(0.9, 0.9, 0.9),
            shininess,
            reflectivity: 0.8,
            transparency: 0.0,
            refractive_index: 1.0,
        }
    }

    pub fn glass(refractive_index: f64) -> Self {
        PhongMaterial {
            ambient: Color::new(0.0, 0.0, 0.0),
            diffuse: Color::new(0.05, 0.05, 0.05),
            specular: Color::new(1.0, 1.0, 1.0),
            shininess: 128.0,
            reflectivity: 0.1,
            transparency: 0.9,
            refractive_index,
        }
    }
}

/// Schlick's approximation for Fresnel reflectance.
pub fn schlick(cosine: f64, refractive_index: f64) -> f64 {
    let r0 = ((1.0 - refractive_index) / (1.0 + refractive_index)).powi(2);
    r0 + (1.0 - r0) * (1.0 - cosine).powi(5)
}
```

### src/light.rs

```rust
use crate::math::*;

#[derive(Debug, Clone)]
pub enum Light {
    Point {
        position: Vec3,
        color: Color,
        intensity: f64,
    },
    Directional {
        direction: Vec3,
        color: Color,
        intensity: f64,
    },
}

impl Light {
    /// Returns (direction_to_light, distance_to_light, color * intensity).
    pub fn illuminate(&self, point: Vec3) -> (Vec3, f64, Color) {
        match self {
            Light::Point { position, color, intensity } => {
                let dir = *position - point;
                let dist = dir.length();
                let attenuation = intensity / (dist * dist);
                (dir.normalized(), dist, *color * attenuation)
            }
            Light::Directional { direction, color, intensity } => {
                ((-*direction).normalized(), f64::MAX, *color * *intensity)
            }
        }
    }
}
```

### src/camera.rs

```rust
use crate::math::*;

pub struct Camera {
    pub origin: Vec3,
    lower_left: Vec3,
    horizontal: Vec3,
    vertical: Vec3,
}

impl Camera {
    pub fn new(
        look_from: Vec3,
        look_at: Vec3,
        vup: Vec3,
        vfov_degrees: f64,
        aspect_ratio: f64,
    ) -> Self {
        let theta = vfov_degrees.to_radians();
        let h = (theta / 2.0).tan();
        let viewport_height = 2.0 * h;
        let viewport_width = aspect_ratio * viewport_height;

        let w = (look_from - look_at).normalized();
        let u = vup.cross(w).normalized();
        let v = w.cross(u);

        let horizontal = u * viewport_width;
        let vertical = v * viewport_height;
        let lower_left = look_from - horizontal * 0.5 - vertical * 0.5 - w;

        Camera {
            origin: look_from,
            lower_left,
            horizontal,
            vertical,
        }
    }

    /// Generate a ray for normalized screen coordinates (s, t) in [0, 1].
    pub fn get_ray(&self, s: f64, t: f64) -> Ray {
        let direction = self.lower_left + self.horizontal * s + self.vertical * t - self.origin;
        Ray::new(self.origin, direction)
    }
}
```

### src/scene.rs

```rust
use serde::Deserialize;
use crate::math::*;
use crate::geometry::*;
use crate::material::*;
use crate::light::*;
use crate::camera::*;

#[derive(Deserialize)]
pub struct SceneConfig {
    pub camera: CameraConfig,
    pub settings: RenderSettings,
    pub materials: Vec<MaterialConfig>,
    pub objects: Vec<ObjectConfig>,
    pub lights: Vec<LightConfig>,
}

#[derive(Deserialize)]
pub struct CameraConfig {
    pub look_from: [f64; 3],
    pub look_at: [f64; 3],
    pub vup: [f64; 3],
    pub fov: f64,
}

#[derive(Deserialize)]
pub struct RenderSettings {
    pub width: u32,
    pub height: u32,
    pub samples_per_pixel: u32,
    pub max_depth: u32,
}

#[derive(Deserialize)]
pub struct MaterialConfig {
    pub name: String,
    #[serde(rename = "type")]
    pub mat_type: String,
    pub color: Option<[f64; 3]>,
    pub shininess: Option<f64>,
    pub refractive_index: Option<f64>,
}

#[derive(Deserialize)]
pub struct ObjectConfig {
    #[serde(rename = "type")]
    pub obj_type: String,
    pub center: Option<[f64; 3]>,
    pub radius: Option<f64>,
    pub point: Option<[f64; 3]>,
    pub normal: Option<[f64; 3]>,
    pub material: String,
}

#[derive(Deserialize)]
pub struct LightConfig {
    #[serde(rename = "type")]
    pub light_type: String,
    pub position: Option<[f64; 3]>,
    pub direction: Option<[f64; 3]>,
    pub color: [f64; 3],
    pub intensity: f64,
}

pub struct Scene {
    pub camera: Camera,
    pub settings: RenderSettings,
    pub objects: Vec<Box<dyn Hittable>>,
    pub materials: Vec<PhongMaterial>,
    pub lights: Vec<Light>,
}

impl Scene {
    pub fn from_config(config: SceneConfig) -> Self {
        let aspect = config.settings.width as f64 / config.settings.height as f64;
        let cam = &config.camera;
        let camera = Camera::new(
            Vec3::new(cam.look_from[0], cam.look_from[1], cam.look_from[2]),
            Vec3::new(cam.look_at[0], cam.look_at[1], cam.look_at[2]),
            Vec3::new(cam.vup[0], cam.vup[1], cam.vup[2]),
            cam.fov,
            aspect,
        );

        let mut material_map: Vec<(String, PhongMaterial)> = Vec::new();
        for mc in &config.materials {
            let mat = match mc.mat_type.as_str() {
                "lambertian" => {
                    let c = mc.color.unwrap_or([0.5, 0.5, 0.5]);
                    PhongMaterial::lambertian(Color::new(c[0], c[1], c[2]))
                }
                "metal" => {
                    let c = mc.color.unwrap_or([0.8, 0.8, 0.8]);
                    PhongMaterial::metal(
                        Color::new(c[0], c[1], c[2]),
                        mc.shininess.unwrap_or(64.0),
                    )
                }
                "glass" => PhongMaterial::glass(mc.refractive_index.unwrap_or(1.5)),
                _ => PhongMaterial::lambertian(Color::new(0.5, 0.5, 0.5)),
            };
            material_map.push((mc.name.clone(), mat));
        }

        let materials: Vec<PhongMaterial> = material_map.iter().map(|(_, m)| m.clone()).collect();

        let find_mat = |name: &str| -> usize {
            material_map
                .iter()
                .position(|(n, _)| n == name)
                .unwrap_or(0)
        };

        let mut objects: Vec<Box<dyn Hittable>> = Vec::new();
        for oc in &config.objects {
            match oc.obj_type.as_str() {
                "sphere" => {
                    let c = oc.center.unwrap_or([0.0, 0.0, 0.0]);
                    objects.push(Box::new(Sphere {
                        center: Vec3::new(c[0], c[1], c[2]),
                        radius: oc.radius.unwrap_or(1.0),
                        material_index: find_mat(&oc.material),
                    }));
                }
                "plane" => {
                    let p = oc.point.unwrap_or([0.0, 0.0, 0.0]);
                    let n = oc.normal.unwrap_or([0.0, 1.0, 0.0]);
                    objects.push(Box::new(Plane {
                        point: Vec3::new(p[0], p[1], p[2]),
                        normal: Vec3::new(n[0], n[1], n[2]).normalized(),
                        material_index: find_mat(&oc.material),
                    }));
                }
                _ => eprintln!("unknown object type: {}", oc.obj_type),
            }
        }

        let lights: Vec<Light> = config.lights.iter().map(|lc| {
            let color = Color::new(lc.color[0], lc.color[1], lc.color[2]);
            match lc.light_type.as_str() {
                "point" => {
                    let p = lc.position.unwrap_or([0.0, 10.0, 0.0]);
                    Light::Point {
                        position: Vec3::new(p[0], p[1], p[2]),
                        color,
                        intensity: lc.intensity,
                    }
                }
                "directional" => {
                    let d = lc.direction.unwrap_or([0.0, -1.0, 0.0]);
                    Light::Directional {
                        direction: Vec3::new(d[0], d[1], d[2]).normalized(),
                        color,
                        intensity: lc.intensity,
                    }
                }
                _ => Light::Point {
                    position: Vec3::new(0.0, 10.0, 0.0),
                    color,
                    intensity: lc.intensity,
                },
            }
        }).collect();

        Scene { camera, settings: config.settings, objects, materials, lights }
    }
}
```

### src/render.rs

```rust
use crate::math::*;
use crate::geometry::*;
use crate::material::*;
use crate::light::*;
use crate::bvh::*;
use crate::scene::*;
use rand::Rng;
use rayon::prelude::*;

pub fn render(scene: &Scene) -> Vec<Color> {
    let width = scene.settings.width as usize;
    let height = scene.settings.height as usize;
    let spp = scene.settings.samples_per_pixel;
    let max_depth = scene.settings.max_depth;

    // Build BVH
    let mut indices: Vec<usize> = (0..scene.objects.len()).collect();
    let bvh = if scene.objects.len() > 1 {
        Some(BvhNode::build(&scene.objects, &mut indices))
    } else {
        None
    };

    // Render scanlines in parallel.
    let pixels: Vec<Color> = (0..height)
        .into_par_iter()
        .flat_map(|j| {
            let mut rng = rand::thread_rng();
            let mut row = Vec::with_capacity(width);
            for i in 0..width {
                let mut color = Color::zero();
                for _ in 0..spp {
                    let u = (i as f64 + rng.gen::<f64>()) / (width as f64 - 1.0);
                    let v = ((height - 1 - j) as f64 + rng.gen::<f64>()) / (height as f64 - 1.0);
                    let ray = scene.camera.get_ray(u, v);
                    color = color + ray_color(
                        &ray, scene, &bvh, max_depth,
                    );
                }
                row.push(color * (1.0 / spp as f64));
            }
            row
        })
        .collect();

    pixels
}

fn ray_color(
    ray: &Ray,
    scene: &Scene,
    bvh: &Option<BvhNode>,
    depth: u32,
) -> Color {
    if depth == 0 {
        return Color::zero();
    }

    let hit = find_closest_hit(ray, scene, bvh);

    match hit {
        None => sky_color(ray),
        Some(rec) => {
            let material = &scene.materials[rec.material_index];
            shade(ray, &rec, material, scene, bvh, depth)
        }
    }
}

fn find_closest_hit(
    ray: &Ray,
    scene: &Scene,
    bvh: &Option<BvhNode>,
) -> Option<HitRecord> {
    match bvh {
        Some(node) => node.hit(ray, EPSILON, INFINITY, &scene.objects),
        None => {
            let mut closest: Option<HitRecord> = None;
            let mut t_max = INFINITY;
            for obj in &scene.objects {
                if let Some(rec) = obj.hit(ray, EPSILON, t_max) {
                    t_max = rec.t;
                    closest = Some(rec);
                }
            }
            closest
        }
    }
}

fn shade(
    ray: &Ray,
    hit: &HitRecord,
    material: &PhongMaterial,
    scene: &Scene,
    bvh: &Option<BvhNode>,
    depth: u32,
) -> Color {
    let mut color = material.ambient;

    // Phong shading for each light.
    for light in &scene.lights {
        let (light_dir, light_dist, light_color) = light.illuminate(hit.point);

        // Shadow ray
        let shadow_origin = hit.point + hit.normal * EPSILON;
        let shadow_ray = Ray::new(shadow_origin, light_dir);
        let in_shadow = find_closest_hit(&shadow_ray, scene, bvh)
            .map_or(false, |shadow_hit| shadow_hit.t < light_dist);

        if in_shadow {
            continue;
        }

        // Diffuse (Lambert)
        let n_dot_l = hit.normal.dot(light_dir).max(0.0);
        color = color + material.diffuse.component_mul(light_color) * n_dot_l;

        // Specular (Blinn-Phong)
        let view_dir = (-ray.direction).normalized();
        let halfway = (light_dir + view_dir).normalized();
        let n_dot_h = hit.normal.dot(halfway).max(0.0);
        let spec = n_dot_h.powf(material.shininess);
        color = color + material.specular.component_mul(light_color) * spec;
    }

    // Reflection
    if material.reflectivity > 0.0 && depth > 1 {
        let reflect_dir = ray.direction.reflect(hit.normal);
        let reflect_origin = hit.point + hit.normal * EPSILON;
        let reflect_ray = Ray::new(reflect_origin, reflect_dir);
        let reflect_color = ray_color(&reflect_ray, scene, bvh, depth - 1);
        color = color + reflect_color * material.reflectivity;
    }

    // Refraction (transparency)
    if material.transparency > 0.0 && depth > 1 {
        let eta_ratio = if hit.front_face {
            1.0 / material.refractive_index
        } else {
            material.refractive_index
        };

        let cos_theta = (-ray.direction).dot(hit.normal).min(1.0);
        let fresnel = schlick(cos_theta, material.refractive_index);

        match ray.direction.refract(hit.normal, eta_ratio) {
            Some(refract_dir) => {
                let refract_origin = hit.point - hit.normal * EPSILON;
                let refract_ray = Ray::new(refract_origin, refract_dir);
                let refract_color = ray_color(&refract_ray, scene, bvh, depth - 1);

                // Blend reflection and refraction using Fresnel.
                let reflect_dir = ray.direction.reflect(hit.normal);
                let reflect_origin = hit.point + hit.normal * EPSILON;
                let reflect_ray = Ray::new(reflect_origin, reflect_dir);
                let reflect_color = ray_color(&reflect_ray, scene, bvh, depth - 1);

                let blended = reflect_color * fresnel + refract_color * (1.0 - fresnel);
                color = color * (1.0 - material.transparency) + blended * material.transparency;
            }
            None => {
                // Total internal reflection
                let reflect_dir = ray.direction.reflect(hit.normal);
                let reflect_origin = hit.point + hit.normal * EPSILON;
                let reflect_ray = Ray::new(reflect_origin, reflect_dir);
                let reflect_color = ray_color(&reflect_ray, scene, bvh, depth - 1);
                color = color * (1.0 - material.transparency) + reflect_color * material.transparency;
            }
        }
    }

    color
}

fn sky_color(ray: &Ray) -> Color {
    let unit_dir = ray.direction.normalized();
    let t = 0.5 * (unit_dir.y + 1.0);
    Color::new(1.0, 1.0, 1.0) * (1.0 - t) + Color::new(0.5, 0.7, 1.0) * t
}

pub fn write_ppm(pixels: &[Color], width: u32, height: u32, path: &str) -> std::io::Result<()> {
    use std::io::Write;
    let mut file = std::fs::File::create(path)?;
    writeln!(file, "P3")?;
    writeln!(file, "{} {}", width, height)?;
    writeln!(file, "255")?;

    for color in pixels {
        let c = color.clamp(0.0, 1.0);
        // Gamma correction (gamma = 2.0)
        let r = (c.x.sqrt() * 255.0) as u8;
        let g = (c.y.sqrt() * 255.0) as u8;
        let b = (c.z.sqrt() * 255.0) as u8;
        writeln!(file, "{} {} {}", r, g, b)?;
    }

    Ok(())
}
```

### src/main.rs

```rust
mod math;
mod geometry;
mod bvh;
mod material;
mod light;
mod camera;
mod scene;
mod render;

use scene::{Scene, SceneConfig};
use render::{render, write_ppm};
use std::time::Instant;

fn main() {
    let args: Vec<String> = std::env::args().collect();
    let scene_path = args.get(1).map_or("scene.json", |s| s.as_str());
    let output_path = args.get(2).map_or("output.ppm", |s| s.as_str());

    let config_str = std::fs::read_to_string(scene_path)
        .unwrap_or_else(|e| panic!("failed to read scene file '{}': {}", scene_path, e));

    let config: SceneConfig = serde_json::from_str(&config_str)
        .unwrap_or_else(|e| panic!("failed to parse scene JSON: {}", e));

    let scene = Scene::from_config(config);

    println!(
        "rendering {}x{} @ {} spp, max depth {}",
        scene.settings.width, scene.settings.height,
        scene.settings.samples_per_pixel, scene.settings.max_depth,
    );
    println!("{} objects, {} lights", scene.objects.len(), scene.lights.len());

    let start = Instant::now();
    let pixels = render(&scene);
    let elapsed = start.elapsed();

    println!("render time: {:.2}s", elapsed.as_secs_f64());

    write_ppm(&pixels, scene.settings.width, scene.settings.height, output_path)
        .unwrap_or_else(|e| panic!("failed to write output: {}", e));

    println!("saved to {}", output_path);
}
```

### Example Scene File (scene.json)

```json
{
  "camera": {
    "look_from": [0, 2, 6],
    "look_at": [0, 0.5, 0],
    "vup": [0, 1, 0],
    "fov": 45
  },
  "settings": {
    "width": 800,
    "height": 600,
    "samples_per_pixel": 16,
    "max_depth": 8
  },
  "materials": [
    { "name": "ground", "type": "lambertian", "color": [0.5, 0.5, 0.5] },
    { "name": "red", "type": "lambertian", "color": [0.8, 0.2, 0.1] },
    { "name": "mirror", "type": "metal", "color": [0.9, 0.9, 0.9], "shininess": 128 },
    { "name": "glass", "type": "glass", "refractive_index": 1.5 }
  ],
  "objects": [
    { "type": "plane", "point": [0, 0, 0], "normal": [0, 1, 0], "material": "ground" },
    { "type": "sphere", "center": [-1.5, 1, 0], "radius": 1.0, "material": "red" },
    { "type": "sphere", "center": [0, 1, 0], "radius": 1.0, "material": "glass" },
    { "type": "sphere", "center": [1.5, 1, 0], "radius": 1.0, "material": "mirror" }
  ],
  "lights": [
    { "type": "point", "position": [5, 10, 5], "color": [1, 1, 1], "intensity": 150 },
    { "type": "directional", "direction": [-1, -1, -0.5], "color": [0.3, 0.3, 0.5], "intensity": 0.3 }
  ]
}
```

## Running

```bash
cargo init raytracer
cd raytracer

# Add dependencies to Cargo.toml (see above)
# Place module files in src/

# Build and run
cargo run --release -- scene.json output.ppm

# View the output
open output.ppm          # macOS
# or: feh output.ppm     # Linux
# or: display output.ppm # ImageMagick

# Benchmark BVH vs brute force: generate a scene with 1000 random spheres
# and time both approaches
```

## Expected Output

```
rendering 800x600 @ 16 spp, max depth 8
4 objects, 2 lights
render time: 2.34s
saved to output.ppm
```

The output image shows:
- A gray ground plane extending to the horizon
- A matte red sphere on the left with visible shadow
- A glass sphere in the center showing refraction (objects behind it appear distorted) and Fresnel reflections at the edges
- A mirror sphere on the right reflecting the red sphere, the glass sphere, and the ground
- Sky gradient from white (horizon) to blue (zenith)
- Smooth edges from 16x supersampling anti-aliasing

## Design Decisions

1. **Phong over physically-based BRDF**: Phong shading is simpler to implement and debug, produces recognizable results (specular highlights, diffuse falloff), and is sufficient for demonstrating ray tracing fundamentals. Upgrading to Cook-Torrance or GGX is a natural extension.

2. **SAH-based BVH over grid/octree**: The surface area heuristic produces near-optimal BVH trees for arbitrary geometry distributions. Grids waste space on empty regions, octrees have deeper traversal for non-uniform scenes. SAH adapts to geometry density automatically.

3. **Scanline parallelism with rayon**: Each scanline is independent -- no shared mutable state during rendering. Rayon's `par_iter` handles work stealing automatically. Tile-based rendering (e.g., 16x16 tiles) can improve cache locality but adds complexity with minimal gain for this scale.

4. **Intrusive free list in BVH nodes (enum)**: Using a Rust enum (`Leaf` / `Interior`) for BVH nodes avoids heap indirection for leaves and lets the compiler optimize pattern matching into jump tables. The alternative (trait object with virtual dispatch) adds cache misses.

5. **Separate material vector with index references**: Objects store a `material_index` instead of cloning or referencing the material. This avoids lifetime complexity, allows materials to be shared across objects, and simplifies JSON deserialization.

6. **Gamma correction at output**: All internal calculations happen in linear color space. Gamma correction (sqrt for gamma 2.0) is applied only when writing pixel values to the image file. This ensures lighting calculations are physically correct.

7. **Epsilon offset for shadow and secondary rays**: Moving the ray origin slightly along the normal prevents shadow acne (the surface intersecting with its own shadow ray) and self-intersection artifacts on reflective and refractive surfaces.

## Common Mistakes

1. **Shadow acne from self-intersection**: If the shadow ray origin is exactly on the surface, floating-point imprecision makes it hit the same surface. Always offset the origin by `normal * EPSILON` (push outward for reflection/shadow, inward for refraction).

2. **Incorrect refraction direction**: When the ray enters a medium (front face), the ratio is `1.0 / n`. When it exits, the ratio is `n / 1.0`. Getting this backwards inverts the distortion. Also, Snell's law can fail when `sin(theta_t) > 1` -- that is total internal reflection, not a bug.

3. **Forgetting to normalize**: Reflected and refracted direction vectors must be normalized before tracing secondary rays. Unnormalized directions cause incorrect intersection distances and visible artifacts.

4. **BVH traversal bug -- not updating t_max**: When the left child returns a hit, the right child must use that hit's `t` as the new `t_max`. Without this, the right child may return a farther hit and the left hit is lost.

5. **Energy creation in Phong model**: If reflectivity + transparency + diffuse contribution > 1.0, the scene appears washed out because more light leaves the surface than arrives. Ensure energy conservation by clamping or normalizing contributions.

## Performance Notes

The primary bottleneck is ray-object intersection testing. For N objects without acceleration:

- **Brute force**: O(N) intersections per ray. A 1000-object scene at 800x600 with 16 spp fires ~7.7 billion intersection tests.
- **BVH**: O(log N) intersections per ray. The same scene requires ~80 million tests -- nearly 100x fewer.

Other optimizations that matter in order of impact:

1. **SIMD ray-AABB intersection**: The slab method for AABB testing vectorizes cleanly with `_mm_max_ps` / `_mm_min_ps`. This is the inner loop of BVH traversal.
2. **Memory layout**: Store BVH nodes in a flat array (implicit tree) instead of heap-allocated Box nodes. This reduces cache misses during traversal.
3. **Ray coherence**: Nearby pixels cast similar rays. Packet tracing (testing 4 or 8 rays against an AABB simultaneously) exploits SIMD width.
4. **Avoid recursion**: Convert recursive ray tracing to an iterative loop with an explicit stack. This eliminates function call overhead for deep recursion.

## Going Further

- Add triangle meshes with OBJ file loading and per-vertex normals (smooth shading)
- Implement texture mapping: UV coordinates on spheres, checkerboard procedural textures on planes
- Add depth of field by jittering the ray origin across a virtual lens aperture
- Implement importance sampling for Monte Carlo path tracing, replacing Phong with a physically-based BRDF
- Add motion blur by interpolating object positions between shutter open and close times
- Implement adaptive sampling: spend more samples on high-variance pixels (edges, reflections), fewer on uniform regions
- Port the inner loop to SIMD using `std::simd` (nightly) or `packed_simd2`
- Add GPU acceleration via `wgpu` compute shaders for massively parallel ray tracing
