# 105. Suffix Array Construction

<!--
difficulty: advanced
category: search-engines-text-processing
languages: [rust]
concepts: [suffix-array, sa-is-algorithm, lcp-array, substring-search, induced-sorting, text-indexing]
estimated_time: 12-16 hours
bloom_level: evaluate
prerequisites: [sorting-algorithms, binary-search, string-algorithms, recursion, arrays-slices]
-->

## Languages

- Rust (stable)

## Prerequisites

- Sorting algorithms and their complexity analysis
- Binary search over sorted arrays
- String comparison and lexicographic ordering
- Recursion and divide-and-conquer strategies
- Array/slice manipulation and index arithmetic

## Learning Objectives

- **Implement** suffix array construction using the SA-IS (Suffix Array by Induced Sorting) algorithm in O(n) time
- **Implement** LCP (Longest Common Prefix) array construction using Kasai's algorithm in O(n) time
- **Apply** binary search over the suffix array for O(m log n) pattern matching where m is pattern length
- **Analyze** how the LCP array accelerates substring search and enables efficient longest repeated substring queries
- **Evaluate** the space-time trade-offs between suffix arrays, suffix trees, and naive approaches for text indexing

## The Challenge

Substring search is a fundamental operation: given a text of N characters and a pattern of M characters, find all positions where the pattern appears. Naive search is O(N*M). KMP and Boyer-Moore achieve O(N+M) for a single pattern but must rescan the text for each new query. When the text is static and queries are many, you need an index.

A suffix array is that index. It is the sorted array of all suffixes of a text, represented by their starting positions. Because suffixes are sorted, binary search locates any pattern in O(M log N) time. Combined with an LCP array (the length of the longest common prefix between adjacent suffixes in sorted order), many string problems become trivial: longest repeated substring, number of distinct substrings, longest common substring between two strings.

The challenge is construction. Naive suffix sorting is O(N^2 log N). The SA-IS algorithm (Suffix Array by Induced Sorting) constructs the suffix array in O(N) time and O(N) space, matching the theoretical lower bound. It classifies suffixes into S-type and L-type, identifies LMS (leftmost S-type) suffixes as pivots, recursively sorts them, then induces the order of all other suffixes from the LMS order.

Build a suffix array construction library in Rust implementing SA-IS, Kasai's LCP algorithm, and pattern matching over the resulting index.

## Requirements

1. Implement **suffix type classification**: scan the input right-to-left, classifying each suffix as S-type (lexicographically smaller than its right neighbor) or L-type (larger). Mark LMS (Left-Most S-type) positions where an L-type is immediately followed by an S-type
2. Implement **bucket sorting**: determine the bucket (range of positions in the suffix array) for each character. L-type suffixes fill buckets from the front, S-type from the back
3. Implement **induced sorting**: given the sorted order of LMS suffixes, induce the positions of all L-type suffixes (left-to-right scan) then all S-type suffixes (right-to-left scan)
4. Implement **recursion**: if LMS suffixes have duplicate rank, reduce the problem by creating a new string from LMS ranks and recursively constructing its suffix array. Use the recursive result to determine the true LMS order
5. Implement the complete **SA-IS algorithm** combining the above steps. The function signature should be `fn build_suffix_array(text: &[u8]) -> Vec<usize>`
6. Implement **Kasai's algorithm** for LCP array construction in O(n): using the inverse suffix array, compute LCP values by exploiting the property that `LCP[rank[i]] >= LCP[rank[i-1]] - 1`
7. Implement **pattern search**: binary search over the suffix array to find the range of suffixes that start with the pattern. Return all matching positions
8. Implement **longest repeated substring**: scan the LCP array for the maximum value; the corresponding suffix gives the answer
9. Implement **count of distinct substrings**: total substrings minus sum of LCP values (`n*(n+1)/2 - sum(LCP)` adjusted for sentinel)
10. Handle the **sentinel character**: append a unique character (byte 0 or `$`) smaller than any character in the input to ensure correct suffix ordering. All implementations must account for the sentinel in their output

## Hints

<details>
<summary>Hint 1: S-type and L-type classification</summary>

Scan right-to-left. The last character (sentinel) is S-type by convention. For each position `i` from `n-2` down to `0`:
- If `text[i] > text[i+1]`: L-type
- If `text[i] < text[i+1]`: S-type
- If `text[i] == text[i+1]`: same type as `i+1`

An LMS position is where `type[i] = S` and `type[i-1] = L`. These are the "interesting" suffixes that anchor the recursion.

```rust
let mut types = vec![false; n]; // false = L, true = S
types[n - 1] = true; // sentinel is S-type
for i in (0..n - 1).rev() {
    types[i] = if text[i] < text[i + 1] {
        true
    } else if text[i] > text[i + 1] {
        false
    } else {
        types[i + 1]
    };
}
```
</details>

<details>
<summary>Hint 2: Bucket computation and induced sorting</summary>

Each character defines a bucket (contiguous range in the suffix array). Compute bucket boundaries from character frequencies. During induced sorting:

1. Place LMS suffixes at the ends of their buckets (right to left within each bucket)
2. Left-to-right pass: for each filled position SA[i], if SA[i]-1 is L-type, place it at the front of its bucket
3. Right-to-left pass: for each filled position SA[i], if SA[i]-1 is S-type, place it at the end of its bucket

The key insight: LMS suffixes alone determine the order of all other suffixes through induction.
</details>

<details>
<summary>Hint 3: Recursive reduction</summary>

After the initial induced sort, check if all LMS suffixes have unique ranks. If yes, the LMS order is determined and no recursion is needed. If not:

1. Assign ranks to LMS substrings (LMS suffix up to the next LMS position). Equal substrings get equal ranks.
2. Create a reduced string of these ranks.
3. Recursively call SA-IS on the reduced string.
4. Use the recursive result to place LMS suffixes in correct order, then induce the full suffix array.

This recursion terminates because each level reduces the input by at least half (LMS suffixes are at most n/2 of all suffixes).
</details>

<details>
<summary>Hint 4: Kasai's LCP algorithm</summary>

The inverse suffix array `rank[i]` gives the position of suffix `i` in the sorted order. Kasai's key insight: if suffix `i` and its predecessor in sorted order share a prefix of length `h`, then suffix `i+1` and its predecessor share at least `h-1`. So compute LCP values in text order (not sorted order), starting each comparison from the previous LCP minus 1:

```rust
let mut h: usize = 0;
for i in 0..n {
    if rank[i] > 0 {
        let j = sa[rank[i] - 1];
        while i + h < n && j + h < n && text[i + h] == text[j + h] {
            h += 1;
        }
        lcp[rank[i]] = h;
        if h > 0 { h -= 1; }
    }
}
```
</details>

<details>
<summary>Hint 5: Binary search for pattern matching</summary>

The suffix array is sorted lexicographically. Binary search finds the leftmost and rightmost suffix that starts with the pattern:

- Lower bound: find the first position where the suffix is >= the pattern
- Upper bound: find the first position where the suffix is > the pattern

The range `[lower, upper)` contains all occurrences. Each comparison is O(m) where m is the pattern length, so total search time is O(m log n).

With LCP information, you can accelerate this to O(m + log n) by skipping prefix comparisons, but the simpler O(m log n) approach is sufficient for this challenge.
</details>

## Acceptance Criteria

- [ ] SA-IS constructs the correct suffix array for known test strings (verify against naive O(n^2 log n) sort)
- [ ] Construction runs in O(n) time: verify by measuring construction time for inputs of 1M, 2M, and 4M characters (should scale linearly)
- [ ] Kasai's algorithm produces correct LCP values for known inputs
- [ ] Pattern search finds all occurrences and returns correct positions
- [ ] Pattern search returns empty result for patterns not in the text
- [ ] Longest repeated substring is correct for multiple test cases
- [ ] Distinct substring count matches expected values for small strings (verifiable by brute force)
- [ ] Sentinel character is handled correctly in all operations
- [ ] Algorithm handles edge cases: single character, all identical characters, already sorted text, reverse sorted text
- [ ] Memory usage is O(n): no auxiliary structures beyond the suffix array, LCP array, and type array
- [ ] All tests pass with `cargo test`

## Research Resources

- [Nong, Zhang, Chan: "Two Efficient Algorithms for Linear Time Suffix Array Construction" (2009)](https://ieeexplore.ieee.org/document/5582081) -- the original SA-IS paper
- [Nong: "Practical Linear-Time O(1)-Workspace Suffix Sorting for Constant Alphabets" (2013)](https://dl.acm.org/doi/10.1145/2493175) -- space-optimized variant
- [Kasai et al.: "Linear-Time Longest-Common-Prefix Computation in Suffix Arrays and Its Applications" (2001)](https://link.springer.com/chapter/10.1007/3-540-48194-X_17) -- Kasai's LCP algorithm
- [SA-IS Algorithm Walkthrough (cp-algorithms)](https://cp-algorithms.com/string/suffix-array.html) -- step-by-step explanation with pseudocode
- [Suffix Array (Wikipedia)](https://en.wikipedia.org/wiki/Suffix_array) -- overview and relationship to suffix trees
- [Abouelhoda et al.: "Replacing Suffix Trees with Enhanced Suffix Arrays" (2004)](https://www.sciencedirect.com/science/article/pii/S0196677404000262) -- shows suffix arrays + LCP can simulate suffix trees
- [libdivsufsort source](https://github.com/y-256/libdivsufsort) -- heavily optimized C implementation for reference
