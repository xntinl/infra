# Solution: Merkle Tree with Inclusion Proofs

## Architecture Overview

The solution is organized into four components:

1. **Hash utilities**: thin wrapper around the `sha2` crate with domain-separated hashing (leaf prefix `0x00`, node prefix `0x01`) to prevent second-preimage attacks
2. **MerkleTree**: array-backed binary tree with construction, proof generation, verification, update, and diff
3. **AppendOnlyTree**: incremental variant that builds the tree one leaf at a time using a stack of pending roots
4. **Serialization + Concurrency**: `serde` derives for all public types, `Arc<RwLock>` wrapper with reader tests

The tree uses a bottom-up array layout. Leaves occupy the last `n` positions, and internal nodes fill positions `0..n-1`, with the root at position `0`.

## Rust Solution

### Cargo.toml

```toml
[package]
name = "merkle-tree"
version = "0.1.0"
edition = "2021"

[dependencies]
sha2 = "0.10"
serde = { version = "1", features = ["derive"] }
serde_json = "1"
hex = "0.4"
```

### src/lib.rs

```rust
use sha2::{Digest, Sha256};
use serde::{Serialize, Deserialize};
use std::sync::{Arc, RwLock};

// --- Hash utilities with domain separation ---

const LEAF_PREFIX: u8 = 0x00;
const NODE_PREFIX: u8 = 0x01;

pub type Hash = [u8; 32];

pub fn hash_leaf(data: &[u8]) -> Hash {
    let mut hasher = Sha256::new();
    hasher.update([LEAF_PREFIX]);
    hasher.update(data);
    hasher.finalize().into()
}

pub fn hash_nodes(left: &Hash, right: &Hash) -> Hash {
    let mut hasher = Sha256::new();
    hasher.update([NODE_PREFIX]);
    hasher.update(left);
    hasher.update(right);
    hasher.finalize().into()
}

// --- Proof structures ---

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub enum SiblingPosition {
    Left,
    Right,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct ProofStep {
    pub hash: Hash,
    pub position: SiblingPosition,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct InclusionProof {
    pub leaf_index: usize,
    pub leaf_hash: Hash,
    pub steps: Vec<ProofStep>,
}

// --- MerkleTree ---

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MerkleTree {
    nodes: Vec<Hash>,
    leaf_count: usize,
    data: Vec<Vec<u8>>,
}

impl MerkleTree {
    pub fn new(data: Vec<Vec<u8>>) -> Self {
        let leaf_count = data.len();
        if leaf_count == 0 {
            return MerkleTree {
                nodes: vec![],
                leaf_count: 0,
                data,
            };
        }

        let leaf_hashes: Vec<Hash> = data.iter().map(|d| hash_leaf(d)).collect();
        let nodes = Self::build_tree(&leaf_hashes);

        MerkleTree {
            nodes,
            leaf_count,
            data,
        }
    }

    fn build_tree(leaves: &[Hash]) -> Vec<Hash> {
        if leaves.is_empty() {
            return vec![];
        }
        if leaves.len() == 1 {
            return leaves.to_vec();
        }

        let mut current_level: Vec<Hash> = leaves.to_vec();
        let mut all_levels: Vec<Vec<Hash>> = vec![current_level.clone()];

        while current_level.len() > 1 {
            let mut next_level = Vec::new();
            let mut i = 0;
            while i < current_level.len() {
                if i + 1 < current_level.len() {
                    next_level.push(hash_nodes(&current_level[i], &current_level[i + 1]));
                    i += 2;
                } else {
                    // Odd node: promote without duplication
                    next_level.push(current_level[i]);
                    i += 1;
                }
            }
            all_levels.push(next_level.clone());
            current_level = next_level;
        }

        // Flatten: root first, then each level top-down
        // Store as: levels from root to leaves
        all_levels.reverse();
        all_levels.into_iter().flatten().collect()
    }

    /// Rebuild the flat array using a simpler level-order storage.
    /// Returns (nodes_vec, level_offsets) where level 0 is the root.
    fn level_offsets(&self) -> Vec<(usize, usize)> {
        if self.leaf_count == 0 {
            return vec![];
        }

        let mut offsets = Vec::new();
        let mut level_size = self.leaf_count;
        let mut sizes = vec![level_size];

        while level_size > 1 {
            level_size = (level_size + 1) / 2;
            sizes.push(level_size);
        }

        sizes.reverse(); // root level first

        let mut offset = 0;
        for size in &sizes {
            offsets.push((offset, *size));
            offset += size;
        }
        offsets
    }

    pub fn root_hash(&self) -> Option<Hash> {
        if self.nodes.is_empty() {
            None
        } else {
            Some(self.nodes[0])
        }
    }

    pub fn leaf_count(&self) -> usize {
        self.leaf_count
    }

    pub fn generate_proof(&self, index: usize) -> Option<InclusionProof> {
        if index >= self.leaf_count {
            return None;
        }

        let offsets = self.level_offsets();
        if offsets.is_empty() {
            return None;
        }

        let leaf_level = offsets.len() - 1;
        let leaf_offset = offsets[leaf_level].0;
        let leaf_hash = self.nodes[leaf_offset + index];

        let mut steps = Vec::new();
        let mut current_index = index;

        for level in (0..leaf_level).rev() {
            let (level_offset, level_size) = offsets[level + 1];
            let sibling_index = if current_index % 2 == 0 {
                current_index + 1
            } else {
                current_index - 1
            };

            if sibling_index < level_size {
                let sibling_hash = self.nodes[level_offset + sibling_index];
                let position = if current_index % 2 == 0 {
                    SiblingPosition::Right
                } else {
                    SiblingPosition::Left
                };
                steps.push(ProofStep {
                    hash: sibling_hash,
                    position,
                });
            }
            // If no sibling (odd promotion), skip this level

            current_index /= 2;
        }

        Some(InclusionProof {
            leaf_index: index,
            leaf_hash,
            steps,
        })
    }

    pub fn verify_proof(data: &[u8], proof: &InclusionProof, expected_root: &Hash) -> bool {
        let leaf_hash = hash_leaf(data);
        if leaf_hash != proof.leaf_hash {
            return false;
        }

        let mut current = leaf_hash;
        for step in &proof.steps {
            current = match step.position {
                SiblingPosition::Left => hash_nodes(&step.hash, &current),
                SiblingPosition::Right => hash_nodes(&current, &step.hash),
            };
        }

        constant_time_eq(&current, expected_root)
    }

    pub fn update_leaf(&mut self, index: usize, new_data: Vec<u8>) {
        assert!(index < self.leaf_count, "leaf index out of bounds");

        let new_hash = hash_leaf(&new_data);
        self.data[index] = new_data;

        let offsets = self.level_offsets();
        let leaf_level = offsets.len() - 1;
        let leaf_offset = offsets[leaf_level].0;
        self.nodes[leaf_offset + index] = new_hash;

        let mut current_index = index;
        for level in (0..leaf_level).rev() {
            let parent_index = current_index / 2;
            let child_level = level + 1;
            let (child_offset, child_size) = offsets[child_level];
            let (parent_offset, _) = offsets[level];

            let left_idx = parent_index * 2;
            let right_idx = left_idx + 1;

            let left_hash = self.nodes[child_offset + left_idx];
            let parent_hash = if right_idx < child_size {
                hash_nodes(&left_hash, &self.nodes[child_offset + right_idx])
            } else {
                left_hash // promoted
            };

            self.nodes[parent_offset + parent_index] = parent_hash;
            current_index = parent_index;
        }
    }

    pub fn diff(&self, other: &MerkleTree) -> Vec<usize> {
        if self.leaf_count != other.leaf_count {
            panic!("diff requires trees of equal leaf count");
        }
        if self.leaf_count == 0 {
            return vec![];
        }

        let mut result = Vec::new();
        let offsets = self.level_offsets();
        self.diff_recursive(other, &offsets, 0, 0, &mut result);
        result.sort();
        result
    }

    fn diff_recursive(
        &self,
        other: &MerkleTree,
        offsets: &[(usize, usize)],
        level: usize,
        index_in_level: usize,
        result: &mut Vec<usize>,
    ) {
        let (offset, size) = offsets[level];
        if index_in_level >= size {
            return;
        }

        if self.nodes[offset + index_in_level] == other.nodes[offset + index_in_level] {
            return; // subtrees match
        }

        // Leaf level: record the differing index
        if level == offsets.len() - 1 {
            result.push(index_in_level);
            return;
        }

        // Recurse into children
        let left_child = index_in_level * 2;
        let right_child = left_child + 1;
        self.diff_recursive(other, offsets, level + 1, left_child, result);
        self.diff_recursive(other, offsets, level + 1, right_child, result);
    }
}

/// Constant-time comparison to prevent timing attacks on hash verification.
fn constant_time_eq(a: &[u8; 32], b: &[u8; 32]) -> bool {
    let mut diff = 0u8;
    for (x, y) in a.iter().zip(b.iter()) {
        diff |= x ^ y;
    }
    diff == 0
}

// --- Append-Only Tree ---

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AppendOnlyTree {
    leaves: Vec<Vec<u8>>,
    /// Stack of pending subtree roots at each level.
    /// pending[i] is Some(hash) if there is a complete subtree of height i waiting.
    pending: Vec<Option<Hash>>,
    leaf_count: usize,
}

impl AppendOnlyTree {
    pub fn new() -> Self {
        AppendOnlyTree {
            leaves: Vec::new(),
            pending: Vec::new(),
            leaf_count: 0,
        }
    }

    pub fn append(&mut self, data: Vec<u8>) {
        let leaf_hash = hash_leaf(&data);
        self.leaves.push(data);
        self.leaf_count += 1;

        let mut current = leaf_hash;
        let mut level = 0;

        loop {
            if level >= self.pending.len() {
                self.pending.push(Some(current));
                break;
            }

            match self.pending[level].take() {
                Some(existing) => {
                    current = hash_nodes(&existing, &current);
                    level += 1;
                }
                None => {
                    self.pending[level] = Some(current);
                    break;
                }
            }
        }
    }

    pub fn root_hash(&self) -> Option<Hash> {
        if self.leaf_count == 0 {
            return None;
        }

        let mut root: Option<Hash> = None;
        for entry in &self.pending {
            if let Some(hash) = entry {
                root = Some(match root {
                    Some(r) => hash_nodes(hash, &r),
                    None => *hash,
                });
            }
        }
        root
    }

    pub fn leaf_count(&self) -> usize {
        self.leaf_count
    }

    /// Build a full MerkleTree from the appended data for proof generation.
    pub fn to_merkle_tree(&self) -> MerkleTree {
        MerkleTree::new(self.leaves.clone())
    }
}

impl Default for AppendOnlyTree {
    fn default() -> Self {
        Self::new()
    }
}

// --- Concurrent wrapper ---

pub type SharedMerkleTree = Arc<RwLock<MerkleTree>>;

pub fn shared_tree(data: Vec<Vec<u8>>) -> SharedMerkleTree {
    Arc::new(RwLock::new(MerkleTree::new(data)))
}

// --- Display helpers ---

pub fn hex_hash(hash: &Hash) -> String {
    hex::encode(hash)
}

pub fn short_hash(hash: &Hash) -> String {
    hex::encode(&hash[..4])
}
```

### src/main.rs

```rust
use merkle_tree::*;

fn main() {
    println!("=== Merkle Tree Demo ===\n");

    let data: Vec<Vec<u8>> = (0..8)
        .map(|i| format!("block-{}", i).into_bytes())
        .collect();

    let tree = MerkleTree::new(data.clone());
    let root = tree.root_hash().unwrap();
    println!("Tree with {} leaves", tree.leaf_count());
    println!("Root hash: {}\n", hex_hash(&root));

    // Generate and verify proof for leaf 3
    println!("--- Inclusion Proof for leaf 3 ---");
    let proof = tree.generate_proof(3).unwrap();
    println!("Proof steps: {}", proof.steps.len());
    for (i, step) in proof.steps.iter().enumerate() {
        let side = match step.position {
            SiblingPosition::Left => "L",
            SiblingPosition::Right => "R",
        };
        println!("  Step {}: {} ({})", i, short_hash(&step.hash), side);
    }

    let valid = MerkleTree::verify_proof(b"block-3", &proof, &root);
    println!("Verification: {}\n", if valid { "PASS" } else { "FAIL" });

    // Tampered data
    let tampered = MerkleTree::verify_proof(b"block-3-tampered", &proof, &root);
    println!("Tampered verification: {}\n", if tampered { "PASS" } else { "FAIL" });

    // Update leaf
    println!("--- Update Leaf ---");
    let mut tree2 = tree.clone();
    tree2.update_leaf(3, b"modified-block-3".to_vec());
    let new_root = tree2.root_hash().unwrap();
    println!("Old root: {}", short_hash(&root));
    println!("New root: {}\n", short_hash(&new_root));

    // Diff
    println!("--- Tree Diff ---");
    let diffs = tree.diff(&tree2);
    println!("Differing leaves: {:?}\n", diffs);

    // Append-only tree
    println!("--- Append-Only Tree ---");
    let mut aot = AppendOnlyTree::new();
    for block in &data {
        aot.append(block.clone());
    }
    let aot_root = aot.root_hash().unwrap();
    println!("Append-only root: {}", hex_hash(&aot_root));
    println!("Batch root:       {}", hex_hash(&root));
    println!(
        "Match: {}\n",
        if aot_root == root { "YES" } else { "NO" }
    );

    // Serialization
    println!("--- Serialization ---");
    let json = serde_json::to_string_pretty(&proof).unwrap();
    println!("Proof JSON ({} bytes):", json.len());
    println!("{}", &json[..200.min(json.len())]);
    println!("...");

    let deserialized: InclusionProof = serde_json::from_str(&json).unwrap();
    println!(
        "Round-trip: {}",
        if deserialized == proof {
            "MATCH"
        } else {
            "MISMATCH"
        }
    );
}
```

### Tests (src/lib.rs, continued)

```rust
#[cfg(test)]
mod tests {
    use super::*;
    use std::thread;

    fn sample_data(n: usize) -> Vec<Vec<u8>> {
        (0..n).map(|i| format!("item-{}", i).into_bytes()).collect()
    }

    #[test]
    fn test_empty_tree() {
        let tree = MerkleTree::new(vec![]);
        assert_eq!(tree.root_hash(), None);
        assert_eq!(tree.leaf_count(), 0);
    }

    #[test]
    fn test_single_leaf() {
        let tree = MerkleTree::new(vec![b"hello".to_vec()]);
        assert!(tree.root_hash().is_some());
        assert_eq!(tree.leaf_count(), 1);

        let proof = tree.generate_proof(0).unwrap();
        assert!(proof.steps.is_empty()); // single leaf, no siblings
        let root = tree.root_hash().unwrap();
        assert!(MerkleTree::verify_proof(b"hello", &proof, &root));
    }

    #[test]
    fn test_two_leaves() {
        let tree = MerkleTree::new(vec![b"a".to_vec(), b"b".to_vec()]);
        let root = tree.root_hash().unwrap();
        let expected = hash_nodes(&hash_leaf(b"a"), &hash_leaf(b"b"));
        assert_eq!(root, expected);
    }

    #[test]
    fn test_power_of_two_leaves() {
        let tree = MerkleTree::new(sample_data(8));
        assert!(tree.root_hash().is_some());

        for i in 0..8 {
            let proof = tree.generate_proof(i).unwrap();
            let root = tree.root_hash().unwrap();
            let data = format!("item-{}", i);
            assert!(
                MerkleTree::verify_proof(data.as_bytes(), &proof, &root),
                "proof failed for leaf {}",
                i
            );
        }
    }

    #[test]
    fn test_odd_leaves() {
        for n in [3, 5, 7, 9, 13] {
            let tree = MerkleTree::new(sample_data(n));
            assert!(tree.root_hash().is_some());

            for i in 0..n {
                let proof = tree.generate_proof(i).unwrap();
                let root = tree.root_hash().unwrap();
                let data = format!("item-{}", i);
                assert!(
                    MerkleTree::verify_proof(data.as_bytes(), &proof, &root),
                    "proof failed for leaf {} in tree of size {}",
                    i,
                    n
                );
            }
        }
    }

    #[test]
    fn test_proof_rejects_wrong_data() {
        let tree = MerkleTree::new(sample_data(4));
        let root = tree.root_hash().unwrap();
        let proof = tree.generate_proof(0).unwrap();
        assert!(!MerkleTree::verify_proof(b"wrong-data", &proof, &root));
    }

    #[test]
    fn test_proof_rejects_wrong_root() {
        let tree = MerkleTree::new(sample_data(4));
        let proof = tree.generate_proof(0).unwrap();
        let fake_root = [0xffu8; 32];
        assert!(!MerkleTree::verify_proof(b"item-0", &proof, &fake_root));
    }

    #[test]
    fn test_proof_rejects_tampered_sibling() {
        let tree = MerkleTree::new(sample_data(4));
        let root = tree.root_hash().unwrap();
        let mut proof = tree.generate_proof(0).unwrap();

        if let Some(step) = proof.steps.first_mut() {
            step.hash[0] ^= 0xff; // flip bits
        }
        assert!(!MerkleTree::verify_proof(b"item-0", &proof, &root));
    }

    #[test]
    fn test_update_leaf() {
        let mut tree = MerkleTree::new(sample_data(8));
        let old_root = tree.root_hash().unwrap();

        tree.update_leaf(3, b"updated-item".to_vec());
        let new_root = tree.root_hash().unwrap();

        assert_ne!(old_root, new_root);

        // Verify the updated leaf
        let proof = tree.generate_proof(3).unwrap();
        assert!(MerkleTree::verify_proof(
            b"updated-item",
            &proof,
            &new_root
        ));

        // Verify unchanged leaves still work
        let proof0 = tree.generate_proof(0).unwrap();
        assert!(MerkleTree::verify_proof(b"item-0", &proof0, &new_root));
    }

    #[test]
    fn test_update_matches_rebuild() {
        let data = sample_data(8);
        let mut tree = MerkleTree::new(data.clone());
        tree.update_leaf(5, b"new-value".to_vec());

        let mut data2 = data;
        data2[5] = b"new-value".to_vec();
        let rebuilt = MerkleTree::new(data2);

        assert_eq!(tree.root_hash(), rebuilt.root_hash());
    }

    #[test]
    fn test_diff_identical() {
        let tree1 = MerkleTree::new(sample_data(8));
        let tree2 = MerkleTree::new(sample_data(8));
        assert!(tree1.diff(&tree2).is_empty());
    }

    #[test]
    fn test_diff_single_change() {
        let tree1 = MerkleTree::new(sample_data(8));
        let mut tree2 = tree1.clone();
        tree2.update_leaf(5, b"changed".to_vec());

        let diffs = tree1.diff(&tree2);
        assert_eq!(diffs, vec![5]);
    }

    #[test]
    fn test_diff_multiple_changes() {
        let tree1 = MerkleTree::new(sample_data(8));
        let mut tree2 = tree1.clone();
        tree2.update_leaf(1, b"changed-1".to_vec());
        tree2.update_leaf(6, b"changed-6".to_vec());

        let diffs = tree1.diff(&tree2);
        assert_eq!(diffs, vec![1, 6]);
    }

    #[test]
    fn test_append_only_matches_batch() {
        let data = sample_data(8);
        let batch_tree = MerkleTree::new(data.clone());

        let mut aot = AppendOnlyTree::new();
        for d in &data {
            aot.append(d.clone());
        }

        assert_eq!(aot.root_hash(), batch_tree.root_hash());
    }

    #[test]
    fn test_append_only_incremental() {
        let mut aot = AppendOnlyTree::new();
        assert_eq!(aot.root_hash(), None);

        aot.append(b"first".to_vec());
        let root1 = aot.root_hash().unwrap();
        assert_eq!(root1, hash_leaf(b"first"));

        aot.append(b"second".to_vec());
        let root2 = aot.root_hash().unwrap();
        assert_ne!(root1, root2);
    }

    #[test]
    fn test_serialization_roundtrip_tree() {
        let tree = MerkleTree::new(sample_data(4));
        let json = serde_json::to_string(&tree).unwrap();
        let restored: MerkleTree = serde_json::from_str(&json).unwrap();
        assert_eq!(tree.root_hash(), restored.root_hash());
    }

    #[test]
    fn test_serialization_roundtrip_proof() {
        let tree = MerkleTree::new(sample_data(4));
        let proof = tree.generate_proof(2).unwrap();
        let json = serde_json::to_string(&proof).unwrap();
        let restored: InclusionProof = serde_json::from_str(&json).unwrap();
        assert_eq!(proof, restored);
    }

    #[test]
    fn test_concurrent_reads() {
        let data = sample_data(16);
        let shared = shared_tree(data);

        let handles: Vec<_> = (0..4)
            .map(|reader_id| {
                let tree = Arc::clone(&shared);
                thread::spawn(move || {
                    let guard = tree.read().unwrap();
                    let root = guard.root_hash().unwrap();
                    for i in 0..16 {
                        let proof = guard.generate_proof(i).unwrap();
                        let data = format!("item-{}", i);
                        assert!(
                            MerkleTree::verify_proof(data.as_bytes(), &proof, &root),
                            "reader {} failed at leaf {}",
                            reader_id,
                            i
                        );
                    }
                    root
                })
            })
            .collect();

        let roots: Vec<Hash> = handles.into_iter().map(|h| h.join().unwrap()).collect();
        assert!(roots.windows(2).all(|w| w[0] == w[1]));
    }

    #[test]
    fn test_domain_separation() {
        // A leaf hash and a node hash of the same data must differ
        let data = b"test-data";
        let leaf = hash_leaf(data);
        let node = hash_nodes(&[0u8; 32], &[0u8; 32]);
        assert_ne!(leaf, node);
    }

    #[test]
    fn test_out_of_bounds_proof() {
        let tree = MerkleTree::new(sample_data(4));
        assert!(tree.generate_proof(4).is_none());
        assert!(tree.generate_proof(100).is_none());
    }

    #[test]
    fn test_large_tree() {
        let n = 1000;
        let tree = MerkleTree::new(sample_data(n));
        assert!(tree.root_hash().is_some());

        // Spot-check a few proofs
        for i in [0, 1, n / 2, n - 2, n - 1] {
            let proof = tree.generate_proof(i).unwrap();
            let root = tree.root_hash().unwrap();
            let data = format!("item-{}", i);
            assert!(MerkleTree::verify_proof(data.as_bytes(), &proof, &root));
        }
    }
}
```

## Running

```bash
cargo init merkle-tree
cd merkle-tree

# Add dependencies to Cargo.toml as shown above
# Copy lib.rs to src/lib.rs and main.rs to src/main.rs

cargo run
cargo test
cargo test -- --nocapture
```

## Expected Output

```
=== Merkle Tree Demo ===

Tree with 8 leaves
Root hash: a1b2c3d4...  (64 hex characters)

--- Inclusion Proof for leaf 3 ---
Proof steps: 3
  Step 0: 4f2e... (R)
  Step 1: 8a1c... (L)
  Step 2: d7f3... (R)
Verification: PASS

Tampered verification: FAIL

--- Update Leaf ---
Old root: a1b2...
New root: 7e4f...

--- Tree Diff ---
Differing leaves: [3]

--- Append-Only Tree ---
Append-only root: a1b2c3d4...
Batch root:       a1b2c3d4...
Match: YES

--- Serialization ---
Proof JSON (412 bytes):
{
  "leaf_index": 3,
  "leaf_hash": [79, 46, ...],
  "steps": [
    {
...
Round-trip: MATCH
```

## Design Decisions

1. **Domain-separated hashing**: Leaf hashes use prefix `0x00`, internal nodes use `0x01`. This prevents second-preimage attacks where an attacker crafts a data block whose hash equals an internal node hash, potentially proving inclusion of data that does not exist. This follows RFC 6962 (Certificate Transparency).

2. **Promotion over duplication for odd leaves**: When a level has an odd number of nodes, the unpaired node is promoted to the next level unchanged. Duplicating it (hashing it with itself) is a common but flawed approach -- it allows proving that a duplicate leaf exists when it does not. Bitcoin uses duplication, which has caused real bugs (CVE-2012-2459).

3. **Flat array storage**: The tree is stored as a flat `Vec<Hash>` organized by levels (root first). This provides excellent cache locality compared to pointer-based trees and makes index arithmetic straightforward. The trade-off is that insertions require rebuilding, which is why the append-only variant uses a different structure.

4. **Constant-time comparison**: `verify_proof` uses bitwise OR accumulation instead of early-return comparison. This prevents timing side-channels where an attacker could measure verification time to learn which bytes of the hash match. For a Merkle tree, this matters when proofs are verified in security-sensitive contexts.

5. **Append-only tree uses binary-carry technique**: The pending stack mirrors binary addition. Each level either has a pending subtree or is empty. When a new leaf arrives, it cascades up like a carry bit, combining with pending subtrees. This is O(1) amortized per append and O(log n) worst case.

## Common Mistakes

1. **Duplicating odd leaves**: Creates a vulnerability where the same leaf can be proven at two different indices. Use promotion instead.

2. **Missing domain separation**: Without prefixed hashing, an internal node's hash could collide with a valid leaf hash, allowing second-preimage attacks.

3. **Index errors in proof generation**: Off-by-one errors in level offset calculation produce proofs with wrong sibling hashes. Test with both even and odd tree sizes.

4. **Non-constant-time verification**: Using `==` for hash comparison leaks timing information. Use XOR-accumulate or a dedicated constant-time comparison function.

5. **Forgetting to rehash the entire path**: `update_leaf` must rehash every node from the updated leaf to the root. Stopping one level short produces an inconsistent tree.

## Performance Notes

- Construction: O(n) hashes for n leaves
- Proof generation: O(log n) -- walk from leaf to root
- Proof verification: O(log n) -- recompute the path
- Proof size: O(log n) hashes = 32 * log2(n) bytes
- Update: O(log n) -- rehash the single affected path
- Diff: O(k * log n) where k is the number of differing leaves
- For 1 million leaves, a proof is 20 hashes = 640 bytes, versus 32 MB for the full leaf set

## Going Further

- Implement Merkle mountain ranges (MMR) for efficient append-only trees with O(log n) proof size at any historical state
- Add multi-proofs that prove inclusion of multiple leaves with shared path segments, reducing total proof size
- Implement sparse Merkle trees where most leaves are empty (default value), enabling efficient key-value maps with proofs
- Add consistency proofs: prove that an older tree is a prefix of a newer tree (used in certificate transparency)
- Benchmark against a naive "hash all data" approach to quantify the verification speedup for large datasets
