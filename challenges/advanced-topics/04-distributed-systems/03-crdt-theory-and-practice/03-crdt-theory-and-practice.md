<!--
type: reference
difficulty: advanced
section: [04-distributed-systems]
concepts: [crdt, state-based-crdt, operation-based-crdt, delta-crdt, semilattice, join, g-counter, pn-counter, or-set, lww-element-set, convergence]
languages: [go, rust]
estimated_reading_time: 75 min
bloom_level: analyze
prerequisites: [eventual-consistency, vector-clocks, set-theory-basics]
papers: [shapiro-2011-crdt, almeida-2016-delta-crdt]
industry_use: [riak, redis-crdt, soundcloud-roshi, figma-multiplayer]
language_contrast: low
-->

# CRDT Theory and Practice

> A CRDT is a data type whose merge function is the join operation of a join-semilattice — once you accept that updates only move "upward" in a partial order and that merge is always idempotent and commutative, you can replicate state across any number of nodes with zero coordination and still converge to the same answer everywhere.

## Mental Model

The fundamental problem with eventual consistency is conflict resolution: two nodes update the same value independently, and when they reconnect, which value wins? Most systems answer this with last-write-wins (LWW), which is simple but loses data — if two users increment a counter independently, LWW keeps one increment and silently drops the other.

CRDTs (Conflict-free Replicated Data Types) solve this by designing data structures whose conflict resolution is semantically correct by construction. A grow-only counter (G-Counter) defines "merge" as component-wise maximum across per-node counters. This merge is idempotent (merging the same state twice gives the same result), commutative (order of merges does not matter), and associative (grouping does not matter). These three properties — together called the join-semilattice property — guarantee that no matter what order replicas exchange state, they all converge to the same final value.

The insight that makes CRDTs powerful is that the semilattice property is a *design constraint you impose on your data type*, not a property the network provides. You design the data type such that all operations are monotone (values can only increase in the partial order), and then replication becomes trivially safe. The tradeoff is that not all operations are expressible this way: a general counter with arbitrary increment/decrement requires the PN-Counter construction (tracking increments and decrements separately), and supporting element removal from a set requires the OR-Set construction (tracking unique "addition tokens" that must be present for an element to be in the set).

Delta CRDTs address the bandwidth problem: state-based CRDTs require sending the full state on each sync, which for large structures (a set with 10,000 elements) is expensive. Delta CRDTs send only the "delta" — the minimal state change since the last sync — while maintaining the same convergence guarantees. The delta is itself a CRDT value in the same lattice, so merging deltas is identical to merging full states.

## Core Concepts

### State-Based CRDTs (CvRDTs): The Merge Function is the Lattice Join

A state-based CRDT defines a partial order on states and a `merge` (join) function that returns the least upper bound of two states. For the G-Counter, the partial order is component-wise ≤ and the join is component-wise max. An update moves the state upward in the partial order; a merge returns the state that is ≥ both inputs. Because the lattice has no "cycles" (states only go up), and because join is idempotent and commutative, any sequence of updates and merges on any replica will converge to the same state.

The full sync cost is the main limitation: on every anti-entropy round, a node sends its entire state to its peers. For a G-Counter with 1,000 nodes, the state is a 1,000-element vector, and every sync sends the full vector. This is why delta CRDTs were developed.

### Operation-Based CRDTs (CmRDTs): Commutative Operations

An operation-based CRDT does not send full state — it sends the operation itself (e.g., `increment(node_id)`). The requirement: all operations must be commutative and idempotent when applied to a replica. The network must guarantee exactly-once, causal delivery of operations (usually implemented with vector clocks). Operations must be delivered in causal order: if operation B causally depends on A (B happened after A), A must be delivered before B.

Op-based CRDTs have lower bandwidth than state-based but require a stronger network (causal delivery). State-based CRDTs require only eventual delivery (even out-of-order, duplicate-safe). In practice, most systems that claim to use "op-based CRDTs" are actually using state-based CRDTs with delta shipping, because causal delivery is expensive to implement correctly.

### Delta CRDTs: Ship Only What Changed

A delta CRDT extends a state-based CRDT with a `delta(state, since)` function that returns the minimal state change needed to bring a stale replica up to date. The delta is in the same lattice as the full state, so merging a delta is identical to merging a full state. The bandwidth improvement is proportional to the ratio of "state changed" to "total state size."

For a G-Counter, the delta of incrementing node 3's counter from 5 to 6 is the vector `[0, 0, 6, 0, ...]` — only one element changed. The full state might be a 1,000-element vector. The receiving replica merges the delta component-wise: `max(local[i], delta[i])` for each `i`.

## Implementation: Go

```go
package main

import (
	"fmt"
	"strings"
	"sync"
)

// GCounter is a grow-only counter: each node maintains its own counter,
// and the global value is the sum of all nodes' counters.
// The partial order: s1 ≤ s2 iff s1[i] ≤ s2[i] for all i.
// The join: merge(s1, s2)[i] = max(s1[i], s2[i]) for all i.
type GCounter struct {
	mu      sync.Mutex
	nodeID  int
	counts  map[int]uint64 // node_id -> increment count
}

func NewGCounter(nodeID int) *GCounter {
	return &GCounter{nodeID: nodeID, counts: make(map[int]uint64)}
}

func (g *GCounter) Increment() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.counts[g.nodeID]++
}

// Value returns the global count: sum of all nodes' local counters.
func (g *GCounter) Value() uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	var total uint64
	for _, c := range g.counts {
		total += c
	}
	return total
}

// Merge performs the join (component-wise max) with another GCounter.
// This operation is idempotent, commutative, and associative.
func (g *GCounter) Merge(other *GCounter) {
	g.mu.Lock()
	other.mu.Lock()
	defer g.mu.Unlock()
	defer other.mu.Unlock()
	for nodeID, count := range other.counts {
		if count > g.counts[nodeID] {
			g.counts[nodeID] = count
		}
	}
}

// State returns a copy of the counter state for shipping to peers.
func (g *GCounter) State() map[int]uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	state := make(map[int]uint64, len(g.counts))
	for k, v := range g.counts {
		state[k] = v
	}
	return state
}

// PNCounter is a positive-negative counter: increment and decrement are both supported
// by maintaining separate G-Counters for increments and decrements.
// Value = sum(increments) - sum(decrements). Decrement-below-zero is allowed.
type PNCounter struct {
	mu       sync.Mutex
	nodeID   int
	positive map[int]uint64
	negative map[int]uint64
}

func NewPNCounter(nodeID int) *PNCounter {
	return &PNCounter{
		nodeID:   nodeID,
		positive: make(map[int]uint64),
		negative: make(map[int]uint64),
	}
}

func (p *PNCounter) Increment() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.positive[p.nodeID]++
}

func (p *PNCounter) Decrement() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.negative[p.nodeID]++
}

func (p *PNCounter) Value() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	var pos, neg uint64
	for _, c := range p.positive { pos += c }
	for _, c := range p.negative { neg += c }
	return int64(pos) - int64(neg)
}

func (p *PNCounter) Merge(other *PNCounter) {
	p.mu.Lock()
	other.mu.Lock()
	defer p.mu.Unlock()
	defer other.mu.Unlock()
	for id, c := range other.positive {
		if c > p.positive[id] { p.positive[id] = c }
	}
	for id, c := range other.negative {
		if c > p.negative[id] { p.negative[id] = c }
	}
}

// ORSet is an Observed-Remove Set: add and remove are both supported.
// Elements are tagged with unique tokens on add; remove removes all tokens for an element.
// An element is "in" the set iff at least one of its add-tokens is present.
// The invariant: concurrent add and remove results in the element being present (add wins).
type ORSet struct {
	mu     sync.Mutex
	nodeID int
	seq    uint64
	// tokens: element -> set of (nodeID, seq) add-tokens still present
	tokens map[string]map[[2]uint64]struct{}
}

func NewORSet(nodeID int) *ORSet {
	return &ORSet{
		nodeID: nodeID,
		tokens: make(map[string]map[[2]uint64]struct{}),
	}
}

// Add tags the element with a fresh unique token and records it.
func (s *ORSet) Add(element string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	token := [2]uint64{uint64(s.nodeID), s.seq}
	if s.tokens[element] == nil {
		s.tokens[element] = make(map[[2]uint64]struct{})
	}
	s.tokens[element][token] = struct{}{}
}

// Remove removes the element by deleting all its observed tokens.
// Any tokens added concurrently (not yet seen by this replica) will remain,
// so the element will still be present after merge — "add wins" concurrency.
func (s *ORSet) Remove(element string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, element)
}

// Contains returns true if the element has any associated tokens.
func (s *ORSet) Contains(element string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.tokens[element]) > 0
}

// Members returns the set of elements currently in the OR-Set.
func (s *ORSet) Members() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]string, 0, len(s.tokens))
	for elem, toks := range s.tokens {
		if len(toks) > 0 {
			result = append(result, elem)
		}
	}
	return result
}

// Merge computes the join of two OR-Sets: the union of all tokens.
// After merge, the set contains an element iff either replica had it with any live token.
func (s *ORSet) Merge(other *ORSet) {
	s.mu.Lock()
	other.mu.Lock()
	defer s.mu.Unlock()
	defer other.mu.Unlock()
	for elem, toks := range other.tokens {
		if s.tokens[elem] == nil {
			s.tokens[elem] = make(map[[2]uint64]struct{})
		}
		for tok := range toks {
			s.tokens[elem][tok] = struct{}{}
		}
	}
}

// LWWElementSet is a last-write-wins set: each element has a timestamp;
// add and remove operations use the timestamp to determine which "wins."
// An element is in the set iff its add-timestamp > its remove-timestamp.
// Requires synchronized clocks (or Lamport timestamps) for correctness.
type LWWElementSet struct {
	mu      sync.Mutex
	adds    map[string]int64 // element -> add timestamp
	removes map[string]int64 // element -> remove timestamp
}

func NewLWWElementSet() *LWWElementSet {
	return &LWWElementSet{
		adds:    make(map[string]int64),
		removes: make(map[string]int64),
	}
}

func (l *LWWElementSet) Add(element string, ts int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ts > l.adds[element] {
		l.adds[element] = ts
	}
}

func (l *LWWElementSet) Remove(element string, ts int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ts > l.removes[element] {
		l.removes[element] = ts
	}
}

func (l *LWWElementSet) Contains(element string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	// "add-wins" variant: element present iff add_ts >= remove_ts
	return l.adds[element] >= l.removes[element] && l.adds[element] > 0
}

func (l *LWWElementSet) Merge(other *LWWElementSet) {
	l.mu.Lock()
	other.mu.Lock()
	defer l.mu.Unlock()
	defer other.mu.Unlock()
	for elem, ts := range other.adds {
		if ts > l.adds[elem] { l.adds[elem] = ts }
	}
	for elem, ts := range other.removes {
		if ts > l.removes[elem] { l.removes[elem] = ts }
	}
}

func main() {
	// GCounter: 3 nodes increment independently, then sync
	fmt.Println("=== GCounter ===")
	gc1 := NewGCounter(1)
	gc2 := NewGCounter(2)
	gc3 := NewGCounter(3)
	gc1.Increment(); gc1.Increment()  // node 1 increments twice
	gc2.Increment()                    // node 2 increments once
	gc3.Increment(); gc3.Increment(); gc3.Increment() // node 3 increments three times
	// Merge all into gc1
	gc1.Merge(gc2); gc1.Merge(gc3)
	fmt.Printf("GCounter value after merge: %d (expected 6)\n", gc1.Value())

	// PNCounter: increments and decrements from different nodes
	fmt.Println("\n=== PNCounter ===")
	pn1 := NewPNCounter(1)
	pn2 := NewPNCounter(2)
	pn1.Increment(); pn1.Increment(); pn1.Increment() // +3
	pn2.Decrement(); pn2.Decrement()                   // -2
	pn1.Merge(pn2)
	fmt.Printf("PNCounter value after merge: %d (expected 1)\n", pn1.Value())

	// ORSet: concurrent add and remove, demonstrating "add wins"
	fmt.Println("\n=== OR-Set ===")
	s1 := NewORSet(1)
	s2 := NewORSet(2)
	s1.Add("alice")
	s2.Add("bob")
	// s1 removes "bob" (hasn't seen s2's add token yet — concurrent removal)
	s1.Remove("bob")
	// Now merge: s2's add token for "bob" is still present → bob is in the merged set
	s1.Merge(s2)
	fmt.Printf("ORSet after concurrent add/remove merge: %v\n", s1.Members())
	fmt.Printf("Contains alice: %v, Contains bob: %v\n",
		s1.Contains("alice"), s1.Contains("bob"))

	// LWWElementSet: add and remove with timestamps
	fmt.Println("\n=== LWW-Element-Set ===")
	lww1 := NewLWWElementSet()
	lww2 := NewLWWElementSet()
	lww1.Add("x", 10)
	lww2.Remove("x", 15) // remove at ts=15 > add at ts=10 → x should not be present
	lww1.Merge(lww2)
	fmt.Printf("LWW: Contains x (removed at t=15, added at t=10): %v (expected false)\n", lww1.Contains("x"))
	lww1.Add("x", 20)   // re-add at ts=20 > remove at ts=15 → x present again
	fmt.Printf("LWW: Contains x (re-added at t=20): %v (expected true)\n", lww1.Contains("x"))

	// Demonstrate idempotency: merging the same state twice is safe
	fmt.Println("\n=== Idempotency check ===")
	gc := NewGCounter(1)
	gc.Increment(); gc.Increment()
	state := gc.State()
	gcOther := &GCounter{nodeID: 2, counts: state}
	before := gc.Value()
	gc.Merge(gcOther)
	gc.Merge(gcOther) // merge twice
	after := gc.Value()
	fmt.Printf("GCounter before: %d, after double-merge: %d (must be equal)\n", before, after)

	_ = strings.Join // avoid unused import warning in demo
}
```

### Go-specific considerations

The `map[string]map[[2]uint64]struct{}` for OR-Set tokens is idiomatic Go for a set of pairs. The outer map is element-indexed, the inner map is the token set (using an empty struct to minimize memory). In production, tokens should use UUIDs rather than `[nodeID, seq]` pairs to avoid token collision when nodes are replaced or restarted with the same ID.

The `sync.Mutex` per CRDT instance allows safe concurrent access from multiple goroutines. Note the lock acquisition order in `Merge`: both `g.mu` and `other.mu` are locked. To prevent deadlock in a system where two replicas might merge with each other simultaneously (goroutine A merges A into B while goroutine B merges B into A), lock acquisition must be ordered by node ID: always acquire the lower-ID mutex first. The demo above acquires in the order (self, other) which is safe only if merges are always initiated by one side.

## Implementation: Rust

```rust
use std::collections::{HashMap, HashSet};
use std::sync::{Arc, Mutex};

// GCounter: grow-only distributed counter.
// Partial order: s1 ≤ s2 iff s1.counts[i] ≤ s2.counts[i] for all i.
#[derive(Debug, Clone, Default)]
struct GCounter {
    node_id: usize,
    counts: HashMap<usize, u64>,
}

impl GCounter {
    fn new(node_id: usize) -> Self {
        GCounter { node_id, counts: HashMap::new() }
    }

    fn increment(&mut self) {
        *self.counts.entry(self.node_id).or_insert(0) += 1;
    }

    fn value(&self) -> u64 {
        self.counts.values().sum()
    }

    // merge is the semilattice join: component-wise max.
    // Idempotent: merge(s, s) = s.
    // Commutative: merge(s1, s2) = merge(s2, s1).
    // Associative: merge(merge(s1, s2), s3) = merge(s1, merge(s2, s3)).
    fn merge(&mut self, other: &GCounter) {
        for (&id, &count) in &other.counts {
            let entry = self.counts.entry(id).or_insert(0);
            if count > *entry { *entry = count; }
        }
    }
}

// PNCounter: increment and decrement via two G-Counters.
#[derive(Debug, Clone)]
struct PNCounter {
    node_id: usize,
    positive: GCounter,
    negative: GCounter,
}

impl PNCounter {
    fn new(node_id: usize) -> Self {
        PNCounter {
            node_id,
            positive: GCounter::new(node_id),
            negative: GCounter::new(node_id),
        }
    }

    fn increment(&mut self) { self.positive.increment(); }
    fn decrement(&mut self) { self.negative.increment(); }
    fn value(&self) -> i64 { self.positive.value() as i64 - self.negative.value() as i64 }

    fn merge(&mut self, other: &PNCounter) {
        self.positive.merge(&other.positive);
        self.negative.merge(&other.negative);
    }
}

// ORSet: observed-remove set with add-wins semantics.
// Each Add tags the element with a unique (node_id, seq) token.
// Remove deletes the observed tokens; concurrent adds survive.
#[derive(Debug, Clone)]
struct ORSet {
    node_id: usize,
    seq: u64,
    tokens: HashMap<String, HashSet<(usize, u64)>>,
}

impl ORSet {
    fn new(node_id: usize) -> Self {
        ORSet { node_id, seq: 0, tokens: HashMap::new() }
    }

    fn add(&mut self, element: &str) {
        self.seq += 1;
        let token = (self.node_id, self.seq);
        self.tokens.entry(element.to_string()).or_default().insert(token);
    }

    fn remove(&mut self, element: &str) {
        self.tokens.remove(element);
    }

    fn contains(&self, element: &str) -> bool {
        self.tokens.get(element).map(|t| !t.is_empty()).unwrap_or(false)
    }

    fn members(&self) -> Vec<&str> {
        self.tokens.iter()
            .filter(|(_, toks)| !toks.is_empty())
            .map(|(e, _)| e.as_str())
            .collect()
    }

    // merge is the lattice join: union of all token sets.
    fn merge(&mut self, other: &ORSet) {
        for (elem, toks) in &other.tokens {
            self.tokens.entry(elem.clone()).or_default().extend(toks.iter().copied());
        }
    }
}

// LWWElementSet: last-write-wins element set.
// add_ts > remove_ts → element present; remove_ts >= add_ts → absent.
#[derive(Debug, Clone, Default)]
struct LWWElementSet {
    adds: HashMap<String, i64>,
    removes: HashMap<String, i64>,
}

impl LWWElementSet {
    fn add(&mut self, element: &str, ts: i64) {
        let e = self.adds.entry(element.to_string()).or_insert(i64::MIN);
        if ts > *e { *e = ts; }
    }

    fn remove(&mut self, element: &str, ts: i64) {
        let e = self.removes.entry(element.to_string()).or_insert(i64::MIN);
        if ts > *e { *e = ts; }
    }

    fn contains(&self, element: &str) -> bool {
        let add_ts = self.adds.get(element).copied().unwrap_or(i64::MIN);
        let rem_ts = self.removes.get(element).copied().unwrap_or(i64::MIN);
        add_ts >= rem_ts && add_ts > i64::MIN
    }

    fn merge(&mut self, other: &LWWElementSet) {
        for (k, &ts) in &other.adds {
            let e = self.adds.entry(k.clone()).or_insert(i64::MIN);
            if ts > *e { *e = ts; }
        }
        for (k, &ts) in &other.removes {
            let e = self.removes.entry(k.clone()).or_insert(i64::MIN);
            if ts > *e { *e = ts; }
        }
    }
}

fn main() {
    println!("=== GCounter ===");
    let mut gc1 = GCounter::new(1);
    let mut gc2 = GCounter::new(2);
    gc1.increment(); gc1.increment();
    gc2.increment(); gc2.increment(); gc2.increment();
    gc1.merge(&gc2);
    println!("GCounter after merge: {} (expected 5)", gc1.value());

    // Idempotency: merging twice gives the same result
    let gc2_clone = gc2.clone();
    gc1.merge(&gc2_clone);
    println!("GCounter after second merge: {} (must still be 5)", gc1.value());

    println!("\n=== PNCounter ===");
    let mut pn1 = PNCounter::new(1);
    let mut pn2 = PNCounter::new(2);
    pn1.increment(); pn1.increment(); pn1.increment(); // +3
    pn2.decrement(); pn2.decrement();                   // -2
    pn1.merge(&pn2);
    println!("PNCounter: {} (expected 1)", pn1.value());

    println!("\n=== OR-Set ===");
    let mut s1 = ORSet::new(1);
    let mut s2 = ORSet::new(2);
    s1.add("alice");
    s2.add("bob");
    s1.remove("bob"); // concurrent: s1 hasn't seen s2's add token
    s1.merge(&s2);    // s2's add token for "bob" survives → add wins
    println!("Members: {:?}", s1.members());
    println!("Contains bob: {} (expected true — add wins)", s1.contains("bob"));

    println!("\n=== LWW-Element-Set ===");
    let mut lww1 = LWWElementSet::default();
    let mut lww2 = LWWElementSet::default();
    lww1.add("x", 10);
    lww2.remove("x", 15);
    lww1.merge(&lww2);
    println!("Contains x (add=10, remove=15): {} (expected false)", lww1.contains("x"));
    lww1.add("x", 20);
    println!("Contains x (add=20, remove=15): {} (expected true)", lww1.contains("x"));
}
```

### Rust-specific considerations

`HashMap<String, HashSet<(usize, u64)>>` for the OR-Set is idiomatic Rust: `HashSet` for unordered token sets, `HashMap` for the element index. The `#[derive(Clone)]` on `ORSet` allows shallow cloning for peer replication without custom code.

The `merge` method takes `&GCounter` (shared reference) and `&mut self` (exclusive reference), which the borrow checker enforces: you cannot merge a counter with itself because you cannot hold both `&mut self` and `&self` simultaneously. This is a compile-time prevention of a subtle bug (merging a counter with itself should be a no-op due to idempotency, but modifying during read would be a data race). In Go, this is a runtime concern guarded by the mutex.

`or_insert(i64::MIN)` for LWW timestamps is more precise than `or_insert(0)` — it makes the "never seen" state explicit as "negative infinity," which correctly handles the edge case of adding an element at timestamp 0.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Token set for OR-Set | `map[[2]uint64]struct{}` — map as set | `HashSet<(usize, u64)>` — dedicated set type |
| Merge method signature | `(g *GCounter) Merge(other *GCounter)` — both mutable | `(&mut self, other: &GCounter)` — enforced: self mutable, other read-only |
| Clone for replication | Manual `State()` method returning `map` copy | `#[derive(Clone)]` — automatic deep copy |
| Concurrent access | `sync.Mutex` per CRDT — explicit | `Arc<Mutex<CRDT>>` — ownership-tracked |
| Integer overflow | `uint64` silently wraps on overflow | Rust panics on overflow in debug, wraps in release (use `saturating_add` in production) |

## Production War Stories

**Riak's CRDT implementation**: Riak 2.0 (2014) shipped the first production CRDT library, implementing Counters (PN-Counter), Sets (OR-Set), Maps (nested CRDTs), Flags, and Registers. The design lesson from Riak's experience: OR-Sets become expensive when elements have high cardinality — a set with 10 million elements accumulates 10 million token entries over time unless compacted. Riak introduced "tombstone purging" (removing tokens for elements that have been removed by all replicas) as a garbage collection mechanism.

**Redis CRDT (Redis Enterprise)**: Redis Enterprise's Active-Active geo-replication uses CRDTs to allow writes to the same keys from different data centers simultaneously. The implementation: strings use LWW, counters use PN-Counter, sets use OR-Set, sorted sets use LWW per score. The tricky case is a sorted set that is both incremented (ZINCRBY) and set (ZADD) concurrently — Redis Enterprise resolves this with a "max wins" LWW on scores with a concurrent-increment counter, which is a custom hybrid CRDT.

**Figma's multiplayer editing**: Figma's real-time collaborative design tool uses operation-based CRDTs for document editing. The "operations" are document mutations (move node, change property, delete layer); commutativity is ensured by designing operations to have no conflicting effects when applied in any order. The key insight from Figma's engineering blog: op-based CRDTs work cleanly when operations are defined at a sufficiently high semantic level. Low-level operations (character-level text edits) require complex commutativity proofs; high-level operations (move this design element) commute naturally.

**SoundCloud's Roshi**: Roshi is a CRDT-based time-series event store built on Redis. Each event is an LWW element in a time-ordered set. The LWW semantics are "add at timestamp T" and "remove at timestamp T+1" (remove always has a higher timestamp than add). The insight: by choosing remove_ts = add_ts + 1, removes are guaranteed to win over concurrent re-adds, which matches the "delete is final" semantics of a social feed.

## Fault Model

| Scenario | CRDT behavior |
|---|---|
| Network partition | Both partitions accept writes; after partition heals, merge() converges to the same state — no data loss |
| Node crash and recovery | Node re-requests full state from any peer; merges to catch up — no coordination needed |
| Duplicate message delivery | Idempotent merge: receiving the same state twice produces the same result |
| Out-of-order delivery | Commutative merge: order of merges does not affect the final state |
| Byzantine node | Not handled: a Byzantine node can send a state that violates monotonicity (e.g., decrement a G-Counter), and the merge will incorrectly apply it. CRDTs assume crash-stop failures only |
| Clock skew (LWW CRDTs) | LWW-based CRDTs are vulnerable to clock skew: a node with a fast clock always wins. Prefer OR-Set over LWW-Element-Set for correctness-critical data |

**The key tradeoff**: CRDTs never block writes and always converge, at the cost of only supporting "monotone" operations. Operations that are naturally non-monotone (arbitrary delete, conditional update, compare-and-swap) cannot be expressed as CRDTs without workarounds. The OR-Set's "add wins" behavior on concurrent add/remove is the canonical example of a semantic compromise required to maintain monotonicity.

## Common Pitfalls

**Pitfall 1: Growing tokens in OR-Set without garbage collection**

Every `Add` operation creates a new unique token. A set that has 1 million elements added and removed over its lifetime accumulates 1 million tokens even if the current set is empty. Without tombstone garbage collection (periodically purging tokens for elements removed by all replicas), the OR-Set's memory grows unboundedly. Production OR-Set implementations require a "stable state" check: a token can be purged only after all replicas have merged the remove operation.

**Pitfall 2: Using LWW-Element-Set for correctness-critical data**

LWW is only safe when clock synchronization is tighter than the resolution of concurrent operations. If two clients concurrently add and remove an element within the same millisecond, and their clocks differ by 1ms, the "wrong" operation may win. Use OR-Set when add/remove correctness matters; use LWW only for approximate data (caches, recommendations, analytics).

**Pitfall 3: PN-Counter can go "below zero" unintentionally**

The PN-Counter allows the value to go negative (decrements from node 2 can exceed increments from node 1 after merge). If your invariant is "value ≥ 0" (e.g., available inventory), a PN-Counter will violate it. The correct solution: use a "bounded counter" CRDT that coordinates once (via a single master) when the balance is low, rather than allowing unconstrained decrements.

**Pitfall 4: Forgetting that state-based CRDTs require shipping full state**

A G-Counter on a cluster of 1,000 nodes ships a 1,000-element vector on every sync. If you have 100,000 counters and sync every 30 seconds, the bandwidth is 100,000 × 1,000 × 8 bytes × 2/30 ≈ 5 GB/s — clearly infeasible. Use delta CRDTs (ship only changed components) or op-based CRDTs with causal delivery for high-cardinality CRDT deployments.

**Pitfall 5: Merging CRDTs from different node ID namespaces**

When a node is replaced (old node 7 decommissioned, new node 7 added), the new node starts incrementing its local counter from 0. After merge, the global G-Counter may show `max(old_node_7_count, new_node_7_count)` — losing the old node's increments. Node IDs must be globally unique across the lifetime of the cluster. Use UUIDs rather than sequential integers.

## Exercises

**Exercise 1** (30 min): Verify the three CRDT properties (idempotency, commutativity, associativity) for the GCounter in Go. Write three functions that each test one property by creating multiple GCounters with random increments, performing random merge orderings, and asserting the final value is always the same.

**Exercise 2** (2-4h): Implement a Map CRDT (as used in Riak) where keys map to inner CRDTs (counters or flags). The Map CRDT's merge is: for each key, merge the inner CRDTs. Support `Remove(key)` by tracking which keys have been "tombstoned" with an OR-Set of key tokens. Implement and test in Go.

**Exercise 3** (4-8h): Implement a delta G-Counter: add a `Delta(since map[int]uint64) map[int]uint64` function that returns only the entries that changed since the caller's last known state. Verify that the receiving replica's `Merge(delta)` produces the same result as `Merge(full_state)`. Measure the bandwidth reduction for a 100-node cluster with 1% of nodes active at any time.

**Exercise 4** (8-15h): Implement a simple distributed collaborative text editor using operation-based CRDTs. Use a CRDT rope or sequence CRDT (e.g., Logoot or LSEQ) where each character has a globally unique position identifier. Two users can insert and delete characters concurrently; after merge, all replicas show the same document. Run three goroutines simulating concurrent edits, merge all operations, and verify the document is identical on all replicas.

## Further Reading

### Foundational Papers
- Shapiro, M. et al. (2011). "A Comprehensive Study of Convergent and Commutative Replicated Data Types." INRIA Technical Report. The definitive catalog of CRDT types with formal proofs of convergence. Section 3 covers the lattice theory; Sections 4-5 cover specific data types.
- Almeida, P.S. et al. (2016). "Delta State Replicated Data Types." *Journal of Parallel and Distributed Computing*. Introduces delta CRDTs; essential reading for bandwidth-efficient CRDT deployment.

### Books
- Kleppmann, M. (2017). *Designing Data-Intensive Applications*. Chapter 5 covers CRDTs in the context of multi-leader and leaderless replication. The shopping cart example makes the OR-Set semantics concrete.
- Shapiro, M. & Preguiça, N. (2010). "Designing a Commutative Replicated Data Type." INRIA Report. Shorter than the 2011 paper; better for a first reading.

### Production Code to Read
- `riak_dt` (https://github.com/basho/riak_dt) — Riak's Erlang CRDT library. `riak_dt_orswot.erl` (OR-Set without tombstones) and `riak_dt_pncounter.erl` are the reference implementations with production GC logic.
- `rust-crdt` (https://github.com/rust-crdt/rust-crdt) — Rust CRDT library. Implements G-Counter, OR-Set, LWW-Map, and others. The trait design (`CmRDT`, `CvRDT`) is a good architecture pattern.
- Redis Enterprise source (limited): The Redis CRDT whitepaper (https://redis.com/wp-content/uploads/2020/01/redis-active-active-technical-whitepaper.pdf) describes the production CRDT types with conflict resolution rules.

### Talks
- Shapiro, M. (2012): "CRDTs: Consistency Without Concurrency Control." Keynote at LADIS 2012. The clearest 30-minute introduction to the theory.
- Burckhardt, S. (2014): "Principles of Eventual Consistency." Microsoft Research. Covers the formal semantics of eventual consistency and CRDTs with a programming-language theory perspective.
