# 1. sync.Mutex

<!--
difficulty: basic
concepts: [sync-mutex, lock-unlock, critical-sections, data-races]
tools: [go]
estimated_time: 20m
bloom_level: remember
prerequisites: [goroutines, channels-basics]
-->

## Prerequisites

- Go 1.22+ installed
- Familiarity with goroutines and basic concurrency
- Understanding of shared memory

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** what a mutex is and why it exists
- **Identify** data races in concurrent Go programs
- **Use** `sync.Mutex` to protect shared state with `Lock` and `Unlock`

## Why sync.Mutex

When multiple goroutines access the same variable and at least one of them writes, you have a data race. Data races lead to unpredictable behavior: corrupted values, crashes, or results that change between runs.

A mutex (mutual exclusion lock) solves this by ensuring only one goroutine can access the protected section of code at a time. When a goroutine calls `Lock()`, any other goroutine calling `Lock()` blocks until the first goroutine calls `Unlock()`. The code between `Lock()` and `Unlock()` is called a critical section.

Go provides `sync.Mutex` in the standard library. It is the simplest synchronization primitive and the foundation for understanding more advanced tools like `sync.RWMutex` and `sync.Map`.

## Step 1 -- Observe a Data Race

Create a project and write a program where multiple goroutines increment a shared counter without synchronization.

```bash
mkdir -p ~/go-exercises/sync-mutex
cd ~/go-exercises/sync-mutex
go mod init sync-mutex
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	counter := 0
	var wg sync.WaitGroup

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			counter++ // DATA RACE: unsynchronized write
		}()
	}

	wg.Wait()
	fmt.Println("Counter:", counter)
}
```

### Intermediate Verification

```bash
go run -race main.go
```

Expected: The race detector reports `DATA RACE` warnings. The counter value will be less than 1000 and may vary between runs.

## Step 2 -- Fix with sync.Mutex

Add a mutex to protect the counter increment:

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	counter := 0
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mu.Lock()
			counter++
			mu.Unlock()
		}()
	}

	wg.Wait()
	fmt.Println("Counter:", counter)
}
```

### Intermediate Verification

```bash
go run -race main.go
```

Expected:

```
Counter: 1000
```

No race detector warnings.

## Step 3 -- Use defer with Unlock

Using `defer mu.Unlock()` immediately after `Lock()` ensures the mutex is always released, even if the critical section panics:

```go
package main

import (
	"fmt"
	"sync"
)

type SafeCounter struct {
	mu    sync.Mutex
	count int
}

func (sc *SafeCounter) Increment() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.count++
}

func (sc *SafeCounter) Value() int {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.count
}

func main() {
	sc := &SafeCounter{}
	var wg sync.WaitGroup

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sc.Increment()
		}()
	}

	wg.Wait()
	fmt.Println("Counter:", sc.Value())
}
```

### Intermediate Verification

```bash
go run -race main.go
```

Expected:

```
Counter: 1000
```

## Step 4 -- Protect a Map

Maps in Go are not safe for concurrent use. Protect a shared map with a mutex:

```go
package main

import (
	"fmt"
	"sync"
)

type SafeMap struct {
	mu sync.Mutex
	m  map[string]int
}

func NewSafeMap() *SafeMap {
	return &SafeMap{m: make(map[string]int)}
}

func (sm *SafeMap) Set(key string, value int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.m[key] = value
}

func (sm *SafeMap) Get(key string) (int, bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	v, ok := sm.m[key]
	return v, ok
}

func main() {
	sm := NewSafeMap()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := fmt.Sprintf("key-%d", n%10)
			sm.Set(key, n)
		}(i)
	}

	wg.Wait()

	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key-%d", i)
		if v, ok := sm.Get(key); ok {
			fmt.Printf("%s = %d\n", key, v)
		}
	}
}
```

### Intermediate Verification

```bash
go run -race main.go
```

Expected: Ten key-value pairs printed, no race warnings. The exact values depend on goroutine scheduling but all 10 keys will be present.

## Common Mistakes

### Forgetting to Unlock

**Wrong:**

```go
mu.Lock()
counter++
// Missing mu.Unlock() -- all other goroutines block forever
```

**What happens:** Deadlock. All other goroutines wait on `Lock()` indefinitely.

**Fix:** Always pair `Lock()` with `Unlock()`. Use `defer mu.Unlock()` right after `Lock()`.

### Copying a Mutex

**Wrong:**

```go
type Counter struct {
	mu    sync.Mutex
	count int
}

func (c Counter) Increment() { // Value receiver copies the mutex
	c.mu.Lock()
	defer c.mu.Unlock()
	c.count++
}
```

**What happens:** Each call gets a different copy of the mutex, so nothing is actually synchronized.

**Fix:** Use a pointer receiver: `func (c *Counter) Increment()`.

### Locking Inside a Loop When Not Needed

**Wrong:**

```go
mu.Lock()
for i := 0; i < 1000; i++ {
	counter++ // Holding lock for entire loop
}
mu.Unlock()
```

**What happens:** While not incorrect, holding the lock for a long time blocks all other goroutines. Only lock for the minimum required duration.

## Verify What You Learned

Run the final program with the race detector:

```bash
go run -race main.go
```

Confirm no race conditions are detected and all keys are printed.

## What's Next

Continue to [02 - sync.RWMutex](../02-sync-rwmutex/02-sync-rwmutex.md) to learn how to use read-write mutexes for read-heavy workloads.

## Summary

- A data race occurs when multiple goroutines access shared data without synchronization and at least one writes
- `sync.Mutex` provides mutual exclusion with `Lock()` and `Unlock()`
- Always use `defer mu.Unlock()` after `Lock()` to prevent forgetting to unlock
- Never copy a mutex -- use pointer receivers when embedding a mutex in a struct
- Use `go run -race` to detect data races during development

## Reference

- [sync.Mutex documentation](https://pkg.go.dev/sync#Mutex)
- [The Go Memory Model](https://go.dev/ref/mem)
- [Data Race Detector](https://go.dev/doc/articles/race_detector)
