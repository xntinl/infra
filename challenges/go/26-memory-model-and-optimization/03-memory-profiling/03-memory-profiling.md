# 3. Memory Profiling

<!--
difficulty: intermediate
concepts: [memory-profiling, heap-profiling, alloc-profiling, pprof, inuse-vs-alloc]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [cpu-profiling-with-pprof, benchmarking-basics]
-->

## Prerequisites

- Go 1.22+ installed
- Completed the CPU profiling exercise
- Familiarity with `go test -bench` and `go tool pprof`

## Learning Objectives

After completing this exercise, you will be able to:

- **Collect** heap and allocation profiles from benchmarks and running programs
- **Distinguish** between `inuse_space`, `inuse_objects`, `alloc_space`, and `alloc_objects`
- **Identify** functions responsible for the most memory allocations

## Why Memory Profiling

Memory allocation in Go triggers garbage collection. The more you allocate, the more the GC has to work, increasing latency and CPU overhead. Memory profiling shows you exactly where allocations happen so you can reduce them where it matters.

Go provides two memory profile types:
- **Heap profile** (`inuse`): shows memory currently held at the time of profiling
- **Alloc profile** (`alloc`): shows all allocations since program start, including those already freed

Both are valuable. Heap profiles help find memory leaks. Alloc profiles help find allocation-heavy code paths that stress the GC.

## Step 1 -- Create a Program with Allocation Hotspots

```bash
mkdir -p ~/go-exercises/mem-profile && cd ~/go-exercises/mem-profile
go mod init mem-profile
```

Create `allocator.go`:

```go
package main

import "fmt"

// LeakyTransform builds results inefficiently.
func LeakyTransform(input []string) []string {
	var results []string // no pre-allocation
	for _, s := range input {
		// each iteration may grow the slice, causing re-allocation
		results = append(results, fmt.Sprintf("processed-%s", s))
	}
	return results
}

// MapBuilder creates a large map with string keys.
func MapBuilder(n int) map[string]int {
	m := make(map[string]int)
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("key-%d", i) // allocates a new string each time
		m[key] = i
	}
	return m
}

// BufferChurn creates and discards byte slices.
func BufferChurn(iterations int) {
	for i := 0; i < iterations; i++ {
		buf := make([]byte, 4096) // allocated every iteration
		buf[0] = byte(i)
		_ = buf
	}
}
```

Create `allocator_test.go`:

```go
package main

import "testing"

func BenchmarkLeakyTransform(b *testing.B) {
	input := make([]string, 100)
	for i := range input {
		input[i] = fmt.Sprintf("item-%d", i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		LeakyTransform(input)
	}
}

func BenchmarkMapBuilder(b *testing.B) {
	for i := 0; i < b.N; i++ {
		MapBuilder(1000)
	}
}

func BenchmarkBufferChurn(b *testing.B) {
	for i := 0; i < b.N; i++ {
		BufferChurn(100)
	}
}
```

## Step 2 -- Collect a Memory Profile

Run benchmarks with memory profiling:

```bash
go test -bench=. -memprofile=mem.prof -benchmem
```

The `-benchmem` flag shows allocations per operation in the benchmark output.

### Intermediate Verification

The benchmark output includes `B/op` (bytes per operation) and `allocs/op` columns. `MapBuilder` should show the highest allocation count.

## Step 3 -- Analyze the Heap Profile

Open the profile:

```bash
go tool pprof -alloc_space mem.prof
```

At the `(pprof)` prompt:

```
top10
```

This shows functions ranked by total bytes allocated. Try `alloc_objects` mode:

```bash
go tool pprof -alloc_objects mem.prof
```

```
top10
```

### Intermediate Verification

`alloc_space` shows `MapBuilder` and `LeakyTransform` as top allocators by bytes. `alloc_objects` shows which functions create the most individual heap objects. `BufferChurn` creates many 4KB objects.

## Step 4 -- Compare inuse vs alloc

For a running program, `inuse_space` shows what's currently on the heap. Capture both from an HTTP server:

Create `server.go`:

```go
package main

import (
	"net/http"
	_ "net/http/pprof"
)

func main() {
	// Keep map in memory to show up in inuse profile
	bigMap := MapBuilder(100_000)
	_ = bigMap

	http.HandleFunc("/churn", func(w http.ResponseWriter, r *http.Request) {
		BufferChurn(1000)
		w.Write([]byte("done\n"))
	})
	http.ListenAndServe(":8080", nil)
}
```

Run the server and hit the endpoint, then capture profiles:

```bash
# Terminal 1
go run server.go allocator.go

# Terminal 2 - generate load
for i in $(seq 1 50); do curl -s http://localhost:8080/churn > /dev/null; done

# Capture heap profile (inuse by default)
go tool pprof http://localhost:8080/debug/pprof/heap

# At (pprof) prompt:
# top10
# Then try: top10 -alloc_space
```

### Intermediate Verification

In `inuse_space` mode, `MapBuilder` dominates because its map stays alive. In `alloc_space` mode, `BufferChurn` also appears because it allocates heavily even though buffers are quickly freed.

## Common Mistakes

| Mistake | Why It Fails |
|---------|-------------|
| Only looking at `inuse` profiles | Misses allocation-heavy paths that cause GC pressure |
| Ignoring `allocs/op` in benchmark output | Raw throughput can hide allocation costs |
| Not using `-benchmem` flag | Allocation counts are not shown by default |
| Confusing bytes allocated with bytes retained | `alloc_space` counts total allocations; `inuse_space` counts current heap |

## Verify What You Learned

1. What is the difference between `alloc_space` and `inuse_space` in a memory profile?
2. How do you enable allocation reporting in benchmark output?
3. Why would `BufferChurn` show up in `alloc_space` but not in `inuse_space`?
4. What HTTP endpoint serves the heap profile when using `net/http/pprof`?

## What's Next

Now that you can find where allocations happen, the next exercise covers benchmarking methodology to measure the impact of your optimizations rigorously.

## Summary

Memory profiling in Go uses `runtime/pprof` or `net/http/pprof` to capture heap snapshots. Profiles can be analyzed in `inuse_space` mode (current heap) or `alloc_space` mode (cumulative allocations). Use `-benchmem` with benchmarks to see `B/op` and `allocs/op`. Reducing allocations reduces GC pressure and improves latency.

## Reference

- [runtime/pprof package](https://pkg.go.dev/runtime/pprof)
- [Go Blog: Profiling Go Programs](https://go.dev/blog/pprof)
- [go tool pprof documentation](https://pkg.go.dev/cmd/pprof)
