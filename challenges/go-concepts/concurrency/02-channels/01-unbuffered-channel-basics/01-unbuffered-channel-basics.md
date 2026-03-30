---
difficulty: basic
concepts: [channels, make, send, receive, synchronization, goroutines]
tools: [go]
estimated_time: 15m
bloom_level: understand
---

# 1. Unbuffered Channel Basics

## Learning Objectives
After completing this exercise, you will be able to:
- **Create** unbuffered channels using `make(chan T)`
- **Send** and **receive** values through channels using the `<-` operator
- **Explain** why unbuffered channels force synchronization between goroutines

## Why Unbuffered Channels

Imagine a print queue in an office. When someone sends a document to the printer, they have to wait until the printer picks it up before they can send the next one. There is no tray to hold pending documents -- the handoff is direct, person to printer.

Unbuffered channels work exactly this way. A send blocks until a receiver is ready, and a receive blocks until a sender is ready. This creates a *rendezvous point*: both goroutines must arrive at the channel operation at the same time for the exchange to happen. The sender knows the receiver has the data the moment the send completes. No race conditions, no data corruption -- just a clean, synchronous handoff.

This forced synchronization is the foundation of Go's concurrency model. Without it, goroutines would be isolated workers with no safe way to share results.

## Step 1 -- A Print Queue: Sending Jobs to a Worker

Create an unbuffered channel to model a print queue. The main goroutine sends print jobs, and a worker goroutine processes them one at a time. Because the channel is unbuffered, main cannot send the next job until the worker has received the current one.

```go
package main

import "fmt"

func main() {
    // make(chan T) creates an unbuffered channel of type T.
    // Zero capacity means every send blocks until a receiver is ready.
    printQueue := make(chan string)

    // Launch the print worker. It receives one job at a time.
    go func() {
        job := <-printQueue
        fmt.Println("Printer: processing", job)
    }()

    // Send a print job. This blocks until the worker calls <-printQueue.
    printQueue <- "invoice-2024.pdf"
    fmt.Println("Main: job accepted by printer")
}
```

Key observations:
- `make(chan string)` creates a channel that transports `string` values
- `printQueue <- value` is a **send** -- the arrow points *into* the channel
- `<-printQueue` is a **receive** -- the arrow points *out of* the channel
- `main` blocks at the send until the worker goroutine receives

### Verification
```bash
go run main.go
# Expected:
#   Printer: processing invoice-2024.pdf
#   Main: job accepted by printer
```

## Step 2 -- Observe the Synchronous Handoff

The rendezvous property means the sender is suspended until a receiver is ready. This program proves it with timestamps: the main goroutine delays its receive, and the worker is stuck waiting the entire time.

```go
package main

import (
    "fmt"
    "time"
)

func main() {
    printQueue := make(chan string)

    go func() {
        fmt.Println("Worker: ready to send job to printer (will block here)")
        printQueue <- "quarterly-report.pdf"
        fmt.Println("Worker: job handed off to printer successfully")
    }()

    // Simulate the printer being busy for 500ms.
    // The worker goroutine is blocked on send the entire time.
    time.Sleep(500 * time.Millisecond)
    fmt.Println("Printer: now ready to accept job")

    job := <-printQueue
    fmt.Printf("Printer: received %q\n", job)

    time.Sleep(50 * time.Millisecond)
}
```

You will see that `"Worker: ready to send"` prints immediately, then after the 500ms delay, `"Printer: now ready"` prints, and only then does the worker's `"job handed off"` appear. The worker was blocked on the send the entire time -- no guessing, no polling.

### Verification
```bash
go run main.go
# Expected output order:
#   Worker: ready to send job to printer (will block here)
#   Printer: now ready to accept job
#   Printer: received "quarterly-report.pdf"
#   Worker: job handed off to printer successfully
```

## Step 3 -- Processing Multiple Jobs Sequentially

Each send/receive pair is a separate synchronization point. Three print jobs flow through the same channel, one at a time, in FIFO order. The worker cannot receive job 2 until job 1 has been handed off.

```go
package main

import (
    "fmt"
    "time"
)

func main() {
    printQueue := make(chan string)

    // Worker goroutine: receives and "prints" each document.
    go func() {
        for i := 0; i < 3; i++ {
            job := <-printQueue
            fmt.Printf("Printer: printing %s...\n", job)
            time.Sleep(100 * time.Millisecond)
            fmt.Printf("Printer: finished %s\n", job)
        }
    }()

    // Main sends three jobs. Each send blocks until the worker receives.
    jobs := []string{"invoice.pdf", "contract.pdf", "memo.pdf"}
    for _, job := range jobs {
        printQueue <- job
        fmt.Printf("Main: %s accepted by printer\n", job)
    }

    time.Sleep(150 * time.Millisecond)
}
```

### Verification
```bash
go run main.go
# Expected: jobs are printed sequentially, each send blocks until the worker picks it up
```

## Step 4 -- Deadlock Detection

If you receive from a channel with no sender (or vice versa), all goroutines are stuck. Go's runtime detects this and panics with a deadlock error. This acts as a safety net during development.

```go
package main

func main() {
    printQueue := make(chan string)
    <-printQueue // no goroutine will ever send -- deadlock!
}
```

### Verification
```bash
go run main.go
# Expected: fatal error: all goroutines are asleep - deadlock!
```

Common causes of deadlock:
- Receiving without a corresponding send
- Sending without a corresponding receive
- Sending and receiving in the same goroutine on an unbuffered channel

## Step 5 -- Channels Are Strongly Typed: Real Job Structs

In production, you rarely send raw strings. Channels carry any Go type -- structs, errors, slices, even other channels. Here, a `PrintJob` struct carries both the document name and its priority, giving the worker all the information it needs.

```go
package main

import "fmt"

type PrintJob struct {
    Document string
    Pages    int
    Priority string
}

func main() {
    printQueue := make(chan PrintJob)

    go func() {
        job := <-printQueue
        fmt.Printf("Printer: %s (%d pages, priority: %s)\n",
            job.Document, job.Pages, job.Priority)
    }()

    printQueue <- PrintJob{
        Document: "annual-report.pdf",
        Pages:    42,
        Priority: "high",
    }

    // Error channels are equally common in production code.
    errCh := make(chan error)
    go func() {
        errCh <- fmt.Errorf("printer offline: paper tray empty")
    }()
    err := <-errCh
    fmt.Println("Error received:", err)
}
```

### Verification
```bash
go run main.go
# Expected:
#   Printer: annual-report.pdf (42 pages, priority: high)
#   Error received: printer offline: paper tray empty
```

## Common Mistakes

### Sending and Receiving in the Same Goroutine

**Wrong:**
```go
package main

func main() {
    ch := make(chan string)
    ch <- "job.pdf"    // blocks forever -- no other goroutine to receive
    job := <-ch
    _ = job
}
```

**What happens:** The send blocks because no one is receiving, but the receive can never execute because the send has not completed. Deadlock.

**Correct:**
```go
package main

import "fmt"

func main() {
    ch := make(chan string)
    go func() {
        ch <- "job.pdf"
    }()
    job := <-ch
    fmt.Println("Received:", job)
}
```

### Forgetting That var Declares a Nil Channel

**Wrong:**
```go
package main

func main() {
    var ch chan string // zero value is nil
    ch <- "job.pdf"   // blocks forever on nil channel!
}
```

**What happens:** A nil channel blocks on both send and receive, forever. The runtime detects the deadlock in simple programs, but in larger programs with other goroutines it can go unnoticed.

**Correct:**
```go
package main

import "fmt"

func main() {
    ch := make(chan string) // always initialize with make
    go func() { ch <- "job.pdf" }()
    fmt.Println(<-ch)
}
```

## Verify What You Learned
1. What happens if you send on an unbuffered channel and no goroutine is ready to receive?
2. Can you send an `int` on a `chan string`? Why or why not?
3. Why is the print queue analogy a good fit for unbuffered channels?

## What's Next
Continue to [02-channel-as-synchronization](../02-channel-as-synchronization/02-channel-as-synchronization.md) to learn how channels replace fragile `time.Sleep` calls with deterministic coordination.

## Summary
- `make(chan T)` creates an unbuffered channel that transports values of type `T`
- `ch <- val` sends (blocks until a receiver is ready)
- `val := <-ch` receives (blocks until a sender is ready)
- Unbuffered channels force a rendezvous -- both sides must be ready simultaneously
- Deadlocks from mismatched sends/receives are caught by Go's runtime
- Always initialize channels with `make` -- nil channels block forever
- Channels are strongly typed -- `chan string` only carries `string` values, `chan PrintJob` carries `PrintJob` values

## Reference
- [A Tour of Go: Channels](https://go.dev/tour/concurrency/2)
- [Effective Go: Channels](https://go.dev/doc/effective_go#channels)
- [Go Spec: Channel types](https://go.dev/ref/spec#Channel_types)
