# 35. Build a Load Balancer

**Difficulty**: Insane

---

## Prerequisites

- Raw TCP socket programming with `:gen_tcp`
- HTTP/1.1 request parsing (minimal subset)
- Elixir GenServer for connection tracking and backend state
- Understanding of health check patterns (active and passive)
- Consistent hashing for stateful routing
- Connection draining and graceful shutdown semantics
- Performance measurement: latency percentiles and throughput benchmarking

---

## Problem Statement

Build a load balancer that operates at both TCP Layer 4 and HTTP Layer 7, distributes traffic across a pool of backends using multiple algorithms, continuously monitors backend health, and supports graceful backend removal without dropping active connections. The system must function correctly under real network conditions where backends can be slow, fail, or recover at any time.

1. Accept incoming TCP connections and transparently proxy them to a selected backend without the client being aware
2. Parse the HTTP `Host` header to implement virtual hosting — routing different domains to different backend pools
3. Implement four load balancing algorithms, each appropriate for different traffic patterns
4. Monitor backend health actively via periodic HTTP probes and passively via response error rate tracking
5. Support sticky sessions so requests from the same client IP consistently reach the same backend
6. Remove a backend from rotation gracefully: stop sending new connections while allowing existing ones to complete
7. Report per-backend statistics continuously, including request rate, error rate, and P50/P99 latency

---

## Acceptance Criteria

- [ ] TCP proxy: accepts a TCP connection and opens a corresponding connection to the selected backend; data flows bidirectionally in both directions simultaneously; the client connection closes when the backend connection closes and vice versa; no data is lost or reordered
- [ ] HTTP proxy: parses the `Host` header and the request line from incoming HTTP/1.1 connections; uses the `Host` value to select the correct backend pool; rewrites the `Host` header before forwarding if configured; passes through all other headers unmodified; supports `keep-alive` so multiple requests share a single client connection
- [ ] Algorithms: `round_robin` cycles through backends in order; `least_connections` selects the backend with the fewest active connections tracked in ETS; `ip_hash` hashes the client IP to consistently select the same backend; `weighted_round_robin` respects a `weight` per backend (higher weight receives proportionally more requests)
- [ ] Active health checks: for each backend, a configurable probe sends `GET {health_path} HTTP/1.1` every `interval` seconds with a `timeout`; a backend is marked unhealthy after `threshold` consecutive failures and healthy after `recovery_threshold` consecutive successes; unhealthy backends are excluded from selection
- [ ] Passive health checks: the proxy tracks the error rate (5xx responses and connection errors) for each backend over a rolling 60-second window; a backend whose error rate exceeds a configurable threshold is temporarily removed and re-probed via active health check before returning to rotation
- [ ] Sticky sessions: when `sticky: :ip_hash` is configured, the same client IP always routes to the same backend as long as that backend is healthy; if the sticky backend becomes unhealthy, the client is rerouted and the new backend becomes the sticky target
- [ ] Connection draining: `PUT /backends/:id/drain` marks a backend as draining; it receives no new connections; the backend is fully removed from the pool when its active connection count reaches zero; `PUT /backends/:id/restore` returns a drained backend to active rotation

---

## What You Will Learn

- Bidirectional TCP proxying with concurrent read/write tasks per connection
- HTTP/1.1 keep-alive connection management at the proxy level
- Consistent hashing for sticky routing without a central session store
- Active vs. passive health monitoring and their complementary failure detection properties
- Connection draining and the challenge of "zero active connections" detection
- Lock-free connection counting with `:atomics` for `least_connections` routing
- High-throughput benchmarking of a TCP proxy using `wrk` or `vegeta`

---

## Hints

- Research how HAProxy implements active and passive health checks and why both are necessary
- Study bidirectional TCP relay: you need two concurrent tasks per proxied connection, one for each direction
- Investigate how `ip_hash` handles client IP extraction when a load balancer sits behind another proxy (X-Forwarded-For header)
- Think about what "zero active connections" means precisely when new connections can arrive at any moment — there is a race condition to solve
- Look into how Nginx `upstream` implements weighted round-robin with a current-weight algorithm (not random)
- Research the `wrk` and `vegeta` tools for generating realistic HTTP load during benchmarking

---

## Reference Material

- HAProxy documentation: load balancing algorithms and health checks (haproxy.org)
- NGINX upstream module documentation (nginx.org)
- "Load Balancing" — O'Reilly (samvaran.io free resource)
- Maglev consistent hashing paper — Google, NSDI 2016
- RFC 7230 — HTTP/1.1 Message Syntax (for Host header and keep-alive semantics)
- `wrk` HTTP benchmarking tool (github.com/wg/wrk)

---

## Difficulty Rating ★★★★★★

Operating correctly at both TCP and HTTP layers while maintaining accurate per-backend state under concurrent load and implementing safe connection draining requires precise coordination across all components.

---

## Estimated Time

50–80 hours
