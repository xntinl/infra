# 131. Vector Clock Causal Broadcast

<!--
difficulty: insane
category: distributed-systems-extended
languages: [go]
concepts: [vector-clocks, causal-ordering, happens-before, causal-broadcast, concurrency-detection]
estimated_time: 12-16 hours
bloom_level: synthesize
prerequisites: [go-basics, goroutines, channels, tcp-networking, serialization, distributed-systems-fundamentals, lamport-clocks]
-->

## Languages

- Go (1.22+)

## Prerequisites

- Goroutines, channels, and `select` for concurrent event processing
- TCP networking (`net.Listener`, `net.Conn`) for reliable message delivery
- `encoding/gob` or `encoding/json` for message serialization
- Lamport timestamps and the happens-before relation
- Understanding of causal consistency: if event A causally precedes event B, then every process that delivers B must have already delivered A

## Learning Objectives

- **Synthesize** a causal broadcast protocol from vector clock primitives, ensuring delivery ordering matches the causal dependency graph
- **Implement** vector clocks with correct increment, merge, and comparison operations for N processes
- **Evaluate** concurrent events: detect when two events are causally independent and neither happened before the other
- **Design** a delivery buffer that holds messages until all causal dependencies are satisfied
- **Create** a visualization of the happens-before partial order across distributed processes

## The Challenge

In distributed systems, messages can arrive out of causal order. Process A sends message M1, then sends M2 that depends on M1. Process B might receive M2 before M1 due to network delays. Delivering M2 before M1 violates causal consistency and can lead to nonsensical state: a reply delivered before the message it replies to, or a state update applied before the state it depends on.

Vector clocks solve this by tracking causal dependencies precisely. Each process maintains a vector of N counters (one per process). When process i sends a message, it increments `VC[i]` and attaches the full vector. When process j receives a message from i with vector `VC_msg`, it delivers the message only when: (1) `VC_msg[i] == VC_local[j's view of i] + 1` (this is the next expected message from i), and (2) for all k != i, `VC_msg[k] <= VC_local[k]` (j has already seen everything the sender had seen). This is the causal delivery condition.

Messages that arrive too early are buffered. After each delivery, the buffer is re-scanned because delivering one message may satisfy the dependencies of another. This is how systems like ISIS (the original causal broadcast system), Kafka with exactly-once semantics, and CRDT replication layers ensure causal consistency.

Implement vector clocks, causal broadcast over TCP with N processes, concurrent event detection, conflict resolution, and happens-before visualization.

## Requirements

1. Vector clock data structure: `VectorClock` type with `Increment(processID)`, `Merge(other)`, `Compare(other) -> (before | after | concurrent)`, `Copy() -> VectorClock`
2. Causal broadcast: a `Process` type that broadcasts messages to all other processes via TCP. Each message carries the sender's vector clock at send time
3. Causal delivery: a process delivers a received message only when the causal delivery condition is satisfied. Messages that arrive early are buffered
4. Delivery buffer: when a message is delivered, re-scan the buffer to check if previously buffered messages can now be delivered (cascade delivery)
5. Concurrent event detection: given two vector clocks, determine if they are causally related (one happened before the other) or concurrent (neither happened before the other)
6. Conflict resolution: when concurrent messages are detected, apply a deterministic tiebreaker (process ID ordering) and log the conflict
7. N-process network: processes connect to each other over TCP. Handle connection establishment, reconnection, and message framing
8. Event log: each process maintains an ordered log of all delivered events with their vector clocks
9. Happens-before visualization: generate a text-based diagram showing the partial order of events across processes, with arrows indicating causal dependencies
10. Metrics: track messages sent, received, buffered, delivered, buffer high-water mark, average delivery delay (time between receipt and delivery), concurrent event pairs detected

## Acceptance Criteria

- [ ] Causal delivery is never violated: if message A causally precedes message B, every process delivers A before B
- [ ] Concurrent messages are correctly identified: the `Compare` function returns `concurrent` when neither vector clock dominates the other
- [ ] Buffered messages are delivered as soon as their causal dependencies are satisfied, not held longer than necessary
- [ ] The system handles N=5 processes with 100 messages each, maintaining causal order under artificial network delays
- [ ] Happens-before visualization correctly renders the partial order for a test scenario
- [ ] All tests pass with `-race` flag
- [ ] Stress test: 10 processes sending messages concurrently with random delays produce no causal delivery violations
- [ ] Cascade delivery works: buffered messages are delivered immediately when their dependencies are satisfied by a newly delivered message
- [ ] Conflict detection logs concurrent event pairs with both vector clocks for debugging

## Going Further

- Replace vector clocks with interval tree clocks (ITC) to support dynamic process creation and destruction without pre-assigned process IDs
- Implement total order broadcast by combining causal broadcast with a deterministic tiebreaker (Lamport timestamp + process ID), and compare its guarantees with causal-only delivery
- Build a causally consistent replicated key-value store where updates are applied in causal order across replicas
- Implement bloom clocks as a space-efficient approximation of vector clocks and measure the trade-off between false positives in concurrency detection and memory savings

## Research Resources

- [Time, Clocks, and the Ordering of Events in a Distributed System (Lamport, 1978)](https://lamport.azurewebsites.net/pubs/time-clocks.pdf) -- the foundational paper on logical clocks and the happens-before relation
- [Virtual Time and Global States of Distributed Systems (Mattern, 1988)](https://www.vs.inf.ethz.ch/publ/papers/VirtTimeGlobworka.pdf) -- introduces vector clocks independently of Fidge
- [Timestamps in Message-Passing Systems That Preserve the Partial Ordering (Fidge, 1988)](https://fileadmin.cs.lth.se/cs/Personal/Amr_Rizk/courses/web_dist_sys/fidge88timestamps.pdf) -- the other independent invention of vector clocks
- [Lightweight Causal and Atomic Group Multicast (Birman et al., 1991)](https://www.cs.cornell.edu/home/rvr/papers/CATOCS.pdf) -- causal broadcast in the ISIS system
- [Conflict-free Replicated Data Types (Shapiro et al., 2011)](https://hal.inria.fr/inria-00609399/document) -- CRDTs use vector clocks for causal consistency in replicated state
