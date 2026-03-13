# 5. Health Checking

<!--
difficulty: insane
concepts: [health-checking, active-health-checks, passive-health-checks, outlier-detection, ejection, circuit-state, tcp-health-check, http-health-check]
tools: [go]
estimated_time: 2h
bloom_level: create
prerequisites: [42-capstone-service-mesh-data-plane/04-load-balancing, 17-http-programming, 14-select-and-context]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercise 04 (load balancing) or equivalent experience with backend management
- Completed Sections 14 (select and context) and 17 (HTTP programming) or equivalent experience

## Learning Objectives

- **Design** a health checking system with both active probing and passive failure observation
- **Create** TCP and HTTP health checkers with configurable thresholds, intervals, and ejection policies
- **Evaluate** the interaction between active health checks, passive outlier detection, and load balancer backend state

## The Challenge

Health checking is what keeps a service mesh from routing traffic into black holes. When a backend instance crashes, becomes overloaded, or starts returning errors, the data plane must detect this and stop sending traffic to it. Service meshes use two complementary approaches: active health checks (periodically probing backends) and passive health checks (observing real traffic for failures).

You will build both. The active health checker periodically probes each backend using configurable checks (TCP connect, HTTP GET expecting a specific status code) and maintains a health state machine: a backend transitions from healthy to unhealthy after a configurable number of consecutive failures, and back to healthy after a configurable number of consecutive successes. The passive health checker (outlier detection) observes actual request outcomes and ejects backends that exceed error rate thresholds within a sliding window. Ejected backends are returned to the pool after an exponentially increasing cooldown period.

The interaction between these two systems is critical: active checks should be able to restore a passively ejected backend, and passive detection should be able to mark a backend unhealthy even if active checks pass (e.g., when the health endpoint is fine but the application returns 500s).

## Requirements

1. Define a `HealthChecker` interface with `Start(ctx context.Context)`, `Stop()`, and `IsHealthy(backend Backend) bool` methods
2. Implement `TCPHealthChecker` that attempts a TCP connection to each backend at a configurable interval, with a configurable connection timeout
3. Implement `HTTPHealthChecker` that sends a GET request to a configurable path and expects a response within a configurable set of acceptable status codes
4. Implement a health state machine: backends transition to unhealthy after N consecutive failures (`unhealthy_threshold`) and back to healthy after M consecutive successes (`healthy_threshold`)
5. Implement passive outlier detection that tracks error rates per backend in a sliding time window (e.g., last 30 seconds) and ejects backends exceeding a configurable error rate threshold
6. Ejected backends are returned to the pool after an exponentially increasing cooldown: `base_ejection_time * 2^(consecutive_ejections - 1)`, capped at a maximum ejection time
7. Ensure at least one backend always remains in the pool -- never eject the last healthy backend (panic protection)
8. Integrate with the load balancer from exercise 04: health status changes must be reflected immediately in routing decisions
9. Expose health check metrics: checks performed, failures detected, ejections, and current health status per backend
10. All health checking operations must be safe for concurrent access

## Hints

- Use a ticker goroutine per backend for active health checks, with a `select` on the ticker channel and the context's `Done()` channel
- For the health state machine, use a simple counter that increments on failure and resets on success (and vice versa for recovery)
- For passive outlier detection, use a ring buffer of timestamped outcomes (success/failure) and count failures within the window on each observation
- Protect the panic protection (never eject last backend) by checking the count of healthy backends before each ejection decision
- Use `time.AfterFunc` for scheduling backend restoration after the ejection cooldown period
- The health checker should notify the load balancer of state changes through a callback or channel, not by directly modifying balancer state

## Success Criteria

1. TCP health checks detect a backend that stops accepting connections within one check interval
2. HTTP health checks detect a backend returning non-200 status codes and mark it unhealthy after the configured threshold
3. Backends transition back to healthy after the configured number of consecutive successes
4. Passive outlier detection ejects backends exceeding the error rate threshold
5. Ejection cooldown increases exponentially with consecutive ejections
6. The last healthy backend is never ejected, even if it exceeds the error rate threshold
7. Health status changes are reflected in load balancer routing decisions within one check interval
8. All tests pass with the `-race` flag enabled

## Research Resources

- [Envoy health checking](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/upstream/health_checking) -- reference design for active health checking
- [Envoy outlier detection](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/upstream/outlier) -- reference design for passive outlier detection
- [Go time.Ticker](https://pkg.go.dev/time#Ticker) -- periodic execution for health check intervals
- [Circuit breaker pattern](https://learn.microsoft.com/en-us/azure/architecture/patterns/circuit-breaker) -- related pattern for failure detection and recovery

## What's Next

Continue to [Traffic Management](../06-traffic-management/06-traffic-management.md) where you will implement retries, timeouts, and circuit breaking to make the proxy resilient to transient failures.

## Summary

- Active health checks proactively probe backends using TCP or HTTP checks at regular intervals
- Passive outlier detection observes real traffic outcomes to detect failures that active checks might miss
- A health state machine with configurable thresholds prevents flapping between healthy and unhealthy states
- Exponential ejection cooldowns prevent rapid oscillation while allowing gradual recovery
- Panic protection ensures at least one backend always remains available for routing
- Health status changes integrate with the load balancer to immediately affect routing decisions
