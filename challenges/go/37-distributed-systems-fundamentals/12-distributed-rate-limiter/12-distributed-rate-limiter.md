# 12. Distributed Rate Limiter

<!--
difficulty: insane
concepts: [rate-limiting, token-bucket, sliding-window, distributed-counter, gossip-aggregation, local-rate-limiting, global-rate-limiting]
tools: [go]
estimated_time: 90m
bloom_level: create
prerequisites: [gossip-protocol, distributed-locking, http-programming]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of rate limiting algorithms (token bucket, sliding window)
- Familiarity with gossip protocols and distributed coordination

## Learning Objectives

- **Create** a distributed rate limiter that enforces global rate limits across multiple nodes
- **Analyze** the accuracy vs performance tradeoffs of different synchronization strategies
- **Evaluate** local-only, centralized, and gossip-based rate limiting approaches

## The Challenge

A single-node rate limiter is straightforward: track requests per client and reject those exceeding the limit. But in a distributed system with multiple API gateway nodes, each node sees only a fraction of the traffic. Without coordination, a client can exceed the global limit by spreading requests across nodes.

Build a distributed rate limiter that enforces global rate limits while keeping latency low. Compare three approaches: purely local (each node enforces `limit/N`), centralized (a single coordinator tracks counts), and gossip-based (nodes periodically exchange counts and converge on the global total).

## Requirements

1. Implement a `TokenBucket` local rate limiter with configurable rate and burst size
2. Implement a `SlidingWindowCounter` local rate limiter for more accurate windowed limiting
3. Implement the local-division strategy: each of N nodes enforces `global_limit/N` independently
4. Implement a centralized rate limiter: all nodes check a shared counter (simulated with a mutex-protected struct or channel-based coordinator)
5. Implement a gossip-based rate limiter: each node tracks local counts and periodically exchanges counts with peers to estimate the global total
6. Implement per-client rate limiting: track limits independently per client ID (API key, IP address, etc.)
7. Benchmark all three strategies: measure accuracy (how close to the global limit before rejection), latency per request decision, and behavior under bursty traffic
8. Handle edge cases: node joins/leaves (limit redistribution), clock skew in window boundaries, counter overflow

## Hints

- Token bucket: tokens are added at a fixed rate. Each request consumes a token. If no tokens are available, the request is rejected. Burst size = bucket capacity.
- Sliding window counter: divide time into fixed windows. Track counts per window. The current count is a weighted sum of the current and previous windows.
- Local division (`limit/N`) is simple but inflexible: if traffic is unevenly distributed, some nodes will reject while others are idle.
- Gossip-based: each node maintains a local counter and a "known" counter for each peer. Periodically exchange counters. Estimate global count as `sum(local + known_peers)`. This has a convergence delay.
- For the centralized approach, consider batching: nodes pre-fetch a batch of tokens (e.g., 100) from the coordinator and use them locally, reducing round trips.
- Use `sync/atomic` for lock-free local counters.

## Success Criteria

1. The token bucket correctly limits requests to the configured rate and burst
2. The sliding window counter provides accurate windowed limiting
3. Local division enforces limits but allows over-limiting under uneven traffic distribution
4. The centralized limiter accurately enforces the global limit
5. The gossip-based limiter converges to accurate global counts within a few gossip rounds
6. Per-client limiting correctly isolates clients from each other
7. Benchmarks quantify the accuracy/latency tradeoff of each approach
8. The system handles node joins/leaves without violating limits by more than a small margin

## Research Resources

- [Rate Limiting at Stripe](https://stripe.com/blog/rate-limiters) -- practical rate limiting strategies
- [Envoy Rate Limiting](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/rate_limit_filter) -- distributed rate limiting in service mesh
- [Token Bucket (Wikipedia)](https://en.wikipedia.org/wiki/Token_bucket)
- [Google Cloud Rate Limiting](https://cloud.google.com/architecture/rate-limiting-strategies-techniques)

## What's Next

Continue to [13 - Sharded Key-Value Store](../13-sharded-key-value-store/13-sharded-key-value-store.md) to build a distributed key-value store with sharding and replication.

## Summary

- Distributed rate limiting requires coordination to enforce global limits across multiple nodes
- Local division is simple but inaccurate under uneven traffic
- Centralized coordination is accurate but adds latency
- Gossip-based aggregation balances accuracy and latency with eventual convergence
- Token bucket and sliding window are the two fundamental rate limiting algorithms
- Per-client limiting enables fair usage enforcement across API consumers
- Production systems use batched token pre-fetching to reduce coordination overhead
