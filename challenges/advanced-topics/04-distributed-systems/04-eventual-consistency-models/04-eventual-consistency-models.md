<!--
type: reference
difficulty: advanced
section: [04-distributed-systems]
concepts: [eventual-consistency, session-guarantees, read-your-writes, monotonic-reads, monotonic-writes, writes-follow-reads, CALM-theorem, convergence, conflict-resolution, LWW]
languages: [go, rust]
estimated_reading_time: 70 min
bloom_level: analyze
prerequisites: [vector-clocks, crdt-theory, cap-theorem]
papers: [terry-1994-session-guarantees, helland-2009-building-on-quicksand, ameloot-2013-calm]
industry_use: [amazon-dynamo, apache-cassandra, riak, couchdb]
language_contrast: low
-->

# Eventual Consistency Models

> "Eventual consistency" is not one model — it is a spectrum from "replicas will agree someday" to "clients observe monotonically consistent views"; the session guarantees define where on that spectrum your system sits, and violating them silently is the source of the most insidious distributed system bugs.

## Mental Model

The CAP theorem tells you that a partition-tolerant system must choose between consistency and availability. But "consistency" in CAP means linearizability (the strongest model), and "availability" means every non-crashed node responds to every request. The real engineering decision is not binary — it is: which *specific* consistency guarantees do clients need, and what is the minimum coordination cost to provide them?

Session guarantees (Terry et al., 1994) operationalize this. A session is the sequence of reads and writes performed by a single client. Four guarantees can be provided independently:

- **Read Your Writes**: after a client writes a value, its subsequent reads always see that value (or a more recent one)
- **Monotonic Reads**: if a client reads a value, subsequent reads always see that value or a newer one (reads never go backward)
- **Monotonic Writes**: writes from a single client are serialized — a replica applies them in the order they were issued
- **Writes Follow Reads**: if a client reads a value and then writes, the write is ordered after the read (the write is "aware of" the read)

These guarantees can be violated in naive eventually consistent systems. Consider a client that writes `x=1` to replica A, then reads `x` from replica B (which has not yet received the write). If the client's session is pinned to replica B (via load balancer), it will read `x=0` — a Read Your Writes violation. The fix: the client carries a session token (a vector clock) that tells any replica "I have seen writes up to this point; you must apply them before serving my read."

The CALM theorem (Consistency As Logical Monotonicity, Hellerstein et al., 2010) goes deeper: a program is eventually consistent *without coordination* if and only if it is *monotone* — meaning additional inputs can only add to its output, never retract it. Set union is monotone (adding an element never removes one). Comparison (`x > y`) is non-monotone (seeing more data can change the result). This is why CRDTs (which are monotone by construction) need no coordination, while transactions (which are non-monotone: a conflict can invalidate a previous decision) require coordination.

## Core Concepts

### Session Guarantees: Read Your Writes

The most commonly violated guarantee in practice. A client writes to a primary and immediately issues a read routed to a secondary that has not yet replicated the write. The read returns stale data, the client is confused, the support ticket is filed.

Implementation: the client carries a "write token" — a logical timestamp or version vector of the last write it issued. Any server that handles a read must either have applied all writes up to that token, or wait until it has. Cassandra implements this with "serial consistency" reads; Dynamo implements it with the concept of "session tokens." A simpler implementation: route all reads and writes from a session to the same replica (sticky sessions) — this trivially satisfies Read Your Writes but eliminates load balancing.

### Monotonic Reads

A weaker guarantee than linearizability but often sufficient: once a client has seen a value at version V, it must never see a version < V again. This prevents the experience of "I just saw the file I uploaded, now it's gone."

Violation scenario: a client reads from replica A (which has version V=10), then the load balancer routes the next read to replica B (which only has V=8). The client sees data "from the past." Fix: the client carries the highest version it has seen; any replica serving a read must have version ≥ that value.

### CALM Theorem: When You Do Not Need Coordination

A distributed program is confluent (order-insensitive, deterministic) if and only if all its logic is monotone (using only monotone relational operations: union, join, projection with monotone predicates). Monotone programs can be run on any replica without coordination because more inputs never change already-determined outputs — they only add new outputs.

The practical implication: identify the non-monotone operations in your system (negation, aggregation, comparison for minimum/maximum) and determine whether they require coordination. If you need `COUNT(*)` to decide whether to proceed, you need coordination. If you only need to accumulate all submitted results, you do not.

### Conflict Resolution: LWW, Vector Clock Merge, Application-Specific

When two replicas diverge and merge, conflicts must be resolved. Three strategies:

**Last Write Wins (LWW)**: use the write timestamp; the higher timestamp's value survives. Simple to implement, but silently discards data. Only correct when the higher-timestamp write genuinely supersedes the earlier one. Clock skew makes this unreliable for sub-second concurrent writes.

**Vector Clock Merge**: track causality with vector clocks. If two writes have a causal relationship (one happened before the other), the later one wins. If they are concurrent (neither happened before the other), surface the conflict to the application for manual resolution ("sibling" values in Riak). No data is lost; the application decides. Requires the application to handle sibling resolution.

**Application-Specific Merge**: the application defines what "merge" means for its data types. Shopping cart: union of items (no item is lost). User profile: LWW per field, not per document. Counter: CRDT semantics. This is the most correct but requires explicit design for each data type.

## Implementation: Go

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

// VectorClock tracks causal ordering for session guarantees.
type VectorClock map[string]uint64

func (v VectorClock) Clone() VectorClock {
	c := make(VectorClock, len(v))
	for k, val := range v {
		c[k] = val
	}
	return c
}

// HappensBefore returns true if v happened strictly before other.
func (v VectorClock) HappensBefore(other VectorClock) bool {
	atLeastOneLess := false
	for node, tick := range v {
		if tick > other[node] {
			return false
		}
		if tick < other[node] {
			atLeastOneLess = true
		}
	}
	for node, tick := range other {
		if v[node] > tick {
			return false
		}
		if v[node] < tick {
			atLeastOneLess = true
		}
	}
	return atLeastOneLess
}

// Concurrent returns true if neither v nor other happened before the other.
func (v VectorClock) Concurrent(other VectorClock) bool {
	return !v.HappensBefore(other) && !other.HappensBefore(v)
}

// Merge returns the component-wise max (the least upper bound).
func (v VectorClock) Merge(other VectorClock) VectorClock {
	result := v.Clone()
	for node, tick := range other {
		if tick > result[node] {
			result[node] = tick
		}
	}
	return result
}

// VersionedValue is a stored value with its causal version.
type VersionedValue struct {
	Value   string
	Version VectorClock
	WallTs  time.Time
}

// Replica simulates an eventually consistent replica.
type Replica struct {
	mu      sync.Mutex
	id      string
	store   map[string][]VersionedValue // key -> list of concurrent versions (siblings)
	version VectorClock
}

func NewReplica(id string) *Replica {
	return &Replica{
		id:      id,
		store:   make(map[string][]VersionedValue),
		version: VectorClock{id: 0},
	}
}

// Write stores a value with a new version derived from the provided session token.
// The session token ensures Monotonic Writes: the write is ordered after any prior read.
func (r *Replica) Write(key, value string, sessionToken VectorClock) VectorClock {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Advance this replica's vector clock
	r.version = r.version.Merge(sessionToken)
	r.version[r.id]++

	vv := VersionedValue{
		Value:   value,
		Version: r.version.Clone(),
		WallTs:  time.Now(),
	}

	// Detect and remove versions this write dominates (causal supersession)
	existing := r.store[key]
	var survivors []VersionedValue
	for _, ev := range existing {
		if !ev.Version.HappensBefore(vv.Version) {
			// ev is concurrent with the new write — keep as a sibling
			survivors = append(survivors, ev)
		}
		// ev.Version.HappensBefore(vv.Version) → this write supersedes ev; discard ev
	}
	survivors = append(survivors, vv)
	r.store[key] = survivors

	fmt.Printf("Replica %s: Write %s=%q version=%v\n", r.id, key, value, vv.Version)
	return vv.Version
}

// Read returns the value(s) for a key, but only if this replica has caught up
// to at least the provided session token (Read Your Writes / Monotonic Reads).
// Returns nil and false if the replica is behind.
func (r *Replica) Read(key string, sessionToken VectorClock) ([]VersionedValue, VectorClock, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Monotonic Reads + Read Your Writes: refuse to serve if replica is behind the token
	for node, tick := range sessionToken {
		if r.version[node] < tick {
			// This replica has not yet applied the writes in the session token
			fmt.Printf("Replica %s: Read of %s rejected (behind session token: have %d, need %d for %s)\n",
				r.id, key, r.version[node], tick, node)
			return nil, nil, false
		}
	}

	versions := r.store[key]
	if len(versions) == 0 {
		return nil, r.version.Clone(), true
	}
	return versions, r.version.Clone(), true
}

// Sync simulates anti-entropy between two replicas: merges all versions.
// In production this is driven by a gossip protocol or replication stream.
func (r *Replica) Sync(other *Replica) {
	r.mu.Lock()
	other.mu.Lock()
	defer r.mu.Unlock()
	defer other.mu.Unlock()

	// Merge version clocks
	r.version = r.version.Merge(other.version)

	// Merge all key-value entries from other into this replica
	for key, otherVersions := range other.store {
		localVersions := r.store[key]
		merged := localVersions
		for _, ov := range otherVersions {
			dominated := false
			for _, lv := range localVersions {
				if ov.Version.HappensBefore(lv.Version) {
					dominated = true // local version supersedes ov
					break
				}
			}
			if !dominated {
				// Check if any local version is dominated by ov
				var keep []VersionedValue
				for _, lv := range merged {
					if !lv.Version.HappensBefore(ov.Version) {
						keep = append(keep, lv)
					}
				}
				keep = append(keep, ov)
				merged = keep
			}
		}
		r.store[key] = merged
	}
	fmt.Printf("Replica %s: Synced from %s, version=%v\n", r.id, other.id, r.version)
}

// LWWResolve resolves sibling conflicts using last-write-wins (wall clock).
// Returns the value with the highest wall clock timestamp.
func LWWResolve(siblings []VersionedValue) string {
	if len(siblings) == 0 {
		return ""
	}
	best := siblings[0]
	for _, s := range siblings[1:] {
		if s.WallTs.After(best.WallTs) {
			best = s
		}
	}
	return best.Value
}

func main() {
	// Create two replicas
	r1 := NewReplica("r1")
	r2 := NewReplica("r2")

	fmt.Println("=== Read Your Writes (session token) ===")

	// Client writes to r1, receives a session token
	token := VectorClock{"client": 0}
	writeToken := r1.Write("x", "hello", token)
	fmt.Printf("Client session token after write: %v\n", writeToken)

	// Client attempts to read from r2, which has not yet synced
	_, _, ok := r2.Read("x", writeToken)
	fmt.Printf("Read from r2 (not synced): ok=%v (expected false)\n", ok)

	// Sync r2 from r1
	r2.Sync(r1)

	// Now r2 satisfies the session token
	vals, _, ok2 := r2.Read("x", writeToken)
	fmt.Printf("Read from r2 (after sync): ok=%v value=%q\n", ok2, vals[0].Value)

	fmt.Println("\n=== Concurrent Writes → Siblings ===")
	// r1 and r2 both write to "y" concurrently (no causality between them)
	r1.Write("y", "from-r1", VectorClock{})
	r2.Write("y", "from-r2", VectorClock{})

	// Sync both ways
	r1.Sync(r2)

	// After sync, r1 has two concurrent versions of "y" (siblings)
	siblings, _, _ := r1.Read("y", VectorClock{})
	fmt.Printf("Siblings of y: %d concurrent versions\n", len(siblings))
	for _, s := range siblings {
		fmt.Printf("  value=%q version=%v\n", s.Value, s.Version)
	}

	// LWW resolution
	resolved := LWWResolve(siblings)
	fmt.Printf("LWW resolved: %q\n", resolved)

	fmt.Println("\n=== Monotonic Writes demonstration ===")
	// Session token ensures writes from this client are ordered
	sessionTok := VectorClock{}
	for i := 1; i <= 3; i++ {
		cmd := fmt.Sprintf("step-%d", i)
		sessionTok = r1.Write("log", cmd, sessionTok)
		fmt.Printf("Session token after write %d: %v\n", i, sessionTok)
	}
}
```

### Go-specific considerations

The `VectorClock` type as `map[string]uint64` uses string node IDs (replica names), which is more readable than integer indices. The clone/merge/happensBefore methods are the core causal ordering primitives that recur throughout distributed systems implementations.

The `Sync` method acquires both replicas' mutexes — in production, anti-entropy is always initiated by the receiving replica (pull-based), so you would only hold the local mutex and call a `GetState()` on the remote replica over the network. The bidirectional lock here simplifies the demo but would deadlock in production if two replicas called `Sync(each other)` simultaneously.

## Implementation: Rust

```rust
use std::collections::HashMap;
use std::time::{SystemTime, UNIX_EPOCH};

type NodeId = String;
type VectorClock = HashMap<NodeId, u64>;

fn vc_merge(a: &VectorClock, b: &VectorClock) -> VectorClock {
    let mut result = a.clone();
    for (node, &tick) in b {
        let e = result.entry(node.clone()).or_insert(0);
        if tick > *e { *e = tick; }
    }
    result
}

fn vc_happens_before(a: &VectorClock, b: &VectorClock) -> bool {
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

fn vc_concurrent(a: &VectorClock, b: &VectorClock) -> bool {
    !vc_happens_before(a, b) && !vc_happens_before(b, a)
}

#[derive(Debug, Clone)]
struct VersionedValue {
    value: String,
    version: VectorClock,
    wall_ts: u64,
}

struct Replica {
    id: String,
    store: HashMap<String, Vec<VersionedValue>>,
    version: VectorClock,
}

impl Replica {
    fn new(id: &str) -> Self {
        let mut version = VectorClock::new();
        version.insert(id.to_string(), 0);
        Replica { id: id.to_string(), store: HashMap::new(), version }
    }

    fn write(&mut self, key: &str, value: &str, session_token: &VectorClock) -> VectorClock {
        self.version = vc_merge(&self.version, session_token);
        *self.version.entry(self.id.clone()).or_insert(0) += 1;
        let vv = VersionedValue {
            value: value.to_string(),
            version: self.version.clone(),
            wall_ts: SystemTime::now().duration_since(UNIX_EPOCH).unwrap().as_millis() as u64,
        };
        let existing = self.store.entry(key.to_string()).or_default();
        // Remove entries dominated by this write
        existing.retain(|ev| !vc_happens_before(&ev.version, &vv.version));
        existing.push(vv.clone());
        println!("Replica {}: Write {}={:?} version={:?}", self.id, key, value, vv.version);
        vv.version
    }

    // Returns None if the replica is behind the session token (Read Your Writes violation).
    fn read(&self, key: &str, session_token: &VectorClock) -> Option<Vec<VersionedValue>> {
        for (node, &tick) in session_token {
            if self.version.get(node).copied().unwrap_or(0) < tick {
                println!("Replica {}: Read of {} rejected (behind session token)", self.id, key);
                return None;
            }
        }
        Some(self.store.get(key).cloned().unwrap_or_default())
    }

    fn sync_from(&mut self, other: &Replica) {
        self.version = vc_merge(&self.version, &other.version);
        for (key, other_versions) in &other.store {
            let local = self.store.entry(key.clone()).or_default();
            for ov in other_versions {
                let dominated = local.iter().any(|lv| vc_happens_before(&ov.version, &lv.version));
                if !dominated {
                    local.retain(|lv| !vc_happens_before(&lv.version, &ov.version));
                    local.push(ov.clone());
                }
            }
        }
        println!("Replica {} synced from {}", self.id, other.id);
    }
}

fn lww_resolve(siblings: &[VersionedValue]) -> Option<&str> {
    siblings.iter().max_by_key(|v| v.wall_ts).map(|v| v.value.as_str())
}

fn main() {
    let mut r1 = Replica::new("r1");
    let mut r2 = Replica::new("r2");

    println!("=== Read Your Writes ===");
    let token = VectorClock::new();
    let write_token = r1.write("x", "hello", &token);
    println!("Session token after write: {:?}", write_token);

    match r2.read("x", &write_token) {
        None => println!("Read from r2 (not synced): rejected (correct)"),
        Some(v) => println!("Read from r2 (not synced): {:?} (unexpected)", v),
    }

    // Sync r2 from r1 first
    let r1_snapshot = r1.store.clone();
    let r1_version = r1.version.clone();
    r2.version = vc_merge(&r2.version, &r1_version);
    for (key, versions) in &r1_snapshot {
        let local = r2.store.entry(key.clone()).or_default();
        for ov in versions {
            let dominated = local.iter().any(|lv| vc_happens_before(&ov.version, &lv.version));
            if !dominated {
                local.retain(|lv| !vc_happens_before(&lv.version, &ov.version));
                local.push(ov.clone());
            }
        }
    }

    match r2.read("x", &write_token) {
        Some(v) if !v.is_empty() => println!("Read from r2 (synced): {:?}", v[0].value),
        _ => println!("Read from r2: not found"),
    }

    println!("\n=== Concurrent Writes → Siblings ===");
    r1.write("y", "from-r1", &VectorClock::new());
    r2.write("y", "from-r2", &VectorClock::new());

    // Sync r1 from r2
    let r2_store = r2.store.clone();
    let r2_version = r2.version.clone();
    r1.version = vc_merge(&r1.version, &r2_version);
    for (key, versions) in &r2_store {
        let local = r1.store.entry(key.clone()).or_default();
        for ov in versions {
            let dominated = local.iter().any(|lv| vc_happens_before(&ov.version, &lv.version));
            if !dominated {
                local.retain(|lv| !vc_happens_before(&lv.version, &ov.version));
                local.push(ov.clone());
            }
        }
    }

    if let Some(siblings) = r1.read("y", &VectorClock::new()) {
        println!("{} sibling(s) of y:", siblings.len());
        for s in &siblings {
            println!("  {:?} (concurrent: {})", s.value,
                if siblings.len() > 1 { vc_concurrent(&siblings[0].version, &siblings[1].version) } else { false });
        }
        if let Some(resolved) = lww_resolve(&siblings) {
            println!("LWW resolved: {:?}", resolved);
        }
    }
}
```

### Rust-specific considerations

`HashMap<NodeId, u64>` for `VectorClock` is equivalent to the Go implementation. The `vc_happens_before` free function rather than a method avoids the borrow checker issue of calling methods on values that are also borrowed elsewhere in the same expression. In a production system, `VectorClock` would be a newtype wrapper (`struct VectorClock(HashMap<NodeId, u64>)`) with trait implementations for `PartialOrd` (happens-before), `BitOr` (merge), and `Display`.

The `retain` / `push` pattern for sibling management mirrors the Go implementation structurally. `retain` is the Rust equivalent of a filtered-in-place operation — it modifies the vector without an intermediate allocation.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Vector clock type | `type VectorClock map[string]uint64` | `type VectorClock = HashMap<NodeId, u64>` |
| Happens-before | Method on VectorClock | Free function (avoids borrow conflicts) |
| Replica mutation | Pointer receiver `*Replica` | `&mut self` — enforced by borrow checker |
| Concurrent access | `sync.Mutex` | `Arc<Mutex<Replica>>` for multi-threaded sharing |
| Sibling tracking | `[]VersionedValue` with loop filtering | `Vec<VersionedValue>` with `.retain()` |
| Time source | `time.Now()` | `SystemTime::now()` |

## Production War Stories

**Amazon Dynamo's session tokens**: The Dynamo paper (DeCandia et al., 2007) describes using vector clocks as "context" objects that clients pass with every request. A client that reads a value receives its vector clock as part of the response; subsequent writes include that clock as proof of what the write is based on. This implements Read Your Writes and Writes Follow Reads simultaneously. The "sloppy quorum" design (writing to any N healthy nodes, not the canonical N nodes) further complicates consistency: a client may write to a "hinted handoff" node during a partition, then read from a canonical node that has not yet received the hinted data, violating Read Your Writes despite carrying the session token.

**Cassandra's tunable consistency levels**: Cassandra exposes `CONSISTENCY ONE`, `QUORUM`, `LOCAL_QUORUM`, `ALL`, and `SERIAL`. `ONE` is pure eventual consistency with no session guarantees. `QUORUM` (where R + W > N) provides strong consistency for that operation in isolation but does not provide session guarantees across operations unless the client routes to the same coordinator. `LOCAL_QUORUM` is a geo-distributed optimization: require a quorum only within the local data center, accepting stale reads from remote data centers. The common misconfiguration: `W=ONE, R=ONE` with three replicas — this gives no consistency guarantee whatsoever, including no Read Your Writes.

**Riak's sibling explosion**: Riak with `allow_mult=true` (the correct setting for vector clock tracking) can accumulate "siblings" — concurrent versions of the same key that must be resolved by the application. A Riak deployment at a social networking company (documented in a 2013 Basho blog post) accumulated thousands of siblings for a single user's profile object because the application never resolved conflicts. Every read returned thousands of versions; the merge operation was O(N²) in sibling count; the system slowed to a crawl. The fix: implement a sibling-pruning merge function that resolves siblings on every read (read-repair) and write.

**CouchDB and MVCC conflict resolution**: CouchDB's built-in conflict resolution uses a deterministic winner selection (highest revision ID wins) as a fallback, but exposes conflicting revisions to the application for proper resolution. The CouchDB replication protocol (used in PouchDB for browser-to-server sync) is the clearest production example of vector-clock-based conflict detection — every document has a `_rev` field that encodes its causal history.

## Fault Model

| Failure | Eventual consistency behavior |
|---|---|
| Network partition | Partitioned replicas accept writes independently; after healing, conflicts are resolved via merge or sibling surfacing |
| Node crash during write | Write is lost unless it was acknowledged by the quorum; partial writes (some replicas got it, others did not) result in inconsistency until read-repair or anti-entropy |
| Stale read (not caught by session token) | Application receives old data; this is the "expected" failure mode of eventual consistency — not a bug, but a guarantee that must be explicitly excluded by the session guarantee |
| Clock skew in LWW | The wrong write wins if the clock skew exceeds the time between concurrent writes; for writes within 1ms, a 10ms clock skew rate causes ~1% incorrect resolutions |
| Sibling accumulation without resolution | Memory and CPU grow unboundedly; eventually causes OOM or timeout; requires application-level sibling resolution on every read |

## Common Pitfalls

**Pitfall 1: Treating "eventual consistency" as "no consistency"**

Eventual consistency guarantees convergence — all replicas will eventually have the same state. It does not say "arbitrary garbage may appear." If your merge function is correct, the converged state is deterministic and correct. The bugs come from incorrect merge functions (e.g., LWW on a structured object where you need per-field merge) or from not understanding which session guarantees are and are not provided.

**Pitfall 2: Relying on wall-clock LWW for correctness-critical data**

LWW with wall clocks is vulnerable to clock skew. Two concurrent writes within a 10ms window on nodes with a 5ms clock skew will resolve incorrectly ~50% of the time. Use vector clocks for causal correctness; use LWW only for approximate data (analytics counters, recommendation scores) where stale data has low impact.

**Pitfall 3: Ignoring session guarantees when using multiple replicas**

The standard Cassandra client (`CQL`) does not provide session guarantees by default — consecutive reads may go to different coordinators. If your application assumes Read Your Writes (e.g., "write profile, then redirect to profile page"), you need to either use `SERIAL` consistency or implement session pinning at the client level.

**Pitfall 4: Not implementing read-repair**

Eventually consistent systems need a mechanism to propagate writes to replicas that missed them. Anti-entropy (periodic full-state comparison) handles this slowly. Read-repair (on every read, compare versions across replicas and repair divergence) handles it immediately. Without either, a replica that was partitioned for one hour may have stale data for days after the partition heals.

**Pitfall 5: Confusing causal consistency with sequential consistency**

Causal consistency guarantees that causally related operations are seen in causal order by all nodes. It does NOT guarantee that all nodes see operations in the same total order. Two nodes that wrote concurrently (no causal relationship) may be seen in different orders by different observers. If your application requires a total order (e.g., a message queue where all consumers must agree on message order), causal consistency is insufficient — you need sequential consistency or a consensus protocol.

## Exercises

**Exercise 1** (30 min): Run the Go implementation and deliberately trigger a Read Your Writes violation: write to r1, immediately read from r2 with an empty session token (ignoring the session guarantee check). Verify you get the old value. Then fix it by passing the write token and re-running. This makes the violation and the fix concrete.

**Exercise 2** (2-4h): Implement Monotonic Reads in the Go implementation. Add a `ReadToken` to the session that tracks the highest version seen in any read. Add a check in `Read`: the replica must have `version ≥ readToken` before serving the read. Demonstrate a scenario where without this check, a client reads version 10 from replica A, then reads version 8 from replica B.

**Exercise 3** (4-8h): Implement a convergent shopping cart using CRDTs (OR-Set of items, each item with a PN-Counter for quantity). Two clients add items concurrently, one client removes an item. After merge, verify that: (1) both clients' adds are present, (2) the removal is respected only if it was not concurrent with a re-add. Compare with a LWW-based shopping cart and show the data loss scenario.

**Exercise 4** (8-15h): Implement the CALM theorem in Go: build a "monotone query engine" that evaluates simple Datalog-style rules over a distributed fact store. Add facts from multiple replicas in any order and verify that the query result is the same regardless of the order. Then add a non-monotone query (negation: "find all users who have NOT logged in today") and show that it requires coordination (reading from all replicas before answering). Measure the coordination overhead.

## Further Reading

### Foundational Papers
- Terry, D. et al. (1994). "Session Guarantees for Weakly Consistent Replicated Data." *PDIS 1994*. The original session guarantees paper. 10 pages, very readable. Defines Read Your Writes, Monotonic Reads, Monotonic Writes, Writes Follow Reads with examples.
- DeCandia, G. et al. (2007). "Dynamo: Amazon's Highly Available Key-Value Store." *SOSP 2007*. Section 5 (implementation) explains vector clock usage for session guarantees. Section 6 (experiences) describes real production failure modes.
- Hellerstein, J.M. (2010). "The Declarative Imperative: Experiences and Conjectures in Distributed Logic." *SIGMOD Record*. Introduces the CALM theorem informally. The formal version is in Ameloot et al. (2013).

### Books
- Kleppmann, M. (2017). *Designing Data-Intensive Applications*. Chapter 5 (Replication) and Chapter 9 (Consistency and Consensus) are the best engineering-focused treatment of eventual consistency models, session guarantees, and their production implications.
- Terry, D. (2013). "Replicated Data Consistency Explained Through Baseball." Microsoft Research. A 10-page technical report that explains all six consistency models through baseball game score reading — the best mental model document.

### Production Code to Read
- `riak_core` (https://github.com/basho/riak_core) — The distributed systems layer of Riak. `riak_core_vnode.erl` shows how vector clocks are managed per vnode; `riak_core_aae_fold.erl` shows active anti-entropy.
- Apache Cassandra source: `src/java/org/apache/cassandra/service/pager/` — Shows how Cassandra handles paging across replicas while maintaining monotonic reads.
- CouchDB source: `src/couch_replicator/src/couch_replicator_changes_reader.erl` — The replication protocol using revision trees for conflict detection.

### Talks
- Bailis, P. (2014): "Highly Available Transactions: Virtues and Limitations." VLDB 2014. Formal characterization of which ACID properties are achievable without coordination.
- Hellerstein, J. & Alvaro, P. (2014): "Keeping CALM: When Distributed Consistency is Easy." CIDR 2015. The CALM theorem with examples from real distributed systems.
