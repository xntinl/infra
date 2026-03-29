package main

import (
	"fmt"
	"runtime"
	"strings"
	"strconv"
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
// Multiple goroutines race to initialize the same resource.
func unsafeInit() {
	fmt.Println("=== Unsafe Initialization ===")
	var db *DatabaseConnection
	initCount := 0
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Race condition: multiple goroutines check db == nil simultaneously
			if db == nil {
				fmt.Printf("Initializing database connection... (goroutine %d)\n", getGoroutineID())
				time.Sleep(50 * time.Millisecond) // simulate expensive init
				mu.Lock()
				initCount++
				mu.Unlock()
				db = &DatabaseConnection{Host: "localhost:5432", Connected: true}
			}
		}()
	}

	wg.Wait()
	fmt.Printf("Database initialized %d times! (should be 1)\n", initCount)
}

// safeInit uses sync.Once to guarantee initialization happens exactly once.
// TODO: Declare a sync.Once and use once.Do(initDB) so that only
// the first goroutine performs initialization.
func safeInit() {
	fmt.Println("\n=== Safe Initialization (sync.Once) ===")
	var db *DatabaseConnection
	var wg sync.WaitGroup

	// TODO: declare var once sync.Once

	initDB := func() {
		fmt.Printf("Initializing database connection... (goroutine %d)\n", getGoroutineID())
		time.Sleep(100 * time.Millisecond)
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
			// TODO: replace direct call with once.Do(initDB)
			if db == nil {
				initDB()
			}
			fmt.Printf("Goroutine using db: connected=%v\n", db.Connected)
		}()
	}

	wg.Wait()
	fmt.Println("All goroutines used the same connection.")
}

// withOnceValue demonstrates sync.OnceValue for lazy initialization with a return value.
// TODO: Use sync.OnceValue to create a getConfig function that loads
// configuration exactly once and returns the cached *Config on subsequent calls.
func withOnceValue() {
	fmt.Println("\n=== sync.OnceValue (Go 1.21+) ===")

	// TODO: Replace this with sync.OnceValue(func() *Config { ... })
	loadConfig := func() *Config {
		fmt.Println("Loading configuration from disk...")
		time.Sleep(50 * time.Millisecond)
		return &Config{
			DatabaseURL: "postgres://localhost/mydb",
			MaxRetries:  3,
			Debug:       true,
		}
	}

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// TODO: call the OnceValue function instead
			cfg := loadConfig()
			fmt.Printf("Goroutine %d: DatabaseURL=%s\n", id, cfg.DatabaseURL)
		}(i)
	}

	wg.Wait()
}

// withOnceFunc demonstrates sync.OnceFunc for one-time side effects.
// TODO: Use sync.OnceFunc to wrap the logger setup so it runs only once.
func withOnceFunc() {
	fmt.Println("\n=== sync.OnceFunc (Go 1.21+) ===")

	// TODO: Wrap this with sync.OnceFunc(...)
	setupLogger := func() {
		fmt.Println("Setting up logger... (expensive one-time operation)")
		time.Sleep(50 * time.Millisecond)
		fmt.Println("Logger ready.")
	}

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			setupLogger() // TODO: after wrapping, only the first call executes
			fmt.Printf("Goroutine %d: logging after setup\n", id)
		}(i)
	}

	wg.Wait()
}
