<!-- difficulty: advanced -->
<!-- category: data-structures, spatial -->
<!-- languages: [rust] -->
<!-- concepts: [r-tree, spatial-indexing, bounding-boxes, nearest-neighbor, tree-balancing, bulk-loading] -->
<!-- estimated_time: 12-18 hours -->
<!-- bloom_level: apply, analyze, evaluate, create -->
<!-- prerequisites: [binary-trees, generics, trait-bounds, computational-geometry-basics, ordering-traits] -->

# Challenge 112: R-Tree Spatial Index

## Languages

Rust (stable, latest edition)

## Prerequisites

- Strong understanding of tree data structures (insertion, splitting, traversal)
- Familiarity with Rust generics, trait bounds, and associated types
- Basic computational geometry: bounding boxes, point-in-rectangle tests, distance calculations
- Understanding of tree balancing heuristics and their impact on query performance

## Learning Objectives

- **Implement** a complete R-tree supporting insert, delete, and multiple query types
- **Apply** bounding box geometry to determine spatial containment, overlap, and expansion cost
- **Design** node splitting strategies (linear and quadratic split) that minimize overlap and dead space
- **Analyze** how split heuristics affect query performance and tree balance
- **Evaluate** the trade-offs between insertion-time optimization and query-time performance
- **Create** a bulk loading algorithm (Sort-Tile-Recursive) for building optimized trees from known datasets

## The Challenge

Build an R-tree, the foundational spatial index used in geographic information systems, game engines, and databases. An R-tree organizes spatial objects by their bounding boxes in a balanced tree where each node's bounding box encloses all its children. This enables efficient pruning: a spatial query can skip entire subtrees whose bounding boxes do not intersect the query region.

Your R-tree must support three core query types: point queries (which objects contain this point?), window/range queries (which objects intersect this rectangle?), and nearest-neighbor queries (what are the k closest objects to this point?). It must handle both 2D and 3D data through a generic dimensionality trait.

The critical algorithmic challenge is node splitting. When a node overflows, you must split it into two nodes that minimize the total bounding box area and overlap. A poor split creates large, overlapping bounding boxes that force queries to visit many nodes. You will implement both linear split (fast, O(n)) and quadratic split (better quality, O(n^2)).

For large datasets, one-by-one insertion produces suboptimal trees. Implement Sort-Tile-Recursive (STR) bulk loading, which sorts objects along each axis and recursively partitions them into leaf nodes, producing nearly optimal trees in O(n log n) time.

## Requirements

1. Define a `BoundingBox<const D: usize>` type representing an axis-aligned bounding box in D dimensions
2. Implement bounding box operations: `contains_point`, `intersects`, `union`, `area`, `margin`, `overlap_area`, `expansion_needed`
3. Define an `RTree<T, const D: usize>` with configurable minimum and maximum entries per node (default M=16, m=M/2)
4. Implement `insert(&mut self, item: T, bbox: BoundingBox<D>)` with choose-subtree (minimum enlargement) and node splitting
5. Implement `delete(&mut self, item: &T, bbox: &BoundingBox<D>)` with condense-tree (reinsert orphaned entries)
6. Implement `search_point(&self, point: [f64; D]) -> Vec<&T>` -- find all entries whose bbox contains the point
7. Implement `search_window(&self, window: &BoundingBox<D>) -> Vec<&T>` -- find all entries whose bbox intersects the window
8. Implement `nearest(&self, point: [f64; D], k: usize) -> Vec<(&T, f64)>` -- find k nearest entries by minimum distance, using a priority queue to prune branches
9. Implement linear split (pick seeds by maximum separation, assign remaining by preference) and quadratic split (pick seeds by maximum dead space, assign remaining by minimum enlargement)
10. Implement STR bulk loading: given a batch of entries, build an optimized tree without individual insertions
11. Support both 2D (`[f64; 2]`) and 3D (`[f64; 3]`) through const generics
12. Build a geographic example: index restaurants by location, query "all restaurants within 5km of a point"

## Hints

Hints for advanced challenges are intentionally sparse. These point you in the right direction without revealing the implementation.

- A `BoundingBox<D>` is fully defined by two corners: `min: [f64; D]` and `max: [f64; D]`. All operations reduce to per-axis comparisons.
- The choose-subtree algorithm picks the child whose bounding box requires the least enlargement to accommodate the new entry. Break ties by choosing the child with the smallest area.
- For node splitting, the "pick seeds" step determines split quality more than the "assign remaining" step. Quadratic pick-seeds tries all O(n^2) pairs and picks the pair that wastes the most area (union area minus individual areas).
- Nearest-neighbor search uses a min-heap of (distance, node) entries. The key optimization: compute the minimum possible distance from the query point to a bounding box (not just the center). Prune any branch whose minimum distance exceeds the current k-th nearest distance.
- STR bulk loading: sort entries by the first coordinate, partition into slabs of size sqrt(n), within each slab sort by the second coordinate, then partition into leaf nodes of size M. Recursively build internal nodes the same way.
- For deletion, after removing an entry, check whether the containing node has fewer than `m` entries. If so, dissolve the node and reinsert all its entries. This condense-tree step is what keeps the tree balanced.

## Acceptance Criteria

- [ ] `BoundingBox<D>` correctly computes containment, intersection, union, area, and expansion for 2D and 3D
- [ ] `insert` places entries correctly and splits nodes when they overflow
- [ ] `delete` removes entries and condenses underflowed nodes
- [ ] `search_point` returns all entries containing a given point
- [ ] `search_window` returns all entries intersecting a given rectangle/box
- [ ] `nearest` returns the k closest entries with correct distances, using branch-and-bound pruning
- [ ] Both linear and quadratic split strategies are implemented and selectable
- [ ] STR bulk loading produces a valid R-tree with better query performance than sequential insertion
- [ ] 2D and 3D queries work correctly using const generics
- [ ] Geographic example: indexing 1000+ restaurants and querying by radius works correctly
- [ ] All tests pass with `cargo test`

## Research Resources

- [The R-tree: A Dynamic Index Structure for Spatial Searching (Guttman, 1984)](http://www-db.deis.unibo.it/courses/SI-LS/papers/Gut84.pdf) -- the original R-tree paper
- [The R*-tree: An Efficient and Robust Access Method (Beckmann et al., 1990)](https://infolab.usc.edu/csci599/Fall2001/paper/rstar-tree.pdf) -- improved split and reinsertion strategies
- [STR: A Simple and Efficient Algorithm for R-Tree Packing (Leutenegger et al., 1997)](https://ieeexplore.ieee.org/document/582015) -- Sort-Tile-Recursive bulk loading
- [Nearest Neighbor Queries (Roussopoulos et al., 1995)](https://www.cs.umd.edu/~nick/papers/nnpaper.pdf) -- branch-and-bound nearest neighbor on R-trees
- [The `rstar` crate source code](https://github.com/georust/rstar) -- production Rust R-tree implementation for reference
- [R-tree visualization (University of Maryland)](https://www.cs.umd.edu/class/fall2019/cmsc420-0201/Lects/lect14-Rtree.pdf) -- visual explanation of splits and queries
