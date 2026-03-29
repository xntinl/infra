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
Every allocation in Go eventually becomes work for the garbage collector. In high-throughput systems -- HTTP servers handling thousands of requests per second, parsers processing streams of data, encoders serializing messages -- creating and discarding temporary objects generates significant GC pressure. This increases latency through GC pauses and wastes CPU time on collection.

`sync.Pool` provides a thread-safe cache of reusable objects. Instead of allocating a new buffer for each request and letting the GC reclaim it, you `Get` a buffer from the pool, use it, and `Put` it back when done. The next goroutine that needs a buffer gets the recycled one instead of allocating fresh memory.

Key characteristics of `sync.Pool`:
- Objects in the pool may be garbage collected at any time (between GC cycles). The pool is not a permanent cache.
- The `New` function creates a fresh object when the pool is empty.
- `Get` and `Put` are safe for concurrent use without external synchronization.
- Objects returned by `Get` must be reset before use -- the pool does not clear them.

## Step 1 -- Create a Pool with New

The `New` function is called when `Get` finds the pool empty:

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	pool := &sync.Pool{
		New: func() any {
			fmt.Println("[pool] Allocating new buffer")
			buf := make([]byte, 0, 1024)
			return &buf
		},
	}

	// First Get: pool empty, calls New
	bufPtr := pool.Get().(*[]byte)
	buf := *bufPtr
	fmt.Printf("Got buffer: len=%d, cap=%d\n", len(buf), cap(buf))

	// Use, reset, return
	buf = append(buf, []byte("hello")...)
	buf = buf[:0] // reset
	*bufPtr = buf
	pool.Put(bufPtr)

	// Second Get: reuses the pooled buffer
	bufPtr2 := pool.Get().(*[]byte)
	buf2 := *bufPtr2
	fmt.Printf("Recycled buffer: len=%d, cap=%d\n", len(buf2), cap(buf2))
	pool.Put(bufPtr2)
}
```

Expected output:
```
[pool] Allocating new buffer
Got buffer: len=0, cap=1024
Recycled buffer: len=0, cap=1024
```

### Intermediate Verification
```bash
go run main.go
```
The second Get should NOT trigger "Allocating new buffer" -- it reuses the pooled object.

## Step 2 -- Concurrent Pool Usage

The pool handles concurrent Get/Put safely:

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

func main() {
	var allocCount atomic.Int64
	pool := &sync.Pool{
		New: func() any {
			allocCount.Add(1)
			buf := make([]byte, 0, 1024)
			return &buf
		},
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				bufPtr := pool.Get().(*[]byte)
				buf := *bufPtr
				buf = append(buf, []byte(fmt.Sprintf("g-%d-i-%d", id, j))...)
				buf = buf[:0]
				*bufPtr = buf
				pool.Put(bufPtr)
			}
		}(i)
	}

	wg.Wait()
	fmt.Printf("Allocations: %d (out of 100 total Get calls)\n", allocCount.Load())
}
```

Expected output:
```
Allocations: ~20 (out of 100 total Get calls)
```

### Intermediate Verification
```bash
go run main.go
```
The allocation count should be much less than 100 (20 goroutines * 5 iterations).

## Step 3 -- Realistic Use Case: JSON Response Builder

The key pattern: build in a pooled buffer, copy the result, return the buffer.

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
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
		buf = append(buf, []byte(fmt.Sprintf(`"user_id":%d,"status":"ok"`, userID))...)
		buf = append(buf, '}')

		// CRITICAL: copy before returning the buffer to the pool
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
			fmt.Printf("User %d: %s\n", id, resp)
		}(i)
	}
	wg.Wait()
}
```

Expected output:
```
User 0: {"user_id":0,"status":"ok"}
User 1: {"user_id":1,"status":"ok"}
...
```

### Intermediate Verification
```bash
go run main.go
```
Each goroutine should produce a valid JSON response.

## Step 4 -- GC Clears the Pool

The program demonstrates that `runtime.GC()` can clear pool contents:

```bash
go run main.go
```

Expected output:
```
Before GC: got recycled buffer (no new allocation)
After GC: pool was cleared, had to allocate a new buffer
```

This is why `sync.Pool` is NOT a permanent cache. If you need objects to persist across GC cycles, use a map with a mutex instead.

## Common Mistakes

### Forgetting to Reset Before Put

```go
bufPtr := pool.Get().(*[]byte)
buf := *bufPtr
buf = append(buf, sensitiveData...)
*bufPtr = buf
pool.Put(bufPtr) // WRONG: next Get receives buffer with old data!
```

**What happens:** The next consumer gets a buffer containing leftover data from the previous user. This can leak sensitive information.

**Fix:** Always reset (`buf = buf[:0]`) before `Put`.

### Using After Put

```go
bufPtr := pool.Get().(*[]byte)
pool.Put(bufPtr)
*bufPtr = append(*bufPtr, data...) // WRONG: another goroutine may already be using it
```

**What happens:** Use-after-free semantics. Once you Put an object back, you must not use it.

### Using Pool as a Permanent Cache
**Wrong:** Expecting objects to persist across GC cycles.

**Reality:** The GC may clear the pool at any time. `sync.Pool` is for reducing allocations, not for caching. If you need a permanent cache, use a map with a mutex.

### Pool of Large Objects Without Size Limits
Pools that store arbitrarily large objects can waste memory. Consider capping the size:
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
