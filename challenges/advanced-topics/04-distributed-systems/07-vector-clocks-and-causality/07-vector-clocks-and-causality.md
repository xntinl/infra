<!--
type: reference
difficulty: advanced
section: [04-distributed-systems]
concepts: [vector-clocks, lamport-timestamps, happens-before, causality, version-vectors, hybrid-logical-clocks, causal-ordering]
languages: [go, rust]
estimated_reading_time: 60 min
bloom_level: analyze
prerequisites: [eventual-consistency, distributed-systems-basics]
papers: [lamport-1978-time-clocks, fidge-1988-vector-clocks, kulkarni-2014-hlc]
industry_use: [riak-version-vectors, cockroachdb-hlc, cassandra-write-timestamps, amazon-dynamodb-context]
language_contrast: low
-->

# Vector Clocks and Causality

> Lamport's insight is that you do not need synchronized clocks to define time in a distributed system — you only need a relation that agrees with causality: if event A caused event B, then A's timestamp is less than B's. Physical clocks are expensive to synchronize; causal order can be maintained cheaply with logical counters.

## Mental Model

In a single-threaded program, events have a total order: the program counter advances from one line to the next, and "happened before" is simply "executed on an earlier line." In a distributed system, there is no shared program counter. Two nodes execute instructions independently, and the only "happened before" relationships that exist are those established by messages: if node A sends a message and node B receives it, then everything on A before the send happened before everything on B after the receive.

Lamport timestamps formalize this: each node maintains a counter that it increments on every event. When a message is sent, the counter is included. When a message is received, the receiver sets its counter to `max(local, received) + 1`. This gives every event a number such that `A → B` (A happened before B) implies `timestamp(A) < timestamp(B)`. But the converse is not true: `timestamp(A) < timestamp(B)` does not imply A happened before B — two concurrent events may have any relative timestamps.

Vector clocks fix this. Instead of one counter, each node maintains one counter per node in the system. Node i increments only its own component on each local event; on message receive, it takes component-wise max with the received vector. Now `VC(A) < VC(B)` (all components of A's vector ≤ B's vector, with at least one strictly less) if and only if A happened before B. Concurrent events are detected when neither `VC(A) < VC(B)` nor `VC(B) < VC(A)`.

Version vectors are a closely related concept used for replica reconciliation (as in Riak): each replica maintains a vector of (node, version) pairs representing "I have seen all writes from node X up to version V_x." Two replicas with different version vectors can determine which writes they each have that the other is missing. Hybrid Logical Clocks (HLC) combine physical time with logical clock properties: the timestamp is always ≥ the local wall clock, advances monotonically, and allows events to be ordered relative to real time for human-readable debugging.

## Core Concepts

### Lamport Timestamps: A Total Order Consistent with Causality

Lamport timestamps provide a total order over all events in a distributed system. The algorithm: each node has a counter `C`. On event: `C++`. On send: `C++`, include `C` in message. On receive of message with counter `m`: `C = max(C, m) + 1`.

Lamport timestamps are sufficient for: debugging (ordering log entries from multiple nodes), lock ordering (always acquire in timestamp order to prevent deadlock), and detecting that an event "might have happened before" another. They are insufficient for detecting concurrent events — two events may have different timestamps yet be truly concurrent (neither happened before the other).

### Vector Clocks: Detecting Concurrency

A vector clock `VC` of size N (one entry per node) enables three determinations:
- `VC(A) < VC(B)`: A happened before B (A → B)
- `VC(B) < VC(A)`: B happened before A (B → A)
- Neither: A and B are concurrent (A || B)

The comparison `VC(A) ≤ VC(B)` means: `VC(A)[i] ≤ VC(B)[i]` for all i. `VC(A) < VC(B)` additionally requires at least one strict inequality. Concurrency means `VC(A) ≤ VC(B)` is false AND `VC(B) ≤ VC(A)` is false.

The cost: vector clocks are O(N) in size and comparison time. For a 1,000-node cluster, vector clocks are 1,000-entry arrays. This is why Riak replaced per-replica vector clocks with "dotted version vectors" (a compressed representation) in version 2.0.

### Version Vectors: Replica Reconciliation

Version vectors are used for tracking which writes each replica has seen, not for ordering events. A version vector `VV` at a replica is a map `{node_id → highest_seen_version}`. Two replicas A and B have diverged if neither `VV(A) ≤ VV(B)` nor `VV(B) ≤ VV(A)` — there are writes in A that B has not seen, and vice versa. The repair process: exchange the writes that the other is missing (the set difference defined by the version vector).

### Hybrid Logical Clocks (HLC)

HLC combines a physical clock `pt` with a logical counter `l`. Each event's timestamp is `(pt, l)` where `pt` is the highest physical time seen and `l` is a counter for events that share the same `pt`. Ordering: compare `pt` first, break ties with `l`. HLC timestamps are always ≥ the local wall clock and advance monotonically even if the wall clock jumps backward (a common NTP adjustment). CockroachDB uses HLC for MVCC transaction timestamps — this allows timestamps to be human-readable while remaining causally consistent.

## Implementation: Go

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

// ---- Lamport Clock ----

type LamportClock struct {
	mu   sync.Mutex
	tick uint64
}

func (c *LamportClock) Increment() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tick++
	return c.tick
}

// Update advances the clock to max(local, received) + 1.
func (c *LamportClock) Update(received uint64) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if received > c.tick {
		c.tick = received
	}
	c.tick++
	return c.tick
}

func (c *LamportClock) Now() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tick
}

// ---- Vector Clock ----

// VectorClock is an N-dimensional logical clock for N nodes.
type VectorClock struct {
	mu    sync.Mutex
	id    string
	clock map[string]uint64
}

func NewVectorClock(nodeID string) *VectorClock {
	return &VectorClock{id: nodeID, clock: map[string]uint64{nodeID: 0}}
}

// Tick increments this node's component on a local event.
func (v *VectorClock) Tick() map[string]uint64 {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.clock[v.id]++
	return v.snapshot()
}

// Send increments this node's component and returns the clock to include in the message.
func (v *VectorClock) Send() map[string]uint64 {
	return v.Tick()
}

// Receive merges a received vector clock and increments the local component.
func (v *VectorClock) Receive(received map[string]uint64) map[string]uint64 {
	v.mu.Lock()
	defer v.mu.Unlock()
	// Component-wise max, then increment own component
	for node, tick := range received {
		if tick > v.clock[node] {
			v.clock[node] = tick
		}
	}
	v.clock[v.id]++
	return v.snapshot()
}

func (v *VectorClock) snapshot() map[string]uint64 {
	s := make(map[string]uint64, len(v.clock))
	for k, val := range v.clock {
		s[k] = val
	}
	return s
}

// HappensBefore returns true if a → b (a strictly happened before b).
func HappensBefore(a, b map[string]uint64) bool {
	allNodes := make(map[string]struct{})
	for k := range a { allNodes[k] = struct{}{} }
	for k := range b { allNodes[k] = struct{}{} }

	atLeastOneLess := false
	for node := range allNodes {
		av, bv := a[node], b[node]
		if av > bv { return false }
		if av < bv { atLeastOneLess = true }
	}
	return atLeastOneLess
}

// Concurrent returns true if neither a → b nor b → a.
func Concurrent(a, b map[string]uint64) bool {
	return !HappensBefore(a, b) && !HappensBefore(b, a)
}

// ---- Hybrid Logical Clock ----

// HLC combines physical time with a logical counter.
// Invariant: HLC timestamp is always ≥ local wall clock.
type HLC struct {
	mu      sync.Mutex
	wallNow func() int64 // injectable for testing
	pt      int64        // highest physical time seen
	l       uint32       // logical counter for events sharing the same pt
}

func NewHLC() *HLC {
	return &HLC{wallNow: func() int64 { return time.Now().UnixNano() }}
}

// Now returns the current HLC timestamp, advancing pt if wall clock has moved forward.
func (h *HLC) Now() (pt int64, l uint32) {
	h.mu.Lock()
	defer h.mu.Unlock()
	wall := h.wallNow()
	if wall > h.pt {
		h.pt = wall
		h.l = 0
	} else {
		h.l++ // wall clock has not advanced; use logical counter
	}
	return h.pt, h.l
}

// Receive merges a received HLC timestamp and advances to reflect the message's causality.
func (h *HLC) Receive(rpt int64, rl uint32) (pt int64, l uint32) {
	h.mu.Lock()
	defer h.mu.Unlock()
	wall := h.wallNow()
	maxPt := h.pt
	if rpt > maxPt { maxPt = rpt }
	if wall > maxPt { maxPt = wall }

	switch {
	case maxPt == h.pt && maxPt == rpt:
		// Both local and received are at the same pt: advance logical counter
		if rl > h.l { h.l = rl }
		h.l++
	case maxPt == h.pt:
		h.l++
	case maxPt == rpt:
		h.l = rl + 1
		h.pt = rpt
	default:
		h.pt = maxPt
		h.l = 0
	}
	return h.pt, h.l
}

// CompareHLC returns -1 if a < b, 0 if equal, 1 if a > b.
func CompareHLC(apt int64, al uint32, bpt int64, bl uint32) int {
	if apt != bpt {
		if apt < bpt { return -1 }
		return 1
	}
	if al != bl {
		if al < bl { return -1 }
		return 1
	}
	return 0
}

func main() {
	// ---- Lamport Clocks ----
	fmt.Println("=== Lamport Timestamps ===")
	var cA, cB, cC LamportClock

	// Node A does a local event, then sends a message
	tA1 := cA.Increment()
	fmt.Printf("A: local event ts=%d\n", tA1)

	tA_send := cA.Increment()
	fmt.Printf("A: send message ts=%d\n", tA_send)

	// Node B receives from A, does a local event, sends to C
	tB1 := cB.Update(tA_send)
	fmt.Printf("B: receive from A ts=%d\n", tB1)
	tB_send := cB.Increment()
	fmt.Printf("B: send to C ts=%d\n", tB_send)

	// Node C has done a concurrent event before receiving from B
	tC_concurrent := cC.Increment()
	fmt.Printf("C: concurrent event ts=%d\n", tC_concurrent)
	tC_recv := cC.Update(tB_send)
	fmt.Printf("C: receive from B ts=%d\n", tC_recv)

	fmt.Printf("\nOrder: A_send=%d < B_recv=%d < B_send=%d < C_recv=%d\n",
		tA_send, tB1, tB_send, tC_recv)
	fmt.Printf("C_concurrent=%d may appear anywhere — concurrent event\n", tC_concurrent)

	// ---- Vector Clocks ----
	fmt.Println("\n=== Vector Clocks ===")
	vcA := NewVectorClock("A")
	vcB := NewVectorClock("B")
	vcC := NewVectorClock("C")

	// A: local event
	va1 := vcA.Tick()
	fmt.Printf("A: local event vc=%v\n", va1)

	// A sends to B
	va_send := vcA.Send()
	fmt.Printf("A: send vc=%v\n", va_send)

	// C: concurrent local event (before receiving anything)
	vc_concurrent := vcC.Tick()
	fmt.Printf("C: concurrent vc=%v\n", vc_concurrent)

	// B receives from A
	vb_recv := vcB.Receive(va_send)
	fmt.Printf("B: receive from A vc=%v\n", vb_recv)

	// B sends to C
	vb_send := vcB.Send()
	fmt.Printf("B: send vc=%v\n", vb_send)

	// C receives from B
	vc_recv := vcC.Receive(vb_send)
	fmt.Printf("C: receive from B vc=%v\n", vc_recv)

	// Causality checks
	fmt.Printf("\nA_send → B_recv: %v (expected true)\n", HappensBefore(va_send, vb_recv))
	fmt.Printf("B_send → C_recv: %v (expected true)\n", HappensBefore(vb_send, vc_recv))
	fmt.Printf("A_send → C_recv: %v (expected true — transitivity)\n", HappensBefore(va_send, vc_recv))
	fmt.Printf("A_send || C_concurrent: %v (expected true — concurrent)\n", Concurrent(va_send, vc_concurrent))
	fmt.Printf("C_concurrent → C_recv: %v (expected true — C's own order)\n", HappensBefore(vc_concurrent, vc_recv))

	// ---- Hybrid Logical Clock ----
	fmt.Println("\n=== Hybrid Logical Clock ===")
	// Simulate two nodes with a shared wall clock (for demo, advance manually)
	wallTick := int64(1000000000) // 1 second in ns
	hlcA := &HLC{wallNow: func() int64 { wallTick += 1000000; return wallTick }}
	hlcB := &HLC{wallNow: func() int64 { wallTick += 1000000; return wallTick }}

	apt1, al1 := hlcA.Now()
	fmt.Printf("A ts=(%d, %d)\n", apt1, al1)
	apt2, al2 := hlcA.Now()
	fmt.Printf("A ts=(%d, %d)\n", apt2, al2)

	// B receives A's timestamp
	bpt, bl := hlcB.Receive(apt2, al2)
	fmt.Printf("B after receiving A: ts=(%d, %d)\n", bpt, bl)

	// HLC comparison
	cmp := CompareHLC(apt2, al2, bpt, bl)
	fmt.Printf("A_send < B_recv: %v (cmp=%d, expected -1)\n", cmp < 0, cmp)
}
```

### Go-specific considerations

The `sync.Mutex` in `VectorClock` protects against concurrent tick/receive calls. In a single-threaded Raft state machine (where causality is tracked per operation), the mutex is unnecessary — but in a real network handler that may receive messages concurrently, it is essential.

The `snapshot()` method returning a copy of the internal map is critical: callers must not retain a reference to the internal map because subsequent operations mutate it. In Go, maps are reference types — returning `v.clock` directly would allow the caller to observe clock changes or, worse, modify the internal state.

## Implementation: Rust

```rust
use std::collections::HashMap;
use std::time::{SystemTime, UNIX_EPOCH};

// ---- Lamport Clock ----
#[derive(Default)]
struct LamportClock(u64);

impl LamportClock {
    fn tick(&mut self) -> u64 {
        self.0 += 1;
        self.0
    }
    fn update(&mut self, received: u64) -> u64 {
        self.0 = self.0.max(received) + 1;
        self.0
    }
    fn now(&self) -> u64 { self.0 }
}

// ---- Vector Clock ----
#[derive(Debug, Clone)]
struct VectorClock {
    id: String,
    clock: HashMap<String, u64>,
}

impl VectorClock {
    fn new(id: &str) -> Self {
        let mut clock = HashMap::new();
        clock.insert(id.to_string(), 0);
        VectorClock { id: id.to_string(), clock }
    }

    fn tick(&mut self) -> HashMap<String, u64> {
        *self.clock.entry(self.id.clone()).or_insert(0) += 1;
        self.clock.clone()
    }

    fn receive(&mut self, received: &HashMap<String, u64>) -> HashMap<String, u64> {
        for (node, &tick) in received {
            let e = self.clock.entry(node.clone()).or_insert(0);
            if tick > *e { *e = tick; }
        }
        *self.clock.entry(self.id.clone()).or_insert(0) += 1;
        self.clock.clone()
    }
}

fn happens_before(a: &HashMap<String, u64>, b: &HashMap<String, u64>) -> bool {
    let all_nodes: std::collections::HashSet<&str> = a.keys().chain(b.keys())
        .map(|s| s.as_str()).collect();
    let mut at_least_one_less = false;
    for node in all_nodes {
        let av = a.get(node).copied().unwrap_or(0);
        let bv = b.get(node).copied().unwrap_or(0);
        if av > bv { return false; }
        if av < bv { at_least_one_less = true; }
    }
    at_least_one_less
}

fn concurrent(a: &HashMap<String, u64>, b: &HashMap<String, u64>) -> bool {
    !happens_before(a, b) && !happens_before(b, a)
}

// ---- Hybrid Logical Clock ----
#[derive(Debug)]
struct HLC {
    pt: u64,  // physical time (nanoseconds)
    l: u32,   // logical counter
}

impl HLC {
    fn new() -> Self { HLC { pt: 0, l: 0 } }

    fn wall_now() -> u64 {
        SystemTime::now().duration_since(UNIX_EPOCH).unwrap().as_nanos() as u64
    }

    fn now(&mut self) -> (u64, u32) {
        let wall = Self::wall_now();
        if wall > self.pt {
            self.pt = wall;
            self.l = 0;
        } else {
            self.l += 1;
        }
        (self.pt, self.l)
    }

    fn receive(&mut self, rpt: u64, rl: u32) -> (u64, u32) {
        let wall = Self::wall_now();
        let max_pt = self.pt.max(rpt).max(wall);
        if max_pt == self.pt && max_pt == rpt {
            self.l = self.l.max(rl) + 1;
        } else if max_pt == self.pt {
            self.l += 1;
        } else if max_pt == rpt {
            self.pt = rpt;
            self.l = rl + 1;
        } else {
            self.pt = max_pt;
            self.l = 0;
        }
        (self.pt, self.l)
    }
}

fn main() {
    println!("=== Lamport Clocks ===");
    let mut ca = LamportClock::default();
    let mut cb = LamportClock::default();
    let ta1 = ca.tick();
    let ta_send = ca.tick();
    let tb_recv = cb.update(ta_send);
    println!("A local: {}, A send: {}, B recv: {}", ta1, ta_send, tb_recv);
    println!("A_send < B_recv: {}", ta_send < tb_recv);

    println!("\n=== Vector Clocks ===");
    let mut vca = VectorClock::new("A");
    let mut vcb = VectorClock::new("B");
    let mut vcc = VectorClock::new("C");

    let va1 = vca.tick();
    let vc_concurrent = vcc.tick(); // C does a local event concurrently
    let va_send = vca.tick();
    let vb_recv = vcb.receive(&va_send);
    let vb_send = vcb.tick();
    let vc_recv = vcc.receive(&vb_send);

    println!("A_send → B_recv: {}", happens_before(&va_send, &vb_recv));
    println!("A_send || C_concurrent: {}", concurrent(&va_send, &vc_concurrent));
    println!("C_concurrent → C_recv: {}", happens_before(&vc_concurrent, &vc_recv));

    println!("\n=== Hybrid Logical Clock ===");
    let mut hlc_a = HLC::new();
    let mut hlc_b = HLC::new();
    let (apt, al) = hlc_a.now();
    let (bpt, bl) = hlc_b.receive(apt, al);
    println!("A: ({}, {}), B after receive: ({}, {})", apt, al, bpt, bl);
    println!("A_send < B_recv (pt): {}", apt < bpt || (apt == bpt && al < bl));

    let _ = va1; // suppress unused warning
}
```

### Rust-specific considerations

`HashMap<String, u64>` is returned by value from `tick()` and `receive()`, which clones the internal map. This is correct for a "send the clock with the message" use case — the clone becomes the message payload, and subsequent local events advance the clock independently.

The `.max()` method on `u64` makes the HLC's `max(local_pt, received_pt, wall_time)` computation readable. In Go this requires explicit `if` comparisons; Rust's functional style eliminates the boilerplate.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Clock as shared state | `*LamportClock` with mutex | No shared state needed (single-threaded demo); `Arc<Mutex<LamportClock>>` for concurrent use |
| Map clone for message payload | `snapshot()` method returning a copy | `.clone()` on HashMap — idiomatic |
| HappensBefore | Free function on `map[string]uint64` | Free function on `&HashMap<String, u64>` |
| Physical time | `time.Now().UnixNano()` | `SystemTime::now()` + `.as_nanos()` |
| HLC comparison | Explicit `if pt1 != pt2` chain | `.cmp()` on tuples is more concise |

## Production War Stories

**Riak and version vectors**: Riak's original vector clock implementation tracked per-client clocks, which led to unbounded clock growth as clients changed over time (each new client ID added an entry). Riak 2.0 replaced per-client vector clocks with "server-side version vectors" (dotted version vectors): only server nodes have entries, and a "dot" (a specific write's node and counter) identifies each write uniquely. This bounded clock size to the number of Riak nodes rather than the number of clients. The transition was a major data migration; the lesson: choose the vector clock entry space carefully before production deployment.

**CockroachDB's HLC**: CockroachDB uses HLC for MVCC transaction timestamps. Each transaction is assigned an HLC timestamp, and MVCC versions are ordered by this timestamp. The key property HLC provides: if two transactions have a causal relationship (T1's commit is observed by T2 before T2 commits), T1's HLC timestamp is strictly less than T2's. This guarantees linearizability without requiring Spanner's commit wait. The bounded clock uncertainty (CockroachDB tolerates ±250ms skew by default) means transaction commits may take up to 250ms extra if the HLC's physical component is ahead of the wall clock.

**Cassandra's write timestamps**: Cassandra uses microsecond-precision wall-clock timestamps for LWW conflict resolution. No vector clocks. The production consequence: if two Cassandra nodes have a 1μs clock skew and two concurrent writes to the same key happen within that window, the "wrong" write wins. Datastax recommends NTP with PPS (pulse-per-second) hardware for sub-10μs skew. The lesson: LWW without causality tracking is correct only when clock skew is smaller than the smallest concurrent write interval.

**Amazon DynamoDB's context**: DynamoDB's "context" object (returned with every read, included with every write) is a version vector encoding which writes are causally prior to this read. It enables "conditional writes" (write only if the current version matches the context). This is the practical API for vector-clock-based optimistic concurrency without exposing vector clocks directly to clients.

## Fault Model

| Failure | Vector clock behavior |
|---|---|
| Node crash | The crashed node's component in all vector clocks freezes; it does not advance until the node recovers. Events on other nodes continue; causality is maintained for events among live nodes |
| Message loss | The receiver never increments its clock for the lost message. Any events causally dependent on that message are not ordered before the receiver's subsequent events — they appear concurrent |
| Clock overflow | `uint64` counters at 1 billion events/second take 584 years to overflow — not a practical concern |
| Node reuse (same ID, new node) | If a new node reuses a dead node's ID, it starts its component at 0. Past events with that node's ID in their vector clock will incorrectly appear to causally precede or follow the new node's events. Solution: assign new IDs to replacement nodes |
| Vector clock merge conflict | There is no conflict in vector clock merge — it is always component-wise max, which is correct regardless of order. The "conflict" is detected (concurrent events), not created, by the comparison |

## Common Pitfalls

**Pitfall 1: Confusing Lamport timestamps with vector clocks**

Lamport timestamps establish a partial order consistent with causality but cannot detect concurrent events. If `ts(A) < ts(B)`, A may or may not have caused B. Vector clocks detect concurrency: if neither `VC(A) ≤ VC(B)` nor `VC(B) ≤ VC(A)`, the events are definitely concurrent. Using Lamport timestamps where vector clocks are needed (e.g., Riak's conflict detection) silently loses write conflicts.

**Pitfall 2: Not including the full vector clock in messages**

A partial vector clock (including only changed entries to save bandwidth) prevents correct causality detection on the receiving side. The receiver cannot tell the difference between "A's component was 0 because it was not included" and "A's component is truly 0." Use delta CRDTs for bandwidth optimization, but always send full vector clocks for causality tracking.

**Pitfall 3: Using wall-clock timestamps for ordering in an eventually consistent system**

Wall clocks are not causally ordered — two events with the same wall-clock timestamp may be concurrent or causally ordered, and there is no way to tell. Using wall-clock LWW (as Cassandra does) is an explicit trade-off of correctness for simplicity. If your system requires causal ordering (e.g., "event B was written in response to reading event A"), wall-clock LWW is wrong.

**Pitfall 4: Growing vector clock unboundedly with many writers**

Each writer (client) that writes to a vector-clock-tracked key adds an entry. In a system with millions of clients, each key's vector clock grows to millions of entries. The fix: use server-side version vectors (Riak's approach) where only nodes — not clients — have entries in the vector clock.

**Pitfall 5: Not resetting logical counter in HLC after physical time advances**

In HLC, when the wall clock advances beyond the current `pt`, the logical counter `l` must be reset to 0. Forgetting this causes `l` to grow monotonically even when `pt` has advanced, which is wasteful (the `pt` component already provides ordering) and can eventually overflow `l` (though `uint32` at 1 billion events/second takes 4 seconds to overflow — more of a concern at nanosecond granularity).

## Exercises

**Exercise 1** (30 min): Run the Go implementation and manually trace the happens-before relationship through three nodes. Draw the spacetime diagram: each node is a vertical line, messages are diagonal arrows, and local events are dots. Verify that every pair of events where one arrow points to the other has `HappensBefore(a, b) == true`.

**Exercise 2** (2-4h): Implement a causal broadcast protocol in Go: a message sent by node A should be delivered to all nodes in causal order. A message M with vector clock `VC(M)` should not be delivered to node B until B has delivered all messages M' where `VC(M') < VC(M)`. Use a pending queue per node. Test with three nodes exchanging messages concurrently.

**Exercise 3** (4-8h): Implement the dotted version vector (DVV) data structure used by Riak 2.0. A DVV is a pair `(VV, dot)` where `VV` is the context (what was seen before the write) and `dot` is the write's unique identifier `(nodeID, counter)`. Implement `join(dvv1, dvv2)` (merge two DVVs), `descends(dvv1, dvv2)` (true if dvv1 causally dominates dvv2), and `concurrent(dvv1, dvv2)`. Test with the classic concurrent-write scenario.

**Exercise 4** (8-15h): Build a simple multi-node key-value store using vector clocks for conflict detection and the OR-Set semantics for conflict resolution (add wins). Three goroutines simulate three replicas; writes carry vector clocks; after sync, replicas that have concurrent writes to the same key must surface both versions as "siblings." Implement a `Resolve(siblings) string` function that merges siblings by concatenating values (for text data) or taking max (for numbers). Benchmark convergence time for 100 concurrent writes across 3 replicas.

## Further Reading

### Foundational Papers
- Lamport, L. (1978). "Time, Clocks, and the Ordering of Events in a Distributed System." *Communications of the ACM*, 21(7). The original paper. Sections 1-2 define the happens-before relation and Lamport timestamps; Section 4 extends to physical clocks.
- Fidge, C. (1988). "Timestamps in Message-Passing Systems That Preserve the Partial Ordering." *Australian Computer Science Communications*. One of two simultaneous papers introducing vector clocks (Mattern's 1989 paper is the other; Mattern's name is more commonly cited).
- Kulkarni, S. et al. (2014). "Logical Physical Clocks and Consistent Snapshots in Globally Distributed Databases." *OPODIS 2014*. The HLC paper. Section 3 (algorithm) and Section 4 (correctness proof) are essential.

### Books
- Attiya, H. & Welch, J. (2004). *Distributed Computing: Fundamentals, Simulations, and Advanced Topics* (2nd ed.). Chapter 5 covers logical clocks and causality with formal proofs.
- Kleppmann, M. (2017). *Designing Data-Intensive Applications*. Chapter 8 (The Trouble with Distributed Systems) covers clock skew and causality; Chapter 9 covers their role in consistency guarantees.

### Production Code to Read
- CockroachDB: `pkg/util/hlc/hlc.go` — The HLC implementation used in production. The `Clock.Update` and `Clock.Now` methods are the reference implementation.
- `riak_core`: `riak_core_dvvset.erl` — Dotted version vectors in Erlang. The `join/2` and `sync/2` functions show the complete DVV algorithm.
- etcd: `client/v3/concurrency/` — Uses Raft-based revision numbers as Lamport timestamps for distributed locks and leases.

### Talks
- Lamport, L. (2015): "Time, Clocks, and the Ordering of Events in a Distributed System." ACM Video Classic — Lamport discussing his 1978 paper 37 years later.
- Shapiro, M. (2011): "A Comprehensive Study of CRDTs." Includes vector clock-based CRDT background.
