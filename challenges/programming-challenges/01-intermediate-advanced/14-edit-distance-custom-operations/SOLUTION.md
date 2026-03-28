# Solution: Edit Distance with Custom Operations

## Architecture Overview

The solution has three layers:

1. **Types layer**: `Operation` enum with context, `OperationCosts` configuration struct, `EditResult` combining distance + script + table
2. **DP engine**: builds the cost table considering all six operations, stores operation choices for backtracking
3. **Visualization layer**: renders the DP table, the backtrack path, and a human-readable alignment

The DP table is `(m+1) x (n+1)` where `m = source.len()` and `n = target.len()`. Each cell stores the minimum cost to transform `source[0..i]` into `target[0..j]`. A parallel table stores which operation achieved that minimum, enabling backtracking.

## Rust Solution

```rust
use std::fmt;

// --- Types ---

#[derive(Debug, Clone, PartialEq)]
pub enum OpKind {
    Match,
    Insert,
    Delete,
    Replace,
    Transpose,
    Merge,
    Split,
}

#[derive(Debug, Clone)]
pub struct Operation {
    pub kind: OpKind,
    pub source_pos: usize,
    pub target_pos: usize,
    pub source_chars: Vec<char>,
    pub target_chars: Vec<char>,
}

impl fmt::Display for Operation {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self.kind {
            OpKind::Match => write!(
                f,
                "Match '{}' at source[{}] -> target[{}]",
                self.source_chars[0], self.source_pos, self.target_pos
            ),
            OpKind::Insert => write!(
                f,
                "Insert '{}' at target[{}]",
                self.target_chars[0], self.target_pos
            ),
            OpKind::Delete => write!(
                f,
                "Delete '{}' at source[{}]",
                self.source_chars[0], self.source_pos
            ),
            OpKind::Replace => write!(
                f,
                "Replace '{}' -> '{}' at source[{}] -> target[{}]",
                self.source_chars[0], self.target_chars[0], self.source_pos, self.target_pos
            ),
            OpKind::Transpose => write!(
                f,
                "Transpose '{}{}' -> '{}{}' at source[{}..{}]",
                self.source_chars[0],
                self.source_chars[1],
                self.target_chars[0],
                self.target_chars[1],
                self.source_pos,
                self.source_pos + 1
            ),
            OpKind::Merge => write!(
                f,
                "Merge '{}{}' -> '{}' at source[{}..{}] -> target[{}]",
                self.source_chars[0],
                self.source_chars[1],
                self.target_chars[0],
                self.source_pos,
                self.source_pos + 1,
                self.target_pos
            ),
            OpKind::Split => write!(
                f,
                "Split '{}' -> '{}{}' at source[{}] -> target[{}..{}]",
                self.source_chars[0],
                self.target_chars[0],
                self.target_chars[1],
                self.source_pos,
                self.target_pos,
                self.target_pos + 1
            ),
        }
    }
}

#[derive(Debug, Clone)]
pub struct OperationCosts {
    pub insert: u32,
    pub delete: u32,
    pub replace: u32,
    pub transpose: u32,
    pub merge: u32,
    pub split: u32,
}

impl Default for OperationCosts {
    fn default() -> Self {
        Self {
            insert: 1,
            delete: 1,
            replace: 1,
            transpose: 1,
            merge: 1,
            split: 1,
        }
    }
}

pub struct EditResult {
    pub distance: u32,
    pub script: Vec<Operation>,
    pub dp_table: Vec<Vec<u32>>,
    pub op_table: Vec<Vec<OpKind>>,
    pub source: Vec<char>,
    pub target: Vec<char>,
}

// --- DP Engine ---

pub fn compute_edit_distance(
    source: &str,
    target: &str,
    costs: &OperationCosts,
) -> EditResult {
    let src: Vec<char> = source.chars().collect();
    let tgt: Vec<char> = target.chars().collect();
    let m = src.len();
    let n = tgt.len();

    let mut dp = vec![vec![u32::MAX; n + 1]; m + 1];
    let mut ops = vec![vec![OpKind::Match; n + 1]; m + 1];

    dp[0][0] = 0;

    for i in 1..=m {
        dp[i][0] = dp[i - 1][0] + costs.delete;
        ops[i][0] = OpKind::Delete;
    }

    for j in 1..=n {
        dp[0][j] = dp[0][j - 1] + costs.insert;
        ops[0][j] = OpKind::Insert;
    }

    for i in 1..=m {
        for j in 1..=n {
            // Match or Replace
            if src[i - 1] == tgt[j - 1] {
                dp[i][j] = dp[i - 1][j - 1];
                ops[i][j] = OpKind::Match;
            } else {
                dp[i][j] = dp[i - 1][j - 1] + costs.replace;
                ops[i][j] = OpKind::Replace;
            }

            // Delete
            let del_cost = dp[i - 1][j].saturating_add(costs.delete);
            if del_cost < dp[i][j] {
                dp[i][j] = del_cost;
                ops[i][j] = OpKind::Delete;
            }

            // Insert
            let ins_cost = dp[i][j - 1].saturating_add(costs.insert);
            if ins_cost < dp[i][j] {
                dp[i][j] = ins_cost;
                ops[i][j] = OpKind::Insert;
            }

            // Transpose: swap adjacent characters
            if i >= 2
                && j >= 2
                && src[i - 1] == tgt[j - 2]
                && src[i - 2] == tgt[j - 1]
            {
                let trans_cost = dp[i - 2][j - 2].saturating_add(costs.transpose);
                if trans_cost < dp[i][j] {
                    dp[i][j] = trans_cost;
                    ops[i][j] = OpKind::Transpose;
                }
            }

            // Merge: two source chars -> one target char
            if i >= 2 {
                let merge_cost = dp[i - 2][j - 1].saturating_add(costs.merge);
                if merge_cost < dp[i][j] {
                    dp[i][j] = merge_cost;
                    ops[i][j] = OpKind::Merge;
                }
            }

            // Split: one source char -> two target chars
            if j >= 2 {
                let split_cost = dp[i - 1][j - 2].saturating_add(costs.split);
                if split_cost < dp[i][j] {
                    dp[i][j] = split_cost;
                    ops[i][j] = OpKind::Split;
                }
            }
        }
    }

    let script = backtrack(&dp, &ops, &src, &tgt);

    EditResult {
        distance: dp[m][n],
        script,
        dp_table: dp,
        op_table: ops,
        source: src,
        target: tgt,
    }
}

fn backtrack_step(kind: &OpKind) -> (usize, usize) {
    match kind {
        OpKind::Match | OpKind::Replace => (1, 1),
        OpKind::Delete => (1, 0),
        OpKind::Insert => (0, 1),
        OpKind::Transpose => (2, 2),
        OpKind::Merge => (2, 1),
        OpKind::Split => (1, 2),
    }
}

fn backtrack(
    _dp: &[Vec<u32>],
    ops: &[Vec<OpKind>],
    source: &[char],
    target: &[char],
) -> Vec<Operation> {
    let mut result = Vec::new();
    let mut i = source.len();
    let mut j = target.len();

    while i > 0 || j > 0 {
        let kind = &ops[i][j];
        let (di, dj) = backtrack_step(kind);

        let op = match kind {
            OpKind::Match | OpKind::Replace => Operation {
                kind: kind.clone(),
                source_pos: i - 1,
                target_pos: j - 1,
                source_chars: vec![source[i - 1]],
                target_chars: vec![target[j - 1]],
            },
            OpKind::Delete => Operation {
                kind: OpKind::Delete,
                source_pos: i - 1,
                target_pos: j,
                source_chars: vec![source[i - 1]],
                target_chars: vec![],
            },
            OpKind::Insert => Operation {
                kind: OpKind::Insert,
                source_pos: i,
                target_pos: j - 1,
                source_chars: vec![],
                target_chars: vec![target[j - 1]],
            },
            OpKind::Transpose => Operation {
                kind: OpKind::Transpose,
                source_pos: i - 2,
                target_pos: j - 2,
                source_chars: vec![source[i - 2], source[i - 1]],
                target_chars: vec![target[j - 2], target[j - 1]],
            },
            OpKind::Merge => Operation {
                kind: OpKind::Merge,
                source_pos: i - 2,
                target_pos: j - 1,
                source_chars: vec![source[i - 2], source[i - 1]],
                target_chars: vec![target[j - 1]],
            },
            OpKind::Split => Operation {
                kind: OpKind::Split,
                source_pos: i - 1,
                target_pos: j - 2,
                source_chars: vec![source[i - 1]],
                target_chars: vec![target[j - 2], target[j - 1]],
            },
        };

        result.push(op);
        i -= di;
        j -= dj;
    }

    result.reverse();
    result
}

// --- Visualization ---

pub fn visualize_dp_table(result: &EditResult) {
    let src = &result.source;
    let tgt = &result.target;
    let dp = &result.dp_table;
    let ops = &result.op_table;

    // Header row
    print!("       ε");
    for ch in tgt {
        print!("    {}", ch);
    }
    println!();

    // Separator
    print!("    ");
    for _ in 0..=(tgt.len()) {
        print!("-----");
    }
    println!();

    for i in 0..=src.len() {
        if i == 0 {
            print!(" ε |");
        } else {
            print!(" {} |", src[i - 1]);
        }

        for j in 0..=tgt.len() {
            let op_marker = match ops[i][j] {
                OpKind::Match => 'm',
                OpKind::Replace => 'R',
                OpKind::Insert => 'I',
                OpKind::Delete => 'D',
                OpKind::Transpose => 'T',
                OpKind::Merge => 'G',
                OpKind::Split => 'S',
            };
            if i == 0 && j == 0 {
                print!(" {:>3} ", dp[i][j]);
            } else {
                print!("{}{:>3} ", op_marker, dp[i][j]);
            }
        }
        println!();
    }
}

pub fn visualize_alignment(result: &EditResult) {
    let mut source_line = String::new();
    let mut ops_line = String::new();
    let mut target_line = String::new();

    for op in &result.script {
        match op.kind {
            OpKind::Match => {
                source_line.push(op.source_chars[0]);
                ops_line.push('|');
                target_line.push(op.target_chars[0]);
            }
            OpKind::Replace => {
                source_line.push(op.source_chars[0]);
                ops_line.push('X');
                target_line.push(op.target_chars[0]);
            }
            OpKind::Delete => {
                source_line.push(op.source_chars[0]);
                ops_line.push('D');
                target_line.push('-');
            }
            OpKind::Insert => {
                source_line.push('-');
                ops_line.push('I');
                target_line.push(op.target_chars[0]);
            }
            OpKind::Transpose => {
                source_line.push(op.source_chars[0]);
                source_line.push(op.source_chars[1]);
                ops_line.push('T');
                ops_line.push('T');
                target_line.push(op.target_chars[0]);
                target_line.push(op.target_chars[1]);
            }
            OpKind::Merge => {
                source_line.push(op.source_chars[0]);
                source_line.push(op.source_chars[1]);
                ops_line.push('G');
                ops_line.push('G');
                target_line.push(op.target_chars[0]);
                target_line.push(' ');
            }
            OpKind::Split => {
                source_line.push(op.source_chars[0]);
                source_line.push(' ');
                ops_line.push('S');
                ops_line.push('S');
                target_line.push(op.target_chars[0]);
                target_line.push(op.target_chars[1]);
            }
        }
    }

    println!("Source: {}", source_line);
    println!("        {}", ops_line);
    println!("Target: {}", target_line);
}

// --- Main ---

fn main() {
    println!("=== Example 1: Standard edit distance ===");
    let costs = OperationCosts::default();
    let result = compute_edit_distance("kitten", "sitting", &costs);
    println!("Distance: {}", result.distance);
    println!("\nEdit script:");
    for op in &result.script {
        println!("  {}", op);
    }
    println!("\nDP Table:");
    visualize_dp_table(&result);
    println!("\nAlignment:");
    visualize_alignment(&result);

    println!("\n=== Example 2: Transposition ===");
    let result = compute_edit_distance("teh", "the", &costs);
    println!("Distance: {}", result.distance);
    println!("\nEdit script:");
    for op in &result.script {
        println!("  {}", op);
    }
    println!("\nAlignment:");
    visualize_alignment(&result);

    println!("\n=== Example 3: Cheap transpose ===");
    let cheap_transpose = OperationCosts {
        transpose: 1,
        replace: 3,
        insert: 3,
        delete: 3,
        merge: 2,
        split: 2,
    };
    let result = compute_edit_distance("abcd", "badc", &cheap_transpose);
    println!("Distance: {}", result.distance);
    println!("\nEdit script:");
    for op in &result.script {
        println!("  {}", op);
    }

    println!("\n=== Example 4: Unicode ===");
    let result = compute_edit_distance("café", "cafe", &costs);
    println!("Distance: {}", result.distance);
    println!("\nEdit script:");
    for op in &result.script {
        println!("  {}", op);
    }
    println!("\nAlignment:");
    visualize_alignment(&result);
}

// --- Tests ---

#[cfg(test)]
mod tests {
    use super::*;

    fn default_costs() -> OperationCosts {
        OperationCosts::default()
    }

    #[test]
    fn test_identical_strings() {
        let result = compute_edit_distance("hello", "hello", &default_costs());
        assert_eq!(result.distance, 0);
        assert!(result.script.iter().all(|op| op.kind == OpKind::Match));
    }

    #[test]
    fn test_empty_to_nonempty() {
        let result = compute_edit_distance("", "abc", &default_costs());
        assert_eq!(result.distance, 3);
        assert!(result.script.iter().all(|op| op.kind == OpKind::Insert));
    }

    #[test]
    fn test_nonempty_to_empty() {
        let result = compute_edit_distance("abc", "", &default_costs());
        assert_eq!(result.distance, 3);
        assert!(result.script.iter().all(|op| op.kind == OpKind::Delete));
    }

    #[test]
    fn test_both_empty() {
        let result = compute_edit_distance("", "", &default_costs());
        assert_eq!(result.distance, 0);
        assert!(result.script.is_empty());
    }

    #[test]
    fn test_classic_levenshtein() {
        let result = compute_edit_distance("kitten", "sitting", &default_costs());
        assert_eq!(result.distance, 3);
    }

    #[test]
    fn test_transposition() {
        let result = compute_edit_distance("ab", "ba", &default_costs());
        assert_eq!(result.distance, 1);
        assert!(result.script.iter().any(|op| op.kind == OpKind::Transpose));
    }

    #[test]
    fn test_transpose_teh_the() {
        let result = compute_edit_distance("teh", "the", &default_costs());
        assert_eq!(result.distance, 1);
    }

    #[test]
    fn test_custom_costs_prefer_transpose() {
        let costs = OperationCosts {
            transpose: 1,
            replace: 5,
            insert: 5,
            delete: 5,
            merge: 5,
            split: 5,
        };
        let result = compute_edit_distance("ab", "ba", &costs);
        assert_eq!(result.distance, 1);
        assert!(result.script.iter().any(|op| op.kind == OpKind::Transpose));
    }

    #[test]
    fn test_custom_costs_avoid_transpose() {
        let costs = OperationCosts {
            transpose: 10,
            replace: 1,
            insert: 1,
            delete: 1,
            merge: 10,
            split: 10,
        };
        let result = compute_edit_distance("ab", "ba", &costs);
        // With expensive transpose, should use delete+insert (cost 2) instead
        assert_eq!(result.distance, 2);
    }

    #[test]
    fn test_merge_operation() {
        let costs = OperationCosts {
            merge: 1,
            replace: 5,
            insert: 5,
            delete: 5,
            transpose: 5,
            split: 5,
        };
        // "ab" -> "x": delete a (5) + replace b->x (5) = 10, or merge ab->x (1) = 1
        let result = compute_edit_distance("ab", "x", &costs);
        assert_eq!(result.distance, 1);
        assert!(result.script.iter().any(|op| op.kind == OpKind::Merge));
    }

    #[test]
    fn test_split_operation() {
        let costs = OperationCosts {
            split: 1,
            replace: 5,
            insert: 5,
            delete: 5,
            transpose: 5,
            merge: 5,
        };
        // "x" -> "ab": replace x->a (5) + insert b (5) = 10, or split x->ab (1) = 1
        let result = compute_edit_distance("x", "ab", &costs);
        assert_eq!(result.distance, 1);
        assert!(result.script.iter().any(|op| op.kind == OpKind::Split));
    }

    #[test]
    fn test_unicode_chars() {
        let result = compute_edit_distance("café", "cafe", &default_costs());
        assert_eq!(result.distance, 1); // Replace 'é' with 'e'
    }

    #[test]
    fn test_unicode_cjk() {
        let result = compute_edit_distance("你好世界", "你好", &default_costs());
        assert_eq!(result.distance, 2); // Delete '世' and '界'
    }

    #[test]
    fn test_script_reconstructs_correctly() {
        let result = compute_edit_distance("abc", "axc", &default_costs());
        assert_eq!(result.distance, 1);

        let replace_ops: Vec<_> = result
            .script
            .iter()
            .filter(|op| op.kind == OpKind::Replace)
            .collect();
        assert_eq!(replace_ops.len(), 1);
        assert_eq!(replace_ops[0].source_chars, vec!['b']);
        assert_eq!(replace_ops[0].target_chars, vec!['x']);
    }

    #[test]
    fn test_pure_insertions() {
        let result = compute_edit_distance("ac", "abc", &default_costs());
        assert_eq!(result.distance, 1);
        let insert_count = result
            .script
            .iter()
            .filter(|op| op.kind == OpKind::Insert)
            .count();
        assert_eq!(insert_count, 1);
    }

    #[test]
    fn test_pure_deletions() {
        let result = compute_edit_distance("abc", "ac", &default_costs());
        assert_eq!(result.distance, 1);
        let delete_count = result
            .script
            .iter()
            .filter(|op| op.kind == OpKind::Delete)
            .count();
        assert_eq!(delete_count, 1);
    }

    #[test]
    fn test_single_char_strings() {
        let result = compute_edit_distance("a", "b", &default_costs());
        assert_eq!(result.distance, 1);
    }

    #[test]
    fn test_dp_table_dimensions() {
        let result = compute_edit_distance("abc", "xy", &default_costs());
        assert_eq!(result.dp_table.len(), 4); // m + 1
        assert_eq!(result.dp_table[0].len(), 3); // n + 1
    }
}
```

## Running

```bash
# Create project
cargo init edit-distance
cd edit-distance

# Replace src/main.rs with the solution code above

# Run
cargo run

# Run tests
cargo test

# Run tests with output
cargo test -- --nocapture
```

## Expected Output

```
=== Example 1: Standard edit distance ===
Distance: 3

Edit script:
  Replace 'k' -> 's' at source[0] -> target[0]
  Match 'i' at source[1] -> target[1]
  Match 't' at source[2] -> target[2]
  Match 't' at source[3] -> target[3]
  Replace 'e' -> 'i' at source[4] -> target[4]
  Match 'n' at source[5] -> target[5]
  Insert 'g' at target[6]

Alignment:
Source: kitten-
        X|||| I
Target: sittin g

=== Example 2: Transposition ===
Distance: 1

Edit script:
  Match 't' at source[0] -> target[0]
  Transpose 'eh' -> 'he' at source[1..2]

Alignment:
Source: teh
        |TT
Target: the

=== Example 4: Unicode ===
Distance: 1

Edit script:
  Match 'c' at source[0] -> target[0]
  Match 'a' at source[1] -> target[1]
  Match 'f' at source[2] -> target[2]
  Replace 'é' -> 'e' at source[3] -> target[3]

Alignment:
Source: café
        |||X
Target: cafe
```

## Design Decisions

1. **`Vec<char>` over byte slices**: Operating on `char` boundaries guarantees correct Unicode handling. The cost is O(n) conversion from `&str`, but edit distance is already O(mn) so this is negligible.

2. **Saturating arithmetic**: Using `saturating_add` when computing costs prevents overflow panics when a cell is initialized to `u32::MAX`. An alternative is to use `Option<u32>` or `i64`, but saturating is simpler and correct here since we never produce costs near `u32::MAX` in practice.

3. **Optimal String Alignment variant**: The transposition implementation uses the OSA variant, not full Damerau-Levenshtein. OSA is simpler (no auxiliary data structures) but does not satisfy the triangle inequality. For full DL, you would need an additional bookkeeping array tracking the last row where each character appeared. OSA is sufficient for most spell-checking and diff applications.

4. **Merge and Split are unconditional**: The current implementation allows merge and split between any characters, treating them as generic "2-to-1" and "1-to-2" operations. A production system would likely constrain these to specific character pairs (e.g., ligature tables). Adding a `MergeTable` trait that validates which merges are allowed is a natural extension.

5. **Parallel ops table**: Storing operations in a separate `Vec<Vec<OpKind>>` instead of embedding them in the DP table keeps the cost computation clean. The memory overhead is one byte per cell (enum discriminant), negligible compared to the 4-byte cost values.

## Common Mistakes

1. **Off-by-one in transpose/merge/split bounds**: Forgetting to check `i >= 2` or `j >= 2` before accessing `dp[i-2][...]` causes panics. Every extended operation must guard its index arithmetic.

2. **Backtracking step size mismatch**: If the step sizes in backtracking do not match the transitions in the forward pass, the reconstructed script will be wrong or the backtracking will miss `(0, 0)`.

3. **Byte indexing on Unicode**: Using `source.as_bytes()[i]` or `&source[i..j]` on strings with multi-byte characters produces panics or garbled output. Always convert to `Vec<char>` first.

4. **Greedy tie-breaking**: When multiple operations produce the same cost, the order of comparison determines which one wins. This is correct (any optimal path is valid) but can produce different scripts for the same input. Tests should check distance, not exact script equality, unless costs are designed to produce a unique path.

5. **Forgetting the match case**: The match (zero-cost) operation is not just an optimization -- it is essential for correct backtracking. Without it, every diagonal step would be recorded as a replace, producing nonsensical scripts.

## Performance Notes

- Time complexity: O(mn) where m and n are the lengths of source and target strings (in characters, not bytes)
- Space complexity: O(mn) for the full DP table and ops table. Can be reduced to O(min(m,n)) if only the distance is needed (no backtracking), but script reconstruction requires the full table
- For very long strings (thousands of characters), consider banded DP that only computes cells within a diagonal band of width `k` (the expected maximum distance). This reduces complexity to O(kn) but requires knowing `k` in advance

## Going Further

- Implement full Damerau-Levenshtein (not just OSA) to satisfy the triangle inequality
- Add a `MergeTable` / `SplitTable` that constrains which character combinations are valid for merge and split
- Implement banded DP for approximate matching of long strings
- Add affine gap costs (opening a gap is expensive, extending it is cheap) as used in bioinformatics sequence alignment
- Build a spell checker that uses this engine with a dictionary and frequency-weighted costs
- Parallelize the DP computation using anti-diagonal wavefront (cells on the same anti-diagonal are independent)
