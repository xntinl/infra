# 6. Closing Channels

<!--
difficulty: intermediate
concepts: [close, comma-ok, zero-value, broadcast, channel-lifecycle]
tools: [go]
estimated_time: 25m
bloom_level: analyze
prerequisites: [goroutines, unbuffered-channels, buffered-channels, range-channels]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01-05 (channels basics through ranging)
- Understanding of channel send/receive mechanics

## Learning Objectives
After completing this exercise, you will be able to:
- **Use** the comma-ok idiom to detect whether a channel is closed
- **Explain** zero-value behavior when reading from a closed channel
- **Implement** broadcasting via `close()` to signal multiple goroutines
- **Avoid** panics from closing or sending on already-closed channels

## Why Understanding Close Semantics

Closing a channel is more than a cleanup step — it's a communication mechanism. When you close a channel, every receiver gets an immediate signal, even if they weren't waiting yet. This makes `close()` a powerful one-to-many broadcast tool.

But `close()` has sharp edges. Sending on a closed channel panics. Closing an already-closed channel panics. These aren't bugs — they're invariants that protect you from data corruption. Understanding exactly when reads return the zero value, how the comma-ok idiom works, and when to close (and when not to) separates confident Go programmers from confused ones.

The key mental model: closing a channel is a statement that says "no more values will ever be sent on this channel." It's a permanent, irreversible action. Once you internalize this, the panics make sense — sending after that statement is a contradiction, and closing twice is redundant and dangerous.

## Step 1 -- Zero-Value Reads After Close

When a channel is closed, receives immediately return the zero value of the channel's type, forever.

```go
ch := make(chan int, 3)
ch <- 10
ch <- 20
close(ch)

fmt.Println(<-ch) // 10 (buffered value)
fmt.Println(<-ch) // 20 (buffered value)
fmt.Println(<-ch) // 0  (zero value — channel closed and empty)
fmt.Println(<-ch) // 0  (still zero — closed channels never block)
```

After all buffered values are drained, every subsequent read returns `0` (for int), `""` (for string), `nil` (for pointers), etc.

### Intermediate Verification
```bash
go run main.go
# Expected: 10, 20, 0, 0
```

## Step 2 -- The Comma-Ok Idiom

Use the two-value receive to distinguish "real zero" from "channel closed":

```go
ch := make(chan int, 2)
ch <- 0 // an actual zero value
close(ch)

val, ok := <-ch
fmt.Println(val, ok) // 0 true  — real value, channel still has data

val, ok = <-ch
fmt.Println(val, ok) // 0 false — zero value because channel is closed
```

When `ok` is `false`, the channel is closed and drained. When `ok` is `true`, the value is real, even if it happens to be the zero value.

### Intermediate Verification
```bash
go run main.go
# Expected:
# 0 true
# 0 false
```

## Step 3 -- Broadcasting with Close

Closing a channel unblocks ALL receivers simultaneously. This is the simplest way to broadcast a signal to multiple goroutines.

```go
quit := make(chan struct{})

for i := 0; i < 5; i++ {
    go func(id int) {
        <-quit // blocks until quit is closed
        fmt.Printf("Worker %d: received shutdown signal\n", id)
    }(i)
}

time.Sleep(100 * time.Millisecond) // let workers start
fmt.Println("Broadcasting shutdown...")
close(quit) // all 5 workers unblock simultaneously

time.Sleep(100 * time.Millisecond) // let workers print
```

Sending `quit <- struct{}{}` would only wake ONE receiver. Closing wakes ALL of them. This is the standard pattern for graceful shutdown.

### Intermediate Verification
```bash
go run main.go
# Expected: all 5 workers print their shutdown message
```

## Step 4 -- Panic: Send on Closed Channel

Attempting to send on a closed channel causes an unrecoverable panic.

```go
ch := make(chan int)
close(ch)
ch <- 42 // panic: send on closed channel
```

This is a programming error. The close statement declared "no more sends", then you sent. Go treats this as a violation.

### Intermediate Verification
```bash
go run main.go
# Expected: panic: send on closed channel
```

## Step 5 -- Panic: Double Close

Closing an already-closed channel also panics.

```go
ch := make(chan int)
close(ch)
close(ch) // panic: close of closed channel
```

This typically happens when multiple goroutines try to close the same channel without coordination.

### Intermediate Verification
```bash
go run main.go
# Expected: panic: close of closed channel
```

## Common Mistakes

### Using Close as "I'm Done Receiving"
**Wrong:**
```go
// Consumer code:
val := <-ch
close(ch) // "I'm done reading"
```
**What happens:** If the producer sends another value, it panics.
**Fix:** Only the sender closes the channel. The receiver just stops reading. If you need to signal the producer to stop, use a separate "done" or "quit" channel.

### Checking Closure With a Race
**Wrong:**
```go
if isOpen(ch) {
    ch <- value // race: channel might close between check and send
}
```
**What happens:** There's no built-in `isOpen()` function, and even if you check with comma-ok on a receive, the state can change between the check and your next operation.
**Fix:** Structure your code so that ownership is clear. The owner (sender) is the only one who closes. Use `select` for non-blocking operations.

## Verify What You Learned

Build a task dispatcher with graceful shutdown in `main.go`:
1. A dispatcher receives tasks from a `tasks` channel and distributes them to 3 worker goroutines
2. Each worker runs in a loop: process tasks until it receives a shutdown signal via a shared `quit` channel
3. Main sends 10 tasks, then closes the `quit` channel to broadcast shutdown
4. Workers finish their current task, print "Worker N: shutting down", and exit
5. Main waits for all workers to confirm exit via a `done` channel

Use the comma-ok idiom in the workers to detect when the task channel is closed vs when a real task arrives.

## What's Next
Continue to [07-nil-channel-behavior](../07-nil-channel-behavior/07-nil-channel-behavior.md) to learn the surprising behavior of nil channels and how to use them strategically.

## Summary
- Closed channels return zero values on receive, immediately and forever
- `val, ok := <-ch` — when `ok` is `false`, the channel is closed and empty
- `close(ch)` unblocks ALL waiting receivers simultaneously (broadcast)
- Sending on a closed channel panics — only the sender should close
- Closing an already-closed channel panics — coordinate who closes
- Close communicates "no more values" — it's a permanent declaration

## Reference
- [Go Spec: Close](https://go.dev/ref/spec#Close)
- [Go Spec: Receive operator](https://go.dev/ref/spec#Receive_operator)
- [Go FAQ: How do I know if a channel is closed?](https://go.dev/doc/faq#closechan)
