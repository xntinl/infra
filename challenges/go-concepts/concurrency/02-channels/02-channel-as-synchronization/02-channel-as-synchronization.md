---
difficulty: basic
concepts: [channels, synchronization, done-channel, signaling, goroutine-coordination]
tools: [go]
estimated_time: 20m
bloom_level: apply
---

# 2. Channel as Synchronization

## Learning Objectives
After completing this exercise, you will be able to:
- **Replace** fragile `time.Sleep` synchronization with channel-based signaling
- **Implement** the done-channel pattern to wait for goroutine completion
- **Explain** why channel synchronization is deterministic while sleep is not

## Why Channel Synchronization

When a web server starts, it needs to complete several initialization tasks before it can accept traffic: load configuration from disk, warm up the cache, and establish a database connection. Each of these tasks runs concurrently, but the server must not start listening until all of them are done.

A naive approach uses `time.Sleep` -- "wait 2 seconds and hope everything finishes." This is a guess, not a guarantee. On a slow machine, under heavy load, or when the database is remote, that guess fails silently. The server starts accepting requests before the database is connected, and users get 500 errors.

Channels give you a guarantee instead of a guess. When you receive from a done channel, you know the goroutine has completed because it sent the signal. Whether the work took 1ms or 10 seconds, the receiver waits exactly as long as needed.

## Step 1 -- The Fragile Sleep Version

Start with a web server startup that uses `time.Sleep` to wait for initialization. Observe how it breaks when a task takes longer than expected.

```go
package main

import (
    "fmt"
    "time"
)

func main() {
    fmt.Println("Server: starting initialization...")

    go func() {
        fmt.Println("  [config] loading configuration...")
        time.Sleep(100 * time.Millisecond)
        fmt.Println("  [config] done")
    }()

    go func() {
        fmt.Println("  [cache] warming cache...")
        time.Sleep(200 * time.Millisecond)
        fmt.Println("  [cache] done")
    }()

    go func() {
        fmt.Println("  [db] connecting to database...")
        time.Sleep(400 * time.Millisecond) // slow connection
        fmt.Println("  [db] done")
    }()

    // Hope that 300ms is enough... but database needs 400ms.
    time.Sleep(300 * time.Millisecond)
    fmt.Println("Server: listening on :8080 (database NOT ready!)")
}
```

The database connection needs 400ms but main only waits 300ms. The server starts accepting requests before the database is ready -- a real bug that causes errors in production.

### Verification
```bash
go run main.go
# You will see the database "done" message is missing or appears after "listening"
```

## Step 2 -- Convert to Done Channels

Replace `time.Sleep` with a done channel. Each initialization task signals completion by sending on the channel. Main receives once per task, guaranteeing all finish before the server starts listening.

```go
package main

import (
    "fmt"
    "time"
)

func main() {
    fmt.Println("Server: starting initialization...")
    done := make(chan string)

    go func() {
        fmt.Println("  [config] loading configuration...")
        time.Sleep(100 * time.Millisecond)
        done <- "config"
    }()

    go func() {
        fmt.Println("  [cache] warming cache...")
        time.Sleep(200 * time.Millisecond)
        done <- "cache"
    }()

    go func() {
        fmt.Println("  [db] connecting to database...")
        time.Sleep(400 * time.Millisecond)
        done <- "db"
    }()

    // Receive once per task. Blocks until ALL three have sent.
    // It does not matter if a task takes 1ms or 10s -- we wait exactly as needed.
    for i := 0; i < 3; i++ {
        component := <-done
        fmt.Printf("  [%s] ready\n", component)
    }
    fmt.Println("Server: all components initialized -- listening on :8080")
}
```

### Verification
```bash
go run main.go
# Expected: all three components print "ready", then the server starts listening
#   Server: starting initialization...
#   [config] loading configuration...
#   [cache] warming cache...
#   [db] connecting to database...
#   [config] ready
#   [cache] ready
#   [db] ready
#   Server: all components initialized -- listening on :8080
```

## Step 3 -- Signal Without Data: struct{}

When a channel is used purely for signaling (the value itself does not matter), use `chan struct{}` instead of `chan bool`. It communicates intent clearly and uses zero memory per value.

```go
package main

import (
    "fmt"
    "time"
)

func main() {
    fmt.Println("Server: starting initialization...")
    configReady := make(chan struct{})
    cacheReady := make(chan struct{})
    dbReady := make(chan struct{})

    go func() {
        time.Sleep(100 * time.Millisecond)
        fmt.Println("  [config] loaded")
        configReady <- struct{}{}
    }()

    go func() {
        time.Sleep(200 * time.Millisecond)
        fmt.Println("  [cache] warmed")
        cacheReady <- struct{}{}
    }()

    go func() {
        time.Sleep(400 * time.Millisecond)
        fmt.Println("  [db] connected")
        dbReady <- struct{}{}
    }()

    // Wait for each component individually.
    // struct{}{} carries no data -- the synchronization IS the message.
    <-configReady
    <-cacheReady
    <-dbReady
    fmt.Println("Server: all systems go -- listening on :8080")
}
```

Why `struct{}` over `bool`? Three reasons:
1. **Intent**: `chan struct{}` says "this is purely a signal" at the type level
2. **Memory**: `struct{}` is zero bytes; `bool` is one byte (negligible, but principled)
3. **Convention**: idiomatic Go uses `chan struct{}` for done/quit channels

### Verification
```bash
go run main.go
# Same deterministic behavior, but with clearer intent in the code
```

## Step 4 -- Collecting Initialization Results

In practice, initialization tasks produce results -- a config object, a cache handle, a database connection. The channel carries both the data AND the synchronization in one operation.

```go
package main

import (
    "fmt"
    "time"
)

type InitResult struct {
    Component string
    Status    string
    Duration  time.Duration
}

func main() {
    fmt.Println("Server: starting initialization...")
    results := make(chan InitResult)

    go func() {
        start := time.Now()
        time.Sleep(100 * time.Millisecond) // simulate config loading
        results <- InitResult{
            Component: "config",
            Status:    "loaded 47 settings",
            Duration:  time.Since(start),
        }
    }()

    go func() {
        start := time.Now()
        time.Sleep(200 * time.Millisecond) // simulate cache warming
        results <- InitResult{
            Component: "cache",
            Status:    "warmed 1200 entries",
            Duration:  time.Since(start),
        }
    }()

    go func() {
        start := time.Now()
        time.Sleep(400 * time.Millisecond) // simulate DB connection
        results <- InitResult{
            Component: "database",
            Status:    "connected to postgres://prod:5432",
            Duration:  time.Since(start),
        }
    }()

    for i := 0; i < 3; i++ {
        r := <-results
        fmt.Printf("  [%s] %s (took %v)\n", r.Component, r.Status, r.Duration.Round(time.Millisecond))
    }
    fmt.Println("Server: ready to accept traffic")
}
```

### Verification
```bash
go run main.go
# Expected: each component reports its status and timing, then server starts
```

## Step 5 -- Total Time Equals the Slowest Task

The power of concurrent initialization: total elapsed time equals the slowest task, not the sum. With channels, you wait exactly as long as the slowest component -- no more, no less.

```go
package main

import (
    "fmt"
    "time"
)

func main() {
    start := time.Now()
    done := make(chan struct{})

    tasks := []struct {
        name     string
        duration time.Duration
    }{
        {"load TLS certs", 100 * time.Millisecond},
        {"parse config", 50 * time.Millisecond},
        {"warm cache", 200 * time.Millisecond},
        {"connect to database", 400 * time.Millisecond},
        {"register health check", 30 * time.Millisecond},
    }

    for _, t := range tasks {
        go func(name string, d time.Duration) {
            fmt.Printf("  [init] %s (%v)\n", name, d)
            time.Sleep(d)
            done <- struct{}{}
        }(t.name, t.duration)
    }

    for range tasks {
        <-done
    }

    elapsed := time.Since(start).Round(10 * time.Millisecond)
    fmt.Printf("Total startup time: %v (parallel -- not %v sequential)\n",
        elapsed, 780*time.Millisecond)
}
```

### Verification
```bash
go run main.go
# Expected: Total startup time ~400ms (slowest task), NOT ~780ms (sum of all)
```

With `time.Sleep` you would have to guess the maximum. With channels, the wait is exactly as long as the slowest task requires.

## Intermediate Verification

Run your programs and confirm:
1. The sleep-based version misses the database initialization
2. The channel-based version always waits for all components
3. The total time is determined by the slowest task, not the sum

## Common Mistakes

### Mismatched Send/Receive Count

**Wrong:**
```go
package main

import "fmt"

func main() {
    done := make(chan struct{})
    for i := 0; i < 5; i++ {
        go func() { done <- struct{}{} }()
    }
    for i := 0; i < 3; i++ { // only waiting for 3!
        <-done
    }
    fmt.Println("done") // 2 goroutines still running (or trying to send)
}
```

**What happens:** Main exits while 2 goroutines are still running. You lose work.

**Correct:** Always match the number of receives to the number of goroutines launched:
```go
for i := 0; i < 5; i++ {
    <-done // receive 5 times for 5 goroutines
}
```

### Sending Before the Work Is Done

**Wrong:**
```go
package main

import (
    "fmt"
    "time"
)

func main() {
    done := make(chan struct{})
    go func() {
        done <- struct{}{} // signal sent BEFORE work!
        time.Sleep(1 * time.Second)
        fmt.Println("database connected")
    }()
    <-done
    fmt.Println("server: accepting traffic") // database is NOT connected yet!
}
```

**What happens:** The signal arrives before the work completes. The server starts accepting traffic with no database connection.

**Correct:** Always send the done signal as the LAST operation in the goroutine.

## Verify What You Learned
1. Why does `time.Sleep` fail as a synchronization mechanism?
2. What happens if a goroutine sends on the done channel before completing its work?
3. When would you use `chan struct{}` instead of `chan string` for signaling?

## What's Next
Continue to [03-buffered-channels](../03-buffered-channels/03-buffered-channels.md) to learn how buffered channels decouple senders from receivers.

## Summary
- `time.Sleep` for synchronization is fragile -- it guesses instead of guaranteeing
- Done channels provide deterministic synchronization: receive blocks until the sender signals
- Use `chan struct{}` for pure signaling where the value does not matter
- To wait for N goroutines, perform N receives on the done channel
- Always send the done signal as the last operation in the goroutine
- Result channels combine synchronization with data transfer
- Total concurrent time equals the slowest task, not the sum of all tasks

## Reference
- [Go Blog: Share Memory By Communicating](https://go.dev/blog/codelab-share)
- [Go Blog: Go Concurrency Patterns](https://go.dev/blog/concurrency-patterns)
- [Effective Go: Channels](https://go.dev/doc/effective_go#channels)
