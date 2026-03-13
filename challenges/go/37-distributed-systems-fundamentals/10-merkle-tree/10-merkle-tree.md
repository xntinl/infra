# 10. Merkle Tree

<!--
difficulty: insane
concepts: [merkle-tree, hash-tree, data-verification, anti-entropy, efficient-sync, proof-of-inclusion]
tools: [go]
estimated_time: 60m
bloom_level: create
prerequisites: [consistent-hashing-ring, gossip-protocol]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of hash functions and tree data structures
- Familiarity with distributed data synchronization concepts

## Learning Objectives

- **Create** a Merkle tree implementation for efficient data verification and synchronization
- **Analyze** how Merkle trees enable O(log N) difference detection between datasets
- **Evaluate** Merkle tree applications in anti-entropy protocols and blockchain systems

## The Challenge

A Merkle tree is a binary tree where each leaf node contains the hash of a data block, and each internal node contains the hash of its children. Comparing two Merkle trees requires only comparing root hashes to detect any difference, and traversing down the tree to locate exactly which data blocks differ -- in O(log N) comparisons instead of O(N).

Distributed databases (Cassandra, DynamoDB) use Merkle trees for anti-entropy repair: two replicas compare their Merkle trees to find and fix inconsistencies without transferring all data. Build a Merkle tree that supports construction from data blocks, incremental updates, difference detection between trees, and proof-of-inclusion verification.

## Requirements

1. Implement a `MerkleTree` struct that builds a binary hash tree from a list of data blocks
2. Implement `RootHash() []byte` that returns the root hash (fingerprint of the entire dataset)
3. Implement `Diff(other *MerkleTree) []int` that returns the indices of data blocks that differ between two trees by traversing only the necessary branches
4. Implement `Update(index int, data []byte)` that updates a single data block and recomputes affected hashes up the tree in O(log N) time
5. Implement `ProofOfInclusion(index int) [][]byte` that returns the sibling hashes needed to verify that a specific data block is part of the tree
6. Implement `VerifyProof(data []byte, index int, proof [][]byte, rootHash []byte) bool` that verifies a proof of inclusion without the full tree
7. Write tests covering: tree construction, root hash determinism, difference detection accuracy, incremental updates, and proof verification
8. Benchmark difference detection vs naive comparison at various dataset sizes

## Hints

- Use `crypto/sha256` for hashing. Hash leaves as `H(data)` and internal nodes as `H(left_child_hash || right_child_hash)`.
- If the number of leaves is not a power of two, pad with empty leaves or duplicate the last leaf.
- For difference detection: compare root hashes. If they differ, recursively compare left subtrees and right subtrees. Only traverse branches where hashes differ.
- Proof of inclusion: the sibling hashes along the path from the leaf to the root. The verifier recomputes the root hash using the data block and the proof hashes.
- Store the tree as a flat array: for a node at index `i`, its children are at `2i+1` and `2i+2`, and its parent is at `(i-1)/2`.
- Anti-entropy: two replicas exchange root hashes. If they differ, exchange subtree hashes to locate exactly which ranges of keys are inconsistent, then sync only those ranges.

## Success Criteria

1. The Merkle tree correctly computes root hashes for arbitrary datasets
2. Identical datasets produce identical root hashes
3. Difference detection correctly identifies all differing data blocks
4. Difference detection visits O(log N + D) nodes where D is the number of differences
5. Incremental updates recompute only O(log N) hashes
6. Proof of inclusion correctly verifies membership without the full tree
7. Benchmarks show order-of-magnitude improvement over naive comparison for large datasets

## Research Resources

- [Merkle Tree (Wikipedia)](https://en.wikipedia.org/wiki/Merkle_tree) -- overview and properties
- [Cassandra Anti-Entropy Repair](https://docs.datastax.com/en/cassandra-oss/3.0/cassandra/operations/opsRepairNodesManualRepair.html) -- production use of Merkle trees
- [Dynamo Paper: Anti-Entropy](https://www.allthingsdistributed.com/files/amazon-dynamo-sosp2007.pdf) -- Section 4.7
- [Certificate Transparency: Merkle Proofs](https://certificate.transparency.dev/howctworks/) -- proof-of-inclusion in practice

## What's Next

Continue to [11 - Service Discovery](../11-service-discovery/11-service-discovery.md) to build a dynamic service registry.

## Summary

- Merkle trees provide O(log N) data verification through hierarchical hashing
- Root hash comparison detects any difference in the underlying data
- Difference detection traverses only branches with differing hashes
- Incremental updates recompute O(log N) hashes from leaf to root
- Proof of inclusion verifies membership with O(log N) sibling hashes
- Used in anti-entropy repair (databases), blockchain (transaction verification), and certificate transparency
