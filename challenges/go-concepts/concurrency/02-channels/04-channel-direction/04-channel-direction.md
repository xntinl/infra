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

This is a core principle of Go's design philosophy: use the type system to make illegal states unrepresentable.

## Step 1 -- Send-Only and Receive-Only Syntax

Understand the syntax by reading the arrow's direction relative to `chan`:

```
chan T      // bidirectional: can send and receive
chan<- T    // send-only: can only send (arrow points INTO chan)
<-chan T    // receive-only: can only receive (arrow points OUT of chan)
```

Mnemonic: the arrow `<-` always represents data flow. `chan<- T` means data flows into the channel (send). `<-chan T` means data flows out of the channel (receive).

## Step 2 -- Write a Producer with Send-Only Channel

A producer function takes a send-only channel and writes values to it.

```go
package main

import "fmt"

// produce can only SEND to out. Attempting to receive would be a compile error.
func produce(out chan<- int, count int) {
    for i := 1; i <= count; i++ {
        out <- i
    }
    close(out) // producers close when done
}

func main() {
    ch := make(chan int)  // bidirectional
    go produce(ch, 5)    // auto-narrows to send-only

    for val := range ch {
        fmt.Println(val)
    }
}
```

Try adding `val := <-out` inside `produce`. The compiler rejects it:
```
invalid operation: cannot receive from send-only channel out
```

### Verification
```bash
go run main.go
# Expected: 1 2 3 4 5 (one per line)
```

## Step 3 -- Write a Consumer with Receive-Only Channel

A consumer takes a receive-only channel and processes incoming values.

```go
package main

import "fmt"

func consume(in <-chan int) {
    for val := range in {
        fmt.Println("Received:", val)
    }
    // close(in)  -- compile error! Can't close a receive-only channel.
    // in <- 99   -- compile error! Can't send to a receive-only channel.
}

func main() {
    ch := make(chan int)
    go func() {
        for _, v := range []int{10, 20, 30} {
            ch <- v
        }
        close(ch)
    }()

    consume(ch) // auto-narrows to receive-only
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

## Step 4 -- Build a Pipeline

Connect producer and consumer through a transformer. Each stage uses directional channels to enforce data flow.

```go
package main

import "fmt"

func produce(out chan<- int, count int) {
    for i := 1; i <= count; i++ {
        out <- i
    }
    close(out)
}

// double reads from in (receive-only) and writes to out (send-only).
// The signature enforces the data flow direction.
func double(in <-chan int, out chan<- int) {
    for val := range in {
        out <- val * 2
    }
    close(out)
}

func main() {
    raw := make(chan int)
    doubled := make(chan int)

    go produce(raw, 5)        // sends 1..5 to raw
    go double(raw, doubled)   // reads raw, writes *2 to doubled

    for val := range doubled {
        fmt.Println("Doubled:", val)
    }
}
```

Each function only has access to the direction it needs. `double` can read from `in` and write to `out`, but not the reverse.

### Verification
```bash
go run main.go
# Expected:
#   Doubled: 2
#   Doubled: 4
#   Doubled: 6
#   Doubled: 8
#   Doubled: 10
```

## Step 5 -- Return Receive-Only Channel from Function

A common pattern is a generator function that creates a channel internally and returns it as receive-only.

```go
package main

import "fmt"

func generateNumbers(n int) <-chan int {
    ch := make(chan int)  // bidirectional inside the function
    go func() {
        for i := 1; i <= n; i++ {
            ch <- i
        }
        close(ch)
    }()
    // Returning chan int as <-chan int is an automatic narrowing conversion.
    // The caller can only receive. The goroutine holds the only send reference.
    return ch
}

func main() {
    nums := generateNumbers(5)
    for val := range nums {
        fmt.Println(val)
    }
}
```

### Verification
```bash
go run main.go
# Expected: 1 2 3 4 5 (one per line)
```

## Step 6 -- Three-Stage Word Pipeline

Wire three string-processing stages together. Each stage only has the direction it needs, enforced by the compiler.

```go
package main

import (
    "fmt"
    "strings"
)

func generateWords(words []string) <-chan string {
    ch := make(chan string)
    go func() {
        for _, w := range words {
            ch <- w
        }
        close(ch)
    }()
    return ch
}

func toUpper(in <-chan string, out chan<- string) {
    for word := range in {
        out <- strings.ToUpper(word)
    }
    close(out)
}

func addPrefix(in <-chan string, out chan<- string) {
    for word := range in {
        out <- "PROCESSED: " + word
    }
    close(out)
}

func main() {
    words := generateWords([]string{"go", "channels", "are", "typed"})
    uppered := make(chan string)
    prefixed := make(chan string)

    go toUpper(words, uppered)
    go addPrefix(uppered, prefixed)

    for result := range prefixed {
        fmt.Println(result)
    }
}
```

### Verification
```bash
go run main.go
# Expected:
#   PROCESSED: GO
#   PROCESSED: CHANNELS
#   PROCESSED: ARE
#   PROCESSED: TYPED
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

### Trying to Widen Permissions

**Wrong:**
```go
func needsBidirectional(ch chan int) { /* ... */ }

var readOnly <-chan int = make(chan int)
needsBidirectional(readOnly) // compile error!
```

**What happens:** Compile error. You cannot widen permissions -- a receive-only channel cannot become bidirectional.

**Fix:** Pass the bidirectional channel, or change the function signature to accept the narrower type.

## What's Next
Continue to [05-ranging-over-channels](../05-ranging-over-channels/05-ranging-over-channels.md) to learn the `for range` pattern for consuming all values from a channel.

## Summary
- `chan<- T` is send-only, `<-chan T` is receive-only
- Bidirectional channels implicitly convert to directional when passed to functions
- The reverse is not true -- you cannot widen a directional channel to bidirectional
- Only send-side code can `close()` a channel; receive-only channels prevent closing
- Directional channels make data flow explicit in function signatures
- Use directional types to catch direction bugs at compile time, not runtime

## Reference
- [A Tour of Go: Channel Directions](https://go.dev/tour/concurrency/4)
- [Go Spec: Channel types](https://go.dev/ref/spec#Channel_types)
- [Go Blog: Pipelines and cancellation](https://go.dev/blog/pipelines)
