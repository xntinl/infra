# 46. CP: Trie Data Structure

**Difficulty**: Avanzado

## Prerequisites

- Solid understanding of Rust ownership, borrowing, and lifetimes
- Familiarity with `HashMap`, `Box`, and recursive data structures
- Completed: exercises on smart pointers, traits, and pattern matching

## Learning Objectives

- Understand the Trie (prefix tree) data structure and its time/space trade-offs
- Implement tries using both `HashMap<char, Box<Node>>` and array-based (fixed alphabet) approaches
- Handle Rust ownership challenges when building tree structures with mutable traversal
- Solve problems: autocomplete, longest common prefix, word counting, maximum XOR pair
- Build a XOR trie for bitwise optimization problems

## Concepts

A Trie (from "retrieval", pronounced "try") is a tree-shaped data structure for storing strings. Each edge represents a character, and each node represents a prefix. Unlike a hash map that stores complete strings, a trie shares common prefixes, making it memory-efficient for dictionaries with overlapping prefixes and enabling prefix-based queries that hash maps cannot do efficiently.

### Why Tries in Rust Are Interesting

Tries are a perfect exercise in Rust ownership because they force you to deal with recursive mutable access. Walking down a trie to insert a string requires `&mut` access at each level, and the borrow checker demands that you do this correctly. The two main approaches -- `HashMap`-based and array-based -- illustrate different trade-offs between flexibility and performance.

### Time Complexity

| Operation | Time | Notes |
|-----------|------|-------|
| Insert | O(L) | L = length of the string |
| Search | O(L) | Exact match |
| Starts with | O(L) | Prefix existence |
| Delete | O(L) | With cleanup of unused nodes |
| Longest common prefix | O(L) | Follow single-child chain |
| Count words with prefix | O(L) | Reach prefix node, read counter |

All operations are independent of the number of strings stored -- they depend only on the length of the query string.

---

## Implementation

### HashMap-Based Trie

This approach works with any character set (Unicode, arbitrary alphabets) but has higher overhead per node due to HashMap allocation.

```rust
use std::collections::HashMap;

#[derive(Default, Debug)]
struct TrieNode {
    children: HashMap<char, Box<TrieNode>>,
    is_end: bool,
    /// Number of words that pass through this node (including words ending here).
    prefix_count: usize,
    /// Number of words that end exactly at this node.
    word_count: usize,
}

struct Trie {
    root: TrieNode,
}

impl Trie {
    fn new() -> Self {
        Self {
            root: TrieNode::default(),
        }
    }

    fn insert(&mut self, word: &str) {
        let mut node = &mut self.root;
        for ch in word.chars() {
            node.prefix_count += 1;
            node = node.children.entry(ch).or_insert_with(|| Box::new(TrieNode::default()));
        }
        node.prefix_count += 1;
        node.is_end = true;
        node.word_count += 1;
    }

    fn search(&self, word: &str) -> bool {
        self.find_node(word).map_or(false, |node| node.is_end)
    }

    fn starts_with(&self, prefix: &str) -> bool {
        self.find_node(prefix).is_some()
    }

    /// Count how many inserted words have the given prefix.
    fn count_with_prefix(&self, prefix: &str) -> usize {
        self.find_node(prefix).map_or(0, |node| node.prefix_count)
    }

    /// Count exact occurrences of a word (supports duplicate inserts).
    fn count_exact(&self, word: &str) -> usize {
        self.find_node(word).map_or(0, |node| node.word_count)
    }

    /// Return all words in the trie with the given prefix.
    fn autocomplete(&self, prefix: &str) -> Vec<String> {
        let mut results = Vec::new();
        if let Some(node) = self.find_node(prefix) {
            let mut current = prefix.to_string();
            Self::collect_words(node, &mut current, &mut results);
        }
        results
    }

    fn find_node(&self, prefix: &str) -> Option<&TrieNode> {
        let mut node = &self.root;
        for ch in prefix.chars() {
            match node.children.get(&ch) {
                Some(child) => node = child,
                None => return None,
            }
        }
        Some(node)
    }

    fn collect_words(node: &TrieNode, current: &mut String, results: &mut Vec<String>) {
        if node.is_end {
            results.push(current.clone());
        }
        // Sort keys for deterministic order
        let mut keys: Vec<char> = node.children.keys().copied().collect();
        keys.sort();
        for ch in keys {
            current.push(ch);
            Self::collect_words(&node.children[&ch], current, results);
            current.pop();
        }
    }

    /// Delete a word from the trie. Returns true if the word existed.
    fn delete(&mut self, word: &str) -> bool {
        Self::delete_recursive(&mut self.root, word, 0)
    }

    fn delete_recursive(node: &mut TrieNode, word: &str, depth: usize) -> bool {
        let chars: Vec<char> = word.chars().collect();

        if depth == chars.len() {
            if !node.is_end {
                return false;
            }
            node.is_end = false;
            node.word_count -= 1;
            node.prefix_count -= 1;
            return true;
        }

        let ch = chars[depth];
        if let Some(child) = node.children.get_mut(&ch) {
            if Self::delete_recursive(child, word, depth + 1) {
                child.prefix_count; // already decremented recursively
                node.prefix_count -= 1;

                // Remove child node if it has no more words passing through it
                if child.prefix_count == 0 {
                    node.children.remove(&ch);
                }
                return true;
            }
        }

        false
    }
}

fn main() {
    let mut trie = Trie::new();

    trie.insert("apple");
    trie.insert("app");
    trie.insert("application");
    trie.insert("apply");
    trie.insert("banana");
    trie.insert("band");
    trie.insert("bandana");

    println!("search 'apple': {}", trie.search("apple"));       // true
    println!("search 'app': {}", trie.search("app"));           // true
    println!("search 'ap': {}", trie.search("ap"));             // false
    println!("starts_with 'ap': {}", trie.starts_with("ap"));   // true
    println!("starts_with 'cat': {}", trie.starts_with("cat")); // false

    println!("count_with_prefix 'app': {}", trie.count_with_prefix("app")); // 4
    println!("count_with_prefix 'ban': {}", trie.count_with_prefix("ban")); // 3

    println!("autocomplete 'app': {:?}", trie.autocomplete("app"));
    // ["app", "apple", "application", "apply"]

    println!("autocomplete 'ban': {:?}", trie.autocomplete("ban"));
    // ["banana", "band", "bandana"]

    trie.delete("app");
    println!("after delete 'app':");
    println!("search 'app': {}", trie.search("app"));           // false
    println!("search 'apple': {}", trie.search("apple"));       // true
    println!("count_with_prefix 'app': {}", trie.count_with_prefix("app")); // 3
}
```

### Array-Based Trie (Fixed Alphabet)

When the alphabet is known and small (e.g., lowercase English letters), an array-based trie is significantly faster due to cache locality and no hashing overhead.

```rust
const ALPHABET_SIZE: usize = 26;

#[derive(Clone)]
struct ArrayTrieNode {
    children: [Option<Box<ArrayTrieNode>>; ALPHABET_SIZE],
    is_end: bool,
    prefix_count: usize,
}

impl ArrayTrieNode {
    fn new() -> Self {
        Self {
            // Cannot use [None; 26] because Box is not Copy.
            // This const-generic initialization works in Rust:
            children: std::array::from_fn(|_| None),
            is_end: false,
            prefix_count: 0,
        }
    }
}

struct ArrayTrie {
    root: ArrayTrieNode,
}

impl ArrayTrie {
    fn new() -> Self {
        Self {
            root: ArrayTrieNode::new(),
        }
    }

    fn char_to_index(ch: char) -> usize {
        (ch as u8 - b'a') as usize
    }

    fn insert(&mut self, word: &str) {
        let mut node = &mut self.root;
        for ch in word.chars() {
            let idx = Self::char_to_index(ch);
            node.prefix_count += 1;
            if node.children[idx].is_none() {
                node.children[idx] = Some(Box::new(ArrayTrieNode::new()));
            }
            node = node.children[idx].as_mut().unwrap();
        }
        node.prefix_count += 1;
        node.is_end = true;
    }

    fn search(&self, word: &str) -> bool {
        let mut node = &self.root;
        for ch in word.chars() {
            let idx = Self::char_to_index(ch);
            match &node.children[idx] {
                Some(child) => node = child,
                None => return false,
            }
        }
        node.is_end
    }

    fn starts_with(&self, prefix: &str) -> bool {
        let mut node = &self.root;
        for ch in prefix.chars() {
            let idx = Self::char_to_index(ch);
            match &node.children[idx] {
                Some(child) => node = child,
                None => return false,
            }
        }
        true
    }

    fn count_with_prefix(&self, prefix: &str) -> usize {
        let mut node = &self.root;
        for ch in prefix.chars() {
            let idx = Self::char_to_index(ch);
            match &node.children[idx] {
                Some(child) => node = child,
                None => return 0,
            }
        }
        node.prefix_count
    }
}

fn main() {
    let mut trie = ArrayTrie::new();

    for word in &["apple", "app", "application", "apply", "banana", "band"] {
        trie.insert(word);
    }

    assert!(trie.search("apple"));
    assert!(trie.search("app"));
    assert!(!trie.search("ap"));
    assert!(trie.starts_with("ap"));
    assert_eq!(trie.count_with_prefix("app"), 4);
    assert_eq!(trie.count_with_prefix("ban"), 2);

    println!("array trie: all assertions passed");
}
```

### Arena-Based Trie (Index-Linked)

For competitive programming, an arena-based approach avoids `Box` entirely. All nodes live in a `Vec`, and children are indices. This is the fastest approach and the easiest to reason about in terms of memory.

```rust
const ALPHA: usize = 26;

struct ArenaTrie {
    nodes: Vec<[i32; ALPHA]>,  // -1 means "no child"
    is_end: Vec<bool>,
    prefix_count: Vec<usize>,
}

impl ArenaTrie {
    fn new() -> Self {
        let mut trie = Self {
            nodes: Vec::new(),
            is_end: Vec::new(),
            prefix_count: Vec::new(),
        };
        trie.new_node(); // root = node 0
        trie
    }

    fn new_node(&mut self) -> usize {
        let id = self.nodes.len();
        self.nodes.push([-1; ALPHA]);
        self.is_end.push(false);
        self.prefix_count.push(0);
        id
    }

    fn insert(&mut self, word: &str) {
        let mut cur = 0usize;
        for ch in word.bytes() {
            let idx = (ch - b'a') as usize;
            self.prefix_count[cur] += 1;
            if self.nodes[cur][idx] == -1 {
                let new_id = self.new_node();
                self.nodes[cur][idx] = new_id as i32;
            }
            cur = self.nodes[cur][idx] as usize;
        }
        self.prefix_count[cur] += 1;
        self.is_end[cur] = true;
    }

    fn search(&self, word: &str) -> bool {
        let mut cur = 0usize;
        for ch in word.bytes() {
            let idx = (ch - b'a') as usize;
            if self.nodes[cur][idx] == -1 {
                return false;
            }
            cur = self.nodes[cur][idx] as usize;
        }
        self.is_end[cur]
    }

    fn count_with_prefix(&self, prefix: &str) -> usize {
        let mut cur = 0usize;
        for ch in prefix.bytes() {
            let idx = (ch - b'a') as usize;
            if self.nodes[cur][idx] == -1 {
                return 0;
            }
            cur = self.nodes[cur][idx] as usize;
        }
        self.prefix_count[cur]
    }
}

fn main() {
    let mut trie = ArenaTrie::new();
    for word in &["apple", "app", "application", "apply", "banana", "band"] {
        trie.insert(word);
    }

    assert!(trie.search("apple"));
    assert!(!trie.search("ap"));
    assert_eq!(trie.count_with_prefix("app"), 4);
    println!("arena trie: all assertions passed");
    println!("total nodes allocated: {}", trie.nodes.len());
}
```

---

## Longest Common Prefix

Find the longest common prefix among a set of strings using a trie. Walk down from the root as long as each node has exactly one child and is not a word ending.

```rust
fn longest_common_prefix(words: &[&str]) -> String {
    if words.is_empty() {
        return String::new();
    }

    let mut trie = Trie::new();
    for word in words {
        trie.insert(word);
    }

    let mut prefix = String::new();
    let mut node = &trie.root;

    loop {
        // Stop if this node is a word ending (one of the words is a prefix of others)
        if node.is_end {
            break;
        }
        // Stop if there are zero or multiple children
        if node.children.len() != 1 {
            break;
        }
        let (&ch, child) = node.children.iter().next().unwrap();
        prefix.push(ch);
        node = child;
    }

    prefix
}

fn main() {
    assert_eq!(longest_common_prefix(&["flower", "flow", "flight"]), "fl");
    assert_eq!(longest_common_prefix(&["interview", "internet", "internal"]), "inter");
    assert_eq!(longest_common_prefix(&["dog", "car", "race"]), "");
    assert_eq!(longest_common_prefix(&["alone"]), "alone");
    assert_eq!(longest_common_prefix(&[]), "");

    println!("longest common prefix: all tests passed");
}
```

---

## XOR Trie

A XOR trie stores numbers in binary form (most significant bit first) and enables finding the maximum XOR pair in O(n * B) where B is the number of bits. For each number, walk the trie greedily trying to take the opposite bit at each level.

```rust
struct XorTrie {
    nodes: Vec<[i32; 2]>,
    count: Vec<usize>,
}

impl XorTrie {
    fn new() -> Self {
        let mut trie = Self {
            nodes: Vec::new(),
            count: Vec::new(),
        };
        trie.new_node(); // root
        trie
    }

    fn new_node(&mut self) -> usize {
        let id = self.nodes.len();
        self.nodes.push([-1, -1]);
        self.count.push(0);
        id
    }

    /// Insert a number into the trie, using `bits` bits (MSB first).
    fn insert(&mut self, num: u64, bits: usize) {
        let mut cur = 0;
        for i in (0..bits).rev() {
            let bit = ((num >> i) & 1) as usize;
            self.count[cur] += 1;
            if self.nodes[cur][bit] == -1 {
                let new_id = self.new_node();
                self.nodes[cur][bit] = new_id as i32;
            }
            cur = self.nodes[cur][bit] as usize;
        }
        self.count[cur] += 1;
    }

    /// Find the maximum XOR of `num` with any number in the trie.
    /// Returns the maximum XOR value.
    fn max_xor(&self, num: u64, bits: usize) -> u64 {
        let mut cur = 0;
        let mut result = 0u64;

        for i in (0..bits).rev() {
            let bit = ((num >> i) & 1) as usize;
            let opposite = 1 - bit;

            // Greedily try the opposite bit to maximize XOR
            if self.nodes[cur][opposite] != -1 {
                result |= 1 << i;
                cur = self.nodes[cur][opposite] as usize;
            } else if self.nodes[cur][bit] != -1 {
                cur = self.nodes[cur][bit] as usize;
            } else {
                break; // trie is empty at this path
            }
        }

        result
    }
}

/// Find the maximum XOR of any pair in the array.
fn max_xor_pair(nums: &[u64]) -> u64 {
    if nums.len() < 2 {
        return 0;
    }

    let bits = 64 - nums.iter().max().unwrap().leading_zeros() as usize;
    let bits = bits.max(1); // at least 1 bit

    let mut trie = XorTrie::new();
    let mut max_xor = 0u64;

    trie.insert(nums[0], bits);
    for &num in &nums[1..] {
        max_xor = max_xor.max(trie.max_xor(num, bits));
        trie.insert(num, bits);
    }

    max_xor
}

fn main() {
    // Example: [3, 10, 5, 25, 2, 8]
    // 3  = 00011
    // 10 = 01010
    // 5  = 00101
    // 25 = 11001
    // 2  = 00010
    // 8  = 01000
    // Maximum XOR pair: 5 XOR 25 = 00101 XOR 11001 = 11100 = 28
    let nums = vec![3, 10, 5, 25, 2, 8];
    assert_eq!(max_xor_pair(&nums), 28);

    let nums = vec![1, 2, 3, 4, 5];
    assert_eq!(max_xor_pair(&nums), 7); // 3 XOR 4 = 7

    let nums = vec![0, 0, 0];
    assert_eq!(max_xor_pair(&nums), 0);

    println!("max XOR pair: all tests passed");
}
```

---

## Exercises

### Exercise 1: Word Dictionary with Wildcards

Implement a trie that supports search with `.` as a wildcard matching any single character. This is LeetCode 211 (Design Add and Search Words Data Structure).

**API:**
```
insert("bad")
insert("dad")
insert("mad")
search("pad") -> false
search("bad") -> true
search(".ad") -> true
search("b..") -> true
search("b.") -> false
```

**Constraint:** Recursive search with backtracking when hitting a wildcard.

<details>
<summary>Solution</summary>

```rust
use std::collections::HashMap;

#[derive(Default)]
struct WildcardTrie {
    children: HashMap<char, Box<WildcardTrie>>,
    is_end: bool,
}

impl WildcardTrie {
    fn new() -> Self {
        Self::default()
    }

    fn insert(&mut self, word: &str) {
        let mut node = self;
        for ch in word.chars() {
            node = node.children.entry(ch).or_insert_with(|| Box::new(Self::default()));
        }
        node.is_end = true;
    }

    fn search(&self, word: &str) -> bool {
        let chars: Vec<char> = word.chars().collect();
        Self::search_recursive(self, &chars, 0)
    }

    fn search_recursive(node: &WildcardTrie, chars: &[char], idx: usize) -> bool {
        if idx == chars.len() {
            return node.is_end;
        }

        let ch = chars[idx];
        if ch == '.' {
            // Wildcard: try all children
            for child in node.children.values() {
                if Self::search_recursive(child, chars, idx + 1) {
                    return true;
                }
            }
            false
        } else {
            match node.children.get(&ch) {
                Some(child) => Self::search_recursive(child, chars, idx + 1),
                None => false,
            }
        }
    }
}

fn main() {
    let mut trie = WildcardTrie::new();
    trie.insert("bad");
    trie.insert("dad");
    trie.insert("mad");

    assert!(!trie.search("pad"));
    assert!(trie.search("bad"));
    assert!(trie.search(".ad"));
    assert!(trie.search("b.."));
    assert!(!trie.search("b."));
    assert!(trie.search("..."));
    assert!(!trie.search("...."));

    trie.insert("a");
    assert!(trie.search("."));
    assert!(trie.search("a"));

    println!("wildcard trie: all tests passed");
}
```
</details>

### Exercise 2: Auto-Complete System

Build an autocomplete system that:
1. Accepts character-by-character input
2. After each character, returns the top-3 most frequently searched words with the current prefix
3. A `'#'` character marks the end of a sentence (add it to the corpus)

**Input:**
```
Initial corpus: [("i love you", 5), ("island", 3), ("ironman", 2), ("i love coding", 2)]
Input sequence: ['i', ' ', 'l', 'o', '#']
After 'i': ["i love you", "island", "i love coding"]  (frequencies: 5, 3, 2)
After ' ': ["i love you", "i love coding"]             (only "i " prefix)
After 'l': ["i love you", "i love coding"]             (only "i l" prefix)
After 'o': ["i love you", "i love coding"]             (only "i lo" prefix)
After '#': []                                           (sentence "i lo" saved)
```

<details>
<summary>Solution</summary>

```rust
use std::collections::HashMap;

#[derive(Default)]
struct AutoNode {
    children: HashMap<char, Box<AutoNode>>,
    /// Maps complete sentences passing through this node to their frequencies.
    sentences: HashMap<String, usize>,
}

struct AutoComplete {
    root: AutoNode,
    current_input: String,
    current_node_path: Vec<*const AutoNode>,
}

impl AutoComplete {
    fn new(corpus: &[(&str, usize)]) -> Self {
        let mut ac = Self {
            root: AutoNode::default(),
            current_input: String::new(),
            current_node_path: Vec::new(),
        };

        for &(sentence, freq) in corpus {
            ac.add_sentence(sentence, freq);
        }

        ac.current_node_path.push(&ac.root as *const AutoNode);
        ac
    }

    fn add_sentence(&mut self, sentence: &str, freq: usize) {
        let mut node = &mut self.root;
        for ch in sentence.chars() {
            node.sentences
                .entry(sentence.to_string())
                .and_modify(|f| *f += freq)
                .or_insert(freq);
            node = node
                .children
                .entry(ch)
                .or_insert_with(|| Box::new(AutoNode::default()));
        }
        node.sentences
            .entry(sentence.to_string())
            .and_modify(|f| *f += freq)
            .or_insert(freq);
    }

    /// Process one character. Returns top-3 suggestions.
    /// '#' terminates the current sentence.
    fn input(&mut self, ch: char) -> Vec<String> {
        if ch == '#' {
            let sentence = std::mem::take(&mut self.current_input);
            if !sentence.is_empty() {
                self.add_sentence(&sentence, 1);
            }
            self.current_node_path.clear();
            self.current_node_path.push(&self.root as *const AutoNode);
            return Vec::new();
        }

        self.current_input.push(ch);

        // Navigate to the next node
        let last_ptr = *self.current_node_path.last().unwrap();
        let last_node = unsafe { &*last_ptr };

        if let Some(child) = last_node.children.get(&ch) {
            let child_ptr = child.as_ref() as *const AutoNode;
            self.current_node_path.push(child_ptr);

            let child_node = unsafe { &*child_ptr };
            Self::top_3(&child_node.sentences)
        } else {
            // No matching prefix -- push a null marker
            self.current_node_path.push(std::ptr::null());
            Vec::new()
        }
    }

    fn top_3(sentences: &HashMap<String, usize>) -> Vec<String> {
        let mut entries: Vec<(&String, &usize)> = sentences.iter().collect();
        // Sort by frequency descending, then lexicographically ascending
        entries.sort_by(|a, b| b.1.cmp(a.1).then(a.0.cmp(b.0)));
        entries.into_iter().take(3).map(|(s, _)| s.clone()).collect()
    }
}

fn main() {
    let corpus = vec![
        ("i love you", 5),
        ("island", 3),
        ("ironman", 2),
        ("i love coding", 2),
    ];

    let mut ac = AutoComplete::new(&corpus);

    let r1 = ac.input('i');
    println!("after 'i': {:?}", r1);
    assert_eq!(r1.len(), 3);
    assert_eq!(r1[0], "i love you");

    let r2 = ac.input(' ');
    println!("after ' ': {:?}", r2);
    assert_eq!(r2.len(), 2);

    let r3 = ac.input('l');
    println!("after 'l': {:?}", r3);

    let r4 = ac.input('o');
    println!("after 'o': {:?}", r4);

    let r5 = ac.input('#');
    println!("after '#': {:?}", r5);
    assert!(r5.is_empty());

    println!("autocomplete system: all tests passed");
}
```
</details>

### Exercise 3: Maximum XOR Subarray

Given an array of integers, find the subarray `[l, r]` whose XOR (a[l] XOR a[l+1] XOR ... XOR a[r]) is maximized.

**Approach:** Compute prefix XOR: `px[i] = a[0] XOR a[1] XOR ... XOR a[i-1]`. Then `XOR(l, r) = px[r+1] XOR px[l]`. Finding the maximum XOR of any two prefix XOR values reduces to the maximum XOR pair problem, solvable with a XOR trie.

**Input:**
```
[1, 2, 3, 4, 5] -> maximum XOR subarray = 7
prefix XOR: [0, 1, 3, 0, 4, 1]
best pair: px[2]=3 XOR px[3]=0... no, px[3]=0 XOR px[5]=1...
Actually: 3 XOR 4 = 7 (subarray [3, 4] = {4, 5}, XOR = 4^5 = 1... no)
Let me recalculate: prefix_xor = [0, 1, 3, 0, 4, 1]
max pair: 0 XOR 7? No... 4 XOR 3 = 7. But 3 is at index 2, 4 is at index 4.
So subarray is a[2..=3] = [3, 4], XOR = 3^4 = 7. Correct!
```

<details>
<summary>Solution</summary>

```rust
struct XorTrie {
    nodes: Vec<[i32; 2]>,
}

impl XorTrie {
    fn new() -> Self {
        let mut trie = Self { nodes: Vec::new() };
        trie.new_node();
        trie
    }

    fn new_node(&mut self) -> usize {
        let id = self.nodes.len();
        self.nodes.push([-1, -1]);
        id
    }

    fn insert(&mut self, num: u64, bits: usize) {
        let mut cur = 0;
        for i in (0..bits).rev() {
            let bit = ((num >> i) & 1) as usize;
            if self.nodes[cur][bit] == -1 {
                let new_id = self.new_node();
                self.nodes[cur][bit] = new_id as i32;
            }
            cur = self.nodes[cur][bit] as usize;
        }
    }

    fn max_xor(&self, num: u64, bits: usize) -> u64 {
        let mut cur = 0;
        let mut result = 0u64;
        for i in (0..bits).rev() {
            let bit = ((num >> i) & 1) as usize;
            let opposite = 1 - bit;
            if self.nodes[cur][opposite] != -1 {
                result |= 1 << i;
                cur = self.nodes[cur][opposite] as usize;
            } else if self.nodes[cur][bit] != -1 {
                cur = self.nodes[cur][bit] as usize;
            } else {
                break;
            }
        }
        result
    }
}

fn max_xor_subarray(arr: &[u64]) -> u64 {
    // Compute prefix XOR
    let mut prefix_xor = vec![0u64; arr.len() + 1];
    for i in 0..arr.len() {
        prefix_xor[i + 1] = prefix_xor[i] ^ arr[i];
    }

    let max_val = *prefix_xor.iter().max().unwrap();
    let bits = if max_val == 0 { 1 } else { 64 - max_val.leading_zeros() as usize };

    let mut trie = XorTrie::new();
    let mut max_xor = 0u64;

    trie.insert(prefix_xor[0], bits);
    for i in 1..prefix_xor.len() {
        max_xor = max_xor.max(trie.max_xor(prefix_xor[i], bits));
        trie.insert(prefix_xor[i], bits);
    }

    max_xor
}

fn main() {
    assert_eq!(max_xor_subarray(&[1, 2, 3, 4, 5]), 7);
    assert_eq!(max_xor_subarray(&[8, 1, 2]), 11); // 8^1^2 = 11
    assert_eq!(max_xor_subarray(&[5, 5]), 5);     // single element 5
    assert_eq!(max_xor_subarray(&[0, 0, 0]), 0);

    println!("max XOR subarray: all tests passed");
}
```
</details>

### Exercise 4: Replace Words with Roots

Given a dictionary of root words and a sentence, replace every word in the sentence with its shortest root (prefix from the dictionary). If no root matches, keep the word unchanged.

**Input:**
```
dictionary = ["cat", "bat", "rat"]
sentence = "the cattle was rattled by the battery"
output = "the cat was rat by the bat"
```

**Input 2:**
```
dictionary = ["a", "b", "c"]
sentence = "aadsfasf absbs bbab cadsfabd"
output = "a a b c"
```

<details>
<summary>Solution</summary>

```rust
use std::collections::HashMap;

#[derive(Default)]
struct TrieNode {
    children: HashMap<char, Box<TrieNode>>,
    is_end: bool,
}

struct RootTrie {
    root: TrieNode,
}

impl RootTrie {
    fn new() -> Self {
        Self {
            root: TrieNode::default(),
        }
    }

    fn insert(&mut self, word: &str) {
        let mut node = &mut self.root;
        for ch in word.chars() {
            node = node.children.entry(ch).or_insert_with(|| Box::new(TrieNode::default()));
        }
        node.is_end = true;
    }

    /// Find the shortest prefix of `word` that exists in the trie.
    /// Returns the prefix if found, otherwise None.
    fn shortest_root<'a>(&self, word: &'a str) -> Option<&'a str> {
        let mut node = &self.root;
        for (i, ch) in word.char_indices() {
            match node.children.get(&ch) {
                Some(child) => {
                    node = child;
                    if node.is_end {
                        // Return up to and including this character
                        return Some(&word[..i + ch.len_utf8()]);
                    }
                }
                None => return None,
            }
        }
        if node.is_end {
            Some(word)
        } else {
            None
        }
    }
}

fn replace_words(dictionary: &[&str], sentence: &str) -> String {
    let mut trie = RootTrie::new();
    for root in dictionary {
        trie.insert(root);
    }

    sentence
        .split_whitespace()
        .map(|word| trie.shortest_root(word).unwrap_or(word))
        .collect::<Vec<_>>()
        .join(" ")
}

fn main() {
    assert_eq!(
        replace_words(&["cat", "bat", "rat"], "the cattle was rattled by the battery"),
        "the cat was rat by the bat"
    );

    assert_eq!(
        replace_words(&["a", "b", "c"], "aadsfasf absbs bbab cadsfabd"),
        "a a b c"
    );

    assert_eq!(
        replace_words(&["abc"], "xyz abc abcdef"),
        "xyz abc abc"
    );

    println!("replace words: all tests passed");
}
```
</details>

### Exercise 5: Count Distinct Substrings

Given a string, count the number of distinct substrings using a trie. Insert every suffix of the string into the trie; each new node created during insertion corresponds to a new distinct substring.

**Input:**
```
"abab" -> distinct substrings: "a", "ab", "aba", "abab", "b", "ba", "bab" = 7
(plus empty string if counted, but we skip it)
"aaa" -> "a", "aa", "aaa" = 3
```

<details>
<summary>Solution</summary>

```rust
const ALPHA: usize = 26;

struct SuffixTrie {
    nodes: Vec<[i32; ALPHA]>,
    node_count: usize,
}

impl SuffixTrie {
    fn new() -> Self {
        let mut trie = Self {
            nodes: Vec::new(),
            node_count: 0,
        };
        trie.new_node();
        trie
    }

    fn new_node(&mut self) -> usize {
        let id = self.nodes.len();
        self.nodes.push([-1; ALPHA]);
        self.node_count += 1;
        id
    }

    /// Insert a string and return how many new nodes were created.
    fn insert(&mut self, s: &str) -> usize {
        let mut cur = 0;
        let mut new_nodes = 0;
        for ch in s.bytes() {
            let idx = (ch - b'a') as usize;
            if self.nodes[cur][idx] == -1 {
                let new_id = self.new_node();
                self.nodes[cur][idx] = new_id as i32;
                new_nodes += 1;
            }
            cur = self.nodes[cur][idx] as usize;
        }
        new_nodes
    }
}

fn count_distinct_substrings(s: &str) -> usize {
    let mut trie = SuffixTrie::new();
    let mut count = 0;

    // Insert every suffix
    for i in 0..s.len() {
        count += trie.insert(&s[i..]);
    }

    count
}

fn main() {
    assert_eq!(count_distinct_substrings("abab"), 7);
    assert_eq!(count_distinct_substrings("aaa"), 3);
    assert_eq!(count_distinct_substrings("abc"), 6); // a, ab, abc, b, bc, c
    assert_eq!(count_distinct_substrings("a"), 1);

    println!("count distinct substrings: all tests passed");
}
```
</details>

---

## Rust Ownership Insights

### Why Tree Structures Are Hard in Rust

Tries expose a core tension in Rust's ownership model. When you write:

```rust
let mut node = &mut self.root;
for ch in word.chars() {
    node = node.children.entry(ch).or_insert_with(|| Box::new(TrieNode::default()));
}
```

The borrow checker must verify that each loop iteration does not create overlapping mutable borrows. This works because `HashMap::entry()` returns a mutable reference to the value, and the previous `node` reference is overwritten (not kept alive).

If you tried to keep a parent reference while also borrowing the child, the compiler would reject it:

```rust
// This does NOT compile:
let parent = &mut self.root;
let child = parent.children.get_mut(&'a').unwrap();
parent.some_field = 42; // ERROR: parent is still borrowed by child
```

This is why the arena-based approach (using indices into a Vec) is popular in competitive programming -- integer indices do not participate in the borrow checker.

### The `std::array::from_fn` Pattern

Initializing arrays of non-Copy types requires `std::array::from_fn`:

```rust
// Cannot do: [None::<Box<Node>>; 26]  -- Box is not Copy
// Instead:
let children: [Option<Box<Node>>; 26] = std::array::from_fn(|_| None);
```

This creates each element independently with a closure, avoiding the Copy requirement.

---

## Common Mistakes

1. **Forgetting to handle empty strings.** An empty string matches the root node. If `root.is_end` is not set, `search("")` returns false, which may or may not be the desired behavior.

2. **Memory leaks with HashMap-based tries.** Inserting millions of strings creates deep node chains. Unlike the arena approach, each node is a separate heap allocation, leading to fragmentation. For large datasets, prefer the arena approach.

3. **Using `char` instead of `u8` for ASCII-only problems.** Rust's `char` is 4 bytes (Unicode scalar value). If you know the input is ASCII, iterate with `.bytes()` instead of `.chars()` for better performance.

4. **Mutable traversal in delete.** Recursive deletion requires passing `&mut` down and cleaning up on the way back. Iterative deletion is possible but awkward because you need to re-traverse to find nodes to remove.

5. **Not handling duplicate insertions.** If `insert("apple")` is called twice, `word_count` must be incremented, and `delete` should only remove one instance. Without `word_count`, deletion after duplicate insertion corrupts the trie.

---

## Verification

```bash
cargo new trie-lab && cd trie-lab
```

Paste any solution into `src/main.rs` and run:

```bash
cargo run
```

Performance test for the arena trie:

```rust
fn bench_arena_trie() {
    use std::time::Instant;
    let mut trie = ArenaTrie::new();

    let start = Instant::now();
    for i in 0..100_000 {
        let word = format!("word{:06}", i);
        trie.insert(&word);
    }
    println!("100K inserts: {:?}", start.elapsed());
    println!("nodes: {}", trie.nodes.len());

    let start = Instant::now();
    let mut found = 0;
    for i in 0..100_000 {
        let word = format!("word{:06}", i);
        if trie.search(&word) { found += 1; }
    }
    println!("100K searches: {:?} (found {found})", start.elapsed());
}
```

---

## What You Learned

- A Trie stores strings in a tree where each edge is a character and shared prefixes share nodes, enabling O(L) insert, search, and prefix queries independent of dataset size.
- The HashMap-based approach handles arbitrary character sets but has higher per-node overhead; the array-based approach is faster for fixed alphabets; the arena-based approach eliminates Box overhead and borrow checker friction entirely.
- Rust's ownership model makes mutable tree traversal non-trivial: the `entry()` API and index-based arenas are the two main strategies for ergonomic mutable trie operations.
- The XOR trie is a powerful technique for bitwise optimization, enabling O(n * B) maximum XOR pair queries by greedily choosing opposite bits at each tree level.
- Tries enable algorithms that hash maps cannot: longest common prefix, autocomplete with ranking, wildcard search, and counting distinct substrings.

## Resources

- [Trie on CP-Algorithms](https://cp-algorithms.com/string/aho_corasick.html)
- [LeetCode: Implement Trie](https://leetcode.com/problems/implement-trie-prefix-tree/)
- [XOR Trie Tutorial (Codeforces)](https://codeforces.com/blog/entry/64717)
- [Rust Ownership and Trees](https://rust-unofficial.github.io/too-many-linked-lists/)
