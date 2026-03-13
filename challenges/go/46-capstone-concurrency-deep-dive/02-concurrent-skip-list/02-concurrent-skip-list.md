<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 3h
-->

# Concurrent Skip List

## The Challenge

Build a concurrent skip list that supports lock-free reads and fine-grained locking for writes, providing an ordered key-value map with O(log n) expected time for search, insert, and delete operations under high concurrency. Unlike hash maps, skip lists maintain key ordering, making them ideal for range scans and ordered iteration. Your implementation must handle concurrent insertions at the same position in the list, deletions of nodes being simultaneously read, and the probabilistic tower height generation that gives skip lists their performance characteristics. The skip list must support generic key and value types, provide a `Range(start, end)` iterator that is consistent with concurrent modifications, and achieve performance competitive with `sync.Map` for point lookups while also supporting ordered operations.

## Requirements

1. Implement a skip list with a maximum height of 32 levels, where each node's height is determined probabilistically (each additional level has a 1/4 probability), giving O(log n) expected lookup time.
2. The `Search(key)` operation must be lock-free: traverse the skip list from the top level down, following `next` pointers loaded atomically, without acquiring any locks.
3. The `Insert(key, value)` operation must use fine-grained locking: lock only the predecessor nodes at each level being modified, validate that the predecessors still point to the expected successors (optimistic validation), and insert the new node bottom-up with atomic pointer stores.
4. The `Delete(key)` operation must use lazy deletion: first logically mark the node as deleted using an atomic flag, then physically unlink it from each level top-down while holding fine-grained locks on predecessors.
5. Implement a `Range(startKey, endKey)` operation that returns an iterator over all key-value pairs in the range in sorted order, providing a snapshot-consistent view by collecting results during a single forward traversal.
6. Handle the case where a concurrent insert adds a node at a position the `Range` iterator has already passed (the iterator should not include it), and the case where a concurrent delete removes a node the iterator is about to visit (the iterator should skip it if it is logically deleted).
7. Support generic key types using Go generics with an `Ordered` constraint, and generic value types with no constraint.
8. Implement `Len()` that returns an approximate count using an atomic counter incremented on insert and decremented on delete, and `Height()` that returns the current maximum tower height.

## Hints

- Use `atomic.Pointer[Node[K,V]]` (Go 1.19+) for the `next` pointers at each level to enable lock-free reads.
- Each node should have: `key K`, `value atomic.Pointer[V]`, `next [maxLevel]atomic.Pointer[Node[K,V]]`, `marked atomic.Bool` (for lazy deletion), `topLevel int`, and `mu sync.Mutex` (for write locking).
- The `findPredecessors` helper traverses from the top level, building arrays of predecessor and successor nodes at each level -- this is the core subroutine used by both insert and delete.
- For optimistic validation in insert: after acquiring locks on predecessors, verify that each predecessor is not marked for deletion and still points to the expected successor.
- Probabilistic height: `height := 1; for height < maxLevel && rand.Int31n(4) == 0 { height++ }`.
- Use a sentinel head node with height `maxLevel` and a nil tail to simplify boundary conditions.
- The Java `ConcurrentSkipListMap` and the Herlihy/Shavit textbook description are excellent references.

## Success Criteria

1. 64 goroutines performing random inserts, deletes, and searches on a skip list of 1 million keys complete without data races (`go test -race`).
2. Search throughput with 32 concurrent readers exceeds 5 million ops/sec on a dataset of 1 million keys.
3. All keys in a `Range(a, z)` query are returned in strictly sorted order.
4. Logically deleted nodes are not visible to searches or range queries.
5. Concurrent insert and delete of the same key is handled correctly: either the key exists or it does not, with no partial state visible.
6. The skip list correctly handles duplicate key inserts by updating the value atomically.
7. Approximate `Len()` is accurate to within 1% of the true count under concurrent modifications.

## Research Resources

- "A Pragmatic Implementation of Non-Blocking Linked-Lists" (Harris, 2001)
- "A Lazy Concurrent List-Based Set Algorithm" (Heller et al., 2006)
- "The Art of Multiprocessor Programming" (Herlihy & Shavit, 2012) -- Chapter 14: SkipLists
- William Pugh, "Skip Lists: A Probabilistic Alternative to Balanced Trees" (1990)
- Java `ConcurrentSkipListMap` source code -- OpenJDK
- Go `sync/atomic` package documentation
