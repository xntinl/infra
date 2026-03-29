# 10. Channel vs Shared Memory

<!--
difficulty: advanced
concepts: [channels-vs-mutex, share-by-communicating, sync.Mutex, design-tradeoffs, concurrency-philosophy]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [goroutines, unbuffered-channels, buffered-channels, channel-direction, close]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01-09 (all previous channel exercises)
- Basic understanding of mutexes (will be explained as needed)

## Learning Objectives
After completing this exercise, you will be able to:
- **Solve** the same concurrency problem with both channels and mutexes
- **Compare** the readability, safety, and complexity of each approach
- **Choose** the right tool: channels for communication, mutexes for state protection
- **Articulate** Go's "share memory by communicating" philosophy

## Why This Comparison Matters

Go's most famous concurrency proverb is: "Don't communicate by sharing memory; share memory by communicating." But this doesn't mean mutexes are bad or channels are always better. It means you should prefer communicating between goroutines (channels) over sharing data structures that require locking (mutexes).

Both tools have their place. Channels excel when goroutines need to pass data, coordinate workflows, or signal events. Mutexes excel when you just need to protect a data structure from concurrent access with minimal overhead.

This exercise puts both approaches side by side for the same problem, so you can develop an intuition for the tradeoffs.

## Step 1 -- The Problem: Data Race

Multiple goroutines increment a shared counter without protection. This is a data race.

```go
package main

import (
    "fmt"
    "sync"
)

func main() {
    counter := 0
    var wg sync.WaitGroup

    for i := 0; i < 1000; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            // DATA RACE: counter++ is read-modify-write, not atomic.
            // Two goroutines can read the same value, both increment,
            // both write -- one increment is lost.
            counter++
        }()
    }

    wg.Wait()
    fmt.Printf("Counter: %d (expected 1000, may be wrong)\n", counter)
}
```

### Verification
```bash
go run -race main.go
# Expected: WARNING: DATA RACE detected, counter may not be 1000
```

The `-race` flag enables Go's race detector -- essential for finding data races during development.

## Step 2 -- Solution A: Mutex

Protect the counter with `sync.Mutex`. Lock before reading/writing, unlock after.

```go
package main

import (
    "fmt"
    "sync"
    "time"
)

func main() {
    counter := 0
    var mu sync.Mutex
    var wg sync.WaitGroup
    start := time.Now()

    for i := 0; i < 1000; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            mu.Lock()
            counter++
            mu.Unlock()
        }()
    }

    wg.Wait()
    elapsed := time.Since(start)
    fmt.Printf("Counter: %d (took %v)\n", counter, elapsed)
}
```

**Pros:** Simple, low overhead, directly protects the data.
**Cons:** Easy to forget the lock, can deadlock with multiple mutexes, doesn't compose well.

### Verification
```bash
go run -race main.go
# Expected: Counter: 1000, no race warnings
```

## Step 3 -- Solution B: Channel

Send increment requests to a single goroutine that owns the counter. No shared memory.

```go
package main

import (
    "fmt"
    "sync"
    "time"
)

func main() {
    start := time.Now()

    increments := make(chan struct{}, 100)
    result := make(chan int)

    // Counter goroutine: the ONLY goroutine that touches the counter.
    go func() {
        counter := 0
        for range increments {
            counter++
        }
        result <- counter
    }()

    // 1000 goroutines send increment signals.
    var wg sync.WaitGroup
    for i := 0; i < 1000; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            increments <- struct{}{}
        }()
    }

    wg.Wait()
    close(increments)
    total := <-result

    elapsed := time.Since(start)
    fmt.Printf("Counter: %d (took %v, no shared state)\n", total, elapsed)
}
```

**Pros:** No shared state, impossible to forget locking, counter goroutine is self-contained.
**Cons:** More boilerplate, overhead of channel operations.

### Verification
```bash
go run -race main.go
# Expected: Counter: 1000, no race warnings
```

## Step 4 -- A Richer Problem: Concurrent Cache

A simple counter is biased toward mutexes. Now try a richer problem where both approaches are more balanced.

### Mutex Version

```go
package main

import (
    "fmt"
    "sync"
)

type MutexCache struct {
    mu    sync.RWMutex
    items map[string]string
}

func NewMutexCache() *MutexCache {
    return &MutexCache{items: make(map[string]string)}
}

func (c *MutexCache) Set(key, value string) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.items[key] = value
}

func (c *MutexCache) Get(key string) (string, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    val, ok := c.items[key]
    return val, ok
}

func main() {
    mc := NewMutexCache()
    mc.Set("language", "Go")
    mc.Set("creator", "Rob Pike")

    if val, ok := mc.Get("language"); ok {
        fmt.Printf("language = %s\n", val)
    }
    if val, ok := mc.Get("creator"); ok {
        fmt.Printf("creator = %s\n", val)
    }
}
```

### Channel Version

```go
package main

import "fmt"

type CacheResponse struct {
    Value string
    Found bool
}

type CacheRequest struct {
    Op    string
    Key   string
    Value string
    Reply chan CacheResponse
}

func cacheService(requests <-chan CacheRequest) {
    items := make(map[string]string)
    for req := range requests {
        switch req.Op {
        case "set":
            items[req.Key] = req.Value
            req.Reply <- CacheResponse{Value: req.Value, Found: true}
        case "get":
            val, ok := items[req.Key]
            req.Reply <- CacheResponse{Value: val, Found: ok}
        }
    }
}

func main() {
    requests := make(chan CacheRequest)
    go cacheService(requests)

    reply := make(chan CacheResponse, 1)

    requests <- CacheRequest{Op: "set", Key: "language", Value: "Go", Reply: reply}
    <-reply
    requests <- CacheRequest{Op: "set", Key: "creator", Value: "Rob Pike", Reply: reply}
    <-reply

    requests <- CacheRequest{Op: "get", Key: "language", Reply: reply}
    resp := <-reply
    fmt.Printf("language = %s\n", resp.Value)

    requests <- CacheRequest{Op: "get", Key: "creator", Reply: reply}
    resp = <-reply
    fmt.Printf("creator = %s\n", resp.Value)

    close(requests)
}
```

### Verification
```bash
go run main.go
# Both produce: language = Go, creator = Rob Pike
```

The mutex version is shorter. The channel version makes the data flow explicit. Which is "better" depends on context. For a simple cache, mutex is often clearer. For a cache that also needs to emit events, maintain history, or coordinate with other services, the channel version extends more naturally.

## Step 5 -- When to Use Which

| Use Channels When | Use Mutexes When |
|---|---|
| Passing ownership of data between goroutines | Protecting internal state of a struct |
| Coordinating multiple goroutines (fan-out, pipeline) | Simple read/write protection (counters, caches) |
| Signaling events (done, quit, ready) | Performance-critical hot paths |
| The "server" pattern (request-response) | You need RWMutex for read-heavy workloads |
| You want to compose concurrency operations | The protected section is small and self-contained |

**Rule of thumb:** If you're transferring data or coordinating work, use channels. If you're guarding access to a shared data structure, use a mutex.

## Step 6 -- Hit Counter Benchmark

Run 10,000 operations on both implementations and compare.

```go
package main

import (
    "fmt"
    "math/rand"
    "sort"
    "sync"
    "time"
)

type PageCount struct {
    Page  string
    Count int
}

var pages = []string{
    "/home", "/about", "/products", "/blog", "/contact",
    "/faq", "/pricing", "/docs", "/login", "/signup",
}

// --- Mutex version ---

type MutexHitCounter struct {
    mu   sync.Mutex
    hits map[string]int
}

func NewMutexHitCounter() *MutexHitCounter {
    return &MutexHitCounter{hits: make(map[string]int)}
}

func (h *MutexHitCounter) Record(page string) {
    h.mu.Lock()
    h.hits[page]++
    h.mu.Unlock()
}

func (h *MutexHitCounter) Total() int {
    h.mu.Lock()
    defer h.mu.Unlock()
    total := 0
    for _, c := range h.hits {
        total += c
    }
    return total
}

func (h *MutexHitCounter) TopPages(n int) []PageCount {
    h.mu.Lock()
    defer h.mu.Unlock()
    var entries []PageCount
    for page, count := range h.hits {
        entries = append(entries, PageCount{page, count})
    }
    sort.Slice(entries, func(i, j int) bool {
        return entries[i].Count > entries[j].Count
    })
    if n < len(entries) {
        entries = entries[:n]
    }
    return entries
}

func main() {
    // Mutex version
    mhc := NewMutexHitCounter()
    var wg1 sync.WaitGroup

    start1 := time.Now()
    for g := 0; g < 100; g++ {
        wg1.Add(1)
        go func() {
            defer wg1.Done()
            for i := 0; i < 100; i++ {
                page := pages[rand.Intn(len(pages))]
                mhc.Record(page)
            }
        }()
    }
    wg1.Wait()
    elapsed1 := time.Since(start1)

    fmt.Printf("Mutex:   total=%d, time=%v\n", mhc.Total(), elapsed1)
    fmt.Println("Top 3:")
    for _, p := range mhc.TopPages(3) {
        fmt.Printf("  %s: %d\n", p.Page, p.Count)
    }

    // Channel version (shown in main.go for full implementation)
    fmt.Println("\nChannel version: see main.go for full benchmark comparison")
    fmt.Println("\nKey insight:")
    fmt.Println("  Mutex is faster for simple state guarding")
    fmt.Println("  Channels shine for coordination and complex workflows")
}
```

### Verification
```bash
go run main.go
# Expected: both versions produce total=10000, mutex is typically faster
```

## Common Mistakes

### Using Channels Where a Mutex Is Simpler

**Wrong:** 50 lines of channel plumbing just to increment a counter.

**Fix:** Use `sync.Mutex` or `sync/atomic` for simple state protection. Don't over-engineer.

### Using Mutex Where Channels Communicate Better

**Wrong:**
```go
var mu sync.Mutex
var buffer []int

// Producer:
mu.Lock()
buffer = append(buffer, value)
mu.Unlock()

// Consumer (must poll in a loop):
for {
    mu.Lock()
    if len(buffer) > 0 {
        val = buffer[0]
        buffer = buffer[1:]
        mu.Unlock()
        break
    }
    mu.Unlock()
    time.Sleep(10 * time.Millisecond) // ugly polling
}
```

**Fix:** Use a channel. It's a built-in thread-safe queue with blocking semantics -- no polling needed.

## What's Next
You've completed the channels section. Continue to the select and multiplexing section to learn how `select` lets you wait on multiple channel operations simultaneously.

## Summary
- Both channels and mutexes solve concurrency problems; they serve different purposes
- Channels are for communication and coordination between goroutines
- Mutexes are for protecting shared state within a goroutine or struct
- For simple state (counters, caches), mutexes are often clearer and faster
- For workflows (pipelines, fan-out, request-response), channels are cleaner
- "Share memory by communicating" is a design preference, not an absolute rule
- Most real Go programs use both tools where each is most appropriate

## Reference
- [Go Blog: Share Memory By Communicating](https://go.dev/blog/codelab-share)
- [Effective Go: Concurrency](https://go.dev/doc/effective_go#concurrency)
- [Go Wiki: Mutex or Channel](https://go.dev/wiki/MutexOrChannel)
- [sync package](https://pkg.go.dev/sync)
