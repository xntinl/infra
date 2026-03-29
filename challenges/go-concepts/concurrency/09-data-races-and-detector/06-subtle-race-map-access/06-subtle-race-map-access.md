---
difficulty: intermediate
concepts: [concurrent map access, fatal error, sync.Mutex, sync.RWMutex, sync.Map, map safety]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [goroutines, sync.WaitGroup, data race concept, sync.Mutex]
---

# 6. Subtle Race: Map Access


## Learning Objectives
After completing this exercise, you will be able to:
- **Reproduce** the "concurrent map writes" fatal error
- **Explain** why Go maps have built-in crash detection for concurrent access
- **Fix** concurrent map access using `sync.Mutex` and `sync.RWMutex`
- **Use** `sync.Map` for specific concurrent map patterns
- **Decide** between `sync.Mutex`, `sync.RWMutex`, and `sync.Map`

## Why Map Races Are Special

Unlike the counter race from exercises 01-05 (which produces silently wrong results), concurrent map access in Go causes the program to **crash immediately** with a fatal error. The Go runtime detects concurrent map read/write or write/write operations and terminates the process.

This is intentional. The Go designers decided that a silent corruption of a map data structure is worse than a crash, because a corrupted map can cause:
- Memory safety violations (reading from freed memory)
- Infinite loops during hash table traversal
- Corruption of unrelated memory

By crashing, the runtime makes the bug **impossible to ignore**.

## Step 1 -- Trigger the Concurrent Map Write Error

The `main.go` includes `racyMapWrite()` behind a command-line flag to avoid crashing the rest of the demos:

```go
package main

import (
    "fmt"
    "sync"
)

func racyMapWrite() {
    m := make(map[int]int)
    var wg sync.WaitGroup

    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            for j := 0; j < 1000; j++ {
                m[id*1000+j] = j // FATAL: concurrent map writes
            }
        }(i)
    }

    wg.Wait()
    fmt.Printf("Map has %d entries\n", len(m))
}

func main() {
    racyMapWrite()
}
```

### Verification
```bash
go run main.go crash-write
```
Expected output:
```
fatal error: concurrent map writes

goroutine 19 [running]:
...
```

This crash is non-deterministic. You may need to run it several times. Even writing to **different keys** crashes because the map's internal hash table is a shared data structure -- bucket resizing during growth affects all keys.

## Step 2 -- Concurrent Read + Write Also Crashes

```bash
go run main.go crash-readwrite
```
Expected:
```
fatal error: concurrent map read and map write
```

Many developers are surprised by this. "Reading is safe" is a common but wrong assumption for Go maps.

## Step 3 -- Fix with Mutex

Protect all map operations with a `sync.Mutex`:

```go
package main

import (
    "fmt"
    "sync"
)

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

    mu.Lock()
    count := len(m)
    mu.Unlock()

    fmt.Printf("Map (mutex) has %d entries (expected 10000)\n", count)
}

func main() {
    safeMapMutex()
}
```

### Verification
```bash
go run -race main.go
```
Expected: 10,000 entries, no crash, no race warnings.

## Step 4 -- Fix with sync.Map

`sync.Map` is designed for concurrent access without external locking:

```go
package main

import (
    "fmt"
    "sync"
)

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

    // sync.Map has no Len() method. Count with Range().
    count := 0
    m.Range(func(_, _ any) bool {
        count++
        return true
    })
    fmt.Printf("Map (sync.Map) has %d entries (expected 10000)\n", count)
}

func main() {
    safeMapSyncMap()
}
```

`sync.Map` API differences from regular maps:
- `Store(key, value)` instead of `m[key] = value`
- `Load(key)` returns `(value, ok)` instead of `m[key]`
- `Delete(key)` instead of `delete(m, key)`
- `Range(func(key, value any) bool)` for iteration
- No `len()` function

### Verification
```bash
go run -race main.go
```
Expected: 10,000 entries, no crash, no race warnings.

## Step 5 -- RWMutex for Read-Heavy Workloads

When reads vastly outnumber writes, `sync.RWMutex` improves throughput by allowing multiple concurrent readers:

```go
package main

import (
    "fmt"
    "math/rand"
    "sync"
)

func safeMapRWMutex() {
    m := make(map[int]int)
    var mu sync.RWMutex
    var wg sync.WaitGroup

    // 1 writer, 9 readers.
    for i := 0; i < 10; i++ {
        wg.Add(1)
        if i == 0 {
            go func() {
                defer wg.Done()
                for j := 0; j < 1000; j++ {
                    mu.Lock() // exclusive: blocks all readers and writers
                    m[j] = j * j
                    mu.Unlock()
                }
            }()
        } else {
            go func() {
                defer wg.Done()
                for j := 0; j < 1000; j++ {
                    mu.RLock() // shared: other readers proceed simultaneously
                    _ = m[rand.Intn(1000)]
                    mu.RUnlock()
                }
            }()
        }
    }

    wg.Wait()
    fmt.Printf("Map has %d entries\n", len(m))
}

func main() {
    safeMapRWMutex()
}
```

### Verification
```bash
go run -race main.go
```
Expected: correct entry count, no race warnings.

## When to Use Which

| Solution | Best For | Trade-off |
|----------|----------|-----------|
| `map + sync.Mutex` | General purpose, mixed read/write | Simple, predictable, but serializes all access |
| `map + sync.RWMutex` | Read-heavy workloads (90%+ reads) | Multiple concurrent readers, but writers block all |
| `sync.Map` | Write-once/read-many OR disjoint key sets | No external lock, but unusual API and not always faster |

**Default recommendation**: start with `map + sync.Mutex`. Only switch to `sync.RWMutex` or `sync.Map` if profiling shows lock contention as a bottleneck.

## Common Mistakes

### Thinking "I Only Write to Different Keys"
**Wrong assumption:** "Each goroutine writes to different keys, so there is no conflict."
**Reality:** The map's internal hash table is shared. Even if goroutines use different keys, the internal bucket restructuring during growth affects the entire map. Any concurrent write triggers the fatal error.

### Using sync.Map for Everything
`sync.Map` is NOT a drop-in replacement for `map + mutex`. It is optimized for two specific patterns:
1. Keys are written once but read many times (cache-like)
2. Multiple goroutines read, write, and overwrite entries for disjoint key sets

For general-purpose maps with mixed read/write patterns, a regular map with `sync.Mutex` is usually more efficient.

### Forgetting to Protect Map Reads
```go
mu.Lock()
m[key] = value
mu.Unlock()
// ...
val := m[key] // BUG: read without lock -- FATAL ERROR
```
**Fix:** Protect ALL map operations (read, write, delete, range) with the same mutex.

### Using RWMutex When Writes Are Frequent
`sync.RWMutex` adds overhead for writer starvation prevention. If writes are frequent (>10%), `sync.Mutex` is simpler and often faster.

## Verify What You Learned

```bash
go run -race main.go
```

1. Confirm zero race warnings from all safe map versions
2. Why does Go crash on concurrent map access instead of producing wrong results?
3. When would you use `sync.Map` over a regular map with `sync.Mutex`?
4. Does writing to different keys in a regular map avoid the crash? Why or why not?

## What's Next
Continue to [07-race-in-closure-loops](../07-race-in-closure-loops/07-race-in-closure-loops.md) to explore the classic closure-in-loop race bug.

## Summary
- Concurrent map access in Go causes a **fatal error**, not silently wrong results
- Both concurrent write+write AND concurrent read+write are fatal
- Fix option 1: `sync.Mutex` wrapping all map operations (general purpose, default choice)
- Fix option 2: `sync.RWMutex` for read-heavy workloads (multiple concurrent readers)
- Fix option 3: `sync.Map` for write-once/read-many or disjoint key sets
- Writing to different keys does NOT make concurrent access safe (internal structure is shared)
- `sync.Map` has a different API (`Store`, `Load`, `Delete`, `Range`) and no `Len` method
- When in doubt, use a regular map with `sync.Mutex`

## Reference
- [Go Blog: Go Maps in Action](https://go.dev/blog/maps)
- [sync.Map Documentation](https://pkg.go.dev/sync#Map)
- [sync.RWMutex Documentation](https://pkg.go.dev/sync#RWMutex)
- [Go FAQ: Why are map operations not defined to be atomic?](https://go.dev/doc/faq#atomic_maps)
