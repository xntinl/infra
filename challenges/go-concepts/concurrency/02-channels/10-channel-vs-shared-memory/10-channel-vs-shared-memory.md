---
difficulty: advanced
concepts: [channels-vs-mutex, share-by-communicating, sync.Mutex, design-tradeoffs, concurrency-philosophy]
tools: [go]
estimated_time: 35m
bloom_level: analyze
---

# 10. Channel vs Shared Memory

## Learning Objectives
After completing this exercise, you will be able to:
- **Solve** the same concurrency problem with both channels and mutexes
- **Compare** the readability, safety, and complexity of each approach
- **Choose** the right tool: channels for communication, mutexes for state protection
- **Articulate** Go's "share memory by communicating" philosophy

## Why This Comparison Matters

Go's most famous concurrency proverb is: "Don't communicate by sharing memory; share memory by communicating." But this does not mean mutexes are bad or channels are always better. It means you should prefer communicating between goroutines (channels) over sharing data structures that require locking (mutexes).

Both tools have their place. Channels excel when goroutines need to pass data, coordinate workflows, or signal events. Mutexes excel when you just need to protect a data structure from concurrent access with minimal overhead.

This exercise implements the same feature -- a concurrent page hit counter with event notification -- both ways, so you can develop an intuition for the tradeoffs.

## Step 1 -- The Problem: Data Race

Multiple goroutines increment a shared counter and notify listeners about page hits. Without protection, this is a data race.

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
    fmt.Printf("Counter: %d (expected 1000, likely wrong)\n", counter)
}
```

### Verification
```bash
go run -race main.go
# Expected: WARNING: DATA RACE detected, counter may not be 1000
```

The `-race` flag enables Go's race detector -- essential for finding data races during development.

## Step 2 -- Solution A: Mutex (Shared Memory)

Protect the hit counter and event notification with `sync.Mutex`. Lock before reading/writing, unlock after. This is the shared-memory approach.

```go
package main

import (
    "fmt"
    "sync"
    "time"
)

type MutexHitCounter struct {
    mu       sync.RWMutex
    counts   map[string]int
    total    int
    onChange func(page string, count int) // event callback
}

func NewMutexHitCounter(onChange func(string, int)) *MutexHitCounter {
    return &MutexHitCounter{
        counts:   make(map[string]int),
        onChange: onChange,
    }
}

func (h *MutexHitCounter) RecordHit(page string) {
    h.mu.Lock()
    h.counts[page]++
    h.total++
    count := h.counts[page]
    h.mu.Unlock()

    if h.onChange != nil {
        h.onChange(page, count)
    }
}

func (h *MutexHitCounter) GetCount(page string) int {
    h.mu.RLock()
    defer h.mu.RUnlock()
    return h.counts[page]
}

func (h *MutexHitCounter) GetTotal() int {
    h.mu.RLock()
    defer h.mu.RUnlock()
    return h.total
}

func main() {
    var notifyMu sync.Mutex
    notifications := 0

    counter := NewMutexHitCounter(func(page string, count int) {
        notifyMu.Lock()
        notifications++
        notifyMu.Unlock()
    })

    var wg sync.WaitGroup
    pages := []string{"/home", "/about", "/api/users", "/api/orders", "/health"}
    start := time.Now()

    for i := 0; i < 1000; i++ {
        wg.Add(1)
        go func(n int) {
            defer wg.Done()
            page := pages[n%len(pages)]
            counter.RecordHit(page)
        }(i)
    }

    wg.Wait()
    elapsed := time.Since(start)

    fmt.Println("=== Mutex Version ===")
    fmt.Printf("Total hits: %d\n", counter.GetTotal())
    for _, page := range pages {
        fmt.Printf("  %-15s %d hits\n", page, counter.GetCount(page))
    }
    fmt.Printf("Notifications sent: %d\n", notifications)
    fmt.Printf("Time: %v\n", elapsed)
}
```

**Pros:** Direct, low overhead, familiar pattern. `RWMutex` allows concurrent reads.
**Cons:** Easy to forget the lock. Callback (`onChange`) runs under the caller's goroutine, which could cause issues if it is slow. Must protect the notification counter with a separate mutex.

### Verification
```bash
go run -race main.go
# Expected: Total hits: 1000, no race warnings
```

## Step 3 -- Solution B: Channels (Share Memory by Communicating)

Send hit events to a single goroutine that owns the counter state. Notifications flow through a separate channel. No shared memory, no mutexes.

```go
package main

import (
    "fmt"
    "sync"
    "time"
)

type HitEvent struct {
    Page string
}

type QueryRequest struct {
    Page  string
    Reply chan int
}

type TotalRequest struct {
    Reply chan int
}

func hitCounterService(
    hits <-chan HitEvent,
    queries <-chan QueryRequest,
    totals <-chan TotalRequest,
    notifications chan<- HitEvent,
) {
    counts := make(map[string]int)
    total := 0

    for {
        select {
        case hit, ok := <-hits:
            if !ok {
                close(notifications)
                return
            }
            counts[hit.Page]++
            total++
            notifications <- hit
        case q := <-queries:
            q.Reply <- counts[q.Page]
        case t := <-totals:
            t.Reply <- total
        }
    }
}

func main() {
    hits := make(chan HitEvent, 100)
    queries := make(chan QueryRequest)
    totals := make(chan TotalRequest)
    notifications := make(chan HitEvent, 100)

    go hitCounterService(hits, queries, totals, notifications)

    // Notification consumer.
    var notifyCount int
    notifyDone := make(chan struct{})
    go func() {
        for range notifications {
            notifyCount++
        }
        notifyDone <- struct{}{}
    }()

    var wg sync.WaitGroup
    pages := []string{"/home", "/about", "/api/users", "/api/orders", "/health"}
    start := time.Now()

    for i := 0; i < 1000; i++ {
        wg.Add(1)
        go func(n int) {
            defer wg.Done()
            page := pages[n%len(pages)]
            hits <- HitEvent{Page: page}
        }(i)
    }

    wg.Wait()
    close(hits)
    <-notifyDone
    elapsed := time.Since(start)

    fmt.Println("=== Channel Version ===")
    reply := make(chan int, 1)
    totals <- TotalRequest{Reply: reply}
    fmt.Printf("Total hits: %d\n", <-reply)

    for _, page := range pages {
        queries <- QueryRequest{Page: page, Reply: reply}
        fmt.Printf("  %-15s %d hits\n", page, <-reply)
    }
    fmt.Printf("Notifications sent: %d\n", notifyCount)
    fmt.Printf("Time: %v\n", elapsed)
}
```

**Pros:** No shared state, impossible to forget locking, notifications flow naturally as a channel stream. The counter goroutine is fully self-contained.
**Cons:** More boilerplate, channel operations have overhead, queries require request-response pattern.

### Verification
```bash
go run -race main.go
# Expected: Total hits: 1000, no race warnings
```

## Step 4 -- Side-by-Side: Same Feature, Both Approaches

Run both implementations in the same program to compare behavior and timing.

```go
package main

import (
    "fmt"
    "sync"
    "time"
)

// --- Mutex version ---

type MutexCounter struct {
    mu     sync.Mutex
    counts map[string]int
    events int
}

func (c *MutexCounter) Record(page string) {
    c.mu.Lock()
    c.counts[page]++
    c.events++
    c.mu.Unlock()
}

func (c *MutexCounter) Snapshot() (map[string]int, int) {
    c.mu.Lock()
    defer c.mu.Unlock()
    snap := make(map[string]int)
    for k, v := range c.counts {
        snap[k] = v
    }
    return snap, c.events
}

// --- Channel version ---

type ChanCounter struct {
    hits    chan string
    snapReq chan chan counterSnapshot
}

type counterSnapshot struct {
    Counts map[string]int
    Events int
}

func NewChanCounter() *ChanCounter {
    c := &ChanCounter{
        hits:    make(chan string, 100),
        snapReq: make(chan chan counterSnapshot),
    }
    go c.run()
    return c
}

func (c *ChanCounter) run() {
    counts := make(map[string]int)
    events := 0
    for {
        select {
        case page, ok := <-c.hits:
            if !ok {
                return
            }
            counts[page]++
            events++
        case reply := <-c.snapReq:
            snap := make(map[string]int)
            for k, v := range counts {
                snap[k] = v
            }
            reply <- counterSnapshot{Counts: snap, Events: events}
        }
    }
}

func (c *ChanCounter) Record(page string) { c.hits <- page }
func (c *ChanCounter) Snapshot() (map[string]int, int) {
    reply := make(chan counterSnapshot, 1)
    c.snapReq <- reply
    s := <-reply
    return s.Counts, s.Events
}

func benchmark(name string, record func(string), snapshot func() (map[string]int, int)) time.Duration {
    var wg sync.WaitGroup
    pages := []string{"/home", "/about", "/api/users", "/api/orders", "/health"}
    start := time.Now()

    for i := 0; i < 10000; i++ {
        wg.Add(1)
        go func(n int) {
            defer wg.Done()
            record(pages[n%len(pages)])
        }(i)
    }

    wg.Wait()
    elapsed := time.Since(start)

    counts, events := snapshot()
    fmt.Printf("=== %s ===\n", name)
    fmt.Printf("  Total events: %d\n", events)
    for _, page := range pages {
        fmt.Printf("  %-15s %d\n", page, counts[page])
    }
    fmt.Printf("  Time: %v\n\n", elapsed)
    return elapsed
}

func main() {
    mc := &MutexCounter{counts: make(map[string]int)}
    mutexTime := benchmark("Mutex", mc.Record, mc.Snapshot)

    cc := NewChanCounter()
    chanTime := benchmark("Channel", cc.Record, cc.Snapshot)
    close(cc.hits)

    fmt.Println("=== Comparison ===")
    fmt.Printf("  Mutex:   %v\n", mutexTime)
    fmt.Printf("  Channel: %v\n", chanTime)
    if mutexTime < chanTime {
        fmt.Println("  Mutex is faster (expected for simple state guarding)")
    } else {
        fmt.Println("  Channel is faster (unusual for this workload)")
    }
}
```

### Verification
```bash
go run -race main.go
# Expected: both produce 10000 total events, mutex is typically faster
```

## Step 5 -- When to Use Which

| Use Channels When | Use Mutexes When |
|---|---|
| Passing ownership of data between goroutines | Protecting internal state of a struct |
| Coordinating multiple goroutines (fan-out, pipeline) | Simple read/write protection (counters, caches) |
| Signaling events (done, quit, ready) | Performance-critical hot paths |
| The "server" pattern (request-response) | You need RWMutex for read-heavy workloads |
| You want to compose concurrency operations | The protected section is small and self-contained |
| Event notification streams need to flow naturally | You only guard access, not coordinate workflow |

**Rule of thumb:** If you are transferring data or coordinating work, use channels. If you are guarding access to a shared data structure, use a mutex. Most real Go programs use both tools where each is most appropriate.

### The Real Consequence

**Without either protection:** Data races corrupt your counters. You report wrong analytics, bill customers incorrectly, or lose events. The `-race` flag catches this in development, but in production it is silent data corruption.

**With mutex when channels are better:** You end up with polling loops, condition variables for notifications, and complex lock ordering. The code becomes fragile and hard to extend.

**With channels when mutex is simpler:** You write 50 lines of channel plumbing for what a 3-line mutex block would handle. Over-engineering.

## Intermediate Verification

Run both versions with `-race` and confirm:
1. Both produce correct counts (no races)
2. The mutex version is typically faster for pure state guarding
3. The channel version handles event notification more naturally
4. Neither has data races

## Common Mistakes

### Using Channels Where a Mutex Is Simpler

**Wrong:** 50 lines of channel plumbing just to increment a counter with no event notification.

**Fix:** Use `sync.Mutex` or `sync/atomic` for simple state protection. Do not over-engineer.

### Using Mutex Where Channels Communicate Better

**Wrong:**
```go
var mu sync.Mutex
var buffer []string

// Producer:
mu.Lock()
buffer = append(buffer, entry)
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

**Fix:** Use a channel. It is a built-in thread-safe queue with blocking semantics -- no polling needed.

## Verify What You Learned
1. For a simple in-memory cache (get/set), which approach would you choose and why?
2. For a pipeline that processes log entries through filter/transform/output stages, which approach and why?
3. What is the real danger of using neither protection for shared state?

## What's Next
You have completed the channels section. Continue to the select and multiplexing section to learn how `select` lets you wait on multiple channel operations simultaneously.

## Summary
- Both channels and mutexes solve concurrency problems; they serve different purposes
- Channels are for communication and coordination between goroutines
- Mutexes are for protecting shared state within a goroutine or struct
- For simple state (counters, caches), mutexes are often clearer and faster
- For workflows (pipelines, fan-out, request-response, event notification), channels are cleaner
- "Share memory by communicating" is a design preference, not an absolute rule
- Most real Go programs use both tools where each is most appropriate

## Reference
- [Go Blog: Share Memory By Communicating](https://go.dev/blog/codelab-share)
- [Effective Go: Concurrency](https://go.dev/doc/effective_go#concurrency)
- [Go Wiki: Mutex or Channel](https://go.dev/wiki/MutexOrChannel)
- [sync package](https://pkg.go.dev/sync)
