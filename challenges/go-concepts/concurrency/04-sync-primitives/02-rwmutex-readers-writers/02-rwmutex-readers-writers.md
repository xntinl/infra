---
difficulty: intermediate
concepts: [sync.RWMutex, RLock, RUnlock, concurrent reads, exclusive writes, read-heavy optimization]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [sync.Mutex, goroutines, WaitGroup]
---

# 2. RWMutex: Readers-Writers


## Learning Objectives
After completing this exercise, you will be able to:
- **Explain** the difference between `sync.Mutex` and `sync.RWMutex`
- **Use** `RLock/RUnlock` for concurrent read access
- **Use** `Lock/Unlock` for exclusive write access
- **Compare** performance of `Mutex` vs `RWMutex` for read-heavy workloads

## Why RWMutex
A regular `sync.Mutex` serializes all access -- even when multiple goroutines only need to read. This is unnecessarily restrictive for read-heavy workloads where writes are infrequent.

Consider a configuration store in a real application. Dozens of goroutines constantly check feature flags, read timeout settings, and look up service URLs. But configuration reloads happen maybe once every few minutes. Forcing all those readers to wait for each other wastes concurrency and adds latency to every request.

`sync.RWMutex` solves this with two levels of locking:
- **Read lock** (`RLock`): multiple goroutines can hold a read lock simultaneously. They only block if a writer holds the exclusive lock.
- **Write lock** (`Lock`): only one goroutine can hold the write lock. It blocks until all readers release their read locks, and no new readers can acquire a read lock while a writer is waiting.

This makes `RWMutex` ideal for data structures that are read far more often than they are written -- configuration stores, feature flag systems, routing tables, and similar shared state.

## Step 1 -- Build a Configuration Store

A configuration store holds application settings that many goroutines read but only an admin endpoint or config watcher updates:

```go
package main

import (
	"fmt"
	"sync"
)

const featureKeyPrefix = "feature."

type ConfigStore struct {
	mu       sync.RWMutex
	settings map[string]string
}

func NewConfigStore() *ConfigStore {
	return &ConfigStore{settings: make(map[string]string)}
}

func (cs *ConfigStore) Get(key string) (string, bool) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	val, ok := cs.settings[key]
	return val, ok
}

func (cs *ConfigStore) Set(key, value string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.settings[key] = value
}

func (cs *ConfigStore) IsFeatureEnabled(feature string) bool {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.settings[featureKeyPrefix+feature] == "true"
}

func (cs *ConfigStore) Reload(newConfig map[string]string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.settings = make(map[string]string, len(newConfig))
	for k, v := range newConfig {
		cs.settings[k] = v
	}
}

// Snapshot returns a COPY to avoid exposing internal state.
func (cs *ConfigStore) Snapshot() map[string]string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	result := make(map[string]string, len(cs.settings))
	for k, v := range cs.settings {
		result[k] = v
	}
	return result
}

func loadInitialConfig(store *ConfigStore) {
	store.Set("db.host", "postgres.internal:5432")
	store.Set("db.pool_size", "25")
	store.Set("feature.dark_mode", "true")
	store.Set("feature.beta_api", "false")
	store.Set("http.timeout", "30s")
}

func printConfigSummary(store *ConfigStore) {
	host, _ := store.Get("db.host")
	fmt.Printf("db.host = %s\n", host)
	fmt.Printf("dark_mode enabled: %v\n", store.IsFeatureEnabled("dark_mode"))
	fmt.Printf("beta_api enabled: %v\n", store.IsFeatureEnabled("beta_api"))

	snap := store.Snapshot()
	fmt.Printf("\nAll settings (%d entries):\n", len(snap))
	for key, value := range snap {
		fmt.Printf("  %s = %s\n", key, value)
	}
}

func main() {
	store := NewConfigStore()
	loadInitialConfig(store)
	printConfigSummary(store)
}
```

Expected output:
```
db.host = postgres.internal:5432
dark_mode enabled: true
beta_api enabled: false

All settings (5 entries):
  db.host = postgres.internal:5432
  db.pool_size = 25
  feature.dark_mode = true
  feature.beta_api = false
  http.timeout = 30s
```

### Intermediate Verification
```bash
go run main.go
```
The basic operations test should print correct settings and feature flag states.

## Step 2 -- Demonstrate Concurrent Readers

In production, many goroutines check feature flags simultaneously. With `RLock`, they all proceed in parallel instead of waiting for each other. To show the difference, this example holds the read lock for the full duration of simulated work. With a regular `Mutex`, this would serialize all 10 handlers; with `RWMutex`, they run concurrently:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

const (
	concurrentReaders   = 10
	simulatedWorkDelay  = 100 * time.Millisecond
)

type ConfigStore struct {
	mu       sync.RWMutex
	settings map[string]string
}

func NewConfigStore() *ConfigStore {
	return &ConfigStore{settings: make(map[string]string)}
}

// GetAndProcess simulates reading config and doing work while holding the read lock.
// In real code, you would release the lock sooner. This exaggerates the effect to
// demonstrate that multiple readers can hold an RLock simultaneously.
func (cs *ConfigStore) GetAndProcess(key string) string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	val := cs.settings[key]
	time.Sleep(simulatedWorkDelay)
	return val
}

func (cs *ConfigStore) Set(key, value string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.settings[key] = value
}

func demonstrateConcurrentReaders(store *ConfigStore) time.Duration {
	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < concurrentReaders; i++ {
		wg.Add(1)
		go func(handlerID int) {
			defer wg.Done()
			val := store.GetAndProcess("feature.dark_mode")
			fmt.Printf("Handler %d: dark_mode=%s (at %v)\n", handlerID, val, time.Since(start).Round(time.Millisecond))
		}(i)
	}

	wg.Wait()
	return time.Since(start)
}

func main() {
	store := NewConfigStore()
	store.Set("feature.dark_mode", "true")

	elapsed := demonstrateConcurrentReaders(store)

	fmt.Printf("\nAll %d handlers finished in %v\n", concurrentReaders, elapsed.Round(time.Millisecond))
	fmt.Println("(With a regular Mutex, this would take ~1s because only one goroutine holds the lock at a time.)")
	fmt.Println("(With RWMutex, ~100ms because all readers hold RLock simultaneously.)")
}
```

Expected output:
```
Handler 0: dark_mode=true (at 100ms)
Handler 1: dark_mode=true (at 100ms)
...
All 10 handlers finished in 100ms
(With a regular Mutex, this would take ~1s because only one goroutine holds the lock at a time.)
(With RWMutex, ~100ms because all readers hold RLock simultaneously.)
```

### Intermediate Verification
```bash
go run main.go
```
All 10 handlers should finish in approximately 100ms, proving they ran concurrently. With a regular `Mutex` and the lock held during the sleep, this would take ~1s because each handler would wait for the previous one to release the lock.

## Step 3 -- Config Reload Blocks All Readers

When the configuration watcher reloads settings, it acquires the write lock. All readers block until the reload completes, ensuring they never see a partially updated config:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

const (
	readerCount         = 3
	writerAcquireDelay  = 10 * time.Millisecond
	reloadSimDelay      = 200 * time.Millisecond
)

type ConfigStore struct {
	mu       sync.RWMutex
	settings map[string]string
}

func NewConfigStore(initial map[string]string) *ConfigStore {
	settings := make(map[string]string, len(initial))
	for k, v := range initial {
		settings[k] = v
	}
	return &ConfigStore{settings: settings}
}

func (cs *ConfigStore) Get(key string) (string, bool) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	val, ok := cs.settings[key]
	return val, ok
}

func (cs *ConfigStore) Reload(newConfig map[string]string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	fmt.Printf("[%v] Config reload: acquired write lock, updating %d settings...\n",
		time.Now().Format("15:04:05.000"), len(newConfig))
	time.Sleep(reloadSimDelay)
	cs.settings = make(map[string]string, len(newConfig))
	for k, v := range newConfig {
		cs.settings[k] = v
	}
	fmt.Printf("[%v] Config reload: complete\n", time.Now().Format("15:04:05.000"))
}

func startConfigReload(store *ConfigStore, wg *sync.WaitGroup, newConfig map[string]string) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		store.Reload(newConfig)
	}()
}

func startBlockedReaders(store *ConfigStore, wg *sync.WaitGroup, start time.Time) {
	for i := 0; i < readerCount; i++ {
		wg.Add(1)
		go func(handlerID int) {
			defer wg.Done()
			fmt.Printf("[%v] Handler %d: waiting for read lock...\n", time.Since(start).Round(time.Millisecond), handlerID)
			val, _ := store.Get("db.host")
			fmt.Printf("[%v] Handler %d: db.host=%s\n", time.Since(start).Round(time.Millisecond), handlerID, val)
		}(i)
	}
}

func main() {
	store := NewConfigStore(map[string]string{
		"db.host": "old-host:5432",
	})
	var wg sync.WaitGroup
	start := time.Now()

	newConfig := map[string]string{
		"db.host": "new-host:5432",
		"db.pool": "50",
	}
	startConfigReload(store, &wg, newConfig)

	time.Sleep(writerAcquireDelay) // let writer acquire lock first

	startBlockedReaders(store, &wg, start)

	wg.Wait()
	fmt.Println("\nReaders saw the NEW config because they waited for the reload to finish.")
}
```

Expected output:
```
Config reload: acquired write lock, updating 2 settings...
[10ms] Handler 0: waiting for read lock...
[10ms] Handler 1: waiting for read lock...
[10ms] Handler 2: waiting for read lock...
Config reload: complete
[210ms] Handler 0: db.host=new-host:5432
[210ms] Handler 1: db.host=new-host:5432
[210ms] Handler 2: db.host=new-host:5432

Readers saw the NEW config because they waited for the reload to finish.
```

### Intermediate Verification
```bash
go run main.go
```
Readers should start waiting around 10ms and only succeed after ~200ms when the reload finishes.

## Step 4 -- Performance Comparison: Mutex vs RWMutex

The real payoff of RWMutex shows in benchmarks. This program simulates a read-heavy workload (feature flag checks) and compares both approaches:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

const (
	benchReaders       = 100
	benchWriters       = 2
	opsPerGoroutine    = 10000
	writeRatio         = 10 // writers do ops/writeRatio operations
)

func runReadWriteWorkload(readFn, writeFn func(), readers, writers, readOps, writeOps int) time.Duration {
	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < readOps; j++ {
				readFn()
			}
		}()
	}

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < writeOps; j++ {
				writeFn()
			}
		}()
	}

	wg.Wait()
	return time.Since(start)
}

func benchMutex(readers, writers, ops int) time.Duration {
	var mu sync.Mutex
	config := map[string]string{"feature.dark_mode": "true"}

	readFn := func() { mu.Lock(); _ = config["feature.dark_mode"]; mu.Unlock() }
	writeFn := func() { mu.Lock(); config["feature.dark_mode"] = "true"; mu.Unlock() }

	return runReadWriteWorkload(readFn, writeFn, readers, writers, ops, ops/writeRatio)
}

func benchRWMutex(readers, writers, ops int) time.Duration {
	var mu sync.RWMutex
	config := map[string]string{"feature.dark_mode": "true"}

	readFn := func() { mu.RLock(); _ = config["feature.dark_mode"]; mu.RUnlock() }
	writeFn := func() { mu.Lock(); config["feature.dark_mode"] = "true"; mu.Unlock() }

	return runReadWriteWorkload(readFn, writeFn, readers, writers, ops, ops/writeRatio)
}

func printBenchResults(mutexTime, rwTime time.Duration) {
	fmt.Printf("Mutex:   %v\n", mutexTime.Round(time.Millisecond))
	fmt.Printf("RWMutex: %v\n", rwTime.Round(time.Millisecond))

	if rwTime < mutexTime {
		speedup := float64(mutexTime) / float64(rwTime)
		fmt.Printf("\nRWMutex is %.1fx faster for this read-heavy config workload.\n", speedup)
	} else {
		fmt.Println("\nMutex was faster (this can happen on machines with few cores).")
	}

	fmt.Println("\nRule of thumb: use RWMutex when reads outnumber writes by 10:1 or more.")
}

func main() {
	fmt.Printf("Benchmark: %d readers, %d writers, %d ops/reader\n\n", benchReaders, benchWriters, opsPerGoroutine)

	mutexTime := benchMutex(benchReaders, benchWriters, opsPerGoroutine)
	rwTime := benchRWMutex(benchReaders, benchWriters, opsPerGoroutine)

	printBenchResults(mutexTime, rwTime)
}
```

Expected output (times vary by machine):
```
Benchmark: 100 readers, 2 writers, 10000 ops/reader

Mutex:   45ms
RWMutex: 15ms

RWMutex is 3.0x faster for this read-heavy config workload.

Rule of thumb: use RWMutex when reads outnumber writes by 10:1 or more.
```

### Intermediate Verification
```bash
go run main.go
```
RWMutex should be noticeably faster for the read-heavy workload.

## Common Mistakes

### Using Lock When RLock Suffices

```go
func (cs *ConfigStore) Get(key string) string {
	cs.mu.Lock() // WRONG: exclusive lock for a read-only operation
	defer cs.mu.Unlock()
	return cs.settings[key]
}
```

**What happens:** Readers serialize unnecessarily, losing the concurrency benefit of RWMutex. You have essentially turned your RWMutex into a regular Mutex. Every feature flag check blocks every other handler.

**Fix:** Use `RLock/RUnlock` for read-only operations.

### Upgrading RLock to Lock

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var mu sync.RWMutex
	config := map[string]string{"db.host": ""}

	mu.RLock()
	val := config["db.host"]
	if val == "" {
		mu.Lock() // DEADLOCK: cannot upgrade RLock to Lock
		config["db.host"] = "localhost:5432"
		mu.Unlock()
	}
	mu.RUnlock()
	fmt.Println("This line is never reached")
}
```

**What happens:** Deadlock. You cannot acquire a write lock while holding a read lock. The write lock waits for all read locks to release, but this goroutine holds a read lock that will never release because it is waiting for the write lock.

**Fix:** Release the read lock first, then acquire the write lock with a double-check:
```go
mu.RLock()
val := config["db.host"]
mu.RUnlock()

if val == "" {
	mu.Lock()
	// Double-check after acquiring write lock -- another goroutine may have set it
	if config["db.host"] == "" {
		config["db.host"] = "localhost:5432"
	}
	mu.Unlock()
}
```

### RWMutex for Write-Heavy Workloads
Using `RWMutex` when writes are frequent provides no benefit over `Mutex` and adds overhead. RWMutex tracks reader counts internally, which costs more than a simple Mutex when there are few or no concurrent readers. `RWMutex` shines only when reads vastly outnumber writes -- like a config store, not a write log.

## Verify What You Learned

Build a feature flag service with a `ConfigStore` that supports `IsEnabled(flag string) bool` and `SetFlag(flag string, enabled bool)`. Run a benchmark with 90% `IsEnabled` calls and 10% `SetFlag` calls from 100 goroutines. Then run the same test with a 50/50 split. Verify that `RWMutex` is faster in the read-heavy case but not in the balanced case.

## What's Next
Continue to [03-waitgroup-wait-for-all](../03-waitgroup-wait-for-all/03-waitgroup-wait-for-all.md) to learn how to wait for a group of goroutines to complete.

## Summary
- `sync.RWMutex` allows multiple concurrent readers with `RLock/RUnlock`
- Writers get exclusive access with `Lock/Unlock`, blocking all readers and other writers
- Use `RWMutex` when reads significantly outnumber writes -- configuration stores, feature flags, routing tables
- You cannot upgrade a read lock to a write lock -- release first, then acquire
- For write-heavy workloads, a regular `Mutex` is simpler and equally fast
- Always return copies from read-locked methods to prevent callers from mutating internal state

## Reference
- [sync.RWMutex documentation](https://pkg.go.dev/sync#RWMutex)
- [Go Blog: Share Memory by Communicating](https://go.dev/blog/codelab-share)
- [Bryan Mills - Rethinking Classical Concurrency Patterns (GopherCon 2018)](https://www.youtube.com/watch?v=5zXAHh5tJqQ)
