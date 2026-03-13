# 7. Connection Pooling Implementation

<!--
difficulty: advanced
concepts: [connection-pool, idle-connections, pool-size, health-check, connection-reuse, sync-pool]
tools: [go]
estimated_time: 40m
bloom_level: analyze
prerequisites: [tcp-server-and-client, sync-primitives, channels-basics]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of TCP connections and lifecycle
- Familiarity with channels, mutexes, and `sync.Pool`
- Understanding of resource management patterns

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** a TCP connection pool with configurable size, idle timeout, and health checks
- **Analyze** the tradeoffs between pool size, connection reuse, and resource consumption
- **Design** a pool that handles connection failures, stale connections, and pool exhaustion
- **Test** pool behavior under concurrent access and failure conditions

## Why Connection Pooling Matters

Establishing a TCP connection requires a three-way handshake (SYN, SYN-ACK, ACK), TLS negotiation (if applicable), and often an application-level handshake (authentication). This latency adds up when your application makes many short-lived connections to the same server. A connection pool maintains a set of reusable connections, amortizing setup cost across many requests.

Go's `database/sql` and `net/http` packages include built-in pools. Understanding how they work lets you build pools for custom protocols and tune existing ones.

## The Problem

Build a generic TCP connection pool that manages a set of reusable connections to a target server. The pool should handle concurrent access, stale connection detection, and graceful draining.

## Requirements

1. **Pool struct** -- `NewPool(addr string, opts PoolOptions) *Pool` with configurable: max open connections, max idle connections, idle timeout, dial timeout, health check function
2. **Get/Put** -- `Get(ctx context.Context) (net.Conn, error)` acquires a connection (from idle pool or new dial); `Put(conn net.Conn)` returns it to the pool
3. **Idle management** -- connections idle longer than `IdleTimeout` are closed on next `Get` or by a background reaper goroutine
4. **Health check** -- before returning an idle connection from `Get`, optionally run a health check function; if it fails, discard and try the next idle connection or dial a new one
5. **Pool exhaustion** -- when max open connections is reached, `Get` blocks until a connection is returned or context is cancelled
6. **Close** -- `Close()` drains all idle connections and prevents new `Get` calls
7. **Metrics** -- expose `Stats()` returning open count, idle count, wait count, and total wait time
8. **Tests** -- verify concurrent get/put, idle eviction, health check failure, pool exhaustion blocking, and graceful close

## Hints

<details>
<summary>Hint 1: Pool structure</summary>

```go
type Pool struct {
    mu          sync.Mutex
    addr        string
    idle        []net.Conn
    openCount   int
    maxOpen     int
    maxIdle     int
    idleTimeout time.Duration
    dialTimeout time.Duration
    healthCheck func(net.Conn) error
    waitCh      chan struct{} // semaphore for max open
    closed      bool
}
```

</details>

<details>
<summary>Hint 2: Get with idle reuse and dial fallback</summary>

```go
func (p *Pool) Get(ctx context.Context) (net.Conn, error) {
    p.mu.Lock()
    if p.closed {
        p.mu.Unlock()
        return nil, ErrPoolClosed
    }

    // Try idle connections
    for len(p.idle) > 0 {
        conn := p.idle[len(p.idle)-1]
        p.idle = p.idle[:len(p.idle)-1]
        p.mu.Unlock()

        if p.healthCheck != nil {
            if err := p.healthCheck(conn); err != nil {
                conn.Close()
                p.mu.Lock()
                p.openCount--
                continue
            }
        }
        return conn, nil
    }

    if p.openCount >= p.maxOpen {
        p.mu.Unlock()
        // Block waiting for a returned connection
        select {
        case <-p.waitCh:
            return p.Get(ctx)
        case <-ctx.Done():
            return nil, ctx.Err()
        }
    }

    p.openCount++
    p.mu.Unlock()
    return net.DialTimeout("tcp", p.addr, p.dialTimeout)
}
```

</details>

<details>
<summary>Hint 3: Put with idle limit</summary>

```go
func (p *Pool) Put(conn net.Conn) {
    p.mu.Lock()
    defer p.mu.Unlock()

    if p.closed || len(p.idle) >= p.maxIdle {
        conn.Close()
        p.openCount--
        return
    }

    p.idle = append(p.idle, conn)

    // Signal any waiters
    select {
    case p.waitCh <- struct{}{}:
    default:
    }
}
```

</details>

## Verification

```bash
go test -v -race ./...
```

Your tests should confirm:
- Connections are reused (dial count < request count)
- Idle connections are evicted after timeout
- Health check failures cause re-dial
- Pool blocks when max open is reached and unblocks when a connection is returned
- Context cancellation unblocks a waiting `Get`
- `Close()` prevents new `Get` calls and drains idle connections
- `Stats()` accurately reports pool state

## What's Next

Continue to [08 - TLS Server and Client](../08-tls-server-and-client/08-tls-server-and-client.md) to add TLS encryption to TCP connections.

## Summary

- A connection pool amortizes TCP/TLS handshake cost by reusing idle connections
- Track open count and idle count separately; enforce max limits on both
- Use a channel or condition variable to block `Get` when the pool is exhausted
- Idle connections must be health-checked or timed out to avoid returning stale connections
- `Close()` must drain idle connections and signal waiters to unblock
- Go's `database/sql.DB` and `net/http.Transport` are production examples of connection pools

## Reference

- [database/sql connection pool](https://pkg.go.dev/database/sql#DB.SetMaxOpenConns)
- [net/http Transport](https://pkg.go.dev/net/http#Transport)
- [sync.Pool](https://pkg.go.dev/sync#Pool)
