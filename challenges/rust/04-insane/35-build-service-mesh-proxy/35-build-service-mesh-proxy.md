# 35. Build a Service Mesh Proxy

**Difficulty**: Insane

## The Challenge

A service mesh proxy is the invisible workhorse of modern microservice architectures, intercepting every byte of network traffic between services to provide load balancing, observability, security, and resilience — all without requiring any changes to application code. Your task is to build an L4/L7 service mesh sidecar proxy in Rust that handles TCP connection management, HTTP/1.1 and HTTP/2 proxying, intelligent load balancing, circuit breaking, retry policies, mutual TLS, and dynamic configuration via an xDS-compatible API. This is a systems programming challenge of the highest order because a proxy sits on the critical path of every request, meaning performance is existential: every microsecond of added latency and every byte of added memory is multiplied by every request in the entire mesh.

At layer 4, your proxy must accept TCP connections, perform transparent proxying (forwarding bytes without interpreting them), and support connection pooling to upstream services. At layer 7, it must parse HTTP/1.1 and HTTP/2 frames, make routing decisions based on headers (host, path, method), and apply per-route policies. The load balancer must distribute requests across a set of healthy upstream endpoints using configurable algorithms: round-robin for simplicity, least-connections for fairness under heterogeneous latency, and consistent hashing (with bounded loads) for cache-friendly distribution. Circuit breaking protects upstream services from cascading failures by tracking error rates and temporarily stopping traffic to unhealthy endpoints. Retry budgets prevent retry storms by limiting the fraction of requests that are retries at any given time. The proxy exposes metrics (request counts, latencies, error rates) via a Prometheus-compatible endpoint and emits structured access logs for every proxied request.

The most challenging aspect is the mutual TLS (mTLS) implementation using rustls, where the proxy must terminate incoming TLS connections (presenting its own certificate), verify the client's certificate against a trusted CA, and establish outgoing TLS connections to upstream services (presenting its certificate as a client). Certificates must be rotatable without connection disruption — a requirement that demands careful handling of TLS session caching and connection draining. On top of all this, the proxy's routing tables, upstream endpoints, and TLS certificates are not statically configured but dynamically pushed from a control plane via the xDS protocol (specifically, Listener Discovery Service, Cluster Discovery Service, Route Discovery Service, and Endpoint Discovery Service). This means your proxy must handle runtime configuration changes while maintaining in-flight requests on existing connections — a concurrency challenge that will exercise every aspect of Rust's async programming model.

## Acceptance Criteria

### TCP Connection Handling (L4)

- [ ] Implement a **TCP listener** that accepts incoming connections on a configurable address and port
  - Use `tokio::net::TcpListener` with `SO_REUSEADDR` and `SO_REUSEPORT` (on Linux)
  - Configurable connection backlog size
  - Track active connections and enforce a maximum connection limit
  - Graceful rejection when at capacity (send TCP RST or HTTP 503 before closing)

- [ ] Implement **transparent TCP proxying** (L4 mode)
  - For non-HTTP protocols, forward bytes bidirectionally between client and upstream
  - Use `tokio::io::copy_bidirectional` or a manual splice loop
  - Support half-close: when the client closes their write side, forward the shutdown to the upstream (and vice versa)
  - Configurable idle timeout: close the connection if no data flows in either direction for N seconds

- [ ] Implement **upstream connection pooling**
  - Maintain a pool of pre-established TCP connections to each upstream endpoint
  - Pool parameters: min idle connections, max connections per endpoint, connection lifetime, idle timeout
  - Connections are health-checked before being handed out (detect half-closed connections)
  - When a pooled connection fails mid-request, transparently retry on a new connection (for idempotent requests)
  - Connection pool is per-upstream-endpoint, not per-upstream-cluster

- [ ] Implement **connection draining** for graceful shutdown and config changes
  - On shutdown signal, stop accepting new connections but allow in-flight requests to complete
  - Configurable drain timeout: force-close connections that haven't completed after N seconds
  - When an upstream endpoint is removed from the config, drain existing connections to it

### HTTP/1.1 Proxying (L7)

- [ ] Parse HTTP/1.1 requests and responses
  - Use `httparse` for zero-copy header parsing
  - Support chunked transfer encoding (reading and forwarding)
  - Support `Content-Length`-based body framing
  - Handle `Connection: keep-alive` and `Connection: close` correctly
  - Support HTTP pipelining (multiple requests on the same connection without waiting for responses)
  - Enforce maximum header size (default 8 KB) and maximum body size (configurable per route)

- [ ] Implement **header manipulation**
  - Add `X-Forwarded-For`, `X-Forwarded-Proto`, `X-Request-Id` headers on incoming requests
  - Strip hop-by-hop headers (`Connection`, `Keep-Alive`, `Transfer-Encoding`, `Proxy-*`, etc.) before forwarding
  - Support configurable header additions and removals per route

- [ ] Implement **request routing** based on HTTP properties
  - Route by `Host` header (virtual hosting)
  - Route by path prefix (e.g., `/api/v1/` -> cluster A, `/api/v2/` -> cluster B)
  - Route by exact path match
  - Route by header presence or value (e.g., `x-canary: true` -> canary cluster)
  - Route by HTTP method
  - Routes are evaluated in priority order; first match wins
  - Default route for unmatched requests (configurable: return 404 or forward to a default cluster)

- [ ] Implement **request and response timeouts**
  - Per-route configurable request timeout (total time from request start to response completion)
  - Connection timeout: maximum time to establish an upstream TCP connection
  - Idle timeout: maximum time between bytes in a streaming response
  - On timeout, return HTTP 504 Gateway Timeout to the client

### HTTP/2 Proxying (L7)

- [ ] Implement HTTP/2 connection handling using the `h2` crate
  - Accept HTTP/2 connections via TLS ALPN negotiation ("h2") and HTTP/2 prior knowledge ("PRI *")
  - Support HTTP/2 multiplexing: multiple concurrent streams on a single connection
  - Forward HTTP/2 requests to upstream endpoints as HTTP/2 (h2-to-h2) or HTTP/1.1 (h2-to-h1, with protocol translation)
  - Respect HTTP/2 flow control: propagate backpressure from upstream to downstream

- [ ] Implement **HTTP/2 connection management**
  - Maintain a pool of HTTP/2 connections to upstream endpoints
  - Multiplex multiple requests on a single upstream HTTP/2 connection (up to `MAX_CONCURRENT_STREAMS`)
  - Handle GOAWAY frames: stop sending new requests on that connection, let in-flight requests complete
  - Implement HTTP/2 PING frames for connection health checking

- [ ] Handle **protocol negotiation and translation**
  - Accept HTTP/1.1 from clients, forward as HTTP/2 to upstream (if upstream supports it)
  - Accept HTTP/2 from clients, forward as HTTP/1.1 to upstream (if upstream does not support HTTP/2)
  - Preserve request semantics across protocol boundaries (headers, body, trailers)
  - Trailers in HTTP/2 -> chunked transfer encoding with trailers in HTTP/1.1

### Load Balancing

- [ ] Implement **round-robin** load balancing
  - Rotate through healthy upstream endpoints in order
  - Skip unhealthy endpoints (determined by health checking)
  - Thread-safe: use an atomic counter, not a mutex

- [ ] Implement **least-connections** load balancing
  - Route to the endpoint with the fewest active connections
  - Track active connection count per endpoint atomically
  - Break ties using round-robin or random selection

- [ ] Implement **consistent hashing** with bounded loads
  - Hash a configurable request property (header value, cookie, source IP) to determine the endpoint
  - Use a hash ring with configurable number of virtual nodes per endpoint
  - Implement the "bounded loads" extension: if the selected endpoint is overloaded (>= 125% of average load), choose the next endpoint on the ring
  - When endpoints are added or removed, only keys mapping to changed ring segments are remapped

- [ ] Implement **weighted round-robin**
  - Each endpoint has a configurable weight (integer, default 1)
  - An endpoint with weight 3 receives 3x the traffic of an endpoint with weight 1
  - Implement smooth weighting (not just repeating endpoints N times) to distribute requests evenly

- [ ] Implement **health checking**
  - Active health checks: periodically send a probe (TCP connect, HTTP GET to a configurable path) to each endpoint
  - Passive health checks: track request success/failure and mark endpoints unhealthy after N consecutive failures
  - An unhealthy endpoint is excluded from load balancing
  - An unhealthy endpoint is re-included after N consecutive successful active health checks
  - Configurable parameters: check interval, healthy threshold, unhealthy threshold, timeout

### Circuit Breaking

- [ ] Implement **connection-level circuit breaking**
  - Maximum number of active connections to a single upstream cluster
  - When the limit is reached, new requests receive HTTP 503 immediately (no queuing)
  - Track and expose overflow counts in metrics

- [ ] Implement **request-level circuit breaking**
  - Maximum number of pending (in-flight) requests to a single upstream cluster
  - Maximum number of requests queued waiting for a connection
  - When either limit is reached, new requests receive HTTP 503

- [ ] Implement **outlier detection** (ejection)
  - Track consecutive 5xx errors from each endpoint
  - After N consecutive errors, eject the endpoint for a configurable duration
  - Ejected endpoints are excluded from load balancing (similar to unhealthy)
  - The ejection duration increases exponentially on repeated ejections (up to a maximum)
  - At most a configurable percentage of endpoints can be ejected simultaneously (to prevent total cluster ejection)

### Retry Policy

- [ ] Implement configurable **retry policies** per route
  - Maximum number of retries (default: 0, meaning no retries)
  - Retryable status codes (e.g., 502, 503, 504)
  - Retryable conditions: connection failure, request timeout, reset before headers
  - Retry backoff: fixed delay, exponential backoff with jitter
  - Non-idempotent methods (POST, PATCH) are NOT retried by default (configurable override)

- [ ] Implement a **retry budget** to prevent retry storms
  - Track the ratio of retry requests to original requests over a sliding time window
  - If the retry ratio exceeds a threshold (e.g., 20%), stop retrying and fail fast
  - The budget is per-upstream-cluster, not per-route
  - Expose retry budget utilization in metrics

- [ ] Implement **hedged requests** (optional but encouraged)
  - After a configurable delay, send a second copy of the request to a different endpoint
  - Return the first successful response and cancel the other
  - Hedging counts against the retry budget

### Mutual TLS (mTLS)

- [ ] Implement **TLS termination** for incoming connections using `rustls`
  - Load the proxy's server certificate and private key from PEM files
  - Support ECDSA and RSA certificates
  - Configure minimum TLS version (1.2 or 1.3)
  - Expose the negotiated ALPN protocol (for HTTP/2 detection)

- [ ] Implement **client certificate verification** (mutual TLS)
  - Require client certificates on configurable listeners
  - Verify client certificates against a trusted CA bundle
  - Extract the client identity (Subject Alternative Name) from the certificate
  - Make the client identity available to routing rules (e.g., route based on calling service)
  - Reject connections with invalid, expired, or untrusted certificates with appropriate TLS alerts

- [ ] Implement **TLS origination** for outgoing connections to upstream endpoints
  - Establish TLS connections to upstream endpoints, presenting the proxy's client certificate
  - Verify the upstream's server certificate against a trusted CA bundle
  - Support SNI (Server Name Indication) for upstream connections
  - Configure per-cluster whether upstream connections use TLS, plaintext, or auto-detect

- [ ] Implement **certificate rotation** without downtime
  - Watch certificate files for changes (using `notify` or periodic polling)
  - When certificates change, create a new TLS configuration
  - New connections use the new certificates; existing connections continue with old certificates
  - Old connections are drained gradually (configurable drain period)
  - No connections are dropped or reset during rotation

### xDS Configuration API

- [ ] Implement a **gRPC client** that connects to an xDS control plane (using tonic)
  - Subscribe to Listener Discovery Service (LDS) — configures listeners (address, port, TLS settings)
  - Subscribe to Route Discovery Service (RDS) — configures HTTP routes (host, path, cluster mapping)
  - Subscribe to Cluster Discovery Service (CDS) — configures upstream clusters (name, lb policy, circuit breaker settings)
  - Subscribe to Endpoint Discovery Service (EDS) — configures upstream endpoints (addresses, weights, health status)

- [ ] Implement **incremental xDS** (delta protocol)
  - Only receive changes (additions, modifications, removals) instead of full state on every update
  - Maintain a local cache of the current configuration
  - Apply deltas atomically: if a route references a cluster that doesn't exist yet, queue the route until the cluster arrives

- [ ] Implement **configuration hot-reloading**
  - When a new LDS/RDS/CDS/EDS response arrives, update the routing tables in-place
  - Use `Arc<RwLock<Config>>` or `arc-swap` for lock-free reader access
  - In-flight requests continue using the configuration snapshot they started with
  - New requests use the updated configuration
  - Log all configuration changes at INFO level

- [ ] Support **static configuration** as a fallback
  - If no xDS server is configured, load configuration from a YAML/TOML file
  - The file format mirrors the xDS resource structure
  - Support file-watching for hot-reload of the static configuration

### Observability

- [ ] Expose **Prometheus-compatible metrics** on a configurable admin endpoint (e.g., `:9901/metrics`)
  - Request counters: total requests, by upstream cluster, by response status code, by route
  - Latency histograms: request duration (p50, p90, p99) by upstream cluster
  - Connection gauges: active downstream connections, active upstream connections, pooled connections
  - Circuit breaker: open/closed state per cluster, overflow count
  - Retry metrics: total retries, retries by status code, retry budget utilization
  - TLS metrics: handshake count, handshake errors, certificate expiration time

- [ ] Emit **structured access logs** for every proxied request
  - Fields: timestamp, client IP, request method, path, host, upstream cluster, upstream endpoint, response status, request duration, bytes sent/received, retry count, TLS version, client identity
  - Format: JSON (configurable to text)
  - Output: stdout, file (with rotation), or both
  - Configurable log level filtering (e.g., only log 5xx responses)

- [ ] Implement a **health check endpoint** on the admin port
  - `/ready` returns 200 when the proxy is accepting traffic (has valid configuration, at least one healthy upstream)
  - `/live` returns 200 when the proxy process is running (basic liveness)
  - `/config_dump` returns the current active configuration as JSON (for debugging)

### Testing

- [ ] Unit tests for each load balancing algorithm
  - Round-robin: verify even distribution over N requests
  - Least-connections: verify requests go to the least-loaded endpoint
  - Consistent hashing: verify the same key always maps to the same endpoint; verify minimal disruption when an endpoint is added/removed
  - Weighted round-robin: verify distribution matches weights

- [ ] Unit tests for circuit breaking
  - Verify that the circuit opens after the configured error threshold
  - Verify that requests are rejected (503) when the circuit is open
  - Verify that the circuit closes after the recovery timeout
  - Verify outlier detection ejects and re-admits endpoints correctly

- [ ] Integration tests for HTTP proxying
  - Start a mock upstream HTTP server and the proxy
  - Send HTTP/1.1 requests through the proxy, verify correct forwarding and response
  - Send HTTP/2 requests through the proxy, verify correct multiplexing
  - Verify header manipulation (X-Forwarded-For, hop-by-hop stripping)
  - Verify routing: requests to different hosts/paths reach different upstreams
  - Verify timeout behavior: slow upstream triggers 504

- [ ] Integration tests for mTLS
  - Generate test CA, server cert, and client cert using `rcgen`
  - Verify that the proxy accepts connections with valid client certs
  - Verify that the proxy rejects connections with expired/untrusted client certs
  - Verify that the proxy establishes TLS to upstream and verifies the upstream cert
  - Verify certificate rotation: change certs, verify new connections use new certs

- [ ] Integration tests for retries
  - Mock upstream that fails the first N requests then succeeds
  - Verify the proxy retries and returns the successful response
  - Verify the proxy respects the maximum retry count
  - Verify the retry budget limits retries under high error rates

- [ ] End-to-end test with dynamic configuration
  - Start a mock xDS server that serves LDS, RDS, CDS, EDS
  - Start the proxy, verify it picks up the configuration
  - Change the xDS server's configuration, verify the proxy updates its routing
  - Add a new upstream endpoint, verify traffic is routed to it
  - Remove an upstream cluster, verify the proxy stops routing to it

### Performance

- [ ] Throughput benchmarks
  - L4 proxying (TCP passthrough): > 100K requests/sec for small payloads (< 1 KB)
  - L7 HTTP/1.1 proxying: > 50K requests/sec for small payloads
  - L7 HTTP/2 proxying (multiplexed): > 30K requests/sec across 100 concurrent streams

- [ ] Latency benchmarks
  - Added latency per request (proxy overhead): p50 < 0.5ms, p99 < 2ms for L7 proxying
  - mTLS handshake time: < 5ms (with session resumption), < 20ms (full handshake)

- [ ] Memory efficiency
  - Idle proxy with 1000 configured endpoints: < 50 MB RSS
  - Per-connection overhead: < 10 KB for L4, < 20 KB for L7 (excluding request buffers)

- [ ] Connection scalability
  - Handle 10K concurrent downstream connections without degradation
  - Handle 1K upstream connection pool entries without excessive memory

### Code Organization

- [ ] Cargo workspace with crates:
  - `proxy` — the main binary crate
  - `transport` — TCP and TLS connection handling
  - `http-codec` — HTTP/1.1 and HTTP/2 parsing and serialization
  - `lb` — load balancing algorithms
  - `resilience` — circuit breaking, retries, retry budgets
  - `xds` — xDS client and configuration management
  - `metrics` — Prometheus metrics collection and exposition
  - `config` — static configuration loading and validation

- [ ] Use trait-based abstractions:
  - `trait LoadBalancer: Send + Sync`: `fn pick(&self, context: &RequestContext) -> Option<Endpoint>`
  - `trait HealthChecker: Send + Sync`: `fn check(&self, endpoint: &Endpoint) -> HealthStatus`
  - `trait ConfigSource: Send + Sync`: `fn subscribe() -> watch::Receiver<Config>`

- [ ] Configuration via YAML for static mode and protobuf for xDS mode
  - Document the YAML schema with examples for common configurations

## Starting Points

- **linkerd2-proxy**: The Rust-based proxy used by the Linkerd service mesh (github.com/linkerd/linkerd2-proxy). This is the closest existing Rust implementation to what you are building. Study its architecture, particularly how it handles service discovery, load balancing, and TLS.
- **Envoy Architecture**: The Envoy proxy documentation (envoyproxy.io) explains the xDS APIs, load balancing algorithms, circuit breaking, outlier detection, and retry policies in extraordinary detail. Even though Envoy is C++, its architecture docs are the best specification for service mesh proxy behavior.
- **xDS Protocol Documentation**: The envoyproxy/data-plane-api repository (github.com) contains the protobuf definitions and the xDS protocol specification (ADS, SotW, Delta).
- **rustls**: The pure-Rust TLS library (github.com/rustls/rustls). Study its API for server and client configuration, certificate verification, and ALPN negotiation.
- **h2**: The Rust HTTP/2 implementation (github.com/hyperium/h2). Study how it handles frames, streams, flow control, and GOAWAY.
- **hyper**: The Rust HTTP library built on h2 and tokio. You may choose to use hyper as a building block or implement HTTP handling yourself for more control.
- **tokio**: Study tokio's TCP listener, TLS integration (via tokio-rustls), and channel primitives for inter-task communication.
- **Load Balancing in the Wild** (Daniel Fireman, 2022): A survey of load balancing algorithms with practical considerations for proxies.

## Hints

1. **Start with L4 TCP proxying.** Accept a connection, look up the upstream endpoint from a static config, connect to it, and splice bytes in both directions. This gives you the foundation: listener, connection lifecycle, upstream connection management. Everything else builds on top.

2. **Use `tokio::io::copy_bidirectional` for L4 proxying, but be aware of its limitations.** It handles the bidirectional byte copying and half-close correctly, but it doesn't give you access to the bytes flowing through (for metrics). For a production proxy, you would write a custom copy loop that also counts bytes.

3. **For HTTP/1.1 parsing, use `httparse` for the headers and manual parsing for the body framing.** `httparse` is zero-copy and extremely fast, but it only parses headers. You need to handle `Content-Length` vs. `Transfer-Encoding: chunked` vs. implicit close (HTTP/1.0) body framing yourself. This is surprisingly tricky — get it right before moving on to HTTP/2.

4. **For HTTP/2, use the `h2` crate rather than implementing the protocol yourself.** HTTP/2 framing, HPACK header compression, and flow control are immensely complex, and `h2` is well-tested. Focus your effort on the proxy logic (routing, load balancing, resilience) rather than protocol mechanics.

5. **Connection pooling is harder than it looks.** You need to handle: connections that are idle-closed by the upstream, connections that hit their maximum lifetime, connections that fail mid-health-check, and the thundering herd problem (many requests arriving simultaneously when the pool is empty). Use a bounded MPSC channel as the pool: each "idle" connection is a message in the channel.

6. **For consistent hashing with bounded loads, implement the algorithm from the "Consistent Hashing with Bounded Loads" paper (Mirrokni et al., 2018).** The key insight is: if the selected node has load >= `ceil(average_load * (1 + epsilon))`, skip it and try the next node on the ring. This provides O(1) balancing guarantee with minimal remapping.

7. **Circuit breaker state transitions should be: Closed -> Open -> Half-Open -> Closed (or back to Open).** In the Half-Open state, allow a limited number of probe requests through. If they succeed, transition to Closed. If they fail, transition back to Open with an increased cooldown. Use an `AtomicU8` to store the state for lock-free reading on the hot path.

8. **For mTLS with rustls, create the `ServerConfig` and `ClientConfig` once and wrap them in `Arc`.** When certificates rotate, create new configs and swap them using `arc_swap::ArcSwap`. This is wait-free for readers (every new TLS handshake reads the current config) and lock-free for writers (the rotation task swaps the config atomically).

9. **For xDS, start with the State-of-the-World (SotW) variant, not Delta.** SotW is simpler: on each response, the server sends the complete set of resources, and the client replaces its local state entirely. Delta xDS is an optimization for large configurations but adds significant complexity. Get SotW working first, then add Delta support.

10. **Use tower's `Service` trait as the abstraction for your request pipeline.** Each layer (routing, load balancing, circuit breaking, retries, timeout) can be a tower middleware that wraps the next layer. This composable architecture is exactly how linkerd2-proxy is built, and it makes testing each layer in isolation trivial.
