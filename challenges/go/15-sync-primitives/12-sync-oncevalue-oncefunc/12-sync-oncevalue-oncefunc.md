# 12. sync.OnceValue and OnceFunc

<!--
difficulty: intermediate
concepts: [sync-oncevalue, sync-oncefunc, lazy-initialization, go-1-21]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [sync-once, goroutines]
-->

## Prerequisites

- Go 1.21+ installed
- Completed the `sync.Once` exercise
- Familiarity with closures and generics

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** how `sync.OnceValue` and `sync.OnceFunc` improve upon `sync.Once`
- **Apply** `OnceValue` for lazy initialization that returns a value
- **Apply** `OnceFunc` as a cleaner alternative to `sync.Once.Do`

## Why sync.OnceValue and OnceFunc

Go 1.21 added `sync.OnceFunc`, `sync.OnceValue[T]`, and `sync.OnceValues[T1, T2]` to address common awkwardness with `sync.Once`:

With `sync.Once`, you need a separate variable to store the result:

```go
var once sync.Once
var db *DB

func GetDB() *DB {
    once.Do(func() { db = connectDB() })
    return db
}
```

With `sync.OnceValue`, the result is returned directly:

```go
var getDB = sync.OnceValue(func() *DB {
    return connectDB()
})
// Usage: db := getDB()
```

`sync.OnceFunc` simplifies the no-return-value case, and `sync.OnceValues` handles two return values (typically value + error).

## Step 1 -- OnceFunc: Run Once Without Return Value

```bash
mkdir -p ~/go-exercises/oncevalue
cd ~/go-exercises/oncevalue
go mod init oncevalue
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	initLogger := sync.OnceFunc(func() {
		fmt.Println("Logger initialized")
	})

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			initLogger()
			fmt.Printf("Goroutine %d: logger ready\n", id)
		}(i)
	}
	wg.Wait()
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: "Logger initialized" appears exactly once. All 5 goroutines print "logger ready".

## Step 2 -- OnceValue: Lazy Singleton with Return Value

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type AppConfig struct {
	DBHost  string
	DBPort  int
	Debug   bool
	Version string
}

var getConfig = sync.OnceValue(func() *AppConfig {
	fmt.Println("Loading configuration...")
	time.Sleep(50 * time.Millisecond) // Simulate loading
	return &AppConfig{
		DBHost:  "db.example.com",
		DBPort:  5432,
		Debug:   false,
		Version: "1.0.0",
	}
})

func main() {
	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			cfg := getConfig()
			fmt.Printf("Goroutine %d: host=%s, version=%s\n",
				id, cfg.DBHost, cfg.Version)
		}(i)
	}

	wg.Wait()
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: "Loading configuration..." appears exactly once. All goroutines see the same config values.

## Step 3 -- OnceValues: Initialization with Error Handling

```go
package main

import (
	"fmt"
	"os"
	"sync"
)

var loadSecret = sync.OnceValues(func() (string, error) {
	fmt.Println("Reading secret from environment...")
	secret := os.Getenv("APP_SECRET")
	if secret == "" {
		return "", fmt.Errorf("APP_SECRET environment variable not set")
	}
	return secret, nil
})

func main() {
	// First call
	secret, err := loadSecret()
	if err != nil {
		fmt.Println("Error:", err)
	} else {
		fmt.Println("Secret loaded:", secret[:3]+"***")
	}

	// Second call -- returns cached result
	secret2, err2 := loadSecret()
	fmt.Printf("Same result: %v, same error: %v\n", secret == secret2, err == err2)
}
```

### Intermediate Verification

```bash
APP_SECRET=mysecretkey go run main.go
```

Expected:

```
Reading secret from environment...
Secret loaded: mys***
Same result: true, same error: true
```

## Common Mistakes

| Mistake | Why It Fails |
|---|---|
| Using `OnceValue` with a function that can fail without `OnceValues` | No way to propagate the error; use `OnceValues` for functions returning `(T, error)` |
| Expecting `OnceValue` to retry on panic | Like `sync.Once`, a panic marks it as "done" |
| Storing `OnceValue` result in a package variable AND calling the func | Redundant; `OnceValue` already caches the result |

## Verify What You Learned

1. Replace a `sync.Once` + separate variable pattern in your code with `sync.OnceValue`
2. Use `sync.OnceValues` for an initialization that returns `(connection, error)`

## What's Next

Continue to [13 - Building a Thread-Safe Cache](../13-building-a-thread-safe-cache/13-building-a-thread-safe-cache.md) to build a production-grade concurrent cache with TTL.

## Summary

- `sync.OnceFunc(f)` returns a function that runs `f` exactly once (no return value)
- `sync.OnceValue(f)` returns a function that runs `f` once and caches its return value
- `sync.OnceValues(f)` handles two return values, ideal for `(T, error)` patterns
- These are cleaner alternatives to `sync.Once.Do` + separate result variables
- Available since Go 1.21

## Reference

- [sync.OnceValue documentation](https://pkg.go.dev/sync#OnceValue)
- [sync.OnceFunc documentation](https://pkg.go.dev/sync#OnceFunc)
- [Go 1.21 release notes](https://go.dev/doc/go1.21#sync)
