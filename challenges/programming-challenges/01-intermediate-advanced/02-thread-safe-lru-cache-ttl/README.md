# 2. Thread-Safe LRU Cache with TTL Expiration

<!--
difficulty: intermediate-advanced
category: caching-and-networking
languages: [go, rust]
concepts: [lru-cache, ttl-expiration, concurrency, mutex, eviction-callbacks, generics]
estimated_time: 4-5 hours
bloom_level: analyze
prerequisites: [go-basics, rust-basics, mutexes, hashmaps, doubly-linked-lists, generics]
-->

## Languages

- Go (1.22+)
- Rust (stable)

## Prerequisites

- Mutex and RWMutex patterns for shared state protection
- HashMap and doubly linked list data structures
- Generics in Go and Rust
- Understanding of cache eviction policies (LRU concept)
- Basic concurrency testing (race detectors, `sync.WaitGroup`, `Arc`)

## Learning Objectives

- **Implement** an LRU cache backed by a hashmap and doubly linked list for O(1) operations
- **Design** a per-entry TTL expiration system with both lazy and active cleanup strategies
- **Analyze** the trade-offs between read-heavy locking (RWMutex) and write-heavy locking (Mutex) in concurrent caches
- **Apply** eviction callbacks to propagate cache invalidation events to external systems
- **Evaluate** cache effectiveness through hit/miss ratio metrics under concurrent workloads

## The Challenge

Caches sit at the boundary between fast and slow. Every database query, API call, or disk read that you can serve from memory instead is latency you eliminate. But memory is finite, so you need an eviction policy. LRU (Least Recently Used) is the most common: when the cache is full, discard the entry that has not been accessed for the longest time.

Real-world caches need more than eviction by capacity. Data goes stale. A DNS record cached for an hour is useful; cached for a week it is dangerous. TTL (Time To Live) expiration gives each entry an independent deadline. When that deadline passes, the entry must disappear regardless of whether the cache is full.

Your task is to build a thread-safe LRU cache that supports per-entry TTL, configurable maximum capacity, hit/miss metrics, and eviction callbacks. You will implement it in both Go and Rust to compare how each language handles interior mutability, generic containers, and concurrent access patterns.

## Requirements

1. Implement a generic `Cache[K, V]` (Go) / `Cache<K, V>` (Rust) with `Get`, `Put`, and `Delete` operations, all O(1) average time
2. Back the cache with a hashmap for key lookup and a doubly linked list for recency ordering
3. On `Get`: move the accessed entry to the front of the list (most recently used) and return the value. Return a miss if the key does not exist or has expired
4. On `Put`: insert at the front. If the cache exceeds max capacity, evict the tail entry (least recently used). If the key already exists, update the value and move to front
5. Support per-entry TTL: each `Put` accepts an optional TTL duration. Entries past their TTL are treated as nonexistent
6. Implement lazy expiration: expired entries are removed when accessed via `Get`
7. Implement active expiration: a background goroutine/thread periodically scans and removes expired entries
8. Register an eviction callback `func(key K, value V)` that fires whenever an entry is evicted (by capacity, TTL, or explicit delete)
9. Track and expose metrics: total hits, total misses, total evictions, current size
10. All operations must be safe for concurrent use from multiple goroutines/threads

## Hints

<details>
<summary>Hint 1: Core data structure layout</summary>

The classic LRU uses a hashmap pointing to nodes in a doubly linked list:

```go
type entry[K comparable, V any] struct {
    key       K
    value     V
    expiresAt time.Time
    prev      *entry[K, V]
    next      *entry[K, V]
}

type Cache[K comparable, V any] struct {
    mu       sync.RWMutex
    items    map[K]*entry[K, V]
    head     *entry[K, V] // most recent
    tail     *entry[K, V] // least recent
    capacity int
}
```

The hashmap gives you O(1) lookup; the linked list gives you O(1) move-to-front and evict-from-tail.
</details>

<details>
<summary>Hint 2: Move-to-front operation</summary>

When an entry is accessed, detach it from its current position and reattach at the head:

```go
func (c *Cache[K, V]) moveToFront(e *entry[K, V]) {
    if e == c.head {
        return
    }
    c.detach(e)
    c.attachFront(e)
}
```

Handle edge cases: single-element list, entry is already head, entry is tail.
</details>

<details>
<summary>Hint 3: Active expiration with background cleanup</summary>

Run a goroutine that wakes up at a fixed interval and walks the list from tail to head, removing expired entries:

```go
func (c *Cache[K, V]) startCleanup(interval time.Duration) {
    ticker := time.NewTicker(interval)
    go func() {
        for range ticker.C {
            c.mu.Lock()
            c.removeExpired()
            c.mu.Unlock()
        }
    }()
}
```

Walk from tail because expired entries tend to cluster there (they have not been accessed recently).
</details>

<details>
<summary>Hint 4: Rust interior mutability pattern</summary>

In Rust, wrap the inner state in `Arc<Mutex<CacheInner<K, V>>>` for shared ownership across threads:

```rust
pub struct Cache<K, V> {
    inner: Arc<Mutex<CacheInner<K, V>>>,
}

struct CacheInner<K, V> {
    entries: HashMap<K, usize>, // key -> index in slab/vec
    order: VecDeque<K>,         // front = most recent
    capacity: usize,
}
```

Consider using a slab allocator or `HashMap` with stable indices instead of raw pointers for the linked list.
</details>

## Acceptance Criteria

- [ ] `Get` returns the value and moves the entry to most-recently-used position
- [ ] `Get` on an expired entry returns a miss and triggers eviction callback
- [ ] `Put` evicts the least-recently-used entry when capacity is exceeded
- [ ] `Put` with TTL causes the entry to expire after the specified duration
- [ ] `Delete` removes the entry and triggers eviction callback
- [ ] Background cleanup goroutine/thread removes expired entries periodically
- [ ] Eviction callback fires for every eviction (capacity, TTL, and explicit delete)
- [ ] Hit/miss/eviction metrics are accurate under concurrent access
- [ ] No data races when accessed from 100+ concurrent goroutines/threads
- [ ] Both Go and Rust implementations pass their respective test suites

## Research Resources

- [Go: sync.RWMutex documentation](https://pkg.go.dev/sync#RWMutex) -- read-write lock semantics for concurrent cache access
- [Rust: std::collections::HashMap](https://doc.rust-lang.org/std/collections/struct.HashMap.html) -- the foundation for key-value storage in the Rust implementation
- [LRU Cache -- Wikipedia](https://en.wikipedia.org/wiki/Cache_replacement_policies#Least_recently_used_(LRU)) -- formal definition of the LRU replacement policy
- [Google Guava Cache Design](https://github.com/google/guava/wiki/CachesExplained) -- production cache design with TTL, eviction listeners, and statistics
- [Rust: Arc and Mutex for shared state](https://doc.rust-lang.org/book/ch16-03-shared-state.html) -- shared ownership patterns in concurrent Rust
