# 13. Building a Thread-Safe Cache

<!--
difficulty: insane
concepts: [concurrent-cache, ttl, eviction, sharding, lock-striping]
tools: [go]
estimated_time: 60m
bloom_level: create
prerequisites: [sync-rwmutex, sync-map, atomic-package, goroutines]
-->

## The Challenge

Design and implement a production-grade, thread-safe in-memory cache with TTL (time-to-live) expiration. The cache must handle high-concurrency read/write workloads efficiently.

## Requirements

1. **Get(key) (value, bool)** -- retrieve a cached value; return false if missing or expired
2. **Set(key, value, ttl)** -- store a value with a time-to-live duration
3. **Delete(key)** -- remove a key from the cache
4. **Size() int** -- return the number of non-expired entries
5. **Cleanup()** -- remove all expired entries (run periodically in background)
6. Must support at least 100 concurrent goroutines reading and writing
7. Must pass the race detector
8. Expired items must not be returned by `Get`, even before cleanup runs

## Hints

<details>
<summary>Hint 1: Entry Structure</summary>

```go
type entry[V any] struct {
    value     V
    expiresAt time.Time
}

func (e *entry[V]) isExpired() bool {
    return time.Now().After(e.expiresAt)
}
```
</details>

<details>
<summary>Hint 2: Sharding for Reduced Contention</summary>

Instead of one mutex for the entire map, split the cache into N shards, each with its own lock. Hash the key to determine which shard to use:

```go
type shard[V any] struct {
    mu    sync.RWMutex
    items map[string]*entry[V]
}

type Cache[V any] struct {
    shards []*shard[V]
    numShards int
}

func (c *Cache[V]) getShard(key string) *shard[V] {
    h := fnv32(key)
    return c.shards[h%uint32(c.numShards)]
}
```
</details>

<details>
<summary>Hint 3: Background Cleanup</summary>

```go
func (c *Cache[V]) startCleanup(interval time.Duration) {
    go func() {
        ticker := time.NewTicker(interval)
        defer ticker.Stop()
        for range ticker.C {
            c.cleanup()
        }
    }()
}
```
</details>

## Success Criteria

1. All operations are race-free under `go run -race`
2. `Get` never returns expired entries
3. Background cleanup removes expired entries
4. Under high concurrency (100+ goroutines), no deadlocks or panics
5. Benchmark shows sharded cache outperforms single-lock cache under contention

Example test program:

```go
package main

import (
	"fmt"
	"hash/fnv"
	"sync"
	"sync/atomic"
	"time"
)

type entry[V any] struct {
	value     V
	expiresAt time.Time
}

func (e *entry[V]) isExpired() bool {
	return time.Now().After(e.expiresAt)
}

type shard[V any] struct {
	mu    sync.RWMutex
	items map[string]*entry[V]
}

type Cache[V any] struct {
	shards    []*shard[V]
	numShards uint32
}

func NewCache[V any](numShards int) *Cache[V] {
	shards := make([]*shard[V], numShards)
	for i := range shards {
		shards[i] = &shard[V]{items: make(map[string]*entry[V])}
	}
	return &Cache[V]{shards: shards, numShards: uint32(numShards)}
}

func (c *Cache[V]) getShard(key string) *shard[V] {
	h := fnv.New32a()
	h.Write([]byte(key))
	return c.shards[h.Sum32()%c.numShards]
}

func (c *Cache[V]) Set(key string, value V, ttl time.Duration) {
	s := c.getShard(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[key] = &entry[V]{value: value, expiresAt: time.Now().Add(ttl)}
}

func (c *Cache[V]) Get(key string) (V, bool) {
	s := c.getShard(key)
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.items[key]
	if !ok || e.isExpired() {
		var zero V
		return zero, false
	}
	return e.value, true
}

func (c *Cache[V]) Delete(key string) {
	s := c.getShard(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, key)
}

func (c *Cache[V]) Size() int {
	total := 0
	for _, s := range c.shards {
		s.mu.RLock()
		for _, e := range s.items {
			if !e.isExpired() {
				total++
			}
		}
		s.mu.RUnlock()
	}
	return total
}

func (c *Cache[V]) Cleanup() {
	for _, s := range c.shards {
		s.mu.Lock()
		for k, e := range s.items {
			if e.isExpired() {
				delete(s.items, k)
			}
		}
		s.mu.Unlock()
	}
}

func (c *Cache[V]) StartCleanup(interval time.Duration, done <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c.Cleanup()
			case <-done:
				return
			}
		}
	}()
}

func main() {
	cache := NewCache[string](16)
	done := make(chan struct{})
	cache.StartCleanup(100*time.Millisecond, done)

	var wg sync.WaitGroup
	var hits, misses atomic.Int64

	// Writers
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				cache.Set(key, fmt.Sprintf("val-%d", j), 50*time.Millisecond)
			}
		}(i)
	}

	// Readers
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				if _, ok := cache.Get(key); ok {
					hits.Add(1)
				} else {
					misses.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond) // Let cleanup run
	close(done)

	fmt.Printf("Hits: %d, Misses: %d\n", hits.Load(), misses.Load())
	fmt.Printf("Size after cleanup: %d\n", cache.Size())
}
```

## Research Resources

- [sync.RWMutex documentation](https://pkg.go.dev/sync#RWMutex)
- [hash/fnv package](https://pkg.go.dev/hash/fnv)
- [groupcache (Go caching library)](https://github.com/golang/groupcache)
- [ristretto (high-performance cache)](https://github.com/dgraph-io/ristretto)
- [Lock Striping (Wikipedia)](https://en.wikipedia.org/wiki/Lock_striping)

## What's Next

Continue to [14 - Contention Profiling](../14-contention-profiling/14-contention-profiling.md) to learn how to profile mutex contention with pprof.

## Summary

- Shard-based caches distribute lock contention across multiple mutexes
- Use `RWMutex` per shard: `RLock` for reads, `Lock` for writes and cleanup
- Check expiration on every `Get` so expired data is never returned
- Run periodic background cleanup to reclaim memory from expired entries
- FNV hash is a fast, simple hash for distributing keys across shards
