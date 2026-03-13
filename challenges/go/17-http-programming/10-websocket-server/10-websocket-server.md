# 10. WebSocket Server

<!--
difficulty: advanced
concepts: [websocket, gorilla-websocket, bidirectional, upgrade, ping-pong, broadcast]
tools: [go, curl, wscat]
estimated_time: 40m
bloom_level: analyze
prerequisites: [http-server, goroutines, channels, sync-primitives]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of goroutines, channels, and mutexes
- Familiarity with HTTP upgrade mechanism
- Optional: `wscat` (`npm install -g wscat`) for interactive testing

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** a WebSocket server using `gorilla/websocket`
- **Manage** concurrent read/write goroutines per connection
- **Build** a chat room that broadcasts messages to all connected clients
- **Handle** ping/pong and graceful connection close

## Why WebSockets

WebSockets provide full-duplex communication over a single TCP connection. Unlike SSE, both client and server can send messages at any time. This makes WebSockets ideal for chat applications, collaborative editing, gaming, and any scenario requiring bidirectional real-time communication. Go's goroutine model maps naturally to the per-connection read/write loops.

## The Problem

Build a WebSocket chat server where clients connect, choose a username, and send messages that are broadcast to all other connected clients. The server must handle concurrent connections, clean disconnections, and ping/pong keepalives.

## Requirements

1. WebSocket upgrade endpoint using `gorilla/websocket`
2. Per-connection read and write goroutines
3. A hub that broadcasts messages to all connected clients
4. Graceful handling of client disconnects
5. Ping/pong keepalive with configurable timeouts

## Step 1 -- Set Up and Basic Echo Server

```bash
mkdir -p ~/go-exercises/websocket
cd ~/go-exercises/websocket
go mod init websocket-server
go get github.com/gorilla/websocket
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // allow all origins for development
	},
}

func echoHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade error: %v", err)
		return
	}
	defer conn.Close()

	for {
		msgType, msg, err := conn.ReadMessage()
		if err != nil {
			log.Printf("read error: %v", err)
			break
		}
		log.Printf("received: %s", msg)

		err = conn.WriteMessage(msgType, msg)
		if err != nil {
			log.Printf("write error: %v", err)
			break
		}
	}
}

func main() {
	http.HandleFunc("/ws", echoHandler)
	fmt.Println("WebSocket server on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
```

### Intermediate Verification

```bash
go run main.go &
# Using wscat:
wscat -c ws://localhost:8080/ws
# Type "hello" and press Enter
```

Expected: The server echoes back `hello`. If `wscat` is not available, use curl to verify the upgrade:

```bash
curl -i -N \
  -H "Connection: Upgrade" \
  -H "Upgrade: websocket" \
  -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
  -H "Sec-WebSocket-Version: 13" \
  http://localhost:8080/ws
```

Expected: HTTP 101 Switching Protocols response.

## Step 2 -- Build a Hub for Broadcasting

Create `hub.go`:

```go
package main

import "sync"

type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]struct{}
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[*Client]struct{}),
	}
}

func (h *Hub) Register(c *Client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	log.Printf("Client %s connected (total: %d)", c.username, h.Count())
}

func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	delete(h.clients, c)
	close(c.send)
	h.mu.Unlock()
	log.Printf("Client %s disconnected (total: %d)", c.username, h.Count())
}

func (h *Hub) Broadcast(msg []byte, sender *Client) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.clients {
		if client == sender {
			continue
		}
		select {
		case client.send <- msg:
		default:
			// drop message for slow client
		}
	}
}

func (h *Hub) Count() int {
	return len(h.clients)
}
```

Add the import at the top of `hub.go`:

```go
package main

import (
	"log"
	"sync"
)
```

### Intermediate Verification

```bash
go build ./...
```

Expected: Clean compilation.

## Step 3 -- Create the Client with Read/Write Goroutines

Create `client.go`:

```go
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 4096
)

type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	send     chan []byte
	username string
}

func NewClient(hub *Hub, conn *websocket.Conn, username string) *Client {
	return &Client{
		hub:      hub,
		conn:     conn,
		send:     make(chan []byte, 256),
		username: username,
	}
}

func (c *Client) ReadPump() {
	defer func() {
		c.hub.Unregister(c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("read error: %v", err)
			}
			break
		}

		formatted := fmt.Sprintf("[%s]: %s", c.username, message)
		c.hub.Broadcast([]byte(formatted), c)
	}
}

func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			err := c.conn.WriteMessage(websocket.TextMessage, msg)
			if err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
```

### Intermediate Verification

```bash
go build ./...
```

Expected: Clean compilation.

## Step 4 -- Wire Everything Together

Update `main.go`:

```go
package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

var hub = NewHub()

func chatHandler(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("name")
	if username == "" {
		username = "anonymous"
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade error: %v", err)
		return
	}

	client := NewClient(hub, conn, username)
	hub.Register(client)

	go client.WritePump()
	client.ReadPump() // blocks until disconnect
}

func main() {
	http.HandleFunc("/ws", chatHandler)
	fmt.Println("Chat server on :8080 -- connect to /ws?name=yourname")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
```

### Intermediate Verification

Open two terminals:

```bash
wscat -c "ws://localhost:8080/ws?name=alice"
# In another terminal:
wscat -c "ws://localhost:8080/ws?name=bob"
```

Type a message in Alice's terminal. Bob should receive `[alice]: <message>` and vice versa.

## Hints

- `ReadPump` runs in the handler goroutine; `WritePump` runs in a separate goroutine -- this avoids concurrent writes to the connection
- The `gorilla/websocket` docs explicitly state: "Connections support one concurrent reader and one concurrent writer"
- Use a buffered `send` channel to prevent slow clients from blocking the broadcaster
- `SetPongHandler` resets the read deadline on each pong, keeping the connection alive
- In production, restrict `CheckOrigin` to your allowed domains

## Verification

- Two clients can exchange messages via the broadcast hub
- Disconnecting a client does not crash the server
- Server logs show client connect/disconnect events with correct totals
- Ping/pong keepalive prevents idle timeouts
- Messages from a sender are not echoed back to the sender

## What's Next

Continue to [11 - HTTP/2 Server Push](../11-http2-server-push/11-http2-server-push.md) to explore HTTP/2-specific features.

## Summary

WebSockets provide full-duplex communication over HTTP. The `gorilla/websocket` package handles the protocol upgrade. Each connection uses two goroutines: `ReadPump` for incoming messages and `WritePump` for outgoing messages and ping/pong. A hub manages client registration, deregistration, and message broadcasting. Always set read limits, write deadlines, and handle pong messages for production reliability.

## Reference

- [gorilla/websocket](https://pkg.go.dev/github.com/gorilla/websocket)
- [RFC 6455 - The WebSocket Protocol](https://tools.ietf.org/html/rfc6455)
- [gorilla/websocket chat example](https://github.com/gorilla/websocket/tree/main/examples/chat)
