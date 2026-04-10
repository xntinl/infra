# Software Architecture Patterns — Reference Overview

## Why This Section Matters

Most developers learn software architecture the hard way: they build a system that works at 1,000 users, then watch it crack at 100,000. The patterns in this section are not academic exercises — they are scars turned into blueprints. Domain-Driven Design emerged from watching teams build the wrong software because engineers and domain experts spoke different languages. CQRS emerged from systems that couldn't scale reads independently of writes. Event Sourcing emerged from audit requirements that couldn't be retrofitted onto mutable databases. Each pattern here has a specific failure mode it prevents, and understanding that failure mode is what separates developers who apply patterns mechanically from architects who know when to use them and, crucially, when not to.

This section is not an introduction to these concepts. It assumes you can write working software. The goal is to take you from "I've heard of DDD" to "I understand why an Aggregate is a consistency boundary and how to enforce that boundary in Go and Rust without a framework."

The code examples are intentionally business-like — orders, payments, inventory — because that is where these patterns earn their keep. A pattern that only works in toy examples is not a pattern; it is a tutorial.

## Subtopics

| # | Topic | Core Problem Solved | Estimated Reading |
|---|-------|---------------------|-------------------|
| 01 | [Domain-Driven Design](./01-domain-driven-design/01-domain-driven-design.md) | Language mismatch between code and domain experts causes the wrong system to be built correctly | 75 min |
| 02 | [CQRS Pattern](./02-cqrs-pattern/02-cqrs-pattern.md) | Read and write models have fundamentally different shapes; forcing one model to serve both causes complexity and scaling bottlenecks | 60 min |
| 03 | [Event Sourcing](./03-event-sourcing/03-event-sourcing.md) | Mutable state destroys audit trails, temporal queries, and the ability to replay business decisions | 80 min |
| 04 | [Hexagonal Architecture](./04-hexagonal-architecture/04-hexagonal-architecture.md) | Business logic entangled with infrastructure (HTTP, SQL, messaging) becomes untestable and immovable | 65 min |
| 05 | [Reactive Systems](./05-reactive-systems/05-reactive-systems.md) | Blocking I/O and unbounded queues cause cascading failures under load | 70 min |
| 06 | [Saga and Distributed Workflows](./06-saga-and-distributed-workflows/06-saga-and-distributed-workflows.md) | Distributed transactions are impossible; sagas provide consistency across service boundaries without 2PC | 75 min |
| 07 | [Microservices Patterns](./07-microservices-patterns/07-microservices-patterns.md) | Distributed systems introduce failure modes that monoliths never had; patterns that prevent distributed monoliths | 70 min |
| 08 | [Event-Driven Architecture](./08-event-driven-architecture/08-event-driven-architecture.md) | Synchronous coupling between services creates temporal dependencies and fragility | 65 min |

## Pattern Relationships

These patterns are not independent. They compose, and understanding their composition is more valuable than understanding each in isolation.

```
Domain-Driven Design
        │
        │  provides the domain model for
        ▼
   Event Sourcing ◄────────────────────────────────────┐
        │                                               │
        │  events feed into                             │ events are the
        ▼                                               │ source of truth for
      CQRS ──── read projections ──── read models      │
        │                                               │
        │  commands/events flow through                 │
        ▼                                               │
  Event-Driven Architecture ─────────────────────────┘
        │
        │  services communicate via events, requiring
        ▼
  Saga and Distributed Workflows ──── coordinates via ──── Microservices Patterns
        │
        │  all of the above sit inside
        ▼
  Hexagonal Architecture  (domain, application, infrastructure separation)
        │
        │  the application layer must be
        ▼
  Reactive Systems  (back-pressure, resilience, elasticity)
```

**Common compositional stacks:**

- **DDD + CQRS + Event Sourcing**: The classic trio. DDD gives you the model and language. Event Sourcing makes the write side an append-only log. CQRS gives you optimized read projections from that log. Used by Axon Framework, EventStoreDB ecosystems.

- **Hexagonal + DDD**: The ports-and-adapters structure maps cleanly onto DDD's domain/application/infrastructure layers. The domain is the inner hexagon. Infrastructure is the adapters. This is the structural foundation most production DDD systems use.

- **Event-Driven + Saga**: Events trigger saga steps. The saga orchestrates compensating transactions when steps fail. Temporal/Cadence is built on exactly this.

- **Microservices + Reactive + Circuit Breaker**: Microservices multiply failure points. Reactive systems handle those failures without cascading. The circuit breaker is the key reliability primitive.

## Pattern Selection Guide

| Situation | Pattern to Reach For | Pattern to Avoid |
|-----------|---------------------|-----------------|
| Complex domain with many business rules | DDD | None — just use it |
| Read-heavy system with complex queries | CQRS | Event Sourcing alone (adds complexity without read optimization) |
| Need full audit trail or time-travel queries | Event Sourcing | Mutable databases |
| Business logic tested without a running database | Hexagonal Architecture | Layered architecture with direct DB calls in business logic |
| High-throughput async processing | Reactive Systems | Synchronous blocking calls |
| Business process spanning multiple services | Saga | Distributed transactions (2PC) |
| Service-to-service communication that must survive partial failures | Microservices Patterns (circuit breaker, bulkhead) | Direct synchronous HTTP chains |
| Services that must decouple in time | Event-Driven Architecture | Synchronous APIs for non-time-critical operations |

## Time Investment

| Level | Description | Estimated Hours |
|-------|-------------|-----------------|
| Comprehension | Read all documents, trace through examples | 8–10 hours |
| Hands-on | Complete the 30-min and 2–4h exercises per topic | 30–40 hours |
| Proficiency | Complete the 4–8h exercises, build small systems | 60–80 hours |
| Production-ready | Complete 8–15h exercises, implement with full concerns | 120–160 hours |

## Prerequisites

Before diving into this section, you should be comfortable with:

- **Go**: goroutines, channels, interfaces, composition over inheritance, error handling idioms
- **Rust**: ownership model, traits, enums with data (`Result`, `Option`), async/await with Tokio, `Arc<Mutex<T>>` vs message passing
- **Databases**: SQL transactions, ACID properties, indexes, the difference between a read replica and a write primary
- **Distributed Systems fundamentals**: the CAP theorem (what it actually says, not the oversimplification), eventual consistency, idempotency, at-least-once vs exactly-once delivery
- **HTTP and messaging basics**: REST, gRPC, message queues (Kafka or RabbitMQ at a conceptual level)

If distributed systems fundamentals are gaps, read "Designing Data-Intensive Applications" (Kleppmann) before the Event Sourcing, Saga, and Event-Driven Architecture sections. It is the single best prerequisite text for this material.
