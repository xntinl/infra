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

Open `main.go`. Implement the `DataStore` struct methods. The store holds a `map[string]string` protected by an `RWMutex`:

```go
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

func (ds *DataStore) GetAll() map[string]string {
    ds.mu.RLock()
    defer ds.mu.RUnlock()
    // Return a copy to avoid exposing internal state
    result := make(map[string]string, len(ds.data))
    for k, v := range ds.data {
        result[k] = v
    }
    return result
}
```

Notice: `Get` and `GetAll` use `RLock` (shared), while `Set` uses `Lock` (exclusive).

### Intermediate Verification
```bash
go run main.go
```
The basic operations test should print that all reads and writes succeeded correctly.

## Step 2 -- Demonstrate Concurrent Readers

Implement `demonstrateConcurrentReads` to show that multiple readers proceed simultaneously:

```go
func demonstrateConcurrentReads(ds *DataStore) {
    fmt.Println("\n=== Concurrent Readers ===")
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

### Intermediate Verification
```bash
go run main.go
```
All 10 readers should finish in approximately 100ms, proving they ran concurrently (not serialized to ~1s).

## Step 3 -- Writer Blocks Readers

Implement `demonstrateWriterBlocking` to show that a writer gets exclusive access:

```go
func demonstrateWriterBlocking(ds *DataStore) {
    fmt.Println("\n=== Writer Blocks Readers ===")
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
            val, _ := ds.Get("writer-key")
            fmt.Printf("[%v] Reader %d: got value %q\n", time.Since(start).Round(time.Millisecond), id, val)
        }(i)
    }

    wg.Wait()
    fmt.Println("Readers had to wait for writer to finish.")
}
```

### Intermediate Verification
```bash
go run main.go
```
Readers should start waiting around 10ms and only succeed after ~200ms when the writer releases.

## Step 4 -- Performance Comparison: Mutex vs RWMutex

Implement `benchmarkComparison` to measure the difference for a read-heavy workload:

```go
func benchmarkComparison() {
    fmt.Println("\n=== Performance Comparison ===")
    const readers = 100
    const readsPerGoroutine = 10000
    const writers = 2
    const writesPerGoroutine = 100

    // Benchmark with regular Mutex
    mutexDuration := benchmarkMutex(readers, readsPerGoroutine, writers, writesPerGoroutine)

    // Benchmark with RWMutex
    rwMutexDuration := benchmarkRWMutex(readers, readsPerGoroutine, writers, writesPerGoroutine)

    fmt.Printf("Mutex:   %v\n", mutexDuration.Round(time.Millisecond))
    fmt.Printf("RWMutex: %v\n", rwMutexDuration.Round(time.Millisecond))

    if rwMutexDuration < mutexDuration {
        speedup := float64(mutexDuration) / float64(rwMutexDuration)
        fmt.Printf("RWMutex is %.1fx faster for this read-heavy workload\n", speedup)
    }
}
```

### Intermediate Verification
```bash
go run main.go
```
For the read-heavy workload (100 readers, 2 writers), `RWMutex` should be noticeably faster.

## Common Mistakes

### Using Lock When RLock Suffices
**Wrong:**
```go
func (ds *DataStore) Get(key string) string {
    ds.mu.Lock() // exclusive lock for a read-only operation
    defer ds.mu.Unlock()
    return ds.data[key]
}
```
**What happens:** Readers serialize unnecessarily, losing the concurrency benefit of RWMutex.

**Fix:** Use `RLock/RUnlock` for read-only operations.

### Upgrading RLock to Lock
**Wrong:**
```go
ds.mu.RLock()
val := ds.data[key]
if val == "" {
    ds.mu.Lock() // DEADLOCK: cannot upgrade RLock to Lock
    ds.data[key] = "default"
    ds.mu.Unlock()
}
ds.mu.RUnlock()
```
**What happens:** Deadlock. You cannot acquire a write lock while holding a read lock.

**Fix:** Release the read lock first, then acquire the write lock:
```go
ds.mu.RLock()
val := ds.data[key]
ds.mu.RUnlock()

if val == "" {
    ds.mu.Lock()
    // Double-check after acquiring write lock
    if ds.data[key] == "" {
        ds.data[key] = "default"
    }
    ds.mu.Unlock()
}
```

### RWMutex for Write-Heavy Workloads
Using `RWMutex` when writes are frequent provides no benefit over `Mutex` and adds overhead. `RWMutex` shines only when reads vastly outnumber writes.

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
