# 11. Build a Custom Distributed Event Bus and Registry

**Difficulty**: Insane

## Prerequisites

- Mastered: GenServer, ETS, Process monitoring, distributed Elixir basics (`:net_kernel`, `Node.connect/1`)
- Mastered: Pattern matching on complex data structures, recursive algorithms
- Familiarity with: AMQP topic exchange routing semantics, vector clocks, distributed systems consistency models

## Problem Statement

Build a distributed process registry and hierarchical event bus in Elixir that operates
across a multi-node cluster without any external dependencies (no Redis, no RabbitMQ,
no libcluster):

1. Implement a `Registry` that maps atom or string names to PIDs. Lookup must be O(1).
   When a registered process dies, its entry must be automatically removed.
2. Implement a `EventBus` with topic hierarchies. Topics are dot-separated strings
   such as `"orders.eu.created"`. A subscriber to `"orders.eu.*"` receives all events
   published under that prefix. A subscriber to `"orders.#"` receives events at any
   depth under `"orders"`.
3. `"*"` matches exactly one segment. `"#"` matches zero or more segments.
4. The system must work across multiple BEAM nodes. A subscription made on node A must
   receive events published from node B.
5. Each topic maintains a configurable history buffer of the last N events. A new
   subscriber can request a replay of that history upon joining.
6. Implement three Quality of Service delivery modes:
   - `:at_most_once` — publish and forget, no retries
   - `:at_least_once` — publisher retries until subscriber acknowledges
   - `:exactly_once` — two-phase commit protocol between publisher and subscriber
7. When a slow subscriber cannot keep up with the publisher, backpressure must activate:
   the publisher either blocks, drops messages, or receives an error, based on
   a configurable overflow strategy.

## Acceptance Criteria

- [ ] `Registry.register(name, pid)` stores the mapping; `Registry.lookup(name)` returns
      `{:ok, pid}` or `{:error, :not_found}` in O(1) average time using ETS.
- [ ] On process death, the registry entry is removed within one monitor cycle — no stale
      entries survive a garbage collection pass.
- [ ] `EventBus.subscribe("orders.eu.*", self())` receives `"orders.eu.created"` and
      `"orders.eu.updated"` but NOT `"orders.us.created"` or `"orders.eu.refunds.issued"`.
- [ ] `EventBus.subscribe("orders.#", self())` receives events at all depths under
      `"orders"` including `"orders"`, `"orders.eu"`, and `"orders.eu.created"`.
- [ ] Wildcard resolution runs in O(S) time where S is the number of active subscriptions,
      not O(T) where T is total topic string length.
- [ ] Publishing from node B delivers to subscribers on node A with no manual routing code.
- [ ] `EventBus.subscribe("metrics.cpu", self(), replay: 50)` delivers the last 50 events
      before delivering new ones.
- [ ] `:at_least_once` delivery retries up to a configurable max with exponential backoff
      until the subscriber replies with `{:ack, event_id}`.
- [ ] `:exactly_once` delivery uses a prepare/commit protocol and is idempotent on the
      subscriber side.
- [ ] When a subscriber mailbox exceeds the configured threshold, the publisher receives
      `{:error, :backpressure}` or blocks depending on the overflow strategy.

## What You Will Learn

- How to implement wildcard trie routing for topic hierarchies efficiently
- The operational semantics of at-least-once vs exactly-once delivery and their cost tradeoffs
- How `pg` (process groups) and `:global` work under the hood — and where they fall short
- Distributed ETS limitations and why registry replication requires explicit gossip or CRDTs
- Backpressure implementation patterns: bounded mailboxes, process monitoring for slow consumers
- The two-phase commit protocol adapted for asynchronous message-passing systems

## Hints

This exercise is intentionally sparse. Research:

- Implement topic matching with a trie where each segment is a trie node; `"*"` and `"#"` are special node keys
- For distributed operation, look at `:pg` (Erlang 23+) as a reference, but do not use it — replicate its internal gossip mechanism
- Exactly-once delivery in distributed systems requires a correlation ID per message and idempotency keys on the receiver
- `GenServer.call` with a timeout is a natural fit for the blocking backpressure strategy; consider using a bounded queue GenServer as a proxy
- History buffers can be implemented as a circular buffer in ETS using a counter key and modular arithmetic

## Reference Material

- AMQP 0-9-1 Model Explained (topic exchange routing): https://www.rabbitmq.com/tutorials/amqp-concepts
- Erlang `:pg` source: `lib/kernel/src/pg.erl`
- "Distributed Systems" — Maarten van Steen & Andrew Tanenbaum, Chapter 6 (Naming)
- "Exactly-once semantics in Apache Kafka" — Narkhede et al., 2017
- Erlang `:global` module documentation and source

## Difficulty Rating

★★★★★★

## Estimated Time

40–55 hours
