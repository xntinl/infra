# 4. sync.Map

<!--
difficulty: intermediate
concepts: [sync-map, load-or-store, range, concurrent-map-access]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [sync-mutex, goroutines, maps]
-->

## Prerequisites

- Go 1.22+ installed
- Completed the `sync.Mutex` exercise
- Familiarity with Go maps and goroutines

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** when `sync.Map` is preferable to a regular map with a mutex
- **Apply** `LoadOrStore`, `Load`, `Store`, `Delete`, and `Range` methods
- **Determine** appropriate use cases for `sync.Map` vs map+mutex

## Why sync.Map

Go's built-in `map` type is not safe for concurrent use. The most common solution is pairing it with a `sync.Mutex` or `sync.RWMutex`. However, `sync.Map` exists for two specific scenarios where it outperforms the mutex approach:

1. **Stable keys**: When entries are written once and read many times (append-only or mostly-read maps).
2. **Disjoint key sets**: When multiple goroutines read, write, and overwrite entries for disjoint sets of keys.

For general-purpose concurrent maps where keys are frequently added and deleted, a regular map with `sync.RWMutex` is usually better. `sync.Map` trades type safety (it uses `any` for keys and values) for reduced lock contention in its target scenarios.

## Step 1 -- Basic Operations

```bash
mkdir -p ~/go-exercises/sync-map
cd ~/go-exercises/sync-map
go mod init sync-map
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var m sync.Map

	// Store values
	m.Store("name", "Alice")
	m.Store("age", 30)
	m.Store("city", "Portland")

	// Load a value
	if val, ok := m.Load("name"); ok {
		fmt.Println("Name:", val)
	}

	// Delete a value
	m.Delete("city")

	// Range over all entries
	m.Range(func(key, value any) bool {
		fmt.Printf("%s: %v\n", key, value)
		return true // return false to stop iteration
	})
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Name: Alice
name: Alice
age: 30
```

The order of Range output may vary.

## Step 2 -- LoadOrStore for Deduplication

`LoadOrStore` stores a value only if the key does not already exist. It returns the existing value and `true` if the key was present, or the new value and `false` if it was stored:

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var m sync.Map
	var wg sync.WaitGroup

	// Multiple goroutines try to initialize the same keys
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			actual, loaded := m.LoadOrStore("counter", id)
			if loaded {
				fmt.Printf("goroutine %d: found existing value %v\n", id, actual)
			} else {
				fmt.Printf("goroutine %d: stored new value %v\n", id, actual)
			}
		}(i)
	}

	wg.Wait()

	val, _ := m.Load("counter")
	fmt.Println("Final value:", val)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: Exactly one goroutine prints "stored new value", all others print "found existing value". The final value matches what the first goroutine stored.

## Step 3 -- Concurrent Cache Example

Build a simple URL visit counter using `sync.Map`:

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

type VisitCounter struct {
	counts sync.Map
}

func (vc *VisitCounter) Visit(url string) int64 {
	actual, _ := vc.counts.LoadOrStore(url, &atomic.Int64{})
	counter := actual.(*atomic.Int64)
	return counter.Add(1)
}

func (vc *VisitCounter) Report() {
	vc.counts.Range(func(key, value any) bool {
		url := key.(string)
		count := value.(*atomic.Int64).Load()
		fmt.Printf("  %s: %d visits\n", url, count)
		return true
	})
}

func main() {
	vc := &VisitCounter{}
	var wg sync.WaitGroup

	urls := []string{"/home", "/about", "/api/data", "/home", "/api/data", "/home"}

	for _, url := range urls {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			count := vc.Visit(u)
			fmt.Printf("Visited %s (count: %d)\n", u, count)
		}(url)
	}

	wg.Wait()
	fmt.Println("\nFinal report:")
	vc.Report()
}
```

### Intermediate Verification

```bash
go run -race main.go
```

Expected: No race conditions. Each URL shows its total visit count. `/home` should have 3 visits.

## Common Mistakes

| Mistake | Why It Fails |
|---|---|
| Using `sync.Map` as a default concurrent map | It is slower than map+mutex for general workloads with mixed reads/writes/deletes |
| Assuming `Range` is a snapshot | `Range` may reflect concurrent modifications during iteration |
| Storing typed values and forgetting type assertions | `sync.Map` uses `any`, so you must assert types on `Load` |
| Using `LoadOrStore` with mutable values and mutating the stored copy | The returned value might be a different goroutine's copy; use pointers |

## Verify What You Learned

1. Run the visit counter with the race detector and confirm no races
2. Try replacing `sync.Map` with a regular `map[string]*atomic.Int64` + `sync.RWMutex` and compare the code complexity

## What's Next

Continue to [05 - sync.Pool](../05-sync-pool/05-sync-pool.md) to learn how to reduce allocation pressure with pooled objects.

## Summary

- `sync.Map` is optimized for append-mostly and disjoint-key-set workloads
- Use `LoadOrStore` for atomic check-and-set operations
- `Range` iterates over all entries but is not a consistent snapshot
- For general-purpose concurrent maps, prefer a regular map with `sync.RWMutex`
- Values are `any` -- you must use type assertions when loading

## Reference

- [sync.Map documentation](https://pkg.go.dev/sync#Map)
- [Go blog: Maps in Go](https://go.dev/blog/maps)
