# 5. Pool: Object Reuse

<!--
difficulty: intermediate
concepts: [sync.Pool, Get, Put, object reuse, GC pressure, buffer pooling]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [goroutines, sync.Mutex, sync.WaitGroup]
-->

## Prerequisites
- Go 1.22+ installed
- Understanding of goroutines and synchronization basics
- Basic awareness of garbage collection in Go

## Learning Objectives
After completing this exercise, you will be able to:
- **Use** `sync.Pool` to reuse objects and reduce allocation pressure
- **Implement** the `New` function for automatic pool population
- **Measure** the allocation reduction using `testing.AllocsPerRun` patterns
- **Apply** buffer pooling to a realistic use case

## Why sync.Pool
Every allocation in Go eventually becomes work for the garbage collector. In high-throughput systems -- HTTP servers handling thousands of requests per second, parsers processing streams of data, encoders serializing messages -- creating and discarding temporary objects generates significant GC pressure. This increases latency through GC pauses and wastes CPU time on collection.

`sync.Pool` provides a thread-safe cache of reusable objects. Instead of allocating a new buffer for each request and letting the GC reclaim it, you `Get` a buffer from the pool, use it, and `Put` it back when done. The next goroutine that needs a buffer gets the recycled one instead of allocating fresh memory.

Key characteristics of `sync.Pool`:
- Objects in the pool may be garbage collected at any time (between GC cycles). The pool is not a permanent cache.
- The `New` function creates a fresh object when the pool is empty.
- `Get` and `Put` are safe for concurrent use without external synchronization.
- Objects returned by `Get` must be reset before use -- the pool does not clear them.

## Step 1 -- Create a Pool with New

Open `main.go`. Implement the buffer pool with a `New` function:

```go
var bufferPool = sync.Pool{
    New: func() any {
        fmt.Println("  [pool] Allocating new buffer")
        buf := make([]byte, 0, 1024)
        return &buf
    },
}
```

The `New` function is called when `Get` finds the pool empty. It should return a pointer to the buffer so the pool stores a reference, not a copy.

### Intermediate Verification
```bash
go run main.go
```
The basic pool demo should show "Allocating new buffer" only when the pool is empty.

## Step 2 -- Get, Use, Reset, Put

Implement `basicPoolDemo` showing the Get/Put lifecycle:

```go
func basicPoolDemo() {
    fmt.Println("=== Basic Pool Demo ===")

    // First Get: pool is empty, calls New
    bufPtr := bufferPool.Get().(*[]byte)
    buf := *bufPtr
    fmt.Printf("Got buffer: len=%d, cap=%d\n", len(buf), cap(buf))

    // Use the buffer
    buf = append(buf, []byte("hello world")...)
    fmt.Printf("After use: len=%d, cap=%d, content=%q\n", len(buf), cap(buf), buf)

    // Reset and put back -- CRITICAL: always reset before Put
    buf = buf[:0]
    *bufPtr = buf
    bufferPool.Put(bufPtr)
    fmt.Println("Buffer returned to pool.")

    // Second Get: pool has a recycled buffer, no New call
    bufPtr2 := bufferPool.Get().(*[]byte)
    buf2 := *bufPtr2
    fmt.Printf("Got recycled buffer: len=%d, cap=%d\n", len(buf2), cap(buf2))
    *bufPtr2 = buf2
    bufferPool.Put(bufPtr2)
}
```

### Intermediate Verification
```bash
go run main.go
```
The second `Get` should NOT trigger "Allocating new buffer" -- it reuses the pooled object.

## Step 3 -- Pool Under Concurrent Load

Implement `concurrentPoolDemo` to show pooling under realistic concurrency:

```go
func concurrentPoolDemo() {
    fmt.Println("\n=== Concurrent Pool Usage ===")

    var wg sync.WaitGroup
    const numGoroutines = 20
    const iterations = 5

    for i := 0; i < numGoroutines; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            for j := 0; j < iterations; j++ {
                // Get a buffer from the pool
                bufPtr := bufferPool.Get().(*[]byte)
                buf := *bufPtr

                // Use it
                buf = append(buf, []byte(fmt.Sprintf("goroutine-%d-iter-%d", id, j))...)

                // Reset and return
                buf = buf[:0]
                *bufPtr = buf
                bufferPool.Put(bufPtr)
            }
        }(i)
    }

    wg.Wait()
    fmt.Println("All goroutines completed. Pool handled concurrent access safely.")
}
```

### Intermediate Verification
```bash
go run main.go
```
All goroutines should complete without panics. The number of "Allocating new buffer" messages should be much less than 100 (20 goroutines * 5 iterations).

## Step 4 -- Measure Allocation Savings

Implement `measureAllocations` to compare allocations with and without pooling:

```go
func measureAllocations() {
    fmt.Println("\n=== Allocation Comparison ===")

    const iterations = 10000

    // Without pool: allocate a new buffer each time
    start := time.Now()
    for i := 0; i < iterations; i++ {
        buf := make([]byte, 0, 1024)
        buf = append(buf, []byte("some data to process")...)
        _ = buf // use and discard
    }
    withoutPool := time.Since(start)

    // With pool: reuse buffers
    pool := &sync.Pool{
        New: func() any {
            buf := make([]byte, 0, 1024)
            return &buf
        },
    }

    start = time.Now()
    for i := 0; i < iterations; i++ {
        bufPtr := pool.Get().(*[]byte)
        buf := *bufPtr
        buf = append(buf, []byte("some data to process")...)
        buf = buf[:0] // reset
        *bufPtr = buf
        pool.Put(bufPtr)
    }
    withPool := time.Since(start)

    fmt.Printf("Without pool: %v (%d allocations)\n", withoutPool, iterations)
    fmt.Printf("With pool:    %v (far fewer allocations)\n", withPool)
}
```

### Intermediate Verification
```bash
go run main.go
```
The pool version should show reduced allocation count and potentially faster execution.

## Step 5 -- Realistic Use Case: JSON Response Builder

Implement `jsonResponseDemo` showing a practical buffer pool for building JSON responses:

```go
func jsonResponseDemo() {
    fmt.Println("\n=== Realistic Use Case: Response Builder ===")

    responsePool := &sync.Pool{
        New: func() any {
            buf := make([]byte, 0, 4096)
            return &buf
        },
    }

    buildResponse := func(userID int) []byte {
        bufPtr := responsePool.Get().(*[]byte)
        buf := *bufPtr

        buf = append(buf, '{')
        buf = append(buf, []byte(fmt.Sprintf(`"user_id":%d,"status":"ok","data":"payload-%d"`, userID, userID))...)
        buf = append(buf, '}')

        // Copy the result before returning the buffer to the pool
        result := make([]byte, len(buf))
        copy(result, buf)

        buf = buf[:0]
        *bufPtr = buf
        responsePool.Put(bufPtr)

        return result
    }

    var wg sync.WaitGroup
    for i := 0; i < 5; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            resp := buildResponse(id)
            fmt.Printf("Response for user %d: %s\n", id, resp)
        }(i)
    }

    wg.Wait()
}
```

### Intermediate Verification
```bash
go run main.go
```
Each goroutine should produce a valid JSON response. Buffers are recycled between responses.

## Common Mistakes

### Forgetting to Reset Before Put
**Wrong:**
```go
bufPtr := pool.Get().(*[]byte)
buf := *bufPtr
buf = append(buf, sensitiveData...)
*bufPtr = buf
pool.Put(bufPtr) // next Get receives buffer with old data!
```
**What happens:** The next consumer gets a buffer containing leftover data from the previous user.

**Fix:** Always reset (`buf = buf[:0]`) before `Put`.

### Storing the Returned Pointer After Put
**Wrong:**
```go
bufPtr := pool.Get().(*[]byte)
pool.Put(bufPtr)
*bufPtr = append(*bufPtr, data...) // another goroutine may already be using it
```
**What happens:** Use-after-free semantics. Once you Put an object back, you must not use it.

### Using Pool as a Permanent Cache
**Wrong:** Expecting objects to persist across GC cycles.

**Reality:** The GC may clear the pool at any time. `sync.Pool` is for reducing allocations, not for caching. If you need a permanent cache, use a map with a mutex.

### Pool of Large Objects Without Size Limits
Pools that store arbitrarily large objects can waste memory. Consider limiting the maximum size of objects you return to the pool:
```go
if cap(*bufPtr) > maxBufSize {
    return // let GC reclaim oversized buffers
}
pool.Put(bufPtr)
```

## Verify What You Learned

Create a pool of `bytes.Buffer` objects. Implement a concurrent log formatter that gets a buffer from the pool, formats a log line with timestamp and message, writes the result to stdout, and returns the buffer to the pool. Run 1000 concurrent log operations and verify correctness.

## What's Next
Continue to [06-cond-signal-broadcast](../06-cond-signal-broadcast/06-cond-signal-broadcast.md) to learn how `sync.Cond` enables goroutines to wait for and signal conditions.

## Summary
- `sync.Pool` is a thread-safe cache of reusable objects that reduces GC pressure
- The `New` function creates fresh objects when the pool is empty
- Always reset objects before calling `Put` to avoid data leakage
- Never use an object after calling `Put` -- another goroutine may receive it
- Pool contents may be cleared by the GC at any time -- it is not a permanent cache
- Common use cases: byte buffers, temporary structs, encoder/decoder state
- Measure with benchmarks to confirm pooling actually helps your workload

## Reference
- [sync.Pool documentation](https://pkg.go.dev/sync#Pool)
- [Go Blog: Profiling Go Programs](https://go.dev/blog/pprof)
- [bytes.Buffer with sync.Pool](https://pkg.go.dev/bytes#Buffer)
