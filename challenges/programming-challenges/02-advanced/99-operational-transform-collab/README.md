# 99. Operational Transform Collaborative Editor

<!--
difficulty: advanced
category: distributed-systems
languages: [go]
concepts: [operational-transform, collaborative-editing, convergence, commutativity, causality, client-server-synchronization]
estimated_time: 8-10 hours
bloom_level: evaluate
prerequisites: [go-basics, goroutines, channels, string-manipulation, state-machines, concurrent-data-structures]
-->

## Languages

- Go (1.22+)

## Prerequisites

- Goroutines and channels for concurrent client simulation
- String/rune manipulation for character-level text operations
- State machines for client synchronization states
- Understanding of concurrency control and causality ordering
- Familiarity with client-server architecture patterns

## Learning Objectives

- **Evaluate** the convergence guarantees of Operational Transform versus alternative approaches (CRDTs, diff-patch) and articulate the trade-offs for real-time collaborative editing
- **Implement** the core OT transformation functions for insert and delete operations that satisfy the TP1 convergence property
- **Design** a server-based OT architecture with operation history, client state tracking, and transformation pipeline
- **Analyze** edge cases in concurrent editing: overlapping deletions, insertions at the same position, cascading transformations against operation history
- **Create** a multi-client simulation with configurable latency that demonstrates convergence under concurrent edits

## The Challenge

Collaborative text editing is deceptively hard. When two users type simultaneously in the same document, their operations are generated against different document states. User A inserts "X" at position 5, while User B deletes the character at position 3. When A receives B's delete, position 5 in A's document is no longer the correct position -- B's deletion shifted everything after position 3 left by one. If A applies B's operation without adjustment, the "X" ends up at the wrong position. The document diverges. This is not a theoretical problem -- it is the exact bug that every naive collaborative editor hits within seconds of concurrent use.

Operational Transform (OT) solves this by transforming operations against each other. Given two concurrent operations A and B that were generated against the same document state, OT computes A' (A transformed against B) and B' (B transformed against A) such that:

```
apply(apply(doc, A), B') = apply(apply(doc, B), A')
```

This is the **TP1 property** (Transformation Property 1). It guarantees that regardless of the order operations are applied, all clients converge to the same document state. Google Docs, Apache Wave (formerly Google Wave), and many collaborative editors are built on OT.

The server-based OT model simplifies the problem. The server maintains a canonical operation history. Each client sends operations tagged with the server revision they were generated against. The server transforms incoming operations against all operations in the history since that revision, then broadcasts the transformed operation to all other clients. Clients transform their pending (unsent) and in-flight operations against incoming server operations.

The subtlety is in the transformation functions. Consider: insert(3, 'a') concurrent with delete(3). Does the insert happen before or after the deleted position? The transformation must handle this consistently. Consider: insert(5, 'x') concurrent with insert(5, 'y'). Both want to insert at the same position -- you must break ties deterministically (e.g., by client ID) or the documents diverge.

Your implementation must handle these edge cases correctly and demonstrate convergence with multiple clients editing simultaneously under simulated network latency.

## Requirements

1. Define two operation types: `Insert(position, character)` and `Delete(position)`. Operations carry a client ID and the server revision they were generated against
2. Implement the transform function: given operations A and B, produce A' and B' satisfying TP1. Handle all nine cases: insert-insert, insert-delete, delete-insert, delete-delete, and same-position tie-breaking
3. Implement an `apply` function that executes an operation on a document (string), returning the new document
4. Build a `Server` that maintains the document state and operation history. On receiving a client operation, transform it against all operations since the client's revision, apply it, append to history, and broadcast to other clients
5. Build a `Client` that maintains a local document, pending operations queue, and in-flight operation tracking. Clients apply local operations immediately (optimistic), send to server, and transform pending operations against incoming server operations
6. Implement the client state machine: Synchronized (no pending ops), AwaitingAck (one op in flight), AwaitingAckWithBuffer (op in flight + buffered ops)
7. Simulate N clients (minimum 4) editing concurrently with configurable per-client latency (50ms-500ms range)
8. After all operations are applied and the system quiesces, verify that all clients and the server hold identical documents
9. Support operation composition: multiple consecutive inserts/deletes by the same client between server acknowledgments are composed into a single operation before sending
10. Provide a test harness that runs deterministic scenarios (fixed seeds, controlled timing) for reproducible convergence testing
11. Metrics: operations generated, operations transformed, transformation time, convergence time, final document length

## Hints

1. The transformation function is the heart of OT. Start with the simple cases. For `insert(p1, c) vs insert(p2, c)`: if p1 < p2, the first insert does not affect the second's position; if p1 > p2, the second insert shifts the first's position right by 1. The edge case is p1 == p2: break ties using client ID ordering (lower ID inserts first). For `insert vs delete`: if the insert position is after the delete position, shift the insert left by 1. For `delete vs delete` at the same position: one delete becomes a no-op (the character was already deleted). Get these nine cases right and the algorithm works.

2. The server is the single source of truth for operation ordering. When a client sends operation O generated at revision R, the server transforms O against history[R], history[R+1], ..., history[current]. Each transformation produces O', which is then transformed against the next operation. This sequential transformation against the history is called "transformation path". The order matters: transformations are not generally associative.

3. For the client state machine, model three states. In **Synchronized**, the client sends operations immediately. In **AwaitingAck**, one operation is in flight -- new local operations are buffered. In **AwaitingAckWithBuffer**, an operation is in flight and buffered operations exist. When the server acknowledges the in-flight operation, send the buffer (composed into one operation) and transition to AwaitingAck. When a server operation arrives, transform pending/buffered operations against it. This model is from the Jupiter system (Nichols et al., 1995).

4. Use channels for the network layer. Each client has a send channel (to server) and receive channel (from server). Add a configurable delay goroutine between client and server channels to simulate latency. This lets you test with deterministic timing without real network code.

5. Compose consecutive operations before sending. If a client types "abc" while waiting for an ack, instead of buffering three separate inserts, compose them into a single operation or a compound operation. This reduces the number of transformations the server must perform and simplifies the client state machine. For composition, an insert at position P followed by an insert at position P+1 can be composed into a multi-character insert.

6. Test the hardest case first: all clients inserting at position 0 simultaneously. This forces every operation to be transformed and exposes tie-breaking bugs immediately. If convergence works for this case, simpler cases will work too.

## Acceptance Criteria

- [ ] Insert and Delete operations correctly modify a document string
- [ ] Transform function handles all nine operation-pair combinations including same-position tie-breaking
- [ ] TP1 property holds: for any concurrent operations A and B, `apply(apply(doc, A), transform(B, A)) == apply(apply(doc, B), transform(A, B))`
- [ ] Server correctly transforms incoming operations against history and maintains consistent state
- [ ] Clients apply operations optimistically and converge with server state after synchronization
- [ ] Client state machine correctly manages Synchronized, AwaitingAck, and AwaitingAckWithBuffer states
- [ ] 4+ concurrent clients with different latencies converge to identical documents
- [ ] Operation composition reduces buffered operations before transmission
- [ ] Deterministic test scenarios produce reproducible results across runs
- [ ] All tests pass with `-race` flag with zero data races detected
- [ ] At least 8 test scenarios: single client, two clients no conflict, two clients same position, multiple clients high contention, rapid typing, delete-heavy, mixed operations, latency variation
- [ ] Metrics accurately report operations generated, transformed, and convergence time

## Going Further

- **Undo support**: Implement operation inversion and transform undo operations against concurrent edits. This requires tracking the transformation history for each operation.
- **Rich text OT**: Extend operations to support formatting (bold, italic) with retain/insert/delete semantics similar to Quill's Delta format.
- **Tree OT**: Implement OT for tree-structured documents (JSON, XML) where operations target paths instead of positions.
- **OT vs CRDT comparison**: Implement the same collaborative editor using CRDTs (e.g., RGA or YATA) and compare convergence guarantees, memory usage, and implementation complexity.

## Starting Points

- **Ellis and Gibbs (1989)**: The original OT paper, "Concurrency Control in Groupware Systems", defines the transformation property and basic algorithm. Start here for the theoretical foundation.
- **Nichols et al., Jupiter system (1995)**: Introduces the client-server OT model with the state machine you will implement. This is the practical architecture used by Google Docs.
- **Sun et al., GOT/GOTO (1998)**: Formalizes TP1 and TP2, analyses correctness conditions, and identifies bugs in earlier OT algorithms. Read this to understand why some transformation functions break under certain conditions.

## Research Resources

- [Ellis, Gibbs: Concurrency Control in Groupware Systems (1989)](https://dl.acm.org/doi/10.1145/67544.66963) -- the foundational OT paper defining transformation properties
- [Nichols et al.: High-Latency, Low-Bandwidth Windowing in the Jupiter Collaboration System (1995)](https://dl.acm.org/doi/10.1145/215585.215706) -- the client-server OT model and state machine
- [Sun et al.: Achieving Convergence, Causality Preservation, and Intention Preservation (1998)](https://dl.acm.org/doi/10.1145/274444.274447) -- formal correctness analysis of OT algorithms
- [Google: What's different about the new Google Docs (2010)](https://drive.googleblog.com/2010/09/whats-different-about-new-google-docs.html) -- OT in production at scale
- [Daniel Spiewak: Understanding and Applying Operational Transformation](https://web.archive.org/web/20220101000000*/http://www.codecommit.com/blog/java/understanding-and-applying-operational-transformation) -- practical walkthrough with implementation details
- [Martin Kleppmann: Designing Data-Intensive Applications, Chapter 5](https://dataintensive.net/) -- modern comparison of OT and CRDT approaches
- [Tiny-OT reference implementation (GitHub)](https://github.com/nicktogo/tinyot) -- minimal OT implementation for study
