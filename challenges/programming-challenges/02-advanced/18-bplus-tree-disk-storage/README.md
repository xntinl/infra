# 18. B+ Tree Index for Disk Storage

<!--
difficulty: advanced
category: databases
languages: [rust]
concepts: [b-plus-tree, disk-io, page-management, buffer-pool, range-queries, concurrency, latch-crabbing]
estimated_time: 12-16 hours
bloom_level: evaluate
prerequisites: [rust-ownership, binary-search, tree-data-structures, file-io, basic-concurrency]
-->

## Languages

- Rust (stable)

## Prerequisites

- Binary search trees and balanced tree invariants
- File I/O and seeking within files (Rust `std::fs::File`)
- Fixed-size page/block abstractions for disk storage
- Basic concurrency primitives (`RwLock`, `Mutex`)
- Understanding of how databases use indices to avoid full table scans

## Learning Objectives

- **Implement** a B+ tree that persists internal and leaf nodes as fixed-size disk pages
- **Design** node splitting and merging algorithms that maintain tree balance after insertions and deletions
- **Evaluate** the trade-offs between branching factor, page size, and tree height for disk-bound workloads
- **Analyze** how leaf node chaining enables efficient range scans without tree traversal per key
- **Apply** latch crabbing to allow concurrent readers and writers without global locks
- **Implement** a buffer pool that caches hot pages in memory with LRU eviction

## The Challenge

Every relational database uses B+ trees as its primary index structure. Unlike binary search trees that store one key per node and live entirely in memory, B+ trees are designed for disk: each node fills an entire page (typically 4-16 KB), maximizing the fan-out ratio so that even billions of keys require only 3-4 disk reads to locate. All values live in leaf nodes, and leaves are linked together for sequential scans.

The key insight behind B+ trees is that disk I/O is measured in pages, not bytes. Reading one byte costs the same as reading 4096 bytes because the disk always transfers a full page. A binary search tree with one key per node wastes almost the entire page on every read. A B+ tree packs hundreds of keys into each node, making every page read maximally useful. With a branching factor of 250 (typical for 4KB pages with 8-byte keys), a tree of height 3 covers 250^3 = 15.6 million keys. Height 4 covers 3.9 billion.

Build a disk-resident B+ tree index. Internal nodes store keys and child page pointers. Leaf nodes store key-value pairs and maintain sibling pointers for range iteration. The tree must handle arbitrary insertions and deletions while maintaining balance, support efficient range queries via the leaf chain, and allow concurrent access through latch crabbing.

The distinction between B trees and B+ trees matters here. In a B tree, values can appear in internal nodes. In a B+ tree, all values live in leaves, and internal nodes contain only keys and child pointers. This design means internal nodes have a higher branching factor (more children per node, since they do not waste space on values) and leaf nodes form a linked list that supports sequential scans without returning to the tree root.

This is the data structure behind every `CREATE INDEX` statement you have ever run. After this challenge, you will understand why databases choose specific page sizes, how index bloat happens, why range scans are fast on indexed columns but slow on unindexed ones, and why `EXPLAIN ANALYZE` reports the number of page reads rather than the number of rows touched.

## Requirements

1. Define a fixed-size page format (default 4096 bytes) for both internal and leaf nodes, with a configurable page size and branching factor. The page header should encode node type, key count, parent pointer, and next-leaf pointer (for leaves)
2. Internal nodes store sorted keys and child page IDs. A node with N keys has N+1 child pointers. Leaf nodes store sorted key-value pairs (both i64 for this implementation) and a pointer to the next leaf
3. Implement `insert(key, value)` that traverses from root to the correct leaf, inserts the entry in sorted position, and splits the node upward if the leaf exceeds maximum capacity. When the root splits, create a new root with the two halves as children
4. Implement `delete(key)` that removes the entry from its leaf. When a leaf falls below minimum occupancy (typically 50%), attempt to borrow an entry from a sibling. If borrowing is not possible, merge the leaf with a sibling and remove the separator key from the parent. Merges can propagate upward
5. Implement `search(key) -> Option<Value>` as a point lookup that traverses from root to leaf using binary search at each level
6. Implement `range_scan(start, end) -> Iterator<(Key, Value)>` that descends to the leaf containing `start` and follows sibling pointers until reaching a key greater than `end`
7. Implement a buffer pool (minimum 64 pages) with LRU eviction and dirty page tracking. All page reads and writes go through the buffer pool, never directly to disk. Track pin counts to prevent eviction of actively-used pages
8. Implement bulk loading: given a sorted iterator of key-value pairs, build the tree bottom-up by filling leaves to a configurable fill factor (default 75%), linking leaves together, then constructing internal node levels one layer at a time until a single root remains
9. Implement latch crabbing for concurrent access: on read operations, acquire a read latch on the root then on the child, releasing the parent once the child latch is held. On write operations, acquire write latches top-down, releasing all ancestor latches once a child is confirmed safe (has room for inserts or is above minimum occupancy for deletes)
10. Persist the tree metadata (root page ID, page count, tree height) in a dedicated header page at page 0. On startup, read the header to locate the root

## Hints

<details>
<summary>Hint 1 -- Page layout</summary>

Use a fixed-size byte array for each page. The first few bytes hold a page header (node type, key count, next leaf pointer for leaves, parent page ID). Keys and values (or child pointers for internal nodes) follow in sorted order. Remaining space after the entries is free space. Calculate max keys per node from page size and key/value sizes at compile time.

For a 4096-byte page with 16 bytes of header space, 8-byte keys, and 8-byte values: a leaf node fits (4096 - 16) / 16 = 255 entries. An internal node with 8-byte keys and 4-byte child pointers fits (4096 - 16 - 4) / 12 = 339 keys (340 children). These numbers determine the tree's branching factor and height.
</details>

<details>
<summary>Hint 2 -- Splitting mechanics</summary>

When a leaf overflows, allocate a new page, move the upper half of keys to it, insert the middle key into the parent. The split key for a leaf goes UP to the parent AND remains in the new right leaf (since all values must be in leaves). For internal nodes, the split key goes UP to the parent and is REMOVED from both children.

Track the split path during descent so you know which parents to update. A split can propagate all the way to the root, which increases tree height by one. The root split is a special case: create a new root with the two halves as children.
</details>

<details>
<summary>Hint 3 -- Latch crabbing protocol</summary>

Acquire a read latch on the root, then on the child. If the child is safe (not full for inserts, not at minimum for deletes), release all ancestor latches. For writes, use write latches instead. This allows multiple readers to traverse concurrently and limits write contention to the subtree that actually changes.

A node is "safe" for insertion if it has room for one more key without splitting. A node is "safe" for deletion if it has more than the minimum number of keys. When a child is safe, no changes can propagate to ancestors, so ancestor latches can be released immediately.
</details>

<details>
<summary>Hint 4 -- Buffer pool design</summary>

Use a `HashMap<PageId, FrameId>` for the page table and a doubly-linked list for LRU ordering. Pin pages that are actively being read or written (pin count > 0 prevents eviction). Flush dirty pages to disk on eviction. Consider using `RwLock<Frame>` per frame so concurrent readers do not block each other.

The critical invariant: every `fetch_page` must be paired with an `unpin_page`. Missing unpins cause buffer pool exhaustion. Consider wrapping page access in an RAII guard that unpins on drop.
</details>

## Acceptance Criteria

- [ ] Point lookups return correct values for all inserted keys, including after splits
- [ ] Insertions that cause leaf splits propagate separator keys correctly to parent nodes
- [ ] Internal node splits create a new root when necessary, increasing tree height by one
- [ ] Deletions that cause underflow trigger borrowing from sibling or merging with sibling
- [ ] Range scans return all keys in [start, end] in sorted order via leaf chain traversal
- [ ] Range scans spanning multiple leaves follow sibling pointers without returning to internal nodes
- [ ] Buffer pool evicts LRU unpinned pages and flushes dirty pages to disk before eviction
- [ ] Buffer pool correctly handles pin counts: pinned pages are never evicted
- [ ] Bulk load builds a valid tree from 1 million sorted entries at least 5x faster than sequential inserts
- [ ] Concurrent readers and writers operate without deadlocks (test with 8+ threads, mixed read/write workload)
- [ ] Tree survives crash simulation: close the file, reopen, verify all committed data is present
- [ ] Inserting keys in reverse order produces the same query results as inserting in forward order
- [ ] Tree height stays at O(log_B(N)) where B is the branching factor

## Going Further

- Support variable-length keys (strings) by storing key prefixes in internal nodes and full keys in leaves
- Implement prefix compression: consecutive keys in a leaf that share a common prefix store only the differing suffix
- Add support for duplicate keys by appending a record ID to make each key unique
- Implement optimistic latch crabbing: assume writes will not cause splits, acquire read latches on the way down, restart with write latches only if a split actually occurs
- Build a secondary index that maps a non-unique column to a set of primary key values
- Benchmark your B+ tree against `std::collections::BTreeMap` for in-memory workloads and against SQLite for disk-bound workloads

## Research Resources

- [CMU 15-445: B+ Tree Index (Andy Pavlo)](https://15445.courses.cs.cmu.edu/fall2024/slides/07-trees1.pdf) -- lecture slides covering B+ tree structure, operations, and concurrency
- [CMU 15-445: More B+ Trees (Andy Pavlo)](https://15445.courses.cs.cmu.edu/fall2024/slides/08-trees2.pdf) -- bulk loading, duplicate keys, and variable-length keys
- [CMU 15-445: Index Concurrency Control](https://15445.courses.cs.cmu.edu/fall2024/slides/09-indexconcurrency.pdf) -- latch crabbing, optimistic locking, leaf scans
- [SQLite B-Tree Implementation](https://www.sqlite.org/btreemodule.html) -- production B-tree design in a real database
- [Database Internals by Alex Petrov, Ch. 2-4](https://www.databass.dev/) -- B-Tree variants, on-disk structures, page organization
- [InnoDB B+ Tree Page Structure](https://blog.jcole.us/2013/01/10/btree-index-structures-in-innodb/) -- Jeremy Cole's deep dive into MySQL's B+ tree pages
- [Lehman & Yao: Efficient Locking for Concurrent Operations on B-Trees](https://www.csd.uoc.gr/~hy460/pdf/p650-lehman.pdf) -- the original concurrent B-tree paper with right-link pointers
- [Andy Pavlo's Intro to Database Systems (YouTube)](https://www.youtube.com/playlist?list=PLSE8ODhjZXjbj8BMuIrRcacnQh20hmY9g) -- full CMU 15-445 lecture playlist, B+ tree lectures are 7-9
- [The Ubiquitous B-Tree (Comer, 1979)](https://dl.acm.org/doi/10.1145/356770.356776) -- the foundational survey paper on B-tree variants
