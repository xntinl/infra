# 18. Event Sourcing Engine

<!--
difficulty: insane
concepts: [event-sourcing, event-store, aggregate, projection, snapshot, event-replay, append-only-log, domain-events]
tools: [go]
estimated_time: 3h
bloom_level: create
prerequisites: [distributed-locking, saga-orchestrator]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of domain-driven design concepts (aggregates, events)
- Familiarity with append-only data structures

## Learning Objectives

- **Create** an event sourcing engine with an event store, aggregate reconstruction, and projections
- **Analyze** the tradeoffs of event sourcing vs state-based persistence
- **Evaluate** snapshot strategies for efficient aggregate loading

## The Challenge

Event sourcing stores every state change as an immutable event in an append-only log. The current state of an entity (aggregate) is reconstructed by replaying all its events from the beginning. This provides a complete audit trail, enables temporal queries ("what was the state at time T?"), and supports multiple read models (projections) built from the same event stream.

Build an event sourcing engine that stores events, reconstructs aggregates, supports snapshotting for performance, and projects events into read-optimized views.

## Requirements

1. Implement an `EventStore` that persists events with metadata:
   - `Append(aggregateID string, events []Event, expectedVersion int) error` -- optimistic concurrency via expected version
   - `Load(aggregateID string, fromVersion int) ([]Event, error)` -- load events for an aggregate
   - `Subscribe(handler func(Event))` -- subscribe to new events for projections
2. Implement an `Event` type with aggregate ID, event type, version, timestamp, and payload
3. Implement an `Aggregate` interface that applies events to reconstruct state:
   - `Apply(event Event)` -- apply a single event to update state
   - `Handle(command Command) ([]Event, error)` -- process a command and produce events
4. Implement a concrete aggregate (e.g., `BankAccount`): handle commands like Deposit, Withdraw, Transfer; produce events like MoneyDeposited, MoneyWithdrawn
5. Implement snapshotting: periodically save the aggregate state so reconstruction does not require replaying all events from the beginning
6. Implement projections: subscribe to events and build read-optimized views (e.g., account balance summary, transaction history)
7. Implement temporal queries: reconstruct the aggregate state at any point in time by replaying events up to that timestamp
8. Implement optimistic concurrency: detect conflicting writes when two processes try to append events to the same aggregate concurrently
9. Write tests for: event persistence, aggregate reconstruction, snapshot loading, projection accuracy, concurrent write conflict detection, temporal queries

## Hints

- The event store can be backed by an in-memory slice or file-based storage. Use a map of `aggregateID -> []Event`.
- Optimistic concurrency: the client specifies the expected version (last seen event number). If the store's current version differs, reject the append. This prevents lost updates.
- Snapshots: save `{aggregateID, version, state}`. To load an aggregate: load the latest snapshot, then replay events from `snapshot.version + 1` to current.
- Projections are eventually consistent read models. A projection subscribes to the event stream and maintains its own materialized view.
- Use Go interfaces for the event payload: `type Event struct { Type string; Data interface{} }`. Use type switches in `Apply`.
- For temporal queries, replay events up to the requested timestamp. This is a key advantage of event sourcing over state-based persistence.
- The command handler should validate business rules against the current state before producing events.

## Success Criteria

1. Events are persisted in append-only order with correct versioning
2. Aggregates reconstruct correctly from their event streams
3. Optimistic concurrency rejects conflicting writes
4. Snapshots reduce aggregate loading time for long event streams
5. Projections build accurate read models from the event stream
6. Temporal queries return correct historical state
7. The bank account example handles deposits, withdrawals, and concurrent operations correctly
8. All events are immutable -- no updates or deletes

## Research Resources

- [Event Sourcing (Martin Fowler)](https://martinfowler.com/eaaDev/EventSourcing.html) -- pattern overview
- [Event Store (Greg Young)](https://www.eventstore.com/) -- dedicated event sourcing database
- [CQRS and Event Sourcing (Greg Young)](https://cqrs.files.wordpress.com/2010/11/cqrs_documents.pdf) -- foundational document
- [Designing Data-Intensive Applications, Chapter 11](https://dataintensive.net/) -- event-driven data flows
- [Versioning in an Event Sourced System (Young)](https://leanpub.com/esversioning) -- handling event schema evolution

## What's Next

Continue to [19 - CQRS and Eventual Consistency](../19-cqrs-eventual-consistency/19-cqrs-eventual-consistency.md) to implement the Command Query Responsibility Segregation pattern.

## Summary

- Event sourcing stores state changes as immutable events in an append-only log
- Aggregate state is reconstructed by replaying events
- Optimistic concurrency prevents conflicting writes through version checking
- Snapshots optimize aggregate loading for long event histories
- Projections build read-optimized views from the event stream
- Temporal queries reconstruct historical state by replaying events to a point in time
- Event sourcing provides audit trails, debugging, and flexibility at the cost of complexity
