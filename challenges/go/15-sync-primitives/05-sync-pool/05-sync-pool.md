# 5. sync.Pool

<!--
difficulty: intermediate
concepts: [sync-pool, get-put, allocation-reduction, gc-interaction, object-reuse]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [sync-mutex, goroutines, garbage-collection-basics]
-->

## Prerequisites

- Go 1.22+ installed
- Familiarity with goroutines and `sync.WaitGroup`
- Basic understanding of memory allocation and garbage collection

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** how `sync.Pool` reduces allocation pressure
- **Apply** `Get` and `Put` to reuse temporary objects
- **Identify** when `sync.Pool` is appropriate and when it is not

## Why sync.Pool

In high-throughput Go programs, allocating and freeing objects rapidly puts pressure on the garbage collector. `sync.Pool` provides a cache of temporary objects that can be reused across goroutines. When you `Get` from a pool, it either returns a previously pooled object or creates a new one via the `New` function. When you `Put` an object back, it becomes available for another goroutine.

The pool is cleared at each garbage collection cycle, so it is only suitable for temporary, short-lived objects -- byte buffers, encoder instances, or scratch structs. It is not a cache for long-lived data.

The standard library uses `sync.Pool` extensively: `fmt` pools print buffers, `encoding/json` pools encoders, and `net/http` pools request structs.

## Step 1 -- Basic Pool Usage

```bash
mkdir -p ~/go-exercises/sync-pool
cd ~/go-exercises/sync-pool
go mod init sync-pool
```

Create `main.go`:

```go
package main

import (
	"bytes"
	"fmt"
	"sync"
)

var bufPool = sync.Pool{
	New: func() any {
		fmt.Println("  allocating new buffer")
		return new(bytes.Buffer)
	},
}

func main() {
	// Get a buffer from the pool (triggers New)
	buf := bufPool.Get().(*bytes.Buffer)
	buf.WriteString("hello world")
	fmt.Println("Buffer:", buf.String())

	// Reset and return to pool
	buf.Reset()
	bufPool.Put(buf)

	// Get again -- reuses the pooled buffer
	buf2 := bufPool.Get().(*bytes.Buffer)
	fmt.Println("Reused buffer len:", buf2.Len())

	buf2.Reset()
	bufPool.Put(buf2)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
  allocating new buffer
Buffer: hello world
Reused buffer len: 0
```

The "allocating" message appears only once because the second `Get` reuses the pooled buffer.

## Step 2 -- Pool in Concurrent Workload

Use a pool to reuse byte buffers across multiple goroutines:

```go
package main

import (
	"bytes"
	"fmt"
	"sync"
	"sync/atomic"
)

func main() {
	var allocations atomic.Int64

	pool := sync.Pool{
		New: func() any {
			allocations.Add(1)
			return new(bytes.Buffer)
		},
	}

	var wg sync.WaitGroup
	iterations := 1000

	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			buf := pool.Get().(*bytes.Buffer)
			buf.Reset()
			fmt.Fprintf(buf, "message-%d", id)
			_ = buf.String()
			pool.Put(buf)
		}(i)
	}

	wg.Wait()
	fmt.Printf("Iterations: %d\n", iterations)
	fmt.Printf("Allocations: %d\n", allocations.Load())
	fmt.Printf("Reuse rate: %.1f%%\n",
		float64(iterations-int(allocations.Load()))/float64(iterations)*100)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: The number of allocations is significantly less than 1000. The exact number depends on goroutine scheduling and GC timing.

## Step 3 -- Benchmarking Pool vs Direct Allocation

Create `pool_test.go`:

```go
package main

import (
	"bytes"
	"sync"
	"testing"
)

var pool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

func BenchmarkWithPool(b *testing.B) {
	for i := 0; i < b.N; i++ {
		buf := pool.Get().(*bytes.Buffer)
		buf.Reset()
		buf.WriteString("benchmark data")
		_ = buf.String()
		pool.Put(buf)
	}
}

func BenchmarkWithoutPool(b *testing.B) {
	for i := 0; i < b.N; i++ {
		buf := new(bytes.Buffer)
		buf.WriteString("benchmark data")
		_ = buf.String()
	}
}
```

### Intermediate Verification

```bash
go test -bench=. -benchmem
```

Expected: `BenchmarkWithPool` shows fewer allocations per operation (0 or 1 allocs/op) compared to `BenchmarkWithoutPool` (1+ allocs/op). The pooled version should also be faster.

## Common Mistakes

| Mistake | Why It Fails |
|---|---|
| Not resetting objects before `Put` | Next consumer gets stale data from the previous use |
| Using pool for long-lived objects | Pool is cleared on GC; objects disappear unpredictably |
| Storing large objects without a size cap | Pool can accumulate oversized buffers, wasting memory |
| Assuming `Get` always returns a pooled object | `Get` calls `New` when the pool is empty |

## Verify What You Learned

1. Run the benchmark and compare allocations with and without the pool
2. Add `runtime.GC()` between `Put` and `Get` in the basic example and observe that the buffer is gone after GC

## What's Next

Continue to [06 - sync.Cond](../06-sync-cond/06-sync-cond.md) to learn about condition variables for producer-consumer patterns.

## Summary

- `sync.Pool` caches temporary objects for reuse, reducing allocation pressure
- `Get` returns a pooled object or calls `New` if the pool is empty
- `Put` returns an object to the pool for reuse by other goroutines
- Always reset objects before putting them back in the pool
- The pool is cleared on garbage collection -- do not use it as a cache for persistent data
- Best for short-lived objects like byte buffers, encoders, and scratch structs

## Reference

- [sync.Pool documentation](https://pkg.go.dev/sync#Pool)
- [Go blog: Profiling Go Programs](https://go.dev/blog/pprof)
