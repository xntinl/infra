---
difficulty: intermediate
concepts: [one-goroutine-per-task, isolation, independence, error handling, channels for results, panic recovery]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [01-launching-goroutines, 02-goroutine-vs-os-thread]
---

# 7. Goroutine Per Request


## Learning Objectives
After completing this exercise, you will be able to:
- **Implement** the one-goroutine-per-request pattern for independent work items
- **Collect** results from multiple goroutines using buffered channels
- **Isolate** failures using `defer/recover` so one goroutine's panic does not crash others
- **Apply** this pattern to simulate real-world concurrent request processing

## Why Goroutine-Per-Request

The goroutine-per-request pattern is one of Go's most common concurrency idioms. Each incoming request, job, or independent task gets its own goroutine. This pattern works because goroutines are cheap enough to create one for every task, and the Go scheduler efficiently multiplexes them onto OS threads.

This approach has three major advantages. First, each task is isolated: a panic in one goroutine does not crash others (provided you recover it). Second, the programming model is straightforward: each goroutine can be written as simple sequential code. Third, it scales naturally: as load increases, more goroutines are created, and the scheduler distributes them across available cores.

In web servers like `net/http`, this pattern is built in -- every incoming HTTP request is handled in its own goroutine. Understanding the pattern helps you apply it to your own use cases: batch processing, fan-out/fan-in, parallel data pipelines, and more. The key discipline is always collecting all results and recovering panics to prevent goroutine leaks and process crashes.

## Step 1 -- Simulating an HTTP Server Request Handler

Each incoming HTTP connection is accepted from a channel (simulating `net.Listener.Accept()`), processed in its own goroutine (parse request, query database, format response), and the response is sent back.

```go
package main

import (
	"fmt"
	"math/rand"
	"time"
)

type Request struct {
	ID     int
	Path   string
	Method string
}

type Response struct {
	RequestID  int
	StatusCode int
	Body       string
	Latency    time.Duration
}

func handleRequest(req Request) Response {
	start := time.Now()

	// Phase 1: Parse request (~1ms)
	time.Sleep(1 * time.Millisecond)

	// Phase 2: Query "database" (variable latency)
	dbLatency := time.Duration(rand.Intn(80)+20) * time.Millisecond
	time.Sleep(dbLatency)

	// Phase 3: Format response (~1ms)
	time.Sleep(1 * time.Millisecond)

	statusCode := 200
	body := fmt.Sprintf(`{"path": %q, "rows": %d}`, req.Path, rand.Intn(100))

	if req.Path == "/admin/danger" {
		statusCode = 500
		body = `{"error": "internal server error"}`
	}

	return Response{
		RequestID:  req.ID,
		StatusCode: statusCode,
		Body:       body,
		Latency:    time.Since(start),
	}
}

func main() {
	// Simulate incoming connections as a channel (like net.Listener.Accept)
	incoming := make(chan Request, 10)
	go func() {
		paths := []string{"/api/users", "/api/orders", "/api/products", "/health",
			"/api/search", "/admin/danger", "/api/metrics", "/api/config"}
		for i, path := range paths {
			incoming <- Request{ID: i + 1, Path: path, Method: "GET"}
		}
		close(incoming)
	}()

	responses := make(chan Response, 10)
	start := time.Now()
	requestCount := 0

	// One goroutine per request (the net/http pattern)
	for req := range incoming {
		requestCount++
		go func(r Request) {
			responses <- handleRequest(r)
		}(req)
	}

	// Collect all responses
	for i := 0; i < requestCount; i++ {
		resp := <-responses
		fmt.Printf("  req %2d  %-20s  %d  %v\n",
			resp.RequestID, resp.Body[:min(30, len(resp.Body))], resp.StatusCode,
			resp.Latency.Round(time.Millisecond))
	}

	wallClock := time.Since(start)
	fmt.Printf("\n  Processed %d requests in %v\n", requestCount, wallClock.Round(time.Millisecond))
	fmt.Printf("  Sequential would have taken: ~%v\n",
		time.Duration(requestCount*50)*time.Millisecond)
}
```

**What's happening here:** Eight simulated HTTP requests arrive via a channel. Each is handled in its own goroutine -- exactly how Go's `net/http` server works. Each handler parses the request, queries a simulated database, and formats a response. The buffered channel holds results without blocking the senders.

**Key insight:** Wall-clock time is approximately equal to the SLOWEST individual request (~100ms), not the SUM of all requests (~400ms). This is the fundamental benefit of the goroutine-per-request model: each connection handler is simple sequential code, but they all run concurrently.

**What would happen with an unbuffered channel?** Goroutines that finish before main reads would block on send. With enough goroutines, this effectively serializes the work because each must wait for main to read before the next can send.

### Intermediate Verification
```bash
go run main.go
```
Expected output (order varies):
```
  req  4  /health               200  23ms
  req  1  /api/users            200  45ms
  req  6  /admin/danger         500  32ms
  req  3  /api/products         200  78ms
  ...

  Processed 8 requests in 82ms
  Sequential would have taken: ~400ms
```

## Step 2 -- Structured Responses with Error Handling

In a real server, you need to collect both successful responses and errors. A timeout on the database, a malformed request, or a missing resource should not prevent other requests from completing.

```go
package main

import (
	"fmt"
	"math/rand"
	"time"
)

type APIResult struct {
	Endpoint string
	Status   int
	Data     string
	Err      error
	Latency  time.Duration
}

func callEndpoint(endpoint string) APIResult {
	start := time.Now()
	latency := time.Duration(rand.Intn(80)+20) * time.Millisecond
	time.Sleep(latency)

	// Simulate different failure modes
	switch {
	case endpoint == "/api/recommendations" && rand.Float32() < 0.6:
		return APIResult{
			Endpoint: endpoint,
			Status:   503,
			Err:      fmt.Errorf("recommendation engine timeout"),
			Latency:  time.Since(start),
		}
	case endpoint == "/api/legacy" && rand.Float32() < 0.4:
		return APIResult{
			Endpoint: endpoint,
			Status:   502,
			Err:      fmt.Errorf("bad gateway: legacy service unreachable"),
			Latency:  time.Since(start),
		}
	default:
		return APIResult{
			Endpoint: endpoint,
			Status:   200,
			Data:     fmt.Sprintf(`{"source": %q, "items": %d}`, endpoint, rand.Intn(50)+1),
			Latency:  time.Since(start),
		}
	}
}

func main() {
	endpoints := []string{
		"/api/user-profile",
		"/api/order-history",
		"/api/recommendations",
		"/api/notifications",
		"/api/legacy",
		"/api/settings",
	}

	results := make(chan APIResult, len(endpoints))

	for _, ep := range endpoints {
		go func(endpoint string) {
			results <- callEndpoint(endpoint)
		}(ep)
	}

	var successes, failures int
	for i := 0; i < len(endpoints); i++ {
		r := <-results
		if r.Err != nil {
			failures++
			fmt.Printf("  FAIL  %-25s %d  error=%v (%v)\n",
				r.Endpoint, r.Status, r.Err, r.Latency.Round(time.Millisecond))
		} else {
			successes++
			fmt.Printf("  OK    %-25s %d  data=%s (%v)\n",
				r.Endpoint, r.Status, r.Data[:min(40, len(r.Data))], r.Latency.Round(time.Millisecond))
		}
	}
	fmt.Printf("\n  Summary: %d succeeded, %d failed out of %d endpoints\n",
		successes, failures, len(endpoints))
}
```

**What's happening here:** The `APIResult` struct carries both success data and error information. Each goroutine calls an endpoint and returns a result regardless of success or failure. Some endpoints randomly fail (simulating real-world flaky services).

**Key insight:** An error in "/api/recommendations" does not prevent "/api/user-profile" from succeeding. Each goroutine is isolated. The error is captured as data, not as a crash. In production, this is how you build resilient API aggregators that return partial results when some upstream services are degraded.

### Intermediate Verification
```bash
go run main.go
```
Expected output (recommendations and legacy may fail):
```
  OK    /api/user-profile         200  data={"source": "/api/user-profile",  (45ms)
  FAIL  /api/recommendations      503  error=recommendation engine timeout (32ms)
  OK    /api/order-history        200  data={"source": "/api/order-history", (67ms)
  OK    /api/notifications        200  data={"source": "/api/notifications", (55ms)
  FAIL  /api/legacy               502  error=bad gateway: legacy service unr (28ms)
  OK    /api/settings             200  data={"source": "/api/settings", "ite (41ms)

  Summary: 4 succeeded, 2 failed out of 6 endpoints
```

## Step 3 -- Panic Isolation: One Bad Handler Cannot Kill the Server

In a real server, a nil pointer dereference or index-out-of-bounds in one request handler must NOT crash the entire process. The `defer/recover` pattern ensures each goroutine handles its own panics.

```go
package main

import (
	"fmt"
	"math/rand"
	"time"
)

type HandlerResult struct {
	RequestID int
	Status    int
	Body      string
	Panicked  bool
}

func main() {
	safeHandler := func(reqID int, results chan<- HandlerResult) {
		defer func() {
			if r := recover(); r != nil {
				results <- HandlerResult{
					RequestID: reqID,
					Status:    500,
					Body:      fmt.Sprintf("recovered from panic: %v", r),
					Panicked:  true,
				}
			}
		}()

		// Request 3 has a bug: write to nil map
		if reqID == 3 {
			var m map[string]string
			m["key"] = "value" // panic: assignment to entry in nil map
		}

		// Request 7 has a different bug: index out of bounds
		if reqID == 7 {
			s := []int{1, 2, 3}
			_ = s[10] // panic: index out of range
		}

		time.Sleep(time.Duration(rand.Intn(50)+10) * time.Millisecond)
		results <- HandlerResult{
			RequestID: reqID,
			Status:    200,
			Body:      fmt.Sprintf("request %d processed successfully", reqID),
		}
	}

	numRequests := 10
	results := make(chan HandlerResult, numRequests)

	for i := 1; i <= numRequests; i++ {
		go safeHandler(i, results)
	}

	var ok, panicked int
	for i := 0; i < numRequests; i++ {
		r := <-results
		status := "  OK "
		if r.Panicked {
			status = "PANIC"
			panicked++
		} else {
			ok++
		}
		fmt.Printf("  [%s] req %2d  %d  %s\n", status, r.RequestID, r.Status, r.Body)
	}

	fmt.Printf("\n  Results: %d OK, %d panicked (recovered), %d total\n", ok, panicked, numRequests)
	fmt.Println("  Server stayed up despite handler bugs. In production, each panic")
	fmt.Println("  would be logged with a stack trace for debugging.")
}
```

**What's happening here:** Ten request handlers run concurrently. Requests 3 and 7 have bugs that cause panics. The `defer/recover` in each goroutine catches the panic and sends a 500 response instead of crashing. All other requests complete normally.

**Key insight:** An UNRECOVERED panic in ANY goroutine crashes the ENTIRE Go process. The `defer/recover` pattern is essential for request handlers. Go's `net/http` server includes this pattern automatically, but when you build your own worker pools or task queues, you MUST add it yourself. Without recovery, one buggy request kills every other in-flight request.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
  [PANIC] req  3  500  recovered from panic: runtime error: ...
  [PANIC] req  7  500  recovered from panic: runtime error: ...
  [  OK ] req  1  200  request 1 processed successfully
  [  OK ] req  2  200  request 2 processed successfully
  [  OK ] req  4  200  request 4 processed successfully
  ...

  Results: 8 OK, 2 panicked (recovered), 10 total
  Server stayed up despite handler bugs. In production, each panic
  would be logged with a stack trace for debugging.
```

## Step 4 -- Full Server Simulation with Metrics

Build a complete HTTP server simulation: accept connections, process each in a goroutine, collect responses, and report server metrics.

```go
package main

import (
	"fmt"
	"math/rand"
	"sort"
	"time"
)

type HTTPRequest struct {
	ID     int
	Method string
	Path   string
}

type HTTPResponse struct {
	RequestID  int
	StatusCode int
	Latency    time.Duration
}

func main() {
	handleHTTP := func(req HTTPRequest) HTTPResponse {
		start := time.Now()

		// Parse request (~1ms)
		time.Sleep(1 * time.Millisecond)

		// Query database (20-100ms depending on endpoint)
		switch req.Path {
		case "/api/search":
			time.Sleep(time.Duration(rand.Intn(60)+40) * time.Millisecond) // slow
		case "/health":
			time.Sleep(2 * time.Millisecond) // fast
		default:
			time.Sleep(time.Duration(rand.Intn(40)+15) * time.Millisecond)
		}

		// Format response (~1ms)
		time.Sleep(1 * time.Millisecond)

		status := 200
		switch {
		case req.ID%11 == 0:
			status = 500
		case req.ID%7 == 0:
			status = 404
		case req.ID%13 == 0:
			status = 429
		}

		return HTTPResponse{
			RequestID:  req.ID,
			StatusCode: status,
			Latency:    time.Since(start),
		}
	}

	paths := []string{"/api/users", "/api/orders", "/api/products", "/health",
		"/api/search", "/api/config", "/api/metrics"}

	numRequests := 20
	responses := make(chan HTTPResponse, numRequests)
	start := time.Now()

	// One goroutine per connection
	for i := 1; i <= numRequests; i++ {
		go func(id int) {
			req := HTTPRequest{
				ID:     id,
				Method: "GET",
				Path:   paths[id%len(paths)],
			}
			responses <- handleHTTP(req)
		}(i)
	}

	// Collect all responses and compute metrics
	statusCounts := map[int]int{}
	var allResponses []HTTPResponse
	var totalLatency time.Duration

	for i := 0; i < numRequests; i++ {
		resp := <-responses
		statusCounts[resp.StatusCode]++
		totalLatency += resp.Latency
		allResponses = append(allResponses, resp)
	}

	wallClock := time.Since(start)

	// Sort by latency for percentile calculation
	sort.Slice(allResponses, func(i, j int) bool {
		return allResponses[i].Latency < allResponses[j].Latency
	})

	p50 := allResponses[len(allResponses)/2].Latency
	p95 := allResponses[int(float64(len(allResponses))*0.95)].Latency
	p99 := allResponses[len(allResponses)-1].Latency

	fmt.Println("=== Server Metrics ===")
	fmt.Printf("  Requests processed:  %d\n", numRequests)
	fmt.Printf("  Wall-clock time:     %v\n", wallClock.Round(time.Millisecond))
	fmt.Printf("  Throughput:          %.0f req/sec\n",
		float64(numRequests)/wallClock.Seconds())
	fmt.Printf("  Concurrency gain:    %.1fx\n",
		float64(totalLatency)/float64(wallClock))
	fmt.Println()
	fmt.Println("  Latency percentiles:")
	fmt.Printf("    p50: %v\n", p50.Round(time.Millisecond))
	fmt.Printf("    p95: %v\n", p95.Round(time.Millisecond))
	fmt.Printf("    p99: %v\n", p99.Round(time.Millisecond))
	fmt.Println()
	fmt.Printf("  Status codes: 200=%d  404=%d  429=%d  500=%d\n",
		statusCounts[200], statusCounts[404], statusCounts[429], statusCounts[500])
}
```

**What's happening here:** 20 HTTP requests are processed concurrently, each in its own goroutine. We compute real server metrics: throughput, latency percentiles (p50/p95/p99), and status code distribution. This is exactly the kind of instrumentation you would add to a production server.

**Key insight:** This is exactly how `net/http` works internally. Each `ListenAndServe` call accepts connections and spawns a goroutine per connection. The server naturally scales because each goroutine handles its request independently. Wall-clock time (~100ms) is a fraction of total work (~1000ms), demonstrating a 10x concurrency gain.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Server Metrics ===
  Requests processed:  20
  Wall-clock time:     95ms
  Throughput:          210 req/sec
  Concurrency gain:    10.5x

  Latency percentiles:
    p50: 35ms
    p95: 85ms
    p99: 98ms

  Status codes: 200=15  404=2  429=1  500=2
```

## Common Mistakes

### Not Buffering the Result Channel

**Wrong -- complete program:**
```go
package main

import (
	"fmt"
	"time"
)

func main() {
	endpoints := []string{"/users", "/orders", "/products", "/reviews", "/config"}
	results := make(chan string) // unbuffered!

	for _, ep := range endpoints {
		go func(e string) {
			time.Sleep(10 * time.Millisecond)
			results <- e // blocks until someone reads
		}(ep)
	}

	for i := 0; i < len(endpoints); i++ {
		fmt.Println(<-results)
	}
}
```

**What happens:** With an unbuffered channel, each goroutine blocks on send until main reads. This effectively serializes the collection, negating the benefit of concurrency.

**Correct -- buffer to expected capacity:**
```go
package main

import (
	"fmt"
	"time"
)

func main() {
	endpoints := []string{"/users", "/orders", "/products", "/reviews", "/config"}
	results := make(chan string, len(endpoints)) // buffered!

	for _, ep := range endpoints {
		go func(e string) {
			time.Sleep(10 * time.Millisecond)
			results <- e // non-blocking: buffer has room
		}(ep)
	}

	for i := 0; i < len(endpoints); i++ {
		fmt.Println(<-results)
	}
}
```

### Forgetting to Collect All Results

**Wrong -- complete program:**
```go
package main

import (
	"fmt"
	"time"
)

func main() {
	results := make(chan string, 10)

	for i := 0; i < 10; i++ {
		go func(id int) {
			time.Sleep(10 * time.Millisecond)
			results <- fmt.Sprintf("response %d", id)
		}(i)
	}

	// Only read 3 responses -- 7 goroutines' results are silently lost
	for i := 0; i < 3; i++ {
		fmt.Println(<-results)
	}
}
```

**What happens:** With a buffered channel, the remaining 7 goroutines complete and their results sit in the buffer until the process exits. With an unbuffered channel, those 7 goroutines would be leaked (blocked on send forever). In a long-running server, this slowly consumes memory.

**Fix:** Always collect exactly as many results as goroutines you launched, or use a `sync.WaitGroup`.

### Not Recovering Panics in Worker Goroutines

**Wrong -- complete program:**
```go
package main

import "fmt"

func handleRequest(id int) {
	if id == 3 {
		panic("nil pointer in request handler")
	}
	fmt.Printf("request %d handled\n", id)
}

func main() {
	for i := 0; i < 5; i++ {
		go handleRequest(i) // if request 3 panics, ENTIRE server crashes
	}
	select {} // block forever (will crash before reaching here)
}
```

**Fix:** Add defer/recover to every worker goroutine:
```go
package main

import "fmt"

func safeHandle(id int) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("request %d panicked: %v\n", id, r)
		}
	}()

	if id == 3 {
		panic("nil pointer in request handler")
	}
	fmt.Printf("request %d handled\n", id)
}

func main() {
	done := make(chan struct{})
	for i := 0; i < 5; i++ {
		go func(id int) {
			safeHandle(id)
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 5; i++ {
		<-done
	}
}
```

## Verify What You Learned

Build an "API gateway" that:
1. Accepts 20 simulated requests from a channel (each with a different endpoint path)
2. Launches one goroutine per request that simulates the full HTTP lifecycle (parse, DB query with random latency, format response)
3. 10% of requests fail randomly with different error codes (400, 500, 503)
4. Each handler uses defer/recover for panic safety
5. Collects all results and prints a server metrics report: throughput, p50/p95/p99 latency, error rate, and status code distribution

**Hint:** Use a `Response` struct with status, latency, and error fields. Buffer the result channel to `len(requests)`.

## What's Next
Continue to [08-million-goroutines](../08-million-goroutines/08-million-goroutines.md) to push goroutines to their scalability limits.

## Summary
- The goroutine-per-request pattern gives each incoming connection its own goroutine
- Use buffered channels (`make(chan T, n)`) to collect results without blocking senders
- Goroutine isolation means a failure (or panic) in one request does not affect others
- Always collect ALL results or use `WaitGroup` to avoid goroutine leaks
- Add `defer/recover` in worker goroutines that might panic -- without it, one bad request kills the server
- Wall-clock time for N concurrent requests approaches the slowest individual request, not the sum
- This pattern is the foundation of Go's HTTP server, gRPC server, and most concurrent applications

## Reference
- [Go Tour: Channels](https://go.dev/tour/concurrency/2)
- [Effective Go: Channels](https://go.dev/doc/effective_go#channels)
- [Go Blog: Go Concurrency Patterns](https://go.dev/blog/pipelines)
- [net/http: Handler goroutine model](https://pkg.go.dev/net/http#Server)
