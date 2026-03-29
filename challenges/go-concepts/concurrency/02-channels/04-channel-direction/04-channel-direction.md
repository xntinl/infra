# 4. Channel Direction

<!--
difficulty: intermediate
concepts: [directional-channels, send-only, receive-only, type-safety, function-signatures]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [goroutines, unbuffered-channels, buffered-channels]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01-03 (channel basics, synchronization, buffered channels)
- Familiarity with Go function signatures and type system

## Learning Objectives
After completing this exercise, you will be able to:
- **Declare** send-only (`chan<- T`) and receive-only (`<-chan T`) channel parameters
- **Write** producer and consumer functions with directional channel constraints
- **Explain** how directional channels prevent bugs at compile time

## Why Channel Direction

A bidirectional channel (`chan T`) lets any code both send and receive. In a real program, this is too permissive. A producer should only send, and a consumer should only receive. If a producer accidentally receives from its output channel, or a consumer sends into its input, you have a subtle bug that's hard to find at runtime.

Go's type system lets you restrict channels to send-only (`chan<- T`) or receive-only (`<-chan T`). The compiler enforces these restrictions, turning potential runtime bugs into compile errors. You get the restriction for free: a bidirectional channel automatically converts to a directional one when passed to a function that expects it.

This is a core principle of Go's design philosophy: use the type system to make illegal states unrepresentable. When a function's signature says `<-chan int`, you know at a glance that it only reads from the channel. No need to inspect the implementation.

## Step 1 -- Send-Only and Receive-Only Syntax

Understand the syntax by reading the arrow's direction relative to `chan`:

```go
chan T      // bidirectional: can send and receive
chan<- T    // send-only: can only send (arrow points INTO chan)
<-chan T    // receive-only: can only receive (arrow points OUT of chan)
```

Mnemonic: the arrow `<-` always represents data flow. `chan<- T` means data flows into the channel (send). `<-chan T` means data flows out of the channel (receive).

### Intermediate Verification
No code to run yet. Make sure you can read and distinguish the three types.

## Step 2 -- Write a Producer with Send-Only Channel

A producer function takes a send-only channel and writes values to it. It cannot accidentally read from the channel.

```go
func produce(out chan<- int) {
    for i := 1; i <= 5; i++ {
        out <- i
    }
    close(out) // producers close when done
}
```

Try adding `val := <-out` inside the function. The compiler rejects it:
```
invalid operation: cannot receive from send-only channel out
```

### Intermediate Verification
```bash
go run main.go
# Should compile and send values successfully
# If you add a receive from out, it should fail to compile
```

## Step 3 -- Write a Consumer with Receive-Only Channel

A consumer takes a receive-only channel and processes incoming values. It cannot accidentally send or close the channel.

```go
func consume(in <-chan int) {
    for val := range in {
        fmt.Println("Received:", val)
    }
}
```

Try adding `in <- 99` or `close(in)` inside the function. Both are compile errors:
```
invalid operation: cannot send to receive-only channel in
invalid operation: cannot close receive-only channel in
```

### Intermediate Verification
```bash
go run main.go
# Should compile and consume values successfully
```

## Step 4 -- Build a Pipeline

Connect producer and consumer through a transformer. Each stage uses directional channels to enforce data flow.

```go
func double(in <-chan int, out chan<- int) {
    for val := range in {
        out <- val * 2
    }
    close(out)
}
```

Wire it together:
```go
raw := make(chan int)
doubled := make(chan int)

go produce(raw)       // produce -> raw
go double(raw, doubled) // raw -> doubled
consume(doubled)       // doubled -> consumer (runs in main)
```

Each function only has access to the direction it needs. `double` can read from `in` and write to `out`, but not the reverse. The pipeline's data flow is enforced by the compiler.

### Intermediate Verification
```bash
go run main.go
# Expected:
# Received: 2
# Received: 4
# Received: 6
# Received: 8
# Received: 10
```

## Step 5 -- Return Receive-Only Channel from Function

A common pattern is a generator function that creates a channel internally and returns it as receive-only:

```go
func generateNumbers(n int) <-chan int {
    ch := make(chan int)
    go func() {
        for i := 1; i <= n; i++ {
            ch <- i
        }
        close(ch)
    }()
    return ch // bidirectional converts to receive-only automatically
}
```

The caller can only receive from the returned channel. The internal goroutine holds the only send-capable reference.

### Intermediate Verification
```bash
go run main.go
# Iterate over the returned channel and print values
```

## Common Mistakes

### Trying to Close a Receive-Only Channel
**Wrong:**
```go
func consumer(in <-chan int) {
    for val := range in {
        fmt.Println(val)
    }
    close(in) // compile error!
}
```
**What happens:** Compile error. Only the sender should close a channel. The type system enforces this convention.
**Fix:** Remove the close. The producer closes the channel when done.

### Passing Directional Channel Where Bidirectional Is Expected
**Wrong:**
```go
func needsBidirectional(ch chan int) { /* ... */ }

var readOnly <-chan int = make(chan int)
needsBidirectional(readOnly) // compile error!
```
**What happens:** Compile error. You cannot widen permissions — a receive-only channel cannot become bidirectional.
**Fix:** Pass the bidirectional channel, or change the function signature to accept the narrower type.

## Verify What You Learned

Build a three-stage word processing pipeline in `main.go`:
1. `generateWords(words []string) <-chan string` -- sends each word, closes channel
2. `toUpper(in <-chan string, out chan<- string)` -- converts each word to uppercase, closes out
3. `addPrefix(in <-chan string, out chan<- string)` -- prepends "PROCESSED: " to each word, closes out

Wire them together and consume the final output in main. Use the words: "go", "channels", "are", "typed".

Expected output:
```
PROCESSED: GO
PROCESSED: CHANNELS
PROCESSED: ARE
PROCESSED: TYPED
```

## What's Next
Continue to [05-ranging-over-channels](../05-ranging-over-channels/05-ranging-over-channels.md) to learn the `for range` pattern for consuming all values from a channel.

## Summary
- `chan<- T` is send-only, `<-chan T` is receive-only
- Bidirectional channels implicitly convert to directional when passed to functions
- The reverse is not true — you cannot widen a directional channel to bidirectional
- Only send-side code can `close()` a channel; receive-only channels prevent closing
- Directional channels make data flow explicit in function signatures
- Use directional types to catch direction bugs at compile time, not runtime

## Reference
- [A Tour of Go: Channel Directions](https://go.dev/tour/concurrency/4)
- [Go Spec: Channel types](https://go.dev/ref/spec#Channel_types)
- [Go Blog: Pipelines and cancellation](https://go.dev/blog/pipelines)
