# 30. Connection Pool with Health Checking

<!--
difficulty: advanced
category: caching-and-networking
languages: [go]
concepts: [connection-pooling, health-checking, circuit-breaker, graceful-shutdown, load-distribution, lifecycle-hooks]
estimated_time: 6-8 hours
bloom_level: evaluate
prerequisites: [go-concurrency, interfaces, context-package, sync-primitives, circuit-breaker-pattern, tcp-networking]
-->

## Languages

- Go (1.22+)

## Prerequisites

- Advanced goroutine patterns (worker pools, background monitors, graceful shutdown)
- Interface-based design and dependency injection
- `context.Context` for cancellation and deadline propagation
- `sync.Mutex`, `sync.Cond`, and atomic operations
- The circuit breaker pattern (states: closed, open, half-open)
- TCP connection lifecycle (dial, read/write, close, half-close)

## Learning Objectives

- **Design** a generic connection pool with pluggable connection factories and lifecycle hooks
- **Evaluate** active versus passive health checking strategies and their impact on tail latency
- **Implement** the circuit breaker pattern to isolate unhealthy backends from the pool
- **Analyze** connection draining mechanics for zero-downtime shutdown under active load
- **Justify** weighted random selection over round-robin for heterogeneous backend distributions

## The Challenge

Connection pools are invisible infrastructure. Every database driver, HTTP client, and gRPC channel uses one internally. When they work, nobody notices. When they fail -- serving requests on dead connections, exhausting file descriptors, or refusing to release connections back -- the symptoms are mysterious timeouts and cascading failures that are difficult to diagnose.

Your task is to build a generic connection pool that manages a fleet of connections to one or more backends. The pool must dynamically scale between a minimum and maximum size, actively verify connection health through background goroutines, and use the circuit breaker pattern to stop sending traffic to backends that are failing. Lifecycle hooks let callers observe every transition: creation, borrow, return, and destruction.

This is an advanced challenge. You will not receive worked examples or complete code snippets. The hints below describe strategies and trade-offs, not implementations. You must design the interfaces, choose the synchronization primitives, and handle the edge cases yourself.

## Requirements

1. Define a `Pool[C any]` generic type that manages connections of any type through a `Connector` interface with `Connect(ctx) (C, error)`, `Validate(C) error`, and `Close(C) error` methods
2. Configure minimum pool size (pre-warmed connections), maximum pool size (hard cap), and idle timeout (close connections unused for longer than this)
3. Dynamic scaling: create connections on demand up to the maximum. When demand drops, shrink back toward the minimum over time
4. Background active health checking: a goroutine periodically calls `Validate()` on idle connections and removes those that fail
5. Passive health checking: when a borrowed connection returns an error during use, mark it as unhealthy instead of returning it to the pool
6. Lifecycle hooks: `OnCreate(C)`, `OnBorrow(C)`, `OnReturn(C)`, `OnDestroy(C)` -- called at the appropriate transition points
7. Circuit breaker per backend: track consecutive failures. When failures exceed a threshold, open the circuit (stop attempting connections for a cooldown period). After cooldown, enter half-open state and allow one probe connection
8. Weighted random selection: when multiple backends are available, select based on configurable weights. Unhealthy backends have their weight reduced to zero
9. `Borrow(ctx) (C, error)` blocks until a connection is available or context expires. `Return(C, error)` returns a connection or signals it should be destroyed (if error is non-nil)
10. Graceful shutdown: `Drain()` stops accepting new borrows, waits for all borrowed connections to be returned, then closes everything. Active requests must complete before shutdown finishes

## Hints

<details>
<summary>Hint 1: Pool state machine</summary>

The pool transitions through states: `running` (normal operation), `draining` (no new borrows, waiting for returns), `stopped` (all connections closed). Use a `sync.Cond` to wake blocked `Borrow()` callers when connections are returned or the pool state changes.
</details>

<details>
<summary>Hint 2: Circuit breaker state transitions</summary>

Three states: closed (healthy, allowing traffic), open (unhealthy, rejecting all attempts), half-open (probing with a single request). The transition from open to half-open is time-based. The transition from half-open to closed or open depends on whether the probe succeeds. Store the state, failure count, and last failure time atomically.
</details>

<details>
<summary>Hint 3: Active health check goroutine</summary>

The health checker must not hold the pool lock while calling `Validate()` because validation involves I/O (network round-trip). Collect idle connections under the lock, release the lock, validate each one, then re-acquire the lock to remove failures. This avoids blocking `Borrow()` and `Return()` during health checks.
</details>

<details>
<summary>Hint 4: Weighted random selection</summary>

For N backends with weights w1...wN, compute the cumulative sum. Generate a random number in [0, total_weight) and binary search for the backend. When a circuit opens, set its weight to zero and recompute the cumulative sum.
</details>

## Acceptance Criteria

- [ ] Pool pre-warms to minimum size on creation
- [ ] `Borrow` creates new connections on demand up to the maximum
- [ ] `Borrow` blocks when all connections are in use and maximum is reached
- [ ] Idle connections are closed after the configured timeout
- [ ] Active health checker removes connections that fail `Validate()`
- [ ] Passive health check discards connections returned with errors
- [ ] Lifecycle hooks fire at the correct transition points
- [ ] Circuit breaker opens after consecutive failure threshold, re-probes after cooldown
- [ ] Weighted random selection distributes traffic proportional to configured weights
- [ ] `Drain()` blocks until all borrowed connections are returned, then closes everything
- [ ] No goroutine leaks after `Drain()` completes
- [ ] Pool handles 1,000 concurrent borrows/returns without races or deadlocks

## Research Resources

- [Go database/sql pool source](https://github.com/golang/go/blob/master/src/database/sql/sql.go) -- study `DB.conn()` and `putConnDBLocked()` for production pooling patterns
- [HikariCP: About Pool Sizing](https://github.com/brettwooldridge/HikariCP/wiki/About-Pool-Sizing) -- foundational analysis of connection pool sizing
- [Microsoft: Circuit Breaker Pattern](https://learn.microsoft.com/en-us/azure/architecture/patterns/circuit-breaker) -- formal state machine and implementation guidance
- [Netflix Hystrix: How It Works](https://github.com/Netflix/Hystrix/wiki/How-it-Works) -- circuit breaker with metrics and fallback (archived but still the reference)
- [gRPC Connection Backoff Protocol](https://github.com/grpc/grpc/blob/master/doc/connection-backoff.md) -- exponential backoff for connection retry
- [Envoy Proxy: Health Checking](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/upstream/health_checking) -- active and passive health check architecture in a production load balancer
