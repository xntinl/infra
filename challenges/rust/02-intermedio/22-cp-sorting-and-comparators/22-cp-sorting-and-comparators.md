# 22. CP: Sorting and Comparators

**Difficulty**: Intermedio

## Prerequisites

- `Vec<T>`, slices, and ownership
- Closures (`|a, b| ...`) and `Fn` traits
- `Ord`, `PartialOrd`, `Eq`, `PartialEq` traits
- Tuple ordering in Rust
- Competitive programming I/O patterns (see exercise 20)

## Learning Objectives

1. Use `sort`, `sort_unstable`, `sort_by`, `sort_by_key`, and `sort_unstable_by` effectively
2. Write custom comparators with closures for multi-key sorting
3. Implement `Ord` and `PartialOrd` for custom structs
4. Handle the `f64` sorting problem (NaN breaks `Ord`)
5. Apply sorting as a preprocessing step for greedy and sweep-line algorithms

---

## Concepts

### Rust Sorting Methods

```rust
let mut v = vec![3, 1, 4, 1, 5, 9, 2, 6];

// Stable sort (preserves order of equal elements)
v.sort();               // requires T: Ord
v.sort_by(|a, b| a.cmp(b));  // custom comparator

// Unstable sort (faster, doesn't preserve order of equals)
v.sort_unstable();
v.sort_unstable_by(|a, b| b.cmp(a)); // descending

// Sort by key extraction (stable)
v.sort_by_key(|&x| std::cmp::Reverse(x));  // descending via Reverse

// sort_unstable_by_key (fastest for key-based)
v.sort_unstable_by_key(|&x| x % 3);  // sort by remainder
```

### Multi-Key Sorting with Tuples

Rust tuples implement `Ord` lexicographically, which makes multi-key sorting elegant:

```rust
let mut people = vec![
    ("Alice", 30),
    ("Bob", 25),
    ("Alice", 25),
];

// Sort by name ascending, then age ascending
people.sort_by_key(|&(name, age)| (name, age));
// [("Alice", 25), ("Alice", 30), ("Bob", 25)]

// Sort by age ascending, then name descending
// Problem: &str doesn't support Reverse well for descending
// Solution: use sort_by with a custom comparator
people.sort_by(|a, b| {
    a.1.cmp(&b.1)                    // age ascending
        .then(b.0.cmp(&a.0))         // name descending (note b, a swapped)
});
```

### The `Ordering` Enum and Chaining

```rust
use std::cmp::Ordering;

// Ordering has three variants: Less, Equal, Greater
// .then() chains comparisons (only used if the first is Equal)
// .then_with(|| ...) is the lazy version

let cmp = a.x.cmp(&b.x)
    .then(a.y.cmp(&b.y))
    .then(a.z.cmp(&b.z));
```

### Implementing `Ord` for Custom Types

```rust
#[derive(Eq, PartialEq)]
struct Event {
    end: i32,
    start: i32,
}

impl Ord for Event {
    fn cmp(&self, other: &Self) -> std::cmp::Ordering {
        self.end.cmp(&other.end)           // sort by end time first
            .then(self.start.cmp(&other.start))  // then by start time
    }
}

impl PartialOrd for Event {
    fn partial_cmp(&self, other: &Self) -> Option<std::cmp::Ordering> {
        Some(self.cmp(other))
    }
}
```

### The f64 Problem

`f64` does NOT implement `Ord` because `NaN != NaN`, which violates total ordering.

```rust
let mut floats = vec![3.1, 1.4, 2.7];

// floats.sort();  // COMPILE ERROR: f64 doesn't impl Ord

// Solution 1: sort_by with partial_cmp + unwrap (crash on NaN)
floats.sort_by(|a, b| a.partial_cmp(b).unwrap());

// Solution 2: sort_by with total_cmp (treats NaN consistently)
floats.sort_by(|a, b| a.total_cmp(b));

// Solution 3: Use OrderedFloat from the `ordered-float` crate (not in std)
// Solution 4: Multiply by 1000 and sort as integers (competition trick)
```

### Sorting as Preprocessing

```
Problem: Merge overlapping intervals
  Input:  [(1,3), (2,6), (8,10), (15,18)]

  Step 1: Sort by start time
          [(1,3), (2,6), (8,10), (15,18)]  (already sorted)

  Step 2: Sweep left to right, merging overlaps
          current = (1,3)
          (2,6):  2 <= 3  => merge to (1,6)
          (8,10): 8 > 6   => emit (1,6), current = (8,10)
          (15,18): 15 > 10 => emit (8,10), current = (15,18)
          emit (15,18)

  Output: [(1,6), (8,10), (15,18)]
```

---

## Problem 1: Custom Sort with Closures

### Statement

Given `n` integers, sort them according to the following rules:
1. Even numbers come before odd numbers.
2. Among even numbers, sort in ascending order.
3. Among odd numbers, sort in descending order.

### Input Format

```
n
a_1 a_2 ... a_n
```

### Output Format

The sorted array on a single line, space-separated.

### Constraints

- 1 <= n <= 10^5
- -10^9 <= a_i <= 10^9

### Examples

```
Input:
7
5 3 2 8 1 4 6

Output:
2 4 6 8 5 3 1
```

Explanation: Evens `[2, 4, 6, 8]` ascending, then odds `[5, 3, 1]` descending.

### Hints

1. Use `sort_by` with a closure that first compares parity (even < odd).
2. For same parity: ascending for even, descending for odd.
3. `a % 2` in Rust preserves sign for negative numbers. Use `a.abs() % 2` or
   `a & 1` for non-negative, or compare `a % 2 == 0` as a boolean.

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
    let mut a: Vec<i64> = (0..n).map(|_| next!(i64)).collect();

    // TODO: Sort `a` using sort_by with a closure
    // Hint: is_even = x % 2 == 0
    // Even before odd: compare (is_even_a as i32) vs (is_even_b as i32)?
    //   Actually, false (0) < true (1), but we want even (true) first...
    //   So compare !is_even_a vs !is_even_b (odd=true sorts after odd=false? No...)
    //   Simplest: use a tuple key with sort_by_key:
    //     if even => (0, a_i)        -- group 0, ascending
    //     if odd  => (1, -a_i)       -- group 1, ascending by -a_i = descending by a_i

    // TODO: Print the sorted array

    let result: Vec<String> = a.iter().map(|x| x.to_string()).collect();
    writeln!(out, "{}", result.join(" ")).unwrap();
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
    let mut a: Vec<i64> = (0..n).map(|_| next!(i64)).collect();

    // Solution using sort_by_key with tuple
    a.sort_by_key(|&x| {
        if x % 2 == 0 {
            (0, x)      // even: group 0, ascending by value
        } else {
            (1, -x)     // odd:  group 1, ascending by -x = descending by x
        }
    });

    let result: Vec<String> = a.iter().map(|x| x.to_string()).collect();
    writeln!(out, "{}", result.join(" ")).unwrap();
}
```

**Alternative** using `sort_by` with `Ordering` chaining:

```rust
use std::cmp::Ordering;

a.sort_by(|&x, &y| {
    let x_even = x % 2 == 0;
    let y_even = y % 2 == 0;

    match (x_even, y_even) {
        (true, false) => Ordering::Less,    // even before odd
        (false, true) => Ordering::Greater, // odd after even
        (true, true) => x.cmp(&y),          // both even: ascending
        (false, false) => y.cmp(&x),        // both odd: descending
    }
});
```

**Complexity**: O(n log n).

</details>

---

## Problem 2: Sort by Multiple Keys

### Statement

You are given `n` students, each with a name (string) and a grade (integer). Sort
the students by:
1. Grade in descending order.
2. If grades are equal, by name in ascending lexicographic order.

### Input Format

```
n
name_1 grade_1
name_2 grade_2
...
name_n grade_n
```

### Output Format

Print each student (name and grade) on a separate line, in sorted order.

### Constraints

- 1 <= n <= 10^5
- Names consist of lowercase English letters, 1 <= |name| <= 20.
- 0 <= grade <= 100.

### Examples

```
Input:
5
alice 90
bob 85
charlie 90
dave 85
eve 95

Output:
eve 95
alice 90
charlie 90
bob 85
dave 85
```

### Hints

1. Store as `Vec<(String, i32)>` or a struct.
2. `sort_by` with `b.1.cmp(&a.1).then(a.0.cmp(&b.0))`.
3. Or `sort_by_key` with `(Reverse(grade), name.clone())` -- but cloning strings in the
   key has overhead.

### Solution Template

```rust
use std::io::{self, Read, Write, BufWriter};

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let mut iter = input.split_whitespace();
    macro_rules! next {
        () => { iter.next().unwrap() };
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() };
    }
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let n: usize = next!(usize);
    let mut students: Vec<(String, i32)> = Vec::with_capacity(n);
    for _ in 0..n {
        let name = next!().to_string();
        let grade = next!(i32);
        students.push((name, grade));
    }

    // TODO: Sort students by grade descending, then name ascending
    // Hint: use sort_by with Ordering chaining

    for (name, grade) in &students {
        writeln!(out, "{} {}", name, grade).unwrap();
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
        () => { iter.next().unwrap() };
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() };
    }
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let n: usize = next!(usize);
    let mut students: Vec<(String, i32)> = Vec::with_capacity(n);
    for _ in 0..n {
        let name = next!().to_string();
        let grade = next!(i32);
        students.push((name, grade));
    }

    students.sort_by(|a, b| {
        b.1.cmp(&a.1)              // grade descending (b before a)
            .then(a.0.cmp(&b.0))   // name ascending (a before b)
    });

    for (name, grade) in &students {
        writeln!(out, "{} {}", name, grade).unwrap();
    }
}
```

**Alternative** with `sort_by_key` and `Reverse`:

```rust
use std::cmp::Reverse;

students.sort_by_key(|(name, grade)| (Reverse(*grade), name.clone()));
```

Note: `name.clone()` allocates a new string for each comparison, so `sort_by` is more
efficient for large inputs.

**Complexity**: O(n log n * L) where L is the max string length (for comparisons).

</details>

---

## Problem 3: Merge Intervals

### Statement

Given `n` intervals `[start_i, end_i]`, merge all overlapping intervals and return the
list of merged intervals sorted by start time.

Two intervals overlap if one starts before or at the point the other ends:
`[1,3]` and `[2,6]` overlap and merge to `[1,6]`.

### Input Format

```
n
start_1 end_1
start_2 end_2
...
start_n end_n
```

### Output Format

Print the number of merged intervals on the first line, then each merged interval on a
separate line as `start end`.

### Constraints

- 1 <= n <= 10^5
- 0 <= start_i <= end_i <= 10^9

### Examples

```
Input:
4
1 3
2 6
8 10
15 18

Output:
3
1 6
8 10
15 18
```

```
Input:
2
1 4
4 5

Output:
1
1 5
```

### Hints

1. Sort intervals by start time.
2. Iterate through sorted intervals, maintaining a "current" merged interval.
3. If the next interval's start <= current end, extend current's end.
4. Otherwise, push current to results and start a new current.
5. Do not forget to push the last current interval.

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
    let mut intervals: Vec<(i64, i64)> = (0..n)
        .map(|_| (next!(i64), next!(i64)))
        .collect();

    // TODO: Sort by start time (tuples sort lexicographically, so this is automatic)

    let mut merged: Vec<(i64, i64)> = Vec::new();

    // TODO: Iterate through sorted intervals
    //   If merged is empty or last merged interval doesn't overlap with current:
    //       push current to merged
    //   Else:
    //       extend the last merged interval's end

    writeln!(out, "{}", merged.len()).unwrap();
    for (s, e) in &merged {
        writeln!(out, "{} {}", s, e).unwrap();
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
    let mut intervals: Vec<(i64, i64)> = (0..n)
        .map(|_| (next!(i64), next!(i64)))
        .collect();

    intervals.sort_unstable();  // sorts by (start, end) lexicographically

    let mut merged: Vec<(i64, i64)> = Vec::new();

    for (s, e) in intervals {
        if let Some(last) = merged.last_mut() {
            if s <= last.1 {
                // Overlapping: extend
                last.1 = last.1.max(e);
            } else {
                merged.push((s, e));
            }
        } else {
            merged.push((s, e));
        }
    }

    writeln!(out, "{}", merged.len()).unwrap();
    for (s, e) in &merged {
        writeln!(out, "{} {}", s, e).unwrap();
    }
}
```

**Rust idiom**: `merged.last_mut()` returns `Option<&mut (i64, i64)>`, letting us modify
the last element in place without indexing.

**Complexity**: O(n log n) for sorting, O(n) for merging.

</details>

---

## Problem 4: Meeting Rooms II (Minimum Rooms)

### Statement

Given `n` meetings with start and end times, find the **minimum number of meeting rooms**
required so that no two overlapping meetings share a room.

### Input Format

```
n
start_1 end_1
start_2 end_2
...
start_n end_n
```

### Output Format

A single integer: the minimum number of rooms needed.

### Constraints

- 1 <= n <= 10^5
- 0 <= start_i < end_i <= 10^9

### Examples

```
Input:
3
0 30
5 10
15 20

Output:
2
```

Explanation: Meetings [0,30] and [5,10] overlap, requiring 2 rooms. [15,20] fits in the
room freed by [5,10].

```
Input:
2
7 10
2 4

Output:
1
```

### Hints

1. **Event sweep approach**: Create events: `+1` at each start, `-1` at each end.
2. Sort events by time. If times are tied, process ends (`-1`) before starts (`+1`)
   -- a room freed at time T can be reused by a meeting starting at time T.
3. Sweep through events tracking the current count. The maximum count is the answer.

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

    // TODO: Create a Vec of events: (time, delta)
    //   For each meeting (s, e): push (s, 1) and (e, -1)
    let mut events: Vec<(i64, i32)> = Vec::with_capacity(2 * n);
    for _ in 0..n {
        let s = next!(i64);
        let e = next!(i64);
        // TODO: Push start and end events
    }

    // TODO: Sort events by (time, delta)
    //   Sorting by delta ensures -1 (end) comes before +1 (start) at same time

    // TODO: Sweep through events, tracking current rooms and max rooms

    // TODO: Print max rooms
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

    let mut events: Vec<(i64, i32)> = Vec::with_capacity(2 * n);
    for _ in 0..n {
        let s = next!(i64);
        let e = next!(i64);
        events.push((s, 1));   // meeting starts
        events.push((e, -1));  // meeting ends
    }

    // Sort by time, then by delta (-1 before +1 at same time)
    events.sort_unstable();

    let mut current = 0i32;
    let mut max_rooms = 0i32;

    for (_, delta) in &events {
        current += delta;
        if current > max_rooms {
            max_rooms = current;
        }
    }

    writeln!(out, "{}", max_rooms).unwrap();
}
```

**Why tuple sorting works here**: `(time, -1)` sorts before `(time, 1)` because
`-1 < 1`. This naturally handles the tie-breaking rule.

**Complexity**: O(n log n) for sorting.

</details>

---

## Problem 5: Relative Sort Array

### Statement

Given two arrays `arr1` and `arr2`, sort `arr1` such that the relative ordering of items
in `arr1` matches the order defined by `arr2`. Elements not in `arr2` should be placed at
the end in ascending order.

### Input Format

```
n m
arr1_1 arr1_2 ... arr1_n
arr2_1 arr2_2 ... arr2_m
```

### Output Format

The sorted `arr1` on a single line, space-separated.

### Constraints

- 1 <= n <= 10^5
- 1 <= m <= n
- 0 <= arr1_i, arr2_i <= 10^6
- All elements of `arr2` are distinct.
- All elements of `arr2` are present in `arr1`.

### Examples

```
Input:
9 6
2 3 1 3 2 4 6 7 9
2 1 4 3 9 6

Output:
2 2 1 4 3 3 9 6 7
```

Explanation: 2 appears first (as in arr2), then 1, then 4, then 3 (twice), then 9, then
6. Finally, 7 is not in arr2, so it goes at the end.

### Hints

1. Build a `HashMap<i32, usize>` mapping each element of `arr2` to its index.
2. Use `sort_by_key` where the key is:
   - If the element is in `arr2`: `(0, index_in_arr2, 0)` -- sort by arr2 order
   - If not: `(1, 0, value)` -- sort after all arr2 elements, ascending by value
3. The tuple's first element separates the two groups.

### Solution Template

```rust
use std::io::{self, Read, Write, BufWriter};
use std::collections::HashMap;

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
    let m: usize = next!(usize);
    let mut arr1: Vec<i64> = (0..n).map(|_| next!(i64)).collect();
    let arr2: Vec<i64> = (0..m).map(|_| next!(i64)).collect();

    // TODO: Build a HashMap mapping each element of arr2 to its index

    // TODO: Sort arr1 using sort_by_key
    //   Key idea: elements in arr2 come first (ordered by arr2 index),
    //   elements NOT in arr2 come last (ordered ascending by value).

    let result: Vec<String> = arr1.iter().map(|x| x.to_string()).collect();
    writeln!(out, "{}", result.join(" ")).unwrap();
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
    let mut iter = input.split_whitespace();
    macro_rules! next {
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() };
    }
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let n: usize = next!(usize);
    let m: usize = next!(usize);
    let mut arr1: Vec<i64> = (0..n).map(|_| next!(i64)).collect();
    let arr2: Vec<i64> = (0..m).map(|_| next!(i64)).collect();

    let order: HashMap<i64, usize> = arr2
        .iter()
        .enumerate()
        .map(|(i, &v)| (v, i))
        .collect();

    arr1.sort_by_key(|&x| {
        match order.get(&x) {
            Some(&idx) => (0, idx as i64),   // in arr2: sort by position
            None => (1, x),                  // not in arr2: sort by value, after
        }
    });

    let result: Vec<String> = arr1.iter().map(|x| x.to_string()).collect();
    writeln!(out, "{}", result.join(" ")).unwrap();
}
```

**Complexity**: O(n log n) for sorting, O(m) for building the map.

</details>

---

## Summary Cheat Sheet

| Method                   | Stable? | Key type     | Use case                        |
|--------------------------|---------|------------- |---------------------------------|
| `sort()`                 | Yes     | `T: Ord`     | Simple ascending                |
| `sort_unstable()`        | No      | `T: Ord`     | Faster simple ascending         |
| `sort_by(cmp)`           | Yes     | closure      | Custom comparator               |
| `sort_unstable_by(cmp)`  | No      | closure      | Fastest custom comparator       |
| `sort_by_key(f)`         | Yes     | extracted key| Multi-key via tuple             |
| `sort_unstable_by_key(f)`| No      | extracted key| Fastest multi-key               |

### Ordering Chaining Pattern

```rust
a_field1.cmp(&b_field1)           // primary key
    .then(a_field2.cmp(&b_field2)) // secondary key
    .then(a_field3.cmp(&b_field3)) // tertiary key
```

Swap `a` and `b` to reverse a particular key. Or wrap in `std::cmp::Reverse`.

### Common Pitfalls

- **f64 sorting**: Use `.total_cmp()` or `.partial_cmp().unwrap()`. Never assume f64
  implements `Ord`.
- **String cloning in `sort_by_key`**: The key is computed and stored per comparison.
  Cloning strings is expensive. Prefer `sort_by` for string-heavy sorting.
- **Stable vs unstable**: `sort_unstable` is ~20% faster. Use it unless you need to
  preserve the relative order of equal elements.
- **Negative modulo**: `(-7) % 2 == -1` in Rust (not 1). Use `.rem_euclid(2)` for
  mathematical modulo.
