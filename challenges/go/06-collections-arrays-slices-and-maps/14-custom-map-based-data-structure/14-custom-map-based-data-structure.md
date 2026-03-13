# 14. Custom Map-Based Data Structure

<!--
difficulty: insane
concepts: [ordered-map, lru-cache, doubly-linked-list, hash-map, generics, concurrent-access, eviction-policy, iterator]
tools: [go]
estimated_time: 60m
bloom_level: create
prerequisites: [maps-creation-access-iteration, map-internals-and-iteration-order, slice-internals, implementing-a-ring-buffer]
-->

## Prerequisites

- Go 1.22+ installed
- Completed all previous exercises in this section (01-13)
- Deep understanding of maps, slices, and generics
- Familiarity with doubly-linked lists and concurrency primitives
- Experience writing comprehensive test suites

## Learning Objectives

- **Create** an ordered map that preserves insertion order while maintaining O(1) lookups
- **Extend** it into an LRU cache with capacity limits and eviction
- **Design** a thread-safe API using `sync.RWMutex`
- **Evaluate** the trade-offs between your implementation and Go's built-in map

## The Challenge

Go's built-in `map` does not preserve insertion order. Python has `OrderedDict`, Java has `LinkedHashMap`, Rust has `IndexMap`. Go has nothing. Your task is to build `OrderedMap[K comparable, V any]` -- a data structure that combines a hash map for O(1) key lookups with a doubly-linked list for O(1) insertion-order iteration and O(1) move-to-front operations.

Then extend it into `LRUCache[K comparable, V any]` -- a least-recently-used cache with a maximum capacity. When the cache is full and a new key is inserted, the least-recently-used entry is evicted. Every `Get` operation promotes the entry to most-recently-used.

This is a real-world data structure used in web servers, database query caches, DNS resolvers, and operating system page tables.

## Requirements

### Part 1: OrderedMap

1. `NewOrderedMap[K comparable, V any]() *OrderedMap[K, V]`
2. `Set(key K, value V)` -- insert at the end if new; update value in place if key exists (do not change order)
3. `Get(key K) (V, bool)` -- O(1) lookup
4. `Delete(key K) bool` -- O(1) removal, returns false if key not present
5. `Len() int`
6. `Keys() []K` -- return keys in insertion order
7. `Values() []V` -- return values in insertion order
8. `Range(fn func(key K, value V) bool)` -- iterate in insertion order, stop if fn returns false
9. `MoveToFront(key K) bool` -- move an existing entry to the front of the order
10. `MoveToBack(key K) bool` -- move an existing entry to the back of the order
11. `Oldest() (K, V, bool)` -- return the oldest entry without removing
12. `Newest() (K, V, bool)` -- return the newest entry without removing

### Part 2: LRU Cache

13. `NewLRUCache[K comparable, V any](capacity int) *LRUCache[K, V]`
14. `Get(key K) (V, bool)` -- lookup + promote to most-recently-used
15. `Put(key K, value V) (evictedKey K, evictedValue V, didEvict bool)` -- insert/update; evict LRU if at capacity
16. `Remove(key K) bool`
17. `Len() int` and `Cap() int`
18. `Peek(key K) (V, bool)` -- lookup without promoting (useful for inspection)
19. `Purge()` -- remove all entries

### Part 3: Thread Safety

20. Wrap the LRU cache with `sync.RWMutex` to create `ConcurrentLRUCache[K, V]`
21. `Get` and `Peek` use write locks (Get mutates order) and read locks respectively
22. All mutating operations use write locks
23. Must pass `go test -race`

### Internal Structure

The doubly-linked list must be implemented by hand (do not use `container/list`). Each node stores:

```
type node[K comparable, V any] struct {
    key   K
    value V
    prev  *node[K, V]
    next  *node[K, V]
}
```

The map stores `map[K]*node[K, V]` for O(1) access to list nodes.

Use sentinel head and tail nodes to simplify insertion/deletion edge cases.

## Success Criteria

1. All operations on `OrderedMap` are O(1) except `Keys()`, `Values()`, and `Range()` which are O(n)
2. `LRUCache` evicts the least-recently-used entry when capacity is exceeded
3. `Get` on the LRU cache promotes the accessed entry to most-recently-used
4. `ConcurrentLRUCache` passes `go test -race` with 100 concurrent goroutines
5. Comprehensive test coverage including:
   - Empty map/cache operations
   - Single-element edge cases
   - Insertion order preserved after updates
   - Correct eviction order (LRU first)
   - Promotion on access changes eviction order
   - Concurrent read/write stress test
6. Benchmark comparing your `OrderedMap.Get` against built-in `map` access (expect 1.5-3x overhead due to linked list maintenance)

## Hints

<details>
<summary>Hint 1: Sentinel nodes simplify list operations</summary>

```go
type OrderedMap[K comparable, V any] struct {
    lookup map[K]*node[K, V]
    head   *node[K, V] // sentinel: head.next is the oldest
    tail   *node[K, V] // sentinel: tail.prev is the newest
}

func NewOrderedMap[K comparable, V any]() *OrderedMap[K, V] {
    om := &OrderedMap[K, V]{
        lookup: make(map[K]*node[K, V]),
        head:   &node[K, V]{},
        tail:   &node[K, V]{},
    }
    om.head.next = om.tail
    om.tail.prev = om.head
    return om
}
```

Sentinel nodes mean you never have to check for nil prev/next during insertion or deletion.
</details>

<details>
<summary>Hint 2: Core list operations</summary>

```go
func (om *OrderedMap[K, V]) insertBefore(at, n *node[K, V]) {
    n.prev = at.prev
    n.next = at
    at.prev.next = n
    at.prev = n
}

func (om *OrderedMap[K, V]) removeNode(n *node[K, V]) {
    n.prev.next = n.next
    n.next.prev = n.prev
    n.prev = nil
    n.next = nil
}
```

All insertions and deletions reduce to these two operations.
</details>

<details>
<summary>Hint 3: LRU eviction is just removing head.next</summary>

```go
func (c *LRUCache[K, V]) evict() (K, V) {
    oldest := c.omap.head.next
    c.omap.removeNode(oldest)
    delete(c.omap.lookup, oldest.key)
    return oldest.key, oldest.value
}
```
</details>

<details>
<summary>Hint 4: Concurrent wrapper</summary>

```go
type ConcurrentLRUCache[K comparable, V any] struct {
    mu    sync.RWMutex
    cache *LRUCache[K, V]
}

func (c *ConcurrentLRUCache[K, V]) Get(key K) (V, bool) {
    c.mu.Lock() // not RLock -- Get mutates order
    defer c.mu.Unlock()
    return c.cache.Get(key)
}

func (c *ConcurrentLRUCache[K, V]) Peek(key K) (V, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.cache.Peek(key)
}
```
</details>

## Research Resources

- [Wikipedia: Cache replacement policies (LRU)](https://en.wikipedia.org/wiki/Cache_replacement_policies#Least_recently_used_(LRU))
- [Python OrderedDict implementation](https://github.com/python/cpython/blob/main/Objects/odictobject.c) -- C implementation of an ordered dictionary
- [hashicorp/golang-lru](https://github.com/hashicorp/golang-lru) -- production LRU cache in Go (study after implementing your own)
- [container/list](https://pkg.go.dev/container/list) -- Go's doubly-linked list (do NOT use it; implement your own for this exercise)
- [sync.RWMutex](https://pkg.go.dev/sync#RWMutex) -- reader-writer mutual exclusion

## What's Next

Congratulations -- you have completed the Collections section. You now have a deep understanding of arrays, slices, and maps in Go, from basic creation to internal mechanics to building custom data structures. Continue to [Section 07 - Structs and Methods](../../07-structs-and-methods/) to learn how Go models data with structs.

## Summary

- An ordered map combines a hash map (`map[K]*node`) with a doubly-linked list
- Sentinel head/tail nodes eliminate nil-checking edge cases in list operations
- All core operations (Get, Set, Delete, MoveToFront) are O(1)
- An LRU cache is an ordered map with a capacity limit and access-time promotion
- `Get` on an LRU must promote (move to back), making it a write operation
- Thread-safe wrappers use `sync.RWMutex`, but `Get` requires a write lock due to promotion
- `Peek` is the only truly read-only lookup operation on an LRU cache
- This pattern is used in production systems: DNS caches, HTTP session stores, database query caches
