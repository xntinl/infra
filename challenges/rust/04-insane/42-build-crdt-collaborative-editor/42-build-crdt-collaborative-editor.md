# 42. Build a CRDT Collaborative Editor

**Difficulty**: Insane

## The Challenge

Collaborative text editing is one of the hardest problems in distributed systems. When multiple users simultaneously type into the same document, their edits must converge to the same final state regardless of the order in which operations are received. Google Docs uses Operational Transformation (OT), which requires a central server to serialize operations. CRDTs (Conflict-free Replicated Data Types) offer a fundamentally different approach: they guarantee convergence through mathematical properties of the data structure itself, enabling true peer-to-peer collaboration with no central authority.

Your task is to build a real-time collaborative text editor backed by a sequence CRDT. You will implement either the RGA (Replicated Growable Array) algorithm or the YATA (Yet Another Transformation Approach) algorithm used by Yjs, one of the most successful CRDT libraries in production. This is not a toy: your implementation must handle character-by-character insertion and deletion, maintain causal ordering through vector clocks, resolve conflicts deterministically when concurrent edits target the same position, and synchronize state between peers over WebSocket connections. You will also implement document state serialization, incremental updates (deltas), and garbage collection of tombstones.

This challenge sits at the intersection of distributed systems theory, data structure design, and real-time networking. You will need to deeply understand happens-before relationships, partial ordering, the CAP theorem's implications for collaborative editing, and why naive approaches like "last writer wins" destroy user intent. A correct implementation means that if Alice inserts "hello" at position 5 and Bob simultaneously inserts "world" at position 5, both peers end up with either "helloworld" or "worldhello" — but critically, they both agree on which one, without any coordination beyond eventually receiving each other's operations.

---

## Acceptance Criteria

### Core CRDT Implementation
- [ ] Implement a sequence CRDT using either RGA or YATA algorithm
- [ ] Each character has a unique identifier consisting of (replica_id, sequence_number) or (replica_id, logical_clock)
- [ ] Unique identifiers are totally ordered: define a deterministic comparison function that breaks ties between concurrent operations
- [ ] Support `insert(position, character)` that translates a local index to a CRDT position between two existing identifiers
- [ ] Support `delete(position)` using tombstone markers rather than physical removal
- [ ] The visible document (excluding tombstones) is always derivable from the internal CRDT state
- [ ] Concurrent inserts at the same position are resolved deterministically based on identifier ordering
- [ ] The data structure satisfies the Strong Eventual Consistency (SEC) property: replicas that have received the same set of operations (in any order) have identical state
- [ ] Implement `insert_range(position, string)` for batch insertion of multiple characters with a single operation (internally creates one CRDT item per character, but batches the position calculation)
- [ ] Implement `delete_range(start, length)` for batch deletion that tombstones a contiguous range of characters
- [ ] Expose a `to_string()` method that efficiently reconstructs the visible document by traversing the internal structure and skipping tombstones
- [ ] Implement `len()` that returns the number of visible characters without traversing the entire structure (maintain a running count)
- [ ] Support Unicode: each CRDT item represents a Unicode scalar value (`char`), not a byte; handle multi-byte UTF-8 correctly throughout
- [ ] Implement `char_at(position)` that returns the character at a given visible index in O(log n) time using the index structure

### Causal Ordering and Vector Clocks
- [ ] Implement vector clocks where each entry tracks the latest known sequence number from each replica
- [ ] Every operation carries the vector clock of the issuing replica at the time of generation
- [ ] Operations are only applied when all causally preceding operations have been applied (causal delivery)
- [ ] Implement a buffer for operations that arrive out of causal order, applying them when their dependencies are satisfied
- [ ] Detect concurrent operations: two operations are concurrent if neither's vector clock dominates the other
- [ ] Implement vector clock merge on receiving remote operations
- [ ] Vector clock comparison: implement partial order (less_than, concurrent, equal) operations
- [ ] Handle the case where a replica goes offline, makes edits, and reconnects with a batch of operations
- [ ] Optimize vector clock storage: for documents with many replicas, consider sparse representation (only store non-zero entries)
- [ ] Implement Lamport timestamps as a secondary ordering mechanism for operations from the same replica
- [ ] The vector clock size grows with the number of replicas that have ever participated — document this trade-off and its implications for long-lived documents

### Conflict Resolution
- [ ] When two replicas insert at the same position concurrently, the tie-breaking rule produces identical ordering on all replicas
- [ ] If using RGA: implement the "right-child wins" or timestamp-based interleaving strategy
- [ ] If using YATA: implement the left-origin/right-origin conflict resolution algorithm
- [ ] Handle the "interleaving anomaly" where concurrent insertions of multi-character sequences should not interleave character-by-character
- [ ] Concurrent delete + insert at the same position: the inserted character survives, the deleted character is tombstoned
- [ ] Concurrent deletes of the same character: both replicas converge (idempotent delete)
- [ ] Write property-based tests (using `proptest` or `quickcheck`) that generate random concurrent edit sequences and verify convergence
- [ ] Document and test the conflict resolution rules with at least 10 specific scenarios showing expected outcomes for concurrent operations
- [ ] Verify that the ordering of concurrent inserts is consistent across all replicas regardless of the order in which they receive operations
- [ ] Handle the "last writer wins" scenario for metadata (e.g., document title) using a separate LWW register CRDT alongside the sequence CRDT

### WebSocket Synchronization
- [ ] Implement a WebSocket server (using `tokio-tungstenite` or `axum` with WebSocket support) that relays operations between connected peers
- [ ] Each peer maintains its own replica of the CRDT
- [ ] Operations are serialized to a binary or JSON wire format and broadcast to all connected peers
- [ ] Implement the sync protocol: when a new peer connects, it receives the full document state (state-based merge) or the complete operation history
- [ ] Support incremental sync: after initial state transfer, only new operations (deltas) are sent
- [ ] Handle peer disconnection and reconnection gracefully, syncing missed operations
- [ ] Implement awareness protocol: broadcast cursor positions and user presence information
- [ ] Operations are applied locally first (optimistic), then broadcast (no round-trip latency for local edits)
- [ ] Implement backpressure: if a slow peer cannot keep up with incoming operations, buffer them up to a limit and then drop the connection gracefully
- [ ] Support both relay server (star topology) and direct peer-to-peer (mesh topology) connection modes
- [ ] Implement message batching on the wire: accumulate multiple operations over a short window (e.g., 10ms) and send as a single WebSocket message to reduce framing overhead

### Document State Management
- [ ] Serialize the full CRDT state to bytes (for persistence or initial sync)
- [ ] Deserialize a CRDT state and reconstruct the document
- [ ] Implement delta encoding: given two vector clocks, compute the minimal set of operations to bring a stale replica up to date
- [ ] Implement snapshot + log compaction: periodically create a snapshot and discard old operations
- [ ] Garbage collection of tombstones: safely remove tombstones when all replicas have observed the delete (requires protocol coordination)
- [ ] Undo/redo support: track the inverse of each local operation and apply it to undo
- [ ] Save and load documents to/from disk with full CRDT metadata
- [ ] Implement version history: store snapshots at regular intervals to allow viewing the document at any point in time
- [ ] Implement a "diff" operation: given two document states (or two vector clocks), compute a human-readable diff showing what changed
- [ ] Support document forking: clone a document's CRDT state into an independent replica that can diverge and optionally merge back later

### Data Structure Internals
- [ ] The internal structure uses a tree or linked-list variant that supports efficient index-to-identifier lookups
- [ ] Implement a "position cache" or skip list to avoid O(n) traversal for every index-based operation
- [ ] Benchmark: inserting 100,000 characters sequentially should complete in under 1 second
- [ ] Benchmark: applying 100,000 random remote operations should complete in under 2 seconds
- [ ] Memory overhead per character should be documented and ideally under 100 bytes for the CRDT metadata
- [ ] Implement a "block" optimization: consecutive characters inserted by the same replica in sequence are stored as a single block with one identifier
- [ ] Implement split and merge operations on blocks: when a remote insert targets the middle of a block, split it into two blocks and insert between them
- [ ] The index structure supports O(log n) lookup from visible position to internal CRDT identifier
- [ ] Maintain a length cache: each node in the index tree tracks the number of visible (non-tombstoned) characters in its subtree
- [ ] Implement a debug mode that validates the integrity of the internal structure after every operation: tree balance, length cache consistency, identifier ordering

### Operational Transformation Comparison
- [ ] Implement a basic OT engine (insert/delete transforms) for the same collaborative editing scenario
- [ ] Implement the `transform(op1, op2)` function for all operation type pairs: insert-insert, insert-delete, delete-insert, delete-delete
- [ ] Demonstrate a case where OT requires a central server to serialize operations, while your CRDT works peer-to-peer
- [ ] Benchmark both approaches for throughput (operations per second) and latency
- [ ] Benchmark memory usage: OT (stores operation history) vs CRDT (stores per-character metadata)
- [ ] Write a test that shows OT divergence when operations are applied in different orders without a central server, while the CRDT converges
- [ ] Document the theoretical trade-offs: OT has lower per-character overhead but requires a central server; CRDTs work peer-to-peer but consume more memory
- [ ] Implement a simple central server for OT to demonstrate the architectural difference

### Testing and Verification
- [ ] Unit tests for every CRDT operation in isolation
- [ ] Integration tests with 2 replicas performing concurrent edits and verifying convergence
- [ ] Integration tests with 3+ replicas with complex interleaving patterns
- [ ] Fuzz testing: generate random operation sequences, apply them in random orders to multiple replicas, assert all replicas converge
- [ ] Test network partitions: split replicas into two groups, let each group edit independently, heal the partition, verify convergence
- [ ] Test with realistic editing patterns: sequential typing, copy-paste, select-and-replace
- [ ] Measure and report convergence time under various network latency simulations
- [ ] Test the "split brain" scenario: two groups of replicas edit independently for an extended period, then reconnect and merge
- [ ] Test with 10+ concurrent replicas to verify scalability of the conflict resolution algorithm
- [ ] Test undo/redo across replicas: verify that undoing an operation on one replica correctly propagates to all others
- [ ] Verify that the serialized CRDT state round-trips correctly: serialize, deserialize, compare visible document and internal state
- [ ] Test garbage collection: verify that tombstones are only removed when safe and that the document remains correct after GC
- [ ] Performance regression tests: ensure that key benchmarks do not degrade as features are added

---

## Starting Points

These are real resources to study before and during implementation:

1. **Martin Kleppmann - "A Conflict-Free Replicated JSON Datatype"** (2017 paper) — Foundational paper on CRDTs for structured data; the sequence CRDT section directly applies to text editing. Read Sections 3 and 4 carefully for the RGA-based approach.

2. **Yjs Source Code** (https://github.com/yjs/yjs) — The most widely deployed CRDT library for collaborative editing. Study `src/structs/Item.js` for the YATA data structure, `src/utils/StructStore.js` for storage, and `src/utils/encoding.js` for the wire format. The conflict resolution is in `Item.integrate()`.

3. **Automerge Source Code** (https://github.com/automerge/automerge) — A Rust-native CRDT library. Study `rust/automerge/src/op_tree/` for the B-tree based operation storage, `rust/automerge/src/sequence/` for the sequence CRDT, and the columnar encoding in `rust/automerge/src/columnar/`.

4. **Roh, Jeon, Kim, Lee - "Replicated Growable Array (RGA)"** (2011 paper) — The original RGA paper. Defines the linked-list structure where each element points to its predecessor, and new elements are inserted after their reference element with timestamp-based ordering.

5. **Nicolaescu, Jahde, Derntl, Klamma - "YATA: Yet Another Transformation Approach"** (2016 paper) — Describes the algorithm used by Yjs. Each item has a left origin and right origin; conflicts are resolved by comparing origins. Simpler than RGA in some ways but handles interleaving differently.

6. **Russ Cox - "CRDTs: The Hard Parts"** (Strange Loop 2020 talk by Martin Kleppmann) — Video and transcript available. Covers the interleaving anomaly, moving items, and other non-obvious CRDT challenges that simple blog posts skip.

7. **Seph Gentle - "A Simple CRDT Text Type"** (https://github.com/josephg/diamond-types) — Diamond Types is a high-performance Rust CRDT implementation. Study the `crate::list` module for a production-quality sequence CRDT implementation with aggressive optimizations.

8. **Victor Grishchenko - "Causal Trees"** (2010 paper) — An alternative approach to sequence CRDTs using a tree structure where each character is a node whose parent is the character after which it was inserted. Elegant but has known interleaving issues.

9. **`tokio-tungstenite`** (https://github.com/snapview/tokio-tungstenite) — Async WebSocket library for Rust. Use this for the peer-to-peer synchronization layer. Study the examples for both client and server usage.

10. **Kevin Jahns - "Are CRDTs Suitable for Shared Editing?"** (2020 blog post) — The Yjs author discusses performance characteristics, memory usage, and real-world deployment challenges. Includes benchmarks comparing Yjs to Automerge v1.

---

## Hints

1. Start with the simplest possible sequence CRDT: a sorted list of `(ID, char, deleted: bool)` tuples where ID is `(replica_id, counter)`. Insert finds the right position by scanning for the left neighbor's ID and then sorting concurrent inserts by replica_id. This is O(n) per operation but gets the semantics right. Optimize later.

2. The hardest part is not the CRDT algorithm itself — it is the translation between "index in the visible document" and "position in the CRDT structure." When the user types at position 5 in their editor, you need to skip over tombstones to find the 5th visible character, then determine which CRDT ID to use as the insertion reference. Build a separate index that maps visible positions to internal positions and keep it updated.

3. For vector clocks, use a `HashMap<ReplicaId, u64>` where `ReplicaId` is a unique identifier (UUID or similar). The partial order is: `vc1 <= vc2` iff for every key `k`, `vc1[k] <= vc2[k]`. Two vector clocks are concurrent if neither dominates the other. This is fundamental to determining which operations need conflict resolution.

4. The causal delivery buffer is essentially a priority queue of pending operations. When you receive an operation with vector clock `vc`, check if all entries in `vc` (minus the sender's own entry) are satisfied by your local vector clock. If not, buffer it. After applying any operation, re-check the buffer — applying one operation may unblock others.

5. For the YATA algorithm specifically: each item stores `left_origin` (the ID of the character that was to its left when it was created) and `right_origin` (the ID of the character that was to its right). When integrating a remote item between two existing items, scan right from `left_origin` and compare `right_origin` values to determine exact position. This is how Yjs handles conflicts without interleaving.

6. The "block optimization" is crucial for performance. If replica A types "hello" sequentially, store it as one block `{id: (A, 5), content: "hello", len: 5}` instead of five separate items. When a remote insert splits the block, split it into two blocks. This reduces memory overhead by 5-10x for typical editing patterns.

7. For the WebSocket sync protocol, implement two phases: (a) state sync — when a peer connects, exchange vector clocks, compute the delta (operations the other peer hasn't seen), and send them; (b) live sync — after initial sync, broadcast every new local operation immediately. Use a message enum: `SyncStep1(vector_clock)`, `SyncStep2(missing_ops)`, `Update(operation)`, `Awareness(cursor_info)`.

8. Tombstone garbage collection requires consensus: you can only delete a tombstone when you are certain all replicas have processed the delete. One approach: periodically exchange "minimum vector clocks" (the component-wise minimum of all known replica clocks). Any tombstone created before this minimum clock is safe to remove. Be careful — a replica that was offline for a long time may rejoin and need those tombstones.

9. For the OT comparison, implement the two transformation functions: `transform_insert_insert(op1, op2)` and `transform_insert_delete(op1, op2)`. Show that without a central server serializing operations, applying `transform(op1, op2)` on one replica and `transform(op2, op1)` on another can diverge in specific edge cases (the "TP2 puzzle"). This is why Google Docs needs a server.

10. Property-based testing is your strongest verification tool here. Define the property: "Generate N replicas. Generate a random sequence of operations distributed across replicas. Apply operations in random order to each replica (respecting causal order). Assert all replicas produce the same visible document." Run this with thousands of random seeds. If it ever fails, you have a convergence bug.

11. For undo/redo, do not try to simply reverse the operation log. Instead, track each operation's "inverse" at creation time: the inverse of `insert(id, char)` is `delete(id)`, and the inverse of `delete(id)` is `restore(id)`. Undo applies the inverse, which is itself a CRDT operation that gets broadcast. This means undo is collaborative: if Alice undoes her insert, Bob sees the character disappear.

12. When benchmarking memory overhead, count the per-character cost: the unique ID (typically 16 bytes for replica UUID + 8 bytes for counter), the left/right origin references (16-24 bytes each), the tombstone flag (1 byte), the character itself (1-4 bytes for UTF-8), and any tree/list pointers (8-16 bytes). A naive implementation uses 80-120 bytes per character. With block optimization, amortize the fixed costs across consecutive characters.

13. For the skip list or tree index optimization, consider a balanced binary tree where each node stores the count of visible (non-tombstoned) characters in its subtree. This lets you do index-to-position lookups in O(log n). Automerge uses a B-tree variant for this. Alternatively, use a simple skip list with O(log n) expected time per operation.

14. Handle UTF-8 carefully. A "character" in your CRDT could be a Unicode scalar value (`char` in Rust) or a byte. If you use `char`, you need to handle the fact that `String::len()` returns byte count, not character count. Consider using `String::chars().nth(n)` or maintaining a separate char-indexed structure. For production quality, consider grapheme clusters, but that adds significant complexity.

15. For the wire protocol, consider using `serde` with `bincode` for efficient binary serialization, or `serde_json` for debuggability during development. Each message should include: the operation type (insert/delete), the unique ID, the character (for insert), the reference position (left neighbor ID), and the sender's vector clock. Keep messages small — in a real editor, users generate hundreds of operations per minute.

16. Test the "partition and heal" scenario thoroughly: create 3 replicas A, B, C. Let A and B communicate (but not C) while all three edit. Then let B and C communicate (but not A). Then reconnect all three. All must converge. This exercises the full causal ordering and conflict resolution machinery.

17. For the awareness protocol (cursor positions and user presence), broadcast a lightweight message every 500ms containing: replica ID, user name/color, cursor position (visible index), and selection range (if any). Do not route these through the CRDT — they are ephemeral and can be lost without consequence. Use a separate "awareness" channel alongside the CRDT operation channel.

18. When implementing the document state serialization, consider two formats: a "snapshot" format that encodes the full tree structure (efficient for initial sync but larger), and a "updates" format that encodes a sequence of operations (efficient for deltas but requires replaying). Yjs uses both: a document is a snapshot plus a log of updates since the snapshot. This hybrid approach balances initial load time with incremental sync efficiency.

19. For the interleaving anomaly: if Alice types "hello" and Bob concurrently types "world", a naive CRDT might produce "hweolrllod" by interleaving characters. The YATA algorithm prevents this by using left/right origins to keep contiguous sequences together. If you use RGA, you must implement a similar mechanism — for example, by grouping characters with consecutive IDs from the same replica and treating them as a unit during conflict resolution.

20. Consider implementing a "rich text" extension: instead of single characters, each element in the CRDT carries formatting attributes (bold, italic, font size). Formatting is represented as a separate CRDT layer (typically a map CRDT) keyed by character ranges. This is how Yjs integrates with editors like ProseMirror and Quill. It is a significant extension but demonstrates the power of composing multiple CRDTs.

21. For document forking and merging: forking is trivial (clone the CRDT state). Merging is also trivial if both forks continued using the same CRDT — just apply all operations from one fork to the other. The CRDT guarantees convergence regardless of divergence duration. This is fundamentally different from text-based merge tools (like `git merge`) which can produce conflicts. A CRDT never has conflicts, though it may produce surprising results when the divergent edits are semantically incompatible.

22. When profiling memory usage, use `std::mem::size_of` to measure struct sizes and a custom allocator (or `jemalloc` with profiling) to measure actual heap usage. Compare against the theoretical minimum: for a document with N characters and R replicas, the minimum metadata is roughly N * (size_of_id + size_of_tombstone_flag) + R * size_of_vector_clock_entry. Any overhead beyond this represents data structure bookkeeping (tree pointers, skip list levels, block metadata).

23. For the real-time sync demonstration, build a minimal terminal UI using `crossterm` or `ratatui`. Display the document text with a cursor for each connected peer (in different colors). Show the vector clock state and operation count in a status bar. This makes the CRDT behavior tangible: you can watch characters from different peers appearing, conflicts being resolved, and convergence happening in real time after a simulated partition heals.

24. The `insert_range` and `delete_range` operations are critical for practical performance. Real editors do not insert characters one at a time — copy-paste inserts thousands of characters at once. Your CRDT must handle this without generating thousands of individual network messages. Batch the range into a single operation on the wire, and on the receiving end, apply the characters as a block. This requires extending your wire protocol to support multi-character operations.

25. Be aware that CRDT text types have a known limitation called "tombstone bloat." In a document where text is repeatedly typed and deleted (as in any real editing session), tombstones accumulate indefinitely until garbage collected. A document that currently contains 1,000 characters may have 100,000 tombstones from past edits. This is why garbage collection is not optional for production use — without it, performance degrades linearly with the total number of operations ever performed, not the current document size.

26. For the comparison with OT, focus on the specific divergence scenario: Replica A inserts "x" at position 2, Replica B deletes position 1. Without a central server, A applies B's delete first and transforms its insert to position 1. B applies A's insert first (at position 2), then applies the delete at position 1, deleting the wrong character. This is the classic OT divergence bug that demonstrates why OT needs a central server to impose a total order. Your CRDT handles this naturally because operations reference unique IDs, not positions.
