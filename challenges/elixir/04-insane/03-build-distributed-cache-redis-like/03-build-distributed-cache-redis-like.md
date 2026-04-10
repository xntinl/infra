# 3. Build a Distributed Cache with Redis-Compatible Protocol

**Difficulty**: Insane

## Prerequisites
- Mastered: All Elixir/OTP intermediate and advanced concepts (GenServer, Supervisor, :gen_tcp, Ranch or raw socket programming)
- Mastered: Distributed systems data placement — consistent hashing, replication, quorum reads/writes
- Familiarity with: Redis internals (persistence modes, pub/sub semantics, RESP wire protocol), LRU eviction policies
- Reading: The Amazon Dynamo paper (DeCandia et al., 2007), the Redis source code (specifically `dict.c`, `ae.c`, `aof.c`)

## Problem Statement

Build a distributed, in-memory cache in Elixir/OTP that speaks a subset of the Redis protocol over TCP. A standard `redis-cli` binary must be able to connect and issue commands without knowing it is talking to Elixir. The system is multi-node: data is distributed across nodes using consistent hashing and replicated for fault tolerance.

Your system must implement:
1. A consistent hashing ring with virtual nodes (V virtual nodes per physical node, configurable). Each key hashes to exactly one primary node; the ring determines responsibility without any central coordinator
2. Replication with a configurable factor R: each key is stored on R consecutive nodes in the ring; reads and writes use quorum (R/2 + 1 acknowledgments required)
3. An LRU eviction policy: when the allocated memory budget is exceeded, evict the least-recently-used key; eviction must not require a full scan
4. TTL-based expiration: each key may carry an optional TTL; expired keys must be removed within ±100ms of their deadline via lazy expiration (on access) plus an active background sweep
5. A pub/sub subsystem: clients may SUBSCRIBE to channels; other clients may PUBLISH to channels; messages route across nodes to all subscribers regardless of which node they are connected to
6. Append-only file (AOF) persistence: every write command is appended to an AOF log on disk before acknowledging the client; on startup, the AOF is replayed in order to restore state
7. Sloppy quorum with hinted handoff: if a target replica is down, route the write to the next available node with a "hint" annotation; when the target recovers, the hinted write is forwarded
8. The RESP2 wire protocol (Redis Serialization Protocol) over raw TCP sockets — every supported command returns exactly the RESP encoding `redis-cli` expects

## Acceptance Criteria

- [ ] **RESP protocol**: `redis-cli -p 6380 SET foo bar` returns `+OK`; `redis-cli -p 6380 GET foo` returns `bar`; `redis-cli -p 6380 DEL foo` returns `:1`; all in correct RESP encoding
- [ ] **Consistent hashing distribution**: With 3 nodes and 150 virtual nodes each, insert 100,000 keys and confirm no single node holds more than 40% of all keys (uniform distribution)
- [ ] **Quorum replication**: With R=2, write a key, immediately kill one replica node, read the key — it must still return the correct value (surviving replica serves the read)
- [ ] **LRU eviction**: Set `max_memory` to 100MB; insert enough keys to exceed the limit; confirm older, less-recently-accessed keys are evicted before newer ones; confirm the cache never exceeds the memory budget
- [ ] **TTL expiration**: `SET foo bar EX 1` — after 1000ms ±100ms, `GET foo` returns nil; verify with 10,000 keys having varying TTLs that no key survives past its deadline by more than 100ms
- [ ] **Pub/sub cross-node**: Client A connects to node 1 and subscribes to channel "news"; Client B connects to node 3 and publishes to "news"; Client A receives the message without polling
- [ ] **AOF persistence**: Write 1,000 keys; kill all nodes abruptly (no graceful shutdown); restart all nodes; confirm all 1,000 keys are present after AOF replay
- [ ] **Network partition — sloppy quorum**: Take one replica offline; write to the shard it owns; confirm the hinted handoff node accepts the write; bring the replica back; confirm it receives the hinted write automatically
- [ ] **Benchmark — reads**: Sustain 100,000 read operations per second on a 3-node cluster from a single client machine
- [ ] **Benchmark — writes**: Sustain 50,000 write operations per second on a 3-node cluster with AOF enabled and R=2 quorum

## What You Will Learn
- How consistent hashing distributes keys without any central directory — and why virtual nodes are essential for uniform balance
- The difference between strict quorum and sloppy quorum (Dynamo-style), and the eventual consistency trade-off each makes
- How to implement a true O(1) LRU cache using a doubly-linked list + hash map (not `:queue`, not ETS ordered_set alone)
- How `redis-cli` frames commands in RESP2 and why the protocol's simplicity enables high throughput
- The read-repair and anti-entropy mechanisms that keep replicas consistent after partial failures
- How append-only files work as a durability mechanism and the trade-off versus RDB snapshots
- How pub/sub systems route messages across nodes without a centralized broker — and why this is hard to make reliable

## Hints

This exercise is intentionally sparse. You are expected to:
- Read the RESP protocol specification before writing any socket code — every byte matters when `redis-cli` is your integration test
- Study the Dynamo paper's "Partitioning" and "Replication" sections carefully before touching consistent hashing code
- LRU in a concurrent system requires careful design: ETS does not give you LRU ordering for free; you need to track access order yourself
- AOF replay must be idempotent — if a write was partially flushed before crash, your parser must detect and skip the truncated entry
- Build a clock-wheel or hierarchical timing wheel for TTL expiration — a naive timer-per-key approach will not survive 1M keys

## Reference Material (Research Required)
- DeCandia, G. et al. (2007). *Dynamo: Amazon's Highly Available Key-Value Store* — the canonical reference for consistent hashing, quorum, and hinted handoff
- Redis RESP2 protocol specification — https://redis.io/docs/reference/protocol-spec — study the wire encoding in full detail
- Redis source code — `src/dict.c` (hash table), `src/aof.c` (AOF persistence), `src/pubsub.c` (pub/sub routing) — do not look at tutorials, read the C source
- Karger, D. et al. (1997). *Consistent Hashing and Random Trees* — the original consistent hashing paper from MIT

## Difficulty Rating
★★★★★★

## Estimated Time
4–7 weeks for an experienced Elixir developer with networking and distributed systems background
