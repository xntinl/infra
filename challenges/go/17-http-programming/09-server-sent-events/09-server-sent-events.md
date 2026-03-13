# 9. Server-Sent Events

<!--
difficulty: advanced
concepts: [server-sent-events, sse, event-stream, flusher, real-time, text-event-stream]
tools: [go, curl]
estimated_time: 35m
bloom_level: analyze
prerequisites: [http-server, middleware-chains, goroutines, channels]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of goroutines and channels
- Familiarity with HTTP handlers and middleware

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** an SSE endpoint using `text/event-stream` content type
- **Use** `http.Flusher` to push data to clients in real time
- **Manage** multiple connected clients with a broker pattern
- **Handle** client disconnection using request context

## Why Server-Sent Events

SSE provides a simple, HTTP-based mechanism for servers to push updates to clients. Unlike WebSockets, SSE is unidirectional (server to client), uses regular HTTP, works through proxies, and reconnects automatically. It is ideal for live dashboards, notifications, log tailing, and progress updates. Go's `http.Flusher` interface makes SSE straightforward to implement.

## The Problem

Build an SSE server that broadcasts timestamped events to all connected clients. When a client connects, it receives a stream of events. When it disconnects, it is cleanly removed from the subscriber list.

## Requirements

1. An SSE endpoint that streams events with proper `text/event-stream` headers
2. A broker that manages multiple subscribers
3. Clean disconnection handling using `r.Context().Done()`
4. Named event types and ID fields for client-side filtering

## Step 1 -- Basic SSE Endpoint

```bash
mkdir -p ~/go-exercises/sse
cd ~/go-exercises/sse
go mod init sse
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"net/http"
	"time"
)

func sseHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	id := 0
	for {
		select {
		case <-r.Context().Done():
			fmt.Println("Client disconnected")
			return
		case t := <-ticker.C:
			id++
			fmt.Fprintf(w, "id: %d\n", id)
			fmt.Fprintf(w, "data: Server time is %s\n\n", t.Format(time.RFC3339))
			flusher.Flush()
		}
	}
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /events", sseHandler)

	fmt.Println("SSE server starting on :8080")
	http.ListenAndServe(":8080", mux)
}
```

The `http.Flusher` interface forces buffered data to the client. Each SSE message ends with a double newline. The `id:` field lets clients resume from the last received event.

### Intermediate Verification

```bash
curl -N http://localhost:8080/events
```

Expected: A stream of events every 2 seconds:

```
id: 1
data: Server time is 2026-03-12T10:00:02Z

id: 2
data: Server time is 2026-03-12T10:00:04Z
```

Press Ctrl+C to disconnect. The server should log "Client disconnected".

## Step 2 -- Build a Broker for Multiple Clients

```go
package main

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

type Broker struct {
	mu          sync.RWMutex
	subscribers map[chan string]struct{}
}

func NewBroker() *Broker {
	return &Broker{
		subscribers: make(map[chan string]struct{}),
	}
}

func (b *Broker) Subscribe() chan string {
	ch := make(chan string, 10)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *Broker) Unsubscribe(ch chan string) {
	b.mu.Lock()
	delete(b.subscribers, ch)
	close(ch)
	b.mu.Unlock()
}

func (b *Broker) Publish(msg string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subscribers {
		select {
		case ch <- msg:
		default:
			// skip slow subscribers
		}
	}
}

func (b *Broker) Count() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}
```

### Intermediate Verification

```bash
go build ./...
```

Expected: Clean compilation.

## Step 3 -- Wire the Broker to SSE and a Publisher

```go
var broker = NewBroker()

func sseHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := broker.Subscribe()
	defer broker.Unsubscribe(ch)

	fmt.Printf("Client connected (total: %d)\n", broker.Count())

	for {
		select {
		case <-r.Context().Done():
			fmt.Printf("Client disconnected (total: %d)\n", broker.Count()-1)
			return
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}

func publishHandler(w http.ResponseWriter, r *http.Request) {
	msg := r.URL.Query().Get("msg")
	if msg == "" {
		http.Error(w, "msg query param required", http.StatusBadRequest)
		return
	}
	broker.Publish(msg)
	fmt.Fprintf(w, "Published to %d subscribers\n", broker.Count())
}

func main() {
	// Background tick publisher
	go func() {
		for t := range time.Tick(5 * time.Second) {
			broker.Publish(fmt.Sprintf("heartbeat at %s", t.Format(time.RFC3339)))
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /events", sseHandler)
	mux.HandleFunc("POST /publish", publishHandler)

	fmt.Println("SSE broker server on :8080")
	http.ListenAndServe(":8080", mux)
}
```

### Intermediate Verification

Open two terminals as subscribers:

```bash
curl -N http://localhost:8080/events
```

In a third terminal, publish:

```bash
curl -X POST "http://localhost:8080/publish?msg=hello+everyone"
```

Expected: Both subscriber terminals receive `data: hello everyone`.

## Step 4 -- Named Events and Retry

SSE supports named event types and a `retry` field:

```go
func sseHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Tell the client to retry every 3 seconds on disconnect
	fmt.Fprintf(w, "retry: 3000\n\n")
	flusher.Flush()

	ch := broker.Subscribe()
	defer broker.Unsubscribe(ch)

	id := 0
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			id++
			fmt.Fprintf(w, "id: %d\n", id)
			fmt.Fprintf(w, "event: message\n")
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}
```

### Intermediate Verification

```bash
curl -N http://localhost:8080/events
```

Expected: First message is `retry: 3000`, then named events with `event: message`.

## Hints

- Buffer the subscriber channel (e.g., `make(chan string, 10)`) to avoid blocking the publisher
- Use `select` with a default case in `Publish` to drop messages for slow subscribers instead of blocking
- The `Last-Event-ID` header is sent by the browser on reconnect -- you can use it to replay missed events
- Test with multiple `curl -N` sessions to verify broadcast behavior

## Verification

- Single client receives streamed events with `id` and `data` fields
- Multiple clients all receive the same published messages
- Disconnecting a client removes it from the subscriber list
- The `retry` directive is sent on initial connection
- Publishing via the `/publish` endpoint broadcasts to all connected clients

## What's Next

Continue to [10 - WebSocket Server](../10-websocket-server/10-websocket-server.md) to learn bidirectional real-time communication.

## Summary

SSE uses `text/event-stream` content type and `http.Flusher` to push events from server to client over HTTP. A broker pattern with channels manages multiple subscribers. Client disconnection is detected via `r.Context().Done()`. SSE supports named events, IDs for resumption, and retry intervals. It is simpler than WebSockets for server-to-client streaming.

## Reference

- [MDN: Server-Sent Events](https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events)
- [SSE Specification](https://html.spec.whatwg.org/multipage/server-sent-events.html)
- [http.Flusher](https://pkg.go.dev/net/http#Flusher)
