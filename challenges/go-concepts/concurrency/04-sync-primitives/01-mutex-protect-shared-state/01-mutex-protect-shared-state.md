---
difficulty: basic
concepts: [sync.Mutex, Lock, Unlock, defer, race condition, critical section]
tools: [go]
estimated_time: 15m
bloom_level: apply
prerequisites: [goroutines, go keyword, WaitGroup basics]
---

# 1. Mutex: Protect Shared State


## Learning Objectives
After completing this exercise, you will be able to:
- **Identify** race conditions caused by unsynchronized access to shared state
- **Protect** shared variables using `sync.Mutex` with `Lock` and `Unlock`
- **Apply** the `defer mu.Unlock()` pattern for safe critical sections
- **Detect** data races using Go's built-in race detector

## Why Mutex
When multiple goroutines read and write the same variable without synchronization, the result is a data race -- one of the most insidious classes of bugs in concurrent programming. The outcome depends on the precise interleaving of goroutine execution, making the bug non-deterministic: your program might appear correct in testing and fail silently in production.

In a real API server, multiple HTTP handlers run concurrently as goroutines. When these handlers share an in-memory cache, every read and write to that cache is a potential data race. Without synchronization, cached responses get corrupted, entries vanish, or the entire process panics from a concurrent map write.

A `sync.Mutex` (mutual exclusion lock) solves this by ensuring that only one goroutine at a time can execute a critical section of code. When a goroutine calls `Lock()`, any other goroutine that also calls `Lock()` will block until the first goroutine calls `Unlock()`. This serializes access to shared state, eliminating the race.

The idiomatic Go pattern is to call `defer mu.Unlock()` immediately after `Lock()`. This guarantees the lock is released even if the critical section panics, preventing deadlocks caused by forgotten unlocks.

## Step 1 -- Observe the Race Condition in a Shared Cache

Imagine an API server that caches responses in memory. Multiple HTTP handler goroutines write to the same map concurrently. Without protection, the cache is corrupted:

```go
package main

import (
	"fmt"
	"sync"
)

const (
	simulatedHandlers = 100
	endpointCount     = 10
)

func simulateUnsafeCacheAccess(cache map[string]string, handlerID int) {
	key := fmt.Sprintf("endpoint-%d", handlerID%endpointCount)
	cache[key] = fmt.Sprintf(`{"handler":%d,"status":"ok"}`, handlerID)
	_ = cache[key]
}

func runUnsafeCacheDemo() {
	cache := make(map[string]string)
	var wg sync.WaitGroup

	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("PANIC: %v\n", r)
			fmt.Println("This is what happens when concurrent goroutines write to an unprotected map.")
		}
	}()

	for i := 0; i < simulatedHandlers; i++ {
		wg.Add(1)
		go func(handlerID int) {
			defer wg.Done()
			simulateUnsafeCacheAccess(cache, handlerID)
		}(i)
	}

	wg.Wait()
	fmt.Printf("Cache has %d entries\n", len(cache))
}

func main() {
	runUnsafeCacheDemo()
}
```

Run it:

```bash
go run main.go
```

You should see a fatal panic:
```
PANIC: concurrent map writes
This is what happens when concurrent goroutines write to an unprotected map.
```

Now run with the race detector to get detailed diagnostics:

```bash
go run -race main.go
```

The race detector reports `DATA RACE` warnings with stack traces showing the conflicting accesses.

### Intermediate Verification
You should see a panic from concurrent map writes, or `WARNING: DATA RACE` output from the race detector. This is the real consequence of sharing a cache without synchronization in a production server.

## Step 2 -- Protect the Cache with sync.Mutex

Wrap every cache access in a Lock/Unlock pair. Every goroutine must acquire the mutex before touching the map:

```go
package main

import (
	"fmt"
	"sync"
)

const (
	simulatedHandlers = 100
	endpointCount     = 10
)

func writeCacheEntry(mu *sync.Mutex, cache map[string]string, key, value string) {
	mu.Lock()
	cache[key] = value
	mu.Unlock()
}

func readCacheEntry(mu *sync.Mutex, cache map[string]string, key string) string {
	mu.Lock()
	defer mu.Unlock()
	return cache[key]
}

func simulateSafeCacheAccess(mu *sync.Mutex, cache map[string]string, handlerID int) {
	key := fmt.Sprintf("endpoint-%d", handlerID%endpointCount)
	response := fmt.Sprintf(`{"handler":%d,"status":"ok"}`, handlerID)
	writeCacheEntry(mu, cache, key, response)
	_ = readCacheEntry(mu, cache, key)
}

func main() {
	cache := make(map[string]string)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < simulatedHandlers; i++ {
		wg.Add(1)
		go func(handlerID int) {
			defer wg.Done()
			simulateSafeCacheAccess(&mu, cache, handlerID)
		}(i)
	}

	wg.Wait()
	fmt.Printf("Cache has %d entries (expected %d)\n", len(cache), endpointCount)
}
```

```bash
go run -race main.go
```

Expected output:
```
Cache has 10 entries (expected 10)
```

No panics, no race warnings. The mutex serializes access so only one goroutine touches the map at a time.

### Intermediate Verification
Run `go run -race main.go`. The program completes cleanly with exactly 10 cache entries and no `DATA RACE` warnings.

## Step 3 -- The defer Unlock Pattern

In production code, critical sections often contain logic that can return early or panic. Using `defer mu.Unlock()` immediately after `Lock()` guarantees the lock is always released:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

const (
	writerCount  = 50
	userKeyCount = 5
)

type SimpleCache struct {
	mu      sync.Mutex
	entries map[string]string
}

func NewSimpleCache() *SimpleCache {
	return &SimpleCache{entries: make(map[string]string)}
}

func (c *SimpleCache) Set(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = value
}

func (c *SimpleCache) Get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	val, ok := c.entries[key]
	return val, ok
}

func populateCacheConcurrently(cache *SimpleCache) {
	var wg sync.WaitGroup

	for i := 0; i < writerCount; i++ {
		wg.Add(1)
		go func(handlerID int) {
			defer wg.Done()
			key := fmt.Sprintf("user-%d", handlerID%userKeyCount)
			value := fmt.Sprintf(`{"id":%d,"ts":"%s"}`, handlerID, time.Now().Format(time.RFC3339Nano))
			cache.Set(key, value)
		}(i)
	}

	wg.Wait()
}

func printCacheContents(cache *SimpleCache) {
	for i := 0; i < userKeyCount; i++ {
		key := fmt.Sprintf("user-%d", i)
		val, ok := cache.Get(key)
		fmt.Printf("  %s: found=%v value=%s\n", key, ok, val)
	}
}

func main() {
	cache := NewSimpleCache()
	populateCacheConcurrently(cache)
	printCacheContents(cache)
}
```

The `defer mu.Unlock()` line executes when the enclosing function returns, guaranteeing the lock is always released. This is especially important when the critical section might return early or panic.

### Intermediate Verification
```bash
go run -race main.go
```
All 5 user keys should be present, no race warnings.

## Step 4 -- Struct-Embedded Mutex: The Cache Type

The idiomatic Go pattern places the mutex alongside the data it protects inside a struct. This is how you would build a real API cache:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

const simulatedRequests = 200

type CacheEntry struct {
	Body     string
	CachedAt time.Time
}

type APICache struct {
	mu      sync.Mutex
	entries map[string]CacheEntry
}

func NewAPICache() *APICache {
	return &APICache{entries: make(map[string]CacheEntry)}
}

func (c *APICache) Set(endpoint string, body string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[endpoint] = CacheEntry{Body: body, CachedAt: time.Now()}
}

func (c *APICache) Get(endpoint string) (CacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[endpoint]
	return entry, ok
}

func (c *APICache) Size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// Snapshot returns a COPY so callers cannot bypass the mutex.
func (c *APICache) Snapshot() map[string]CacheEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make(map[string]CacheEntry, len(c.entries))
	for k, v := range c.entries {
		result[k] = v
	}
	return result
}

func simulateHandlerTraffic(cache *APICache, endpoints []string) {
	var wg sync.WaitGroup

	for i := 0; i < simulatedRequests; i++ {
		wg.Add(1)
		go func(handlerID int) {
			defer wg.Done()
			endpoint := endpoints[handlerID%len(endpoints)]
			cache.Set(endpoint, fmt.Sprintf(`{"handler":%d,"status":"ok"}`, handlerID))

			if entry, ok := cache.Get(endpoint); ok {
				_ = entry.Body
			}
		}(i)
	}

	wg.Wait()
}

func printCacheSnapshot(cache *APICache) {
	snap := cache.Snapshot()
	fmt.Printf("Cache has %d endpoints:\n", len(snap))
	for endpoint, entry := range snap {
		fmt.Printf("  %s -> cached at %s\n", endpoint, entry.CachedAt.Format("15:04:05.000"))
	}
}

func main() {
	cache := NewAPICache()
	endpoints := []string{"/api/users", "/api/orders", "/api/products", "/api/health", "/api/config"}

	simulateHandlerTraffic(cache, endpoints)
	printCacheSnapshot(cache)
}
```

Expected output:
```
Cache has 5 endpoints:
  /api/users -> cached at 14:23:01.123
  /api/orders -> cached at 14:23:01.124
  /api/products -> cached at 14:23:01.123
  /api/health -> cached at 14:23:01.124
  /api/config -> cached at 14:23:01.123
```

The key design points:
- The mutex is unexported (`mu`), preventing external code from locking it incorrectly.
- `Snapshot()` returns a copy of the map, so callers cannot mutate internal state without the mutex.
- Every method that touches `entries` acquires the lock first.

### Intermediate Verification
```bash
go run -race main.go
```
All 5 endpoints cached, no race warnings.

## Common Mistakes

### Forgetting to Unlock

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var mu sync.Mutex
	mu.Lock()
	fmt.Println("acquired lock")
	// Forgot mu.Unlock() -- any goroutine that calls mu.Lock() will block forever.
	// This causes a deadlock if main also tries to lock again.
	mu.Lock() // DEADLOCK: blocks forever waiting for itself
	fmt.Println("this line is never reached")
}
```

**What happens:** Deadlock. All goroutines waiting on Lock will block permanently.

**Fix:** Always pair Lock with Unlock. Use `defer mu.Unlock()` immediately after Lock.

### Copying a Mutex

```go
package main

import (
	"fmt"
	"sync"
)

func cacheSet(mu sync.Mutex, cache map[string]string, key, val string) {
	mu.Lock()
	defer mu.Unlock()
	cache[key] = val // this lock is independent of the original -- no protection!
}

func main() {
	var mu sync.Mutex
	cache := make(map[string]string)
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			cacheSet(mu, cache, fmt.Sprintf("k%d", id), "v") // each goroutine gets its own mutex copy!
		}(i)
	}

	wg.Wait()
	fmt.Printf("Entries: %d (likely panic or wrong count due to copied mutex)\n", len(cache))
}
```

**What happens:** Each goroutine locks its own copy -- no mutual exclusion at all. Concurrent map writes will panic.

**Fix:** Always pass `*sync.Mutex` (a pointer), or better, embed the mutex in a struct:
```go
func cacheSet(mu *sync.Mutex, cache map[string]string, key, val string) {
	mu.Lock()
	defer mu.Unlock()
	cache[key] = val
}
```

### Locking Too Broadly

```go
mu.Lock()
result := fetchFromDatabase(userID) // holds the lock during slow I/O
cache[userID] = result
mu.Unlock()
```

**What happens:** All goroutines are serialized through the database call, eliminating concurrency benefits. Your API server handles one request at a time.

**Fix:** Only hold the lock for the shared state access:
```go
result := fetchFromDatabase(userID) // no lock needed here
mu.Lock()
cache[userID] = result
mu.Unlock()
```

## Verify What You Learned

Extend the `APICache` to support a `Delete(endpoint string)` method and a `SetWithTTL(endpoint, body string, ttl time.Duration)` that records an expiration time. Add a `CleanExpired()` method that removes all entries past their TTL. Launch 100 goroutines that randomly set, get, and delete entries, and run with `-race` to confirm there are no data races.

## What's Next
Continue to [02-rwmutex-readers-writers](../02-rwmutex-readers-writers/02-rwmutex-readers-writers.md) to learn how `sync.RWMutex` allows multiple concurrent readers while still protecting writes.

## Summary
- A data race occurs when multiple goroutines access shared state without synchronization and at least one writes
- In API servers, shared in-memory caches are the most common source of data races
- `sync.Mutex` provides mutual exclusion: only one goroutine holds the lock at a time
- Always use `defer mu.Unlock()` immediately after `mu.Lock()` for safety
- Never copy a mutex -- pass it by pointer or embed it in a struct
- Minimize the critical section: hold the lock only while accessing shared state, not during I/O
- Return copies from locked methods to prevent callers from bypassing the mutex
- Use `go run -race` to detect data races during development

## Reference
- [sync.Mutex documentation](https://pkg.go.dev/sync#Mutex)
- [Go Data Race Detector](https://go.dev/doc/articles/race_detector)
- [Go Blog: Share Memory by Communicating](https://go.dev/blog/codelab-share)
