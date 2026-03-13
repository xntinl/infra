# 4. Connection Timeouts and Deadlines

<!--
difficulty: advanced
concepts: [set-deadline, set-read-deadline, set-write-deadline, dial-timeout, net-error-timeout]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [tcp-server-and-client, error-handling, context]
-->

## Prerequisites

- Go 1.22+ installed
- Completed TCP Server and Client exercise
- Understanding of `net.Conn` and error handling
- Familiarity with `context.Context` and `time.Duration`

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** `SetDeadline`, `SetReadDeadline`, and `SetWriteDeadline` to TCP connections
- **Analyze** the difference between connection timeout (`DialTimeout`) and operation deadlines
- **Implement** per-operation timeouts that reset after each successful I/O
- **Handle** timeout errors correctly using `net.Error` interface

## Why Connection Timeouts Matter

Without timeouts, a TCP Read or Write can block forever. A slow client that stops sending data will hold a goroutine and connection open indefinitely. A server that stops responding will cause the client to hang. Timeouts are mandatory for production networking code.

Go provides three levels of timeout control: `DialTimeout` for connection establishment, `SetDeadline` for absolute deadlines, and `SetReadDeadline`/`SetWriteDeadline` for per-direction deadlines. Understanding when to use each is critical.

## The Problem

Build a TCP server and client that use timeouts at every level. The server disconnects idle clients. The client detects unresponsive servers. Both handle timeout errors gracefully.

## Requirements

1. **Dial timeout** -- client uses `net.DialTimeout` or `net.Dialer{Timeout}` to limit connection time
2. **Idle timeout** -- server sets a `ReadDeadline` after each message; if the client does not send within the deadline, the server disconnects
3. **Write deadline** -- server sets a `WriteDeadline` before writing responses to detect slow clients
4. **Per-operation reset** -- deadlines reset after each successful operation (sliding window pattern)
5. **Timeout detection** -- check `err.(net.Error).Timeout()` to distinguish timeouts from other errors
6. **Context-based dialer** -- demonstrate `net.Dialer.DialContext` for context-aware connection with cancellation
7. **Tests** -- test idle timeout (client goes silent) and dial timeout (unreachable server)

## Hints

<details>
<summary>Hint 1: Setting idle timeout on server</summary>

```go
func handleConn(conn net.Conn, idleTimeout time.Duration) {
    defer conn.Close()
    buf := make([]byte, 1024)
    for {
        conn.SetReadDeadline(time.Now().Add(idleTimeout))
        n, err := conn.Read(buf)
        if err != nil {
            if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
                log.Printf("client idle timeout: %s", conn.RemoteAddr())
                return
            }
            return
        }
        conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
        conn.Write(buf[:n])
    }
}
```

</details>

<details>
<summary>Hint 2: Context-aware dialer</summary>

```go
ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
defer cancel()

dialer := net.Dialer{}
conn, err := dialer.DialContext(ctx, "tcp", "localhost:9000")
if err != nil {
    // could be timeout, cancellation, or connection refused
}
```

</details>

<details>
<summary>Hint 3: Testing idle timeout</summary>

```go
func TestIdleTimeout(t *testing.T) {
    // Start server with 100ms idle timeout
    // Connect client, send one message, then wait
    // Server should close the connection after ~100ms
    conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
    _, err := conn.Read(buf)
    if err == nil {
        t.Error("expected connection to be closed by server")
    }
}
```

</details>

## Verification

```bash
go test -v -race -timeout 30s ./...
```

Your tests should confirm:
- Server disconnects idle clients after the configured timeout
- Client detects connection timeout to unreachable servers
- Timeout errors are correctly identified via `net.Error` interface
- Active connections with regular I/O are not timed out (deadline resets)

## What's Next

Continue to [05 - TCP Keep-Alive](../05-tcp-keep-alive/05-tcp-keep-alive.md) to learn how TCP keep-alive detects dead connections.

## Summary

- `SetDeadline` sets an absolute time after which Read/Write return a timeout error
- `SetReadDeadline`/`SetWriteDeadline` control timeouts per direction independently
- Reset deadlines after each successful I/O for a sliding idle timeout
- `net.DialTimeout` limits connection establishment time; `DialContext` adds cancellation
- Check `err.(net.Error).Timeout()` to distinguish timeout errors from other failures
- Every production TCP connection must have deadlines to prevent indefinite blocking

## Reference

- [net.Conn.SetDeadline](https://pkg.go.dev/net#Conn)
- [net.DialTimeout](https://pkg.go.dev/net#DialTimeout)
- [net.Dialer](https://pkg.go.dev/net#Dialer)
