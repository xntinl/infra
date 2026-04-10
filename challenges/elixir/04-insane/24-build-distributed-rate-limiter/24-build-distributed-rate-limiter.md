# 24. Build a Distributed Rate Limiter

**Difficulty**: Insane

---

## Prerequisites

- Elixir distributed node clustering (`:net_kernel`, `Node.connect/1`)
- GenServer, ETS, and `:persistent_term`
- Understanding of distributed consensus and quorum systems
- Monotonic clocks and NTP clock skew implications
- Elixir/OTP distributed messaging (`:rpc`, `GenServer.call` to remote nodes)
- Familiarity with token bucket and sliding window algorithms

---

## Problem Statement

Build a distributed rate limiter that remains correct under node failures, network partitions, and NTP clock skew. The system must:

1. Implement both token bucket (burst-tolerant) and sliding window (exact count) algorithms, selectable per account
2. Distribute state across a cluster so no single node holds the full picture, yet checks remain correct
3. Use quorum reads and writes so the system tolerates a minority of nodes failing without allowing excess requests through
4. Remain correct even when system clocks on different nodes differ by up to 100 milliseconds
5. Reduce cross-node coordination through a lease mechanism where a node acquires the right to approve up to K tokens for T seconds locally
6. Sustain 500k checks per second across a three-node cluster with P99 latency under 1 millisecond

---

## Acceptance Criteria

- [ ] Token bucket: each account has a bucket of capacity N tokens; tokens regenerate at rate R per second; requests consume tokens; the bucket never exceeds N
- [ ] Sliding window: count exact requests in the last T seconds using a structure that does not reset on window boundaries (no fixed-window approximation)
- [ ] Distributed state: account state is sharded across nodes using consistent hashing; no node stores all accounts; adding a node triggers rebalancing
- [ ] Quorum reads/writes: a check requires acknowledgement from `floor(N/2) + 1` nodes holding replicas of that account's shard; a single node failure does not allow over-limit requests
- [ ] Clock skew tolerance: the implementation accounts for up to 100ms of NTP drift; a token that was valid 50ms ago on another node is still counted correctly
- [ ] Lease-based local approval: a node can acquire a lease granting it authority to approve up to K requests for account A within T seconds without per-request coordination; the lease is released or expires safely
- [ ] Benchmark: a three-node cluster processes 500k `check/2` calls per second with P99 latency ≤ 1ms under sustained load; results verified with a load test script included in the project

---

## What You Will Learn

- Consistent hashing for stateless sharding without a coordinator
- Quorum protocols and their trade-offs (availability vs. consistency)
- Clock skew in distributed systems and why logical clocks matter
- Lease-based optimization to reduce coordination overhead
- High-performance Elixir: ETS vs. process state, avoiding mailbox bottlenecks
- Benchmarking distributed Elixir with Benchee and custom load generators

---

## Hints

- Research how Cloudflare implements rate limiting at edge with eventual consistency and local leases
- Study the difference between fixed window, sliding window log, and sliding window counter algorithms
- Investigate consistent hashing with virtual nodes to minimize resharding during topology changes
- Think carefully about what quorum means when replicas can be stale by up to one lease period
- Look into `:atomics` and `:counters` in Elixir for lock-free shared state within a node
- Research how Stripe handles the "thundering herd" when rate limit leases expire simultaneously

---

## Reference Material

- "How We Built Rate Limiting Capable of Scaling to Millions of Domains" — Cloudflare Blog
- Stripe Engineering Blog: rate limiting and idempotency
- "An Analysis of Hash Table Performance" — for consistent hashing internals
- Riak Core documentation on consistent hashing and vnodes
- Erlang `:atomics` and `:counters` module documentation

---

## Difficulty Rating ★★★★★★

Combining correctness under partial failure, clock skew, and sub-millisecond latency at high throughput is one of the hardest problems in distributed systems engineering.

---

## Estimated Time

60–90 hours
