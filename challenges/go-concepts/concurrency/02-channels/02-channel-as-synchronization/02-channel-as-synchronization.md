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

const (
	configLoadTime   = 100 * time.Millisecond
	cacheWarmTime    = 200 * time.Millisecond
	dbConnectTime    = 400 * time.Millisecond
	fragileWaitGuess = 300 * time.Millisecond
)

// simulateInit pretends to initialize a component for the given duration.
func simulateInit(name string, duration time.Duration) {
	fmt.Printf("  [%s] loading...\n", name)
	time.Sleep(duration)
	fmt.Printf("  [%s] done\n", name)
}

func main() {
	fmt.Println("Server: starting initialization...")

	go simulateInit("config", configLoadTime)
	go simulateInit("cache", cacheWarmTime)
	go simulateInit("db", dbConnectTime)

	// Hope that 300ms is enough... but database needs 400ms.
	time.Sleep(fragileWaitGuess)
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

const componentCount = 3

// initComponent simulates initialization work and signals completion
// by sending its name on the done channel.
func initComponent(name string, duration time.Duration, done chan<- string) {
	fmt.Printf("  [%s] loading...\n", name)
	time.Sleep(duration)
	done <- name
}

// waitForComponents receives exactly count signals from the done
// channel, printing each component as it becomes ready.
func waitForComponents(done <-chan string, count int) {
	for i := 0; i < count; i++ {
		component := <-done
		fmt.Printf("  [%s] ready\n", component)
	}
}

func main() {
	fmt.Println("Server: starting initialization...")
	done := make(chan string)

	go initComponent("config", 100*time.Millisecond, done)
	go initComponent("cache", 200*time.Millisecond, done)
	go initComponent("db", 400*time.Millisecond, done)

	waitForComponents(done, componentCount)
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

// signalWhenReady simulates initialization work and closes the signal
// channel when complete. Closing (instead of sending) makes the intent
// unambiguous: this is purely a synchronization signal.
func signalWhenReady(name string, duration time.Duration, signal chan struct{}) {
	time.Sleep(duration)
	fmt.Printf("  [%s] ready\n", name)
	signal <- struct{}{}
}

func main() {
	fmt.Println("Server: starting initialization...")
	configReady := make(chan struct{})
	cacheReady := make(chan struct{})
	dbReady := make(chan struct{})

	go signalWhenReady("config", 100*time.Millisecond, configReady)
	go signalWhenReady("cache", 200*time.Millisecond, cacheReady)
	go signalWhenReady("db", 400*time.Millisecond, dbReady)

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

// InitResult carries both the outcome and timing of an initialization task.
type InitResult struct {
	Component string
	Status    string
	Duration  time.Duration
}

// runInitTask simulates initialization work and reports the result
// (including elapsed time) on the results channel.
func runInitTask(component string, status string, workDuration time.Duration, results chan<- InitResult) {
	start := time.Now()
	time.Sleep(workDuration)
	results <- InitResult{
		Component: component,
		Status:    status,
		Duration:  time.Since(start),
	}
}

// collectResults receives exactly count results and prints each one.
func collectResults(results <-chan InitResult, count int) {
	for i := 0; i < count; i++ {
		r := <-results
		fmt.Printf("  [%s] %s (took %v)\n",
			r.Component, r.Status, r.Duration.Round(time.Millisecond))
	}
}

func main() {
	fmt.Println("Server: starting initialization...")
	results := make(chan InitResult)

	go runInitTask("config", "loaded 47 settings", 100*time.Millisecond, results)
	go runInitTask("cache", "warmed 1200 entries", 200*time.Millisecond, results)
	go runInitTask("database", "connected to postgres://prod:5432", 400*time.Millisecond, results)

	collectResults(results, 3)
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

const expectedSequentialTime = 780 * time.Millisecond

// StartupTask defines a named initialization task with its expected duration.
type StartupTask struct {
	Name     string
	Duration time.Duration
}

// runTask simulates an initialization task and signals completion on the done channel.
func runTask(task StartupTask, done chan<- struct{}) {
	fmt.Printf("  [init] %s (%v)\n", task.Name, task.Duration)
	time.Sleep(task.Duration)
	done <- struct{}{}
}

// launchAllTasks starts every task concurrently and waits for all to finish.
func launchAllTasks(tasks []StartupTask) {
	done := make(chan struct{})

	for _, task := range tasks {
		go runTask(task, done)
	}

	for range tasks {
		<-done
	}
}

func main() {
	tasks := []StartupTask{
		{"load TLS certs", 100 * time.Millisecond},
		{"parse config", 50 * time.Millisecond},
		{"warm cache", 200 * time.Millisecond},
		{"connect to database", 400 * time.Millisecond},
		{"register health check", 30 * time.Millisecond},
	}

	start := time.Now()
	launchAllTasks(tasks)
	elapsed := time.Since(start).Round(10 * time.Millisecond)

	fmt.Printf("Total startup time: %v (parallel -- not %v sequential)\n",
		elapsed, expectedSequentialTime)
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
