<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 3.5h
-->

# Concurrent B-Tree

## The Challenge

Implement a concurrent B+ tree that supports high-throughput parallel reads and writes using optimistic lock coupling (OLC), also known as the "latch crabbing" technique with optimistic validation. A B+ tree stores all values in leaf nodes and uses internal nodes purely for routing, making it ideal for range scans and disk-oriented storage engines. Your implementation must handle concurrent insertions that trigger node splits, concurrent deletions that trigger node merges, and concurrent range scans that traverse multiple leaf nodes, all without holding a global lock. The tree must support millions of concurrent operations per second by allowing readers to traverse without acquiring any locks (using version counters for validation) and writers to hold only the minimal set of node locks required for structural modifications.

## Requirements

1. Implement a B+ tree with a configurable order (fanout, default 128) where internal nodes store keys and child pointers, and leaf nodes store key-value pairs with a `next` pointer to the right sibling for efficient range scans.
2. Implement optimistic lock coupling for reads: each node has a version counter that is incremented at the start and end of every structural modification; a reader loads the version before reading, performs its work without locking, then validates the version hasn't changed; if it has, the reader restarts from the root.
3. Implement pessimistic lock coupling for writes: a writer traverses the tree acquiring and releasing write locks in a top-down crabbing fashion -- a lock on a child is acquired before the lock on the parent is released, but the parent lock is released early if the child is not full (for insert) or not at minimum occupancy (for delete), limiting lock scope.
4. Handle node splits: when an insertion causes a leaf to exceed its capacity, split the leaf into two halves, insert the middle key into the parent, and handle cascading splits up to the root; a root split increases the tree height.
5. Handle node merges and redistributions: when a deletion causes a leaf to fall below minimum occupancy, either redistribute keys with a sibling or merge with a sibling, updating the parent; handle cascading merges.
6. Implement `Range(startKey, endKey)` that performs a lock-free traversal to the starting leaf using optimistic lock coupling, then follows `next` pointers across leaves, validating each leaf's version before reading its keys.
7. Support generic key and value types using Go generics with an `Ordered` constraint on keys.
8. Implement bulk loading: given a sorted stream of key-value pairs, build the B+ tree bottom-up by filling leaf nodes to capacity and constructing internal nodes layer by layer, which is significantly faster than individual inserts.

## Hints

- Each node's version counter uses the lowest bit as a "locked" flag: odd version means locked, even means unlocked. A writer sets the version to odd (locked), performs its modification, then increments to even (unlocked). A reader that observes an odd version knows a write is in progress and must wait or restart.
- Use `atomic.Uint64` for version counters to ensure visibility across goroutines.
- Lock crabbing optimization: when descending for an insert, if a child node is less than full (has room for one more key), the parent definitely won't need to be modified, so release the parent lock immediately.
- For leaf `next` pointers, use `atomic.Pointer[LeafNode]` so range scans can follow them without locks.
- Bulk loading: fill leaves left to right, then build internal nodes bottom-up by taking the first key of every Nth leaf (where N is the order), creating internal nodes that point to groups of leaves.
- Test with `-race` and also with stress tests that mix inserts, deletes, and range scans at equal proportions.
- The LMDB and BoltDB codebases are good references for B+ tree implementation in practice.

## Success Criteria

1. Insert 1 million keys and verify that all can be retrieved with `Get`, the tree structure satisfies B+ tree invariants (all leaves at the same depth, node occupancy within bounds), and leaf `next` pointers form a correctly sorted linked list.
2. 32 concurrent goroutines performing random inserts, deletes, and point lookups pass `go test -race` with no data races.
3. Range scans return keys in sorted order and are consistent (no missing keys that were present throughout the scan, no phantom keys that were never inserted).
4. Concurrent inserts achieve at least 2 million ops/sec on 8 cores with 8 writer goroutines.
5. Concurrent reads (32 goroutines, read-only workload) achieve at least 10 million ops/sec via optimistic lock coupling with no writer contention.
6. Bulk loading 10 million sorted keys completes in under 5 seconds and produces a tree with optimal space utilization (leaves filled to at least 90% capacity).
7. Tree height remains O(log_B n) after 1 million random inserts and deletes.

## Research Resources

- Viktor Leis et al., "The Adaptive Radix Tree: ARTful Indexing for Main-Memory Databases" (2013) -- OLC technique
- "The Ubiquitous B-Tree" (Comer, 1979) -- classic B-tree survey
- "Efficient Locking for Concurrent Operations on B-Trees" (Lehman & Yao, 1981) -- B-link trees
- BoltDB source code -- https://github.com/etcd-io/bbolt -- pure Go B+ tree
- Google btree package -- https://github.com/google/btree -- Go B-tree implementation
- "Modern B-Tree Techniques" (Graefe, 2011) -- comprehensive survey of B-tree variants
