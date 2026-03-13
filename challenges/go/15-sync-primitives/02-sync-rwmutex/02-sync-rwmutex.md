# 2. sync.RWMutex

<!--
difficulty: intermediate
concepts: [sync-rwmutex, rlock-runlock, read-heavy-workloads, reader-writer-lock]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [sync-mutex, goroutines]
-->

## Prerequisites

- Go 1.22+ installed
- Completed the `sync.Mutex` exercise
- Familiarity with goroutines and `sync.WaitGroup`

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the difference between `sync.Mutex` and `sync.RWMutex`
- **Apply** `RLock`/`RUnlock` for concurrent reads and `Lock`/`Unlock` for exclusive writes
- **Determine** when an `RWMutex` provides a benefit over a plain `Mutex`

## Why sync.RWMutex

A regular `Mutex` allows only one goroutine at a time, regardless of whether it is reading or writing. In workloads where reads vastly outnumber writes, this creates unnecessary contention -- readers block each other even though concurrent reads are safe.

`sync.RWMutex` solves this with two lock modes:

- **Read lock** (`RLock`/`RUnlock`): Multiple goroutines can hold a read lock simultaneously. They only block if a writer holds the write lock.
- **Write lock** (`Lock`/`Unlock`): Exclusive access. A writer blocks all readers and other writers.

This is a classic readers-writer lock pattern. Use it when your data is read frequently and written infrequently, such as configuration caches, in-memory lookup tables, or feature flags.

## Step 1 -- Basic RWMutex Usage

Create a project and write a thread-safe configuration store:

```bash
mkdir -p ~/go-exercises/rwmutex
cd ~/go-exercises/rwmutex
go mod init rwmutex
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type Config struct {
	mu       sync.RWMutex
	settings map[string]string
}

func NewConfig() *Config {
	return &Config{settings: make(map[string]string)}
}

func (c *Config) Get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	val, ok := c.settings[key]
	return val, ok
}

func (c *Config) Set(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.settings[key] = value
}

func main() {
	cfg := NewConfig()
	cfg.Set("host", "localhost")
	cfg.Set("port", "8080")

	var wg sync.WaitGroup

	// Start 10 readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if val, ok := cfg.Get("host"); ok {
					_ = val
				}
				time.Sleep(time.Millisecond)
			}
			fmt.Printf("Reader %d done\n", id)
		}(i)
	}

	// Start 2 writers
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				cfg.Set("host", fmt.Sprintf("server-%d-%d", id, j))
				time.Sleep(10 * time.Millisecond)
			}
			fmt.Printf("Writer %d done\n", id)
		}(i)
	}

	wg.Wait()

	val, _ := cfg.Get("host")
	fmt.Println("Final host:", val)
}
```

### Intermediate Verification

```bash
go run -race main.go
```

Expected: All readers and writers complete with no race warnings. The final host value will be one of the last values written.

## Step 2 -- Compare Performance: Mutex vs RWMutex

Create a benchmark that compares plain `Mutex` with `RWMutex` in a read-heavy scenario:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func benchMutex(readers, writers int, iterations int) time.Duration {
	var mu sync.Mutex
	data := 0
	var wg sync.WaitGroup

	start := time.Now()

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				mu.Lock()
				_ = data
				mu.Unlock()
			}
		}()
	}

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations/10; j++ {
				mu.Lock()
				data++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	return time.Since(start)
}

func benchRWMutex(readers, writers int, iterations int) time.Duration {
	var mu sync.RWMutex
	data := 0
	var wg sync.WaitGroup

	start := time.Now()

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				mu.RLock()
				_ = data
				mu.RUnlock()
			}
		}()
	}

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations/10; j++ {
				mu.Lock()
				data++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	return time.Since(start)
}

func main() {
	readers := 100
	writers := 2
	iterations := 10000

	muDur := benchMutex(readers, writers, iterations)
	rwDur := benchRWMutex(readers, writers, iterations)

	fmt.Printf("Mutex:   %v\n", muDur)
	fmt.Printf("RWMutex: %v\n", rwDur)
	fmt.Printf("Speedup: %.2fx\n", float64(muDur)/float64(rwDur))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: `RWMutex` is faster than `Mutex` in this read-heavy scenario. The exact speedup depends on your hardware but should be greater than 1x.

## Step 3 -- Thread-Safe Cache with Read-Through

Build a read-through cache that uses `RWMutex` to allow concurrent reads while serializing cache misses:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type Cache struct {
	mu    sync.RWMutex
	store map[string]string
}

func NewCache() *Cache {
	return &Cache{store: make(map[string]string)}
}

func (c *Cache) GetOrLoad(key string, loader func(string) string) string {
	// Try read lock first
	c.mu.RLock()
	if val, ok := c.store[key]; ok {
		c.mu.RUnlock()
		return val
	}
	c.mu.RUnlock()

	// Cache miss -- acquire write lock
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if val, ok := c.store[key]; ok {
		return val
	}

	val := loader(key)
	c.store[key] = val
	return val
}

func main() {
	cache := NewCache()
	loader := func(key string) string {
		time.Sleep(10 * time.Millisecond) // Simulate slow lookup
		return "value-for-" + key
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := fmt.Sprintf("key-%d", id%5)
			val := cache.GetOrLoad(key, loader)
			fmt.Printf("goroutine %2d: %s = %s\n", id, key, val)
		}(i)
	}

	wg.Wait()
}
```

### Intermediate Verification

```bash
go run -race main.go
```

Expected: All 20 goroutines print their results with no race warnings. Each unique key is loaded only once due to the double-check pattern.

## Common Mistakes

| Mistake | Why It Fails |
|---|---|
| Using `RLock` for writes | Data race -- multiple goroutines can hold `RLock` simultaneously, so writes are not protected |
| Upgrading `RLock` to `Lock` without releasing | Deadlock -- you cannot upgrade a read lock to a write lock; you must `RUnlock` first |
| Using `RWMutex` when writes are frequent | `RWMutex` has higher overhead than `Mutex`; with frequent writes, a plain `Mutex` may be faster |
| Forgetting the double-check in read-through patterns | Multiple goroutines may load the same value concurrently after a cache miss |

## Verify What You Learned

1. Run the cache example with `-race` and confirm no races
2. Modify the benchmark to use 50/50 read/write ratio and observe that `RWMutex` loses its advantage

## What's Next

Continue to [03 - sync.Once](../03-sync-once/03-sync-once.md) to learn how to run initialization code exactly once.

## Summary

- `sync.RWMutex` allows multiple concurrent readers or a single exclusive writer
- Use `RLock`/`RUnlock` for read-only access and `Lock`/`Unlock` for writes
- Best suited for read-heavy workloads; with frequent writes, prefer a plain `Mutex`
- Cannot upgrade from read lock to write lock -- release the read lock first
- Double-check pattern prevents redundant work when promoting from read to write lock

## Reference

- [sync.RWMutex documentation](https://pkg.go.dev/sync#RWMutex)
- [Go blog: Share Memory By Communicating](https://go.dev/blog/codelab-share)
