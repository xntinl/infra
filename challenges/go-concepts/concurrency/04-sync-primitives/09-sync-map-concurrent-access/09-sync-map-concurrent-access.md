# 9. sync.Map: Concurrent Access

<!--
difficulty: intermediate
concepts: [sync.Map, Load, Store, LoadOrStore, Delete, Range, concurrent map access]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [sync.Mutex, goroutines, sync.WaitGroup, maps]
-->

## Prerequisites
- Go 1.22+ installed
- Understanding of `sync.Mutex` and `sync.RWMutex`
- Familiarity with Go maps and goroutines

## Learning Objectives
After completing this exercise, you will be able to:
- **Demonstrate** why concurrent access to a regular Go map panics
- **Use** `sync.Map` methods: `Load`, `Store`, `LoadOrStore`, `Delete`, `Range`
- **Compare** `sync.Map` with a regular map protected by `sync.RWMutex`
- **Decide** when `sync.Map` is appropriate vs. a map with mutex

## Why sync.Map
Go maps are not safe for concurrent use. If multiple goroutines read and write a map simultaneously without synchronization, the runtime will detect the race and panic with a fatal error: `concurrent map read and map write`. This is a deliberate safety mechanism -- Go crashes loudly rather than silently corrupting data.

The standard fix is wrapping the map with a `sync.RWMutex`. However, `sync.Map` exists for two specific use cases where it outperforms a mutex-protected map:

1. **Append-only maps**: keys are written once and then only read (cache, registry). `sync.Map` eliminates lock contention on reads.
2. **Disjoint key access**: different goroutines work on different key subsets. `sync.Map` avoids locking the entire map for unrelated operations.

For general-purpose concurrent maps, a regular `map` with `sync.RWMutex` is typically simpler and often faster. Use `sync.Map` only when profiling shows it helps.

## Step 1 -- The Map Panic

Open `main.go`. The `showMapPanic` function demonstrates the concurrent access panic:

```go
func showMapPanic() {
    fmt.Println("=== Regular Map Panic ===")
    m := make(map[int]int)
    var wg sync.WaitGroup

    // This will likely panic with "concurrent map writes"
    defer func() {
        if r := recover(); r != nil {
            fmt.Printf("PANIC recovered: %v\n", r)
            fmt.Println("Regular maps are NOT safe for concurrent access!\n")
        }
    }()

    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func(n int) {
            defer wg.Done()
            m[n] = n * n    // concurrent write -- UNSAFE
            _ = m[n]         // concurrent read -- UNSAFE with concurrent writes
        }(i)
    }

    wg.Wait()
}
```

### Intermediate Verification
```bash
go run main.go
```
The program should panic (or be caught by recover) with a concurrent map access error.

## Step 2 -- Replace with sync.Map

Implement `syncMapBasics` showing all core operations:

```go
func syncMapBasics() {
    fmt.Println("=== sync.Map Basics ===")
    var m sync.Map

    // Store: set key-value pairs
    m.Store("name", "Go")
    m.Store("version", "1.22")
    m.Store("mascot", "Gopher")

    // Load: retrieve a value
    val, ok := m.Load("name")
    fmt.Printf("Load 'name': %v (found: %v)\n", val, ok)

    val, ok = m.Load("missing")
    fmt.Printf("Load 'missing': %v (found: %v)\n", val, ok)

    // LoadOrStore: load if exists, store if not
    actual, loaded := m.LoadOrStore("version", "2.0")
    fmt.Printf("LoadOrStore 'version': %v (was loaded: %v)\n", actual, loaded)

    actual, loaded = m.LoadOrStore("new-key", "new-value")
    fmt.Printf("LoadOrStore 'new-key': %v (was loaded: %v)\n", actual, loaded)

    // Delete: remove a key
    m.Delete("mascot")
    _, ok = m.Load("mascot")
    fmt.Printf("After Delete 'mascot': found=%v\n", ok)

    // Range: iterate over all entries
    fmt.Println("All entries:")
    m.Range(func(key, value any) bool {
        fmt.Printf("  %v: %v\n", key, value)
        return true // return false to stop iteration
    })
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
All operations should work correctly. LoadOrStore for existing "version" returns "1.22" (loaded=true). For new "new-key" it stores and returns "new-value" (loaded=false).

## Step 3 -- Concurrent sync.Map Access

Implement `concurrentSyncMap` to prove sync.Map handles concurrent access:

```go
func concurrentSyncMap() {
    fmt.Println("=== Concurrent sync.Map ===")
    var m sync.Map
    var wg sync.WaitGroup

    // 100 goroutines writing concurrently
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func(n int) {
            defer wg.Done()
            m.Store(n, n*n)
        }(i)
    }

    // 100 goroutines reading concurrently
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func(n int) {
            defer wg.Done()
            m.Load(n)
        }(i)
    }

    wg.Wait()

    // Count entries
    count := 0
    m.Range(func(_, _ any) bool {
        count++
        return true
    })
    fmt.Printf("Stored %d entries concurrently with no panic.\n\n", count)
}
```

### Intermediate Verification
```bash
go run -race main.go
```
No panics, no data races. All 100 entries should be stored correctly.

## Step 4 -- Compare sync.Map vs Map+RWMutex

Implement a comparison for different workload patterns:

```go
func comparePerformance() {
    fmt.Println("=== Performance Comparison ===")

    const n = 100000

    // Read-heavy workload (90% reads, 10% writes)
    fmt.Println("Read-heavy workload (90% reads, 10% writes):")
    syncMapTime := benchmarkSyncMap(n, 0.9)
    rwMutexTime := benchmarkRWMutexMap(n, 0.9)
    fmt.Printf("  sync.Map:      %v\n", syncMapTime.Round(time.Millisecond))
    fmt.Printf("  map+RWMutex:   %v\n", rwMutexTime.Round(time.Millisecond))

    // Write-heavy workload (50% reads, 50% writes)
    fmt.Println("Write-heavy workload (50% reads, 50% writes):")
    syncMapTime = benchmarkSyncMap(n, 0.5)
    rwMutexTime = benchmarkRWMutexMap(n, 0.5)
    fmt.Printf("  sync.Map:      %v\n", syncMapTime.Round(time.Millisecond))
    fmt.Printf("  map+RWMutex:   %v\n", rwMutexTime.Round(time.Millisecond))
}
```

### Intermediate Verification
```bash
go run main.go
```
For read-heavy workloads, `sync.Map` may be competitive or faster. For write-heavy workloads, `map+RWMutex` is typically faster.

## Common Mistakes

### Using sync.Map for Everything
**Wrong:** Replacing all concurrent maps with `sync.Map` blindly.

**Reality:** `sync.Map` is optimized for two patterns (append-only, disjoint keys). For general concurrent map access, `map+RWMutex` is simpler and often faster.

### Type Assertions Everywhere
**Annoying:**
```go
val, _ := m.Load("count")
count := val.(int) // type assertion on every access
```
**Reality:** `sync.Map` stores `any` types, requiring type assertions. If your map is type-homogeneous, a generic map with mutex is more ergonomic.

### Mixing Range with Delete
**Subtle:**
```go
m.Range(func(key, value any) bool {
    if shouldDelete(value) {
        m.Delete(key) // safe, but may or may not affect ongoing Range
    }
    return true
})
```
Deleting during Range is safe (no panic), but the deleted key may or may not be visited by subsequent Range iterations. The behavior is non-deterministic.

### Assuming Range Sees a Consistent Snapshot
`Range` does not take a snapshot. Other goroutines can `Store` or `Delete` entries during Range execution. If you need a consistent snapshot, use a regular map with a read lock.

## Verify What You Learned

Build a concurrent cache with `sync.Map` that supports:
- `GetOrCompute(key, func() value)`: load from cache or compute and store atomically using `LoadOrStore`
- `Evict(key)`: remove an entry
- `Size()`: count entries using `Range`

Test with 50 concurrent goroutines accessing overlapping keys.

## What's Next
Continue to [10-build-thread-safe-counter](../10-build-thread-safe-counter/10-build-thread-safe-counter.md) to build a comprehensive thread-safe counter using all sync primitives you have learned.

## Summary
- Regular Go maps panic under concurrent read-write access
- `sync.Map` provides `Load`, `Store`, `LoadOrStore`, `Delete`, and `Range` for concurrent access
- `sync.Map` excels at append-only maps and disjoint key access patterns
- For general concurrent maps, prefer `map` + `sync.RWMutex` -- it is simpler and often faster
- `sync.Map` stores `any` types, requiring type assertions on read
- `Range` does not provide a consistent snapshot -- entries may be added or removed during iteration
- Profile before choosing `sync.Map` -- do not use it as a default

## Reference
- [sync.Map documentation](https://pkg.go.dev/sync#Map)
- [Go Blog: Maps in Action](https://go.dev/blog/maps)
- [sync.Map GopherCon Talk](https://www.youtube.com/watch?v=C1EtfDnsdDs)
