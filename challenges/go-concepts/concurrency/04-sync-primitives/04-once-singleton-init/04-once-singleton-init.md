---
difficulty: intermediate
concepts: [sync.Once, Do, sync.OnceValue, sync.OnceFunc, lazy initialization, thread safety]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [goroutines, sync.Mutex, sync.WaitGroup]
---

# 4. Once: Singleton Initialization


## Learning Objectives
After completing this exercise, you will be able to:
- **Use** `sync.Once` to guarantee code runs exactly once across all goroutines
- **Fix** double-initialization bugs in concurrent code
- **Apply** `sync.OnceValue` and `sync.OnceFunc` for cleaner lazy initialization (Go 1.21+)
- **Explain** why `sync.Once` is preferred over manual mutex-based singleton patterns

## Why sync.Once
In any server application, expensive resources -- database connection pools, gRPC clients, TLS configurations -- should be created once and shared. Lazy initialization (creating the resource on first use rather than at startup) is a common pattern because it avoids paying the cost if the resource is never needed, and it simplifies dependency ordering.

But in a concurrent server, dozens of goroutines handling incoming requests might call `GetDB()` simultaneously during startup. Without synchronization, you get multiple connection pools, wasted file descriptors, redundant migrations, or corrupted state from partially initialized resources.

You might think a mutex solves this:
```go
mu.Lock()
if pool == nil {
    pool = createConnectionPool()
}
mu.Unlock()
```

This works but has a cost: every subsequent call pays the lock overhead even though initialization is already done. The "double-checked locking" optimization is notoriously tricky to get right across memory models.

`sync.Once` solves this elegantly. It guarantees that the function passed to `Do` executes exactly once, regardless of how many goroutines call it concurrently. After the first call completes, all subsequent calls return immediately with zero overhead.

## Step 1 -- The Double Initialization Problem

Multiple HTTP handlers call `GetDB()` on the first request. Without `sync.Once`, the connection pool is created multiple times:

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type DBPool struct {
	Host        string
	MaxConns    int
	Initialized bool
}

func main() {
	var pool *DBPool
	var initCount atomic.Int64
	var wg sync.WaitGroup

	getDB := func() *DBPool {
		if pool == nil { // UNSAFE: multiple goroutines can pass this check
			initCount.Add(1)
			fmt.Printf("  Initializing DB pool... (goroutine #%d to reach this point)\n", initCount.Load())
			time.Sleep(50 * time.Millisecond) // simulate connection setup
			pool = &DBPool{Host: "postgres:5432", MaxConns: 25, Initialized: true}
		}
		return pool
	}

	fmt.Println("=== Unsafe Initialization (race condition) ===")
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(handlerID int) {
			defer wg.Done()
			db := getDB()
			_ = db
		}(i)
	}

	wg.Wait()
	fmt.Printf("DB pool created %d times (should be 1)\n", initCount.Load())
	fmt.Println("Wasted connections, possible corruption, resource leak.")
}
```

Expected output:
```
=== Unsafe Initialization (race condition) ===
  Initializing DB pool... (goroutine #1 to reach this point)
  Initializing DB pool... (goroutine #2 to reach this point)
  Initializing DB pool... (goroutine #3 to reach this point)
  ...
DB pool created 15+ times (should be 1)
Wasted connections, possible corruption, resource leak.
```

### Intermediate Verification
Run several times. The initialization count should vary but consistently be greater than 1. With `-race`, you will also see data race warnings on the `pool` variable.

## Step 2 -- Fix with sync.Once

`sync.Once` guarantees the initialization runs exactly once. All concurrent callers block until the first execution completes, then return immediately:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type DBPool struct {
	Host        string
	MaxConns    int
	Connected   bool
}

func main() {
	var once sync.Once
	var pool *DBPool
	var wg sync.WaitGroup

	initPool := func() {
		fmt.Println("  Connecting to database...")
		time.Sleep(100 * time.Millisecond) // simulate TCP handshake + auth
		fmt.Println("  Running migration check...")
		time.Sleep(50 * time.Millisecond) // simulate migration
		pool = &DBPool{Host: "postgres:5432", MaxConns: 25, Connected: true}
		fmt.Println("  DB pool ready.")
	}

	fmt.Println("=== Safe Initialization with sync.Once ===")
	start := time.Now()

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(handlerID int) {
			defer wg.Done()
			once.Do(initPool) // only the first caller executes initPool
			fmt.Printf("  Handler %d: using pool (connected=%v) at %v\n",
				handlerID, pool.Connected, time.Since(start).Round(time.Millisecond))
		}(i)
	}

	wg.Wait()
	fmt.Println("\nAll 20 handlers share the same pool. Initialization happened exactly once.")
}
```

Expected output:
```
=== Safe Initialization with sync.Once ===
  Connecting to database...
  Running migration check...
  DB pool ready.
  Handler 3: using pool (connected=true) at 153ms
  Handler 0: using pool (connected=true) at 153ms
  ...

All 20 handlers share the same pool. Initialization happened exactly once.
```

### Intermediate Verification
```bash
go run -race main.go
```
"Connecting to database..." appears exactly once. All 20 handlers report `connected=true`. No race warnings.

## Step 3 -- sync.OnceValue: Lazy DB Connection with Return Value

Go 1.21 introduced `sync.OnceValue` for functions that return a value. This is the cleanest pattern for lazy singletons:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type DBPool struct {
	Host     string
	MaxConns int
	PingOK   bool
}

func main() {
	getPool := sync.OnceValue(func() *DBPool {
		fmt.Println("  [init] Establishing connection pool...")
		time.Sleep(80 * time.Millisecond)
		pool := &DBPool{
			Host:     "postgres.internal:5432",
			MaxConns: 25,
			PingOK:   true,
		}
		fmt.Println("  [init] Pool ready.")
		return pool
	})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(handlerID int) {
			defer wg.Done()
			pool := getPool() // first call initializes, rest return cached
			fmt.Printf("  Handler %d: host=%s maxConns=%d\n", handlerID, pool.Host, pool.MaxConns)
		}(i)
	}

	wg.Wait()
}
```

Expected output:
```
  [init] Establishing connection pool...
  [init] Pool ready.
  Handler 0: host=postgres.internal:5432 maxConns=25
  Handler 1: host=postgres.internal:5432 maxConns=25
  ...
```

`sync.OnceValue` returns a function that, on first call, executes the initializer and caches the result. All subsequent calls return the cached value without re-executing. No separate variable needed to store the singleton.

### Intermediate Verification
```bash
go run main.go
```
"Establishing connection pool..." appears exactly once. All handlers print the same host and maxConns.

## Step 4 -- sync.OnceFunc: One-Time Migration Runner

`sync.OnceFunc` wraps a function with no return value, useful for one-time side effects like running migrations or registering metrics:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	runMigrations := sync.OnceFunc(func() {
		fmt.Println("  [migrate] Checking schema version...")
		time.Sleep(50 * time.Millisecond)
		fmt.Println("  [migrate] Applying migration 001_create_users...")
		time.Sleep(30 * time.Millisecond)
		fmt.Println("  [migrate] Applying migration 002_add_sessions...")
		time.Sleep(30 * time.Millisecond)
		fmt.Println("  [migrate] All migrations applied.")
	})

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(handlerID int) {
			defer wg.Done()
			runMigrations() // only first call runs the migrations
			fmt.Printf("  Handler %d: serving requests (migrations complete)\n", handlerID)
		}(i)
	}

	wg.Wait()
}
```

Expected output:
```
  [migrate] Checking schema version...
  [migrate] Applying migration 001_create_users...
  [migrate] Applying migration 002_add_sessions...
  [migrate] All migrations applied.
  Handler 0: serving requests (migrations complete)
  Handler 1: serving requests (migrations complete)
  ...
```

### Intermediate Verification
```bash
go run main.go
```
Migration messages appear once. All handlers proceed after migrations complete.

## Step 5 -- Important Behaviors

Three critical properties of `sync.Once` that affect production code:

**1. Once tracks calls, not functions.** Passing a different function to a second `Do` call has no effect:

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var once sync.Once
	once.Do(func() { fmt.Println("postgres pool created") })
	once.Do(func() { fmt.Println("redis pool created") }) // NEVER executes
	fmt.Println("Only one pool was created.")
}
```

**2. Panic is considered "done".** If the initializer panics, `Once` still marks it as completed. Subsequent `Do` calls will NOT retry:

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var once sync.Once

	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Recovered from panic: %v\n", r)
		}
		// This will NOT retry the initialization
		once.Do(func() {
			fmt.Println("This retry never runs because Once is already 'done'")
		})
		fmt.Println("Once considers the panicked call as 'done'. No retry.")
	}()

	once.Do(func() {
		panic("connection refused")
	})
}
```

**3. Recursive Do deadlocks.** Calling `once.Do(f)` from within `f` will deadlock because the inner `Do` waits for the outer to complete:

```go
// DO NOT RUN -- this deadlocks
var once sync.Once
once.Do(func() {
    once.Do(func() { // DEADLOCK: inner Do waits for outer
        fmt.Println("inner")
    })
})
```

### Intermediate Verification
```bash
go run main.go
```
The first two behaviors should demonstrate their properties. Do not run the recursive example -- it hangs.

## Common Mistakes

### Passing Different Functions to Do

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var once sync.Once
	once.Do(func() { fmt.Println("init postgres") })
	once.Do(func() { fmt.Println("init redis") }) // never executes!
	// Output: only "init postgres"
}
```

**What happens:** The second function is silently ignored. `sync.Once` guarantees exactly one execution, regardless of which function is passed.

**Fix:** Use separate `sync.Once` instances for independent initializations.

### Deadlock Inside Once

```go
var once sync.Once
once.Do(func() {
    once.Do(func() { // DEADLOCK: recursive Do call
        fmt.Println("inner")
    })
})
```

**What happens:** Deadlock. The inner `Do` waits for the outer `Do` to complete, which waits for the inner `Do`.

**Fix:** Never call `Do` recursively on the same `sync.Once`.

### Ignoring Panics in Once
If the function passed to `Do` panics, `sync.Once` still considers it "done". Subsequent calls to `Do` will not re-execute. If initialization can fail, handle errors inside the function:

```go
package main

import (
	"errors"
	"fmt"
	"sync"
)

func main() {
	var once sync.Once
	var pool *struct{ Host string }
	var initErr error

	initPool := func() {
		pool, initErr = nil, errors.New("connection refused: postgres:5432")
		if initErr != nil {
			fmt.Printf("Init failed: %v (will not retry with sync.Once)\n", initErr)
		}
	}

	once.Do(initPool)
	fmt.Printf("pool=%v, err=%v\n", pool, initErr)

	// Callers must check initErr before using pool
	if initErr != nil {
		fmt.Println("Application should fail fast or implement retry outside of Once.")
	}
}
```

## Verify What You Learned

Create a `ServiceRegistry` that lazily initializes three independent resources (DB pool, Redis client, gRPC connection) using separate `sync.OnceValue` instances. Each resource should be initialized independently on first access. Write a test program that accesses all three from 100 concurrent goroutines and verifies each is initialized exactly once.

## What's Next
Continue to [05-pool-object-reuse](../05-pool-object-reuse/05-pool-object-reuse.md) to learn how `sync.Pool` recycles objects to reduce garbage collection pressure.

## Summary
- `sync.Once` guarantees a function executes exactly once, even with concurrent callers
- All concurrent callers block until the first execution completes, then return immediately
- The primary use case is lazy initialization of expensive singletons: DB pools, gRPC clients, config loaders
- `sync.OnceValue` (Go 1.21+) caches the return value of a one-time initialization
- `sync.OnceFunc` (Go 1.21+) wraps a side-effect function for one-time execution
- Never call `Do` recursively on the same `Once` -- it deadlocks
- If the function panics, `Once` still considers it done -- handle errors internally
- Prefer `sync.Once` over manual mutex-based singleton patterns

## Reference
- [sync.Once documentation](https://pkg.go.dev/sync#Once)
- [sync.OnceValue documentation](https://pkg.go.dev/sync#OnceValue)
- [sync.OnceFunc documentation](https://pkg.go.dev/sync#OnceFunc)
- [The Go Memory Model](https://go.dev/ref/mem)
