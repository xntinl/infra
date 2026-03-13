# 1. TCP Server and Client

<!--
difficulty: intermediate
concepts: [net-listen, net-dial, tcp-echo, connection-handling, read-write]
tools: [go, nc]
estimated_time: 25m
bloom_level: apply
prerequisites: [io-filesystem, error-handling, goroutines]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of I/O readers and writers
- Basic error handling
- Familiarity with goroutines

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** `net.Listen` to create a TCP server that accepts connections
- **Use** `net.Dial` to create a TCP client that connects to a server
- **Implement** a basic echo server that reads data and writes it back
- **Apply** proper connection closing and error handling

## Why TCP Networking in Go Matters

Go's `net` package provides a clean, portable interface for network programming. Unlike lower-level languages, Go handles platform differences (epoll on Linux, kqueue on macOS) behind the scenes. The same `net.Listen` and `net.Dial` API works for TCP, UDP, Unix sockets, and more.

Understanding TCP fundamentals -- connections, reads, writes, and proper cleanup -- is the foundation for everything from HTTP servers to custom protocols.

## Step 1 -- Create a TCP Echo Server

Create a project and write a TCP server that echoes back whatever it receives.

```bash
mkdir -p ~/go-exercises/tcp-echo
cd ~/go-exercises/tcp-echo
go mod init tcp-echo
```

Create `server.go`:

```go
package main

import (
	"fmt"
	"io"
	"log"
	"net"
)

func handleConnection(conn net.Conn) {
	defer conn.Close()
	log.Printf("new connection from %s", conn.RemoteAddr())

	buf := make([]byte, 1024)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			if err != io.EOF {
				log.Printf("read error: %v", err)
			}
			return
		}

		_, err = conn.Write(buf[:n])
		if err != nil {
			log.Printf("write error: %v", err)
			return
		}
	}
}

func main() {
	listener, err := net.Listen("tcp", ":9000")
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()
	fmt.Println("TCP echo server listening on :9000")

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("accept error: %v", err)
			continue
		}
		go handleConnection(conn)
	}
}
```

### Intermediate Verification

In one terminal:

```bash
go run server.go
```

In another terminal, use `nc` (netcat):

```bash
echo "hello" | nc localhost 9000
```

Expected: `hello` is echoed back.

## Step 2 -- Create a TCP Client

Create `client.go`:

```go
package main

import (
	"fmt"
	"log"
	"net"
)

func main() {
	conn, err := net.Dial("tcp", "localhost:9000")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	message := "Hello, TCP server!"
	_, err = conn.Write([]byte(message))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("sent: %s\n", message)

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("received: %s\n", string(buf[:n]))
}
```

### Intermediate Verification

With the server still running:

```bash
go run client.go
```

Expected:

```
sent: Hello, TCP server!
received: Hello, TCP server!
```

## Step 3 -- Use io.Copy for the Echo

Replace the manual read-write loop with `io.Copy`:

```go
func handleConnection(conn net.Conn) {
	defer conn.Close()
	log.Printf("new connection from %s", conn.RemoteAddr())
	_, err := io.Copy(conn, conn)
	if err != nil {
		log.Printf("copy error: %v", err)
	}
}
```

`io.Copy` reads from the second argument (the connection as `io.Reader`) and writes to the first argument (the connection as `io.Writer`). This is idiomatic Go for stream copying.

### Intermediate Verification

```bash
echo "test io.Copy" | nc localhost 9000
```

Expected: `test io.Copy` echoed back.

## Step 4 -- Write a Test

Create `server_test.go`:

```go
package main

import (
	"net"
	"testing"
)

func TestEchoServer(t *testing.T) {
	// Start server on random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handleConnection(conn)
		}
	}()

	// Connect client
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	msg := "hello test"
	_, err = conn.Write([]byte(msg))
	if err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}

	got := string(buf[:n])
	if got != msg {
		t.Errorf("got %q, want %q", got, msg)
	}
}
```

### Intermediate Verification

```bash
go test -v -race ./...
```

Expected: test passes, server echoes correctly.

## Common Mistakes

### Not Closing Connections

**Wrong:**

```go
conn, err := net.Dial("tcp", "localhost:9000")
// use conn but never close it
```

**What happens:** The connection leaks, consuming file descriptors on both client and server.

**Fix:** Always `defer conn.Close()` immediately after a successful dial or accept.

### Ignoring Partial Reads

**Wrong:**

```go
buf := make([]byte, 1024)
conn.Read(buf)
fmt.Println(string(buf)) // prints 1024 bytes including garbage
```

**What happens:** `Read` returns the number of bytes actually read, which may be less than the buffer size.

**Fix:** Always use the returned `n`: `string(buf[:n])`.

### Using a Fixed Port in Tests

**Wrong:**

```go
listener, _ := net.Listen("tcp", ":9000") // may conflict with other tests
```

**Fix:** Use port 0: `net.Listen("tcp", "127.0.0.1:0")` to let the OS assign a free port.

## Verify What You Learned

Run the server, send multiple messages with the client, and verify each is echoed correctly:

```bash
go test -v -race ./...
```

## What's Next

Continue to [02 - UDP Server and Client](../02-udp-server-and-client/02-udp-server-and-client.md) to learn UDP communication in Go.

## Summary

- `net.Listen("tcp", addr)` creates a TCP listener; `listener.Accept()` accepts connections
- `net.Dial("tcp", addr)` connects to a TCP server
- `net.Conn` implements both `io.Reader` and `io.Writer`
- `io.Copy(conn, conn)` is an idiomatic way to implement echo
- Always use the return value of `Read` to know how many bytes were received
- Use port 0 in tests for automatic port assignment

## Reference

- [net package](https://pkg.go.dev/net)
- [net.Listen](https://pkg.go.dev/net#Listen)
- [net.Dial](https://pkg.go.dev/net#Dial)
