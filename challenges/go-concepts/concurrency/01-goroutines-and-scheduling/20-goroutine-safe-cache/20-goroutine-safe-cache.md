---
difficulty: advanced
concepts: [background goroutine, goroutine lifecycle, mutex for shared state, TTL expiration, concurrent read/write]
tools: [go]
estimated_time: 45m
bloom_level: create
---


# 20. Goroutine-Safe Cache


## Learning Objectives
After completing this exercise, you will be able to:
- **Architect** a system with multiple cooperating goroutines: background cleanup and concurrent request handlers
- **Implement** a cache with TTL-based expiration driven by a background goroutine
- **Coordinate** goroutine lifecycle using a stop channel for clean shutdown
- **Reason** about concurrent access patterns where a mutex protects shared state accessed by multiple goroutines


## Why a Goroutine-Safe Cache

Every API service eventually needs a cache. Database queries are expensive, and many requests ask for the same data within short windows. A cache stores results in memory with a time-to-live (TTL), so subsequent requests get instant responses instead of hitting the database.

The concurrency challenge is architectural: multiple request-handling goroutines read and write cache entries simultaneously, while a background goroutine periodically scans for expired entries and evicts them. This is not a single fan-out pattern -- it is multiple goroutines with different roles sharing a data structure over an extended period.

The mutex in this exercise is a tool, not the teaching focus. The real lesson is goroutine architecture: how to design a long-running background goroutine that cooperates with request goroutines, how to signal it to stop cleanly, and how to reason about what happens when 10 goroutines and a cleanup routine all touch the same cache. This pattern appears in every production Go service that uses in-memory caching, rate limiting, or connection tracking.


## Step 1 -- Basic Cache Without TTL

Start with the simplest useful cache: a map protected by a mutex, with Get, Set, and Delete operations. No expiration, no background goroutines yet.

```go
package main

import (
	"fmt"
	"sync"
)

type Cache struct {
	mu      sync.RWMutex
	entries map[string]interface{}
}

func NewCache() *Cache {
	return &Cache{
		entries: make(map[string]interface{}),
	}
}

func (c *Cache) Set(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = value
}

func (c *Cache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	val, ok := c.entries[key]
	return val, ok
}

func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

func main() {
	cache := NewCache()

	cache.Set("user:1001", "Alice")
	cache.Set("user:1002", "Bob")
	cache.Set("user:1003", "Charlie")

	fmt.Printf("Cache size: %d\n\n", cache.Len())

	for _, key := range []string{"user:1001", "user:1002", "user:9999"} {
		if val, ok := cache.Get(key); ok {
			fmt.Printf("  GET %-12s -> %v\n", key, val)
		} else {
			fmt.Printf("  GET %-12s -> MISS\n", key)
		}
	}

	cache.Delete("user:1002")
	fmt.Printf("\nAfter deleting user:1002: size=%d\n", cache.Len())

	if _, ok := cache.Get("user:1002"); !ok {
		fmt.Println("  user:1002 confirmed deleted")
	}
}
```

**What's happening here:** The `Cache` struct wraps a map with a `sync.RWMutex`. `Get` uses `RLock` (multiple readers can proceed simultaneously), while `Set` and `Delete` use `Lock` (exclusive access). This is the standard read-heavy cache pattern -- reads are cheap, writes serialize.

**Key insight:** `sync.RWMutex` is chosen over `sync.Mutex` because caches are read-heavy. A regular mutex would force readers to wait for each other, but `RWMutex` allows concurrent reads. The write lock is exclusive -- when a write is in progress, all reads and other writes wait.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
Cache size: 3

  GET user:1001    -> Alice
  GET user:1002    -> Bob
  GET user:9999    -> MISS

After deleting user:1002: size=2
  user:1002 confirmed deleted
```


## Step 2 -- Add TTL to Cache Entries

Wrap each value with an expiration timestamp. Get returns a miss if the entry has expired, even if it is still physically in the map.

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type cacheEntry struct {
	value     interface{}
	expiresAt time.Time
}

func (e cacheEntry) isExpired() bool {
	return time.Now().After(e.expiresAt)
}

type Cache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
}

func NewCache() *Cache {
	return &Cache{
		entries: make(map[string]cacheEntry),
	}
}

func (c *Cache) Set(key string, value interface{}, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = cacheEntry{
		value:     value,
		expiresAt: time.Now().Add(ttl),
	}
}

func (c *Cache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[key]
	if !ok || entry.isExpired() {
		return nil, false
	}
	return entry.value, true
}

func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	count := 0
	for _, entry := range c.entries {
		if !entry.isExpired() {
			count++
		}
	}
	return count
}

func (c *Cache) physicalLen() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

func main() {
	cache := NewCache()

	cache.Set("session:abc", "user-alice", 200*time.Millisecond)
	cache.Set("session:def", "user-bob", 500*time.Millisecond)
	cache.Set("config:theme", "dark", 1*time.Second)

	fmt.Printf("Immediately after setting:\n")
	fmt.Printf("  Logical size: %d | Physical size: %d\n\n", cache.Len(), cache.physicalLen())

	for _, key := range []string{"session:abc", "session:def", "config:theme"} {
		if val, ok := cache.Get(key); ok {
			fmt.Printf("  GET %-15s -> %v\n", key, val)
		} else {
			fmt.Printf("  GET %-15s -> MISS (expired)\n", key)
		}
	}

	fmt.Println("\n--- Waiting 300ms (session:abc expires) ---")
	time.Sleep(300 * time.Millisecond)

	fmt.Printf("  Logical size: %d | Physical size: %d\n", cache.Len(), cache.physicalLen())
	for _, key := range []string{"session:abc", "session:def", "config:theme"} {
		if val, ok := cache.Get(key); ok {
			fmt.Printf("  GET %-15s -> %v\n", key, val)
		} else {
			fmt.Printf("  GET %-15s -> MISS (expired)\n", key)
		}
	}

	fmt.Println("\n--- Waiting 300ms more (session:def expires) ---")
	time.Sleep(300 * time.Millisecond)

	fmt.Printf("  Logical size: %d | Physical size: %d\n", cache.Len(), cache.physicalLen())
	fmt.Println("  Note: physical size is still 3 -- expired entries not yet removed from map")
}
```

**What's happening here:** Each entry stores an `expiresAt` timestamp. `Get` checks expiration before returning -- if the entry has expired, it returns a miss. `Len` counts only non-expired entries (logical size). But the expired entries remain in the map (physical size stays at 3), wasting memory. This motivates the background cleanup goroutine in the next step.

**Key insight:** Lazy expiration (checking at read time) is simple but leaks memory. Entries that are never read again sit in the map forever. For a long-running service caching millions of keys, this memory leak is a real production issue. The solution is a background goroutine that periodically scans and evicts expired entries.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
Immediately after setting:
  Logical size: 3 | Physical size: 3

  GET session:abc     -> user-alice
  GET session:def     -> user-bob
  GET config:theme    -> dark

--- Waiting 300ms (session:abc expires) ---
  Logical size: 2 | Physical size: 3
  GET session:abc     -> MISS (expired)
  GET session:def     -> user-bob
  GET config:theme    -> dark

--- Waiting 300ms more (session:def expires) ---
  Logical size: 1 | Physical size: 3
  Note: physical size is still 3 -- expired entries not yet removed from map
```


## Step 3 -- Background Cleanup Goroutine

Launch a goroutine that wakes up periodically, scans the map for expired entries, and deletes them. Use a stop channel to shut down the goroutine cleanly.

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type cacheEntry struct {
	value     interface{}
	expiresAt time.Time
}

func (e cacheEntry) isExpired() bool {
	return time.Now().After(e.expiresAt)
}

type Cache struct {
	mu              sync.RWMutex
	entries         map[string]cacheEntry
	stop            chan struct{}
	cleanupInterval time.Duration
}

func NewCache(cleanupInterval time.Duration) *Cache {
	c := &Cache{
		entries:         make(map[string]cacheEntry),
		stop:            make(chan struct{}),
		cleanupInterval: cleanupInterval,
	}
	go c.cleanupLoop()
	return c
}

func (c *Cache) cleanupLoop() {
	ticker := time.NewTicker(c.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			evicted := c.evictExpired()
			if evicted > 0 {
				fmt.Printf("    [cleanup] evicted %d expired entries\n", evicted)
			}
		case <-c.stop:
			fmt.Println("    [cleanup] goroutine stopped")
			return
		}
	}
}

func (c *Cache) evictExpired() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	evicted := 0
	for key, entry := range c.entries {
		if entry.isExpired() {
			delete(c.entries, key)
			evicted++
		}
	}
	return evicted
}

func (c *Cache) Set(key string, value interface{}, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = cacheEntry{
		value:     value,
		expiresAt: time.Now().Add(ttl),
	}
}

func (c *Cache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[key]
	if !ok || entry.isExpired() {
		return nil, false
	}
	return entry.value, true
}

func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

func (c *Cache) Stop() {
	close(c.stop)
}

func main() {
	cache := NewCache(200 * time.Millisecond)

	cache.Set("key-a", "value-a", 150*time.Millisecond)
	cache.Set("key-b", "value-b", 350*time.Millisecond)
	cache.Set("key-c", "value-c", 550*time.Millisecond)
	cache.Set("key-d", "value-d", 1*time.Second)

	fmt.Printf("Cache populated: %d entries\n\n", cache.Len())

	checkpoints := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		200 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
	}

	elapsed := time.Duration(0)
	for _, wait := range checkpoints {
		time.Sleep(wait)
		elapsed += wait
		fmt.Printf("  t=%v | entries in map: %d\n", elapsed, cache.Len())
	}

	fmt.Println()
	cache.Stop()
	time.Sleep(50 * time.Millisecond)
	fmt.Println("\nCache stopped. Background goroutine exited cleanly.")
}
```

**What's happening here:** `NewCache` launches a background goroutine that runs `cleanupLoop`. Every 200ms, the ticker fires and `evictExpired` scans the map, deleting any entry whose `expiresAt` is in the past. The `stop` channel is used for clean shutdown -- `cache.Stop()` closes the channel, which causes the select in the goroutine to take the stop branch and return.

**Key insight:** The cleanup goroutine and the request code share the same `entries` map, protected by the same mutex. The cleanup goroutine takes a write lock during eviction (it modifies the map), which means requests briefly pause during cleanup. For a cache with millions of entries, you might batch the eviction or use a different strategy, but for typical caches this is perfectly fine.

**Why `close(c.stop)` and not `c.stop <- struct{}{}`?** Closing a channel wakes up all goroutines blocked on it. If you ever have multiple cleanup goroutines (e.g., one per shard), `close` stops all of them with a single call. Sending a value would wake only one.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
Cache populated: 4 entries

  t=100ms | entries in map: 4
    [cleanup] evicted 1 expired entries
  t=300ms | entries in map: 3
    [cleanup] evicted 1 expired entries
  t=500ms | entries in map: 2
    [cleanup] evicted 1 expired entries
  t=700ms | entries in map: 1
  t=1.1s | entries in map: 0
    [cleanup] evicted 1 expired entries

    [cleanup] goroutine stopped

Cache stopped. Background goroutine exited cleanly.
```


## Step 4 -- Concurrent Requests with Background Cleanup

Bring it all together: 10 request goroutines reading and writing the cache concurrently, while the background cleanup goroutine evicts expired entries. Track hit/miss statistics.

```go
package main

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

const (
	requestGoroutines = 10
	opsPerGoroutine   = 20
	cleanupInterval   = 300 * time.Millisecond
	minTTL            = 200 * time.Millisecond
	maxTTL            = 800 * time.Millisecond
)

type cacheEntry struct {
	value     interface{}
	expiresAt time.Time
}

func (e cacheEntry) isExpired() bool {
	return time.Now().After(e.expiresAt)
}

type CacheStats struct {
	Hits     int64
	Misses   int64
	Sets     int64
	Evicted  int64
}

type Cache struct {
	mu              sync.RWMutex
	entries         map[string]cacheEntry
	stop            chan struct{}
	cleanupInterval time.Duration
	stats           CacheStats
}

func NewCache(cleanupInterval time.Duration) *Cache {
	c := &Cache{
		entries:         make(map[string]cacheEntry),
		stop:            make(chan struct{}),
		cleanupInterval: cleanupInterval,
	}
	go c.cleanupLoop()
	return c
}

func (c *Cache) cleanupLoop() {
	ticker := time.NewTicker(c.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			evicted := c.evictExpired()
			atomic.AddInt64(&c.stats.Evicted, int64(evicted))
		case <-c.stop:
			return
		}
	}
}

func (c *Cache) evictExpired() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	evicted := 0
	for key, entry := range c.entries {
		if entry.isExpired() {
			delete(c.entries, key)
			evicted++
		}
	}
	return evicted
}

func (c *Cache) Set(key string, value interface{}, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = cacheEntry{
		value:     value,
		expiresAt: time.Now().Add(ttl),
	}
	atomic.AddInt64(&c.stats.Sets, 1)
}

func (c *Cache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[key]
	if !ok || entry.isExpired() {
		atomic.AddInt64(&c.stats.Misses, 1)
		return nil, false
	}
	atomic.AddInt64(&c.stats.Hits, 1)
	return entry.value, true
}

func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

func (c *Cache) Stop() {
	close(c.stop)
}

func (c *Cache) Stats() CacheStats {
	return CacheStats{
		Hits:    atomic.LoadInt64(&c.stats.Hits),
		Misses:  atomic.LoadInt64(&c.stats.Misses),
		Sets:    atomic.LoadInt64(&c.stats.Sets),
		Evicted: atomic.LoadInt64(&c.stats.Evicted),
	}
}

func simulateRequests(id int, cache *Cache, wg *sync.WaitGroup) {
	defer wg.Done()

	keys := []string{"user:1", "user:2", "user:3", "product:1", "product:2",
		"config:theme", "config:locale", "session:abc"}

	for i := 0; i < opsPerGoroutine; i++ {
		key := keys[rand.Intn(len(keys))]

		if _, ok := cache.Get(key); !ok {
			ttl := minTTL + time.Duration(rand.Intn(int(maxTTL-minTTL)))
			value := fmt.Sprintf("data-from-goroutine-%d-op-%d", id, i)
			cache.Set(key, value, ttl)
		}

		time.Sleep(time.Duration(20+rand.Intn(30)) * time.Millisecond)
	}
}

func main() {
	rand.Seed(time.Now().UnixNano())
	cache := NewCache(cleanupInterval)

	fmt.Printf("=== Goroutine-Safe Cache ===\n")
	fmt.Printf("  Request goroutines: %d\n", requestGoroutines)
	fmt.Printf("  Operations per goroutine: %d\n", opsPerGoroutine)
	fmt.Printf("  Cleanup interval: %v\n", cleanupInterval)
	fmt.Printf("  TTL range: %v - %v\n\n", minTTL, maxTTL)

	start := time.Now()
	var wg sync.WaitGroup

	for i := 1; i <= requestGoroutines; i++ {
		wg.Add(1)
		go simulateRequests(i, cache, &wg)
	}

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s := cache.Stats()
				fmt.Printf("  [monitor] t=%v | size=%d | hits=%d misses=%d sets=%d evicted=%d\n",
					time.Since(start).Round(100*time.Millisecond),
					cache.Len(), s.Hits, s.Misses, s.Sets, s.Evicted)
			case <-done:
				return
			}
		}
	}()

	wg.Wait()
	close(done)
	elapsed := time.Since(start)

	cache.Stop()
	time.Sleep(50 * time.Millisecond)

	stats := cache.Stats()

	fmt.Printf("\n=== Final Report ===\n")
	fmt.Printf("  Duration:     %v\n", elapsed.Round(time.Millisecond))
	fmt.Printf("  Total ops:    %d (gets + sets)\n", stats.Hits+stats.Misses+stats.Sets)
	fmt.Printf("  Cache hits:   %d\n", stats.Hits)
	fmt.Printf("  Cache misses: %d\n", stats.Misses)
	fmt.Printf("  Cache sets:   %d\n", stats.Sets)
	fmt.Printf("  Evicted:      %d\n", stats.Evicted)
	fmt.Printf("  Remaining:    %d entries\n", cache.Len())

	totalReads := stats.Hits + stats.Misses
	if totalReads > 0 {
		hitRate := float64(stats.Hits) / float64(totalReads) * 100
		fmt.Printf("  Hit rate:     %.1f%%\n", hitRate)
	}

	fmt.Printf("\n  Goroutine architecture:\n")
	fmt.Printf("    - %d request goroutines (completed)\n", requestGoroutines)
	fmt.Printf("    - 1 cleanup goroutine (stopped)\n")
	fmt.Printf("    - 1 monitor goroutine (stopped)\n")
}
```

**What's happening here:** Three types of goroutines interact with the cache simultaneously:
1. **10 request goroutines** -- each performs 20 operations (get, and set on miss) with random delays between operations
2. **1 cleanup goroutine** -- wakes every 300ms to evict expired entries
3. **1 monitor goroutine** -- prints live stats every 500ms

The `CacheStats` struct uses atomic operations for counters because they are incremented from many goroutines. The mutex protects the map; the atomics protect the counters. Both are necessary for correctness.

**Key insight:** This is goroutine architecture in action. The cache is a shared resource accessed by goroutines with different roles and different lifecycles. Request goroutines come and go. The cleanup goroutine runs for the entire cache lifetime. The monitor goroutine is auxiliary. All three types coordinate through the same mutex and stop channels. Understanding this multi-goroutine architecture -- not just "launch N goroutines and wait" -- is what separates basic concurrency from production-grade design.

### Intermediate Verification
```bash
go run main.go
```
Expected output (values vary due to randomness):
```
=== Goroutine-Safe Cache ===
  Request goroutines: 10
  Operations per goroutine: 20
  Cleanup interval: 300ms
  TTL range: 200ms - 800ms

  [monitor] t=500ms | size=7 | hits=42 misses=58 sets=58 evicted=12
  [monitor] t=1s | size=5 | hits=89 misses=71 sets=71 evicted=38

=== Final Report ===
  Duration:     1.05s
  Total ops:    330 (gets + sets)
  Cache hits:   130
  Cache misses: 70
  Cache sets:   70
  Evicted:      55
  Remaining:    5 entries
  Hit rate:     65.0%

  Goroutine architecture:
    - 10 request goroutines (completed)
    - 1 cleanup goroutine (stopped)
    - 1 monitor goroutine (stopped)
```


## Common Mistakes

### Not Stopping the Background Goroutine (Goroutine Leak)

**Wrong -- complete program:**
```go
package main

import (
	"fmt"
	"time"
)

type Cache struct {
	entries map[string]string
}

func NewCache() *Cache {
	c := &Cache{entries: make(map[string]string)}
	go func() {
		for {
			time.Sleep(1 * time.Second)
			fmt.Println("cleanup running...") // runs forever, even after cache is unused
		}
	}()
	return c
}

func main() {
	cache := NewCache()
	cache.entries["key"] = "value"
	fmt.Println("done")
	// cache goes out of scope but cleanup goroutine keeps running
	// In a long-running service, creating caches without stopping them leaks goroutines
}
```
**What happens:** The background goroutine has no way to stop. Every time you create a new cache without stopping the old one, you leak a goroutine. In a test suite creating caches per test, you accumulate hundreds of leaked goroutines.

**Correct -- use a stop channel:**
```go
package main

import (
	"fmt"
	"time"
)

type Cache struct {
	entries map[string]string
	stop    chan struct{}
}

func NewCache() *Cache {
	c := &Cache{
		entries: make(map[string]string),
		stop:    make(chan struct{}),
	}
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				fmt.Println("cleanup running...")
			case <-c.stop:
				fmt.Println("cleanup stopped")
				return
			}
		}
	}()
	return c
}

func (c *Cache) Stop() { close(c.stop) }

func main() {
	cache := NewCache()
	cache.entries["key"] = "value"
	cache.Stop()
	time.Sleep(50 * time.Millisecond)
	fmt.Println("done -- goroutine exited cleanly")
}
```

### Using time.Sleep Instead of a Ticker in the Cleanup Loop

**Wrong -- complete program:**
```go
package main

import (
	"fmt"
	"time"
)

func main() {
	stop := make(chan struct{})
	go func() {
		for {
			time.Sleep(1 * time.Second) // cannot be interrupted by stop
			select {
			case <-stop:
				fmt.Println("stopped")
				return
			default:
				fmt.Println("cleaning...")
			}
		}
	}()

	time.Sleep(500 * time.Millisecond)
	close(stop)
	// goroutine is stuck in time.Sleep for up to 1 second before it checks stop
	time.Sleep(1500 * time.Millisecond)
}
```
**What happens:** `time.Sleep` is not interruptible. When you close `stop`, the goroutine is blocked in sleep and won't check the stop channel until the sleep finishes. Shutdown is delayed by up to one full interval.

**Correct -- use time.Ticker with select:**
```go
package main

import (
	"fmt"
	"time"
)

func main() {
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				fmt.Println("cleaning...")
			case <-stop:
				fmt.Println("stopped immediately")
				return
			}
		}
	}()

	time.Sleep(500 * time.Millisecond)
	close(stop)
	time.Sleep(100 * time.Millisecond) // goroutine exits almost immediately
}
```

### Incrementing Counters Without Atomic Operations

**Wrong -- complete program:**
```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var hits int // not atomic, not mutex-protected
	var wg sync.WaitGroup

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hits++ // DATA RACE: concurrent read-modify-write
		}()
	}
	wg.Wait()
	fmt.Println("hits:", hits) // likely less than 1000
}
```
**What happens:** `hits++` is not atomic -- it reads, increments, and writes in three steps. Two goroutines can read the same value, both increment to the same result, and one increment is lost. The race detector catches this.

**Correct -- use sync/atomic:**
```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

func main() {
	var hits int64
	var wg sync.WaitGroup

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			atomic.AddInt64(&hits, 1)
		}()
	}
	wg.Wait()
	fmt.Println("hits:", hits) // always 1000
}
```


## Verify What You Learned

Build a "rate limiter cache" that:
1. Creates a cache where each key represents an API client, and the value is the request count in the current window
2. Each entry has a 1-second TTL (the rate limit window)
3. 5 "client" goroutines each make 30 rapid requests, incrementing their count via Get-then-Set
4. A background cleanup goroutine runs every 500ms to evict expired windows
5. A rate limit of 10 requests per window -- when a client's count reaches 10, subsequent requests within the window are rejected
6. After all goroutines complete, print per-client stats: total attempted, accepted, rejected, and the number of rate limit windows that were created

**Hint:** The Get-Set pattern for incrementing is not atomic, but for a rate limiter the slight inaccuracy is acceptable. If you want perfect accuracy, use a mutex around the Get-increment-Set sequence.


## What's Next
Continue to [21-Goroutine Profiling with pprof](../21-goroutine-profiling-pprof/21-goroutine-profiling-pprof.md) to learn how to diagnose goroutine issues in production using runtime profiling tools.


## Summary
- A background cleanup goroutine is the standard pattern for TTL-based cache eviction in Go services
- `sync.RWMutex` allows concurrent reads while serializing writes -- ideal for read-heavy caches
- The stop channel pattern (`close(c.stop)` + `select` in the goroutine) provides clean, immediate shutdown
- Use `time.Ticker` with `select` instead of `time.Sleep` so the goroutine can respond to stop signals without delay
- Atomic operations protect simple counters; the mutex protects the map -- both are needed for different access patterns
- Goroutine-safe cache architecture involves three cooperating goroutine roles: request handlers, background cleanup, and optional monitoring
- Always stop background goroutines when the cache is no longer needed to prevent goroutine leaks in tests and long-running services


## Reference
- [sync.RWMutex](https://pkg.go.dev/sync#RWMutex)
- [sync/atomic](https://pkg.go.dev/sync/atomic)
- [time.Ticker](https://pkg.go.dev/time#Ticker)
- [Go Blog: Share Memory By Communicating](https://go.dev/blog/codelab-share)
- [Effective Go: Concurrency](https://go.dev/doc/effective_go#concurrency)
