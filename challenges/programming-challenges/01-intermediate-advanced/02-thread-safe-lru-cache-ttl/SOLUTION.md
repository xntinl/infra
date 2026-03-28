# Solution: Thread-Safe LRU Cache with TTL Expiration

## Architecture Overview

The cache combines two data structures: a **hashmap** for O(1) key lookup and a **doubly linked list** for O(1) recency tracking. Each entry lives in both structures simultaneously. The hashmap maps keys to list nodes, and the list maintains access order (most recent at head, least recent at tail).

TTL expiration uses a dual strategy: **lazy expiration** checks timestamps on access and removes stale entries on demand; **active expiration** runs a background sweep that proactively purges expired entries to prevent unbounded memory growth.

Thread safety wraps the entire cache state in a mutex. A `sync.RWMutex` (Go) or `Mutex` (Rust) protects all reads and writes. The eviction callback fires while the lock is held to guarantee ordering, though in production you might fire callbacks asynchronously to avoid holding the lock during slow callbacks.

```
┌──────────────────────────────────────────┐
│                 Cache                     │
│  ┌─────────┐     ┌──────────────────┐    │
│  │ HashMap  │────>│ Doubly Linked    │    │
│  │ K -> Node│     │ List (by recency)│    │
│  └─────────┘     │ HEAD ... TAIL    │    │
│                  └──────────────────┘    │
│  ┌─────────┐     ┌──────────────────┐    │
│  │ Metrics  │     │ Background       │    │
│  │ hit/miss │     │ Cleanup Thread   │    │
│  └─────────┘     └──────────────────┘    │
└──────────────────────────────────────────┘
```

---

## Go Solution

### Project Setup

```bash
mkdir lru-cache && cd lru-cache
go mod init lru-cache
```

### Implementation

```go
// cache.go
package lrucache

import (
	"sync"
	"time"
)

type entry[K comparable, V any] struct {
	key       K
	value     V
	expiresAt time.Time
	hasTTL    bool
	prev      *entry[K, V]
	next      *entry[K, V]
}

func (e *entry[K, V]) isExpired() bool {
	return e.hasTTL && time.Now().After(e.expiresAt)
}

type EvictionReason int

const (
	EvictedByCapacity EvictionReason = iota
	EvictedByTTL
	EvictedByDelete
)

type Metrics struct {
	Hits      int64
	Misses    int64
	Evictions int64
	Size      int
}

type OnEvict[K comparable, V any] func(key K, value V, reason EvictionReason)

type Cache[K comparable, V any] struct {
	mu        sync.Mutex
	items     map[K]*entry[K, V]
	head      *entry[K, V]
	tail      *entry[K, V]
	capacity  int
	onEvict   OnEvict[K, V]
	hits      int64
	misses    int64
	evictions int64
	stopCh    chan struct{}
}

func New[K comparable, V any](capacity int, opts ...Option[K, V]) *Cache[K, V] {
	c := &Cache[K, V]{
		items:    make(map[K]*entry[K, V], capacity),
		capacity: capacity,
		stopCh:   make(chan struct{}),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type Option[K comparable, V any] func(*Cache[K, V])

func WithEvictionCallback[K comparable, V any](fn OnEvict[K, V]) Option[K, V] {
	return func(c *Cache[K, V]) {
		c.onEvict = fn
	}
}

func WithActiveExpiration[K comparable, V any](interval time.Duration) Option[K, V] {
	return func(c *Cache[K, V]) {
		go c.cleanupLoop(interval)
	}
}

func (c *Cache[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.items[key]
	if !ok {
		c.misses++
		var zero V
		return zero, false
	}

	if e.isExpired() {
		c.removeEntry(e, EvictedByTTL)
		c.misses++
		var zero V
		return zero, false
	}

	c.moveToFront(e)
	c.hits++
	return e.value, true
}

func (c *Cache[K, V]) Put(key K, value V, ttl ...time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if e, ok := c.items[key]; ok {
		e.value = value
		if len(ttl) > 0 && ttl[0] > 0 {
			e.expiresAt = time.Now().Add(ttl[0])
			e.hasTTL = true
		}
		c.moveToFront(e)
		return
	}

	e := &entry[K, V]{key: key, value: value}
	if len(ttl) > 0 && ttl[0] > 0 {
		e.expiresAt = time.Now().Add(ttl[0])
		e.hasTTL = true
	}

	c.items[key] = e
	c.pushFront(e)

	if len(c.items) > c.capacity {
		c.evictTail()
	}
}

func (c *Cache[K, V]) Delete(key K) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.items[key]
	if !ok {
		return false
	}
	c.removeEntry(e, EvictedByDelete)
	return true
}

func (c *Cache[K, V]) Metrics() Metrics {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Metrics{
		Hits:      c.hits,
		Misses:    c.misses,
		Evictions: c.evictions,
		Size:      len(c.items),
	}
}

func (c *Cache[K, V]) Stop() {
	close(c.stopCh)
}

// --- linked list operations ---

func (c *Cache[K, V]) pushFront(e *entry[K, V]) {
	e.prev = nil
	e.next = c.head
	if c.head != nil {
		c.head.prev = e
	}
	c.head = e
	if c.tail == nil {
		c.tail = e
	}
}

func (c *Cache[K, V]) detach(e *entry[K, V]) {
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		c.head = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		c.tail = e.prev
	}
	e.prev = nil
	e.next = nil
}

func (c *Cache[K, V]) moveToFront(e *entry[K, V]) {
	if c.head == e {
		return
	}
	c.detach(e)
	c.pushFront(e)
}

func (c *Cache[K, V]) evictTail() {
	if c.tail == nil {
		return
	}
	c.removeEntry(c.tail, EvictedByCapacity)
}

func (c *Cache[K, V]) removeEntry(e *entry[K, V], reason EvictionReason) {
	c.detach(e)
	delete(c.items, e.key)
	c.evictions++
	if c.onEvict != nil {
		c.onEvict(e.key, e.value, reason)
	}
}

func (c *Cache[K, V]) cleanupLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.removeExpired()
		case <-c.stopCh:
			return
		}
	}
}

func (c *Cache[K, V]) removeExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	current := c.tail
	for current != nil {
		prev := current.prev
		if current.isExpired() {
			c.removeEntry(current, EvictedByTTL)
		}
		current = prev
	}
}
```

### Tests

```go
// cache_test.go
package lrucache

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestGetPut(t *testing.T) {
	c := New[string, int](3)

	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("c", 3)

	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Fatalf("expected a=1, got %v, %v", v, ok)
	}
	if v, ok := c.Get("b"); !ok || v != 2 {
		t.Fatalf("expected b=2, got %v, %v", v, ok)
	}
}

func TestLRUEviction(t *testing.T) {
	evicted := make(map[string]int)
	c := New[string, int](2, WithEvictionCallback[string, int](func(k string, v int, r EvictionReason) {
		evicted[k] = v
	}))

	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("c", 3) // evicts "a"

	if _, ok := c.Get("a"); ok {
		t.Fatal("expected a to be evicted")
	}
	if evicted["a"] != 1 {
		t.Fatalf("expected eviction callback for a=1, got %v", evicted["a"])
	}
}

func TestLRUAccessOrder(t *testing.T) {
	c := New[string, int](2)

	c.Put("a", 1)
	c.Put("b", 2)
	c.Get("a")    // a is now most recent
	c.Put("c", 3) // evicts "b" (least recent)

	if _, ok := c.Get("b"); ok {
		t.Fatal("expected b to be evicted, not a")
	}
	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Fatal("expected a to still exist")
	}
}

func TestTTLExpiration(t *testing.T) {
	c := New[string, int](10)

	c.Put("temp", 42, 50*time.Millisecond)
	if v, ok := c.Get("temp"); !ok || v != 42 {
		t.Fatal("expected temp=42 before expiry")
	}

	time.Sleep(60 * time.Millisecond)

	if _, ok := c.Get("temp"); ok {
		t.Fatal("expected temp to be expired")
	}
}

func TestActiveExpiration(t *testing.T) {
	c := New[string, int](10, WithActiveExpiration[string, int](20*time.Millisecond))
	defer c.Stop()

	c.Put("expire-me", 1, 30*time.Millisecond)

	time.Sleep(80 * time.Millisecond)

	m := c.Metrics()
	if m.Size != 0 {
		t.Fatalf("expected size 0 after active expiration, got %d", m.Size)
	}
}

func TestDelete(t *testing.T) {
	var evictedKey string
	c := New[string, int](10, WithEvictionCallback[string, int](func(k string, v int, r EvictionReason) {
		evictedKey = k
	}))

	c.Put("x", 1)
	c.Delete("x")

	if _, ok := c.Get("x"); ok {
		t.Fatal("expected x to be deleted")
	}
	if evictedKey != "x" {
		t.Fatal("expected eviction callback on delete")
	}
}

func TestMetrics(t *testing.T) {
	c := New[string, int](2)

	c.Put("a", 1)
	c.Get("a") // hit
	c.Get("b") // miss
	c.Put("b", 2)
	c.Put("c", 3) // evicts a

	m := c.Metrics()
	if m.Hits != 1 {
		t.Fatalf("expected 1 hit, got %d", m.Hits)
	}
	if m.Misses != 1 {
		t.Fatalf("expected 1 miss, got %d", m.Misses)
	}
	if m.Evictions != 1 {
		t.Fatalf("expected 1 eviction, got %d", m.Evictions)
	}
}

func TestConcurrentAccess(t *testing.T) {
	c := New[int, int](100)
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				key := (id*1000 + j) % 200
				c.Put(key, j)
				c.Get(key)
			}
		}(i)
	}
	wg.Wait()

	m := c.Metrics()
	fmt.Printf("Concurrent test: size=%d, hits=%d, misses=%d, evictions=%d\n",
		m.Size, m.Hits, m.Misses, m.Evictions)
}
```

### Running

```bash
go test -v -race ./...
```

### Expected Output

```
=== RUN   TestGetPut
--- PASS: TestGetPut (0.00s)
=== RUN   TestLRUEviction
--- PASS: TestLRUEviction (0.00s)
=== RUN   TestLRUAccessOrder
--- PASS: TestLRUAccessOrder (0.00s)
=== RUN   TestTTLExpiration
--- PASS: TestTTLExpiration (0.06s)
=== RUN   TestActiveExpiration
--- PASS: TestActiveExpiration (0.08s)
=== RUN   TestDelete
--- PASS: TestDelete (0.00s)
=== RUN   TestMetrics
--- PASS: TestMetrics (0.00s)
=== RUN   TestConcurrentAccess
Concurrent test: size=100, hits=..., misses=..., evictions=...
--- PASS: TestConcurrentAccess (0.xx)
PASS
```

---

## Rust Solution

### Project Setup

```bash
cargo new lru_cache --lib
cd lru_cache
```

### Implementation

```rust
// src/lib.rs
use std::collections::HashMap;
use std::hash::Hash;
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::{Duration, Instant};

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum EvictionReason {
    Capacity,
    TTL,
    Delete,
}

type EvictionCallback<K, V> = Box<dyn Fn(&K, &V, EvictionReason) + Send + 'static>;

struct Entry<K, V> {
    key: K,
    value: V,
    expires_at: Option<Instant>,
    prev: Option<usize>,
    next: Option<usize>,
}

impl<K, V> Entry<K, V> {
    fn is_expired(&self) -> bool {
        self.expires_at.map_or(false, |t| Instant::now() > t)
    }
}

struct CacheInner<K, V> {
    entries: Vec<Option<Entry<K, V>>>,
    index: HashMap<K, usize>,
    free_slots: Vec<usize>,
    head: Option<usize>,
    tail: Option<usize>,
    capacity: usize,
    hits: u64,
    misses: u64,
    evictions: u64,
    on_evict: Option<EvictionCallback<K, V>>,
}

#[derive(Debug, Clone)]
pub struct Metrics {
    pub hits: u64,
    pub misses: u64,
    pub evictions: u64,
    pub size: usize,
}

pub struct Cache<K, V> {
    inner: Arc<Mutex<CacheInner<K, V>>>,
    stop_flag: Arc<Mutex<bool>>,
}

impl<K, V> Cache<K, V>
where
    K: Eq + Hash + Clone + Send + 'static,
    V: Clone + Send + 'static,
{
    pub fn new(capacity: usize) -> Self {
        Cache {
            inner: Arc::new(Mutex::new(CacheInner {
                entries: Vec::with_capacity(capacity),
                index: HashMap::with_capacity(capacity),
                free_slots: Vec::new(),
                head: None,
                tail: None,
                capacity,
                hits: 0,
                misses: 0,
                evictions: 0,
                on_evict: None,
            })),
            stop_flag: Arc::new(Mutex::new(false)),
        }
    }

    pub fn set_eviction_callback<F>(&self, f: F)
    where
        F: Fn(&K, &V, EvictionReason) + Send + 'static,
    {
        let mut inner = self.inner.lock().unwrap();
        inner.on_evict = Some(Box::new(f));
    }

    pub fn start_cleanup(&self, interval: Duration) {
        let inner = Arc::clone(&self.inner);
        let stop = Arc::clone(&self.stop_flag);
        thread::spawn(move || loop {
            thread::sleep(interval);
            if *stop.lock().unwrap() {
                return;
            }
            let mut cache = inner.lock().unwrap();
            cache.remove_expired();
        });
    }

    pub fn stop(&self) {
        *self.stop_flag.lock().unwrap() = true;
    }

    pub fn get(&self, key: &K) -> Option<V> {
        let mut inner = self.inner.lock().unwrap();
        let slot = match inner.index.get(key) {
            Some(&s) => s,
            None => {
                inner.misses += 1;
                return None;
            }
        };

        if inner.entries[slot].as_ref().unwrap().is_expired() {
            inner.remove_slot(slot, EvictionReason::TTL);
            inner.misses += 1;
            return None;
        }

        inner.move_to_front(slot);
        inner.hits += 1;
        Some(inner.entries[slot].as_ref().unwrap().value.clone())
    }

    pub fn put(&self, key: K, value: V, ttl: Option<Duration>) {
        let mut inner = self.inner.lock().unwrap();

        if let Some(&slot) = inner.index.get(&key) {
            let entry = inner.entries[slot].as_mut().unwrap();
            entry.value = value;
            entry.expires_at = ttl.map(|d| Instant::now() + d);
            inner.move_to_front(slot);
            return;
        }

        let expires_at = ttl.map(|d| Instant::now() + d);
        let entry = Entry {
            key: key.clone(),
            value,
            expires_at,
            prev: None,
            next: None,
        };

        let slot = if let Some(free) = inner.free_slots.pop() {
            inner.entries[free] = Some(entry);
            free
        } else {
            inner.entries.push(Some(entry));
            inner.entries.len() - 1
        };

        inner.index.insert(key, slot);
        inner.push_front(slot);

        if inner.index.len() > inner.capacity {
            inner.evict_tail();
        }
    }

    pub fn delete(&self, key: &K) -> bool {
        let mut inner = self.inner.lock().unwrap();
        let slot = match inner.index.get(key) {
            Some(&s) => s,
            None => return false,
        };
        inner.remove_slot(slot, EvictionReason::Delete);
        true
    }

    pub fn metrics(&self) -> Metrics {
        let inner = self.inner.lock().unwrap();
        Metrics {
            hits: inner.hits,
            misses: inner.misses,
            evictions: inner.evictions,
            size: inner.index.len(),
        }
    }
}

impl<K, V> CacheInner<K, V>
where
    K: Eq + Hash + Clone,
    V: Clone,
{
    fn push_front(&mut self, slot: usize) {
        let entry = self.entries[slot].as_mut().unwrap();
        entry.prev = None;
        entry.next = self.head;

        if let Some(old_head) = self.head {
            self.entries[old_head].as_mut().unwrap().prev = Some(slot);
        }
        self.head = Some(slot);
        if self.tail.is_none() {
            self.tail = Some(slot);
        }
    }

    fn detach(&mut self, slot: usize) {
        let (prev, next) = {
            let entry = self.entries[slot].as_ref().unwrap();
            (entry.prev, entry.next)
        };

        match prev {
            Some(p) => self.entries[p].as_mut().unwrap().next = next,
            None => self.head = next,
        }
        match next {
            Some(n) => self.entries[n].as_mut().unwrap().prev = prev,
            None => self.tail = prev,
        }

        let entry = self.entries[slot].as_mut().unwrap();
        entry.prev = None;
        entry.next = None;
    }

    fn move_to_front(&mut self, slot: usize) {
        if self.head == Some(slot) {
            return;
        }
        self.detach(slot);
        self.push_front(slot);
    }

    fn evict_tail(&mut self) {
        if let Some(tail) = self.tail {
            self.remove_slot(tail, EvictionReason::Capacity);
        }
    }

    fn remove_slot(&mut self, slot: usize, reason: EvictionReason) {
        self.detach(slot);
        let entry = self.entries[slot].take().unwrap();
        self.index.remove(&entry.key);
        self.free_slots.push(slot);
        self.evictions += 1;

        if let Some(ref cb) = self.on_evict {
            cb(&entry.key, &entry.value, reason);
        }
    }

    fn remove_expired(&mut self) {
        let expired: Vec<usize> = self
            .entries
            .iter()
            .enumerate()
            .filter_map(|(i, e)| {
                e.as_ref().and_then(|entry| {
                    if entry.is_expired() {
                        Some(i)
                    } else {
                        None
                    }
                })
            })
            .collect();

        for slot in expired {
            self.remove_slot(slot, EvictionReason::TTL);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicU64, Ordering};

    #[test]
    fn test_get_put() {
        let cache = Cache::new(3);
        cache.put("a".to_string(), 1, None);
        cache.put("b".to_string(), 2, None);

        assert_eq!(cache.get(&"a".to_string()), Some(1));
        assert_eq!(cache.get(&"b".to_string()), Some(2));
        assert_eq!(cache.get(&"c".to_string()), None);
    }

    #[test]
    fn test_lru_eviction() {
        let evict_count = Arc::new(AtomicU64::new(0));
        let count_clone = Arc::clone(&evict_count);

        let cache = Cache::new(2);
        cache.set_eviction_callback(move |_k, _v, _r| {
            count_clone.fetch_add(1, Ordering::SeqCst);
        });

        cache.put("a".to_string(), 1, None);
        cache.put("b".to_string(), 2, None);
        cache.put("c".to_string(), 3, None);

        assert_eq!(cache.get(&"a".to_string()), None);
        assert_eq!(evict_count.load(Ordering::SeqCst), 1);
    }

    #[test]
    fn test_access_order() {
        let cache = Cache::new(2);
        cache.put("a".to_string(), 1, None);
        cache.put("b".to_string(), 2, None);
        cache.get(&"a".to_string()); // a is now most recent
        cache.put("c".to_string(), 3, None); // evicts b

        assert_eq!(cache.get(&"b".to_string()), None);
        assert_eq!(cache.get(&"a".to_string()), Some(1));
    }

    #[test]
    fn test_ttl_expiration() {
        let cache = Cache::new(10);
        cache.put("temp".to_string(), 42, Some(Duration::from_millis(50)));

        assert_eq!(cache.get(&"temp".to_string()), Some(42));
        thread::sleep(Duration::from_millis(60));
        assert_eq!(cache.get(&"temp".to_string()), None);
    }

    #[test]
    fn test_active_expiration() {
        let cache = Cache::new(10);
        cache.start_cleanup(Duration::from_millis(20));
        cache.put("expire".to_string(), 1, Some(Duration::from_millis(30)));

        thread::sleep(Duration::from_millis(80));
        let m = cache.metrics();
        assert_eq!(m.size, 0);
        cache.stop();
    }

    #[test]
    fn test_metrics() {
        let cache = Cache::new(2);
        cache.put("a".to_string(), 1, None);
        cache.get(&"a".to_string()); // hit
        cache.get(&"z".to_string()); // miss
        cache.put("b".to_string(), 2, None);
        cache.put("c".to_string(), 3, None); // evicts a

        let m = cache.metrics();
        assert_eq!(m.hits, 1);
        assert_eq!(m.misses, 1);
        assert_eq!(m.evictions, 1);
    }

    #[test]
    fn test_concurrent_access() {
        let cache = Arc::new(Cache::new(100));
        let mut handles = vec![];

        for i in 0..100 {
            let c = Arc::clone(&cache);
            handles.push(thread::spawn(move || {
                for j in 0..1000 {
                    let key = format!("{}", (i * 1000 + j) % 200);
                    c.put(key.clone(), j, None);
                    c.get(&key);
                }
            }));
        }

        for h in handles {
            h.join().unwrap();
        }

        let m = cache.metrics();
        println!(
            "Concurrent: size={}, hits={}, misses={}, evictions={}",
            m.size, m.hits, m.misses, m.evictions
        );
    }
}
```

### Running

```bash
cargo test -- --nocapture
```

### Expected Output

```
running 7 tests
test tests::test_get_put ... ok
test tests::test_lru_eviction ... ok
test tests::test_access_order ... ok
test tests::test_ttl_expiration ... ok
test tests::test_active_expiration ... ok
test tests::test_metrics ... ok
test tests::test_concurrent_access ... ok
Concurrent: size=100, hits=..., misses=..., evictions=...

test result: ok. 7 passed; 0 failed; 0 ignored
```

---

## Design Decisions

**Why a doubly linked list instead of a `VecDeque`?** A `VecDeque` gives O(1) push/pop at both ends but O(n) removal from the middle. Every `Get` operation needs to move an arbitrary element to the front. With a linked list, move-to-front is O(1) detach + O(1) push_front. The hashmap stores pointers (Go) or indices (Rust) directly to the node, so there is no search involved.

**Why `Mutex` instead of `RWMutex`?** In theory, `RWMutex` allows concurrent reads. In practice, `Get` is not a pure read: it updates the linked list order and modifies metrics counters. A `Get` under `RWMutex` would still need a write lock. Promoting from read to write lock is not supported and leads to deadlocks. A single `Mutex` is simpler and avoids the subtle bugs.

**Why slab allocation in Rust instead of raw pointers?** Rust's ownership model makes doubly linked lists with raw pointers notoriously painful (requires `unsafe`). Using a `Vec<Option<Entry>>` as a slab allocator with index-based links is safe, cache-friendly, and avoids all lifetime issues. The `free_slots` vec tracks holes for reuse.

**Callback under lock vs. async.** The eviction callback fires while the lock is held. This guarantees that the callback sees a consistent state and that eviction ordering is preserved. The trade-off is that a slow callback blocks all cache operations. In production, you would fire callbacks through a channel to a dedicated goroutine.

## Common Mistakes

**Forgetting to check TTL on `Get`.** If you only check expiration during background cleanup, stale data is served between cleanup intervals. Lazy expiration on every `Get` is essential.

**Not returning global tokens on per-bucket rejection.** In dual-bucket rate limiting, if you consume a global token but then the per-connection bucket rejects the request, you must return the global token. Otherwise, the global bucket drains without actual traffic flowing.

**Race in active expiration.** The cleanup goroutine must hold the lock while scanning and removing entries. If you release the lock between finding an expired entry and removing it, another goroutine might access or modify it.

**Linked list corruption on edge cases.** Single-element list, move-head-to-front (no-op), and remove-tail-when-tail-is-also-head all need explicit handling. Test these cases individually.

## Performance Notes

- Both implementations are O(1) for `Get`, `Put`, and `Delete` (amortized for hashmap resizing)
- The Go implementation uses `sync.Mutex`, which adds ~20-50ns of overhead per operation under low contention
- The Rust slab-based linked list is more cache-friendly than pointer-based: entries are contiguous in memory, reducing cache misses
- Active expiration walks the entire list under lock, which is O(n). Keep the cleanup interval proportional to cache size to avoid long lock holds

## Going Further

- Implement `GetOrSet(key, factory func() V, ttl)` that atomically fetches or computes a value, preventing the thundering herd problem where many goroutines simultaneously compute the same expensive value
- Add sharded locking: partition the keyspace into N shards, each with its own mutex and linked list, to reduce lock contention
- Implement an LFU (Least Frequently Used) variant using a frequency counter per entry and a min-heap or frequency-bucketed list
- Add persistent snapshots: serialize the cache to disk and restore on startup for warm restarts
- Benchmark against `hashicorp/golang-lru` and `moka` (Rust) to understand where your implementation differs in throughput and memory overhead
