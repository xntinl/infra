# 34. Build an API Gateway

**Difficulty**: Insane

---

## Prerequisites

- Elixir HTTP client and server (`:httpc`, `Mint`, or raw TCP)
- GenServer and ETS for service registry state
- Circuit breaker pattern theory (Hystrix, Resilience4j concepts)
- JWT structure and verification (HMAC-SHA256, RS256)
- Distributed rate limiting concepts
- Caching strategies: TTL, cache invalidation, stale-while-revalidate
- OpenTelemetry or custom tracing concepts

---

## Problem Statement

Build an API gateway that sits between clients and a collection of backend microservices. The gateway must handle service discovery, intelligent load balancing, authentication, rate limiting, circuit breaking, response caching, request/response transformation, and observability — all without the client knowing which backend instance serves any given request.

1. Maintain a live service registry where backend services register themselves and the gateway discovers their instances
2. Route incoming requests to the correct backend based on path prefix or host header, then balance across healthy instances
3. Validate authentication credentials (JWT tokens and API keys) at the gateway before forwarding to backends
4. Enforce per-key and per-IP rate limits without a central coordinator (distributed, lease-based)
5. Protect each backend with a circuit breaker that opens when error rate exceeds a threshold
6. Cache `GET` responses with TTL and conditional request support to reduce backend load
7. Transform requests and responses in transit: add/remove headers, rewrite paths, modify JSON bodies
8. Emit per-request metrics and distributed trace spans for every proxied request

---

## Acceptance Criteria

- [ ] Service discovery: backends `POST /register` with `{name, host, port, health_path, weight}`; the gateway polls each registered instance's `health_path` every 10 seconds; unhealthy instances are removed from the rotation automatically; instances deregister with `DELETE /register/:id`
- [ ] Load balancing: supports `round_robin`, `least_connections`, and `weighted_round_robin` algorithms, selectable per service; `ip_hash` is also available for sticky-session use cases; the algorithm is hot-swappable without restart
- [ ] Authentication: `Authorization: Bearer <jwt>` tokens are verified against a configured public key or shared secret; `X-API-Key: <key>` is validated against a key registry; requests without valid credentials are rejected with `401`; authentication is skippable per route (public routes)
- [ ] Rate limiting: per API key and per IP, using a sliding window algorithm; limits are configurable per service; the gateway uses a lease-based approach so individual nodes can approve requests locally without per-request coordination; rate limit headers (`X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`) are included in all responses
- [ ] Circuit breaker: each backend service has an independent circuit breaker with configurable `error_threshold` (percentage) and `open_duration`; in `open` state all requests immediately return `503`; after `open_duration` the breaker enters `half_open` and allows one probe request; success closes it, failure reopens it
- [ ] Caching: `GET` responses with `Cache-Control: max-age=N` or a configured default TTL are cached in ETS; cache key includes method, path, and query string; `If-None-Match` / `ETag` is supported; stale entries are evicted on a background schedule
- [ ] Request transformation: a per-route configuration allows: `add_header`, `remove_header`, `rewrite_path` (regex replace), `add_query_param`, and `inject_json_field` (add a field to a JSON request body); transformations apply before forwarding
- [ ] Observability: every proxied request emits a metric `{service, route, status_code, duration_ms}`; a distributed trace span is created with `trace_id` propagated via `X-Trace-Id` header; `GET /metrics` returns all metrics in Prometheus format; `GET /services` returns current registry state with health and circuit breaker status

---

## What You Will Learn

- Service registry design and health check polling
- Load balancing algorithms and connection tracking
- JWT verification (parsing, signature validation, claims checking)
- Circuit breaker state machine with half-open probe logic
- HTTP reverse proxy mechanics: connection reuse, header forwarding, body streaming
- Cache invalidation strategies and ETag-based conditional requests
- Distributed tracing context propagation (W3C Trace Context spec)

---

## Hints

- Research the W3C Trace Context specification for `traceparent` header format
- Study the Hystrix circuit breaker design document for the state machine and metrics window approach
- Investigate how `Mint` handles connection pooling for efficient reverse proxy forwarding
- Think about how `least_connections` tracking works correctly when the gateway is itself clustered
- Look into JWT verification without a library: the structure is `base64(header).base64(payload).signature`; HMAC-SHA256 or RSA verification is in the Erlang standard library
- Research how Nginx implements `proxy_cache_bypass` and `stale-while-revalidate` for inspiration

---

## Reference Material

- W3C Trace Context Specification (w3.org/TR/trace-context)
- "Hystrix: Latency and Fault Tolerance for Distributed Systems" — Netflix Tech Blog
- JWT specification (RFC 7519)
- NGINX documentation on `proxy_pass`, `proxy_cache`, and health checks
- "Building Microservices" — Sam Newman, O'Reilly (gateway chapter)

---

## Difficulty Rating ★★★★★★

Combining seven independent distributed systems concerns (discovery, load balancing, auth, rate limiting, circuit breaking, caching, observability) into a single coherent system is a production-engineering challenge.

---

## Estimated Time

70–110 hours
