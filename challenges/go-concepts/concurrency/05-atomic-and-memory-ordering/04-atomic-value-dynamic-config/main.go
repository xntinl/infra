package main

// Exercise: Atomic Value for Dynamic Configuration
// Instructions: see 04-atomic-value-dynamic-config.md

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ServerConfig represents application configuration that can change at runtime.
type ServerConfig struct {
	Port         int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	MaxConns     int
	Debug        bool
}

// Step 1: Implement basicAtomicValue.
// Create an atomic.Value, store a ServerConfig, load it, type-assert, and print.
func basicAtomicValue() {
	var config atomic.Value

	// TODO: store a ServerConfig{Port: 8080, ReadTimeout: 5s, WriteTimeout: 10s, MaxConns: 100, Debug: false}
	// TODO: load it, type-assert to ServerConfig, print Port, MaxConns, Debug
	_ = config.Store // hint: config.Store(ServerConfig{...})
	_ = config.Load  // hint: cfg := config.Load().(ServerConfig)
}

// Step 2: Implement dynamicConfig.
// A reloader goroutine updates the config 3 times (every 50ms).
// Three reader goroutines load the config every 20ms (10 iterations each).
func dynamicConfig() {
	var config atomic.Value
	var wg sync.WaitGroup

	// TODO: store initial config (Port: 8080, MaxConns: 100)

	// TODO: launch reloader goroutine that stores 3 updated configs at 50ms intervals
	//   Update 1: MaxConns=200
	//   Update 2: Port=9090, MaxConns=500, Debug=true
	//   Update 3: MaxConns=1000, Debug=false

	// TODO: launch 3 reader goroutines, each loading and printing config 10 times at 20ms intervals

	// TODO: wait for all goroutines
	_ = wg.Wait     // hint: use WaitGroup to synchronize
	_ = config.Store // hint: store config updates
	_ = config.Load  // hint: load current config
	_ = time.Sleep   // hint: simulate delays
}

// Step 3: Implement validatedConfig.
// Create loadConfig and getConfig helpers.
// loadConfig validates the config before storing it (reject invalid port or MaxConns).
// Demonstrate that invalid config is rejected and the old config is preserved.
func validatedConfig() {
	var config atomic.Value

	// TODO: create loadConfig function that validates Port (1-65535) and MaxConns (>0)
	//       before calling config.Store()
	// TODO: create getConfig function that loads and type-asserts

	// TODO: load a valid config, print it
	// TODO: try loading an invalid config (Port: -1), print the error
	// TODO: load config again, confirm it is unchanged
	_ = config.Store // hint: store validated config
	_ = config.Load  // hint: load current config
	_ = fmt.Errorf   // hint: return error for invalid config
}

// FeatureFlags represents toggleable feature flags.
type FeatureFlags struct {
	DarkMode  bool
	BetaAPI   bool
	RateLimit int
}

// Verify: Implement featureFlagSystem.
// 1. Store initial FeatureFlags{DarkMode: false, BetaAPI: false, RateLimit: 100}
// 2. Launch a toggler goroutine that flips DarkMode and BetaAPI every 30ms for 5 cycles
// 3. Launch 3 service goroutines that read flags every 10ms for 20 cycles
// 4. Run with -race to verify safety
func featureFlagSystem() {
	var flags atomic.Value
	var wg sync.WaitGroup

	// TODO: store initial flags
	// TODO: launch toggler goroutine
	// TODO: launch 3 service reader goroutines
	// TODO: wait for all goroutines
	_ = wg.Wait    // hint: use WaitGroup to synchronize
	_ = flags.Store // hint: store FeatureFlags
	_ = flags.Load  // hint: load current flags
}

func main() {
	fmt.Println("Exercise: Atomic Value for Dynamic Configuration")
	fmt.Println()

	fmt.Println("=== Step 1: Basic atomic.Value ===")
	basicAtomicValue()
	fmt.Println()

	fmt.Println("=== Step 2: Dynamic Config (readers + reloader) ===")
	dynamicConfig()
	fmt.Println()

	fmt.Println("=== Step 3: Validated Config ===")
	validatedConfig()
	fmt.Println()

	fmt.Println("=== Verify: Feature Flag System ===")
	featureFlagSystem()
	fmt.Println()
}
