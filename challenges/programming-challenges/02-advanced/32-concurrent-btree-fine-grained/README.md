# 32. Concurrent B-Tree with Fine-Grained Locking

```yaml
difficulty: advanced
languages: [go, rust]
time_estimate: 10-16 hours
tags: [btree, concurrency, locking, latch-crabbing, data-structures]
bloom_level: [analyze, evaluate]
```

## Prerequisites

- B-tree fundamentals: structure invariants, search, insert with split, delete with merge and redistribution
- Difference between B-tree and B+ tree (where values live, leaf linking)
- Concurrency primitives: mutexes, read-write locks, lock ordering, latch vs lock semantics
- Deadlock prevention strategies: lock ordering, two-phase locking, timeout-based recovery
- Memory safety under concurrent access: data races, use-after-free in lock-free code
- Go: `sync.RWMutex`, `sync/atomic`, goroutines, `testing.B` benchmarks
- Rust: `Arc`, `RwLock`, `Mutex`, `AtomicU64`, `criterion` or built-in benchmarks

## Learning Objectives

After completing this challenge you will be able to:

- **Evaluate** trade-offs between coarse-grained and fine-grained locking strategies for tree structures, and articulate when each is appropriate
- **Implement** latch crabbing (lock coupling) to allow concurrent readers and writers on different subtrees of the same tree
- **Analyze** deadlock potential in hierarchical locking schemes and apply prevention strategies based on lock ordering invariants
- **Design** a range scan operation that provides snapshot consistency under concurrent modifications without holding locks on the entire scan range
- **Judge** when optimistic locking outperforms pessimistic locking based on workload characteristics (read ratio, tree depth, split frequency)
- **Compare** your implementation's throughput scaling against a global-lock baseline across varying concurrency levels

## The Challenge

B-trees are the backbone of nearly every database index and filesystem. When multiple threads or goroutines access a B-tree concurrently, the simplest approach is a global lock: one mutex protecting the entire tree. This serializes all operations. Under 8 threads, a global-lock B-tree achieves roughly the same throughput as a single thread because every operation waits for every other operation to finish.

Database storage engines solve this with latch crabbing (also called lock coupling): acquire a latch on the child before releasing the parent, and release the parent early when the child is "safe" (will not split or merge from the current operation). This allows operations on different subtrees to proceed in parallel.

The key insight is the difference between "safe" and "unsafe" nodes. A node is safe for insertion if it has room for one more key without splitting. A node is safe for deletion if it has more than the minimum number of keys. When the child is safe, the parent is unaffected by the operation, so its latch can be released immediately. When the child is unsafe, the parent must be held because the operation may propagate upward (split or merge).

Build a B-tree that supports concurrent search, insert, delete, and range scan operations using fine-grained per-node latches. Implement both pessimistic (standard latch crabbing) and optimistic (assume no structural changes, restart on failure) protocols. The optimistic protocol avoids latch acquisition on the hot read path, falling back to the pessimistic protocol only when a structural modification is detected.

There are three concurrency protocols to implement, each with different performance characteristics:

- **Pessimistic reads**: Acquire read latch on child, release read latch on parent. Multiple readers can proceed in parallel because read latches are shared. Cost: one latch acquire/release per tree level.
- **Pessimistic writes**: Acquire write latch on child, release parent write latch only when the child is safe. Writers block each other on the same path. Cost: one write latch per tree level, held until safety is determined.
- **Optimistic reads**: Traverse without any latches, recording version counters. Validate at the leaf. Restart if any version changed. Cost: zero latches in the common case, full restart in the rare case.

## Requirements

1. Implement a B-tree with configurable order (minimum degree `t`, where each node stores between `t-1` and `2t-1` keys). Each node stores keys in sorted order, with internal nodes holding child pointers. Leaf nodes store values directly. The tree must support generic key types that are ordered (`cmp.Ordered` in Go, `Ord` in Rust) and generic value types.

2. Implement per-node read-write latches. Readers acquire shared latches, writers acquire exclusive latches. Latches are separate from application-level locks: they are short-lived (held only during physical traversal), protect physical tree structure, and are never held across I/O operations or user callbacks. Each node carries its own `sync.RWMutex` (Go) or `RwLock` (Rust).

3. Implement search with latch crabbing: acquire read latch on child, release read latch on parent. The traversal descends from root to leaf, holding at most two read latches simultaneously (current node and its parent). Multiple concurrent searches must proceed without blocking each other because read latches are shared.

4. Implement insert with latch crabbing: acquire write latch on child, release parent write latch only when the child is safe (has fewer than `2t - 1` keys, meaning it will not split). If the child is unsafe, continue holding the parent latch because a split may propagate upward. When a safe child is found, release all held ancestor latches at once. Handle the special case of root splitting: create a new root node, update the tree's root pointer under the tree-level lock, then proceed with the insert.

5. Implement delete with latch crabbing: acquire write latch on child, release parent write latch only when the child is safe (has more than `t - 1` keys, meaning it will not need to merge or redistribute with a sibling). Handle the three deletion sub-cases: key in leaf (direct removal), key in internal node (replace with predecessor/successor and delete from child), and merge/redistribute when a child is below minimum occupancy.

6. Implement optimistic locking for reads: traverse without acquiring any latches, recording the version counter of each visited node. At the leaf, validate that no version counters changed since they were recorded. If any version changed (indicating a concurrent split, merge, or redistribution), discard the result and restart the search from the root. This protocol has zero latch overhead in the common case. Track the restart rate in a counter so you can measure how often structural modifications force retries.

7. Implement range scan that provides a consistent snapshot: latch leaf nodes left-to-right, collecting results, releasing each leaf latch before acquiring the next sibling. Handle the case where a leaf splits during the scan. The scan should accept a low key, a high key, and return all key-value pairs in that range. Consider implementing a leaf-level sibling pointer (B+ tree style) to enable efficient left-to-right scanning without re-traversing from the root.

8. Implement both Go and Rust versions. Go uses `sync.RWMutex` per node. Rust uses `Arc<RwLock<NodeInner>>` with careful lifetime management.

9. Write a stress test that launches 8+ concurrent goroutines/threads performing a mix of insertions, deletions, searches, and range scans on a shared tree. Use Go's race detector (`-race`) and Rust's thread sanitizer to verify no data races exist. The test must insert 100k+ keys and verify every inserted (non-deleted) key is still retrievable after all operations complete.

10. Benchmark your B-tree under different contention levels: read-only (100% search), write-only (100% insert), mixed (80% read / 20% write), and range-scan-heavy. Compare throughput at 1, 2, 4, 8, and 16 concurrent goroutines/threads. Include a global-lock B-tree as the baseline. Plot the speedup curve relative to single-threaded execution. In Go, use `testing.B` with `b.RunParallel`. In Rust, use `criterion` with a multi-threaded runtime.

## Hints

- Latch ordering is always top-down (root to leaf). Never acquire a parent latch while holding a child latch. This single rule prevents all deadlocks without needing a deadlock detector or timeout-based recovery.

- A node is "safe" for insert if `len(keys) < 2*t - 1` (it will not split). A node is "safe" for delete if `len(keys) > t - 1` (it will not need redistribution). When you determine the child is safe, release all ancestor latches at once. The "safe" check is the core optimization: it determines how early parent latches can be released.

- For optimistic reads, add a `version` counter to each node that increments on every structural modification (split, merge, key redistribution). The reader records the version at each node during descent and validates them at the leaf. If any version changed, restart. The retry rate is low under typical workloads because structural modifications are infrequent relative to reads.

- Root splitting requires special handling. When the root needs to split, a new root is created. This changes the tree's root pointer, which must be protected separately (e.g., with a tree-level RWMutex) from the per-node latches. Keep the tree-level lock acquisition brief: hold it only while swapping the root pointer, not during the entire split operation.

- In Rust, the main challenge is that `RwLockReadGuard` and `RwLockWriteGuard` borrow from the `RwLock` they are locking. When you traverse from parent to child, you need to hold the child guard while dropping the parent guard. This requires careful scoping or using `Arc<RwLock<NodeInner>>` so that the lock guard's lifetime is independent of the parent node's lifetime.

## Key Concepts

**Latch vs Lock**: In database terminology, a latch is a short-lived synchronization primitive that protects physical data structures (pages, nodes) during access. A lock is a long-lived mechanism that protects logical data (rows, key ranges) for transaction isolation. This challenge deals with latches only.

**Safe node**: A node is safe for an operation if the operation cannot cause the node to undergo structural changes. For insert, a node with fewer than `2t - 1` keys is safe because inserting into it or its subtree will not cause it to split. For delete, a node with more than `t - 1` keys is safe because deletion from its subtree will not cause it to merge or redistribute.

**Latch crabbing protocol**: Start at the root. Acquire latch on child. If child is safe, release all latches held on ancestors. If child is unsafe, keep ancestor latches. This ensures that if a structural change propagates upward, the necessary parent latches are already held.

**Optimistic concurrency**: Instead of acquiring latches, read the version counter before traversing through a node and validate after reaching the leaf. If no versions changed, the read is valid. This avoids all latch overhead but requires a retry mechanism. The trade-off: zero cost in the common case (no concurrent writes), full restart in the uncommon case (concurrent structural modification on the traversal path).

**Invariants to maintain under concurrency**:
- Every internal node with `k` keys has exactly `k + 1` children
- All keys within a node are sorted
- Every non-root node has between `t - 1` and `2t - 1` keys
- All leaves are at the same depth
- For any key `K` in an internal node at position `i`: all keys in child `i` are less than `K`, and all keys in child `i + 1` are greater than `K`

A violation of any invariant after concurrent operations indicates a concurrency bug. Write a verification function that checks all invariants and run it after every stress test.

## Acceptance Criteria

- [ ] B-tree operations (search, insert, delete) are correct under single-threaded execution
- [ ] Concurrent reads do not block each other (shared latches)
- [ ] Concurrent writes to different subtrees proceed in parallel
- [ ] No deadlocks under any interleaving of concurrent operations
- [ ] Optimistic reads restart correctly when a structural modification invalidates the path
- [ ] Range scan returns a consistent snapshot even under concurrent inserts and deletes
- [ ] Stress test: 8+ goroutines/threads performing mixed read/write operations on a tree with 100k+ keys, no panics, no data corruption
- [ ] Passes Go race detector (`-race`) and Rust thread sanitizer without warnings
- [ ] Benchmark shows measurable throughput improvement over global-lock B-tree at 4+ concurrent threads for read-heavy workloads
- [ ] Optimistic reads achieve higher throughput than pessimistic reads under low-contention scenarios
- [ ] Tree integrity verified after stress test: in-order traversal produces sorted keys, all node key counts within B-tree invariant bounds
- [ ] Both Go and Rust implementations pass equivalent test suites

## Resources

- [B-Trees - Wikipedia](https://en.wikipedia.org/wiki/B-tree) - Structure, invariants, operations, complexity analysis
- [Lehman & Yao: "Efficient Locking for Concurrent Operations on B-Trees" (1981)](https://www.csd.uoc.gr/~hy460/pdf/p650-lehman.pdf) - The foundational paper on B-link trees with concurrent access. Introduces the right-link pointer that allows readers to proceed without holding parent latches
- [Graefe: "A Survey of B-Tree Locking Techniques" (2010)](https://15721.courses.cs.cmu.edu/spring2017/papers/06-latching/a16-graefe.pdf) - Comprehensive survey covering latch crabbing, optimistic latching, Bw-tree, and OLFIT techniques. The definitive reference for this challenge
- [CMU 15-445: Database Storage](https://15445.courses.cs.cmu.edu/) - Lecture notes on B+ tree concurrency control with diagrams of latch crabbing protocols
- [CMU 15-721: Advanced Database Systems](https://15721.courses.cs.cmu.edu/) - Covers Bw-tree, OLFIT, and modern latch-free approaches
- [CLRS Chapter 18: B-Trees](https://mitpress.mit.edu/books/introduction-to-algorithms-fourth-edition) - Formal treatment of B-tree operations and their correctness proofs
- [BoltDB source (Go)](https://github.com/etcd-io/bbolt) - Production B+ tree in Go, study `node.go` and `bucket.go` for tree structure and `tx.go` for transaction isolation
- [sled source (Rust)](https://github.com/spacejam/sled) - Lock-free B+ tree in Rust using epoch-based reclamation. Study for Rust-specific concurrency patterns
- [The Art of Multiprocessor Programming, Chapter 11](https://www.oreilly.com/library/view/the-art-of/9780123705914/) - Concurrent data structures theory, lock coupling, and optimistic synchronization
- [Go sync.RWMutex documentation](https://pkg.go.dev/sync#RWMutex) - Read-write mutex semantics in Go
- [Rust std::sync::RwLock documentation](https://doc.rust-lang.org/std/sync/struct.RwLock.html) - Read-write lock semantics in Rust, including poisoning behavior
- [Go Race Detector](https://go.dev/doc/articles/race_detector) - How to use the race detector for verifying concurrent data structure correctness
- [parking_lot crate (Rust)](https://docs.rs/parking_lot/latest/parking_lot/) - Alternative RwLock without poisoning, faster for short critical sections
