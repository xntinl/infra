# 3. Buffered Channels

<!--
difficulty: basic
concepts: [buffered-channels, capacity, blocking, len, cap, channel-semantics]
tools: [go]
estimated_time: 20m
bloom_level: understand
prerequisites: [goroutines, unbuffered-channels]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01-02 (unbuffered channels, synchronization)
- Understanding of send/receive blocking behavior

## Learning Objectives
After completing this exercise, you will be able to:
- **Create** buffered channels with `make(chan T, capacity)`
- **Predict** when sends and receives will block based on buffer state
- **Use** `len()` and `cap()` to inspect channel state
- **Compare** buffered and unbuffered channel behavior

## Why Buffered Channels

Unbuffered channels require both sender and receiver to be ready at the same time. This is perfect for synchronization, but sometimes you want to decouple the sender from the receiver. A producer might generate values in bursts, or you might want to allow some goroutines to complete their sends without waiting.

Buffered channels have an internal queue. A send only blocks when the buffer is full, and a receive only blocks when the buffer is empty. This lets the sender "drop off" values without waiting for the receiver, up to the buffer's capacity.

Think of it like a mailbox: you can drop off letters (send) without the recipient being home, as long as the mailbox isn't full. The recipient can pick up letters (receive) whenever they're ready. But if the mailbox overflows, you have to wait.

## Step 1 -- Create a Buffered Channel

Create a buffered channel with capacity 3 and send values without a receiver goroutine.

```go
package main

import "fmt"

func main() {
    ch := make(chan int, 3) // buffer can hold 3 ints

    // All three sends succeed immediately -- no goroutine needed.
    // With an unbuffered channel, these would deadlock.
    ch <- 10
    ch <- 20
    ch <- 30
    // ch <- 40 would block here -- buffer is full!

    // Receives drain the buffer in FIFO order.
    fmt.Println(<-ch) // 10
    fmt.Println(<-ch) // 20
    fmt.Println(<-ch) // 30
}
```

Key difference from unbuffered: you can send three values *without* any goroutine receiving them. The buffer holds the values until they're consumed.

### Verification
```bash
go run main.go
# Expected:
#   10
#   20
#   30
```

## Step 2 -- Observe Blocking When Full

Fill the buffer completely, then try to send one more value. This blocks until someone receives.

```go
package main

import (
    "fmt"
    "time"
)

func main() {
    ch := make(chan int, 2)
    ch <- 1
    ch <- 2
    fmt.Printf("Buffer state: len=%d, cap=%d (full)\n", len(ch), cap(ch))

    go func() {
        time.Sleep(500 * time.Millisecond)
        val := <-ch
        fmt.Printf("Drained one: %d -- made room\n", val)
    }()

    fmt.Println("Sending 3rd value (will block)...")
    ch <- 3  // blocks until the goroutine receives
    fmt.Println("3rd value sent successfully!")
}
```

The send on line `ch <- 3` blocks for ~500ms because the buffer is full. Once the goroutine receives a value and makes room, the send completes.

### Verification
```bash
go run main.go
# Expected:
#   Buffer state: len=2, cap=2 (full)
#   Sending 3rd value (will block)...
#   Drained one: 1 -- made room
#   3rd value sent successfully!
```

## Step 3 -- Inspect with len() and cap()

`len(ch)` returns the number of values currently in the buffer. `cap(ch)` returns the total capacity. These are useful for diagnostics but should NOT be used for synchronization (the values can change between checking and acting).

```go
package main

import "fmt"

func main() {
    ch := make(chan string, 5)
    fmt.Printf("Empty:      len=%d  cap=%d\n", len(ch), cap(ch))

    ch <- "a"
    ch <- "b"
    fmt.Printf("After 2:    len=%d  cap=%d\n", len(ch), cap(ch))

    <-ch
    fmt.Printf("After recv: len=%d  cap=%d\n", len(ch), cap(ch))

    ch <- "c"
    ch <- "d"
    ch <- "e"
    fmt.Printf("After 3:    len=%d  cap=%d\n", len(ch), cap(ch))

    ch <- "f"
    fmt.Printf("Full:       len=%d  cap=%d\n", len(ch), cap(ch))
}
```

### Verification
```bash
go run main.go
# Expected:
#   Empty:      len=0  cap=5
#   After 2:    len=2  cap=5
#   After recv: len=1  cap=5
#   After 3:    len=4  cap=5
#   Full:       len=5  cap=5
```

## Step 4 -- Unbuffered vs Buffered Timing

This comparison demonstrates the timing difference. With an unbuffered channel, the producer must wait for each receive. With a buffered channel, the producer sends all values instantly.

```go
package main

import (
    "fmt"
    "time"
)

func main() {
    // --- Unbuffered: producer waits for each receive ---
    fmt.Println("Unbuffered (producer waits each time):")
    unbuffered := make(chan int)
    start := time.Now()

    go func() {
        for i := 1; i <= 5; i++ {
            unbuffered <- i
            fmt.Printf("  Sent %d at +%v\n", i, time.Since(start).Round(time.Millisecond))
        }
    }()

    for i := 0; i < 5; i++ {
        time.Sleep(100 * time.Millisecond)
        <-unbuffered
    }
    fmt.Printf("Unbuffered total: %v\n\n", time.Since(start).Round(time.Millisecond))

    // --- Buffered: producer sends all 5 almost instantly ---
    fmt.Println("Buffered (cap=5, producer sends instantly):")
    buffered := make(chan int, 5)
    start = time.Now()

    go func() {
        for i := 1; i <= 5; i++ {
            buffered <- i
            fmt.Printf("  Sent %d at +%v\n", i, time.Since(start).Round(time.Millisecond))
        }
    }()

    time.Sleep(10 * time.Millisecond) // let producer fill buffer
    for i := 0; i < 5; i++ {
        time.Sleep(100 * time.Millisecond)
        <-buffered
    }
    fmt.Printf("Buffered total: %v\n", time.Since(start).Round(time.Millisecond))
}
```

### Verification
```bash
go run main.go
# The unbuffered sends are spaced ~100ms apart (waiting for consumer)
# The buffered sends complete in <1ms (all 5 fit in buffer)
```

## Step 5 -- Producer-Consumer with Buffer Monitoring

A realistic producer-consumer where the producer is 3x faster than the consumer. Watch the buffer fill up, block the producer, then drain as the consumer catches up.

```go
package main

import (
    "fmt"
    "time"
)

func main() {
    ch := make(chan int, 3)
    done := make(chan struct{})

    // Producer: fast (50ms per item)
    go func() {
        for i := 1; i <= 10; i++ {
            ch <- i
            fmt.Printf("Produced: %2d | buffer: %d/%d\n", i, len(ch), cap(ch))
            time.Sleep(50 * time.Millisecond)
        }
        close(ch)
    }()

    // Consumer: slow (150ms per item) -- buffer will fill up
    go func() {
        for val := range ch {
            fmt.Printf("Consumed: %2d | buffer: %d/%d\n", val, len(ch), cap(ch))
            time.Sleep(150 * time.Millisecond)
        }
        done <- struct{}{}
    }()

    <-done
    fmt.Println("All items processed")
}
```

### Verification
```bash
go run main.go
# You'll see the buffer fill to 3/3, then the producer blocks until the consumer drains
```

## Common Mistakes

### Using Buffer Size as a Synchronization Mechanism

**Wrong:**
```go
package main

func main() {
    ch := make(chan int, 100)
    // "I'll just make the buffer big enough"
    for i := 0; i < 200; i++ {
        ch <- i // blocks at item 101!
    }
}
```

**What happens:** If you produce more than the buffer holds, you block. A large buffer hides the problem temporarily but doesn't solve it.

**Fix:** Use appropriate buffer sizes based on your throughput needs, not as a replacement for proper synchronization.

### Checking len() Before Sending

**Wrong:**
```go
// In concurrent code:
if len(ch) < cap(ch) {
    ch <- value // RACE: another goroutine might have filled it
}
```

**What happens:** Between checking `len()` and sending, another goroutine might fill the buffer. The send still blocks.

**Fix:** Just send. If you need non-blocking behavior, use `select` with a `default` case (covered in the select section).

## What's Next
Continue to [04-channel-direction](../04-channel-direction/04-channel-direction.md) to learn how directional channel types enforce correct usage at compile time.

## Summary
- `make(chan T, n)` creates a channel with buffer capacity `n`
- Sends block only when the buffer is full; receives block only when empty
- `len(ch)` returns current items in buffer; `cap(ch)` returns total capacity
- Buffered channels decouple producer and consumer timing
- Buffer size is not a synchronization mechanism -- choose it based on throughput needs
- Unbuffered = synchronization point; Buffered = async queue

## Reference
- [A Tour of Go: Buffered Channels](https://go.dev/tour/concurrency/3)
- [Effective Go: Channels](https://go.dev/doc/effective_go#channels)
- [Go Spec: Making channels](https://go.dev/ref/spec#Making_slices_maps_and_channels)
