---
difficulty: advanced
concepts: [multiplexer, request-routing, reply-channel, channel-of-channels, handler-goroutines]
tools: [go]
estimated_time: 40m
bloom_level: create
---

# 22. Channel Request Multiplexer

## Learning Objectives
After completing this exercise, you will be able to:
- **Design** a request multiplexer that routes incoming requests to type-specific handler goroutines
- **Embed** reply channels in request structs for bidirectional communication
- **Build** a router goroutine that dispatches work based on request type
- **Handle** unknown request types gracefully without blocking or panicking

## Why Channel Request Multiplexers

An API gateway receives all kinds of requests on a single endpoint: user lookups, order queries, inventory checks. Each request type requires different logic, different data sources, different latency characteristics. Processing everything in a single goroutine creates a bottleneck. Processing everything in unbounded goroutines gives no control over concurrency per handler type.

A channel multiplexer solves this: all requests arrive on a single intake channel. A router goroutine reads each request, inspects its type, and forwards it to the appropriate handler channel. Each handler type runs as its own goroutine (or pool of goroutines), processing requests from its dedicated channel. Responses flow back through a reply channel embedded in each request.

This pattern gives you typed routing, per-handler concurrency control, clean separation of concerns, and a single point of observation for all traffic. It is the channel-based equivalent of an HTTP router, built entirely from goroutines and channels.

## Step 1 -- Single Handler: All Requests to One Place

Start simple: a single intake channel, one handler goroutine. Every request gets the same handler. The reply channel pattern is the focus here.

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

const handlerDelay = 50 * time.Millisecond

// Response represents the result of processing a request.
type Response struct {
	RequestID int
	Body      string
}

// Request represents an incoming API request with a reply channel.
type Request struct {
	ID      int
	Type    string
	Payload string
	Reply   chan Response
}

// NewRequest creates a request with an initialized reply channel.
func NewRequest(id int, reqType, payload string) Request {
	return Request{
		ID:      id,
		Type:    reqType,
		Payload: payload,
		Reply:   make(chan Response, 1),
	}
}

// handler processes requests from the intake channel.
func handler(name string, intake <-chan Request, wg *sync.WaitGroup) {
	defer wg.Done()
	for req := range intake {
		time.Sleep(handlerDelay)
		req.Reply <- Response{
			RequestID: req.ID,
			Body:      fmt.Sprintf("[%s] processed: %s", name, req.Payload),
		}
	}
}

func main() {
	intake := make(chan Request, 10)
	var wg sync.WaitGroup

	wg.Add(1)
	go handler("generic", intake, &wg)

	requests := []Request{
		NewRequest(1, "user", "lookup alice"),
		NewRequest(2, "order", "status ORD-42"),
		NewRequest(3, "inventory", "check SKU-100"),
	}

	// Send all requests.
	for _, req := range requests {
		intake <- req
	}
	close(intake)

	// Collect responses (each request has its own reply channel).
	for _, req := range requests {
		resp := <-req.Reply
		fmt.Printf("  request %d -> %s\n", resp.RequestID, resp.Body)
	}

	wg.Wait()
}
```

Key observations:
- Each `Request` carries its own `Reply` channel -- the sender knows exactly where to listen
- The reply channel is buffered with capacity 1 so the handler never blocks on reply
- Closing `intake` causes the handler's `range` loop to exit cleanly

### Verification
```bash
go run main.go
# Expected: all 3 requests processed by the generic handler
```

## Step 2 -- Three Handlers with Type-Based Routing

Add a router goroutine that reads from the intake channel and forwards each request to a type-specific handler channel. Three handlers process their respective request types.

```go
package main

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const routerHandlerDelay = 50 * time.Millisecond

type Response struct {
	RequestID int
	Body      string
}

type Request struct {
	ID      int
	Type    string
	Payload string
	Reply   chan Response
}

func NewRequest(id int, reqType, payload string) Request {
	return Request{
		ID:      id,
		Type:    reqType,
		Payload: payload,
		Reply:   make(chan Response, 1),
	}
}

// HandlerFunc processes a single request and returns a response body.
type HandlerFunc func(req Request) string

func userHandler(req Request) string {
	time.Sleep(routerHandlerDelay)
	name := strings.TrimPrefix(req.Payload, "lookup ")
	return fmt.Sprintf("User found: %s (id: U-%d)", name, req.ID*100)
}

func orderHandler(req Request) string {
	time.Sleep(routerHandlerDelay)
	orderID := strings.TrimPrefix(req.Payload, "status ")
	return fmt.Sprintf("Order %s: shipped, ETA 2 days", orderID)
}

func inventoryHandler(req Request) string {
	time.Sleep(routerHandlerDelay)
	sku := strings.TrimPrefix(req.Payload, "check ")
	return fmt.Sprintf("SKU %s: 42 units in stock", sku)
}

// runHandler reads from its channel, processes with the given function,
// and sends responses back through each request's reply channel.
func runHandler(name string, ch <-chan Request, fn HandlerFunc, wg *sync.WaitGroup) {
	defer wg.Done()
	for req := range ch {
		body := fn(req)
		req.Reply <- Response{
			RequestID: req.ID,
			Body:      fmt.Sprintf("[%s] %s", name, body),
		}
	}
}

// router reads from intake and dispatches requests to typed handler channels.
func router(intake <-chan Request, routes map[string]chan Request) {
	for req := range intake {
		if ch, ok := routes[req.Type]; ok {
			ch <- req
		} else {
			req.Reply <- Response{
				RequestID: req.ID,
				Body:      fmt.Sprintf("unknown request type: %q", req.Type),
			}
		}
	}
	for _, ch := range routes {
		close(ch)
	}
}

func main() {
	intake := make(chan Request, 20)

	userCh := make(chan Request, 10)
	orderCh := make(chan Request, 10)
	inventoryCh := make(chan Request, 10)

	routes := map[string]chan Request{
		"user":      userCh,
		"order":     orderCh,
		"inventory": inventoryCh,
	}

	var wg sync.WaitGroup
	wg.Add(3)
	go runHandler("UserHandler", userCh, userHandler, &wg)
	go runHandler("OrderHandler", orderCh, orderHandler, &wg)
	go runHandler("InventoryHandler", inventoryCh, inventoryHandler, &wg)

	go router(intake, routes)

	requests := []Request{
		NewRequest(1, "user", "lookup alice"),
		NewRequest(2, "order", "status ORD-42"),
		NewRequest(3, "inventory", "check SKU-100"),
		NewRequest(4, "user", "lookup bob"),
		NewRequest(5, "order", "status ORD-99"),
		NewRequest(6, "inventory", "check SKU-200"),
	}

	for _, req := range requests {
		intake <- req
	}
	close(intake)

	for _, req := range requests {
		resp := <-req.Reply
		fmt.Printf("  request %d -> %s\n", resp.RequestID, resp.Body)
	}

	wg.Wait()
}
```

The router is a single goroutine that reads from intake and writes to the correct handler channel based on `req.Type`. Each handler goroutine only sees requests it knows how to process. The router closes all handler channels when intake is closed, propagating shutdown.

### Verification
```bash
go run main.go
# Expected: user requests go to UserHandler, orders to OrderHandler, etc.
# Each response includes the handler name
```

## Step 3 -- 15 Concurrent Clients with Mixed Requests

Simulate realistic load: 15 clients send requests concurrently. Each client sends a random request type and waits for its response. The router dispatches in real time.

```go
package main

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	clientCount       = 15
	loadHandlerDelay  = 30 * time.Millisecond
)

type Response struct {
	RequestID int
	Body      string
	Latency   time.Duration
}

type Request struct {
	ID        int
	Type      string
	Payload   string
	Reply     chan Response
	CreatedAt time.Time
}

func NewRequest(id int, reqType, payload string) Request {
	return Request{
		ID:        id,
		Type:      reqType,
		Payload:   payload,
		Reply:     make(chan Response, 1),
		CreatedAt: time.Now(),
	}
}

type HandlerFunc func(req Request) string

func userHandler(req Request) string {
	time.Sleep(loadHandlerDelay)
	name := strings.TrimPrefix(req.Payload, "lookup ")
	return fmt.Sprintf("found user %s", name)
}

func orderHandler(req Request) string {
	time.Sleep(loadHandlerDelay * 2)
	orderID := strings.TrimPrefix(req.Payload, "status ")
	return fmt.Sprintf("order %s: delivered", orderID)
}

func inventoryHandler(req Request) string {
	time.Sleep(loadHandlerDelay)
	sku := strings.TrimPrefix(req.Payload, "check ")
	return fmt.Sprintf("SKU %s: 17 units", sku)
}

func runHandler(name string, ch <-chan Request, fn HandlerFunc, wg *sync.WaitGroup) {
	defer wg.Done()
	for req := range ch {
		body := fn(req)
		req.Reply <- Response{
			RequestID: req.ID,
			Body:      fmt.Sprintf("[%s] %s", name, body),
			Latency:   time.Since(req.CreatedAt).Round(time.Millisecond),
		}
	}
}

func router(intake <-chan Request, routes map[string]chan Request) {
	for req := range intake {
		if ch, ok := routes[req.Type]; ok {
			ch <- req
		} else {
			req.Reply <- Response{
				RequestID: req.ID,
				Body:      fmt.Sprintf("unknown type: %q", req.Type),
				Latency:   time.Since(req.CreatedAt).Round(time.Millisecond),
			}
		}
	}
	for _, ch := range routes {
		close(ch)
	}
}

func main() {
	intake := make(chan Request, 30)

	userCh := make(chan Request, 10)
	orderCh := make(chan Request, 10)
	inventoryCh := make(chan Request, 10)

	routes := map[string]chan Request{
		"user":      userCh,
		"order":     orderCh,
		"inventory": inventoryCh,
	}

	var handlerWG sync.WaitGroup
	handlerWG.Add(3)
	go runHandler("UserHandler", userCh, userHandler, &handlerWG)
	go runHandler("OrderHandler", orderCh, orderHandler, &handlerWG)
	go runHandler("InventoryHandler", inventoryCh, inventoryHandler, &handlerWG)

	go router(intake, routes)

	// Define the request mix for each client.
	requestMix := []struct {
		reqType string
		payload string
	}{
		{"user", "lookup alice"},
		{"order", "status ORD-10"},
		{"inventory", "check SKU-50"},
		{"user", "lookup bob"},
		{"order", "status ORD-20"},
		{"user", "lookup carol"},
		{"inventory", "check SKU-60"},
		{"order", "status ORD-30"},
		{"user", "lookup dave"},
		{"inventory", "check SKU-70"},
		{"user", "lookup eve"},
		{"order", "status ORD-40"},
		{"inventory", "check SKU-80"},
		{"user", "lookup frank"},
		{"order", "status ORD-50"},
	}

	// Launch clients concurrently.
	var clientWG sync.WaitGroup
	responses := make(chan Response, clientCount)

	epoch := time.Now()
	for i := 0; i < clientCount; i++ {
		clientWG.Add(1)
		go func(clientID int) {
			defer clientWG.Done()
			mix := requestMix[clientID%len(requestMix)]
			req := NewRequest(clientID+1, mix.reqType, mix.payload)
			intake <- req
			resp := <-req.Reply
			responses <- resp
		}(i)
	}

	clientWG.Wait()
	close(intake)
	close(responses)

	// Collect and print results.
	userCount, orderCount, invCount := 0, 0, 0
	fmt.Printf("%-5s %-20s %-10s %s\n", "REQ", "HANDLER", "LATENCY", "RESULT")
	fmt.Println("-----------------------------------------------------------")
	for resp := range responses {
		fmt.Printf("%-5d %-20s %-10s %s\n",
			resp.RequestID, extractHandler(resp.Body), resp.Latency, resp.Body)
		switch {
		case strings.HasPrefix(resp.Body, "[User"):
			userCount++
		case strings.HasPrefix(resp.Body, "[Order"):
			orderCount++
		case strings.HasPrefix(resp.Body, "[Inventory"):
			invCount++
		}
	}

	handlerWG.Wait()
	totalTime := time.Since(epoch).Round(time.Millisecond)

	fmt.Printf("\n=== Summary ===\n")
	fmt.Printf("Total clients: %d\n", clientCount)
	fmt.Printf("User requests: %d | Order requests: %d | Inventory requests: %d\n",
		userCount, orderCount, invCount)
	fmt.Printf("Wall time:     %v\n", totalTime)
}

func extractHandler(body string) string {
	if idx := strings.Index(body, "]"); idx > 0 {
		return body[1:idx]
	}
	return "unknown"
}
```

### Verification
```bash
go run -race main.go
# Expected: all 15 requests routed to correct handlers
# Wall time much less than 15 * handler_delay (concurrent processing)
# No race warnings
```

## Step 4 -- Unknown Type Handler

Add graceful handling for unrecognized request types. Instead of silently dropping them, route unknown types to a fallback handler that returns a clear error response.

```go
package main

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	finalHandlerDelay = 30 * time.Millisecond
	finalClientCount  = 18
)

type Response struct {
	RequestID int
	Body      string
	Latency   time.Duration
	Error     bool
}

type Request struct {
	ID        int
	Type      string
	Payload   string
	Reply     chan Response
	CreatedAt time.Time
}

func NewRequest(id int, reqType, payload string) Request {
	return Request{
		ID:        id,
		Type:      reqType,
		Payload:   payload,
		Reply:     make(chan Response, 1),
		CreatedAt: time.Now(),
	}
}

type HandlerFunc func(req Request) string

func userHandler(req Request) string {
	time.Sleep(finalHandlerDelay)
	return fmt.Sprintf("user %s found", strings.TrimPrefix(req.Payload, "lookup "))
}

func orderHandler(req Request) string {
	time.Sleep(finalHandlerDelay)
	return fmt.Sprintf("order %s: shipped", strings.TrimPrefix(req.Payload, "status "))
}

func inventoryHandler(req Request) string {
	time.Sleep(finalHandlerDelay)
	return fmt.Sprintf("SKU %s: in stock", strings.TrimPrefix(req.Payload, "check "))
}

func unknownHandler(req Request) string {
	return fmt.Sprintf("no handler for type %q (payload: %s)", req.Type, req.Payload)
}

func runHandler(name string, ch <-chan Request, fn HandlerFunc, isError bool, wg *sync.WaitGroup) {
	defer wg.Done()
	for req := range ch {
		body := fn(req)
		req.Reply <- Response{
			RequestID: req.ID,
			Body:      fmt.Sprintf("[%s] %s", name, body),
			Latency:   time.Since(req.CreatedAt).Round(time.Millisecond),
			Error:     isError,
		}
	}
}

func router(intake <-chan Request, routes map[string]chan Request, unknown chan Request) {
	for req := range intake {
		if ch, ok := routes[req.Type]; ok {
			ch <- req
		} else {
			unknown <- req
		}
	}
	for _, ch := range routes {
		close(ch)
	}
	close(unknown)
}

func main() {
	intake := make(chan Request, 30)

	userCh := make(chan Request, 10)
	orderCh := make(chan Request, 10)
	inventoryCh := make(chan Request, 10)
	unknownCh := make(chan Request, 10)

	routes := map[string]chan Request{
		"user":      userCh,
		"order":     orderCh,
		"inventory": inventoryCh,
	}

	var handlerWG sync.WaitGroup
	handlerWG.Add(4)
	go runHandler("UserHandler", userCh, userHandler, false, &handlerWG)
	go runHandler("OrderHandler", orderCh, orderHandler, false, &handlerWG)
	go runHandler("InventoryHandler", inventoryCh, inventoryHandler, false, &handlerWG)
	go runHandler("UnknownHandler", unknownCh, unknownHandler, true, &handlerWG)

	go router(intake, routes, unknownCh)

	// Request mix includes unknown types.
	requestDefs := []struct {
		reqType string
		payload string
	}{
		{"user", "lookup alice"},
		{"order", "status ORD-10"},
		{"inventory", "check SKU-50"},
		{"user", "lookup bob"},
		{"payment", "charge $99.99"},     // unknown
		{"order", "status ORD-20"},
		{"analytics", "page-views today"}, // unknown
		{"inventory", "check SKU-60"},
		{"user", "lookup carol"},
		{"shipping", "track PKG-001"},     // unknown
		{"order", "status ORD-30"},
		{"inventory", "check SKU-70"},
		{"user", "lookup dave"},
		{"order", "status ORD-40"},
		{"refund", "process REF-55"},      // unknown
		{"user", "lookup eve"},
		{"inventory", "check SKU-80"},
		{"order", "status ORD-50"},
	}

	var clientWG sync.WaitGroup
	responses := make(chan Response, finalClientCount)

	epoch := time.Now()
	for i := 0; i < finalClientCount; i++ {
		clientWG.Add(1)
		go func(clientID int) {
			defer clientWG.Done()
			def := requestDefs[clientID%len(requestDefs)]
			req := NewRequest(clientID+1, def.reqType, def.payload)
			intake <- req
			resp := <-req.Reply
			responses <- resp
		}(i)
	}

	clientWG.Wait()
	close(intake)
	close(responses)

	successCount, errorCount := 0, 0
	fmt.Printf("%-5s %-7s %-20s %-8s %s\n",
		"REQ", "STATUS", "HANDLER", "LATENCY", "BODY")
	fmt.Println("--------------------------------------------------------------")
	for resp := range responses {
		status := "OK"
		if resp.Error {
			status = "ERR"
			errorCount++
		} else {
			successCount++
		}
		handler := extractHandler(resp.Body)
		fmt.Printf("%-5d %-7s %-20s %-8s %s\n",
			resp.RequestID, status, handler, resp.Latency, resp.Body)
	}

	handlerWG.Wait()
	totalTime := time.Since(epoch).Round(time.Millisecond)

	fmt.Printf("\n=== Summary ===\n")
	fmt.Printf("Total requests:   %d\n", finalClientCount)
	fmt.Printf("Successful:       %d\n", successCount)
	fmt.Printf("Unknown type:     %d\n", errorCount)
	fmt.Printf("Wall time:        %v\n", totalTime)
}

func extractHandler(body string) string {
	if idx := strings.Index(body, "]"); idx > 0 {
		return body[1:idx]
	}
	return "unknown"
}
```

### Verification
```bash
go run -race main.go
# Expected:
# user/order/inventory requests: OK with correct handler
# payment/analytics/shipping/refund: ERR with UnknownHandler
# All requests get a response (no dropped requests)
# No race warnings
```

## Common Mistakes

### Forgetting the Reply Channel Buffer

**Wrong:**
```go
Reply: make(chan Response) // unbuffered
```

**What happens:** If the client has not started receiving yet, the handler blocks on `req.Reply <- resp`. With single-goroutine handlers processing sequentially, this blocks all subsequent requests in that handler's queue.

**Fix:** Buffer the reply channel with capacity 1:
```go
Reply: make(chan Response, 1)
```
The handler sends and moves on. The client reads at its own pace.

### Closing Handler Channels from the Client Side

**Wrong:**
```go
close(userCh) // client code closes the handler channel
```

**What happens:** The router may still be dispatching requests to that channel. Sending to a closed channel panics.

**Fix:** Only the router closes handler channels, after the intake channel is closed and all requests have been dispatched:
```go
func router(intake <-chan Request, routes map[string]chan Request, unknown chan Request) {
    for req := range intake { /* dispatch */ }
    for _, ch := range routes { close(ch) }
    close(unknown)
}
```

### Dropping Requests for Unknown Types

**Wrong:**
```go
if ch, ok := routes[req.Type]; ok {
    ch <- req
}
// else: request silently dropped, client blocks on Reply forever
```

**What happens:** The client goroutine blocks on `<-req.Reply` forever. In a real server, this leaks goroutines.

**Fix:** Always send a response, even for unknown types:
```go
if ch, ok := routes[req.Type]; ok {
    ch <- req
} else {
    unknown <- req // route to fallback handler
}
```

## Verify What You Learned
1. Why is the reply channel embedded in the request struct instead of being a shared response channel?
2. What guarantees that all handler channels are closed after the intake channel closes?
3. How would you add per-handler concurrency limits (e.g., max 5 concurrent order lookups)?

## What's Next
Continue to [23-Channel Request-Response (RPC)](../23-channel-request-response/23-channel-request-response.md) to learn the fundamental channel-based RPC pattern for serializing access to shared state.

## Summary
- A multiplexer routes requests from a single intake channel to type-specific handler goroutines
- Each request carries an embedded `Reply` channel for bidirectional communication
- The router goroutine is the single dispatch point -- it reads intake, inspects type, and forwards
- Handler goroutines process their specific request type and respond via the reply channel
- Unknown request types must always get a response to prevent goroutine leaks
- The reply channel should be buffered (capacity 1) so handlers never block on response delivery
- The router closes all handler channels when intake closes, propagating clean shutdown

## Reference
- [Go Concurrency Patterns (Rob Pike)](https://go.dev/talks/2012/concurrency.slide)
- [Go Blog: Share Memory By Communicating](https://go.dev/blog/codelab-share)
- [Effective Go: Channels](https://go.dev/doc/effective_go#channels)
- [Advanced Go Concurrency Patterns](https://go.dev/talks/2013/advconc.slide)
