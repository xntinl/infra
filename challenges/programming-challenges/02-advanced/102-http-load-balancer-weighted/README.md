# 102. HTTP Load Balancer with Weighted Routing

<!--
difficulty: advanced
category: web-servers-and-http
languages: [go]
concepts: [reverse-proxy, weighted-round-robin, least-connections, health-checking, sticky-sessions, connection-draining, request-retry]
estimated_time: 8-12 hours
bloom_level: evaluate
prerequisites: [go-concurrency, tcp-networking, http-protocol, sync-primitives, context-package, connection-pooling]
-->

## Languages

- Go (1.22+)

## Prerequisites

- Advanced goroutine patterns (worker pools, background monitors, graceful lifecycle management)
- TCP connection mechanics: dialing, proxying bytes bidirectionally, timeouts
- HTTP/1.1 protocol: request/response format, headers, chunked encoding, `Connection: close`
- `sync.Mutex`, `sync.RWMutex`, and atomic operations for shared state
- `context.Context` for cancellation and deadline propagation
- Understanding of reverse proxy mechanics: receiving a client request, forwarding to a backend, relaying the response

## Learning Objectives

- **Design** a layer 7 HTTP load balancer that distributes traffic across weighted backends
- **Implement** multiple load balancing algorithms (weighted round-robin, least-connections) with runtime switching
- **Evaluate** active health checking strategies and their impact on backend availability
- **Implement** sticky sessions via cookie-based affinity and analyze their trade-offs with load distribution
- **Analyze** graceful backend draining mechanics and how to remove a backend without dropping in-flight requests
- **Justify** retry and request buffering decisions when backends fail mid-request

## The Challenge

A load balancer sits between clients and backend servers, distributing requests to prevent any single server from being overwhelmed. In a production environment, backends have different capacities (a 32-core machine should receive more traffic than an 8-core machine), backends fail and recover dynamically, certain clients need session affinity, and removing a backend for maintenance must not drop active requests.

Your task is to build a Layer 7 (HTTP-aware) load balancer from raw TCP connections. The balancer parses incoming HTTP requests, selects a backend using configurable algorithms, proxies the request and response, and handles the full lifecycle: health checking, failure detection, retry on failure, sticky sessions, and graceful draining.

This is not a simple TCP proxy. The balancer understands HTTP: it can inspect headers, add forwarding headers (`X-Forwarded-For`, `X-Request-ID`), rewrite the `Host` header, and make routing decisions based on request content.

This is an advanced challenge. Hints describe strategies and trade-offs, not implementations. You must design the data structures, choose the synchronization primitives, and handle the concurrency edge cases yourself.

## Requirements

1. Accept TCP connections from clients and parse HTTP/1.1 requests from raw bytes (method, path, headers, body based on Content-Length or chunked transfer-encoding)
2. Maintain a configurable pool of backend servers, each with a weight (integer, higher = more traffic), and a health status (healthy, unhealthy, draining)
3. Implement **weighted round-robin**: backends receive requests proportional to their weight. A backend with weight 3 receives three times more requests than one with weight 1
4. Implement **least-connections**: route each request to the healthy backend with the fewest active connections, with weight as a tiebreaker
5. Allow runtime switching between algorithms without restarting the balancer
6. **Active health checking**: a background goroutine periodically sends HTTP requests to each backend's health endpoint. Mark backends as unhealthy after N consecutive failures. Mark them healthy again after M consecutive successes
7. **Sticky sessions**: when enabled, set a cookie (`__lb_backend=<backend_id>`) on the first response. Subsequent requests from the same client are routed to the same backend. If the sticky backend is unhealthy, fall back to the normal algorithm and set a new cookie
8. **Request retry**: if a backend fails to accept the connection or returns a 502/503, retry the request on the next backend (up to a configurable max retries). This requires buffering the request body before forwarding
9. **Graceful draining**: a `Drain(backendID)` API marks a backend as draining. It stops receiving new requests but existing in-flight requests complete. Once all in-flight requests finish, the backend is removed
10. Add proxy headers to forwarded requests: `X-Forwarded-For` (client IP), `X-Forwarded-Proto`, `Via`, and optionally rewrite the `Host` header
11. Return proper error responses: 502 Bad Gateway when all backends fail, 503 Service Unavailable when all backends are unhealthy

## Hints

<details>
<summary>Hint 1: Weighted round-robin with a smooth distribution</summary>

Naive WRR repeats backend A three times, then B twice, then C once. This creates bursts. Smooth WRR (Nginx's algorithm) tracks each backend's `currentWeight`, adds its `effectiveWeight` each round, selects the backend with the highest `currentWeight`, then subtracts the total weight from the selected backend's `currentWeight`. This distributes requests evenly: A, B, A, C, A, B instead of A, A, A, B, B, C.
</details>

<details>
<summary>Hint 2: Least-connections with atomic tracking</summary>

Each backend tracks its active connection count with `atomic.Int64`. When routing, select the backend with `active / weight` as the score (lower is better). Increment on forward, decrement when the response completes. The atomic counter avoids locking on the hot path. Use a read lock only when iterating over backends to avoid seeing a partially-updated list during reconfiguration.
</details>

<details>
<summary>Hint 3: Health checker state machine</summary>

Each backend has a health state with two counters: consecutive successes and consecutive failures. On a health check success, increment successes and reset failures (and vice versa). Transition from unhealthy to healthy requires M consecutive successes, preventing a single lucky response from reactivating a flapping backend. Run health checks in a dedicated goroutine with a `time.Ticker`, not in the request path.
</details>

<details>
<summary>Hint 4: Request buffering for retry</summary>

To retry a failed request on another backend, you must have the complete request (including body) available for re-sending. Read the request body into a buffer before forwarding. Set a maximum buffer size to prevent memory exhaustion from large uploads. If the body exceeds the limit, disable retry for that request and stream it directly.
</details>

## Acceptance Criteria

- [ ] Balancer accepts HTTP/1.1 connections and correctly proxies requests/responses to backends
- [ ] Weighted round-robin distributes requests proportional to backend weights (verified statistically over 1000 requests)
- [ ] Least-connections routes to the backend with the fewest active connections
- [ ] Algorithm switching works at runtime without dropping in-flight requests
- [ ] Health checker marks backends unhealthy after N failures and healthy after M successes
- [ ] Unhealthy backends receive zero new requests
- [ ] Sticky sessions route the same client to the same backend via cookies
- [ ] Sticky session falls back gracefully when the sticky backend is unavailable
- [ ] Failed requests retry on the next backend up to the configured limit
- [ ] `Drain()` stops new requests to a backend while in-flight requests complete
- [ ] Proxy headers (`X-Forwarded-For`, `Via`) are added to forwarded requests
- [ ] Returns 502 when all backends fail for a specific request, 503 when none are healthy
- [ ] No goroutine leaks after shutdown (all health checkers, all connection handlers exit)
- [ ] Handles 500 concurrent connections without deadlocks or data races

## Research Resources

- [Nginx: Upstream module -- weighted round-robin](https://nginx.org/en/docs/http/ngx_http_upstream_module.html) -- Nginx's smooth WRR algorithm with `currentWeight` tracking
- [HAProxy Configuration Manual: balance algorithms](https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/) -- comprehensive overview of load balancing strategies
- [Envoy Proxy: Load Balancing](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/upstream/load_balancing/overview) -- modern L7 proxy architecture
- [RFC 7239: Forwarded HTTP Extension](https://tools.ietf.org/html/rfc7239) -- standardized proxy header format
- [Envoy: Health Checking](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/upstream/health_checking) -- active and passive health check strategies
- [Google SRE Book: Load Balancing in the Datacenter](https://sre.google/sre-book/load-balancing-datacenter/) -- weighted and subsetting strategies at scale
- [Cloudflare Blog: How we built Pingora](https://blog.cloudflare.com/how-we-built-pingora-the-proxy-that-connects-cloudflare-to-the-internet/) -- modern proxy architecture decisions
