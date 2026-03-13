# 30. CP: HashMaps and Counting

**Difficulty**: Intermedio

## Prerequisites
- Completed: ownership, borrowing, iterators, closures, Option/Result
- Comfortable with `HashMap` basics (insert, get, contains_key)
- Familiar with iterators and collecting results

## Learning Objectives
After completing this exercise, you will be able to:
- Use the `Entry` API (`or_insert`, `or_default`, `and_modify`) for efficient map operations
- Solve classic competitive programming problems using HashMaps
- Choose between `HashMap` and `BTreeMap` based on ordering needs
- Build frequency counters and use them for grouping, anagram detection, and more
- Understand hashing considerations: `DefaultHasher`, `FxHashMap`, and when performance matters

## Concepts

### The Entry API

The most powerful feature of Rust's `HashMap` for competitive programming is the `Entry` API. It avoids double lookups (check-then-insert) by providing a single entry point:

```rust
use std::collections::HashMap;

let mut counts: HashMap<&str, u32> = HashMap::new();

// Without entry API — two lookups
if let Some(count) = counts.get_mut("apple") {
    *count += 1;
} else {
    counts.insert("apple", 1);
}

// With entry API — one lookup
*counts.entry("apple").or_insert(0) += 1;
```

The entry API variants:

```rust
let mut map: HashMap<String, Vec<i32>> = HashMap::new();

// or_insert: insert a default value if key is missing
map.entry("key".to_string()).or_insert(Vec::new()).push(1);

// or_insert_with: lazily compute default (avoids allocation if key exists)
map.entry("key".to_string()).or_insert_with(Vec::new).push(2);

// or_default: uses Default::default() — Vec::new() for Vec, 0 for numbers
map.entry("key".to_string()).or_default().push(3);

// and_modify + or_insert: modify existing OR insert new
map.entry("key".to_string())
    .and_modify(|v| v.push(99))
    .or_insert(vec![99]);
```

### Frequency Counting Pattern

The most common CP pattern with HashMaps:

```rust
fn frequency(items: &[i32]) -> HashMap<i32, usize> {
    let mut freq = HashMap::new();
    for &item in items {
        *freq.entry(item).or_insert(0) += 1;
    }
    freq
}

// Or with iterators:
fn frequency_iter(items: &[i32]) -> HashMap<i32, usize> {
    items.iter().fold(HashMap::new(), |mut acc, &item| {
        *acc.entry(item).or_insert(0) += 1;
        acc
    })
}
```

### HashMap vs BTreeMap

| Feature | HashMap | BTreeMap |
|---------|---------|----------|
| Lookup | O(1) average | O(log n) |
| Insertion | O(1) average | O(log n) |
| Iteration order | Arbitrary | Sorted by key |
| Range queries | Not supported | `range()`, `range_mut()` |
| Use when | Speed matters most | Need sorted keys or ranges |

```rust
use std::collections::BTreeMap;

let mut sorted: BTreeMap<&str, i32> = BTreeMap::new();
sorted.insert("banana", 2);
sorted.insert("apple", 5);
sorted.insert("cherry", 1);

// Iterates in key order: apple, banana, cherry
for (key, value) in &sorted {
    println!("{}: {}", key, value);
}

// Range query — keys from "b" to "d"
for (key, value) in sorted.range("b".."d") {
    println!("{}: {}", key, value);
}
```

### Hashing Considerations

Rust's `HashMap` uses `SipHash` by default — designed to be resistant to HashDoS attacks. For CP where you control the input:

- **Default (`SipHash`)**: safe, good enough for most problems.
- **`FxHashMap` (from `rustc-hash` crate)**: faster for integer and small keys, no DoS protection.

For competitive programming, the default hasher is fine. If you hit time limits on large inputs, consider `FxHashMap`.

### Common CP Techniques with HashMaps

1. **Two-pass**: count frequencies first, then query.
2. **One-pass with complement**: for "two sum" — check if complement exists while iterating.
3. **Grouping**: use a HashMap where values are Vecs to group items by a key.
4. **Sliding window**: use a HashMap to track frequencies in a window.

## Exercises

### Exercise 1: Two Sum

Given an array of integers and a target sum, return the indices of two numbers that add up to the target. Each input has exactly one solution, and you cannot use the same element twice.

```rust
use std::collections::HashMap;

// TODO: Implement two_sum
// Strategy: iterate once, for each number check if (target - num) is already
// in the map. If yes, return the pair of indices. If no, insert num -> index.
//
// Return: (index1, index2) where index1 < index2
fn two_sum(nums: &[i32], target: i32) -> (usize, usize) {
    // TODO: Use a HashMap<i32, usize> to store value -> index
    todo!()
}

fn main() {
    let tests = vec![
        (vec![2, 7, 11, 15], 9),
        (vec![3, 2, 4], 6),
        (vec![3, 3], 6),
        (vec![1, 5, 3, 7, 2, 8], 10),
        (vec![-1, -2, -3, -4, -5], -8),
    ];

    for (nums, target) in tests {
        let (i, j) = two_sum(&nums, target);
        println!(
            "nums={:?}, target={} -> indices=({}, {}), values=({}, {}), sum={}",
            nums, target, i, j, nums[i], nums[j], nums[i] + nums[j]
        );
    }
}
```

Expected output:
```
nums=[2, 7, 11, 15], target=9 -> indices=(0, 1), values=(2, 7), sum=9
nums=[3, 2, 4], target=6 -> indices=(1, 2), values=(2, 4), sum=6
nums=[3, 3], target=6 -> indices=(0, 1), values=(3, 3), sum=6
nums=[1, 5, 3, 7, 2, 8], target=10 -> indices=(1, 3), values=(5, 7), sum=12
nums=[-1, -2, -3, -4, -5], target=-8 -> indices=(2, 4), values=(-3, -5), sum=-8
```

Note: for `[1, 5, 3, 7, 2, 8]` with target 10, the first valid pair found depends on iteration order. The expected output shows `(1, 3)` for `5 + 7 = 12`, but the correct pair for target 10 is `(1, 5)` for `5 + 8 = 13`... actually `(2, 3)` for `3 + 7 = 10`. Let me fix the expected output:

Expected output:
```
nums=[2, 7, 11, 15], target=9 -> indices=(0, 1), values=(2, 7), sum=9
nums=[3, 2, 4], target=6 -> indices=(1, 2), values=(2, 4), sum=6
nums=[3, 3], target=6 -> indices=(0, 1), values=(3, 3), sum=6
nums=[1, 5, 3, 7, 2, 8], target=10 -> indices=(2, 3), values=(3, 7), sum=10
nums=[-1, -2, -3, -4, -5], target=-8 -> indices=(2, 4), values=(-3, -5), sum=-8
```

<details>
<summary>Solution</summary>

```rust
use std::collections::HashMap;

fn two_sum(nums: &[i32], target: i32) -> (usize, usize) {
    let mut seen: HashMap<i32, usize> = HashMap::new();

    for (i, &num) in nums.iter().enumerate() {
        let complement = target - num;
        if let Some(&j) = seen.get(&complement) {
            return (j, i);
        }
        seen.insert(num, i);
    }

    unreachable!("Problem guarantees exactly one solution")
}

fn main() {
    let tests = vec![
        (vec![2, 7, 11, 15], 9),
        (vec![3, 2, 4], 6),
        (vec![3, 3], 6),
        (vec![1, 5, 3, 7, 2, 8], 10),
        (vec![-1, -2, -3, -4, -5], -8),
    ];

    for (nums, target) in tests {
        let (i, j) = two_sum(&nums, target);
        println!(
            "nums={:?}, target={} -> indices=({}, {}), values=({}, {}), sum={}",
            nums, target, i, j, nums[i], nums[j], nums[i] + nums[j]
        );
    }
}
```
</details>

### Exercise 2: Group Anagrams

Given an array of strings, group anagrams together. Two strings are anagrams if they contain the same characters with the same frequencies.

```rust
use std::collections::HashMap;

// TODO: Implement group_anagrams
// Strategy: for each word, create a "signature" by sorting its characters.
// Words with the same signature are anagrams.
// Use a HashMap<String, Vec<String>> to group them.
//
// Return groups sorted by first element, each group also sorted.
fn group_anagrams(words: &[&str]) -> Vec<Vec<String>> {
    // TODO:
    // 1. Create a HashMap where key = sorted chars, value = Vec of original words
    // 2. For each word, sort its chars to create the key
    // 3. Use the entry API to insert into the correct group
    // 4. Collect all groups, sort each group, and sort the groups
    todo!()
}

fn main() {
    let words = vec!["eat", "tea", "tan", "ate", "nat", "bat"];
    let groups = group_anagrams(&words);

    println!("Input: {:?}", words);
    println!("Groups:");
    for group in &groups {
        println!("  {:?}", group);
    }

    println!();

    let words2 = vec!["listen", "silent", "hello", "world", "enlist", "tinsel", "olleh"];
    let groups2 = group_anagrams(&words2);

    println!("Input: {:?}", words2);
    println!("Groups:");
    for group in &groups2 {
        println!("  {:?}", group);
    }
}
```

Expected output:
```
Input: ["eat", "tea", "tan", "ate", "nat", "bat"]
Groups:
  ["ate", "eat", "tea"]
  ["bat"]
  ["nat", "tan"]

Input: ["listen", "silent", "hello", "world", "enlist", "tinsel", "olleh"]
Groups:
  ["enlist", "listen", "silent", "tinsel"]
  ["hello", "olleh"]
  ["world"]
```

<details>
<summary>Solution</summary>

```rust
use std::collections::HashMap;

fn group_anagrams(words: &[&str]) -> Vec<Vec<String>> {
    let mut groups: HashMap<String, Vec<String>> = HashMap::new();

    for &word in words {
        let mut chars: Vec<char> = word.chars().collect();
        chars.sort();
        let key: String = chars.into_iter().collect();

        groups
            .entry(key)
            .or_default()
            .push(word.to_string());
    }

    let mut result: Vec<Vec<String>> = groups.into_values().collect();

    // Sort each group internally
    for group in &mut result {
        group.sort();
    }

    // Sort groups by first element
    result.sort_by(|a, b| a[0].cmp(&b[0]));

    result
}

fn main() {
    let words = vec!["eat", "tea", "tan", "ate", "nat", "bat"];
    let groups = group_anagrams(&words);

    println!("Input: {:?}", words);
    println!("Groups:");
    for group in &groups {
        println!("  {:?}", group);
    }

    println!();

    let words2 = vec!["listen", "silent", "hello", "world", "enlist", "tinsel", "olleh"];
    let groups2 = group_anagrams(&words2);

    println!("Input: {:?}", words2);
    println!("Groups:");
    for group in &groups2 {
        println!("  {:?}", group);
    }
}
```
</details>

### Exercise 3: Longest Consecutive Sequence

Given an unsorted array of integers, find the length of the longest consecutive elements sequence. Your algorithm must run in O(n) time.

For example: `[100, 4, 200, 1, 3, 2]` -> the longest consecutive sequence is `[1, 2, 3, 4]`, length = 4.

```rust
use std::collections::HashSet;

// TODO: Implement longest_consecutive
// Strategy:
// 1. Put all numbers in a HashSet for O(1) lookup
// 2. For each number, check if it is the START of a sequence
//    (i.e., num - 1 is NOT in the set)
// 3. If it is a start, count how many consecutive numbers follow
// 4. Track the maximum length
//
// This is O(n) because each number is visited at most twice
// (once in the outer loop, once as part of a consecutive chain).
fn longest_consecutive(nums: &[i32]) -> usize {
    todo!()
}

fn main() {
    let tests: Vec<(Vec<i32>, usize)> = vec![
        (vec![100, 4, 200, 1, 3, 2], 4),
        (vec![0, 3, 7, 2, 5, 8, 4, 6, 0, 1], 9),
        (vec![1, 2, 0, 1], 3),
        (vec![9, 1, 4, 7, 3, -1, 0, 5, 8, -1, 6], 7),
        (vec![], 0),
        (vec![42], 1),
    ];

    for (nums, expected) in tests {
        let result = longest_consecutive(&nums);
        let status = if result == expected { "PASS" } else { "FAIL" };
        println!(
            "[{}] nums={:?} -> {} (expected {})",
            status, nums, result, expected
        );
    }
}
```

Expected output:
```
[PASS] nums=[100, 4, 200, 1, 3, 2] -> 4 (expected 4)
[PASS] nums=[0, 3, 7, 2, 5, 8, 4, 6, 0, 1] -> 9 (expected 9)
[PASS] nums=[1, 2, 0, 1] -> 3 (expected 3)
[PASS] nums=[9, 1, 4, 7, 3, -1, 0, 5, 8, -1, 6] -> 7 (expected 7)
[PASS] nums=[] -> 0 (expected 0)
[PASS] nums=[42] -> 1 (expected 1)
```

<details>
<summary>Solution</summary>

```rust
use std::collections::HashSet;

fn longest_consecutive(nums: &[i32]) -> usize {
    let set: HashSet<i32> = nums.iter().copied().collect();
    let mut max_len = 0;

    for &num in &set {
        // Only start counting from the beginning of a sequence
        if !set.contains(&(num - 1)) {
            let mut current = num;
            let mut length = 1;

            while set.contains(&(current + 1)) {
                current += 1;
                length += 1;
            }

            max_len = max_len.max(length);
        }
    }

    max_len
}

fn main() {
    let tests: Vec<(Vec<i32>, usize)> = vec![
        (vec![100, 4, 200, 1, 3, 2], 4),
        (vec![0, 3, 7, 2, 5, 8, 4, 6, 0, 1], 9),
        (vec![1, 2, 0, 1], 3),
        (vec![9, 1, 4, 7, 3, -1, 0, 5, 8, -1, 6], 7),
        (vec![], 0),
        (vec![42], 1),
    ];

    for (nums, expected) in tests {
        let result = longest_consecutive(&nums);
        let status = if result == expected { "PASS" } else { "FAIL" };
        println!(
            "[{}] nums={:?} -> {} (expected {})",
            status, nums, result, expected
        );
    }
}
```
</details>

### Exercise 4: Top K Frequent Elements

Given an integer array and an integer k, return the k most frequent elements. You may return the answer in any order.

```rust
use std::collections::HashMap;

// TODO: Implement top_k_frequent
// Strategy (bucket sort approach — O(n)):
// 1. Count frequency of each number using a HashMap
// 2. Create "buckets" where index = frequency, value = list of numbers with that frequency
//    (bucket size = nums.len() + 1, since max frequency = nums.len())
// 3. Iterate buckets from highest to lowest, collecting numbers until you have k
//
// Alternative strategy (heap approach — O(n log k)):
// Use a BinaryHeap or sort by frequency.
fn top_k_frequent(nums: &[i32], k: usize) -> Vec<i32> {
    todo!()
}

fn main() {
    let tests = vec![
        (vec![1, 1, 1, 2, 2, 3], 2),
        (vec![1], 1),
        (vec![1, 2, 3, 1, 2, 1, 2, 3, 3, 3], 2),
        (vec![4, 1, -1, 2, -1, 2, 3], 2),
    ];

    for (nums, k) in tests {
        let mut result = top_k_frequent(&nums, k);
        result.sort(); // Sort for consistent output
        println!("nums={:?}, k={} -> {:?}", nums, k, result);
    }
}
```

Expected output:
```
nums=[1, 1, 1, 2, 2, 3], k=2 -> [1, 2]
nums=[1], k=1 -> [1]
nums=[1, 2, 3, 1, 2, 1, 2, 3, 3, 3], k=2 -> [1, 3]
nums=[4, 1, -1, 2, -1, 2, 3], k=2 -> [-1, 2]
```

<details>
<summary>Solution</summary>

```rust
use std::collections::HashMap;

fn top_k_frequent(nums: &[i32], k: usize) -> Vec<i32> {
    // Step 1: Count frequencies
    let mut freq: HashMap<i32, usize> = HashMap::new();
    for &num in nums {
        *freq.entry(num).or_insert(0) += 1;
    }

    // Step 2: Bucket sort — index is frequency
    let mut buckets: Vec<Vec<i32>> = vec![Vec::new(); nums.len() + 1];
    for (&num, &count) in &freq {
        buckets[count].push(num);
    }

    // Step 3: Collect from highest frequency
    let mut result = Vec::new();
    for bucket in buckets.iter().rev() {
        for &num in bucket {
            result.push(num);
            if result.len() == k {
                return result;
            }
        }
    }

    result
}

fn main() {
    let tests = vec![
        (vec![1, 1, 1, 2, 2, 3], 2),
        (vec![1], 1),
        (vec![1, 2, 3, 1, 2, 1, 2, 3, 3, 3], 2),
        (vec![4, 1, -1, 2, -1, 2, 3], 2),
    ];

    for (nums, k) in tests {
        let mut result = top_k_frequent(&nums, k);
        result.sort();
        println!("nums={:?}, k={} -> {:?}", nums, k, result);
    }
}
```
</details>

### Exercise 5: Isomorphic Strings

Two strings `s` and `t` are isomorphic if the characters in `s` can be replaced to get `t`, where:
- Each character maps to exactly one character.
- No two characters map to the same character.
- Order is preserved.

```rust
use std::collections::HashMap;

// TODO: Implement is_isomorphic
// Strategy: maintain two maps:
// 1. s_to_t: maps each char in s to the corresponding char in t
// 2. t_to_s: maps each char in t to the corresponding char in s
// For each pair (sc, tc):
//   - If sc is already mapped to something other than tc, return false
//   - If tc is already mapped to something other than sc, return false
//   - Otherwise, insert both mappings
fn is_isomorphic(s: &str, t: &str) -> bool {
    todo!()
}

// BONUS TODO: Implement `group_isomorphic` that groups words by their
// isomorphic "pattern". Two words are in the same group if they are
// isomorphic to each other.
//
// Hint: encode each word as a pattern like "0.1.2.1.0" where each unique
// char gets the next number. Words with the same pattern are isomorphic.
fn isomorphic_pattern(word: &str) -> String {
    // TODO: Map each unique character to a sequential number
    // Return the pattern as a string of numbers separated by dots
    todo!()
}

fn group_isomorphic(words: &[&str]) -> Vec<Vec<String>> {
    // TODO: Group words by their isomorphic pattern
    todo!()
}

fn main() {
    let tests = vec![
        ("egg", "add", true),
        ("foo", "bar", false),
        ("paper", "title", true),
        ("ab", "aa", false),
        ("abcabc", "xyzxyz", true),
        ("abc", "def", true),
        ("aab", "xyz", false),
    ];

    println!("=== Isomorphic Pairs ===");
    for (s, t, expected) in &tests {
        let result = is_isomorphic(s, t);
        let status = if result == *expected { "PASS" } else { "FAIL" };
        println!("[{}] is_isomorphic(\"{}\", \"{}\") = {} (expected {})", status, s, t, result, expected);
    }

    println!("\n=== Isomorphic Patterns ===");
    let words = vec!["abc", "bcd", "egg", "add", "foo", "bar", "aba", "xyx"];
    for word in &words {
        println!("  \"{}\" -> pattern: {}", word, isomorphic_pattern(word));
    }

    println!("\n=== Isomorphic Groups ===");
    let groups = group_isomorphic(&words);
    for group in &groups {
        println!("  {:?}", group);
    }
}
```

Expected output:
```
=== Isomorphic Pairs ===
[PASS] is_isomorphic("egg", "add") = true (expected true)
[PASS] is_isomorphic("foo", "bar") = false (expected false)
[PASS] is_isomorphic("paper", "title") = true (expected true)
[PASS] is_isomorphic("ab", "aa") = false (expected false)
[PASS] is_isomorphic("abcabc", "xyzxyz") = true (expected true)
[PASS] is_isomorphic("abc", "def") = true (expected true)
[PASS] is_isomorphic("aab", "xyz") = false (expected false)

=== Isomorphic Patterns ===
  "abc" -> pattern: 0.1.2
  "bcd" -> pattern: 0.1.2
  "egg" -> pattern: 0.1.1
  "add" -> pattern: 0.1.1
  "foo" -> pattern: 0.1.1
  "bar" -> pattern: 0.1.2
  "aba" -> pattern: 0.1.0
  "xyx" -> pattern: 0.1.0

=== Isomorphic Groups ===
  ["abc", "bcd", "bar"]
  ["aba", "xyx"]
  ["egg", "add", "foo"]
```

<details>
<summary>Solution</summary>

```rust
use std::collections::HashMap;

fn is_isomorphic(s: &str, t: &str) -> bool {
    if s.len() != t.len() {
        return false;
    }

    let mut s_to_t: HashMap<char, char> = HashMap::new();
    let mut t_to_s: HashMap<char, char> = HashMap::new();

    for (sc, tc) in s.chars().zip(t.chars()) {
        match (s_to_t.get(&sc), t_to_s.get(&tc)) {
            (Some(&mapped_t), Some(&mapped_s)) => {
                if mapped_t != tc || mapped_s != sc {
                    return false;
                }
            }
            (None, None) => {
                s_to_t.insert(sc, tc);
                t_to_s.insert(tc, sc);
            }
            _ => return false, // One mapped, the other not
        }
    }

    true
}

fn isomorphic_pattern(word: &str) -> String {
    let mut map: HashMap<char, usize> = HashMap::new();
    let mut next_id = 0;
    let pattern: Vec<String> = word
        .chars()
        .map(|c| {
            let id = *map.entry(c).or_insert_with(|| {
                let id = next_id;
                next_id += 1;
                id
            });
            id.to_string()
        })
        .collect();
    pattern.join(".")
}

fn group_isomorphic(words: &[&str]) -> Vec<Vec<String>> {
    let mut groups: HashMap<String, Vec<String>> = HashMap::new();

    for &word in words {
        let pattern = isomorphic_pattern(word);
        groups
            .entry(pattern)
            .or_default()
            .push(word.to_string());
    }

    let mut result: Vec<Vec<String>> = groups.into_values().collect();
    for group in &mut result {
        group.sort();
    }
    result.sort_by(|a, b| a[0].cmp(&b[0]));
    result
}

fn main() {
    let tests = vec![
        ("egg", "add", true),
        ("foo", "bar", false),
        ("paper", "title", true),
        ("ab", "aa", false),
        ("abcabc", "xyzxyz", true),
        ("abc", "def", true),
        ("aab", "xyz", false),
    ];

    println!("=== Isomorphic Pairs ===");
    for (s, t, expected) in &tests {
        let result = is_isomorphic(s, t);
        let status = if result == *expected { "PASS" } else { "FAIL" };
        println!(
            "[{}] is_isomorphic(\"{}\", \"{}\") = {} (expected {})",
            status, s, t, result, expected
        );
    }

    println!("\n=== Isomorphic Patterns ===");
    let words = vec!["abc", "bcd", "egg", "add", "foo", "bar", "aba", "xyx"];
    for word in &words {
        println!("  \"{}\" -> pattern: {}", word, isomorphic_pattern(word));
    }

    println!("\n=== Isomorphic Groups ===");
    let groups = group_isomorphic(&words);
    for group in &groups {
        println!("  {:?}", group);
    }
}
```
</details>

## Common Mistakes

### Mistake 1: Double Lookup Instead of Entry API

```rust
// Slow — two lookups
if map.contains_key(&key) {
    *map.get_mut(&key).unwrap() += 1;
} else {
    map.insert(key, 1);
}

// Fast — one lookup
*map.entry(key).or_insert(0) += 1;
```

### Mistake 2: Iterating HashMap and Expecting Order

```rust
let mut map = HashMap::new();
map.insert(3, "three");
map.insert(1, "one");
map.insert(2, "two");

// Order is NOT guaranteed to be 1, 2, 3!
for (k, v) in &map {
    println!("{}: {}", k, v); // arbitrary order
}
```

**Fix**: Use `BTreeMap` for sorted iteration, or collect into a `Vec` and sort.

### Mistake 3: Forgetting That `entry()` Takes Ownership of the Key

```rust
let key = String::from("hello");
map.entry(key).or_insert(0); // `key` is moved into the map!
// println!("{}", key); // Error: key was moved
```

**Fix**: Clone the key if you need it later, or use `&str` keys with a `HashMap<String, V>` by calling `entry(key.to_string())`.

### Mistake 4: Using HashMap for Sorted Output in CP

When a problem asks for sorted output, using `HashMap` means an extra sort step. If you need sorted keys throughout, use `BTreeMap` from the start:

```rust
use std::collections::BTreeMap;

let mut freq: BTreeMap<i32, usize> = BTreeMap::new();
for &num in &[3, 1, 4, 1, 5, 9, 2, 6] {
    *freq.entry(num).or_insert(0) += 1;
}
// Iteration is automatically sorted by key
for (num, count) in &freq {
    println!("{}: {}", num, count); // 1, 2, 3, 4, 5, 6, 9
}
```

## Verification

```bash
cargo run
```

For each exercise, verify:
1. All test cases show `[PASS]`.
2. Try adding edge cases — empty arrays, single elements, all duplicates.
3. Verify time complexity by considering the number of HashMap operations.
4. Try replacing `HashMap` with `BTreeMap` — does the output change for sorted operations?

## What You Learned

- The `entry` API (`or_insert`, `or_default`, `and_modify`) avoids double lookups and is the idiomatic way to update HashMap values.
- Two-sum, group anagrams, longest consecutive sequence, and top-k frequent are classic problems solvable in O(n) with HashMaps.
- `HashMap` gives O(1) average operations; `BTreeMap` gives O(log n) with sorted iteration.
- Frequency counting is the most common HashMap pattern in competitive programming.
- Isomorphic pattern encoding lets you group structurally identical strings.
- Rust's ownership model means `entry()` takes ownership of the key — clone when needed.

## What's Next

Continue with more advanced data structures and algorithms — trees, graphs, and dynamic programming all benefit from HashMap-based memoization.

## Resources

- [std::collections::HashMap](https://doc.rust-lang.org/std/collections/struct.HashMap.html)
- [std::collections::BTreeMap](https://doc.rust-lang.org/std/collections/struct.BTreeMap.html)
- [Entry API documentation](https://doc.rust-lang.org/std/collections/hash_map/enum.Entry.html)
- [LeetCode — Two Sum](https://leetcode.com/problems/two-sum/)
- [LeetCode — Group Anagrams](https://leetcode.com/problems/group-anagrams/)
- [LeetCode — Longest Consecutive Sequence](https://leetcode.com/problems/longest-consecutive-sequence/)
- [LeetCode — Top K Frequent Elements](https://leetcode.com/problems/top-k-frequent-elements/)
