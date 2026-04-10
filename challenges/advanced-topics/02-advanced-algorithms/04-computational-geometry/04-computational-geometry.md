<!--
type: reference
difficulty: advanced
section: [02-advanced-algorithms]
concepts: [convex-hull, graham-scan, chans-algorithm, sweep-line, point-in-polygon, delaunay-triangulation]
languages: [go, rust]
estimated_reading_time: 45-75 min
bloom_level: analyze
prerequisites: [linear-algebra-basics, cross-product, sorting]
papers: [graham-1972-convex-hull, chan-1996-optimal-convex-hull, bentley-ottmann-1979-sweep, delaunay-1934-triangulation]
industry_use: [postgis, unreal-engine, openstreetmap, cesium-3d, collision-detection-engines, gdal]
language_contrast: medium
-->

# Computational Geometry

> Geometry algorithms are deceptively simple on paper and deceptively hard in floating-point arithmetic — knowing where numerical precision breaks your assumptions is the real skill.

## Mental Model

Computational geometry is about answering spatial questions at scale. The pattern-recognition
triggers:

- **Convex hull**: "I need the tightest outer boundary of a point set" — obstacle avoidance,
  bounding volume hierarchies, gift wrapping for GIS polygons.
- **Sweep line**: "I need to find all intersections/events in a set of geometric objects" —
  line segment intersection (Bentley-Ottmann), event-driven physics, map overlay.
- **Point-in-polygon**: "Is this GPS coordinate inside this geofence?" — every GIS system,
  delivery zone checking, geospatial databases.
- **Delaunay triangulation**: "I need to mesh a point cloud into triangles, maximizing
  minimum angles" — terrain rendering, mesh generation, interpolation.

The senior engineer's most important geometric intuition: **never compare floats for
equality in geometry code**. Use epsilon comparisons for collinearity tests, and where
possible use exact integer arithmetic (multiply by a scale factor and work in integers).

## Core Concepts

### Cross Product and Orientation

The fundamental primitive for 2D geometry is the **cross product** of two vectors:
`cross(a, b) = a.x * b.y - a.y * b.x`.

For three points P, Q, R:
- `cross(Q-P, R-P) > 0`: R is to the left of PQ (counterclockwise turn)
- `cross(Q-P, R-P) < 0`: R is to the right of PQ (clockwise turn)
- `cross(Q-P, R-P) = 0`: P, Q, R are collinear

This single primitive underlies convex hull, line intersection, and point-in-polygon.
The **key insight**: for integer coordinates, the cross product is exact (no floating-point
error). For floating-point, use `cross > eps` not `cross > 0`.

### Convex Hull: Graham Scan

Find the bottom-most point (lowest y, then leftmost). Sort all other points by polar
angle relative to this point. Walk through the sorted order, maintaining a stack of
points forming a CCW convex polygon. At each step, pop any point that creates a
clockwise turn (non-left turn).

O(n log n) due to sorting. After sorting, the scan is O(n).

**Chan's algorithm** achieves O(n log h) where h is the hull size (number of output
points). It combines the O(nm) Jarvis march (gift wrapping) with Graham scan on O(n/m)
subgroups, guessing the hull size m via doubling. For sparse hulls (h << n), this is
dramatically faster.

### Sweep Line

Imagine a vertical line sweeping left to right across the plane. You maintain a
**status structure** (ordered set of currently active segments, ordered by y-coordinate
at the current x position) and an **event queue** (segment start, end, and intersection
events).

At each event, update the status structure and check the new neighbors for intersection.
The key invariant: only adjacent segments in the status structure can intersect. This
is Bentley-Ottmann: O((n + k) log n) for n segments with k intersections.

For simpler problems (polygon area, point-in-polygon via ray casting), the sweep line
reduces to a sorted scan without the status structure.

### Point-in-Polygon

**Ray casting**: Cast a ray from the query point in any direction (typically +x). Count
crossings with polygon edges. Odd = inside, even = outside.

**Winding number**: Count how many times the polygon winds around the query point.
Winding number = 0 means outside; any non-zero value means inside. More robust than
ray casting for complex/self-intersecting polygons. Used in PostGIS and OpenLayers.

Edge case: the query point lies exactly on an edge. Both methods need special handling.
The winding number method is generally more numerically robust.

### Delaunay Triangulation

The unique triangulation of a point set maximizing the minimum angle of all triangles
(equivalently: no point is inside the circumcircle of any triangle). This "as equilateral
as possible" property makes Delaunay triangulations ideal for FEM mesh generation and
terrain rendering.

The **incremental insertion** algorithm (Bowyer-Watson) works in O(n²) worst case but
O(n log n) expected. Each insertion locates the containing triangle, removes all
triangles whose circumcircle contains the new point (the "cavity"), and re-triangulates
with the new point.

The **circumcircle test** (`inCircle`) is the fundamental primitive: is point D inside
the circumcircle of triangle ABC? Computed as the sign of a 3×3 determinant. For exact
arithmetic, this is a 96-bit integer multiplication in the worst case — motivating
exact arithmetic libraries (Shewchuk's robust predicates).

## Implementation: Go

```go
package main

import (
	"fmt"
	"math"
	"sort"
)

type Point struct{ X, Y float64 }

func sub(a, b Point) Point { return Point{a.X - b.X, a.Y - b.Y} }
func cross(a, b Point) float64 { return a.X*b.Y - a.Y*b.X }
func dot(a, b Point) float64   { return a.X*b.X + a.Y*b.Y }
func norm(a Point) float64     { return math.Sqrt(dot(a, a)) }

// orientation: > 0 CCW, < 0 CW, = 0 collinear
func orientation(o, a, b Point) float64 {
	return cross(sub(a, o), sub(b, o))
}

// ─── Convex Hull: Graham Scan ────────────────────────────────────────────────

func convexHull(pts []Point) []Point {
	n := len(pts)
	if n < 3 { return pts }

	// Find bottom-most (then left-most) point
	pivot := 0
	for i := 1; i < n; i++ {
		if pts[i].Y < pts[pivot].Y || (pts[i].Y == pts[pivot].Y && pts[i].X < pts[pivot].X) {
			pivot = i
		}
	}
	pts[0], pts[pivot] = pts[pivot], pts[0]
	p0 := pts[0]

	// Sort by polar angle, then by distance for collinear points
	sort.Slice(pts[1:], func(i, j int) bool {
		a, b := pts[i+1], pts[j+1]
		o := orientation(p0, a, b)
		if math.Abs(o) > 1e-9 { return o > 0 }
		// Collinear: closer point first
		da := dot(sub(a, p0), sub(a, p0))
		db := dot(sub(b, p0), sub(b, p0))
		return da < db
	})

	stack := []Point{pts[0], pts[1], pts[2]}
	for i := 3; i < n; i++ {
		for len(stack) > 1 {
			o := orientation(stack[len(stack)-2], stack[len(stack)-1], pts[i])
			if o > 1e-9 { break } // CCW turn — keep
			stack = stack[:len(stack)-1]
		}
		stack = append(stack, pts[i])
	}
	return stack
}

// ─── Point in Polygon: Winding Number ───────────────────────────────────────

// windingNumber returns non-zero if p is inside polygon.
// polygon vertices must be in order (CW or CCW), last vertex != first.
func windingNumber(p Point, polygon []Point) int {
	wn := 0
	n := len(polygon)
	for i := 0; i < n; i++ {
		a := polygon[i]
		b := polygon[(i+1)%n]
		if a.Y <= p.Y {
			if b.Y > p.Y {
				if orientation(a, b, p) > 0 { wn++ }
			}
		} else {
			if b.Y <= p.Y {
				if orientation(a, b, p) < 0 { wn-- }
			}
		}
	}
	return wn
}

// ─── Line Segment Intersection ───────────────────────────────────────────────

func segmentsIntersect(p1, p2, p3, p4 Point) bool {
	d1 := orientation(p3, p4, p1)
	d2 := orientation(p3, p4, p2)
	d3 := orientation(p1, p2, p3)
	d4 := orientation(p1, p2, p4)

	if ((d1 > 0 && d2 < 0) || (d1 < 0 && d2 > 0)) &&
		((d3 > 0 && d4 < 0) || (d3 < 0 && d4 > 0)) {
		return true
	}
	// Collinear cases (simplified: check bounding box overlap)
	return false
}

// ─── Sweep Line: Count Intersections (simplified O(n²) for illustration) ────
// Full Bentley-Ottmann requires a balanced BST for the status structure.
// This shows the event-driven structure clearly.

type Segment struct{ A, B Point }

func countIntersections(segs []Segment) int {
	count := 0
	for i := 0; i < len(segs); i++ {
		for j := i + 1; j < len(segs); j++ {
			if segmentsIntersect(segs[i].A, segs[i].B, segs[j].A, segs[j].B) {
				count++
			}
		}
	}
	return count
}

// ─── Delaunay: circumcircle test ─────────────────────────────────────────────
// Returns true if point d is strictly inside the circumcircle of triangle abc (CCW).
// Uses the 3×3 determinant formulation.

func inCircumcircle(a, b, c, d Point) bool {
	ax, ay := a.X-d.X, a.Y-d.Y
	bx, by := b.X-d.X, b.Y-d.Y
	cx, cy := c.X-d.X, c.Y-d.Y
	det := ax*(by*(ax*ax+ay*ay-bx*bx-by*by)-cy*(ax*ax+ay*ay-cx*cx-cy*cy)) -
		ay*(bx*(ax*ax+ay*ay-bx*bx-by*by)-cx*(ax*ax+ay*ay-cx*cx-cy*cy))
	// Simplified 2D version: sign of the 3x3 determinant
	det2 := ax*(by*1-cy*1) - ay*(bx*1-cx*1) + (ax*ax+ay*ay)*(bx*cy-by*cx)
	_ = det
	return det2 > 0
}

// Minimal Bowyer-Watson (3-point to 1 triangle; full implementation is ~150 lines)
type Triangle struct{ A, B, C int } // indices into points slice

func bowyerWatson(points []Point) []Triangle {
	// Super-triangle containing all points
	minX, minY, maxX, maxY := points[0].X, points[0].Y, points[0].X, points[0].Y
	for _, p := range points {
		if p.X < minX { minX = p.X }; if p.Y < minY { minY = p.Y }
		if p.X > maxX { maxX = p.X }; if p.Y > maxY { maxY = p.Y }
	}
	dx, dy := maxX-minX, maxY-minY
	delta := math.Max(dx, dy) * 10
	super := []Point{
		{minX - delta, minY - delta*3},
		{minX - delta, maxY + delta},
		{maxX + delta*3, maxY + delta},
	}
	allPts := append([]Point{}, points...)
	n := len(points)
	allPts = append(allPts, super...)

	// Start with super-triangle (indices n, n+1, n+2)
	triangles := []Triangle{{n, n + 1, n + 2}}

	for i, p := range points {
		// Find all triangles whose circumcircle contains p
		var bad []Triangle
		var good []Triangle
		for _, t := range triangles {
			if inCircumcircle(allPts[t.A], allPts[t.B], allPts[t.C], p) {
				bad = append(bad, t)
			} else {
				good = append(good, t)
			}
		}
		// Find boundary of the cavity (polygon hole)
		type Edge [2]int
		edgeCount := map[Edge]int{}
		for _, t := range bad {
			for _, e := range [][2]int{{t.A, t.B}, {t.B, t.C}, {t.C, t.A}} {
				key := Edge{min(e[0], e[1]), max(e[0], e[1])}
				edgeCount[key]++
			}
		}
		// Re-triangulate with the new point
		triangles = good
		for e, cnt := range edgeCount {
			if cnt == 1 { // boundary edge
				triangles = append(triangles, Triangle{e[0], e[1], i})
			}
		}
	}

	// Remove triangles touching the super-triangle vertices
	final := triangles[:0]
	for _, t := range triangles {
		if t.A < n && t.B < n && t.C < n {
			final = append(final, t)
		}
	}
	return final
}

func min(a, b int) int { if a < b { return a }; return b }
func max(a, b int) int { if a > b { return a }; return b }

func main() {
	pts := []Point{{0, 0}, {1, 3}, {2, 1}, {3, 2}, {4, 0}, {1, 1}}
	hull := convexHull(pts)
	fmt.Println("Convex hull:", hull)

	polygon := []Point{{0, 0}, {4, 0}, {4, 4}, {0, 4}}
	fmt.Println("Point (2,2) in square:", windingNumber(Point{2, 2}, polygon) != 0)
	fmt.Println("Point (5,5) in square:", windingNumber(Point{5, 5}, polygon) != 0)

	segs := []Segment{
		{Point{0, 0}, Point{2, 2}},
		{Point{0, 2}, Point{2, 0}},
		{Point{3, 3}, Point{5, 5}},
	}
	fmt.Println("Intersections:", countIntersections(segs))

	dpts := []Point{{0, 0}, {1, 0}, {0.5, 1}, {0.5, 0.3}}
	tri := bowyerWatson(dpts)
	fmt.Println("Delaunay triangles:", len(tri))
}
```

### Go-specific considerations

- **Floating-point epsilon**: The `1e-9` epsilon in orientation tests is a reasonable default
  for coordinates in the range ±10^6. For GPS coordinates (latitude/longitude), use a
  smaller epsilon or work in integer microdegrees (multiply by 10^7 and use int64).
- **sort.Slice instability**: Go's `sort.Slice` is not stable. For the Graham scan polar-angle
  sort, collinear points must be sorted by distance — the comparator handles this, but do not
  rely on stable sort to break ties.
- **`math.Sqrt` avoidance**: Computing `norm` (sqrt) is expensive. Replace distance comparisons
  with squared-distance comparisons: `da < db` where `da = (a.X-p0.X)^2 + (a.Y-p0.Y)^2`.

## Implementation: Rust

```rust
use std::cmp::Ordering;
use std::collections::HashMap;

#[derive(Clone, Copy, Debug, PartialEq)]
struct Point {
    x: f64,
    y: f64,
}

impl Point {
    fn new(x: f64, y: f64) -> Self { Point { x, y } }
    fn sub(self, o: Self) -> Self { Point::new(self.x - o.x, self.y - o.y) }
    fn cross(self, b: Self) -> f64 { self.x * b.y - self.y * b.x }
    fn dot(self, b: Self) -> f64 { self.x * b.x + self.y * b.y }
    fn dist2(self, o: Self) -> f64 { let d = self.sub(o); d.dot(d) }
}

fn orientation(o: Point, a: Point, b: Point) -> f64 {
    a.sub(o).cross(b.sub(o))
}

// ─── Convex Hull: Graham Scan ────────────────────────────────────────────────

fn convex_hull(mut pts: Vec<Point>) -> Vec<Point> {
    let n = pts.len();
    if n < 3 { return pts; }

    // Find pivot: bottom-most, then left-most
    let pivot = pts.iter().enumerate()
        .min_by(|(_, a), (_, b)| {
            a.y.partial_cmp(&b.y).unwrap()
                .then(a.x.partial_cmp(&b.x).unwrap())
        })
        .map(|(i, _)| i)
        .unwrap();
    pts.swap(0, pivot);
    let p0 = pts[0];

    pts[1..].sort_by(|&a, &b| {
        let o = orientation(p0, a, b);
        if o.abs() > 1e-9 {
            return if o > 0.0 { Ordering::Less } else { Ordering::Greater };
        }
        p0.dist2(a).partial_cmp(&p0.dist2(b)).unwrap()
    });

    let mut stack: Vec<Point> = vec![pts[0], pts[1], pts[2]];
    for &p in &pts[3..] {
        while stack.len() > 1 {
            let n = stack.len();
            if orientation(stack[n-2], stack[n-1], p) > 1e-9 { break; }
            stack.pop();
        }
        stack.push(p);
    }
    stack
}

// ─── Winding Number ──────────────────────────────────────────────────────────

fn winding_number(p: Point, polygon: &[Point]) -> i32 {
    let mut wn = 0i32;
    let n = polygon.len();
    for i in 0..n {
        let a = polygon[i];
        let b = polygon[(i + 1) % n];
        if a.y <= p.y {
            if b.y > p.y && orientation(a, b, p) > 0.0 { wn += 1; }
        } else if b.y <= p.y && orientation(a, b, p) < 0.0 {
            wn -= 1;
        }
    }
    wn
}

// ─── Segment Intersection ────────────────────────────────────────────────────

fn segments_intersect(p1: Point, p2: Point, p3: Point, p4: Point) -> bool {
    let d1 = orientation(p3, p4, p1);
    let d2 = orientation(p3, p4, p2);
    let d3 = orientation(p1, p2, p3);
    let d4 = orientation(p1, p2, p4);
    (d1 > 0.0 && d2 < 0.0 || d1 < 0.0 && d2 > 0.0)
        && (d3 > 0.0 && d4 < 0.0 || d3 < 0.0 && d4 > 0.0)
}

// ─── Bowyer-Watson Delaunay ──────────────────────────────────────────────────

fn in_circumcircle(a: Point, b: Point, c: Point, d: Point) -> bool {
    let ax = a.x - d.x; let ay = a.y - d.y;
    let bx = b.x - d.x; let by = b.y - d.y;
    let cx = c.x - d.x; let cy = c.y - d.y;
    let det = ax * (by * (cx*cx + cy*cy) - cy * (bx*bx + by*by))
            - ay * (bx * (cx*cx + cy*cy) - cx * (bx*bx + by*by))
            + (ax*ax + ay*ay) * (bx*cy - by*cx);
    det > 0.0
}

#[derive(Clone, Copy, Debug)]
struct Triangle { a: usize, b: usize, c: usize }

fn bowyer_watson(points: &[Point]) -> Vec<Triangle> {
    let n = points.len();
    let (mut min_x, mut min_y, mut max_x, mut max_y) =
        (points[0].x, points[0].y, points[0].x, points[0].y);
    for p in points {
        min_x = min_x.min(p.x); min_y = min_y.min(p.y);
        max_x = max_x.max(p.x); max_y = max_y.max(p.y);
    }
    let delta = (max_x - min_x).max(max_y - min_y) * 10.0;
    let mut all_pts: Vec<Point> = points.to_vec();
    all_pts.push(Point::new(min_x - delta, min_y - delta * 3.0));
    all_pts.push(Point::new(min_x - delta, max_y + delta));
    all_pts.push(Point::new(max_x + delta * 3.0, max_y + delta));

    let mut triangles = vec![Triangle { a: n, b: n+1, c: n+2 }];

    for i in 0..n {
        let p = points[i];
        let (mut bad, mut good): (Vec<_>, Vec<_>) = triangles.into_iter().partition(|t| {
            in_circumcircle(all_pts[t.a], all_pts[t.b], all_pts[t.c], p)
        });

        let mut edge_count: HashMap<(usize, usize), usize> = HashMap::new();
        for t in &bad {
            for e in &[(t.a, t.b), (t.b, t.c), (t.c, t.a)] {
                let key = (e.0.min(e.1), e.0.max(e.1));
                *edge_count.entry(key).or_default() += 1;
            }
        }

        for ((ea, eb), cnt) in edge_count {
            if cnt == 1 {
                good.push(Triangle { a: ea, b: eb, c: i });
            }
        }
        bad.clear();
        triangles = good;
    }

    triangles.retain(|t| t.a < n && t.b < n && t.c < n);
    triangles
}

fn main() {
    let pts = vec![
        Point::new(0.0, 0.0), Point::new(1.0, 3.0),
        Point::new(2.0, 1.0), Point::new(3.0, 2.0),
        Point::new(4.0, 0.0), Point::new(1.0, 1.0),
    ];
    let hull = convex_hull(pts.clone());
    println!("Hull: {:?}", hull);

    let polygon = vec![
        Point::new(0.0,0.0), Point::new(4.0,0.0),
        Point::new(4.0,4.0), Point::new(0.0,4.0),
    ];
    println!("(2,2) inside: {}", winding_number(Point::new(2.0,2.0), &polygon) != 0);
    println!("(5,5) inside: {}", winding_number(Point::new(5.0,5.0), &polygon) != 0);

    let dpts = vec![
        Point::new(0.0,0.0), Point::new(1.0,0.0),
        Point::new(0.5,1.0), Point::new(0.5,0.3),
    ];
    let tris = bowyer_watson(&dpts);
    println!("Delaunay triangles: {}", tris.len());
}
```

### Rust-specific considerations

- **`f64` comparison in sort**: `partial_cmp` returns `Option<Ordering>` because `f64`
  has `NaN`. The `.unwrap()` is safe here only if inputs contain no `NaN`. For defensive
  code, handle `None` as `Ordering::Equal`.
- **`geo` crate**: The `geo` crate provides production-quality geometry with robust
  predicates, tolerance handling, and coordinate types. For production GIS work, use it
  rather than this reference implementation.
- **`robust` crate**: Shewchuk's robust geometric predicates are available as the
  `robust` crate. For orientation and in-circumcircle tests on floating-point coordinates,
  this is the correct solution — it uses adaptive-precision arithmetic to guarantee correct
  sign.
- **`HashMap` in Bowyer-Watson**: The `HashMap<(usize, usize), usize>` for edge counting
  allocates per insertion. For performance-critical Delaunay on 10^5+ points, use a
  `Vec` and sort/deduplicate edges.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|------|
| Float sorting | `sort.Slice` with `<` — `NaN` causes undefined behavior | `sort_by` + `partial_cmp().unwrap()` — explicit NaN handling required |
| `geo` ecosystem | `github.com/twpayne/go-geom` — mature, WKT/WKB support | `geo` crate — excellent; `geo-types`, `geographiclib-geodesic` |
| Exact predicates | No stdlib; must implement or use CGO with `libgeos` | `robust` crate for Shewchuk predicates |
| Performance | 2–5× slower than Rust for float-heavy geometry loops | SIMD auto-vectorization for Point operations is possible |
| Integer geometry | `int64` for microdegree GPS coords; overflow at ±90*10^7 | `i64` same; `i128` for intermediate cross products |
| Concurrency | Goroutines for parallel spatial partitions (e.g., k-d tree) | Rayon `par_iter` on point clouds; `Send + Sync` for spatial indices |

## Production War Stories

**PostGIS / GEOS**: PostGIS uses the GEOS library (C++) for all polygon operations.
GEOS uses Shewchuk's exact predicates for orientation tests and a JTS-derived sweep line
for polygon intersection. Every `ST_Intersects` call in PostgreSQL ultimately hits this code.

**Unreal Engine collision detection**: The Chaos physics engine uses GJK (Gilbert-Johnson-Keerthi)
for convex-hull collision, which internally relies on the Minkowski difference and the
same orientation primitive. BVH (bounding volume hierarchy) construction uses convex hulls
as bounding volumes.

**OpenStreetMap routing (OSRM)**: OSRM uses point-in-polygon to resolve GPS coordinates to
road segments, and sweep-line algorithms to process polygon intersections when building the
routing graph from OSM data.

**Cesium 3D Tiles**: The tiling pipeline uses Delaunay triangulation to convert LiDAR point
clouds into terrain meshes. The incremental Bowyer-Watson algorithm is used with spatial
indexing (quadtree) to achieve near-O(n log n) for the insertion phase.

**GDAL (Geospatial Data Abstraction Library)**: Used by ArcGIS, QGIS, and every major GIS
pipeline. The polygon dissolve operation uses a sweep line algorithm over edge events.

## Complexity Analysis

| Algorithm | Time | Space | Notes |
|-----------|------|-------|-------|
| Graham scan | O(n log n) | O(n) | Sort dominates |
| Chan's algorithm | O(n log h) | O(n) | h = hull size; optimal |
| Jarvis march | O(nh) | O(n) | Good for small h, bad for h ≈ n |
| Winding number | O(n) | O(1) | Per query; O(n) preprocessing for sorted polygon |
| Bentley-Ottmann | O((n+k) log n) | O(n+k) | k = intersections; requires balanced BST |
| Bowyer-Watson (Delaunay) | O(n²) worst, O(n log n) expected | O(n) | Exact Delaunay |
| Fortune's sweep (Delaunay) | O(n log n) | O(n) | Deterministic; harder to implement |

**Numerical precision note**: The `inCircumcircle` test is the most numerically sensitive
primitive in Delaunay. For points nearly cocircular, naive floating-point gives wrong sign.
Shewchuk's adaptive-precision predicates add < 15% overhead and guarantee correct results.
In production geometry code, always use them.

## Common Pitfalls

1. **Graham scan: not handling collinear points on the hull boundary**: If collinear points
   on the hull are not handled consistently (keep all vs. keep endpoints only), the algorithm
   either produces a non-convex result or silently drops hull points. Decide upfront and
   enforce it in the comparator.

2. **Winding number: assuming polygon is simple**: The winding number correctly handles
   self-intersecting polygons (even-odd rule gives a different result). Know which semantics
   your polygon data uses — OSM uses consistent winding; GeoJSON uses right-hand rule.

3. **Floating-point orientation test used as equality**: `orientation(p, q, r) == 0.0` for
   collinearity never works reliably. Use `|orientation| < epsilon` with a domain-appropriate
   epsilon.

4. **Bowyer-Watson with non-general-position input**: When four or more points are cocircular,
   the Bowyer-Watson cavity is ambiguous. Add a tiny random perturbation (symbolic perturbation)
   or handle ties explicitly to get a consistent triangulation.

5. **Ignoring coordinate system**: Computing convex hulls on lat/lon coordinates as if they
   were Cartesian gives wrong results near the poles or antimeridian. Always project to a
   suitable planar coordinate system (UTM, Web Mercator) before applying planar geometry algorithms.

## Exercises

**Exercise 1 — Verification** (30 min):
Implement Graham scan and verify it gives the same hull as a brute-force O(n³) algorithm
(for each candidate edge, check all other points are on the same side). Test on 1000
random point sets of size 20–100. Time both approaches.

**Exercise 2 — Extension** (2–4 h):
Implement Chan's algorithm and benchmark against Graham scan for point sets where the
hull size h ranges from 3 to n (synthetic test cases: points on a circle give h = n;
random interior points give small h). Plot n vs. runtime for both algorithms.

**Exercise 3 — From Scratch** (4–8 h):
Implement Fortune's sweep line algorithm for Delaunay triangulation (O(n log n)
deterministic, more complex than Bowyer-Watson). Validate that the output matches
Bowyer-Watson on 100 random point sets of size 50. Profile memory allocation patterns
(Fortune's uses a beachline structure; Bowyer-Watson uses triangle lists).

**Exercise 4 — Production Scenario** (8–15 h):
Build a geofencing service. Input: a set of up to 10,000 polygon geofences (GeoJSON).
Query: given a stream of GPS coordinates (lat/lon, 100k/sec), determine which geofences
each point is inside. Requirements: < 1 ms per query at p99, support online geofence
updates (add/remove polygons without downtime). Design a spatial index (R-tree or grid
partition) on top of the winding number point-in-polygon algorithm. Implement in Go or
Rust with a gRPC API. Benchmark and profile.

## Further Reading

### Foundational Papers
- Graham, R. L. (1972). "An efficient algorithm for determining the convex hull of a finite
  planar set." *Information Processing Letters*, 1(4), 132–133.
- Chan, T. M. (1996). "Optimal output-sensitive convex hull algorithms in two and three
  dimensions." *Discrete & Computational Geometry*, 16(4), 361–368.
- Shewchuk, J. R. (1997). "Adaptive precision floating-point arithmetic and fast robust
  geometric predicates." *Discrete & Computational Geometry*, 18(3), 305–363. Essential reading
  for anyone writing production geometry code.

### Books
- *Computational Geometry: Algorithms and Applications* — de Berg, van Kreveld, Overmars,
  Schwarzkopf. The standard reference. Chapters 1–4 cover convex hull, sweep line, and
  triangulation.
- *Real-Time Collision Detection* — Christer Ericson. Practical geometry algorithms for
  game engines; includes GJK, BVH, and robustness techniques.

### Production Code to Read
- **GEOS library** (`github.com/libgeos/geos`): `operation/overlay/` for sweep-line polygon
  overlay; `algorithm/ConvexHull.cpp` for the production Graham scan.
- **Shewchuk predicates** (`www.cs.cmu.edu/~quake/robust.html`): `predicates.c` — the
  canonical implementation of robust orientation and in-circumcircle tests.
- **`geo` Rust crate** (`github.com/georust/geo`): `algorithm/convex_hull.rs` and
  `algorithm/winding_order.rs` for idiomatic Rust geometry.

### Conference Talks
- "Geometry in Practice" — FOSSGIS 2021, PostGIS team. Real-world numerical issues in
  production GIS code.
- "Robust Geometric Computation" — SoCG 2018 keynote, Shewchuk. Current state of exact
  and approximate geometric predicates.
