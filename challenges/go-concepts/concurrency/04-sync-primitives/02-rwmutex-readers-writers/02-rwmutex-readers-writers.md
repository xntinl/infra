# 2. RWMutex: Readers-Writers

<!--
difficulty: intermediate
concepts: [sync.RWMutex, RLock, RUnlock, concurrent reads, exclusive writes, read-heavy optimization]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [sync.Mutex, goroutines, WaitGroup]
-->

## Prerequisites
- Go 1.22+ installed
- Understanding of `sync.Mutex` (exercise 01)
- Familiarity with goroutines and `sync.WaitGroup`

## Learning Objectives
After completing this exercise, you will be able to:
- **Explain** the difference between `sync.Mutex` and `sync.RWMutex`
- **Use** `RLock/RUnlock` for concurrent read access
- **Use** `Lock/Unlock` for exclusive write access
- **Compare** performance of `Mutex` vs `RWMutex` for read-heavy workloads

## Why RWMutex
A regular `sync.Mutex` serializes all access -- even when multiple goroutines only need to read. This is unnecessarily restrictive for read-heavy workloads where writes are infrequent. If ten goroutines want to read a configuration map and only one occasionally updates it, forcing all readers to wait for each other wastes concurrency.

`sync.RWMutex` solves this with two levels of locking:
- **Read lock** (`RLock`): multiple goroutines can hold a read lock simultaneously. They only block if a writer holds the exclusive lock.
- **Write lock** (`Lock`): only one goroutine can hold the write lock. It blocks until all readers release their read locks, and no new readers can acquire a read lock while a writer is waiting.

This makes `RWMutex` ideal for data structures that are read far more often than they are written -- caches, configuration stores, routing tables, and similar shared state.

## Step 1 -- Build a Thread-Safe Data Store

Run `main.go` to see the DataStore with RLock for reads and Lock for writes:

```go
package main

import (
	"fmt"
	"sync"
)

type DataStore struct {
	mu   sync.RWMutex
	data map[string]string
}

func NewDataStore() *DataStore {
	return &DataStore{data: make(map[string]string)}
}

func (ds *DataStore) Get(key string) (string, bool) {
	ds.mu.RLock()         // shared read lock -- multiple readers OK
	defer ds.mu.RUnlock()
	val, ok := ds.data[key]
	return val, ok
}

func (ds *DataStore) Set(key, value string) {
	ds.mu.Lock()          // exclusive write lock -- blocks all readers and writers
	defer ds.mu.Unlock()
	ds.data[key] = value
}

func (ds *DataStore) GetAll() map[string]string {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	result := make(map[string]string, len(ds.data))
	for k, v := range ds.data {
		result[k] = v
	}
	return result // return a COPY to avoid exposing internal state
}

func main() {
	ds := NewDataStore()
	ds.Set("name", "Go")
	ds.Set("version", "1.22")

	name, ok := ds.Get("name")
	fmt.Printf("Get 'name': %s (found: %v)\n", name, ok)

	all := ds.GetAll()
	fmt.Printf("All entries: %v\n", all)
}
```

Expected output:
```
Get 'name': Go (found: true)
All entries: map[name:Go version:1.22]
```

### Intermediate Verification
```bash
go run main.go
```
The basic operations test should print that all reads and writes succeeded correctly.

## Step 2 -- Demonstrate Concurrent Readers

The program shows that multiple readers proceed simultaneously:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type DataStore struct {
	mu   sync.RWMutex
	data map[string]string
}

func NewDataStore() *DataStore {
	return &DataStore{data: make(map[string]string)}
}

func (ds *DataStore) Get(key string) (string, bool) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	val, ok := ds.data[key]
	return val, ok
}

func (ds *DataStore) Set(key, value string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.data[key] = value
}

func main() {
	ds := NewDataStore()
	ds.Set("shared-key", "shared-value")

	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			val, _ := ds.Get("shared-key")
			time.Sleep(100 * time.Millisecond) // simulate read processing
			fmt.Printf("Reader %d read: %s (at %v)\n", id, val, time.Since(start).Round(time.Millisecond))
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)
	fmt.Printf("All 10 readers finished in %v\n", elapsed.Round(time.Millisecond))
	fmt.Println("(If serialized, this would take ~1s. Concurrent readers finish in ~100ms.)")
}
```

Expected output:
```
Reader 0 read: shared-value (at 100ms)
Reader 1 read: shared-value (at 100ms)
...
All 10 readers finished in 100ms
(If serialized, this would take ~1s. Concurrent readers finish in ~100ms.)
```

### Intermediate Verification
```bash
go run main.go
```
All 10 readers should finish in approximately 100ms, proving they ran concurrently (not serialized to ~1s).

## Step 3 -- Writer Blocks Readers

The program demonstrates that a writer gets exclusive access:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type DataStore struct {
	mu   sync.RWMutex
	data map[string]string
}

func main() {
	ds := &DataStore{data: make(map[string]string)}
	var wg sync.WaitGroup
	start := time.Now()

	// Start a writer that holds the lock for 200ms
	wg.Add(1)
	go func() {
		defer wg.Done()
		ds.mu.Lock()
		fmt.Printf("[%v] Writer: acquired exclusive lock\n", time.Since(start).Round(time.Millisecond))
		time.Sleep(200 * time.Millisecond)
		ds.data["writer-key"] = "writer-value"
		fmt.Printf("[%v] Writer: releasing lock\n", time.Since(start).Round(time.Millisecond))
		ds.mu.Unlock()
	}()

	time.Sleep(10 * time.Millisecond) // let writer acquire lock first

	// Start readers that will block until the writer releases
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			fmt.Printf("[%v] Reader %d: waiting for read lock...\n", time.Since(start).Round(time.Millisecond), id)
			ds.mu.RLock()
			val := ds.data["writer-key"]
			ds.mu.RUnlock()
			fmt.Printf("[%v] Reader %d: got value %q\n", time.Since(start).Round(time.Millisecond), id, val)
		}(i)
	}

	wg.Wait()
	fmt.Println("Readers had to wait for writer to finish.")
}
```

Expected output:
```
[0ms] Writer: acquired exclusive lock
[10ms] Reader 0: waiting for read lock...
[10ms] Reader 1: waiting for read lock...
[10ms] Reader 2: waiting for read lock...
[200ms] Writer: releasing lock
[200ms] Reader 0: got value "writer-value"
[200ms] Reader 1: got value "writer-value"
[200ms] Reader 2: got value "writer-value"
Readers had to wait for writer to finish.
```

### Intermediate Verification
```bash
go run main.go
```
Readers should start waiting around 10ms and only succeed after ~200ms when the writer releases.

## Step 4 -- Performance Comparison: Mutex vs RWMutex

The program benchmarks read-heavy workloads with both approaches:

```bash
go run main.go
```

Expected output (times vary by machine):
```
=== 4. Performance Comparison: Mutex vs RWMutex ===
Mutex:   45ms
RWMutex: 15ms
RWMutex is 3.0x faster for this read-heavy workload
```

For the read-heavy workload (100 readers, 2 writers), `RWMutex` should be noticeably faster because reads proceed concurrently.

## Common Mistakes

### Using Lock When RLock Suffices

```go
func (ds *DataStore) Get(key string) string {
	ds.mu.Lock() // WRONG: exclusive lock for a read-only operation
	defer ds.mu.Unlock()
	return ds.data[key]
}
```

**What happens:** Readers serialize unnecessarily, losing the concurrency benefit of RWMutex. You have essentially turned your RWMutex into a regular Mutex.

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
	data := map[string]string{"key": ""}

	mu.RLock()
	val := data["key"]
	if val == "" {
		mu.Lock() // DEADLOCK: cannot upgrade RLock to Lock
		data["key"] = "default"
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
val := data["key"]
mu.RUnlock()

if val == "" {
	mu.Lock()
	// Double-check after acquiring write lock -- another goroutine may have set it
	if data["key"] == "" {
		data["key"] = "default"
	}
	mu.Unlock()
}
```

### RWMutex for Write-Heavy Workloads
Using `RWMutex` when writes are frequent provides no benefit over `Mutex` and adds overhead. RWMutex tracks reader counts internally, which costs more than a simple Mutex when there are few or no concurrent readers. `RWMutex` shines only when reads vastly outnumber writes.

## Verify What You Learned

Build a concurrent cache that supports `Get`, `Set`, and `Delete` operations. Run a benchmark with 90% reads and 10% writes, then with 50/50. Verify that `RWMutex` is faster in the read-heavy case but not in the balanced case.

## What's Next
Continue to [03-waitgroup-wait-for-all](../03-waitgroup-wait-for-all/03-waitgroup-wait-for-all.md) to learn how to wait for a group of goroutines to complete.

## Summary
- `sync.RWMutex` allows multiple concurrent readers with `RLock/RUnlock`
- Writers get exclusive access with `Lock/Unlock`, blocking all readers and other writers
- Use `RWMutex` when reads significantly outnumber writes
- You cannot upgrade a read lock to a write lock -- release first, then acquire
- For write-heavy workloads, a regular `Mutex` is simpler and equally fast
- Always return copies from read-locked methods to prevent callers from mutating internal state

## Reference
- [sync.RWMutex documentation](https://pkg.go.dev/sync#RWMutex)
- [Go Blog: Share Memory by Communicating](https://go.dev/blog/codelab-share)
- [Bryan Mills - Rethinking Classical Concurrency Patterns (GopherCon 2018)](https://www.youtube.com/watch?v=5zXAHh5tJqQ)
