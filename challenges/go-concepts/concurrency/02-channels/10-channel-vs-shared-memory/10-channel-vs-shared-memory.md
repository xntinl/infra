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

Both tools have their place. Channels excel when goroutines need to pass data, coordinate workflows, or signal events. Mutexes excel when you just need to protect a data structure from concurrent access with minimal overhead. The key is understanding when each is appropriate and why.

This exercise puts both approaches side by side for the same problem, so you can develop an intuition for the tradeoffs. Most real Go programs use both — channels for orchestration and mutexes for low-level state protection.

## Step 1 -- The Problem: Concurrent Counter

Multiple goroutines increment a shared counter. This is the simplest concurrency problem and the best canvas for comparing approaches.

Without any protection:
```go
var counter int
var wg sync.WaitGroup

for i := 0; i < 1000; i++ {
    wg.Add(1)
    go func() {
        defer wg.Done()
        counter++ // DATA RACE!
    }()
}
wg.Wait()
fmt.Println("Counter:", counter) // wrong result, different each run
```

Run with `-race` to see the data race:
```bash
go run -race main.go
```

### Intermediate Verification
```bash
go run -race main.go
# Expected: WARNING: DATA RACE detected
```

## Step 2 -- Solution A: Mutex

Protect the counter with a `sync.Mutex`. Lock before reading/writing, unlock after.

```go
var counter int
var mu sync.Mutex
var wg sync.WaitGroup

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
fmt.Println("Counter:", counter) // always 1000
```

**Pros:** Simple, low overhead, directly protects the data.
**Cons:** Easy to forget the lock, can deadlock with multiple mutexes, doesn't compose well.

### Intermediate Verification
```bash
go run -race main.go
# Expected: Counter: 1000, no race warnings
```

## Step 3 -- Solution B: Channel

Send increment requests to a single goroutine that owns the counter. No shared memory.

```go
func counterService(ops <-chan func(int) int, queries <-chan chan int) {
    counter := 0
    for {
        select {
        case op := <-ops:
            counter = op(counter)
        case reply := <-queries:
            reply <- counter
        }
    }
}
```

Or the simpler version with a dedicated increment channel:

```go
increments := make(chan struct{}, 100)
done := make(chan struct{})

go func() {
    counter := 0
    for range increments {
        counter++
    }
    // ... report final count
}()

for i := 0; i < 1000; i++ {
    increments <- struct{}{}
}
close(increments)
```

**Pros:** No shared state, impossible to forget locking, counter goroutine is self-contained.
**Cons:** More boilerplate, overhead of channel operations, harder for simple cases.

### Intermediate Verification
```bash
go run -race main.go
# Expected: Counter: 1000, no race warnings
```

## Step 4 -- A More Complex Problem: Cache

A simple counter is biased toward mutexes (it's too simple for channels to shine). Now solve a richer problem: a concurrent cache with get/set/delete operations.

### Mutex Version
```go
type MutexCache struct {
    mu    sync.RWMutex
    items map[string]string
}

func (c *MutexCache) Get(key string) (string, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    val, ok := c.items[key]
    return val, ok
}

func (c *MutexCache) Set(key, value string) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.items[key] = value
}
```

### Channel Version
```go
type CacheRequest struct {
    Op    string
    Key   string
    Value string
    Reply chan CacheResponse
}

func cacheService(requests <-chan CacheRequest) {
    items := make(map[string]string)
    for req := range requests {
        // handle get, set, delete...
    }
}
```

Compare: which is clearer? Which is easier to extend with logging, metrics, or expiration?

### Intermediate Verification
```bash
go run -race main.go
# Both versions produce correct results with no races
```

## Step 5 -- When to Use Which

| Use Channels When | Use Mutexes When |
|---|---|
| Passing ownership of data between goroutines | Protecting internal state of a struct |
| Coordinating multiple goroutines (fan-out, pipeline) | Simple read/write protection (counters, caches) |
| Signaling events (done, quit, ready) | Performance-critical hot paths |
| The "server" pattern (request-response) | You need RWMutex for read-heavy workloads |
| You want to compose concurrency operations | The protected section is small and self-contained |

**Rule of thumb:** If you're transferring data or coordinating work, use channels. If you're guarding access to a shared data structure, use a mutex.

### Intermediate Verification
No code to run. Internalize the decision framework.

## Common Mistakes

### Using Channels Where a Mutex Is Simpler
**Wrong:**
```go
// 50 lines of channel plumbing just to increment a counter
type IncrementRequest struct { Reply chan int }
func counterService(reqs <-chan IncrementRequest) { ... }
```
**What happens:** Overengineered. The channel version is 5x more code for no benefit.
**Fix:** Use `sync.Mutex` or `sync/atomic` for simple state protection.

### Using Mutex Where Channels Communicate Better
**Wrong:**
```go
// Producer and consumer share a slice with mutex
var mu sync.Mutex
var buffer []int

// Producer:
mu.Lock()
buffer = append(buffer, value)
mu.Unlock()

// Consumer:
mu.Lock()
if len(buffer) > 0 { val = buffer[0]; buffer = buffer[1:] }
mu.Unlock()
```
**What happens:** You've reimplemented a channel poorly. The consumer must poll in a loop.
**Fix:** Use a channel. It's a built-in thread-safe queue with blocking semantics.

## Verify What You Learned

Build both a channel version and a mutex version of a **hit counter service** in `main.go`:

Requirements:
1. Track page views: `record(page string)` increments the count for a page
2. Query: `topPages(n int)` returns the top N pages by hit count
3. Launch 100 goroutines, each recording 100 random page views across 10 pages
4. After all goroutines finish, query top 3 pages from both implementations
5. Verify both produce the same total count (10,000 hits)
6. Benchmark: measure time for each approach with `time.Since`

Compare: Which was easier to write? Which is faster? Which would be easier to extend with expiration or persistence?

## What's Next
You've completed the channels section. Continue to [03-select-and-multiplexing](../../03-select-and-multiplexing/) to learn how `select` lets you wait on multiple channel operations simultaneously.

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
