# 6. cgo Performance Overhead

<!--
difficulty: advanced
concepts: [cgo-overhead, goroutine-stack, c-thread-switch, benchmark-cgo, pure-go-alternative]
tools: [go, gcc]
estimated_time: 30m
bloom_level: analyze
prerequisites: [cgo-basics, passing-data-go-and-c, benchmarking-methodology]
-->

## Prerequisites

- Go 1.22+ installed with a working C compiler
- Completed cgo basics and data passing (exercises 4-5)
- Experience writing Go benchmarks

## Learning Objectives

After completing this exercise, you will be able to:

- **Measure** the per-call overhead of cgo function calls
- **Explain** why cgo calls are expensive (stack switching, signal masking, goroutine scheduling)
- **Determine** when the overhead justifies using cgo vs pure Go
- **Apply** batching and amortization strategies to reduce cgo call frequency

## Why Understanding cgo Overhead

Every cgo call has a fixed overhead of roughly 50-200 nanoseconds (depending on platform). This overhead comes from: (1) switching from Go's segmented/growable stack to a C-compatible fixed stack, (2) informing the Go scheduler that the goroutine is blocked in C so other goroutines can run, (3) setting up signal handlers, and (4) the function call itself through the cgo trampoline.

For a single call, 100ns is nothing. For a million calls per second in a tight loop, it is 100ms of pure overhead. Knowing the cost lets you make informed decisions: batch small C calls into one large call, or rewrite the C function in Go if it is simple enough.

## Step 1 -- Measure Raw Call Overhead

```bash
mkdir -p ~/go-exercises/cgo-perf && cd ~/go-exercises/cgo-perf
go mod init cgo-perf
```

Create `bench_test.go`:

```go
package main

import (
	"testing"
)

/*
#include <stdint.h>

// Trivial function: measures pure call overhead
int32_t cgo_noop(void) {
    return 0;
}

int32_t cgo_add(int32_t a, int32_t b) {
    return a + b;
}

// Slightly more work
int32_t cgo_sum_n(int32_t n) {
    int32_t total = 0;
    for (int32_t i = 0; i < n; i++) {
        total += i;
    }
    return total;
}
*/
import "C"

// Pure Go equivalents
func goNoop() int32 {
    return 0
}

func goAdd(a, b int32) int32 {
    return a + b
}

func goSumN(n int32) int32 {
    var total int32
    for i := int32(0); i < n; i++ {
        total += i
    }
    return total
}

// Benchmark: cgo noop vs Go noop (pure overhead measurement)
func BenchmarkCgoNoop(b *testing.B) {
    var r C.int32_t
    for i := 0; i < b.N; i++ {
        r = C.cgo_noop()
    }
    _ = r
}

func BenchmarkGoNoop(b *testing.B) {
    var r int32
    for i := 0; i < b.N; i++ {
        r = goNoop()
    }
    _ = r
}

// Benchmark: cgo add vs Go add
func BenchmarkCgoAdd(b *testing.B) {
    var r C.int32_t
    for i := 0; i < b.N; i++ {
        r = C.cgo_add(3, 4)
    }
    _ = r
}

func BenchmarkGoAdd(b *testing.B) {
    var r int32
    for i := 0; i < b.N; i++ {
        r = goAdd(3, 4)
    }
    _ = r
}
```

```bash
go test -bench=BenchmarkCgoNoop -benchmem -count=5
go test -bench=BenchmarkGoNoop -benchmem -count=5
go test -bench=BenchmarkCgoAdd -benchmem -count=5
go test -bench=BenchmarkGoAdd -benchmem -count=5
```

### Intermediate Verification

The Go noop should be sub-nanosecond (compiler may inline it to nothing). The cgo noop should be 50-200ns -- this is the raw cost of crossing the cgo boundary. The ratio reveals how many times you can afford to cross per second.

## Step 2 -- Amortization: Work vs Overhead

```go
// Benchmark with increasing work per call
func BenchmarkCgoSum10(b *testing.B) {
    for i := 0; i < b.N; i++ { C.cgo_sum_n(10) }
}
func BenchmarkGoSum10(b *testing.B) {
    for i := 0; i < b.N; i++ { goSumN(10) }
}

func BenchmarkCgoSum100(b *testing.B) {
    for i := 0; i < b.N; i++ { C.cgo_sum_n(100) }
}
func BenchmarkGoSum100(b *testing.B) {
    for i := 0; i < b.N; i++ { goSumN(100) }
}

func BenchmarkCgoSum1000(b *testing.B) {
    for i := 0; i < b.N; i++ { C.cgo_sum_n(1000) }
}
func BenchmarkGoSum1000(b *testing.B) {
    for i := 0; i < b.N; i++ { goSumN(1000) }
}

func BenchmarkCgoSum10000(b *testing.B) {
    for i := 0; i < b.N; i++ { C.cgo_sum_n(10000) }
}
func BenchmarkGoSum10000(b *testing.B) {
    for i := 0; i < b.N; i++ { goSumN(10000) }
}
```

```bash
go test -bench=BenchmarkCgoSum -benchmem
go test -bench=BenchmarkGoSum -benchmem
```

### Intermediate Verification

At N=10, cgo overhead dominates (cgo is 10-50x slower). At N=1000, the work inside C amortizes the overhead (cgo is roughly comparable). At N=10000, C may be slightly faster if the compiler optimizes the C loop better, or Go may match it with its own optimizations. The crossover point is where cgo becomes worthwhile.

## Step 3 -- Batching Strategy

Instead of calling C once per element, batch elements:

```go
/*
#include <stdint.h>

// Process one element
int32_t process_one(int32_t x) {
    return x * x + 1;
}

// Process a batch of elements
void process_batch(const int32_t* in, int32_t* out, int len) {
    for (int i = 0; i < len; i++) {
        out[i] = in[i] * in[i] + 1;
    }
}
*/
import "C"

import "unsafe"

func goBatchProcess(in []int32) []int32 {
    out := make([]int32, len(in))
    for i, x := range in {
        out[i] = x*x + 1
    }
    return out
}

func BenchmarkCgoOneByOne1000(b *testing.B) {
    data := make([]int32, 1000)
    for i := range data { data[i] = int32(i) }
    out := make([]int32, 1000)
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        for j := range data {
            out[j] = int32(C.process_one(C.int32_t(data[j])))
        }
    }
    _ = out
}

func BenchmarkCgoBatch1000(b *testing.B) {
    data := make([]int32, 1000)
    for i := range data { data[i] = int32(i) }
    out := make([]int32, 1000)
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        C.process_batch(
            (*C.int32_t)(unsafe.Pointer(&data[0])),
            (*C.int32_t)(unsafe.Pointer(&out[0])),
            C.int(len(data)),
        )
    }
    _ = out
}

func BenchmarkGoBatch1000(b *testing.B) {
    data := make([]int32, 1000)
    for i := range data { data[i] = int32(i) }
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _ = goBatchProcess(data)
    }
}
```

```bash
go test -bench=Benchmark.*1000 -benchmem
```

## Step 4 -- Goroutine Scaling Impact

```go
import (
    "sync"
    "testing"
)

func BenchmarkCgoParallel(b *testing.B) {
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            C.cgo_add(1, 2)
        }
    })
}

func BenchmarkGoParallel(b *testing.B) {
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            goAdd(1, 2)
        }
    })
}
```

```bash
go test -bench=BenchmarkCgoParallel -cpu=1,2,4,8 -benchmem
go test -bench=BenchmarkGoParallel -cpu=1,2,4,8 -benchmem
```

### Intermediate Verification

cgo calls scale less well with GOMAXPROCS because each call occupies an OS thread (Go's M). With many concurrent cgo calls, the scheduler must create more threads, increasing contention. Pure Go calls scale linearly with P's.

## Hints

- cgo overhead is ~50-200ns per call on modern hardware -- measure your specific platform
- The overhead is fixed regardless of how much work the C function does
- Batch C calls: one cgo call processing 1000 items beats 1000 cgo calls processing 1 item each
- cgo calls lock the goroutine to an OS thread -- this limits concurrency scaling
- `CGO_ENABLED=0` eliminates cgo entirely and may produce faster pure-Go builds of the standard library
- Profile with `go test -bench=. -cpuprofile=cpu.out` and examine `runtime.cgocall` time
- On hot paths (>1M calls/sec), prefer pure Go unless C provides algorithmic advantages (SIMD, specialized libraries)

## Verification

- cgo noop is 50-200x slower than Go noop, confirming the boundary-crossing cost
- As work per call increases (N=10 to N=10000), the cgo/Go ratio approaches 1:1
- Batch cgo call is 100-1000x faster than one-by-one cgo calls for 1000 elements
- Pure Go batch is competitive with cgo batch for simple arithmetic
- Parallel benchmarks show cgo scaling worse than pure Go with increasing GOMAXPROCS

## What's Next

Understanding cgo overhead informs the decision of when to use cgo vs pure Go. The next exercise covers modern `unsafe.Slice` and `unsafe.String` functions introduced in Go 1.17+.

## Summary

Every cgo call pays a fixed ~100ns overhead for stack switching, scheduler notification, and signal handling. This cost is independent of the C function's work. For simple operations, cgo is 50-200x slower than pure Go. Amortize the overhead by batching: pass arrays/slices to C for bulk processing instead of calling per-element. cgo calls also pin goroutines to OS threads, limiting concurrency. Use cgo only when C provides capabilities Go cannot match (specialized libraries, hardware access, SIMD). For pure computation, benchmark before assuming C is faster.

## Reference

- [cgo overhead discussion](https://go.dev/wiki/cgo#turning-c-arrays-into-go-slices)
- [Dave Cheney: cgo is not Go](https://dave.cheney.net/2016/01/18/cgo-is-not-go)
- [Go benchmark documentation](https://pkg.go.dev/testing#hdr-Benchmarks)
