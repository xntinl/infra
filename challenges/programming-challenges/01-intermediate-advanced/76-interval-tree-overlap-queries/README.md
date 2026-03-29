# 76. Interval Tree Overlap Queries

```yaml
difficulty: intermediate-advanced
languages: [rust]
time_estimate: 5-7 hours
tags: [interval-tree, augmented-bst, range-queries, scheduling, overlap-detection, iterators]
bloom_level: [apply, analyze, create]
```

## Prerequisites

- Binary search trees: insertion, deletion, rotations, in-order traversal
- Augmented data structures: storing derived information at each node
- Interval arithmetic: overlap conditions, containment, merging
- Rust ownership and borrowing: tree structures with `Box`, `Option`, references
- Iterator trait implementation in Rust

## Learning Objectives

After completing this challenge you will be able to:

- **Implement** an augmented BST where each node stores a max-endpoint for its subtree
- **Query** efficiently for all intervals overlapping a point or a range
- **Maintain** the augmentation invariant during insertions and deletions
- **Design** an in-order iterator over intervals using an explicit stack
- **Apply** interval trees to real-world scheduling and calendar overlap detection

## The Challenge

Calendar applications, resource schedulers, and genomic range databases all need to answer the question: "Which intervals overlap with this point or range?" A brute-force scan is O(n) per query. An interval tree -- a BST augmented with subtree max-endpoints -- answers overlap queries in O(log n + k) where k is the number of overlapping intervals.

Build an interval tree in Rust. Each node stores an interval `[low, high]` and a `max` value representing the highest endpoint in its subtree. Support insertion, deletion, point overlap queries, range overlap queries, merging of overlapping intervals, and an in-order iterator. Apply it to a scheduling scenario where you detect meeting conflicts.

## Requirements

1. Define an `Interval` type with `low` and `high` fields (both inclusive). Implement ordering by `low` endpoint (ties broken by `high`). Define an `IntervalTree` backed by an augmented BST where each node stores an interval and the maximum `high` value in its subtree.

2. Implement `insert(interval)` that inserts into the BST ordered by `low` endpoint and updates `max` values along the insertion path. Every ancestor's `max` must reflect the new interval if it extends the subtree maximum.

3. Implement `delete(interval)` that removes the interval from the tree. Handle all BST deletion cases (leaf, one child, two children with in-order successor). Update `max` values along the path after removal.

4. Implement `query_point(point) -> Vec<Interval>` that finds all intervals containing the given point. An interval `[low, high]` contains point `p` if `low <= p <= high`. Use the `max` augmentation to prune subtrees: if a subtree's `max < point`, skip it entirely.

5. Implement `query_range(low, high) -> Vec<Interval>` that finds all intervals overlapping with the query range. Two intervals overlap if `a.low <= b.high && b.low <= a.high`. Again, prune using `max` values.

6. Implement `merge_overlapping() -> Vec<Interval>` that returns a new list of intervals where all overlapping intervals have been merged. Sort by `low`, then sweep and merge. This does not modify the tree.

7. Implement `IntoIterator` and a custom `IntervalTreeIter` that yields intervals in sorted order (by `low` endpoint). Use an explicit stack for the in-order traversal to avoid recursive lifetime issues.

8. Rust only. Use idiomatic Rust patterns: `Option<Box<Node>>` for child pointers, proper ownership, no `unsafe` code.

## Hints

<details>
<summary>Hint 1: Node structure with augmentation</summary>

Each node stores the interval, the subtree max, and optional left/right children. The `max` is always the maximum of the node's own `high`, left child's `max`, and right child's `max`.

```rust
#[derive(Debug, Clone, PartialEq)]
pub struct Interval {
    pub low: i64,
    pub high: i64,
}

struct Node {
    interval: Interval,
    max: i64,
    left: Option<Box<Node>>,
    right: Option<Box<Node>>,
}

impl Node {
    fn update_max(&mut self) {
        self.max = self.interval.high;
        if let Some(ref left) = self.left {
            self.max = self.max.max(left.max);
        }
        if let Some(ref right) = self.right {
            self.max = self.max.max(right.max);
        }
    }
}
```

</details>

<details>
<summary>Hint 2: Pruning overlap queries</summary>

When querying for a point, at each node: (1) check if the current interval contains the point, (2) if the left child exists and `left.max >= point`, recurse left, (3) otherwise recurse right. For range queries, check both subtrees but prune when `max < query.low`.

```rust
fn query_point_recursive(node: &Option<Box<Node>>, point: i64, results: &mut Vec<Interval>) {
    let Some(n) = node else { return };
    if n.interval.low <= point && point <= n.interval.high {
        results.push(n.interval.clone());
    }
    if let Some(ref left) = n.left {
        if left.max >= point {
            query_point_recursive(&n.left, point, results);
        }
    }
    query_point_recursive(&n.right, point, results);
}
```

</details>

<details>
<summary>Hint 3: BST deletion with successor</summary>

When deleting a node with two children, find the in-order successor (smallest node in right subtree), copy its interval to the current node, then delete the successor from the right subtree. Update `max` values bottom-up after the deletion.

</details>

<details>
<summary>Hint 4: In-order iterator with explicit stack</summary>

Push all left children onto a stack. On each `next()`, pop the top, yield its interval, then push all left children of its right child. This avoids recursion and works well with Rust's ownership model.

```rust
pub struct IntervalTreeIter {
    stack: Vec<(Interval, Option<Box<Node>>)>,
}
```

</details>

<details>
<summary>Hint 5: Merge overlapping intervals</summary>

Collect all intervals in sorted order (use the iterator). Initialize a merged list with the first interval. For each subsequent interval, if it overlaps with the last merged interval, extend the last merged interval's `high`. Otherwise, push a new interval.

</details>

## Acceptance Criteria

- [ ] Insert maintains BST ordering by `low` endpoint and correct `max` augmentation
- [ ] Point queries return all and only intervals containing the query point
- [ ] Range queries return all and only intervals overlapping the query range
- [ ] Queries skip subtrees where `max` is below the query (pruning works)
- [ ] Delete correctly handles leaf, one-child, and two-child cases
- [ ] Delete updates `max` values along the entire path to root
- [ ] Merge overlapping intervals produces minimal non-overlapping intervals
- [ ] Iterator yields intervals in sorted order by `low` endpoint
- [ ] Empty tree queries return empty results without panicking
- [ ] Single-interval tree operations work correctly
- [ ] Scheduling scenario: detect all conflicting meetings in a calendar

## Resources

- [Interval Tree - Wikipedia](https://en.wikipedia.org/wiki/Interval_tree)
- [CLRS Chapter 14: Augmenting Data Structures](https://mitpress.mit.edu/books/introduction-to-algorithms-fourth-edition) - Interval trees as augmented BSTs
- [Augmented Search Trees](https://en.wikipedia.org/wiki/Augmented_data_structure)
- [The Rust Programming Language: Smart Pointers](https://doc.rust-lang.org/book/ch15-00-smart-pointers.html) - Box, Option patterns for trees
- [Implementing Iterators in Rust](https://doc.rust-lang.org/book/ch13-02-iterators.html)
- [Interval Scheduling Problem](https://en.wikipedia.org/wiki/Interval_scheduling) - Real-world motivation
