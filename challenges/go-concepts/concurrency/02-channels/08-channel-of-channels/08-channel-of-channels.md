# 8. Channel of Channels

<!--
difficulty: advanced
concepts: [channel-of-channels, request-response, future, service-pattern]
tools: [go]
estimated_time: 35m
bloom_level: create
prerequisites: [goroutines, unbuffered-channels, buffered-channels, channel-direction, close]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01-06 (channels through closing)
- Comfortable with directional channels and struct types

## Learning Objectives
After completing this exercise, you will be able to:
- **Design** request types that carry response channels
- **Implement** the request-response pattern using channels of channels
- **Build** a service goroutine that processes requests sequentially
- **Explain** how this pattern provides safe concurrent access to shared state

## Why Channel of Channels

Most channel examples show one-way data flow: producer sends, consumer receives. But real systems need request-response: "do this computation and give me back the answer." HTTP servers, database queries, and RPC calls all follow this pattern.

In Go, you model this by sending a request that includes a channel for the response. The requester creates a one-shot response channel, embeds it in the request, and sends the request on a shared channel. The service goroutine receives the request, processes it, and sends the result back on the embedded response channel.

This pattern gives you safe concurrent access to shared state without mutexes. The service goroutine is the only one that touches the state. Clients communicate through channels, which are inherently safe. The Go proverb applies perfectly: "Don't communicate by sharing memory; share memory by communicating."

## Step 1 -- Request Type with Response Channel

Define a request struct that carries a response channel -- the "return address."

```go
type MathRequest struct {
    Op    string  // "add", "mul", "sub"
    A, B  float64
    Reply chan float64 // caller creates this, service sends result here
}
```

Each requester creates its own Reply channel, so responses go to the right place even with multiple concurrent callers.

## Step 2 -- Build the Service Goroutine

The service runs in a single goroutine, reading requests from a channel and responding on each request's Reply channel.

```go
package main

import "fmt"

type MathRequest struct {
    Op    string
    A, B  float64
    Reply chan float64
}

func mathService(requests <-chan MathRequest) {
    for req := range requests {
        var result float64
        switch req.Op {
        case "add":
            result = req.A + req.B
        case "mul":
            result = req.A * req.B
        case "sub":
            result = req.A - req.B
        default:
            result = 0
        }
        req.Reply <- result // send response to the caller
    }
}

func main() {
    requests := make(chan MathRequest)
    go mathService(requests)

    reply := make(chan float64, 1)

    requests <- MathRequest{Op: "add", A: 3, B: 4, Reply: reply}
    fmt.Printf("3 + 4 = %.0f\n", <-reply)

    requests <- MathRequest{Op: "mul", A: 5, B: 6, Reply: reply}
    fmt.Printf("5 * 6 = %.0f\n", <-reply)

    close(requests)
}
```

Because the service is a single goroutine processing requests sequentially, all state access is safe -- no mutexes needed.

### Verification
```bash
go run main.go
# Expected:
#   3 + 4 = 7
#   5 * 6 = 30
```

## Step 3 -- Concurrent Requests

Multiple goroutines can send requests to the same service simultaneously. Each gets its own response because each creates its own reply channel.

```go
package main

import (
    "fmt"
    "sync"
)

type MathRequest struct {
    Op    string
    A, B  float64
    Reply chan float64
}

func mathService(requests <-chan MathRequest) {
    for req := range requests {
        var result float64
        switch req.Op {
        case "add":
            result = req.A + req.B
        case "mul":
            result = req.A * req.B
        }
        req.Reply <- result
    }
}

func main() {
    requests := make(chan MathRequest)
    go mathService(requests)

    var wg sync.WaitGroup
    for i := 1; i <= 5; i++ {
        wg.Add(1)
        go func(n float64) {
            defer wg.Done()
            // Private reply channel: only THIS goroutine reads from it.
            reply := make(chan float64, 1)
            requests <- MathRequest{Op: "mul", A: n, B: n, Reply: reply}
            result := <-reply
            fmt.Printf("%.0f * %.0f = %.0f\n", n, n, result)
        }(float64(i))
    }

    wg.Wait()
    close(requests)
}
```

### Verification
```bash
go run main.go
# Expected (order may vary):
#   1 * 1 = 1
#   2 * 2 = 4
#   3 * 3 = 9
#   4 * 4 = 16
#   5 * 5 = 25
```

## Step 4 -- Key-Value Store Service

A more realistic example: a concurrent-safe key-value store. The service goroutine owns the map; clients interact through request channels.

```go
package main

import "fmt"

type KVResponse struct {
    Value string
    Found bool
}

type KVRequest struct {
    Op    string // "get", "set", "delete"
    Key   string
    Value string // for "set"
    Reply chan KVResponse
}

func kvService(requests <-chan KVRequest) {
    store := make(map[string]string)
    for req := range requests {
        switch req.Op {
        case "set":
            store[req.Key] = req.Value
            req.Reply <- KVResponse{Value: req.Value, Found: true}
        case "get":
            val, ok := store[req.Key]
            req.Reply <- KVResponse{Value: val, Found: ok}
        case "delete":
            delete(store, req.Key)
            req.Reply <- KVResponse{Found: true}
        }
    }
}

// Helpers wrap the channel protocol into clean function calls.
func kvSet(requests chan<- KVRequest, key, value string) {
    reply := make(chan KVResponse, 1)
    requests <- KVRequest{Op: "set", Key: key, Value: value, Reply: reply}
    <-reply
}

func kvGet(requests chan<- KVRequest, key string) (string, bool) {
    reply := make(chan KVResponse, 1)
    requests <- KVRequest{Op: "get", Key: key, Reply: reply}
    resp := <-reply
    return resp.Value, resp.Found
}

func main() {
    requests := make(chan KVRequest)
    go kvService(requests)

    kvSet(requests, "language", "Go")
    kvSet(requests, "year", "2009")

    if val, ok := kvGet(requests, "language"); ok {
        fmt.Printf("GET language: %s (found=%v)\n", val, ok)
    }
    if val, ok := kvGet(requests, "year"); ok {
        fmt.Printf("GET year: %s (found=%v)\n", val, ok)
    }
    val, ok := kvGet(requests, "missing")
    fmt.Printf("GET missing: %q (found=%v)\n", val, ok)

    close(requests)
}
```

### Verification
```bash
go run main.go
# Expected:
#   GET language: Go (found=true)
#   GET year: 2009 (found=true)
#   GET missing: "" (found=false)
```

## Step 5 -- Bank Account Service

A richer example with deposits, withdrawals (that can fail), and balance queries.

```go
package main

import (
    "fmt"
    "sync"
)

type BankResponse struct {
    Balance float64
    Error   string
}

type BankRequest struct {
    Op     string // "deposit", "withdraw", "balance"
    Amount float64
    Reply  chan BankResponse
}

func bankService(requests <-chan BankRequest) {
    var balance float64
    for req := range requests {
        switch req.Op {
        case "deposit":
            balance += req.Amount
            req.Reply <- BankResponse{Balance: balance}
        case "withdraw":
            if req.Amount > balance {
                req.Reply <- BankResponse{
                    Balance: balance,
                    Error:   fmt.Sprintf("insufficient funds: have $%.2f, want $%.2f", balance, req.Amount),
                }
            } else {
                balance -= req.Amount
                req.Reply <- BankResponse{Balance: balance}
            }
        case "balance":
            req.Reply <- BankResponse{Balance: balance}
        }
    }
}

func main() {
    requests := make(chan BankRequest)
    go bankService(requests)

    var wg sync.WaitGroup

    // 5 goroutines deposit $100 each.
    for i := 0; i < 5; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            reply := make(chan BankResponse, 1)
            requests <- BankRequest{Op: "deposit", Amount: 100, Reply: reply}
            resp := <-reply
            fmt.Printf("Goroutine %d: deposited $100, balance: $%.2f\n", id, resp.Balance)
        }(i)
    }

    // 5 goroutines try to withdraw $80 each.
    for i := 5; i < 10; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            reply := make(chan BankResponse, 1)
            requests <- BankRequest{Op: "withdraw", Amount: 80, Reply: reply}
            resp := <-reply
            if resp.Error != "" {
                fmt.Printf("Goroutine %d: withdraw failed: %s\n", id, resp.Error)
            } else {
                fmt.Printf("Goroutine %d: withdrew $80, balance: $%.2f\n", id, resp.Balance)
            }
        }(i)
    }

    wg.Wait()

    reply := make(chan BankResponse, 1)
    requests <- BankRequest{Op: "balance", Reply: reply}
    resp := <-reply
    fmt.Printf("Final balance: $%.2f\n", resp.Balance)

    close(requests)
}
```

### Verification
```bash
go run main.go
# Expected: deposits and withdrawals with correct balance tracking
# Final balance depends on ordering (some withdrawals may fail)
```

## Common Mistakes

### Forgetting to Create the Reply Channel

**Wrong:**
```go
req := MathRequest{Op: "add", A: 1, B: 2}
requests <- req
// req.Reply is nil -- service blocks forever trying to send to nil channel
```

**What happens:** The service tries to send to a nil channel, which blocks forever.

**Fix:** Always initialize the Reply channel: `Reply: make(chan float64, 1)`.

### Unbuffered Reply Channel Blocking the Service

**Wrong:**
```go
reply := make(chan float64) // unbuffered
requests <- MathRequest{Op: "add", A: 1, B: 2, Reply: reply}
// If the requester doesn't receive promptly, the service blocks
```

**What happens:** The service goroutine is stuck waiting for the requester to receive. It can't process other requests.

**Fix:** Use `make(chan float64, 1)` for the reply channel. The service can send and immediately move to the next request.

## What's Next
Continue to [09-buffered-channel-as-semaphore](../09-buffered-channel-as-semaphore/09-buffered-channel-as-semaphore.md) to learn how to use buffered channel capacity to limit concurrency.

## Summary
- Embed a reply channel in request structs for request-response communication
- The service goroutine owns state and processes requests sequentially -- no mutexes
- Each requester creates a private reply channel so responses route correctly
- Use buffered reply channels (`make(chan T, 1)`) so the service doesn't block on responses
- This pattern implements Go's philosophy: share memory by communicating
- Channel-of-channels enables safe concurrent access patterns for shared state

## Reference
- [Go Blog: Share Memory By Communicating](https://go.dev/blog/codelab-share)
- [Effective Go: Channels of channels](https://go.dev/doc/effective_go#chan_of_chan)
- [Go Concurrency Patterns (Rob Pike)](https://go.dev/talks/2012/concurrency.slide)
