<!-- difficulty: advanced -->
<!-- category: cryptography -->
<!-- languages: [rust] -->
<!-- concepts: [merkle-tree, sha256, cryptographic-proofs, serialization, concurrent-access, audit-log] -->
<!-- estimated_time: 7-10 hours -->
<!-- bloom_level: analyze, evaluate -->
<!-- prerequisites: [rust-basics, sha256-hashing, binary-trees, serde-serialization, arc-rwlock] -->

# Challenge 28: Merkle Tree with Inclusion Proofs

## Languages

Rust (stable, latest edition)

## Prerequisites

- Binary tree construction and traversal in Rust (box-based or array-based)
- SHA-256 hashing concepts: fixed output size, avalanche effect, one-way property (the `sha2` crate handles the computation)
- Understanding of cryptographic hash properties: collision resistance (finding two inputs with the same hash is computationally infeasible), preimage resistance (finding an input that hashes to a given output is infeasible), second-preimage resistance (finding a different input with the same hash as a given input is infeasible)
- Familiarity with `Arc<RwLock<T>>` for concurrent read access patterns
- Basic serialization with `serde` (JSON or binary formats)
- Understanding of logarithmic vs linear complexity and why it matters for large datasets

## Learning Objectives

- **Implement** a Merkle tree data structure with SHA-256 hashing, domain-separated leaf/node
  prefixes, and correct handling of odd-leaf cases
- **Analyze** the space and verification complexity of inclusion proofs versus transmitting the
  full dataset, and why O(log n) proof size enables lightweight clients
- **Evaluate** trade-offs between different strategies for handling odd numbers of leaves
  (duplication vs. promotion) and the security implications of each
- **Design** an append-only variant suitable for audit logs where historical entries are
  immutable, using the binary-carry accumulation technique
- **Create** efficient tree update and diff operations that rehash or compare only the
  affected O(log n) path from leaf to root

## The Challenge

A Merkle tree is a binary tree where every leaf contains the hash of a data block and every internal node contains the hash of its two children. The root hash is a single fixed-size fingerprint of the entire dataset. Changing any single byte in any leaf changes the root hash. This property -- a compact commitment to arbitrarily large data -- makes Merkle trees foundational to modern distributed systems.

The power of a Merkle tree lies in inclusion proofs. To prove that a specific piece of data exists in the tree, you only need to provide the sibling hashes along the path from that leaf to the root -- log(n) hashes for n leaves. The verifier recomputes the root hash using these siblings and checks it against the known root. This is how Bitcoin SPV clients verify transactions without downloading the entire blockchain, how certificate transparency logs prove inclusion, and how systems like IPFS and Git track content integrity.

Consider the economics: for a tree with 1 million leaves, an inclusion proof requires only 20 hashes (640 bytes). Without Merkle proofs, proving inclusion requires transmitting the entire dataset (32 MB of hashes). That is a 50,000x reduction in proof size.

Build a Merkle tree that supports construction from arbitrary data, inclusion proof generation and verification, efficient tree comparison for diff detection, and an append-only mode for audit logs. The tree must handle odd numbers of leaves, support updates with minimal rehashing, and allow concurrent read access.

A critical design decision is how to handle odd numbers of leaves. The naive approach -- duplicating the unpaired leaf -- creates a second-preimage vulnerability where an attacker can prove that a non-existent duplicate leaf is in the tree. The correct approach is to promote the unpaired node to the next level without duplication, and to use domain-separated hashing (different hash prefixes for leaves vs internal nodes) as specified in RFC 6962.

## Requirements

1. Implement `MerkleTree` that constructs a tree from a `Vec<Vec<u8>>` of data blocks, hashing each leaf with SHA-256
2. Use domain-separated hashing: leaf hashes use prefix byte `0x00` before the data, internal nodes use prefix byte `0x01` before concatenating children hashes. This prevents second-preimage attacks where an internal node hash could be confused with a leaf hash
3. Internal nodes hash the concatenation of their children's hashes: `H(0x01 || left_hash || right_hash)`
4. Handle odd numbers of leaves by promoting the unpaired node to the next level without duplication
5. Implement `root_hash() -> Option<[u8; 32]>` returning the tree's root hash (`None` for empty trees)
6. Implement `leaf_count() -> usize` returning the number of data blocks in the tree
7. Implement `generate_proof(index: usize) -> Option<InclusionProof>` that produces the sibling hashes and their positions (left or right) along the path from leaf to root. Return `None` for out-of-bounds indices
8. Define `InclusionProof` as a struct containing: the leaf index, the leaf hash, and a `Vec<ProofStep>` where each step has a sibling hash and a `SiblingPosition` enum (`Left` or `Right`)
9. Implement `verify_proof(data: &[u8], proof: &InclusionProof, root: &[u8; 32]) -> bool` as a static method that verifies without access to the full tree. Use constant-time comparison for the final root hash check to prevent timing side-channels
10. Implement `update_leaf(index: usize, new_data: Vec<u8>)` that updates a single leaf and rehashes only the O(log n) nodes on the path to the root
11. Implement `diff(other: &MerkleTree) -> Vec<usize>` that compares two trees of equal size and returns indices of leaves that differ, using top-down recursive hash comparison to skip matching subtrees in O(k log n) time where k is the number of differing leaves
12. Implement an `AppendOnlyTree` variant where leaves can only be appended, never modified or removed. It must produce the same root hash as constructing a `MerkleTree` from the same data in batch
13. Implement `Serialize`/`Deserialize` (via `serde` derives) for `MerkleTree`, `InclusionProof`, `ProofStep`, and `SiblingPosition`
14. Wrap the tree in `Arc<RwLock<MerkleTree>>` for concurrent read access, with tests spawning multiple reader threads that verify proofs simultaneously
15. Write comprehensive tests covering: empty tree, single leaf, two leaves, power-of-two leaves (4, 8, 16), odd leaves (3, 5, 7, 9, 13), proof verification for every leaf in a tree, tampered data rejection, tampered sibling hash rejection, wrong root rejection, update correctness (update matches full rebuild), diff detection (0, 1, and multiple diffs), append-only equivalence, serialization round-trip, large tree spot checks (1000 leaves)

## Hints

1. Structure the tree as a flat vector (array-based binary tree) rather than a pointer-based
   tree. For a tree with `n` leaves, store nodes level-by-level. The root is at index 0, its
   children at 1 and 2, and so on. For non-perfect trees (odd leaves), track level boundaries
   with a vector of `(offset, size)` tuples. This makes both proof generation and update
   operations straightforward index arithmetic without pointer chasing.

2. For inclusion proofs, walk from the target leaf to the root. At each level, determine
   the sibling's index (if your index is even, sibling is index+1; if odd, sibling is
   index-1). Include the sibling's hash and whether it was on the left or right. The verifier
   reconstructs the path: if the sibling is on the left, compute `H(0x01 || sibling || current)`;
   if on the right, compute `H(0x01 || current || sibling)`. The final hash must equal the
   known root. When a level has an odd number of nodes and the target is the unpaired last node,
   skip that level (no sibling to include).

3. The diff algorithm is where Merkle trees shine. Start at the root: if root hashes match,
   the trees are identical (return empty diff). If they differ, recurse into both children.
   Only descend into subtrees whose hashes differ. This is O(k log n) where k is the number
   of differing leaves. For a tree with 1 million leaves where only 3 have changed, the
   diff visits about 60 nodes instead of 1 million.

4. For the append-only variant, maintain a `Vec<Option<Hash>>` as a stack of pending subtree
   roots at each level. When a new leaf arrives, it either fills an empty slot at level 0 or
   combines with the existing level-0 entry to form a level-1 subtree, cascading upward like
   binary addition carries. To compute the root hash, fold the pending entries from bottom to
   top. This is the same algorithm used by certificate transparency logs (RFC 6962 Section 2.1).

## Acceptance Criteria

- [ ] Tree of two known data blocks produces a manually verifiable root hash:
      `H(0x01 || H(0x00 || block0) || H(0x00 || block1))`
- [ ] Inclusion proofs verify correctly for every leaf in trees of size 1, 4, 7, and 16
- [ ] Verification rejects: wrong data, tampered sibling hash, swapped sibling position, wrong root
- [ ] Domain-separated hashing is used (leaf prefix `0x00`, node prefix `0x01`)
- [ ] Odd number of leaves is handled by promotion, not duplication
- [ ] `update_leaf` changes only the path to root and produces the same result as a full rebuild
      with modified data
- [ ] `diff` returns only differing leaf indices and does not visit matching subtrees
- [ ] `AppendOnlyTree` produces the same root hash as batch `MerkleTree` construction for sizes
      1 through 20
- [ ] `AppendOnlyTree` supports converting to a full `MerkleTree` for proof generation
- [ ] Serialization round-trip preserves tree and proof structures exactly (JSON)
- [ ] Deserialized proof verifies correctly against the original root hash
- [ ] Concurrent read test with 4+ threads verifying proofs simultaneously completes without
      deadlock, all threads see the same root hash
- [ ] `generate_proof` returns `None` for out-of-bounds indices
- [ ] `root_hash` returns `None` for empty trees
- [ ] Large tree test: 1000 leaves, spot-check 5 random proofs
- [ ] All tests pass with `cargo test`
- [ ] No external dependencies beyond `sha2`, `serde`, `serde_json`, and `hex`

## Research Resources

- [Merkle Tree -- Wikipedia](https://en.wikipedia.org/wiki/Merkle_tree) -- foundational concepts and applications
- [Certificate Transparency RFC 6962](https://datatracker.ietf.org/doc/html/rfc6962) -- sections 2.1 and 2.1.1 define the Merkle tree hash used in CT logs, including the leaf/node hash prefix convention
- [Bitcoin Developer Guide: Merkle Trees](https://developer.bitcoin.org/devguide/block_chain.html#merkle-trees) -- how Bitcoin constructs Merkle trees for transaction verification
- [Second Preimage Attack on Merkle Trees](https://flawed.net.nz/2018/02/21/attacking-merkle-trees-with-a-second-preimage-attack/) -- why duplicating odd leaves is dangerous and how to prevent it with domain separation
- [Merkle Tree in Rust (tutorial)](https://medium.com/@_ricardobalk/merkle-tree-in-rust-an-introduction-to-the-merkle-tree-data-structure-in-rust-a85e93cb01f5) -- walkthrough of a basic Rust implementation
- [sha2 crate documentation](https://docs.rs/sha2/latest/sha2/) -- the SHA-256 implementation you will use
- [serde documentation](https://serde.rs/) -- serialization framework for Rust
- [Certificate Transparency: How It Works](https://certificate.transparency.dev/howctworks/) -- practical application of append-only Merkle trees
- [Crosby & Wallach: Authenticated Dictionaries (2009)](https://www.usenix.org/legacy/event/sec09/tech/full_papers/crosby.pdf) -- efficient authenticated data structures using Merkle trees
- [Rust `hex` crate](https://docs.rs/hex/latest/hex/) -- hex encoding for readable hash output in tests and debugging
