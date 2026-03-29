<!-- difficulty: advanced -->
<!-- category: game-development-and-graphics -->
<!-- languages: [rust] -->
<!-- concepts: [collision-detection, sat, gjk, impulse-resolution, spatial-hashing, verlet-integration, rigid-body-dynamics] -->
<!-- estimated_time: 12-18 hours -->
<!-- bloom_level: analyze, evaluate -->
<!-- prerequisites: [2d-vector-math, trigonometry, newtons-laws, rust-enums, trait-objects, iterators] -->

# Challenge 89: 2D Physics Engine -- Collision Detection and Response

## Languages

Rust (stable, latest edition)

## Prerequisites

- 2D vector math: dot product, cross product (scalar in 2D), normalization, perpendicular vectors
- Basic trigonometry and rotation matrices
- Newton's laws of motion: force, mass, acceleration, impulse, momentum
- Rust enums for shape variants and pattern matching
- Trait objects or generics for polymorphic shape handling
- Iterators and spatial data structure concepts

## Learning Objectives

- **Implement** a broad-phase collision detection system using spatial hashing that reduces pairwise checks from O(n^2) to near-O(n)
- **Implement** narrow-phase collision detection using the Separating Axis Theorem (SAT) for convex polygons and circle-polygon pairs
- **Analyze** the physics of impulse-based collision response with restitution and friction
- **Evaluate** the stability trade-offs between explicit Euler, semi-implicit Euler, and Verlet integration
- **Design** a modular physics pipeline that separates broad phase, narrow phase, resolution, and integration into independent stages

## Background

Physics engines are the invisible infrastructure of every game that involves movement and collision. When two objects overlap, the engine must detect the overlap (collision detection), compute how deep they penetrated and along which axis (contact generation), and push them apart while adjusting their velocities (collision response). This happens every frame, for every pair of objects that might interact.

The computational challenge is quadratic: n objects produce n*(n-1)/2 potential collision pairs. Broad-phase algorithms like spatial hashing or sweep-and-prune reduce this by quickly discarding pairs that are obviously too far apart. Only the surviving pairs go through the expensive narrow-phase test, which computes exact contact points and penetration depth.

Collision response applies impulses -- instantaneous changes in velocity -- to separate objects and simulate bouncing (restitution) and sliding (friction). The math comes directly from Newton's laws and conservation of momentum. Getting this right produces satisfying, physically plausible behavior. Getting it wrong produces jittering, tunneling, or objects passing through each other.

Rotation makes everything harder. A non-central impact on a box imparts angular momentum. The impulse formula must account for the moment of inertia and the lever arm (the vector from the center of mass to the contact point). The 2D case is simpler than 3D because angular velocity is a scalar rather than a vector, but the math still requires careful derivation.

## The Challenge

Build a 2D physics engine from scratch. Support three shape types: circles, axis-aligned bounding boxes (AABBs), and convex polygons. Implement broad-phase collision detection with spatial hashing (divide space into a uniform grid; each body is inserted into every cell its AABB overlaps). Only pairs sharing at least one grid cell proceed to narrow-phase testing.

Implement narrow-phase detection with SAT for polygon-polygon and polygon-circle pairs, and analytic tests for circle-circle and circle-AABB. Compute contact manifolds with penetration depth and contact normal. Each collision pair type (circle-circle, circle-polygon, polygon-polygon, circle-AABB) has a dedicated detection function that returns a `ContactManifold` or `None`.

For collision response, implement impulse-based resolution. When two objects collide, compute the impulse magnitude from their relative velocity, masses, and restitution coefficient. Apply the impulse to separate their velocities along the contact normal. Add Coulomb friction to the tangential component. For integration, implement semi-implicit Euler (update velocity first, then position) which is simple and stable for game physics.

Build a simulation loop that runs at a fixed timestep, processes all collisions, and outputs the state of all bodies. The output should be suitable for rendering (positions and rotations per frame) or verification (logged collision events).

Stacking is the stress test. Place five boxes on top of each other on a static floor. If your impulse solver runs a single iteration, the stack collapses because each contact is resolved independently without knowledge of the others. Running multiple solver iterations (4-8) per frame lets the system converge toward a stable resting configuration. This is the same approach used by Box2D and Bullet.

The physics pipeline runs every frame in this order: (1) apply external forces, (2) broad phase, (3) narrow phase, (4) resolve collisions with multiple iterations, (5) positional correction, (6) integrate. Each stage is independent and testable in isolation.

## Requirements

1. Implement `Vec2` with full arithmetic: add, subtract, scale, dot product, cross product (returns scalar in 2D: the z-component of the 3D cross), perpendicular vector, normalize, length, squared length, distance between two points, rotate by angle
2. Implement shape types as an enum: `Circle { radius }`, `AABB { half_extents: Vec2 }`, `ConvexPolygon { vertices: Vec<Vec2> }` (vertices stored in local space with counter-clockwise winding order). Each shape must be able to compute its world-space AABB given a position and rotation
3. Implement `RigidBody` with: position, rotation (angle in radians), linear velocity, angular velocity, mass (and inverse mass, where 0 means static/infinite-mass), moment of inertia (and inverse), restitution coefficient (0 = no bounce, 1 = perfect bounce), friction coefficient, shape, force and torque accumulators
4. Implement spatial hash broad phase: divide world space into a uniform grid. Each body is inserted into every cell its AABB overlaps. Return all pairs sharing at least one cell, deduplicated
5. Implement SAT narrow phase for convex polygon vs convex polygon: project both shapes onto each edge normal, check for overlap on all axes. If all axes overlap, the collision is real -- return the axis with minimum penetration as the contact normal
6. Implement circle vs circle narrow phase: distance between centers minus sum of radii. Contact normal is the normalized center-to-center vector
7. Implement circle vs convex polygon narrow phase: test Voronoi regions (face, edge, vertex) to find the closest feature and penetration depth
8. Generate a `ContactManifold` for each collision: contact point, contact normal (pointing from body A to body B), and penetration depth
9. Implement impulse-based collision response: compute the impulse scalar `j = -(1 + e) * v_rel_dot_n / (1/m_a + 1/m_b + angular_terms)` where `e` is the combined restitution. Apply impulse to linear and angular velocities of both bodies
10. Implement Coulomb friction: compute a tangential impulse clamped by `mu * |j_normal|`. Apply it perpendicular to the contact normal
11. Implement positional correction (Baumgarte stabilization or direct position projection) to prevent objects from sinking into each other due to floating-point drift
12. Implement semi-implicit Euler integration: apply forces (gravity), update velocity, then update position from the new velocity. Use a fixed timestep
13. Write a simulation driver: given a set of bodies and N timesteps, run the full pipeline each step and output body positions and rotations per step. Log collision events (which pair, what impulse magnitude) for debugging
14. Implement multiple solver iterations per step (configurable, default 4-8) to improve stacking stability. Each iteration re-resolves all active contacts
15. Write tests: circle-circle collision detection and response, AABB overlap check, polygon SAT with separating axis found and with minimum overlap, impulse conservation (total momentum before = after), resting contact stability (stacked boxes settle without exploding), spatial hash pair reduction verification

## Hints

1. Start with circles only. Circle-circle detection is trivial, and impulse-based response for circles has no angular component if you treat them as point masses. Once this works, add rotation and convex polygons. Build the full pipeline (broad phase -> narrow phase -> response -> integration) with circles before adding other shapes.

2. For SAT, the candidate separating axes for two convex polygons are the edge normals of both polygons. If you find any axis where the projections do not overlap, the shapes are separated. If all overlap, the axis with the smallest overlap is the contact normal and the overlap is the penetration depth. Remember to normalize each axis before projecting.

3. Inverse mass simplifies the code enormously. Static bodies have `inv_mass = 0` and `inv_inertia = 0`. The impulse formula naturally handles static bodies without special cases: the impulse only affects the dynamic body because the static body's inverse mass contribution is zero. This eliminates all `if is_static` checks from the solver.

4. The fixed timestep loop should accumulate elapsed time and step the simulation in fixed increments (e.g., 1/60s). This prevents physics instability from variable frame rates. If the frame is slow, run multiple physics steps to catch up, capped at a maximum (e.g., 5) to prevent the spiral-of-death where more physics work causes more lag which causes more physics work.

5. For circle vs convex polygon: find the closest edge or vertex to the circle center. The contact normal is either the edge normal (if the circle center projects onto the edge) or the center-to-vertex direction (if the center is past an edge endpoint). Test both cases.

6. Use `split_at_mut` to borrow two rigid bodies from the same `Vec` simultaneously. Rust's borrow checker prevents `&mut bodies[i]` and `&mut bodies[j]` -- the split pattern is the safe alternative without `unsafe`.

## Acceptance Criteria

- [ ] Circle-circle collisions detect and resolve correctly: two circles approaching each other bounce apart
- [ ] Circle vs convex polygon detects collisions and returns correct contact normal
- [ ] SAT detects overlapping convex polygons and returns the minimum penetration axis
- [ ] SAT correctly identifies separated polygons by finding a separating axis
- [ ] Spatial hashing reduces collision checks: 1000 bodies with grid size matching body scale produces far fewer than 500,000 pair checks
- [ ] Impulse response conserves momentum: total momentum before and after collision is equal (within float tolerance)
- [ ] A ball dropped onto a static floor bounces with decreasing height proportional to restitution coefficient
- [ ] Fully elastic collision (restitution = 1.0) preserves kinetic energy
- [ ] Friction causes a sliding object to decelerate and stop
- [ ] Static bodies (infinite mass) are immovable and do not drift under any force or collision
- [ ] Positional correction prevents visible sinking at rest
- [ ] Angular velocity is affected by off-center impacts: a ball hitting the edge of a box causes rotation
- [ ] The simulation runs 1000 bodies at 60 FPS physics timestep without frame drops (release build)
- [ ] Unit tests verify each collision pair type and impulse math independently
- [ ] No tunneling for objects moving at reasonable speeds (< 1 body-width per timestep)
- [ ] Degenerate cases handled: zero-mass body pairs, zero-velocity collisions, coincident positions

## Key Concepts

**Broad phase vs narrow phase**: Broad phase is cheap and imprecise (AABB overlap). Narrow phase is expensive and exact (SAT, GJK). The broad phase filters 95%+ of pairs, so the narrow phase only runs on pairs likely to collide. Without broad phase, performance is O(n^2) and unusable above ~100 bodies.

**Impulse vs force-based response**: Forces are accumulated and integrated over time. Impulses are instantaneous velocity changes. For collision response, impulses are preferred because collisions happen at a point in time, not over a duration. The impulse magnitude depends on the relative velocity along the contact normal, the masses, and the restitution coefficient.

**Moment of inertia**: The rotational equivalent of mass. A circle's moment of inertia is `(1/2) * m * r^2`. A rectangle's is `(1/12) * m * (w^2 + h^2)`. Higher inertia means harder to rotate. The impulse formula includes angular terms based on the lever arm (contact point relative to center of mass) and inverse inertia.

**Restitution and friction**: Restitution (coefficient of restitution, `e`) controls bounciness: 0 means perfectly inelastic (no bounce), 1 means perfectly elastic (full bounce). Friction (Coulomb model) opposes sliding motion: the tangential force is bounded by `mu * normal_force`. In impulse-based systems, friction is a tangential impulse clamped by `mu * |j_normal|`.

**Positional correction**: Even with correct impulse math, floating-point drift causes objects to sink into each other over time at rest. Baumgarte stabilization pushes overlapping bodies apart by a fraction of the penetration depth each frame. The correction rate (typically 0.2) must be low enough to avoid jittering but high enough to prevent visible sinking. An alternative is split impulse, which separates velocity correction from positional correction.

## Research Resources

- [Real-Time Collision Detection (Ericson)](https://www.realtimerendering.com/intersections.html) -- the definitive reference for collision detection algorithms
- [Game Physics Engine Development (Millington)](https://www.gameenginebook.com/) -- covers impulse-based resolution, friction, and integration
- [Separating Axis Theorem (Metanet Software)](https://www.metanetsoftware.com/2016/n-tutorial-a-collision-detection-and-response) -- visual SAT tutorial with interactive demos
- [GJK Algorithm Explained](https://blog.winter.dev/2020/gjk-algorithm/) -- alternative to SAT for convex shape intersection
- [Allen Chou - Physics for Game Programmers](https://allenchou.net/game-physics-series/) -- practical game physics series
- [Box2D Source Code](https://github.com/erincatto/box2d) -- the most widely studied 2D physics engine, excellent reference for contact solving
- [Erin Catto - GDC Presentations](https://box2d.org/publications/) -- sequential impulses, constraint solving, from the creator of Box2D
- [Verlet Integration (Thomas Jakobsen)](https://www.cs.cmu.edu/afs/cs/academic/class/15462-s13/www/lec_slides/Jakobsen.pdf) -- the Hitman game physics paper
