---
difficulty: intermediate
concepts: [sync.Map, Load, Store, LoadOrStore, Delete, Range, concurrent map access]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [sync.Mutex, goroutines, sync.WaitGroup, maps]
---

# 9. sync.Map: Concurrent Access


## Learning Objectives
After completing this exercise, you will be able to:
- **Demonstrate** why concurrent access to a regular Go map panics
- **Use** `sync.Map` methods: `Load`, `Store`, `LoadOrStore`, `Delete`, `Range`
- **Compare** `sync.Map` with a regular map protected by `sync.RWMutex`
- **Decide** when `sync.Map` is appropriate vs. a map with mutex

## Why sync.Map
Go maps are not safe for concurrent use. If multiple goroutines read and write a map simultaneously without synchronization, the runtime will detect the race and panic with a fatal error: `concurrent map read and map write`. This is a deliberate safety mechanism -- Go crashes loudly rather than silently corrupting data.

The scenario: an HTTP server stores user sessions in memory. Handler goroutines create sessions on login, read sessions to validate requests, update sessions on activity, and delete sessions on logout. All of this happens concurrently across thousands of requests.

The standard fix is wrapping the map with a `sync.RWMutex`. However, `sync.Map` exists for two specific use cases where it outperforms a mutex-protected map:

1. **Append-only maps**: keys are written once and then only read (session creation followed by many reads). `sync.Map` eliminates lock contention on reads.
2. **Disjoint key access**: different goroutines work on different key subsets (each user's session is touched only by that user's requests). `sync.Map` avoids locking the entire map for unrelated operations.

For general-purpose concurrent maps, a regular `map` with `sync.RWMutex` is typically simpler and often faster. Use `sync.Map` only when profiling shows it helps.

## Step 1 -- The Map Panic: Concurrent Session Writes

Multiple HTTP handlers try to write sessions to a regular map simultaneously:

```go
package main

import (
	"fmt"
	"sync"
)

const concurrentUsers = 100

func simulateUnsafeSessionAccess(sessions map[string]string, userID int) {
	sessionID := fmt.Sprintf("sess-%d", userID)
	sessions[sessionID] = fmt.Sprintf("user-%d", userID) // concurrent write -- UNSAFE
	_ = sessions[sessionID]                                // concurrent read -- UNSAFE
}

func demonstrateMapPanic() {
	sessions := make(map[string]string)
	var wg sync.WaitGroup

	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("PANIC: %v\n", r)
			fmt.Println("Regular maps are NOT safe for concurrent session storage!")
		}
	}()

	for i := 0; i < concurrentUsers; i++ {
		wg.Add(1)
		go func(userID int) {
			defer wg.Done()
			simulateUnsafeSessionAccess(sessions, userID)
		}(i)
	}

	wg.Wait()
}

func main() {
	demonstrateMapPanic()
}
```

Expected output:
```
PANIC: concurrent map writes
Regular maps are NOT safe for concurrent session storage!
```

### Intermediate Verification
```bash
go run main.go
```
The program should panic (caught by recover) with a concurrent map access error.

## Step 2 -- Session Store with sync.Map

Build a concurrent session store using `sync.Map`:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type Session struct {
	UserID    string
	CreatedAt time.Time
	Data      map[string]string
}

type SessionStore struct {
	sessions sync.Map
}

func (s *SessionStore) Create(sessionID string, sess Session) {
	s.sessions.Store(sessionID, sess)
}

func (s *SessionStore) Find(sessionID string) (Session, bool) {
	val, ok := s.sessions.Load(sessionID)
	if !ok {
		return Session{}, false
	}
	return val.(Session), true
}

func (s *SessionStore) GetOrCreate(sessionID string, newSession Session) (Session, bool) {
	actual, loaded := s.sessions.LoadOrStore(sessionID, newSession)
	return actual.(Session), loaded
}

func (s *SessionStore) Delete(sessionID string) {
	s.sessions.Delete(sessionID)
}

func (s *SessionStore) CountActive() int {
	count := 0
	s.sessions.Range(func(key, value any) bool {
		count++
		sess := value.(Session)
		fmt.Printf("  Active: %s -> user=%s\n", key, sess.UserID)
		return true
	})
	return count
}

func demonstrateSyncMapOperations() {
	store := &SessionStore{}

	store.Create("sess-abc123", Session{
		UserID:    "user-1",
		CreatedAt: time.Now(),
		Data:      map[string]string{"role": "admin"},
	})
	store.Create("sess-def456", Session{
		UserID:    "user-2",
		CreatedAt: time.Now(),
		Data:      map[string]string{"role": "viewer"},
	})

	if sess, ok := store.Find("sess-abc123"); ok {
		fmt.Printf("Found session: user=%s, role=%s\n", sess.UserID, sess.Data["role"])
	}

	// LoadOrStore: create session only if it does not exist (login idempotency)
	candidate := Session{UserID: "user-2", CreatedAt: time.Now(), Data: map[string]string{"role": "editor"}}
	existing, alreadyExists := store.GetOrCreate("sess-def456", candidate)
	if alreadyExists {
		fmt.Printf("Session already exists: user=%s, role=%s (not overwritten)\n", existing.UserID, existing.Data["role"])
	}

	store.Delete("sess-abc123")
	_, found := store.Find("sess-abc123")
	fmt.Printf("After logout: session found=%v\n", found)

	count := store.CountActive()
	fmt.Printf("Total active sessions: %d\n", count)
}

func main() {
	demonstrateSyncMapOperations()
}
```

Expected output:
```
Found session: user=user-1, role=admin
Session already exists: user=user-2, role=viewer (not overwritten)
After logout: session found=false
  Active: sess-def456 -> user=user-2
Total active sessions: 1
```

### Intermediate Verification
```bash
go run main.go
```
All operations should work correctly.

## Step 3 -- Concurrent Session Management

Simulate a real server with concurrent login, validation, and logout:

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const (
	concurrentUsers     = 50
	validationsPerUser  = 10
)

type Session struct {
	UserID    string
	CreatedAt time.Time
}

type SessionMetrics struct {
	Logins      atomic.Int64
	Validations atomic.Int64
	Logouts     atomic.Int64
}

func simulateUserLifecycle(sessions *sync.Map, userID int, metrics *SessionMetrics) {
	sessKey := fmt.Sprintf("sess-%d", userID)

	sessions.Store(sessKey, Session{
		UserID:    fmt.Sprintf("user-%d", userID),
		CreatedAt: time.Now(),
	})
	metrics.Logins.Add(1)

	for j := 0; j < validationsPerUser; j++ {
		if val, ok := sessions.Load(sessKey); ok {
			_ = val.(Session).UserID
			metrics.Validations.Add(1)
		}
	}

	sessions.Delete(sessKey)
	metrics.Logouts.Add(1)
}

func countRemainingSessions(sessions *sync.Map) int {
	remaining := 0
	sessions.Range(func(_, _ any) bool {
		remaining++
		return true
	})
	return remaining
}

func runConcurrentSessionTest() {
	var sessions sync.Map
	var wg sync.WaitGroup
	metrics := &SessionMetrics{}

	for i := 0; i < concurrentUsers; i++ {
		wg.Add(1)
		go func(userID int) {
			defer wg.Done()
			simulateUserLifecycle(&sessions, userID, metrics)
		}(i)
	}

	wg.Wait()

	remaining := countRemainingSessions(&sessions)

	fmt.Println("=== Session Store Results ===")
	fmt.Printf("Logins:      %d\n", metrics.Logins.Load())
	fmt.Printf("Validations: %d\n", metrics.Validations.Load())
	fmt.Printf("Logouts:     %d\n", metrics.Logouts.Load())
	fmt.Printf("Remaining:   %d (should be 0)\n", remaining)
}

func main() {
	runConcurrentSessionTest()
}
```

Expected output:
```
=== Session Store Results ===
Logins:      50
Validations: 500
Logouts:     50
Remaining:   0 (should be 0)
```

### Intermediate Verification
```bash
go run -race main.go
```
No panics, no data races. All 50 sessions created, validated, and cleaned up.

## Step 4 -- When to Use What: Benchmark Comparison

Compare `sync.Map` vs `map+RWMutex` for different workloads. sync.Map wins when reads dominate and keys are disjoint:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

const (
	opsPerGoroutine = 5000
	prePopulateKeys = 1000
	writeRatio      = 10 // writers do ops/writeRatio operations
)

func keyForIndex(base, ops, iteration, keySpace int) string {
	return fmt.Sprintf("key-%d", (base*ops+iteration)%keySpace)
}

func benchSyncMap(readers, writers, ops int) time.Duration {
	var m sync.Map
	var wg sync.WaitGroup

	for i := 0; i < prePopulateKeys; i++ {
		m.Store(fmt.Sprintf("key-%d", i), i)
	}

	start := time.Now()

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				m.Load(keyForIndex(workerID, ops, j, prePopulateKeys))
			}
		}(i)
	}

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < ops/writeRatio; j++ {
				m.Store(keyForIndex(workerID, ops, j, prePopulateKeys), j)
			}
		}(i)
	}

	wg.Wait()
	return time.Since(start)
}

func benchMutexMap(readers, writers, ops int) time.Duration {
	var mu sync.RWMutex
	m := make(map[string]int)
	var wg sync.WaitGroup

	for i := 0; i < prePopulateKeys; i++ {
		m[fmt.Sprintf("key-%d", i)] = i
	}

	start := time.Now()

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				mu.RLock()
				_ = m[keyForIndex(workerID, ops, j, prePopulateKeys)]
				mu.RUnlock()
			}
		}(i)
	}

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < ops/writeRatio; j++ {
				mu.Lock()
				m[keyForIndex(workerID, ops, j, prePopulateKeys)] = j
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()
	return time.Since(start)
}

func runWorkloadComparison(label string, readers, writers int) {
	fmt.Printf("=== %s ===\n", label)
	syncTime := benchSyncMap(readers, writers, opsPerGoroutine)
	mutexTime := benchMutexMap(readers, writers, opsPerGoroutine)
	fmt.Printf("  sync.Map:    %v\n", syncTime.Round(time.Millisecond))
	fmt.Printf("  map+RWMutex: %v\n", mutexTime.Round(time.Millisecond))
}

func main() {
	runWorkloadComparison("Read-Heavy Workload (90% reads, 10% writes)", 90, 10)
	fmt.Println()
	runWorkloadComparison("Write-Heavy Workload (50% reads, 50% writes)", 50, 50)

	fmt.Println("\nConclusion:")
	fmt.Println("  sync.Map can win for read-heavy, disjoint-key workloads.")
	fmt.Println("  map+RWMutex is simpler and often faster for general use.")
	fmt.Println("  Profile your actual workload before choosing sync.Map.")
}
```

Expected output (times vary):
```
=== Read-Heavy Workload (90% reads, 10% writes) ===
  sync.Map:    12ms
  map+RWMutex: 18ms

=== Write-Heavy Workload (50% reads, 50% writes) ===
  sync.Map:    25ms
  map+RWMutex: 15ms

Conclusion:
  sync.Map can win for read-heavy, disjoint-key workloads.
  map+RWMutex is simpler and often faster for general use.
  Profile your actual workload before choosing sync.Map.
```

### Intermediate Verification
```bash
go run main.go
```
Read-heavy should favor sync.Map; write-heavy should favor map+RWMutex.

## Common Mistakes

### Using sync.Map for Everything

**Wrong:** Replacing all concurrent maps with `sync.Map` blindly.

**Reality:** `sync.Map` is optimized for two patterns (append-only, disjoint keys). For general concurrent map access, `map+RWMutex` is simpler and often faster. A session store with mostly reads is a good fit. A metrics counter that every goroutine writes to is not.

### Type Assertions Everywhere

```go
val, _ := sessions.Load("sess-abc123")
sess := val.(Session) // type assertion on every access -- runtime panic if wrong type
```

**Reality:** `sync.Map` stores `any` types, requiring type assertions on every read. If your map is type-homogeneous (all values are `Session`), a generic `map[string]Session` with a mutex is more ergonomic and type-safe. The compiler catches type errors at build time instead of runtime.

### Mixing Range with Delete

```go
sessions.Range(func(key, value any) bool {
    sess := value.(Session)
    if time.Since(sess.CreatedAt) > 24*time.Hour {
        sessions.Delete(key) // safe (no panic), but order is non-deterministic
    }
    return true
})
```

Deleting during Range is safe (no panic), but the deleted key may or may not be visited by subsequent Range iterations. The behavior is non-deterministic. For session cleanup, this is usually acceptable because the next cleanup pass will catch anything missed.

### Assuming Range Sees a Consistent Snapshot
`Range` does not take a snapshot. Other goroutines can `Store` or `Delete` entries during Range execution. If you need a consistent snapshot, use a regular map with a read lock.

## Verify What You Learned

Build a concurrent session store that supports:
- `GetOrCreate(sessionID string, factory func() Session) Session`: load from store or create atomically using `LoadOrStore`
- `Touch(sessionID string)`: update the `LastAccess` timestamp
- `CleanExpired(maxAge time.Duration) int`: remove all sessions older than maxAge, return count removed

Test with 100 concurrent goroutines performing random operations. Compare the implementation with both `sync.Map` and `map+RWMutex`. Verify correctness with `-race`.

## What's Next
Continue to [10-build-thread-safe-counter](../10-build-thread-safe-counter/10-build-thread-safe-counter.md) to build a comprehensive metrics system using all sync primitives you have learned.

## Summary
- Regular Go maps panic under concurrent read-write access
- `sync.Map` provides `Load`, `Store`, `LoadOrStore`, `Delete`, and `Range` for concurrent access
- `sync.Map` excels at read-heavy workloads with stable or disjoint keys -- session stores, config caches, service registries
- For general concurrent maps, prefer `map` + `sync.RWMutex` -- it is simpler, type-safe, and often faster
- `sync.Map` stores `any` types, requiring type assertions on read -- no compile-time type safety
- `Range` does not provide a consistent snapshot -- entries may be added or removed during iteration
- Profile before choosing `sync.Map` -- do not use it as a default

## Reference
- [sync.Map documentation](https://pkg.go.dev/sync#Map)
- [Go Blog: Maps in Action](https://go.dev/blog/maps)
- [sync.Map GopherCon Talk](https://www.youtube.com/watch?v=C1EtfDnsdDs)
