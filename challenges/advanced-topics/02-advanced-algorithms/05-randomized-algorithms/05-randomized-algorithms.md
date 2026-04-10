<!--
type: reference
difficulty: advanced
section: [02-advanced-algorithms]
concepts: [treap, randomized-quicksort, monte-carlo-vs-las-vegas, skip-list, reservoir-sampling, miller-rabin]
languages: [go, rust]
estimated_reading_time: 45-75 min
bloom_level: analyze
prerequisites: [probability-basics, expected-value, binary-search-trees, hash-functions]
papers: [aragon-seidel-1989-treap, vitter-1985-reservoir-sampling, miller-1976-rabin-1980-primality]
industry_use: [redis-skip-list, postgresql-random-sampling, openssl-primality, clickhouse-sampling]
language_contrast: medium
-->

# Randomized Algorithms

> Randomization often produces simpler, faster algorithms than deterministic alternatives — not by sacrificing correctness, but by trading worst-case guarantees for high-probability guarantees that are indistinguishable from certainty in practice.

## Mental Model

Randomized algorithms fall into two categories, and confusing them causes production bugs:

- **Las Vegas algorithms**: Always correct; only the *runtime* is random. Randomized
  QuickSort is Las Vegas: it always sorts correctly, but an adversarial input cannot
  force O(n²) behavior because pivot selection is random.

- **Monte Carlo algorithms**: Always fast; only the *result* is random (with bounded
  error probability). Miller-Rabin primality testing is Monte Carlo: it always terminates
  in O(k log² n) time, but may declare a composite number as prime with probability 4^(-k).
  You control the error probability by choosing k.

The senior engineer's trigger: "Is the worst case of a deterministic algorithm being
exploited in practice?" (Use randomization to eliminate adversarial inputs.) Or: "Is
an exact answer too expensive and an approximate answer acceptable?" (Use Monte Carlo.)

A third category worth knowing: **probabilistic data structures** (skip lists, treaps)
use randomization to achieve *expected* O(log n) operations where deterministic
alternatives require careful balancing. They are simpler to implement correctly than
AVL or red-black trees and perform comparably in practice.

## Core Concepts

### Treap: Randomized Binary Search Tree

A treap assigns each node a random priority. The tree maintains two invariants:
- **BST property** on keys (left key < parent key < right key)
- **Heap property** on priorities (parent priority > children priorities)

The key insight: a random permutation of insertions into a BST produces the same
distribution of tree shapes as a treap. The expected height is O(log n), and rotations
during insert/delete maintain both invariants with at most O(log n) rotations.

**Why prefer over AVL/Red-Black in production**: Treap operations are simpler to implement
correctly (no multi-case rotation logic). The split and merge operations (which do not
exist in standard AVL/RB) make treaps ideal for *order statistics* (k-th element),
*interval operations* (split at position k, merge two treaps), and *persistent* data
structures.

Redis uses a skip list (not a treap, but based on similar probabilistic guarantees)
for sorted sets because it is simpler to implement than a red-black tree while
delivering comparable performance.

### Randomized QuickSort Analysis

The expected number of comparisons is O(n log n). The proof uses *indicator random variables*:
define `X_{ij} = 1` if element i and element j are compared. Two elements are compared
exactly when one is chosen as pivot before any element between them (in sorted order).
The probability of this is `2/(j-i+1)`. Summing over all pairs gives the O(n log n) bound.

The **adversarial input problem**: a deterministic QuickSort with "first element as pivot"
runs O(n²) on sorted input. Random pivot selection eliminates this: no input can be
adversarial because the algorithm's randomness is independent of the input.

### Skip List

A randomized linked list with multiple levels. Each node is promoted to level k with
probability p^k (typically p = 0.5). The expected number of nodes at level k is n×p^k.
Expected number of levels is log_{1/p}(n).

Search traverses from the top level down, skipping large sections of the list. Expected
time: O(log n). Space: O(n) expected.

**Probabilistic analysis**: the probability of any operation exceeding O(log n) by a
constant factor c is at most 1/n^c. This makes skip lists "correct with high probability"
— the failure probability shrinks faster than any polynomial in n, which is operationally
indistinguishable from deterministic guarantees.

### Reservoir Sampling

Maintain a *reservoir* of size k from an *unknown-length stream*. At step i:
- If i ≤ k: add the item directly.
- If i > k: with probability k/i, replace a uniformly random reservoir element with the new item.

Proof of uniformity: by induction. After step i, each of the i seen items is in the
reservoir with probability k/i. Adding item i+1: it replaces any existing item with
probability k/(i+1). Each existing item survives with probability (1 - k/i × 1/k) × k/i =
k/(i+1). The invariant holds.

**Why this matters**: You cannot know the stream length in advance (log files, event
streams, sensor data). Reservoir sampling gives you a uniform random sample of exactly
k items in a single pass with O(k) memory.

### Miller-Rabin Primality Test

Deterministic primality testing is O(n^(1/2)) (trial division) or O((log n)^6) (AKS
algorithm — too slow in practice). Miller-Rabin is a Monte Carlo test: O(k log² n) with
false-positive probability ≤ 4^(-k).

A number n is prime if for every base a coprime to n: a^(n-1) ≡ 1 (mod n) (Fermat's
little theorem). Miller-Rabin strengthens this: write n-1 = 2^s × d. Then either
a^d ≡ 1 (mod n) or a^(2^r × d) ≡ -1 (mod n) for some r < s. Any composite number
will fail this test for at least 3/4 of all possible bases a.

For deterministic behavior in a fixed range: using the specific bases {2, 3, 5, 7, 11,
13, 17, 19, 23, 29, 31, 37} is deterministically correct for all n < 3.317×10^24 —
which covers all 64-bit integers.

## Implementation: Go

```go
package main

import (
	"fmt"
	"math/rand"
)

// ─── Treap ───────────────────────────────────────────────────────────────────

type TreapNode struct {
	key, priority int
	sz            int
	left, right   *TreapNode
}

func newNode(key int) *TreapNode {
	return &TreapNode{key: key, priority: rand.Int(), sz: 1}
}

func sz(t *TreapNode) int {
	if t == nil { return 0 }
	return t.sz
}

func pull(t *TreapNode) {
	if t != nil { t.sz = 1 + sz(t.left) + sz(t.right) }
}

// split: left subtree has keys < key, right has keys >= key
func split(t *TreapNode, key int) (*TreapNode, *TreapNode) {
	if t == nil { return nil, nil }
	if t.key < key {
		l, r := split(t.right, key)
		t.right = l
		pull(t)
		return t, r
	}
	l, r := split(t.left, key)
	t.left = r
	pull(t)
	return l, t
}

func merge(l, r *TreapNode) *TreapNode {
	if l == nil { return r }
	if r == nil { return l }
	if l.priority > r.priority {
		l.right = merge(l.right, r)
		pull(l)
		return l
	}
	r.left = merge(l, r.left)
	pull(r)
	return r
}

type Treap struct{ root *TreapNode }

func (t *Treap) Insert(key int) {
	l, r := split(t.root, key)
	t.root = merge(merge(l, newNode(key)), r)
}

func (t *Treap) Delete(key int) {
	l, r := split(t.root, key)
	_, r = split(r, key+1)
	t.root = merge(l, r)
}

func (t *Treap) Contains(key int) bool {
	cur := t.root
	for cur != nil {
		if key == cur.key { return true }
		if key < cur.key { cur = cur.left } else { cur = cur.right }
	}
	return false
}

// KthSmallest returns the k-th smallest key (1-indexed).
func (t *Treap) KthSmallest(k int) int {
	cur := t.root
	for cur != nil {
		leftSz := sz(cur.left)
		if k == leftSz+1 { return cur.key }
		if k <= leftSz { cur = cur.left } else { k -= leftSz + 1; cur = cur.right }
	}
	return -1 // not found
}

// ─── Reservoir Sampling ──────────────────────────────────────────────────────

// ReservoirSample returns a uniform random sample of k items from an iterator.
func ReservoirSample(stream []int, k int) []int {
	reservoir := make([]int, k)
	copy(reservoir, stream[:k])
	for i := k; i < len(stream); i++ {
		j := rand.Intn(i + 1)
		if j < k {
			reservoir[j] = stream[i]
		}
	}
	return reservoir
}

// ─── Miller-Rabin Primality Test ─────────────────────────────────────────────

func mulmod(a, b, m uint64) uint64 {
	// For m up to 2^62, this avoids overflow using 128-bit intermediate.
	// Go lacks native uint128; use this safe method for correctness.
	result := uint64(0)
	a %= m
	for b > 0 {
		if b&1 == 1 { result = (result + a) % m }
		a = (a + a) % m
		b >>= 1
	}
	return result
}

func powmod(base, exp, mod uint64) uint64 {
	result := uint64(1)
	base %= mod
	for exp > 0 {
		if exp&1 == 1 { result = mulmod(result, base, mod) }
		base = mulmod(base, base, mod)
		exp >>= 1
	}
	return result
}

// millerRabinTest checks if n is a strong probable prime to base a.
func millerRabinTest(n, a uint64) bool {
	if n%a == 0 { return n == a }
	d := n - 1
	r := 0
	for d%2 == 0 { d /= 2; r++ }
	x := powmod(a, d, n)
	if x == 1 || x == n-1 { return true }
	for i := 0; i < r-1; i++ {
		x = mulmod(x, x, n)
		if x == n-1 { return true }
	}
	return false
}

// IsPrime is deterministic for all n < 3.317 × 10^24 using known witness set.
func IsPrime(n uint64) bool {
	if n < 2 { return false }
	for _, p := range []uint64{2, 3, 5, 7, 11, 13, 17, 19, 23, 29, 31, 37} {
		if n == p { return true }
		if !millerRabinTest(n, p) { return false }
	}
	return true
}

// ─── Randomized QuickSort ────────────────────────────────────────────────────

func quicksort(a []int) {
	if len(a) <= 1 { return }
	pivotIdx := rand.Intn(len(a))
	a[0], a[pivotIdx] = a[pivotIdx], a[0]
	pivot := a[0]
	i, j := 1, len(a)-1
	for i <= j {
		for i <= j && a[i] <= pivot { i++ }
		for i <= j && a[j] > pivot { j-- }
		if i < j { a[i], a[j] = a[j], a[i] }
	}
	a[0], a[j] = a[j], a[0]
	quicksort(a[:j])
	quicksort(a[j+1:])
}

func main() {
	// Treap demo
	t := &Treap{}
	for _, v := range []int{5, 3, 7, 1, 4, 6, 8} { t.Insert(v) }
	fmt.Println("Contains 4:", t.Contains(4))     // true
	fmt.Println("3rd smallest:", t.KthSmallest(3)) // 4
	t.Delete(4)
	fmt.Println("After delete 4, contains 4:", t.Contains(4)) // false

	// Reservoir sampling demo
	stream := make([]int, 1000)
	for i := range stream { stream[i] = i }
	sample := ReservoirSample(stream, 10)
	fmt.Println("Reservoir sample (10 from 1000):", sample)

	// Miller-Rabin demo
	for _, n := range []uint64{2, 17, 97, 100, 1000003, 1000033} {
		fmt.Printf("IsPrime(%d) = %v\n", n, IsPrime(n))
	}

	// QuickSort demo
	a := []int{9, 3, 7, 1, 5, 8, 2, 6, 4}
	quicksort(a)
	fmt.Println("Sorted:", a)
}
```

### Go-specific considerations

- **`rand.Int()` vs `rand.New`**: The global `rand` source is safe for concurrent use
  (since Go 1.20 it uses a per-goroutine source internally). For reproducible tests, use
  `rand.New(rand.NewSource(seed))` and pass it explicitly.
- **`mulmod` overflow**: Go has no native `uint128`. The shown shift-and-add mulmod is
  O(64) iterations. For high-throughput primality testing (e.g., key generation), use
  `math/big.Int` for modular multiplication or CGo to call into a native 128-bit multiply.
- **Treap recursion depth**: In the worst case (degenerate priority ordering), split/merge
  recurse to depth O(log n) expected but O(n) worst case. For n ≤ 10^6, the expected
  depth of ~20 is fine. For safety, add an explicit check or convert to iterative.

## Implementation: Rust

```rust
use rand::Rng;

// ─── Treap ───────────────────────────────────────────────────────────────────
// Index-based treap (no Box<Node>) to avoid allocator pressure in hot paths.

struct TreapPool {
    key: Vec<i64>,
    priority: Vec<u64>,
    sz: Vec<usize>,
    left: Vec<Option<usize>>,
    right: Vec<Option<usize>>,
}

impl TreapPool {
    fn new(capacity: usize) -> Self {
        TreapPool {
            key: Vec::with_capacity(capacity),
            priority: Vec::with_capacity(capacity),
            sz: Vec::with_capacity(capacity),
            left: Vec::with_capacity(capacity),
            right: Vec::with_capacity(capacity),
        }
    }

    fn alloc(&mut self, k: i64) -> usize {
        let id = self.key.len();
        self.key.push(k);
        self.priority.push(rand::thread_rng().gen());
        self.sz.push(1);
        self.left.push(None);
        self.right.push(None);
        id
    }

    fn pull(&mut self, t: usize) {
        let l_sz = self.left[t].map_or(0, |l| self.sz[l]);
        let r_sz = self.right[t].map_or(0, |r| self.sz[r]);
        self.sz[t] = 1 + l_sz + r_sz;
    }

    // Returns (left, right) where left has keys < split_key
    fn split(&mut self, t: Option<usize>, split_key: i64) -> (Option<usize>, Option<usize>) {
        match t {
            None => (None, None),
            Some(t) => {
                if self.key[t] < split_key {
                    let r = self.right[t];
                    let (l2, r2) = self.split(r, split_key);
                    self.right[t] = l2;
                    self.pull(t);
                    (Some(t), r2)
                } else {
                    let l = self.left[t];
                    let (l2, r2) = self.split(l, split_key);
                    self.left[t] = r2;
                    self.pull(t);
                    (l2, Some(t))
                }
            }
        }
    }

    fn merge(&mut self, l: Option<usize>, r: Option<usize>) -> Option<usize> {
        match (l, r) {
            (None, r) => r,
            (l, None) => l,
            (Some(l), Some(r)) => {
                if self.priority[l] > self.priority[r] {
                    let lr = self.right[l];
                    let merged = self.merge(lr, Some(r));
                    self.right[l] = merged;
                    self.pull(l);
                    Some(l)
                } else {
                    let rl = self.left[r];
                    let merged = self.merge(Some(l), rl);
                    self.left[r] = merged;
                    self.pull(r);
                    Some(r)
                }
            }
        }
    }

    fn kth(&self, mut t: Option<usize>, mut k: usize) -> Option<i64> {
        while let Some(node) = t {
            let l_sz = self.left[node].map_or(0, |l| self.sz[l]);
            if k == l_sz { return Some(self.key[node]); }
            if k < l_sz { t = self.left[node]; }
            else { k -= l_sz + 1; t = self.right[node]; }
        }
        None
    }
}

struct Treap {
    pool: TreapPool,
    root: Option<usize>,
}

impl Treap {
    fn new() -> Self { Treap { pool: TreapPool::new(64), root: None } }

    fn insert(&mut self, key: i64) {
        let (l, r) = self.pool.split(self.root, key);
        let node = self.pool.alloc(key);
        self.root = self.pool.merge(self.pool.merge(l, Some(node)), r);
    }

    fn delete(&mut self, key: i64) {
        let (l, r) = self.pool.split(self.root, key);
        let (_, r) = self.pool.split(r, key + 1);
        self.root = self.pool.merge(l, r);
    }

    fn kth_smallest(&self, k: usize) -> Option<i64> {
        self.pool.kth(self.root, k)
    }
}

// ─── Reservoir Sampling ──────────────────────────────────────────────────────

fn reservoir_sample(stream: &[i64], k: usize) -> Vec<i64> {
    let mut rng = rand::thread_rng();
    let mut reservoir = stream[..k].to_vec();
    for (i, &item) in stream[k..].iter().enumerate() {
        let j = rng.gen_range(0..=(i + k));
        if j < k { reservoir[j] = item; }
    }
    reservoir
}

// ─── Miller-Rabin ────────────────────────────────────────────────────────────

fn mulmod(mut a: u64, mut b: u64, m: u64) -> u64 {
    // Use u128 for exact intermediate computation
    ((a as u128 * b as u128) % m as u128) as u64
}

fn powmod(mut base: u64, mut exp: u64, m: u64) -> u64 {
    let mut result = 1u64;
    base %= m;
    while exp > 0 {
        if exp & 1 == 1 { result = mulmod(result, base, m); }
        base = mulmod(base, base, m);
        exp >>= 1;
    }
    result
}

fn miller_rabin_test(n: u64, a: u64) -> bool {
    if n % a == 0 { return n == a; }
    let mut d = n - 1;
    let mut r = 0u32;
    while d % 2 == 0 { d /= 2; r += 1; }
    let mut x = powmod(a, d, n);
    if x == 1 || x == n - 1 { return true; }
    for _ in 0..r-1 {
        x = mulmod(x, x, n);
        if x == n - 1 { return true; }
    }
    false
}

fn is_prime(n: u64) -> bool {
    if n < 2 { return false; }
    for &p in &[2u64, 3, 5, 7, 11, 13, 17, 19, 23, 29, 31, 37] {
        if n == p { return true; }
        if !miller_rabin_test(n, p) { return false; }
    }
    true
}

// ─── Randomized QuickSort ────────────────────────────────────────────────────

fn quicksort(a: &mut [i64]) {
    if a.len() <= 1 { return; }
    let pivot_idx = rand::thread_rng().gen_range(0..a.len());
    a.swap(0, pivot_idx);
    let pivot = a[0];
    let (mut i, mut j) = (1usize, a.len() - 1);
    while i <= j {
        while i <= j && a[i] <= pivot { i += 1; }
        while i <= j && a[j] > pivot { j -= 1; }
        if i < j { a.swap(i, j); }
    }
    a.swap(0, j);
    let (left, right) = a.split_at_mut(j);
    quicksort(left);
    quicksort(&mut right[1..]);
}

fn main() {
    // Treap demo
    let mut t = Treap::new();
    for &v in &[5i64, 3, 7, 1, 4, 6, 8] { t.insert(v); }
    println!("3rd smallest: {:?}", t.kth_smallest(2)); // 0-indexed: Some(4)
    t.delete(4);
    println!("3rd smallest after delete 4: {:?}", t.kth_smallest(2)); // Some(5)

    // Reservoir sampling
    let stream: Vec<i64> = (0..1000).collect();
    let sample = reservoir_sample(&stream, 10);
    println!("Reservoir sample: {:?}", &sample[..5]);

    // Miller-Rabin
    for &n in &[2u64, 17, 97, 100, 1_000_003, 1_000_033] {
        println!("is_prime({}) = {}", n, is_prime(n));
    }

    // QuickSort
    let mut a = vec![9i64, 3, 7, 1, 5, 8, 2, 6, 4];
    quicksort(&mut a);
    println!("Sorted: {:?}", a);
}
```

### Rust-specific considerations

- **`u128` for mulmod**: Rust has native `u128`, making `mulmod` a one-liner
  `((a as u128 * b as u128) % m as u128) as u64`. This is significantly faster than the
  Go shift-and-add approach and avoids overflow for all 64-bit inputs.
- **`rand` crate**: Rust has no standard PRNG. The `rand` crate is the de facto standard.
  `rand::thread_rng()` gives a cryptographically seeded, thread-local RNG. For reproducible
  tests, use `rand::rngs::StdRng::seed_from_u64(42)`.
- **Pool-based treap vs. `Box<Node>`**: `Box<Node>` is simpler to write but causes one
  heap allocation per insertion. The pool-based approach pre-allocates a `Vec` and uses
  indices. For a treap of 10^5 nodes, the pool approach reduces allocation overhead by ~10×.
- **QuickSort stack overflow**: Rust's default stack is 8 MB. For `quicksort` on arrays of
  10^6 elements, the recursion depth is O(log n) ≈ 20 expected, which is safe. The worst
  case O(n) depth is not possible with random pivots unless the RNG produces a very unlikely
  sequence (probability < 2^(-n)).

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|------|
| `u128` for mulmod | No native u128; need shift-and-add or `math/big` | Native `u128`; one-liner mulmod |
| PRNG | `math/rand` stdlib; global source + per-instance | `rand` crate; `thread_rng()` is secure and fast |
| Treap with boxes | `*TreapNode` pointers; GC manages memory | `Box<Node>` works but pools are faster |
| Reservoir sampling | Simple slice copy; GC handles grow | `Vec` with `to_vec()`; no allocation after initial copy |
| Miller-Rabin correctness | Manual mulmod needed for n > 2^62 | `u128` handles all 64-bit n exactly |
| Skip list | No stdlib; hand-roll or use `BTreeMap` | `skiplist` crate; or use `BTreeMap` |

## Production War Stories

**Redis sorted sets**: Redis uses a skip list (not a treap) for the internal sorted set
data structure. The skip list provides O(log n) rank queries and range scans in a single
data structure, which BTree doesn't support as cleanly. The implementation is ~500 lines
of C (`t_zset.c`). The probabilistic guarantee is sufficient for Redis's SLA requirements.

**PostgreSQL `tablesample`**: The BERNOULLI and SYSTEM tablesample methods use reservoir
sampling variants. The `pg_sample` extension uses Vitter's Algorithm Z for efficient
large-table sampling without a full scan.

**OpenSSL / BoringSSL RSA key generation**: Miller-Rabin (with 64 rounds for 2048-bit keys,
giving false-positive probability < 4^(-64)) is used in both libraries. The deterministic
witness set optimization (as shown here) is used for small primes; probabilistic rounds are
used for large candidate primes during key generation.

**ClickHouse sampling clauses**: ClickHouse's `SAMPLE k` clause uses a hash-based
sampling that approximates reservoir sampling. The underlying random number generation
uses a seeded Mersenne Twister for reproducibility.

**Terraform/HashiCorp tools**: Treap-based ordered maps appear in Consul's internal
service registry and in Terraform's resource dependency graph. The split/merge operations
allow efficient copy-on-write semantics for transaction isolation.

## Complexity Analysis

| Algorithm | Expected Time | Worst-Case Time | Space | Notes |
|-----------|--------------|-----------------|-------|-------|
| Treap insert/delete | O(log n) | O(n) (with prob. 1/n!) | O(n) | Expected depth O(log n) |
| Treap split/merge | O(log n) | O(n) | O(1) extra | Same as insert |
| Randomized QuickSort | O(n log n) | O(n²) with prob. < 1/n^c | O(log n) stack | |
| Skip list | O(log n) | O(n) per query (prob. 1/n^c) | O(n) expected | |
| Reservoir sampling | O(n) | O(n) | O(k) | Exact; single pass |
| Miller-Rabin (k rounds) | O(k log² n) | Same | O(1) | Error prob ≤ 4^(-k) |

**"With high probability"**: Skip list and treap guarantees are not just "expected" —
they hold with probability 1 - O(1/n^c) for any constant c you choose (by tuning constants).
This is stronger than expectation alone and is why they are safe for production use.

## Common Pitfalls

1. **Confusing Las Vegas and Monte Carlo**: Using Miller-Rabin for an application that
   requires zero false positives (e.g., confirming a certificate is valid) without
   understanding its error probability. Miller-Rabin is Las Vegas *only* with the
   deterministic witness set; randomized Miller-Rabin is Monte Carlo.

2. **Global random source in concurrent code (pre Go 1.20)**: In Go versions before 1.20,
   `rand.Intn` on the global source is protected by a mutex, creating contention in concurrent
   treap operations. Use `rand.New(rand.NewSource(seed))` per goroutine.

3. **Treap priority collisions**: If two nodes get the same random priority, the heap
   property becomes ambiguous and the tree may become unbalanced. Use 64-bit random priorities
   (collision probability ≈ n²/2^64 ≈ 0 for n ≤ 10^9).

4. **Reservoir sampling: off-by-one in the random index range**: The replacement step
   must sample `j` from `[0, i]` (inclusive), not `[0, i)`. Sampling from `[0, i)` biases
   the later stream elements. In Go: `rand.Intn(i+1)`; in Rust: `rng.gen_range(0..=i)`.

5. **Miller-Rabin for n < 2**: The algorithm assumes n ≥ 2. Testing n=0 or n=1 (or
   negative in a signed representation) hits undefined behavior in the `d = n-1` subtraction.
   Always check `n < 2` as the first guard.

## Exercises

**Exercise 1 — Verification** (30 min):
Implement reservoir sampling and empirically verify uniformity: sample k=10 items from
a stream of n=10,000 items, repeat 100,000 times, and compute the empirical probability
of each item appearing in the sample. It should be approximately k/n = 0.1% per item.
Use a chi-squared test to confirm the distribution is statistically uniform.

**Exercise 2 — Extension** (2–4 h):
Extend the treap to support implicit key (position-based) operations: insert/delete at
arbitrary positions in a sequence, and query the k-th element. This is used in text
editors (rope data structure) and sequence alignment. Implement a sequence that supports
`insert(pos, val)`, `delete(pos)`, and `query_kth(k)` in O(log n) expected time.

**Exercise 3 — From Scratch** (4–8 h):
Implement a skip list with the same API as the treap (insert, delete, contains, kth).
Compare empirical performance against the treap for 10^5 and 10^6 operations (random
inserts + queries). Measure: operation throughput, memory usage, and cache miss rate
(using `perf stat` on Linux). Document which performs better and why.

**Exercise 4 — Production Scenario** (8–15 h):
Build a distributed sampling service for log analytics. Input: N worker nodes each
streaming log events at 100k events/sec. Goal: maintain a uniform random sample of k=1000
events from the combined stream without centralizing all events. Design a distributed
reservoir sampling protocol (workers maintain local reservoirs; the coordinator merges
them). Implement the coordinator and worker as gRPC services. Prove the merged sample is
uniform. Benchmark the coordinator's throughput and the statistical quality of the sample.

## Further Reading

### Foundational Papers
- Aragon, C., & Seidel, R. (1989). "Randomized search trees." *FOCS 1989*. The treap paper.
- Vitter, J. S. (1985). "Random sampling with a reservoir." *ACM TOMS*, 11(1), 37–57.
  The original reservoir sampling paper; Algorithm Z is the fast version.
- Miller, G. L. (1976). "Riemann's hypothesis and tests for primality." *JCSS*, 13(3).
- Rabin, M. O. (1980). "Probabilistic algorithm for testing primality." *Journal of Number
  Theory*, 12(1), 128–138.

### Books
- *Randomized Algorithms* — Motwani & Raghavan. The standard graduate textbook. Chapter 1–3
  for QuickSort analysis; Chapter 10 for data structures.
- *Probability and Computing* — Mitzenmacher & Upfal. More accessible than Motwani.
  Chapter 5 (skip lists), Chapter 6 (hashing), Chapter 9 (balls and bins).

### Production Code to Read
- **Redis skip list** (`src/t_zset.c`): `zslCreate`, `zslInsert`, `zslDelete`. The
  authoritative production skip list in ~500 lines of C.
- **Go's `sort.Slice`** (`src/sort/zsortfunc.go`): Pattern-defeating quicksort (pdqsort)
  which uses randomization to prevent adversarial input — the production answer to the
  randomized QuickSort problem.
- **OpenSSL BN_is_prime** (`crypto/bn/bn_prime.c`): Miller-Rabin with the deterministic
  witness set for common sizes plus probabilistic rounds for large inputs.

### Conference Talks
- "How Redis Uses Skip Lists" — Redis Day 2019, Antirez. Explains why skip list was
  chosen over red-black tree for sorted sets.
- "Randomized Data Structures" — MIT 6.851 (Advanced Data Structures), Lecture 7.
  Erik Demaine's treatment of treaps and skip lists.
