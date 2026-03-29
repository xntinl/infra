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
ch := make(chan int, 3) // buffer can hold 3 ints

ch <- 10  // doesn't block — buffer has room
ch <- 20  // doesn't block
ch <- 30  // doesn't block
// ch <- 40 would block here — buffer is full!

fmt.Println(<-ch) // 10 (FIFO order)
fmt.Println(<-ch) // 20
fmt.Println(<-ch) // 30
```

Key difference from unbuffered: you can send three values *without* any goroutine receiving them. The buffer holds the values until they're consumed.

### Intermediate Verification
```bash
go run main.go
# Expected:
# 10
# 20
# 30
```

## Step 2 -- Observe Blocking When Full

Fill the buffer completely, then try to send one more value. This blocks until someone receives.

```go
ch := make(chan int, 2)
ch <- 1
ch <- 2
fmt.Println("Buffer full, len:", len(ch), "cap:", cap(ch))

go func() {
    time.Sleep(500 * time.Millisecond)
    val := <-ch
    fmt.Println("Received:", val, "— made room in buffer")
}()

fmt.Println("Sending 3rd value (will block until space available)...")
ch <- 3
fmt.Println("3rd value sent!")
```

### Intermediate Verification
```bash
go run main.go
# Expected:
# Buffer full, len: 2 cap: 2
# Sending 3rd value (will block until space available)...
# Received: 1 — made room in buffer
# 3rd value sent!
```

## Step 3 -- Inspect with len() and cap()

`len(ch)` returns the number of values currently in the buffer. `cap(ch)` returns the total capacity. These are useful for diagnostics but should not be used for synchronization (the values can change between checking and acting).

```go
ch := make(chan string, 5)
fmt.Printf("Empty:  len=%d cap=%d\n", len(ch), cap(ch))

ch <- "a"
ch <- "b"
fmt.Printf("After 2: len=%d cap=%d\n", len(ch), cap(ch))

<-ch
fmt.Printf("After recv: len=%d cap=%d\n", len(ch), cap(ch))
```

### Intermediate Verification
```bash
go run main.go
# Expected:
# Empty:  len=0 cap=5
# After 2: len=2 cap=5
# After recv: len=1 cap=5
```

## Step 4 -- Compare Unbuffered vs Buffered

Write a comparison that demonstrates the timing difference between unbuffered and buffered channels when a producer sends 5 values.

With an unbuffered channel, the producer can only send one value at a time and must wait for each receive. With a buffered channel (capacity 5), the producer sends all 5 instantly and the consumer reads them at its own pace.

### Intermediate Verification
```bash
go run main.go
# The unbuffered version takes longer because each send waits for a receive
# The buffered version's sends complete almost instantly
```

## Common Mistakes

### Using Buffer Size as a Synchronization Mechanism
**Wrong:**
```go
ch := make(chan int, 100)
// "I'll just make the buffer big enough"
for i := 0; i < 200; i++ {
    ch <- i // blocks at item 101!
}
```
**What happens:** If you produce more than the buffer holds, you block. A large buffer hides the problem temporarily but doesn't solve it.
**Fix:** Use appropriate buffer sizes based on your throughput needs, not as a replacement for proper synchronization.

### Checking len() Before Sending
**Wrong:**
```go
if len(ch) < cap(ch) {
    ch <- value // RACE: another goroutine might have filled it
}
```
**What happens:** Between checking `len()` and sending, another goroutine might fill the buffer. The send still blocks.
**Fix:** Just send. If you need non-blocking behavior, use `select` with a `default` case (covered in section 03).

## Verify What You Learned

Implement a producer-consumer system in `main.go`: a producer goroutine generates numbers 1 through 10, sending each to a buffered channel of capacity 3. A consumer goroutine receives and prints each number. Use `len()` to print the buffer occupancy after each send and receive. Observe how the buffer fills and drains as the producer and consumer run at different speeds (add small sleeps to make this visible).

## What's Next
Continue to [04-channel-direction](../04-channel-direction/04-channel-direction.md) to learn how directional channel types enforce correct usage at compile time.

## Summary
- `make(chan T, n)` creates a channel with buffer capacity `n`
- Sends block only when the buffer is full; receives block only when empty
- `len(ch)` returns current items in buffer; `cap(ch)` returns total capacity
- Buffered channels decouple producer and consumer timing
- Buffer size is not a synchronization mechanism — choose it based on throughput needs
- Unbuffered = synchronization point; Buffered = async queue

## Reference
- [A Tour of Go: Buffered Channels](https://go.dev/tour/concurrency/3)
- [Effective Go: Channels](https://go.dev/doc/effective_go#channels)
- [Go Spec: Making channels](https://go.dev/ref/spec#Making_slices_maps_and_channels)
