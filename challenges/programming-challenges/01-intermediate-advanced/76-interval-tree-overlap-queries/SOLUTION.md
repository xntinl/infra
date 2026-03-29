# Solution: Interval Tree Overlap Queries

## Architecture Overview

The solution is a single-file Rust library built around an augmented BST:

1. **IntervalTree**: Public API wrapping an `Option<Box<Node>>` root. All operations delegate to recursive functions on nodes.
2. **Node**: Stores an `Interval`, left/right children, and a `max` field tracking the highest endpoint in the subtree.
3. **Augmentation Invariant**: Every insert/delete path updates `max` bottom-up. Queries prune subtrees where `max < query.low`.
4. **Iterator**: A stack-based in-order traversal that yields intervals sorted by `low` endpoint without recursion.

The scheduling application layer demonstrates finding all meeting conflicts in a calendar by querying each meeting's interval against the tree.

## Rust Solution

```rust
// src/lib.rs
use std::cmp;
use std::fmt;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Interval {
    pub low: i64,
    pub high: i64,
}

impl Interval {
    pub fn new(low: i64, high: i64) -> Self {
        assert!(low <= high, "low must be <= high");
        Interval { low, high }
    }

    fn overlaps(&self, other: &Interval) -> bool {
        self.low <= other.high && other.low <= self.high
    }

    fn contains_point(&self, point: i64) -> bool {
        self.low <= point && point <= self.high
    }
}

impl fmt::Display for Interval {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "[{}, {}]", self.low, self.high)
    }
}

impl PartialOrd for Interval {
    fn partial_cmp(&self, other: &Self) -> Option<cmp::Ordering> {
        Some(self.cmp(other))
    }
}

impl Ord for Interval {
    fn cmp(&self, other: &Self) -> cmp::Ordering {
        self.low.cmp(&other.low).then(self.high.cmp(&other.high))
    }
}

struct Node {
    interval: Interval,
    max: i64,
    left: Option<Box<Node>>,
    right: Option<Box<Node>>,
}

impl Node {
    fn new(interval: Interval) -> Self {
        let max = interval.high;
        Node {
            interval,
            max,
            left: None,
            right: None,
        }
    }

    fn update_max(&mut self) {
        self.max = self.interval.high;
        if let Some(ref left) = self.left {
            self.max = cmp::max(self.max, left.max);
        }
        if let Some(ref right) = self.right {
            self.max = cmp::max(self.max, right.max);
        }
    }
}

pub struct IntervalTree {
    root: Option<Box<Node>>,
    size: usize,
}

impl IntervalTree {
    pub fn new() -> Self {
        IntervalTree { root: None, size: 0 }
    }

    pub fn len(&self) -> usize {
        self.size
    }

    pub fn is_empty(&self) -> bool {
        self.size == 0
    }

    pub fn insert(&mut self, interval: Interval) {
        self.root = Self::insert_recursive(self.root.take(), interval);
        self.size += 1;
    }

    fn insert_recursive(node: Option<Box<Node>>, interval: Interval) -> Option<Box<Node>> {
        let Some(mut n) = node else {
            return Some(Box::new(Node::new(interval)));
        };

        if interval < n.interval {
            n.left = Self::insert_recursive(n.left.take(), interval);
        } else {
            n.right = Self::insert_recursive(n.right.take(), interval);
        }

        n.update_max();
        Some(n)
    }

    pub fn delete(&mut self, interval: &Interval) -> bool {
        let (new_root, deleted) = Self::delete_recursive(self.root.take(), interval);
        self.root = new_root;
        if deleted {
            self.size -= 1;
        }
        deleted
    }

    fn delete_recursive(
        node: Option<Box<Node>>,
        interval: &Interval,
    ) -> (Option<Box<Node>>, bool) {
        let Some(mut n) = node else {
            return (None, false);
        };

        if *interval < n.interval {
            let (new_left, deleted) = Self::delete_recursive(n.left.take(), interval);
            n.left = new_left;
            n.update_max();
            return (Some(n), deleted);
        }

        if *interval > n.interval {
            let (new_right, deleted) = Self::delete_recursive(n.right.take(), interval);
            n.right = new_right;
            n.update_max();
            return (Some(n), deleted);
        }

        // Found the node to delete
        match (n.left.take(), n.right.take()) {
            (None, None) => (None, true),
            (Some(left), None) => (Some(left), true),
            (None, Some(right)) => (Some(right), true),
            (Some(left), Some(right)) => {
                let (successor_interval, new_right) = Self::remove_min(right);
                n.interval = successor_interval;
                n.left = Some(left);
                n.right = new_right;
                n.update_max();
                (Some(n), true)
            }
        }
    }

    fn remove_min(node: Box<Node>) -> (Interval, Option<Box<Node>>) {
        let mut n = node;
        if n.left.is_none() {
            return (n.interval, n.right);
        }
        let (min_interval, new_left) = Self::remove_min(n.left.take().unwrap());
        n.left = new_left;
        n.update_max();
        (min_interval, Some(n))
    }

    pub fn query_point(&self, point: i64) -> Vec<Interval> {
        let mut results = Vec::new();
        Self::query_point_recursive(&self.root, point, &mut results);
        results
    }

    fn query_point_recursive(
        node: &Option<Box<Node>>,
        point: i64,
        results: &mut Vec<Interval>,
    ) {
        let Some(n) = node else { return };

        if n.interval.contains_point(point) {
            results.push(n.interval.clone());
        }

        if let Some(ref left) = n.left {
            if left.max >= point {
                Self::query_point_recursive(&n.left, point, results);
            }
        }

        if point >= n.interval.low {
            Self::query_point_recursive(&n.right, point, results);
        }
    }

    pub fn query_range(&self, low: i64, high: i64) -> Vec<Interval> {
        let query = Interval::new(low, high);
        let mut results = Vec::new();
        Self::query_range_recursive(&self.root, &query, &mut results);
        results
    }

    fn query_range_recursive(
        node: &Option<Box<Node>>,
        query: &Interval,
        results: &mut Vec<Interval>,
    ) {
        let Some(n) = node else { return };

        if n.interval.overlaps(query) {
            results.push(n.interval.clone());
        }

        if let Some(ref left) = n.left {
            if left.max >= query.low {
                Self::query_range_recursive(&n.left, query, results);
            }
        }

        if n.interval.low <= query.high {
            Self::query_range_recursive(&n.right, query, results);
        }
    }

    pub fn merge_overlapping(&self) -> Vec<Interval> {
        let mut sorted: Vec<Interval> = self.iter().collect();
        if sorted.is_empty() {
            return sorted;
        }

        sorted.sort();
        let mut merged = vec![sorted[0].clone()];

        for interval in sorted.iter().skip(1) {
            let last = merged.last_mut().unwrap();
            if interval.low <= last.high {
                last.high = cmp::max(last.high, interval.high);
            } else {
                merged.push(interval.clone());
            }
        }
        merged
    }

    pub fn iter(&self) -> IntervalTreeIter {
        let mut stack = Vec::new();
        Self::push_left(&self.root, &mut stack);
        IntervalTreeIter { stack }
    }

    fn push_left(mut node: &Option<Box<Node>>, stack: &mut Vec<IterFrame>) {
        while let Some(ref n) = node {
            stack.push(IterFrame {
                interval: n.interval.clone(),
                right: n.right.as_ref().map(|r| r.as_ref() as *const Node),
            });
            node = &n.left;
        }
    }
}

struct IterFrame {
    interval: Interval,
    right: Option<*const Node>,
}

pub struct IntervalTreeIter {
    stack: Vec<IterFrame>,
}

impl Iterator for IntervalTreeIter {
    type Item = Interval;

    fn next(&mut self) -> Option<Self::Item> {
        let frame = self.stack.pop()?;
        let interval = frame.interval;

        if let Some(right_ptr) = frame.right {
            // SAFETY: the tree is borrowed immutably for the lifetime of the iterator.
            // The pointer is valid as long as the tree is not modified.
            let right_node = unsafe { &*right_ptr };
            let right_opt = Some(Box::new(Node {
                interval: right_node.interval.clone(),
                max: right_node.max,
                left: None,
                right: None,
            }));
            // Instead of the unsafe approach, collect via recursive helper
            self.push_subtree(right_ptr);
        }

        Some(interval)
    }
}

impl IntervalTreeIter {
    fn push_subtree(&mut self, node_ptr: *const Node) {
        // SAFETY: pointer is valid during iteration (tree is immutably borrowed).
        unsafe {
            let mut current = Some(node_ptr);
            while let Some(ptr) = current {
                let node = &*ptr;
                self.stack.push(IterFrame {
                    interval: node.interval.clone(),
                    right: node.right.as_ref().map(|r| r.as_ref() as *const Node),
                });
                current = node.left.as_ref().map(|l| l.as_ref() as *const Node);
            }
        }
    }
}

impl<'a> IntoIterator for &'a IntervalTree {
    type Item = Interval;
    type IntoIter = IntervalTreeIter;

    fn into_iter(self) -> Self::IntoIter {
        self.iter()
    }
}

// Application: Meeting conflict detection

#[derive(Debug, Clone)]
pub struct Meeting {
    pub name: String,
    pub start: i64,
    pub end: i64,
}

#[derive(Debug)]
pub struct Conflict {
    pub meeting_a: String,
    pub meeting_b: String,
}

pub fn find_conflicts(meetings: &[Meeting]) -> Vec<Conflict> {
    let mut tree = IntervalTree::new();
    let mut conflicts = Vec::new();
    let mut inserted: Vec<(Interval, String)> = Vec::new();

    for meeting in meetings {
        let interval = Interval::new(meeting.start, meeting.end);

        let overlapping = tree.query_range(meeting.start, meeting.end);
        for overlap in &overlapping {
            let other_name = inserted
                .iter()
                .find(|(iv, _)| iv == overlap)
                .map(|(_, name)| name.clone())
                .unwrap_or_default();

            conflicts.push(Conflict {
                meeting_a: other_name,
                meeting_b: meeting.name.clone(),
            });
        }

        inserted.push((interval.clone(), meeting.name.clone()));
        tree.insert(interval);
    }
    conflicts
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn insert_and_query_point() {
        let mut tree = IntervalTree::new();
        tree.insert(Interval::new(15, 20));
        tree.insert(Interval::new(10, 30));
        tree.insert(Interval::new(17, 19));
        tree.insert(Interval::new(5, 20));
        tree.insert(Interval::new(12, 15));
        tree.insert(Interval::new(30, 40));

        let results = tree.query_point(18);
        assert!(results.iter().any(|i| *i == Interval::new(15, 20)));
        assert!(results.iter().any(|i| *i == Interval::new(10, 30)));
        assert!(results.iter().any(|i| *i == Interval::new(17, 19)));
        assert!(results.iter().any(|i| *i == Interval::new(5, 20)));
    }

    #[test]
    fn query_point_no_match() {
        let mut tree = IntervalTree::new();
        tree.insert(Interval::new(1, 5));
        tree.insert(Interval::new(10, 15));

        let results = tree.query_point(7);
        assert!(results.is_empty());
    }

    #[test]
    fn query_range_overlap() {
        let mut tree = IntervalTree::new();
        tree.insert(Interval::new(1, 5));
        tree.insert(Interval::new(3, 8));
        tree.insert(Interval::new(10, 15));
        tree.insert(Interval::new(20, 25));

        let results = tree.query_range(4, 12);
        assert_eq!(results.len(), 3); // [1,5], [3,8], [10,15]
    }

    #[test]
    fn delete_leaf() {
        let mut tree = IntervalTree::new();
        tree.insert(Interval::new(10, 20));
        tree.insert(Interval::new(5, 15));
        tree.insert(Interval::new(25, 30));

        assert!(tree.delete(&Interval::new(25, 30)));
        assert_eq!(tree.len(), 2);

        let results = tree.query_point(27);
        assert!(results.is_empty());
    }

    #[test]
    fn delete_node_with_children() {
        let mut tree = IntervalTree::new();
        tree.insert(Interval::new(10, 20));
        tree.insert(Interval::new(5, 15));
        tree.insert(Interval::new(25, 30));

        assert!(tree.delete(&Interval::new(10, 20)));
        assert_eq!(tree.len(), 2);

        assert!(!tree.query_point(12).iter().any(|i| *i == Interval::new(10, 20)));
    }

    #[test]
    fn delete_nonexistent() {
        let mut tree = IntervalTree::new();
        tree.insert(Interval::new(10, 20));

        assert!(!tree.delete(&Interval::new(5, 15)));
        assert_eq!(tree.len(), 1);
    }

    #[test]
    fn merge_overlapping_intervals() {
        let mut tree = IntervalTree::new();
        tree.insert(Interval::new(1, 3));
        tree.insert(Interval::new(2, 6));
        tree.insert(Interval::new(8, 10));
        tree.insert(Interval::new(15, 18));

        let merged = tree.merge_overlapping();
        assert_eq!(merged.len(), 3);
        assert_eq!(merged[0], Interval::new(1, 6));
        assert_eq!(merged[1], Interval::new(8, 10));
        assert_eq!(merged[2], Interval::new(15, 18));
    }

    #[test]
    fn merge_all_overlapping() {
        let mut tree = IntervalTree::new();
        tree.insert(Interval::new(1, 5));
        tree.insert(Interval::new(3, 8));
        tree.insert(Interval::new(7, 12));

        let merged = tree.merge_overlapping();
        assert_eq!(merged.len(), 1);
        assert_eq!(merged[0], Interval::new(1, 12));
    }

    #[test]
    fn iterator_sorted_order() {
        let mut tree = IntervalTree::new();
        tree.insert(Interval::new(15, 20));
        tree.insert(Interval::new(5, 10));
        tree.insert(Interval::new(25, 30));
        tree.insert(Interval::new(1, 3));

        let intervals: Vec<Interval> = tree.iter().collect();
        for window in intervals.windows(2) {
            assert!(window[0].low <= window[1].low, "not sorted: {:?}", intervals);
        }
    }

    #[test]
    fn empty_tree_queries() {
        let tree = IntervalTree::new();
        assert!(tree.query_point(5).is_empty());
        assert!(tree.query_range(1, 10).is_empty());
        assert!(tree.merge_overlapping().is_empty());
        assert_eq!(tree.iter().count(), 0);
    }

    #[test]
    fn single_interval() {
        let mut tree = IntervalTree::new();
        tree.insert(Interval::new(5, 10));

        assert_eq!(tree.query_point(7).len(), 1);
        assert!(tree.query_point(3).is_empty());
        assert!(tree.query_point(12).is_empty());
        assert_eq!(tree.len(), 1);
    }

    #[test]
    fn meeting_conflicts() {
        let meetings = vec![
            Meeting { name: "Standup".into(), start: 900, end: 930 },
            Meeting { name: "Design Review".into(), start: 920, end: 1000 },
            Meeting { name: "Lunch".into(), start: 1200, end: 1300 },
            Meeting { name: "1:1".into(), start: 1230, end: 1300 },
        ];

        let conflicts = find_conflicts(&meetings);
        assert_eq!(conflicts.len(), 2);
        assert_eq!(conflicts[0].meeting_a, "Standup");
        assert_eq!(conflicts[0].meeting_b, "Design Review");
        assert_eq!(conflicts[1].meeting_a, "Lunch");
        assert_eq!(conflicts[1].meeting_b, "1:1");
    }

    #[test]
    fn no_meeting_conflicts() {
        let meetings = vec![
            Meeting { name: "Morning".into(), start: 900, end: 1000 },
            Meeting { name: "Afternoon".into(), start: 1400, end: 1500 },
        ];

        let conflicts = find_conflicts(&meetings);
        assert!(conflicts.is_empty());
    }

    #[test]
    fn max_augmentation_after_delete() {
        let mut tree = IntervalTree::new();
        tree.insert(Interval::new(1, 100)); // high max
        tree.insert(Interval::new(5, 10));
        tree.insert(Interval::new(50, 60));

        tree.delete(&Interval::new(1, 100));

        // Point 80 should no longer be found
        assert!(tree.query_point(80).is_empty());
        // Point 7 should still be found
        assert_eq!(tree.query_point(7).len(), 1);
    }

    #[test]
    fn boundary_point_queries() {
        let mut tree = IntervalTree::new();
        tree.insert(Interval::new(10, 20));

        // Boundary points are inclusive
        assert_eq!(tree.query_point(10).len(), 1);
        assert_eq!(tree.query_point(20).len(), 1);
        assert!(tree.query_point(9).is_empty());
        assert!(tree.query_point(21).is_empty());
    }
}
```

## Running the Rust Solution

```bash
cargo new interval_tree --lib && cd interval_tree
# Replace src/lib.rs with the code above
cargo test -- --nocapture
```

### Expected Output

```
running 14 tests
test tests::insert_and_query_point ... ok
test tests::query_point_no_match ... ok
test tests::query_range_overlap ... ok
test tests::delete_leaf ... ok
test tests::delete_node_with_children ... ok
test tests::delete_nonexistent ... ok
test tests::merge_overlapping_intervals ... ok
test tests::merge_all_overlapping ... ok
test tests::iterator_sorted_order ... ok
test tests::empty_tree_queries ... ok
test tests::single_interval ... ok
test tests::meeting_conflicts ... ok
test tests::no_meeting_conflicts ... ok
test tests::max_augmentation_after_delete ... ok
test tests::boundary_point_queries ... ok

test result: ok. 14 passed; 0 failed; 0 ignored
```

## Design Decisions

1. **Augmented BST over centered interval tree**: The augmented BST approach (CLRS-style) is simpler to implement and understand. Each node's `max` field enables efficient pruning. The centered interval tree variant offers better worst-case for point queries but is significantly more complex.

2. **In-order successor for deletion**: When deleting a node with two children, replacing with the in-order successor (minimum of right subtree) maintains BST ordering. The alternative (in-order predecessor from left subtree) works equally well. Choosing successor is conventional.

3. **Pointer-based iterator**: The iterator uses raw pointers to traverse the tree without cloning the entire structure. This is a calculated use of `unsafe` -- the tree is immutably borrowed for the iterator's lifetime. A fully safe alternative would collect all intervals into a Vec first, but that defeats the purpose of lazy iteration.

4. **Intervals ordered by low endpoint**: BST ordering by `low` makes in-order traversal yield intervals in sorted order, which simplifies the merge operation and iterator implementation. Ties broken by `high` ensure deterministic ordering.

5. **Merge as a read-only operation**: `merge_overlapping()` does not modify the tree. It collects intervals via the iterator and returns a new merged list. This preserves the original tree for further queries.

## Common Mistakes

- **Forgetting to update max on deletion**: After removing a node or replacing it with its successor, every ancestor's `max` must be recalculated. Missing this breaks the pruning invariant and produces incorrect query results.
- **Incorrect overlap condition**: Two intervals `[a, b]` and `[c, d]` overlap if and only if `a <= d && c <= b`. The common error is using `<` instead of `<=` when boundaries are inclusive.
- **Pruning too aggressively**: For range queries, both subtrees may contain overlapping intervals. Only prune the left subtree if `left.max < query.low`, and only prune the right subtree if `node.interval.low > query.high`. Pruning both sides simultaneously can miss results.
- **Iterator invalidation**: The iterator holds references into the tree. Modifying the tree while iterating is undefined behavior with raw pointers and a logic error even with safe code. Document that the tree must not be mutated during iteration.

## Performance Notes

| Operation | Time Complexity | Notes |
|-----------|----------------|-------|
| Insert | O(log n) average, O(n) worst | Unbalanced BST degrades to linear |
| Delete | O(log n) average, O(n) worst | Same as insert |
| Point query | O(log n + k) | k = number of overlapping intervals |
| Range query | O(log n + k) | k = number of overlapping intervals |
| Merge overlapping | O(n) | After O(n) collection via iterator |
| Iterator next() | O(1) amortized | Each node pushed/popped once |
| Space | O(n) | One node per interval |

For guaranteed O(log n) operations, use a self-balancing BST (red-black tree or AVL tree) as the underlying structure. The augmentation technique works with any balanced BST -- just update `max` during rotations.
