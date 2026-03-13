# 47. CP: Monotonic Stack and Queue

**Difficulty**: Avanzado

## Prerequisites

- Solid understanding of `Vec`, `VecDeque`, stacks, and queues in Rust
- Familiarity with amortized complexity analysis
- Completed: exercises on iterators, pattern matching, and collections

## Learning Objectives

- Understand the monotonic stack invariant and why it produces amortized O(n) solutions
- Implement the "next greater element" and "next smaller element" patterns
- Solve classic problems: largest rectangle in histogram, trapping rain water, daily temperatures
- Use `VecDeque` as a monotonic deque for sliding window maximum/minimum in O(n)
- Recognize when a problem reduces to a monotonic stack/queue pattern

## Concepts

A monotonic stack is a stack that maintains its elements in either strictly increasing or strictly decreasing order. When a new element arrives that would violate the order, elements are popped until the invariant is restored. This seemingly simple idea solves an entire family of problems in O(n) that would otherwise require O(n^2).

The key insight is that each element is pushed and popped at most once. Even though the inner loop (popping) might process multiple elements for a single push, the total number of pops across all iterations cannot exceed n. This is the amortized O(n) argument.

### Monotonic Increasing Stack

Maintains elements from bottom to top in increasing order. When a new element `x` arrives:
1. Pop all elements greater than or equal to `x` (or greater than `x` for non-strict)
2. Push `x`

Used for: **next smaller element** (to the left or right).

### Monotonic Decreasing Stack

Maintains elements from bottom to top in decreasing order. When a new element `x` arrives:
1. Pop all elements less than or equal to `x`
2. Push `x`

Used for: **next greater element** (to the left or right).

### Why Indices, Not Values

In practice, we push indices onto the stack, not values. This lets us compute distances, widths, and positions -- which is what most problems actually need.

---

## Implementation

### Next Greater Element

For each element, find the next element to its right that is strictly greater. If none exists, the answer is -1.

```rust
/// For each element in `arr`, find the index of the next greater element
/// to its right. Returns -1 (as i32) if no greater element exists.
fn next_greater_element(arr: &[i32]) -> Vec<i32> {
    let n = arr.len();
    let mut result = vec![-1i32; n];
    let mut stack: Vec<usize> = Vec::new(); // stack of indices

    for i in 0..n {
        // Pop all indices whose values are less than arr[i].
        // For each popped index, arr[i] is its "next greater element".
        while let Some(&top) = stack.last() {
            if arr[top] < arr[i] {
                stack.pop();
                result[top] = arr[i];
            } else {
                break;
            }
        }
        stack.push(i);
    }

    // Remaining indices in the stack have no next greater element (already -1).
    result
}

fn main() {
    let arr = vec![2, 1, 2, 4, 3];
    let nge = next_greater_element(&arr);
    println!("{:?}", nge); // [4, 2, 4, -1, -1]

    let arr = vec![5, 4, 3, 2, 1];
    let nge = next_greater_element(&arr);
    println!("{:?}", nge); // [-1, -1, -1, -1, -1]

    let arr = vec![1, 2, 3, 4, 5];
    let nge = next_greater_element(&arr);
    println!("{:?}", nge); // [2, 3, 4, 5, -1]
}
```

### Next Smaller Element

The mirror operation: for each element, find the next smaller element to its right.

```rust
fn next_smaller_element(arr: &[i32]) -> Vec<i32> {
    let n = arr.len();
    let mut result = vec![-1i32; n];
    let mut stack: Vec<usize> = Vec::new();

    for i in 0..n {
        while let Some(&top) = stack.last() {
            if arr[top] > arr[i] {
                stack.pop();
                result[top] = arr[i];
            } else {
                break;
            }
        }
        stack.push(i);
    }

    result
}

fn main() {
    let arr = vec![4, 8, 5, 2, 25];
    let nse = next_smaller_element(&arr);
    println!("{:?}", nse); // [2, 5, 2, -1, -1]
}
```

### Previous Greater/Smaller Element

Process the array from right to left (or equivalently, iterate left-to-right but read the stack differently). Here is the left-to-right approach where we read the answer from the stack top:

```rust
/// For each element, find the previous greater element (to its left).
fn previous_greater_element(arr: &[i32]) -> Vec<i32> {
    let n = arr.len();
    let mut result = vec![-1i32; n];
    let mut stack: Vec<usize> = Vec::new(); // monotonic decreasing (from bottom to top)

    for i in 0..n {
        // Pop elements that are not greater than arr[i]
        while let Some(&top) = stack.last() {
            if arr[top] <= arr[i] {
                stack.pop();
            } else {
                break;
            }
        }
        // The top of the stack (if any) is the previous greater element
        if let Some(&top) = stack.last() {
            result[i] = arr[top];
        }
        stack.push(i);
    }

    result
}

fn main() {
    let arr = vec![10, 4, 2, 20, 40, 12, 30];
    let pge = previous_greater_element(&arr);
    println!("{:?}", pge); // [-1, 10, 4, -1, -1, 40, 40]
}
```

---

## Sliding Window Maximum/Minimum

The monotonic deque (using `VecDeque`) solves sliding window problems in O(n). Maintain a deque of indices where the corresponding values are in decreasing order (for maximum) or increasing order (for minimum).

### Sliding Window Maximum

```rust
use std::collections::VecDeque;

/// For each window of size `k`, find the maximum element.
/// Returns a vector of length `n - k + 1`.
fn sliding_window_max(arr: &[i32], k: usize) -> Vec<i32> {
    assert!(k > 0 && k <= arr.len());

    let mut result = Vec::with_capacity(arr.len() - k + 1);
    let mut deque: VecDeque<usize> = VecDeque::new();

    for i in 0..arr.len() {
        // Remove indices outside the current window
        while let Some(&front) = deque.front() {
            if front + k <= i {
                deque.pop_front();
            } else {
                break;
            }
        }

        // Remove indices whose values are less than arr[i] from the back.
        // They can never be the maximum for any future window.
        while let Some(&back) = deque.back() {
            if arr[back] <= arr[i] {
                deque.pop_back();
            } else {
                break;
            }
        }

        deque.push_back(i);

        // The front of the deque is always the index of the maximum
        if i >= k - 1 {
            result.push(arr[deque[0]]);
        }
    }

    result
}

/// Sliding window minimum -- mirror of maximum.
fn sliding_window_min(arr: &[i32], k: usize) -> Vec<i32> {
    assert!(k > 0 && k <= arr.len());

    let mut result = Vec::with_capacity(arr.len() - k + 1);
    let mut deque: VecDeque<usize> = VecDeque::new();

    for i in 0..arr.len() {
        while let Some(&front) = deque.front() {
            if front + k <= i {
                deque.pop_front();
            } else {
                break;
            }
        }

        while let Some(&back) = deque.back() {
            if arr[back] >= arr[i] {
                deque.pop_back();
            } else {
                break;
            }
        }

        deque.push_back(i);

        if i >= k - 1 {
            result.push(arr[deque[0]]);
        }
    }

    result
}

fn main() {
    let arr = vec![1, 3, -1, -3, 5, 3, 6, 7];
    let k = 3;

    let maxes = sliding_window_max(&arr, k);
    println!("window max: {:?}", maxes); // [3, 3, 5, 5, 6, 7]

    let mins = sliding_window_min(&arr, k);
    println!("window min: {:?}", mins); // [-1, -3, -3, -3, 3, 3]
}
```

### Amortized Analysis

Each element is pushed into the deque exactly once and popped at most once (either from the back when a larger element arrives, or from the front when it leaves the window). Total operations across all iterations: at most 2n pushes + 2n pops = O(n).

---

## Classic Problems

### Largest Rectangle in Histogram

Given an array of bar heights, find the area of the largest rectangle that fits within the histogram. This is LeetCode 84.

The key insight: for each bar `i`, the largest rectangle using bar `i` as the shortest bar extends left until it hits a shorter bar and extends right until it hits a shorter bar. A monotonic increasing stack gives us both boundaries.

```rust
fn largest_rectangle_in_histogram(heights: &[i32]) -> i64 {
    let n = heights.len();
    let mut stack: Vec<usize> = Vec::new();
    let mut max_area: i64 = 0;

    for i in 0..=n {
        // Use 0 as a sentinel for the right boundary
        let current_height = if i < n { heights[i] } else { 0 };

        while let Some(&top) = stack.last() {
            if heights[top] > current_height {
                stack.pop();
                let height = heights[top] as i64;
                // Width: from (previous stack top + 1) to (i - 1)
                let width = if let Some(&prev) = stack.last() {
                    (i - prev - 1) as i64
                } else {
                    i as i64
                };
                max_area = max_area.max(height * width);
            } else {
                break;
            }
        }

        stack.push(i);
    }

    max_area
}

fn main() {
    assert_eq!(largest_rectangle_in_histogram(&[2, 1, 5, 6, 2, 3]), 10);
    assert_eq!(largest_rectangle_in_histogram(&[2, 4]), 4);
    assert_eq!(largest_rectangle_in_histogram(&[1, 1, 1, 1, 1]), 5);
    assert_eq!(largest_rectangle_in_histogram(&[5, 4, 3, 2, 1]), 9);
    assert_eq!(largest_rectangle_in_histogram(&[1, 2, 3, 4, 5]), 9);

    println!("largest rectangle: all tests passed");
}
```

### Trapping Rain Water

Given an elevation map, compute how much rain water can be trapped. This is LeetCode 42.

**Approach with monotonic stack:** Maintain a decreasing stack. When we encounter a bar taller than the stack top, water can be trapped between the current bar and the previous taller bar in the stack.

```rust
fn trap_rain_water(height: &[i32]) -> i64 {
    let mut stack: Vec<usize> = Vec::new();
    let mut water: i64 = 0;

    for i in 0..height.len() {
        while let Some(&top) = stack.last() {
            if height[top] < height[i] {
                stack.pop();
                // The popped bar is the bottom of the "pool"
                if let Some(&left) = stack.last() {
                    // Water level is min(left wall, right wall) - bottom
                    let bounded_height = height[left].min(height[i]) - height[top];
                    let width = (i - left - 1) as i64;
                    water += bounded_height as i64 * width;
                }
            } else {
                break;
            }
        }
        stack.push(i);
    }

    water
}

fn main() {
    assert_eq!(trap_rain_water(&[0, 1, 0, 2, 1, 0, 1, 3, 2, 1, 2, 1]), 6);
    assert_eq!(trap_rain_water(&[4, 2, 0, 3, 2, 5]), 9);
    assert_eq!(trap_rain_water(&[1, 2, 3, 4, 5]), 0); // ascending, no trap
    assert_eq!(trap_rain_water(&[5, 4, 3, 2, 1]), 0); // descending, no trap
    assert_eq!(trap_rain_water(&[3, 0, 3]), 3);

    println!("trapping rain water: all tests passed");
}
```

### Daily Temperatures

Given daily temperatures, for each day find how many days until a warmer temperature. LeetCode 739.

```rust
fn daily_temperatures(temperatures: &[i32]) -> Vec<i32> {
    let n = temperatures.len();
    let mut result = vec![0i32; n];
    let mut stack: Vec<usize> = Vec::new();

    for i in 0..n {
        while let Some(&top) = stack.last() {
            if temperatures[top] < temperatures[i] {
                stack.pop();
                result[top] = (i - top) as i32;
            } else {
                break;
            }
        }
        stack.push(i);
    }

    result
}

fn main() {
    let temps = vec![73, 74, 75, 71, 69, 72, 76, 73];
    let result = daily_temperatures(&temps);
    println!("{:?}", result); // [1, 1, 4, 2, 1, 1, 0, 0]
    assert_eq!(result, vec![1, 1, 4, 2, 1, 1, 0, 0]);

    println!("daily temperatures: all tests passed");
}
```

---

## Generic Monotonic Stack

We can build a reusable monotonic stack that computes both "next greater" and "previous greater" in one pass:

```rust
use std::cmp::Ordering;

/// Result of monotonic stack analysis for each element.
#[derive(Debug, Clone)]
struct MonoResult {
    /// Index of the next element satisfying the condition, or None.
    next: Option<usize>,
    /// Index of the previous element satisfying the condition, or None.
    prev: Option<usize>,
}

/// Compute next/previous greater (or smaller) for every element.
///
/// `order`: Ordering::Greater finds next/prev greater element.
///          Ordering::Less finds next/prev smaller element.
/// `strict`: if true, uses strict comparison (> or <).
///           if false, uses non-strict (>= or <=).
fn monotonic_analysis<T: Ord>(arr: &[T], order: Ordering, strict: bool) -> Vec<MonoResult> {
    let n = arr.len();
    let mut results = vec![MonoResult { next: None, prev: None }; n];
    let mut stack: Vec<usize> = Vec::new();

    let should_pop = |stack_val: &T, new_val: &T| -> bool {
        let cmp = new_val.cmp(stack_val);
        match order {
            Ordering::Greater => {
                if strict { cmp == Ordering::Greater } else { cmp != Ordering::Less }
            }
            Ordering::Less => {
                if strict { cmp == Ordering::Less } else { cmp != Ordering::Greater }
            }
            Ordering::Equal => cmp == Ordering::Equal,
        }
    };

    for i in 0..n {
        while let Some(&top) = stack.last() {
            if should_pop(&arr[top], &arr[i]) {
                stack.pop();
                results[top].next = Some(i);
            } else {
                break;
            }
        }
        // The current stack top is the "previous" element for i
        if let Some(&top) = stack.last() {
            results[i].prev = Some(top);
        }
        stack.push(i);
    }

    results
}

fn main() {
    let arr = vec![2, 1, 2, 4, 3];

    let greater = monotonic_analysis(&arr, Ordering::Greater, true);
    println!("next/prev greater for {:?}:", arr);
    for (i, r) in greater.iter().enumerate() {
        println!("  [{}] = {}: next={:?}, prev={:?}", i, arr[i], r.next, r.prev);
    }
    // [0] = 2: next=Some(3), prev=None
    // [1] = 1: next=Some(2), prev=Some(0)
    // [2] = 2: next=Some(3), prev=Some(0)
    // [3] = 4: next=None, prev=None
    // [4] = 3: next=None, prev=Some(3)

    let smaller = monotonic_analysis(&arr, Ordering::Less, true);
    println!("\nnext/prev smaller for {:?}:", arr);
    for (i, r) in smaller.iter().enumerate() {
        println!("  [{}] = {}: next={:?}, prev={:?}", i, arr[i], r.next, r.prev);
    }
}
```

---

## Exercises

### Exercise 1: Stock Span Problem

The stock span for day `i` is the number of consecutive days (ending at day `i`) where the price was less than or equal to the price on day `i`. The span always includes day `i` itself.

**Input:**
```
prices = [100, 80, 60, 70, 60, 75, 85]
spans  = [1,   1,  1,  2,  1,  4,  6]
```

Explanation: Day 5 (price 75) has span 4 because prices on days 2,3,4,5 are [60,70,60,75] which are all <= 75.

Implement this in O(n) using a monotonic stack.

<details>
<summary>Solution</summary>

```rust
fn stock_span(prices: &[i32]) -> Vec<usize> {
    let n = prices.len();
    let mut spans = vec![0usize; n];
    let mut stack: Vec<usize> = Vec::new(); // stack of indices, monotonic decreasing prices

    for i in 0..n {
        // Pop all days with price <= prices[i]
        while let Some(&top) = stack.last() {
            if prices[top] <= prices[i] {
                stack.pop();
            } else {
                break;
            }
        }

        // Span = distance from the previous day with a strictly greater price
        spans[i] = if let Some(&prev) = stack.last() {
            i - prev
        } else {
            i + 1 // no previous greater price, span covers all days from 0
        };

        stack.push(i);
    }

    spans
}

fn main() {
    let prices = vec![100, 80, 60, 70, 60, 75, 85];
    let spans = stock_span(&prices);
    assert_eq!(spans, vec![1, 1, 1, 2, 1, 4, 6]);

    let prices = vec![10, 20, 30, 40, 50];
    let spans = stock_span(&prices);
    assert_eq!(spans, vec![1, 2, 3, 4, 5]);

    let prices = vec![50, 40, 30, 20, 10];
    let spans = stock_span(&prices);
    assert_eq!(spans, vec![1, 1, 1, 1, 1]);

    println!("stock span: all tests passed");
}
```
</details>

### Exercise 2: Maximal Rectangle

Given a binary matrix (2D grid of 0s and 1s), find the area of the largest rectangle containing only 1s. This is LeetCode 85.

**Approach:** For each row, compute the histogram of heights (how many consecutive 1s above including current row), then apply the "largest rectangle in histogram" algorithm.

**Input:**
```
matrix = [
    [1, 0, 1, 0, 0],
    [1, 0, 1, 1, 1],
    [1, 1, 1, 1, 1],
    [1, 0, 0, 1, 0],
]
Answer: 6 (rows 1-2, columns 2-4)
```

<details>
<summary>Solution</summary>

```rust
fn largest_rectangle_in_histogram(heights: &[i32]) -> i64 {
    let n = heights.len();
    let mut stack: Vec<usize> = Vec::new();
    let mut max_area: i64 = 0;

    for i in 0..=n {
        let current_height = if i < n { heights[i] } else { 0 };

        while let Some(&top) = stack.last() {
            if heights[top] > current_height {
                stack.pop();
                let height = heights[top] as i64;
                let width = if let Some(&prev) = stack.last() {
                    (i - prev - 1) as i64
                } else {
                    i as i64
                };
                max_area = max_area.max(height * width);
            } else {
                break;
            }
        }

        stack.push(i);
    }

    max_area
}

fn maximal_rectangle(matrix: &[Vec<i32>]) -> i64 {
    if matrix.is_empty() || matrix[0].is_empty() {
        return 0;
    }

    let rows = matrix.len();
    let cols = matrix[0].len();
    let mut heights = vec![0i32; cols];
    let mut max_area: i64 = 0;

    for r in 0..rows {
        // Update histogram heights
        for c in 0..cols {
            if matrix[r][c] == 1 {
                heights[c] += 1;
            } else {
                heights[c] = 0;
            }
        }

        // Find largest rectangle for this row's histogram
        max_area = max_area.max(largest_rectangle_in_histogram(&heights));
    }

    max_area
}

fn main() {
    let matrix = vec![
        vec![1, 0, 1, 0, 0],
        vec![1, 0, 1, 1, 1],
        vec![1, 1, 1, 1, 1],
        vec![1, 0, 0, 1, 0],
    ];

    assert_eq!(maximal_rectangle(&matrix), 6);

    let matrix = vec![vec![0]];
    assert_eq!(maximal_rectangle(&matrix), 0);

    let matrix = vec![vec![1]];
    assert_eq!(maximal_rectangle(&matrix), 1);

    let matrix = vec![
        vec![1, 1, 1],
        vec![1, 1, 1],
        vec![1, 1, 1],
    ];
    assert_eq!(maximal_rectangle(&matrix), 9);

    println!("maximal rectangle: all tests passed");
}
```
</details>

### Exercise 3: Sum of Subarray Minimums

Given an array, compute the sum of `min(subarray)` for all contiguous subarrays. LeetCode 907.

**Approach:** For each element `arr[i]`, count how many subarrays have `arr[i]` as their minimum. Use a monotonic stack to find:
- `left[i]`: number of contiguous elements to the left (including i) where arr[i] is the minimum
- `right[i]`: number of contiguous elements to the right (including i) where arr[i] is the minimum

The contribution of `arr[i]` = `arr[i] * left[i] * right[i]`.

Handle duplicates carefully: use strict comparison on one side and non-strict on the other.

**Input:**
```
arr = [3, 1, 2, 4] -> subarrays: [3]=3, [1]=1, [2]=2, [4]=4,
  [3,1]=1, [1,2]=1, [2,4]=2, [3,1,2]=1, [1,2,4]=1, [3,1,2,4]=1
  Sum = 3+1+2+4+1+1+2+1+1+1 = 17
```

<details>
<summary>Solution</summary>

```rust
const MOD: i64 = 1_000_000_007;

fn sum_of_subarray_minimums(arr: &[i32]) -> i64 {
    let n = arr.len();

    // left[i] = number of elements from i going left where arr[i] is min
    //   (distance to previous smaller element, using strict <)
    let mut left = vec![0i64; n];
    let mut stack: Vec<usize> = Vec::new();

    for i in 0..n {
        // Pop elements >= arr[i] (strict: previous SMALLER element)
        while let Some(&top) = stack.last() {
            if arr[top] >= arr[i] {
                stack.pop();
            } else {
                break;
            }
        }
        left[i] = if let Some(&prev) = stack.last() {
            (i - prev) as i64
        } else {
            (i + 1) as i64
        };
        stack.push(i);
    }

    // right[i] = number of elements from i going right where arr[i] is min
    //   (distance to next strictly smaller element, using strict <)
    let mut right = vec![0i64; n];
    stack.clear();

    for i in (0..n).rev() {
        // Pop elements > arr[i] (non-strict: next SMALLER OR EQUAL element)
        // Using > (not >=) on the right side prevents double-counting duplicates
        while let Some(&top) = stack.last() {
            if arr[top] > arr[i] {
                stack.pop();
            } else {
                break;
            }
        }
        right[i] = if let Some(&next) = stack.last() {
            (next - i) as i64
        } else {
            (n - i) as i64
        };
        stack.push(i);
    }

    let mut total = 0i64;
    for i in 0..n {
        total = (total + arr[i] as i64 % MOD * (left[i] % MOD) % MOD * (right[i] % MOD) % MOD) % MOD;
    }

    total
}

fn main() {
    assert_eq!(sum_of_subarray_minimums(&[3, 1, 2, 4]), 17);
    assert_eq!(sum_of_subarray_minimums(&[11, 81, 94, 43, 3]), 444);

    // Single element
    assert_eq!(sum_of_subarray_minimums(&[5]), 5);

    // All same
    assert_eq!(sum_of_subarray_minimums(&[2, 2, 2]), 12);
    // Subarrays: [2]=2, [2]=2, [2]=2, [2,2]=2, [2,2]=2, [2,2,2]=2 -> 12

    println!("sum of subarray minimums: all tests passed");
}
```
</details>

### Exercise 4: Sliding Window Maximum with Deque

Implement a `SlidingWindowMax` struct that processes a stream of elements one at a time. It should support:
- `push(value)`: add a new element to the window
- `pop_front()`: remove the oldest element from the window
- `max()`: return the current maximum in O(1)

Use a `VecDeque` to maintain the monotonic invariant.

Build it as a reusable struct, then use it to solve the sliding window maximum problem.

<details>
<summary>Solution</summary>

```rust
use std::collections::VecDeque;

/// A sliding window that tracks the maximum in O(1) amortized.
struct SlidingWindowMax {
    /// Deque of (index, value), monotonically decreasing by value.
    deque: VecDeque<(usize, i64)>,
    /// Index counter for elements pushed.
    push_count: usize,
    /// Index of the next element to be logically removed.
    pop_index: usize,
}

impl SlidingWindowMax {
    fn new() -> Self {
        Self {
            deque: VecDeque::new(),
            push_count: 0,
            pop_index: 0,
        }
    }

    fn push(&mut self, value: i64) {
        let idx = self.push_count;
        self.push_count += 1;

        // Remove elements from the back that are smaller than the new value
        while let Some(&(_, back_val)) = self.deque.back() {
            if back_val <= value {
                self.deque.pop_back();
            } else {
                break;
            }
        }

        self.deque.push_back((idx, value));
    }

    fn pop_front(&mut self) {
        // Remove the front if it corresponds to the element being popped
        if let Some(&(front_idx, _)) = self.deque.front() {
            if front_idx == self.pop_index {
                self.deque.pop_front();
            }
        }
        self.pop_index += 1;
    }

    fn max(&self) -> Option<i64> {
        self.deque.front().map(|&(_, v)| v)
    }

    fn len(&self) -> usize {
        self.push_count - self.pop_index
    }

    fn is_empty(&self) -> bool {
        self.len() == 0
    }
}

fn sliding_window_max(arr: &[i64], k: usize) -> Vec<i64> {
    let mut window = SlidingWindowMax::new();
    let mut result = Vec::with_capacity(arr.len() - k + 1);

    for i in 0..arr.len() {
        window.push(arr[i]);

        if window.len() > k {
            window.pop_front();
        }

        if i >= k - 1 {
            result.push(window.max().unwrap());
        }
    }

    result
}

fn main() {
    let arr: Vec<i64> = vec![1, 3, -1, -3, 5, 3, 6, 7];
    let result = sliding_window_max(&arr, 3);
    assert_eq!(result, vec![3, 3, 5, 5, 6, 7]);

    let arr: Vec<i64> = vec![1];
    let result = sliding_window_max(&arr, 1);
    assert_eq!(result, vec![1]);

    let arr: Vec<i64> = vec![9, 8, 7, 6, 5, 4, 3, 2, 1];
    let result = sliding_window_max(&arr, 3);
    assert_eq!(result, vec![9, 8, 7, 6, 5, 4, 3]);

    // Test the struct directly
    let mut w = SlidingWindowMax::new();
    w.push(5);
    w.push(3);
    w.push(8);
    assert_eq!(w.max(), Some(8));
    w.pop_front(); // remove 5
    assert_eq!(w.max(), Some(8));
    w.pop_front(); // remove 3
    assert_eq!(w.max(), Some(8));
    w.pop_front(); // remove 8
    assert!(w.is_empty());

    println!("sliding window max: all tests passed");
}
```
</details>

### Exercise 5: Online Stock Price with Multiple Queries

You are processing a stream of stock prices. After each new price arrives, answer ALL of these:
1. **Span**: how many consecutive days (ending today) the price was <= today's price
2. **Next warmer in last 30 days**: among the last 30 prices, how many have a warmer day within those 30 days
3. **Current sliding window max** (window size 5)

Build a combined system that maintains all three data structures simultaneously.

**Input:**
```
prices arriving: [100, 80, 60, 70, 60, 75, 85, 90, 65, 70]
```

<details>
<summary>Solution</summary>

```rust
use std::collections::VecDeque;

struct StockAnalyzer {
    prices: Vec<i32>,
    // For span calculation
    span_stack: Vec<usize>,
    // For sliding window max (size 5)
    window_deque: VecDeque<usize>,
    window_size: usize,
}

impl StockAnalyzer {
    fn new(window_size: usize) -> Self {
        Self {
            prices: Vec::new(),
            span_stack: Vec::new(),
            window_deque: VecDeque::new(),
            window_size,
        }
    }

    fn add_price(&mut self, price: i32) -> StockReport {
        let idx = self.prices.len();
        self.prices.push(price);

        // 1. Compute span
        while let Some(&top) = self.span_stack.last() {
            if self.prices[top] <= price {
                self.span_stack.pop();
            } else {
                break;
            }
        }
        let span = if let Some(&prev) = self.span_stack.last() {
            idx - prev
        } else {
            idx + 1
        };
        self.span_stack.push(idx);

        // 2. Sliding window max
        while let Some(&front) = self.window_deque.front() {
            if idx >= self.window_size && front <= idx - self.window_size {
                self.window_deque.pop_front();
            } else {
                break;
            }
        }
        while let Some(&back) = self.window_deque.back() {
            if self.prices[back] <= price {
                self.window_deque.pop_back();
            } else {
                break;
            }
        }
        self.window_deque.push_back(idx);
        let window_max = self.prices[*self.window_deque.front().unwrap()];

        // 3. Count prices in last 30 days that have a "next warmer" within those 30 days
        let window_30_start = if idx >= 29 { idx - 29 } else { 0 };
        let recent: Vec<i32> = self.prices[window_30_start..=idx].to_vec();
        let count_with_warmer = count_next_warmer(&recent);

        StockReport {
            day: idx,
            price,
            span,
            window_max,
            count_with_next_warmer_in_30: count_with_warmer,
        }
    }
}

fn count_next_warmer(prices: &[i32]) -> usize {
    let n = prices.len();
    let mut count = 0;
    let mut stack: Vec<usize> = Vec::new();

    for i in 0..n {
        while let Some(&top) = stack.last() {
            if prices[top] < prices[i] {
                stack.pop();
                count += 1;
            } else {
                break;
            }
        }
        stack.push(i);
    }

    count
}

#[derive(Debug)]
struct StockReport {
    day: usize,
    price: i32,
    span: usize,
    window_max: i32,
    count_with_next_warmer_in_30: usize,
}

fn main() {
    let mut analyzer = StockAnalyzer::new(5);
    let prices = [100, 80, 60, 70, 60, 75, 85, 90, 65, 70];

    for &price in &prices {
        let report = analyzer.add_price(price);
        println!(
            "day {}: price={}, span={}, window_max={}, warmer_count={}",
            report.day, report.price, report.span,
            report.window_max, report.count_with_next_warmer_in_30
        );
    }

    // Verify some known values
    // Day 0: price=100, span=1, window_max=100
    // Day 5: price=75, span=4 (60,70,60,75 are all <=75...
    //   actually 80>75, so span goes back to day 2: i=5, prev_greater is index 1 (80)
    //   span = 5 - 1 = 4)

    println!("\nstock analyzer: complete");
}
```
</details>

---

## Pattern Recognition Guide

| Signal in Problem Statement | Technique |
|---|---|
| "Next greater/smaller element" | Monotonic stack, single pass |
| "Previous greater/smaller element" | Monotonic stack, reading from stack top |
| "Maximum/minimum in sliding window" | Monotonic deque (VecDeque) |
| "Largest rectangle" | Monotonic increasing stack + width calculation |
| "Trapping water" | Monotonic decreasing stack + bounded height |
| "Sum of subarray min/max" | Left/right span via monotonic stack |
| "Stock span" | Monotonic decreasing stack |
| "Count of visible elements" | Monotonic stack (buildings/people problem) |

---

## Common Mistakes

1. **Forgetting the sentinel.** In "largest rectangle in histogram", not pushing a sentinel (height 0) at the end means bars remaining in the stack are never processed. The sentinel forces all remaining bars to pop.

2. **Strict vs non-strict comparison with duplicates.** When counting subarray minimums, using `>=` on both left and right double-counts subarrays. Use strict (`>`) on one side and non-strict (`>=`) on the other.

3. **Index vs value confusion.** Always push indices, not values. Read `arr[stack.last()]` when you need the value. Pushing values loses positional information.

4. **Off-by-one in width calculation.** When the stack is empty after popping, the width extends from index 0 to the current index (width = `i`, not `i - 1`).

5. **Using `VecDeque` as a stack.** `VecDeque` supports both `push_back`/`pop_back` (stack) and `push_front`/`pop_front`. For sliding window problems, you need to pop from both ends. For pure stack problems, a `Vec` is simpler and faster.

---

## Verification

```bash
cargo new monotonic-stack-lab && cd monotonic-stack-lab
```

Paste any solution into `src/main.rs` and run:

```bash
cargo run
```

Stress test for sliding window maximum:

```rust
fn stress_test() {
    use std::time::Instant;
    let n = 1_000_000;
    let k = 1000;
    let arr: Vec<i32> = (0..n).map(|i| ((i * 7 + 13) % 1000) as i32).collect();

    let start = Instant::now();
    let result = sliding_window_max(&arr, k);
    println!("sliding max (n={n}, k={k}): {:?}, {} results", start.elapsed(), result.len());
}
```

Expected: under 50ms for 1 million elements.

---

## What You Learned

- A monotonic stack maintains sorted order by popping elements that violate the invariant when a new element arrives. Each element is pushed and popped at most once, giving amortized O(n) for the entire array.
- The "next greater element" pattern (and its variants: next smaller, previous greater, previous smaller) is the foundation for a large family of competitive programming problems.
- The largest rectangle in histogram algorithm uses a monotonic increasing stack: when a shorter bar is encountered, all taller bars on the stack are "resolved" by computing their maximal rectangle width.
- Trapping rain water uses a monotonic decreasing stack: when a taller bar is found, water is computed layer by layer between the current bar and the previous taller bar.
- `VecDeque` as a monotonic deque solves sliding window max/min in O(n) by maintaining candidates in decreasing (or increasing) order and removing expired elements from the front.
- Careful handling of duplicates (strict vs non-strict comparison) is critical when counting contributions of each element to avoid double-counting.

## Resources

- [CP-Algorithms: Stack and Queue Modifications](https://cp-algorithms.com/data_structures/stack_queue_modification.html)
- [LeetCode 84: Largest Rectangle in Histogram](https://leetcode.com/problems/largest-rectangle-in-histogram/)
- [LeetCode 42: Trapping Rain Water](https://leetcode.com/problems/trapping-rain-water/)
- [LeetCode 239: Sliding Window Maximum](https://leetcode.com/problems/sliding-window-maximum/)
- [LeetCode 907: Sum of Subarray Minimums](https://leetcode.com/problems/sum-of-subarray-minimums/)
