# 6. Building a Line-Based Protocol

<!--
difficulty: advanced
concepts: [line-protocol, bufio-scanner, message-framing, command-parsing, protocol-design]
tools: [go, nc]
estimated_time: 45m
bloom_level: analyze
prerequisites: [tcp-server-and-client, concurrent-tcp-server, bufio-package]
-->

## Prerequisites

- Go 1.22+ installed
- Completed TCP Server and Client and Concurrent TCP Server exercises
- Familiarity with `bufio.Scanner` and `bufio.Writer`
- Understanding of message framing in stream protocols

## Learning Objectives

After completing this exercise, you will be able to:

- **Design** a line-based text protocol with commands, responses, and error codes
- **Implement** message framing using newline delimiters and `bufio.Scanner`
- **Handle** protocol state machines for multi-step interactions (authentication, sessions)
- **Test** protocol correctness with automated client tests

## Why Line-Based Protocols Matter

TCP is a byte stream -- it has no concept of messages. Your application must define where one message ends and the next begins (framing). The simplest framing strategy is newline-delimited text: each line is one message. This is how SMTP, Redis (RESP), FTP control, and many internal protocols work. It is human-readable, debuggable with netcat, and easy to parse with `bufio.Scanner`.

## The Problem

Build a key-value store server with a line-based text protocol. Clients connect over TCP and send commands like `SET key value`, `GET key`, `DEL key`, and `QUIT`. The server responds with status lines like `+OK`, `$value`, `-ERR message`, and tracks per-session state.

## Requirements

1. **Protocol spec**:
   - Commands: `SET key value\n`, `GET key\n`, `DEL key\n`, `KEYS\n`, `QUIT\n`
   - Success responses: `+OK\n`, `$value\n`, `+DELETED\n`
   - Error responses: `-ERR unknown command\n`, `-ERR key not found\n`
   - `KEYS` returns one key per line, terminated by `+END\n`
2. **Server** -- concurrent server with goroutine-per-connection; shared store protected by `sync.RWMutex`
3. **Scanner-based parsing** -- use `bufio.Scanner` for reading lines, `bufio.Writer` with `Flush()` for responses
4. **Command parser** -- parse commands by splitting on spaces; handle edge cases (empty lines, extra whitespace, values with spaces)
5. **Client library** -- a Go client type with `Set(key, value)`, `Get(key)`, `Del(key)`, and `Close()` methods that speak the protocol
6. **Tests** -- test the protocol through the client library against a test server

## Hints

<details>
<summary>Hint 1: Server command handler</summary>

```go
func (s *Server) handleConn(conn net.Conn) {
    defer conn.Close()
    scanner := bufio.NewScanner(conn)
    writer := bufio.NewWriter(conn)

    for scanner.Scan() {
        line := scanner.Text()
        parts := strings.SplitN(line, " ", 3)
        cmd := strings.ToUpper(parts[0])

        var response string
        switch cmd {
        case "SET":
            if len(parts) < 3 {
                response = "-ERR usage: SET key value"
            } else {
                s.store.Set(parts[1], parts[2])
                response = "+OK"
            }
        case "GET":
            if len(parts) < 2 {
                response = "-ERR usage: GET key"
            } else if val, ok := s.store.Get(parts[1]); ok {
                response = "$" + val
            } else {
                response = "-ERR key not found"
            }
        case "QUIT":
            fmt.Fprintln(writer, "+BYE")
            writer.Flush()
            return
        default:
            response = "-ERR unknown command"
        }
        fmt.Fprintln(writer, response)
        writer.Flush()
    }
}
```

</details>

<details>
<summary>Hint 2: Client library</summary>

```go
type Client struct {
    conn    net.Conn
    scanner *bufio.Scanner
    writer  *bufio.Writer
}

func (c *Client) Set(key, value string) error {
    fmt.Fprintf(c.writer, "SET %s %s\n", key, value)
    c.writer.Flush()
    resp, err := c.readLine()
    if err != nil {
        return err
    }
    if resp != "+OK" {
        return fmt.Errorf("unexpected response: %s", resp)
    }
    return nil
}

func (c *Client) Get(key string) (string, error) {
    fmt.Fprintf(c.writer, "GET %s\n", key)
    c.writer.Flush()
    resp, err := c.readLine()
    if err != nil {
        return "", err
    }
    if strings.HasPrefix(resp, "$") {
        return resp[1:], nil
    }
    return "", fmt.Errorf(resp)
}
```

</details>

<details>
<summary>Hint 3: Thread-safe store</summary>

```go
type Store struct {
    mu   sync.RWMutex
    data map[string]string
}

func (s *Store) Get(key string) (string, bool) {
    s.mu.RLock()
    defer s.mu.RUnlock()
    v, ok := s.data[key]
    return v, ok
}

func (s *Store) Set(key, value string) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.data[key] = value
}
```

</details>

## Verification

```bash
go test -v -race ./...

# Manual testing with netcat
go run . &
nc localhost 9000
SET name Alice
GET name
DEL name
GET name
QUIT
```

Your tests should confirm:
- SET/GET/DEL/KEYS commands work correctly through the client library
- Unknown commands return `-ERR unknown command`
- Missing keys return `-ERR key not found`
- Concurrent clients can SET and GET without races
- QUIT cleanly closes the connection

## What's Next

Continue to [07 - Connection Pooling Implementation](../07-connection-pooling-implementation/07-connection-pooling-implementation.md) to build a reusable TCP connection pool.

## Summary

- TCP is a byte stream; line-based protocols use `\n` as a message delimiter
- Use `bufio.Scanner` to read lines and `bufio.Writer` with `Flush()` to write responses
- `strings.SplitN` handles commands with variable arguments (e.g., values with spaces)
- Protect shared state with `sync.RWMutex` for concurrent read-heavy workloads
- Build a client library that encapsulates the protocol for clean test code
- Line-based protocols are human-debuggable with netcat

## Reference

- [bufio.Scanner](https://pkg.go.dev/bufio#Scanner)
- [bufio.Writer](https://pkg.go.dev/bufio#Writer)
- [Redis protocol specification (RESP)](https://redis.io/docs/reference/protocol-spec/)
