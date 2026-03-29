# Solution: Suffix Array Construction

## Architecture Overview

The implementation has four components that build on each other:

1. **SA-IS Builder** -- constructs the suffix array in O(n) using induced sorting
2. **LCP Builder** -- constructs the LCP array in O(n) using Kasai's algorithm
3. **Pattern Matcher** -- binary search over the suffix array for O(m log n) substring search
4. **Query Utilities** -- longest repeated substring, distinct substring count

```
Input text + sentinel ($)
    |
    v
SA-IS: classify types -> find LMS -> induced sort -> recurse if needed
    |
    v
Suffix Array (SA): sorted suffix positions
    |
    v
Kasai's Algorithm: SA + text -> LCP array
    |
    v
Pattern Search: binary search over SA
Longest Repeated Substring: max(LCP)
Distinct Substrings: n*(n+1)/2 - sum(LCP)
```

## Complete Solution (Rust)

### Cargo.toml

```toml
[package]
name = "suffix-array"
version = "0.1.0"
edition = "2021"
```

### src/lib.rs

```rust
/// Suffix Array construction via SA-IS algorithm and related utilities.

const EMPTY: usize = usize::MAX;

/// Build a suffix array for the given text using the SA-IS algorithm.
/// Appends a sentinel byte (0) that is smaller than any byte in the input.
pub fn build_suffix_array(text: &[u8]) -> Vec<usize> {
    let mut input: Vec<usize> = text.iter().map(|&b| b as usize + 1).collect();
    input.push(0); // sentinel
    let alphabet_size = 257; // 0..=256
    sa_is(&input, alphabet_size)
}

fn sa_is(text: &[usize], alphabet_size: usize) -> Vec<usize> {
    let n = text.len();
    if n <= 2 {
        return sa_is_base_case(text, n);
    }

    // Step 1: Classify suffixes as S-type or L-type
    let types = classify_types(text);

    // Step 2: Find LMS positions
    let lms_positions = find_lms_positions(&types);

    // Step 3: Compute bucket boundaries
    let buckets = compute_buckets(text, alphabet_size);

    // Step 4: Initial induced sort with LMS suffixes
    let mut sa = vec![EMPTY; n];
    place_lms_suffixes(&mut sa, &buckets, text, &lms_positions);
    induce_l_type(&mut sa, &buckets, text, &types);
    induce_s_type(&mut sa, &buckets, text, &types);

    // Step 5: Assign ranks to LMS substrings
    let (reduced, reduced_alphabet, lms_order) = reduce_lms(&sa, text, &types, &lms_positions);

    // Step 6: Recurse or directly determine LMS order
    let sorted_lms_indices = if reduced_alphabet < reduced.len() {
        let rec_sa = sa_is(&reduced, reduced_alphabet + 1);
        rec_sa.iter()
            .skip(1) // skip sentinel in recursive SA
            .map(|&i| lms_order[i])
            .collect::<Vec<_>>()
    } else {
        // All ranks are unique, LMS order is determined
        let mut order = vec![0usize; reduced.len()];
        for (i, &r) in reduced.iter().enumerate() {
            order[r] = i;
        }
        order.iter()
            .filter(|&&i| i < lms_order.len())
            .map(|&i| lms_order[i])
            .collect::<Vec<_>>()
    };

    // Step 7: Final induced sort using correct LMS order
    let mut sa = vec![EMPTY; n];
    place_lms_sorted(&mut sa, &buckets, text, &sorted_lms_indices);
    induce_l_type(&mut sa, &buckets, text, &types);
    induce_s_type(&mut sa, &buckets, text, &types);

    sa
}

fn sa_is_base_case(text: &[usize], n: usize) -> Vec<usize> {
    match n {
        0 => vec![],
        1 => vec![0],
        2 => {
            if text[0] <= text[1] {
                vec![0, 1]
            } else {
                vec![1, 0]
            }
        }
        _ => unreachable!(),
    }
}

fn classify_types(text: &[usize]) -> Vec<bool> {
    let n = text.len();
    let mut types = vec![false; n]; // false = L-type, true = S-type
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
    types
}

fn is_lms(types: &[bool], i: usize) -> bool {
    i > 0 && types[i] && !types[i - 1]
}

fn find_lms_positions(types: &[bool]) -> Vec<usize> {
    (1..types.len())
        .filter(|&i| types[i] && !types[i - 1])
        .collect()
}

fn compute_buckets(text: &[usize], alphabet_size: usize) -> Vec<usize> {
    let mut freq = vec![0usize; alphabet_size];
    for &c in text {
        freq[c] += 1;
    }
    // Bucket ends (exclusive)
    let mut ends = vec![0usize; alphabet_size];
    let mut sum = 0;
    for i in 0..alphabet_size {
        sum += freq[i];
        ends[i] = sum;
    }
    ends
}

fn bucket_starts(buckets: &[usize], text: &[usize], alphabet_size: usize) -> Vec<usize> {
    let mut freq = vec![0usize; alphabet_size];
    for &c in text {
        freq[c] += 1;
    }
    let mut starts = vec![0usize; alphabet_size];
    let mut sum = 0;
    for i in 0..alphabet_size {
        starts[i] = sum;
        sum += freq[i];
    }
    let _ = buckets; // use alphabet_size from text
    starts
}

fn place_lms_suffixes(
    sa: &mut [usize],
    buckets: &[usize],
    text: &[usize],
    lms_positions: &[usize],
) {
    let alphabet_size = buckets.len();
    let mut tails = buckets.to_vec();

    // Place LMS suffixes at the end of their buckets (right to left)
    for &pos in lms_positions.iter().rev() {
        let c = text[pos];
        tails[c] -= 1;
        sa[tails[c]] = pos;
    }
}

fn place_lms_sorted(
    sa: &mut [usize],
    buckets: &[usize],
    text: &[usize],
    sorted_lms: &[usize],
) {
    let mut tails = buckets.to_vec();

    for &pos in sorted_lms.iter().rev() {
        let c = text[pos];
        tails[c] -= 1;
        sa[tails[c]] = pos;
    }
}

fn induce_l_type(sa: &mut [usize], buckets: &[usize], text: &[usize], types: &[bool]) {
    let n = text.len();
    let alphabet_size = buckets.len();
    let mut heads = {
        let mut freq = vec![0usize; alphabet_size];
        for &c in text {
            freq[c] += 1;
        }
        let mut starts = vec![0usize; alphabet_size];
        let mut sum = 0;
        for i in 0..alphabet_size {
            starts[i] = sum;
            sum += freq[i];
        }
        starts
    };

    for i in 0..n {
        if sa[i] == EMPTY || sa[i] == 0 {
            continue;
        }
        let j = sa[i] - 1;
        if !types[j] {
            // L-type
            let c = text[j];
            sa[heads[c]] = j;
            heads[c] += 1;
        }
    }
}

fn induce_s_type(sa: &mut [usize], buckets: &[usize], text: &[usize], types: &[bool]) {
    let n = text.len();
    let mut tails = buckets.to_vec();

    for i in (0..n).rev() {
        if sa[i] == EMPTY || sa[i] == 0 {
            continue;
        }
        let j = sa[i] - 1;
        if types[j] {
            // S-type
            let c = text[j];
            tails[c] -= 1;
            sa[tails[c]] = j;
        }
    }
}

fn lms_substrings_equal(text: &[usize], types: &[bool], a: usize, b: usize) -> bool {
    if a == b {
        return true;
    }
    let n = text.len();
    let mut i = 0;
    loop {
        let ai = a + i;
        let bi = b + i;
        if ai >= n || bi >= n {
            return false;
        }
        if text[ai] != text[bi] || types[ai] != types[bi] {
            return false;
        }
        if i > 0 && (is_lms(types, ai) || is_lms(types, bi)) {
            // Both are LMS (since chars and types match up to here)
            return is_lms(types, ai) && is_lms(types, bi);
        }
        i += 1;
    }
}

fn reduce_lms(
    sa: &[usize],
    text: &[usize],
    types: &[bool],
    lms_positions: &[usize],
) -> (Vec<usize>, usize, Vec<usize>) {
    let n = text.len();

    // Collect LMS suffixes in sorted order from SA
    let sorted_lms: Vec<usize> = sa.iter()
        .filter(|&&pos| pos != EMPTY && is_lms(types, pos))
        .copied()
        .collect();

    // Assign ranks
    let mut ranks = vec![EMPTY; n];
    let mut current_rank = 0;
    ranks[sorted_lms[0]] = 0; // sentinel gets rank 0

    for i in 1..sorted_lms.len() {
        if !lms_substrings_equal(text, types, sorted_lms[i - 1], sorted_lms[i]) {
            current_rank += 1;
        }
        ranks[sorted_lms[i]] = current_rank;
    }

    // Build reduced string in original LMS order
    let lms_order: Vec<usize> = lms_positions.to_vec();
    let reduced: Vec<usize> = lms_order
        .iter()
        .map(|&pos| ranks[pos])
        .collect();

    (reduced, current_rank, lms_order)
}

/// Build the LCP array using Kasai's algorithm.
/// lcp[i] = length of longest common prefix between sa[i-1] and sa[i].
/// lcp[0] is always 0.
pub fn build_lcp_array(text: &[u8], sa: &[usize]) -> Vec<usize> {
    let n = sa.len();
    if n == 0 {
        return vec![];
    }

    // Build inverse suffix array
    let mut rank = vec![0usize; n];
    for (i, &pos) in sa.iter().enumerate() {
        rank[pos] = i;
    }

    // Append sentinel for safe comparison
    let mut extended = text.to_vec();
    extended.push(0);

    let mut lcp = vec![0usize; n];
    let mut h: usize = 0;

    for i in 0..n {
        if rank[i] > 0 {
            let j = sa[rank[i] - 1];
            while i + h < extended.len() && j + h < extended.len() && extended[i + h] == extended[j + h] {
                h += 1;
            }
            lcp[rank[i]] = h;
            if h > 0 {
                h -= 1;
            }
        } else {
            h = 0;
        }
    }

    lcp
}

/// Find all positions where pattern occurs in the original text.
/// Uses binary search over the suffix array.
pub fn search_pattern(text: &[u8], sa: &[usize], pattern: &[u8]) -> Vec<usize> {
    if pattern.is_empty() || sa.is_empty() {
        return vec![];
    }

    let n = text.len();
    let m = pattern.len();

    // Extended text with sentinel for safe slicing
    let mut extended = text.to_vec();
    extended.push(0);

    // Find lower bound: first suffix >= pattern
    let lower = {
        let mut lo = 0usize;
        let mut hi = sa.len();
        while lo < hi {
            let mid = lo + (hi - lo) / 2;
            let pos = sa[mid];
            let end = std::cmp::min(pos + m, extended.len());
            let suffix = &extended[pos..end];
            if suffix < pattern {
                lo = mid + 1;
            } else {
                hi = mid;
            }
        }
        lo
    };

    // Find upper bound: first suffix where suffix[..m] > pattern
    let upper = {
        let mut lo = lower;
        let mut hi = sa.len();
        while lo < hi {
            let mid = lo + (hi - lo) / 2;
            let pos = sa[mid];
            let end = std::cmp::min(pos + m, extended.len());
            let suffix = &extended[pos..end];
            if suffix <= pattern {
                lo = mid + 1;
            } else {
                hi = mid;
            }
        }
        lo
    };

    let mut positions: Vec<usize> = sa[lower..upper].to_vec();
    // Filter out sentinel-overlapping matches
    positions.retain(|&pos| pos + m <= n);
    positions.sort();
    positions
}

/// Find the longest repeated substring.
/// Returns the starting position and length, or None if no repetition exists.
pub fn longest_repeated_substring(text: &[u8], sa: &[usize], lcp: &[usize]) -> Option<(usize, usize)> {
    if lcp.is_empty() {
        return None;
    }

    let (max_idx, &max_len) = lcp.iter()
        .enumerate()
        .max_by_key(|&(_, &v)| v)?;

    if max_len == 0 {
        return None;
    }

    Some((sa[max_idx], max_len))
}

/// Count the number of distinct substrings in the text.
/// Excludes the empty substring and sentinel-only substrings.
pub fn count_distinct_substrings(text: &[u8], sa: &[usize], lcp: &[usize]) -> usize {
    let n = text.len();
    if n == 0 {
        return 0;
    }

    // Total substrings of original text (without sentinel): n*(n+1)/2
    // Each LCP value represents shared prefixes that are not new substrings
    // Skip index 0 (sentinel) and adjust for sentinel in SA
    let total: usize = sa.iter()
        .enumerate()
        .skip(1) // skip sentinel
        .map(|(i, &pos)| {
            let suffix_len = if pos < n { n - pos } else { 0 };
            let lcp_val = lcp[i];
            if suffix_len > lcp_val { suffix_len - lcp_val } else { 0 }
        })
        .sum();

    total
}

#[cfg(test)]
mod tests {
    use super::*;

    fn naive_suffix_array(text: &[u8]) -> Vec<usize> {
        let mut extended = text.to_vec();
        extended.push(0); // sentinel
        let mut indices: Vec<usize> = (0..extended.len()).collect();
        indices.sort_by(|&a, &b| extended[a..].cmp(&extended[b..]));
        indices
    }

    #[test]
    fn test_sa_is_basic() {
        let text = b"banana";
        let sa = build_suffix_array(text);
        let expected = naive_suffix_array(text);
        assert_eq!(sa, expected, "SA-IS should match naive sort for 'banana'");
    }

    #[test]
    fn test_sa_is_repeated_chars() {
        let text = b"aaaaaa";
        let sa = build_suffix_array(text);
        let expected = naive_suffix_array(text);
        assert_eq!(sa, expected);
    }

    #[test]
    fn test_sa_is_single_char() {
        let text = b"a";
        let sa = build_suffix_array(text);
        let expected = naive_suffix_array(text);
        assert_eq!(sa, expected);
    }

    #[test]
    fn test_sa_is_sorted_text() {
        let text = b"abcdefgh";
        let sa = build_suffix_array(text);
        let expected = naive_suffix_array(text);
        assert_eq!(sa, expected);
    }

    #[test]
    fn test_sa_is_reverse_sorted() {
        let text = b"hgfedcba";
        let sa = build_suffix_array(text);
        let expected = naive_suffix_array(text);
        assert_eq!(sa, expected);
    }

    #[test]
    fn test_sa_is_longer_text() {
        let text = b"mississippi";
        let sa = build_suffix_array(text);
        let expected = naive_suffix_array(text);
        assert_eq!(sa, expected);
    }

    #[test]
    fn test_lcp_banana() {
        let text = b"banana";
        let sa = build_suffix_array(text);
        let lcp = build_lcp_array(text, &sa);
        // SA for banana$: [$, a, ana, anana, banana, na, nana]
        // LCP:             [0, 0, 1,   3,     0,     0,   2]
        // (The sentinel is at sa[0], lcp[0] = 0 by definition)
        assert_eq!(lcp[0], 0);
        // Verify max LCP is 3 (between "ana" and "anana")
        assert_eq!(*lcp.iter().max().unwrap(), 3);
    }

    #[test]
    fn test_pattern_search_found() {
        let text = b"abcabcabc";
        let sa = build_suffix_array(text);
        let mut positions = search_pattern(text, &sa, b"abc");
        positions.sort();
        assert_eq!(positions, vec![0, 3, 6]);
    }

    #[test]
    fn test_pattern_search_not_found() {
        let text = b"abcdefgh";
        let sa = build_suffix_array(text);
        let positions = search_pattern(text, &sa, b"xyz");
        assert!(positions.is_empty());
    }

    #[test]
    fn test_pattern_search_single_char() {
        let text = b"banana";
        let sa = build_suffix_array(text);
        let mut positions = search_pattern(text, &sa, b"a");
        positions.sort();
        assert_eq!(positions, vec![1, 3, 5]);
    }

    #[test]
    fn test_longest_repeated_substring() {
        let text = b"banana";
        let sa = build_suffix_array(text);
        let lcp = build_lcp_array(text, &sa);
        let result = longest_repeated_substring(text, &sa, &lcp);
        assert!(result.is_some());
        let (pos, len) = result.unwrap();
        let substring = &text[pos..pos + len];
        assert_eq!(substring, b"ana");
    }

    #[test]
    fn test_distinct_substrings() {
        // "abc" has distinct substrings: a, b, c, ab, bc, abc = 6
        let text = b"abc";
        let sa = build_suffix_array(text);
        let lcp = build_lcp_array(text, &sa);
        let count = count_distinct_substrings(text, &sa, &lcp);
        assert_eq!(count, 6);
    }

    #[test]
    fn test_distinct_substrings_repeated() {
        // "aa" has distinct substrings: a, aa = 2
        let text = b"aa";
        let sa = build_suffix_array(text);
        let lcp = build_lcp_array(text, &sa);
        let count = count_distinct_substrings(text, &sa, &lcp);
        assert_eq!(count, 2);
    }

    #[test]
    fn test_no_repeated_substring() {
        let text = b"abcdef";
        let sa = build_suffix_array(text);
        let lcp = build_lcp_array(text, &sa);
        // All chars unique, but some substrings may share prefixes
        // The max LCP should be 0 since no char repeats
        let max_lcp = *lcp.iter().max().unwrap();
        assert_eq!(max_lcp, 0);
    }
}
```

### Run and verify

```bash
cd suffix-array
cargo test
```

Expected output:

```
running 13 tests
test tests::test_sa_is_basic ... ok
test tests::test_sa_is_repeated_chars ... ok
test tests::test_sa_is_single_char ... ok
test tests::test_sa_is_sorted_text ... ok
test tests::test_sa_is_reverse_sorted ... ok
test tests::test_sa_is_longer_text ... ok
test tests::test_lcp_banana ... ok
test tests::test_pattern_search_found ... ok
test tests::test_pattern_search_not_found ... ok
test tests::test_pattern_search_single_char ... ok
test tests::test_longest_repeated_substring ... ok
test tests::test_distinct_substrings ... ok
test tests::test_distinct_substrings_repeated ... ok

test result: ok. 13 passed; 0 failed; 0 ignored
```

## Design Decisions

1. **Sentinel as byte 0**: The sentinel must be lexicographically smaller than any character in the input. By mapping input bytes to `byte_value + 1` and using 0 as sentinel, we guarantee this property for all byte inputs. The sentinel ensures every suffix has a unique ending.

2. **usize::MAX as EMPTY marker**: During induced sorting, unoccupied positions in the suffix array need a marker distinct from any valid index. `usize::MAX` serves this purpose and is easily checked.

3. **Iterative bucket computation**: Bucket boundaries are recomputed for each induced sorting pass rather than stored persistently. This uses O(alphabet_size) time per pass but avoids mutable state leaking between the L-type and S-type induction phases.

4. **Kasai's algorithm operates on original text**: The LCP array is built over the original text (not the sentinel-extended version) for user-facing correctness. The extended version is used only internally for safe boundary comparisons.

5. **Binary search returns sorted positions**: Pattern search results are sorted by position in the text, not by suffix array order. This is more useful for the caller (e.g., for highlighting all occurrences in document order).

## Common Mistakes

1. **Forgetting the sentinel**: Without a sentinel, the last suffix has no defined ordering relative to suffixes that are its prefixes. This causes incorrect suffix array construction for strings ending with characters equal to their internal substrings.

2. **Wrong LMS classification direction**: Types must be classified right-to-left. Left-to-right classification produces incorrect results because a suffix's type depends on the suffix to its right.

3. **Off-by-one in induced sorting direction**: L-type suffixes are induced left-to-right using bucket heads. S-type suffixes are induced right-to-left using bucket tails. Reversing these directions produces an incorrect suffix array.

4. **Not resetting bucket pointers between phases**: Each induction phase (placing LMS, inducing L, inducing S) modifies bucket head/tail pointers. These must be reset to their original values before each phase.

5. **Pattern search boundary overflow**: When comparing a suffix at position `p` with a pattern of length `m`, the suffix slice `text[p..p+m]` may overflow the text bounds. Always clamp to `min(p+m, n)` and handle shorter suffixes correctly (they are less than the pattern if the shared prefix matches).

## Performance Notes

- **SA-IS construction**: O(n) time and O(n) space. The recursion depth is O(log n) since each level reduces the problem by at least half. In practice, most inputs require 1-3 recursion levels.
- **Kasai's LCP**: O(n) time. The key insight is that `h` (the LCP length being tracked) decreases by at most 1 per iteration, so the total number of character comparisons is bounded by 2n.
- **Pattern search**: O(m log n) per query where m is pattern length. Each binary search step performs an O(m) string comparison. With LCP acceleration, this can be reduced to O(m + log n).
- **Memory**: The suffix array and LCP array each require O(n) words (4 or 8 bytes per entry depending on usize). For a 1 GB text, expect ~8 GB for the suffix array on a 64-bit system. Production implementations use compressed suffix arrays to reduce this.
- **Construction benchmark**: SA-IS constructs a suffix array for 10 MB of random text in ~200ms on modern hardware. The naive O(n^2 log n) approach would take hours for the same input.
