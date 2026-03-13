# 14. Contention Profiling

<!--
difficulty: insane
concepts: [mutex-profiling, pprof, contention, runtime-metrics, block-profiling]
tools: [go, pprof]
estimated_time: 60m
bloom_level: create
prerequisites: [sync-mutex, sync-rwmutex, goroutines, http-basics]
-->

## The Challenge

Use Go's built-in profiling tools to identify and resolve mutex contention in a concurrent application. Learn to read mutex and block profiles from `pprof` to find the exact lines where goroutines spend time waiting for locks.

## Requirements

1. Build a program with intentional mutex contention (a hot mutex bottleneck)
2. Enable the `mutex` and `block` profilers via `runtime`
3. Capture profiles using `runtime/pprof` or `net/http/pprof`
4. Analyze profiles with `go tool pprof` to identify contention hotspots
5. Refactor the code to reduce contention (e.g., sharding, lock-free operations, or reducing critical section size)
6. Capture profiles again and demonstrate measurable improvement

## Hints

<details>
<summary>Hint 1: Enabling Profilers</summary>

```go
import "runtime"

func init() {
    runtime.SetMutexProfileFraction(1) // Record every contention event
    runtime.SetBlockProfileRate(1)      // Record every block event
}
```
</details>

<details>
<summary>Hint 2: Writing Profile to File</summary>

```go
import "runtime/pprof"

f, _ := os.Create("mutex.prof")
defer f.Close()
pprof.Lookup("mutex").WriteTo(f, 0)
```

Then analyze with:
```bash
go tool pprof mutex.prof
```
</details>

<details>
<summary>Hint 3: HTTP-Based Profiling</summary>

```go
import _ "net/http/pprof"
import "net/http"

go func() {
    http.ListenAndServe(":6060", nil)
}()
```

Then open `http://localhost:6060/debug/pprof/mutex` in a browser or use:
```bash
go tool pprof http://localhost:6060/debug/pprof/mutex
```
</details>

<details>
<summary>Hint 4: Example Program with Contention</summary>

```go
package main

import (
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"runtime/pprof"
	"sync"
	"time"
)

func init() {
	runtime.SetMutexProfileFraction(1)
	runtime.SetBlockProfileRate(1)
}

// -- Contended version: single mutex for all operations --

type SlowStore struct {
	mu   sync.Mutex
	data map[string]int
}

func NewSlowStore() *SlowStore {
	return &SlowStore{data: make(map[string]int)}
}

func (s *SlowStore) Increment(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key]++
	// Simulate work inside the critical section
	time.Sleep(time.Microsecond)
}

func (s *SlowStore) Get(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data[key]
}

// -- Optimized version: sharded locks --

type FastStore struct {
	shards    [16]shardStore
}

type shardStore struct {
	mu   sync.Mutex
	data map[string]int
}

func NewFastStore() *FastStore {
	fs := &FastStore{}
	for i := range fs.shards {
		fs.shards[i].data = make(map[string]int)
	}
	return fs
}

func (fs *FastStore) shard(key string) *shardStore {
	h := 0
	for _, c := range key {
		h = h*31 + int(c)
	}
	if h < 0 {
		h = -h
	}
	return &fs.shards[h%16]
}

func (fs *FastStore) Increment(key string) {
	s := fs.shard(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key]++
	time.Sleep(time.Microsecond)
}

func (fs *FastStore) Get(key string) int {
	s := fs.shard(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data[key]
}

func benchmark(name string, increment func(string), numGoroutines, opsPerGoroutine int) time.Duration {
	keys := make([]string, 100)
	for i := range keys {
		keys[i] = fmt.Sprintf("key-%d", i)
	}

	var wg sync.WaitGroup
	start := time.Now()

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				increment(keys[rand.Intn(len(keys))])
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)
	fmt.Printf("%s: %v\n", name, elapsed)
	return elapsed
}

func main() {
	slow := NewSlowStore()
	fast := NewFastStore()

	goroutines := 50
	ops := 200

	fmt.Println("=== Benchmarking ===")
	slowTime := benchmark("SlowStore", slow.Increment, goroutines, ops)
	fastTime := benchmark("FastStore", fast.Increment, goroutines, ops)
	fmt.Printf("Speedup: %.2fx\n", float64(slowTime)/float64(fastTime))

	// Write mutex profile
	f, err := os.Create("mutex.prof")
	if err == nil {
		pprof.Lookup("mutex").WriteTo(f, 0)
		f.Close()
		fmt.Println("\nMutex profile written to mutex.prof")
		fmt.Println("Analyze with: go tool pprof mutex.prof")
	}

	// Write block profile
	f2, err := os.Create("block.prof")
	if err == nil {
		pprof.Lookup("block").WriteTo(f2, 0)
		f2.Close()
		fmt.Println("Block profile written to block.prof")
		fmt.Println("Analyze with: go tool pprof block.prof")
	}
}
```
</details>

## Success Criteria

1. The contended version shows measurable lock contention in the mutex profile
2. The optimized version shows significantly reduced contention
3. `go tool pprof` can identify the exact line of code causing contention
4. You can explain the top entries in the mutex and block profiles
5. The optimized version is at least 2x faster under contention

Analyze the profile:

```bash
go tool pprof mutex.prof
# Then type: top
# Then type: list Increment
```

## Research Resources

- [Profiling Go Programs (blog)](https://go.dev/blog/pprof)
- [runtime/pprof documentation](https://pkg.go.dev/runtime/pprof)
- [net/http/pprof documentation](https://pkg.go.dev/net/http/pprof)
- [runtime.SetMutexProfileFraction](https://pkg.go.dev/runtime#SetMutexProfileFraction)
- [runtime.SetBlockProfileRate](https://pkg.go.dev/runtime#SetBlockProfileRate)
- [Diagnostics (Go docs)](https://go.dev/doc/diagnostics)

## What's Next

You have completed Section 15 on Sync Primitives. Continue to [Section 16 - Concurrency Patterns](../../16-concurrency-patterns/01-pipeline-pattern/01-pipeline-pattern.md) to learn how to compose these primitives into larger concurrent designs.

## Summary

- `runtime.SetMutexProfileFraction(1)` enables mutex contention profiling
- `runtime.SetBlockProfileRate(1)` enables goroutine blocking profiling
- Write profiles with `pprof.Lookup("mutex").WriteTo(f, 0)`
- Analyze with `go tool pprof <file>` using commands like `top`, `list`, and `web`
- Common fixes for contention: sharding, reducing critical section size, switching to `RWMutex`, or using atomics
