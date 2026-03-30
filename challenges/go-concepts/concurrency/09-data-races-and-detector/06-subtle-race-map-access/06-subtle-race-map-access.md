---
difficulty: intermediate
concepts: [concurrent map access, fatal error, sync.Mutex, sync.RWMutex, session store, production crash]
tools: [go]
estimated_time: 30m
bloom_level: apply
---

# 6. Subtle Race: Map Access


## Learning Objectives
After completing this exercise, you will be able to:
- **Reproduce** the "concurrent map writes" fatal error that crashes Go servers in production
- **Explain** why this is NOT detected by the race detector but causes a FATAL crash
- **Fix** concurrent map access using `sync.RWMutex` for read-heavy workloads
- **Build** a thread-safe user session store suitable for production use

## Why Map Races Are Special

Unlike the counter race from exercises 01-05 (which produces silently wrong results), concurrent map access in Go causes the program to **crash immediately** with a fatal error. The Go runtime detects concurrent map read/write or write/write operations and terminates the process.

This is NOT a data race in the traditional sense. The runtime's built-in map concurrency check is a separate mechanism from the `-race` flag. It is a **hard crash**: no recovery, no graceful shutdown, no error handling. Your server goes down instantly.

This is one of Go's most common production crashes. It typically appears when:
- Multiple HTTP handlers read/write a shared session map
- A cache is accessed without synchronization
- Configuration is reloaded while being read

The Go designers intentionally chose a crash over silent corruption, because a corrupted map can cause memory safety violations, infinite loops during hash table traversal, and corruption of unrelated memory.

## Step 1 -- Build the Racy Session Store

Create a user session store where multiple HTTP handlers read and write sessions concurrently. This simulates a real authentication layer:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

const (
	writerHandlers      = 10
	sessionsPerHandler  = 100
	sessionDuration     = 1 * time.Hour
)

// Session represents a user authentication session.
type Session struct {
	UserID    string
	Token     string
	ExpiresAt time.Time
}

// RacySessionStore demonstrates a session store WITHOUT synchronization.
// BUG: concurrent map writes cause a fatal crash.
type RacySessionStore struct {
	sessions map[string]Session
}

func NewRacySessionStore() *RacySessionStore {
	return &RacySessionStore{sessions: make(map[string]Session)}
}

// Create writes a session to the unprotected map.
// FATAL: concurrent calls cause "fatal error: concurrent map writes".
func (s *RacySessionStore) Create(token string, session Session) {
	s.sessions[token] = session
}

func (s *RacySessionStore) Count() int {
	return len(s.sessions)
}

func simulateConcurrentLogins(store *RacySessionStore) {
	var wg sync.WaitGroup

	for i := 0; i < writerHandlers; i++ {
		wg.Add(1)
		go func(handlerID int) {
			defer wg.Done()
			for j := 0; j < sessionsPerHandler; j++ {
				token := fmt.Sprintf("token-%d-%d", handlerID, j)
				store.Create(token, Session{
					UserID:    fmt.Sprintf("user-%d", handlerID),
					Token:     token,
					ExpiresAt: time.Now().Add(sessionDuration),
				})
			}
		}(i)
	}

	wg.Wait()
	fmt.Printf("Sessions stored: %d\n", store.Count())
}

func main() {
	fmt.Println("=== Concurrent Map Write Crash ===")
	fmt.Println("This WILL crash with: fatal error: concurrent map writes")
	fmt.Println()

	store := NewRacySessionStore()
	simulateConcurrentLogins(store)
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
fatal error: concurrent map writes

goroutine 19 [running]:
...
exit status 2
```

You may need to run it a few times. The crash is non-deterministic but highly likely with 10 goroutines. Even though every goroutine writes to **different keys**, the map's internal hash table is a shared data structure: bucket resizing during growth affects all keys.

## Step 2 -- The Read+Write Crash

Even reading while another goroutine writes causes a fatal crash. This surprises many developers who assume "reading is safe":

```go
package main

import (
	"fmt"
	"sync"
)

const (
	prePopulatedSessions = 100
	concurrentWriters    = 5
	concurrentReaders    = 5
	operationsPerWorker  = 200
)

// Session represents a user authentication session.
type Session struct {
	UserID    string
	Token     string
}

// RacySessionStore demonstrates a session store WITHOUT synchronization.
// BUG: concurrent reads and writes cause a fatal crash.
type RacySessionStore struct {
	sessions map[string]Session
}

func NewRacySessionStore() *RacySessionStore {
	return &RacySessionStore{sessions: make(map[string]Session)}
}

func (s *RacySessionStore) Create(token string, session Session) {
	s.sessions[token] = session // FATAL when called concurrently with Get
}

func (s *RacySessionStore) Get(token string) Session {
	return s.sessions[token] // FATAL when called concurrently with Create
}

func (s *RacySessionStore) Count() int {
	return len(s.sessions)
}

func prePopulate(store *RacySessionStore, count int) {
	for i := 0; i < count; i++ {
		token := fmt.Sprintf("token-%d", i)
		store.Create(token, Session{
			UserID: fmt.Sprintf("user-%d", i),
			Token:  token,
		})
	}
}

func launchWriters(store *RacySessionStore, wg *sync.WaitGroup) {
	for i := 0; i < concurrentWriters; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < operationsPerWorker; j++ {
				token := fmt.Sprintf("new-token-%d-%d", id, j)
				store.Create(token, Session{
					UserID: fmt.Sprintf("user-%d", id),
					Token:  token,
				})
			}
		}(i)
	}
}

func launchReaders(store *RacySessionStore, wg *sync.WaitGroup) {
	for i := 0; i < concurrentReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < operationsPerWorker; j++ {
				token := fmt.Sprintf("token-%d", j%prePopulatedSessions)
				_ = store.Get(token) // FATAL: concurrent read + write
			}
		}()
	}
}

func main() {
	fmt.Println("=== Concurrent Map Read + Write Crash ===")
	fmt.Println("This WILL crash with: fatal error: concurrent map read and map write")
	fmt.Println()

	store := NewRacySessionStore()
	prePopulate(store, prePopulatedSessions)

	var wg sync.WaitGroup
	launchWriters(store, &wg)
	launchReaders(store, &wg)
	wg.Wait()

	fmt.Printf("Sessions: %d\n", store.Count())
}
```

### Verification
```bash
go run main.go
```
Expected:
```
fatal error: concurrent map read and map write
```

In a real server, this crash happens when the authentication middleware reads the session map while a login handler writes to it. The server goes down without warning.

## Step 3 -- Fix with sync.RWMutex

For a session store, reads vastly outnumber writes. `sync.RWMutex` allows multiple concurrent readers while writers get exclusive access:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type Session struct {
	UserID    string
	Token     string
	ExpiresAt time.Time
}

type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]Session
}

func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions: make(map[string]Session),
	}
}

func (s *SessionStore) Create(token string, session Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[token] = session
}

func (s *SessionStore) Get(token string) (Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[token]
	return sess, ok
}

func (s *SessionStore) Delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

func (s *SessionStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

func (s *SessionStore) CleanExpired() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	cleaned := 0
	now := time.Now()
	for token, sess := range s.sessions {
		if now.After(sess.ExpiresAt) {
			delete(s.sessions, token)
			cleaned++
		}
	}
	return cleaned
}

func main() {
	store := NewSessionStore()
	var wg sync.WaitGroup

	fmt.Println("=== Thread-Safe Session Store ===")

	// Writers: login handlers creating sessions.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(handlerID int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				token := fmt.Sprintf("token-%d-%d", handlerID, j)
				store.Create(token, Session{
					UserID:    fmt.Sprintf("user-%d", handlerID),
					Token:     token,
					ExpiresAt: time.Now().Add(1 * time.Hour),
				})
			}
		}(i)
	}

	// Readers: auth middleware checking sessions.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			found := 0
			for j := 0; j < 200; j++ {
				token := fmt.Sprintf("token-%d-%d", readerID%10, j%100)
				if _, ok := store.Get(token); ok {
					found++
				}
			}
		}(i)
	}

	wg.Wait()

	fmt.Printf("  Total sessions: %d (expected 1000)\n", store.Count())
	fmt.Println()
	fmt.Println("Why RWMutex for session stores:")
	fmt.Println("  - Auth middleware checks sessions on EVERY request (reads)")
	fmt.Println("  - Login/logout create/delete sessions occasionally (writes)")
	fmt.Println("  - Ratio is ~100:1 reads to writes")
	fmt.Println("  - RWMutex allows all readers to proceed simultaneously")
	fmt.Println("  - Only writers need exclusive access")
}
```

Key design points:
- `RLock()`/`RUnlock()` for read operations: multiple readers proceed simultaneously
- `Lock()`/`Unlock()` for write operations: exclusive access, blocks all readers and writers
- ALL map operations are protected, including `len()`, `delete()`, and iteration
- `CleanExpired()` takes a write lock for the entire cleanup operation because it modifies and iterates the map

### Verification
```bash
go run -race main.go
```
Expected: 1000 sessions, zero race warnings, no crashes.

## Step 4 -- Demonstrate the Read Concurrency Advantage

Show that `RWMutex` allows parallel reads while `Mutex` serializes everything:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

const (
	preloadEntries    = 1000
	writesPerWriter   = 100
	readsPerReader    = 1000
	benchReaders      = 50
	benchWriters      = 2
)

// LockBenchResult holds the timing outcome of a lock strategy benchmark.
type LockBenchResult struct {
	Label   string
	Elapsed time.Duration
}

// MutexMapBench benchmarks sync.Mutex on a read-heavy map workload.
type MutexMapBench struct {
	mu sync.Mutex
	m  map[int]int
}

func NewMutexMapBench() *MutexMapBench {
	m := make(map[int]int, preloadEntries)
	for i := 0; i < preloadEntries; i++ {
		m[i] = i
	}
	return &MutexMapBench{m: m}
}

func (b *MutexMapBench) Run(readers, writers int) LockBenchResult {
	var wg sync.WaitGroup
	start := time.Now()

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < writesPerWriter; j++ {
				b.mu.Lock()
				b.m[preloadEntries+id*writesPerWriter+j] = j
				b.mu.Unlock()
			}
		}(w)
	}

	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < readsPerReader; j++ {
				b.mu.Lock()
				_ = b.m[j%preloadEntries]
				b.mu.Unlock()
			}
		}()
	}

	wg.Wait()
	return LockBenchResult{"Mutex", time.Since(start)}
}

// RWMutexMapBench benchmarks sync.RWMutex on a read-heavy map workload.
type RWMutexMapBench struct {
	mu sync.RWMutex
	m  map[int]int
}

func NewRWMutexMapBench() *RWMutexMapBench {
	m := make(map[int]int, preloadEntries)
	for i := 0; i < preloadEntries; i++ {
		m[i] = i
	}
	return &RWMutexMapBench{m: m}
}

func (b *RWMutexMapBench) Run(readers, writers int) LockBenchResult {
	var wg sync.WaitGroup
	start := time.Now()

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < writesPerWriter; j++ {
				b.mu.Lock()
				b.m[preloadEntries+id*writesPerWriter+j] = j
				b.mu.Unlock()
			}
		}(w)
	}

	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < readsPerReader; j++ {
				b.mu.RLock()
				_ = b.m[j%preloadEntries]
				b.mu.RUnlock()
			}
		}()
	}

	wg.Wait()
	return LockBenchResult{"RWMutex", time.Since(start)}
}

func printBenchComparison(mutex, rwMutex LockBenchResult) {
	fmt.Printf("  %-10s %v\n", mutex.Label+":", mutex.Elapsed)
	fmt.Printf("  %-10s %v\n", rwMutex.Label+":", rwMutex.Elapsed)
	fmt.Println()
	fmt.Println("RWMutex wins because 50 readers proceed in parallel,")
	fmt.Println("while Mutex forces all 50 to take turns.")
}

func main() {
	fmt.Printf("=== Read-Heavy Workload: %d readers, %d writers ===\n\n",
		benchReaders, benchWriters)

	mutexBench := NewMutexMapBench()
	mutexResult := mutexBench.Run(benchReaders, benchWriters)

	rwBench := NewRWMutexMapBench()
	rwResult := rwBench.Run(benchReaders, benchWriters)

	printBenchComparison(mutexResult, rwResult)
}
```

### Verification
```bash
go run main.go
```

With 50 readers and 2 writers, `RWMutex` allows all readers to proceed simultaneously. `Mutex` serializes all 50 readers, making them take turns.

## Common Mistakes

### Thinking "I Only Write to Different Keys"
**Wrong assumption:** "Each goroutine writes to different keys, so there is no conflict."
**Reality:** The map's internal hash table is shared. Even if goroutines use different keys, internal bucket restructuring during growth affects the entire map. Any concurrent write triggers the fatal error.

### Forgetting to Protect Map Reads
```go
mu.Lock()
m[key] = value
mu.Unlock()
// ...
val := m[key] // BUG: read without lock -- FATAL ERROR
```
**Fix:** Protect ALL map operations (read, write, delete, range) with the same mutex.

### Using Mutex When RWMutex Would Help
For session stores, caches, and configuration maps where reads dominate, `sync.Mutex` forces unnecessary serialization of readers. Use `sync.RWMutex` when the read-to-write ratio is high (10:1 or more).

### Using RWMutex When Writes Are Frequent
`sync.RWMutex` has higher overhead per operation than `sync.Mutex` due to writer starvation prevention. If writes are frequent (more than 10% of operations), `sync.Mutex` is simpler and often faster.

## Verify What You Learned

1. Confirm zero race warnings from all safe versions with `go run -race main.go`
2. Why does Go crash on concurrent map access instead of producing wrong results?
3. When would you use `sync.RWMutex` over `sync.Mutex`?
4. Does writing to different keys in a regular map avoid the crash? Why or why not?

## What's Next
Continue to [07-race-in-closure-loops](../07-race-in-closure-loops/07-race-in-closure-loops.md) to explore the classic closure-in-loop race bug.

## Summary
- Concurrent map access in Go causes a **fatal error**, not silently wrong results
- Both concurrent write+write AND concurrent read+write are fatal crashes
- This is NOT detected by `-race`: it is a runtime check that kills the process immediately
- This is one of Go's most common production crashes, especially in session stores and caches
- Fix with `sync.RWMutex` for read-heavy workloads: multiple readers proceed simultaneously, writers get exclusive access
- Writing to different keys does NOT make concurrent access safe (internal structure is shared)
- Protect ALL map operations: reads, writes, deletes, and iteration
- Use `sync.Mutex` as the default; upgrade to `sync.RWMutex` when reads vastly outnumber writes

## Reference
- [Go Blog: Go Maps in Action](https://go.dev/blog/maps)
- [sync.RWMutex Documentation](https://pkg.go.dev/sync#RWMutex)
- [Go FAQ: Why are map operations not defined to be atomic?](https://go.dev/doc/faq#atomic_maps)
