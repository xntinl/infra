# 18. Connection Draining

<!--
difficulty: advanced
concepts: [graceful-shutdown, connection-draining, drain-timeout, signal-handling, in-flight-requests, health-checks]
tools: [go]
estimated_time: 40m
bloom_level: apply
prerequisites: [concurrent-tcp-server, tcp-keep-alive, http-programming, context]
-->

## Prerequisites

- Go 1.22+ installed
- Completed Concurrent TCP Server and TCP Keep-Alive exercises
- Understanding of `context.Context` and cancellation
- Familiarity with OS signals (`SIGTERM`, `SIGINT`)

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** graceful connection draining for both TCP and HTTP servers
- **Design** a shutdown sequence: stop accepting new connections, wait for in-flight requests, force-close after timeout
- **Analyze** the interaction between signal handling, context cancellation, and connection lifecycle
- **Apply** health check endpoint state transitions (healthy -> draining -> stopped) for load balancer integration

## Why Connection Draining Matters

When you deploy a new version, the old process must shut down. If it closes immediately, in-flight requests fail. Connection draining is a controlled shutdown: the server stops accepting new connections, waits for active requests to complete (up to a timeout), then force-closes remaining connections. Load balancers check a health endpoint to know when to stop sending new traffic. Without proper draining, every deployment causes errors for active users.

## The Problem

Build a TCP and HTTP server that supports graceful shutdown with configurable drain timeout, health check state transitions, and in-flight request tracking.

## Requirements

1. **Signal handling** -- catch `SIGTERM` and `SIGINT` to initiate graceful shutdown
2. **Stop accepting** -- close the listener to stop accepting new connections immediately on shutdown signal
3. **In-flight tracking** -- track active connections/requests using a `sync.WaitGroup` or atomic counter
4. **Drain timeout** -- wait up to a configurable duration for active connections to finish; force-close after timeout
5. **Health endpoint** -- expose `/health` that returns 200 when healthy and 503 when draining, so load balancers stop sending traffic
6. **HTTP server** -- use `http.Server.Shutdown()` for the HTTP server path, demonstrating Go's built-in graceful shutdown
7. **TCP server** -- implement manual connection draining for a raw TCP server (no built-in `Shutdown`)
8. **Connection notification** -- notify active connections that the server is draining (e.g., set a short deadline so long-polling connections close promptly)
9. **Tests** -- verify that in-flight requests complete, new connections are rejected during drain, and the server exits after the drain timeout

## Hints

<details>
<summary>Hint 1: HTTP server graceful shutdown</summary>

```go
srv := &http.Server{Addr: ":8080", Handler: mux}

go func() {
    sig := make(chan os.Signal, 1)
    signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
    <-sig

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    srv.Shutdown(ctx) // Stops accepting, waits for in-flight
}()

srv.ListenAndServe()
```

</details>

<details>
<summary>Hint 2: TCP server manual draining</summary>

```go
type TCPServer struct {
    listener net.Listener
    wg       sync.WaitGroup
    quit     chan struct{}
    draining atomic.Bool
}

func (s *TCPServer) Drain(timeout time.Duration) {
    s.draining.Store(true)
    s.listener.Close() // Stop accepting

    done := make(chan struct{})
    go func() {
        s.wg.Wait()
        close(done)
    }()

    select {
    case <-done:
        log.Println("all connections drained")
    case <-time.After(timeout):
        log.Println("drain timeout, force closing")
    }
}
```

</details>

<details>
<summary>Hint 3: Health check state machine</summary>

```go
type HealthState int32

const (
    StateHealthy  HealthState = 0
    StateDraining HealthState = 1
    StateStopped  HealthState = 2
)

var state atomic.Int32

func healthHandler(w http.ResponseWriter, r *http.Request) {
    if HealthState(state.Load()) != StateHealthy {
        w.WriteHeader(http.StatusServiceUnavailable)
        fmt.Fprint(w, "draining")
        return
    }
    w.WriteHeader(http.StatusOK)
    fmt.Fprint(w, "ok")
}
```

</details>

## Verification

```bash
go test -v -race ./...
```

Your tests should confirm:
- In-flight requests complete successfully during drain
- New connections are rejected after shutdown signal
- Health endpoint returns 503 during draining
- Server exits after drain timeout even if connections remain
- HTTP `Shutdown()` waits for active handlers to return
- TCP draining tracks connections via WaitGroup accurately

## What's Next

Continue to [19 - Building a SOCKS5 Proxy](../19-building-a-socks5-proxy/19-building-a-socks5-proxy.md) to implement the SOCKS5 proxy protocol from scratch.

## Summary

- Connection draining is a three-phase shutdown: stop accepting, wait for in-flight, force-close after timeout
- `http.Server.Shutdown()` implements graceful shutdown for HTTP servers out of the box
- Raw TCP servers need manual draining with `sync.WaitGroup` to track active connections
- Health check endpoints signal load balancers to stop sending traffic before the server shuts down
- Signal handling (`SIGTERM`, `SIGINT`) triggers the drain sequence in production deployments
- A drain timeout prevents the server from hanging indefinitely on stuck connections

## Reference

- [http.Server.Shutdown](https://pkg.go.dev/net/http#Server.Shutdown)
- [os/signal](https://pkg.go.dev/os/signal)
- [Kubernetes Pod Termination](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-termination)
