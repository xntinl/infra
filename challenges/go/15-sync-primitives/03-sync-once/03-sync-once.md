# 3. sync.Once

<!--
difficulty: intermediate
concepts: [sync-once, lazy-initialization, singleton-pattern, do-method]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [sync-mutex, goroutines]
-->

## Prerequisites

- Go 1.22+ installed
- Completed the `sync.Mutex` exercise
- Familiarity with goroutines and `sync.WaitGroup`

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** what `sync.Once` guarantees and how `Do()` works
- **Apply** `sync.Once` for lazy initialization of expensive resources
- **Identify** common pitfalls like calling `Do` with different functions

## Why sync.Once

Some initialization should happen exactly once regardless of how many goroutines trigger it: opening a database connection pool, loading a configuration file, or initializing a logger. You could protect this with a mutex and a boolean flag, but `sync.Once` does this more cleanly and efficiently.

`sync.Once` guarantees that the function passed to `Do()` runs exactly once, even if `Do()` is called from multiple goroutines simultaneously. After the first call completes, all subsequent calls return immediately. The first call blocks all other callers until it finishes, ensuring the resource is fully initialized before anyone uses it.

## Step 1 -- Basic Once Usage

```bash
mkdir -p ~/go-exercises/sync-once
cd ~/go-exercises/sync-once
go mod init sync-once
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var once sync.Once
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			once.Do(func() {
				fmt.Printf("Initialized by goroutine %d\n", id)
			})
			fmt.Printf("Goroutine %d continues\n", id)
		}(i)
	}

	wg.Wait()
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: "Initialized by goroutine N" appears exactly once. All 10 "continues" messages appear.

## Step 2 -- Lazy Singleton Pattern

Use `sync.Once` to create a database connection that initializes on first use:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type DB struct {
	Host string
}

var (
	dbInstance *DB
	dbOnce     sync.Once
)

func GetDB() *DB {
	dbOnce.Do(func() {
		fmt.Println("Connecting to database...")
		time.Sleep(100 * time.Millisecond) // Simulate connection time
		dbInstance = &DB{Host: "localhost:5432"}
		fmt.Println("Database connected")
	})
	return dbInstance
}

func main() {
	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			db := GetDB()
			fmt.Printf("Goroutine %d got DB: %s\n", id, db.Host)
		}(i)
	}

	wg.Wait()
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: "Connecting to database..." and "Database connected" appear exactly once. All 5 goroutines print the same host.

## Step 3 -- Once with Error Handling

`sync.Once` runs the function only once, even if it fails. To handle initialization errors, capture the error inside the closure:

```go
package main

import (
	"errors"
	"fmt"
	"sync"
)

type Service struct {
	once sync.Once
	conn string
	err  error
}

func (s *Service) Connect(addr string) error {
	s.once.Do(func() {
		if addr == "" {
			s.err = errors.New("empty address")
			return
		}
		s.conn = "connected-to-" + addr
		fmt.Println("Connected to", addr)
	})
	return s.err
}

func main() {
	svc := &Service{}

	// First call initializes
	if err := svc.Connect("localhost:8080"); err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Println("Connection:", svc.conn)

	// Second call is a no-op
	if err := svc.Connect("other-host:9090"); err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Println("Connection:", svc.conn) // Still the original
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Connected to localhost:8080
Connection: connected-to-localhost:8080
Connection: connected-to-localhost:8080
```

## Common Mistakes

| Mistake | Why It Fails |
|---|---|
| Calling `Do` with different functions | Only the first function runs; subsequent calls with different functions are ignored |
| Expecting `Do` to retry on panic | If the function panics, `Once` still considers it "done" -- subsequent calls are no-ops |
| Copying a `sync.Once` value | Copies do not share state; each copy can run its function independently |
| Not storing errors from initialization | `Do` runs once even on failure; you must capture and check the error |

## Verify What You Learned

1. Modify the singleton example to use an empty address and verify the error propagates to all goroutines
2. Call `GetDB()` from multiple goroutines and confirm the initialization message appears exactly once

## What's Next

Continue to [04 - sync.Map](../04-sync-map/04-sync-map.md) to learn about Go's concurrent-safe map implementation.

## Summary

- `sync.Once.Do(f)` runs `f` exactly once, even across multiple goroutines
- Subsequent calls to `Do` return immediately without running the function
- Use `sync.Once` for lazy initialization of singletons, connections, and expensive resources
- If initialization can fail, store the error inside the closure and check it on every access
- Never copy a `sync.Once` value

## Reference

- [sync.Once documentation](https://pkg.go.dev/sync#Once)
- [Effective Go: init functions](https://go.dev/doc/effective_go#init)
