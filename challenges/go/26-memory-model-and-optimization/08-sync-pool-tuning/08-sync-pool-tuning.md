# 8. sync.Pool Tuning

<!--
difficulty: advanced
concepts: [sync-pool, object-reuse, gc-interaction, pool-tuning, hot-paths]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [memory-profiling, benchmarking-methodology, sync-primitives]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of `sync.Pool` basics
- Familiarity with memory profiling and benchmarking

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** how `sync.Pool` interacts with the garbage collector
- **Tune** pool usage for hot paths to reduce allocation pressure
- **Avoid** common pitfalls: putting dirty objects back, pool misuse for long-lived data
- **Measure** the impact of pooling on allocation rate and throughput

## Why sync.Pool Tuning

`sync.Pool` reuses temporary objects to reduce allocation pressure. On hot paths (request handlers, parsers, encoders), creating and discarding objects every iteration generates significant GC work. A well-tuned pool eliminates most of these allocations.

However, pools are cleared on every GC cycle. They are not caches -- they provide best-effort reuse with no retention guarantees. Tuning involves choosing the right objects to pool, resetting state properly, and sizing buffers appropriately.

## The Problem

Build a JSON processing pipeline that handles high-throughput requests. Measure allocation rates with and without pooling, and tune the pool for optimal performance.

## Requirements

1. Implement a request processing pipeline with buffer pooling
2. Reset pooled objects correctly before returning them
3. Benchmark with and without pooling
4. Demonstrate the GC interaction by forcing collection mid-benchmark

## Step 1 -- Baseline Without Pooling

```bash
mkdir -p ~/go-exercises/sync-pool-tuning && cd ~/go-exercises/sync-pool-tuning
go mod init sync-pool-tuning
```

Create `processor.go`:

```go
package main

import (
	"bytes"
	"encoding/json"
	"sync"
)

type Request struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Data string `json:"data"`
}

type Response struct {
	ID      int    `json:"id"`
	Message string `json:"message"`
}

// ProcessWithoutPool allocates a new buffer every call.
func ProcessWithoutPool(req *Request) ([]byte, error) {
	resp := Response{
		ID:      req.ID,
		Message: "processed: " + req.Name,
	}
	buf := new(bytes.Buffer)
	if err := json.NewEncoder(buf).Encode(resp); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// bufPool pools bytes.Buffer instances.
var bufPool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}

// ProcessWithPool reuses buffers from the pool.
func ProcessWithPool(req *Request) ([]byte, error) {
	resp := Response{
		ID:      req.ID,
		Message: "processed: " + req.Name,
	}

	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset() // CRITICAL: always reset before use
	defer bufPool.Put(buf)

	if err := json.NewEncoder(buf).Encode(resp); err != nil {
		return nil, err
	}

	// Must copy: the buffer will be reused after Put.
	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	return result, nil
}
```

## Step 2 -- Pool Encoder Objects Too

For maximum benefit, pool the encoder and its associated buffer together:

```go
type encoderBundle struct {
	buf     *bytes.Buffer
	encoder *json.Encoder
}

var encoderPool = sync.Pool{
	New: func() interface{} {
		buf := new(bytes.Buffer)
		return &encoderBundle{
			buf:     buf,
			encoder: json.NewEncoder(buf),
		}
	},
}

func ProcessWithEncoderPool(req *Request) ([]byte, error) {
	resp := Response{
		ID:      req.ID,
		Message: "processed: " + req.Name,
	}

	bundle := encoderPool.Get().(*encoderBundle)
	bundle.buf.Reset()
	defer encoderPool.Put(bundle)

	if err := bundle.encoder.Encode(resp); err != nil {
		return nil, err
	}

	result := make([]byte, bundle.buf.Len())
	copy(result, bundle.buf.Bytes())
	return result, nil
}
```

## Step 3 -- Benchmark

Create `processor_test.go`:

```go
package main

import "testing"

var sampleRequest = &Request{
	ID:   42,
	Name: "benchmark-user",
	Data: "some payload data for testing",
}

func BenchmarkWithoutPool(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := ProcessWithoutPool(sampleRequest)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWithPool(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := ProcessWithPool(sampleRequest)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWithEncoderPool(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := ProcessWithEncoderPool(sampleRequest)
		if err != nil {
			b.Fatal(err)
		}
	}
}
```

```bash
go test -bench=. -benchmem -count=5
```

## Hints

- Always call `Reset()` on buffers retrieved from the pool
- Copy data out before calling `Put()` -- the buffer contents are no longer safe after return
- Pool objects are cleared after each GC cycle; this is by design, not a bug
- Pool `New` functions should return reasonably-sized objects (not pre-grown buffers)
- Use `b.ReportAllocs()` to see the allocation difference clearly
- If the pooled object has pointer fields, consider zeroing them to avoid holding references

## Verification

- `BenchmarkWithPool` shows fewer `allocs/op` than `BenchmarkWithoutPool`
- `BenchmarkWithEncoderPool` shows even fewer allocations
- Memory profiling confirms buffer allocations are mostly eliminated on the hot path
- The results are functionally identical (same JSON output)

## What's Next

With pooling optimized, the next exercise explores the `go tool trace` for visualizing goroutine scheduling and GC behavior.

## Summary

`sync.Pool` reduces allocation pressure on hot paths by reusing temporary objects. Always `Reset()` pooled objects before use and copy data out before returning objects to the pool. Pool objects are cleared on GC, so they work for temporary reuse only. Bundle related allocations (buffer + encoder) for maximum benefit. Always benchmark to confirm pooling provides a measurable improvement.

## Reference

- [sync.Pool documentation](https://pkg.go.dev/sync#Pool)
- [Go Blog: sync.Pool](https://go.dev/doc/effective_go#allocation_new)
