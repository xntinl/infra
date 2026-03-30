---
difficulty: advanced
concepts: [request-response, reply-channel, channel-based-RPC, goroutine-server-loop, typed-request-response]
tools: [go]
estimated_time: 40m
bloom_level: create
prerequisites: [channels, goroutines, structs]
---

# 23. Channel-Based Request-Response (RPC Pattern)

## Learning Objectives
After completing this exercise, you will be able to:
- **Build** a key-value store service that runs as a single goroutine owning all data
- **Design** typed request and response structs with embedded reply channels for bidirectional communication
- **Implement** multiple concurrent clients that send Get, Set, and Delete operations through a shared request channel
- **Apply** the channel-based RPC pattern as a lock-free alternative to mutex-protected shared state

## Why Channel-Based Request-Response

In many Go services, shared state is protected with a `sync.Mutex`. This works, but as the number of operations grows, you end up with a lock that serializes everything anyway. Worse, mutexes scatter locking concerns across every caller -- any function that touches the state must acquire the lock correctly. One missed lock is a data race. One misplaced defer is a deadlock.

The channel-based RPC pattern inverts this: a single goroutine owns the state and is the only one that ever reads or writes it. All other goroutines communicate with it by sending typed requests on a channel and waiting for responses on a per-request reply channel. The owner goroutine processes requests sequentially, which serializes access naturally -- no mutex needed.

This is the pattern Rob Pike describes as "don't communicate by sharing memory; share memory by communicating." It is used internally by `net/http`'s server, many database connection pools, and service meshes. The tradeoff is explicit: slightly more ceremony in request/response structs, but the data owner is obvious, testable in isolation, and impossible to access incorrectly.

## Step 1 -- Single Operation: Set and Reply

Start with the simplest possible version: one client sends a Set request, the server processes it, and replies on the embedded channel.

```go
package main

import (
	"fmt"
	"time"
)

const serverShutdownGrace = 100 * time.Millisecond

// OpType identifies the kind of operation requested.
type OpType int

const (
	OpSet OpType = iota
	OpGet
	OpDelete
)

// Request represents a typed operation sent to the KV server.
type Request struct {
	Op    OpType
	Key   string
	Value string
	Reply chan Response
}

// Response carries the result of a single KV operation.
type Response struct {
	Value string
	Found bool
	Error string
}

// NewSetRequest creates a Set request with a buffered reply channel.
func NewSetRequest(key, value string) Request {
	return Request{
		Op:    OpSet,
		Key:   key,
		Value: value,
		Reply: make(chan Response, 1),
	}
}

// kvServer runs in its own goroutine, owning all data.
func kvServer(requests <-chan Request) {
	store := make(map[string]string)
	for req := range requests {
		switch req.Op {
		case OpSet:
			store[req.Key] = req.Value
			req.Reply <- Response{Value: req.Value, Found: true}
		}
	}
}

func main() {
	requests := make(chan Request, 10)
	go kvServer(requests)

	// Client sends a Set request and waits for confirmation.
	setReq := NewSetRequest("user:1", "alice")
	requests <- setReq
	resp := <-setReq.Reply
	fmt.Printf("SET user:1 -> %s (ok: %v)\n", resp.Value, resp.Found)

	close(requests)
	time.Sleep(serverShutdownGrace)
}
```

Key observations:
- The `store` map lives entirely inside `kvServer` -- no goroutine can touch it directly
- Each `Request` carries a `Reply` channel buffered to 1, so the server never blocks on sending
- The server's `for range` loop exits cleanly when `requests` is closed

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
SET user:1 -> alice (ok: true)
```

## Step 2 -- Get and Delete with Error Handling

Extend the server to handle all three operations. Get returns the value if found; Delete removes the key and confirms.

```go
package main

import (
	"fmt"
	"time"
)

const serverGrace = 100 * time.Millisecond

type OpType int

const (
	OpSet OpType = iota
	OpGet
	OpDelete
)

type Request struct {
	Op    OpType
	Key   string
	Value string
	Reply chan Response
}

type Response struct {
	Value string
	Found bool
	Error string
}

func newRequest(op OpType, key, value string) Request {
	return Request{
		Op:    op,
		Key:   key,
		Value: value,
		Reply: make(chan Response, 1),
	}
}

func NewSetRequest(key, value string) Request { return newRequest(OpSet, key, value) }
func NewGetRequest(key string) Request        { return newRequest(OpGet, key, "") }
func NewDeleteRequest(key string) Request      { return newRequest(OpDelete, key, "") }

// kvServer processes all operations sequentially -- no mutex needed.
func kvServer(requests <-chan Request) {
	store := make(map[string]string)
	for req := range requests {
		switch req.Op {
		case OpSet:
			store[req.Key] = req.Value
			req.Reply <- Response{Value: req.Value, Found: true}
		case OpGet:
			val, ok := store[req.Key]
			if ok {
				req.Reply <- Response{Value: val, Found: true}
			} else {
				req.Reply <- Response{Found: false, Error: fmt.Sprintf("key %q not found", req.Key)}
			}
		case OpDelete:
			if _, ok := store[req.Key]; ok {
				delete(store, req.Key)
				req.Reply <- Response{Found: true}
			} else {
				req.Reply <- Response{Found: false, Error: fmt.Sprintf("key %q not found", req.Key)}
			}
		}
	}
}

// sendAndPrint sends a request and prints the response.
func sendAndPrint(label string, req Request, requests chan<- Request) {
	requests <- req
	resp := <-req.Reply
	if resp.Error != "" {
		fmt.Printf("%-20s error: %s\n", label, resp.Error)
	} else {
		fmt.Printf("%-20s value: %q  found: %v\n", label, resp.Value, resp.Found)
	}
}

func main() {
	requests := make(chan Request, 10)
	go kvServer(requests)

	sendAndPrint("SET user:1", NewSetRequest("user:1", "alice"), requests)
	sendAndPrint("SET user:2", NewSetRequest("user:2", "bob"), requests)
	sendAndPrint("GET user:1", NewGetRequest("user:1"), requests)
	sendAndPrint("GET user:99", NewGetRequest("user:99"), requests)
	sendAndPrint("DELETE user:2", NewDeleteRequest("user:2"), requests)
	sendAndPrint("GET user:2", NewGetRequest("user:2"), requests)

	close(requests)
	time.Sleep(serverGrace)
}
```

The server handles every operation inside a single `switch`. Because requests arrive sequentially on the channel, the map is always consistent -- a Get after a Set always sees the value.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
SET user:1           value: "alice"  found: true
SET user:2           value: "bob"  found: true
GET user:1           value: "alice"  found: true
GET user:99          error: key "user:99" not found
DELETE user:2        found: true
GET user:2           error: key "user:2" not found
```

## Step 3 -- Multiple Concurrent Clients

Launch 10 client goroutines that perform Set then Get operations concurrently. The server serializes all access naturally.

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

const clientCount = 10

type OpType int

const (
	OpSet OpType = iota
	OpGet
	OpDelete
)

type Request struct {
	Op    OpType
	Key   string
	Value string
	Reply chan Response
}

type Response struct {
	Value string
	Found bool
	Error string
}

func newRequest(op OpType, key, value string) Request {
	return Request{
		Op:    op,
		Key:   key,
		Value: value,
		Reply: make(chan Response, 1),
	}
}

func NewSetRequest(key, value string) Request { return newRequest(OpSet, key, value) }
func NewGetRequest(key string) Request        { return newRequest(OpGet, key, "") }
func NewDeleteRequest(key string) Request      { return newRequest(OpDelete, key, "") }

func kvServer(requests <-chan Request) {
	store := make(map[string]string)
	for req := range requests {
		switch req.Op {
		case OpSet:
			store[req.Key] = req.Value
			req.Reply <- Response{Value: req.Value, Found: true}
		case OpGet:
			val, ok := store[req.Key]
			if ok {
				req.Reply <- Response{Value: val, Found: true}
			} else {
				req.Reply <- Response{Found: false, Error: fmt.Sprintf("key %q not found", req.Key)}
			}
		case OpDelete:
			if _, ok := store[req.Key]; ok {
				delete(store, req.Key)
				req.Reply <- Response{Found: true}
			} else {
				req.Reply <- Response{Found: false, Error: fmt.Sprintf("key %q not found", req.Key)}
			}
		}
	}
}

// clientWorker performs a Set followed by a Get for verification.
func clientWorker(id int, requests chan<- Request, wg *sync.WaitGroup) {
	defer wg.Done()

	key := fmt.Sprintf("session:%d", id)
	value := fmt.Sprintf("data-for-client-%d", id)

	// Set the value.
	setReq := NewSetRequest(key, value)
	requests <- setReq
	setResp := <-setReq.Reply

	// Get it back to verify.
	getReq := NewGetRequest(key)
	requests <- getReq
	getResp := <-getReq.Reply

	if getResp.Found && getResp.Value == value {
		fmt.Printf("client %2d: SET %s -> confirmed (set: %v, get: %q)\n",
			id, key, setResp.Found, getResp.Value)
	} else {
		fmt.Printf("client %2d: MISMATCH expected %q got %q\n",
			id, value, getResp.Value)
	}
}

func main() {
	requests := make(chan Request, 50)
	go kvServer(requests)

	var wg sync.WaitGroup
	epoch := time.Now()

	for i := 1; i <= clientCount; i++ {
		wg.Add(1)
		go clientWorker(i, requests, &wg)
	}

	wg.Wait()
	close(requests)

	elapsed := time.Since(epoch).Round(time.Millisecond)
	fmt.Printf("\n%d clients completed in %v (no mutex, no races)\n", clientCount, elapsed)
}
```

Even though 10 goroutines send requests concurrently, every operation is serialized through the server's channel. The `store` map is never accessed by more than one goroutine.

### Intermediate Verification
```bash
go run -race main.go
```
Expected output (order varies):
```
client  3: SET session:3 -> confirmed (set: true, get: "data-for-client-3")
client  1: SET session:1 -> confirmed (set: true, get: "data-for-client-1")
...
10 clients completed in Xms (no mutex, no races)
```
The `-race` flag confirms no data races.

## Step 4 -- Clean Shutdown with Done Channel

Add a `done` channel so the server reports how many operations it processed before shutting down. Clients perform Set, Get, and Delete operations in sequence.

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

const finalClientCount = 10

type OpType int

const (
	OpSet OpType = iota
	OpGet
	OpDelete
)

type Request struct {
	Op    OpType
	Key   string
	Value string
	Reply chan Response
}

type Response struct {
	Value string
	Found bool
	Error string
}

// ServerStats reports what the server processed.
type ServerStats struct {
	Sets    int
	Gets    int
	Deletes int
}

func newRequest(op OpType, key, value string) Request {
	return Request{
		Op:    op,
		Key:   key,
		Value: value,
		Reply: make(chan Response, 1),
	}
}

func NewSetRequest(key, value string) Request { return newRequest(OpSet, key, value) }
func NewGetRequest(key string) Request        { return newRequest(OpGet, key, "") }
func NewDeleteRequest(key string) Request      { return newRequest(OpDelete, key, "") }

// kvServer runs until requests is closed, then sends stats on done.
func kvServer(requests <-chan Request, done chan<- ServerStats) {
	store := make(map[string]string)
	var stats ServerStats

	for req := range requests {
		switch req.Op {
		case OpSet:
			store[req.Key] = req.Value
			req.Reply <- Response{Value: req.Value, Found: true}
			stats.Sets++
		case OpGet:
			val, ok := store[req.Key]
			if ok {
				req.Reply <- Response{Value: val, Found: true}
			} else {
				req.Reply <- Response{Found: false, Error: fmt.Sprintf("key %q not found", req.Key)}
			}
			stats.Gets++
		case OpDelete:
			if _, ok := store[req.Key]; ok {
				delete(store, req.Key)
				req.Reply <- Response{Found: true}
			} else {
				req.Reply <- Response{Found: false, Error: fmt.Sprintf("key %q not found", req.Key)}
			}
			stats.Deletes++
		}
	}

	done <- stats
}

func clientWorker(id int, requests chan<- Request, wg *sync.WaitGroup) {
	defer wg.Done()

	key := fmt.Sprintf("session:%d", id)
	value := fmt.Sprintf("token-%d", id)

	// Set.
	setReq := NewSetRequest(key, value)
	requests <- setReq
	<-setReq.Reply

	// Get and verify.
	getReq := NewGetRequest(key)
	requests <- getReq
	getResp := <-getReq.Reply

	// Delete.
	delReq := NewDeleteRequest(key)
	requests <- delReq
	<-delReq.Reply

	// Verify deletion.
	getReq2 := NewGetRequest(key)
	requests <- getReq2
	getResp2 := <-getReq2.Reply

	fmt.Printf("client %2d: set=%q -> get=%q -> delete -> exists=%v\n",
		id, value, getResp.Value, getResp2.Found)
}

func main() {
	requests := make(chan Request, 100)
	done := make(chan ServerStats, 1)

	go kvServer(requests, done)

	var wg sync.WaitGroup
	epoch := time.Now()

	for i := 1; i <= finalClientCount; i++ {
		wg.Add(1)
		go clientWorker(i, requests, &wg)
	}

	wg.Wait()
	close(requests)

	stats := <-done
	elapsed := time.Since(epoch).Round(time.Millisecond)

	fmt.Printf("\n=== Server Stats ===\n")
	fmt.Printf("SETs:    %d\n", stats.Sets)
	fmt.Printf("GETs:    %d\n", stats.Gets)
	fmt.Printf("DELETEs: %d\n", stats.Deletes)
	fmt.Printf("Total:   %d ops in %v\n", stats.Sets+stats.Gets+stats.Deletes, elapsed)
}
```

### Intermediate Verification
```bash
go run -race main.go
```
Expected output (order varies):
```
client  2: set="token-2" -> get="token-2" -> delete -> exists=false
client  5: set="token-5" -> get="token-5" -> delete -> exists=false
...

=== Server Stats ===
SETs:    10
GETs:    20
DELETEs: 10
Total:   40 ops in Xms
```
Each client does 4 operations (set, get, delete, get), so 10 clients produce 40 total.

## Common Mistakes

### Unbuffered Reply Channel
**What happens:** The server blocks on `req.Reply <- resp` until the client reads. If the client has not started reading yet (or if multiple requests queue up), the server stalls and stops processing all other requests.
**Fix:** Always buffer the reply channel with capacity 1: `Reply: make(chan Response, 1)`. The server sends and immediately moves to the next request.

### Accessing the Map Outside the Server Goroutine
**What happens:** A developer adds a "quick read" that accesses the store map directly from a client goroutine, bypassing the channel. This creates a data race that `go run -race` detects -- or worse, causes silent corruption.
**Fix:** The map must never be referenced outside `kvServer`. Every access goes through a Request. If you need bulk reads, add a new OpType (e.g., `OpList`) that the server handles internally.

### Forgetting to Close the Request Channel
**What happens:** The server's `for range` loop blocks forever waiting for more requests. The program hangs on shutdown -- the `done` channel never receives stats.
**Fix:** Close the `requests` channel after all clients complete. Use `sync.WaitGroup` to coordinate: `wg.Wait(); close(requests)`.

## Verify What You Learned
Add an `OpList` operation that returns all keys as a comma-separated string. The server iterates the map internally and sends the result through the reply channel. Verify that 5 clients can list keys while other clients are setting and deleting concurrently.

## What's Next
Continue to [24. Channel-Based Retry with Exponential Backoff](../24-channel-retry-backoff/24-channel-retry-backoff.md) to learn how to build retry logic as a composable channel pipeline stage -- separating retry policy from business logic using timer-based delays.

## Summary
- A single goroutine owns all shared state, eliminating the need for mutexes
- Typed Request/Response structs with embedded reply channels provide type-safe RPC over channels
- Each reply channel is buffered to 1 so the server never blocks on responses
- Multiple concurrent clients send requests through a shared channel; the server serializes access naturally
- A done channel provides clean shutdown with server statistics
- The channel-based RPC pattern is the Go-idiomatic alternative to lock-based shared state

## Reference
- [Go Blog: Share Memory By Communicating](https://go.dev/blog/codelab-share)
- [Effective Go: Channels](https://go.dev/doc/effective_go#channels)
- [Go Concurrency Patterns (Rob Pike)](https://go.dev/talks/2012/concurrency.slide)
- [Go Wiki: MutexOrChannel](https://go.dev/wiki/MutexOrChannel)
