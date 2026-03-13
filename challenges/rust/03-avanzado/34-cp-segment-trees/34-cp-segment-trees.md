# 34. CP: Segment Trees

## Difficulty: Avanzado

## Introduction

A segment tree is a data structure that answers range queries and supports point or range updates in O(log n) time. It is one of the most versatile tools in competitive programming, handling problems from range minimum/sum queries to interval scheduling, inversion counting, and coordinate compression.

The key idea: a segment tree is a binary tree where each node represents a contiguous range of the array. The root represents `[0, n)`, its children represent `[0, n/2)` and `[n/2, n)`, and so on down to individual elements at the leaves. Any query range `[l, r)` can be decomposed into O(log n) nodes.

In Rust, segment trees are naturally implemented as arrays (no pointer-based trees needed), which gives excellent cache performance. This exercise covers both recursive and iterative implementations, point updates, range updates with lazy propagation, and multiple problem applications.

---

## Segment Tree Fundamentals

### Array-Based Storage

A segment tree for an array of size n uses at most `4 * n` nodes (stored in a flat array). For a node at index `i`:
- Left child: `2 * i + 1`
- Right child: `2 * i + 2`
- Parent: `(i - 1) / 2`

Alternatively, using 1-indexed storage:
- Left child: `2 * i`
- Right child: `2 * i + 1`
- Parent: `i / 2`

### When to Use a Segment Tree

| Problem Type | Alternative | Why Segment Tree Wins |
|-------------|------------|----------------------|
| Range sum + point update | Fenwick tree (BIT) | Segment tree is more general |
| Range min/max query | Sparse table | Segment tree supports updates |
| Range update + range query | Nothing simpler | Lazy propagation required |
| Count inversions | Merge sort | Segment tree approach generalizes |
| Interval coloring | Brute force | O(log n) per operation vs O(n) |

---

## Implementation 1: Iterative Segment Tree (Point Update, Range Query)

### Theory

The iterative (bottom-up) segment tree is the fastest and simplest variant for point updates + range queries. It uses a `2 * n` sized array where elements `[n, 2n)` are the leaves (original array) and elements `[1, n)` are internal nodes.

This implementation avoids recursion entirely, making it both faster and simpler.

```rust
struct SegTree {
    n: usize,
    tree: Vec<i64>,
}

impl SegTree {
    /// Build from an array. Operation: sum (change + to min/max as needed).
    fn new(arr: &[i64]) -> Self {
        let n = arr.len();
        let mut tree = vec![0i64; 2 * n];

        // Fill leaves
        for i in 0..n {
            tree[n + i] = arr[i];
        }

        // Build internal nodes bottom-up
        for i in (1..n).rev() {
            tree[i] = tree[2 * i] + tree[2 * i + 1];
        }

        SegTree { n, tree }
    }

    /// Point update: set arr[pos] = value
    fn update(&mut self, mut pos: usize, value: i64) {
        pos += self.n; // go to leaf
        self.tree[pos] = value;

        // Update ancestors
        pos >>= 1;
        while pos >= 1 {
            self.tree[pos] = self.tree[2 * pos] + self.tree[2 * pos + 1];
            pos >>= 1;
        }
    }

    /// Range query: sum of arr[l..r) (half-open interval)
    fn query(&self, mut l: usize, mut r: usize) -> i64 {
        let mut result = 0i64;
        l += self.n;
        r += self.n;

        while l < r {
            if l & 1 == 1 {
                result += self.tree[l];
                l += 1;
            }
            if r & 1 == 1 {
                r -= 1;
                result += self.tree[r];
            }
            l >>= 1;
            r >>= 1;
        }

        result
    }
}
```

**How the query works:** We start at the leaves and move up. At each level, if the left boundary `l` is a right child (odd index), we include it and move right. If the right boundary `r` is a right child (odd index), we include the left sibling and move left. This collects O(log n) nodes that exactly cover `[l, r)`.

---

## Problem 1: Range Sum Query with Point Updates

Given an array of integers, support two operations:
1. `update(i, val)`: Set `arr[i] = val`.
2. `query(l, r)`: Return the sum of `arr[l..r)`.

**Example:**
```
arr = [1, 3, 5, 7, 9, 11]

query(1, 4) = 3 + 5 + 7 = 15
update(2, 10)  -> arr = [1, 3, 10, 7, 9, 11]
query(1, 4) = 3 + 10 + 7 = 20
query(0, 6) = 1 + 3 + 10 + 7 + 9 + 11 = 41
```

**Hints:**
1. Use the iterative segment tree from above.
2. `update(i, val)` modifies the leaf at `n + i` and propagates up.
3. `query(l, r)` uses the two-pointer bottom-up technique.

<details>
<summary>Solution</summary>

```rust
struct SegTree {
    n: usize,
    tree: Vec<i64>,
}

impl SegTree {
    fn new(arr: &[i64]) -> Self {
        let n = arr.len();
        let mut tree = vec![0i64; 2 * n];
        for i in 0..n {
            tree[n + i] = arr[i];
        }
        for i in (1..n).rev() {
            tree[i] = tree[2 * i] + tree[2 * i + 1];
        }
        SegTree { n, tree }
    }

    fn update(&mut self, mut pos: usize, value: i64) {
        pos += self.n;
        self.tree[pos] = value;
        pos >>= 1;
        while pos >= 1 {
            self.tree[pos] = self.tree[2 * pos] + self.tree[2 * pos + 1];
            pos >>= 1;
        }
    }

    fn query(&self, mut l: usize, mut r: usize) -> i64 {
        let mut result = 0i64;
        l += self.n;
        r += self.n;
        while l < r {
            if l & 1 == 1 {
                result += self.tree[l];
                l += 1;
            }
            if r & 1 == 1 {
                r -= 1;
                result += self.tree[r];
            }
            l >>= 1;
            r >>= 1;
        }
        result
    }
}

fn main() {
    let arr = vec![1i64, 3, 5, 7, 9, 11];
    let mut seg = SegTree::new(&arr);

    println!("{}", seg.query(1, 4)); // 15
    seg.update(2, 10);
    println!("{}", seg.query(1, 4)); // 20
    println!("{}", seg.query(0, 6)); // 41
}
```

</details>

**Complexity Analysis:**
- **Build:** O(n).
- **Update:** O(log n).
- **Query:** O(log n).
- **Space:** O(n) (exactly 2n elements).
- **Trade-offs:** The iterative version is 2-3x faster than recursive in practice due to no function call overhead and better cache behavior. However, it does not support lazy propagation easily (the recursive version is needed for that).

---

## Implementation 2: Recursive Segment Tree (Range Min Query)

The recursive version is more flexible: it naturally supports lazy propagation and can be adapted to any associative operation.

```rust
struct SegTreeMin {
    n: usize,
    tree: Vec<i64>,
}

impl SegTreeMin {
    fn new(arr: &[i64]) -> Self {
        let n = arr.len();
        let mut tree = vec![i64::MAX; 4 * n];
        let mut st = SegTreeMin { n, tree };
        st.build(arr, 0, 0, n);
        st
    }

    fn build(&mut self, arr: &[i64], node: usize, start: usize, end: usize) {
        if end - start == 1 {
            self.tree[node] = arr[start];
            return;
        }
        let mid = (start + end) / 2;
        self.build(arr, 2 * node + 1, start, mid);
        self.build(arr, 2 * node + 2, mid, end);
        self.tree[node] = self.tree[2 * node + 1].min(self.tree[2 * node + 2]);
    }

    fn update(&mut self, pos: usize, val: i64, node: usize, start: usize, end: usize) {
        if end - start == 1 {
            self.tree[node] = val;
            return;
        }
        let mid = (start + end) / 2;
        if pos < mid {
            self.update(pos, val, 2 * node + 1, start, mid);
        } else {
            self.update(pos, val, 2 * node + 2, mid, end);
        }
        self.tree[node] = self.tree[2 * node + 1].min(self.tree[2 * node + 2]);
    }

    /// Query min in [l, r)
    fn query(&self, l: usize, r: usize, node: usize, start: usize, end: usize) -> i64 {
        if l >= end || r <= start {
            return i64::MAX; // identity for min
        }
        if l <= start && end <= r {
            return self.tree[node];
        }
        let mid = (start + end) / 2;
        let left = self.query(l, r, 2 * node + 1, start, mid);
        let right = self.query(l, r, 2 * node + 2, mid, end);
        left.min(right)
    }

    // Convenience wrappers
    fn point_update(&mut self, pos: usize, val: i64) {
        self.update(pos, val, 0, 0, self.n);
    }

    fn range_min(&self, l: usize, r: usize) -> i64 {
        self.query(l, r, 0, 0, self.n)
    }
}
```

---

## Implementation 3: Lazy Propagation (Range Update + Range Query)

### Theory

Lazy propagation defers range updates to child nodes. When we update a range, we store the pending update in a "lazy" array. When we need to access a child, we "push down" the lazy value first.

This turns range updates from O(n) to O(log n) while maintaining O(log n) queries.

**Key principle:** Before accessing any child node, call `push_down` to apply pending lazy values.

### Problem 2: Range Add, Range Sum Query

Support two operations:
1. `range_add(l, r, val)`: Add `val` to every element in `arr[l..r)`.
2. `range_sum(l, r)`: Return the sum of `arr[l..r)`.

**Example:**
```
arr = [0, 0, 0, 0, 0]

range_add(1, 4, 3)   -> arr = [0, 3, 3, 3, 0]
range_sum(0, 5) = 9
range_add(2, 5, 2)   -> arr = [0, 3, 5, 5, 2]
range_sum(1, 3) = 3 + 5 = 8
range_sum(0, 5) = 15
```

**Hints:**
1. Each node stores the sum of its range.
2. The lazy value represents a pending addition to all elements in the range.
3. `push_down`: add `lazy[node] * (child_range_size)` to each child's sum, transfer lazy to children, reset lazy[node].
4. Always push down before recursing into children.

<details>
<summary>Solution</summary>

```rust
struct LazySegTree {
    n: usize,
    tree: Vec<i64>,
    lazy: Vec<i64>,
}

impl LazySegTree {
    fn new(arr: &[i64]) -> Self {
        let n = arr.len();
        let mut st = LazySegTree {
            n,
            tree: vec![0; 4 * n],
            lazy: vec![0; 4 * n],
        };
        st.build(arr, 0, 0, n);
        st
    }

    fn build(&mut self, arr: &[i64], node: usize, start: usize, end: usize) {
        if end - start == 1 {
            self.tree[node] = arr[start];
            return;
        }
        let mid = (start + end) / 2;
        self.build(arr, 2 * node + 1, start, mid);
        self.build(arr, 2 * node + 2, mid, end);
        self.tree[node] = self.tree[2 * node + 1] + self.tree[2 * node + 2];
    }

    fn push_down(&mut self, node: usize, start: usize, end: usize) {
        if self.lazy[node] != 0 {
            let mid = (start + end) / 2;
            let left = 2 * node + 1;
            let right = 2 * node + 2;

            // Apply lazy to children
            self.tree[left] += self.lazy[node] * (mid - start) as i64;
            self.lazy[left] += self.lazy[node];

            self.tree[right] += self.lazy[node] * (end - mid) as i64;
            self.lazy[right] += self.lazy[node];

            self.lazy[node] = 0;
        }
    }

    fn range_add_impl(
        &mut self,
        l: usize,
        r: usize,
        val: i64,
        node: usize,
        start: usize,
        end: usize,
    ) {
        if l >= end || r <= start {
            return;
        }
        if l <= start && end <= r {
            // Entire range is covered
            self.tree[node] += val * (end - start) as i64;
            self.lazy[node] += val;
            return;
        }
        self.push_down(node, start, end);
        let mid = (start + end) / 2;
        self.range_add_impl(l, r, val, 2 * node + 1, start, mid);
        self.range_add_impl(l, r, val, 2 * node + 2, mid, end);
        self.tree[node] = self.tree[2 * node + 1] + self.tree[2 * node + 2];
    }

    fn range_sum_impl(
        &mut self,
        l: usize,
        r: usize,
        node: usize,
        start: usize,
        end: usize,
    ) -> i64 {
        if l >= end || r <= start {
            return 0;
        }
        if l <= start && end <= r {
            return self.tree[node];
        }
        self.push_down(node, start, end);
        let mid = (start + end) / 2;
        let left = self.range_sum_impl(l, r, 2 * node + 1, start, mid);
        let right = self.range_sum_impl(l, r, 2 * node + 2, mid, end);
        left + right
    }

    // Convenience wrappers
    fn range_add(&mut self, l: usize, r: usize, val: i64) {
        let n = self.n;
        self.range_add_impl(l, r, val, 0, 0, n);
    }

    fn range_sum(&mut self, l: usize, r: usize) -> i64 {
        let n = self.n;
        self.range_sum_impl(l, r, 0, 0, n)
    }
}

fn main() {
    let arr = vec![0i64, 0, 0, 0, 0];
    let mut seg = LazySegTree::new(&arr);

    seg.range_add(1, 4, 3);
    println!("{}", seg.range_sum(0, 5)); // 9

    seg.range_add(2, 5, 2);
    println!("{}", seg.range_sum(1, 3)); // 8
    println!("{}", seg.range_sum(0, 5)); // 15
}
```

</details>

**Complexity Analysis:**
- **Build:** O(n).
- **Range update:** O(log n).
- **Range query:** O(log n).
- **Space:** O(n) (8n elements for tree + lazy).
- **Trade-offs:** Lazy propagation adds constant-factor overhead (~2x slower than non-lazy for point updates). Only use when range updates are required.

---

## Problem 3: Count Inversions

Given an array of integers, count the number of inversions: pairs (i, j) where i < j but arr[i] > arr[j].

**Example:**
```
arr = [3, 1, 2, 5, 4]

Inversions: (3,1), (3,2), (5,4) -> 3
```

**Approach:** Process elements from left to right. For each element, count how many previously inserted elements are greater than it. Use a segment tree over the value range (with coordinate compression if values are large).

**Hints:**
1. Coordinate compress values to range `[0, n)`.
2. Build a segment tree that counts occurrences of each value.
3. For each element v (processed left to right), the number of inversions it contributes is `query(v+1, n)` (count of elements already inserted that are greater than v).
4. Then `update(v, +1)` to mark v as inserted.

<details>
<summary>Solution</summary>

```rust
struct BIT {
    tree: Vec<i64>,
    n: usize,
}

impl BIT {
    fn new(n: usize) -> Self {
        BIT {
            tree: vec![0; n + 1],
            n,
        }
    }

    fn update(&mut self, mut i: usize, val: i64) {
        i += 1; // 1-indexed
        while i <= self.n {
            self.tree[i] += val;
            i += i & i.wrapping_neg();
        }
    }

    fn prefix_sum(&self, mut i: usize) -> i64 {
        i += 1; // 1-indexed
        let mut sum = 0i64;
        while i > 0 {
            sum += self.tree[i];
            i -= i & i.wrapping_neg();
        }
        sum
    }

    fn range_sum(&self, l: usize, r: usize) -> i64 {
        if l == 0 {
            self.prefix_sum(r)
        } else {
            self.prefix_sum(r) - self.prefix_sum(l - 1)
        }
    }
}

/// Using a segment tree approach (via BIT for simplicity, same idea)
fn count_inversions_segtree(arr: &[i64]) -> i64 {
    let n = arr.len();
    if n <= 1 {
        return 0;
    }

    // Coordinate compression
    let mut sorted = arr.to_vec();
    sorted.sort();
    sorted.dedup();

    let compress = |val: i64| -> usize {
        sorted.binary_search(&val).unwrap()
    };

    let m = sorted.len();
    let mut bit = BIT::new(m);
    let mut inversions = 0i64;

    for &val in arr {
        let c = compress(val);
        // Count elements already inserted with value > val
        if c + 1 < m {
            inversions += bit.range_sum(c + 1, m - 1);
        }
        bit.update(c, 1);
    }

    inversions
}

/// Alternative: using a full segment tree
fn count_inversions_full_segtree(arr: &[i64]) -> i64 {
    let n = arr.len();
    if n <= 1 {
        return 0;
    }

    // Coordinate compression
    let mut sorted = arr.to_vec();
    sorted.sort();
    sorted.dedup();
    let m = sorted.len();

    let compress = |val: i64| -> usize {
        sorted.binary_search(&val).unwrap()
    };

    // Iterative segment tree for sum queries over value range
    let mut tree = vec![0i64; 2 * m];

    let seg_update = |tree: &mut Vec<i64>, mut pos: usize, val: i64, n: usize| {
        pos += n;
        tree[pos] += val;
        let mut p = pos >> 1;
        while p >= 1 {
            tree[p] = tree[2 * p] + tree[2 * p + 1];
            p >>= 1;
        }
    };

    let seg_query = |tree: &Vec<i64>, mut l: usize, mut r: usize, n: usize| -> i64 {
        let mut result = 0i64;
        l += n;
        r += n;
        while l < r {
            if l & 1 == 1 {
                result += tree[l];
                l += 1;
            }
            if r & 1 == 1 {
                r -= 1;
                result += tree[r];
            }
            l >>= 1;
            r >>= 1;
        }
        result
    };

    let mut inversions = 0i64;

    for &val in arr {
        let c = compress(val);
        // Count elements with compressed value in (c, m)
        if c + 1 < m {
            inversions += seg_query(&tree, c + 1, m, m);
        }
        seg_update(&mut tree, c, 1, m);
    }

    inversions
}

fn main() {
    let arr = vec![3i64, 1, 2, 5, 4];
    println!("Inversions (BIT): {}", count_inversions_segtree(&arr)); // 3
    println!("Inversions (SegTree): {}", count_inversions_full_segtree(&arr)); // 3

    let arr2 = vec![5i64, 4, 3, 2, 1];
    println!("Inversions: {}", count_inversions_segtree(&arr2)); // 10 (fully reversed)

    let arr3 = vec![1i64, 2, 3, 4, 5];
    println!("Inversions: {}", count_inversions_segtree(&arr3)); // 0 (sorted)
}
```

</details>

**Complexity Analysis:**
- **Time:** O(n log n) -- n elements, each with O(log n) update and query.
- **Space:** O(n) for the segment tree over compressed values.
- **Trade-offs:** Merge sort also counts inversions in O(n log n) but the segment tree approach generalizes to many variations (inversions with constraints, weighted inversions, etc.). The BIT version is simpler and faster by a constant factor.

---

## Problem 4: Range Set, Range Sum (Lazy Propagation with Assignment)

Support two operations:
1. `range_set(l, r, val)`: Set every element in `arr[l..r)` to `val`.
2. `range_sum(l, r)`: Return the sum of `arr[l..r)`.

This is different from range-add because assignment is not composable in the same way (assigning twice does not add). The lazy value must represent "set to this value", and a special sentinel indicates "no pending set".

**Example:**
```
arr = [1, 2, 3, 4, 5]

range_set(1, 4, 10)  -> arr = [1, 10, 10, 10, 5]
range_sum(0, 5) = 36
range_set(0, 3, 0)   -> arr = [0, 0, 0, 10, 5]
range_sum(0, 5) = 15
```

**Hints:**
1. Use `Option<i64>` for lazy: `None` means no pending set, `Some(val)` means set entire range to val.
2. In `push_down`, if lazy is `Some(val)`, set both children's sums to `val * child_range_size` and set their lazy to `Some(val)`.
3. When applying a new `range_set`, it overwrites any existing lazy (assignment beats previous assignment).

<details>
<summary>Solution</summary>

```rust
struct LazySetSegTree {
    n: usize,
    tree: Vec<i64>,
    lazy: Vec<Option<i64>>,
}

impl LazySetSegTree {
    fn new(arr: &[i64]) -> Self {
        let n = arr.len();
        let mut st = LazySetSegTree {
            n,
            tree: vec![0; 4 * n],
            lazy: vec![None; 4 * n],
        };
        st.build(arr, 0, 0, n);
        st
    }

    fn build(&mut self, arr: &[i64], node: usize, start: usize, end: usize) {
        if end - start == 1 {
            self.tree[node] = arr[start];
            return;
        }
        let mid = (start + end) / 2;
        self.build(arr, 2 * node + 1, start, mid);
        self.build(arr, 2 * node + 2, mid, end);
        self.tree[node] = self.tree[2 * node + 1] + self.tree[2 * node + 2];
    }

    fn push_down(&mut self, node: usize, start: usize, end: usize) {
        if let Some(val) = self.lazy[node] {
            let mid = (start + end) / 2;
            let left = 2 * node + 1;
            let right = 2 * node + 2;

            self.tree[left] = val * (mid - start) as i64;
            self.lazy[left] = Some(val);

            self.tree[right] = val * (end - mid) as i64;
            self.lazy[right] = Some(val);

            self.lazy[node] = None;
        }
    }

    fn range_set_impl(
        &mut self,
        l: usize,
        r: usize,
        val: i64,
        node: usize,
        start: usize,
        end: usize,
    ) {
        if l >= end || r <= start {
            return;
        }
        if l <= start && end <= r {
            self.tree[node] = val * (end - start) as i64;
            self.lazy[node] = Some(val);
            return;
        }
        self.push_down(node, start, end);
        let mid = (start + end) / 2;
        self.range_set_impl(l, r, val, 2 * node + 1, start, mid);
        self.range_set_impl(l, r, val, 2 * node + 2, mid, end);
        self.tree[node] = self.tree[2 * node + 1] + self.tree[2 * node + 2];
    }

    fn range_sum_impl(
        &mut self,
        l: usize,
        r: usize,
        node: usize,
        start: usize,
        end: usize,
    ) -> i64 {
        if l >= end || r <= start {
            return 0;
        }
        if l <= start && end <= r {
            return self.tree[node];
        }
        self.push_down(node, start, end);
        let mid = (start + end) / 2;
        self.range_sum_impl(l, r, 2 * node + 1, start, mid)
            + self.range_sum_impl(l, r, 2 * node + 2, mid, end)
    }

    fn range_set(&mut self, l: usize, r: usize, val: i64) {
        let n = self.n;
        self.range_set_impl(l, r, val, 0, 0, n);
    }

    fn range_sum(&mut self, l: usize, r: usize) -> i64 {
        let n = self.n;
        self.range_sum_impl(l, r, 0, 0, n)
    }
}

fn main() {
    let arr = vec![1i64, 2, 3, 4, 5];
    let mut seg = LazySetSegTree::new(&arr);

    seg.range_set(1, 4, 10);
    println!("{}", seg.range_sum(0, 5)); // 36

    seg.range_set(0, 3, 0);
    println!("{}", seg.range_sum(0, 5)); // 15
}
```

</details>

**Complexity Analysis:**
- **Time:** O(log n) per operation.
- **Space:** O(n).
- **Trade-offs:** Using `Option<i64>` for lazy is idiomatic Rust and clearer than sentinel values like `-1` or `i64::MIN`. The `Option` adds 8 bytes per node (for the discriminant), which is negligible.

---

## Problem 5: Interval Coloring (Distinct Colors in Range)

Given an array where each element starts with color 0, support:
1. `paint(l, r, color)`: Paint all elements in `[l, r)` with `color`.
2. `count_colors(l, r)`: Return the number of distinct colors in `[l, r)`.

This is significantly harder. A segment tree alone cannot efficiently answer "distinct count" queries. The standard approach for interval coloring with assignment is to use a segment tree with lazy propagation where each node tracks whether its entire range has a uniform color.

For this simplified version, we support `paint` and `query_color(i)` (single point color query), plus we can iterate to count colors in a range.

**A more practical variant:** Count the number of distinct color segments. When the entire range of a node has the same color, store it. Otherwise, merge from children.

<details>
<summary>Solution</summary>

```rust
/// Simplified interval coloring: range assign + point query.
/// For distinct color counting in a range, we iterate over the
/// segment tree's structure to find color-change boundaries.
struct ColorSegTree {
    n: usize,
    color: Vec<Option<i64>>, // Some(c) if entire range is color c
    lazy: Vec<Option<i64>>,
}

impl ColorSegTree {
    fn new(n: usize, initial_color: i64) -> Self {
        let mut st = ColorSegTree {
            n,
            color: vec![None; 4 * n],
            lazy: vec![None; 4 * n],
        };
        st.init(initial_color, 0, 0, n);
        st
    }

    fn init(&mut self, c: i64, node: usize, start: usize, end: usize) {
        self.color[node] = Some(c);
        if end - start == 1 {
            return;
        }
        let mid = (start + end) / 2;
        self.init(c, 2 * node + 1, start, mid);
        self.init(c, 2 * node + 2, mid, end);
    }

    fn push_down(&mut self, node: usize, start: usize, end: usize) {
        if let Some(c) = self.lazy[node] {
            let mid = (start + end) / 2;
            let left = 2 * node + 1;
            let right = 2 * node + 2;

            self.color[left] = Some(c);
            self.lazy[left] = Some(c);

            self.color[right] = Some(c);
            self.lazy[right] = Some(c);

            self.lazy[node] = None;
        }
    }

    fn paint_impl(&mut self, l: usize, r: usize, c: i64, node: usize, start: usize, end: usize) {
        if l >= end || r <= start {
            return;
        }
        if l <= start && end <= r {
            self.color[node] = Some(c);
            self.lazy[node] = Some(c);
            return;
        }
        self.push_down(node, start, end);
        let mid = (start + end) / 2;
        self.paint_impl(l, r, c, 2 * node + 1, start, mid);
        self.paint_impl(l, r, c, 2 * node + 2, mid, end);

        // Merge: if both children have the same color, parent is uniform too
        let lc = self.color[2 * node + 1];
        let rc = self.color[2 * node + 2];
        self.color[node] = if lc == rc { lc } else { None };
    }

    /// Collect all colors in [l, r) into a set
    fn collect_colors_impl(
        &mut self,
        l: usize,
        r: usize,
        node: usize,
        start: usize,
        end: usize,
        colors: &mut std::collections::HashSet<i64>,
    ) {
        if l >= end || r <= start {
            return;
        }
        // If this node has a uniform color and is fully within query range
        if let Some(c) = self.color[node] {
            if l <= start && end <= r {
                colors.insert(c);
                return;
            }
        }
        if end - start == 1 {
            if let Some(c) = self.color[node] {
                colors.insert(c);
            }
            return;
        }
        self.push_down(node, start, end);
        let mid = (start + end) / 2;
        self.collect_colors_impl(l, r, 2 * node + 1, start, mid, colors);
        self.collect_colors_impl(l, r, 2 * node + 2, mid, end, colors);
    }

    fn paint(&mut self, l: usize, r: usize, c: i64) {
        let n = self.n;
        self.paint_impl(l, r, c, 0, 0, n);
    }

    fn count_colors(&mut self, l: usize, r: usize) -> usize {
        let n = self.n;
        let mut colors = std::collections::HashSet::new();
        self.collect_colors_impl(l, r, 0, 0, n, &mut colors);
        colors.len()
    }
}

fn main() {
    let mut seg = ColorSegTree::new(10, 0);

    seg.paint(2, 6, 1);  // positions 2..6 are color 1
    seg.paint(4, 8, 2);  // positions 4..8 are color 2

    // Now: [0,0,1,1,2,2,2,2,0,0]
    println!("Colors in [0,10): {}", seg.count_colors(0, 10)); // 3 (colors 0, 1, 2)
    println!("Colors in [2,6): {}", seg.count_colors(2, 6));   // 2 (colors 1, 2)
    println!("Colors in [0,2): {}", seg.count_colors(0, 2));   // 1 (color 0)

    seg.paint(0, 10, 5);
    println!("Colors in [0,10): {}", seg.count_colors(0, 10)); // 1 (color 5)
}
```

</details>

**Complexity Analysis:**
- **Paint:** O(log n) amortized (the number of "color segments" decreases or stays the same per paint).
- **Count colors:** O(S log n) where S is the number of distinct color segments in the range. In the worst case, this is O(n), but after a paint operation that creates at most O(log n) new segments, it stays manageable.
- **Space:** O(n).
- **Trade-offs:** Distinct counting in a range is inherently harder than sum/min/max. For exact "distinct count" with arbitrary updates, persistent segment trees or offline algorithms (Mo's algorithm) may be needed.

---

## Generic Segment Tree

For competitive programming, having a generic segment tree that works with any monoid (associative operation with identity) is valuable:

```rust
trait Monoid {
    fn identity() -> Self;
    fn combine(a: &Self, b: &Self) -> Self;
}

struct GenericSegTree<T: Monoid + Clone> {
    n: usize,
    tree: Vec<T>,
}

impl<T: Monoid + Clone> GenericSegTree<T> {
    fn new(arr: &[T]) -> Self {
        let n = arr.len();
        let mut tree = vec![T::identity(); 2 * n];
        for i in 0..n {
            tree[n + i] = arr[i].clone();
        }
        for i in (1..n).rev() {
            tree[i] = T::combine(&tree[2 * i], &tree[2 * i + 1]);
        }
        GenericSegTree { n, tree }
    }

    fn update(&mut self, mut pos: usize, val: T) {
        pos += self.n;
        self.tree[pos] = val;
        pos >>= 1;
        while pos >= 1 {
            self.tree[pos] = T::combine(&self.tree[2 * pos], &self.tree[2 * pos + 1]);
            pos >>= 1;
        }
    }

    fn query(&self, mut l: usize, mut r: usize) -> T {
        let mut left_result = T::identity();
        let mut right_result = T::identity();
        l += self.n;
        r += self.n;
        while l < r {
            if l & 1 == 1 {
                left_result = T::combine(&left_result, &self.tree[l]);
                l += 1;
            }
            if r & 1 == 1 {
                r -= 1;
                right_result = T::combine(&self.tree[r], &right_result);
            }
            l >>= 1;
            r >>= 1;
        }
        T::combine(&left_result, &right_result)
    }
}

// Example: Range minimum
#[derive(Clone)]
struct Min(i64);

impl Monoid for Min {
    fn identity() -> Self { Min(i64::MAX) }
    fn combine(a: &Self, b: &Self) -> Self { Min(a.0.min(b.0)) }
}

// Example: Range sum
#[derive(Clone)]
struct Sum(i64);

impl Monoid for Sum {
    fn identity() -> Self { Sum(0) }
    fn combine(a: &Self, b: &Self) -> Self { Sum(a.0 + b.0) }
}

// Example: Range GCD
#[derive(Clone)]
struct Gcd(i64);

impl Monoid for Gcd {
    fn identity() -> Self { Gcd(0) } // gcd(0, x) = x
    fn combine(a: &Self, b: &Self) -> Self {
        fn gcd(a: i64, b: i64) -> i64 {
            if b == 0 { a } else { gcd(b, a % b) }
        }
        Gcd(gcd(a.0.abs(), b.0.abs()))
    }
}
```

This generic version lets you swap the monoid without rewriting the tree structure.

---

## Comparison of Approaches

| Feature | Iterative | Recursive | Lazy Recursive |
|---------|-----------|-----------|---------------|
| Point update | O(log n) | O(log n) | O(log n) |
| Range update | Not supported | Not supported | O(log n) |
| Range query | O(log n) | O(log n) | O(log n) |
| Code simplicity | Very simple | Moderate | Complex |
| Constant factor | Fastest | ~1.5x slower | ~2x slower |
| Memory | 2n | 4n | 8n |

## Common Pitfalls in Rust

1. **Off-by-one in ranges:** The iterative segment tree uses half-open intervals `[l, r)`. Mixing this with closed intervals `[l, r]` is a common source of bugs.
2. **Array size:** Recursive segment trees need `4 * n` nodes. Using `2 * n` causes out-of-bounds access.
3. **push_down before recursing:** Forgetting to call `push_down` before accessing children in lazy propagation silently produces wrong answers. This is the single most common bug.
4. **Integer overflow:** Range sums can overflow `i32` quickly (n=10^5 elements of value 10^9). Always use `i64`.
5. **Recursive depth:** For n up to 10^5, the recursion depth is ~17, well within limits. For n up to 10^6, depth is ~20. No stack overflow risk for segment trees.

---

## Further Reading

- **CSES Problem Set** -- "Dynamic Range Sum Queries", "Range Update Queries", "Range Minimum Queries".
- **Competitive Programmer's Handbook** (Laaksonen) -- Chapter 9 (Range Queries).
- **cp-algorithms.com** -- "Segment Tree" with lazy propagation, persistent segment trees, and 2D segment trees.
- **Codeforces EDU** -- Segment tree course with interactive problems.
