# 24. CP: Greedy Algorithms

**Difficulty**: Intermedio

## Prerequisites

- Sorting and custom comparators (exercise 22)
- Iterators, closures, and `sort_by` / `sort_by_key`
- Basic proof reasoning (exchange argument, greedy stays ahead)
- Competitive programming I/O patterns (see exercise 20)

## Learning Objectives

1. Identify problems where a greedy strategy yields an optimal solution
2. Prove correctness using the "greedy stays ahead" or "exchange argument" techniques
3. Implement greedy algorithms efficiently in Rust
4. Recognize when greedy fails and dynamic programming is needed instead
5. Use sorting as a preprocessing step for greedy selection

---

## Concepts

### What is a Greedy Algorithm?

A greedy algorithm makes the locally optimal choice at each step, hoping to reach a
globally optimal solution. Unlike dynamic programming, it never reconsiders past choices.

```
Greedy decision at each step:

  Choices: [A, B, C, D, E]

  Step 1: Pick the "best" locally  =>  A
  Step 2: Pick the "best" from remaining  =>  C
  Step 3: Pick the "best" from remaining  =>  E
  ...

  No backtracking! Once chosen, it's final.
```

### When Does Greedy Work?

A greedy algorithm works when the problem has two properties:

1. **Greedy Choice Property**: A locally optimal choice leads to a globally optimal
   solution. We can prove this by showing that replacing any other choice with the greedy
   one doesn't make things worse.

2. **Optimal Substructure**: An optimal solution to the problem contains optimal solutions
   to subproblems.

### Proof Technique: Exchange Argument

```
Claim: Greedy solution G is optimal.

Proof by contradiction:
  1. Assume optimal solution O != G.
  2. Find the first point where G and O differ.
  3. Show you can "exchange" O's choice for G's choice without
     making the solution worse.
  4. Repeat until O = G.  Contradiction.
```

### Proof Technique: Greedy Stays Ahead

```
Claim: At every step, the greedy solution is at least as good
       as any other solution.

Proof by induction:
  Base: After step 1, greedy is at least as good.
  Step: If greedy is ahead after step k, it stays ahead after step k+1.
```

### Common Greedy Patterns

```
Pattern                  | Sort by          | Greedy choice
-------------------------+------------------+---------------------------
Interval scheduling      | End time         | Pick earliest-ending
Interval partitioning    | Start time       | Assign to any compatible
Fractional knapsack      | Value/weight     | Take highest ratio first
Task scheduling (deadline)| Deadline        | Schedule latest-deadline first
Huffman coding           | Frequency        | Merge two smallest
Minimum coins            | Denomination     | Take largest coin first*

* Only works for certain coin systems (e.g., US coins). Fails in general.
```

---

## Problem 1: Activity Selection (Interval Scheduling)

### Statement

You have `n` activities, each with a start time and end time. Select the **maximum
number** of activities that do not overlap. Two activities are compatible if one ends
before or at the time the other starts (i.e., `end_i <= start_j`).

### Input Format

```
n
start_1 end_1
start_2 end_2
...
start_n end_n
```

### Output Format

A single integer: the maximum number of non-overlapping activities.

### Constraints

- 1 <= n <= 10^5
- 0 <= start_i < end_i <= 10^9

### Examples

```
Input:
6
1 3
2 5
3 6
5 7
6 8
8 9

Output:
4
```

Explanation: Select activities (1,3), (3,6), (6,8), (8,9). Or (1,3), (5,7), (8,9) gives
only 3. The optimal is 4.

Wait, let's verify: (1,3), (3,6), (6,8), (8,9) -- all compatible. That's 4.

```
Input:
3
1 2
2 3
3 4

Output:
3
```

### Hints

1. Sort activities by **end time** (ascending).
2. Greedily pick the next activity whose start time >= the end time of the last selected.
3. This always yields the maximum number of compatible activities.

### Greedy Choice Proof

```
Why sort by end time?

  Suppose the optimal solution O doesn't include the activity with
  the earliest end time (call it E). O must include some other activity
  F that overlaps the time slot where E fits. Since E ends no later
  than F, replacing F with E in O doesn't break any compatibility
  and doesn't reduce the count. So we can always include E.
```

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
    let mut activities: Vec<(i64, i64)> = (0..n)
        .map(|_| (next!(i64), next!(i64)))
        .collect();

    // TODO: Sort by end time
    //   activities.sort_unstable_by_key(|&(_, end)| end);

    // TODO: Greedily select activities
    //   let mut count = 0;
    //   let mut last_end = 0;  // or i64::MIN
    //   For each (start, end) in sorted order:
    //     if start >= last_end => select it, update last_end, increment count

    // TODO: Print count
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
    let mut activities: Vec<(i64, i64)> = (0..n)
        .map(|_| (next!(i64), next!(i64)))
        .collect();

    // Sort by end time
    activities.sort_unstable_by_key(|&(_, end)| end);

    let mut count = 0usize;
    let mut last_end: i64 = i64::MIN;

    for &(start, end) in &activities {
        if start >= last_end {
            count += 1;
            last_end = end;
        }
    }

    writeln!(out, "{}", count).unwrap();
}
```

**Complexity**: O(n log n) for sorting, O(n) for the greedy scan.

</details>

---

## Problem 2: Fractional Knapsack

### Statement

You have a knapsack with capacity `W` and `n` items. Each item has a weight `w_i` and a
value `v_i`. You can take **fractions** of items (unlike 0/1 knapsack). Maximize the
total value.

### Input Format

```
n W
v_1 w_1
v_2 w_2
...
v_n w_n
```

### Output Format

A single number: the maximum value with **exactly 3 decimal places**.

### Constraints

- 1 <= n <= 10^5
- 1 <= W <= 10^9
- 1 <= v_i, w_i <= 10^6

### Examples

```
Input:
3 50
60 10
100 20
120 30

Output:
240.000
```

Explanation: Take all of item 1 (60), all of item 2 (100), and 2/3 of item 3
(120 * 20/30 = 80). Total = 240.

```
Input:
2 10
500 30
400 20

Output:
166.667
```

### Hints

1. Compute value-to-weight ratio for each item.
2. Sort by ratio in descending order.
3. Greedily take as much of the highest-ratio item as possible.
4. Use `f64` for the computation. Format output with `{:.3}`.

### Greedy Choice Proof

```
The item with the highest value/weight ratio gives the most value per
unit of capacity used. Taking it first (or as much as possible) is
always at least as good as any other strategy.

Exchange argument: If the optimal solution takes less of the highest-ratio
item and more of a lower-ratio item, swapping units between them
increases total value. Contradiction.
```

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
    let capacity: f64 = next!(f64);

    let mut items: Vec<(f64, f64)> = (0..n)
        .map(|_| {
            let v = next!(f64);
            let w = next!(f64);
            (v, w)
        })
        .collect();

    // TODO: Sort items by value/weight ratio in descending order
    //   Use sort_by with f64::total_cmp (reversed)

    // TODO: Greedily fill the knapsack
    //   let mut remaining_capacity = capacity;
    //   let mut total_value = 0.0;
    //   For each item (v, w):
    //     If w <= remaining_capacity => take all of it
    //     Else => take fraction: remaining_capacity / w * v
    //     Update remaining_capacity
    //     If remaining_capacity <= 0 => break

    // TODO: Print total_value with 3 decimal places
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
    let capacity: f64 = next!(f64);

    let mut items: Vec<(f64, f64)> = (0..n)
        .map(|_| {
            let v = next!(f64);
            let w = next!(f64);
            (v, w)
        })
        .collect();

    // Sort by value/weight ratio, descending
    items.sort_by(|a, b| {
        let ratio_a = a.0 / a.1;
        let ratio_b = b.0 / b.1;
        ratio_b.total_cmp(&ratio_a)  // descending
    });

    let mut remaining = capacity;
    let mut total_value = 0.0f64;

    for &(v, w) in &items {
        if remaining <= 0.0 {
            break;
        }
        if w <= remaining {
            total_value += v;
            remaining -= w;
        } else {
            total_value += v * (remaining / w);
            remaining = 0.0;
        }
    }

    writeln!(out, "{:.3}", total_value).unwrap();
}
```

**Why `total_cmp`?** The `f64` type does not implement `Ord` because `NaN != NaN`.
`total_cmp` provides a total ordering that handles NaN consistently. In competitive
programming with well-formed input, `partial_cmp().unwrap()` also works.

**Complexity**: O(n log n) for sorting, O(n) for the greedy selection.

**Note**: This greedy does NOT work for the 0/1 knapsack (where you must take whole
items). That requires dynamic programming.

</details>

---

## Problem 3: Jump Game

### Statement

You are given an array of `n` non-negative integers. Each element represents the maximum
jump length from that position. Starting at index 0, determine if you can reach the last
index.

### Input Format

```
n
a_1 a_2 ... a_n
```

### Output Format

Print `YES` if you can reach the last index, `NO` otherwise.

### Constraints

- 1 <= n <= 10^5
- 0 <= a_i <= 10^5

### Examples

```
Input:
5
2 3 1 1 4

Output:
YES
```

Explanation: Jump 1 to index 1, then 3 to index 4.

```
Input:
5
3 2 1 0 4

Output:
NO
```

Explanation: You always arrive at index 3 (value 0) and can never reach index 4.

### Hints

1. Track the **farthest reachable** index as you scan left to right.
2. At each index `i`, if `i > farthest`, you cannot reach this position -- answer is NO.
3. Otherwise, update `farthest = max(farthest, i + a[i])`.
4. If `farthest >= n - 1`, answer is YES.

### Greedy Choice Proof

```
At each position, we extend the "reachable frontier" as far as possible.
If at any point the frontier hasn't reached position i, no sequence of
jumps from earlier positions could reach i either (since we already
considered all of them). So the greedy check is both necessary and
sufficient.
```

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
    let a: Vec<usize> = (0..n).map(|_| next!(usize)).collect();

    let mut farthest: usize = 0;
    let mut reachable = true;

    for i in 0..n {
        // TODO: If i > farthest, we can't reach index i => not reachable
        // TODO: Update farthest = max(farthest, i + a[i])
        // TODO: Early exit if farthest >= n - 1
    }

    writeln!(out, "{}", if reachable { "YES" } else { "NO" }).unwrap();
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
    let a: Vec<usize> = (0..n).map(|_| next!(usize)).collect();

    let mut farthest: usize = 0;
    let mut reachable = true;

    for i in 0..n {
        if i > farthest {
            reachable = false;
            break;
        }
        farthest = farthest.max(i + a[i]);
        if farthest >= n - 1 {
            break; // early exit: we can reach the end
        }
    }

    writeln!(out, "{}", if reachable { "YES" } else { "NO" }).unwrap();
}
```

**Complexity**: O(n) time, O(1) extra space.

**Follow-up**: "Jump Game II" asks for the *minimum number of jumps*. That also has a
greedy O(n) solution using BFS-like level tracking.

</details>

---

## Problem 4: Minimum Number of Coins

### Statement

Given an infinite supply of coins with denominations `d_1, d_2, ..., d_m` and a target
amount `S`, find the **minimum number of coins** needed to make exactly `S`. If it is
impossible, print `-1`.

**Important**: This problem uses dynamic programming for the general case. However, for
the special denominations `[1, 5, 10, 25]` (like US coins), a greedy approach works.
Implement both approaches.

### Input Format

```
m S
d_1 d_2 ... d_m
```

### Output Format

A single integer: the minimum number of coins, or `-1` if impossible.

### Constraints

- 1 <= m <= 20
- 1 <= S <= 10^6
- 1 <= d_i <= 10^6

### Examples

```
Input:
4 41
1 5 10 25

Output:
4
```

Explanation: 25 + 10 + 5 + 1 = 41, using 4 coins.

```
Input:
3 6
1 3 4

Output:
2
```

Explanation: 3 + 3 = 6, using 2 coins. (Greedy would pick 4 + 1 + 1 = 3 coins!)

```
Input:
2 3
2 5

Output:
-1
```

### Hints

1. **Greedy (only for canonical coin systems)**: Sort denominations descending, take as
   many of the largest coin as possible, then next largest, etc.
2. **DP (general)**: `dp[i]` = minimum coins to make amount `i`.
   `dp[0] = 0`, `dp[i] = min(dp[i - d_j] + 1)` for all valid denominations `d_j`.
3. The problem requires the general DP approach since arbitrary denominations are given.

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

    let m: usize = next!(usize);
    let s: usize = next!(usize);
    let coins: Vec<usize> = (0..m).map(|_| next!(usize)).collect();

    // --- DP approach (correct for all coin systems) ---
    const INF: usize = usize::MAX / 2;
    let mut dp = vec![INF; s + 1];
    dp[0] = 0;

    // TODO: For each amount from 1 to s:
    //   For each coin denomination:
    //     If coin <= amount and dp[amount - coin] + 1 < dp[amount]:
    //       dp[amount] = dp[amount - coin] + 1

    // TODO: Print dp[s] if it's not INF, otherwise -1
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

    let m: usize = next!(usize);
    let s: usize = next!(usize);
    let coins: Vec<usize> = (0..m).map(|_| next!(usize)).collect();

    const INF: usize = usize::MAX / 2;
    let mut dp = vec![INF; s + 1];
    dp[0] = 0;

    for amount in 1..=s {
        for &coin in &coins {
            if coin <= amount && dp[amount - coin] + 1 < dp[amount] {
                dp[amount] = dp[amount - coin] + 1;
            }
        }
    }

    if dp[s] >= INF {
        writeln!(out, "-1").unwrap();
    } else {
        writeln!(out, "{}", dp[s]).unwrap();
    }
}
```

**Greedy version (only for canonical systems like US coins)**:

```rust
fn greedy_coins(coins: &mut Vec<usize>, s: usize) -> usize {
    coins.sort_unstable_by(|a, b| b.cmp(a)); // descending
    let mut remaining = s;
    let mut count = 0;
    for &coin in coins.iter() {
        count += remaining / coin;
        remaining %= coin;
    }
    if remaining == 0 { count } else { usize::MAX } // usize::MAX means impossible
}
```

**Why greedy fails for `[1, 3, 4]` with target 6**: Greedy picks 4, then 1, then 1
(3 coins), but 3+3 uses only 2 coins. The greedy choice property does not hold for
arbitrary denominations.

**Complexity**: DP is O(S * m). Greedy is O(m log m + m).

</details>

---

## Problem 5: Assign Cookies

### Statement

You have `n` children and `m` cookies. Each child `i` has a greed factor `g_i`
(minimum cookie size they will accept). Each cookie `j` has a size `s_j`. Each child gets
at most one cookie, and each cookie goes to at most one child. A child is content if
their cookie's size >= their greed factor. Maximize the number of content children.

### Input Format

```
n m
g_1 g_2 ... g_n
s_1 s_2 ... s_m
```

### Output Format

A single integer: the maximum number of content children.

### Constraints

- 1 <= n, m <= 3 * 10^4
- 1 <= g_i, s_j <= 2^31 - 1

### Examples

```
Input:
3 2
1 2 3
1 1

Output:
1
```

Explanation: Only one child (greed=1) can be satisfied with a cookie of size 1.

```
Input:
2 3
1 2
1 2 3

Output:
2
```

### Hints

1. Sort both children (by greed) and cookies (by size) in ascending order.
2. Use two pointers: one for children, one for cookies.
3. For each cookie, if it satisfies the current (least greedy remaining) child,
   assign it and move both pointers. Otherwise, this cookie is too small for any
   remaining child? No -- move the cookie pointer (try a bigger cookie).

Actually, the correct greedy: try to satisfy the least greedy child first with the
smallest cookie that works. This maximizes remaining cookies for greedier children.

### Greedy Choice Proof

```
Sort children by greed ascending, cookies by size ascending.
For the least greedy child, the smallest cookie that satisfies them
is the best choice. Using a bigger cookie would be wasteful -- that
bigger cookie might be needed for a greedier child.

Exchange argument: If the optimal assigns a bigger cookie to the least
greedy child, we can swap it with a smaller satisfying cookie (if one
exists) without reducing total content children.
```

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
    let m: usize = next!(usize);
    let mut greed: Vec<i64> = (0..n).map(|_| next!(i64)).collect();
    let mut cookies: Vec<i64> = (0..m).map(|_| next!(i64)).collect();

    // TODO: Sort both arrays ascending

    // TODO: Two-pointer greedy
    //   let mut child = 0;   // index into greed
    //   let mut cookie = 0;  // index into cookies
    //   while child < n && cookie < m:
    //     if cookies[cookie] >= greed[child]:
    //       child satisfied! increment both
    //     else:
    //       cookie too small for this child, try next cookie

    // TODO: Print the number of satisfied children (= child pointer value)
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
    let m: usize = next!(usize);
    let mut greed: Vec<i64> = (0..n).map(|_| next!(i64)).collect();
    let mut cookies: Vec<i64> = (0..m).map(|_| next!(i64)).collect();

    greed.sort_unstable();
    cookies.sort_unstable();

    let mut child: usize = 0;
    let mut cookie: usize = 0;

    while child < n && cookie < m {
        if cookies[cookie] >= greed[child] {
            // This cookie satisfies this child
            child += 1;
        }
        // Move to next cookie regardless
        cookie += 1;
    }

    writeln!(out, "{}", child).unwrap();
}
```

**Key insight**: The cookie pointer always advances. If a cookie is too small for the
current child, it is too small for all remaining children (since they are sorted), so
it cannot be used at all.

**Complexity**: O(n log n + m log m) for sorting, O(n + m) for the two-pointer scan.

</details>

---

## Summary: When Greedy Works vs Fails

| Problem Type             | Greedy Works? | Why / Why Not                            |
|--------------------------|---------------|------------------------------------------|
| Activity/interval select | Yes           | Earliest end time = greedy choice        |
| Fractional knapsack      | Yes           | Best ratio first is provably optimal     |
| 0/1 knapsack             | **No**        | Cannot split items; need DP              |
| Jump game (reachability) | Yes           | Farthest reach is monotonic              |
| Coin change (canonical)  | Yes           | Special denomination structure           |
| Coin change (arbitrary)  | **No**        | Counterexample: `[1,3,4]` target 6      |
| Assign cookies           | Yes           | Smallest sufficient match is optimal     |
| Huffman coding           | Yes           | Merge smallest frequencies first         |
| Shortest path (general)  | **No**        | Negative edges break greedy (use DP/BF)  |

### Common Pitfalls

- **Assuming greedy works without proof**: Always verify the greedy choice property.
  If in doubt, try small counterexamples.
- **Forgetting to sort**: Most greedy algorithms require sorted input as a first step.
- **Integer overflow**: When computing sums or products in greedy, use `i64` or `i128`.
- **Off-by-one in two-pointer greedy**: Make sure both pointers advance correctly.
- **Greedy on unrelated problems**: Problems requiring optimal substructure across all
  choices (like longest common subsequence) cannot be solved greedily.
