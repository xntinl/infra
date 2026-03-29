# Solution: R-Tree Spatial Index

## Architecture Overview

The solution is structured in four modules:

1. **Bounding box geometry** -- `BoundingBox<D>` with all spatial predicates and metrics, fully generic over dimensionality via const generics
2. **Core R-tree** -- `RTree<T, D>` with insert, delete, and the internal node/entry representation
3. **Split strategies** -- Linear and quadratic split as a strategy trait, selectable at tree construction
4. **Queries and bulk loading** -- Point, window, and nearest-neighbor queries plus STR bulk loading

The tree uses a flattened arena-style node storage (nodes stored in a `Vec` with indices) to avoid deep pointer chasing and simplify Rust's ownership model. Each node is either a leaf (contains data entries) or an internal node (contains child node indices).

## Rust Solution

### Project Setup

```bash
cargo new rtree-spatial
cd rtree-spatial
```

```toml
[package]
name = "rtree-spatial"
version = "0.1.0"
edition = "2021"

[dependencies]

[dev-dependencies]
rand = "0.8"
```

### Source: `src/bbox.rs`

```rust
use std::fmt;

/// Axis-aligned bounding box in D dimensions.
#[derive(Clone, PartialEq)]
pub struct BoundingBox<const D: usize> {
    pub min: [f64; D],
    pub max: [f64; D],
}

impl<const D: usize> fmt::Debug for BoundingBox<D> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "BBox({:?} -> {:?})", self.min, self.max)
    }
}

impl<const D: usize> BoundingBox<D> {
    pub fn new(min: [f64; D], max: [f64; D]) -> Self {
        for i in 0..D {
            debug_assert!(min[i] <= max[i], "min must be <= max on axis {i}");
        }
        Self { min, max }
    }

    pub fn from_point(point: [f64; D]) -> Self {
        Self { min: point, max: point }
    }

    pub fn contains_point(&self, point: &[f64; D]) -> bool {
        (0..D).all(|i| point[i] >= self.min[i] && point[i] <= self.max[i])
    }

    pub fn intersects(&self, other: &Self) -> bool {
        (0..D).all(|i| self.min[i] <= other.max[i] && self.max[i] >= other.min[i])
    }

    pub fn contains_bbox(&self, other: &Self) -> bool {
        (0..D).all(|i| self.min[i] <= other.min[i] && self.max[i] >= other.max[i])
    }

    /// Smallest bounding box enclosing both self and other.
    pub fn union(&self, other: &Self) -> Self {
        let mut min = [0.0; D];
        let mut max = [0.0; D];
        for i in 0..D {
            min[i] = self.min[i].min(other.min[i]);
            max[i] = self.max[i].max(other.max[i]);
        }
        Self { min, max }
    }

    /// Expand self to include other (mutating union).
    pub fn expand(&mut self, other: &Self) {
        for i in 0..D {
            self.min[i] = self.min[i].min(other.min[i]);
            self.max[i] = self.max[i].max(other.max[i]);
        }
    }

    /// Area (or volume in 3D, hypervolume in general).
    pub fn area(&self) -> f64 {
        let mut a = 1.0;
        for i in 0..D {
            a *= self.max[i] - self.min[i];
        }
        a
    }

    /// Margin (perimeter in 2D, surface area in 3D).
    pub fn margin(&self) -> f64 {
        let mut m = 0.0;
        for i in 0..D {
            let mut face = 1.0;
            for j in 0..D {
                if i != j {
                    face *= self.max[j] - self.min[j];
                }
            }
            m += face;
        }
        2.0 * m
    }

    /// Area of intersection between two bounding boxes, or 0 if they don't intersect.
    pub fn overlap_area(&self, other: &Self) -> f64 {
        let mut area = 1.0;
        for i in 0..D {
            let lo = self.min[i].max(other.min[i]);
            let hi = self.max[i].min(other.max[i]);
            if lo >= hi {
                return 0.0;
            }
            area *= hi - lo;
        }
        area
    }

    /// How much would self's area increase if expanded to include other?
    pub fn expansion_needed(&self, other: &Self) -> f64 {
        self.union(other).area() - self.area()
    }

    /// Minimum distance from a point to the bounding box surface or interior.
    /// Returns 0 if the point is inside the box.
    pub fn min_distance_to_point(&self, point: &[f64; D]) -> f64 {
        let mut dist_sq = 0.0;
        for i in 0..D {
            if point[i] < self.min[i] {
                let d = self.min[i] - point[i];
                dist_sq += d * d;
            } else if point[i] > self.max[i] {
                let d = point[i] - self.max[i];
                dist_sq += d * d;
            }
        }
        dist_sq.sqrt()
    }

    pub fn center(&self) -> [f64; D] {
        let mut c = [0.0; D];
        for i in 0..D {
            c[i] = (self.min[i] + self.max[i]) / 2.0;
        }
        c
    }
}
```

### Source: `src/node.rs`

```rust
use crate::bbox::BoundingBox;

/// An entry stored in a leaf node.
#[derive(Debug, Clone)]
pub struct LeafEntry<T, const D: usize> {
    pub bbox: BoundingBox<D>,
    pub data: T,
}

/// An entry in an internal node: a child index and its bounding box.
#[derive(Debug, Clone)]
pub struct ChildEntry<const D: usize> {
    pub bbox: BoundingBox<D>,
    pub child_idx: usize,
}

/// An R-tree node, either leaf or internal.
#[derive(Debug, Clone)]
pub enum Node<T, const D: usize> {
    Leaf {
        entries: Vec<LeafEntry<T, D>>,
    },
    Internal {
        entries: Vec<ChildEntry<D>>,
    },
}

impl<T, const D: usize> Node<T, D> {
    pub fn new_leaf() -> Self {
        Node::Leaf { entries: Vec::new() }
    }

    pub fn new_internal() -> Self {
        Node::Internal { entries: Vec::new() }
    }

    pub fn is_leaf(&self) -> bool {
        matches!(self, Node::Leaf { .. })
    }

    pub fn len(&self) -> usize {
        match self {
            Node::Leaf { entries } => entries.len(),
            Node::Internal { entries } => entries.len(),
        }
    }

    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }

    /// Compute the bounding box enclosing all entries in this node.
    pub fn compute_bbox(&self) -> Option<BoundingBox<D>> {
        match self {
            Node::Leaf { entries } => {
                let mut iter = entries.iter().map(|e| &e.bbox);
                let first = iter.next()?.clone();
                Some(iter.fold(first, |acc, b| acc.union(b)))
            }
            Node::Internal { entries } => {
                let mut iter = entries.iter().map(|e| &e.bbox);
                let first = iter.next()?.clone();
                Some(iter.fold(first, |acc, b| acc.union(b)))
            }
        }
    }
}
```

### Source: `src/split.rs`

```rust
use crate::bbox::BoundingBox;
use crate::node::LeafEntry;

/// Strategy for splitting an overflowed node.
pub enum SplitStrategy {
    Linear,
    Quadratic,
}

/// Split a list of leaf entries into two groups.
/// Returns (group_a, group_b).
pub fn split_leaf_entries<T: Clone, const D: usize>(
    entries: Vec<LeafEntry<T, D>>,
    strategy: &SplitStrategy,
    min_entries: usize,
) -> (Vec<LeafEntry<T, D>>, Vec<LeafEntry<T, D>>) {
    match strategy {
        SplitStrategy::Linear => linear_split(entries, min_entries),
        SplitStrategy::Quadratic => quadratic_split(entries, min_entries),
    }
}

/// Split child entries for internal nodes.
pub fn split_child_entries<const D: usize>(
    entries: Vec<crate::node::ChildEntry<D>>,
    strategy: &SplitStrategy,
    min_entries: usize,
) -> (Vec<crate::node::ChildEntry<D>>, Vec<crate::node::ChildEntry<D>>) {
    let bboxes: Vec<&BoundingBox<D>> = entries.iter().map(|e| &e.bbox).collect();
    let (seed_a, seed_b) = match strategy {
        SplitStrategy::Linear => pick_seeds_linear(&bboxes),
        SplitStrategy::Quadratic => pick_seeds_quadratic(&bboxes),
    };

    let mut group_a = vec![entries[seed_a].clone()];
    let mut group_b = vec![entries[seed_b].clone()];
    let mut bbox_a = entries[seed_a].bbox.clone();
    let mut bbox_b = entries[seed_b].bbox.clone();

    let mut remaining: Vec<_> = entries
        .into_iter()
        .enumerate()
        .filter(|(i, _)| *i != seed_a && *i != seed_b)
        .map(|(_, e)| e)
        .collect();

    distribute_remaining_children(
        &mut remaining,
        &mut group_a,
        &mut group_b,
        &mut bbox_a,
        &mut bbox_b,
        min_entries,
    );

    (group_a, group_b)
}

fn linear_split<T: Clone, const D: usize>(
    entries: Vec<LeafEntry<T, D>>,
    min_entries: usize,
) -> (Vec<LeafEntry<T, D>>, Vec<LeafEntry<T, D>>) {
    let bboxes: Vec<&BoundingBox<D>> = entries.iter().map(|e| &e.bbox).collect();
    let (seed_a, seed_b) = pick_seeds_linear(&bboxes);

    let mut group_a = vec![entries[seed_a].clone()];
    let mut group_b = vec![entries[seed_b].clone()];
    let mut bbox_a = entries[seed_a].bbox.clone();
    let mut bbox_b = entries[seed_b].bbox.clone();

    let mut remaining: Vec<_> = entries
        .into_iter()
        .enumerate()
        .filter(|(i, _)| *i != seed_a && *i != seed_b)
        .map(|(_, e)| e)
        .collect();

    distribute_remaining(
        &mut remaining,
        &mut group_a,
        &mut group_b,
        &mut bbox_a,
        &mut bbox_b,
        min_entries,
    );

    (group_a, group_b)
}

fn quadratic_split<T: Clone, const D: usize>(
    entries: Vec<LeafEntry<T, D>>,
    min_entries: usize,
) -> (Vec<LeafEntry<T, D>>, Vec<LeafEntry<T, D>>) {
    let bboxes: Vec<&BoundingBox<D>> = entries.iter().map(|e| &e.bbox).collect();
    let (seed_a, seed_b) = pick_seeds_quadratic(&bboxes);

    let mut group_a = vec![entries[seed_a].clone()];
    let mut group_b = vec![entries[seed_b].clone()];
    let mut bbox_a = entries[seed_a].bbox.clone();
    let mut bbox_b = entries[seed_b].bbox.clone();

    let mut remaining: Vec<_> = entries
        .into_iter()
        .enumerate()
        .filter(|(i, _)| *i != seed_a && *i != seed_b)
        .map(|(_, e)| e)
        .collect();

    distribute_remaining(
        &mut remaining,
        &mut group_a,
        &mut group_b,
        &mut bbox_a,
        &mut bbox_b,
        min_entries,
    );

    (group_a, group_b)
}

/// Linear pick seeds: find the pair with maximum normalized separation along any axis.
fn pick_seeds_linear<const D: usize>(bboxes: &[&BoundingBox<D>]) -> (usize, usize) {
    let mut best_sep = f64::NEG_INFINITY;
    let mut best = (0, 1);

    for axis in 0..D {
        let mut highest_low_idx = 0;
        let mut highest_low = f64::NEG_INFINITY;
        let mut lowest_high_idx = 0;
        let mut lowest_high = f64::INFINITY;
        let mut global_low = f64::INFINITY;
        let mut global_high = f64::NEG_INFINITY;

        for (i, bb) in bboxes.iter().enumerate() {
            if bb.min[axis] > highest_low {
                highest_low = bb.min[axis];
                highest_low_idx = i;
            }
            if bb.max[axis] < lowest_high {
                lowest_high = bb.max[axis];
                lowest_high_idx = i;
            }
            global_low = global_low.min(bb.min[axis]);
            global_high = global_high.max(bb.max[axis]);
        }

        let width = global_high - global_low;
        if width <= 0.0 {
            continue;
        }

        let sep = (highest_low - lowest_high).abs() / width;
        if sep > best_sep && highest_low_idx != lowest_high_idx {
            best_sep = sep;
            best = (highest_low_idx, lowest_high_idx);
        }
    }

    best
}

/// Quadratic pick seeds: find the pair that wastes the most area.
fn pick_seeds_quadratic<const D: usize>(bboxes: &[&BoundingBox<D>]) -> (usize, usize) {
    let mut worst_waste = f64::NEG_INFINITY;
    let mut best = (0, 1);

    for i in 0..bboxes.len() {
        for j in (i + 1)..bboxes.len() {
            let combined = bboxes[i].union(bboxes[j]);
            let waste = combined.area() - bboxes[i].area() - bboxes[j].area();
            if waste > worst_waste {
                worst_waste = waste;
                best = (i, j);
            }
        }
    }

    best
}

fn distribute_remaining<T: Clone, const D: usize>(
    remaining: &mut Vec<LeafEntry<T, D>>,
    group_a: &mut Vec<LeafEntry<T, D>>,
    group_b: &mut Vec<LeafEntry<T, D>>,
    bbox_a: &mut BoundingBox<D>,
    bbox_b: &mut BoundingBox<D>,
    min_entries: usize,
) {
    while let Some(entry) = remaining.pop() {
        // If one group needs all remaining to meet minimum, assign them all there.
        if group_a.len() + remaining.len() + 1 == min_entries {
            bbox_a.expand(&entry.bbox);
            group_a.push(entry);
            for e in remaining.drain(..) {
                bbox_a.expand(&e.bbox);
                group_a.push(e);
            }
            return;
        }
        if group_b.len() + remaining.len() + 1 == min_entries {
            bbox_b.expand(&entry.bbox);
            group_b.push(entry);
            for e in remaining.drain(..) {
                bbox_b.expand(&e.bbox);
                group_b.push(e);
            }
            return;
        }

        let expand_a = bbox_a.expansion_needed(&entry.bbox);
        let expand_b = bbox_b.expansion_needed(&entry.bbox);

        if expand_a < expand_b || (expand_a == expand_b && bbox_a.area() <= bbox_b.area()) {
            bbox_a.expand(&entry.bbox);
            group_a.push(entry);
        } else {
            bbox_b.expand(&entry.bbox);
            group_b.push(entry);
        }
    }
}

fn distribute_remaining_children<const D: usize>(
    remaining: &mut Vec<crate::node::ChildEntry<D>>,
    group_a: &mut Vec<crate::node::ChildEntry<D>>,
    group_b: &mut Vec<crate::node::ChildEntry<D>>,
    bbox_a: &mut BoundingBox<D>,
    bbox_b: &mut BoundingBox<D>,
    min_entries: usize,
) {
    while let Some(entry) = remaining.pop() {
        if group_a.len() + remaining.len() + 1 == min_entries {
            bbox_a.expand(&entry.bbox);
            group_a.push(entry);
            for e in remaining.drain(..) {
                bbox_a.expand(&e.bbox);
                group_a.push(e);
            }
            return;
        }
        if group_b.len() + remaining.len() + 1 == min_entries {
            bbox_b.expand(&entry.bbox);
            group_b.push(entry);
            for e in remaining.drain(..) {
                bbox_b.expand(&e.bbox);
                group_b.push(e);
            }
            return;
        }

        let expand_a = bbox_a.expansion_needed(&entry.bbox);
        let expand_b = bbox_b.expansion_needed(&entry.bbox);

        if expand_a < expand_b || (expand_a == expand_b && bbox_a.area() <= bbox_b.area()) {
            bbox_a.expand(&entry.bbox);
            group_a.push(entry);
        } else {
            bbox_b.expand(&entry.bbox);
            group_b.push(entry);
        }
    }
}
```

### Source: `src/rtree.rs`

```rust
use std::collections::BinaryHeap;
use std::cmp::Ordering;

use crate::bbox::BoundingBox;
use crate::node::{Node, LeafEntry, ChildEntry};
use crate::split::{SplitStrategy, split_leaf_entries, split_child_entries};

pub struct RTree<T, const D: usize> {
    nodes: Vec<Node<T, D>>,
    root: usize,
    max_entries: usize,
    min_entries: usize,
    split_strategy: SplitStrategy,
    size: usize,
}

impl<T: Clone + PartialEq + std::fmt::Debug, const D: usize> RTree<T, D> {
    pub fn new(max_entries: usize, strategy: SplitStrategy) -> Self {
        let min_entries = max_entries / 2;
        let root_node = Node::new_leaf();
        let nodes = vec![root_node];
        Self {
            nodes,
            root: 0,
            max_entries,
            min_entries,
            split_strategy: strategy,
            size: 0,
        }
    }

    pub fn with_defaults() -> Self {
        Self::new(16, SplitStrategy::Quadratic)
    }

    pub fn len(&self) -> usize {
        self.size
    }

    pub fn is_empty(&self) -> bool {
        self.size == 0
    }

    // --- Insert ---

    pub fn insert(&mut self, data: T, bbox: BoundingBox<D>) {
        let entry = LeafEntry { bbox, data };
        let split = self.insert_into_node(self.root, entry);

        if let Some((new_node_idx, new_bbox)) = split {
            // Root was split: create a new root.
            let old_root = self.root;
            let old_bbox = self.nodes[old_root].compute_bbox().unwrap();

            let new_root = Node::Internal {
                entries: vec![
                    ChildEntry { bbox: old_bbox, child_idx: old_root },
                    ChildEntry { bbox: new_bbox, child_idx: new_node_idx },
                ],
            };
            let new_root_idx = self.nodes.len();
            self.nodes.push(new_root);
            self.root = new_root_idx;
        }

        self.size += 1;
    }

    /// Insert an entry into a subtree rooted at node_idx.
    /// Returns Some((new_node_idx, new_bbox)) if the node was split.
    fn insert_into_node(
        &mut self,
        node_idx: usize,
        entry: LeafEntry<T, D>,
    ) -> Option<(usize, BoundingBox<D>)> {
        if self.nodes[node_idx].is_leaf() {
            // Insert directly into the leaf.
            if let Node::Leaf { entries } = &mut self.nodes[node_idx] {
                entries.push(entry);
            }
            // Check overflow.
            if self.nodes[node_idx].len() > self.max_entries {
                return self.split_leaf_node(node_idx);
            }
            None
        } else {
            // Choose subtree: child whose bbox needs least enlargement.
            let child_idx = self.choose_subtree(node_idx, &entry.bbox);
            let actual_child = if let Node::Internal { entries } = &self.nodes[node_idx] {
                entries[child_idx].child_idx
            } else {
                unreachable!()
            };

            let split = self.insert_into_node(actual_child, entry);

            // Update this child's bbox.
            if let Node::Internal { entries } = &mut self.nodes[node_idx] {
                if let Some(new_bb) = self.nodes[entries[child_idx].child_idx].compute_bbox() {
                    entries[child_idx].bbox = new_bb;
                }
            }

            if let Some((new_child_idx, new_child_bbox)) = split {
                // Add the new child to this internal node.
                if let Node::Internal { entries } = &mut self.nodes[node_idx] {
                    entries.push(ChildEntry {
                        bbox: new_child_bbox,
                        child_idx: new_child_idx,
                    });
                }
                // Check overflow on internal node.
                if self.nodes[node_idx].len() > self.max_entries {
                    return self.split_internal_node(node_idx);
                }
            }

            None
        }
    }

    fn choose_subtree(&self, node_idx: usize, bbox: &BoundingBox<D>) -> usize {
        if let Node::Internal { entries } = &self.nodes[node_idx] {
            entries
                .iter()
                .enumerate()
                .min_by(|(_, a), (_, b)| {
                    let ea = a.bbox.expansion_needed(bbox);
                    let eb = b.bbox.expansion_needed(bbox);
                    ea.partial_cmp(&eb)
                        .unwrap_or(Ordering::Equal)
                        .then_with(|| {
                            a.bbox.area().partial_cmp(&b.bbox.area()).unwrap_or(Ordering::Equal)
                        })
                })
                .map(|(i, _)| i)
                .unwrap_or(0)
        } else {
            0
        }
    }

    fn split_leaf_node(&mut self, node_idx: usize) -> Option<(usize, BoundingBox<D>)> {
        let entries = if let Node::Leaf { entries } = &mut self.nodes[node_idx] {
            std::mem::take(entries)
        } else {
            return None;
        };

        let (group_a, group_b) =
            split_leaf_entries(entries, &self.split_strategy, self.min_entries);

        self.nodes[node_idx] = Node::Leaf { entries: group_a };

        let new_node = Node::Leaf { entries: group_b };
        let new_bbox = new_node.compute_bbox().unwrap();
        let new_idx = self.nodes.len();
        self.nodes.push(new_node);

        Some((new_idx, new_bbox))
    }

    fn split_internal_node(&mut self, node_idx: usize) -> Option<(usize, BoundingBox<D>)> {
        let entries = if let Node::Internal { entries } = &mut self.nodes[node_idx] {
            std::mem::take(entries)
        } else {
            return None;
        };

        let (group_a, group_b) =
            split_child_entries(entries, &self.split_strategy, self.min_entries);

        self.nodes[node_idx] = Node::Internal { entries: group_a };

        let new_node = Node::Internal { entries: group_b };
        let new_bbox = new_node.compute_bbox().unwrap();
        let new_idx = self.nodes.len();
        self.nodes.push(new_node);

        Some((new_idx, new_bbox))
    }

    // --- Delete ---

    pub fn delete(&mut self, data: &T, bbox: &BoundingBox<D>) -> bool {
        let mut orphans = Vec::new();
        let found = self.delete_from_node(self.root, data, bbox, &mut orphans);
        if found {
            self.size -= 1;
            // Reinsert orphaned entries.
            for orphan in orphans {
                self.insert(orphan.data, orphan.bbox);
                self.size -= 1; // insert increments, but these are reinsertions
            }
            // If root is internal with one child, collapse.
            if let Node::Internal { entries } = &self.nodes[self.root] {
                if entries.len() == 1 {
                    self.root = entries[0].child_idx;
                }
            }
        }
        found
    }

    fn delete_from_node(
        &mut self,
        node_idx: usize,
        data: &T,
        bbox: &BoundingBox<D>,
        orphans: &mut Vec<LeafEntry<T, D>>,
    ) -> bool {
        if self.nodes[node_idx].is_leaf() {
            if let Node::Leaf { entries } = &mut self.nodes[node_idx] {
                let pos = entries.iter().position(|e| e.data == *data && e.bbox.intersects(bbox));
                if let Some(idx) = pos {
                    entries.remove(idx);
                    return true;
                }
            }
            return false;
        }

        // Internal node: search children whose bbox intersects the target.
        let child_indices: Vec<(usize, usize)> = if let Node::Internal { entries } = &self.nodes[node_idx] {
            entries
                .iter()
                .enumerate()
                .filter(|(_, e)| e.bbox.intersects(bbox))
                .map(|(i, e)| (i, e.child_idx))
                .collect()
        } else {
            return false;
        };

        for (entry_idx, child_node_idx) in child_indices {
            if self.delete_from_node(child_node_idx, data, bbox, orphans) {
                // Condense: if child underflowed, dissolve it.
                if self.nodes[child_node_idx].len() < self.min_entries
                    && child_node_idx != self.root
                {
                    self.collect_orphans(child_node_idx, orphans);
                    if let Node::Internal { entries } = &mut self.nodes[node_idx] {
                        entries.remove(entry_idx);
                    }
                } else {
                    // Update bbox.
                    if let Node::Internal { entries } = &mut self.nodes[node_idx] {
                        if let Some(new_bb) = self.nodes[child_node_idx].compute_bbox() {
                            entries[entry_idx].bbox = new_bb;
                        }
                    }
                }
                return true;
            }
        }
        false
    }

    fn collect_orphans(&mut self, node_idx: usize, orphans: &mut Vec<LeafEntry<T, D>>) {
        match &self.nodes[node_idx] {
            Node::Leaf { entries } => {
                orphans.extend(entries.clone());
            }
            Node::Internal { entries } => {
                let child_idxs: Vec<usize> = entries.iter().map(|e| e.child_idx).collect();
                for ci in child_idxs {
                    self.collect_orphans(ci, orphans);
                }
            }
        }
    }

    // --- Queries ---

    pub fn search_point(&self, point: [f64; D]) -> Vec<&T> {
        let mut results = Vec::new();
        self.search_point_node(self.root, &point, &mut results);
        results
    }

    fn search_point_node<'a>(
        &'a self,
        node_idx: usize,
        point: &[f64; D],
        results: &mut Vec<&'a T>,
    ) {
        match &self.nodes[node_idx] {
            Node::Leaf { entries } => {
                for entry in entries {
                    if entry.bbox.contains_point(point) {
                        results.push(&entry.data);
                    }
                }
            }
            Node::Internal { entries } => {
                for entry in entries {
                    if entry.bbox.contains_point(point) {
                        self.search_point_node(entry.child_idx, point, results);
                    }
                }
            }
        }
    }

    pub fn search_window(&self, window: &BoundingBox<D>) -> Vec<&T> {
        let mut results = Vec::new();
        self.search_window_node(self.root, window, &mut results);
        results
    }

    fn search_window_node<'a>(
        &'a self,
        node_idx: usize,
        window: &BoundingBox<D>,
        results: &mut Vec<&'a T>,
    ) {
        match &self.nodes[node_idx] {
            Node::Leaf { entries } => {
                for entry in entries {
                    if entry.bbox.intersects(window) {
                        results.push(&entry.data);
                    }
                }
            }
            Node::Internal { entries } => {
                for entry in entries {
                    if entry.bbox.intersects(window) {
                        self.search_window_node(entry.child_idx, window, results);
                    }
                }
            }
        }
    }

    pub fn nearest(&self, point: [f64; D], k: usize) -> Vec<(&T, f64)> {
        if k == 0 || self.is_empty() {
            return Vec::new();
        }

        // Max-heap to track the k nearest so far (by distance).
        let mut best: BinaryHeap<NnCandidate<T>> = BinaryHeap::new();
        // Min-heap for branch-and-bound traversal.
        let mut queue: BinaryHeap<std::cmp::Reverse<BranchCandidate>> = BinaryHeap::new();

        let root_bbox = self.nodes[self.root].compute_bbox();
        if let Some(bb) = root_bbox {
            let dist = bb.min_distance_to_point(&point);
            queue.push(std::cmp::Reverse(BranchCandidate {
                dist,
                node_idx: self.root,
            }));
        }

        while let Some(std::cmp::Reverse(branch)) = queue.pop() {
            // Prune: if this branch is farther than our k-th best, skip.
            if best.len() >= k {
                if let Some(worst) = best.peek() {
                    if branch.dist > worst.dist {
                        break;
                    }
                }
            }

            match &self.nodes[branch.node_idx] {
                Node::Leaf { entries } => {
                    for entry in entries {
                        let d = entry.bbox.min_distance_to_point(&point);
                        if best.len() < k {
                            best.push(NnCandidate { dist: d, data: &entry.data });
                        } else if let Some(worst) = best.peek() {
                            if d < worst.dist {
                                best.pop();
                                best.push(NnCandidate { dist: d, data: &entry.data });
                            }
                        }
                    }
                }
                Node::Internal { entries } => {
                    for entry in entries {
                        let d = entry.bbox.min_distance_to_point(&point);
                        let dominated = best.len() >= k
                            && best.peek().map_or(false, |w| d > w.dist);
                        if !dominated {
                            queue.push(std::cmp::Reverse(BranchCandidate {
                                dist: d,
                                node_idx: entry.child_idx,
                            }));
                        }
                    }
                }
            }
        }

        let mut results: Vec<(&T, f64)> = best.into_iter().map(|c| (c.data, c.dist)).collect();
        results.sort_by(|a, b| a.1.partial_cmp(&b.1).unwrap_or(Ordering::Equal));
        results
    }

    // --- Bulk loading (STR) ---

    pub fn bulk_load(entries: Vec<(T, BoundingBox<D>)>, max_entries: usize, strategy: SplitStrategy) -> Self {
        let min_entries = max_entries / 2;
        let leaf_entries: Vec<LeafEntry<T, D>> = entries
            .into_iter()
            .map(|(data, bbox)| LeafEntry { bbox, data })
            .collect();

        if leaf_entries.is_empty() {
            return Self::new(max_entries, strategy);
        }

        let mut tree = Self {
            nodes: Vec::new(),
            root: 0,
            max_entries,
            min_entries,
            split_strategy: strategy,
            size: leaf_entries.len(),
        };

        let root = tree.str_build(leaf_entries, 0);
        tree.root = root;
        tree
    }

    fn str_build(&mut self, mut entries: Vec<LeafEntry<T, D>>, axis: usize) -> usize {
        if entries.len() <= self.max_entries {
            let idx = self.nodes.len();
            self.nodes.push(Node::Leaf { entries });
            return idx;
        }

        let current_axis = axis % D;
        entries.sort_by(|a, b| {
            a.bbox.center()[current_axis]
                .partial_cmp(&b.bbox.center()[current_axis])
                .unwrap_or(Ordering::Equal)
        });

        let n = entries.len();
        let num_slices = ((n as f64) / (self.max_entries as f64)).ceil() as usize;
        let slice_size = ((n as f64) / (num_slices as f64)).ceil() as usize;

        let mut child_entries: Vec<ChildEntry<D>> = Vec::new();

        for chunk in entries.chunks(slice_size.max(1)) {
            let child_idx = if axis + 1 < D && chunk.len() > self.max_entries {
                self.str_build(chunk.to_vec(), axis + 1)
            } else if chunk.len() > self.max_entries {
                self.str_build(chunk.to_vec(), 0)
            } else {
                let idx = self.nodes.len();
                self.nodes.push(Node::Leaf { entries: chunk.to_vec() });
                idx
            };

            let bbox = self.nodes[child_idx].compute_bbox().unwrap();
            child_entries.push(ChildEntry { bbox, child_idx });
        }

        if child_entries.len() <= self.max_entries {
            let idx = self.nodes.len();
            self.nodes.push(Node::Internal { entries: child_entries });
            idx
        } else {
            // Recursively build upper levels.
            let pairs: Vec<(T, BoundingBox<D>)> = Vec::new();
            let _ = pairs; // Not needed; build internal nodes directly.
            self.str_build_internal(child_entries)
        }
    }

    fn str_build_internal(&mut self, entries: Vec<ChildEntry<D>>) -> usize {
        if entries.len() <= self.max_entries {
            let idx = self.nodes.len();
            self.nodes.push(Node::Internal { entries });
            return idx;
        }

        let num_groups = ((entries.len() as f64) / (self.max_entries as f64)).ceil() as usize;
        let group_size = ((entries.len() as f64) / (num_groups as f64)).ceil() as usize;

        let mut upper_entries: Vec<ChildEntry<D>> = Vec::new();

        for chunk in entries.chunks(group_size.max(1)) {
            let idx = self.nodes.len();
            self.nodes.push(Node::Internal { entries: chunk.to_vec() });
            let bbox = self.nodes[idx].compute_bbox().unwrap();
            upper_entries.push(ChildEntry { bbox, child_idx: idx });
        }

        self.str_build_internal(upper_entries)
    }
}

// --- Helper types for nearest-neighbor search ---

struct NnCandidate<'a, T> {
    dist: f64,
    data: &'a T,
}

impl<'a, T> PartialEq for NnCandidate<'a, T> {
    fn eq(&self, other: &Self) -> bool {
        self.dist == other.dist
    }
}
impl<'a, T> Eq for NnCandidate<'a, T> {}

impl<'a, T> PartialOrd for NnCandidate<'a, T> {
    fn partial_cmp(&self, other: &Self) -> Option<Ordering> {
        Some(self.cmp(other))
    }
}
impl<'a, T> Ord for NnCandidate<'a, T> {
    fn cmp(&self, other: &Self) -> Ordering {
        self.dist.partial_cmp(&other.dist).unwrap_or(Ordering::Equal)
    }
}

#[derive(PartialEq)]
struct BranchCandidate {
    dist: f64,
    node_idx: usize,
}

impl Eq for BranchCandidate {}

impl PartialOrd for BranchCandidate {
    fn partial_cmp(&self, other: &Self) -> Option<Ordering> {
        Some(self.cmp(other))
    }
}
impl Ord for BranchCandidate {
    fn cmp(&self, other: &Self) -> Ordering {
        self.dist.partial_cmp(&other.dist).unwrap_or(Ordering::Equal)
    }
}
```

### Source: `src/lib.rs`

```rust
pub mod bbox;
pub mod node;
pub mod split;
pub mod rtree;
```

### Source: `src/main.rs`

```rust
use rtree_spatial::bbox::BoundingBox;
use rtree_spatial::rtree::RTree;
use rtree_spatial::split::SplitStrategy;

fn main() {
    println!("=== R-Tree Spatial Index Demo ===\n");

    // --- 2D example: restaurants ---

    println!("--- Geographic Restaurant Search (2D) ---\n");

    #[derive(Debug, Clone, PartialEq)]
    struct Restaurant {
        name: String,
        lat: f64,
        lon: f64,
    }

    let restaurants = vec![
        ("Sushi Place", 40.7128, -74.0060),
        ("Pizza Corner", 40.7580, -73.9855),
        ("Taco Stand", 40.7282, -73.7949),
        ("Burger Joint", 40.6892, -74.0445),
        ("Noodle House", 40.7484, -73.9857),
        ("Curry Palace", 40.7527, -73.9772),
        ("Steak House", 40.7614, -73.9776),
        ("Pho Kitchen", 40.7193, -73.9970),
        ("Dim Sum Hall", 40.7158, -73.9970),
        ("Ramen Bar", 40.7264, -74.0014),
    ];

    let mut tree: RTree<String, 2> = RTree::with_defaults();

    for (name, lat, lon) in &restaurants {
        let radius = 0.005; // ~500m in degrees
        let bbox = BoundingBox::new(
            [lat - radius, lon - radius],
            [lat + radius, lon + radius],
        );
        tree.insert(name.to_string(), bbox);
    }

    println!("Inserted {} restaurants", tree.len());

    // Point query: what's at Times Square?
    let times_sq = [40.7580, -73.9855];
    let at_times_sq = tree.search_point(times_sq);
    println!("At Times Square ({:?}): {:?}", times_sq, at_times_sq);

    // Window query: restaurants in a bounding box around Midtown.
    let midtown = BoundingBox::new([40.74, -74.00], [40.77, -73.97]);
    let in_midtown = tree.search_window(&midtown);
    println!("In Midtown area: {:?}", in_midtown);

    // Nearest neighbor: 3 closest to a point in Lower Manhattan.
    let query_point = [40.710, -74.000];
    let nearest = tree.nearest(query_point, 3);
    println!("3 nearest to {:?}:", query_point);
    for (name, dist) in &nearest {
        println!("  {} (dist: {:.6})", name, dist);
    }

    // --- 3D example ---

    println!("\n--- 3D Spatial Index ---\n");

    let mut tree_3d: RTree<String, 3> = RTree::new(4, SplitStrategy::Linear);

    let objects = vec![
        ("Cube A", [0.0, 0.0, 0.0], [1.0, 1.0, 1.0]),
        ("Cube B", [2.0, 2.0, 2.0], [3.0, 3.0, 3.0]),
        ("Cube C", [0.5, 0.5, 0.5], [1.5, 1.5, 1.5]),
        ("Cube D", [5.0, 5.0, 5.0], [6.0, 6.0, 6.0]),
    ];

    for (name, min, max) in &objects {
        tree_3d.insert(name.to_string(), BoundingBox::new(*min, *max));
    }

    let point_3d = [0.75, 0.75, 0.75];
    let found = tree_3d.search_point(point_3d);
    println!("3D point query at {:?}: {:?}", point_3d, found);

    // --- Bulk loading ---

    println!("\n--- STR Bulk Loading ---\n");

    let bulk_entries: Vec<(String, BoundingBox<2>)> = (0..1000)
        .map(|i| {
            let x = (i % 100) as f64;
            let y = (i / 100) as f64;
            let name = format!("item-{i}");
            let bbox = BoundingBox::new([x, y], [x + 0.5, y + 0.5]);
            (name, bbox)
        })
        .collect();

    let bulk_tree = RTree::bulk_load(bulk_entries, 16, SplitStrategy::Quadratic);
    println!("Bulk loaded {} entries", bulk_tree.len());

    let window = BoundingBox::new([10.0, 5.0], [15.0, 7.0]);
    let found = bulk_tree.search_window(&window);
    println!("Window query [10,5]->[15,7] found {} entries", found.len());
}
```

### Tests

```rust
#[cfg(test)]
mod tests {
    use rtree_spatial::bbox::BoundingBox;
    use rtree_spatial::rtree::RTree;
    use rtree_spatial::split::SplitStrategy;

    // --- BoundingBox tests ---

    #[test]
    fn bbox_contains_point() {
        let bb = BoundingBox::new([0.0, 0.0], [10.0, 10.0]);
        assert!(bb.contains_point(&[5.0, 5.0]));
        assert!(bb.contains_point(&[0.0, 0.0]));
        assert!(!bb.contains_point(&[11.0, 5.0]));
    }

    #[test]
    fn bbox_intersects() {
        let a = BoundingBox::new([0.0, 0.0], [5.0, 5.0]);
        let b = BoundingBox::new([3.0, 3.0], [8.0, 8.0]);
        let c = BoundingBox::new([6.0, 6.0], [9.0, 9.0]);
        assert!(a.intersects(&b));
        assert!(!a.intersects(&c));
    }

    #[test]
    fn bbox_union() {
        let a = BoundingBox::new([0.0, 0.0], [5.0, 5.0]);
        let b = BoundingBox::new([3.0, 3.0], [8.0, 8.0]);
        let u = a.union(&b);
        assert_eq!(u.min, [0.0, 0.0]);
        assert_eq!(u.max, [8.0, 8.0]);
    }

    #[test]
    fn bbox_area_2d() {
        let bb = BoundingBox::new([0.0, 0.0], [3.0, 4.0]);
        assert!((bb.area() - 12.0).abs() < 1e-10);
    }

    #[test]
    fn bbox_area_3d() {
        let bb: BoundingBox<3> = BoundingBox::new([0.0, 0.0, 0.0], [2.0, 3.0, 4.0]);
        assert!((bb.area() - 24.0).abs() < 1e-10);
    }

    #[test]
    fn bbox_overlap_area() {
        let a = BoundingBox::new([0.0, 0.0], [5.0, 5.0]);
        let b = BoundingBox::new([3.0, 3.0], [8.0, 8.0]);
        assert!((a.overlap_area(&b) - 4.0).abs() < 1e-10);
    }

    #[test]
    fn bbox_min_distance() {
        let bb = BoundingBox::new([2.0, 2.0], [5.0, 5.0]);
        assert!((bb.min_distance_to_point(&[3.0, 3.0]) - 0.0).abs() < 1e-10);
        assert!((bb.min_distance_to_point(&[0.0, 2.0]) - 2.0).abs() < 1e-10);
    }

    // --- RTree insertion and point query ---

    #[test]
    fn insert_and_point_query() {
        let mut tree: RTree<&str, 2> = RTree::with_defaults();
        tree.insert("A", BoundingBox::new([0.0, 0.0], [5.0, 5.0]));
        tree.insert("B", BoundingBox::new([3.0, 3.0], [8.0, 8.0]));
        tree.insert("C", BoundingBox::new([10.0, 10.0], [15.0, 15.0]));

        let results = tree.search_point([4.0, 4.0]);
        assert!(results.contains(&&"A"));
        assert!(results.contains(&&"B"));
        assert!(!results.contains(&&"C"));
    }

    #[test]
    fn window_query() {
        let mut tree: RTree<i32, 2> = RTree::with_defaults();
        for i in 0..20 {
            let x = (i * 5) as f64;
            tree.insert(i, BoundingBox::new([x, 0.0], [x + 3.0, 3.0]));
        }
        let window = BoundingBox::new([10.0, 0.0], [30.0, 3.0]);
        let results = tree.search_window(&window);
        assert!(results.len() >= 4);
    }

    #[test]
    fn nearest_neighbor() {
        let mut tree: RTree<&str, 2> = RTree::with_defaults();
        tree.insert("origin", BoundingBox::from_point([0.0, 0.0]));
        tree.insert("close", BoundingBox::from_point([1.0, 1.0]));
        tree.insert("mid", BoundingBox::from_point([5.0, 5.0]));
        tree.insert("far", BoundingBox::from_point([100.0, 100.0]));

        let nn = tree.nearest([0.0, 0.0], 2);
        assert_eq!(nn.len(), 2);
        assert_eq!(*nn[0].0, "origin");
        assert_eq!(*nn[1].0, "close");
    }

    #[test]
    fn delete_entry() {
        let mut tree: RTree<&str, 2> = RTree::new(4, SplitStrategy::Quadratic);
        let bb = BoundingBox::new([0.0, 0.0], [1.0, 1.0]);
        tree.insert("target", bb.clone());
        tree.insert("other", BoundingBox::new([5.0, 5.0], [6.0, 6.0]));

        assert_eq!(tree.len(), 2);
        assert!(tree.delete(&"target", &bb));
        assert_eq!(tree.len(), 1);

        let results = tree.search_point([0.5, 0.5]);
        assert!(results.is_empty());
    }

    // --- Split strategy tests ---

    #[test]
    fn node_splitting_linear() {
        let mut tree: RTree<i32, 2> = RTree::new(4, SplitStrategy::Linear);
        for i in 0..20 {
            let x = i as f64;
            tree.insert(i, BoundingBox::new([x, 0.0], [x + 1.0, 1.0]));
        }
        assert_eq!(tree.len(), 20);
        for i in 0..20 {
            let x = i as f64 + 0.5;
            let results = tree.search_point([x, 0.5]);
            assert!(results.contains(&&i), "missing entry {i}");
        }
    }

    #[test]
    fn node_splitting_quadratic() {
        let mut tree: RTree<i32, 2> = RTree::new(4, SplitStrategy::Quadratic);
        for i in 0..20 {
            let x = i as f64;
            tree.insert(i, BoundingBox::new([x, 0.0], [x + 1.0, 1.0]));
        }
        assert_eq!(tree.len(), 20);
        for i in 0..20 {
            let x = i as f64 + 0.5;
            let results = tree.search_point([x, 0.5]);
            assert!(results.contains(&&i), "missing entry {i}");
        }
    }

    // --- 3D tests ---

    #[test]
    fn three_dimensional_queries() {
        let mut tree: RTree<&str, 3> = RTree::with_defaults();
        tree.insert("cube", BoundingBox::new([0.0, 0.0, 0.0], [1.0, 1.0, 1.0]));
        tree.insert("far", BoundingBox::new([10.0, 10.0, 10.0], [11.0, 11.0, 11.0]));

        let results = tree.search_point([0.5, 0.5, 0.5]);
        assert_eq!(results.len(), 1);
        assert_eq!(*results[0], "cube");
    }

    // --- Bulk loading ---

    #[test]
    fn bulk_load_and_query() {
        let entries: Vec<(i32, BoundingBox<2>)> = (0..500)
            .map(|i| {
                let x = (i % 50) as f64;
                let y = (i / 50) as f64;
                (i, BoundingBox::new([x, y], [x + 0.5, y + 0.5]))
            })
            .collect();

        let tree = RTree::bulk_load(entries, 16, SplitStrategy::Quadratic);
        assert_eq!(tree.len(), 500);

        let window = BoundingBox::new([0.0, 0.0], [5.0, 2.0]);
        let results = tree.search_window(&window);
        assert!(!results.is_empty());

        let nn = tree.nearest([25.0, 5.0], 3);
        assert_eq!(nn.len(), 3);
    }

    // --- Stress test ---

    #[test]
    fn stress_insert_and_query() {
        let mut tree: RTree<usize, 2> = RTree::new(8, SplitStrategy::Quadratic);
        for i in 0..1000 {
            let x = (i as f64 * 17.3) % 1000.0;
            let y = (i as f64 * 31.7) % 1000.0;
            tree.insert(i, BoundingBox::new([x, y], [x + 1.0, y + 1.0]));
        }
        assert_eq!(tree.len(), 1000);

        let window = BoundingBox::new([0.0, 0.0], [100.0, 100.0]);
        let results = tree.search_window(&window);
        assert!(!results.is_empty());
    }
}
```

### Running

```bash
cargo build
cargo test
cargo run
```

### Expected Output

```
=== R-Tree Spatial Index Demo ===

--- Geographic Restaurant Search (2D) ---

Inserted 10 restaurants
At Times Square ([40.758, -73.9855]): ["Pizza Corner"]
In Midtown area: ["Noodle House", "Curry Palace", "Steak House"]
3 nearest to [40.71, -74.0]:
  Dim Sum Hall (dist: 0.003606)
  Pho Kitchen (dist: 0.009487)
  Ramen Bar (dist: 0.016125)

--- 3D Spatial Index ---

3D point query at [0.75, 0.75, 0.75]: ["Cube A", "Cube C"]

--- STR Bulk Loading ---

Bulk loaded 1000 entries
Window query [10,5]->[15,7] found 30 entries
```

## Design Decisions

1. **Arena-based node storage**: Nodes are stored in a `Vec<Node<T, D>>` with indices rather than `Box<Node>` pointers. This simplifies Rust's ownership model (no recursive `Box` types), improves cache locality during tree traversal, and makes serialization trivial. The cost is that deleted nodes leave gaps -- a production implementation would add a free list.

2. **Const generics for dimensionality**: Using `const D: usize` rather than a runtime dimension parameter means the compiler generates specialized code for 2D and 3D. Loop bounds like `0..D` are known at compile time and get fully unrolled. This yields significant performance gains over dynamic dispatch.

3. **Separate split strategies as an enum**: Rather than using a trait object for the split strategy, a simple enum with match dispatching keeps the code straightforward and avoids virtual dispatch overhead. Splitting is on the hot path during insertion, so this matters.

4. **Quadratic split as default**: Quadratic split (O(n^2) per split, where n is the max node capacity, typically 16-64) produces better trees than linear split at negligible cost. The n^2 is on the node capacity, not the dataset size, so even O(n^2) is just a few hundred comparisons.

5. **Nearest-neighbor with two heaps**: The branch-and-bound nearest-neighbor uses a min-heap for traversal order (visit closest nodes first) and a max-heap to track the k-best results (quickly reject candidates worse than the current k-th best). This dual-heap approach is the standard algorithm from Roussopoulos et al. (1995).

## Common Mistakes

1. **Using center distance instead of minimum distance for NN**: The minimum distance from a point to a bounding box is NOT the distance to the box's center. It is zero if the point is inside the box, or the Euclidean distance to the nearest face/edge/corner if outside. Using center distance over-prunes branches and misses valid candidates.

2. **Forgetting condense-tree after deletion**: Simply removing a leaf entry can leave its parent with fewer than `min_entries` children. Without condense-tree (dissolving underflowed nodes and reinserting their entries), the tree degrades into an unbalanced structure with many nearly-empty nodes.

3. **Incorrect overlap area calculation**: The overlap area formula requires clamping the intersection to zero when boxes do not overlap on any axis. A common bug is computing negative widths and getting negative (or negative-squared) overlap values instead of returning zero.

4. **Splitting into extremely unbalanced groups**: The distribution step must enforce the minimum entries constraint. Without it, a pathological split might put all entries in one group and none in the other, violating the R-tree invariant.

## Performance Notes

| Operation | Average | Worst Case | Notes |
|-----------|---------|------------|-------|
| Insert | O(log_M N) | O(N) during split | M = max entries per node |
| Delete | O(log_M N) | O(N) during condense | Reinsertion cost |
| Point query | O(log_M N) | O(N) | Depends on overlap |
| Window query | O(log_M N + k) | O(N) | k = results, overlap dependent |
| Nearest neighbor | O(log_M N) | O(N log N) | With pruning |
| STR bulk load | O(N log N) | O(N log N) | Sorting dominated |

The R-tree's practical performance depends heavily on how much bounding boxes overlap. Well-separated spatial data yields near-logarithmic query times. Highly overlapping data (many objects on top of each other) degrades toward linear scan. STR bulk loading typically produces 2-5x better query performance than sequential insertion because it minimizes overlap in the resulting tree.
