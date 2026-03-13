# 4. Load Balancing

<!--
difficulty: insane
concepts: [load-balancing, round-robin, least-connections, consistent-hashing, weighted-backends, connection-pooling, locality-aware-routing]
tools: [go]
estimated_time: 3h
bloom_level: create
prerequisites: [42-capstone-service-mesh-data-plane/02-l7-http-proxy, 15-sync-primitives, 26-memory-model-and-optimization]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-02 (L4/L7 proxy) or equivalent proxy experience
- Completed Section 15 (sync primitives) and 26 (memory model) or equivalent experience

## Learning Objectives

- **Design** a pluggable load balancing framework supporting multiple algorithms behind a common interface
- **Create** round-robin, least-connections, and consistent hash load balancing implementations with weighted backend support
- **Evaluate** the behavioral differences between load balancing algorithms under varying traffic patterns including backend failures and hot keys

## The Challenge

A service mesh data plane must distribute traffic intelligently across multiple instances of an upstream service. Different load balancing algorithms suit different traffic patterns: round-robin provides simplicity and even distribution, least-connections adapts to backends with varying processing speeds, and consistent hashing ensures requests for the same key always reach the same backend (critical for caching and session affinity).

You will build a load balancing framework with a common `Balancer` interface, then implement three algorithms: weighted round-robin, least-connections, and consistent hashing with bounded loads. Each balancer must integrate with the health checking system (built in exercise 05) by only routing to healthy backends. The consistent hash implementation must use a ring with virtual nodes to ensure even distribution and must support bounded loads to prevent hotspotting. All implementations must be safe for concurrent use from thousands of goroutines making simultaneous routing decisions.

The subtlety lies in correctness under concurrency: the least-connections balancer must atomically increment and decrement connection counts, the consistent hash ring must support dynamic membership changes without disrupting existing mappings, and weighted round-robin must handle weight changes without starving any backend.

## Requirements

1. Define a `Balancer` interface with `Pick(ctx context.Context, key string) (Backend, error)` and `Release(backend Backend)` methods
2. Define a `Backend` struct with address, weight, health status, active connections, and metadata fields
3. Implement `RoundRobinBalancer` that cycles through healthy backends, respecting weights (a backend with weight 3 gets 3x the traffic of weight 1)
4. Implement `LeastConnectionsBalancer` that picks the healthy backend with the fewest active connections, breaking ties with round-robin
5. Implement `ConsistentHashBalancer` using a hash ring with configurable virtual nodes per backend (default 150), picking based on the provided key
6. Add bounded load support to the consistent hash balancer: if the selected backend's load exceeds `average_load * 1.25`, probe the next node on the ring
7. Support dynamic backend membership: backends can be added and removed at runtime, and the balancer must update without dropping in-flight requests
8. Implement connection counting that increments on `Pick()` and decrements on `Release()` using atomic operations
9. Integrate health status: balancers must skip unhealthy backends and return an error if no healthy backends are available
10. Expose per-backend metrics: total requests routed, active connections, and average latency (updated via a callback)

## Hints

- For weighted round-robin, use the smooth weighted round-robin algorithm: each backend has a `currentWeight` that is incremented by its `effectiveWeight` each round, and the backend with the highest `currentWeight` is selected and decremented by the total weight sum
- For least-connections, use `atomic.Int64` for connection counts and scan all healthy backends on each pick -- with typical backend counts (<100), a linear scan is faster than maintaining a heap
- For consistent hashing, hash each backend to `virtualNodes` positions on a uint32 ring using `crc32.ChecksumIEEE` or `xxhash`, then binary search for the first ring position >= `hash(key)`
- Protect the hash ring with a `sync.RWMutex` -- reads are far more frequent than membership changes
- For bounded loads, calculate the average load across all backends and reject candidates exceeding `1.25 * average`, walking clockwise on the ring until an acceptable backend is found
- Use `sort.Search` for efficient binary search on the sorted ring positions

## Success Criteria

1. Weighted round-robin distributes traffic proportionally to backend weights over 10000 requests
2. Least-connections correctly routes to the backend with the fewest active connections
3. Consistent hashing routes the same key to the same backend deterministically
4. Adding or removing a backend from the consistent hash ring only remaps approximately `1/n` of keys (where n is the number of backends)
5. Bounded load consistent hashing prevents any backend from exceeding 1.25x the average load
6. Unhealthy backends receive zero traffic across all algorithms
7. Connection counts are accurate under concurrent Pick/Release from 100 goroutines
8. All tests pass with the `-race` flag enabled

## Research Resources

- [Consistent Hashing with Bounded Loads (Google Research)](https://research.google/blog/consistent-hashing-with-bounded-loads/) -- the algorithm for preventing hotspots in consistent hashing
- [Envoy load balancing policies](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/upstream/load_balancing/load_balancers) -- reference implementations of multiple balancing algorithms
- [Smooth Weighted Round-Robin (nginx)](https://github.com/phusion/nginx/commit/27e94984486058d73157038f7950a0a36ecc6e35) -- the algorithm used in nginx for weighted round-robin
- [Go sync/atomic package](https://pkg.go.dev/sync/atomic) -- atomic operations for lock-free connection counting
- [Maglev: A Fast and Reliable Software Network Load Balancer](https://research.google/pubs/maglev-a-fast-and-reliable-software-network-load-balancer/) -- Google's production consistent hashing approach

## What's Next

Continue to [Health Checking](../05-health-checking/05-health-checking.md) where you will implement active and passive health checking to feed backend health status into the load balancer.

## Summary

- A pluggable `Balancer` interface allows swapping algorithms without changing the proxy core
- Weighted round-robin distributes traffic proportionally using the smooth weighted algorithm
- Least-connections adapts to heterogeneous backend processing speeds by tracking active requests
- Consistent hashing with virtual nodes provides stable key-to-backend mapping with minimal disruption on membership changes
- Bounded loads prevent hotspots by redistributing excess load to neighboring ring positions
- Atomic operations and read-write locks enable high-throughput concurrent access to balancer state
