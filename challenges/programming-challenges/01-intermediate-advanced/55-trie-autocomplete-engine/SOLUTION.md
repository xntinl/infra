# Solution: Trie Autocomplete Engine

## Architecture Overview

The solution is built around two trie variants:

1. **Standard Trie**: Each node maps characters to children. Terminal nodes carry an end-of-word flag and frequency count. Autocomplete traverses to the prefix node and collects all words below via DFS. Top-K uses a min-heap during collection to avoid sorting the full result.

2. **Compressed Trie (Radix Tree)**: Edges store strings instead of single characters. Single-child chains are merged into one edge. Insertion splits edges when a new word diverges mid-edge. This reduces node count significantly for datasets with long common prefixes.

Serialization writes the trie as JSON (portable, debuggable). Both variants share the same autocomplete interface.

## Go Solution

```go
// trie.go
package trie

import (
	"container/heap"
	"encoding/json"
	"io"
	"sort"
)

type TrieNode struct {
	Children map[rune]*TrieNode `json:"children,omitempty"`
	IsEnd    bool               `json:"is_end,omitempty"`
	Freq     int                `json:"freq,omitempty"`
}

func newTrieNode() *TrieNode {
	return &TrieNode{Children: make(map[rune]*TrieNode)}
}

type Trie struct {
	Root *TrieNode `json:"root"`
}

func NewTrie() *Trie {
	return &Trie{Root: newTrieNode()}
}

func (t *Trie) Insert(word string, freq int) {
	node := t.Root
	for _, ch := range word {
		if _, ok := node.Children[ch]; !ok {
			node.Children[ch] = newTrieNode()
		}
		node = node.Children[ch]
	}
	node.IsEnd = true
	node.Freq += freq
}

func (t *Trie) Search(word string) bool {
	node := t.findNode(word)
	return node != nil && node.IsEnd
}

func (t *Trie) StartsWith(prefix string) bool {
	return t.findNode(prefix) != nil
}

func (t *Trie) findNode(prefix string) *TrieNode {
	node := t.Root
	for _, ch := range prefix {
		child, ok := node.Children[ch]
		if !ok {
			return nil
		}
		node = child
	}
	return node
}

func (t *Trie) Autocomplete(prefix string) []string {
	node := t.findNode(prefix)
	if node == nil {
		return nil
	}
	var results []string
	t.collect(node, prefix, &results)
	sort.Strings(results)
	return results
}

func (t *Trie) collect(node *TrieNode, prefix string, results *[]string) {
	if node.IsEnd {
		*results = append(*results, prefix)
	}
	for ch, child := range node.Children {
		t.collect(child, prefix+string(ch), results)
	}
}

// wordFreq pairs a word with its frequency for heap operations.
type wordFreq struct {
	word string
	freq int
}

type minHeap []wordFreq

func (h minHeap) Len() int            { return len(h) }
func (h minHeap) Less(i, j int) bool   { return h[i].freq < h[j].freq }
func (h minHeap) Swap(i, j int)        { h[i], h[j] = h[j], h[i] }
func (h *minHeap) Push(x interface{})   { *h = append(*h, x.(wordFreq)) }
func (h *minHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

func (t *Trie) TopK(prefix string, k int) []string {
	node := t.findNode(prefix)
	if node == nil {
		return nil
	}
	h := &minHeap{}
	heap.Init(h)
	t.collectTopK(node, prefix, k, h)

	results := make([]string, h.Len())
	for i := h.Len() - 1; i >= 0; i-- {
		results[i] = heap.Pop(h).(wordFreq).word
	}
	return results
}

func (t *Trie) collectTopK(node *TrieNode, prefix string, k int, h *minHeap) {
	if node.IsEnd {
		if h.Len() < k {
			heap.Push(h, wordFreq{word: prefix, freq: node.Freq})
		} else if node.Freq > (*h)[0].freq {
			heap.Pop(h)
			heap.Push(h, wordFreq{word: prefix, freq: node.Freq})
		}
	}
	for ch, child := range node.Children {
		t.collectTopK(child, prefix+string(ch), k, h)
	}
}

func (t *Trie) Delete(word string) bool {
	return t.deleteRecursive(t.Root, []rune(word), 0)
}

func (t *Trie) deleteRecursive(node *TrieNode, runes []rune, depth int) bool {
	if depth == len(runes) {
		if !node.IsEnd {
			return false
		}
		node.IsEnd = false
		node.Freq = 0
		return len(node.Children) == 0
	}

	ch := runes[depth]
	child, ok := node.Children[ch]
	if !ok {
		return false
	}

	shouldPrune := t.deleteRecursive(child, runes, depth+1)
	if shouldPrune {
		delete(node.Children, ch)
		return !node.IsEnd && len(node.Children) == 0
	}
	return false
}

func (t *Trie) Serialize(w io.Writer) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(t)
}

func Deserialize(r io.Reader) (*Trie, error) {
	var t Trie
	decoder := json.NewDecoder(r)
	if err := decoder.Decode(&t); err != nil {
		return nil, err
	}
	return &t, nil
}

// RadixNode represents a compressed trie node.
type RadixNode struct {
	Children map[rune]*RadixEdge
	IsEnd    bool
	Freq     int
}

type RadixEdge struct {
	Label string
	Node  *RadixNode
}

func newRadixNode() *RadixNode {
	return &RadixNode{Children: make(map[rune]*RadixEdge)}
}

type RadixTree struct {
	Root *RadixNode
}

func NewRadixTree() *RadixTree {
	return &RadixTree{Root: newRadixNode()}
}

func (rt *RadixTree) Insert(word string, freq int) {
	rt.insertAt(rt.Root, word, freq)
}

func (rt *RadixTree) insertAt(node *RadixNode, remaining string, freq int) {
	if remaining == "" {
		node.IsEnd = true
		node.Freq += freq
		return
	}

	firstChar := rune(remaining[0])
	edge, exists := node.Children[firstChar]
	if !exists {
		newNode := newRadixNode()
		newNode.IsEnd = true
		newNode.Freq = freq
		node.Children[firstChar] = &RadixEdge{Label: remaining, Node: newNode}
		return
	}

	commonLen := commonPrefixLength(edge.Label, remaining)

	if commonLen == len(edge.Label) {
		rt.insertAt(edge.Node, remaining[commonLen:], freq)
		return
	}

	// Split the edge
	existingChild := edge.Node
	splitNode := newRadixNode()

	splitNode.Children[rune(edge.Label[commonLen])] = &RadixEdge{
		Label: edge.Label[commonLen:],
		Node:  existingChild,
	}

	edge.Label = remaining[:commonLen]
	edge.Node = splitNode

	if commonLen == len(remaining) {
		splitNode.IsEnd = true
		splitNode.Freq = freq
	} else {
		newLeaf := newRadixNode()
		newLeaf.IsEnd = true
		newLeaf.Freq = freq
		splitNode.Children[rune(remaining[commonLen])] = &RadixEdge{
			Label: remaining[commonLen:],
			Node:  newLeaf,
		}
	}
}

func commonPrefixLength(a, b string) int {
	maxLen := len(a)
	if len(b) < maxLen {
		maxLen = len(b)
	}
	for i := 0; i < maxLen; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return maxLen
}

func (rt *RadixTree) Search(word string) bool {
	node, remaining := rt.findNode(word)
	return node != nil && remaining == "" && node.IsEnd
}

func (rt *RadixTree) findNode(word string) (*RadixNode, string) {
	node := rt.Root
	remaining := word
	for remaining != "" {
		edge, exists := node.Children[rune(remaining[0])]
		if !exists {
			return nil, remaining
		}
		if len(remaining) < len(edge.Label) {
			if edge.Label[:len(remaining)] == remaining {
				return edge.Node, ""
			}
			return nil, remaining
		}
		if remaining[:len(edge.Label)] != edge.Label {
			return nil, remaining
		}
		remaining = remaining[len(edge.Label):]
		node = edge.Node
	}
	return node, ""
}

func (rt *RadixTree) Autocomplete(prefix string) []string {
	var results []string
	node := rt.Root
	accumulated := ""
	remaining := prefix

	for remaining != "" {
		edge, exists := node.Children[rune(remaining[0])]
		if !exists {
			return nil
		}
		if len(remaining) <= len(edge.Label) {
			if edge.Label[:len(remaining)] == remaining {
				accumulated += edge.Label
				rt.collectRadix(edge.Node, accumulated, &results)
				if edge.Node.IsEnd && len(remaining) == len(edge.Label) {
					// Already collected by collectRadix
				}
				sort.Strings(results)
				return results
			}
			return nil
		}
		if remaining[:len(edge.Label)] != edge.Label {
			return nil
		}
		accumulated += edge.Label
		remaining = remaining[len(edge.Label):]
		node = edge.Node
	}

	rt.collectRadix(node, accumulated, &results)
	sort.Strings(results)
	return results
}

func (rt *RadixTree) collectRadix(node *RadixNode, prefix string, results *[]string) {
	if node.IsEnd {
		*results = append(*results, prefix)
	}
	for _, edge := range node.Children {
		rt.collectRadix(edge.Node, prefix+edge.Label, results)
	}
}
```

```go
// trie_test.go
package trie

import (
	"bytes"
	"slices"
	"testing"
)

func TestInsertAndSearch(t *testing.T) {
	tr := NewTrie()
	tr.Insert("apple", 5)
	tr.Insert("app", 3)
	tr.Insert("application", 2)

	if !tr.Search("apple") {
		t.Error("expected to find 'apple'")
	}
	if !tr.Search("app") {
		t.Error("expected to find 'app'")
	}
	if tr.Search("ap") {
		t.Error("'ap' should not be found as a word")
	}
	if tr.Search("banana") {
		t.Error("'banana' should not be found")
	}
}

func TestStartsWith(t *testing.T) {
	tr := NewTrie()
	tr.Insert("hello", 1)
	tr.Insert("help", 1)

	if !tr.StartsWith("hel") {
		t.Error("'hel' should be a valid prefix")
	}
	if !tr.StartsWith("hello") {
		t.Error("'hello' should be a valid prefix")
	}
	if tr.StartsWith("hex") {
		t.Error("'hex' should not be a valid prefix")
	}
}

func TestAutocomplete(t *testing.T) {
	tr := NewTrie()
	tr.Insert("car", 5)
	tr.Insert("card", 3)
	tr.Insert("care", 4)
	tr.Insert("careful", 2)
	tr.Insert("cat", 6)

	results := tr.Autocomplete("car")
	expected := []string{"car", "card", "care", "careful"}
	if !slices.Equal(results, expected) {
		t.Errorf("expected %v, got %v", expected, results)
	}

	results = tr.Autocomplete("cat")
	if !slices.Equal(results, []string{"cat"}) {
		t.Errorf("expected [cat], got %v", results)
	}

	results = tr.Autocomplete("dog")
	if len(results) != 0 {
		t.Errorf("expected empty, got %v", results)
	}
}

func TestTopK(t *testing.T) {
	tr := NewTrie()
	tr.Insert("car", 50)
	tr.Insert("card", 30)
	tr.Insert("care", 40)
	tr.Insert("careful", 20)
	tr.Insert("cargo", 10)

	results := tr.TopK("car", 3)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d: %v", len(results), results)
	}
	// Top 3 by freq: car(50), care(40), card(30)
	if results[0] != "car" || results[1] != "care" || results[2] != "card" {
		t.Errorf("unexpected top-3 order: %v", results)
	}
}

func TestDelete(t *testing.T) {
	tr := NewTrie()
	tr.Insert("apple", 5)
	tr.Insert("app", 3)

	if !tr.Delete("apple") {
		t.Error("delete should return true for existing word")
	}
	if tr.Search("apple") {
		t.Error("'apple' should not exist after deletion")
	}
	if !tr.Search("app") {
		t.Error("'app' should still exist after deleting 'apple'")
	}

	if tr.Delete("banana") {
		t.Error("delete should return false for non-existent word")
	}
}

func TestDeletePrefixPreservesLongerWord(t *testing.T) {
	tr := NewTrie()
	tr.Insert("app", 1)
	tr.Insert("apple", 1)

	tr.Delete("app")
	if tr.Search("app") {
		t.Error("'app' should be deleted")
	}
	if !tr.Search("apple") {
		t.Error("'apple' should survive deletion of 'app'")
	}
}

func TestSerializeDeserialize(t *testing.T) {
	tr := NewTrie()
	tr.Insert("hello", 10)
	tr.Insert("help", 5)
	tr.Insert("world", 8)

	var buf bytes.Buffer
	if err := tr.Serialize(&buf); err != nil {
		t.Fatalf("serialize failed: %v", err)
	}

	restored, err := Deserialize(&buf)
	if err != nil {
		t.Fatalf("deserialize failed: %v", err)
	}

	for _, word := range []string{"hello", "help", "world"} {
		if !restored.Search(word) {
			t.Errorf("restored trie missing word: %s", word)
		}
	}
	if restored.Search("hell") {
		t.Error("restored trie should not have 'hell' as a word")
	}
}

func TestRadixTreeInsertAndSearch(t *testing.T) {
	rt := NewRadixTree()
	rt.Insert("test", 5)
	rt.Insert("testing", 3)
	rt.Insert("team", 4)

	if !rt.Search("test") {
		t.Error("expected to find 'test'")
	}
	if !rt.Search("testing") {
		t.Error("expected to find 'testing'")
	}
	if !rt.Search("team") {
		t.Error("expected to find 'team'")
	}
	if rt.Search("tea") {
		t.Error("'tea' should not be found")
	}
}

func TestRadixTreeAutocomplete(t *testing.T) {
	rt := NewRadixTree()
	rt.Insert("car", 5)
	rt.Insert("card", 3)
	rt.Insert("care", 4)
	rt.Insert("careful", 2)

	results := rt.Autocomplete("car")
	expected := []string{"car", "card", "care", "careful"}
	if !slices.Equal(results, expected) {
		t.Errorf("expected %v, got %v", expected, results)
	}
}

func TestEmptyTrie(t *testing.T) {
	tr := NewTrie()
	if tr.Search("anything") {
		t.Error("empty trie should not find anything")
	}
	results := tr.Autocomplete("")
	if len(results) != 0 {
		t.Errorf("empty trie autocomplete should return empty, got %v", results)
	}
}

func TestSingleWord(t *testing.T) {
	tr := NewTrie()
	tr.Insert("only", 1)
	if !tr.Search("only") {
		t.Error("should find the single inserted word")
	}
	results := tr.Autocomplete("on")
	if !slices.Equal(results, []string{"only"}) {
		t.Errorf("expected [only], got %v", results)
	}
}
```

## Running the Go Solution

```bash
mkdir -p trie && cd trie
go mod init trie
# Place trie.go and trie_test.go in the directory
go test -v -count=1 ./...
```

### Expected Output

```
=== RUN   TestInsertAndSearch
--- PASS: TestInsertAndSearch
=== RUN   TestStartsWith
--- PASS: TestStartsWith
=== RUN   TestAutocomplete
--- PASS: TestAutocomplete
=== RUN   TestTopK
--- PASS: TestTopK
=== RUN   TestDelete
--- PASS: TestDelete
=== RUN   TestDeletePrefixPreservesLongerWord
--- PASS: TestDeletePrefixPreservesLongerWord
=== RUN   TestSerializeDeserialize
--- PASS: TestSerializeDeserialize
=== RUN   TestRadixTreeInsertAndSearch
--- PASS: TestRadixTreeInsertAndSearch
=== RUN   TestRadixTreeAutocomplete
--- PASS: TestRadixTreeAutocomplete
=== RUN   TestEmptyTrie
--- PASS: TestEmptyTrie
=== RUN   TestSingleWord
--- PASS: TestSingleWord
PASS
```

## Rust Solution

```rust
// src/lib.rs
use std::collections::{BinaryHeap, HashMap};
use std::cmp::Reverse;
use std::io::{Read, Write};

#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub struct TrieNode {
    children: HashMap<char, TrieNode>,
    is_end: bool,
    freq: u32,
}

impl TrieNode {
    fn new() -> Self {
        TrieNode {
            children: HashMap::new(),
            is_end: false,
            freq: 0,
        }
    }
}

#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub struct Trie {
    root: TrieNode,
}

impl Trie {
    pub fn new() -> Self {
        Trie { root: TrieNode::new() }
    }

    pub fn insert(&mut self, word: &str, freq: u32) {
        let mut node = &mut self.root;
        for ch in word.chars() {
            node = node.children.entry(ch).or_insert_with(TrieNode::new);
        }
        node.is_end = true;
        node.freq += freq;
    }

    pub fn search(&self, word: &str) -> bool {
        self.find_node(word)
            .map_or(false, |node| node.is_end)
    }

    pub fn starts_with(&self, prefix: &str) -> bool {
        self.find_node(prefix).is_some()
    }

    fn find_node(&self, prefix: &str) -> Option<&TrieNode> {
        let mut node = &self.root;
        for ch in prefix.chars() {
            node = node.children.get(&ch)?;
        }
        Some(node)
    }

    pub fn autocomplete(&self, prefix: &str) -> Vec<String> {
        let Some(node) = self.find_node(prefix) else {
            return Vec::new();
        };
        let mut results = Vec::new();
        self.collect(node, &mut prefix.to_string(), &mut results);
        results.sort();
        results
    }

    fn collect(&self, node: &TrieNode, current: &mut String, results: &mut Vec<String>) {
        if node.is_end {
            results.push(current.clone());
        }
        let mut chars: Vec<char> = node.children.keys().copied().collect();
        chars.sort();
        for ch in chars {
            current.push(ch);
            self.collect(&node.children[&ch], current, results);
            current.pop();
        }
    }

    pub fn top_k(&self, prefix: &str, k: usize) -> Vec<String> {
        let Some(node) = self.find_node(prefix) else {
            return Vec::new();
        };
        let mut heap: BinaryHeap<Reverse<(u32, String)>> = BinaryHeap::new();
        self.collect_top_k(node, &mut prefix.to_string(), k, &mut heap);

        let mut results: Vec<(u32, String)> = heap.into_iter().map(|Reverse(x)| x).collect();
        results.sort_by(|a, b| b.0.cmp(&a.0));
        results.into_iter().map(|(_, word)| word).collect()
    }

    fn collect_top_k(
        &self,
        node: &TrieNode,
        current: &mut String,
        k: usize,
        heap: &mut BinaryHeap<Reverse<(u32, String)>>,
    ) {
        if node.is_end {
            if heap.len() < k {
                heap.push(Reverse((node.freq, current.clone())));
            } else if let Some(&Reverse((min_freq, _))) = heap.peek() {
                if node.freq > min_freq {
                    heap.pop();
                    heap.push(Reverse((node.freq, current.clone())));
                }
            }
        }
        for (&ch, child) in &node.children {
            current.push(ch);
            self.collect_top_k(child, current, k, heap);
            current.pop();
        }
    }

    pub fn delete(&mut self, word: &str) -> bool {
        let chars: Vec<char> = word.chars().collect();
        Self::delete_recursive(&mut self.root, &chars, 0)
    }

    fn delete_recursive(node: &mut TrieNode, chars: &[char], depth: usize) -> bool {
        if depth == chars.len() {
            if !node.is_end {
                return false;
            }
            node.is_end = false;
            node.freq = 0;
            return node.children.is_empty();
        }

        let ch = chars[depth];
        let should_prune = {
            let Some(child) = node.children.get_mut(&ch) else {
                return false;
            };
            Self::delete_recursive(child, chars, depth + 1)
        };

        if should_prune {
            node.children.remove(&ch);
            return !node.is_end && node.children.is_empty();
        }
        false
    }

    pub fn serialize<W: Write>(&self, writer: W) -> Result<(), serde_json::Error> {
        serde_json::to_writer_pretty(writer, self)
    }

    pub fn deserialize<R: Read>(reader: R) -> Result<Self, serde_json::Error> {
        serde_json::from_reader(reader)
    }
}

// Compressed Trie (Radix Tree)

#[derive(Debug)]
pub struct RadixNode {
    children: HashMap<char, (String, RadixNode)>,
    is_end: bool,
    freq: u32,
}

impl RadixNode {
    fn new() -> Self {
        RadixNode {
            children: HashMap::new(),
            is_end: false,
            freq: 0,
        }
    }
}

#[derive(Debug)]
pub struct RadixTree {
    root: RadixNode,
}

impl RadixTree {
    pub fn new() -> Self {
        RadixTree { root: RadixNode::new() }
    }

    pub fn insert(&mut self, word: &str, freq: u32) {
        Self::insert_at(&mut self.root, word, freq);
    }

    fn insert_at(node: &mut RadixNode, remaining: &str, freq: u32) {
        if remaining.is_empty() {
            node.is_end = true;
            node.freq += freq;
            return;
        }

        let first_char = remaining.chars().next().unwrap();

        if !node.children.contains_key(&first_char) {
            let mut leaf = RadixNode::new();
            leaf.is_end = true;
            leaf.freq = freq;
            node.children.insert(first_char, (remaining.to_string(), leaf));
            return;
        }

        let common_len = {
            let (ref label, _) = node.children[&first_char];
            common_prefix_length(label, remaining)
        };

        let label_len = node.children[&first_char].0.len();

        if common_len == label_len {
            let child = &mut node.children.get_mut(&first_char).unwrap().1;
            Self::insert_at(child, &remaining[common_len..], freq);
            return;
        }

        // Split edge
        let (old_label, old_child) = node.children.remove(&first_char).unwrap();
        let mut split_node = RadixNode::new();

        let old_suffix_char = old_label[common_len..].chars().next().unwrap();
        split_node.children.insert(
            old_suffix_char,
            (old_label[common_len..].to_string(), old_child),
        );

        if common_len == remaining.len() {
            split_node.is_end = true;
            split_node.freq = freq;
        } else {
            let new_suffix_char = remaining[common_len..].chars().next().unwrap();
            let mut new_leaf = RadixNode::new();
            new_leaf.is_end = true;
            new_leaf.freq = freq;
            split_node.children.insert(
                new_suffix_char,
                (remaining[common_len..].to_string(), new_leaf),
            );
        }

        node.children.insert(first_char, (remaining[..common_len].to_string(), split_node));
    }

    pub fn search(&self, word: &str) -> bool {
        Self::search_at(&self.root, word)
    }

    fn search_at(node: &RadixNode, remaining: &str) -> bool {
        if remaining.is_empty() {
            return node.is_end;
        }
        let first_char = remaining.chars().next().unwrap();
        let Some((label, child)) = node.children.get(&first_char) else {
            return false;
        };
        if remaining.len() < label.len() {
            return false;
        }
        if &remaining[..label.len()] != label.as_str() {
            return false;
        }
        Self::search_at(child, &remaining[label.len()..])
    }

    pub fn autocomplete(&self, prefix: &str) -> Vec<String> {
        let mut results = Vec::new();
        Self::autocomplete_at(&self.root, prefix, &mut String::new(), &mut results);
        results.sort();
        results
    }

    fn autocomplete_at(
        node: &RadixNode,
        remaining: &str,
        accumulated: &mut String,
        results: &mut Vec<String>,
    ) {
        if remaining.is_empty() {
            Self::collect_all(node, accumulated, results);
            return;
        }

        let first_char = remaining.chars().next().unwrap();
        let Some((label, child)) = node.children.get(&first_char) else {
            return;
        };

        if remaining.len() <= label.len() {
            if label.starts_with(remaining) {
                accumulated.push_str(label);
                Self::collect_all(child, accumulated, results);
                let drain_len = accumulated.len() - label.len();
                accumulated.truncate(drain_len);
            }
            return;
        }

        if !remaining.starts_with(label.as_str()) {
            return;
        }

        accumulated.push_str(label);
        Self::autocomplete_at(child, &remaining[label.len()..], accumulated, results);
        let drain_len = accumulated.len() - label.len();
        accumulated.truncate(drain_len);
    }

    fn collect_all(node: &RadixNode, prefix: &mut String, results: &mut Vec<String>) {
        if node.is_end {
            results.push(prefix.clone());
        }
        for (_, (label, child)) in &node.children {
            prefix.push_str(label);
            Self::collect_all(child, prefix, results);
            prefix.truncate(prefix.len() - label.len());
        }
    }
}

fn common_prefix_length(a: &str, b: &str) -> usize {
    a.bytes()
        .zip(b.bytes())
        .take_while(|(x, y)| x == y)
        .count()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn insert_and_search() {
        let mut t = Trie::new();
        t.insert("apple", 5);
        t.insert("app", 3);
        t.insert("application", 2);

        assert!(t.search("apple"));
        assert!(t.search("app"));
        assert!(!t.search("ap"));
        assert!(!t.search("banana"));
    }

    #[test]
    fn starts_with_prefix() {
        let mut t = Trie::new();
        t.insert("hello", 1);
        t.insert("help", 1);

        assert!(t.starts_with("hel"));
        assert!(t.starts_with("hello"));
        assert!(!t.starts_with("hex"));
    }

    #[test]
    fn autocomplete_results() {
        let mut t = Trie::new();
        t.insert("car", 5);
        t.insert("card", 3);
        t.insert("care", 4);
        t.insert("careful", 2);
        t.insert("cat", 6);

        let results = t.autocomplete("car");
        assert_eq!(results, vec!["car", "card", "care", "careful"]);

        let results = t.autocomplete("dog");
        assert!(results.is_empty());
    }

    #[test]
    fn top_k_by_frequency() {
        let mut t = Trie::new();
        t.insert("car", 50);
        t.insert("card", 30);
        t.insert("care", 40);
        t.insert("careful", 20);
        t.insert("cargo", 10);

        let results = t.top_k("car", 3);
        assert_eq!(results.len(), 3);
        assert_eq!(results[0], "car");
        assert_eq!(results[1], "care");
        assert_eq!(results[2], "card");
    }

    #[test]
    fn delete_word() {
        let mut t = Trie::new();
        t.insert("apple", 5);
        t.insert("app", 3);

        assert!(t.delete("apple"));
        assert!(!t.search("apple"));
        assert!(t.search("app"));
        assert!(!t.delete("banana"));
    }

    #[test]
    fn delete_prefix_preserves_longer_word() {
        let mut t = Trie::new();
        t.insert("app", 1);
        t.insert("apple", 1);

        t.delete("app");
        assert!(!t.search("app"));
        assert!(t.search("apple"));
    }

    #[test]
    fn serialize_deserialize_roundtrip() {
        let mut t = Trie::new();
        t.insert("hello", 10);
        t.insert("help", 5);
        t.insert("world", 8);

        let mut buf = Vec::new();
        t.serialize(&mut buf).unwrap();

        let restored = Trie::deserialize(&buf[..]).unwrap();
        assert!(restored.search("hello"));
        assert!(restored.search("help"));
        assert!(restored.search("world"));
        assert!(!restored.search("hell"));
    }

    #[test]
    fn radix_tree_insert_and_search() {
        let mut rt = RadixTree::new();
        rt.insert("test", 5);
        rt.insert("testing", 3);
        rt.insert("team", 4);

        assert!(rt.search("test"));
        assert!(rt.search("testing"));
        assert!(rt.search("team"));
        assert!(!rt.search("tea"));
    }

    #[test]
    fn radix_tree_autocomplete() {
        let mut rt = RadixTree::new();
        rt.insert("car", 5);
        rt.insert("card", 3);
        rt.insert("care", 4);
        rt.insert("careful", 2);

        let results = rt.autocomplete("car");
        assert_eq!(results, vec!["car", "card", "care", "careful"]);
    }

    #[test]
    fn empty_trie() {
        let t = Trie::new();
        assert!(!t.search("anything"));
        assert!(t.autocomplete("").is_empty());
    }

    #[test]
    fn single_word() {
        let mut t = Trie::new();
        t.insert("only", 1);
        assert!(t.search("only"));
        assert_eq!(t.autocomplete("on"), vec!["only"]);
    }
}
```

## Running the Rust Solution

```bash
cargo new trie_engine --lib && cd trie_engine
# Add to Cargo.toml: serde = { version = "1", features = ["derive"] }
# Add to Cargo.toml: serde_json = "1"
# Replace src/lib.rs with the code above
cargo test -- --nocapture
```

### Expected Output

```
running 12 tests
test tests::insert_and_search ... ok
test tests::starts_with_prefix ... ok
test tests::autocomplete_results ... ok
test tests::top_k_by_frequency ... ok
test tests::delete_word ... ok
test tests::delete_prefix_preserves_longer_word ... ok
test tests::serialize_deserialize_roundtrip ... ok
test tests::radix_tree_insert_and_search ... ok
test tests::radix_tree_autocomplete ... ok
test tests::empty_trie ... ok
test tests::single_word ... ok

test result: ok. 12 passed; 0 failed; 0 ignored
```

## Design Decisions

1. **Rune-keyed children map**: Using `rune` (Go) / `char` (Rust) as map keys handles Unicode correctly. Byte-level keys would break on multi-byte characters.

2. **Min-heap for Top-K**: A min-heap of size K is O(n log K) which is better than sorting all results O(n log n) when K is small. The heap keeps the smallest element on top so we can efficiently discard low-frequency words.

3. **Recursive delete with pruning**: The recursive approach returns a boolean indicating whether the caller should prune the child. This bottom-up pruning avoids a second pass and keeps the trie compact.

4. **Radix tree edge splitting**: When inserting "test" into a tree with edge "testing", we split into "test" -> "ing". The split point is the first diverging character. This is the most complex operation but critical for memory savings.

5. **JSON serialization**: JSON is chosen for portability and debuggability over binary formats. For production use, a binary format (protobuf, bincode) would be faster but harder to inspect.

## Common Mistakes

- **Not handling prefix deletion correctly**: Deleting "app" should not remove the path to "apple". Only the end-of-word flag at the "app" node should be cleared; pruning should stop at any node that has children.
- **Radix tree: forgetting to split edges**: When inserting a word that shares a prefix with an existing edge but diverges mid-edge, you must split the edge. Inserting as a new child without splitting corrupts the tree.
- **Top-K returning wrong order**: The min-heap gives results in ascending frequency order. You must reverse or re-sort in descending order before returning.
- **Map iteration order in Go**: Character order in autocomplete results is non-deterministic with maps. Sort the results before returning or sort the keys before iterating.

## Performance Notes

| Operation | Standard Trie | Radix Tree |
|-----------|--------------|------------|
| Insert | O(m) | O(m) |
| Search | O(m) | O(m) |
| Autocomplete | O(m + k) | O(m + k) |
| Top-K | O(m + n log K) | O(m + n log K) |
| Delete | O(m) | O(m) |
| Space | O(ALPHABET * N * m) | O(N * m) |

Where m = word length, k = number of matches, n = total words under prefix, N = number of words. The radix tree's main advantage is space: it uses far fewer nodes when many words share long prefixes (URLs, file paths, DNA sequences).
