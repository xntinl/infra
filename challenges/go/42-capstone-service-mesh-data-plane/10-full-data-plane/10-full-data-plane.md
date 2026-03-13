# 10. Full Data Plane

<!--
difficulty: insane
concepts: [service-mesh, data-plane, integration, sidecar-proxy, configuration-driven, end-to-end-testing, performance-testing, production-readiness]
tools: [go]
estimated_time: 6h
bloom_level: create
prerequisites: [42-capstone-service-mesh-data-plane/09-control-plane-grpc]
-->

## Prerequisites

- Go 1.22+ installed
- Completed all exercises 01-09 in this section or equivalent experience building each component independently
- Understanding of how production service mesh data planes (Envoy, Linkerd-proxy) integrate L4/L7 proxying, security, traffic management, and observability

## Learning Objectives

- **Design** a complete, configuration-driven service mesh data plane by integrating L4/L7 proxying, mTLS, load balancing, health checking, traffic management, rate limiting, observability, and control plane communication into a cohesive system
- **Create** a sidecar proxy that can be deployed alongside a service to transparently handle all inbound and outbound traffic
- **Evaluate** the end-to-end behavior of the integrated data plane under realistic failure scenarios, traffic patterns, and configuration changes

## The Challenge

You have built all the pieces. Now you must assemble them into a complete, working service mesh data plane. This is the capstone integration challenge -- the difficulty is not in any single component, but in making nine independently developed systems work together correctly, efficiently, and reliably.

A production data plane receives its configuration from a control plane, opens listeners, terminates mTLS, routes requests through L7 or L4, applies traffic management policies (retries, timeouts, circuit breaking), enforces rate limits, load balances across healthy upstream endpoints, collects metrics on everything, and does all of this for thousands of concurrent requests without introducing meaningful latency overhead. Your integrated proxy must do the same.

The integration challenges are numerous. The mTLS layer must feed the authenticated peer identity into the rate limiter's descriptor extraction. The health checker must update the load balancer's backend pool. The circuit breaker must interact correctly with the retry policy. The control plane client must apply configuration updates atomically without disrupting in-flight requests. The metrics system must capture the behavior of every other component. And all of this must be configurable, testable, and performant.

You will also implement the operational surface: a startup sequence that initializes components in the correct dependency order, a shutdown sequence that drains connections gracefully, an admin API for operational inspection, and a configuration validation layer that rejects invalid configurations before they can crash the proxy.

## Requirements

1. Implement a `DataPlane` struct that owns and coordinates all components: L4/L7 proxy, mTLS termination, load balancer, health checker, traffic management (retries, timeouts, circuit breaker), rate limiter, metrics registry, and xDS client
2. Implement a startup sequence that initializes components in dependency order: metrics registry first, then xDS client (to receive configuration), then health checker, load balancer, traffic management, rate limiter, mTLS, and finally the listener
3. Implement a shutdown sequence: stop accepting new connections, drain active connections with a configurable timeout, stop health checkers, disconnect from the control plane, and flush final metrics
4. Implement a request processing pipeline that chains components in the correct order: mTLS termination -> request parsing -> rate limiting -> route matching -> circuit breaker check -> timeout wrapping -> upstream selection (load balancer) -> retry logic -> response streaming -> metrics recording
5. Wire the mTLS peer identity into the rate limiter's descriptor extraction so rate limits can be applied per authenticated service identity
6. Wire the health checker's backend state changes into the load balancer's backend pool updates
7. Wire the circuit breaker state into the metrics system so circuit breaker transitions are observable
8. Implement an admin API on a separate port exposing: `/ready` (readiness probe), `/health` (liveness probe), `/metrics` (Prometheus metrics), `/config` (current active configuration as JSON), and `/clusters` (upstream cluster status including health and active connections)
9. Implement configuration validation that checks for: duplicate listener ports, routes referencing non-existent clusters, clusters referencing non-existent endpoints, and circular retry configurations
10. Implement hot restart support: a new proxy instance can start, receive configuration, and take over the listener socket from the old instance using socket passing, enabling zero-downtime binary upgrades
11. Write end-to-end tests that deploy the full data plane with a mock control plane and multiple mock upstream backends, then verify: request routing, mTLS authentication, load balancing distribution, health check ejection, circuit breaker activation, rate limiting enforcement, retry behavior, and metrics accuracy
12. Write a load test that measures p99 latency overhead introduced by the proxy under sustained 10,000 requests per second

## Hints

- Use the builder pattern for `DataPlane` construction: `NewDataPlane().WithMTLS(config).WithLoadBalancer(algo).WithRetryPolicy(policy).Build()` makes configuration ergonomic and testable
- For the request processing pipeline, use the middleware pattern: each component is an `http.Handler` wrapper, and they are composed via function chaining
- For the admin API, run a separate `http.Server` on a different port to isolate admin traffic from data traffic
- For hot restart, use `net.FileListener` with Unix domain socket passing via `syscall.Sendmsg` -- the old process sends the listener file descriptor to the new process over a Unix socket
- For configuration validation, implement it as a separate pass before applying configuration, returning all validation errors rather than stopping at the first
- For end-to-end tests, use `httptest.NewUnstartedServer` with custom TLS configurations to simulate mTLS-authenticated upstream backends
- For load testing, use a fixed pool of goroutines each making sequential requests to avoid measuring goroutine scheduling overhead as proxy latency

## Success Criteria

1. The integrated proxy starts up correctly, receives configuration from the control plane, and begins routing traffic
2. The shutdown sequence drains all active connections within the configured timeout without data loss
3. The request processing pipeline applies all policies in the correct order for every request
4. mTLS peer identities are correctly propagated to rate limiting and access logging
5. Health check state changes are reflected in load balancer routing decisions within one check interval
6. Circuit breaker state transitions are recorded in metrics and visible via the admin API
7. Configuration validation rejects invalid configurations with descriptive error messages
8. The admin API correctly reports readiness, health, metrics, configuration, and cluster status
9. End-to-end tests demonstrate correct behavior across all integrated components
10. The proxy introduces less than 1ms of p99 latency overhead at 10,000 requests per second for a pass-through request
11. All tests pass with the `-race` flag enabled
12. No goroutine leaks after startup, sustained load, and shutdown

## Research Resources

- [Envoy architecture overview](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/arch_overview) -- the reference architecture for a production service mesh data plane
- [Linkerd proxy architecture](https://linkerd.io/2/reference/architecture/) -- alternative data plane architecture focused on simplicity
- [Envoy hot restart](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/operations/hot_restart) -- zero-downtime binary upgrade via socket passing
- [Unix domain socket file descriptor passing](https://man7.org/linux/man-pages/man7/unix.7.html) -- mechanism for passing listener sockets between processes
- [Go net.FileListener](https://pkg.go.dev/net#FileListener) -- creating a listener from an existing file descriptor
- [Go testing/benchmark](https://pkg.go.dev/testing#hdr-Benchmarks) -- benchmark framework for latency measurement

## Summary

- A complete service mesh data plane integrates nine distinct systems into a cohesive request processing pipeline
- Component initialization and shutdown ordering is critical for correctness -- dependencies must be started before dependents
- The middleware pattern enables clean composition of the request processing pipeline
- Cross-component wiring (mTLS identity to rate limiter, health checker to load balancer) is where integration complexity lives
- Configuration validation prevents runtime failures by catching invalid configurations before they are applied
- Hot restart enables zero-downtime binary upgrades by passing listener sockets between process generations
- End-to-end testing with realistic failure scenarios validates that integrated behavior matches component-level expectations
- Performance testing under load ensures that the integration overhead does not negate the benefits of the data plane
