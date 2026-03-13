# 11. Service Discovery

<!--
difficulty: insane
concepts: [service-discovery, service-registry, health-checking, dns-based-discovery, client-side-discovery, server-side-discovery, watch-api]
tools: [go]
estimated_time: 90m
bloom_level: create
prerequisites: [gossip-protocol, consistent-hashing-ring, http-programming]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of gossip protocols and consistent hashing
- Familiarity with HTTP programming and health checks

## Learning Objectives

- **Create** a service discovery system with registration, health checking, and query resolution
- **Analyze** consistency vs availability tradeoffs in service discovery
- **Evaluate** different discovery patterns: client-side, server-side, and DNS-based

## The Challenge

In a microservices architecture, services need to find each other. Service discovery is the mechanism: services register themselves with a registry, the registry health-checks registered instances, and clients query the registry to find healthy instances of the service they need.

Build a service discovery system from scratch. Implement service registration with TTL-based expiry, active and passive health checking, DNS-based resolution, and a watch API for real-time updates. The registry should handle node failures gracefully and support multiple instances per service.

## Requirements

1. Implement a `ServiceRegistry` that stores service instances with metadata (address, port, tags, health status)
2. Implement service registration with TTL: instances must periodically re-register or they are deregistered
3. Implement health checking: the registry periodically sends HTTP health check requests to registered instances
4. Implement service lookup: query by service name, optionally filter by tags, return only healthy instances
5. Implement a watch API: clients subscribe to changes for a service and receive notifications when instances are added, removed, or change health status
6. Implement client-side load balancing: the client receives all healthy instances and selects one (round-robin, random, weighted)
7. Implement DNS-based discovery: serve SRV records for registered services so standard DNS clients can discover them
8. Write an end-to-end demo: multiple service instances register, health checks run, one instance fails, clients receive updates and route traffic to healthy instances

## Hints

- Use an HTTP server for the registry API: `POST /register`, `DELETE /deregister`, `GET /services/{name}`, `GET /health`.
- TTL-based expiry: store `lastHeartbeat` per instance. A background goroutine reaps instances that have not heartbeated within the TTL.
- For the watch API, use long polling or Server-Sent Events (SSE). Long polling: the client sends a blocking request with a version number; the server responds when the version changes.
- Health checks: send HTTP GET to the instance's health endpoint. Mark as unhealthy after N consecutive failures, healthy after M consecutive successes.
- DNS SRV records: `_servicename._tcp.domain SRV priority weight port target`. Use `net` package to serve DNS responses or integrate with `miekg/dns`.
- For availability, replicate the registry using gossip (from exercise 02) so that no single registry node is a point of failure.

## Success Criteria

1. Services can register and deregister with the registry
2. Expired registrations (TTL exceeded) are automatically removed
3. Health checks correctly detect unhealthy instances
4. Service lookup returns only healthy instances
5. The watch API notifies clients of changes within a reasonable latency
6. Client-side load balancing distributes requests across healthy instances
7. The system handles registry node failure gracefully (if gossip-replicated)
8. DNS-based discovery resolves service names to addresses

## Research Resources

- [Consul Service Discovery](https://developer.hashicorp.com/consul/docs/concepts/service-discovery) -- production service discovery architecture
- [Netflix Eureka](https://github.com/Netflix/eureka) -- client-side discovery pattern
- [CoreDNS](https://coredns.io/) -- DNS-based service discovery
- [Designing Data-Intensive Applications, Chapter 8](https://dataintensive.net/) -- service discovery in distributed systems

## What's Next

Continue to [12 - Distributed Rate Limiter](../12-distributed-rate-limiter/12-distributed-rate-limiter.md) to build a rate limiter that works across multiple nodes.

## Summary

- Service discovery enables dynamic service-to-service communication in distributed systems
- Registration with TTL ensures stale entries are cleaned up automatically
- Health checking detects and removes unhealthy instances from the pool
- Watch APIs provide real-time notifications of topology changes
- DNS-based discovery provides compatibility with standard tooling
- Production systems (Consul, Eureka, CoreDNS) combine these patterns with replication for availability
