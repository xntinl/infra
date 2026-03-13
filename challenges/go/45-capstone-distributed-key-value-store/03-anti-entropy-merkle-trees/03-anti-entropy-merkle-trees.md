<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 3h
-->

# Anti-Entropy with Merkle Trees

## The Challenge

Implement an anti-entropy protocol using Merkle trees that enables replicas of each partition to efficiently detect and repair inconsistencies in the background. Each partition maintains a Merkle tree built over its key-value data, and replicas periodically exchange tree roots and then recursively compare subtrees to identify the exact key ranges that differ, transferring only the divergent data. Your implementation must handle the case where Merkle trees become stale due to ongoing writes by supporting incremental tree updates, handle hash collisions gracefully, and operate without pausing foreground read/write traffic. The anti-entropy process must be able to synchronize millions of keys between two replicas by exchanging only kilobytes of tree metadata when divergence is small.

## Requirements

1. Build a Merkle tree data structure where leaf nodes correspond to fixed-size key ranges (segments) of the partition's keyspace, and each leaf hash is computed over all key-value-timestamp triples in that range using SHA-256.
2. Internal nodes store the hash of their concatenated children's hashes, forming a binary tree of configurable depth (default 15 levels, yielding 32,768 leaf segments).
3. Implement incremental Merkle tree updates: when a key is written or deleted, only the leaf segment containing that key and its ancestors up to the root are recomputed, achieving O(log n) update cost per mutation.
4. Build an anti-entropy synchronization protocol where two replicas exchange their Merkle tree roots; if roots differ, they recursively exchange child hashes level by level, identifying only the leaf segments that differ.
5. For each differing leaf segment, the replica with the newer data (determined by comparing timestamps of individual keys) streams the divergent key-value pairs to the stale replica, which applies them using last-write-wins resolution.
6. The anti-entropy process must run in a background goroutine with a configurable interval (default 60 seconds) and must not block foreground reads or writes -- use a consistent snapshot of the partition's data for tree computation.
7. Handle the bootstrap case where a new or empty replica joins and has a completely different Merkle tree; the protocol should degrade to a full key-range transfer without crashing or excessive memory usage.
8. Implement protocol statistics: track and expose the number of segments compared, segments differing, keys transferred, bytes transferred, and synchronization duration for each anti-entropy round.

## Hints

- Divide the keyspace (e.g., the SHA-256 hash space) into 2^depth equal segments; each key maps to a segment by hashing and taking the top `depth` bits.
- Store the Merkle tree as an array-based binary heap for cache-friendly access: node `i` has children at `2i+1` and `2i+2`.
- For incremental updates, maintain a dirty set of leaf indices; batch recomputation at a configurable interval or before each anti-entropy round.
- Use a snapshot iterator (point-in-time read of the storage engine) to compute leaf hashes without locking out writes.
- The wire protocol for tree exchange can reuse your existing RPC layer: define opcodes for `MerkleRoot`, `MerkleChildren(nodeIndex)`, and `KeyRangeTransfer(startKey, endKey)`.
- To avoid transferring the full tree structure, send only the hashes at each level as the remote requests them.

## Success Criteria

1. Two replicas with 1 million identical keys produce matching Merkle roots and exchange zero data during anti-entropy.
2. After artificially corrupting 100 keys on one replica (out of 1 million), anti-entropy detects exactly the affected segments and transfers only the 100 divergent keys.
3. The number of network round trips for synchronization is bounded by the tree depth (15 levels), not the number of keys.
4. Incremental Merkle tree updates after a single `Put` complete in under 100 microseconds.
5. Anti-entropy running every 5 seconds does not degrade foreground `Get` throughput by more than 5%.
6. A completely empty replica bootstraps from a full replica of 1 million keys via anti-entropy without out-of-memory errors and completes within 30 seconds.
7. All synchronization statistics are accurately tracked and can be queried programmatically.

## Research Resources

- "Dynamo: Amazon's Highly Available Key-Value Store" (DeCandia et al., 2007) -- Merkle tree anti-entropy
- "Cassandra Anti-Entropy Repair" -- https://cassandra.apache.org/doc/latest/operating/repair.html
- "Merkle Tree" -- https://en.wikipedia.org/wiki/Merkle_tree
- Ralph Merkle, "A Digital Signature Based on a Conventional Encryption Function" (1987)
- Go `crypto/sha256` package for hash computation
- "Efficient Data Synchronization with Merkle Trees" -- research on segment-based comparison
