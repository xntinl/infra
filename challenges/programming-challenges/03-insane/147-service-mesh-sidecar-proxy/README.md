# 147. Service Mesh Sidecar Proxy

```yaml
difficulty: insane
languages: [go]
time_estimate: 45-65 hours
tags: [networking, service-mesh, proxy, mtls, load-balancing, circuit-breaker, rate-limiting, xds]
bloom_level: [create]
```

## Prerequisites

- Network programming: TCP sockets, raw connections, `net` and `net/http` packages, iptables concepts
- TLS/mTLS: certificate generation, `crypto/tls`, x509 certificate validation, mutual authentication
- Concurrency: goroutines, channels, `sync` primitives, connection lifecycle management
- HTTP/1.1 protocol: request parsing, header manipulation, chunked transfer encoding
- Systems programming: Linux networking fundamentals, transparent proxying concepts

## Learning Objectives

After completing this challenge you will be able to:

- **Create** a transparent TCP proxy that intercepts traffic via iptables redirect without application changes
- **Create** an L7 HTTP routing engine with header-based matching rules and path rewriting
- **Create** a resilient proxy with circuit breaking, retry with exponential backoff, and rate limiting
- **Create** a mutual TLS tunnel between sidecar proxies for zero-trust service communication
- **Create** a dynamic configuration system using an xDS-like streaming API for hot-reload

## The Challenge

Build a service mesh sidecar proxy from scratch. No Envoy, no Linkerd-proxy, no external libraries for core proxy logic. Your sidecar sits alongside an application process, transparently intercepts all inbound and outbound TCP traffic, and provides L7 HTTP routing, load balancing, circuit breaking, retries, rate limiting, mutual TLS, health checking, and observability. Configuration updates arrive via a streaming xDS-like control plane API without requiring proxy restarts.

This is the data plane component described in service mesh architectures, built from first principles.

## Requirements

1. **Transparent TCP proxying**: Intercept inbound and outbound traffic using `SO_ORIGINAL_DST` socket option (Linux) or equivalent. The application connects to localhost or upstream IPs as normal; the sidecar captures and routes the connection. Support both inbound (traffic arriving for the local app) and outbound (traffic the local app sends to other services) interception.

2. **L7 HTTP routing**: Parse HTTP/1.1 requests on intercepted connections. Match routes based on path prefix, exact path, header values, and header regex. Support weighted traffic splitting across multiple upstream clusters (for canary deployments). Rewrite paths and inject/remove headers per route.

3. **Load balancing**: Maintain upstream clusters (groups of endpoints). Implement round-robin and least-connections algorithms. Track active connection counts per endpoint. Remove unhealthy endpoints from rotation. Support endpoint weights.

4. **Circuit breaking**: Per-cluster circuit breaker with configurable max connections, max pending requests, and max retries. Track consecutive failures per endpoint. Open the circuit after a threshold, enter half-open state after a timeout, close on success. Reject requests immediately when circuit is open.

5. **Retry with exponential backoff**: Configurable retry policies per route: max retries, retryable status codes (502, 503, 504), retry budget (max percentage of requests that can be retries). Exponential backoff with jitter between attempts. Do not retry non-idempotent methods unless explicitly configured.

6. **Rate limiting**: Local rate limiting per route using token bucket algorithm. Configurable requests-per-second and burst size. Return HTTP 429 with `Retry-After` header when limit is exceeded.

7. **Mutual TLS**: Generate and load CA certificates, server certificates, and client certificates. All sidecar-to-sidecar communication uses mTLS. Validate peer certificates against the mesh CA. Plaintext traffic between the sidecar and its local application (localhost only). Automatic certificate rotation when new certs are provided via config.

8. **Health checking**: Active health checks to upstream endpoints (HTTP GET to configurable path). Configurable interval, timeout, and healthy/unhealthy thresholds. Mark endpoints as healthy or unhealthy and feed status into load balancer and circuit breaker.

9. **Metrics emission**: Track and expose: requests per second, latency histograms (p50, p95, p99), error rates, active connections, circuit breaker state, rate limiter rejections. Expose via a `/stats` HTTP endpoint in Prometheus exposition format.

10. **Config hot-reload via xDS-like API**: Implement a gRPC or HTTP streaming endpoint where the proxy connects to a control plane. Receive cluster, route, and listener configuration updates as streams. Apply changes without dropping existing connections. Version configs and support NACK on invalid configuration.

## Hints

1. Build the TCP proxy layer first, forwarding bytes bidirectionally between inbound and outbound connections. Layer HTTP parsing on top only for connections identified as HTTP. Non-HTTP TCP traffic should pass through unchanged.

2. For circuit breaking, model the state machine explicitly (closed, open, half-open) with atomic state transitions. The half-open state allows a single probe request through; its outcome determines whether to close or re-open the circuit.

3. For transparent proxying without actual iptables, you can simulate it: have the application configure the sidecar as an explicit HTTP proxy, or use `SO_ORIGINAL_DST` on Linux. For testing, a listener that accepts and forwards is sufficient to validate the routing logic.

## Acceptance Criteria

- [ ] TCP proxy transparently forwards connections: application connects to upstream IP, sidecar intercepts and routes to correct backend
- [ ] HTTP routing matches requests by path, headers, and regex; weighted splitting distributes traffic proportionally (within 5% tolerance over 1000 requests)
- [ ] Round-robin and least-connections load balancing distribute requests correctly across endpoints
- [ ] Circuit breaker opens after configurable consecutive failures, rejects requests when open, transitions through half-open to closed on success
- [ ] Retries fire on 502/503/504 with exponential backoff and jitter; retry budget prevents retry storms
- [ ] Rate limiter enforces requests-per-second with token bucket; returns 429 with Retry-After
- [ ] mTLS established between two sidecar instances; connections with invalid certificates are rejected
- [ ] Health checker marks endpoints unhealthy after threshold failures and removes them from load balancer pool
- [ ] Metrics endpoint exposes request count, latency percentiles, error rate, and circuit breaker state
- [ ] Configuration updates via xDS-like stream apply without dropping in-flight requests

## Resources

- [Envoy Proxy Architecture Overview](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/arch_overview) - Reference architecture for sidecar proxies
- [Matt Klein: "The Universal Data Plane API" (2017)](https://blog.envoyproxy.io/the-universal-data-plane-api-d15cec7a) - xDS API design rationale
- [Circuit Breaker pattern (Microsoft)](https://learn.microsoft.com/en-us/azure/architecture/patterns/circuit-breaker) - Circuit breaker state machine
- [Token Bucket Algorithm (Wikipedia)](https://en.wikipedia.org/wiki/Token_bucket) - Rate limiting foundation
- [RFC 8446: TLS 1.3](https://datatracker.ietf.org/doc/html/rfc8446) - TLS protocol specification
- [Kubernetes Service Mesh Interface (SMI)](https://smi-spec.io/) - Service mesh API abstractions
