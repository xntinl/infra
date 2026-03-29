# 6. Subtle Race: Map Access

<!--
difficulty: intermediate
concepts: [concurrent map access, fatal error, sync.Mutex, sync.Map, map safety]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [goroutines, sync.WaitGroup, data race concept, sync.Mutex]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01-05 (data races and fixes)
- Familiarity with Go maps

## Learning Objectives
After completing this exercise, you will be able to:
- **Reproduce** the "concurrent map writes" fatal error
- **Explain** why Go maps have built-in crash detection for concurrent access
- **Fix** concurrent map access using `sync.Mutex`
- **Use** `sync.Map` for specific concurrent map patterns
- **Decide** between `sync.Mutex` and `sync.Map` for different workloads

## Why Map Races Are Special
Unlike the counter race from exercises 01-05 (which produces silently wrong results), concurrent map access in Go causes the program to **crash immediately** with a fatal error. The Go runtime detects concurrent map read/write or write/write operations and terminates the process.

This is intentional. The Go designers decided that a silent corruption of a map data structure is worse than a crash, because a corrupted map can cause memory safety violations (reading from freed memory, infinite loops during hash table traversal, etc.). By crashing, the runtime makes the bug impossible to ignore.

This exercise introduces a different category of race consequence: not just wrong results, but fatal crashes.

## Step 1 -- Trigger the Concurrent Map Write Error

Edit `main.go` and implement `racyMapWrite`. Launch multiple goroutines that write to the same map concurrently:

```go
func racyMapWrite() {
    m := make(map[int]int)
    var wg sync.WaitGroup

    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            for j := 0; j < 1000; j++ {
                m[id*1000+j] = j // concurrent map write
            }
        }(i)
    }

    wg.Wait()
    fmt.Printf("Map has %d entries\n", len(m))
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected: the program crashes with a message like:
```
fatal error: concurrent map writes

goroutine 19 [running]:
...
```

This crash is non-deterministic. You may need to run it several times if the timing does not trigger the crash on the first attempt.

## Step 2 -- Fix with Mutex

Implement `safeMapMutex` using a `sync.Mutex` to protect all map operations:

```go
func safeMapMutex() {
    m := make(map[int]int)
    var mu sync.Mutex
    var wg sync.WaitGroup

    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            for j := 0; j < 1000; j++ {
                mu.Lock()
                m[id*1000+j] = j
                mu.Unlock()
            }
        }(i)
    }

    wg.Wait()
    fmt.Printf("Safe map (mutex) has %d entries\n", len(m))
}
```

### Intermediate Verification
```bash
go run -race main.go
```
No crash, no race warning, correct count of 10,000 entries.

## Step 3 -- Fix with sync.Map

Implement `safeMapSyncMap` using `sync.Map`, which is designed for concurrent access without external locking:

```go
func safeMapSyncMap() {
    var m sync.Map
    var wg sync.WaitGroup

    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            for j := 0; j < 1000; j++ {
                m.Store(id*1000+j, j)
            }
        }(i)
    }

    wg.Wait()

    // Count entries (sync.Map has no Len method)
    count := 0
    m.Range(func(_, _ any) bool {
        count++
        return true
    })
    fmt.Printf("Safe map (sync.Map) has %d entries\n", count)
}
```

`sync.Map` has a different API from regular maps:
- `Store(key, value)` instead of `m[key] = value`
- `Load(key)` returns `(value, ok)` instead of `m[key]`
- `Delete(key)` instead of `delete(m, key)`
- `Range(func(key, value any) bool)` for iteration
- No `len()` function

### Intermediate Verification
```bash
go run -race main.go
```
No crash, no race warning, correct count.

## Step 4 -- Concurrent Read and Write

Implement `racyMapReadWrite` to show that even concurrent read + write causes a fatal error (not just write + write):

```go
func racyMapReadWrite() {
    m := make(map[int]int)
    m[1] = 100 // pre-populate

    var wg sync.WaitGroup

    // Writer
    wg.Add(1)
    go func() {
        defer wg.Done()
        for i := 0; i < 10000; i++ {
            m[1] = i
        }
    }()

    // Reader
    wg.Add(1)
    go func() {
        defer wg.Done()
        for i := 0; i < 10000; i++ {
            _ = m[1]
        }
    }()

    wg.Wait()
}
```

### Intermediate Verification
```bash
go run main.go
```
This also crashes. Concurrent read + write is equally fatal.

## Common Mistakes

### Thinking "I Only Write to Different Keys"
**Wrong assumption:** "Each goroutine writes to different keys, so there is no conflict."
**Reality:** The map's internal hash table is shared. Even if goroutines use different keys, the internal bucket restructuring during growth affects the entire map. Any concurrent write triggers the fatal error.

### Using sync.Map for Everything
`sync.Map` is optimized for two specific patterns:
1. Keys are written once but read many times (cache-like)
2. Multiple goroutines read, write, and overwrite entries for disjoint key sets

For general-purpose maps with mixed read/write patterns, a regular map with `sync.Mutex` (or `sync.RWMutex` for read-heavy workloads) is usually more efficient.

### Forgetting to Protect Map Reads
**Wrong:**
```go
mu.Lock()
m[key] = value
mu.Unlock()
// ...
val := m[key] // BUG: read without lock
```
**Fix:** Protect ALL map operations (read, write, delete, range) with the same mutex.

## Verify What You Learned

1. Run the safe versions with `go run -race main.go` to confirm no races
2. Why does Go crash on concurrent map access instead of producing wrong results?
3. When would you use `sync.Map` over a regular map with `sync.Mutex`?
4. Does writing to different keys in a regular map avoid the crash? Why or why not?

## What's Next
Continue to [07-race-in-closure-loops](../07-race-in-closure-loops/07-race-in-closure-loops.md) to explore the classic closure-in-loop race bug.

## Summary
- Concurrent map access in Go causes a **fatal error**, not silently wrong results
- Both concurrent write+write and concurrent read+write are fatal
- Fix option 1: `sync.Mutex` wrapping all map operations (general purpose)
- Fix option 2: `sync.Map` for specific patterns (write-once/read-many, disjoint key sets)
- Writing to different keys does NOT make concurrent access safe (internal structure is shared)
- `sync.Map` has a different API (Store, Load, Delete, Range) and no Len method
- When in doubt, use a regular map with `sync.Mutex`

## Reference
- [Go Blog: Go Maps in Action](https://go.dev/blog/maps)
- [sync.Map Documentation](https://pkg.go.dev/sync#Map)
- [Go FAQ: Why are map operations not defined to be atomic?](https://go.dev/doc/faq#atomic_maps)
