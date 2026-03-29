# 1. Mutex: Protect Shared State

<!--
difficulty: basic
concepts: [sync.Mutex, Lock, Unlock, defer, race condition, critical section]
tools: [go]
estimated_time: 15m
bloom_level: apply
prerequisites: [goroutines, go keyword, WaitGroup basics]
-->

## Prerequisites
- Go 1.22+ installed
- Ability to launch goroutines with the `go` keyword
- Understanding of shared memory between goroutines

## Learning Objectives
After completing this exercise, you will be able to:
- **Identify** race conditions caused by unsynchronized access to shared state
- **Protect** shared variables using `sync.Mutex` with `Lock` and `Unlock`
- **Apply** the `defer mu.Unlock()` pattern for safe critical sections
- **Detect** data races using Go's built-in race detector

## Why Mutex
When multiple goroutines read and write the same variable without synchronization, the result is a data race -- one of the most insidious classes of bugs in concurrent programming. The outcome depends on the precise interleaving of goroutine execution, making the bug non-deterministic: your program might appear correct in testing and fail silently in production.

A `sync.Mutex` (mutual exclusion lock) solves this by ensuring that only one goroutine at a time can execute a critical section of code. When a goroutine calls `Lock()`, any other goroutine that also calls `Lock()` will block until the first goroutine calls `Unlock()`. This serializes access to shared state, eliminating the race.

The idiomatic Go pattern is to call `defer mu.Unlock()` immediately after `Lock()`. This guarantees the lock is released even if the critical section panics, preventing deadlocks caused by forgotten unlocks.

## Step 1 -- Observe the Race Condition

Run `main.go` and observe the unsafe counter produces an incorrect result:

```bash
go run main.go
```

You should see output like:
```
=== 1. Unsafe Counter (no mutex) ===
Expected: 1000000, Got: 547832
Race condition detected! Lost 452168 increments.
```

The exact number will vary between runs. Now run with Go's race detector to confirm:

```bash
go run -race main.go
```

The race detector will report `DATA RACE` warnings with stack traces showing the conflicting accesses.

### Intermediate Verification
You should see `WARNING: DATA RACE` output from the race detector pointing to the `counter++` line.

## Step 2 -- Understand the Fix with sync.Mutex

Read the `safeIncrement` function. It wraps the counter increment in a Lock/Unlock pair:

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
			for j := 0; j < 1000; j++ {
				mu.Lock()
				counter++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	fmt.Printf("Expected: 1000000, Got: %d\n", counter)
}
```

### Intermediate Verification
```bash
go run main.go
```
The safe counter should always print exactly `1000000`.

```bash
go run -race main.go
```
No `DATA RACE` warnings should appear for the safe version.

## Step 3 -- The defer Unlock Pattern

The `safeIncrementWithDefer` function extracts the critical section into a helper closure using `defer`:

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

	increment := func() {
		mu.Lock()
		defer mu.Unlock() // runs when increment() returns
		counter++
	}

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				increment()
			}
		}()
	}

	wg.Wait()
	fmt.Printf("Expected: 1000000, Got: %d\n", counter)
}
```

The `defer mu.Unlock()` line executes when `increment()` returns, guaranteeing the lock is always released. This is especially important when the critical section might return early or panic.

### Intermediate Verification
```bash
go run -race main.go
```
All three functions should run. The unsafe version shows an incorrect count; both safe versions show exactly `1000000`.

## Step 4 -- Struct with Embedded Mutex

The idiomatic Go pattern places the mutex alongside the data it protects inside a struct:

```go
package main

import (
	"fmt"
	"sync"
)

type ScoreBoard struct {
	mu     sync.Mutex
	scores map[string]int
}

func NewScoreBoard() *ScoreBoard {
	return &ScoreBoard{scores: make(map[string]int)}
}

func (sb *ScoreBoard) AddPoint(player string) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.scores[player]++
}

// Returns a COPY so callers cannot bypass the mutex.
func (sb *ScoreBoard) Scores() map[string]int {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	result := make(map[string]int, len(sb.scores))
	for k, v := range sb.scores {
		result[k] = v
	}
	return result
}

func main() {
	board := NewScoreBoard()
	var wg sync.WaitGroup

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			board.AddPoint("alice")
		}()
	}

	wg.Wait()
	fmt.Printf("Alice's score: %d\n", board.Scores()["alice"])
}
```

Expected output:
```
Alice's score: 1000
```

### Intermediate Verification
Run `go run -race main.go` on the full program. All five demos should complete without any race warnings.

## Step 5 -- Protecting a Shared Map

Maps in Go are NOT safe for concurrent access. Writing from multiple goroutines without a mutex causes a fatal runtime panic. The `protectSharedMap` function demonstrates the fix:

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var mu sync.Mutex
	m := make(map[int]int)
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				key := base*10 + j
				mu.Lock()
				m[key] = key * key
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()
	fmt.Printf("Map has %d entries (expected 1000).\n", len(m))
}
```

Expected output:
```
Map has 1000 entries (expected 1000).
```

## Common Mistakes

### Forgetting to Unlock

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var mu sync.Mutex
	mu.Lock()
	fmt.Println("acquired lock")
	// Forgot mu.Unlock() -- any goroutine that calls mu.Lock() will block forever.
	// This causes a deadlock if main also tries to lock again.
	mu.Lock() // DEADLOCK: blocks forever waiting for itself
	fmt.Println("this line is never reached")
}
```

**What happens:** Deadlock. All goroutines waiting on Lock will block permanently.

**Fix:** Always pair Lock with Unlock. Use `defer mu.Unlock()` immediately after Lock.

### Copying a Mutex

```go
package main

import (
	"fmt"
	"sync"
)

func doWork(mu sync.Mutex, counter *int) { // receives a COPY of the mutex
	mu.Lock()
	defer mu.Unlock()
	*counter++ // this lock is independent of the original -- no protection!
}

func main() {
	var mu sync.Mutex
	counter := 0
	var wg sync.WaitGroup

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			doWork(mu, &counter) // each goroutine gets its own mutex copy!
		}()
	}

	wg.Wait()
	fmt.Printf("Counter: %d (likely != 1000 due to copied mutex)\n", counter)
}
```

**What happens:** Each goroutine locks its own copy -- no mutual exclusion at all.

**Fix:** Always pass `*sync.Mutex` (a pointer):
```go
func doWork(mu *sync.Mutex, counter *int) {
	mu.Lock()
	defer mu.Unlock()
	*counter++
}
```

### Locking Too Broadly

```go
mu.Lock()
result := expensiveComputation() // holds the lock during slow work
counter += result
mu.Unlock()
```

**What happens:** All goroutines are serialized through the expensive computation, eliminating concurrency benefits.

**Fix:** Only hold the lock for the shared state access:
```go
result := expensiveComputation() // no lock needed here
mu.Lock()
counter += result
mu.Unlock()
```

## Verify What You Learned

Modify the program to protect a shared `map[string]int` instead of a simple counter. Launch 100 goroutines that each insert 100 key-value pairs into the map. Confirm with `-race` that there are no data races, and that the map contains all expected entries.

## What's Next
Continue to [02-rwmutex-readers-writers](../02-rwmutex-readers-writers/02-rwmutex-readers-writers.md) to learn how `sync.RWMutex` allows multiple concurrent readers while still protecting writes.

## Summary
- A data race occurs when multiple goroutines access shared state without synchronization and at least one writes
- `sync.Mutex` provides mutual exclusion: only one goroutine holds the lock at a time
- Always use `defer mu.Unlock()` immediately after `mu.Lock()` for safety
- Never copy a mutex -- pass it by pointer or embed it in a struct
- Minimize the critical section: hold the lock only while accessing shared state
- Return copies from locked methods to prevent callers from bypassing the mutex
- Use `go run -race` to detect data races during development

## Reference
- [sync.Mutex documentation](https://pkg.go.dev/sync#Mutex)
- [Go Data Race Detector](https://go.dev/doc/articles/race_detector)
- [Go Blog: Share Memory by Communicating](https://go.dev/blog/codelab-share)
