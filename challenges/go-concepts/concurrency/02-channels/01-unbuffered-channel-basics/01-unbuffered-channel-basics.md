---
difficulty: basic
concepts: [channels, make, send, receive, synchronization, goroutines]
tools: [go]
estimated_time: 15m
bloom_level: understand
prerequisites: [goroutines, go-basics]
---

# 1. Unbuffered Channel Basics


## Learning Objectives
After completing this exercise, you will be able to:
- **Create** unbuffered channels using `make(chan T)`
- **Send** and **receive** values through channels using the `<-` operator
- **Explain** why unbuffered channels force synchronization between goroutines

## Why Unbuffered Channels

Channels are Go's primary mechanism for communication between goroutines. While goroutines give you concurrency, channels give you *coordination*. Without channels, goroutines would be isolated workers with no safe way to share results.

An unbuffered channel has zero capacity -- it cannot hold any value. This means a send operation blocks until another goroutine is ready to receive, and a receive blocks until another goroutine sends. This creates a *rendezvous point*: both goroutines must arrive at the channel operation at the same time for the exchange to happen.

This forced synchronization is powerful because it gives you a guarantee: when a receive completes, you know the sender has executed everything up to and including the send. No race conditions, no data corruption -- just a clean handoff.

## Step 1 -- Create and Use a Channel

Create a channel of type `string`, launch a goroutine that sends a greeting, and receive it in `main`.

```go
package main

import "fmt"

func main() {
    // make(chan T) creates an unbuffered channel of type T
    messages := make(chan string)

    // Launch a goroutine that sends a value.
    // The arrow points INTO the channel: data flows from right to left.
    go func() {
        messages <- "hello from goroutine"
    }()

    // Receive the value (blocks until the goroutine sends).
    // The arrow points OUT of the channel: data flows from channel to variable.
    msg := <-messages
    fmt.Println(msg)
}
```

Key observations:
- `make(chan string)` creates a channel that transports `string` values
- `messages <- value` is a **send** -- the arrow points *into* the channel
- `<-messages` is a **receive** -- the arrow points *out of* the channel
- `main` blocks at `<-messages` until the goroutine sends

### Verification
```bash
go run main.go
# Expected: hello from goroutine
```

## Step 2 -- Observe Send-Blocks-Until-Receive

The rendezvous property means the sender goroutine is suspended until a receiver is ready. This program proves it with timestamps.

```go
package main

import (
    "fmt"
    "time"
)

func main() {
    messages := make(chan string)

    go func() {
        fmt.Println("goroutine: about to send (will block here)")
        // This send blocks because main has not called <-messages yet.
        // The goroutine is suspended here for ~500ms until main receives.
        messages <- "data"
        // This only prints AFTER main's receive unblocks us.
        fmt.Println("goroutine: send completed (unblocked by receiver)")
    }()

    // Simulate main being busy. The goroutine is blocked on send this whole time.
    time.Sleep(500 * time.Millisecond)
    fmt.Println("main: about to receive (after 500ms delay)")

    val := <-messages
    fmt.Printf("main: received %q\n", val)

    // Small sleep to let the goroutine's final print execute.
    time.Sleep(50 * time.Millisecond)
}
```

You will see that `"goroutine: about to send"` prints first, then after the 500ms sleep, `"main: about to receive"` prints, and only then does the goroutine's `"goroutine: send completed"` appear. The goroutine was blocked on the send the entire time.

### Verification
```bash
go run main.go
# Expected output order:
#   goroutine: about to send (will block here)
#   main: about to receive (after 500ms delay)
#   main: received "data"
#   goroutine: send completed (unblocked by receiver)
```

If you changed the sleep to 0, both prints would happen nearly simultaneously. If you increased it to 5 seconds, the goroutine would be suspended for 5 seconds. The channel adapts to the actual timing -- no guessing.

## Step 3 -- Multiple Values Through One Channel

Each send/receive pair is a separate synchronization point. Three values flow through the same channel sequentially.

```go
package main

import "fmt"

func main() {
    ch := make(chan int)

    // The sender goroutine sends three values sequentially.
    // Each send blocks until the corresponding receive happens in main.
    go func() {
        ch <- 10
        ch <- 20
        ch <- 30
    }()

    // Each receive unblocks the sender, allowing it to proceed to the next send.
    // Values arrive in FIFO order: 10, then 20, then 30.
    for i := 0; i < 3; i++ {
        val := <-ch
        fmt.Println("Received:", val)
    }
}
```

### Verification
```bash
go run main.go
# Expected:
#   Received: 10
#   Received: 20
#   Received: 30
```

## Step 4 -- Deadlock Detection

If you receive from a channel with no sender (or vice versa), all goroutines are stuck. Go's runtime detects this and panics with a deadlock error.

```go
package main

func main() {
    ch := make(chan int)
    <-ch // no goroutine will ever send -- deadlock!
}
```

### Verification
```bash
go run main.go
# Expected: fatal error: all goroutines are asleep - deadlock!
```

This deadlock detection is a safety net during development. If you see this error, it means you have a mismatch between senders and receivers. Common causes:
- Receiving without a corresponding send
- Sending without a corresponding receive
- Sending and receiving in the same goroutine on an unbuffered channel

## Step 5 -- Channels Are Strongly Typed

You can create channels for any Go type: structs, errors, slices, even other channels.

```go
package main

import "fmt"

type Point struct{ X, Y int }

func main() {
    // A channel of Point values.
    pointCh := make(chan Point)
    go func() {
        pointCh <- Point{3, 4}
    }()
    p := <-pointCh
    fmt.Println("Point received:", p)

    // A channel of error values.
    errCh := make(chan error)
    go func() {
        errCh <- fmt.Errorf("something went wrong")
    }()
    err := <-errCh
    fmt.Println("Error received:", err)
}
```

### Verification
```bash
go run main.go
# Expected:
#   Point received: {3 4}
#   Error received: something went wrong
```

## Common Mistakes

### Sending and Receiving in the Same Goroutine

**Wrong:**
```go
package main

func main() {
    ch := make(chan int)
    ch <- 42       // blocks forever -- no other goroutine to receive
    val := <-ch
    _ = val
}
```

**What happens:** The send blocks because no one is receiving, but the receive can never execute because the send hasn't completed. Deadlock.

**Correct:**
```go
package main

import "fmt"

func main() {
    ch := make(chan int)
    go func() {
        ch <- 42 // send from a separate goroutine
    }()
    val := <-ch
    fmt.Println(val) // 42
}
```

### Forgetting That var Declares a Nil Channel

**Wrong:**
```go
package main

func main() {
    var ch chan int // zero value is nil
    ch <- 42       // blocks forever on nil channel!
}
```

**What happens:** A nil channel blocks on both send and receive, forever. The runtime does detect the deadlock in this case, but in more complex programs it might not.

**Correct:**
```go
package main

import "fmt"

func main() {
    ch := make(chan int) // always initialize with make
    go func() { ch <- 42 }()
    fmt.Println(<-ch) // 42
}
```

## What's Next
Continue to [02-channel-as-synchronization](../02-channel-as-synchronization/02-channel-as-synchronization.md) to learn how channels replace fragile `time.Sleep` calls.

## Summary
- `make(chan T)` creates an unbuffered channel that transports values of type `T`
- `ch <- val` sends (blocks until a receiver is ready)
- `val := <-ch` receives (blocks until a sender is ready)
- Unbuffered channels force a rendezvous -- both sides must be ready
- Deadlocks from mismatched sends/receives are caught by Go's runtime
- Always initialize channels with `make` -- nil channels block forever
- Channels are strongly typed -- `chan int` only carries `int` values

## Reference
- [A Tour of Go: Channels](https://go.dev/tour/concurrency/2)
- [Effective Go: Channels](https://go.dev/doc/effective_go#channels)
- [Go Spec: Channel types](https://go.dev/ref/spec#Channel_types)
