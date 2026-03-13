# 32. CP: Convex Hull Trick

**Difficulty**: Insane

## The Challenge

The Convex Hull Trick (CHT) is one of the most powerful DP optimization techniques in competitive programming, transforming certain O(n^2) dynamic programming recurrences into O(n log n) or even O(n) solutions. The core insight is elegant: when your DP transition can be expressed as minimizing or maximizing a set of linear functions evaluated at a query point, you can maintain a convex hull of these lines and answer each query in logarithmic or amortized constant time. This technique appears in an astonishing number of competition problems, from straightforward cost minimization to deeply disguised recurrence relations that only reveal their linear structure after algebraic manipulation.

Your task is to implement both the offline (sorted-slopes) variant and the online (Li Chao tree) variant of the Convex Hull Trick, and then apply them to solve a suite of classic DP optimization problems. The offline variant leverages the monotonicity of query points and/or slopes to maintain a stack-based convex hull where insertions and queries run in amortized O(1). The Li Chao tree variant handles arbitrary insertion and query orders by maintaining a segment tree over the query domain, inserting lines lazily and answering point queries in O(log C) where C is the domain size. Both variants require careful handling of numerical precision, degenerate cases (parallel lines, coincident lines, integer overflow), and the distinction between upper and lower hulls depending on whether you are minimizing or maximizing.

Beyond the raw data structures, this challenge demands that you solve four complete problems end-to-end: a basic "minimum cost over linear functions" problem, the APIO Commando problem (partitioning soldiers into groups with a quadratic cost function that can be linearized), a frog jumping problem with quadratic cost (where the DP transition involves terms like `(j - i)^2`), and a problem requiring the Li Chao tree to handle insertions interleaved with queries in arbitrary order. Each problem requires you to recognize the CHT structure hidden in the recurrence, algebraically transform the transition into the required `min/max over (m*x + b)` form, and then apply the appropriate variant. You must handle all edge cases including negative coordinates, large values requiring 128-bit arithmetic, and queries at domain boundaries.

## Acceptance Criteria

### Core Data Structures

- [ ] Implement an offline Convex Hull Trick (CHT) structure for the **minimum** case
  - Lines are added with non-increasing slopes
  - Queries are made with non-decreasing x-values
  - Each line is represented as `y = m*x + b` with `i64` coefficients
  - Insertion amortized O(1) using a stack/deque with back-popping of dominated lines
  - Query amortized O(1) using a pointer that advances monotonically
  - Correctly detect and remove lines that are never optimal (the "bad line" check)
  - Handle the edge case where two lines have identical slopes (keep the one with smaller intercept for min)
  - Handle the edge case where the intersection point is exactly at a queried x-coordinate

- [ ] Implement an offline CHT structure for the **maximum** case
  - Symmetric to the minimum case but maintaining an upper hull
  - Lines added with non-decreasing slopes
  - Correctly handle the duality between upper and lower hulls

- [ ] Implement a **Li Chao tree** for arbitrary-order insertions and queries
  - Segment tree over a discretized or coordinate-compressed query domain
  - Support `add_line(m, b)` — insert a new line into the tree
  - Support `query(x) -> i64` — return the minimum (or maximum) value at point x
  - Each node stores at most one "dominant" line; insertions push non-dominant segments down recursively
  - O(log C) per insertion and O(log C) per query where C is the domain range
  - Support both static domain (known range upfront) and dynamic domain (coordinate compression)
  - Handle negative x-values in the query domain correctly

- [ ] Implement a **Li Chao tree with line segments** (not just full lines)
  - Support `add_segment(m, b, xl, xr)` — insert a line segment active only on [xl, xr]
  - Properly restrict the segment to only affect nodes whose intervals overlap [xl, xr]
  - O(log^2 C) per segment insertion

### Numerical Robustness

- [ ] All intersection computations avoid floating-point arithmetic entirely
  - Use the cross-multiplication trick: line `j` makes line `k` redundant if `(b_k - b_j) * (m_j - m_i) <= (b_j - b_i) * (m_k - m_j)` (adjusted for the comparison direction)
  - Handle potential `i64` overflow by using `i128` intermediate products where necessary
  - Document the maximum input magnitude your implementation supports without overflow

- [ ] Correctly handle degenerate cases
  - Two lines with identical slopes and different intercepts
  - Two identical lines
  - Three lines all intersecting at the same point
  - Query at x = 0 when all intercepts are equal
  - Lines with slope = 0 (horizontal lines)
  - Lines with very large slopes (near i64::MAX / domain_size)

- [ ] Include overflow-safe comparison functions
  - Implement a helper that compares `a/b` vs `c/d` without division, using cross-multiplication with `i128`
  - Prove (via comments or tests) that this avoids overflow for your stated input bounds

### Problem 1: Basic Minimum over Linear Functions

- [ ] Solve the following problem:
  - Given n lines `y_i = m_i * x + b_i` and q queries `x_j`, for each query return `min over all i of (m_i * x_j + b_i)`
  - Constraints: `1 <= n, q <= 500_000`, `-10^9 <= m_i, b_i, x_j <= 10^9`
  - Input: first line is `n q`, then n lines of `m_i b_i`, then q lines of `x_j`
  - Output: q lines, each the minimum value

- [ ] Solve using the offline CHT when queries are sorted
  - Sort lines by slope, sort queries by x-value
  - Achieve O(n log n + q log q) total from the sorting, O(n + q) for the CHT phase

- [ ] Solve using the Li Chao tree for unsorted queries
  - Insert all lines, then answer queries in input order
  - Achieve O((n + q) log C) where C is the x-domain range

- [ ] Both solutions produce identical output on all test cases

### Problem 2: APIO Commando (Soldier Partitioning)

- [ ] Solve the APIO 2010 "Commando" problem:
  - n soldiers in a line, each with fighting power `x_i`
  - Partition into contiguous groups; each group's strength is `a * S^2 + b * S + c` where `S` is the sum of fighting powers in the group, and `a, b, c` are given constants with `a < 0`
  - Maximize total strength over all groups
  - Constraints: `1 <= n <= 1_000_000`, `-5 <= a <= -1`, `-10^7 <= b, c <= 10^7`, `1 <= x_i <= 100`

- [ ] Define the DP recurrence:
  - `dp[i] = max over j < i of { dp[j] + a*(prefix[i] - prefix[j])^2 + b*(prefix[i] - prefix[j]) + c }`
  - Show the algebraic expansion that isolates terms into `slope * query_point + intercept` form
  - Document the derivation in comments: which variable is the "slope," which is the "query point"

- [ ] Apply the offline CHT (maximum version)
  - Verify that slopes are monotonic given the constraints (a < 0, x_i > 0, prefix sums increasing)
  - Verify that query points are monotonic
  - Achieve O(n) DP transitions after the CHT optimization

- [ ] Handle the arithmetic carefully
  - prefix sums can reach up to 10^8, squared terms up to 10^16, which fits in i64
  - But products of two such terms in the "bad line" check require i128
  - Validate with a test case where n = 1_000_000 and all x_i = 100

### Problem 3: Frog Jumping with Quadratic Cost

- [ ] Solve the following problem:
  - n stones at heights `h_1, h_2, ..., h_n`
  - A frog starts at stone 1 and must reach stone n
  - Jumping from stone i to stone j (j > i) costs `(h_j - h_i)^2 + C` for a given constant C
  - Find the minimum total cost
  - Constraints: `1 <= n <= 500_000`, `0 <= h_i <= 10^6`, `1 <= C <= 10^12`

- [ ] Define the DP recurrence:
  - `dp[i] = min over j < i of { dp[j] + (h_i - h_j)^2 + C }`
  - Expand: `dp[i] = min over j { dp[j] + h_j^2 - 2*h_i*h_j + h_i^2 + C }`
  - Identify the CHT form: line for each j has slope `-2*h_j` and intercept `dp[j] + h_j^2`, queried at `x = h_i`, plus the additive term `h_i^2 + C`

- [ ] Handle the non-monotonic query points
  - Heights `h_i` are NOT necessarily sorted, so queries are not monotonic
  - This means the offline CHT pointer trick does NOT apply
  - Use the Li Chao tree variant, or sort queries and process with offline CHT using careful index management
  - Document which approach you chose and why

- [ ] Verify with edge cases:
  - All heights equal (cost is just `C` per jump, problem reduces to choosing number of jumps)
  - Strictly increasing heights
  - Strictly decreasing heights
  - n = 2 (single jump, trivial)
  - Large C that discourages many jumps vs small C that encourages many jumps

### Problem 4: Online CHT with Interleaved Operations

- [ ] Solve the following problem using a Li Chao tree:
  - Process a sequence of operations:
    - `ADD m b` — add the line y = m*x + b
    - `QUERY x` — print the minimum y-value at position x across all lines added so far
  - Operations are interleaved in arbitrary order
  - Constraints: up to 500_000 operations, `-10^9 <= m, b, x <= 10^9`
  - At least one ADD occurs before the first QUERY

- [ ] The Li Chao tree must handle the full range of x-values
  - Either use a domain of [-10^9, 10^9] with the tree depth ~31
  - Or coordinate-compress the query x-values if known ahead of time (offline variant)
  - Implement both approaches and compare performance

- [ ] Ensure correctness against a brute-force oracle
  - For test inputs with n <= 5000, verify against O(n*q) brute force
  - Generate random operations and cross-check

### Performance Requirements

- [ ] Offline CHT (sorted slopes + sorted queries): amortized O(1) per insertion and query
  - Total runtime O(n + q) after sorting
  - Benchmark with n = q = 1_000_000 and verify < 100ms

- [ ] Li Chao tree: O(log C) per operation
  - Total runtime O((n + q) * log(2 * 10^9)) for the online problem
  - Benchmark with 500_000 operations and verify < 200ms

- [ ] Memory usage:
  - Offline CHT: O(n) for the line stack
  - Li Chao tree: O(n * log C) nodes in the worst case (with dynamic node allocation)
  - Implement the Li Chao tree with arena-based allocation (Vec<Node>) to avoid per-node heap allocations

- [ ] No heap allocations in the hot path (insertion/query) for the offline CHT
  - Pre-allocate the line vector with known capacity
  - The query pointer is just an index

### Testing and Verification

- [ ] Unit tests for the offline CHT:
  - Insert 3 lines, verify hull has the correct 2 or 3 lines
  - Query at intersection points returns the correct value
  - Query at extremes (very negative and very positive x) returns correct values
  - Stress test: insert 100_000 random lines with decreasing slopes, query 100_000 random sorted x-values, verify against brute force

- [ ] Unit tests for the Li Chao tree:
  - Single line: query at multiple points
  - Two crossing lines: query before, at, and after intersection
  - Many lines: stress test against brute force
  - Line segments: verify a segment only affects queries within its range

- [ ] End-to-end tests for each problem:
  - At least 3 hand-crafted test cases with known answers per problem
  - At least 1 stress test (random large input) per problem verified against brute-force or known-good solution
  - Edge case tests for each problem as described above

- [ ] Verify no integer overflow occurs in any test:
  - Use debug-mode overflow checks (`#[cfg(debug_assertions)]`)
  - Run the stress tests in both debug and release mode

### Code Quality

- [ ] Separate the CHT data structures into a reusable module
  - `mod cht { pub struct OfflineCHT { ... } pub struct LiChaoTree { ... } }`
  - Each struct has clear documentation on when to use it

- [ ] Each problem solver is a separate function that takes input and produces output
  - `fn solve_basic_min(lines: &[(i64, i64)], queries: &[i64]) -> Vec<i64>`
  - `fn solve_commando(n: usize, a: i64, b: i64, c: i64, x: &[i64]) -> i64`
  - `fn solve_frog(h: &[i64], c: i64) -> i64`
  - `fn solve_online(ops: &[Operation]) -> Vec<i64>`

- [ ] Include detailed comments explaining each algebraic transformation
  - For each problem, show the DP recurrence, the expansion, the identification of slope/intercept/query-point, and the monotonicity argument (if applicable)

- [ ] Provide a `main()` that reads from stdin in competitive-programming style (fast I/O) and dispatches to the appropriate solver based on a command-line argument or first input line

## Starting Points

- **cp-algorithms**: Convex Hull Trick article at cp-algorithms.com — covers the offline variant with detailed pseudocode and the "bad line" removal condition
- **cp-algorithms**: Li Chao Tree article — covers the segment tree approach with line-segment support
- **Jeff Erickson's Algorithms textbook**: Chapter on dynamic programming optimization techniques, free online
- **APIO 2010 Commando**: Available on online judges like SPOJ (APIO10A), Kattis, and the APIO archive — the canonical CHT problem
- **Codeforces blog**: "Convex Hull Trick — Pair of Lines" by adamant — excellent treatment of the algebraic details
- **PEG Wiki**: Convex Hull Trick page — compact reference with implementation notes
- **Competitive Programming 4** by Steven Halim: Section on DP optimization techniques including CHT

## Hints

1. **The "bad line" check is the heart of CHT.** When inserting a new line, you pop lines from the stack if the intersection of the new line with the second-to-last line comes before the intersection of the last two lines on the stack. Draw this on paper until it clicks — the popped line is now "below the hull" and can never be optimal.

2. **For the algebraic transformation**, the general pattern is: take your DP recurrence `dp[i] = min/max over j of { f(i, j) }`, expand `f(i, j)`, and group terms into those that depend only on `j` (these form the line's intercept), those that are a product of something depending on `j` and something depending on `i` (the slope times the query point), and those that depend only on `i` (additive constants outside the min/max). This separation is what makes CHT applicable.

3. **For the Commando problem**, after expanding `a*(P_i - P_j)^2 + b*(P_i - P_j) + c`, you get terms like `a*P_j^2 - 2a*P_i*P_j + dp[j] + ...`. The slope for line `j` is `-2a*P_j` and since `a < 0` and `P_j` is increasing, the slopes are increasing — perfect for the maximum CHT variant. The query point is `P_i`, also increasing. This gives O(n) total.

4. **For the frog problem**, the heights are not sorted, which breaks the monotone-query assumption. You have two options: (a) use a Li Chao tree which handles arbitrary query order natively, or (b) use a "divide and conquer" CHT approach where you process the DP in a specific order. The Li Chao tree approach is simpler and sufficient for the given constraints.

5. **Integer overflow is the most common source of WA in CHT problems.** In the Commando problem, prefix sums reach ~10^8, and the "bad line" cross-multiplication involves products of three such values, reaching ~10^24 — far beyond i64. Always use i128 for the comparison. In Rust, you can write `(a as i128) * (b as i128)` in the comparison functions.

6. **The Li Chao tree doesn't need to be built over the entire [-10^9, 10^9] range eagerly.** Use dynamic node creation: start with just a root node, and create children only when a line insertion recurses into them. This keeps memory proportional to O(n * log C) where n is the number of inserted lines, rather than O(C).

7. **For competitive programming I/O in Rust**, avoid using `println!` in a loop — it flushes after every line. Instead, build a `String` or use `BufWriter<Stdout>` to batch output. Similarly, use `BufReader` for input. This can make the difference between TLE and AC on problems with 500K queries.

8. **To debug CHT implementations**, print the hull after each insertion. For a correct lower hull (minimization), the slopes should be in increasing order, and for an upper hull (maximization), in decreasing order. If you see a slope out of order, your "bad line" check has a bug.

9. **Line segments in the Li Chao tree** are trickier than full lines. When inserting a segment [xl, xr], you need to descend the tree and only apply the line to nodes whose intervals are fully contained within [xl, xr]. For partially overlapping nodes, recurse into both children. This is similar to range updates in a segment tree and gives O(log^2 C) per segment.

10. **Testing strategy**: For each problem, first implement a brute-force O(n^2) solver. Then implement the CHT-optimized solver. Generate random inputs of increasing size and assert that both produce the same output. Once confident, run the CHT solver on the maximum-size input to verify performance. Keep the brute-force solver in your test suite permanently.
