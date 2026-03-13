# 7. Rate Limiting

<!--
difficulty: insane
concepts: [rate-limiting, token-bucket, sliding-window, per-client-limits, global-rate-limiting, rate-limit-headers, descriptor-matching]
tools: [go]
estimated_time: 2h
bloom_level: create
prerequisites: [42-capstone-service-mesh-data-plane/06-traffic-management, 15-sync-primitives, 14-select-and-context]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-06 (proxy and traffic management) or equivalent experience
- Completed Sections 14-15 (concurrency primitives) or equivalent experience

## Learning Objectives

- **Design** a rate limiting system supporting per-client, per-route, and global rate limits using token bucket and sliding window algorithms
- **Create** a descriptor-based rate limit engine that matches requests to rate limit rules using configurable criteria
- **Evaluate** the trade-offs between local (per-instance) and global (distributed) rate limiting in a multi-proxy deployment

## The Challenge

Rate limiting protects upstream services from being overwhelmed by traffic surges, whether from misbehaving clients, traffic spikes, or denial-of-service attacks. In a service mesh, the data plane enforces rate limits transparently, applying different limits based on client identity, route, headers, or any combination of request attributes.

You will build a rate limiting engine that supports two algorithms: token bucket (for smoothing burst traffic) and sliding window (for strict per-second limits). The engine uses a descriptor-based matching system where each incoming request is classified into rate limit descriptors (e.g., client IP, authenticated identity, route name), and these descriptors are matched against rate limit rules. Multiple descriptors can be combined (e.g., limit client X to 100 req/s on route Y). The engine must handle the case where a single request matches multiple rate limit rules, applying the most restrictive limit.

The challenge extends to implementation details: rate limiters must be memory-efficient (you cannot create a new token bucket for every unique IP address seen), must clean up state for inactive clients, and must return standard rate limit response headers (`X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`) so clients can self-regulate.

## Requirements

1. Implement a `TokenBucket` rate limiter with configurable rate (tokens per second) and burst size (maximum tokens)
2. Implement a `SlidingWindow` rate limiter that counts requests in a sliding time window with configurable window duration and max requests
3. Define rate limit descriptors as key-value pairs extracted from requests (e.g., `{"client_ip": "1.2.3.4", "route": "/api/v1"}`)
4. Define rate limit rules that match on descriptor combinations and specify the algorithm, rate, and burst/window configuration
5. Implement a rule matching engine that finds all applicable rules for a request's descriptors and applies the most restrictive limit
6. Return standard rate limit headers in the response: `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset` (Unix timestamp)
7. Return 429 Too Many Requests when any applicable rate limit is exceeded, with a `Retry-After` header
8. Implement automatic cleanup of rate limiter state for clients that have been inactive beyond a configurable TTL
9. Support extracting descriptors from: client IP, request path, request headers, and mTLS peer identity (from context)
10. Expose rate limiting metrics: requests allowed, requests rate-limited, and active rate limiter instances per rule

## Hints

- For the token bucket, use `time.Now()` to compute elapsed time since the last request and add `elapsed * rate` tokens, capped at burst size -- this avoids needing a background goroutine to refill tokens
- For the sliding window, use a circular buffer of fixed-size time slots (e.g., 10 slots for a 10-second window) and count across all non-expired slots
- Use a `sync.Map` keyed by descriptor hash to store per-client rate limiters, and run a periodic cleanup goroutine that evicts entries older than the TTL
- For descriptor matching, sort descriptors and concatenate them into a canonical string key for rule lookup
- Compute `X-RateLimit-Reset` as the time when the next token will be available (token bucket) or when the oldest slot expires (sliding window)
- Consider using `sync.Pool` for rate limiter instances if allocation pressure becomes significant

## Success Criteria

1. Token bucket rate limiter correctly allows burst traffic up to the configured burst size and then limits to the configured rate
2. Sliding window rate limiter correctly counts requests within the configured window and rejects excess requests
3. Descriptor matching correctly identifies applicable rules for multi-dimensional descriptors
4. The most restrictive matching rule is applied when multiple rules match a single request
5. Rate limit response headers are correctly set, including `Retry-After` on 429 responses
6. Inactive client state is cleaned up after the configured TTL
7. The rate limiter handles at least 10000 unique clients without excessive memory growth
8. All tests pass with the `-race` flag enabled

## Research Resources

- [Token bucket algorithm](https://en.wikipedia.org/wiki/Token_bucket) -- foundational algorithm for rate limiting with burst support
- [Envoy rate limiting](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/rate_limit_filter) -- reference design for descriptor-based rate limiting
- [IETF RateLimit header fields](https://datatracker.ietf.org/doc/draft-ietf-httpapi-ratelimit-headers/) -- standard rate limit response headers
- [Sliding window rate limiting (Cloudflare)](https://blog.cloudflare.com/counting-things-a-lot-of-different-things/) -- practical sliding window implementation
- [Go sync.Map](https://pkg.go.dev/sync#Map) -- concurrent map for per-client rate limiter storage

## What's Next

Continue to [Observability](../08-observability/08-observability.md) where you will add metrics collection and histograms to provide full visibility into proxy behavior.

## Summary

- Token bucket rate limiting allows burst traffic while enforcing a sustained rate limit
- Sliding window rate limiting provides strict per-window request counting without burst allowance
- Descriptor-based matching enables flexible multi-dimensional rate limit rules
- Standard rate limit headers enable clients to self-regulate and avoid unnecessary requests
- Automatic cleanup of inactive client state prevents unbounded memory growth
- The most restrictive matching rule prevents any single dimension from being bypassed
