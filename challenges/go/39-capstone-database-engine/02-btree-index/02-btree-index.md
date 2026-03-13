<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 3h
-->

# B+Tree Index

The B+Tree is the dominant on-disk indexing structure in virtually every relational database because it provides guaranteed O(log n) lookups, efficient range scans, and graceful behavior under concurrent modifications. Your task is to implement a complete, disk-backed B+Tree index in Go. Unlike the toy in-memory versions found in textbooks, yours must operate on fixed-size disk pages, manage page splits and merges correctly, support both point lookups and range scans via linked leaf pages, handle duplicate keys, and integrate with the buffer pool manager you will build in a subsequent exercise. This is the data structure that will power your database engine's secondary indexes.

## Requirements

1. Define a page-based B+Tree where each node occupies exactly one 4096-byte disk page. Internal nodes store keys and child page IDs. Leaf nodes store key-value pairs (where values are record IDs / page-offset pairs) and a `nextLeaf` pointer to the right sibling leaf page for range scans. Design the on-page binary layout with a page header (node type, key count, right sibling pointer, parent pointer) followed by a sorted key array and a pointer/value array.

2. Implement `Insert(key []byte, value RecordID) error` that traverses from the root to the correct leaf, inserts the key-value pair in sorted order, and handles leaf splits when the page is full. Leaf splits must propagate a copy of the middle key up to the parent internal node. Internal node splits must push the middle key up further. Handle the special case of root splits by creating a new root page.

3. Implement `Search(key []byte) (RecordID, bool, error)` that performs a point lookup by traversing from root to leaf in O(log_B n) page reads where B is the branching factor. Return the associated record ID if the key exists. For duplicate keys, return the first occurrence.

4. Implement `RangeScan(startKey, endKey []byte) (*Iterator, error)` that returns an iterator over all key-value pairs where `startKey <= key <= endKey`. The iterator must follow `nextLeaf` pointers to traverse across leaf pages. Implement `Next() (key []byte, value RecordID, ok bool)` on the iterator. An open-ended scan (nil endKey) must scan to the end of the tree.

5. Implement `Delete(key []byte) (bool, error)` that removes a key-value pair from the tree. When a leaf node falls below 50% capacity after deletion, attempt to redistribute keys with a sibling. If redistribution is not possible (sibling is also at minimum), merge the leaf with its sibling and remove the separator key from the parent. Handle cascading merges up to the root, and shrink the tree height if the root has only one child after a merge.

6. Implement page serialization and deserialization so that nodes can be written to and read from a `PageStore` interface (`ReadPage(pageID uint64) ([]byte, error)`, `WritePage(pageID uint64, data []byte) error`). For this exercise, back the `PageStore` with a flat file where page N starts at byte offset `N * 4096`. Track free pages with a free list stored on a dedicated metadata page.

7. Support variable-length keys up to 256 bytes by using an indirection scheme within each page: store a sorted array of offsets pointing to key data packed from the end of the page toward the middle, enabling efficient binary search on the offset array while accommodating variable-length keys without wasting space.

8. Write tests covering: insertion of 100,000 random keys and verification that all can be found, range scans returning correct sorted subsets, deletion of every other key followed by re-verification, tree height verification (a tree with 100,000 entries and order 200 should have height <= 3), concurrent read/write tests with a read-write mutex, and a benchmark comparing B+Tree lookups to a Go map for datasets that exceed available memory simulation.

## Hints

- The branching factor (order) of your B+Tree is determined by how many keys fit in a 4096-byte page. For 8-byte fixed keys with 8-byte pointers, internal nodes hold roughly 250 keys; for variable-length keys, it depends on average key size.
- For binary search within a page, `sort.Search` works well with a comparator over the key array.
- When splitting, be careful about whether you copy the middle key up (leaf split) or push it up (internal split) -- this distinction is critical for correctness.
- For the free list, a simple approach is to maintain a linked list of free page IDs stored in a dedicated page, with each free page pointing to the next free page.
- Use `bytes.Compare` for key comparison to get lexicographic ordering of variable-length keys.
- Leaf `nextLeaf` pointers form a singly-linked list at the leaf level, enabling efficient full-tree scans without touching internal nodes.

## Success Criteria

1. Inserting 100,000 random 16-byte keys into the B+Tree and searching for each one returns the correct record ID with zero false negatives.
2. Range scans return results in strictly sorted key order and include exactly the keys within the specified bounds.
3. After deleting 50,000 of 100,000 keys, all remaining keys are still findable and all deleted keys return not-found.
4. The tree height remains logarithmic: no more than 4 levels for 100,000 entries with a page size of 4096 bytes.
5. Serialization round-trips produce identical trees: write the entire tree to disk pages, read it back, and verify every key is recoverable.
6. The free list correctly reclaims pages from merged nodes and reuses them for subsequent inserts.
7. No data races under concurrent reads and writes verified with `-race` flag.

## Research Resources

- [B+Tree Visualization (University of San Francisco)](https://www.cs.usfca.edu/~galles/visualization/BPlusTree.html)
- [CMU 15-445 Database Systems - B+Tree Index](https://15445.courses.cs.cmu.edu/fall2024/slides/07-trees.pdf)
- [Modern B-Tree Techniques (Goetz Graefe)](https://w6113.github.io/files/papers/btreesurvey-graefe.pdf)
- [How Databases Store Data on Disk (Page Layout)](https://arpitbhayani.me/blogs/pages-in-database/)
- [BoltDB B+Tree Implementation in Go](https://github.com/etcd-io/bbolt)
