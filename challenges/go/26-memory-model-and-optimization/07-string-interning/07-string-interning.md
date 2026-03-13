# 7. String Interning

<!--
difficulty: advanced
concepts: [string-interning, string-allocations, deduplication, memory-reduction, intern-pool]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [memory-profiling, escape-analysis, sync-primitives]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of Go string internals (string header = pointer + length)
- Familiarity with memory profiling

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** why duplicate strings waste memory in Go
- **Implement** a thread-safe string intern pool
- **Measure** memory savings from string interning
- **Evaluate** when interning is worth the complexity

## Why String Interning

In Go, each string is a header containing a pointer and a length. When you parse CSV data, JSON responses, or log files, the same string values appear thousands of times. Each duplicate is a separate heap allocation, wasting memory and increasing GC pressure.

String interning stores one canonical copy of each unique string and returns references to it. This trades a small amount of lookup overhead for potentially large memory savings.

## The Problem

Build a string intern pool and demonstrate its effectiveness with a realistic workload: processing records where certain fields have low cardinality (e.g., country codes, status values).

## Requirements

1. Implement a thread-safe string intern pool
2. Process a large dataset with repetitive string fields
3. Compare memory usage with and without interning
4. Handle the pool's lifecycle (preventing unbounded growth)

## Step 1 -- Implement the Intern Pool

```bash
mkdir -p ~/go-exercises/string-intern && cd ~/go-exercises/string-intern
go mod init string-intern
```

Create `intern.go`:

```go
package main

import "sync"

// Interner deduplicates strings by maintaining a pool of canonical copies.
type Interner struct {
	mu   sync.RWMutex
	pool map[string]string
}

func NewInterner() *Interner {
	return &Interner{pool: make(map[string]string)}
}

// Intern returns the canonical copy of s. If s has not been seen
// before, it is added to the pool. All returned strings sharing the
// same value point to the same underlying byte array.
func (in *Interner) Intern(s string) string {
	// Fast path: read lock.
	in.mu.RLock()
	if canonical, ok := in.pool[s]; ok {
		in.mu.RUnlock()
		return canonical
	}
	in.mu.RUnlock()

	// Slow path: write lock.
	in.mu.Lock()
	defer in.mu.Unlock()

	// Double-check after acquiring write lock.
	if canonical, ok := in.pool[s]; ok {
		return canonical
	}

	// Clone the string to break any reference to a larger buffer
	// (e.g., a substring of a large read buffer).
	cloned := clone(s)
	in.pool[cloned] = cloned
	return cloned
}

func (in *Interner) Len() int {
	in.mu.RLock()
	defer in.mu.RUnlock()
	return len(in.pool)
}

// clone creates an independent copy of s.
func clone(s string) string {
	b := make([]byte, len(s))
	copy(b, s)
	return string(b)
}
```

## Step 2 -- Simulate a Workload

Create `workload.go`:

```go
package main

import (
	"fmt"
	"math/rand"
	"runtime"
	"strings"
)

type Record struct {
	Country string
	Status  string
	City    string
}

var countries = []string{"US", "UK", "DE", "FR", "JP", "AU", "CA", "BR", "IN", "CN"}
var statuses = []string{"active", "inactive", "pending", "suspended"}
var cities = []string{"New York", "London", "Berlin", "Paris", "Tokyo",
	"Sydney", "Toronto", "Sao Paulo", "Mumbai", "Beijing"}

func generateRecords(n int) [][]string {
	records := make([][]string, n)
	for i := range records {
		records[i] = []string{
			countries[rand.Intn(len(countries))],
			statuses[rand.Intn(len(statuses))],
			cities[rand.Intn(len(cities))],
		}
	}
	return records
}

func processWithoutInterning(raw [][]string) []Record {
	records := make([]Record, len(raw))
	for i, r := range raw {
		// Simulate parsing: create new string from substring
		records[i] = Record{
			Country: strings.Clone(r[0]),
			Status:  strings.Clone(r[1]),
			City:    strings.Clone(r[2]),
		}
	}
	return records
}

func processWithInterning(raw [][]string, interner *Interner) []Record {
	records := make([]Record, len(raw))
	for i, r := range raw {
		records[i] = Record{
			Country: interner.Intern(r[0]),
			Status:  interner.Intern(r[1]),
			City:    interner.Intern(r[2]),
		}
	}
	return records
}

func memoryUsage() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func main() {
	const numRecords = 1_000_000
	raw := generateRecords(numRecords)

	before := memoryUsage()
	withoutIntern := processWithoutInterning(raw)
	after := memoryUsage()
	fmt.Printf("Without interning: %d records, heap = %d KB\n",
		len(withoutIntern), (after-before)/1024)

	_ = withoutIntern
	withoutIntern = nil
	runtime.GC()

	before = memoryUsage()
	interner := NewInterner()
	withIntern := processWithInterning(raw, interner)
	after = memoryUsage()
	fmt.Printf("With interning:    %d records, heap = %d KB, unique strings = %d\n",
		len(withIntern), (after-before)/1024, interner.Len())
}
```

```bash
go run intern.go workload.go
```

## Step 3 -- Benchmark

Create `intern_test.go`:

```go
package main

import "testing"

func BenchmarkWithoutInterning(b *testing.B) {
	raw := generateRecords(10_000)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		processWithoutInterning(raw)
	}
}

func BenchmarkWithInterning(b *testing.B) {
	raw := generateRecords(10_000)
	interner := NewInterner()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		processWithInterning(raw, interner)
	}
}
```

```bash
go test -bench=. -benchmem
```

## Hints

- `strings.Clone` (Go 1.20+) creates an independent string copy, simulating real parsing
- The RWMutex fast path avoids contention for already-interned strings
- Interning is most effective when cardinality is low relative to total records
- Consider bounded interning (LRU eviction) for high-cardinality fields
- Go 1.23 added `unique.Handle` in the standard library, which provides built-in interning

## Verification

- Without interning, each of the 1M records allocates 3 strings (3M allocations)
- With interning, only ~24 unique strings are allocated (10 countries + 4 statuses + 10 cities)
- Memory savings should be significant (often 50%+ for string-heavy structs)
- The interned benchmark shows fewer `allocs/op` than the non-interned version

## What's Next

String interning reduces allocations. The next exercise covers `sync.Pool` tuning to reuse temporary objects on hot paths.

## Summary

String interning stores one canonical copy of each unique string value, eliminating duplicate allocations. Implement it with a map protected by `sync.RWMutex` using a read-lock fast path. It is most effective for low-cardinality fields processed in high volume. Always measure with memory profiling and benchmarks to confirm the benefit outweighs the lookup overhead.

## Reference

- [strings.Clone](https://pkg.go.dev/strings#Clone)
- [unique package (Go 1.23+)](https://pkg.go.dev/unique)
- [Go Strings Internals](https://go.dev/blog/strings)
