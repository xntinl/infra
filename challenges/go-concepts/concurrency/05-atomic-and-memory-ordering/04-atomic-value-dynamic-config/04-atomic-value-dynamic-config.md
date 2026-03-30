---
difficulty: intermediate
concepts: [atomic.Value, Store, Load, hot-reload, dynamic config, immutable swap]
tools: [go]
estimated_time: 30m
bloom_level: apply
---

# 4. Atomic Value for Dynamic Configuration

## Learning Objectives
After completing this exercise, you will be able to:
- **Build** a hot-reloadable configuration system using `atomic.Value`
- **Explain** why `atomic.Value` is necessary for swapping complex types (structs, maps) atomically
- **Implement** a config watcher that detects changes and swaps the entire config without locks
- **Avoid** the critical pitfalls: mutating stored values, storing nil, type inconsistency

## Why atomic.Value for Configuration Hot-Reload

Production services need to change configuration without restarting: update rate limits, rotate database connection strings, enable feature flags, adjust log levels. The standard pattern is a background goroutine that watches a config source (file, API, environment) and swaps the active configuration when changes are detected.

The atomic functions from previous exercises (`AddInt64`, `LoadInt32`, `CompareAndSwapInt64`) only operate on primitive numeric types. A real config struct has strings, durations, booleans, nested structs. You need to swap the entire struct at once so that no handler goroutine ever sees a half-old, half-new configuration.

`atomic.Value` stores and loads `interface{}` values atomically. A writer prepares a complete new config struct, calls `Store()`, and every subsequent `Load()` by any handler goroutine sees the new value immediately. There is no lock, no contention between readers, and the swap is instantaneous. Readers never block each other and never block the writer.

## Step 1 -- Store and Load a Config Struct

The fundamental operations: store an initial config, load it back, swap it with a new version:

```go
package main

import (
	"fmt"
	"sync/atomic"
	"time"
)

type ServiceConfig struct {
	ListenAddr     string
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	MaxConnections int
	LogLevel       string
	DatabaseURL    string
}

func main() {
	var config atomic.Value

	// Store initial config. After this, all subsequent Store calls
	// MUST use the same concrete type (ServiceConfig).
	config.Store(ServiceConfig{
		ListenAddr:     ":8080",
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxConnections: 100,
		LogLevel:       "info",
		DatabaseURL:    "postgres://localhost:5432/myapp",
	})

	// Load returns interface{} -- type assertion recovers the concrete type
	cfg := config.Load().(ServiceConfig)
	fmt.Printf("Listen: %s, MaxConns: %d, LogLevel: %s\n",
		cfg.ListenAddr, cfg.MaxConnections, cfg.LogLevel)

	// Swap atomically replaces and returns the old value (Go 1.17+)
	old := config.Swap(ServiceConfig{
		ListenAddr:     ":9090",
		ReadTimeout:    3 * time.Second,
		WriteTimeout:   5 * time.Second,
		MaxConnections: 500,
		LogLevel:       "debug",
		DatabaseURL:    "postgres://primary.db:5432/myapp",
	}).(ServiceConfig)

	newCfg := config.Load().(ServiceConfig)
	fmt.Printf("Old listen: %s -> New listen: %s\n", old.ListenAddr, newCfg.ListenAddr)
	fmt.Printf("Old maxConns: %d -> New maxConns: %d\n", old.MaxConnections, newCfg.MaxConnections)
}
```

### Verification
```bash
go run main.go
```
Expected output shows the initial config and the swapped values.

## Step 2 -- Config Watcher with Live Reload

A watcher goroutine detects config changes and swaps the active config. Handler goroutines load the current config on every request. Readers never block each other and never block the writer:

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type ServiceConfig struct {
	RateLimit      int
	MaxConnections int
	LogLevel       string
	MaintenanceMsg string
}

type ConfigManager struct {
	current atomic.Value
}

func NewConfigManager(initial ServiceConfig) *ConfigManager {
	cm := &ConfigManager{}
	cm.current.Store(initial)
	return cm
}

func (cm *ConfigManager) Get() ServiceConfig {
	return cm.current.Load().(ServiceConfig)
}

func (cm *ConfigManager) Update(newConfig ServiceConfig) ServiceConfig {
	old := cm.current.Swap(newConfig).(ServiceConfig)
	return old
}

func main() {
	manager := NewConfigManager(ServiceConfig{
		RateLimit:      100,
		MaxConnections: 50,
		LogLevel:       "info",
		MaintenanceMsg: "",
	})

	var wg sync.WaitGroup

	// Config watcher: simulates detecting file changes and reloading
	wg.Add(1)
	go func() {
		defer wg.Done()
		updates := []ServiceConfig{
			{RateLimit: 200, MaxConnections: 100, LogLevel: "info", MaintenanceMsg: ""},
			{RateLimit: 200, MaxConnections: 100, LogLevel: "debug", MaintenanceMsg: "Deploying v2.1"},
			{RateLimit: 500, MaxConnections: 200, LogLevel: "info", MaintenanceMsg: ""},
		}
		for i, cfg := range updates {
			time.Sleep(50 * time.Millisecond)
			old := manager.Update(cfg)
			fmt.Printf("[watcher] reload %d: rateLimit %d->%d, logLevel %s->%s\n",
				i+1, old.RateLimit, cfg.RateLimit, old.LogLevel, cfg.LogLevel)
		}
	}()

	// Request handlers: load config on each request
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(handlerID int) {
			defer wg.Done()
			for req := 0; req < 12; req++ {
				cfg := manager.Get()
				action := "200 OK"
				if cfg.MaintenanceMsg != "" {
					action = fmt.Sprintf("503 %s", cfg.MaintenanceMsg)
				}
				fmt.Printf("[handler %d req %02d] rateLimit=%d logLevel=%s -> %s\n",
					handlerID, req, cfg.RateLimit, cfg.LogLevel, action)
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
Handlers see config changes take effect in real time. During the maintenance window, handlers respond 503. After the final reload, handlers see the new rate limit. No race warnings.

## Step 3 -- Config Validation and Rollback

In production, invalid configs should be rejected. The previous valid config is preserved:

```go
package main

import (
	"fmt"
	"sync/atomic"
	"time"
)

type ServiceConfig struct {
	ListenAddr     string
	MaxConnections int
	RateLimit      int
	ReadTimeout    time.Duration
}

type ConfigManager struct {
	current atomic.Value
	version int
}

func NewConfigManager(initial ServiceConfig) *ConfigManager {
	cm := &ConfigManager{version: 1}
	cm.current.Store(initial)
	return cm
}

func (cm *ConfigManager) Get() ServiceConfig {
	return cm.current.Load().(ServiceConfig)
}

func (cm *ConfigManager) Reload(cfg ServiceConfig) error {
	if cfg.MaxConnections < 1 {
		return fmt.Errorf("MaxConnections must be positive, got %d", cfg.MaxConnections)
	}
	if cfg.RateLimit < 1 {
		return fmt.Errorf("RateLimit must be positive, got %d", cfg.RateLimit)
	}
	if cfg.ReadTimeout < time.Millisecond {
		return fmt.Errorf("ReadTimeout too small: %v", cfg.ReadTimeout)
	}
	if cfg.ListenAddr == "" {
		return fmt.Errorf("ListenAddr cannot be empty")
	}

	cm.version++
	cm.current.Store(cfg)
	fmt.Printf("[config] v%d loaded: listen=%s maxConns=%d rateLimit=%d\n",
		cm.version, cfg.ListenAddr, cfg.MaxConnections, cfg.RateLimit)
	return nil
}

func main() {
	mgr := NewConfigManager(ServiceConfig{
		ListenAddr:     ":8080",
		MaxConnections: 100,
		RateLimit:      200,
		ReadTimeout:    5 * time.Second,
	})

	fmt.Printf("Initial: %+v\n\n", mgr.Get())

	// Valid reload
	err := mgr.Reload(ServiceConfig{
		ListenAddr:     ":9090",
		MaxConnections: 500,
		RateLimit:      1000,
		ReadTimeout:    3 * time.Second,
	})
	fmt.Printf("Valid reload error: %v\n", err)
	fmt.Printf("Current: listen=%s maxConns=%d\n\n", mgr.Get().ListenAddr, mgr.Get().MaxConnections)

	// Invalid reload -- rejected, previous config preserved
	err = mgr.Reload(ServiceConfig{
		ListenAddr:     ":9090",
		MaxConnections: -1,
		RateLimit:      1000,
		ReadTimeout:    3 * time.Second,
	})
	fmt.Printf("Invalid reload error: %v\n", err)
	fmt.Printf("Unchanged: listen=%s maxConns=%d\n\n",
		mgr.Get().ListenAddr, mgr.Get().MaxConnections)

	// Another invalid reload
	err = mgr.Reload(ServiceConfig{
		ListenAddr:     "",
		MaxConnections: 100,
		RateLimit:      500,
		ReadTimeout:    3 * time.Second,
	})
	fmt.Printf("Invalid reload error: %v\n", err)
	fmt.Printf("Still unchanged: listen=%s maxConns=%d\n",
		mgr.Get().ListenAddr, mgr.Get().MaxConnections)
}
```

### Verification
```bash
go run main.go
```
Valid config loads succeed. Invalid configs are rejected with descriptive errors. The active config remains unchanged after a rejected reload.

## Step 4 -- Full Hot-Reload System with File Simulation

A complete hot-reload system: initial config, periodic watcher, concurrent handlers, graceful config transitions. Uses `atomic.Value` to ensure handlers always see a consistent config snapshot:

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type AppConfig struct {
	Version       int
	DatabasePool  int
	CacheEnabled  bool
	CacheTTL      time.Duration
	RateLimitRPS  int
	AllowedOrigin string
}

func (c AppConfig) String() string {
	return fmt.Sprintf("v%d pool=%d cache=%v cacheTTL=%v rps=%d origin=%s",
		c.Version, c.DatabasePool, c.CacheEnabled,
		c.CacheTTL, c.RateLimitRPS, c.AllowedOrigin)
}

type HotConfig struct {
	config  atomic.Value
	onChange []func(old, new AppConfig)
}

func NewHotConfig(initial AppConfig) *HotConfig {
	hc := &HotConfig{}
	hc.config.Store(initial)
	return hc
}

func (hc *HotConfig) Get() AppConfig {
	return hc.config.Load().(AppConfig)
}

func (hc *HotConfig) OnChange(fn func(old, new AppConfig)) {
	hc.onChange = append(hc.onChange, fn)
}

func (hc *HotConfig) Swap(newCfg AppConfig) {
	old := hc.config.Swap(newCfg).(AppConfig)
	for _, fn := range hc.onChange {
		fn(old, newCfg)
	}
}

func main() {
	cfg := NewHotConfig(AppConfig{
		Version:       1,
		DatabasePool:  10,
		CacheEnabled:  false,
		CacheTTL:      0,
		RateLimitRPS:  100,
		AllowedOrigin: "https://app.example.com",
	})

	cfg.OnChange(func(old, new AppConfig) {
		fmt.Printf("[event] config changed: v%d -> v%d\n", old.Version, new.Version)
		if old.DatabasePool != new.DatabasePool {
			fmt.Printf("[event]   pool size: %d -> %d\n", old.DatabasePool, new.DatabasePool)
		}
		if old.CacheEnabled != new.CacheEnabled {
			fmt.Printf("[event]   cache: %v -> %v\n", old.CacheEnabled, new.CacheEnabled)
		}
	})

	var wg sync.WaitGroup

	// Config file watcher
	wg.Add(1)
	go func() {
		defer wg.Done()
		revisions := []AppConfig{
			{Version: 2, DatabasePool: 20, CacheEnabled: true, CacheTTL: 5 * time.Minute,
				RateLimitRPS: 100, AllowedOrigin: "https://app.example.com"},
			{Version: 3, DatabasePool: 20, CacheEnabled: true, CacheTTL: 10 * time.Minute,
				RateLimitRPS: 500, AllowedOrigin: "https://app.example.com"},
			{Version: 4, DatabasePool: 50, CacheEnabled: true, CacheTTL: 10 * time.Minute,
				RateLimitRPS: 500, AllowedOrigin: "https://v2.example.com"},
		}
		for _, rev := range revisions {
			time.Sleep(40 * time.Millisecond)
			cfg.Swap(rev)
		}
	}()

	// Request handlers
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for req := 0; req < 10; req++ {
				c := cfg.Get()
				fmt.Printf("[handler %d req %02d] %s\n", id, req, c)
				time.Sleep(15 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
	fmt.Printf("\n[final] %s\n", cfg.Get())
}
```

### Verification
```bash
go run -race main.go
```
Handlers see config transitions in real time. Change events fire on each swap. Each handler always sees a complete, consistent config snapshot -- never a mix of old and new fields. No race warnings.

## Intermediate Verification

Run the race detector on each step:
```bash
go run -race main.go
```
All steps should pass with zero race warnings.

## Common Mistakes

### Mutating a Struct After Storing It

**Wrong:**
```go
package main

import (
	"fmt"
	"sync/atomic"
)

type Config struct {
	Port int
	Host string
}

func main() {
	cfg := &Config{Port: 8080, Host: "localhost"}
	var v atomic.Value
	v.Store(cfg)

	cfg.Port = 9090 // BUG: mutates the stored value -- data race!
	fmt.Println(v.Load().(*Config).Port) // readers may see 8080 or 9090
}
```

**What happens:** Storing a pointer and then modifying the struct it points to is a data race. Readers see a partially modified struct.

**Fix:** Create a new struct for every update. Treat stored values as immutable:
```go
v.Store(&Config{Port: 9090, Host: "localhost"}) // new allocation
```

### Storing Different Concrete Types

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

**Fix:** Always store the same concrete type. If you need flexibility, define an interface and always store that interface type.

### Loading Before Any Store

**Wrong:**
```go
package main

import "sync/atomic"

type Config struct{ Port int }

func main() {
	var config atomic.Value
	cfg := config.Load()   // returns nil -- no value stored yet
	_ = cfg.(Config)       // PANIC: type assertion on nil
}
```

**Fix:** Always store an initial value before any goroutine calls Load, or check for nil:
```go
val := config.Load()
if val == nil { return }
cfg := val.(Config)
```

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

**Fix:** Use a zero-value struct or a pointer type as the stored type. If "no config" is a valid state, use a pointer: `v.Store((*Config)(nil))` after first storing a non-nil `*Config`.

## Verify What You Learned

1. Why can't you use `atomic.Int64` or `atomic.Bool` for a multi-field config struct?
2. What happens if you call `Load()` before any `Store()` on an `atomic.Value`?
3. Why is it critical to never modify a struct after storing a pointer to it?
4. How does `atomic.Value` compare with a mutex for read-heavy workloads?

## What's Next
Continue to [05-spinlock-with-atomic](../05-spinlock-with-atomic/05-spinlock-with-atomic.md) to build a spinlock from atomic CAS -- and understand why you should almost never use one in production Go code.

## Summary
- `atomic.Value` stores and loads arbitrary types atomically, enabling lock-free config hot-reload
- The hot-reload pattern: store initial config, a watcher goroutine swaps it, handlers load per-request
- All `Store` calls must use the same concrete type; storing nil panics
- Never modify a value after storing it -- always create a new value for updates (immutable swap)
- `Swap` (Go 1.17+) atomically replaces and returns the old value, useful for change notifications
- Validate configs before storing them; rejected configs preserve the previous valid state
- `atomic.Value` has zero contention between readers, making it ideal for read-heavy server configs

## Reference
- [atomic.Value type](https://pkg.go.dev/sync/atomic#Value)
- [Go Memory Model](https://go.dev/ref/mem)
- [Go Blog: Share Memory By Communicating](https://go.dev/blog/codelab-share)
