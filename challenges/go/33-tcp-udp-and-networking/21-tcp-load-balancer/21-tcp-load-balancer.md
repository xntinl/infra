# 21. TCP Load Balancer

<!--
difficulty: insane
concepts: [load-balancer, round-robin, least-connections, health-check, backend-pool, connection-tracking, weighted-balancing]
tools: [go]
estimated_time: 90m
bloom_level: create
prerequisites: [concurrent-tcp-server, connection-pooling-implementation, connection-draining, tcp-keep-alive]
-->

## Prerequisites

- Go 1.22+ installed
- Completed Concurrent TCP Server, Connection Pooling, and Connection Draining exercises
- Understanding of load balancing concepts (round-robin, least connections, health checks)
- Experience with concurrent programming, atomic operations, and TCP connection lifecycle

## Learning Objectives

- **Create** an L4 TCP load balancer that distributes connections across a pool of backend servers
- **Implement** multiple load balancing algorithms: round-robin, weighted round-robin, and least connections
- **Design** active and passive health checking to remove unhealthy backends from rotation

## The Challenge

Load balancers distribute incoming connections across multiple backend servers to improve throughput, reduce latency, and provide fault tolerance. L4 load balancers operate at the TCP layer -- they accept a client connection, select a backend, establish a connection to it, and relay bytes bidirectionally without understanding the application protocol.

Your task is to build a TCP load balancer with configurable balancing algorithms, active health checking, connection tracking, and graceful backend removal. The balancer must handle backend failures transparently, retrying on a different backend when a connection attempt fails.

## Requirements

1. Accept incoming TCP connections on a configurable listen address and distribute them to a pool of backend servers
2. Implement three balancing algorithms, selectable at startup: round-robin, weighted round-robin (backends have configurable weights), and least-connections (route to the backend with fewest active connections)
3. Implement bidirectional byte relay between client and selected backend using goroutines, with proper half-close handling
4. Implement active health checking: periodically (configurable interval) open a TCP connection to each backend; mark backends that fail to connect as unhealthy and exclude them from selection
5. Implement passive health checking: track consecutive connection failures per backend; mark as unhealthy after a configurable failure threshold
6. Automatically restore backends to healthy state after a configurable number of successful health checks
7. When a backend connection fails during selection, retry on the next available backend (up to a configurable retry count)
8. Track per-backend metrics: active connections, total connections, bytes sent, bytes received, health check failures
9. Expose a metrics summary via a simple HTTP endpoint on a separate port
10. Support graceful shutdown: stop accepting new connections, drain active connections with a configurable timeout
11. Write integration tests using loopback TCP servers as backends

## Hints

- Use an interface for the balancing algorithm so you can swap implementations without changing the core proxy logic
- For least-connections, maintain an atomic counter per backend that increments on connect and decrements on disconnect
- Active health checks should run in a separate goroutine per backend, using a ticker for periodic checks
- Use `sync.RWMutex` to protect the backend pool so health checks can update backend status while the balancer reads it
- For retry on failure, implement a `selectBackend` that skips the previously failed backend

## Success Criteria

1. The load balancer distributes connections across backends using the configured algorithm
2. Round-robin distributes connections evenly across all healthy backends
3. Least-connections routes new connections to the backend with the fewest active connections
4. Unhealthy backends are excluded from selection and automatically restored when they recover
5. Connection failure triggers transparent retry on a different backend
6. The metrics endpoint reports accurate per-backend connection counts and byte totals
7. Graceful shutdown drains active connections within the configured timeout
8. The balancer handles at least 100 concurrent connections across 5 backends without goroutine leaks
9. All tests pass with the `-race` flag enabled

## Research Resources

- [HAProxy Architecture](https://www.haproxy.org/download/2.8/doc/architecture.txt) -- reference L4/L7 load balancer design
- [Envoy Load Balancing](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/upstream/load_balancing/overview) -- load balancing algorithms and health checking
- [NGINX TCP/UDP Load Balancing](https://docs.nginx.com/nginx/admin-guide/load-balancer/tcp-udp-load-balancer/) -- L4 load balancing configuration
- [Go net package](https://pkg.go.dev/net) -- TCP listener, dialer, and connection APIs

## What's Next

Continue to [22 - Building a Port Scanner](../22-building-a-port-scanner/22-building-a-port-scanner.md) to build a concurrent port scanner with service detection.

## Summary

- L4 load balancers distribute TCP connections without understanding the application protocol
- Round-robin provides even distribution; least-connections adapts to backends with varying response times
- Active health checks proactively detect backend failures; passive checks detect failures during normal traffic
- Transparent retry on connection failure improves reliability without client awareness
- Connection tracking enables least-connections balancing and operational visibility
- Graceful shutdown with connection draining prevents in-flight request failures during deployments
