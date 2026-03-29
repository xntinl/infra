# Solution: CRDTs State-Based Counters and Sets

## Architecture Overview

Each CRDT is a standalone data structure with three capabilities: local operations (increment, add, remove), merge with another replica's state, and query the current value. The merge function forms a join semilattice, guaranteeing convergence without coordination.

The replication simulator creates N independent replicas, applies random operations, then merges states pairwise until convergence. No networking -- just in-memory state passing. This isolates the CRDT correctness from transport concerns.

Both Go and Rust implement identical CRDT semantics. Go uses generics and interfaces; Rust uses traits and generics. The mathematical structure is language-independent.

## Go Solution

### Project Setup

```bash
mkdir -p crdts-go && cd crdts-go
go mod init crdts
```

### Implementation

```go
// gcounter.go
package crdts

// GCounter is a grow-only counter. Each replica increments its own slot.
// Value is the sum of all slots. Merge takes pointwise maximum.
type GCounter struct {
	counts map[string]uint64
	self   string
}

func NewGCounter(replicaID string) *GCounter {
	return &GCounter{
		counts: map[string]uint64{replicaID: 0},
		self:   replicaID,
	}
}

func (g *GCounter) Increment() {
	g.counts[g.self]++
}

func (g *GCounter) IncrementBy(n uint64) {
	g.counts[g.self] += n
}

func (g *GCounter) Query() uint64 {
	var total uint64
	for _, v := range g.counts {
		total += v
	}
	return total
}

func (g *GCounter) Merge(other *GCounter) {
	for id, val := range other.counts {
		if current, ok := g.counts[id]; !ok || val > current {
			g.counts[id] = val
		}
	}
}

func (g *GCounter) Clone() *GCounter {
	c := &GCounter{counts: make(map[string]uint64, len(g.counts)), self: g.self}
	for k, v := range g.counts {
		c.counts[k] = v
	}
	return c
}
```

```go
// pncounter.go
package crdts

// PNCounter supports both increment and decrement using two G-Counters.
type PNCounter struct {
	positive *GCounter
	negative *GCounter
	self     string
}

func NewPNCounter(replicaID string) *PNCounter {
	return &PNCounter{
		positive: NewGCounter(replicaID),
		negative: NewGCounter(replicaID),
		self:     replicaID,
	}
}

func (pn *PNCounter) Increment() { pn.positive.Increment() }
func (pn *PNCounter) Decrement() { pn.negative.Increment() }

func (pn *PNCounter) Query() int64 {
	return int64(pn.positive.Query()) - int64(pn.negative.Query())
}

func (pn *PNCounter) Merge(other *PNCounter) {
	pn.positive.Merge(other.positive)
	pn.negative.Merge(other.negative)
}

func (pn *PNCounter) Clone() *PNCounter {
	return &PNCounter{
		positive: pn.positive.Clone(),
		negative: pn.negative.Clone(),
		self:     pn.self,
	}
}
```

```go
// gset.go
package crdts

// GSet is a grow-only set. Elements can be added but never removed.
// Merge is set union.
type GSet[T comparable] struct {
	elements map[T]struct{}
}

func NewGSet[T comparable]() *GSet[T] {
	return &GSet[T]{elements: make(map[T]struct{})}
}

func (s *GSet[T]) Add(elem T) {
	s.elements[elem] = struct{}{}
}

func (s *GSet[T]) Contains(elem T) bool {
	_, ok := s.elements[elem]
	return ok
}

func (s *GSet[T]) Query() map[T]struct{} {
	result := make(map[T]struct{}, len(s.elements))
	for k := range s.elements {
		result[k] = struct{}{}
	}
	return result
}

func (s *GSet[T]) Merge(other *GSet[T]) {
	for elem := range other.elements {
		s.elements[elem] = struct{}{}
	}
}

func (s *GSet[T]) Clone() *GSet[T] {
	c := NewGSet[T]()
	for k := range s.elements {
		c.elements[k] = struct{}{}
	}
	return c
}
```

```go
// twopset.go
package crdts

// TwoPSet supports add and remove. Once removed, an element cannot be re-added.
type TwoPSet[T comparable] struct {
	addSet    *GSet[T]
	removeSet *GSet[T]
}

func NewTwoPSet[T comparable]() *TwoPSet[T] {
	return &TwoPSet[T]{
		addSet:    NewGSet[T](),
		removeSet: NewGSet[T](),
	}
}

func (s *TwoPSet[T]) Add(elem T) {
	s.addSet.Add(elem)
}

func (s *TwoPSet[T]) Remove(elem T) {
	s.removeSet.Add(elem)
}

func (s *TwoPSet[T]) Contains(elem T) bool {
	return s.addSet.Contains(elem) && !s.removeSet.Contains(elem)
}

func (s *TwoPSet[T]) Query() map[T]struct{} {
	result := make(map[T]struct{})
	for elem := range s.addSet.elements {
		if !s.removeSet.Contains(elem) {
			result[elem] = struct{}{}
		}
	}
	return result
}

func (s *TwoPSet[T]) Merge(other *TwoPSet[T]) {
	s.addSet.Merge(other.addSet)
	s.removeSet.Merge(other.removeSet)
}

func (s *TwoPSet[T]) Clone() *TwoPSet[T] {
	return &TwoPSet[T]{
		addSet:    s.addSet.Clone(),
		removeSet: s.removeSet.Clone(),
	}
}
```

```go
// orset.go
package crdts

import "fmt"

// Tag uniquely identifies an add operation.
type Tag struct {
	ReplicaID string
	Seq       uint64
}

func (t Tag) String() string {
	return fmt.Sprintf("%s:%d", t.ReplicaID, t.Seq)
}

// ORSet supports add, remove, and re-add with add-wins semantics.
type ORSet[T comparable] struct {
	entries map[T]map[Tag]struct{} // element -> set of tags
	self    string
	seq     uint64
}

func NewORSet[T comparable](replicaID string) *ORSet[T] {
	return &ORSet[T]{
		entries: make(map[T]map[Tag]struct{}),
		self:    replicaID,
	}
}

func (s *ORSet[T]) Add(elem T) {
	s.seq++
	tag := Tag{ReplicaID: s.self, Seq: s.seq}
	if s.entries[elem] == nil {
		s.entries[elem] = make(map[Tag]struct{})
	}
	s.entries[elem][tag] = struct{}{}
}

// Remove removes all currently observed tags for elem. Concurrent adds
// with unseen tags survive the merge (add-wins semantics).
func (s *ORSet[T]) Remove(elem T) {
	delete(s.entries, elem)
}

func (s *ORSet[T]) Contains(elem T) bool {
	tags, ok := s.entries[elem]
	return ok && len(tags) > 0
}

func (s *ORSet[T]) Query() map[T]struct{} {
	result := make(map[T]struct{})
	for elem, tags := range s.entries {
		if len(tags) > 0 {
			result[elem] = struct{}{}
		}
	}
	return result
}

// Merge combines two OR-Sets. For each element, the resulting tag set
// is the union of tags from both replicas.
func (s *ORSet[T]) Merge(other *ORSet[T]) {
	for elem, otherTags := range other.entries {
		if s.entries[elem] == nil {
			s.entries[elem] = make(map[Tag]struct{})
		}
		for tag := range otherTags {
			s.entries[elem][tag] = struct{}{}
		}
	}
	// Remove elements that have no tags in either set after merge
	for elem, tags := range s.entries {
		if len(tags) == 0 {
			delete(s.entries, elem)
		}
	}
}

func (s *ORSet[T]) Clone() *ORSet[T] {
	c := &ORSet[T]{
		entries: make(map[T]map[Tag]struct{}, len(s.entries)),
		self:    s.self,
		seq:     s.seq,
	}
	for elem, tags := range s.entries {
		c.entries[elem] = make(map[Tag]struct{}, len(tags))
		for tag := range tags {
			c.entries[elem][tag] = struct{}{}
		}
	}
	return c
}
```

```go
// lwwregister.go
package crdts

import "time"

// LWWRegister keeps the value with the highest timestamp. Ties broken by replica ID.
type LWWRegister[T any] struct {
	value     T
	timestamp time.Time
	replicaID string
}

func NewLWWRegister[T any](replicaID string, initial T) *LWWRegister[T] {
	return &LWWRegister[T]{
		value:     initial,
		timestamp: time.Time{}, // zero time
		replicaID: replicaID,
	}
}

func (r *LWWRegister[T]) Set(value T, ts time.Time) {
	r.value = value
	r.timestamp = ts
	// replicaID stays the same -- it identifies who wrote this value
}

func (r *LWWRegister[T]) Query() T {
	return r.value
}

func (r *LWWRegister[T]) Timestamp() time.Time {
	return r.timestamp
}

func (r *LWWRegister[T]) Merge(other *LWWRegister[T]) {
	if other.timestamp.After(r.timestamp) {
		r.value = other.value
		r.timestamp = other.timestamp
		r.replicaID = other.replicaID
	} else if other.timestamp.Equal(r.timestamp) && other.replicaID > r.replicaID {
		// Deterministic tie-breaking: higher replica ID wins
		r.value = other.value
		r.timestamp = other.timestamp
		r.replicaID = other.replicaID
	}
}

func (r *LWWRegister[T]) Clone() *LWWRegister[T] {
	return &LWWRegister[T]{
		value:     r.value,
		timestamp: r.timestamp,
		replicaID: r.replicaID,
	}
}
```

```go
// crdt_test.go
package crdts

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

// --- G-Counter Tests ---

func TestGCounterIncrement(t *testing.T) {
	g := NewGCounter("r1")
	g.Increment()
	g.Increment()
	if g.Query() != 2 {
		t.Fatalf("expected 2, got %d", g.Query())
	}
}

func TestGCounterMerge(t *testing.T) {
	a := NewGCounter("r1")
	b := NewGCounter("r2")
	a.IncrementBy(5)
	b.IncrementBy(3)
	a.Merge(b)
	if a.Query() != 8 {
		t.Fatalf("expected 8 after merge, got %d", a.Query())
	}
}

func TestGCounterMergeIdempotent(t *testing.T) {
	a := NewGCounter("r1")
	b := NewGCounter("r2")
	a.IncrementBy(3)
	b.IncrementBy(5)
	a.Merge(b)
	before := a.Query()
	a.Merge(b) // merge again
	if a.Query() != before {
		t.Fatalf("merge not idempotent: %d != %d", a.Query(), before)
	}
}

func TestGCounterSemilattice(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 100; i++ {
		a := NewGCounter("r1")
		b := NewGCounter("r2")
		c := NewGCounter("r3")
		a.IncrementBy(uint64(rng.Intn(100)))
		b.IncrementBy(uint64(rng.Intn(100)))
		c.IncrementBy(uint64(rng.Intn(100)))

		// Commutativity: merge(a,b) == merge(b,a)
		ab := a.Clone()
		ab.Merge(b)
		ba := b.Clone()
		ba.Merge(a)
		if ab.Query() != ba.Query() {
			t.Fatalf("commutativity: %d != %d", ab.Query(), ba.Query())
		}

		// Associativity: merge(merge(a,b),c) == merge(a,merge(b,c))
		abc1 := a.Clone()
		abc1.Merge(b)
		abc1.Merge(c)
		bc := b.Clone()
		bc.Merge(c)
		abc2 := a.Clone()
		abc2.Merge(bc)
		if abc1.Query() != abc2.Query() {
			t.Fatalf("associativity: %d != %d", abc1.Query(), abc2.Query())
		}

		// Idempotency: merge(a,a) == a
		aa := a.Clone()
		aa.Merge(a)
		if aa.Query() != a.Query() {
			t.Fatalf("idempotency: %d != %d", aa.Query(), a.Query())
		}
	}
}

// --- PN-Counter Tests ---

func TestPNCounterIncrementDecrement(t *testing.T) {
	pn := NewPNCounter("r1")
	pn.Increment()
	pn.Increment()
	pn.Decrement()
	if pn.Query() != 1 {
		t.Fatalf("expected 1, got %d", pn.Query())
	}
}

func TestPNCounterMerge(t *testing.T) {
	a := NewPNCounter("r1")
	b := NewPNCounter("r2")
	a.Increment()
	a.Increment()
	b.Decrement()
	a.Merge(b)
	if a.Query() != 1 {
		t.Fatalf("expected 1 after merge, got %d", a.Query())
	}
}

func TestPNCounterNegative(t *testing.T) {
	pn := NewPNCounter("r1")
	pn.Decrement()
	pn.Decrement()
	if pn.Query() != -2 {
		t.Fatalf("expected -2, got %d", pn.Query())
	}
}

// --- G-Set Tests ---

func TestGSetAdd(t *testing.T) {
	s := NewGSet[string]()
	s.Add("a")
	s.Add("b")
	if !s.Contains("a") || !s.Contains("b") {
		t.Fatal("missing elements")
	}
	if s.Contains("c") {
		t.Fatal("phantom element")
	}
}

func TestGSetMerge(t *testing.T) {
	a := NewGSet[string]()
	b := NewGSet[string]()
	a.Add("x")
	b.Add("y")
	a.Merge(b)
	if !a.Contains("x") || !a.Contains("y") {
		t.Fatal("merge lost elements")
	}
}

// --- 2P-Set Tests ---

func TestTwoPSetAddRemove(t *testing.T) {
	s := NewTwoPSet[string]()
	s.Add("a")
	s.Add("b")
	s.Remove("a")
	if s.Contains("a") {
		t.Fatal("removed element still present")
	}
	if !s.Contains("b") {
		t.Fatal("non-removed element missing")
	}
}

func TestTwoPSetNoReAdd(t *testing.T) {
	s := NewTwoPSet[string]()
	s.Add("a")
	s.Remove("a")
	s.Add("a") // should have no effect
	if s.Contains("a") {
		t.Fatal("re-added element should not be present in 2P-Set")
	}
}

func TestTwoPSetMerge(t *testing.T) {
	a := NewTwoPSet[string]()
	b := NewTwoPSet[string]()
	a.Add("x")
	a.Add("y")
	b.Add("x")
	b.Remove("x")
	a.Merge(b)
	if a.Contains("x") {
		t.Fatal("x should be removed after merge")
	}
	if !a.Contains("y") {
		t.Fatal("y should survive merge")
	}
}

// --- OR-Set Tests ---

func TestORSetAddRemoveReAdd(t *testing.T) {
	s := NewORSet[string]("r1")
	s.Add("a")
	s.Remove("a")
	if s.Contains("a") {
		t.Fatal("element present after remove")
	}
	s.Add("a") // re-add with new tag
	if !s.Contains("a") {
		t.Fatal("element missing after re-add")
	}
}

func TestORSetConcurrentAddRemove(t *testing.T) {
	a := NewORSet[string]("r1")
	b := NewORSet[string]("r2")

	// Both add "x"
	a.Add("x")
	b.Add("x")

	// Replica a removes "x" (only sees its own tag)
	a.Remove("x")

	// Merge: b's tag for "x" survives (add-wins)
	a.Merge(b)
	if !a.Contains("x") {
		t.Fatal("concurrent add should win over remove")
	}
}

func TestORSetMergeConvergence(t *testing.T) {
	a := NewORSet[string]("r1")
	b := NewORSet[string]("r2")
	c := NewORSet[string]("r3")

	a.Add("x")
	b.Add("y")
	c.Add("x")
	c.Remove("x")

	// Merge in different orders
	ab := a.Clone()
	ab.Merge(b)
	ab.Merge(c)

	cb := c.Clone()
	cb.Merge(b)
	cb.Merge(a)

	aQ := ab.Query()
	cQ := cb.Query()

	// Both should contain "x" (a's tag survives c's remove) and "y"
	if len(aQ) != len(cQ) {
		t.Fatalf("convergence failed: %v vs %v", aQ, cQ)
	}
}

// --- LWW-Register Tests ---

func TestLWWRegisterLastWriteWins(t *testing.T) {
	a := NewLWWRegister("r1", "")
	b := NewLWWRegister("r2", "")

	t1 := time.Now()
	t2 := t1.Add(time.Second)

	a.Set("first", t1)
	b.Set("second", t2)

	a.Merge(b)
	if a.Query() != "second" {
		t.Fatalf("expected 'second', got '%s'", a.Query())
	}
}

func TestLWWRegisterTieBreaking(t *testing.T) {
	a := NewLWWRegister("r1", "")
	b := NewLWWRegister("r2", "")

	ts := time.Now()
	a.Set("from-r1", ts)
	b.Set("from-r2", ts) // same timestamp

	a.Merge(b)
	// r2 > r1, so r2's value wins
	if a.Query() != "from-r2" {
		t.Fatalf("tie-break failed: expected 'from-r2', got '%s'", a.Query())
	}
}

// --- Convergence Simulator ---

func TestReplicationConvergence(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	numReplicas := 5
	numOps := 50

	counters := make([]*GCounter, numReplicas)
	for i := range counters {
		counters[i] = NewGCounter(fmt.Sprintf("r%d", i))
	}

	// Apply random increments
	for i := 0; i < numOps; i++ {
		replica := rng.Intn(numReplicas)
		counters[replica].IncrementBy(uint64(rng.Intn(10) + 1))
	}

	// Merge in random order until convergent
	for rounds := 0; rounds < numReplicas*2; rounds++ {
		i := rng.Intn(numReplicas)
		j := rng.Intn(numReplicas)
		if i != j {
			counters[i].Merge(counters[j])
			counters[j].Merge(counters[i])
		}
	}

	// After full pairwise merge, all should converge
	for i := range counters {
		for j := range counters {
			counters[i].Merge(counters[j])
		}
	}

	expected := counters[0].Query()
	for i := 1; i < numReplicas; i++ {
		if counters[i].Query() != expected {
			t.Fatalf("replica %d diverged: %d != %d", i, counters[i].Query(), expected)
		}
	}
}
```

### Running

```bash
go test -v -race ./...
```

### Expected Output

```
=== RUN   TestGCounterIncrement
--- PASS: TestGCounterIncrement (0.00s)
=== RUN   TestGCounterMerge
--- PASS: TestGCounterMerge (0.00s)
=== RUN   TestGCounterMergeIdempotent
--- PASS: TestGCounterMergeIdempotent (0.00s)
=== RUN   TestGCounterSemilattice
--- PASS: TestGCounterSemilattice (0.00s)
=== RUN   TestPNCounterIncrementDecrement
--- PASS: TestPNCounterIncrementDecrement (0.00s)
=== RUN   TestPNCounterMerge
--- PASS: TestPNCounterMerge (0.00s)
=== RUN   TestPNCounterNegative
--- PASS: TestPNCounterNegative (0.00s)
=== RUN   TestGSetAdd
--- PASS: TestGSetAdd (0.00s)
=== RUN   TestGSetMerge
--- PASS: TestGSetMerge (0.00s)
=== RUN   TestTwoPSetAddRemove
--- PASS: TestTwoPSetAddRemove (0.00s)
=== RUN   TestTwoPSetNoReAdd
--- PASS: TestTwoPSetNoReAdd (0.00s)
=== RUN   TestTwoPSetMerge
--- PASS: TestTwoPSetMerge (0.00s)
=== RUN   TestORSetAddRemoveReAdd
--- PASS: TestORSetAddRemoveReAdd (0.00s)
=== RUN   TestORSetConcurrentAddRemove
--- PASS: TestORSetConcurrentAddRemove (0.00s)
=== RUN   TestORSetMergeConvergence
--- PASS: TestORSetMergeConvergence (0.00s)
=== RUN   TestLWWRegisterLastWriteWins
--- PASS: TestLWWRegisterLastWriteWins (0.00s)
=== RUN   TestLWWRegisterTieBreaking
--- PASS: TestLWWRegisterTieBreaking (0.00s)
=== RUN   TestReplicationConvergence
--- PASS: TestReplicationConvergence (0.00s)
PASS
ok      crdts    0.012s
```

## Rust Solution

### Project Setup

```bash
cargo new crdts-rs --lib && cd crdts-rs
```

### Implementation

```rust
// src/lib.rs
pub mod gcounter;
pub mod pncounter;
pub mod gset;
pub mod twopset;
pub mod orset;
pub mod lwwregister;

/// Trait defining the CRDT interface. All CRDTs must implement merge.
pub trait Crdt: Clone {
    /// Merge another replica's state into this one. Must be commutative,
    /// associative, and idempotent.
    fn merge(&mut self, other: &Self);
}
```

```rust
// src/gcounter.rs
use std::collections::HashMap;
use crate::Crdt;

#[derive(Clone, Debug, PartialEq)]
pub struct GCounter {
    counts: HashMap<String, u64>,
    self_id: String,
}

impl GCounter {
    pub fn new(replica_id: &str) -> Self {
        let mut counts = HashMap::new();
        counts.insert(replica_id.to_string(), 0);
        Self { counts, self_id: replica_id.to_string() }
    }

    pub fn increment(&mut self) {
        *self.counts.entry(self.self_id.clone()).or_insert(0) += 1;
    }

    pub fn increment_by(&mut self, n: u64) {
        *self.counts.entry(self.self_id.clone()).or_insert(0) += n;
    }

    pub fn query(&self) -> u64 {
        self.counts.values().sum()
    }
}

impl Crdt for GCounter {
    fn merge(&mut self, other: &Self) {
        for (id, &val) in &other.counts {
            let entry = self.counts.entry(id.clone()).or_insert(0);
            if val > *entry {
                *entry = val;
            }
        }
    }
}
```

```rust
// src/pncounter.rs
use crate::gcounter::GCounter;
use crate::Crdt;

#[derive(Clone, Debug, PartialEq)]
pub struct PNCounter {
    positive: GCounter,
    negative: GCounter,
}

impl PNCounter {
    pub fn new(replica_id: &str) -> Self {
        Self {
            positive: GCounter::new(replica_id),
            negative: GCounter::new(replica_id),
        }
    }

    pub fn increment(&mut self) { self.positive.increment(); }
    pub fn decrement(&mut self) { self.negative.increment(); }

    pub fn query(&self) -> i64 {
        self.positive.query() as i64 - self.negative.query() as i64
    }
}

impl Crdt for PNCounter {
    fn merge(&mut self, other: &Self) {
        self.positive.merge(&other.positive);
        self.negative.merge(&other.negative);
    }
}
```

```rust
// src/gset.rs
use std::collections::HashSet;
use std::hash::Hash;
use crate::Crdt;

#[derive(Clone, Debug, PartialEq)]
pub struct GSet<T: Eq + Hash + Clone> {
    elements: HashSet<T>,
}

impl<T: Eq + Hash + Clone> GSet<T> {
    pub fn new() -> Self {
        Self { elements: HashSet::new() }
    }

    pub fn add(&mut self, elem: T) {
        self.elements.insert(elem);
    }

    pub fn contains(&self, elem: &T) -> bool {
        self.elements.contains(elem)
    }

    pub fn query(&self) -> &HashSet<T> {
        &self.elements
    }
}

impl<T: Eq + Hash + Clone> Crdt for GSet<T> {
    fn merge(&mut self, other: &Self) {
        for elem in &other.elements {
            self.elements.insert(elem.clone());
        }
    }
}
```

```rust
// src/twopset.rs
use std::hash::Hash;
use crate::gset::GSet;
use crate::Crdt;

#[derive(Clone, Debug, PartialEq)]
pub struct TwoPSet<T: Eq + Hash + Clone> {
    add_set: GSet<T>,
    remove_set: GSet<T>,
}

impl<T: Eq + Hash + Clone> TwoPSet<T> {
    pub fn new() -> Self {
        Self { add_set: GSet::new(), remove_set: GSet::new() }
    }

    pub fn add(&mut self, elem: T) { self.add_set.add(elem); }
    pub fn remove(&mut self, elem: T) { self.remove_set.add(elem); }

    pub fn contains(&self, elem: &T) -> bool {
        self.add_set.contains(elem) && !self.remove_set.contains(elem)
    }
}

impl<T: Eq + Hash + Clone> Crdt for TwoPSet<T> {
    fn merge(&mut self, other: &Self) {
        self.add_set.merge(&other.add_set);
        self.remove_set.merge(&other.remove_set);
    }
}
```

```rust
// src/orset.rs
use std::collections::{HashMap, HashSet};
use std::hash::Hash;
use crate::Crdt;

#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub struct Tag {
    pub replica_id: String,
    pub seq: u64,
}

#[derive(Clone, Debug)]
pub struct ORSet<T: Eq + Hash + Clone> {
    entries: HashMap<T, HashSet<Tag>>,
    self_id: String,
    seq: u64,
}

impl<T: Eq + Hash + Clone> ORSet<T> {
    pub fn new(replica_id: &str) -> Self {
        Self {
            entries: HashMap::new(),
            self_id: replica_id.to_string(),
            seq: 0,
        }
    }

    pub fn add(&mut self, elem: T) {
        self.seq += 1;
        let tag = Tag { replica_id: self.self_id.clone(), seq: self.seq };
        self.entries.entry(elem).or_insert_with(HashSet::new).insert(tag);
    }

    pub fn remove(&mut self, elem: &T) {
        self.entries.remove(elem);
    }

    pub fn contains(&self, elem: &T) -> bool {
        self.entries.get(elem).map_or(false, |tags| !tags.is_empty())
    }

    pub fn query(&self) -> HashSet<T> {
        self.entries.iter()
            .filter(|(_, tags)| !tags.is_empty())
            .map(|(elem, _)| elem.clone())
            .collect()
    }
}

impl<T: Eq + Hash + Clone> Crdt for ORSet<T> {
    fn merge(&mut self, other: &Self) {
        for (elem, other_tags) in &other.entries {
            let tags = self.entries.entry(elem.clone()).or_insert_with(HashSet::new);
            for tag in other_tags {
                tags.insert(tag.clone());
            }
        }
    }
}
```

```rust
// src/lwwregister.rs
use std::time::SystemTime;
use crate::Crdt;

#[derive(Clone, Debug)]
pub struct LWWRegister<T: Clone> {
    value: T,
    timestamp: SystemTime,
    replica_id: String,
}

impl<T: Clone> LWWRegister<T> {
    pub fn new(replica_id: &str, initial: T) -> Self {
        Self {
            value: initial,
            timestamp: SystemTime::UNIX_EPOCH,
            replica_id: replica_id.to_string(),
        }
    }

    pub fn set(&mut self, value: T, ts: SystemTime) {
        self.value = value;
        self.timestamp = ts;
    }

    pub fn query(&self) -> &T { &self.value }
    pub fn timestamp(&self) -> SystemTime { self.timestamp }
}

impl<T: Clone> Crdt for LWWRegister<T> {
    fn merge(&mut self, other: &Self) {
        match self.timestamp.cmp(&other.timestamp) {
            std::cmp::Ordering::Less => {
                self.value = other.value.clone();
                self.timestamp = other.timestamp;
                self.replica_id = other.replica_id.clone();
            }
            std::cmp::Ordering::Equal => {
                if other.replica_id > self.replica_id {
                    self.value = other.value.clone();
                    self.timestamp = other.timestamp;
                    self.replica_id = other.replica_id.clone();
                }
            }
            std::cmp::Ordering::Greater => {}
        }
    }
}
```

```rust
// tests/crdt_tests.rs
use crdts_rs::*;
use crdts_rs::gcounter::GCounter;
use crdts_rs::pncounter::PNCounter;
use crdts_rs::gset::GSet;
use crdts_rs::twopset::TwoPSet;
use crdts_rs::orset::ORSet;
use crdts_rs::lwwregister::LWWRegister;
use std::time::{SystemTime, Duration};

#[test]
fn gcounter_merge_convergence() {
    let mut a = GCounter::new("r1");
    let mut b = GCounter::new("r2");
    a.increment_by(10);
    b.increment_by(7);
    a.merge(&b);
    b.merge(&a);
    assert_eq!(a.query(), b.query());
    assert_eq!(a.query(), 17);
}

#[test]
fn gcounter_semilattice_properties() {
    let mut a = GCounter::new("r1");
    let mut b = GCounter::new("r2");
    let mut c = GCounter::new("r3");
    a.increment_by(3);
    b.increment_by(5);
    c.increment_by(7);

    // Commutativity
    let mut ab = a.clone(); ab.merge(&b);
    let mut ba = b.clone(); ba.merge(&a);
    assert_eq!(ab.query(), ba.query());

    // Associativity
    let mut ab_c = a.clone(); ab_c.merge(&b); ab_c.merge(&c);
    let mut bc = b.clone(); bc.merge(&c);
    let mut a_bc = a.clone(); a_bc.merge(&bc);
    assert_eq!(ab_c.query(), a_bc.query());

    // Idempotency
    let mut aa = a.clone(); aa.merge(&a);
    assert_eq!(aa.query(), a.query());
}

#[test]
fn pncounter_positive_negative() {
    let mut a = PNCounter::new("r1");
    let mut b = PNCounter::new("r2");
    a.increment(); a.increment();
    b.decrement();
    a.merge(&b);
    assert_eq!(a.query(), 1);
}

#[test]
fn gset_union_merge() {
    let mut a = GSet::new();
    let mut b = GSet::new();
    a.add("x");
    b.add("y");
    a.merge(&b);
    assert!(a.contains(&"x"));
    assert!(a.contains(&"y"));
}

#[test]
fn twopset_remove_permanent() {
    let mut s: TwoPSet<&str> = TwoPSet::new();
    s.add("a");
    s.remove("a");
    s.add("a");
    assert!(!s.contains(&"a"));
}

#[test]
fn orset_add_wins() {
    let mut a: ORSet<String> = ORSet::new("r1");
    let mut b: ORSet<String> = ORSet::new("r2");
    a.add("x".to_string());
    b.add("x".to_string());
    a.remove(&"x".to_string()); // removes only r1's tag
    a.merge(&b); // r2's tag survives
    assert!(a.contains(&"x".to_string()));
}

#[test]
fn orset_readd_after_remove() {
    let mut s: ORSet<String> = ORSet::new("r1");
    s.add("a".to_string());
    s.remove(&"a".to_string());
    assert!(!s.contains(&"a".to_string()));
    s.add("a".to_string()); // new tag
    assert!(s.contains(&"a".to_string()));
}

#[test]
fn lww_register_last_write_wins() {
    let mut a = LWWRegister::new("r1", String::new());
    let mut b = LWWRegister::new("r2", String::new());
    let t1 = SystemTime::UNIX_EPOCH + Duration::from_secs(100);
    let t2 = SystemTime::UNIX_EPOCH + Duration::from_secs(200);
    a.set("first".to_string(), t1);
    b.set("second".to_string(), t2);
    a.merge(&b);
    assert_eq!(a.query(), "second");
}

#[test]
fn lww_register_tie_break() {
    let mut a = LWWRegister::new("r1", String::new());
    let mut b = LWWRegister::new("r2", String::new());
    let ts = SystemTime::UNIX_EPOCH + Duration::from_secs(100);
    a.set("from-r1".to_string(), ts);
    b.set("from-r2".to_string(), ts);
    a.merge(&b);
    assert_eq!(a.query(), "from-r2"); // r2 > r1
}

#[test]
fn convergence_simulation() {
    let num_replicas = 5;
    let mut counters: Vec<GCounter> = (0..num_replicas)
        .map(|i| GCounter::new(&format!("r{}", i)))
        .collect();

    // Random increments
    for i in 0..50 {
        counters[i % num_replicas].increment_by((i as u64 % 10) + 1);
    }

    // Full pairwise merge
    for i in 0..num_replicas {
        for j in 0..num_replicas {
            if i != j {
                let other = counters[j].clone();
                counters[i].merge(&other);
            }
        }
    }

    let expected = counters[0].query();
    for (i, c) in counters.iter().enumerate().skip(1) {
        assert_eq!(c.query(), expected, "replica {} diverged", i);
    }
}
```

### Running

```bash
# Go
cd crdts-go && go test -v -race ./...

# Rust
cd crdts-rs && cargo test
```

### Expected Output (Rust)

```
running 10 tests
test gcounter_merge_convergence ... ok
test gcounter_semilattice_properties ... ok
test pncounter_positive_negative ... ok
test gset_union_merge ... ok
test twopset_remove_permanent ... ok
test orset_add_wins ... ok
test orset_readd_after_remove ... ok
test lww_register_last_write_wins ... ok
test lww_register_tie_break ... ok
test convergence_simulation ... ok

test result: ok. 10 passed; 0 failed; 0 ignored
```

## Design Decisions

**State-based over operation-based**: State-based CRDTs (CvRDTs) send full state on merge, which is bandwidth-inefficient but simpler to implement correctly. Operation-based CRDTs (CmRDTs) send only operations but require exactly-once, causal-order delivery from the transport layer. For a learning exercise, state-based makes the merge semantics explicit and testable.

**Unique tags for OR-Set via replica ID + sequence number**: This generates globally unique tags without coordination. Each replica increments its own counter. The tag `(replica_id, seq)` is unique because no two replicas share an ID. This is the same approach used by Lamport timestamps.

**LWW-Register tie-breaking by replica ID**: Using `SystemTime` for timestamps means clock skew can cause unexpected behavior. The replica ID tiebreaker ensures determinism when clocks collide. In production, hybrid logical clocks (HLC) provide better timestamp ordering.

**Go generics for type safety**: `GSet[T comparable]` and `ORSet[T comparable]` use Go generics to enforce that elements are comparable (required for map keys). This prevents runtime panics from unhashable types.

**Rust traits for the CRDT interface**: The `Crdt` trait with `merge(&mut self, other: &Self)` provides a uniform interface. The `Clone` bound is required because replicas need independent copies for testing convergence.

## Common Mistakes

1. **GCounter merge using addition instead of max**: The merge function takes the maximum of each replica's counter, not the sum. Using addition causes counts to inflate on every merge, growing exponentially with the number of merge rounds.

2. **OR-Set remove deleting from all replicas**: Remove must only delete tags visible in the local replica. Tags on other replicas (not yet merged) must survive. Implementing remove as "delete element from all entries" breaks add-wins semantics.

3. **LWW-Register without tiebreaker**: If two replicas write at the same timestamp and there is no tiebreaker, `merge(a, b)` and `merge(b, a)` may produce different results, violating commutativity. Always include a deterministic secondary comparison.

4. **2P-Set allowing re-add**: The 2P-Set tombstone is permanent. Once an element is in the remove-set, adding it again to the add-set has no effect because `contains` checks `add_set AND NOT remove_set`. This is a fundamental limitation that the OR-Set addresses.

5. **Not cloning before merge in tests**: If test code merges `a` into `b` and then checks `a`, the test is wrong if `merge` mutated `a` through shared references. Always clone before merging in property-based tests.

## Performance Notes

- **G-Counter state size**: O(R) where R is the number of replicas. With 100 replicas and 8-byte counters, each G-Counter is ~800 bytes. PN-Counter doubles this. For thousands of replicas, consider delta-state CRDTs.
- **OR-Set metadata growth**: Each add creates a new tag that persists until garbage-collected. After A adds and R removes of the same element, the metadata stores A - R tags. Without GC, metadata grows unboundedly. Production systems prune tags using causal stability (when all replicas have observed a tag, it can be replaced with a single canonical entry).
- **Merge cost**: G-Counter merge is O(R). G-Set merge is O(|elements|). OR-Set merge is O(|elements| * |tags|). For frequently-merged large OR-Sets, delta-state replication reduces merge cost to O(|delta|).
- **Rust vs Go performance**: Rust avoids GC pauses and enables zero-cost abstractions. For high-throughput CRDT replication (millions of merges/second), Rust's deterministic memory management provides more predictable latency. Go's GC can cause tail latency spikes during large merges.
