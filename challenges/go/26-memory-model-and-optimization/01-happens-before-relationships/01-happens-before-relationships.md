# 1. Happens-Before Relationships

<!--
difficulty: intermediate
concepts: [memory-model, happens-before, synchronization-points, data-races, ordering-guarantees]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [goroutines, channels, sync-mutex]
-->

## Prerequisites

- Go 1.22+ installed
- Solid understanding of goroutines and channels
- Experience with `sync.Mutex` and `sync.WaitGroup`

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the Go memory model's happens-before relation
- **Identify** which synchronization operations create happens-before edges
- **Apply** correct synchronization to guarantee visibility of writes across goroutines

## Why Happens-Before Relationships

When two goroutines access the same variable without synchronization, neither goroutine is guaranteed to observe the other's writes. The Go memory model defines a partial order called "happens-before" that determines when a read in one goroutine is guaranteed to see a write from another.

Without understanding happens-before, you write code that works "most of the time" but fails unpredictably under load or on different hardware. The Go race detector catches some of these bugs, but understanding the model lets you reason about correctness before running the code.

Key happens-before edges in Go:
- A send on a channel happens-before the corresponding receive completes
- The closing of a channel happens-before a receive of the zero value
- The nth call to `sync.Mutex.Unlock` happens-before the (n+1)th call to `Lock`
- A single call of `f()` from `once.Do(f)` happens-before any call to `once.Do(f)` returns
- `sync.WaitGroup.Done` happens-before the corresponding `Wait` returns

## Step 1 -- Observe Broken Visibility

Create a project:

```bash
mkdir -p ~/go-exercises/happens-before && cd ~/go-exercises/happens-before
go mod init happens-before
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	var data int
	var ready bool

	go func() {
		data = 42
		ready = true
	}()

	// This is WRONG: no happens-before edge guarantees
	// we see both writes.
	time.Sleep(1 * time.Millisecond)
	if ready {
		fmt.Println("data:", data)
	} else {
		fmt.Println("not ready")
	}
}
```

Run with the race detector:

```bash
go run -race main.go
```

### Intermediate Verification

You should see a data race warning. The race detector reports the unsynchronized access to `ready` and `data`. The `time.Sleep` does **not** create a happens-before edge.

## Step 2 -- Fix with a Channel

Replace the broken pattern with a channel, which guarantees happens-before ordering:

```go
package main

import "fmt"

func main() {
	var data int
	done := make(chan struct{})

	go func() {
		data = 42
		close(done) // close happens-before receive of zero value
	}()

	<-done // blocks until close, establishing happens-before
	fmt.Println("data:", data) // guaranteed to see 42
}
```

```bash
go run -race main.go
```

### Intermediate Verification

No race warnings. The channel close creates a happens-before edge: the write `data = 42` happens-before `close(done)`, which happens-before `<-done` completes, which happens-before the `fmt.Println` reads `data`.

## Step 3 -- Demonstrate sync.Once Guarantees

Create `once.go`:

```go
package main

import (
	"fmt"
	"sync"
)

type Config struct {
	Value string
}

var (
	config *Config
	once   sync.Once
)

func getConfig() *Config {
	once.Do(func() {
		// All writes inside once.Do happen-before any
		// once.Do call returns, even from other goroutines.
		config = &Config{Value: "production"}
	})
	return config
}

func main() {
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := getConfig()
			if c.Value != "production" {
				panic("broken visibility")
			}
		}()
	}
	wg.Wait()
	fmt.Println("all goroutines saw config correctly")
}
```

```bash
go run -race once.go
```

### Intermediate Verification

All 100 goroutines observe `Value: "production"`. The `sync.Once` guarantee ensures the write to `config` is visible to every goroutine after `once.Do` returns.

## Step 4 -- Mutex Happens-Before Chain

Create `mutex_chain.go` to demonstrate that `Unlock` on the nth call happens-before `Lock` on the (n+1)th:

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var mu sync.Mutex
	var messages []string

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			mu.Lock()
			// Inside the critical section, we see all previous
			// writes to messages because each Unlock happens-before
			// the next Lock.
			messages = append(messages, fmt.Sprintf("goroutine-%d", id))
			mu.Unlock()
		}(i)
	}

	wg.Wait()
	fmt.Println("messages:", messages)
	fmt.Println("count:", len(messages)) // always 5
}
```

```bash
go run -race mutex_chain.go
```

### Intermediate Verification

The count is always 5 and no race is detected. Each `Unlock` creates a happens-before edge to the next `Lock`, so the goroutine acquiring the lock sees all previous appends.

## Common Mistakes

| Mistake | Why It Fails |
|---------|-------------|
| Using `time.Sleep` for synchronization | `Sleep` does not create happens-before edges |
| Reading a flag variable without synchronization | The compiler/CPU may reorder or cache the read |
| Assuming assignment order = visibility order | Without synchronization, writes can appear in any order to other goroutines |
| Using `sync/atomic` but forgetting it only orders the atomic variable itself | Non-atomic variables near an atomic op still need proper synchronization |

## Verify What You Learned

1. What is a happens-before relationship in the Go memory model?
2. Name three synchronization operations that create happens-before edges.
3. Why does `time.Sleep` not guarantee that a goroutine sees another goroutine's writes?
4. In the channel example, trace the chain of happens-before edges from `data = 42` to `fmt.Println("data:", data)`.

## What's Next

With a solid grasp of the memory model, you're ready to learn how to profile your programs for CPU usage using `pprof` in the next exercise.

## Summary

The Go memory model defines a happens-before partial order that determines write visibility across goroutines. Synchronization primitives -- channels, mutexes, `sync.Once`, `sync.WaitGroup` -- create happens-before edges. Without these edges, the compiler and hardware are free to reorder operations, making concurrent programs behave unpredictably. Always use proper synchronization rather than relying on timing assumptions.

## Reference

- [The Go Memory Model](https://go.dev/ref/mem)
- [Go Race Detector](https://go.dev/doc/articles/race_detector)
