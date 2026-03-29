package main

// Atomic Value for Dynamic Configuration — Production-quality educational code
//
// Demonstrates atomic.Value for storing and swapping arbitrary types:
// 1. Basic store and load with type assertion
// 2. Dynamic config with concurrent readers and a reloader
// 3. Validated config with rejection of invalid values
// 4. Feature flag system with toggler and service readers
//
// Expected output:
//   === Example 1: Basic atomic.Value ===
//     Port: 8080, MaxConns: 100, Debug: false
//
//   === Example 2: Dynamic Config (readers + reloader) ===
//     [reloader] Updated: Port=8080, MaxConns=200
//     [reader 0] Port=8080, MaxConns=100
//     ...
//     [reloader] Updated: Port=9090, MaxConns=500, Debug=true
//     ...
//     [reloader] Updated: Port=9090, MaxConns=1000
//     ...
//
//   === Example 3: Validated Config ===
//     Loaded: Port=8080, MaxConns=100
//     Rejected: invalid port: -1
//     Config unchanged: Port=8080, MaxConns=100
//     Updated: Port=9090, MaxConns=500
//
//   === Example 4: Feature Flag System ===
//     [toggler] cycle 0: DarkMode=true, BetaAPI=true
//     [service 0] DarkMode=false, BetaAPI=false, RateLimit=100
//     ...

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ServerConfig represents application configuration that can change at runtime.
// The struct is immutable once stored in atomic.Value -- never modify a stored
// instance. Create a new struct for each update.
type ServerConfig struct {
	Port         int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	MaxConns     int
	Debug        bool
}

// basicAtomicValue demonstrates the fundamental atomic.Value operations.
// Store() accepts any value, Load() returns interface{}, and you need a
// type assertion to recover the concrete type.
func basicAtomicValue() {
	var config atomic.Value

	// Store the initial configuration.
	// RULE: once you store a value of type T, all subsequent Store calls
	// must also use type T. Storing a different type panics.
	config.Store(ServerConfig{
		Port:         8080,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		MaxConns:     100,
		Debug:        false,
	})

	// Load returns interface{} — type assertion required
	cfg := config.Load().(ServerConfig)
	fmt.Printf("  Port: %d, MaxConns: %d, Debug: %v\n", cfg.Port, cfg.MaxConns, cfg.Debug)

	// Swap replaces the value and returns the old one (Go 1.17+)
	old := config.Swap(ServerConfig{
		Port:         9090,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 5 * time.Second,
		MaxConns:     500,
		Debug:        true,
	}).(ServerConfig)
	fmt.Printf("  Old port: %d, New port: %d\n", old.Port, config.Load().(ServerConfig).Port)
}

// dynamicConfig demonstrates the hot-reload pattern: multiple reader goroutines
// serve requests using the current config, while a reloader goroutine swaps
// the config atomically. Readers never block each other and never block the writer.
func dynamicConfig() {
	var config atomic.Value

	// Store initial config
	config.Store(ServerConfig{
		Port:         8080,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		MaxConns:     100,
		Debug:        false,
	})

	var wg sync.WaitGroup

	// Reloader: updates config every 50ms (simulates hot-reload from file/API)
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
			// Store replaces the ENTIRE config atomically. Readers never see
			// a partially updated struct — they get the old or the new, never a mix.
			config.Store(cfg)
			fmt.Printf("  [reloader] Updated: Port=%d, MaxConns=%d, Debug=%v\n",
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

// validatedConfig wraps atomic.Value with validation logic. Invalid configs
// are rejected, and the previous valid config is preserved. This is the
// production pattern: validate-then-store, never store-then-validate.
func validatedConfig() {
	var config atomic.Value

	// loadConfig validates before storing. Returns error if invalid.
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

	// getConfig loads and type-asserts the current config
	getConfig := func() ServerConfig {
		return config.Load().(ServerConfig)
	}

	// Load valid config
	if err := loadConfig(ServerConfig{
		Port: 8080, MaxConns: 100, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second,
	}); err != nil {
		fmt.Printf("  Error: %v\n", err)
		return
	}
	cfg := getConfig()
	fmt.Printf("  Loaded: Port=%d, MaxConns=%d\n", cfg.Port, cfg.MaxConns)

	// Try invalid config — rejected, old config preserved
	if err := loadConfig(ServerConfig{Port: -1, MaxConns: 100}); err != nil {
		fmt.Printf("  Rejected: %v\n", err)
	}
	cfg = getConfig()
	fmt.Printf("  Config unchanged: Port=%d, MaxConns=%d\n", cfg.Port, cfg.MaxConns)

	// Load another valid config — succeeds
	if err := loadConfig(ServerConfig{
		Port: 9090, MaxConns: 500, ReadTimeout: 3 * time.Second, WriteTimeout: 5 * time.Second,
	}); err != nil {
		fmt.Printf("  Error: %v\n", err)
		return
	}
	cfg = getConfig()
	fmt.Printf("  Updated: Port=%d, MaxConns=%d\n", cfg.Port, cfg.MaxConns)
}

// FeatureFlags represents toggleable feature flags loaded at runtime.
type FeatureFlags struct {
	DarkMode  bool
	BetaAPI   bool
	RateLimit int
}

// featureFlagSystem demonstrates concurrent flag reading and toggling.
// A toggler goroutine flips flags every 30ms. Service goroutines read flags
// every 10ms. Each reader always sees a consistent snapshot of all flags.
func featureFlagSystem() {
	var flags atomic.Value

	// Store initial flags
	flags.Store(FeatureFlags{
		DarkMode:  false,
		BetaAPI:   false,
		RateLimit: 100,
	})

	var wg sync.WaitGroup

	// Toggler: flips DarkMode and BetaAPI every 30ms for 5 cycles
	wg.Add(1)
	go func() {
		defer wg.Done()
		for cycle := 0; cycle < 5; cycle++ {
			time.Sleep(30 * time.Millisecond)
			// Read current, create new with toggles flipped
			current := flags.Load().(FeatureFlags)
			updated := FeatureFlags{
				DarkMode:  !current.DarkMode,
				BetaAPI:   !current.BetaAPI,
				RateLimit: current.RateLimit,
			}
			flags.Store(updated)
			fmt.Printf("  [toggler] cycle %d: DarkMode=%v, BetaAPI=%v\n",
				cycle, updated.DarkMode, updated.BetaAPI)
		}
	}()

	// Service readers: read flags every 10ms for 20 cycles
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				f := flags.Load().(FeatureFlags)
				// DarkMode and BetaAPI always match each other (toggled together)
				// because the atomic swap is all-or-nothing.
				fmt.Printf("  [service %d] DarkMode=%v, BetaAPI=%v, RateLimit=%d\n",
					id, f.DarkMode, f.BetaAPI, f.RateLimit)
				time.Sleep(10 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
}

func main() {
	fmt.Println("Atomic Value for Dynamic Configuration")
	fmt.Println()

	fmt.Println("=== Example 1: Basic atomic.Value ===")
	basicAtomicValue()
	fmt.Println()

	fmt.Println("=== Example 2: Dynamic Config (readers + reloader) ===")
	dynamicConfig()
	fmt.Println()

	fmt.Println("=== Example 3: Validated Config ===")
	validatedConfig()
	fmt.Println()

	fmt.Println("=== Example 4: Feature Flag System ===")
	featureFlagSystem()
	fmt.Println()
}
