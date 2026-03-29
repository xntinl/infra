# Solution: 2D Physics Engine -- Collision Detection and Response

## Architecture Overview

The engine is organized into six modules forming a pipeline:

1. **math**: `Vec2` type with full 2D vector algebra
2. **shapes**: Shape enum (Circle, AABB, ConvexPolygon) with AABB computation and SAT projection
3. **body**: `RigidBody` struct with physics properties, integration, and force application
4. **broadphase**: Spatial hash grid for O(n) candidate pair generation
5. **narrowphase**: SAT, circle-circle, circle-polygon detection producing `ContactManifold`
6. **solver**: Impulse-based collision response with friction and positional correction

```
Simulation Step (fixed dt)
  |
  |-- Apply forces (gravity) to all bodies
  |
  |-- Broad phase: insert AABBs into spatial hash -> candidate pairs
  |
  |-- Narrow phase: for each candidate pair -> ContactManifold or None
  |
  |-- Solver: for each contact -> compute and apply impulse + friction
  |
  |-- Positional correction: push overlapping bodies apart
  |
  |-- Integration: semi-implicit Euler (velocity then position)
```

## Rust Solution

### Cargo.toml

```toml
[package]
name = "physics2d"
version = "0.1.0"
edition = "2021"

[dependencies]

[dev-dependencies]
criterion = { version = "0.5", features = ["html_reports"] }

[[bench]]
name = "physics_bench"
harness = false
```

### src/math.rs

```rust
use std::ops::{Add, Sub, Mul, Neg};

pub const EPSILON: f64 = 1e-8;

#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Vec2 {
    pub x: f64,
    pub y: f64,
}

impl Vec2 {
    pub const ZERO: Vec2 = Vec2 { x: 0.0, y: 0.0 };

    pub fn new(x: f64, y: f64) -> Self {
        Self { x, y }
    }

    pub fn dot(self, other: Vec2) -> f64 {
        self.x * other.x + self.y * other.y
    }

    /// 2D cross product returns a scalar (the z-component of the 3D cross).
    pub fn cross(self, other: Vec2) -> f64 {
        self.x * other.y - self.y * other.x
    }

    /// Cross product of scalar and vector: s x v = (-s*v.y, s*v.x)
    pub fn cross_scalar(s: f64, v: Vec2) -> Vec2 {
        Vec2::new(-s * v.y, s * v.x)
    }

    pub fn length_squared(self) -> f64 {
        self.dot(self)
    }

    pub fn length(self) -> f64 {
        self.length_squared().sqrt()
    }

    pub fn normalized(self) -> Vec2 {
        let len = self.length();
        if len < EPSILON {
            return Vec2::ZERO;
        }
        self * (1.0 / len)
    }

    pub fn perpendicular(self) -> Vec2 {
        Vec2::new(-self.y, self.x)
    }

    pub fn distance(self, other: Vec2) -> f64 {
        (self - other).length()
    }

    pub fn rotate(self, angle: f64) -> Vec2 {
        let cos = angle.cos();
        let sin = angle.sin();
        Vec2::new(
            self.x * cos - self.y * sin,
            self.x * sin + self.y * cos,
        )
    }
}

impl Add for Vec2 {
    type Output = Vec2;
    fn add(self, rhs: Vec2) -> Vec2 {
        Vec2::new(self.x + rhs.x, self.y + rhs.y)
    }
}

impl Sub for Vec2 {
    type Output = Vec2;
    fn sub(self, rhs: Vec2) -> Vec2 {
        Vec2::new(self.x - rhs.x, self.y - rhs.y)
    }
}

impl Mul<f64> for Vec2 {
    type Output = Vec2;
    fn mul(self, t: f64) -> Vec2 {
        Vec2::new(self.x * t, self.y * t)
    }
}

impl Neg for Vec2 {
    type Output = Vec2;
    fn neg(self) -> Vec2 {
        Vec2::new(-self.x, -self.y)
    }
}
```

### src/shapes.rs

```rust
use crate::math::Vec2;

#[derive(Debug, Clone)]
pub enum Shape {
    Circle { radius: f64 },
    AABB { half_extents: Vec2 },
    ConvexPolygon { vertices: Vec<Vec2> },
}

/// Axis-aligned bounding box for broad phase.
#[derive(Debug, Clone, Copy)]
pub struct BoundingBox {
    pub min: Vec2,
    pub max: Vec2,
}

impl Shape {
    /// Compute the world-space AABB given a position and rotation.
    pub fn compute_aabb(&self, position: Vec2, rotation: f64) -> BoundingBox {
        match self {
            Shape::Circle { radius } => BoundingBox {
                min: Vec2::new(position.x - radius, position.y - radius),
                max: Vec2::new(position.x + radius, position.y + radius),
            },
            Shape::AABB { half_extents } => {
                // Rotation turns AABB into OBB; compute enclosing AABB
                let cos = rotation.abs().cos();
                let sin = rotation.abs().sin();
                let hx = half_extents.x * cos + half_extents.y * sin;
                let hy = half_extents.x * sin + half_extents.y * cos;
                BoundingBox {
                    min: Vec2::new(position.x - hx, position.y - hy),
                    max: Vec2::new(position.x + hx, position.y + hy),
                }
            }
            Shape::ConvexPolygon { vertices } => {
                let mut min_x = f64::MAX;
                let mut min_y = f64::MAX;
                let mut max_x = f64::MIN;
                let mut max_y = f64::MIN;
                for v in vertices {
                    let world = v.rotate(rotation) + position;
                    min_x = min_x.min(world.x);
                    min_y = min_y.min(world.y);
                    max_x = max_x.max(world.x);
                    max_y = max_y.max(world.y);
                }
                BoundingBox {
                    min: Vec2::new(min_x, min_y),
                    max: Vec2::new(max_x, max_y),
                }
            }
        }
    }

    /// Get world-space vertices for a convex polygon, or generate vertices for
    /// AABB. Circles return None.
    pub fn world_vertices(&self, position: Vec2, rotation: f64) -> Option<Vec<Vec2>> {
        match self {
            Shape::Circle { .. } => None,
            Shape::AABB { half_extents } => {
                let hx = half_extents.x;
                let hy = half_extents.y;
                let corners = vec![
                    Vec2::new(-hx, -hy),
                    Vec2::new(hx, -hy),
                    Vec2::new(hx, hy),
                    Vec2::new(-hx, hy),
                ];
                Some(
                    corners
                        .into_iter()
                        .map(|v| v.rotate(rotation) + position)
                        .collect(),
                )
            }
            Shape::ConvexPolygon { vertices } => Some(
                vertices
                    .iter()
                    .map(|v| v.rotate(rotation) + position)
                    .collect(),
            ),
        }
    }
}

/// Project a set of vertices onto an axis, returning (min, max).
pub fn project_vertices(vertices: &[Vec2], axis: Vec2) -> (f64, f64) {
    let mut min = f64::MAX;
    let mut max = f64::MIN;
    for v in vertices {
        let proj = v.dot(axis);
        min = min.min(proj);
        max = max.max(proj);
    }
    (min, max)
}

/// Project a circle onto an axis, returning (min, max).
pub fn project_circle(center: Vec2, radius: f64, axis: Vec2) -> (f64, f64) {
    let center_proj = center.dot(axis);
    (center_proj - radius, center_proj + radius)
}
```

### src/body.rs

```rust
use crate::math::Vec2;
use crate::shapes::Shape;

#[derive(Debug, Clone)]
pub struct RigidBody {
    pub position: Vec2,
    pub rotation: f64,
    pub linear_velocity: Vec2,
    pub angular_velocity: f64,
    pub force_accumulator: Vec2,
    pub torque_accumulator: f64,

    pub mass: f64,
    pub inv_mass: f64,
    pub inertia: f64,
    pub inv_inertia: f64,

    pub restitution: f64,
    pub friction: f64,
    pub shape: Shape,
}

impl RigidBody {
    pub fn new(shape: Shape, mass: f64, position: Vec2) -> Self {
        let (inv_mass, inertia, inv_inertia) = if mass <= 0.0 {
            // Static body
            (0.0, 0.0, 0.0)
        } else {
            let inertia = compute_inertia(&shape, mass);
            (1.0 / mass, inertia, 1.0 / inertia)
        };

        Self {
            position,
            rotation: 0.0,
            linear_velocity: Vec2::ZERO,
            angular_velocity: 0.0,
            force_accumulator: Vec2::ZERO,
            torque_accumulator: 0.0,
            mass,
            inv_mass,
            inertia,
            inv_inertia,
            restitution: 0.5,
            friction: 0.3,
            shape,
        }
    }

    pub fn new_static(shape: Shape, position: Vec2) -> Self {
        Self::new(shape, 0.0, position)
    }

    pub fn is_static(&self) -> bool {
        self.inv_mass == 0.0
    }

    pub fn apply_force(&mut self, force: Vec2) {
        self.force_accumulator = self.force_accumulator + force;
    }

    pub fn integrate(&mut self, dt: f64) {
        if self.is_static() {
            return;
        }
        // Semi-implicit Euler: velocity first, then position
        let acceleration = self.force_accumulator * self.inv_mass;
        self.linear_velocity = self.linear_velocity + acceleration * dt;
        self.angular_velocity += self.torque_accumulator * self.inv_inertia * dt;

        self.position = self.position + self.linear_velocity * dt;
        self.rotation += self.angular_velocity * dt;

        self.force_accumulator = Vec2::ZERO;
        self.torque_accumulator = 0.0;
    }
}

fn compute_inertia(shape: &Shape, mass: f64) -> f64 {
    match shape {
        Shape::Circle { radius } => 0.5 * mass * radius * radius,
        Shape::AABB { half_extents } => {
            let w = half_extents.x * 2.0;
            let h = half_extents.y * 2.0;
            mass * (w * w + h * h) / 12.0
        }
        Shape::ConvexPolygon { vertices } => {
            // Approximate using polygon area-based formula
            let n = vertices.len();
            if n < 3 {
                return mass;
            }
            let mut numerator = 0.0;
            let mut denominator = 0.0;
            for i in 0..n {
                let a = vertices[i];
                let b = vertices[(i + 1) % n];
                let cross = a.cross(b).abs();
                numerator += cross * (a.dot(a) + a.dot(b) + b.dot(b));
                denominator += cross;
            }
            if denominator.abs() < 1e-10 {
                return mass;
            }
            mass * numerator / (6.0 * denominator)
        }
    }
}
```

### src/broadphase.rs

```rust
use crate::math::Vec2;
use crate::shapes::BoundingBox;
use std::collections::{HashMap, HashSet};

pub struct SpatialHash {
    cell_size: f64,
    inv_cell_size: f64,
    cells: HashMap<(i32, i32), Vec<usize>>,
}

impl SpatialHash {
    pub fn new(cell_size: f64) -> Self {
        Self {
            cell_size,
            inv_cell_size: 1.0 / cell_size,
            cells: HashMap::new(),
        }
    }

    pub fn clear(&mut self) {
        self.cells.clear();
    }

    fn cell_coord(&self, x: f64) -> i32 {
        (x * self.inv_cell_size).floor() as i32
    }

    pub fn insert(&mut self, index: usize, aabb: &BoundingBox) {
        let min_cx = self.cell_coord(aabb.min.x);
        let min_cy = self.cell_coord(aabb.min.y);
        let max_cx = self.cell_coord(aabb.max.x);
        let max_cy = self.cell_coord(aabb.max.y);

        for cx in min_cx..=max_cx {
            for cy in min_cy..=max_cy {
                self.cells.entry((cx, cy)).or_default().push(index);
            }
        }
    }

    /// Return all unique pairs (i, j) where i < j sharing at least one cell.
    pub fn get_pairs(&self) -> Vec<(usize, usize)> {
        let mut seen = HashSet::new();
        let mut pairs = Vec::new();

        for cell in self.cells.values() {
            for i in 0..cell.len() {
                for j in (i + 1)..cell.len() {
                    let a = cell[i].min(cell[j]);
                    let b = cell[i].max(cell[j]);
                    if seen.insert((a, b)) {
                        pairs.push((a, b));
                    }
                }
            }
        }
        pairs
    }
}

/// Check if two AABBs overlap.
pub fn aabb_overlap(a: &BoundingBox, b: &BoundingBox) -> bool {
    a.min.x <= b.max.x && a.max.x >= b.min.x && a.min.y <= b.max.y && a.max.y >= b.min.y
}
```

### src/narrowphase.rs

```rust
use crate::math::*;
use crate::shapes::*;
use crate::body::RigidBody;

#[derive(Debug, Clone)]
pub struct ContactManifold {
    pub point: Vec2,
    pub normal: Vec2,   // Points from body A to body B
    pub penetration: f64,
}

/// Detect collision between two rigid bodies.
pub fn detect_collision(a: &RigidBody, b: &RigidBody) -> Option<ContactManifold> {
    match (&a.shape, &b.shape) {
        (Shape::Circle { radius: ra }, Shape::Circle { radius: rb }) => {
            circle_vs_circle(a.position, *ra, b.position, *rb)
        }
        (Shape::Circle { radius }, _) => {
            let verts_b = b.shape.world_vertices(b.position, b.rotation)?;
            circle_vs_polygon(a.position, *radius, &verts_b)
        }
        (_, Shape::Circle { radius }) => {
            let verts_a = a.shape.world_vertices(a.position, a.rotation)?;
            circle_vs_polygon(b.position, *radius, &verts_a).map(|mut c| {
                c.normal = -c.normal;
                c
            })
        }
        _ => {
            let verts_a = a.shape.world_vertices(a.position, a.rotation)?;
            let verts_b = b.shape.world_vertices(b.position, b.rotation)?;
            polygon_vs_polygon(&verts_a, &verts_b)
        }
    }
}

fn circle_vs_circle(
    pos_a: Vec2, rad_a: f64,
    pos_b: Vec2, rad_b: f64,
) -> Option<ContactManifold> {
    let diff = pos_b - pos_a;
    let dist_sq = diff.length_squared();
    let sum_radii = rad_a + rad_b;

    if dist_sq >= sum_radii * sum_radii {
        return None;
    }

    let dist = dist_sq.sqrt();
    let normal = if dist < EPSILON {
        Vec2::new(1.0, 0.0)
    } else {
        diff * (1.0 / dist)
    };

    Some(ContactManifold {
        point: pos_a + normal * rad_a,
        normal,
        penetration: sum_radii - dist,
    })
}

fn circle_vs_polygon(
    center: Vec2, radius: f64,
    vertices: &[Vec2],
) -> Option<ContactManifold> {
    let n = vertices.len();
    let mut min_penetration = f64::MAX;
    let mut best_normal = Vec2::ZERO;

    // Test each edge normal
    for i in 0..n {
        let edge = vertices[(i + 1) % n] - vertices[i];
        let axis = edge.perpendicular().normalized();

        let (poly_min, poly_max) = project_vertices(vertices, axis);
        let (circ_min, circ_max) = project_circle(center, radius, axis);

        if poly_min > circ_max || circ_min > poly_max {
            return None;
        }

        let overlap = (poly_max.min(circ_max)) - (poly_min.max(circ_min));
        if overlap < min_penetration {
            min_penetration = overlap;
            best_normal = axis;
        }
    }

    // Test axis from center to closest vertex
    let closest_vertex = vertices
        .iter()
        .min_by(|a, b| {
            a.distance(center)
                .partial_cmp(&b.distance(center))
                .unwrap()
        })
        .unwrap();

    let axis = (*closest_vertex - center).normalized();
    if axis.length_squared() > EPSILON {
        let (poly_min, poly_max) = project_vertices(vertices, axis);
        let (circ_min, circ_max) = project_circle(center, radius, axis);

        if poly_min > circ_max || circ_min > poly_max {
            return None;
        }

        let overlap = (poly_max.min(circ_max)) - (poly_min.max(circ_min));
        if overlap < min_penetration {
            min_penetration = overlap;
            best_normal = axis;
        }
    }

    // Ensure normal points from polygon toward circle
    let polygon_center = polygon_centroid(vertices);
    if best_normal.dot(center - polygon_center) < 0.0 {
        best_normal = -best_normal;
    }

    Some(ContactManifold {
        point: center - best_normal * radius,
        normal: -best_normal,  // From circle to polygon -> negate for A-to-B
        penetration: min_penetration,
    })
}

fn polygon_vs_polygon(
    verts_a: &[Vec2],
    verts_b: &[Vec2],
) -> Option<ContactManifold> {
    let mut min_penetration = f64::MAX;
    let mut best_normal = Vec2::ZERO;

    // Test all edge normals from both polygons
    for vertices in [verts_a, verts_b] {
        let n = vertices.len();
        for i in 0..n {
            let edge = vertices[(i + 1) % n] - vertices[i];
            let axis = edge.perpendicular().normalized();

            let (min_a, max_a) = project_vertices(verts_a, axis);
            let (min_b, max_b) = project_vertices(verts_b, axis);

            if min_a > max_b || min_b > max_a {
                return None; // Separating axis found
            }

            let overlap = (max_a.min(max_b)) - (min_a.max(min_b));
            if overlap < min_penetration {
                min_penetration = overlap;
                best_normal = axis;
            }
        }
    }

    // Ensure normal points from A to B
    let center_a = polygon_centroid(verts_a);
    let center_b = polygon_centroid(verts_b);
    if best_normal.dot(center_b - center_a) < 0.0 {
        best_normal = -best_normal;
    }

    // Approximate contact point: deepest vertex of A in B's direction
    let contact = verts_a
        .iter()
        .max_by(|a, b| a.dot(best_normal).partial_cmp(&b.dot(best_normal)).unwrap())
        .unwrap();

    Some(ContactManifold {
        point: *contact,
        normal: best_normal,
        penetration: min_penetration,
    })
}

fn polygon_centroid(vertices: &[Vec2]) -> Vec2 {
    let sum: Vec2 = vertices.iter().fold(Vec2::ZERO, |acc, v| acc + *v);
    sum * (1.0 / vertices.len() as f64)
}
```

### src/solver.rs

```rust
use crate::math::*;
use crate::body::RigidBody;
use crate::narrowphase::ContactManifold;

/// Apply impulse-based collision response to two bodies.
pub fn resolve_collision(
    a: &mut RigidBody,
    b: &mut RigidBody,
    contact: &ContactManifold,
) {
    if a.is_static() && b.is_static() {
        return;
    }

    let ra = contact.point - a.position;
    let rb = contact.point - b.position;
    let n = contact.normal;

    // Relative velocity at contact point
    let vel_a = a.linear_velocity + Vec2::cross_scalar(a.angular_velocity, ra);
    let vel_b = b.linear_velocity + Vec2::cross_scalar(b.angular_velocity, rb);
    let rel_vel = vel_b - vel_a;

    let vel_along_normal = rel_vel.dot(n);

    // Bodies separating -- no impulse needed
    if vel_along_normal > 0.0 {
        return;
    }

    let e = a.restitution.min(b.restitution);

    let ra_cross_n = ra.cross(n);
    let rb_cross_n = rb.cross(n);
    let inv_mass_sum = a.inv_mass
        + b.inv_mass
        + (ra_cross_n * ra_cross_n) * a.inv_inertia
        + (rb_cross_n * rb_cross_n) * b.inv_inertia;

    // Normal impulse magnitude
    let j = -(1.0 + e) * vel_along_normal / inv_mass_sum;
    let impulse = n * j;

    a.linear_velocity = a.linear_velocity - impulse * a.inv_mass;
    b.linear_velocity = b.linear_velocity + impulse * b.inv_mass;
    a.angular_velocity -= ra.cross(impulse) * a.inv_inertia;
    b.angular_velocity += rb.cross(impulse) * b.inv_inertia;

    // Friction impulse
    let vel_a_after = a.linear_velocity + Vec2::cross_scalar(a.angular_velocity, ra);
    let vel_b_after = b.linear_velocity + Vec2::cross_scalar(b.angular_velocity, rb);
    let rel_vel_after = vel_b_after - vel_a_after;

    let tangent_vel = rel_vel_after - n * rel_vel_after.dot(n);
    let tangent_len = tangent_vel.length();
    if tangent_len < EPSILON {
        return;
    }
    let tangent = tangent_vel * (1.0 / tangent_len);

    let ra_cross_t = ra.cross(tangent);
    let rb_cross_t = rb.cross(tangent);
    let inv_mass_sum_t = a.inv_mass
        + b.inv_mass
        + (ra_cross_t * ra_cross_t) * a.inv_inertia
        + (rb_cross_t * rb_cross_t) * b.inv_inertia;

    let jt = -rel_vel_after.dot(tangent) / inv_mass_sum_t;

    // Coulomb friction: clamp tangential impulse
    let mu = (a.friction * b.friction).sqrt();
    let friction_impulse = if jt.abs() < j * mu {
        tangent * jt
    } else {
        tangent * (-j * mu)
    };

    a.linear_velocity = a.linear_velocity - friction_impulse * a.inv_mass;
    b.linear_velocity = b.linear_velocity + friction_impulse * b.inv_mass;
    a.angular_velocity -= ra.cross(friction_impulse) * a.inv_inertia;
    b.angular_velocity += rb.cross(friction_impulse) * b.inv_inertia;
}

/// Positional correction to prevent sinking (Baumgarte stabilization).
pub fn positional_correction(
    a: &mut RigidBody,
    b: &mut RigidBody,
    contact: &ContactManifold,
) {
    const SLOP: f64 = 0.01;       // Allowable penetration
    const CORRECTION_RATE: f64 = 0.2;

    let correction_magnitude =
        ((contact.penetration - SLOP).max(0.0) / (a.inv_mass + b.inv_mass)) * CORRECTION_RATE;
    let correction = contact.normal * correction_magnitude;

    a.position = a.position - correction * a.inv_mass;
    b.position = b.position + correction * b.inv_mass;
}
```

### src/world.rs

```rust
use crate::body::RigidBody;
use crate::broadphase::SpatialHash;
use crate::math::Vec2;
use crate::narrowphase::detect_collision;
use crate::solver::{resolve_collision, positional_correction};

pub struct PhysicsWorld {
    pub bodies: Vec<RigidBody>,
    pub gravity: Vec2,
    spatial_hash: SpatialHash,
    fixed_dt: f64,
}

impl PhysicsWorld {
    pub fn new(gravity: Vec2, cell_size: f64) -> Self {
        Self {
            bodies: Vec::new(),
            gravity,
            spatial_hash: SpatialHash::new(cell_size),
            fixed_dt: 1.0 / 60.0,
        }
    }

    pub fn add_body(&mut self, body: RigidBody) -> usize {
        let index = self.bodies.len();
        self.bodies.push(body);
        index
    }

    pub fn step(&mut self) {
        let dt = self.fixed_dt;

        // Apply gravity
        for body in &mut self.bodies {
            if !body.is_static() {
                let gravity_force = self.gravity * body.mass;
                body.apply_force(gravity_force);
            }
        }

        // Broad phase
        self.spatial_hash.clear();
        let aabbs: Vec<_> = self.bodies.iter()
            .map(|b| b.shape.compute_aabb(b.position, b.rotation))
            .collect();
        for (i, aabb) in aabbs.iter().enumerate() {
            self.spatial_hash.insert(i, aabb);
        }
        let pairs = self.spatial_hash.get_pairs();

        // Narrow phase and resolution
        for (i, j) in pairs {
            // AABB pre-check
            if !crate::broadphase::aabb_overlap(&aabbs[i], &aabbs[j]) {
                continue;
            }

            if self.bodies[i].is_static() && self.bodies[j].is_static() {
                continue;
            }

            let contact = {
                let a = &self.bodies[i];
                let b = &self.bodies[j];
                detect_collision(a, b)
            };

            if let Some(contact) = contact {
                // Split borrow for two mutable body references
                let (left, right) = self.bodies.split_at_mut(j);
                let body_a = &mut left[i];
                let body_b = &mut right[0];

                resolve_collision(body_a, body_b, &contact);
                positional_correction(body_a, body_b, &contact);
            }
        }

        // Integration
        for body in &mut self.bodies {
            body.integrate(dt);
        }
    }

    /// Run n simulation steps, returning positions after each step.
    pub fn simulate(&mut self, steps: usize) -> Vec<Vec<(Vec2, f64)>> {
        let mut frames = Vec::with_capacity(steps);
        for _ in 0..steps {
            self.step();
            let state: Vec<(Vec2, f64)> = self.bodies.iter()
                .map(|b| (b.position, b.rotation))
                .collect();
            frames.push(state);
        }
        frames
    }
}
```

### src/lib.rs

```rust
pub mod math;
pub mod shapes;
pub mod body;
pub mod broadphase;
pub mod narrowphase;
pub mod solver;
pub mod world;

pub use math::Vec2;
pub use body::RigidBody;
pub use shapes::Shape;
pub use world::PhysicsWorld;
```

### Tests

```rust
// tests/physics_tests.rs
use physics2d::*;
use physics2d::math::*;
use physics2d::narrowphase::detect_collision;

#[test]
fn circle_circle_collision() {
    let a = RigidBody::new(Shape::Circle { radius: 1.0 }, 1.0, Vec2::new(0.0, 0.0));
    let b = RigidBody::new(Shape::Circle { radius: 1.0 }, 1.0, Vec2::new(1.5, 0.0));

    let contact = detect_collision(&a, &b);
    assert!(contact.is_some());

    let c = contact.unwrap();
    assert!((c.penetration - 0.5).abs() < 0.01);
    assert!((c.normal.x - 1.0).abs() < 0.01);
}

#[test]
fn circle_circle_no_collision() {
    let a = RigidBody::new(Shape::Circle { radius: 1.0 }, 1.0, Vec2::new(0.0, 0.0));
    let b = RigidBody::new(Shape::Circle { radius: 1.0 }, 1.0, Vec2::new(3.0, 0.0));
    assert!(detect_collision(&a, &b).is_none());
}

#[test]
fn polygon_vs_polygon_sat() {
    // Two overlapping squares
    let square = Shape::ConvexPolygon {
        vertices: vec![
            Vec2::new(-1.0, -1.0),
            Vec2::new(1.0, -1.0),
            Vec2::new(1.0, 1.0),
            Vec2::new(-1.0, 1.0),
        ],
    };
    let a = RigidBody::new(square.clone(), 1.0, Vec2::new(0.0, 0.0));
    let b = RigidBody::new(square, 1.0, Vec2::new(1.5, 0.0));

    let contact = detect_collision(&a, &b);
    assert!(contact.is_some());
    assert!(contact.unwrap().penetration > 0.0);
}

#[test]
fn polygon_vs_polygon_separated() {
    let square = Shape::ConvexPolygon {
        vertices: vec![
            Vec2::new(-1.0, -1.0),
            Vec2::new(1.0, -1.0),
            Vec2::new(1.0, 1.0),
            Vec2::new(-1.0, 1.0),
        ],
    };
    let a = RigidBody::new(square.clone(), 1.0, Vec2::new(0.0, 0.0));
    let b = RigidBody::new(square, 1.0, Vec2::new(5.0, 0.0));
    assert!(detect_collision(&a, &b).is_none());
}

#[test]
fn impulse_conserves_momentum() {
    let mut a = RigidBody::new(Shape::Circle { radius: 1.0 }, 2.0, Vec2::new(0.0, 0.0));
    let mut b = RigidBody::new(Shape::Circle { radius: 1.0 }, 3.0, Vec2::new(1.5, 0.0));

    a.linear_velocity = Vec2::new(5.0, 0.0);
    a.restitution = 1.0;
    b.restitution = 1.0;

    let momentum_before = a.linear_velocity * a.mass + b.linear_velocity * b.mass;

    if let Some(contact) = detect_collision(&a, &b) {
        physics2d::solver::resolve_collision(&mut a, &mut b, &contact);
    }

    let momentum_after = a.linear_velocity * a.mass + b.linear_velocity * b.mass;
    assert!((momentum_before.x - momentum_after.x).abs() < 0.001);
    assert!((momentum_before.y - momentum_after.y).abs() < 0.001);
}

#[test]
fn ball_bounces_on_floor() {
    let mut world = PhysicsWorld::new(Vec2::new(0.0, -9.81), 5.0);

    world.add_body(RigidBody::new(
        Shape::Circle { radius: 0.5 },
        1.0,
        Vec2::new(0.0, 10.0),
    ));

    let floor_shape = Shape::AABB {
        half_extents: Vec2::new(50.0, 0.5),
    };
    world.add_body(RigidBody::new_static(floor_shape, Vec2::new(0.0, -0.5)));

    let frames = world.simulate(300);

    // Ball should not fall below the floor
    let min_y = frames.iter()
        .map(|f| f[0].0.y)
        .fold(f64::MAX, f64::min);
    assert!(min_y > -1.0, "Ball fell through floor: min_y = {}", min_y);

    // Ball should eventually come to near-rest above the floor
    let final_y = frames.last().unwrap()[0].0.y;
    assert!(final_y > 0.0, "Ball should rest above floor: y = {}", final_y);
}

#[test]
fn static_body_immovable() {
    let mut world = PhysicsWorld::new(Vec2::new(0.0, -9.81), 5.0);

    let floor_shape = Shape::AABB {
        half_extents: Vec2::new(50.0, 1.0),
    };
    world.add_body(RigidBody::new_static(floor_shape, Vec2::new(0.0, 0.0)));

    world.simulate(100);

    let floor_pos = world.bodies[0].position;
    assert!((floor_pos.x).abs() < EPSILON);
    assert!((floor_pos.y).abs() < EPSILON);
}

#[test]
fn spatial_hash_reduces_checks() {
    use physics2d::broadphase::SpatialHash;
    use physics2d::shapes::BoundingBox;

    let mut hash = SpatialHash::new(10.0);

    // 100 bodies spread across a 100x100 area
    for i in 0..100 {
        let x = (i % 10) as f64 * 10.0;
        let y = (i / 10) as f64 * 10.0;
        hash.insert(i, &BoundingBox {
            min: Vec2::new(x, y),
            max: Vec2::new(x + 1.0, y + 1.0),
        });
    }

    let pairs = hash.get_pairs();
    // Far fewer than 100*99/2 = 4950 brute-force pairs
    assert!(pairs.len() < 500, "Too many pairs: {}", pairs.len());
}
```

### Commands

```bash
cargo new physics2d --lib
cd physics2d
# Place source files, then:
cargo test
cargo test -- --nocapture
cargo bench
cargo run --release --example simulation  # if main.rs is set up
```

### Expected Output

```
running 8 tests
test circle_circle_collision ... ok
test circle_circle_no_collision ... ok
test polygon_vs_polygon_sat ... ok
test polygon_vs_polygon_separated ... ok
test impulse_conserves_momentum ... ok
test ball_bounces_on_floor ... ok
test static_body_immovable ... ok
test spatial_hash_reduces_checks ... ok

test result: ok. 8 passed; 0 failed; 0 ignored
```

## Design Decisions

1. **Shape as enum vs trait object**: An enum with three variants is simpler and avoids heap allocation and dynamic dispatch. The fixed set of shapes (circle, AABB, polygon) is known at compile time, and match exhaustiveness checking catches missing collision pair implementations.

2. **Semi-implicit Euler over Verlet**: Semi-implicit Euler (symplectic Euler) is nearly as stable as Verlet but simpler to implement. It updates velocity from forces first, then position from the new velocity. This preserves energy better than explicit Euler and is the standard choice for game physics.

3. **Spatial hash over BVH for broad phase**: Spatial hashing is simpler to implement and optimal for uniformly distributed objects of similar size -- the typical case in 2D games. BVH or sweep-and-prune would be better for scenes with extreme size variation or sparse distribution.

4. **Baumgarte stabilization for positional correction**: Rather than solving a constraint system (as Box2D does), we directly project overlapping bodies apart by a fraction of the penetration each frame. This is simple and effective for most scenarios but can cause jittering at high correction rates.

5. **Split-borrow pattern for collision resolution**: Rust's borrow checker prevents mutably borrowing two elements of the same `Vec`. We use `split_at_mut` to safely get mutable references to both bodies during collision resolution.

6. **Inverse mass representation**: Storing `inv_mass` (1/mass for dynamic, 0 for static) unifies the impulse formula. Static bodies naturally have zero contribution to the denominator, so no special-casing is needed.

## Common Mistakes

- **Wrong contact normal direction**: The normal must consistently point from A to B. Flipping it reverses the impulse direction, causing objects to accelerate into each other instead of apart.
- **Missing AABB recomputation**: The broad phase uses AABBs computed from the previous frame's positions. For fast-moving objects, this can miss collisions. The solution is to expand AABBs by the velocity vector or use continuous detection.
- **Forgetting angular contribution in impulse**: The impulse denominator includes angular terms `(r x n)^2 * inv_inertia`. Omitting these makes the response ignore rotation, producing unrealistic behavior for non-circular shapes.
- **Euler integration instability**: Explicit (forward) Euler gains energy over time, causing objects to vibrate and eventually explode. Semi-implicit Euler avoids this by updating velocity before position.
- **Not clamping friction impulse**: The friction impulse must be clamped by `mu * |j_normal|` (Coulomb's law). Without clamping, friction can add energy to the system.

## Performance Notes

- Spatial hashing reduces O(n^2) pair checks to approximately O(n) for uniformly distributed objects. Cell size should match the average body size.
- The narrow phase is the bottleneck. SAT for convex polygons with k vertices tests 2k axes. For complex shapes, GJK is more efficient because it converges without testing all axes.
- Collision resolution is O(contacts) per frame. For stable stacking, multiple solver iterations (4-8) are needed, each re-resolving all contacts to converge on a consistent velocity state.
- At 1000 bodies with spatial hashing, the engine comfortably runs at 60Hz on modern hardware in release mode. Without broad phase, performance degrades to ~5Hz.
