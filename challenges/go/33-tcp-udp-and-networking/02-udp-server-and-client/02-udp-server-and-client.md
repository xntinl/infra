# 2. UDP Server and Client

<!--
difficulty: intermediate
concepts: [net-listenpacket, udp, datagram, packet-conn, connectionless]
tools: [go, nc]
estimated_time: 25m
bloom_level: apply
prerequisites: [tcp-server-and-client, error-handling]
-->

## Prerequisites

- Go 1.22+ installed
- Completed TCP Server and Client exercise
- Understanding of the difference between TCP (stream) and UDP (datagram)

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** `net.ListenPacket` to create a UDP server
- **Use** `net.Dial` for connected UDP and `net.ListenPacket` for unconnected UDP
- **Apply** the key differences between TCP and UDP in Go's `net` package
- **Analyze** when UDP is appropriate (low latency, loss tolerance, broadcast)

## Why UDP Matters

UDP is connectionless and unreliable -- packets can arrive out of order, duplicated, or not at all. But this makes it fast: no connection setup, no teardown, no head-of-line blocking. DNS, video streaming, gaming, and metrics collection use UDP because speed matters more than guaranteed delivery.

In Go, UDP uses `PacketConn` instead of `Conn`. The server does not call `Accept` because there are no connections -- it reads datagrams from any source and sends responses back to the source address.

## Step 1 -- Create a UDP Echo Server

```bash
mkdir -p ~/go-exercises/udp-echo
cd ~/go-exercises/udp-echo
go mod init udp-echo
```

Create `server.go`:

```go
package main

import (
	"fmt"
	"log"
	"net"
)

func main() {
	pc, err := net.ListenPacket("udp", ":9001")
	if err != nil {
		log.Fatal(err)
	}
	defer pc.Close()
	fmt.Println("UDP echo server listening on :9001")

	buf := make([]byte, 1024)
	for {
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			log.Printf("read error: %v", err)
			continue
		}
		log.Printf("received %d bytes from %s: %s", n, addr, string(buf[:n]))

		_, err = pc.WriteTo(buf[:n], addr)
		if err != nil {
			log.Printf("write error: %v", err)
		}
	}
}
```

### Intermediate Verification

In one terminal:

```bash
go run server.go
```

In another:

```bash
echo -n "hello udp" | nc -u -w1 localhost 9001
```

Expected: `hello udp` is echoed back.

## Step 2 -- Create a UDP Client

Create `client.go`:

```go
package main

import (
	"fmt"
	"log"
	"net"
)

func main() {
	conn, err := net.Dial("udp", "localhost:9001")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	message := "Hello, UDP server!"
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

Note: `net.Dial("udp", addr)` creates a "connected" UDP socket. It does not establish a connection (UDP is connectionless), but it sets a default destination so you can use `Write`/`Read` instead of `WriteTo`/`ReadFrom`.

### Intermediate Verification

```bash
go run client.go
```

Expected:

```
sent: Hello, UDP server!
received: Hello, UDP server!
```

## Step 3 -- Handle Multiple Clients

Unlike TCP, a single UDP socket handles all clients. There is no per-client goroutine needed for basic request-response patterns.

Create `multi_client_test.go`:

```go
package main

import (
	"net"
	"sync"
	"testing"
)

func TestMultipleUDPClients(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()

	// Start server
	go func() {
		buf := make([]byte, 1024)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			pc.WriteTo(buf[:n], addr)
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			conn, err := net.Dial("udp", pc.LocalAddr().String())
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()

			msg := []byte(fmt.Sprintf("client-%d", id))
			conn.Write(msg)

			buf := make([]byte, 1024)
			n, err := conn.Read(buf)
			if err != nil {
				t.Error(err)
				return
			}
			if string(buf[:n]) != string(msg) {
				t.Errorf("client %d: got %q, want %q", id, string(buf[:n]), string(msg))
			}
		}(i)
	}
	wg.Wait()
}
```

### Intermediate Verification

```bash
go test -v -race ./...
```

Expected: all 10 clients send and receive their messages correctly.

## Common Mistakes

### Expecting Reliable Delivery

**Wrong assumption:** "If I send a UDP packet, it will arrive."

**Reality:** UDP packets can be lost, especially under network congestion. For critical data, implement application-level acknowledgements or use TCP.

### Using Large Datagrams

**Wrong:**

```go
data := make([]byte, 65536)
conn.Write(data) // may be fragmented or dropped
```

**Fix:** Keep UDP datagrams under the MTU (typically 1400-1472 bytes for IPv4 over Ethernet) to avoid IP fragmentation.

### Not Setting Read Deadlines

**Wrong:**

```go
conn.Read(buf) // blocks forever if no response
```

**Fix:** Set a deadline:

```go
conn.SetReadDeadline(time.Now().Add(2 * time.Second))
n, err := conn.Read(buf)
if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
    // handle timeout
}
```

## Verify What You Learned

Run the test suite:

```bash
go test -v -race ./...
```

## What's Next

Continue to [03 - Concurrent TCP Server](../03-concurrent-tcp-server/03-concurrent-tcp-server.md) to build a TCP server that handles many simultaneous connections.

## Summary

- `net.ListenPacket("udp", addr)` creates a UDP server; use `ReadFrom`/`WriteTo` for each datagram
- `net.Dial("udp", addr)` creates a connected UDP client; use `Read`/`Write`
- UDP is connectionless -- one socket handles all clients without `Accept`
- Keep datagrams under the MTU to avoid fragmentation
- UDP does not guarantee delivery, ordering, or deduplication
- Set read deadlines to prevent blocking forever on missing responses

## Reference

- [net.ListenPacket](https://pkg.go.dev/net#ListenPacket)
- [net.PacketConn](https://pkg.go.dev/net#PacketConn)
- [UDP on Wikipedia](https://en.wikipedia.org/wiki/User_Datagram_Protocol)
