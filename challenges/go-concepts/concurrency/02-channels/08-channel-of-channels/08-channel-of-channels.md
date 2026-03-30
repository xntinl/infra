---
difficulty: advanced
concepts: [channel-of-channels, request-response, future, service-pattern]
tools: [go]
estimated_time: 35m
bloom_level: create
---

# 8. Channel of Channels

## Learning Objectives
After completing this exercise, you will be able to:
- **Design** request types that carry response channels
- **Implement** the request-response pattern using channels of channels
- **Build** a service goroutine that processes requests sequentially
- **Explain** how this pattern provides safe concurrent access to shared state

## Why Channel of Channels

Most channel examples show one-way data flow: producer sends, consumer receives. But real systems need request-response: a client sends a request and waits for the answer. HTTP servers, database queries, and RPC calls all follow this pattern.

In Go, you model this by embedding a response channel inside the request. The client creates a one-shot response channel, includes it in the request struct, and sends the request on a shared channel. The service goroutine receives the request, processes it, and sends the result back on the embedded response channel.

This is how you build safe concurrent access to shared state without mutexes. The service goroutine is the only one that touches the state. Clients communicate through channels, which are inherently safe. The Go proverb applies perfectly: "Don't communicate by sharing memory; share memory by communicating."

## Step 1 -- The Request-Response Pattern

Define a request struct that carries a response channel -- the "return address." Each client creates its own Reply channel, so responses go to the right caller even with concurrent requests.

```go
package main

import "fmt"

type LookupRequest struct {
    UserID int
    Reply  chan LookupResponse
}

type LookupResponse struct {
    Name  string
    Email string
    Found bool
}

func userService(requests <-chan LookupRequest) {
    // The service owns this data. No mutex needed -- only this goroutine reads/writes it.
    users := map[int]struct{ Name, Email string }{
        1: {"Alice", "alice@corp.com"},
        2: {"Bob", "bob@corp.com"},
        3: {"Carol", "carol@corp.com"},
    }

    for req := range requests {
        if user, ok := users[req.UserID]; ok {
            req.Reply <- LookupResponse{Name: user.Name, Email: user.Email, Found: true}
        } else {
            req.Reply <- LookupResponse{Found: false}
        }
    }
}

func main() {
    requests := make(chan LookupRequest)
    go userService(requests)

    // Client creates a reply channel, sends a request, waits for the response.
    reply := make(chan LookupResponse, 1)

    requests <- LookupRequest{UserID: 2, Reply: reply}
    resp := <-reply
    fmt.Printf("User 2: %s <%s> (found=%v)\n", resp.Name, resp.Email, resp.Found)

    requests <- LookupRequest{UserID: 99, Reply: reply}
    resp = <-reply
    fmt.Printf("User 99: found=%v\n", resp.Found)

    close(requests)
}
```

Because the service is a single goroutine processing requests sequentially, all state access is safe -- no mutexes needed.

### Verification
```bash
go run main.go
# Expected:
#   User 2: Bob <bob@corp.com> (found=true)
#   User 99: found=false
```

## Step 2 -- Concurrent Clients

Multiple goroutines send requests to the same service simultaneously. Each gets its own response because each creates its own reply channel.

```go
package main

import (
    "fmt"
    "sync"
)

type LookupRequest struct {
    UserID int
    Reply  chan string
}

func userService(requests <-chan LookupRequest) {
    users := map[int]string{
        1: "Alice", 2: "Bob", 3: "Carol",
        4: "Dave", 5: "Eve",
    }
    for req := range requests {
        if name, ok := users[req.UserID]; ok {
            req.Reply <- name
        } else {
            req.Reply <- "unknown"
        }
    }
}

func main() {
    requests := make(chan LookupRequest)
    go userService(requests)

    var wg sync.WaitGroup
    for id := 1; id <= 5; id++ {
        wg.Add(1)
        go func(userID int) {
            defer wg.Done()
            // Each goroutine creates its own reply channel.
            // Responses are routed to the correct caller automatically.
            reply := make(chan string, 1)
            requests <- LookupRequest{UserID: userID, Reply: reply}
            name := <-reply
            fmt.Printf("Client looked up user %d: %s\n", userID, name)
        }(id)
    }

    wg.Wait()
    close(requests)
}
```

### Verification
```bash
go run main.go
# Expected (order may vary):
#   Client looked up user 1: Alice
#   Client looked up user 2: Bob
#   Client looked up user 3: Carol
#   Client looked up user 4: Dave
#   Client looked up user 5: Eve
```

## Step 3 -- RPC-Style Broker with Multiple Operations

Build a more realistic request-response broker that supports multiple operations: get, set, and delete. This simulates an in-memory configuration service that multiple microservices query concurrently.

```go
package main

import "fmt"

type ConfigResponse struct {
    Value string
    Found bool
}

type ConfigRequest struct {
    Op    string // "get", "set", "delete"
    Key   string
    Value string // used by "set"
    Reply chan ConfigResponse
}

func configService(requests <-chan ConfigRequest) {
    store := make(map[string]string)
    for req := range requests {
        switch req.Op {
        case "set":
            store[req.Key] = req.Value
            req.Reply <- ConfigResponse{Value: req.Value, Found: true}
        case "get":
            val, ok := store[req.Key]
            req.Reply <- ConfigResponse{Value: val, Found: ok}
        case "delete":
            delete(store, req.Key)
            req.Reply <- ConfigResponse{Found: true}
        }
    }
}

// Helper functions wrap the channel protocol into clean API calls.
func configSet(requests chan<- ConfigRequest, key, value string) {
    reply := make(chan ConfigResponse, 1)
    requests <- ConfigRequest{Op: "set", Key: key, Value: value, Reply: reply}
    <-reply
}

func configGet(requests chan<- ConfigRequest, key string) (string, bool) {
    reply := make(chan ConfigResponse, 1)
    requests <- ConfigRequest{Op: "get", Key: key, Reply: reply}
    resp := <-reply
    return resp.Value, resp.Found
}

func configDelete(requests chan<- ConfigRequest, key string) {
    reply := make(chan ConfigResponse, 1)
    requests <- ConfigRequest{Op: "delete", Key: key, Reply: reply}
    <-reply
}

func main() {
    requests := make(chan ConfigRequest)
    go configService(requests)

    configSet(requests, "db.host", "postgres.prod.internal")
    configSet(requests, "db.port", "5432")
    configSet(requests, "cache.ttl", "300s")

    if val, ok := configGet(requests, "db.host"); ok {
        fmt.Printf("db.host = %s\n", val)
    }
    if val, ok := configGet(requests, "cache.ttl"); ok {
        fmt.Printf("cache.ttl = %s\n", val)
    }

    configDelete(requests, "cache.ttl")
    val, ok := configGet(requests, "cache.ttl")
    fmt.Printf("cache.ttl after delete: %q (found=%v)\n", val, ok)

    close(requests)
}
```

### Verification
```bash
go run main.go
# Expected:
#   db.host = postgres.prod.internal
#   cache.ttl = 300s
#   cache.ttl after delete: "" (found=false)
```

## Step 4 -- Rate Limiter Service

A richer example: a rate limiter that tracks request counts per API key. Multiple API gateway goroutines check limits concurrently. The service goroutine owns the counter state -- no race conditions possible.

```go
package main

import (
    "fmt"
    "sync"
)

type RateLimitResponse struct {
    Allowed   bool
    Remaining int
}

type RateLimitRequest struct {
    APIKey string
    Reply  chan RateLimitResponse
}

func rateLimiterService(requests <-chan RateLimitRequest, maxPerKey int) {
    counts := make(map[string]int)
    for req := range requests {
        current := counts[req.APIKey]
        if current < maxPerKey {
            counts[req.APIKey]++
            req.Reply <- RateLimitResponse{
                Allowed:   true,
                Remaining: maxPerKey - current - 1,
            }
        } else {
            req.Reply <- RateLimitResponse{
                Allowed:   false,
                Remaining: 0,
            }
        }
    }
}

func checkRateLimit(requests chan<- RateLimitRequest, apiKey string) RateLimitResponse {
    reply := make(chan RateLimitResponse, 1)
    requests <- RateLimitRequest{APIKey: apiKey, Reply: reply}
    return <-reply
}

func main() {
    requests := make(chan RateLimitRequest)
    go rateLimiterService(requests, 3) // max 3 requests per key

    // Simulate concurrent API gateway instances checking limits.
    var wg sync.WaitGroup
    apiKeys := []string{"key-alice", "key-alice", "key-alice", "key-alice", "key-bob", "key-bob"}

    for i, key := range apiKeys {
        wg.Add(1)
        go func(reqNum int, apiKey string) {
            defer wg.Done()
            resp := checkRateLimit(requests, apiKey)
            status := "ALLOWED"
            if !resp.Allowed {
                status = "BLOCKED"
            }
            fmt.Printf("Request %d [%s]: %s (remaining: %d)\n",
                reqNum+1, apiKey, status, resp.Remaining)
        }(i, key)
    }

    wg.Wait()
    close(requests)
}
```

### Verification
```bash
go run main.go
# Expected: first 3 requests for key-alice are ALLOWED, 4th is BLOCKED
# Both requests for key-bob are ALLOWED
```

## Step 5 -- Request with Error Handling

In production, service operations can fail. Include an error field in the response to communicate failures back to the caller, simulating a real RPC system.

```go
package main

import (
    "fmt"
    "sync"
)

type AccountResponse struct {
    Balance float64
    Error   string
}

type AccountRequest struct {
    Op     string // "deposit", "withdraw", "balance"
    Amount float64
    Reply  chan AccountResponse
}

func accountService(requests <-chan AccountRequest) {
    var balance float64
    for req := range requests {
        switch req.Op {
        case "deposit":
            if req.Amount <= 0 {
                req.Reply <- AccountResponse{Balance: balance, Error: "deposit amount must be positive"}
                continue
            }
            balance += req.Amount
            req.Reply <- AccountResponse{Balance: balance}
        case "withdraw":
            if req.Amount > balance {
                req.Reply <- AccountResponse{
                    Balance: balance,
                    Error:   fmt.Sprintf("insufficient funds: have $%.2f, want $%.2f", balance, req.Amount),
                }
                continue
            }
            balance -= req.Amount
            req.Reply <- AccountResponse{Balance: balance}
        case "balance":
            req.Reply <- AccountResponse{Balance: balance}
        }
    }
}

func main() {
    requests := make(chan AccountRequest)
    go accountService(requests)

    var wg sync.WaitGroup

    // 5 goroutines deposit $100 each.
    for i := 0; i < 5; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            reply := make(chan AccountResponse, 1)
            requests <- AccountRequest{Op: "deposit", Amount: 100, Reply: reply}
            resp := <-reply
            fmt.Printf("Client %d: deposited $100, balance: $%.2f\n", id, resp.Balance)
        }(i)
    }

    // 5 goroutines try to withdraw $80 each (not all will succeed).
    for i := 5; i < 10; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            reply := make(chan AccountResponse, 1)
            requests <- AccountRequest{Op: "withdraw", Amount: 80, Reply: reply}
            resp := <-reply
            if resp.Error != "" {
                fmt.Printf("Client %d: withdraw FAILED: %s\n", id, resp.Error)
            } else {
                fmt.Printf("Client %d: withdrew $80, balance: $%.2f\n", id, resp.Balance)
            }
        }(i)
    }

    wg.Wait()

    reply := make(chan AccountResponse, 1)
    requests <- AccountRequest{Op: "balance", Reply: reply}
    resp := <-reply
    fmt.Printf("Final balance: $%.2f\n", resp.Balance)

    close(requests)
}
```

### Verification
```bash
go run main.go
# Expected: deposits and withdrawals with correct balance tracking
# Some withdrawals may fail with "insufficient funds"
# Final balance reflects all successful operations
```

## Intermediate Verification

Run the programs and confirm:
1. Each client receives the correct response via its private reply channel
2. The service goroutine processes requests without any mutex or shared state
3. Multiple concurrent clients get correct, non-mixed responses
4. Error cases are communicated through the response struct

## Common Mistakes

### Forgetting to Create the Reply Channel

**Wrong:**
```go
req := LookupRequest{UserID: 42}
requests <- req
// req.Reply is nil -- service blocks forever trying to send to nil channel
```

**What happens:** The service tries to send to a nil channel, which blocks forever. The service goroutine is stuck and cannot process any further requests.

**Fix:** Always initialize the Reply channel: `Reply: make(chan LookupResponse, 1)`.

### Unbuffered Reply Channel Blocking the Service

**Wrong:**
```go
reply := make(chan string) // unbuffered
requests <- LookupRequest{UserID: 1, Reply: reply}
// If the client does not receive promptly, the service blocks
```

**What happens:** The service goroutine is stuck waiting for the client to receive. It cannot process other requests from other clients.

**Fix:** Use `make(chan string, 1)` for the reply channel. The service can send and immediately move to the next request.

## Verify What You Learned
1. Why does each client need its own reply channel?
2. Why is `make(chan T, 1)` preferred over `make(chan T)` for reply channels?
3. How does this pattern eliminate the need for mutexes on shared state?

## What's Next
Continue to [09-buffered-channel-as-semaphore](../09-buffered-channel-as-semaphore/09-buffered-channel-as-semaphore.md) to learn how to use buffered channel capacity to limit concurrency.

## Summary
- Embed a reply channel in request structs for request-response communication
- The service goroutine owns state and processes requests sequentially -- no mutexes
- Each requester creates a private reply channel so responses route correctly
- Use buffered reply channels (`make(chan T, 1)`) so the service does not block on responses
- This pattern implements Go's philosophy: share memory by communicating
- Channel-of-channels enables safe concurrent access patterns (config services, rate limiters, account systems)

## Reference
- [Go Blog: Share Memory By Communicating](https://go.dev/blog/codelab-share)
- [Effective Go: Channels of channels](https://go.dev/doc/effective_go#chan_of_chan)
- [Go Concurrency Patterns (Rob Pike)](https://go.dev/talks/2012/concurrency.slide)
