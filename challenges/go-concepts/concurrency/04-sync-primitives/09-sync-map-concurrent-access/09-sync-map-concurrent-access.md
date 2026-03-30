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

func main() {
	sessions := make(map[string]string)
	var wg sync.WaitGroup

	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("PANIC: %v\n", r)
			fmt.Println("Regular maps are NOT safe for concurrent session storage!")
		}
	}()

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(userID int) {
			defer wg.Done()
			sessionID := fmt.Sprintf("sess-%d", userID)
			sessions[sessionID] = fmt.Sprintf("user-%d", userID) // concurrent write -- UNSAFE
			_ = sessions[sessionID]                                // concurrent read -- UNSAFE
		}(i)
	}

	wg.Wait()
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

func main() {
	var sessions sync.Map

	// Create sessions
	sessions.Store("sess-abc123", Session{
		UserID:    "user-1",
		CreatedAt: time.Now(),
		Data:      map[string]string{"role": "admin"},
	})
	sessions.Store("sess-def456", Session{
		UserID:    "user-2",
		CreatedAt: time.Now(),
		Data:      map[string]string{"role": "viewer"},
	})

	// Load a session
	val, ok := sessions.Load("sess-abc123")
	if ok {
		sess := val.(Session)
		fmt.Printf("Found session: user=%s, role=%s\n", sess.UserID, sess.Data["role"])
	}

	// LoadOrStore: create session only if it does not exist (login idempotency)
	newSession := Session{UserID: "user-2", CreatedAt: time.Now(), Data: map[string]string{"role": "editor"}}
	actual, loaded := sessions.LoadOrStore("sess-def456", newSession)
	if loaded {
		existing := actual.(Session)
		fmt.Printf("Session already exists: user=%s, role=%s (not overwritten)\n", existing.UserID, existing.Data["role"])
	}

	// Delete a session (logout)
	sessions.Delete("sess-abc123")
	_, ok = sessions.Load("sess-abc123")
	fmt.Printf("After logout: session found=%v\n", ok)

	// Range: count all active sessions
	count := 0
	sessions.Range(func(key, value any) bool {
		count++
		sess := value.(Session)
		fmt.Printf("  Active: %s -> user=%s\n", key, sess.UserID)
		return true
	})
	fmt.Printf("Total active sessions: %d\n", count)
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

type Session struct {
	UserID    string
	CreatedAt time.Time
}

func main() {
	var sessions sync.Map
	var wg sync.WaitGroup
	var logins, validations, logouts atomic.Int64

	// Simulate 50 concurrent users
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(userID int) {
			defer wg.Done()
			sessKey := fmt.Sprintf("sess-%d", userID)

			// Login: create session
			sessions.Store(sessKey, Session{
				UserID:    fmt.Sprintf("user-%d", userID),
				CreatedAt: time.Now(),
			})
			logins.Add(1)

			// Validate: read session 10 times (simulating multiple requests)
			for j := 0; j < 10; j++ {
				if val, ok := sessions.Load(sessKey); ok {
					_ = val.(Session).UserID
					validations.Add(1)
				}
			}

			// Logout: delete session
			sessions.Delete(sessKey)
			logouts.Add(1)
		}(i)
	}

	wg.Wait()

	remaining := 0
	sessions.Range(func(_, _ any) bool {
		remaining++
		return true
	})

	fmt.Println("=== Session Store Results ===")
	fmt.Printf("Logins:      %d\n", logins.Load())
	fmt.Printf("Validations: %d\n", validations.Load())
	fmt.Printf("Logouts:     %d\n", logouts.Load())
	fmt.Printf("Remaining:   %d (should be 0)\n", remaining)
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

func benchSyncMap(readers, writers int, ops int) time.Duration {
	var m sync.Map
	var wg sync.WaitGroup

	// Pre-populate
	for i := 0; i < 1000; i++ {
		m.Store(fmt.Sprintf("key-%d", i), i)
	}

	start := time.Now()

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				m.Load(fmt.Sprintf("key-%d", (id*ops+j)%1000))
			}
		}(i)
	}

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < ops/10; j++ {
				m.Store(fmt.Sprintf("key-%d", (id*ops+j)%1000), j)
			}
		}(i)
	}

	wg.Wait()
	return time.Since(start)
}

func benchMutexMap(readers, writers int, ops int) time.Duration {
	var mu sync.RWMutex
	m := make(map[string]int)
	var wg sync.WaitGroup

	for i := 0; i < 1000; i++ {
		m[fmt.Sprintf("key-%d", i)] = i
	}

	start := time.Now()

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				mu.RLock()
				_ = m[fmt.Sprintf("key-%d", (id*ops+j)%1000)]
				mu.RUnlock()
			}
		}(i)
	}

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < ops/10; j++ {
				mu.Lock()
				m[fmt.Sprintf("key-%d", (id*ops+j)%1000)] = j
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()
	return time.Since(start)
}

func main() {
	const ops = 5000

	fmt.Println("=== Read-Heavy Workload (90% reads, 10% writes) ===")
	syncTime := benchSyncMap(90, 10, ops)
	mutexTime := benchMutexMap(90, 10, ops)
	fmt.Printf("  sync.Map:    %v\n", syncTime.Round(time.Millisecond))
	fmt.Printf("  map+RWMutex: %v\n", mutexTime.Round(time.Millisecond))

	fmt.Println("\n=== Write-Heavy Workload (50% reads, 50% writes) ===")
	syncTime = benchSyncMap(50, 50, ops)
	mutexTime = benchMutexMap(50, 50, ops)
	fmt.Printf("  sync.Map:    %v\n", syncTime.Round(time.Millisecond))
	fmt.Printf("  map+RWMutex: %v\n", mutexTime.Round(time.Millisecond))

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
