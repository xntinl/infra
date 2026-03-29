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

In Go, you model this by sending a request that includes a channel for the response. The requester creates a one-shot response channel, embeds it in the request, and sends the request on a shared channel. The service goroutine receives the request, processes it, and sends the result back on the embedded response channel. The requester blocks on its private response channel until the answer arrives.

This pattern gives you safe concurrent access to shared state without mutexes. The service goroutine is the only one that touches the state. Clients communicate through channels, which are inherently safe. The Go proverb applies perfectly: "Don't communicate by sharing memory; share memory by communicating."

## Step 1 -- Request Type with Response Channel

Define a request struct that carries a response channel.

```go
type MathRequest struct {
    Op    string  // "add", "mul"
    A, B  float64
    Reply chan float64 // caller creates this, service sends result here
}
```

The `Reply` channel is the return address. Each requester creates its own, so responses go to the right place.

### Intermediate Verification
No code to run yet. Understand the structure.

## Step 2 -- Build the Service Goroutine

The service runs in a single goroutine, reading requests from a channel and responding on each request's Reply channel.

```go
func mathService(requests <-chan MathRequest) {
    for req := range requests {
        var result float64
        switch req.Op {
        case "add":
            result = req.A + req.B
        case "mul":
            result = req.A * req.B
        default:
            result = 0
        }
        req.Reply <- result // send response to the caller
    }
}
```

Because the service is a single goroutine processing requests sequentially, all state access is safe — no mutexes needed.

### Intermediate Verification
```bash
go run main.go
# Service starts and waits for requests
```

## Step 3 -- Send Requests and Receive Responses

Create requests with a response channel and send them to the service.

```go
requests := make(chan MathRequest)
go mathService(requests)

// Send a request
reply := make(chan float64)
requests <- MathRequest{Op: "add", A: 3, B: 4, Reply: reply}
result := <-reply
fmt.Println("3 + 4 =", result)

// Send another request
requests <- MathRequest{Op: "mul", A: 5, B: 6, Reply: reply}
result = <-reply
fmt.Println("5 * 6 =", result)
```

Each request creates (or reuses) a reply channel. The service sends the answer back on that specific channel.

### Intermediate Verification
```bash
go run main.go
# Expected:
# 3 + 4 = 7
# 5 * 6 = 30
```

## Step 4 -- Concurrent Requests

Multiple goroutines can send requests to the same service simultaneously. Each gets its own response because each creates its own reply channel.

```go
for i := 0; i < 5; i++ {
    go func(n float64) {
        reply := make(chan float64) // private reply channel
        requests <- MathRequest{Op: "mul", A: n, B: n, Reply: reply}
        result := <-reply
        fmt.Printf("%.0f * %.0f = %.0f\n", n, n, result)
    }(float64(i))
}
```

### Intermediate Verification
```bash
go run main.go
# 5 results, each goroutine gets its own correct answer
```

## Step 5 -- Key-Value Store Service

Build a more realistic example: a key-value store that's safe for concurrent access. The service goroutine owns the map, and clients interact through request channels.

```go
type KVRequest struct {
    Op    string // "get", "set", "delete"
    Key   string
    Value string         // for "set"
    Reply chan KVResponse
}

type KVResponse struct {
    Value string
    Found bool
}
```

The service holds a `map[string]string` internally. Only the service goroutine reads and writes the map, so there are no race conditions.

### Intermediate Verification
```bash
go run main.go
# Set and get operations return correct values
# Concurrent access works safely
```

## Common Mistakes

### Forgetting to Create the Reply Channel
**Wrong:**
```go
req := MathRequest{Op: "add", A: 1, B: 2}
requests <- req
// req.Reply is nil — service panics sending to nil channel
```
**What happens:** The service tries to send to a nil channel, which blocks forever (or causes deadlock).
**Fix:** Always initialize the Reply channel: `Reply: make(chan float64)` or `Reply: make(chan float64, 1)`.

### Unbuffered Reply Channel Blocking the Service
**Wrong:**
```go
reply := make(chan float64) // unbuffered
requests <- MathRequest{..., Reply: reply}
// If the requester doesn't receive promptly, the service blocks
```
**What happens:** The service goroutine is stuck waiting for the requester to receive. It can't process other requests.
**Fix:** Use `make(chan float64, 1)` for the reply channel. The service can send and immediately move to the next request, even if the requester hasn't received yet.

## Verify What You Learned

Build a concurrent bank account service in `main.go`:
1. Define request types for `deposit`, `withdraw`, and `balance` operations
2. The service goroutine holds the balance in a local variable (no shared state)
3. `deposit` adds to balance, `withdraw` subtracts (fail if insufficient), `balance` returns current amount
4. Each response includes the new balance and an error string (empty if success)
5. Launch 10 concurrent goroutines: 5 deposit $100, 5 try to withdraw $80
6. After all operations, query the balance and verify it's correct

The bank service must be safe for concurrent access with no mutexes.

## What's Next
Continue to [09-buffered-channel-as-semaphore](../09-buffered-channel-as-semaphore/09-buffered-channel-as-semaphore.md) to learn how to use buffered channel capacity to limit concurrency.

## Summary
- Embed a reply channel in request structs for request-response communication
- The service goroutine owns state and processes requests sequentially — no mutexes
- Each requester creates a private reply channel so responses route correctly
- Use buffered reply channels (`make(chan T, 1)`) so the service doesn't block on responses
- This pattern implements Go's philosophy: share memory by communicating
- Channel-of-channels enables safe concurrent access patterns for shared state

## Reference
- [Go Blog: Share Memory By Communicating](https://go.dev/blog/codelab-share)
- [Effective Go: Channels of channels](https://go.dev/doc/effective_go#chan_of_chan)
- [Go Concurrency Patterns (Rob Pike)](https://go.dev/talks/2012/concurrency.slide)
