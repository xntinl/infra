# 55. Trie Autocomplete Engine

```yaml
difficulty: intermediate-advanced
languages: [go, rust]
time_estimate: 5-7 hours
tags: [trie, prefix-tree, radix-tree, autocomplete, string-algorithms, serialization]
bloom_level: [apply, analyze, create]
```

## Prerequisites

- Tree data structures: insertion, traversal, recursive node operations
- String manipulation: prefix matching, UTF-8 awareness
- Generic programming: Go generics (type parameters), Rust generics with trait bounds
- File I/O basics for serialization/deserialization
- Hash maps and frequency counting patterns

## Learning Objectives

After completing this challenge you will be able to:

- **Implement** a trie with insert, search, prefix enumeration, and delete operations
- **Design** a top-K suggestion system using frequency-weighted prefix traversal
- **Optimize** memory by compressing single-child chains into a radix tree (Patricia trie)
- **Analyze** time and space trade-offs between standard trie and compressed trie variants
- **Implement** serialization/deserialization for persistent trie storage

## The Challenge

Search engines, IDEs, and command-line tools rely on autocomplete to surface suggestions as users type. The backbone of this feature is the trie (prefix tree), a tree where each path from root to a marked node represents a stored word. Typing a prefix navigates to the corresponding node; all words below that node are valid completions.

Build a full-featured autocomplete engine backed by a trie. Support exact word insertion with frequency tracking, prefix-based retrieval of all matching words, top-K suggestions ranked by frequency, word deletion, and a compressed (radix tree) variant that merges single-child chains to reduce memory overhead. Include serialization so the trie can be saved to disk and reloaded.

## Requirements

1. Define a trie node that stores children keyed by character, a boolean end-of-word marker, and a frequency counter. The trie supports `Insert(word, frequency)`, `Search(word) -> bool`, and `StartsWith(prefix) -> bool`.

2. Implement `Autocomplete(prefix) -> []string` that returns all words sharing the given prefix. Traverse to the prefix node, then perform DFS to collect all words below it.

3. Implement `TopK(prefix, k) -> []string` that returns the top K words by frequency for a prefix. Use a min-heap of size K during traversal to avoid sorting the full result set.

4. Implement `Delete(word) -> bool` that removes a word. If the deleted node has no children, prune the branch upward. If it has children, only unmark end-of-word and reset frequency.

5. Implement a compressed trie (radix tree) variant where edges store strings instead of single characters. Insertion must handle splitting edges when a new word diverges mid-edge. Search and prefix enumeration must work identically to the standard trie.

6. Implement `Serialize(writer)` and `Deserialize(reader)` that write/read the trie to/from a binary or JSON format. The round-trip must preserve all words, frequencies, and structure.

7. Implement both Go and Rust versions with idiomatic patterns for each language.

## Hints

<details>
<summary>Hint 1: Trie node structure</summary>

Each node maps characters to children. The end-of-word flag marks complete words. Frequency is stored at terminal nodes to rank suggestions.

```go
type TrieNode struct {
    children map[rune]*TrieNode
    isEnd    bool
    freq     int
}
```

```rust
struct TrieNode {
    children: HashMap<char, TrieNode>,
    is_end: bool,
    freq: u32,
}
```

</details>

<details>
<summary>Hint 2: DFS collection for autocomplete</summary>

Navigate to the node matching the prefix. Then recursively collect all words below that node by appending each character along the path.

```go
func (t *Trie) collect(node *TrieNode, prefix string, results *[]string) {
    if node.isEnd {
        *results = append(*results, prefix)
    }
    for ch, child := range node.children {
        t.collect(child, prefix+string(ch), results)
    }
}
```

</details>

<details>
<summary>Hint 3: Top-K with min-heap</summary>

Maintain a min-heap of size K during DFS. When you find a terminal node, push it. If the heap exceeds K, pop the minimum. After traversal the heap contains the top K results.

```go
type wordFreq struct {
    word string
    freq int
}
// Use container/heap with a min-heap ordered by freq
```

</details>

<details>
<summary>Hint 4: Radix tree edge splitting</summary>

When inserting "test" into a radix tree that has edge "testing", split "testing" into "test" -> "ing". The split point is where the new word and existing edge diverge. Track edge labels as strings on the parent-to-child connection.

```rust
struct RadixNode {
    children: HashMap<char, (String, RadixNode)>, // (edge_label, child)
    is_end: bool,
    freq: u32,
}
```

</details>

<details>
<summary>Hint 5: Delete with branch pruning</summary>

After unmarking end-of-word, walk back up the tree. Any node that is not end-of-word and has zero children can be removed from its parent. Stop pruning when you reach a node that is end-of-word or has other children.

</details>

## Acceptance Criteria

- [ ] Insert words with frequency, search returns true only for inserted words
- [ ] `StartsWith` correctly distinguishes prefixes from non-prefixes
- [ ] `Autocomplete` returns all words matching a prefix (empty prefix returns all words)
- [ ] `TopK` returns exactly K results ordered by descending frequency
- [ ] `Delete` removes words and prunes empty branches
- [ ] Deleting a prefix of another word does not delete the longer word
- [ ] Compressed trie merges single-child chains and splits edges on divergence
- [ ] Compressed trie produces identical autocomplete results as the standard trie
- [ ] Serialize then deserialize produces an identical trie (all words, frequencies preserved)
- [ ] Both Go and Rust implementations pass equivalent test suites
- [ ] Empty trie and single-word trie edge cases handled correctly

## Resources

- [Trie - Wikipedia](https://en.wikipedia.org/wiki/Trie)
- [Radix Tree - Wikipedia](https://en.wikipedia.org/wiki/Radix_tree)
- [Autocomplete with Tries - Medium](https://medium.com/basecs/trying-to-understand-tries-3ec6bede0014)
- [Go container/heap](https://pkg.go.dev/container/heap) - Standard library heap interface
- [The Rust Programming Language: Collections](https://doc.rust-lang.org/book/ch08-00-common-collections.html)
- [Efficient Auto-Complete with Trie](https://leetcode.com/problems/implement-trie-prefix-tree/) - LeetCode problem for practice
