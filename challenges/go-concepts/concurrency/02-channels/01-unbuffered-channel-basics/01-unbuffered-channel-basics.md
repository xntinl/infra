# 1. Unbuffered Channel Basics

<!--
difficulty: basic
concepts: [channels, make, send, receive, synchronization, goroutines]
tools: [go]
estimated_time: 15m
bloom_level: understand
prerequisites: [goroutines, go-basics]
-->

## Prerequisites
- Go 1.22+ installed
- Understanding of goroutines (section 01-goroutines)
- Basic Go syntax (functions, types)

## Learning Objectives
After completing this exercise, you will be able to:
- **Create** unbuffered channels using `make(chan T)`
- **Send** and **receive** values through channels using the `<-` operator
- **Explain** why unbuffered channels force synchronization between goroutines

## Why Unbuffered Channels

Channels are Go's primary mechanism for communication between goroutines. While goroutines give you concurrency, channels give you *coordination*. Without channels, goroutines would be isolated workers with no safe way to share results.

An unbuffered channel has zero capacity — it cannot hold any value. This means a send operation blocks until another goroutine is ready to receive, and a receive blocks until another goroutine sends. This creates a *rendezvous point*: both goroutines must arrive at the channel operation at the same time for the exchange to happen.

This forced synchronization is powerful because it gives you a guarantee: when a receive completes, you know the sender has executed everything up to and including the send. No race conditions, no data corruption — just a clean handoff.

## Step 1 -- Create and Use a Channel

Create a channel of type `string`, launch a goroutine that sends a greeting, and receive it in `main`.

```go
func main() {
    // make(chan T) creates an unbuffered channel of type T
    messages := make(chan string)

    // Launch a goroutine that sends a value
    go func() {
        messages <- "hello from goroutine"
    }()

    // Receive the value (blocks until the goroutine sends)
    msg := <-messages
    fmt.Println(msg)
}
```

Key observations:
- `make(chan string)` creates a channel that transports `string` values
- `messages <- value` is a **send** — the arrow points *into* the channel
- `<-messages` is a **receive** — the arrow points *out of* the channel
- `main` blocks at `<-messages` until the goroutine sends

### Intermediate Verification
```bash
go run main.go
# Expected: hello from goroutine
```

## Step 2 -- Observe Send-Blocks-Until-Receive

Add print statements to prove that the send blocks until the receiver is ready.

```go
go func() {
    fmt.Println("goroutine: about to send")
    messages <- "data"
    fmt.Println("goroutine: send completed")
}()

time.Sleep(500 * time.Millisecond)
fmt.Println("main: about to receive")
msg := <-messages
fmt.Println("main: received:", msg)
```

You will see that `"goroutine: about to send"` prints first, then after the 500ms sleep, `"main: about to receive"` prints, and only then does the goroutine's `"goroutine: send completed"` appear. The goroutine was blocked on the send the entire time.

### Intermediate Verification
```bash
go run main.go
# Expected output order:
# goroutine: about to send
# main: about to receive
# goroutine: send completed   (or interleaved with "main: received")
# main: received: data
```

## Step 3 -- Multiple Sends and Receives

Send three values through a channel from a goroutine and receive them one at a time. Each send/receive pair is a separate synchronization point.

Implement the `sendThreeValues` function in `main.go` that sends three integers through the channel. Then receive and print each one in `main`.

### Intermediate Verification
```bash
go run main.go
# Expected:
# Received: 10
# Received: 20
# Received: 30
```

## Step 4 -- Deadlock Detection

Try to receive from a channel with no sender. Go's runtime detects this and panics with a deadlock message.

```go
ch := make(chan int)
val := <-ch // no goroutine will ever send — deadlock!
```

Run this and observe the error. Understanding deadlock messages is essential for debugging channel code.

### Intermediate Verification
```bash
go run main.go
# Expected: fatal error: all goroutines are asleep - Loss deadlock!
```

## Common Mistakes

### Sending and Receiving in the Same Goroutine
**Wrong:**
```go
ch := make(chan int)
ch <- 42       // blocks forever — no other goroutine to receive
val := <-ch
```
**What happens:** The send blocks because no one is receiving, but the receive can never execute because the send hasn't completed. Deadlock.
**Fix:** Always send and receive from different goroutines, or use a buffered channel.

### Forgetting That Channels Are References
**Wrong:**
```go
var ch chan int    // zero value is nil
ch <- 42          // blocks forever on nil channel!
```
**What happens:** A nil channel blocks on both send and receive, forever. No deadlock detection because the runtime considers it a valid (though permanently blocked) operation.
**Fix:** Always initialize with `make(chan int)`.

## Verify What You Learned

Modify `main.go` to complete the final challenge: create two goroutines — one sends even numbers (2, 4, 6) and the other sends odd numbers (1, 3, 5) on separate channels. In `main`, receive all six values and print their sum. The result should be 21.

## What's Next
Continue to [02-channel-as-synchronization](../02-channel-as-synchronization/02-channel-as-synchronization.md) to learn how channels replace fragile `time.Sleep` calls.

## Summary
- `make(chan T)` creates an unbuffered channel that transports values of type `T`
- `ch <- val` sends (blocks until a receiver is ready)
- `val := <-ch` receives (blocks until a sender is ready)
- Unbuffered channels force a rendezvous — both sides must be ready
- Deadlocks from mismatched sends/receives are caught by Go's runtime
- Always initialize channels with `make` — nil channels block forever

## Reference
- [A Tour of Go: Channels](https://go.dev/tour/concurrency/2)
- [Effective Go: Channels](https://go.dev/doc/effective_go#channels)
- [Go Spec: Channel types](https://go.dev/ref/spec#Channel_types)
