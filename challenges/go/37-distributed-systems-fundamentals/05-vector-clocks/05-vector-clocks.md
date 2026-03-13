# 5. Vector Clocks and Causality

<!--
difficulty: advanced
concepts: [vector-clock, causal-ordering, happens-before, concurrent-events, conflict-detection, lamport-timestamp]
tools: [go]
estimated_time: 40m
bloom_level: analyze
prerequisites: [sync-primitives, goroutines-and-channels, distributed-locking]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of concurrency and message passing
- Familiarity with the concept of partial ordering in distributed systems

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** vector clocks for tracking causal relationships between events
- **Determine** whether two events are causally related or concurrent
- **Demonstrate** conflict detection in a replicated key-value store using vector clocks
- **Compare** vector clocks with Lamport timestamps for ordering guarantees

## Why Vector Clocks Matter

In distributed systems, there is no global clock. Events on different nodes cannot be totally ordered by wall-clock time. Vector clocks capture the *causal* relationship between events: if event A caused event B (directly or transitively), the vector clock of A is less than B's. If neither is less than the other, the events are concurrent -- a conflict that must be resolved. DynamoDB, Riak, and other eventually consistent systems use vector clocks for conflict detection.

## The Problem

Build a vector clock implementation and use it in a simulated distributed key-value store where multiple nodes can write to the same key concurrently. The vector clocks detect conflicts and present them for resolution.

## Requirements

1. **Implement a `VectorClock` type** with operations:

```go
type VectorClock map[string]int64

func NewVectorClock() VectorClock
func (vc VectorClock) Increment(nodeID string)
func (vc VectorClock) Merge(other VectorClock)
func (vc VectorClock) Copy() VectorClock

// Comparison
func (vc VectorClock) HappensBefore(other VectorClock) bool
func (vc VectorClock) IsConcurrent(other VectorClock) bool
func (vc VectorClock) Equals(other VectorClock) bool
```

2. **Implement the happens-before relation**: VC(a) < VC(b) if and only if every component of a is <= the corresponding component of b, and at least one is strictly less.

3. **Build a replicated key-value store** where writes carry vector clocks:

```go
type VersionedValue struct {
    Value string
    Clock VectorClock
}

type ReplicaNode struct {
    ID     string
    Store  map[string][]VersionedValue // Key -> list of concurrent versions
    Clock  VectorClock
}

func (n *ReplicaNode) Put(key, value string)
func (n *ReplicaNode) Get(key string) []VersionedValue
func (n *ReplicaNode) Sync(peer *ReplicaNode)
```

4. **Demonstrate conflict detection** -- two nodes write to the same key without syncing, producing concurrent versions:

```go
func demonstrateConflict() {
    nodeA := NewReplicaNode("A")
    nodeB := NewReplicaNode("B")

    nodeA.Put("x", "value-from-A")
    nodeB.Put("x", "value-from-B")
    // These writes are concurrent -- vector clocks detect the conflict

    nodeA.Sync(nodeB)
    versions := nodeA.Get("x")
    // versions contains both values -- client must resolve
}
```

5. **Implement conflict resolution** -- last-writer-wins (using a tiebreaker) and application-level merge.

6. **Compare with Lamport timestamps** -- show that Lamport timestamps can order events but cannot detect concurrency:

```go
func demonstrateLamportLimitation() {
    // Two concurrent events may have different Lamport timestamps
    // but this does not mean one caused the other
}
```

## Hints

- Vector clocks have one component per node. When node N performs an event, it increments `vc[N]`. When receiving a message, it merges (component-wise max) and then increments.
- Happens-before: `a < b` iff `forall i: a[i] <= b[i]` AND `exists j: a[j] < b[j]`.
- Concurrent: neither `a < b` nor `b < a`.
- In a replicated store, each key can have multiple concurrent versions (siblings). The client chooses how to resolve: LWW, merge, or manual.
- Lamport timestamps provide a total order but lose concurrency information. If `L(a) < L(b)`, you cannot tell if `a` caused `b` or they are concurrent.
- Amazon's Dynamo paper is the canonical reference for vector clocks in key-value stores.

## Verification

```bash
go run main.go
go test -v ./...
```

Confirm that:
1. Causally related events are correctly ordered by vector clocks
2. Concurrent events are detected (neither happens-before the other)
3. The key-value store stores multiple concurrent versions (siblings)
4. Syncing resolves causal duplicates but preserves concurrent versions
5. Lamport timestamps cannot distinguish concurrent from causally related events

## What's Next

Continue to [06 - Raft Leader Election](../06-raft-leader-election/06-raft-leader-election.md) to implement consensus-based leader election.

## Summary

- Vector clocks track causal relationships: each node maintains a counter per node in the system
- The happens-before relation captures causality: if A's clock is strictly less than B's, A causally precedes B
- Concurrent events (no causal relationship) represent conflicts that must be resolved
- Replicated stores use vector clocks to detect and present conflicts to clients
- Lamport timestamps provide total ordering but lose concurrency information
- Vector clocks grow with the number of nodes -- practical systems often use bounded variants

## Reference

- [Dynamo: Amazon's Highly Available Key-Value Store](https://www.allthingsdistributed.files.com/files/amazon-dynamo-sosp2007.pdf)
- [Time, Clocks, and the Ordering of Events (Lamport)](https://lamport.azurewebsites.net/pubs/time-clocks.pdf)
- [Virtual Time and Global States of Distributed Systems (Mattern)](https://dl.acm.org/doi/10.1016/0304-3975%2893%2990122-A)
