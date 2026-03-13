# 29. CP: Suffix Array and LCP

**Difficulty**: Insane

## The Challenge

The suffix array is arguably the most versatile string data structure in competitive programming, serving as a space-efficient alternative to suffix trees while enabling solutions to an enormous class of string problems. A suffix array is simply the array of all suffixes of a string sorted in lexicographic order, represented by their starting indices. What makes it formidable is the combination of efficient construction algorithms (O(n log n) or even O(n)) with the Longest Common Prefix (LCP) array, which records the length of the longest common prefix between consecutive suffixes in sorted order. Together, these two arrays unlock answers to problems that would otherwise require complex suffix tree or suffix automaton implementations: counting distinct substrings, finding the longest repeated substring, computing the longest common substring of multiple strings, performing fast pattern matching, and answering arbitrary LCP queries between any two suffixes using range minimum queries on the LCP array.

Your task is to implement O(n log n) suffix array construction using the prefix-doubling (also called "Karp-Miller-Rosenberg") approach with radix sort, and then optionally implement the SA-IS (Suffix Array by Induced Sorting) algorithm for linear-time construction. You must implement Kasai's algorithm for computing the LCP array in O(n) from the suffix array and the inverse suffix array. On top of these foundational structures, you will build a sparse table for O(1) range minimum queries on the LCP array, enabling O(1) LCP queries between any two suffixes. The prefix-doubling approach iteratively sorts suffixes by their first 2^k characters, using the previous round's ranking as keys for radix sort in each new round, achieving O(n log n) total. SA-IS works by classifying suffixes as S-type or L-type, identifying the LMS (left-most S-type) suffixes as a reduced problem, recursively sorting them, and then inducing the full sorted order in linear time. Both algorithms demand meticulous implementation in Rust, with careful attention to sentinel characters, array bounds, and the distinction between ranks and indices.

Beyond the raw construction, this challenge requires you to solve five complete string problems using your suffix array and LCP infrastructure: counting the number of distinct substrings of a string, finding the longest repeated substring (appearing at least twice), computing the longest common substring of two strings via the concatenation trick with a separator character, performing pattern matching (finding all occurrences of a pattern in a text) via binary search on the suffix array, and counting the total number of occurrences of multiple patterns. Each problem tests a different aspect of your understanding: distinct substring counting exploits the relationship between total substrings and the LCP array, the longest repeated substring is the maximum value in the LCP array, the longest common substring requires tracking which original string each suffix belongs to, and pattern matching leverages the sorted order to find the range of suffixes that share the pattern as a prefix. Throughout, you must handle edge cases including empty strings, single-character strings, strings with all identical characters, and inputs with characters spanning the full ASCII range.

## Acceptance Criteria

### Suffix Array Construction: Prefix-Doubling O(n log n)

- [ ] Implement the prefix-doubling (Karp-Miller-Rosenberg) algorithm
  - Start with initial ranks based on character values
  - In each iteration, sort suffixes by pairs `(rank[i], rank[i + 2^k])` using radix sort
  - Use two passes of counting sort (radix sort on second key, then first key) for O(n) per iteration
  - Perform O(log n) iterations until all ranks are unique or the doubling length exceeds n
  - The final `sa[]` array maps sorted position to starting index: `sa[i]` is the starting index of the i-th lexicographically smallest suffix
  - Also compute the inverse suffix array `rank[]` where `rank[i]` is the sorted position of the suffix starting at index i

- [ ] Handle the sentinel / boundary conditions correctly
  - When `i + 2^k >= n`, the second key is effectively -infinity (that suffix is shorter and thus lexicographically smaller among suffixes sharing the same first key)
  - Assign rank 0 or -1 to out-of-bounds positions to handle this
  - Ensure stable sorting so that ties are broken consistently

- [ ] Verify correctness on known examples
  - `"banana"` should produce suffix array `[5, 3, 1, 0, 4, 2]` (suffixes: "a", "ana", "anana", "banana", "na", "nana")
  - `"aaaaaa"` should produce `[5, 4, 3, 2, 1, 0]`
  - `"abcabc"` should be verified against a naive O(n^2 log n) sort
  - Empty string produces an empty suffix array

### Suffix Array Construction: SA-IS O(n) (Stretch Goal)

- [ ] Implement the SA-IS algorithm by Nong, Zhang, and Chan
  - Classify each suffix as S-type (suffix[i] < suffix[i+1] lexicographically) or L-type
  - Identify LMS (Left-Most S-type) suffixes: positions where `type[i] = S` and `type[i-1] = L`
  - Place LMS suffixes into their buckets, then induce L-type suffixes left-to-right, then induce S-type suffixes right-to-left
  - Reduce the problem: compare adjacent LMS substrings, assign new alphabet symbols, and recursively build the suffix array of the reduced string
  - Use the recursively computed order of LMS suffixes to seed the final induced sorting pass
  - Handle the base case when all LMS substrings are unique (no recursion needed)

- [ ] Achieve true O(n) time and O(n) space
  - No sorting step beyond bucket placement and induction
  - The recursion depth is O(log n) in the worst case, but total work across all levels is O(n)
  - Use a single working buffer to avoid per-level allocations

- [ ] Produce identical results to the prefix-doubling algorithm on all test inputs

### LCP Array: Kasai's Algorithm O(n)

- [ ] Implement Kasai's algorithm (also known as the Kasai-Lee-Arimura-Arikawa-Park algorithm)
  - Input: the string, the suffix array `sa[]`, and the inverse suffix array `rank[]`
  - Initialize `k = 0` (the current LCP length being tracked)
  - Iterate through positions `i = 0, 1, ..., n-1` in text order (not sorted order)
  - For each position i, compare the suffix at i with the suffix immediately preceding it in sorted order: `sa[rank[i] - 1]`
  - Exploit the key insight: `lcp[rank[i]] >= lcp[rank[i-1]] - 1`, so `k` only decreases by at most 1 per step, giving amortized O(n) total comparisons
  - `lcp[i]` stores the length of the longest common prefix between `sa[i-1]` and `sa[i]` (for `i >= 1`)
  - `lcp[0]` is undefined or 0 (no predecessor in sorted order)

- [ ] Verify on known examples
  - `"banana"`: LCP array should be `[-, 1, 3, 0, 0, 2]` (where `-` denotes undefined for position 0)
  - `"aaaaaa"`: LCP array should be `[-, 1, 2, 3, 4, 5]`
  - `"abcdef"` (all distinct characters): LCP array should be all zeros (except position 0)

### Sparse Table for Range Minimum Queries

- [ ] Build a sparse table over the LCP array for O(1) range minimum queries
  - Precompute `sparse[k][i] = min(lcp[i], lcp[i+1], ..., lcp[i + 2^k - 1])` for all valid k, i
  - Construction: O(n log n) time and space
  - Query `rmq(l, r)`: compute `k = floor(log2(r - l + 1))`, return `min(sparse[k][l], sparse[k][r - 2^k + 1])`
  - Precompute the logarithm table to avoid floating-point log calls during queries

- [ ] Implement the `lcp_query(i, j)` function
  - Given two positions i and j in the original string, return the length of their longest common prefix
  - Convert to sorted positions using `rank[]`: let `a = rank[i]`, `b = rank[j]`, ensure `a < b`
  - The answer is `rmq(a + 1, b)` on the LCP array
  - Handle the edge case where `i == j` (LCP is `n - i`)

### Problem 1: Number of Distinct Substrings

- [ ] Given a string of length n, compute the total number of distinct substrings
  - Every suffix of length `len` contributes `len` substrings (its prefixes), giving `n*(n+1)/2` total
  - But substrings shared between consecutive sorted suffixes are counted multiple times
  - The number of duplicates is exactly `sum(lcp[i] for i in 1..n)`
  - Answer: `n*(n+1)/2 - sum(lcp[i])`
  - Constraints: `1 <= n <= 1_000_000`, string consists of lowercase English letters
  - Verify: `"abab"` has 7 distinct substrings: "a", "ab", "aba", "abab", "b", "ba", "bab"

- [ ] Handle edge cases
  - Single character: answer is 1
  - All identical characters (e.g., `"aaaa"`): answer is n
  - All distinct characters (e.g., `"abcd"`): answer is `n*(n+1)/2`

### Problem 2: Longest Repeated Substring

- [ ] Find the longest substring that appears at least twice in the string
  - The answer is the maximum value in the LCP array
  - Also output the actual substring (not just its length), using `sa[i]` and the max LCP position to extract it
  - If no substring repeats (all LCP values are 0), output that no repeated substring exists
  - Constraints: `1 <= n <= 1_000_000`

- [ ] Extend to "longest substring appearing at least k times"
  - Use a sliding window of size k-1 over the LCP array, tracking the minimum in each window
  - The answer is the maximum of these minimums
  - Use a monotonic deque (sliding window minimum) for O(n) total
  - Constraints: `2 <= k <= n`

- [ ] Verify with examples
  - `"banana"`: longest repeated substring is `"ana"` (length 3)
  - `"abcabc"`: longest repeated substring is `"abc"` (length 3)
  - `"abcdef"`: no repeated substring
  - `"aaaaaa"`: longest repeated is `"aaaaa"` (length 5, appears at positions 0 and 1)

### Problem 3: Longest Common Substring of Two Strings

- [ ] Given two strings s1 and s2, find the longest substring common to both
  - Concatenate as `s1 + '#' + s2` where `'#'` is a separator not appearing in either string
  - Build the suffix array and LCP array of the concatenated string
  - The answer is the maximum LCP[i] such that `sa[i-1]` and `sa[i]` belong to different original strings
  - A suffix belongs to s1 if `sa[i] < len(s1)`, and to s2 if `sa[i] > len(s1)` (skipping the separator)
  - Constraints: `1 <= |s1|, |s2| <= 500_000`

- [ ] Output both the length and the actual common substring

- [ ] Handle edge cases
  - One string is a substring of the other
  - No common substring (answer is 0, e.g., `"abc"` and `"xyz"`)
  - Both strings are identical
  - One or both strings have length 1

- [ ] Extend to longest common substring of k strings (stretch goal)
  - Concatenate all k strings with distinct separators
  - Use a sliding window on the sorted suffix array, tracking how many distinct original strings are represented
  - Binary search on the answer length, or use a more direct approach with the LCP array

### Problem 4: Pattern Matching via Suffix Array

- [ ] Given a text t and a pattern p, find all starting positions where p occurs in t
  - Build the suffix array of t
  - Binary search for the lower bound: the first suffix in sorted order that has p as a prefix
  - Binary search for the upper bound: the last such suffix
  - All suffixes in the range [lower, upper] are matches; report their starting positions `sa[lower..=upper]`
  - Each binary search step compares the pattern against a suffix in O(|p|) time
  - Total: O(|p| * log|t|) for the search, after O(|t| log |t|) preprocessing

- [ ] Optimize the binary search using the LCP array
  - Use the "LCP-aware binary search" technique that avoids redundant character comparisons
  - Maintain the LCP between the pattern and the current low/high boundaries
  - This reduces the total comparison cost to O(|p| + log|t|) per query
  - Implement both the naive and optimized versions and compare performance

- [ ] Constraints: `1 <= |t| <= 1_000_000`, `1 <= |p| <= |t|`, up to 100_000 patterns queried against the same text

- [ ] Verify correctness
  - Pattern at the beginning, middle, and end of text
  - Pattern occurring zero times, once, and multiple times
  - Pattern equal to the entire text
  - Single-character pattern

### Problem 5: Counting Total Occurrences of Multiple Patterns

- [ ] Given a text t and q patterns p_1, ..., p_q, for each pattern report the number of occurrences
  - Reuse the suffix array built once for t
  - For each pattern, perform the binary search to find the range [lower, upper] in the suffix array
  - The count is `upper - lower + 1` (or 0 if pattern not found)
  - Total: O(|t| log |t| + sum(|p_i|) * log|t|)
  - Constraints: `1 <= |t| <= 1_000_000`, `1 <= q <= 100_000`, `sum(|p_i|) <= 1_000_000`

- [ ] Output the count for each pattern on a separate line

### Performance Requirements

- [ ] Suffix array construction (prefix-doubling): O(n log n) time
  - Benchmark with n = 1_000_000 random lowercase string: must complete in under 1 second in release mode
  - Benchmark with n = 1_000_000 string of all 'a's (worst case for some implementations): under 1 second

- [ ] Suffix array construction (SA-IS, if implemented): O(n) time
  - Benchmark with n = 1_000_000: must complete in under 500ms
  - Must be measurably faster than the prefix-doubling approach on strings of length >= 500_000

- [ ] LCP array construction (Kasai): O(n) time
  - Benchmark with n = 1_000_000: under 200ms

- [ ] Sparse table construction: O(n log n) time, queries O(1)
  - Benchmark 1_000_000 random LCP queries after construction: under 500ms total

- [ ] Pattern matching with 100_000 patterns against 1M text: under 2 seconds total

- [ ] Memory: suffix array, LCP array, rank array, and sparse table must fit in under 200MB for n = 1_000_000

### Competitive Programming I/O

- [ ] Provide a `main()` function with fast I/O using `BufReader` and `BufWriter`
  - Read from stdin, write to stdout
  - Use a command or first-line indicator to select which problem to solve
  - Parse input efficiently (avoid per-line allocations where possible)

- [ ] Input/output format for each problem is clearly specified in the code comments

### Code Structure and Quality

- [ ] Organize into reusable modules
  - `mod suffix_array` containing `build_sa_prefix_doubling`, `build_sa_sais` (if implemented), `build_lcp_kasai`
  - `mod sparse_table` containing `SparseTable::new(data)`, `SparseTable::query(l, r)`
  - `mod problems` containing each problem solver as a standalone function

- [ ] Each algorithm function has a doc comment explaining the algorithm, its time/space complexity, and invariants

- [ ] No `unsafe` code unless clearly justified (e.g., unchecked indexing for inner-loop performance with a documented safety proof)

- [ ] Use `i64` or `usize` as appropriate; document any overflow considerations

### Testing and Verification

- [ ] Unit tests for suffix array construction
  - At least 5 hand-crafted strings with known suffix arrays
  - Stress test: generate 10_000 random strings of length 100, compare against naive O(n^2 log n) suffix sorting
  - Property test: for every valid suffix array, `text[sa[i]..] < text[sa[i+1]..]` lexicographically

- [ ] Unit tests for LCP array
  - Verify against brute-force character-by-character LCP computation for small strings
  - Verify the identity `sum(lcp[i]) + n*(n+1)/2 - sum(lcp[i]) = n*(n+1)/2` (tautology check that LCP values are non-negative and bounded)
  - Verify `lcp[i] <= n - sa[i]` and `lcp[i] <= n - sa[i-1]` for all valid i

- [ ] Unit tests for sparse table
  - Verify `rmq(i, i) == lcp[i]` for all valid i
  - Verify `rmq(0, n-1)` equals the global minimum of the LCP array
  - Stress test against naive O(n) range minimum for 100_000 random queries

- [ ] Unit tests for each problem
  - At least 3 hand-crafted test cases per problem with known expected outputs
  - Cross-validation: for the distinct-substrings problem, generate all substrings via brute force and count unique ones via HashSet, compare against the formula-based answer
  - For pattern matching, compare against `str::find` / `str::matches` in Rust's standard library

- [ ] Benchmarks
  - Use `std::time::Instant` or Rust's built-in benchmarking to measure each component
  - Print timing breakdowns: SA construction, LCP construction, sparse table construction, and query phase
  - Verify all performance targets on a representative machine in release mode

## Starting Points

- **cp-algorithms**: Suffix Array article at cp-algorithms.com covers both the O(n log n) prefix-doubling construction with counting sort and the O(n log^2 n) simpler variant with comparison-based sorting, along with applications
- **cp-algorithms**: Suffix Array Applications article covers distinct substrings, longest repeated substring, longest common substring, and pattern matching with detailed explanations
- **Nong, Zhang, Chan (2009)**: "Two Efficient Algorithms for Linear Time Suffix Array Construction" -- the original SA-IS paper, available on ResearchGate and IEEE
- **Kasai et al. (2001)**: "Linear-Time Longest-Common-Prefix Computation in Suffix Arrays and Its Applications" -- the original LCP construction paper
- **Competitive Programming 4** by Steven Halim: Chapter on string processing covers suffix arrays and all five problem types with worked examples
- **e-maxx (cp-algorithms predecessor)**: Russian-language articles with pseudocode that many competitive programmers reference
- **Stanford ACM-ICPC Team Reference**: Contains compact suffix array implementations suitable for contest use

## Hints

1. **The prefix-doubling approach has a subtle boundary condition.** When computing the second key for suffix `i` in iteration `k`, you need `rank[i + 2^(k-1)]`. If `i + 2^(k-1) >= n`, this suffix is shorter than the doubling length and should sort before longer suffixes with the same prefix. Assign rank -1 (or 0 if using unsigned) to out-of-bounds positions and ensure your radix sort handles this correctly.

2. **Radix sort with counting sort is essential for O(n log n).** A comparison-based sort in each iteration gives O(n log^2 n), which may be too slow for n = 10^6 with tight time limits. Implement two-pass counting sort: first by the second key, then stable-sort by the first key. The alphabet size for counting sort is at most n (the number of distinct ranks), so each pass is O(n).

3. **Kasai's algorithm exploits a beautiful invariant.** If the LCP between suffixes at ranks r and r-1 is k, then the LCP between the suffixes starting one position later in the text (at ranks rank[i+1] and some neighbor) is at least k-1. This means the variable `k` in the algorithm decreases by at most 1 per iteration of the outer loop, so the total number of character comparisons across all iterations is at most 2n.

4. **For the distinct-substrings problem**, think about it from the perspective of new substrings each suffix introduces. The suffix `sa[i]` has length `n - sa[i]` and thus introduces `n - sa[i]` prefixes as substrings. But `lcp[i]` of these are shared with the previous suffix in sorted order. So the new substrings introduced by position i in the sorted order is `(n - sa[i]) - lcp[i]`. Summing gives the total.

5. **For the longest common substring with the separator trick**, be very careful that the separator character does not appear in either input string and that it compares as less than or greater than all characters in the alphabet. Using `'#'` (ASCII 35) works when strings contain only lowercase letters. Also ensure that LCP values are not inflated by comparisons that cross the separator -- since the separator is unique, any LCP that would cross it stops at the separator.

6. **Binary search on the suffix array for pattern matching has a subtlety with `lower_bound` vs `upper_bound`.** For the lower bound, you want the first suffix that is >= the pattern. For the upper bound, you want the last suffix that starts with the pattern, which is the last suffix < pattern + "infinity" (in practice, the last suffix where the first |p| characters equal p). Implement these as two separate binary searches with slightly different comparison functions.

7. **The LCP-aware binary search optimization** maintains two values: `lcp_lo` (the LCP between the pattern and `text[sa[lo]..]`) and `lcp_hi` (the LCP between the pattern and `text[sa[hi]..]`). When examining the midpoint, you already know that the first `min(lcp_lo, lcp_hi)` characters match, so you can start comparing from that position. This avoids re-comparing characters you have already verified.

8. **For the SA-IS algorithm**, the trickiest part is the induced sorting. After placing the LMS suffixes, you scan left-to-right to induce L-type suffixes and right-to-left to induce S-type suffixes. The key insight is that if `sa[j] - 1` is L-type, it belongs in the next available position in its bucket's beginning, and if it's S-type, it belongs in the next available position in its bucket's end. Getting the bucket boundary bookkeeping right is where most bugs occur.

9. **In Rust, string slicing by byte index is natural since `&str` is UTF-8, but for competitive programming you almost always work with ASCII.** Convert the input to `&[u8]` or `Vec<u8>` early and work with byte values directly. This avoids any UTF-8 boundary issues and lets you index freely. Use `s.as_bytes()` for the conversion.

10. **Memory layout matters for performance at n = 10^6.** Keep the suffix array, rank array, and LCP array as contiguous `Vec<usize>` or `Vec<i32>` (using `i32` halves memory and improves cache performance when n fits in 2^31). The sparse table should also be a flat 2D array stored as `Vec<Vec<i32>>` or a single `Vec<i32>` with manual index calculation for better cache locality.

11. **When testing the concatenation trick for longest common substring**, a common bug is to forget that the concatenated string has length `|s1| + 1 + |s2|` (including the separator). All index calculations must account for this. Also, the LCP between two suffixes from different strings can never exceed `min(remaining_length_in_s1, remaining_length_in_s2)` because the separator blocks further matching.

12. **For the "at least k times" variant of longest repeated substring**, the sliding window minimum with a monotonic deque is the cleanest approach. Maintain a deque of indices into the LCP array such that `lcp[deque[i]]` is non-decreasing. When the window slides past an element, pop from the front. When inserting a new element, pop from the back while the new element is smaller. The front of the deque always holds the minimum. This gives O(n) total.
