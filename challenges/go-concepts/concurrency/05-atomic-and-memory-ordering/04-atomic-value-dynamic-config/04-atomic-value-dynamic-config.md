# 4. Atomic Value for Dynamic Configuration

<!--
difficulty: intermediate
concepts: [atomic.Value, Store, Load, dynamic config, hot-reload, type assertion]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [goroutines, sync.WaitGroup, atomic Load/Store, structs, interfaces]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01-02 (atomic add, load/store)
- Familiarity with Go structs and type assertions

## Learning Objectives
After completing this exercise, you will be able to:
- **Use** `atomic.Value` to store and load arbitrary types atomically
- **Implement** a lock-free dynamic configuration pattern
- **Explain** the constraints on `atomic.Value` (consistent typing, no nil)
- **Compare** `atomic.Value` with mutex-protected config and understand the trade-offs

## Why atomic.Value

The atomic functions we have seen so far (`AddInt64`, `LoadInt64`, `CompareAndSwapInt64`) operate on primitive numeric types. But real programs need to atomically swap richer values: configuration structs, connection pools, routing tables, feature flags.

`atomic.Value` stores and loads `interface{}` values atomically. A writer prepares a new config struct, then calls `Store()`. All subsequent `Load()` calls by reader goroutines see the new value immediately. There is no lock, no contention between readers, and the swap is instantaneous.

This is the standard pattern for hot-reloading configuration in Go servers. The server starts with an initial config, then a background goroutine watches for changes (file, API, signal) and swaps the config atomically. Request-handling goroutines load the current config on each request.

## Example 1 -- Store and Load a Config Struct

The fundamental operations on `atomic.Value`:

```go
package main

import (
	"fmt"
	"sync/atomic"
	"time"
)

type ServerConfig struct {
	Port         int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	MaxConns     int
	Debug        bool
}

func main() {
	var config atomic.Value

	// Store initial config. After this call, all subsequent Store calls
	// MUST use the same concrete type (ServerConfig). Storing a different
	// type panics at runtime.
	config.Store(ServerConfig{
		Port:         8080,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		MaxConns:     100,
		Debug:        false,
	})

	// Load returns interface{} — type assertion recovers the concrete type
	cfg := config.Load().(ServerConfig)
	fmt.Printf("Port: %d, MaxConns: %d, Debug: %v\n", cfg.Port, cfg.MaxConns, cfg.Debug)

	// Swap replaces the value atomically and returns the old one (Go 1.17+)
	old := config.Swap(ServerConfig{
		Port: 9090, ReadTimeout: 3 * time.Second,
		WriteTimeout: 5 * time.Second, MaxConns: 500, Debug: true,
	}).(ServerConfig)
	fmt.Printf("Old port: %d, New port: %d\n",
		old.Port, config.Load().(ServerConfig).Port)
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
Port: 8080, MaxConns: 100, Debug: false
Old port: 8080, New port: 9090
```

## Example 2 -- Dynamic Config with Readers and a Reloader

Multiple reader goroutines serve requests using the current config. A reloader goroutine swaps the config periodically. Readers never block each other and never block the writer.

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type ServerConfig struct {
	Port     int
	MaxConns int
	Debug    bool
}

func main() {
	var config atomic.Value
	config.Store(ServerConfig{Port: 8080, MaxConns: 100, Debug: false})

	var wg sync.WaitGroup

	// Reloader: updates config every 50ms
	wg.Add(1)
	go func() {
		defer wg.Done()
		updates := []ServerConfig{
			{Port: 8080, MaxConns: 200, Debug: false},
			{Port: 9090, MaxConns: 500, Debug: true},
			{Port: 9090, MaxConns: 1000, Debug: false},
		}
		for _, cfg := range updates {
			time.Sleep(50 * time.Millisecond)
			config.Store(cfg) // atomic swap — readers see old or new, never partial
			fmt.Printf("[reloader] Port=%d, MaxConns=%d, Debug=%v\n",
				cfg.Port, cfg.MaxConns, cfg.Debug)
		}
	}()

	// Readers: load config every 20ms
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				cfg := config.Load().(ServerConfig)
				fmt.Printf("[reader %d] Port=%d, MaxConns=%d\n", id, cfg.Port, cfg.MaxConns)
				time.Sleep(20 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
}
```

### Verification
```bash
go run -race main.go
```
Readers initially see Port=8080, then switch to 9090 as the reloader applies updates. No race warnings.

## Example 3 -- Config with Validation

In production, config values should be validated before storing. Invalid configs are rejected, preserving the previous valid config:

```go
package main

import (
	"fmt"
	"sync/atomic"
	"time"
)

type ServerConfig struct {
	Port     int
	MaxConns int
}

func main() {
	var config atomic.Value

	loadConfig := func(cfg ServerConfig) error {
		if cfg.Port < 1 || cfg.Port > 65535 {
			return fmt.Errorf("invalid port: %d", cfg.Port)
		}
		if cfg.MaxConns < 1 {
			return fmt.Errorf("MaxConns must be positive, got %d", cfg.MaxConns)
		}
		config.Store(cfg)
		return nil
	}

	getConfig := func() ServerConfig {
		return config.Load().(ServerConfig)
	}

	// Valid config loads successfully
	_ = loadConfig(ServerConfig{Port: 8080, MaxConns: 100})
	fmt.Printf("Loaded: Port=%d\n", getConfig().Port) // 8080

	// Invalid config is rejected — old config preserved
	err := loadConfig(ServerConfig{Port: -1, MaxConns: 100})
	fmt.Printf("Rejected: %v\n", err)
	fmt.Printf("Unchanged: Port=%d\n", getConfig().Port) // still 8080

	// Another valid config succeeds
	_ = loadConfig(ServerConfig{Port: 9090, MaxConns: 500})
	fmt.Printf("Updated: Port=%d\n", getConfig().Port) // 9090

	_ = time.Second // used in full example
}
```

### Verification
```bash
go run main.go
```
Expected: initial config loads, invalid config is rejected with error, config remains at Port=8080.

## Example 4 -- Feature Flag System

A complete feature flag system: a toggler goroutine flips flags, service goroutines read them. Each reader always sees a consistent snapshot of ALL flags because `Store` replaces the entire struct atomically.

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type FeatureFlags struct {
	DarkMode  bool
	BetaAPI   bool
	RateLimit int
}

func main() {
	var flags atomic.Value
	flags.Store(FeatureFlags{DarkMode: false, BetaAPI: false, RateLimit: 100})

	var wg sync.WaitGroup

	// Toggler: flips DarkMode and BetaAPI every 30ms
	wg.Add(1)
	go func() {
		defer wg.Done()
		for cycle := 0; cycle < 5; cycle++ {
			time.Sleep(30 * time.Millisecond)
			current := flags.Load().(FeatureFlags)
			flags.Store(FeatureFlags{
				DarkMode:  !current.DarkMode,
				BetaAPI:   !current.BetaAPI,
				RateLimit: current.RateLimit,
			})
		}
	}()

	// Service readers: read flags every 10ms
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				f := flags.Load().(FeatureFlags)
				fmt.Printf("[service %d] DarkMode=%v, BetaAPI=%v, RateLimit=%d\n",
					id, f.DarkMode, f.BetaAPI, f.RateLimit)
				time.Sleep(10 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
}
```

### Verification
```bash
go run -race main.go
```
Services always see consistent flag states. DarkMode and BetaAPI always match (toggled together) because the Store is atomic. No race warnings.

## Common Mistakes

### Storing Different Types

**Wrong:**
```go
package main

import "sync/atomic"

func main() {
	var v atomic.Value
	v.Store("hello")
	v.Store(42) // PANIC: store of inconsistently typed value
}
```

**What happens:** `atomic.Value` panics if you store a value of a different type than the first stored value.

**Fix:** Always store the same concrete type. If you need flexibility, define an interface and always store that interface type.

### Storing nil

**Wrong:**
```go
package main

import "sync/atomic"

func main() {
	var v atomic.Value
	v.Store(nil) // PANIC: store of nil value
}
```

**What happens:** `Store(nil)` panics.

**Fix:** Use a pointer type where nil has meaning, or initialize with a default value before any Load.

### Modifying a Struct After Storing

**Wrong:**
```go
package main

import (
	"fmt"
	"sync/atomic"
)

type Config struct {
	Port int
}

func main() {
	cfg := &Config{Port: 8080}
	var v atomic.Value
	v.Store(cfg)
	cfg.Port = 9090 // BUG: mutates the stored value — data race!
	fmt.Println(v.Load().(*Config).Port) // readers may see 8080 or 9090
}
```

**What happens:** If you store a pointer and then modify the struct it points to, readers see a partially modified struct. This is a data race.

**Fix:** Never modify a struct after storing a pointer to it. Create a new struct for each update:
```go
v.Store(&Config{Port: 9090}) // new allocation, old one is immutable
```

### Loading Before Any Store

**Wrong:**
```go
package main

import "sync/atomic"

type Config struct{ Port int }

func main() {
	var config atomic.Value
	cfg := config.Load()    // returns nil — no value stored yet
	_ = cfg.(Config)        // PANIC: interface conversion on nil
}
```

**Fix:** Always store an initial value before any goroutine calls `Load`, or check for nil:
```go
val := config.Load()
if val == nil {
    // use default config
    return
}
cfg := val.(Config)
```

## What's Next
Continue to [05-spinlock-with-atomic](../05-spinlock-with-atomic/05-spinlock-with-atomic.md) to build a spinlock from atomic CAS -- and understand why you should almost never do this in production.

## Summary
- `atomic.Value` stores and loads arbitrary types atomically, enabling lock-free config patterns
- All `Store` calls must use the same concrete type; storing nil panics
- The hot-reload pattern: store initial config, a reloader goroutine swaps it, readers load on each request
- Never modify a value after storing it; always create a new value for updates
- Always store an initial value before any `Load` call to avoid nil type assertions
- `Swap` (Go 1.17+) atomically replaces and returns the old value
- For complex update logic (validation, rollback), wrap `Store` in a helper function

## Reference
- [atomic.Value type](https://pkg.go.dev/sync/atomic#Value)
- [Go Blog: Share Memory By Communicating](https://go.dev/blog/codelab-share)
- [Go Memory Model](https://go.dev/ref/mem)
