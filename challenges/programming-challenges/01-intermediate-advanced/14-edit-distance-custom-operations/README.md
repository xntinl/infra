<!-- difficulty: intermediate-advanced -->
<!-- category: algorithms -->
<!-- languages: [rust] -->
<!-- concepts: [dynamic-programming, edit-distance, unicode, dp-table-visualization, backtracking] -->
<!-- estimated_time: 5-7 hours -->
<!-- bloom_level: apply, analyze -->
<!-- prerequisites: [rust-basics, dynamic-programming-fundamentals, unicode-handling, trait-implementations] -->

# Challenge 14: Edit Distance with Custom Operations

## Languages

Rust (stable, latest edition)

## Prerequisites

- Familiarity with dynamic programming and 2D table construction
- Understanding of the classic Levenshtein distance algorithm
- Rust ownership, borrowing, and lifetime annotations
- Basic Unicode awareness: `char` vs byte, grapheme clusters
- Implementing `Display` and custom traits in Rust

## Learning Objectives

- **Implement** a generalized edit distance algorithm supporting six distinct operation types with weighted costs
- **Apply** dynamic programming with backtracking to reconstruct the optimal edit script
- **Analyze** how custom operation costs change the optimal alignment between strings
- **Design** a Unicode-aware string comparison engine that handles multi-byte characters correctly
- **Compare** the effects of different cost configurations on real-world string matching scenarios

## The Challenge

The classic Levenshtein distance supports three operations: insert, delete, and replace. Real-world string transformation often requires more. A spell checker benefits from transposition (typing "teh" instead of "the"). A text normalization pipeline needs merge (combining "a" + "e" into "ae") and split (breaking a ligature into parts).

Build a generalized edit distance engine that supports six operations: insert, delete, replace, transpose (swap two adjacent characters), merge (combine two source characters into one target character), and split (expand one source character into two target characters). Each operation has a configurable cost. The engine must reconstruct the full edit script -- the exact sequence of operations that transforms the source into the target -- not just the numeric distance. It must handle Unicode strings correctly, operating on `char` boundaries rather than bytes.

Include a visualization mode that prints the DP table and the alignment between source and target strings, showing which operation was applied at each step.

## Requirements

1. Define an `Operation` enum with variants: `Insert`, `Delete`, `Replace`, `Transpose`, `Match` (zero-cost identity), `Merge`, `Split`
2. Define an `OperationCosts` struct with configurable cost per operation type (default: all costs = 1, match = 0)
3. Implement the DP table construction handling all six operations, with `Transpose` following Damerau's extension (adjacent-only swap)
4. Implement backtracking through the DP table to reconstruct the optimal edit script as a `Vec<Operation>`
5. Each `Operation` in the script must carry context: source position, target position, and the character(s) involved
6. Handle Unicode correctly: operate on `Vec<char>` from `str::chars()`, not bytes
7. Implement `Display` for the edit script in a human-readable format (e.g., `Replace 'a' -> 'o' at position 3`)
8. Implement DP table visualization showing costs and arrows/operation markers
9. Implement a visual string alignment showing source and target characters with operation annotations between them
10. Write tests covering: identical strings, empty strings, pure insertions, pure deletions, transpositions, merge and split scenarios, Unicode strings with multi-byte characters, and custom cost configurations

## Hints

<details>
<summary>Hint 1: DP table dimensions for merge and split</summary>

Standard edit distance uses a 2D table of size `(m+1) x (n+1)`. Merge consumes 2 characters from the source and produces 1 in the target, so it transitions from `dp[i-2][j-1]`. Split consumes 1 from the source and produces 2 in the target, transitioning from `dp[i-1][j-2]`. You must check bounds before accessing these cells:

```rust
// Merge: two source chars -> one target char
if i >= 2 && j >= 1 {
    let merged = format!("{}{}", source[i - 2], source[i - 1]);
    let target_str = target[j - 1].to_string();
    if merged == target_str || (source[i - 2] == target[j - 1] /* custom merge check */) {
        let cost = dp[i - 2][j - 1] + costs.merge;
        if cost < dp[i][j] {
            dp[i][j] = cost;
            ops[i][j] = Operation::Merge;
        }
    }
}
```

</details>

<details>
<summary>Hint 2: Transpose requires Damerau's condition</summary>

Transposition swaps two adjacent characters. It transitions from `dp[i-2][j-2]` and requires that `source[i-2] == target[j-1]` AND `source[i-1] == target[j-2]`:

```rust
if i >= 2 && j >= 2
    && source[i - 1] == target[j - 2]
    && source[i - 2] == target[j - 1]
{
    let cost = dp[i - 2][j - 2] + costs.transpose;
    if cost < dp[i][j] {
        dp[i][j] = cost;
        ops[i][j] = Operation::Transpose;
    }
}
```

Be careful: this is the "optimal string alignment" variant, not full Damerau-Levenshtein. The difference matters when substrings can be both transposed and edited.

</details>

<details>
<summary>Hint 3: Backtracking with variable step sizes</summary>

Standard backtracking steps by `(1,0)`, `(0,1)`, or `(1,1)`. With merge, split, and transpose, step sizes vary:

```rust
fn backtrack_step(op: &Operation) -> (usize, usize) {
    match op {
        Operation::Match | Operation::Replace => (1, 1),
        Operation::Delete => (1, 0),
        Operation::Insert => (0, 1),
        Operation::Transpose => (2, 2),
        Operation::Merge => (2, 1),
        Operation::Split => (1, 2),
    }
}
```

Walk from `(m, n)` back to `(0, 0)`, collecting operations in reverse, then reverse the collected vector.

</details>

<details>
<summary>Hint 4: DP table visualization</summary>

Print the table with source characters as row headers and target characters as column headers. Mark each cell with the operation that produced it using single-character codes: `M`=match, `R`=replace, `D`=delete, `I`=insert, `T`=transpose, `G`=merge, `S`=split. Highlight the backtrack path:

```rust
fn visualize_table(dp: &[Vec<u32>], ops: &[Vec<Operation>], source: &[char], target: &[char]) {
    print!("      ");
    for ch in target {
        print!("  {:>3}", ch);
    }
    println!();
    for (i, row) in dp.iter().enumerate() {
        if i == 0 { print!("    "); } else { print!(" {:>2} ", source[i - 1]); }
        for val in row {
            print!(" {:>3}", val);
        }
        println!();
    }
}
```

</details>

## Acceptance Criteria

- [ ] All six operations (insert, delete, replace, transpose, merge, split) are implemented and produce correct distances
- [ ] Edit scripts reconstruct the exact transformation from source to target
- [ ] Custom costs alter the optimal path (e.g., cheap transpose prefers swaps over delete+insert)
- [ ] Unicode strings with multi-byte characters (CJK, emoji, accented) produce correct results
- [ ] DP table visualization renders a readable grid with operation markers
- [ ] String alignment visualization shows the correspondence between source and target characters
- [ ] Empty string edge cases return correct distances (empty->target = sum of inserts, source->empty = sum of deletes)
- [ ] Identical strings return distance 0 with all-match edit scripts
- [ ] All tests pass with `cargo test`

## Research Resources

- [Levenshtein Distance -- Wikipedia](https://en.wikipedia.org/wiki/Levenshtein_distance) -- the foundational algorithm
- [Damerau-Levenshtein Distance -- Wikipedia](https://en.wikipedia.org/wiki/Damerau%E2%80%93Levenshtein_distance) -- the transposition extension and distinction between OSA and full DL
- [Wagner-Fischer Algorithm](https://en.wikipedia.org/wiki/Wagner%E2%80%93Fischer_algorithm) -- the DP approach used to compute edit distance
- [Rust `char` and Unicode](https://doc.rust-lang.org/std/primitive.char.html) -- why char is a Unicode scalar value, not a byte
- [Edit Distance with Affine Gap Penalties (Gotoh, 1982)](https://doi.org/10.1016/0022-2836(82)90398-9) -- related technique for bioinformatics sequence alignment
- [Optimal String Alignment vs. Damerau-Levenshtein](https://en.wikipedia.org/wiki/Damerau%E2%80%93Levenshtein_distance#Optimal_string_alignment_distance) -- the triangle inequality subtlety
