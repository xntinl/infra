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

This is the standard pattern for hot-reloading configuration in Go servers. The server starts with an initial config, then a background goroutine watches for changes (file, API, signal) and swaps the config atomically. Request-handling goroutines load the current config on each request. No mutex needed, no blocking between readers.

## Step 1 -- Store and Load a Config Struct

Define a configuration struct and use `atomic.Value` to store and retrieve it:

```go
type ServerConfig struct {
    Port         int
    ReadTimeout  time.Duration
    WriteTimeout time.Duration
    MaxConns     int
    Debug        bool
}

func basicAtomicValue() {
    var config atomic.Value

    // Store initial config
    config.Store(ServerConfig{
        Port:         8080,
        ReadTimeout:  5 * time.Second,
        WriteTimeout: 10 * time.Second,
        MaxConns:     100,
        Debug:        false,
    })

    // Load and use -- type assertion required
    cfg := config.Load().(ServerConfig)
    fmt.Printf("  Port: %d, MaxConns: %d, Debug: %v\n", cfg.Port, cfg.MaxConns, cfg.Debug)
}
```

`Store` accepts any value, but once you store a value of a given type, all subsequent `Store` calls must use the same type. Storing a different type panics. `Load` returns `interface{}`, so you need a type assertion to recover the concrete type.

### Intermediate Verification
```bash
go run main.go
```
Expected output: `Port: 8080, MaxConns: 100, Debug: false`

## Step 2 -- Dynamic Config with Readers and a Reloader

Implement a realistic scenario: multiple reader goroutines serving requests, and a reloader goroutine that updates the config periodically.

```go
func dynamicConfig() {
    var config atomic.Value

    config.Store(ServerConfig{
        Port:         8080,
        ReadTimeout:  5 * time.Second,
        WriteTimeout: 10 * time.Second,
        MaxConns:     100,
        Debug:        false,
    })

    var wg sync.WaitGroup

    // Reloader: updates config every 50ms (simulates hot-reload)
    wg.Add(1)
    go func() {
        defer wg.Done()
        updates := []ServerConfig{
            {Port: 8080, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, MaxConns: 200, Debug: false},
            {Port: 9090, ReadTimeout: 3 * time.Second, WriteTimeout: 5 * time.Second, MaxConns: 500, Debug: true},
            {Port: 9090, ReadTimeout: 3 * time.Second, WriteTimeout: 5 * time.Second, MaxConns: 1000, Debug: false},
        }
        for _, cfg := range updates {
            time.Sleep(50 * time.Millisecond)
            config.Store(cfg)
            fmt.Printf("  [reloader] Updated config: Port=%d, MaxConns=%d, Debug=%v\n",
                cfg.Port, cfg.MaxConns, cfg.Debug)
        }
    }()

    // Readers: load config every 20ms (simulates request handling)
    for i := 0; i < 3; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            for j := 0; j < 10; j++ {
                cfg := config.Load().(ServerConfig)
                fmt.Printf("  [reader %d] Port=%d, MaxConns=%d\n", id, cfg.Port, cfg.MaxConns)
                time.Sleep(20 * time.Millisecond)
            }
        }(i)
    }

    wg.Wait()
}
```

Readers never block each other and never block the writer. Each reader sees a consistent snapshot of the config -- never a partially updated struct. This is because `Store` replaces the entire value atomically.

### Intermediate Verification
```bash
go run -race main.go
```
You will see readers initially using Port=8080, then switching to 9090 as the reloader applies updates. No race warnings.

## Step 3 -- Config with Validation and Immutability

In production, config values should be immutable once stored. Store a pointer to a config struct to enforce this convention and add validation:

```go
func validatedConfig() {
    var config atomic.Value

    loadConfig := func(cfg *ServerConfig) error {
        if cfg.Port < 1 || cfg.Port > 65535 {
            return fmt.Errorf("invalid port: %d", cfg.Port)
        }
        if cfg.MaxConns < 1 {
            return fmt.Errorf("MaxConns must be positive, got %d", cfg.MaxConns)
        }
        config.Store(cfg) // store pointer -- the struct is immutable once stored
        return nil
    }

    getConfig := func() *ServerConfig {
        return config.Load().(*ServerConfig)
    }

    err := loadConfig(&ServerConfig{Port: 8080, MaxConns: 100, Debug: false,
        ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second})
    if err != nil {
        fmt.Printf("  Error: %v\n", err)
        return
    }

    cfg := getConfig()
    fmt.Printf("  Validated config: Port=%d, MaxConns=%d\n", cfg.Port, cfg.MaxConns)

    // Try invalid config -- rejected, old config preserved
    err = loadConfig(&ServerConfig{Port: -1, MaxConns: 100})
    if err != nil {
        fmt.Printf("  Rejected invalid config: %v\n", err)
    }

    cfg = getConfig()
    fmt.Printf("  Config unchanged: Port=%d, MaxConns=%d\n", cfg.Port, cfg.MaxConns)
}
```

Storing a pointer means readers get a shared reference to an immutable struct. The convention is: never modify a config after storing it. Create a new struct for each update.

### Intermediate Verification
```bash
go run main.go
```
The invalid config is rejected and the original config remains active.

## Common Mistakes

### Storing Different Types
**Wrong:**
```go
var v atomic.Value
v.Store("hello")
v.Store(42) // PANIC: inconsistent type
```
**What happens:** `atomic.Value` panics if you store a value of a different type than the first stored value.

**Fix:** Always store the same concrete type. If you need flexibility, store an interface type consistently.

### Storing nil
**Wrong:**
```go
var v atomic.Value
v.Store(nil) // PANIC: cannot store nil
```
**What happens:** `Store(nil)` panics.

**Fix:** Use a sentinel value or a pointer type where `nil` has a meaning. Or initialize with a default value before any Load.

### Modifying a Struct After Storing
**Wrong:**
```go
cfg := ServerConfig{Port: 8080}
config.Store(&cfg)
cfg.Port = 9090 // mutates the stored value -- data race!
```
**What happens:** Readers see a partially modified struct. This is a data race.

**Fix:** Never modify a struct after storing a pointer to it. Create a new struct for each update.

### Loading Before Any Store
**Wrong:**
```go
var config atomic.Value
cfg := config.Load() // returns nil
cfg.(ServerConfig)   // PANIC: nil type assertion
```
**What happens:** `Load` returns nil if no value has been stored. The type assertion panics.

**Fix:** Always store an initial value before any goroutine calls `Load`, or check for nil before the type assertion.

## Verify What You Learned

Implement a `FeatureFlags` system:
1. Define a `FeatureFlags` struct with fields: `DarkMode bool`, `BetaAPI bool`, `RateLimit int`
2. Store initial flags in an `atomic.Value`
3. Launch a "toggle" goroutine that flips `DarkMode` and `BetaAPI` every 30ms for 5 cycles
4. Launch 3 "service" goroutines that read the flags every 10ms for 20 cycles, printing the state
5. Verify no races with `go run -race`

## What's Next
Continue to [05-spinlock-with-atomic](../05-spinlock-with-atomic/05-spinlock-with-atomic.md) to build a spinlock from atomic CAS -- and understand why you should almost never do this in production.

## Summary
- `atomic.Value` stores and loads arbitrary types atomically, enabling lock-free config patterns
- All `Store` calls must use the same concrete type; storing nil panics
- The hot-reload pattern: store initial config, a reloader goroutine swaps it, readers load on each request
- Never modify a value after storing it; always create a new value for updates
- Always store an initial value before any `Load` call to avoid nil type assertions
- For complex update logic (validation, rollback), wrap `Store` in a helper function

## Reference
- [atomic.Value type](https://pkg.go.dev/sync/atomic#Value)
- [Go Blog: Share Memory By Communicating](https://go.dev/blog/codelab-share)
- [Go Memory Model](https://go.dev/ref/mem)
