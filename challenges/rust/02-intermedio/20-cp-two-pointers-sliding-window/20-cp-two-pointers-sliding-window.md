# 20. CP: Two Pointers and Sliding Window

**Difficulty**: Intermedio

## Prerequisites

- Ownership, borrowing, and slices in Rust
- Iterators and `enumerate()`
- `HashMap` and `HashSet` basics
- Reading from stdin / writing to stdout

## Learning Objectives

1. Apply the two-pointer technique on sorted and unsorted data
2. Implement fixed-size and variable-size sliding windows
3. Recognize when a problem can be reduced to a two-pointer or sliding-window pattern
4. Write competitive-programming-style I/O in Rust efficiently
5. Distinguish index-based vs iterator-based two-pointer idioms in Rust

---

## Competitive Programming I/O in Rust

Before diving into the problems, here is the standard template used throughout these
exercises for fast I/O:

```rust
use std::io::{self, Read, Write, BufWriter};

fn main() {
    // Read all input at once (fast)
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let mut iter = input.split_whitespace();

    // Macro to parse next token
    macro_rules! next {
        ($t:ty) => {
            iter.next().unwrap().parse::<$t>().unwrap()
        };
    }

    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    // --- solve here ---
    let n: usize = next!(usize);
    let a: Vec<i64> = (0..n).map(|_| next!(i64)).collect();

    writeln!(out, "{}", a.len()).unwrap();
}
```

**Why `BufWriter`?** Each `println!` flushes. Wrapping stdout in a `BufWriter` batches
writes and can be 10-100x faster on large output.

---

## Concepts

### Two Pointers

The two-pointer technique uses two indices (or references) that move through a data
structure, typically an array or string, to solve problems in O(n) instead of O(n^2).

```
Opposite direction (sorted array):
  [1, 2, 4, 7, 11, 15]
   ^                 ^
   L                 R
   ------>     <------

  If arr[L] + arr[R] < target  => move L right  (need bigger sum)
  If arr[L] + arr[R] > target  => move R left   (need smaller sum)
  If arr[L] + arr[R] == target => found!
```

```
Same direction (fast/slow):
  [a, b, c, d, e, f, g]
   ^  ^
   S  F
   -------->
   -------->

  Slow pointer trails; fast pointer scouts ahead.
  Used for: removing duplicates, linked list cycles, partitioning.
```

### Sliding Window

A sliding window maintains a contiguous sub-range `[left..right)` and expands or
contracts it while maintaining some invariant.

```
Fixed-size window (size k=3):

  arr: [2, 1, 5, 1, 3, 2]
        [-----]                 sum = 8
           [-----]              sum = 7
              [-----]           sum = 9  <-- max
                 [-----]        sum = 6

Variable-size window:

  arr: [2, 3, 1, 2, 4, 3]   target_sum = 7
        [------->           expand until sum >= 7
        [--------]          sum = 8 >= 7, record len=4
           [-----]          shrink from left, sum = 6 < 7
           [--------]       expand, sum = 10 >= 7, record len=4
              [-----]       shrink, sum = 7, record len=3
              ...
```

### Index-Based vs Iterator-Based in Rust

```rust
// Index-based: classic two-pointer with indices
let (mut l, mut r) = (0, nums.len() - 1);
while l < r {
    // use nums[l], nums[r]
}

// Iterator-based: using .iter().enumerate()
// Good for read-only scanning, not always for shrinking windows
let mut window_sum = 0i64;
let mut left = 0;
for (right, &val) in nums.iter().enumerate() {
    window_sum += val;
    while window_sum > target {
        window_sum -= nums[left];
        left += 1;
    }
}
```

Both are idiomatic Rust. Index-based is more common in competitive programming because
it maps directly to the pointer manipulation logic.

---

## Problem 1: Pair Sum in Sorted Array

### Statement

Given a **sorted** array of `n` integers and a target value `t`, find whether there
exist two distinct elements whose sum equals `t`. If yes, print their **1-based** indices.
If no such pair exists, print `-1`.

### Input Format

```
n t
a_1 a_2 ... a_n
```

### Output Format

If a pair exists, print `i j` (1-based, `i < j`) on a single line. If multiple pairs
exist, print the one with the smallest `i`. If no pair exists, print `-1`.

### Constraints

- 2 <= n <= 2 * 10^5
- -10^9 <= a_i <= 10^9
- -10^9 <= t <= 10^9
- The array is sorted in non-decreasing order.

### Examples

```
Input:
6 9
1 2 4 5 7 11

Output:
2 5
```

Explanation: `a[2] + a[5] = 2 + 7 = 9`.

```
Input:
3 100
1 2 3

Output:
-1
```

### Hints

1. Start with `left = 0` and `right = n - 1`.
2. If the sum is too small, increment `left`. If too large, decrement `right`.
3. Time complexity: O(n). Space: O(1).

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
    let t: i64 = next!(i64);
    let a: Vec<i64> = (0..n).map(|_| next!(i64)).collect();

    // TODO: Initialize two pointers `left` and `right`

    // TODO: Loop while left < right
    //   - Compute sum = a[left] + a[right]
    //   - If sum == t => print (left+1, right+1) and return
    //   - If sum < t  => move left pointer right
    //   - If sum > t  => move right pointer left

    // TODO: If loop ends without finding, print -1
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
    let t: i64 = next!(i64);
    let a: Vec<i64> = (0..n).map(|_| next!(i64)).collect();

    let mut left: usize = 0;
    let mut right: usize = n - 1;

    while left < right {
        let sum = a[left] + a[right];
        if sum == t {
            writeln!(out, "{} {}", left + 1, right + 1).unwrap();
            return;
        } else if sum < t {
            left += 1;
        } else {
            right -= 1;
        }
    }

    writeln!(out, "-1").unwrap();
}
```

**Complexity**: O(n) time, O(1) extra space (beyond input storage).

</details>

---

## Problem 2: Longest Substring Without Repeating Characters

### Statement

Given a string `s` consisting of printable ASCII characters, find the length of the
longest substring that contains no repeated characters.

### Input Format

```
s
```

A single line containing the string `s`.

### Output Format

A single integer: the length of the longest substring without repeating characters.

### Constraints

- 1 <= |s| <= 10^5
- `s` consists of printable ASCII characters (codes 32-126).

### Examples

```
Input:
abcabcbb

Output:
3
```

Explanation: `"abc"` is the longest substring without repeats.

```
Input:
bbbbb

Output:
1
```

```
Input:
pwwkew

Output:
3
```

Explanation: `"wke"` is the answer. Note that `"pwke"` is a subsequence, not a substring.

### Hints

1. Use a sliding window `[left, right)` that maintains the invariant: no duplicates inside.
2. Track character positions with a `HashMap<u8, usize>` (byte -> last seen index).
3. When you encounter a character already in the window, jump `left` forward past its
   previous occurrence.

### Solution Template

```rust
use std::io::{self, Read, Write, BufWriter};
use std::collections::HashMap;

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let s = input.trim().as_bytes();

    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let mut last_seen: HashMap<u8, usize> = HashMap::new();
    let mut best = 0usize;
    let mut left = 0usize;

    for right in 0..s.len() {
        let ch = s[right];

        // TODO: If `ch` was seen at index `prev` and `prev >= left`,
        //       move `left` to `prev + 1`

        // TODO: Update `last_seen` for `ch`

        // TODO: Update `best` with the current window length
    }

    writeln!(out, "{}", best).unwrap();
}
```

<details>
<summary>Solution</summary>

```rust
use std::io::{self, Read, Write, BufWriter};
use std::collections::HashMap;

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let s = input.trim().as_bytes();

    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let mut last_seen: HashMap<u8, usize> = HashMap::new();
    let mut best = 0usize;
    let mut left = 0usize;

    for right in 0..s.len() {
        let ch = s[right];

        if let Some(&prev) = last_seen.get(&ch) {
            if prev >= left {
                left = prev + 1;
            }
        }

        last_seen.insert(ch, right);

        let window_len = right - left + 1;
        if window_len > best {
            best = window_len;
        }
    }

    writeln!(out, "{}", best).unwrap();
}
```

**Rust note**: Working with `as_bytes()` avoids UTF-8 multi-byte issues and is much
faster for ASCII-only competitive programming input.

**Complexity**: O(n) time, O(min(n, 128)) space for the HashMap.

</details>

---

## Problem 3: Minimum Window Substring

### Statement

Given two strings `s` and `p`, find the shortest contiguous substring of `s` that
contains every character of `p` (including duplicates). If no such window exists, print
an empty line.

### Input Format

```
s
p
```

Two lines, each containing a string.

### Output Format

The shortest window substring. If there are multiple windows of the same length, print
the one that appears first (leftmost). If no valid window exists, print an empty line.

### Constraints

- 1 <= |s|, |p| <= 10^5
- Both strings consist of uppercase and lowercase English letters.

### Examples

```
Input:
ADOBECODEBANC
ABC

Output:
BANC
```

```
Input:
a
a

Output:
a
```

```
Input:
a
aa

Output:

```

(Empty output because `s` has only one `a` but `p` requires two.)

### Hints

1. Count character frequencies in `p` (`need` map).
2. Expand the window by moving `right`. Track how many characters are "satisfied".
3. Once all characters are satisfied, try shrinking from `left`.
4. Keep track of the best (shortest) valid window found.

### Solution Template

```rust
use std::io::{self, Read, Write, BufWriter};

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let mut lines = input.lines();
    let s: &[u8] = lines.next().unwrap().trim().as_bytes();
    let p: &[u8] = lines.next().unwrap().trim().as_bytes();

    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    // Frequency count for p
    let mut need = [0i32; 128];
    for &ch in p {
        need[ch as usize] += 1;
    }

    // How many distinct characters still need to be fully covered
    let required: usize = need.iter().filter(|&&x| x > 0).count();

    let mut window = [0i32; 128];
    let mut formed = 0usize; // how many distinct chars are fully satisfied
    let mut left = 0usize;

    let mut best_len = usize::MAX;
    let mut best_left = 0usize;

    for right in 0..s.len() {
        let ch = s[right] as usize;
        window[ch] += 1;

        // TODO: If window[ch] == need[ch] and need[ch] > 0, increment `formed`

        // TODO: While `formed == required`:
        //   - Update best_len and best_left if current window is shorter
        //   - Shrink window from left:
        //       - Decrement window count for s[left]
        //       - If count dropped below need, decrement `formed`
        //       - Increment left
    }

    if best_len == usize::MAX {
        writeln!(out).unwrap();
    } else {
        let answer = std::str::from_utf8(&s[best_left..best_left + best_len]).unwrap();
        writeln!(out, "{}", answer).unwrap();
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
    let mut lines = input.lines();
    let s: &[u8] = lines.next().unwrap().trim().as_bytes();
    let p: &[u8] = lines.next().unwrap().trim().as_bytes();

    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let mut need = [0i32; 128];
    for &ch in p {
        need[ch as usize] += 1;
    }

    let required: usize = need.iter().filter(|&&x| x > 0).count();

    let mut window = [0i32; 128];
    let mut formed = 0usize;
    let mut left = 0usize;

    let mut best_len = usize::MAX;
    let mut best_left = 0usize;

    for right in 0..s.len() {
        let ch = s[right] as usize;
        window[ch] += 1;

        if need[ch] > 0 && window[ch] == need[ch] {
            formed += 1;
        }

        while formed == required {
            let current_len = right - left + 1;
            if current_len < best_len {
                best_len = current_len;
                best_left = left;
            }

            let left_ch = s[left] as usize;
            window[left_ch] -= 1;
            if need[left_ch] > 0 && window[left_ch] < need[left_ch] {
                formed -= 1;
            }
            left += 1;
        }
    }

    if best_len == usize::MAX {
        writeln!(out).unwrap();
    } else {
        let answer = std::str::from_utf8(&s[best_left..best_left + best_len]).unwrap();
        writeln!(out, "{}", answer).unwrap();
    }
}
```

**Key insight**: Using fixed-size arrays `[0i32; 128]` instead of `HashMap` is a common
competitive programming trick for ASCII characters -- it's much faster.

**Complexity**: O(|s| + |p|) time, O(1) space (the arrays are constant size).

</details>

---

## Problem 4: Container With Most Water

### Statement

You are given `n` non-negative integers `h_1, h_2, ..., h_n` where each represents a
vertical line at position `i` with height `h_i`. Find two lines that, together with the
x-axis, form a container that holds the most water.

Return the maximum amount of water the container can store.

```
  |              |
  |  |           |
  |  |     |     |
  |  |     |  |  |
  |  |  |  |  |  |
  +-----------------
  1  8  6  2  5  4  8  3  7
     ^                    ^
     Left              Right
     area = min(8,7) * (8-1) = 49
```

### Input Format

```
n
h_1 h_2 ... h_n
```

### Output Format

A single integer: the maximum area.

### Constraints

- 2 <= n <= 10^5
- 0 <= h_i <= 10^4

### Examples

```
Input:
9
1 8 6 2 5 4 8 3 7

Output:
49
```

```
Input:
2
1 1

Output:
1
```

### Hints

1. Use two pointers starting at the leftmost and rightmost positions.
2. The area is `min(h[left], h[right]) * (right - left)`.
3. Always move the pointer pointing to the **shorter** line -- the taller line cannot
   possibly benefit from moving inward.

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
    let h: Vec<i64> = (0..n).map(|_| next!(i64)).collect();

    let mut left: usize = 0;
    let mut right: usize = n - 1;
    let mut max_area: i64 = 0;

    while left < right {
        // TODO: Compute the area between left and right
        // TODO: Update max_area
        // TODO: Move the pointer at the shorter line inward
    }

    writeln!(out, "{}", max_area).unwrap();
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
    let h: Vec<i64> = (0..n).map(|_| next!(i64)).collect();

    let mut left: usize = 0;
    let mut right: usize = n - 1;
    let mut max_area: i64 = 0;

    while left < right {
        let width = (right - left) as i64;
        let height = h[left].min(h[right]);
        let area = width * height;
        if area > max_area {
            max_area = area;
        }

        if h[left] < h[right] {
            left += 1;
        } else {
            right -= 1;
        }
    }

    writeln!(out, "{}", max_area).unwrap();
}
```

**Why this works**: By always moving the shorter pointer, we never miss the optimal pair.
If we moved the taller pointer, the width would decrease and the height could only stay
the same or decrease, so the area can only get smaller.

**Complexity**: O(n) time, O(1) extra space.

</details>

---

## Problem 5: Subarray With Given Sum

### Statement

Given an array of `n` **positive** integers and a target sum `t`, find the length of the
shortest contiguous subarray whose sum is **greater than or equal to** `t`. If no such
subarray exists, print `0`.

### Input Format

```
n t
a_1 a_2 ... a_n
```

### Output Format

A single integer: the minimum length of a subarray with sum >= `t`, or `0` if impossible.

### Constraints

- 1 <= n <= 10^5
- 1 <= t <= 10^9
- 1 <= a_i <= 10^4

### Examples

```
Input:
6 7
2 3 1 2 4 3

Output:
2
```

Explanation: The subarray `[4, 3]` has sum 7 >= 7 and length 2.

```
Input:
3 11
1 1 1

Output:
0
```

### Hints

1. Because all values are positive, a sliding window works: expanding always increases
   the sum, shrinking always decreases it.
2. Expand `right` until `sum >= t`, then shrink `left` while maintaining `sum >= t`.
3. Track the minimum window length seen.

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
    let t: i64 = next!(i64);
    let a: Vec<i64> = (0..n).map(|_| next!(i64)).collect();

    let mut left = 0usize;
    let mut current_sum: i64 = 0;
    let mut min_len = usize::MAX;

    for right in 0..n {
        // TODO: Add a[right] to current_sum

        // TODO: While current_sum >= t:
        //   - Update min_len with (right - left + 1) if smaller
        //   - Subtract a[left] from current_sum
        //   - Increment left
    }

    // TODO: Print min_len if it was updated, otherwise 0
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
    let t: i64 = next!(i64);
    let a: Vec<i64> = (0..n).map(|_| next!(i64)).collect();

    let mut left = 0usize;
    let mut current_sum: i64 = 0;
    let mut min_len = usize::MAX;

    for right in 0..n {
        current_sum += a[right];

        while current_sum >= t {
            let window_len = right - left + 1;
            if window_len < min_len {
                min_len = window_len;
            }
            current_sum -= a[left];
            left += 1;
        }
    }

    if min_len == usize::MAX {
        writeln!(out, "0").unwrap();
    } else {
        writeln!(out, "{}", min_len).unwrap();
    }
}
```

**Important**: This approach only works for **positive** values. If the array can contain
zeroes or negatives, the monotonicity property breaks and you would need a prefix-sum +
binary search approach instead (O(n log n)).

**Complexity**: O(n) time. Each element is added at most once and removed at most once.

</details>

---

## Summary Cheat Sheet

| Pattern              | When to use                                      | Key invariant                 |
|----------------------|--------------------------------------------------|-------------------------------|
| Opposite two-pointer | Sorted array, pair problems                      | `left < right`, move by comp  |
| Same-dir two-pointer | Removing/partitioning in place                   | Slow trails fast              |
| Fixed sliding window | Max/min/sum of subarrays of exact size k          | Window is always size k       |
| Variable window      | Shortest/longest subarray with some property      | Expand right, shrink left     |
| Frequency window     | Substring with character constraints              | Count maps + `formed` counter |

### Common Pitfalls in Rust

- **Off-by-one with `usize`**: Be careful subtracting from `usize` -- if `right` is 0 and
  you do `right - 1`, it wraps around. Use `checked_sub()` or start from 1.
- **Borrow checker with slices**: If you need mutable access to two elements at different
  indices, use `split_at_mut()` or index with the raw array.
- **Byte vs char indexing**: For ASCII problems, `s.as_bytes()` is faster and avoids
  multi-byte issues. For Unicode, use `s.chars()` but indices become tricky.
