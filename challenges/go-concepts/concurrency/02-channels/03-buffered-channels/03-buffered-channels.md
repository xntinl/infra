---
difficulty: basic
concepts: [buffered-channels, capacity, blocking, len, cap, channel-semantics]
tools: [go]
estimated_time: 20m
bloom_level: understand
---

# 3. Buffered Channels

## Learning Objectives
After completing this exercise, you will be able to:
- **Create** buffered channels with `make(chan T, capacity)`
- **Predict** when sends and receives will block based on buffer state
- **Use** `len()` and `cap()` to inspect channel state
- **Compare** buffered and unbuffered channel behavior

## Why Buffered Channels

Consider a logging pipeline. Your application goroutines generate log entries in bursts -- a single HTTP request might produce 5 log lines in quick succession. The log writer, however, flushes to disk slowly (I/O is orders of magnitude slower than in-memory operations). With an unbuffered channel, every goroutine blocks on each log call until the writer finishes the previous entry. Your application slows to the speed of your disk.

Buffered channels solve this. They have an internal queue: a send only blocks when the buffer is full, and a receive only blocks when the buffer is empty. Application goroutines "drop off" log entries and continue immediately, as long as the buffer has space. When the writer falls behind and the buffer fills up, senders block -- this is called *backpressure*, and it prevents memory from growing without bound.

This is the fundamental tradeoff: buffered channels decouple the timing of producers and consumers, absorbing bursts while still applying backpressure when the consumer is overwhelmed.

## Step 1 -- A Logging Buffer

Create a buffered channel to model a log buffer. Application code can write several log entries without blocking, and the log writer consumes them at its own pace.

```go
package main

import "fmt"

func main() {
    // Buffer can hold 5 log entries before any must be consumed.
    logBuffer := make(chan string, 5)

    // Application code writes logs. None of these block because
    // the buffer has capacity. With an unbuffered channel, each
    // would deadlock (no receiver goroutine).
    logBuffer <- "[INFO] request received: GET /api/users"
    logBuffer <- "[INFO] auth token validated"
    logBuffer <- "[INFO] query executed: SELECT * FROM users"
    logBuffer <- "[INFO] response sent: 200 OK (12ms)"

    fmt.Printf("Log buffer: %d/%d entries\n", len(logBuffer), cap(logBuffer))

    // Log writer drains entries in FIFO order.
    for len(logBuffer) > 0 {
        entry := <-logBuffer
        fmt.Println("  Written:", entry)
    }
}
```

Key difference from unbuffered: you can send four values *without* any goroutine receiving them. The buffer holds the values until they are consumed.

### Verification
```bash
go run main.go
# Expected:
#   Log buffer: 4/5 entries
#   Written: [INFO] request received: GET /api/users
#   Written: [INFO] auth token validated
#   Written: [INFO] query executed: SELECT * FROM users
#   Written: [INFO] response sent: 200 OK (12ms)
```

## Step 2 -- Backpressure: What Happens When the Buffer Is Full

Fill the log buffer completely, then try to write one more entry. The sender blocks until the writer drains at least one entry -- this is backpressure protecting you from unbounded memory growth.

```go
package main

import (
    "fmt"
    "time"
)

func main() {
    logBuffer := make(chan string, 2)
    logBuffer <- "[ERROR] connection timeout"
    logBuffer <- "[ERROR] retry failed"
    fmt.Printf("Buffer state: %d/%d (full)\n", len(logBuffer), cap(logBuffer))

    // Simulate a slow log writer that drains one entry after 500ms.
    go func() {
        time.Sleep(500 * time.Millisecond)
        entry := <-logBuffer
        fmt.Printf("Writer drained: %s\n", entry)
    }()

    fmt.Println("App: writing 3rd log entry (will block -- buffer full)...")
    logBuffer <- "[WARN] circuit breaker tripped"
    fmt.Println("App: 3rd entry written (writer made room)")
}
```

The send blocks for ~500ms because the buffer is full. Once the writer receives a value and makes room, the send completes. Without this backpressure, the application would silently drop logs or consume memory without bound.

### Verification
```bash
go run main.go
# Expected:
#   Buffer state: 2/2 (full)
#   App: writing 3rd log entry (will block -- buffer full)...
#   Writer drained: [ERROR] connection timeout
#   App: 3rd entry written (writer made room)
```

## Step 3 -- Monitoring the Buffer with len() and cap()

`len(ch)` returns the number of values currently in the buffer. `cap(ch)` returns the total capacity. These are useful for diagnostics (dashboards, metrics) but should NOT be used for synchronization -- the values change between checking and acting in concurrent code.

```go
package main

import "fmt"

func main() {
    logBuffer := make(chan string, 5)
    fmt.Printf("Empty:       %d/%d\n", len(logBuffer), cap(logBuffer))

    logBuffer <- "[INFO] startup"
    logBuffer <- "[INFO] ready"
    fmt.Printf("After 2:     %d/%d\n", len(logBuffer), cap(logBuffer))

    <-logBuffer
    fmt.Printf("After drain: %d/%d\n", len(logBuffer), cap(logBuffer))

    logBuffer <- "[WARN] high latency"
    logBuffer <- "[ERROR] timeout"
    logBuffer <- "[ERROR] retry"
    logBuffer <- "[INFO] recovered"
    fmt.Printf("After burst: %d/%d\n", len(logBuffer), cap(logBuffer))
}
```

### Verification
```bash
go run main.go
# Expected:
#   Empty:       0/5
#   After 2:     2/5
#   After drain: 1/5
#   After burst: 5/5
```

## Step 4 -- Unbuffered vs Buffered: The Timing Difference

This comparison demonstrates why buffered channels matter for logging. With an unbuffered channel, the application waits for each log write. With a buffered channel, the application writes all logs instantly and continues its work.

```go
package main

import (
    "fmt"
    "time"
)

func main() {
    // --- Unbuffered: app blocks on every log call ---
    fmt.Println("=== Unbuffered (app blocks each time) ===")
    unbuffered := make(chan string)
    start := time.Now()

    go func() {
        for i := 0; i < 5; i++ {
            entry := <-unbuffered
            _ = entry
            time.Sleep(100 * time.Millisecond) // simulate disk write
        }
    }()

    for i := 1; i <= 5; i++ {
        unbuffered <- fmt.Sprintf("[INFO] request %d", i)
        fmt.Printf("  Logged request %d at +%v\n", i, time.Since(start).Round(time.Millisecond))
    }
    fmt.Printf("  Total: %v (app was blocked by disk)\n\n", time.Since(start).Round(time.Millisecond))

    // --- Buffered: app fires all logs and moves on ---
    fmt.Println("=== Buffered (cap=5, app writes instantly) ===")
    buffered := make(chan string, 5)
    start = time.Now()

    for i := 1; i <= 5; i++ {
        buffered <- fmt.Sprintf("[INFO] request %d", i)
        fmt.Printf("  Logged request %d at +%v\n", i, time.Since(start).Round(time.Millisecond))
    }
    fmt.Printf("  Total: %v (app continued immediately)\n", time.Since(start).Round(time.Millisecond))

    // Drain buffered entries (log writer catches up later).
    for len(buffered) > 0 {
        <-buffered
    }
}
```

### Verification
```bash
go run main.go
# Unbuffered logs are spaced ~100ms apart (blocked by disk write)
# Buffered logs complete in <1ms (all 5 fit in buffer)
```

## Step 5 -- Full Logging Pipeline with Backpressure

A realistic logging pipeline where the application generates log entries 3x faster than the writer can flush them. Watch the buffer fill up, block the fast producer, then drain as the writer catches up.

```go
package main

import (
    "fmt"
    "time"
)

func main() {
    logBuffer := make(chan string, 3)
    done := make(chan struct{})

    // Application: generates logs every 50ms (fast).
    go func() {
        levels := []string{"INFO", "WARN", "ERROR", "DEBUG", "INFO",
            "ERROR", "INFO", "WARN", "INFO", "INFO"}
        for i, level := range levels {
            entry := fmt.Sprintf("[%s] event-%d", level, i+1)
            logBuffer <- entry
            fmt.Printf("App queued:  %-25s | buffer: %d/%d\n",
                entry, len(logBuffer), cap(logBuffer))
            time.Sleep(50 * time.Millisecond)
        }
        close(logBuffer)
    }()

    // Writer: flushes to "disk" every 150ms (slow -- 3x slower).
    go func() {
        for entry := range logBuffer {
            fmt.Printf("Writer flush: %-25s | buffer: %d/%d\n",
                entry, len(logBuffer), cap(logBuffer))
            time.Sleep(150 * time.Millisecond)
        }
        done <- struct{}{}
    }()

    <-done
    fmt.Println("All log entries flushed")
}
```

### Verification
```bash
go run main.go
# You will see the buffer fill to 3/3, then the app blocks until the writer drains
```

## Intermediate Verification

Run the programs and confirm:
1. Buffered sends succeed without a receiver (up to capacity)
2. A full buffer blocks the sender until the consumer makes room
3. The unbuffered version is significantly slower for the application than the buffered version

## Common Mistakes

### Using Buffer Size as a Replacement for Proper Design

**Wrong:**
```go
package main

func main() {
    ch := make(chan string, 10000)
    // "I'll just make the buffer huge so it never fills"
    for i := 0; i < 20000; i++ {
        ch <- "log entry" // blocks at entry 10001!
    }
}
```

**What happens:** If you produce more than the buffer holds, you block. A large buffer hides the problem temporarily but does not solve it. In production, the burst will eventually exceed your buffer and the application stalls anyway.

**Fix:** Size the buffer based on expected burst sizes and consumer throughput, not as a substitute for backpressure handling.

### Checking len() Before Sending

**Wrong:**
```go
// In concurrent code:
if len(logBuffer) < cap(logBuffer) {
    logBuffer <- entry // RACE: another goroutine might have filled it
}
```

**What happens:** Between checking `len()` and sending, another goroutine might fill the buffer. The send still blocks.

**Fix:** Just send. If you need non-blocking behavior, use `select` with a `default` case (covered in the select section).

## Verify What You Learned
1. What is the difference between `make(chan string)` and `make(chan string, 10)`?
2. What happens when you send to a full buffered channel?
3. When would you choose a buffered channel over an unbuffered one in a logging system?

## What's Next
Continue to [04-channel-direction](../04-channel-direction/04-channel-direction.md) to learn how directional channel types enforce correct usage at compile time.

## Summary
- `make(chan T, n)` creates a channel with buffer capacity `n`
- Sends block only when the buffer is full; receives block only when empty
- `len(ch)` returns current items in buffer; `cap(ch)` returns total capacity
- Buffered channels absorb bursts between fast producers and slow consumers
- Backpressure (blocking when full) prevents unbounded memory growth
- Buffer size is not a synchronization mechanism -- choose it based on burst size and throughput
- Unbuffered = synchronization point; Buffered = async queue with backpressure

## Reference
- [A Tour of Go: Buffered Channels](https://go.dev/tour/concurrency/3)
- [Effective Go: Channels](https://go.dev/doc/effective_go#channels)
- [Go Spec: Making channels](https://go.dev/ref/spec#Making_slices_maps_and_channels)
