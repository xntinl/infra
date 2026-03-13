# 19. CQRS and Eventual Consistency

<!--
difficulty: insane
concepts: [cqrs, command-query-separation, read-model, write-model, eventual-consistency, projection-lag, consistency-boundary]
tools: [go]
estimated_time: 3h
bloom_level: create
prerequisites: [event-sourcing-engine, distributed-locking]
-->

## Prerequisites

- Go 1.22+ installed
- Completed the event sourcing exercise (exercise 18)
- Understanding of eventual consistency and CAP theorem

## Learning Objectives

- **Create** a CQRS system with separate write and read models connected by an event bus
- **Analyze** consistency guarantees and propagation lag between write and read sides
- **Evaluate** strategies for handling stale reads and ensuring causal consistency

## The Challenge

CQRS (Command Query Responsibility Segregation) separates the write model (commands that change state) from the read model (queries that return state). The write side produces events, and the read side consumes events to build materialized views optimized for specific query patterns. This allows independent scaling, optimization, and evolution of each side.

The fundamental challenge is eventual consistency: after a write, the read model is temporarily stale. Clients may read data that does not reflect their most recent write. You need strategies to handle this: causal consistency tokens, read-your-writes guarantees, and stale-read detection.

## Requirements

1. Implement the write side: a command handler that validates commands against the aggregate state, produces events, and stores them in the event store
2. Implement an event bus that delivers events from the write side to read-side projections
3. Implement at least three read model projections optimized for different query patterns:
   - Summary view (e.g., account balance)
   - List view (e.g., recent transactions)
   - Aggregate view (e.g., spending by category)
4. Implement causal consistency: the write side returns a version token; the client includes this token on subsequent reads; the read side waits until the projection has caught up to that version
5. Implement read-your-writes consistency: after a command, the client can read the write model directly (bypassing the projection) if the projection has not caught up
6. Measure and report projection lag: the time between event production and read model update
7. Implement projection rebuilding: the ability to rebuild a read model from scratch by replaying all events (for schema changes or bug fixes)
8. Implement a demo application (e.g., e-commerce: place order, view order, list orders, view analytics) that demonstrates CQRS with eventual consistency
9. Write tests for: command validation, projection accuracy, causal consistency enforcement, projection rebuilding correctness

## Hints

- The event bus can be a simple Go channel for in-process CQRS, or simulate an external message broker with buffered channels and subscriber registration.
- Causal consistency tokens: the write side returns the event version number. The client sends this version on reads. The read side blocks until its projection version >= the requested version (with a timeout).
- Projection lag depends on event bus latency and projection processing speed. Monitor the gap between the latest event version and each projection's processed version.
- Projection rebuilding: create a new projection, replay all events from the store, then swap the old projection for the new one. Support zero-downtime rebuilding by running both in parallel.
- CQRS does not require event sourcing -- you can use it with a traditional database on the write side. But the combination is powerful: events are the natural bridge between write and read models.
- Consider using separate goroutines for each projection consumer. This allows projections to process at their own pace.

## Success Criteria

1. Commands are validated and produce events on the write side
2. Projections build accurate read models from the event stream
3. Each projection is optimized for its specific query pattern
4. Causal consistency tokens prevent stale reads after writes
5. Projection lag is measurable and bounded
6. Projection rebuilding produces identical results to incremental building
7. The demo application works end-to-end with CQRS
8. Read and write models can be independently scaled (demonstrated by adding more projection workers)

## Research Resources

- [CQRS (Martin Fowler)](https://martinfowler.com/bliki/CQRS.html) -- pattern overview
- [CQRS Documents (Greg Young)](https://cqrs.files.wordpress.com/2010/11/cqrs_documents.pdf) -- foundational document
- [Designing Data-Intensive Applications, Chapter 11-12](https://dataintensive.net/) -- event-driven architectures
- [Pat Helland: Life Beyond Distributed Transactions](https://queue.acm.org/detail.cfm?id=3025012) -- consistency without transactions

## What's Next

Continue to [20 - Distributed Transaction Coordinator](../20-distributed-transaction-coordinator/20-distributed-transaction-coordinator.md) to build a comprehensive transaction coordination system.

## Summary

- CQRS separates write (command) and read (query) models for independent optimization and scaling
- Events bridge the write and read sides, enabling multiple specialized projections
- Eventual consistency is inherent: read models lag behind the write model
- Causal consistency tokens enable read-your-writes guarantees
- Projection rebuilding supports schema evolution and bug fixes
- CQRS adds complexity but provides flexibility, scalability, and auditability
