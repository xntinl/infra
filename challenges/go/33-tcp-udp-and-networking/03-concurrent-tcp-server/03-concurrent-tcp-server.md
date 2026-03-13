# 3. Concurrent TCP Server

<!--
difficulty: intermediate
concepts: [goroutine-per-connection, connection-limit, graceful-shutdown, connection-tracking]
tools: [go, nc]
estimated_time: 30m
bloom_level: apply
prerequisites: [tcp-server-and-client, goroutines, sync-primitives, context]
-->

## Prerequisites

- Go 1.22+ installed
- Completed TCP Server and Client exercise
- Understanding of goroutines and `sync.WaitGroup`
- Familiarity with `context.Context`

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** a TCP server that handles many simultaneous connections using goroutines
- **Apply** connection limiting with a semaphore to prevent resource exhaustion
- **Design** graceful shutdown that waits for active connections to finish
- **Analyze** the goroutine-per-connection model and its trade-offs

## Why Concurrent TCP Servers Matter

A real TCP server must handle hundreds or thousands of simultaneous connections. Go's goroutine-per-connection model is simple and efficient: each accepted connection gets its own goroutine. Since goroutines are lightweight (a few KB of stack), this scales well.

But unlimited connection acceptance can exhaust file descriptors or memory. A production server needs connection limits, graceful shutdown (stop accepting, drain existing connections), and proper cleanup.

## Step 1 -- Basic Goroutine-Per-Connection Server

```bash
mkdir -p ~/go-exercises/concurrent-tcp
cd ~/go-exercises/concurrent-tcp
go mod init concurrent-tcp
```

Create `server.go`:

```go
package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strings"
)

func handleConn(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.ToLower(line) == "quit" {
			fmt.Fprintln(conn, "goodbye")
			return
		}
		response := strings.ToUpper(line)
		fmt.Fprintln(conn, response)
	}
}

func main() {
	ln, err := net.Listen("tcp", ":9002")
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()
	log.Println("server listening on :9002")

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("accept: %v", err)
			continue
		}
		go handleConn(conn)
	}
}
```

### Intermediate Verification

Start the server and connect multiple clients simultaneously:

```bash
go run server.go &
echo -e "hello\nworld\nquit" | nc localhost 9002
echo -e "foo\nbar\nquit" | nc localhost 9002
```

Expected: each client gets uppercased responses independently.

## Step 2 -- Add Connection Limiting

Use a buffered channel as a semaphore to limit concurrent connections:

```go
package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strings"
)

const maxConnections = 100

func handleConn(conn net.Conn, sem chan struct{}) {
	defer conn.Close()
	defer func() { <-sem }() // release semaphore slot

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.ToLower(line) == "quit" {
			fmt.Fprintln(conn, "goodbye")
			return
		}
		fmt.Fprintln(conn, strings.ToUpper(line))
	}
}

func main() {
	ln, err := net.Listen("tcp", ":9002")
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()

	sem := make(chan struct{}, maxConnections)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("accept: %v", err)
			continue
		}
		sem <- struct{}{} // acquire slot (blocks if at max)
		go handleConn(conn, sem)
	}
}
```

### Intermediate Verification

```bash
go test -v -race ./...
```

## Step 3 -- Add Graceful Shutdown

```go
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"os/signal"
	"strings"
	"sync"
	"syscall"
)

type Server struct {
	listener net.Listener
	wg       sync.WaitGroup
	quit     chan struct{}
}

func NewServer(addr string) (*Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &Server{listener: ln, quit: make(chan struct{})}, nil
}

func (s *Server) Serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.quit:
				return
			default:
				log.Printf("accept: %v", err)
				continue
			}
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConn(conn)
		}()
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.ToLower(line) == "quit" {
			fmt.Fprintln(conn, "goodbye")
			return
		}
		fmt.Fprintln(conn, strings.ToUpper(line))
	}
}

func (s *Server) Shutdown() {
	close(s.quit)
	s.listener.Close()
	s.wg.Wait()
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv, err := NewServer(":9002")
	if err != nil {
		log.Fatal(err)
	}
	log.Println("server listening on :9002")

	go srv.Serve()

	<-ctx.Done()
	log.Println("shutting down...")
	srv.Shutdown()
	log.Println("server stopped")
}
```

### Intermediate Verification

```bash
go run server.go &
PID=$!
echo -e "hello\nquit" | nc localhost 9002
kill -INT $PID
# Server should print "shutting down..." and "server stopped"
```

## Common Mistakes

### Forgetting WaitGroup in Shutdown

**Wrong:**

```go
func (s *Server) Shutdown() {
    s.listener.Close() // stops accepting
    // but active connections are killed immediately
}
```

**Fix:** Track active connections with `sync.WaitGroup` and wait for them to finish.

### Not Handling Accept Errors After Close

**Wrong:**

```go
conn, err := s.listener.Accept()
if err != nil {
    log.Fatal(err) // crashes on shutdown
}
```

**Fix:** Check if the error is due to the listener being closed (shutdown path).

## Verify What You Learned

```bash
go test -v -race ./...
```

## What's Next

Continue to [04 - Connection Timeouts and Deadlines](../04-connection-timeouts-and-deadlines/04-connection-timeouts-and-deadlines.md) to learn how to set deadlines on TCP connections.

## Summary

- Goroutine-per-connection is Go's standard model for TCP servers
- Use a buffered channel as a semaphore to limit concurrent connections
- Graceful shutdown: stop accepting, wait for active connections, then exit
- Track active connections with `sync.WaitGroup` for clean shutdown
- Handle `Accept` errors that occur when the listener is closed during shutdown

## Reference

- [net.Listener](https://pkg.go.dev/net#Listener)
- [Graceful shutdown patterns](https://go.dev/doc/modules/layout#server-project)
- [signal.NotifyContext](https://pkg.go.dev/os/signal#NotifyContext)
