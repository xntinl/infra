# 21. Build a Message Broker (AMQP-like)

**Difficulty**: Insane

---

## Prerequisites

- Elixir processes, GenServer, Supervisor trees
- TCP socket programming with `:gen_tcp`
- Binary pattern matching and protocol parsing
- ETS tables for concurrent state
- OTP fault tolerance and restart strategies
- Understanding of messaging patterns (pub/sub, point-to-point, routing)

---

## Problem Statement

Build a message broker that implements a meaningful subset of the AMQP 0-9-1 protocol. The broker must:

1. Accept TCP connections from AMQP 0-9-1 compatible clients (e.g., the `amqp` Elixir library)
2. Implement the full exchange-binding-queue topology so publishers and consumers are fully decoupled
3. Guarantee message delivery semantics through publisher confirms and consumer acknowledgements
4. Persist durable messages and queues so they survive broker restarts
5. Route undeliverable or expired messages to a configurable dead letter exchange
6. Handle slow or failed consumers gracefully without blocking the entire queue
7. Support wildcard topic routing using the `*` (one word) and `#` (zero or more words) AMQP conventions

---

## Acceptance Criteria

- [ ] Exchanges: implement `direct`, `fanout`, and `topic` exchange types with correct routing semantics
- [ ] Queues: support named queues with `durable`, `exclusive`, and `auto-delete` flags; queues persist across broker restarts when marked durable
- [ ] Bindings: any exchange can be bound to any queue with a routing key; topic wildcards `*` and `#` are correctly evaluated
- [ ] Publisher confirms: broker sends a `basic.ack` or `basic.nack` back to the publisher confirming the message was enqueued (or rejected)
- [ ] Consumer acks: broker holds unacknowledged messages in-flight; messages are requeued if the consumer disconnects or sends `basic.nack` with `requeue=true`
- [ ] Dead letter exchange: queues accept a `x-dead-letter-exchange` argument; messages that are rejected without requeue, or expire via TTL, are forwarded to that exchange
- [ ] Message TTL: queues accept `x-message-ttl`; messages older than the TTL are removed and dead-lettered if a DLX is configured
- [ ] Durability: messages published as `delivery_mode=2` survive a broker restart when the target queue is also durable
- [ ] AMQP 0-9-1 wire protocol: a real AMQP client (`amqp` library) connects, declares resources, publishes, and consumes without modification

---

## What You Will Learn

- Binary protocol parsing and framing in Elixir
- Process-per-connection architecture with supervision
- ETS and DETS for concurrent, persistent state
- Implementing reliability guarantees (at-least-once delivery)
- Pattern matching for wildcard routing (trie or recursive matching)
- Back-pressure and flow control in message systems
- OTP restart strategies for stateful services

---

## Hints

- Research the AMQP 0-9-1 frame structure: frame type, channel, size, payload, frame-end sentinel
- Study how RabbitMQ uses a process per channel, not per connection
- Investigate how to store messages durably with DETS or a write-ahead log
- Look into how topic matching with `#` and `*` is implemented efficiently (routing trie)
- Think about what happens to in-flight messages when a consumer crashes mid-processing
- Research the difference between `basic.reject` and `basic.nack` in AMQP

---

## Reference Material

- AMQP 0-9-1 Complete Reference Card (rabbitmq.com)
- "RabbitMQ in Action" — Videla & Williams
- RabbitMQ source code: `rabbit_exchange_type_topic.erl` for wildcard routing
- AMQP 0-9-1 Protocol Specification (OASIS)
- Erlang `gen_tcp` documentation for binary mode socket handling

---

## Difficulty Rating ★★★★★★

Protocol implementation combined with stateful reliability guarantees makes this one of the most demanding distributed systems exercises in the curriculum.

---

## Estimated Time

60–100 hours
