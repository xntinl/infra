# 5. TCP Keep-Alive

<!--
difficulty: advanced
concepts: [tcp-keepalive, net-tcpconn, keepalive-config, dead-connection-detection, idle-timeout]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [tcp-server-and-client, connection-timeouts-and-deadlines]
-->

## Prerequisites

- Go 1.22+ installed
- Completed TCP Server and Client exercise
- Understanding of connection timeouts and deadlines
- Basic knowledge of TCP connection lifecycle

## Learning Objectives

After completing this exercise, you will be able to:

- **Configure** TCP keep-alive on both server and client connections
- **Analyze** how keep-alive detects dead connections versus idle connections
- **Implement** application-level heartbeats as a complement to TCP keep-alive
- **Test** dead connection detection in a controlled environment

## Why TCP Keep-Alive Matters

A TCP connection can sit idle indefinitely without either side knowing whether the peer is still alive. If a client crashes, loses network, or a NAT/firewall silently drops the mapping, the server holds resources for a connection that will never produce data again. TCP keep-alive sends periodic probes on idle connections to detect dead peers. Without it, your server leaks file descriptors and memory for ghost connections.

Go 1.23+ provides `net.KeepAliveConfig` for fine-grained control over keep-alive timing. Earlier versions use `SetKeepAlive` and `SetKeepAlivePeriod`.

## The Problem

Build a TCP server that configures keep-alive to detect dead connections within 30 seconds. Implement an application-level heartbeat protocol as a fallback for environments where TCP keep-alive probes are filtered.

## Requirements

1. **TCP keep-alive** -- enable keep-alive on accepted connections using `TCPConn.SetKeepAlive(true)` and `SetKeepAlivePeriod(10 * time.Second)`
2. **KeepAliveConfig (Go 1.23+)** -- use `net.KeepAliveConfig` with `Enable: true`, `Idle`, `Interval`, and `Count` fields for precise control
3. **ListenConfig** -- configure keep-alive defaults on the listener using `net.ListenConfig{KeepAlive: 15 * time.Second}`
4. **Application heartbeat** -- implement a ping/pong protocol: client sends "PING\n" every 5 seconds, server responds "PONG\n"; if no ping is received within 15 seconds, server closes the connection
5. **Dead connection test** -- write a test that connects, enables keep-alive, then closes the underlying OS connection (simulating a crash) and verifies the server detects it
6. **Tests** -- verify keep-alive is enabled on connections and heartbeat timeout works

## Hints

<details>
<summary>Hint 1: Enabling keep-alive on accepted connections</summary>

```go
conn, err := listener.Accept()
if err != nil {
    return err
}

tcpConn, ok := conn.(*net.TCPConn)
if !ok {
    return fmt.Errorf("not a TCP connection")
}
tcpConn.SetKeepAlive(true)
tcpConn.SetKeepAlivePeriod(10 * time.Second)
```

</details>

<details>
<summary>Hint 2: ListenConfig with keep-alive defaults</summary>

```go
lc := net.ListenConfig{
    KeepAlive: 15 * time.Second, // default for accepted connections
}
listener, err := lc.Listen(ctx, "tcp", ":9000")
```

</details>

<details>
<summary>Hint 3: Application-level heartbeat</summary>

```go
func (s *Server) handleWithHeartbeat(conn net.Conn) {
    defer conn.Close()
    scanner := bufio.NewScanner(conn)

    for {
        conn.SetReadDeadline(time.Now().Add(15 * time.Second))
        if !scanner.Scan() {
            log.Printf("client %s timed out or disconnected", conn.RemoteAddr())
            return
        }
        line := scanner.Text()
        if line == "PING" {
            fmt.Fprintln(conn, "PONG")
        }
    }
}
```

</details>

## Verification

```bash
go test -v -race ./...
```

Your tests should confirm:
- Keep-alive is enabled on accepted connections
- The heartbeat protocol responds PONG to PING
- The server closes connections that miss heartbeats beyond the timeout
- `ListenConfig.KeepAlive` sets defaults on new connections

## What's Next

Continue to [06 - Building a Line-Based Protocol](../06-building-a-line-based-protocol/06-building-a-line-based-protocol.md) to design and implement a custom text protocol over TCP.

## Summary

- TCP keep-alive sends probes on idle connections to detect dead peers
- Use `SetKeepAlive(true)` and `SetKeepAlivePeriod()` on `*net.TCPConn`
- Go 1.23+ adds `net.KeepAliveConfig` for `Idle`, `Interval`, and `Count` control
- `net.ListenConfig{KeepAlive: d}` sets keep-alive defaults for all accepted connections
- Application-level heartbeats provide faster detection than OS-level keep-alive
- Combine TCP keep-alive with read deadlines for robust dead connection detection

## Reference

- [net.TCPConn.SetKeepAlive](https://pkg.go.dev/net#TCPConn.SetKeepAlive)
- [net.ListenConfig](https://pkg.go.dev/net#ListenConfig)
- [TCP Keep-Alive HOWTO](https://tldp.org/HOWTO/TCP-Keepalive-HOWTO/)
