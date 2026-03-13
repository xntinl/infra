# 21. CP: Binary Search Patterns

**Difficulty**: Intermedio

## Prerequisites

- Slices, `Vec<T>`, and ownership in Rust
- Closures and `Fn` trait basics
- Integer arithmetic and overflow awareness
- Competitive programming I/O patterns (see exercise 20)

## Learning Objectives

1. Implement classic binary search from scratch and understand its edge cases
2. Use Rust's `partition_point`, `binary_search`, and related slice methods
3. Apply "binary search on the answer" to optimization problems
4. Recognize the monotonicity property that enables binary search
5. Handle integer overflow and off-by-one errors in search bounds

---

## Concepts

### The Binary Search Principle

Binary search works whenever there is a **monotonic predicate** -- a function `f(x)` that
is `false` for all values below some threshold and `true` for all values above it (or
vice versa). We find the boundary in O(log n).

```
Index:     0   1   2   3   4   5   6   7   8   9
Value:     2   5   8  12  16  23  38  56  72  91
                          ^
                        target = 16

Step 1:  lo=0, hi=9, mid=4  =>  arr[4]=16 == target  =>  FOUND!

If target = 20:
Step 1:  lo=0, hi=9, mid=4  =>  16 < 20  =>  lo=5
Step 2:  lo=5, hi=9, mid=7  =>  56 > 20  =>  hi=6
Step 3:  lo=5, hi=6, mid=5  =>  23 > 20  =>  hi=4
Step 4:  lo=5, hi=4  =>  lo > hi  =>  NOT FOUND
```

### Lower Bound / Upper Bound / partition_point

Rust's `partition_point` is the most versatile tool. It takes a predicate and returns the
first index where the predicate becomes `false`:

```
arr:        [1, 3, 3, 3, 5, 7]
pred(x<3):   T  F  F  F  F  F   => partition_point = 1  (lower_bound of 3)
pred(x<=3):  T  T  T  T  F  F   => partition_point = 4  (upper_bound of 3)
pred(x<5):   T  T  T  T  F  F   => partition_point = 4  (lower_bound of 5)
```

```rust
let arr = vec![1, 3, 3, 3, 5, 7];

// Lower bound: first index where arr[i] >= target
let lb = arr.partition_point(|&x| x < 3);     // 1

// Upper bound: first index where arr[i] > target
let ub = arr.partition_point(|&x| x <= 3);    // 4

// Count of 3s:
let count = ub - lb; // 3
```

### Binary Search on the Answer

When the problem asks "what is the minimum/maximum value X such that some condition
holds?", you can often binary search on X itself:

```
Problem: Ship packages with capacity C. Minimize C.

capacity:    1    2    3    ...   15   16   17   ...
feasible?:   no   no   no  ...   no   yes  yes  ...
                                       ^
                                 answer = 16

Binary search on the answer space [lo, hi]:
  lo = max(single_package_weight)
  hi = sum(all_weights)
  Find minimum C where feasible(C) == true
```

### Rust Standard Library Methods

```rust
let v = vec![1, 3, 5, 7, 9];

// Exact match: returns Result<usize, usize>
//   Ok(index)  if found
//   Err(index) where it would be inserted
v.binary_search(&5);        // Ok(2)
v.binary_search(&6);        // Err(3)

// With custom comparator
v.binary_search_by(|probe| probe.cmp(&5));

// By key extraction
let tuples = vec![(1, 'a'), (3, 'b'), (5, 'c')];
tuples.binary_search_by_key(&3, |&(k, _)| k);  // Ok(1)

// partition_point: first index where predicate is false
v.partition_point(|&x| x < 5);  // 2

// sort_unstable is faster (doesn't preserve order of equal elements)
let mut v = vec![3, 1, 4, 1, 5];
v.sort_unstable();  // [1, 1, 3, 4, 5]
```

---

## Problem 1: Classic Binary Search

### Statement

Given a sorted array of `n` distinct integers and `q` queries, for each query value `x`,
determine if `x` exists in the array. If yes, print its 1-based index. If not, print `-1`.

### Input Format

```
n q
a_1 a_2 ... a_n
x_1 x_2 ... x_q
```

### Output Format

For each query, print the result on a separate line.

### Constraints

- 1 <= n, q <= 2 * 10^5
- -10^9 <= a_i, x_i <= 10^9
- Array is sorted and all elements are distinct.

### Examples

```
Input:
5 3
1 3 5 7 9
5 2 9

Output:
3
-1
5
```

### Hints

1. Use Rust's `binary_search()` which returns `Ok(index)` or `Err(_)`.
2. Alternatively, implement it yourself with `lo` and `hi`.
3. Be mindful of 1-based indexing in the output.

### Solution Template

```rust
use std::io::{self, Read, Write, BufWriter};

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let mut iter = input.split_whitespace();
    macro_rules! next {
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() };
    }
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let n: usize = next!(usize);
    let q: usize = next!(usize);
    let a: Vec<i64> = (0..n).map(|_| next!(i64)).collect();

    for _ in 0..q {
        let x: i64 = next!(i64);

        // TODO: Use a.binary_search(&x) and match on Ok/Err
        // Print 1-based index if found, -1 otherwise
    }
}
```

<details>
<summary>Solution</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let mut iter = input.split_whitespace();
    macro_rules! next {
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() };
    }
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let n: usize = next!(usize);
    let q: usize = next!(usize);
    let a: Vec<i64> = (0..n).map(|_| next!(i64)).collect();

    for _ in 0..q {
        let x: i64 = next!(i64);

        match a.binary_search(&x) {
            Ok(idx) => writeln!(out, "{}", idx + 1).unwrap(),
            Err(_) => writeln!(out, "-1").unwrap(),
        }
    }
}
```

**Manual implementation** (for understanding):

```rust
fn binary_search_manual(a: &[i64], x: i64) -> Option<usize> {
    let (mut lo, mut hi) = (0i64, a.len() as i64 - 1);
    while lo <= hi {
        let mid = lo + (hi - lo) / 2;  // avoid overflow
        match a[mid as usize].cmp(&x) {
            std::cmp::Ordering::Equal => return Some(mid as usize),
            std::cmp::Ordering::Less => lo = mid + 1,
            std::cmp::Ordering::Greater => hi = mid - 1,
        }
    }
    None
}
```

**Complexity**: O(log n) per query, O(n + q log n) total.

</details>

---

## Problem 2: Count of a Value (Lower/Upper Bound)

### Statement

Given a sorted array of `n` integers (may contain duplicates) and `q` queries, for each
query value `x`, print how many times `x` appears in the array.

### Input Format

```
n q
a_1 a_2 ... a_n
x_1 x_2 ... x_q
```

### Output Format

For each query, print the count on a separate line.

### Constraints

- 1 <= n, q <= 2 * 10^5
- -10^9 <= a_i, x_i <= 10^9
- Array is sorted in non-decreasing order.

### Examples

```
Input:
8 3
1 2 2 2 3 3 5 8
2 3 4

Output:
3
2
0
```

### Hints

1. `count(x) = upper_bound(x) - lower_bound(x)`
2. Use `partition_point` twice: once with `|&v| v < x` (lower bound) and once with
   `|&v| v <= x` (upper bound).
3. This is O(log n) per query.

### Solution Template

```rust
use std::io::{self, Read, Write, BufWriter};

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let mut iter = input.split_whitespace();
    macro_rules! next {
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() };
    }
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let n: usize = next!(usize);
    let q: usize = next!(usize);
    let a: Vec<i64> = (0..n).map(|_| next!(i64)).collect();

    for _ in 0..q {
        let x: i64 = next!(i64);

        // TODO: Find lower_bound using partition_point(|&v| v < x)
        // TODO: Find upper_bound using partition_point(|&v| v <= x)
        // TODO: Print upper_bound - lower_bound
    }
}
```

<details>
<summary>Solution</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let mut iter = input.split_whitespace();
    macro_rules! next {
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() };
    }
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let n: usize = next!(usize);
    let q: usize = next!(usize);
    let a: Vec<i64> = (0..n).map(|_| next!(i64)).collect();

    for _ in 0..q {
        let x: i64 = next!(i64);

        let lower = a.partition_point(|&v| v < x);
        let upper = a.partition_point(|&v| v <= x);
        writeln!(out, "{}", upper - lower).unwrap();
    }
}
```

**Why `partition_point`?** It is Rust's equivalent of C++'s `lower_bound` and
`upper_bound`. The predicate must be `true` for all elements before the partition point
and `false` for all elements from the partition point onward.

**Complexity**: O(log n) per query.

</details>

---

## Problem 3: Search in Rotated Sorted Array

### Statement

A sorted array of `n` distinct integers has been rotated at some pivot. For example,
`[4, 5, 6, 7, 0, 1, 2]` is a rotation of `[0, 1, 2, 4, 5, 6, 7]`.

Given the rotated array and a target `x`, find the index of `x` (0-based). If `x` is
not in the array, print `-1`.

### Input Format

```
n x
a_1 a_2 ... a_n
```

### Output Format

The 0-based index of `x`, or `-1` if not found.

### Constraints

- 1 <= n <= 2 * 10^5
- -10^9 <= a_i, x <= 10^9
- All elements are distinct.

### Examples

```
Input:
7 0
4 5 6 7 0 1 2

Output:
4
```

```
Input:
7 3
4 5 6 7 0 1 2

Output:
-1
```

### Hints

1. At each step of binary search, one half of the array is guaranteed to be sorted.
2. Check which half is sorted, then determine if the target lies in that sorted half.
3. If `a[lo] <= a[mid]`, the left half is sorted. If target is in `[a[lo], a[mid])`,
   search left; otherwise search right.

### Solution Template

```rust
use std::io::{self, Read, Write, BufWriter};

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let mut iter = input.split_whitespace();
    macro_rules! next {
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() };
    }
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let n: usize = next!(usize);
    let x: i64 = next!(i64);
    let a: Vec<i64> = (0..n).map(|_| next!(i64)).collect();

    let mut lo: usize = 0;
    let mut hi: usize = n - 1;
    let mut result: i64 = -1;

    while lo <= hi {
        let mid = lo + (hi - lo) / 2;

        if a[mid] == x {
            result = mid as i64;
            break;
        }

        // TODO: Determine which half is sorted
        // If left half [lo..mid] is sorted:
        //   If x >= a[lo] && x < a[mid] => hi = mid - 1
        //   Else => lo = mid + 1
        // Else (right half [mid..hi] is sorted):
        //   If x > a[mid] && x <= a[hi] => lo = mid + 1
        //   Else => hi = mid - 1

        // CAREFUL: `hi` is usize, so `hi = mid - 1` when mid=0 will overflow!
        // Guard with: if mid == 0 { break; } before decrementing.
    }

    writeln!(out, "{}", result).unwrap();
}
```

<details>
<summary>Solution</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let mut iter = input.split_whitespace();
    macro_rules! next {
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() };
    }
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let n: usize = next!(usize);
    let x: i64 = next!(i64);
    let a: Vec<i64> = (0..n).map(|_| next!(i64)).collect();

    let mut lo: usize = 0;
    let mut hi: usize = n - 1;
    let mut result: i64 = -1;

    while lo <= hi {
        let mid = lo + (hi - lo) / 2;

        if a[mid] == x {
            result = mid as i64;
            break;
        }

        if a[lo] <= a[mid] {
            // Left half is sorted
            if x >= a[lo] && x < a[mid] {
                if mid == 0 { break; }
                hi = mid - 1;
            } else {
                lo = mid + 1;
            }
        } else {
            // Right half is sorted
            if x > a[mid] && x <= a[hi] {
                lo = mid + 1;
            } else {
                if mid == 0 { break; }
                hi = mid - 1;
            }
        }
    }

    writeln!(out, "{}", result).unwrap();
}
```

**Rust pitfall**: Since `lo` and `hi` are `usize`, doing `hi = mid - 1` when `mid == 0`
causes an underflow panic in debug mode (or wraps in release). The guard `if mid == 0 { break; }` prevents this.

**Complexity**: O(log n).

</details>

---

## Problem 4: Minimum Capacity to Ship Packages (Binary Search on Answer)

### Statement

A conveyor belt delivers packages that must be shipped within `d` days. The i-th package
has weight `w_i`. Packages must be shipped **in order** (you cannot reorder them). Each
day, you load packages in order onto a ship until the next package would exceed the
ship's capacity. Find the **minimum** ship capacity that allows all packages to be
shipped within `d` days.

### Input Format

```
n d
w_1 w_2 ... w_n
```

### Output Format

A single integer: the minimum ship capacity.

### Constraints

- 1 <= d <= n <= 5 * 10^4
- 1 <= w_i <= 500

### Examples

```
Input:
10 5
1 2 3 4 5 6 7 8 9 10

Output:
15
```

Explanation: With capacity 15, the loads per day are:
`[1,2,3,4,5]`, `[6,7]`, `[8]`, `[9]`, `[10]` -- 5 days.

```
Input:
3 3
1 1 1

Output:
1
```

### Hints

1. The minimum possible capacity is `max(w_i)` (must fit the heaviest package).
2. The maximum possible capacity is `sum(w_i)` (ship everything in one day).
3. Binary search on capacity `c` in `[lo, hi]`. For each `c`, simulate the greedy
   loading and count how many days are needed.
4. If days_needed <= d, capacity is feasible; try smaller. Otherwise, try larger.

### Solution Template

```rust
use std::io::{self, Read, Write, BufWriter};

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let mut iter = input.split_whitespace();
    macro_rules! next {
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() };
    }
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let n: usize = next!(usize);
    let d: usize = next!(usize);
    let w: Vec<i64> = (0..n).map(|_| next!(i64)).collect();

    // TODO: Compute lo = max element, hi = sum of all elements

    // TODO: Implement `fn feasible(weights: &[i64], capacity: i64, days: usize) -> bool`
    //   Greedily load packages, count days needed.

    // TODO: Binary search on [lo, hi]
    //   while lo < hi {
    //       let mid = lo + (hi - lo) / 2;
    //       if feasible => hi = mid
    //       else => lo = mid + 1
    //   }

    // TODO: Print lo (the answer)
}
```

<details>
<summary>Solution</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

fn feasible(weights: &[i64], capacity: i64, max_days: usize) -> bool {
    let mut days = 1usize;
    let mut current_load: i64 = 0;

    for &w in weights {
        if current_load + w > capacity {
            days += 1;
            current_load = 0;
        }
        current_load += w;
    }

    days <= max_days
}

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let mut iter = input.split_whitespace();
    macro_rules! next {
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() };
    }
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let n: usize = next!(usize);
    let d: usize = next!(usize);
    let w: Vec<i64> = (0..n).map(|_| next!(i64)).collect();

    let mut lo: i64 = *w.iter().max().unwrap();
    let mut hi: i64 = w.iter().sum();

    while lo < hi {
        let mid = lo + (hi - lo) / 2;
        if feasible(&w, mid, d) {
            hi = mid;
        } else {
            lo = mid + 1;
        }
    }

    writeln!(out, "{}", lo).unwrap();
}
```

**Pattern**: "Binary search on the answer" is extremely common in competitive
programming. The key insight is recognizing that `feasible(capacity)` is monotonic:
once a capacity works, all larger capacities also work.

**Complexity**: O(n log S) where S = sum(w_i) - max(w_i).

</details>

---

## Problem 5: Integer Square Root

### Statement

Given a non-negative integer `n`, compute `floor(sqrt(n))` without using floating-point
arithmetic.

### Input Format

```
n
```

### Output Format

A single integer: the largest integer `r` such that `r * r <= n`.

### Constraints

- 0 <= n <= 10^18

### Examples

```
Input:
8

Output:
2
```

Explanation: sqrt(8) = 2.828..., floor is 2.

```
Input:
16

Output:
4
```

```
Input:
0

Output:
0
```

### Hints

1. Binary search on `r` in `[0, min(n, 10^9)]`. (sqrt(10^18) = 10^9)
2. Check if `r * r <= n`. Be careful with overflow: `r * r` can overflow `i64` if `r`
   is around 10^9. Use `u128` or check `r <= n / r` instead.
3. Find the largest `r` such that `r * r <= n`.

### Solution Template

```rust
use std::io::{self, Read, Write, BufWriter};

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let n: u64 = input.trim().parse().unwrap();

    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    if n == 0 {
        writeln!(out, "0").unwrap();
        return;
    }

    // TODO: Set lo = 1, hi = n.min(1_000_000_000)  (or just n.min(3_000_000_000))

    // TODO: Binary search for the largest r where r*r <= n
    //   Use u128 to avoid overflow: (mid as u128) * (mid as u128) <= (n as u128)
    //   If mid*mid <= n => lo = mid (try bigger)
    //   Else => hi = mid - 1 (too big)
    //   CAREFUL with the loop condition and how you compute mid to avoid infinite loop

    // TODO: Print the result
}
```

<details>
<summary>Solution</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let n: u64 = input.trim().parse().unwrap();

    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    if n == 0 {
        writeln!(out, "0").unwrap();
        return;
    }

    let mut lo: u64 = 1;
    let mut hi: u64 = n.min(3_000_000_000); // sqrt(10^18) < 10^9, but safe margin

    while lo < hi {
        // Round mid UP to avoid infinite loop when lo + 1 == hi
        let mid = lo + (hi - lo + 1) / 2;

        if (mid as u128) * (mid as u128) <= n as u128 {
            lo = mid;      // mid is feasible, try bigger
        } else {
            hi = mid - 1;  // mid is too big
        }
    }

    writeln!(out, "{}", lo).unwrap();
}
```

**Critical detail**: When searching for the **last** element satisfying a condition
(as opposed to the first), you must round `mid` **up** (`lo + (hi - lo + 1) / 2`) to
avoid an infinite loop where `lo` never advances.

**Alternative with `partition_point`**:

```rust
// Treat [0..=hi] as the search space
let hi = n.min(3_000_000_000) as usize + 1;
let r = (0..hi).collect::<Vec<_>>(); // impractical for large ranges!
// partition_point works on slices, not ranges, so manual binary search
// is preferred here.
```

For this problem, manual binary search is the right approach.

**Complexity**: O(log n).

</details>

---

## Summary Cheat Sheet

| Rust Method                      | Equivalent (C++)              | Returns                   |
|----------------------------------|-------------------------------|---------------------------|
| `slice.binary_search(&val)`      | `binary_search` (exact)       | `Result<usize, usize>`   |
| `slice.partition_point(\|x\| p)` | `lower_bound` / `upper_bound` | `usize` (first false)    |
| `slice.sort_unstable()`          | `sort`                        | `()` (in-place)          |
| `slice.binary_search_by(cmp)`    | custom binary search          | `Result<usize, usize>`   |
| `slice.binary_search_by_key`     | custom binary search by field | `Result<usize, usize>`   |

### Binary Search Patterns Summary

```
Find exact value:           binary_search(&val)
Find first >= val:          partition_point(|&x| x < val)
Find first > val:           partition_point(|&x| x <= val)
Find last <= val:           partition_point(|&x| x <= val) - 1
Count of val:               pp(|x| x<=val) - pp(|x| x<val)
Search on answer (min):     while lo < hi { mid = lo+(hi-lo)/2; if ok => hi=mid else lo=mid+1 }
Search on answer (max):     while lo < hi { mid = lo+(hi-lo+1)/2; if ok => lo=mid else hi=mid-1 }
```

### Common Pitfalls

- **`usize` underflow**: `hi = mid - 1` when `mid == 0` panics. Use `i64` or guard.
- **Infinite loop**: When searching for the **max** feasible value, round `mid` up.
- **Overflow**: `mid = (lo + hi) / 2` can overflow. Use `lo + (hi - lo) / 2`.
- **Off-by-one in bounds**: Decide if `hi` is inclusive or exclusive and be consistent.
