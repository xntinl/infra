# 45. CP: Fenwick Tree / BIT

**Difficulty**: Avanzado

## Prerequisites

- Solid understanding of binary representation and bitwise operations
- Familiarity with prefix sums and their limitations (static arrays, no updates)
- Completed: exercises on iterators, generics, and traits

## Learning Objectives

- Understand the Binary Indexed Tree (BIT/Fenwick Tree) data structure and its bit-manipulation core
- Implement point update + prefix query in O(log n)
- Implement range update + point query using the dual BIT technique
- Extend to 2D for matrix prefix sums with updates
- Solve competitive programming problems: range sum queries, inversion counting, order statistics

## Concepts

The Fenwick Tree (also called Binary Indexed Tree or BIT) is one of the most elegant data structures in competitive programming. It answers prefix sum queries and handles point updates in O(log n), using only an array and two bitwise operations. No pointers, no complex balancing -- just arithmetic on indices.

### The Problem with Prefix Sums

A prefix sum array lets you compute the sum of any range `[l, r]` in O(1), but updating a single element costs O(n) because you must rebuild the entire prefix array. A Fenwick Tree provides O(log n) for both operations.

### How It Works

The key insight is that every positive integer can be decomposed into powers of 2. The Fenwick Tree stores partial sums where each index `i` is responsible for a range of elements determined by the lowest set bit of `i`.

The lowest set bit of `i` is computed as `i & (-i)` (or equivalently `i & i.wrapping_neg()` in Rust, since `-i` on unsigned types requires wrapping). This value determines how many elements index `i` "covers":

```
Index (1-based) | Binary | Lowest Bit | Covers elements
1               | 0001   | 1          | [1, 1]
2               | 0010   | 2          | [1, 2]
3               | 0011   | 1          | [3, 3]
4               | 0100   | 4          | [1, 4]
5               | 0101   | 1          | [5, 5]
6               | 0110   | 2          | [5, 6]
7               | 0111   | 1          | [7, 7]
8               | 1000   | 8          | [1, 8]
```

**Prefix query** (sum of elements 1..=i): Walk from `i` toward 0, adding `tree[i]` at each step. To move to the next node, strip the lowest set bit: `i -= i & (-i)`.

**Point update** (add `delta` to element at index `i`): Walk from `i` toward `n`, adding `delta` to `tree[i]` at each step. To move to the next node, add the lowest set bit: `i += i & (-i)`.

### Visual Example

Suppose we have the array `[0, 3, 2, -1, 6, 5, 4, -3, 1]` (1-indexed, index 0 unused):

```
tree[1] = a[1]                         = 3
tree[2] = a[1] + a[2]                  = 5
tree[3] = a[3]                         = -1
tree[4] = a[1] + a[2] + a[3] + a[4]   = 10
tree[5] = a[5]                         = 5
tree[6] = a[5] + a[6]                  = 9
tree[7] = a[7]                         = -3
tree[8] = a[1]+...+a[8]                = 17
```

Query prefix_sum(6):
- i=6 (110): add tree[6]=9, strip lowest bit -> i=4 (100)
- i=4 (100): add tree[4]=10, strip lowest bit -> i=0
- Result: 9 + 10 = 19 (which is 3+2-1+6+5+4 = 19)

---

## Implementation

### Basic Fenwick Tree: Point Update + Prefix Query

```rust
/// A Fenwick Tree (Binary Indexed Tree) supporting point updates
/// and prefix sum queries, both in O(log n).
///
/// Uses 1-based indexing internally. The user-facing API accepts
/// 0-based indices and converts internally.
struct FenwickTree {
    tree: Vec<i64>,
    n: usize,
}

impl FenwickTree {
    /// Create a Fenwick Tree for `n` elements, all initialized to 0.
    fn new(n: usize) -> Self {
        Self {
            tree: vec![0; n + 1],
            n,
        }
    }

    /// Build a Fenwick Tree from an existing slice in O(n).
    /// This is faster than calling `update` n times (which would be O(n log n)).
    fn from_slice(data: &[i64]) -> Self {
        let n = data.len();
        let mut tree = vec![0i64; n + 1];

        // Copy data into 1-based positions
        for i in 0..n {
            tree[i + 1] = data[i];
        }

        // Propagate partial sums upward
        for i in 1..=n {
            let parent = i + (i & i.wrapping_neg());
            if parent <= n {
                tree[parent] += tree[i];
            }
        }

        Self { tree, n }
    }

    /// Add `delta` to the element at 0-based index `idx`.
    fn update(&mut self, idx: usize, delta: i64) {
        let mut i = idx + 1; // convert to 1-based
        while i <= self.n {
            self.tree[i] += delta;
            i += i & i.wrapping_neg(); // add lowest set bit
        }
    }

    /// Return the prefix sum of elements [0, idx] (inclusive, 0-based).
    fn prefix_sum(&self, idx: usize) -> i64 {
        let mut sum = 0i64;
        let mut i = idx + 1; // convert to 1-based
        while i > 0 {
            sum += self.tree[i];
            i -= i & i.wrapping_neg(); // strip lowest set bit
        }
        sum
    }

    /// Return the sum of elements in the range [left, right] (inclusive, 0-based).
    fn range_sum(&self, left: usize, right: usize) -> i64 {
        if left == 0 {
            self.prefix_sum(right)
        } else {
            self.prefix_sum(right) - self.prefix_sum(left - 1)
        }
    }

    /// Set the element at 0-based index `idx` to `value`.
    /// Requires knowing the current value, which we compute via range query.
    fn set(&mut self, idx: usize, value: i64) {
        let current = self.range_sum(idx, idx);
        self.update(idx, value - current);
    }
}

fn main() {
    let data = vec![3, 2, -1, 6, 5, 4, -3, 1];
    let mut bit = FenwickTree::from_slice(&data);

    println!("prefix_sum(0) = {}", bit.prefix_sum(0)); // 3
    println!("prefix_sum(3) = {}", bit.prefix_sum(3)); // 3+2-1+6 = 10
    println!("prefix_sum(5) = {}", bit.prefix_sum(5)); // 3+2-1+6+5+4 = 19
    println!("range_sum(2, 5) = {}", bit.range_sum(2, 5)); // -1+6+5+4 = 14

    bit.update(2, 5); // a[2] was -1, now -1+5 = 4
    println!("after update(2, +5):");
    println!("prefix_sum(5) = {}", bit.prefix_sum(5)); // 24
    println!("range_sum(2, 2) = {}", bit.range_sum(2, 2)); // 4

    bit.set(0, 10); // a[0] = 10
    println!("after set(0, 10):");
    println!("prefix_sum(0) = {}", bit.prefix_sum(0)); // 10
}
```

### Range Update + Point Query (Dual BIT)

Sometimes you need the opposite: update an entire range and query a single point. A single Fenwick Tree can handle this by storing differences. Adding `delta` to range `[l, r]` becomes two point updates on the difference array: `update(l, +delta)` and `update(r+1, -delta)`. A point query at index `i` is then `prefix_sum(i)`.

```rust
/// A dual Fenwick Tree supporting range updates and point queries.
/// Internally it stores a difference array in a standard BIT.
struct DualFenwickTree {
    bit: FenwickTree,
}

impl DualFenwickTree {
    fn new(n: usize) -> Self {
        Self {
            bit: FenwickTree::new(n),
        }
    }

    /// Add `delta` to all elements in [left, right] (0-based, inclusive).
    fn range_update(&mut self, left: usize, right: usize, delta: i64) {
        self.bit.update(left, delta);
        if right + 1 < self.bit.n {
            self.bit.update(right + 1, -delta);
        }
    }

    /// Query the value at 0-based index `idx`.
    fn point_query(&self, idx: usize) -> i64 {
        self.bit.prefix_sum(idx)
    }
}

fn main() {
    let mut dual = DualFenwickTree::new(8);

    // Add 5 to range [1, 4]
    dual.range_update(1, 4, 5);
    // Add 3 to range [3, 6]
    dual.range_update(3, 6, 3);

    // Expected: [0, 5, 5, 8, 8, 3, 3, 0]
    for i in 0..8 {
        print!("{} ", dual.point_query(i));
    }
    println!();
}
```

### Range Update + Range Query

For both range updates and range queries, we use two BITs. The math behind this uses the identity:

If we maintain two BITs, `B1` and `B2`, such that a range update `[l, r] += delta` performs:
- `B1.update(l, delta)`, `B1.update(r+1, -delta)`
- `B2.update(l, delta * (l-1))`, `B2.update(r+1, -delta * r)`

Then `prefix_sum(i) = B1.prefix_sum(i) * i - B2.prefix_sum(i)`.

```rust
struct RangeRangeBIT {
    b1: FenwickTree,
    b2: FenwickTree,
    n: usize,
}

impl RangeRangeBIT {
    fn new(n: usize) -> Self {
        Self {
            b1: FenwickTree::new(n),
            b2: FenwickTree::new(n),
            n,
        }
    }

    /// Add `delta` to all elements in [left, right] (0-based, inclusive).
    fn range_update(&mut self, left: usize, right: usize, delta: i64) {
        let l = left as i64;
        let r = right as i64;

        self.b1.update(left, delta);
        self.b2.update(left, delta * (l));

        if right + 1 < self.n {
            self.b1.update(right + 1, -delta);
            self.b2.update(right + 1, -delta * (r + 1));
        }
    }

    /// Return the prefix sum of elements [0, idx] after all range updates.
    fn prefix_sum(&self, idx: usize) -> i64 {
        let i = idx as i64;
        self.b1.prefix_sum(idx) * (i + 1) - self.b2.prefix_sum(idx)
    }

    /// Return the sum of elements in [left, right] (0-based, inclusive).
    fn range_sum(&self, left: usize, right: usize) -> i64 {
        if left == 0 {
            self.prefix_sum(right)
        } else {
            self.prefix_sum(right) - self.prefix_sum(left - 1)
        }
    }
}

fn main() {
    let mut rr = RangeRangeBIT::new(8);

    rr.range_update(1, 4, 5); // [0, 5, 5, 5, 5, 0, 0, 0]
    rr.range_update(3, 6, 3); // [0, 5, 5, 8, 8, 3, 3, 0]

    println!("range_sum(0, 7) = {}", rr.range_sum(0, 7)); // 32
    println!("range_sum(1, 4) = {}", rr.range_sum(1, 4)); // 26
    println!("range_sum(3, 6) = {}", rr.range_sum(3, 6)); // 22
}
```

### 2D Fenwick Tree

The 2D Fenwick Tree extends the same principle to a matrix. Point updates and prefix rectangle queries both run in O(log(rows) * log(cols)).

```rust
struct FenwickTree2D {
    tree: Vec<Vec<i64>>,
    rows: usize,
    cols: usize,
}

impl FenwickTree2D {
    fn new(rows: usize, cols: usize) -> Self {
        Self {
            tree: vec![vec![0i64; cols + 1]; rows + 1],
            rows,
            cols,
        }
    }

    /// Add `delta` to the cell at (row, col), both 0-based.
    fn update(&mut self, row: usize, col: usize, delta: i64) {
        let mut r = row + 1;
        while r <= self.rows {
            let mut c = col + 1;
            while c <= self.cols {
                self.tree[r][c] += delta;
                c += c & c.wrapping_neg();
            }
            r += r & r.wrapping_neg();
        }
    }

    /// Return the sum of the rectangle [(0,0), (row,col)] (inclusive, 0-based).
    fn prefix_sum(&self, row: usize, col: usize) -> i64 {
        let mut sum = 0i64;
        let mut r = row + 1;
        while r > 0 {
            let mut c = col + 1;
            while c > 0 {
                sum += self.tree[r][c];
                c -= c & c.wrapping_neg();
            }
            r -= r & r.wrapping_neg();
        }
        sum
    }

    /// Return the sum of the rectangle [(r1,c1), (r2,c2)] (inclusive, 0-based).
    fn range_sum(&self, r1: usize, c1: usize, r2: usize, c2: usize) -> i64 {
        let mut result = self.prefix_sum(r2, c2);
        if r1 > 0 {
            result -= self.prefix_sum(r1 - 1, c2);
        }
        if c1 > 0 {
            result -= self.prefix_sum(r2, c1 - 1);
        }
        if r1 > 0 && c1 > 0 {
            result += self.prefix_sum(r1 - 1, c1 - 1);
        }
        result
    }
}

fn main() {
    let mut bit2d = FenwickTree2D::new(4, 4);

    // Fill a 4x4 matrix:
    // 1 2 3 4
    // 5 6 7 8
    // 9 0 1 2
    // 3 4 5 6
    let matrix = [
        [1, 2, 3, 4],
        [5, 6, 7, 8],
        [9, 0, 1, 2],
        [3, 4, 5, 6],
    ];

    for r in 0..4 {
        for c in 0..4 {
            bit2d.update(r, c, matrix[r][c]);
        }
    }

    // Sum of entire matrix
    println!("total = {}", bit2d.prefix_sum(3, 3)); // 76

    // Sum of submatrix [(1,1), (2,2)] = 6+7+0+1 = 14
    println!("sub = {}", bit2d.range_sum(1, 1, 2, 2));

    // Update cell (1,1) from 6 to 10 (delta = +4)
    bit2d.update(1, 1, 4);
    println!("sub after update = {}", bit2d.range_sum(1, 1, 2, 2)); // 18
}
```

---

## Generic Implementation

We can make the Fenwick Tree generic over any type that supports addition and subtraction:

```rust
use std::ops::{Add, AddAssign, Sub};

struct GenericBIT<T> {
    tree: Vec<T>,
    n: usize,
}

impl<T> GenericBIT<T>
where
    T: Copy + Default + Add<Output = T> + AddAssign + Sub<Output = T>,
{
    fn new(n: usize) -> Self {
        Self {
            tree: vec![T::default(); n + 1],
            n,
        }
    }

    fn update(&mut self, idx: usize, delta: T) {
        let mut i = idx + 1;
        while i <= self.n {
            self.tree[i] += delta;
            i += i & i.wrapping_neg();
        }
    }

    fn prefix_sum(&self, idx: usize) -> T {
        let mut sum = T::default();
        let mut i = idx + 1;
        while i > 0 {
            sum = sum + self.tree[i];
            i -= i & i.wrapping_neg();
        }
        sum
    }

    fn range_sum(&self, left: usize, right: usize) -> T {
        if left == 0 {
            self.prefix_sum(right)
        } else {
            self.prefix_sum(right) - self.prefix_sum(left - 1)
        }
    }
}

fn main() {
    // Works with i32
    let mut bit_i32 = GenericBIT::<i32>::new(5);
    bit_i32.update(0, 10);
    bit_i32.update(1, 20);
    bit_i32.update(2, 30);
    println!("i32 prefix_sum(2) = {}", bit_i32.prefix_sum(2)); // 60

    // Works with f64
    let mut bit_f64 = GenericBIT::<f64>::new(5);
    bit_f64.update(0, 1.5);
    bit_f64.update(1, 2.7);
    bit_f64.update(2, 3.3);
    println!("f64 prefix_sum(2) = {}", bit_f64.prefix_sum(2)); // 7.5
}
```

---

## Complexity Analysis

| Operation | Time | Space |
|-----------|------|-------|
| Build from slice | O(n) | O(n) |
| Point update | O(log n) | - |
| Prefix query | O(log n) | - |
| Range query | O(log n) | - |
| Range update (dual) | O(log n) | - |
| 2D point update | O(log(r) * log(c)) | O(r * c) |
| 2D prefix query | O(log(r) * log(c)) | - |

Compared to a segment tree, the Fenwick Tree uses 2x less memory (array of `n+1` vs `4n`), has a smaller constant factor, and is simpler to implement. The trade-off is that segment trees can handle more general operations (min, max, GCD) while Fenwick Trees require an invertible operation (typically addition/subtraction).

---

## Exercises

### Exercise 1: Dynamic Range Sum Queries

Given an array of `n` integers and `q` operations of two types:
- `Update(i, v)`: set the element at index `i` to `v`
- `Query(l, r)`: return the sum of elements in `[l, r]`

Process all operations and collect query results.

**Input:**
```
n = 8, initial = [1, 3, 5, 7, 9, 11, 13, 15]
operations:
  Query(0, 3)    -> 16
  Update(2, 10)  -> a[2] = 10
  Query(0, 3)    -> 21
  Update(5, 0)   -> a[5] = 0
  Query(2, 6)    -> 39 (10+7+9+0+13)
  Query(0, 7)    -> 58
```

<details>
<summary>Solution</summary>

```rust
struct FenwickTree {
    tree: Vec<i64>,
    n: usize,
}

impl FenwickTree {
    fn from_slice(data: &[i64]) -> Self {
        let n = data.len();
        let mut tree = vec![0i64; n + 1];
        for i in 0..n {
            tree[i + 1] = data[i];
        }
        for i in 1..=n {
            let parent = i + (i & i.wrapping_neg());
            if parent <= n {
                tree[parent] += tree[i];
            }
        }
        Self { tree, n }
    }

    fn update(&mut self, idx: usize, delta: i64) {
        let mut i = idx + 1;
        while i <= self.n {
            self.tree[i] += delta;
            i += i & i.wrapping_neg();
        }
    }

    fn prefix_sum(&self, idx: usize) -> i64 {
        let mut sum = 0i64;
        let mut i = idx + 1;
        while i > 0 {
            sum += self.tree[i];
            i -= i & i.wrapping_neg();
        }
        sum
    }

    fn range_sum(&self, left: usize, right: usize) -> i64 {
        if left == 0 {
            self.prefix_sum(right)
        } else {
            self.prefix_sum(right) - self.prefix_sum(left - 1)
        }
    }
}

enum Op {
    Update(usize, i64),
    Query(usize, usize),
}

fn solve(initial: &[i64], ops: &[Op]) -> Vec<i64> {
    let mut bit = FenwickTree::from_slice(initial);
    let mut current = initial.to_vec();
    let mut results = Vec::new();

    for op in ops {
        match *op {
            Op::Update(idx, value) => {
                let delta = value - current[idx];
                current[idx] = value;
                bit.update(idx, delta);
            }
            Op::Query(l, r) => {
                results.push(bit.range_sum(l, r));
            }
        }
    }

    results
}

fn main() {
    let initial = vec![1, 3, 5, 7, 9, 11, 13, 15];
    let ops = vec![
        Op::Query(0, 3),
        Op::Update(2, 10),
        Op::Query(0, 3),
        Op::Update(5, 0),
        Op::Query(2, 6),
        Op::Query(0, 7),
    ];

    let results = solve(&initial, &ops);
    assert_eq!(results, vec![16, 21, 39, 58]);
    println!("all queries correct: {:?}", results);
}
```
</details>

### Exercise 2: Count Inversions

An inversion in an array is a pair `(i, j)` where `i < j` but `a[i] > a[j]`. Count the total number of inversions using a Fenwick Tree.

**Approach:** Process the array from right to left. For each element `a[i]`, the number of elements already processed that are smaller than `a[i]` equals the prefix sum `bit.prefix_sum(a[i] - 1)`. After counting, add `a[i]` to the BIT.

**Constraint:** Elements are in range `[1, n]` (coordinate compress if needed).

**Input:**
```
[3, 1, 2, 5, 4] -> 3 inversions: (3,1), (3,2), (5,4)
[5, 4, 3, 2, 1] -> 10 inversions (maximum for n=5)
[1, 2, 3, 4, 5] -> 0 inversions (sorted)
```

<details>
<summary>Solution</summary>

```rust
struct FenwickTree {
    tree: Vec<i64>,
    n: usize,
}

impl FenwickTree {
    fn new(n: usize) -> Self {
        Self {
            tree: vec![0; n + 1],
            n,
        }
    }

    fn update(&mut self, idx: usize, delta: i64) {
        let mut i = idx + 1;
        while i <= self.n {
            self.tree[i] += delta;
            i += i & i.wrapping_neg();
        }
    }

    fn prefix_sum(&self, idx: usize) -> i64 {
        let mut sum = 0;
        let mut i = idx + 1;
        while i > 0 {
            sum += self.tree[i];
            i -= i & i.wrapping_neg();
        }
        sum
    }
}

/// Coordinate compress: map values to [0, k-1] preserving relative order.
fn coordinate_compress(arr: &[i64]) -> Vec<usize> {
    let mut sorted: Vec<i64> = arr.to_vec();
    sorted.sort();
    sorted.dedup();

    arr.iter()
        .map(|&x| sorted.binary_search(&x).unwrap())
        .collect()
}

fn count_inversions(arr: &[i64]) -> i64 {
    let compressed = coordinate_compress(arr);
    let max_val = compressed.iter().copied().max().unwrap_or(0);
    let mut bit = FenwickTree::new(max_val + 1);
    let mut inversions = 0i64;

    // Process from right to left
    for &val in compressed.iter().rev() {
        // Count elements already in BIT that are smaller than val
        if val > 0 {
            inversions += bit.prefix_sum(val - 1);
        }
        bit.update(val, 1);
    }

    inversions
}

fn main() {
    assert_eq!(count_inversions(&[3, 1, 2, 5, 4]), 3);
    assert_eq!(count_inversions(&[5, 4, 3, 2, 1]), 10);
    assert_eq!(count_inversions(&[1, 2, 3, 4, 5]), 0);
    assert_eq!(count_inversions(&[2, 1, 3, 1, 2]), 4);

    println!("all inversion counts correct");
}
```
</details>

### Exercise 3: Number of Elements Less Than X in a Range

Given an array, answer queries of the form: "How many elements in `a[l..=r]` are strictly less than `x`?"

**Approach:** Use offline processing. Sort queries by `x`, sort elements by value, and sweep. Alternatively, use a BIT with coordinate compression: for each query, count elements already inserted into the BIT up to position `x-1`.

For the online version (all queries answered independently), use a persistent BIT or merge sort tree. Here we implement the simpler offline approach.

**Input:**
```
array = [4, 2, 7, 1, 5, 3, 6]
queries:
  (0, 6, 4) -> elements < 4 in entire array: {2, 1, 3} -> 3
  (1, 4, 5) -> elements < 5 in a[1..=4]: {2, 1} from {2, 7, 1, 5} -> 2
  (2, 5, 6) -> elements < 6 in a[2..=5]: {1, 5, 3} from {7, 1, 5, 3} -> 3
```

<details>
<summary>Solution</summary>

```rust
struct FenwickTree {
    tree: Vec<i64>,
    n: usize,
}

impl FenwickTree {
    fn new(n: usize) -> Self {
        Self {
            tree: vec![0; n + 1],
            n,
        }
    }

    fn update(&mut self, idx: usize, delta: i64) {
        let mut i = idx + 1;
        while i <= self.n {
            self.tree[i] += delta;
            i += i & i.wrapping_neg();
        }
    }

    fn prefix_sum(&self, idx: usize) -> i64 {
        let mut sum = 0;
        let mut i = idx + 1;
        while i > 0 {
            sum += self.tree[i];
            i -= i & i.wrapping_neg();
        }
        sum
    }

    fn range_sum(&self, left: usize, right: usize) -> i64 {
        if left == 0 {
            self.prefix_sum(right)
        } else {
            self.prefix_sum(right) - self.prefix_sum(left - 1)
        }
    }
}

/// Query: how many elements in a[l..=r] are strictly less than x?
struct Query {
    left: usize,
    right: usize,
    x: i64,
    original_index: usize,
}

fn solve(arr: &[i64], queries: &[(usize, usize, i64)]) -> Vec<i64> {
    let n = arr.len();

    // Create (value, original_index) pairs, sorted by value
    let mut elements: Vec<(i64, usize)> = arr.iter().copied().enumerate().map(|(i, v)| (v, i)).collect();
    elements.sort();

    // Create queries sorted by x
    let mut qs: Vec<Query> = queries
        .iter()
        .enumerate()
        .map(|(idx, &(l, r, x))| Query {
            left: l,
            right: r,
            x,
            original_index: idx,
        })
        .collect();
    qs.sort_by_key(|q| q.x);

    let mut bit = FenwickTree::new(n);
    let mut results = vec![0i64; queries.len()];
    let mut elem_idx = 0;

    for q in &qs {
        // Insert all elements with value < q.x
        while elem_idx < elements.len() && elements[elem_idx].0 < q.x {
            let pos = elements[elem_idx].1;
            bit.update(pos, 1);
            elem_idx += 1;
        }
        results[q.original_index] = bit.range_sum(q.left, q.right);
    }

    results
}

fn main() {
    let arr = vec![4, 2, 7, 1, 5, 3, 6];
    let queries = vec![
        (0, 6, 4), // 3
        (1, 4, 5), // 2
        (2, 5, 6), // 3
    ];

    let results = solve(&arr, &queries);
    assert_eq!(results, vec![3, 2, 3]);
    println!("all range-count queries correct: {:?}", results);
}
```
</details>

### Exercise 4: 2D Range Sum with Updates

Given an `R x C` matrix (initially all zeros), process operations:
- `Update(r, c, delta)`: add `delta` to cell (r, c)
- `Query(r1, c1, r2, c2)`: return the sum of the submatrix

**Input:**
```
R=4, C=4
Update(0, 0, 5)
Update(1, 1, 3)
Update(2, 2, 7)
Update(3, 3, 1)
Query(0, 0, 3, 3)  -> 16
Query(1, 1, 2, 2)  -> 10
Update(1, 1, -3)   -> cell (1,1) becomes 0
Query(1, 1, 2, 2)  -> 7
```

<details>
<summary>Solution</summary>

```rust
struct FenwickTree2D {
    tree: Vec<Vec<i64>>,
    rows: usize,
    cols: usize,
}

impl FenwickTree2D {
    fn new(rows: usize, cols: usize) -> Self {
        Self {
            tree: vec![vec![0i64; cols + 1]; rows + 1],
            rows,
            cols,
        }
    }

    fn update(&mut self, row: usize, col: usize, delta: i64) {
        let mut r = row + 1;
        while r <= self.rows {
            let mut c = col + 1;
            while c <= self.cols {
                self.tree[r][c] += delta;
                c += c & c.wrapping_neg();
            }
            r += r & r.wrapping_neg();
        }
    }

    fn prefix_sum(&self, row: usize, col: usize) -> i64 {
        let mut sum = 0i64;
        let mut r = row + 1;
        while r > 0 {
            let mut c = col + 1;
            while c > 0 {
                sum += self.tree[r][c];
                c -= c & c.wrapping_neg();
            }
            r -= r & r.wrapping_neg();
        }
        sum
    }

    fn range_sum(&self, r1: usize, c1: usize, r2: usize, c2: usize) -> i64 {
        let mut result = self.prefix_sum(r2, c2);
        if r1 > 0 {
            result -= self.prefix_sum(r1 - 1, c2);
        }
        if c1 > 0 {
            result -= self.prefix_sum(r2, c1 - 1);
        }
        if r1 > 0 && c1 > 0 {
            result += self.prefix_sum(r1 - 1, c1 - 1);
        }
        result
    }
}

fn main() {
    let mut bit2d = FenwickTree2D::new(4, 4);

    bit2d.update(0, 0, 5);
    bit2d.update(1, 1, 3);
    bit2d.update(2, 2, 7);
    bit2d.update(3, 3, 1);

    assert_eq!(bit2d.range_sum(0, 0, 3, 3), 16);
    assert_eq!(bit2d.range_sum(1, 1, 2, 2), 10);

    bit2d.update(1, 1, -3);
    assert_eq!(bit2d.range_sum(1, 1, 2, 2), 7);

    println!("all 2D queries correct");
}
```
</details>

### Exercise 5: K-th Smallest Element (Order Statistics)

Given a dynamic set of integers (insert and delete), answer "what is the k-th smallest element?" using a Fenwick Tree as a frequency array.

**Approach:** Maintain a BIT where `bit.update(x, 1)` means "insert x" and `bit.update(x, -1)` means "delete x". The prefix sum `bit.prefix_sum(x)` gives the count of elements <= x. To find the k-th smallest, binary search on the BIT: find the smallest `x` such that `prefix_sum(x) >= k`.

The BIT supports a faster O(log n) search by walking down the tree instead of binary searching:

```
fn find_kth(bit, k):
    pos = 0
    remaining = k
    for bit_pos in (log2(n)..=0):
        next = pos + (1 << bit_pos)
        if next <= n and tree[next] < remaining:
            remaining -= tree[next]
            pos = next
    return pos  // 0-based index of the k-th smallest
```

**Input:**
```
Insert: 3, 1, 4, 1, 5, 9, 2, 6
Find 1st smallest -> 1
Find 3rd smallest -> 2
Find 5th smallest -> 4
Delete 1 (one copy)
Find 3rd smallest -> 3
```

<details>
<summary>Solution</summary>

```rust
struct OrderStatisticsBIT {
    tree: Vec<i64>,
    n: usize,
}

impl OrderStatisticsBIT {
    fn new(max_value: usize) -> Self {
        Self {
            tree: vec![0; max_value + 1],
            n: max_value,
        }
    }

    fn update(&mut self, val: usize, delta: i64) {
        let mut i = val + 1;
        while i <= self.n {
            self.tree[i] += delta;
            i += i & i.wrapping_neg();
        }
    }

    fn insert(&mut self, val: usize) {
        self.update(val, 1);
    }

    fn delete(&mut self, val: usize) {
        self.update(val, -1);
    }

    /// Find the k-th smallest element (1-indexed k).
    /// Uses O(log n) tree descent instead of binary search.
    fn kth_smallest(&self, k: i64) -> usize {
        let mut pos = 0usize;
        let mut remaining = k;
        let mut bit_mask = 1;
        while bit_mask <= self.n {
            bit_mask <<= 1;
        }
        bit_mask >>= 1;

        while bit_mask > 0 {
            let next = pos + bit_mask;
            if next <= self.n && self.tree[next] < remaining {
                remaining -= self.tree[next];
                pos = next;
            }
            bit_mask >>= 1;
        }

        pos // 0-based value
    }
}

fn main() {
    let mut os = OrderStatisticsBIT::new(10);

    // Insert: 3, 1, 4, 1, 5, 9, 2, 6
    for &v in &[3, 1, 4, 1, 5, 9, 2, 6] {
        os.insert(v);
    }

    // Sorted: [1, 1, 2, 3, 4, 5, 6, 9]
    assert_eq!(os.kth_smallest(1), 1);
    assert_eq!(os.kth_smallest(3), 2);
    assert_eq!(os.kth_smallest(5), 4);

    // Delete one copy of 1
    os.delete(1);
    // Sorted: [1, 2, 3, 4, 5, 6, 9]
    assert_eq!(os.kth_smallest(3), 3);
    assert_eq!(os.kth_smallest(1), 1);
    assert_eq!(os.kth_smallest(7), 9);

    println!("all order statistics queries correct");
}
```
</details>

---

## Common Mistakes

1. **Off-by-one with 1-based indexing.** The BIT uses 1-based indexing internally. Forgetting to add 1 when converting from 0-based user input causes silent incorrect results, not panics.

2. **Using `i & -i` on unsigned types.** In Rust, negating a `usize` is a compilation error. Use `i & i.wrapping_neg()` instead.

3. **Not handling the `left == 0` case in `range_sum`.** Computing `prefix_sum(left - 1)` when `left == 0` causes underflow on unsigned types. Always special-case `left == 0`.

4. **Forgetting to track current values for `set` operations.** The BIT stores cumulative data, not individual elements. To set `a[i] = v`, you need the delta `v - current[i]`, which means maintaining a shadow array of current values.

5. **Building from slice in O(n log n) instead of O(n).** Calling `update` for each element is O(n log n). The O(n) build propagates each `tree[i]` to its parent `tree[i + (i & i.wrapping_neg())]` in a single pass.

---

## Verification

```bash
cargo new fenwick-tree-lab && cd fenwick-tree-lab
```

Paste any solution into `src/main.rs` and run:

```bash
cargo run
```

For competitive programming practice, test with large inputs:

```rust
fn stress_test() {
    use std::time::Instant;
    let n = 1_000_000;
    let mut bit = FenwickTree::new(n);

    let start = Instant::now();
    for i in 0..n {
        bit.update(i, (i as i64) % 100);
    }
    println!("1M updates: {:?}", start.elapsed());

    let start = Instant::now();
    let mut sum = 0i64;
    for i in 0..n {
        sum += bit.prefix_sum(i);
    }
    println!("1M queries: {:?} (sum: {sum})", start.elapsed());
}
```

Expected: under 200ms for 1 million operations.

---

## What You Learned

- The Fenwick Tree is a compact, cache-friendly data structure that uses bit manipulation to decompose indices into power-of-2 ranges, achieving O(log n) prefix queries and point updates.
- The `i & i.wrapping_neg()` operation (lowest set bit) is the engine that drives both update and query traversal paths through the tree.
- The dual BIT technique (storing differences) inverts the operation: range updates become O(log n) and point queries remain O(log n).
- Using two BITs together enables both range updates and range queries simultaneously.
- The 2D extension multiplies the logarithmic factors: O(log(r) * log(c)) per operation.
- Fenwick Trees as frequency arrays support order statistics (k-th smallest) with an efficient O(log n) tree descent.
- Coordinate compression maps arbitrary values to a compact `[0, k)` range, enabling BIT-based solutions for problems with large value domains.

## Resources

- [Fenwick Tree on CP-Algorithms](https://cp-algorithms.com/data_structures/fenwick.html)
- [TopCoder: Binary Indexed Trees](https://www.topcoder.com/thrive/articles/Binary%20Indexed%20Trees)
- [Peter Fenwick's original paper (1994)](https://citeseerx.ist.psu.edu/viewdoc/summary?doi=10.1.1.14.8917)
- [Competitive Programmer's Handbook, Chapter 9](https://cses.fi/book/book.pdf)
