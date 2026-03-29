// Exercise 04: sync.Once -- Singleton Initialization
//
// Demonstrates guaranteed one-time execution for lazy initialization.
// Covers: sync.Once Do(), sync.OnceValue, sync.OnceFunc, double-init problem.
//
// Expected output (approximate):
//
//   === 1. Unsafe Initialization (race condition) ===
//   Initializing database... (goroutine X)
//   Initializing database... (goroutine Y)
//   ...
//   Database initialized 5+ times! (should be 1)
//
//   === 2. Safe Initialization (sync.Once) ===
//   Initializing database... (goroutine X)
//   Database initialized successfully.
//   Goroutine using db: connected=true
//   ... (10 goroutines, all see connected=true)
//   All 10 goroutines used the same connection.
//
//   === 3. sync.OnceValue -- Lazy Config ===
//   Loading configuration from disk...
//   Goroutine 0: DatabaseURL=postgres://localhost/mydb
//   ... (5 goroutines, all same config)
//   Config loaded once, used 5 times.
//
//   === 4. sync.OnceFunc -- One-Time Setup ===
//   Setting up logger... (expensive one-time operation)
//   Logger ready.
//   Goroutine 0: logging after setup
//   ... (5 goroutines)
//   All goroutines completed.
//
//   === 5. Important Behaviors ===
//   First function executed
//   Second function was ignored (Once tracks calls, not functions)
//   Once with panic: recovered from: init failed
//   Subsequent call: panic considered "done" -- function not re-executed
//
// Run: go run main.go

package main

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DatabaseConnection represents an expensive-to-create resource.
type DatabaseConnection struct {
	Host      string
	Connected bool
}

// Config represents application configuration loaded from disk.
type Config struct {
	DatabaseURL string
	MaxRetries  int
	Debug       bool
}

func main() {
	unsafeInit()
	safeInit()
	withOnceValue()
	withOnceFunc()
	importantBehaviors()
}

// getGoroutineID extracts the goroutine ID for demonstration purposes.
// NOTE: This is for educational use only -- never use goroutine IDs in production.
func getGoroutineID() int {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	field := strings.Fields(strings.TrimPrefix(string(buf[:n]), "goroutine "))[0]
	id, _ := strconv.Atoi(field)
	return id
}

// unsafeInit demonstrates the double-initialization problem.
// Multiple goroutines race to check db == nil and all see nil simultaneously,
// causing the resource to be created multiple times.
func unsafeInit() {
	fmt.Println("=== 1. Unsafe Initialization (race condition) ===")

	var db *DatabaseConnection
	initCount := 0
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Race: multiple goroutines check db == nil at the same instant.
			// All see nil and proceed to initialize.
			if db == nil {
				fmt.Printf("Initializing database... (goroutine %d)\n", getGoroutineID())
				time.Sleep(50 * time.Millisecond) // simulate expensive init
				mu.Lock()
				initCount++
				mu.Unlock()
				db = &DatabaseConnection{Host: "localhost:5432", Connected: true}
			}
		}()
	}

	wg.Wait()
	fmt.Printf("Database initialized %d times! (should be 1)\n\n", initCount)
}

// safeInit uses sync.Once to guarantee initialization happens exactly once.
// All concurrent callers of once.Do(f) block until the first call completes.
// After that, subsequent calls return immediately with zero overhead.
func safeInit() {
	fmt.Println("=== 2. Safe Initialization (sync.Once) ===")

	var once sync.Once
	var db *DatabaseConnection
	var wg sync.WaitGroup

	initDB := func() {
		fmt.Printf("Initializing database... (goroutine %d)\n", getGoroutineID())
		time.Sleep(100 * time.Millisecond) // simulate expensive init
		db = &DatabaseConnection{
			Host:      "localhost:5432",
			Connected: true,
		}
		fmt.Println("Database initialized successfully.")
	}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			once.Do(initDB) // only the first goroutine executes initDB
			// All goroutines safely access db after this point because
			// once.Do provides a happens-before guarantee.
			fmt.Printf("Goroutine using db: connected=%v\n", db.Connected)
		}()
	}

	wg.Wait()
	fmt.Println("All 10 goroutines used the same connection.")
	fmt.Println()
}

// withOnceValue demonstrates sync.OnceValue for lazy initialization with a return value.
// OnceValue wraps a function that returns a value: the function executes on the first
// call, and all subsequent calls return the cached result without re-executing.
func withOnceValue() {
	fmt.Println("=== 3. sync.OnceValue -- Lazy Config ===")

	// sync.OnceValue returns a function that caches the result of the first call.
	// The type parameter ensures type safety -- no type assertions needed.
	getConfig := sync.OnceValue(func() *Config {
		fmt.Println("Loading configuration from disk...")
		time.Sleep(50 * time.Millisecond) // simulate disk I/O
		return &Config{
			DatabaseURL: "postgres://localhost/mydb",
			MaxRetries:  3,
			Debug:       true,
		}
	})

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			cfg := getConfig() // first call loads, subsequent calls return cached
			fmt.Printf("Goroutine %d: DatabaseURL=%s\n", id, cfg.DatabaseURL)
		}(i)
	}

	wg.Wait()
	fmt.Println("Config loaded once, used 5 times.")
	fmt.Println()
}

// withOnceFunc demonstrates sync.OnceFunc for one-time side effects.
// OnceFunc wraps a function with no return value so it runs only once.
// Unlike Once.Do, the result is a standalone callable function.
func withOnceFunc() {
	fmt.Println("=== 4. sync.OnceFunc -- One-Time Setup ===")

	// sync.OnceFunc returns a function that executes the wrapped function
	// on the first call and is a no-op on subsequent calls.
	setupLogger := sync.OnceFunc(func() {
		fmt.Println("Setting up logger... (expensive one-time operation)")
		time.Sleep(50 * time.Millisecond) // simulate setup
		fmt.Println("Logger ready.")
	})

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			setupLogger() // only the first call executes the setup
			fmt.Printf("Goroutine %d: logging after setup\n", id)
		}(i)
	}

	wg.Wait()
	fmt.Println("All goroutines completed.")
	fmt.Println()
}

// importantBehaviors demonstrates edge cases and important properties of sync.Once.
func importantBehaviors() {
	fmt.Println("=== 5. Important Behaviors ===")

	// Behavior 1: Once tracks whether Do was called, not which function was passed.
	// The second function is NEVER executed.
	var once1 sync.Once
	once1.Do(func() { fmt.Println("First function executed") })
	once1.Do(func() { fmt.Println("This will never print") })
	fmt.Println("Second function was ignored (Once tracks calls, not functions)")

	// Behavior 2: If the function panics, Once still considers it "done".
	// Subsequent calls will NOT re-execute -- the panic is the final state.
	var once2 sync.Once
	func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("Once with panic: recovered from: %v\n", r)
			}
		}()
		once2.Do(func() {
			panic("init failed")
		})
	}()
	once2.Do(func() {
		fmt.Println("This will never print either")
	})
	fmt.Println("Subsequent call: panic considered \"done\" -- function not re-executed")

	// Behavior 3: Recursive Do on the same Once deadlocks.
	// (Not demonstrated because it would hang the program.)
	// var once3 sync.Once
	// once3.Do(func() {
	//     once3.Do(func() { ... }) // DEADLOCK: inner Do waits for outer to finish
	// })
}
