---
difficulty: intermediate
concepts: [directional-channels, send-only, receive-only, type-safety, function-signatures]
tools: [go]
estimated_time: 25m
bloom_level: apply
---

# 4. Channel Direction

## Learning Objectives
After completing this exercise, you will be able to:
- **Declare** send-only (`chan<- T`) and receive-only (`<-chan T`) channel parameters
- **Write** producer and consumer functions with directional channel constraints
- **Build** a multi-stage data processing pipeline with enforced data flow
- **Explain** how directional channels prevent bugs at compile time

## Why Channel Direction

In a data processing pipeline, data flows in one direction: source produces records, a filter removes invalid ones, a transformer reshapes the data, and an output stage writes the results. If a filter stage accidentally *sends* data back into its input channel instead of reading from it, you have a subtle bug that is hard to find at runtime.

Go's type system lets you restrict channels to send-only (`chan<- T`) or receive-only (`<-chan T`). The compiler enforces these restrictions, turning potential runtime bugs into compile errors. A producer function that accepts `chan<- Record` cannot accidentally receive from that channel. A consumer that accepts `<-chan Record` cannot accidentally send.

This is how you make data flow explicit in function signatures. When you read `func filter(in <-chan Record, out chan<- Record)`, you instantly know: data flows from `in` to `out`. The compiler guarantees it.

## Step 1 -- Understand the Syntax

Read the arrow's direction relative to `chan`:

```
chan T      // bidirectional: can send and receive
chan<- T    // send-only: can only send (arrow points INTO chan)
<-chan T    // receive-only: can only receive (arrow points OUT of chan)
```

Mnemonic: the arrow `<-` always represents data flow. `chan<- T` means data flows into the channel (send). `<-chan T` means data flows out of the channel (receive).

## Step 2 -- CSV Record Producer

Build a producer that reads CSV records and sends them through a channel. The function returns a `<-chan` (receive-only for the caller). Only the internal goroutine can send.

```go
package main

import (
    "fmt"
    "strings"
)

type Record struct {
    Name   string
    Email  string
    Amount float64
}

// readCSV returns a receive-only channel. The caller can only consume records.
// The goroutine inside owns the send side -- no one else can write to it.
func readCSV(csvData string) <-chan Record {
    ch := make(chan Record) // bidirectional inside the function
    go func() {
        for _, line := range strings.Split(csvData, "\n") {
            fields := strings.Split(line, ",")
            if len(fields) != 3 {
                continue
            }
            var amount float64
            fmt.Sscanf(fields[2], "%f", &amount)
            ch <- Record{
                Name:   strings.TrimSpace(fields[0]),
                Email:  strings.TrimSpace(fields[1]),
                Amount: amount,
            }
        }
        close(ch)
    }()
    return ch // auto-narrows from chan Record to <-chan Record
}

func main() {
    csv := `Alice,alice@corp.com,150.00
Bob,bob@corp.com,75.50
Carol,carol@corp.com,200.00`

    for rec := range readCSV(csv) {
        fmt.Printf("Record: %s <%s> $%.2f\n", rec.Name, rec.Email, rec.Amount)
    }
}
```

### Verification
```bash
go run main.go
# Expected:
#   Record: Alice <alice@corp.com> $150.00
#   Record: Bob <bob@corp.com> $75.50
#   Record: Carol <carol@corp.com> $200.00
```

## Step 3 -- Filter Stage with Directional Channels

A filter reads records from `<-chan` (receive-only) and writes passing records to `chan<-` (send-only). The signature enforces the data flow direction: data can only move from `in` to `out`.

```go
package main

import (
    "fmt"
    "strings"
)

type Record struct {
    Name   string
    Email  string
    Amount float64
}

func readCSV(csvData string) <-chan Record {
    ch := make(chan Record)
    go func() {
        for _, line := range strings.Split(csvData, "\n") {
            fields := strings.Split(line, ",")
            if len(fields) != 3 {
                continue
            }
            var amount float64
            fmt.Sscanf(fields[2], "%f", &amount)
            ch <- Record{
                Name:   strings.TrimSpace(fields[0]),
                Email:  strings.TrimSpace(fields[1]),
                Amount: amount,
            }
        }
        close(ch)
    }()
    return ch
}

// filterHighValue reads from in (receive-only) and writes to out (send-only).
// Only records with Amount >= minAmount pass through.
// Try adding `val := <-out` inside this function -- the compiler rejects it.
func filterHighValue(in <-chan Record, out chan<- Record, minAmount float64) {
    for rec := range in {
        if rec.Amount >= minAmount {
            out <- rec
        }
    }
    close(out)
}

func main() {
    csv := `Alice,alice@corp.com,150.00
Bob,bob@corp.com,75.50
Carol,carol@corp.com,200.00
Dave,dave@corp.com,50.00
Eve,eve@corp.com,300.00`

    raw := readCSV(csv)
    filtered := make(chan Record)
    go filterHighValue(raw, filtered, 100.00)

    for rec := range filtered {
        fmt.Printf("High-value: %s $%.2f\n", rec.Name, rec.Amount)
    }
}
```

### Verification
```bash
go run main.go
# Expected:
#   High-value: Alice $150.00
#   High-value: Carol $200.00
#   High-value: Eve $300.00
```

## Step 4 -- Full Pipeline: Filter, Transform, Output

Connect three stages into a complete data processing pipeline. Each stage uses directional channels to enforce data flow. The compiler guarantees no stage can accidentally read from its output or write to its input.

```go
package main

import (
    "fmt"
    "strings"
)

type Record struct {
    Name   string
    Email  string
    Amount float64
}

type OutputRecord struct {
    Label string
    Total string
}

func readCSV(csvData string) <-chan Record {
    ch := make(chan Record)
    go func() {
        for _, line := range strings.Split(csvData, "\n") {
            fields := strings.Split(line, ",")
            if len(fields) != 3 {
                continue
            }
            var amount float64
            fmt.Sscanf(fields[2], "%f", &amount)
            ch <- Record{
                Name:   strings.TrimSpace(fields[0]),
                Email:  strings.TrimSpace(fields[1]),
                Amount: amount,
            }
        }
        close(ch)
    }()
    return ch
}

func filterHighValue(in <-chan Record, out chan<- Record, minAmount float64) {
    for rec := range in {
        if rec.Amount >= minAmount {
            out <- rec
        }
    }
    close(out)
}

// transform reads Records and produces OutputRecords.
// in is receive-only, out is send-only -- data flows one direction.
func transform(in <-chan Record, out chan<- OutputRecord) {
    for rec := range in {
        out <- OutputRecord{
            Label: fmt.Sprintf("%s (%s)", rec.Name, rec.Email),
            Total: fmt.Sprintf("$%.2f", rec.Amount*1.1), // apply 10% tax
        }
    }
    close(out)
}

func main() {
    csv := `Alice,alice@corp.com,150.00
Bob,bob@corp.com,75.50
Carol,carol@corp.com,200.00
Dave,dave@corp.com,50.00
Eve,eve@corp.com,300.00`

    // Pipeline: readCSV -> filter (>= $100) -> transform (add tax) -> output
    raw := readCSV(csv)
    filtered := make(chan Record)
    transformed := make(chan OutputRecord)

    go filterHighValue(raw, filtered, 100.00)
    go transform(filtered, transformed)

    fmt.Println("=== Invoice Report (High-Value Clients, +10% Tax) ===")
    for out := range transformed {
        fmt.Printf("  %s => %s\n", out.Label, out.Total)
    }
}
```

### Verification
```bash
go run main.go
# Expected:
#   === Invoice Report (High-Value Clients, +10% Tax) ===
#     Alice (alice@corp.com) => $165.00
#     Carol (carol@corp.com) => $220.00
#     Eve (eve@corp.com) => $330.00
```

## Step 5 -- Compile-Time Protection in Action

This step demonstrates the compile-time errors you get when you violate channel direction. Try each mistake and observe the compiler error.

```go
package main

import "fmt"

// This function can ONLY send.
func producerBroken(out chan<- int) {
    // Uncomment to see compile error:
    // val := <-out  // invalid operation: cannot receive from send-only channel
    out <- 42
}

// This function can ONLY receive.
func consumerBroken(in <-chan int) {
    val := <-in
    fmt.Println(val)
    // Uncomment to see compile error:
    // in <- 99      // invalid operation: cannot send to receive-only channel
    // close(in)     // invalid operation: cannot close receive-only channel
}

func main() {
    ch := make(chan int)
    go producerBroken(ch) // auto-narrows to chan<- int
    consumerBroken(ch)    // auto-narrows to <-chan int

    // You CANNOT widen permissions:
    // var readOnly <-chan int = make(chan int)
    // producerBroken(readOnly) // compile error: cannot use readOnly as chan<- int
}
```

### Verification
```bash
go run main.go
# Expected: 42
# Uncomment the broken lines to see compile errors
```

## Intermediate Verification

Review your pipeline and confirm:
1. Each stage function clearly declares its data flow via `<-chan` and `chan<-`
2. The compiler rejects any attempt to read from an output or write to an input
3. Each stage closes its output channel when its input is exhausted

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

## Verify What You Learned
1. What does `chan<- Record` mean versus `<-chan Record`?
2. Why should the filter stage accept `<-chan` for input and `chan<-` for output instead of using bidirectional channels?
3. Who should close the channel in a pipeline -- the sender or the receiver?

## What's Next
Continue to [05-ranging-over-channels](../05-ranging-over-channels/05-ranging-over-channels.md) to learn the `for range` pattern for consuming all values from a channel.

## Summary
- `chan<- T` is send-only, `<-chan T` is receive-only
- Bidirectional channels implicitly convert to directional when passed to functions
- The reverse is not true -- you cannot widen a directional channel to bidirectional
- Only send-side code can `close()` a channel; receive-only channels prevent closing
- Directional channels make data flow explicit in function signatures
- Use directional types in pipeline stages to catch direction bugs at compile time, not runtime

## Reference
- [A Tour of Go: Channel Directions](https://go.dev/tour/concurrency/4)
- [Go Spec: Channel types](https://go.dev/ref/spec#Channel_types)
- [Go Blog: Pipelines and cancellation](https://go.dev/blog/pipelines)
