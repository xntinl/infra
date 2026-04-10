# Distributed Systems — Reference Overview

> Distributed systems are not about making things fast — they are about making things correct in the presence of partial failure. Every algorithm in this section exists because network partitions happen, nodes crash at arbitrary moments, and clocks lie.

## Why This Section Matters

The systems you operate every day — Kubernetes, Kafka, CockroachDB, Cassandra, etcd, Consul, Redis Cluster — are built on a small set of distributed systems primitives that were formalized in academic papers between 1978 and 2014. The gap between knowing that "etcd uses Raft" and being able to reason about what happens when an etcd leader crashes mid-write, whether your client will see stale data, and how long linearizability is violated — that gap is what this section closes.

Distributed systems failures are not like application bugs. They are partial: three of five nodes respond, the fourth is slow, the fifth has partitioned. The algorithms in this section define precisely what guarantees hold in each failure scenario and what the cost of those guarantees is in latency, throughput, and operational complexity. An engineer who can read the Raft paper and then read etcd's election timeout configuration and understand the connection — that engineer debugs incidents in 20 minutes that others debug in four hours.

This section is organized around the fundamental tension in distributed systems: the CAP theorem tells you that you cannot have consistency, availability, and partition tolerance simultaneously, but it does not tell you *which consistency model* to pick, *how available* you need to be under which failure modes, or *what the implementation cost* is for each choice. These subtopics answer those questions, in Go and Rust, with production references.

## Subtopics

| # | Topic | Key Concepts | Est. Reading | Difficulty |
|---|-------|-------------|-------------|------------|
| 1 | [Raft Consensus](./01-raft-consensus/01-raft-consensus.md) | leader election, log replication, log compaction, membership change | 90 min | advanced |
| 2 | [Paxos and Variants](./02-paxos-and-variants/02-paxos-and-variants.md) | single-decree Paxos, Multi-Paxos, Fast Paxos, EPaxos | 90 min | advanced |
| 3 | [CRDT Theory and Practice](./03-crdt-theory-and-practice/03-crdt-theory-and-practice.md) | state-based, op-based, delta CRDTs, semilattice join, G-Counter, OR-Set | 75 min | advanced |
| 4 | [Eventual Consistency Models](./04-eventual-consistency-models/04-eventual-consistency-models.md) | session guarantees, CALM theorem, convergence, conflict resolution | 70 min | advanced |
| 5 | [Consistent Hashing and Partitioning](./05-consistent-hashing-and-partitioning/05-consistent-hashing-and-partitioning.md) | hash ring, virtual nodes, rendezvous hashing, jump consistent hash, rebalancing | 65 min | advanced |
| 6 | [Distributed Transactions](./06-distributed-transactions/06-distributed-transactions.md) | 2PC, 3PC, Saga pattern, Spanner TrueTime, Calvin deterministic ordering | 80 min | advanced |
| 7 | [Vector Clocks and Causality](./07-vector-clocks-and-causality/07-vector-clocks-and-causality.md) | Lamport timestamps, vector clocks, version vectors, HLC | 60 min | advanced |
| 8 | [Gossip and Epidemic Protocols](./08-gossip-and-epidemic-protocols/08-gossip-and-epidemic-protocols.md) | SWIM membership, phi-accrual failure detector, anti-entropy, convergence | 70 min | advanced |
| 9 | [Byzantine Fault Tolerance](./09-byzantine-fault-tolerance/09-byzantine-fault-tolerance.md) | Byzantine generals, PBFT, Tendermint, BFT vs CFT | 85 min | advanced |

## Consistency Model Map

The consistency models in distributed systems form a partial order from strongest (most expensive) to weakest (most available). Understanding where each system sits on this spectrum is essential for correctness reasoning.

```
STRONG ◄─────────────────────────────────────────────────────────────── WEAK

Linearizability   Serializability   Sequential    Causal      Eventual
      │                  │          Consistency  Consistency  Consistency
      │                  │               │            │            │
  etcd/Raft         Spanner           ZooKeeper    DynamoDB    Cassandra
  CockroachDB       CockroachDB       (by default) (causal)   (ONE level)
  (strong reads)    (SSI)                          Riak        Riak
                                                   (siblings)  (allow_mult)

──────────────────────────────────────────────────────────────────────────────
 What you pay:  highest latency    serializable     moderate     lowest
                (requires quorum   tx overhead      overhead     latency
                 round-trip)                                    highest
                                                               availability
```

**Key distinctions:**
- **Linearizability**: Every operation appears to take effect instantaneously at some point between its invocation and response. Reads always reflect the latest write. Cost: requires quorum reads (or leader reads) even for reads-only operations.
- **Serializability**: Transactions execute as if in some serial order, but that order need not correspond to real time. Two transactions may both read a stale value if neither has committed yet.
- **Sequential consistency**: All nodes see operations in the same order, but that order need not match real time. Processors are consistent with themselves.
- **Causal consistency**: Causally related operations are seen in causal order by all nodes. Concurrent operations may be seen in different orders. Riak with vector clocks.
- **Eventual consistency**: If no new writes arrive, all replicas will eventually converge. No ordering guarantees whatsoever.

## Failure Model Taxonomy

Each algorithm in this section makes assumptions about the failure model:

| Failure Type | Definition | Handled By |
|---|---|---|
| Crash-stop | Node fails by stopping; does not recover | Raft, Paxos, 2PC |
| Crash-recovery | Node fails and may recover with durable state | Raft (with log persistence), Spanner |
| Network partition | Nodes alive but cannot communicate | Raft (quorum requires majority), CRDTs (always available) |
| Omission | Messages are dropped (subset of network partition) | All consensus protocols |
| Byzantine | Node behaves arbitrarily; may send conflicting messages | PBFT, Tendermint (requires 3f+1 nodes) |
| Clock skew | Clocks differ by up to δ | HLC, TrueTime (bounded skew) |

## Dependency Map

```
Vector Clocks & Causality ──────────────────► CRDT Theory
(happens-before relation)                     (causal ordering enables
                                               op-based CRDTs)

Vector Clocks & Causality ──────────────────► Eventual Consistency
                                               (session guarantees need
                                               causal tracking)

Raft Consensus ─────────────────────────────► Distributed Transactions
(log replication = atomic                     (2PC coordinator uses a
 commitment protocol)                          replicated log for durability)

Paxos and Variants ─────────────────────────► Raft Consensus
(Raft is a restricted                         (understanding Paxos makes
 form of Multi-Paxos)                          Raft's design choices clear)

Consistent Hashing ─────────────────────────► Eventual Consistency
(partitioning determines                      (each partition has its
 replica placement)                            own consistency properties)

Gossip Protocols ───────────────────────────► Eventual Consistency
(dissemination mechanism                      (gossip achieves eventual
 for replica synchronization)                  convergence)

Byzantine FT ───────────────────────────────► (standalone; contrast with
                                               Raft/Paxos for CFT)
```

**Recommended read order for a first pass:**

1. Vector Clocks and Causality (foundational; every other topic references happens-before)
2. Raft Consensus (most approachable consensus algorithm; good mental model for the rest)
3. Paxos and Variants (understand what Raft simplified; historical context)
4. Eventual Consistency Models (CAP tension; session guarantees)
5. CRDT Theory and Practice (elegant solution to the availability/consistency tradeoff)
6. Consistent Hashing and Partitioning (practical; every sharded system uses this)
7. Gossip and Epidemic Protocols (dissemination layer; how state spreads)
8. Distributed Transactions (most operationally complex; builds on Raft)
9. Byzantine Fault Tolerance (most mathematically involved; read last)

## Time Investment

- **Survey** (Mental Model + Go vs Rust comparison only, all 9 subtopics): ~8h
- **Working knowledge** (read fully + run both implementations per subtopic): ~18h
- **Mastery** (all exercises + further reading per subtopic): ~80-120h

## Prerequisites

Before starting this section you should be comfortable with:

- **Networking**: TCP/IP semantics; what a network partition means in practice; why UDP has no delivery guarantees; what a socket timeout is and why it is not the same as a failed node
- **Concurrency**: goroutines and channels (Go); `async/await` with Tokio (Rust); mutexes and condition variables; what a race condition is
- **Go**: `net` package; goroutines for I/O; `sync.Mutex`, `sync/atomic`; JSON encoding; channels for state machine transitions; `context.Context` for cancellation
- **Rust**: Tokio async runtime; `Arc<Mutex<T>>`; `enum` for state machines; `serde` for serialization; `tokio::sync::mpsc` for message passing
- **Algorithms**: Big-O analysis; hash functions (consistent hashing builds on this); binary search (jump consistent hash); modular arithmetic (Paxos ballot numbers)
- **Systems**: What a write-ahead log is; why fsync is expensive; what a quorum is (majority); the difference between durability and availability

**Recommended prior sections:**
- Concurrency and Parallelism (covers atomic operations and memory ordering referenced in lock-free distributed state machines)
- Advanced Data Structures (skip lists appear in distributed databases; Bloom filters in gossip protocols)
