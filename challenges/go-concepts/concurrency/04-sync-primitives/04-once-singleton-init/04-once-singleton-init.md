# 4. Once: Singleton Initialization

<!--
difficulty: intermediate
concepts: [sync.Once, Do, sync.OnceValue, sync.OnceFunc, lazy initialization, thread safety]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [goroutines, sync.Mutex, sync.WaitGroup]
-->

## Prerequisites
- Go 1.22+ installed
- Understanding of `sync.Mutex` and race conditions
- Familiarity with `sync.WaitGroup`

## Learning Objectives
After completing this exercise, you will be able to:
- **Use** `sync.Once` to guarantee code runs exactly once across all goroutines
- **Fix** double-initialization bugs in concurrent code
- **Apply** `sync.OnceValue` and `sync.OnceFunc` for cleaner lazy initialization (Go 1.21+)
- **Explain** why `sync.Once` is preferred over manual mutex-based singleton patterns

## Why sync.Once
Lazy initialization -- creating expensive resources only when first needed -- is a common pattern. But in concurrent programs, multiple goroutines might try to initialize the resource simultaneously, leading to double initialization, wasted work, or corrupted state.

You might think a mutex solves this:
```go
mu.Lock()
if instance == nil {
    instance = createExpensiveResource()
}
mu.Unlock()
```

This works but has a cost: every subsequent access pays the lock overhead even though initialization is already done. The "double-checked locking" optimization is notoriously tricky to get right across memory models.

`sync.Once` solves this elegantly. It guarantees that the function passed to `Do` executes exactly once, regardless of how many goroutines call it concurrently. After the first call completes, all subsequent calls return immediately with zero overhead. It is the idiomatic Go solution for one-time initialization.

Go 1.21 added `sync.OnceValue` and `sync.OnceFunc`, which wrap the pattern for functions that return values or that should be stored as callable closures.

## Step 1 -- The Double Initialization Problem

Open `main.go`. The `unsafeInit` function shows what happens when multiple goroutines race to initialize a shared resource:

```bash
go run main.go
```

Expected output:
```
=== Unsafe Initialization ===
Initializing database connection... (goroutine X)
Initializing database connection... (goroutine Y)
Initializing database connection... (goroutine Z)
Database initialized 3 times! (should be 1)
```

Multiple goroutines create separate connections, wasting resources and potentially causing conflicts.

### Intermediate Verification
Run several times. The initialization count should vary but consistently be greater than 1.

## Step 2 -- Fix with sync.Once

Implement `safeInit` using `sync.Once`:

```go
func safeInit() {
    fmt.Println("\n=== Safe Initialization (sync.Once) ===")
    var once sync.Once
    var db *DatabaseConnection
    var wg sync.WaitGroup

    initDB := func() {
        fmt.Printf("Initializing database connection... (goroutine %d)\n", getGoroutineID())
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
            once.Do(initDB) // only the first caller executes initDB
            fmt.Printf("Goroutine using db: connected=%v\n", db.Connected)
        }()
    }

    wg.Wait()
    fmt.Println("All goroutines used the same connection.")
}
```

### Intermediate Verification
```bash
go run main.go
```
You should see "Initializing database connection..." printed exactly once, followed by 10 goroutines confirming `connected=true`.

## Step 3 -- sync.OnceValue for Return Values

Go 1.21 introduced `sync.OnceValue` for functions that return a value:

```go
func withOnceValue() {
    fmt.Println("\n=== sync.OnceValue (Go 1.21+) ===")

    getConfig := sync.OnceValue(func() *Config {
        fmt.Println("Loading configuration from disk...")
        time.Sleep(50 * time.Millisecond)
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
            cfg := getConfig() // returns the cached *Config after first call
            fmt.Printf("Goroutine %d: DatabaseURL=%s\n", id, cfg.DatabaseURL)
        }(i)
    }

    wg.Wait()
}
```

`sync.OnceValue` returns a function that, on first call, executes the initializer and caches the result. All subsequent calls return the cached value without re-executing.

### Intermediate Verification
```bash
go run main.go
```
"Loading configuration from disk..." should appear exactly once. All goroutines should print the same DatabaseURL.

## Step 4 -- sync.OnceFunc for Side Effects

`sync.OnceFunc` wraps a function with no return value, useful for one-time setup:

```go
func withOnceFunc() {
    fmt.Println("\n=== sync.OnceFunc (Go 1.21+) ===")

    setupLogger := sync.OnceFunc(func() {
        fmt.Println("Setting up logger... (expensive one-time operation)")
        time.Sleep(50 * time.Millisecond)
        fmt.Println("Logger ready.")
    })

    var wg sync.WaitGroup
    for i := 0; i < 5; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            setupLogger() // only first call executes the function
            fmt.Printf("Goroutine %d: logging after setup\n", id)
        }(i)
    }

    wg.Wait()
}
```

### Intermediate Verification
```bash
go run main.go
```
"Setting up logger..." appears once. All goroutines log after setup is complete.

## Common Mistakes

### Passing Different Functions to Do
**Wrong:**
```go
var once sync.Once
once.Do(func() { fmt.Println("first") })
once.Do(func() { fmt.Println("second") }) // never executes
```
**What happens:** The second function is silently ignored. `sync.Once` guarantees exactly one execution, regardless of which function is passed.

**Key insight:** `sync.Once` tracks whether `Do` has been called, not which function was passed.

### Deadlock Inside Once
**Wrong:**
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
If the function passed to `Do` panics, `sync.Once` still considers it "done". Subsequent calls to `Do` will not re-execute the function. If initialization can fail, handle errors inside the function or use a different pattern.

## Verify What You Learned

Create a `ServiceRegistry` that lazily initializes three services (database, cache, message queue) using `sync.OnceValue`. Each service should be initialized independently on first access. Write a test that accesses all three services from 100 concurrent goroutines and verifies each is initialized exactly once.

## What's Next
Continue to [05-pool-object-reuse](../05-pool-object-reuse/05-pool-object-reuse.md) to learn how `sync.Pool` recycles objects to reduce garbage collection pressure.

## Summary
- `sync.Once` guarantees a function executes exactly once, even with concurrent callers
- All concurrent callers block until the first execution completes, then return immediately
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
