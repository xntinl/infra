# 8. atomic.Value Config Hot Reload

<!--
difficulty: advanced
concepts: [atomic-value, concurrent-config, hot-reload, lock-free-reads]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [atomic-package, goroutines, structs]
-->

## Prerequisites

- Go 1.22+ installed
- Completed the atomic package exercise
- Familiarity with struct types and pointers

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** how `atomic.Value` provides lock-free reads for immutable config objects
- **Implement** a hot-reload configuration system using `atomic.Value`
- **Analyze** the trade-offs between `atomic.Value` and `sync.RWMutex` for config access

## Why atomic.Value for Config Hot Reload

In production services, configuration often needs to change without restarting the process: feature flags, rate limits, database connection strings. Every request handler reads the config, but updates are rare.

`atomic.Value` lets you store and load entire config structs atomically. Readers call `Load()` without any lock, getting a consistent snapshot. Writers call `Store()` to replace the config atomically. Since Go structs are value types, storing a pointer to an immutable config struct ensures readers always see a complete, consistent configuration.

This pattern gives you zero-contention reads with safe concurrent updates -- ideal for read-heavy config access.

## The Problem

Build a configuration management system where:
- A config struct holds application settings
- Worker goroutines read the config on every iteration
- A reloader goroutine periodically updates the config
- All reads are lock-free using `atomic.Value`

## Requirements

1. Define an immutable `Config` struct with fields like `MaxConnections`, `Timeout`, and `FeatureFlags`
2. Use `atomic.Value` (or `atomic.Pointer[Config]` for Go 1.19+) to store the config
3. Multiple reader goroutines access the config without locking
4. A writer goroutine periodically replaces the config
5. No data races under the race detector

## Hints

<details>
<summary>Hint 1: atomic.Value Basics</summary>

```go
var config atomic.Value
config.Store(&Config{MaxConn: 100})
cfg := config.Load().(*Config)
```

All values stored must be the same concrete type.
</details>

<details>
<summary>Hint 2: Using atomic.Pointer (Go 1.19+)</summary>

```go
var config atomic.Pointer[Config]
config.Store(&Config{MaxConn: 100})
cfg := config.Load()  // returns *Config directly, no type assertion
```
</details>

<details>
<summary>Hint 3: Complete Implementation</summary>

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type Config struct {
	MaxConnections int
	TimeoutMs      int
	FeatureFlags   map[string]bool
	Version        int
}

type ConfigManager struct {
	config atomic.Pointer[Config]
}

func NewConfigManager(initial *Config) *ConfigManager {
	cm := &ConfigManager{}
	cm.config.Store(initial)
	return cm
}

func (cm *ConfigManager) Get() *Config {
	return cm.config.Load()
}

func (cm *ConfigManager) Update(newConfig *Config) {
	cm.config.Store(newConfig)
}

func main() {
	initial := &Config{
		MaxConnections: 100,
		TimeoutMs:      5000,
		FeatureFlags:   map[string]bool{"dark_mode": false, "beta_api": false},
		Version:        1,
	}

	cm := NewConfigManager(initial)
	var wg sync.WaitGroup

	// 10 reader goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				cfg := cm.Get()
				if j%25 == 0 {
					fmt.Printf("Reader %d: version=%d, maxConn=%d, darkMode=%v\n",
						id, cfg.Version, cfg.MaxConnections, cfg.FeatureFlags["dark_mode"])
				}
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	// Config updater
	wg.Add(1)
	go func() {
		defer wg.Done()
		for v := 2; v <= 4; v++ {
			time.Sleep(15 * time.Millisecond)
			newCfg := &Config{
				MaxConnections: 100 * v,
				TimeoutMs:      5000 / v,
				FeatureFlags:   map[string]bool{"dark_mode": v >= 3, "beta_api": v >= 4},
				Version:        v,
			}
			cm.Update(newCfg)
			fmt.Printf("*** Config updated to version %d ***\n", v)
		}
	}()

	wg.Wait()

	final := cm.Get()
	fmt.Printf("\nFinal config: version=%d, maxConn=%d\n", final.Version, final.MaxConnections)
}
```
</details>

## Verification

```bash
go run -race main.go
```

Expected: Readers see progressively newer config versions. No race conditions. The final config is version 4 with `MaxConnections=400`.

## What's Next

Continue to [09 - Lock Ordering and Deadlock Prevention](../09-lock-ordering-deadlock-prevention/09-lock-ordering-deadlock-prevention.md) to learn how to avoid deadlocks when using multiple mutexes.

## Summary

- `atomic.Value` stores and loads values atomically without locks
- Ideal for read-heavy patterns like configuration, where reads vastly outnumber writes
- Store pointers to immutable structs to ensure readers get consistent snapshots
- `atomic.Pointer[T]` (Go 1.19+) provides type-safe access without type assertions
- Never mutate a config struct after storing it -- always create a new one

## Reference

- [atomic.Value documentation](https://pkg.go.dev/sync/atomic#Value)
- [atomic.Pointer documentation](https://pkg.go.dev/sync/atomic#Pointer)
