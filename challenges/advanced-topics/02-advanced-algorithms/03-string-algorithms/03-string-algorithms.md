<!--
type: reference
difficulty: advanced
section: [02-advanced-algorithms]
concepts: [suffix-array, sa-is, lcp-array, kasai-algorithm, aho-corasick, z-algorithm, suffix-automaton]
languages: [go, rust]
estimated_reading_time: 60-90 min
bloom_level: analyze
prerequisites: [string-fundamentals, arrays, hash-maps, finite-automata-basics]
papers: [nong-zhang-chan-2009-sa-is, kasai-2001-lcp, aho-corasick-1975, crochemore-2001-suffix-automaton]
industry_use: [elasticsearch, grep, bwa-genome-aligner, ripgrep, splunk, postgresql-pg-trgm]
language_contrast: high
-->

# String Algorithms

> Strings are the most common data type in production systems, yet most engineers reach only for regex — missing the 100×–1000× speedups that specialized string algorithms provide.

## Mental Model

Strings are not just arrays of characters. They have *algebraic structure*: substrings
can be indexed, shared suffixes can be sorted, pattern vocabularies can be compiled into
automata. The right data structure or algorithm turns a quadratic scan into a linear one.

The pattern-recognition triggers for each algorithm in this section:

- **Suffix array**: You need to search for many different substrings in a large fixed
  text, or you want to know the longest common substring of two strings, or you want to
  count distinct substrings. A suffix array is a sorted index of all O(n²) substrings
  built in O(n) time. Used in genome aligners (BWA, Bowtie) and full-text search.

- **LCP array**: The suffix array alone gives O(log n) substring search. The LCP array
  (longest common prefix between consecutive sorted suffixes) enables O(1) range
  minimum queries, turning many string problems from O(n log n) to O(n). Kasai's
  algorithm builds the LCP array in O(n) from the suffix array.

- **Aho-Corasick**: You have a dictionary of k patterns and a large text. You want to
  find all occurrences of all patterns in one pass. Aho-Corasick compiles the patterns
  into a finite automaton in O(sum of pattern lengths) and processes the text in O(n + matches).
  This is how `grep -F` works, how antivirus scanners work, and how Elasticsearch's
  multi-term matching works.

- **Z-algorithm**: You want to precompute, for each position i in a string, the length
  of the longest substring starting at i that is also a prefix of the string. Linear
  time, no hash collisions (unlike Rabin-Karp). Used for single-pattern search and for
  comparing strings against a pattern.

- **Suffix automaton (SAM)**: The minimal DFA accepting all suffixes of a string. It
  has O(n) states and transitions, yet represents O(n²) substrings. Used for distinct
  substring counting, finding the longest repeated substring, and online construction
  (add characters one by one).

## Core Concepts

### Suffix Array (SA-IS Algorithm)

A suffix array `SA[i]` gives the starting index of the i-th lexicographically smallest
suffix. Naive construction is O(n² log n) (sort n suffixes of average length n/2).
The DC3/Skew algorithm achieves O(n). SA-IS (Suffix Array by Induced Sorting) achieves
O(n) with better cache behavior.

SA-IS classifies each position as S-type (the suffix starting there is smaller than its
successor) or L-type (larger). A position is an LMS (Left-Most S-type) if it is S-type
and its predecessor is L-type. Induced sorting propagates the order from LMS suffixes
(which are sorted recursively) to all suffixes in two linear passes.

**Why it matters in production**: Genome aligners index a 3-billion-character genome
once and then answer billions of short-read alignment queries. The suffix array (or its
compressed variant, FM-index) is the foundation.

### LCP Array and Kasai's Algorithm

`LCP[i]` = length of the longest common prefix between `SA[i]` and `SA[i-1]`. Combined
with a range minimum query (RMQ) structure, it answers "what is the longest common
prefix of suffix SA[i] and suffix SA[j]?" in O(1) — enabling O(n) algorithms for
problems that naively need O(n²).

Kasai's insight: if you know `LCP[rank[i]]` (the LCP between suffix i and its
predecessor in sorted order), then `LCP[rank[i+1]] >= LCP[rank[i]] - 1`. This lets
you compute all n LCP values in a single left-to-right scan.

### Aho-Corasick Automaton

Build a trie from all patterns. Then add **failure links**: for each node, the failure
link points to the longest proper suffix of the current prefix that is also a prefix
of some pattern. These links let you, on mismatch, "fall back" to the best matching
prefix without re-reading the text.

The construction is a BFS from the root. Processing the text is a single scan: at
each character, follow the automaton transition (or failure link). Every match is
recorded by following output links (dictionary links) at each accepting state.

Time complexity: O(Σ|patterns| + |text| + total_matches). This is optimal — you
cannot do better without hashing (which trades correctness guarantees for speed).

### Z-Algorithm

`Z[i]` = length of the longest string starting at position i of string S that is
also a prefix of S. By definition, `Z[0]` is undefined (the entire string matches
itself). The algorithm maintains a window `[l, r]` that is the rightmost Z-box seen.
For each new position i, it uses previously computed Z values to avoid re-comparison:
if i is inside the current Z-box, `Z[i] >= min(Z[i-l], r-i+1)`, and we extend from
there.

Usage for pattern matching: concatenate `pattern + '$' + text`. A position in the text
part with `Z[i] = len(pattern)` is a match. O(n + m) total, no hash, no false positives.

### Suffix Automaton (SAM)

The SAM is built online: add one character at a time, O(1) amortized per character.
The key structure is the `link` (suffix link): it points to the state representing the
longest proper suffix of the current state that is accepted by a different set of
positions. The set of all substrings of string S corresponds to paths from the initial
state in the SAM — every path is a substring, every substring is a path.

Total states ≤ 2n-1, total transitions ≤ 3n-4. This linear bound means the SAM is
the most compact representation of all substrings.

## Implementation: Go

```go
package main

import "fmt"

// ─── Z-Algorithm ─────────────────────────────────────────────────────────────

func zFunction(s []byte) []int {
	n := len(s)
	z := make([]int, n)
	l, r := 0, 0
	for i := 1; i < n; i++ {
		if i < r {
			z[i] = min(r-i, z[i-l])
		}
		for i+z[i] < n && s[z[i]] == s[i+z[i]] {
			z[i]++
		}
		if i+z[i] > r {
			l, r = i, i+z[i]
		}
	}
	return z
}

func zSearch(pattern, text string) []int {
	combined := []byte(pattern + "$" + text)
	z := zFunction(combined)
	plen := len(pattern)
	var matches []int
	for i := plen + 1; i < len(combined); i++ {
		if z[i] == plen {
			matches = append(matches, i-plen-1)
		}
	}
	return matches
}

func min(a, b int) int {
	if a < b { return a }
	return b
}

// ─── Suffix Array (O(n log n) via doubling) ──────────────────────────────────
// Full SA-IS is ~200 lines; the doubling approach is production-adequate for n ≤ 10^6.

func buildSuffixArray(s string) []int {
	n := len(s)
	sa := make([]int, n)
	rank := make([]int, n)
	tmp := make([]int, n)

	for i := range sa { sa[i] = i; rank[i] = int(s[i]) }

	for gap := 1; gap < n; gap <<= 1 {
		// comparator closes over rank and gap
		lessFunc := func(a, b int) bool {
			if rank[a] != rank[b] { return rank[a] < rank[b] }
			ra := -1; if a+gap < n { ra = rank[a+gap] }
			rb := -1; if b+gap < n { rb = rank[b+gap] }
			return ra < rb
		}
		sortByLess(sa, lessFunc)

		tmp[sa[0]] = 0
		for i := 1; i < n; i++ {
			tmp[sa[i]] = tmp[sa[i-1]]
			if lessFunc(sa[i-1], sa[i]) { tmp[sa[i]]++ }
		}
		copy(rank, tmp)
	}
	return sa
}

// Minimal insertion sort for illustration; use sort.Slice in production
func sortByLess(a []int, less func(i, j int) bool) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && less(a[j], a[j-1]); j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

// Kasai's LCP array construction — O(n)
func buildLCP(s string, sa []int) []int {
	n := len(sa)
	rank := make([]int, n)
	for i, v := range sa { rank[v] = i }
	lcp := make([]int, n)
	k := 0
	for i := 0; i < n; i++ {
		if rank[i] == 0 { k = 0; continue }
		j := sa[rank[i]-1]
		for i+k < n && j+k < n && s[i+k] == s[j+k] { k++ }
		lcp[rank[i]] = k
		if k > 0 { k-- }
	}
	return lcp
}

// ─── Aho-Corasick ─────────────────────────────────────────────────────────────

type ACNode struct {
	children [256]*ACNode
	fail     *ACNode
	output   []int // pattern indices ending at this node
}

type AhoCorasick struct {
	root     *ACNode
	patterns []string
}

func NewAhoCorasick(patterns []string) *AhoCorasick {
	ac := &AhoCorasick{root: &ACNode{}, patterns: patterns}
	// Build trie
	for i, p := range patterns {
		cur := ac.root
		for _, c := range []byte(p) {
			if cur.children[c] == nil {
				cur.children[c] = &ACNode{}
			}
			cur = cur.children[c]
		}
		cur.output = append(cur.output, i)
	}
	// BFS to build failure links
	queue := []*ACNode{}
	for _, child := range ac.root.children {
		if child != nil {
			child.fail = ac.root
			queue = append(queue, child)
		}
	}
	for len(queue) > 0 {
		cur := queue[0]; queue = queue[1:]
		for c, child := range cur.children {
			if child == nil { continue }
			fail := cur.fail
			for fail != nil && fail.children[c] == nil {
				fail = fail.fail
			}
			if fail == nil {
				child.fail = ac.root
			} else {
				child.fail = fail.children[c]
				if child.fail == child { child.fail = ac.root }
			}
			// Merge output links (dictionary links)
			child.output = append(child.output, child.fail.output...)
			queue = append(queue, child)
		}
	}
	return ac
}

// Search returns a list of (position, pattern_index) for all matches.
func (ac *AhoCorasick) Search(text string) [][2]int {
	var results [][2]int
	cur := ac.root
	for i, c := range []byte(text) {
		for cur != ac.root && cur.children[c] == nil {
			cur = cur.fail
		}
		if cur.children[c] != nil {
			cur = cur.children[c]
		}
		for _, pi := range cur.output {
			results = append(results, [2]int{i - len(ac.patterns[pi]) + 1, pi})
		}
	}
	return results
}

// ─── Suffix Automaton ────────────────────────────────────────────────────────

type SAMState struct {
	len  int
	link int
	next [26]int
}

type SAM struct {
	st   []SAMState
	last int
}

func NewSAM(maxLen int) *SAM {
	sam := &SAM{st: make([]SAMState, 0, 2*maxLen)}
	sam.st = append(sam.st, SAMState{len: 0, link: -1})
	for i := range sam.st[0].next { sam.st[0].next[i] = -1 }
	sam.last = 0
	return sam
}

func (sam *SAM) Extend(c int) {
	cur := len(sam.st)
	sam.st = append(sam.st, SAMState{len: sam.st[sam.last].len + 1, link: -1})
	for i := range sam.st[cur].next { sam.st[cur].next[i] = -1 }

	p := sam.last
	for p != -1 && sam.st[p].next[c] == -1 {
		sam.st[p].next[c] = cur
		p = sam.st[p].link
	}
	if p == -1 {
		sam.st[cur].link = 0
	} else {
		q := sam.st[p].next[c]
		if sam.st[p].len+1 == sam.st[q].len {
			sam.st[cur].link = q
		} else {
			clone := len(sam.st)
			sam.st = append(sam.st, sam.st[q]) // copy q
			sam.st[clone].len = sam.st[p].len + 1
			for p != -1 && sam.st[p].next[c] == q {
				sam.st[p].next[c] = clone
				p = sam.st[p].link
			}
			sam.st[q].link = clone
			sam.st[cur].link = clone
		}
	}
	sam.last = cur
}

// CountDistinctSubstrings counts distinct non-empty substrings in O(n).
func (sam *SAM) CountDistinctSubstrings() int64 {
	var total int64
	for i := 1; i < len(sam.st); i++ {
		total += int64(sam.st[i].len - sam.st[sam.st[i].link].len)
	}
	return total
}

func main() {
	// Z-algorithm demo
	matches := zSearch("abc", "xabcyabcz")
	fmt.Println("Z-search matches:", matches) // [1 5]

	// Suffix array + LCP
	s := "banana"
	sa := buildSuffixArray(s)
	lcp := buildLCP(s, sa)
	fmt.Println("Suffix array:", sa)   // [5 3 1 0 4 2]
	fmt.Println("LCP array:", lcp)     // [0 1 3 0 0 2]

	// Aho-Corasick demo
	ac := NewAhoCorasick([]string{"he", "she", "his", "hers"})
	hits := ac.Search("ahishers")
	fmt.Println("Aho-Corasick hits:", hits) // [{1,2},{3,1},{5,3},{3,0}] (pos, pattern)

	// SAM demo
	sam := NewSAM(10)
	for _, c := range "abcbc" {
		sam.Extend(int(c - 'a'))
	}
	fmt.Println("Distinct substrings:", sam.CountDistinctSubstrings()) // 12
}
```

### Go-specific considerations

- **`sort.Slice` for suffix array**: The doubling construction uses a custom comparator.
  Replace the demonstration's insertion sort with `sort.Slice(sa, func(i, j int) bool {...})`
  for O(n log² n) in practice. For truly O(n log n), use radix sort on pairs.
- **Aho-Corasick memory**: Each `ACNode` holds 256 pointers. For a dictionary of 1000 ASCII
  patterns, this creates up to 1000×(avg pattern length) nodes × 256 pointers = significant
  heap pressure. Use a `map[byte]*ACNode` for sparse alphabets; the pointer-array version is
  only faster when the alphabet utilization is high.
- **SAM alphabet assumption**: The SAM uses `[26]int` (lowercase ASCII). For arbitrary byte
  sequences (log data, binary), change to `[256]int` or `map[byte]int`.

## Implementation: Rust

```rust
use std::collections::VecDeque;

// ─── Z-Algorithm ─────────────────────────────────────────────────────────────

fn z_function(s: &[u8]) -> Vec<usize> {
    let n = s.len();
    let mut z = vec![0usize; n];
    let (mut l, mut r) = (0usize, 0usize);
    for i in 1..n {
        if i < r {
            z[i] = (r - i).min(z[i - l]);
        }
        while i + z[i] < n && s[z[i]] == s[i + z[i]] {
            z[i] += 1;
        }
        if i + z[i] > r {
            l = i;
            r = i + z[i];
        }
    }
    z
}

fn z_search(pattern: &[u8], text: &[u8]) -> Vec<usize> {
    let mut combined = Vec::with_capacity(pattern.len() + 1 + text.len());
    combined.extend_from_slice(pattern);
    combined.push(b'$');
    combined.extend_from_slice(text);
    let z = z_function(&combined);
    let plen = pattern.len();
    z.iter()
        .enumerate()
        .skip(plen + 1)
        .filter(|&(_, &zi)| zi == plen)
        .map(|(i, _)| i - plen - 1)
        .collect()
}

// ─── Suffix Array (O(n log²n) via doubling) ──────────────────────────────────

fn build_suffix_array(s: &[u8]) -> Vec<usize> {
    let n = s.len();
    let mut sa: Vec<usize> = (0..n).collect();
    let mut rank: Vec<i64> = s.iter().map(|&c| c as i64).collect();
    let mut tmp = vec![0i64; n];

    let mut gap = 1;
    while gap < n {
        let rank_ref = &rank;
        sa.sort_by(|&a, &b| {
            let ra = rank_ref[a];
            let rb = rank_ref[b];
            if ra != rb { return ra.cmp(&rb); }
            let ra2 = if a + gap < n { rank_ref[a + gap] } else { -1 };
            let rb2 = if b + gap < n { rank_ref[b + gap] } else { -1 };
            ra2.cmp(&rb2)
        });
        tmp[sa[0]] = 0;
        for i in 1..n {
            let prev = sa[i - 1];
            let cur = sa[i];
            tmp[cur] = tmp[prev];
            let r_prev = (rank[prev], if prev + gap < n { rank[prev + gap] } else { -1 });
            let r_cur  = (rank[cur],  if cur  + gap < n { rank[cur  + gap] } else { -1 });
            if r_prev != r_cur { tmp[cur] += 1; }
        }
        rank.copy_from_slice(&tmp);
        gap <<= 1;
    }
    sa
}

fn build_lcp(s: &[u8], sa: &[usize]) -> Vec<usize> {
    let n = sa.len();
    let mut rank = vec![0usize; n];
    for (i, &v) in sa.iter().enumerate() { rank[v] = i; }
    let mut lcp = vec![0usize; n];
    let mut k = 0usize;
    for i in 0..n {
        if rank[i] == 0 { k = 0; continue; }
        let j = sa[rank[i] - 1];
        while i + k < n && j + k < n && s[i + k] == s[j + k] { k += 1; }
        lcp[rank[i]] = k;
        if k > 0 { k -= 1; }
    }
    lcp
}

// ─── Aho-Corasick ─────────────────────────────────────────────────────────────

struct AhoCorasick {
    // Flat representation: children[node * 256 + c] = child node
    children: Vec<usize>,
    fail: Vec<usize>,
    output: Vec<Vec<usize>>, // pattern indices per node
    num_nodes: usize,
}

impl AhoCorasick {
    const NONE: usize = usize::MAX;

    fn new(patterns: &[&str]) -> Self {
        let mut ac = AhoCorasick {
            children: vec![Self::NONE; 256],
            fail: vec![0; 1],
            output: vec![vec![]],
            num_nodes: 1,
        };

        for (pi, &p) in patterns.iter().enumerate() {
            let mut cur = 0usize;
            for &c in p.as_bytes() {
                let ci = c as usize;
                if ac.children[cur * 256 + ci] == Self::NONE {
                    ac.children.extend(vec![Self::NONE; 256]);
                    ac.fail.push(0);
                    ac.output.push(vec![]);
                    ac.children[cur * 256 + ci] = ac.num_nodes;
                    ac.num_nodes += 1;
                }
                cur = ac.children[cur * 256 + ci];
            }
            ac.output[cur].push(pi);
        }

        // BFS for failure links
        let mut queue = VecDeque::new();
        for c in 0..256usize {
            let child = ac.children[c];
            if child != Self::NONE {
                ac.fail[child] = 0;
                queue.push_back(child);
            }
        }
        while let Some(cur) = queue.pop_front() {
            let fail_cur = ac.fail[cur];
            // Merge output from fail node
            let extra: Vec<usize> = ac.output[fail_cur].clone();
            ac.output[cur].extend(extra);
            for c in 0..256usize {
                let child = ac.children[cur * 256 + c];
                if child != Self::NONE {
                    let mut f = fail_cur;
                    while f != 0 && ac.children[f * 256 + c] == Self::NONE {
                        f = ac.fail[f];
                    }
                    let fc = ac.children[f * 256 + c];
                    ac.fail[child] = if fc == Self::NONE || fc == child { 0 } else { fc };
                    queue.push_back(child);
                }
            }
        }
        ac
    }

    fn search(&self, text: &[u8]) -> Vec<(usize, usize)> {
        let mut results = Vec::new();
        let mut cur = 0usize;
        for (i, &c) in text.iter().enumerate() {
            let ci = c as usize;
            while cur != 0 && self.children[cur * 256 + ci] == Self::NONE {
                cur = self.fail[cur];
            }
            if self.children[cur * 256 + ci] != Self::NONE {
                cur = self.children[cur * 256 + ci];
            }
            for &pi in &self.output[cur] {
                results.push((i, pi));
            }
        }
        results
    }
}

// ─── Suffix Automaton ────────────────────────────────────────────────────────

#[derive(Clone)]
struct SAMState {
    len: usize,
    link: usize,
    next: [usize; 26],
}

impl SAMState {
    fn new(len: usize, link: usize) -> Self {
        SAMState { len, link, next: [usize::MAX; 26] }
    }
}

struct SAM {
    states: Vec<SAMState>,
    last: usize,
}

impl SAM {
    fn new() -> Self {
        let mut sam = SAM { states: Vec::new(), last: 0 };
        sam.states.push(SAMState::new(0, usize::MAX));
        sam
    }

    fn extend(&mut self, c: usize) {
        let cur = self.states.len();
        self.states.push(SAMState::new(self.states[self.last].len + 1, usize::MAX));
        let mut p = self.last;

        loop {
            if self.states[p].next[c] == usize::MAX {
                self.states[p].next[c] = cur;
                if self.states[p].link == usize::MAX { break; }
                p = self.states[p].link;
            } else {
                let q = self.states[p].next[c];
                if self.states[p].len + 1 == self.states[q].len {
                    self.states[cur].link = q;
                } else {
                    let clone = self.states.len();
                    let mut cloned = self.states[q].clone();
                    cloned.len = self.states[p].len + 1;
                    self.states.push(cloned);
                    loop {
                        self.states[p].next[c] = clone;
                        if self.states[p].link == usize::MAX { break; }
                        let pp = self.states[p].link;
                        if self.states[pp].next[c] != q { break; }
                        p = pp;
                    }
                    self.states[q].link = clone;
                    self.states[cur].link = clone;
                }
                break;
            }
        }
        self.last = cur;
    }

    fn count_distinct_substrings(&self) -> u64 {
        self.states.iter().skip(1).map(|st| {
            let link_len = if st.link == usize::MAX { 0 } else { self.states[st.link].len };
            (st.len - link_len) as u64
        }).sum()
    }
}

fn main() {
    // Z-search
    let matches = z_search(b"abc", b"xabcyabcz");
    println!("Z-search: {:?}", matches); // [1, 5]

    // Suffix array
    let s = b"banana";
    let sa = build_suffix_array(s);
    let lcp = build_lcp(s, &sa);
    println!("SA: {:?}", sa);
    println!("LCP: {:?}", lcp);

    // Aho-Corasick
    let ac = AhoCorasick::new(&["he", "she", "his", "hers"]);
    let hits = ac.search(b"ahishers");
    println!("AC hits: {:?}", hits);

    // SAM
    let mut sam = SAM::new();
    for c in b"abcbc" { sam.extend((*c - b'a') as usize); }
    println!("Distinct substrings: {}", sam.count_distinct_substrings()); // 12
}
```

### Rust-specific considerations

- **Flat Aho-Corasick trie**: The `children: Vec<usize>` with `node * 256 + c` indexing
  avoids `Vec<[usize; 256]>` which would require `Default` and clone complexity. It also
  grows dynamically. The `aho-corasick` crate on crates.io is production-hardened and
  handles Unicode — prefer it over this reference implementation for real use.
- **SAM borrow in extend**: The `extend` method needs mutable access to `self.states` while
  also reading `self.states[p]`. The loop structure sidesteps this by indexing explicitly
  rather than holding a borrow across the mutation.
- **Suffix array Unicode**: The implementation works on `&[u8]`. For Unicode strings,
  collect `char` values as `u32` and adapt the rank comparisons.
- **`aho-corasick` crate vs. hand-rolled**: For production use, `aho-corasick = "1"` is the
  right choice. It implements DFA-mode (precomputed next states for all characters, no failure
  link traversal at search time) and SIMD acceleration. Hand-rolling is only justified for
  constrained environments or custom alphabets.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|------|
| String type | `string` is immutable `[]byte` slice; `[]byte` for mutable | `&str` / `String` / `&[u8]`; explicit conversions required |
| Aho-Corasick with Unicode | `range s` iterates runes automatically | Must use `s.chars()` or `s.as_bytes()` explicitly |
| SAM ownership | Pointer-based states work naturally with GC | Index-based (flat Vec) needed to avoid borrow conflict |
| Crate ecosystem | Minimal stdlib string support | `aho-corasick`, `suffix`, `bstr` crates are excellent |
| Regex performance | `regexp` package uses RE2 (linear time) | `regex` crate also RE2-based, with SIMD; faster for most patterns |
| Memory for SA | `[]int` slice, GC managed | `Vec<usize>` — zero GC overhead, deterministic allocation |
| Concurrency | Parallel search with goroutines + channels | Rayon parallel iterators; thread-safe by construction |

## Production War Stories

**BWA / Bowtie (DNA alignment)**: These genome aligners index the human genome (3 GB) as
a compressed suffix array (BWT + FM-index, which is built on top of the suffix array).
Aligning a short read is an O(m log n) binary search in the suffix array. A single run
aligns billions of 150-bp reads against this index. The SA-IS construction runs once;
alignment runs continuously.

**Elasticsearch full-text search**: Multi-term phrase queries use an Aho-Corasick-like
automaton over the inverted index posting lists. The `TermsQuery` DSL compiles to an
automaton that matches any of the given terms in a single pass over the token stream.

**Ripgrep (rg)**: Uses the `aho-corasick` crate for multi-pattern search. For single
patterns, it uses SIMD-accelerated Boyer-Moore-Horspool. The suffix array is used in
`ripgrep-all` for compressed file search. Andrew Gallant (burntsushi) wrote a blog post
("Regex engine internals as a library") detailing the design.

**Splunk log indexing**: Bloom filters on n-grams for fast substring search, with the
suffix array used for exact phrase matching in the slower exact-search path. The LCP
array enables their "longest common event prefix" feature.

**PostgreSQL `pg_trgm`**: Trigram-based similarity search uses a compressed suffix array
variant. The `LIKE` and `ILIKE` operators with `pg_trgm` index use GIN/GiST index
structures built on top of suffix-array concepts.

## Complexity Analysis

| Algorithm | Build | Query | Space |
|-----------|-------|-------|-------|
| Z-function | O(n) | — | O(n) |
| Suffix array (doubling) | O(n log² n) | O(log n) search | O(n) |
| Suffix array (SA-IS) | O(n) | O(log n) search | O(n) |
| LCP array (Kasai) | O(n) | O(1) with RMQ | O(n) |
| Aho-Corasick | O(Σ|patterns| × Σ) | O(|text| + matches) | O(Σ|patterns| × Σ) |
| Suffix automaton | O(n) amortized | O(m) pattern search | O(n) states, O(n) transitions |

**Hidden costs**: The suffix array binary search for a pattern of length m takes O(m log n)
character comparisons (not O(log n)). With the LCP array and RMQ, this reduces to
O(m + log n). The Aho-Corasick DFA-mode precomputes transitions for all characters,
trading O(Σ|patterns| × |Σ|) construction space for O(1) per character during search.

## Common Pitfalls

1. **Z-algorithm off-by-one**: `Z[0]` is conventionally left 0 (or n). Some implementations
   set it to n, which breaks the pattern-matching check `Z[i] == len(pattern)`. Use a
   sentinel character between pattern and text to avoid ambiguity.

2. **Suffix array: equal ranks cause infinite loop**: In the doubling construction, if the
   comparator breaks ties arbitrarily (e.g., by index) rather than using the suffix at
   position `i+gap`, the rank assignment fails to converge. Always compare `(rank[i], rank[i+gap])`.

3. **Aho-Corasick: missing dictionary (output) links**: Failure links point to the longest
   proper suffix that is a valid state. But an accepting state reachable via a failure link
   is also a match. Without output links (following the failure chain to collect all accepting
   states on the failure path), you miss matches that are suffixes of other patterns.

4. **SAM: extend modifies clone's link before checking**: In the cloning branch of SAM
   extend, you must set `states[q].link = clone` AND `states[cur].link = clone` before
   returning. Setting only one of these leaves the automaton in an inconsistent state that
   causes wrong distinct-substring counts.

5. **Kasai LCP: forgetting to reset k at rank 0**: When processing the suffix with `rank[i] = 0`,
   there is no predecessor. Setting `k = 0` here is mandatory — failing to do so reads a
   garbage LCP value and all subsequent entries are wrong.

## Exercises

**Exercise 1 — Verification** (30 min):
Implement the Z-algorithm and verify it matches the output of `strings.Index` in Go's stdlib
for 1000 randomly generated pattern/text pairs. Measure the time for patterns of length 10
in texts of length 10^6 vs. naive O(nm) search. Record the speedup.

**Exercise 2 — Extension** (2–4 h):
Build a "longest common substring of two strings" solution using the suffix array.
Concatenate `s1 + '#' + s2`, build the suffix array and LCP array, then scan LCP for
the maximum value where adjacent entries come from different strings. Verify against a
brute-force O(n²) solution. Analyze the memory footprint for |s1| = |s2| = 10^6.

**Exercise 3 — From Scratch** (4–8 h):
Implement the full SA-IS algorithm for suffix array construction (not the doubling approach
shown). SA-IS is the basis of `libdivsufsort` used in all production genome aligners. Verify
your implementation against the O(n log² n) doubling version on random inputs of length
10^6. Measure construction time for both.

**Exercise 4 — Production Scenario** (8–15 h):
Build a multi-pattern log analyzer service. Input: a YAML config file listing 50–200 regex
patterns (simple fixed strings plus some character classes). Compile the fixed-string
patterns into an Aho-Corasick automaton. For patterns that require true regex, fall back to
the standard regex engine. Process a 10 GB log file in a streaming fashion (never loading
more than 64 MB at once). Output a JSON summary: count and positions of each pattern match.
Implement in both Go and Rust. Benchmark peak memory usage and throughput. Document which
patterns benefit from Aho-Corasick vs. regex and why.

## Further Reading

### Foundational Papers
- Nong, G., Zhang, S., & Chan, W. H. (2009). "Linear suffix array construction by almost
  pure induced-sorting." *Data Compression Conference*. The SA-IS paper.
- Kasai, T., et al. (2001). "Linear-time longest-common-prefix computation in suffix arrays
  and its applications." *CPM 2001*, LNCS 2089.
- Aho, A. V., & Corasick, M. J. (1975). "Efficient string matching: an aid to bibliographic
  search." *Communications of the ACM*, 18(6), 333–340.
- Crochemore, M., & Vérin, R. (1997). "On compact directed acyclic word graphs." In
  *Structures in Logic and Computer Science*, LNCS 1261. Foundation for the SAM.

### Books
- *Algorithms on Strings, Trees, and Sequences* — Dan Gusfield. The definitive text.
  Chapters 7–9 (suffix trees/arrays), 17 (multiple pattern matching).
- *String Algorithms in C* — Henry Spencer. Practical implementations.
- *Competitive Programmer's Handbook* — Antti Laaksonen. Chapter 26 for string DP and
  suffix structures (freely available online).

### Production Code to Read
- **`aho-corasick` crate** (`github.com/BurntSushi/aho-corasick`): Study `src/dfa.rs` for
  the DFA-mode precomputed transition table and `src/nfa/noncontiguous.rs` for the NFA mode.
- **`libdivsufsort`** (`github.com/y-256/libdivsufsort`): The SA-IS implementation used in
  BWA, samtools, and most bioinformatics tools. `divsufsort.c` is the core.
- **Go `regexp` package** (`src/regexp/`): `syntax/parse.go` shows how patterns are compiled
  to NFA; `onepass.go` shows the DFA optimization.

### Conference Talks
- "Rust's String Handling" — RustConf 2019, Andrew Gallant. Deep dive into `bstr` and
  the design decisions behind byte-string handling.
- "String Searching Algorithms" — MIT 6.006 Lecture 9 (OCW). KMP, Rabin-Karp, and
  the Z-algorithm with proof sketches.
