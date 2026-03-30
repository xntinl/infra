---
difficulty: intermediate
concepts: [sync.Pool, Get, Put, object reuse, GC pressure, buffer pooling]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [goroutines, sync.Mutex, sync.WaitGroup]
---

# 5. Pool: Object Reuse


## Learning Objectives
After completing this exercise, you will be able to:
- **Use** `sync.Pool` to reuse objects and reduce allocation pressure
- **Implement** the `New` function for automatic pool population
- **Apply** the Get/Reset/Put lifecycle correctly
- **Recognize** that `sync.Pool` is not a permanent cache -- GC may clear it

## Why sync.Pool
Every allocation in Go eventually becomes work for the garbage collector. In high-throughput systems -- HTTP servers handling thousands of requests per second, log formatters writing thousands of lines per second, JSON encoders serializing responses -- creating and discarding temporary `bytes.Buffer` objects generates significant GC pressure. This increases latency through GC pauses and wastes CPU time on collection.

Consider an HTTP server: each request needs a buffer to build the response body. At 10,000 requests per second, that is 10,000 buffer allocations per second, each one becoming garbage immediately after the response is sent. The GC has to track and reclaim all of them.

`sync.Pool` provides a thread-safe cache of reusable objects. Instead of allocating a new buffer for each request and letting the GC reclaim it, you `Get` a buffer from the pool, use it, `Reset` it, and `Put` it back when done. The next request gets the recycled buffer instead of allocating fresh memory.

Key characteristics of `sync.Pool`:
- Objects in the pool may be garbage collected at any time (between GC cycles). The pool is not a permanent cache.
- The `New` function creates a fresh object when the pool is empty.
- `Get` and `Put` are safe for concurrent use without external synchronization.
- Objects returned by `Get` must be reset before use -- the pool does not clear them.

## Step 1 -- Create a Buffer Pool for HTTP Responses

Each HTTP handler needs a `bytes.Buffer` to build its response. The pool provides reusable buffers:

```go
package main

import (
	"bytes"
	"fmt"
	"sync"
)

func main() {
	bufferPool := &sync.Pool{
		New: func() any {
			fmt.Println("[pool] Allocating new bytes.Buffer")
			return new(bytes.Buffer)
		},
	}

	// First Get: pool empty, calls New
	buf := bufferPool.Get().(*bytes.Buffer)
	fmt.Printf("Got buffer: len=%d\n", buf.Len())

	// Simulate building an HTTP response
	buf.WriteString(`{"status":"ok","data":[1,2,3]}`)
	fmt.Printf("After write: len=%d\n", buf.Len())

	// Reset and return to pool
	buf.Reset()
	bufferPool.Put(buf)

	// Second Get: reuses the pooled buffer (no allocation)
	buf2 := bufferPool.Get().(*bytes.Buffer)
	fmt.Printf("Recycled buffer: len=%d (reset worked)\n", buf2.Len())
	bufferPool.Put(buf2)
}
```

Expected output:
```
[pool] Allocating new bytes.Buffer
Got buffer: len=0
After write: len=30
Recycled buffer: len=0 (reset worked)
```

### Intermediate Verification
```bash
go run main.go
```
The second Get should NOT trigger "Allocating new bytes.Buffer" -- it reuses the pooled object.

## Step 2 -- Concurrent Buffer Pool Under Load

Simulate an HTTP server handling 100 concurrent requests. Each request gets a buffer, builds a response, and returns the buffer:

```go
package main

import (
	"bytes"
	"fmt"
	"sync"
	"sync/atomic"
)

func main() {
	var allocCount atomic.Int64

	bufferPool := &sync.Pool{
		New: func() any {
			allocCount.Add(1)
			return new(bytes.Buffer)
		},
	}

	var wg sync.WaitGroup
	const totalRequests = 100
	const requestsPerHandler = 5

	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		go func(requestID int) {
			defer wg.Done()
			for j := 0; j < requestsPerHandler; j++ {
				buf := bufferPool.Get().(*bytes.Buffer)
				buf.Reset() // always reset before use

				// Build response
				fmt.Fprintf(buf, `{"request":%d,"iteration":%d,"status":"ok"}`, requestID, j)

				// In real code: write buf.Bytes() to http.ResponseWriter
				_ = buf.Bytes()

				buf.Reset()
				bufferPool.Put(buf)
			}
		}(i)
	}

	wg.Wait()
	totalGetCalls := totalRequests * requestsPerHandler
	fmt.Printf("Total Get() calls: %d\n", totalGetCalls)
	fmt.Printf("Actual allocations: %d\n", allocCount.Load())
	fmt.Printf("Reuse rate: %.1f%%\n", float64(totalGetCalls-int(allocCount.Load()))/float64(totalGetCalls)*100)
}
```

Expected output:
```
Total Get() calls: 500
Actual allocations: ~50
Reuse rate: ~90.0%
```

### Intermediate Verification
```bash
go run main.go
```
The allocation count should be much less than 500. The pool reuses buffers across goroutines.

## Step 3 -- Realistic HTTP Response Builder

The full pattern: get a buffer from the pool, build the response, copy the result out, reset, and return the buffer. This is exactly how production HTTP middleware works:

```go
package main

import (
	"bytes"
	"fmt"
	"sync"
)

var responsePool = &sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

func buildJSONResponse(userID int, name string) []byte {
	buf := responsePool.Get().(*bytes.Buffer)
	buf.Reset()

	// Build the JSON response in the pooled buffer
	buf.WriteString(`{"user_id":`)
	fmt.Fprintf(buf, "%d", userID)
	buf.WriteString(`,"name":"`)
	buf.WriteString(name)
	buf.WriteString(`","status":"active"}`)

	// CRITICAL: copy before returning the buffer to the pool.
	// After Put, another goroutine may overwrite the buffer contents.
	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())

	buf.Reset()
	responsePool.Put(buf)
	return result
}

func main() {
	users := []struct {
		ID   int
		Name string
	}{
		{1, "Alice"}, {2, "Bob"}, {3, "Carol"}, {4, "Dave"}, {5, "Eve"},
	}

	var wg sync.WaitGroup
	responses := make([][]byte, len(users))

	for i, u := range users {
		wg.Add(1)
		go func(idx int, id int, name string) {
			defer wg.Done()
			responses[idx] = buildJSONResponse(id, name)
		}(i, u.ID, u.Name)
	}

	wg.Wait()

	fmt.Println("=== HTTP Responses ===")
	for _, resp := range responses {
		fmt.Printf("  %s\n", resp)
	}
}
```

Expected output:
```
=== HTTP Responses ===
  {"user_id":1,"name":"Alice","status":"active"}
  {"user_id":2,"name":"Bob","status":"active"}
  {"user_id":3,"name":"Carol","status":"active"}
  {"user_id":4,"name":"Dave","status":"active"}
  {"user_id":5,"name":"Eve","status":"active"}
```

### Intermediate Verification
```bash
go run -race main.go
```
Each goroutine should produce a valid JSON response with no race warnings.

## Step 4 -- Benchmarking: Pool vs Fresh Allocation

Measure the actual difference. This benchmark simulates 10,000 requests with and without pooling:

```go
package main

import (
	"bytes"
	"fmt"
	"runtime"
	"sync"
	"time"
)

func benchWithoutPool(iterations int) (time.Duration, uint64) {
	var memBefore runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memBefore)

	start := time.Now()
	var wg sync.WaitGroup

	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			buf := new(bytes.Buffer) // fresh allocation every time
			fmt.Fprintf(buf, `{"request":%d,"data":"some payload here for request %d"}`, id, id)
			_ = buf.Bytes()
			// buf becomes garbage -- GC must reclaim it
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)

	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)
	return elapsed, memAfter.TotalAlloc - memBefore.TotalAlloc
}

func benchWithPool(iterations int) (time.Duration, uint64) {
	pool := &sync.Pool{
		New: func() any { return new(bytes.Buffer) },
	}

	var memBefore runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memBefore)

	start := time.Now()
	var wg sync.WaitGroup

	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			buf := pool.Get().(*bytes.Buffer)
			buf.Reset()
			fmt.Fprintf(buf, `{"request":%d,"data":"some payload here for request %d"}`, id, id)
			_ = buf.Bytes()
			buf.Reset()
			pool.Put(buf) // returned for reuse
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)

	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)
	return elapsed, memAfter.TotalAlloc - memBefore.TotalAlloc
}

func main() {
	const iterations = 10000

	fmt.Printf("Benchmark: %d simulated HTTP requests\n\n", iterations)

	timeNP, allocNP := benchWithoutPool(iterations)
	timeP, allocP := benchWithPool(iterations)

	fmt.Printf("Without pool: %v, ~%d KB allocated\n", timeNP.Round(time.Millisecond), allocNP/1024)
	fmt.Printf("With pool:    %v, ~%d KB allocated\n", timeP.Round(time.Millisecond), allocP/1024)

	if allocP < allocNP {
		fmt.Printf("\nPool reduced allocations by %.0f%%\n", float64(allocNP-allocP)/float64(allocNP)*100)
	}
}
```

Expected output (values vary):
```
Benchmark: 10000 simulated HTTP requests

Without pool: 45ms, ~2048 KB allocated
With pool:    30ms, ~512 KB allocated

Pool reduced allocations by 75%
```

### Intermediate Verification
```bash
go run main.go
```
The pooled version should show significantly fewer allocations.

## Common Mistakes

### Forgetting to Reset Before Put

```go
buf := pool.Get().(*bytes.Buffer)
buf.WriteString(sensitiveUserData)
pool.Put(buf) // WRONG: next Get receives buffer with old data!
```

**What happens:** The next consumer gets a buffer containing leftover data from the previous user. This can leak sensitive information (auth tokens, PII) between requests.

**Fix:** Always `buf.Reset()` before `pool.Put(buf)`.

### Using After Put

```go
buf := pool.Get().(*bytes.Buffer)
pool.Put(buf)
buf.WriteString(data) // WRONG: another goroutine may already be using this buffer
```

**What happens:** Use-after-free semantics. Once you Put an object back, you must not use it. Another goroutine may have already called Get and started writing to the same buffer.

### Using Pool as a Permanent Cache
**Wrong:** Expecting objects to persist across GC cycles.

**Reality:** The GC may clear the pool at any time. `sync.Pool` is for reducing allocations, not for caching data. If you need a permanent cache, use a map with a mutex.

### Pool of Large Objects Without Size Limits
Pools that store arbitrarily large buffers can waste memory. If one request causes a buffer to grow to 10 MB, that oversized buffer stays in the pool. Consider capping the size:
```go
buf.Reset()
if buf.Cap() > maxBufSize {
    return // let GC reclaim oversized buffers
}
pool.Put(buf)
```

## Verify What You Learned

Build a log formatting system using `sync.Pool`. Create a pool of `bytes.Buffer` objects. Implement a `FormatLog(level, message string) string` function that gets a buffer from the pool, formats a log line with timestamp and level, copies the result, resets, and returns the buffer. Run 1000 concurrent log operations and verify correctness. Benchmark against a version that allocates a fresh buffer each time.

## What's Next
Continue to [06-cond-signal-broadcast](../06-cond-signal-broadcast/06-cond-signal-broadcast.md) to learn how `sync.Cond` enables goroutines to wait for and signal conditions.

## Summary
- `sync.Pool` is a thread-safe cache of reusable objects that reduces GC pressure
- The primary use case is `bytes.Buffer` pooling in HTTP servers and log formatters
- The `New` function creates fresh objects when the pool is empty
- Always `Reset()` objects before calling `Put` to avoid data leakage between requests
- Never use an object after calling `Put` -- another goroutine may receive it
- Copy data out of pooled buffers before returning them: `copy(result, buf.Bytes())`
- Pool contents may be cleared by the GC at any time -- it is not a permanent cache
- Cap oversized buffers to prevent memory waste

## Reference
- [sync.Pool documentation](https://pkg.go.dev/sync#Pool)
- [Go Blog: Profiling Go Programs](https://go.dev/blog/pprof)
- [bytes.Buffer with sync.Pool](https://pkg.go.dev/bytes#Buffer)
