# 53. Build a Real-time Collaboration Engine
**Difficulty**: Insane

## Prerequisites
- Mastered: Phoenix Channels and WebSocket lifecycle, GenServer with persistent state, OTP supervision trees, ETS for shared state, binary serialization (`:erlang.term_to_binary/1`, Jason), PubSub, CRDT data structures (exercise 20 of this level)
- Study first: "Operational Transformation" (Ellis & Gibbs 1989 original paper), Yjs source code and YATA algorithm, Automerge paper ("A Conflict-Free Replicated JSON Datatype"), "Designing Data-Intensive Applications" chapter 5 (Replication), Myers diff algorithm

## Problem Statement
Build a real-time collaborative document editing engine — functionally equivalent to the core of Google Docs or Notion — where multiple users can concurrently edit the same document, see each other's cursors, and always converge to the same document state regardless of network reordering or temporary disconnects.

1. Implement Operational Transformation (OT) for plain-text documents: define the `insert(position, text)` and `delete(position, length)` operation types; implement the `transform(op1, op2) -> op1'` function that adjusts `op1` assuming `op2` was applied first; implement `compose(op1, op2) -> op` that merges two sequential ops; verify with the classic "AB" diamond convergence test.
2. Implement a CRDT alternative using a Logoot/YATA-style positional identifier scheme: each character has a globally unique fractional position (list of `{site_id, sequence}` pairs) that determines its place in the document without transformation; insertions and deletions commute without coordination; the document is stored as a sorted sequence of `{position, char, tombstone}` tuples.
3. Build presence awareness over Phoenix Channels: when a user joins a document, broadcast their cursor position (line/column) and selection range to all other users in the document; track cursor updates on every keystroke and throttle broadcasts to 50 ms intervals; on disconnect, remove the user's cursor from all peers within 1 second.
4. Implement per-user undo/redo: each user maintains a local operation history stack; `undo` reverses the user's last operation (not any concurrent operation); because concurrent operations may have shifted positions, undo must transform the inverse operation through all operations applied after it — do not use a global undo stack.
5. Implement conflict resolution for simultaneous edits: two users inserting at the same position must deterministically order their insertions (by user ID or Lamport timestamp as tiebreaker) so both clients converge to identical text; this must hold for sequences of concurrent operations, not just pairs.
6. Support offline editing: a client accumulates operations while disconnected; on reconnect, it sends the buffered operations with the Lamport clock value at which each was generated; the server applies them using OT (transforming against operations applied during the offline period) and sends the client the catch-up operations it missed.
7. Implement document versioning: every 100 operations (or on explicit save), snapshot the full document state to the database with a version number and the Lamport clock at that point; support `get_version(doc_id, version)` to retrieve a historical snapshot; implement `diff(version_a, version_b)` returning a list of OT operations that transforms A into B.
8. Implement section-level permissions: a document is divided into sections (paragraphs or blocks identified by ID); each section has an ACL mapping user IDs to `{owner, editor, viewer}`; operations targeting a section the user cannot edit are rejected at the server before being applied; viewers receive all updates but their local edits are blocked client-side.

## Acceptance Criteria
- [ ] OT convergence: any two sequences of concurrent operations applied in any order converge to the same document — verified by a property-based test generating random operation pairs and asserting `apply(doc, transform(op1, op2)) == apply(apply(doc, op2), op1)` for 10,000 random cases
- [ ] CRDT convergence: the YATA/Logoot implementation converges for concurrent inserts and deletes without requiring a server round-trip for ordering — verified by a simulation of 5 concurrent clients applying random edits and asserting all reach identical final state
- [ ] Presence: cursor positions from all connected users are visible within 100 ms of a cursor move; a user disconnecting removes their cursor from peers within 1 second; presence state is consistent (no ghost cursors after reconnect)
- [ ] Undo/redo: a user's undo reverses only their own operations, not concurrent operations from other users; undo in the presence of 3 concurrent users produces the correct document — verified by a multi-client test
- [ ] Conflict resolution: two users inserting at position 5 simultaneously always produce the same character order on both clients — verified by a test that replays the same concurrent operations in reverse order and asserts identical output
- [ ] Offline merge: a client that makes 20 edits while offline reconnects and the server merges them correctly — the final document equals what would have been produced if the offline edits had been applied online with OT against the concurrent server operations
- [ ] Versioning: `get_version/2` retrieves any historical snapshot; `diff/2` between two versions produces valid OT operations that when applied transform the older version into the newer one exactly
- [ ] Permissions: an `editor` user's operations are applied; a `viewer` user's operations are rejected with an error; an `owner` can modify the section ACL and the change takes effect on the next operation without reconnecting

## What You Will Learn
- Why distributed state is fundamentally hard: the impossibility of achieving both consistency and availability under partition (CAP), and how OT and CRDTs choose different trade-off points
- Operational Transformation mechanics: the `transform` and `compose` functions, why they are subtle to implement correctly, and the class of bugs that arise from naive implementations
- CRDT positional identifiers: how Logoot/YATA avoid the need for a central server ordering operations by embedding order into the identifier itself
- Lamport clocks and vector clocks: how logical time enables causal ordering without synchronized wall clocks
- Presence at scale: throttling, tombstoning, and the difference between ephemeral (presence) and durable (document) state in a Phoenix Channel context
- Per-user undo in a collaborative context: why a global undo stack is wrong and how to transform inverse operations through a concurrent history

## Hints (research topics, NO tutorials)
- The classic OT diamond test: client A applies `insert(0, "a")`, client B applies `insert(0, "b")` concurrently from the same base document; after transformation, both clients must see `"ab"` (or `"ba"` consistently) — use this as your first unit test
- Study how Yjs uses a doubly-linked list of `Item` structs with left/right origin references to resolve insertion conflicts without transformation
- For presence throttling: use `Process.send_after(self(), :flush_cursor, 50)` and coalesce updates within the window rather than broadcasting on every keystroke
- Offline merge: store `{lamport_clock, op}` pairs in the client buffer; on reconnect send the buffer sorted by clock; the server transforms each buffered op against the ops it applied between `client_clock` and `server_clock`
- For versioning: snapshot at a `GenServer` with `handle_cast(:snapshot, state)` every 100 ops; store snapshots in PostgreSQL as JSONB with an index on `(doc_id, version)`
- Property-based testing with `StreamData`: generate `op = %{type: :insert | :delete, pos: integer, text: string}`; verify commutativity and idempotency properties for 10k generated cases

## Reference Material
- Ellis & Gibbs (1989). "Concurrency Control in Groupware Systems" — the original OT paper
- Nicolaescu et al. (2016). "Yjs: A Framework for Near Real-Time P2P Shared Editing on Arbitrary Data Types" — https://github.com/yjs/yjs
- Kleppmann & Beresford (2017). "A Conflict-Free Replicated JSON Datatype" (Automerge) — https://arxiv.org/abs/1608.03960
- Weiss, Urso, Molli (2009). "Logoot: A Scalable Optimistic Replication Algorithm for Collaborative Editing on P2P Networks"
- "Designing Data-Intensive Applications" — Kleppmann, chapter 5 (Replication) and chapter 9 (Consistency and Consensus)
- Myers (1986). "An O(ND) Difference Algorithm and Its Variations" — for the diff/versioning component

## Difficulty Rating ★★★★★★★
Achieving convergence correctness in OT under all interleavings of concurrent operations is one of the hardest problems in distributed systems. The combination of OT correctness, CRDT implementation, per-user undo under concurrency, and offline merge makes this among the most algorithmically demanding exercises in the curriculum.

## Estimated Time
150–250 hours
